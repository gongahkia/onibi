from __future__ import annotations

from collections import Counter
from datetime import UTC, datetime
from hashlib import sha256
from pathlib import Path
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from piranesi import __version__
from piranesi.host.api import load_host_report
from piranesi.host.models import HostFinding, HostPostureReport

RemediationFormat = Literal["json", "markdown"]

_SEVERITY_RANK = {
    "informational": 0,
    "low": 1,
    "medium": 2,
    "high": 3,
    "critical": 4,
}


class RemediationAction(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    priority: int
    title: str
    category: str
    severity: str
    related_finding_ids: list[str] = Field(default_factory=list)
    owner: str = "TODO: assign owner"
    estimated_effort: str
    risk_reduction_estimate: float
    remediation: str
    verification_command: str
    rollback_considerations: str
    dependencies: list[str] = Field(default_factory=list)


class RemediationPlan(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: int = 1
    generated_at: str = Field(default_factory=lambda: datetime.now(UTC).isoformat())
    piranesi_version: str = __version__
    target: str
    source_report: str | None = None
    summary: dict[str, object] = Field(default_factory=dict)
    actions: list[RemediationAction] = Field(default_factory=list)


class HostChangedFinding(BaseModel):
    model_config = ConfigDict(extra="forbid")

    match_key: str
    changed_fields: list[str] = Field(default_factory=list)
    before: HostFinding
    after: HostFinding


class HostDiffResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: int = 1
    before_target: str
    after_target: str
    summary: dict[str, int] = Field(default_factory=dict)
    new: list[HostFinding] = Field(default_factory=list)
    fixed: list[HostFinding] = Field(default_factory=list)
    changed: list[HostChangedFinding] = Field(default_factory=list)
    unchanged: list[HostFinding] = Field(default_factory=list)
    suppressed: list[HostFinding] = Field(default_factory=list)


class RemediationVerification(BaseModel):
    model_config = ConfigDict(extra="forbid")

    passed: bool
    summary: dict[str, int]
    diff: HostDiffResult
    message: str


def build_remediation_plan(
    report_or_path: HostPostureReport | str | Path,
    *,
    source_report: str | Path | None = None,
) -> RemediationPlan:
    report = _coerce_report(report_or_path)
    active_findings = [finding for finding in report.findings if not finding.suppressed]
    ranked = sorted(active_findings, key=_finding_priority_sort_key)
    actions = [
        _action_for_finding(report, finding, index + 1) for index, finding in enumerate(ranked)
    ]
    total_risk_reduction = round(sum(action.risk_reduction_estimate for action in actions), 1)
    return RemediationPlan(
        target=report.target,
        source_report=None if source_report is None else str(source_report),
        summary={
            "action_count": len(actions),
            "finding_count": len(active_findings),
            "suppressed_finding_count": sum(1 for finding in report.findings if finding.suppressed),
            "estimated_total_risk_reduction": total_risk_reduction,
            "by_severity": dict(Counter(finding.severity for finding in active_findings)),
        },
        actions=actions,
    )


def write_remediation_plan(
    report_path: str | Path,
    output_path: str | Path,
) -> tuple[Path, Path]:
    output = Path(output_path).expanduser().resolve(strict=False)
    markdown_path = output
    if markdown_path.suffix == "":
        markdown_path.mkdir(parents=True, exist_ok=True)
        markdown_path = markdown_path / "remediation-plan.md"
    json_path = markdown_path.with_suffix(".json")
    plan = build_remediation_plan(report_path, source_report=report_path)
    markdown_path.parent.mkdir(parents=True, exist_ok=True)
    markdown_path.write_text(render_remediation_plan_markdown(plan), encoding="utf-8")
    json_path.write_text(plan.model_dump_json(indent=2), encoding="utf-8")
    return markdown_path, json_path


def diff_host_reports(
    before: HostPostureReport | str | Path,
    after: HostPostureReport | str | Path,
) -> HostDiffResult:
    before_report = _coerce_report(before)
    after_report = _coerce_report(after)
    before_unmatched = list(before_report.findings)
    after_unmatched = list(after_report.findings)

    new: list[HostFinding] = []
    fixed: list[HostFinding] = []
    changed: list[HostChangedFinding] = []
    unchanged: list[HostFinding] = []
    suppressed: list[HostFinding] = []

    for before_finding in list(before_unmatched):
        match = _pop_best_match(before_finding, after_unmatched, before_report, after_report)
        before_unmatched.remove(before_finding)
        if match is None:
            if before_finding.suppressed:
                suppressed.append(before_finding)
            else:
                fixed.append(before_finding)
            continue
        if match.suppressed or before_finding.suppressed:
            suppressed.append(match)
            continue
        changed_fields = _changed_fields(before_finding, match)
        if changed_fields:
            changed.append(
                HostChangedFinding(
                    match_key=_finding_match_key(match, after_report),
                    changed_fields=changed_fields,
                    before=before_finding,
                    after=match,
                )
            )
        else:
            unchanged.append(match)

    for after_finding in after_unmatched:
        if after_finding.suppressed:
            suppressed.append(after_finding)
        else:
            new.append(after_finding)

    summary = {
        "new": len(new),
        "fixed": len(fixed),
        "changed": len(changed),
        "unchanged": len(unchanged),
        "suppressed": len(suppressed),
    }
    return HostDiffResult(
        before_target=before_report.target,
        after_target=after_report.target,
        summary=summary,
        new=sorted(new, key=_host_finding_sort_key),
        fixed=sorted(fixed, key=_host_finding_sort_key),
        changed=sorted(changed, key=lambda item: item.match_key),
        unchanged=sorted(unchanged, key=_host_finding_sort_key),
        suppressed=sorted(suppressed, key=_host_finding_sort_key),
    )


def verify_remediation(
    before: HostPostureReport | str | Path,
    after: HostPostureReport | str | Path,
) -> RemediationVerification:
    diff = diff_host_reports(before, after)
    passed = diff.summary["new"] == 0 and diff.summary["changed"] == 0
    fixed = diff.summary["fixed"]
    message = (
        f"remediation verification passed: {fixed} finding(s) fixed"
        if passed
        else (
            "remediation verification needs review: "
            f"{diff.summary['new']} new and {diff.summary['changed']} changed finding(s)"
        )
    )
    return RemediationVerification(
        passed=passed,
        summary=dict(diff.summary),
        diff=diff,
        message=message,
    )


def render_remediation_plan_markdown(plan: RemediationPlan) -> str:
    lines = [
        "# Piranesi Remediation Plan",
        "",
        f"- Target: `{plan.target}`",
        f"- Generated: `{plan.generated_at}`",
        f"- Actions: **{plan.summary.get('action_count', 0)}**",
        f"- Estimated risk reduction: **{plan.summary.get('estimated_total_risk_reduction', 0)}**",
        "",
        "## Prioritized Actions",
        "",
    ]
    if not plan.actions:
        lines.append("No active findings require remediation.")
        return "\n".join(lines).rstrip() + "\n"
    for action in plan.actions:
        lines.extend(
            [
                f"### {action.priority}. {action.title}",
                "",
                f"- Owner: {action.owner}",
                f"- Severity: `{action.severity}`",
                f"- Category: `{action.category}`",
                f"- Related findings: {', '.join(action.related_finding_ids)}",
                f"- Estimated effort: {action.estimated_effort}",
                f"- Risk reduction estimate: {action.risk_reduction_estimate:.1f}",
                f"- Verification command: `{action.verification_command}`",
                "- Dependencies: "
                f"{', '.join(action.dependencies) if action.dependencies else 'none'}",
                f"- Rollback: {action.rollback_considerations}",
                "",
                action.remediation,
                "",
            ]
        )
    return "\n".join(lines).rstrip() + "\n"


def render_remediation_checklist(
    report_or_path: HostPostureReport | str | Path,
    *,
    output_format: RemediationFormat = "markdown",
) -> str:
    plan = build_remediation_plan(report_or_path)
    if output_format == "json":
        return plan.model_dump_json(indent=2) + "\n"
    lines = [
        "# Piranesi Remediation Checklist",
        "",
        f"Target: `{plan.target}`",
        "",
    ]
    if not plan.actions:
        lines.append("- [ ] No active findings require remediation.")
        return "\n".join(lines).rstrip() + "\n"
    current_category: str | None = None
    for action in plan.actions:
        if action.category != current_category:
            current_category = action.category
            lines.extend(["", f"## {current_category.title()}", ""])
        lines.append(
            "- [ ] "
            f"{action.title} ({', '.join(action.related_finding_ids)}) "
            f"- owner: {action.owner}; verify: `{action.verification_command}`"
        )
    return "\n".join(lines).rstrip() + "\n"


def render_host_diff_markdown(diff: HostDiffResult) -> str:
    lines = [
        "# Piranesi Host Diff",
        "",
        f"- Before target: `{diff.before_target}`",
        f"- After target: `{diff.after_target}`",
        (
            f"- New: **{diff.summary['new']}** | Fixed: **{diff.summary['fixed']}** | "
            f"Changed: **{diff.summary['changed']}** | "
            f"Unchanged: **{diff.summary['unchanged']}** | "
            f"Suppressed: **{diff.summary['suppressed']}**"
        ),
        "",
    ]
    lines.extend(_diff_section("New", diff.new))
    lines.extend(_diff_section("Fixed", diff.fixed))
    lines.extend(_changed_section(diff.changed))
    lines.extend(_diff_section("Unchanged", diff.unchanged))
    lines.extend(_diff_section("Suppressed", diff.suppressed))
    return "\n".join(lines).rstrip() + "\n"


def render_remediation_verification_markdown(verification: RemediationVerification) -> str:
    lines = [
        "# Piranesi Remediation Verification",
        "",
        f"- Passed: `{str(verification.passed).lower()}`",
        f"- Message: {verification.message}",
        (
            f"- New: **{verification.summary['new']}** | "
            f"Fixed: **{verification.summary['fixed']}** | "
            f"Changed: **{verification.summary['changed']}** | "
            f"Unchanged: **{verification.summary['unchanged']}** | "
            f"Suppressed: **{verification.summary['suppressed']}**"
        ),
    ]
    return "\n".join(lines).rstrip() + "\n"


def _coerce_report(report_or_path: HostPostureReport | str | Path) -> HostPostureReport:
    if isinstance(report_or_path, HostPostureReport):
        return report_or_path
    return load_host_report(report_or_path)


def _action_for_finding(
    report: HostPostureReport,
    finding: HostFinding,
    priority: int,
) -> RemediationAction:
    title = _action_title(finding)
    return RemediationAction(
        id=_action_id(report, finding),
        priority=priority,
        title=title,
        category=finding.category,
        severity=finding.severity,
        related_finding_ids=[finding.id],
        estimated_effort=_estimated_effort(finding),
        risk_reduction_estimate=_risk_reduction(finding),
        remediation=finding.remediation,
        verification_command=_verification_command(finding),
        rollback_considerations=_rollback_considerations(finding),
        dependencies=_dependencies_for_finding(finding),
    )


def _action_id(report: HostPostureReport, finding: HostFinding) -> str:
    material = "|".join([report.target, finding.id, finding.rule_id or "", finding.title])
    return "remediate-" + sha256(material.encode("utf-8")).hexdigest()[:12]


def _action_title(finding: HostFinding) -> str:
    if finding.category == "coverage":
        return f"Collect evidence for {finding.title}"
    return f"Remediate {finding.title}"


def _estimated_effort(finding: HostFinding) -> str:
    if finding.category == "coverage":
        return "small"
    if finding.rule_id in {
        "host.listener.high_risk_service",
        "host.firewall.inactive_public_services",
        "host.baseline.openscap",
    }:
        return "medium"
    if finding.severity in {"critical", "high"}:
        return "medium"
    return "small"


def _risk_reduction(finding: HostFinding) -> float:
    risk = finding.risk.total if finding.risk is not None else _SEVERITY_RANK[finding.severity] * 10
    return round(min(100.0, risk), 1)


def _verification_command(finding: HostFinding) -> str:
    if finding.rule_id == "host.cve.trivy":
        return (
            "piranesi collect --output piranesi-evidence --trivy && "
            "piranesi assess piranesi-evidence"
        )
    if finding.rule_id and finding.rule_id.startswith("host.ssh."):
        return (
            "piranesi collect --output piranesi-evidence --no-trivy && "
            "piranesi assess piranesi-evidence"
        )
    if finding.rule_id and finding.rule_id.startswith("host.listener."):
        return (
            "piranesi collect --output piranesi-evidence --no-trivy && "
            "piranesi assess piranesi-evidence"
        )
    if finding.category == "coverage":
        return "piranesi collect --output piranesi-evidence && piranesi assess piranesi-evidence"
    return "piranesi assess piranesi-evidence --output piranesi-output"


def _rollback_considerations(finding: HostFinding) -> str:
    if finding.rule_id == "host.cve.trivy":
        return "Keep package manager logs and note the previous package version before upgrade."
    if finding.rule_id and finding.rule_id.startswith("host.ssh."):
        return "Keep a break-glass console session and back up sshd_config before changes."
    if finding.rule_id and finding.rule_id.startswith("host.listener."):
        return "Record current listener/service state before disabling or firewalling the service."
    return "Document the pre-change state and keep a copy of edited configuration files."


def _dependencies_for_finding(finding: HostFinding) -> list[str]:
    if finding.rule_id == "host.listener.ssh_public":
        return ["Confirm firewall policy before changing SSH exposure."]
    if finding.rule_id and finding.rule_id.startswith("host.ssh."):
        return ["Confirm an alternate admin access path before SSH hardening."]
    if finding.category == "coverage":
        return ["Install or enable the missing evidence collector before verification."]
    return []


def _finding_priority_sort_key(finding: HostFinding) -> tuple[float, int, str]:
    risk = finding.risk.total if finding.risk is not None else 0.0
    return (-risk, -_SEVERITY_RANK[finding.severity], finding.id)


def _pop_best_match(
    before: HostFinding,
    after_candidates: list[HostFinding],
    before_report: HostPostureReport,
    after_report: HostPostureReport,
) -> HostFinding | None:
    for index, candidate in enumerate(after_candidates):
        if candidate.id == before.id:
            return after_candidates.pop(index)
    before_keys = _finding_match_keys(before, before_report)
    for index, candidate in enumerate(after_candidates):
        if before_keys & _finding_match_keys(candidate, after_report):
            return after_candidates.pop(index)
    return None


def _finding_match_keys(
    finding: HostFinding,
    report: HostPostureReport,
) -> set[str]:
    keys = {_finding_match_key(finding, report)}
    if finding.rule_id and finding.instance_key:
        keys.add(
            "|".join(
                [
                    "rule-instance",
                    report.target,
                    finding.rule_id,
                    finding.instance_key,
                ]
            )
        )
    if finding.rule_id and finding.affected_component:
        keys.add(
            "|".join(
                [
                    "rule-component",
                    report.target,
                    finding.rule_id,
                    finding.affected_component,
                ]
            )
        )
    return keys


def _finding_match_key(finding: HostFinding, report: HostPostureReport) -> str:
    return "|".join(
        [
            report.target,
            finding.rule_id or "",
            finding.instance_key or "",
            finding.affected_component or "",
        ]
    )


def _changed_fields(before: HostFinding, after: HostFinding) -> list[str]:
    fields: list[str] = []
    for name in ("title", "severity", "confidence", "remediation", "category"):
        if getattr(before, name) != getattr(after, name):
            fields.append(name)
    if _risk_reduction(before) != _risk_reduction(after):
        fields.append("risk")
    return fields


def _host_finding_sort_key(finding: HostFinding) -> tuple[int, str, str]:
    return (-_SEVERITY_RANK[finding.severity], finding.rule_id or "", finding.id)


def _diff_section(title: str, findings: list[HostFinding]) -> list[str]:
    lines = [f"## {title}", ""]
    if not findings:
        lines.append("No findings.")
        lines.append("")
        return lines
    for finding in findings:
        lines.append(f"- `{finding.id}` {finding.severity} {finding.title}")
    lines.append("")
    return lines


def _changed_section(changed: list[HostChangedFinding]) -> list[str]:
    lines = ["## Changed", ""]
    if not changed:
        lines.append("No findings.")
        lines.append("")
        return lines
    for item in changed:
        lines.append(
            "- "
            f"`{item.after.id}` {item.after.severity} {item.after.title} "
            f"(changed: {', '.join(item.changed_fields)})"
        )
    lines.append("")
    return lines


__all__ = [
    "HostChangedFinding",
    "HostDiffResult",
    "RemediationAction",
    "RemediationPlan",
    "RemediationVerification",
    "build_remediation_plan",
    "diff_host_reports",
    "render_host_diff_markdown",
    "render_remediation_checklist",
    "render_remediation_plan_markdown",
    "render_remediation_verification_markdown",
    "verify_remediation",
    "write_remediation_plan",
]
