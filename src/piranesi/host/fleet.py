from __future__ import annotations

from collections import Counter
from collections.abc import Sequence
from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import Path

from piranesi.detect.suppression import SuppressionRule, load_ignore_file_with_diagnostics
from piranesi.host.analyze import AnalysisSelection, analyze_snapshot
from piranesi.host.ingest import HostInputError, load_host_input
from piranesi.host.models import FleetHostSummary, FleetReport, HostFinding, HostPostureReport
from piranesi.host.policy import HostPolicy, apply_fleet_policy, apply_host_policy
from piranesi.host.report import write_fleet_report_outputs, write_host_report_outputs
from piranesi.host.suppression import apply_host_suppressions
from piranesi.llm.provider import LLMProvider

_SEVERITIES = ("critical", "high", "medium", "low", "informational")
_SEVERITY_RANK = {
    "informational": 0,
    "low": 1,
    "medium": 2,
    "high": 3,
    "critical": 4,
}


class FleetInputError(ValueError):
    """Raised when the top-level fleet input cannot be assessed."""


@dataclass(frozen=True, slots=True)
class FleetAssessResult:
    report: FleetReport
    host_reports: dict[str, HostPostureReport]
    fail_fast_triggered: bool = False


def assess_fleet_evidence(
    fleet_evidence: str | Path,
    output_dir: str | Path,
    *,
    analysis: AnalysisSelection = "deterministic",
    provider: LLMProvider | None = None,
    report_format: str = "both",
    fail_fast: bool = False,
    treat_private_as_public: bool = False,
    root_suppression_rules: Sequence[SuppressionRule] = (),
    policy: HostPolicy | None = None,
    jobs: int = 1,
) -> FleetAssessResult:
    _ = jobs
    evidence_root = Path(fleet_evidence)
    if not evidence_root.is_dir():
        raise FleetInputError(f"fleet evidence directory does not exist: {evidence_root}")
    output_path = Path(output_dir)
    output_path.mkdir(parents=True, exist_ok=True)
    children = _fleet_children(evidence_root)
    host_reports: dict[str, HostPostureReport] = {}
    host_summaries: list[FleetHostSummary] = []
    fail_fast_triggered = False
    for child in children:
        try:
            child_rules = _child_suppression_rules(child)
            snapshot = load_host_input(child)
            report = analyze_snapshot(
                snapshot,
                analysis=analysis,
                provider=provider,
                treat_private_as_public=treat_private_as_public,
            )
            report = apply_host_suppressions(
                report,
                [*root_suppression_rules, *child_rules],
            )
            if policy is not None:
                report = apply_host_policy(report, policy)
            host_output = output_path / "hosts" / child.name
            write_host_report_outputs(report, host_output, report_format=report_format)
            host_summaries.append(_successful_host_summary(child, report))
            host_reports[child.name] = report
        except Exception as exc:
            host_summaries.append(_failed_host_summary(child, exc))
            if fail_fast:
                fail_fast_triggered = True
                break
    fleet_report = build_fleet_report(host_summaries, host_reports)
    if policy is not None:
        fleet_report = apply_fleet_policy(fleet_report, host_reports, policy)
    write_fleet_report_outputs(fleet_report, output_path, report_format=report_format)
    return FleetAssessResult(
        report=fleet_report,
        host_reports=host_reports,
        fail_fast_triggered=fail_fast_triggered,
    )


def build_fleet_report(
    hosts: list[FleetHostSummary],
    host_reports: dict[str, HostPostureReport],
) -> FleetReport:
    success_hosts = [host for host in hosts if host.status == "ok"]
    failed_hosts = [host for host in hosts if host.status == "error"]
    severity_counts: Counter[str] = Counter()
    total_findings = 0
    for host in success_hosts:
        total_findings += host.findings_total
        severity_counts.update(host.by_severity)
    summary = {
        "findings_total": total_findings,
        "by_severity": _ordered_counts(severity_counts),
        "worst_hosts": _worst_hosts(success_hosts),
        "highest_risk_findings": _highest_risk_findings(host_reports),
        "failed_hosts": [
            {
                "target": host.target,
                "evidence_path": host.evidence_path,
                "error": host.error,
            }
            for host in failed_hosts
        ],
        "evidence_gaps_by_host": _evidence_gaps_by_host(host_reports),
        "tool_coverage": _tool_coverage(host_reports),
    }
    return FleetReport(
        generated_at=datetime.now(UTC).isoformat(),
        host_count=len(hosts),
        success_count=len(success_hosts),
        failure_count=len(failed_hosts),
        summary=summary,
        hosts=hosts,
    )


def _fleet_children(root: Path) -> list[Path]:
    return sorted(
        [path for path in root.iterdir() if path.is_dir() and not path.name.startswith(".")],
        key=lambda path: path.name,
    )


def _child_suppression_rules(child: Path) -> list[SuppressionRule]:
    validation = load_ignore_file_with_diagnostics(child)
    if validation.invalid_entries:
        joined = "; ".join(validation.invalid_entries)
        raise FleetInputError(f"invalid child suppression file {validation.path}: {joined}")
    return list(validation.rules)


def _successful_host_summary(
    evidence_path: Path,
    report: HostPostureReport,
) -> FleetHostSummary:
    by_severity = _summary_by_severity(report)
    report_path = Path("hosts") / evidence_path.name / "host-report.json"
    return FleetHostSummary(
        target=report.target,
        evidence_path=str(evidence_path),
        report_path=str(report_path),
        posture_score=report.posture_score,
        findings_total=int(report.summary.get("findings_total") or 0),
        by_severity=by_severity,
        top_risks=_top_risk_strings(report.findings),
    )


def _failed_host_summary(evidence_path: Path, exc: Exception) -> FleetHostSummary:
    label = evidence_path.name
    message = _stable_error_message(evidence_path, exc)
    return FleetHostSummary(
        target=label,
        evidence_path=str(evidence_path),
        status="error",
        error=message,
    )


def _stable_error_message(evidence_path: Path, exc: Exception) -> str:
    message = str(exc) if isinstance(exc, HostInputError) else f"{type(exc).__name__}: {exc}"
    absolute = str(evidence_path.resolve(strict=False))
    rendered = str(evidence_path)
    return message.replace(absolute, rendered)


def _summary_by_severity(report: HostPostureReport) -> dict[str, int]:
    raw = report.summary.get("by_severity")
    if not isinstance(raw, dict):
        return {}
    counter = Counter({str(key): int(value) for key, value in raw.items()})
    return _ordered_counts(counter)


def _ordered_counts(counter: Counter[str]) -> dict[str, int]:
    return {severity: counter[severity] for severity in _SEVERITIES if counter[severity]}


def _top_risk_strings(findings: list[HostFinding], *, limit: int = 3) -> list[str]:
    rendered: list[str] = []
    for finding in _rank_fleet_findings(findings)[:limit]:
        risk = finding.risk.total if finding.risk is not None else 0.0
        rendered.append(f"{risk:.1f}/100 {finding.severity} {finding.title}")
    return rendered


def _worst_hosts(hosts: list[FleetHostSummary], *, limit: int = 5) -> list[dict[str, object]]:
    ranked = sorted(hosts, key=lambda host: (host.posture_score, host.target, host.evidence_path))
    return [
        {
            "target": host.target,
            "evidence_path": host.evidence_path,
            "posture_score": host.posture_score,
            "findings_total": host.findings_total,
        }
        for host in ranked[:limit]
    ]


def _highest_risk_findings(
    host_reports: dict[str, HostPostureReport],
    *,
    limit: int = 10,
) -> list[dict[str, object]]:
    rows: list[tuple[str, HostFinding]] = []
    for report in host_reports.values():
        for finding in report.findings:
            if finding.suppressed:
                continue
            rows.append((report.target, finding))
    rows = sorted(
        rows,
        key=lambda item: (
            -_finding_risk_total(item[1]),
            -_SEVERITY_RANK[item[1].severity],
            item[0],
            item[1].id,
        ),
    )
    return [
        {
            "target": target,
            "finding_id": finding.id,
            "title": finding.title,
            "severity": finding.severity,
            "risk_total": _finding_risk_total(finding),
        }
        for target, finding in rows[:limit]
    ]


def _evidence_gaps_by_host(
    host_reports: dict[str, HostPostureReport],
) -> dict[str, list[str]]:
    gaps: dict[str, list[str]] = {}
    for report in host_reports.values():
        completeness = report.host_metadata.get("evidence_completeness")
        if not isinstance(completeness, dict):
            continue
        missing = [str(key) for key, value in sorted(completeness.items()) if value is False]
        if missing:
            gaps[report.target] = missing
    return gaps


def _tool_coverage(host_reports: dict[str, HostPostureReport]) -> dict[str, int]:
    tools: Counter[str] = Counter()
    for report in host_reports.values():
        raw_tools = report.host_metadata.get("raw_tools")
        if isinstance(raw_tools, list):
            tools.update(str(tool) for tool in raw_tools)
        collected = report.host_metadata.get("tools")
        if isinstance(collected, list):
            tools.update(str(tool) for tool in collected)
    return dict(sorted(tools.items()))


def _rank_fleet_findings(findings: list[HostFinding]) -> list[HostFinding]:
    return sorted(
        findings,
        key=lambda finding: (
            _finding_risk_total(finding),
            _SEVERITY_RANK[finding.severity],
            finding.confidence,
            finding.title,
        ),
        reverse=True,
    )


def _finding_risk_total(finding: HostFinding) -> float:
    if finding.risk is not None:
        return finding.risk.total
    return round(_SEVERITY_RANK[finding.severity] * finding.confidence * 20.0, 1)


def load_fleet_report(path: str | Path) -> FleetReport:
    candidate = Path(path)
    report_path = candidate / "fleet-report.json" if candidate.is_dir() else candidate
    return FleetReport.model_validate_json(report_path.read_text(encoding="utf-8"))


def fleet_findings_at_or_above(
    host_reports: dict[str, HostPostureReport],
    minimum_severity: str,
) -> list[HostFinding]:
    threshold = _SEVERITY_RANK[minimum_severity]
    findings: list[HostFinding] = []
    for report in host_reports.values():
        findings.extend(
            finding
            for finding in report.findings
            if not finding.suppressed and _SEVERITY_RANK[finding.severity] >= threshold
        )
    return findings
