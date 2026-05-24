# ruff: noqa: E501

from __future__ import annotations

import json
from pathlib import Path
from typing import cast

from piranesi.host.models import (
    FleetHostSummary,
    FleetReport,
    HostFinding,
    HostHypothesis,
    HostHypothesisReport,
    HostPostureReport,
)


def write_host_report_outputs(
    report: HostPostureReport,
    output_dir: str | Path,
    *,
    report_format: str = "both",
) -> None:
    path = Path(output_dir)
    path.mkdir(parents=True, exist_ok=True)
    format_name = report_format.lower()
    if format_name in {"json", "both", "all"}:
        (path / "host-report.json").write_text(report.model_dump_json(indent=2), encoding="utf-8")
    if format_name in {"markdown", "md", "both", "all"}:
        (path / "host-report.md").write_text(render_host_markdown(report), encoding="utf-8")
    if format_name in {"pdf", "all"}:
        (path / "host-report.pdf").write_bytes(render_host_pdf(report))
    if format_name in {"dashboard", "all"}:
        write_host_dashboard(report, path)


def write_host_hypothesis_outputs(
    report: HostHypothesisReport,
    output_dir: str | Path,
) -> None:
    path = Path(output_dir)
    path.mkdir(parents=True, exist_ok=True)
    (path / "host-hypotheses.json").write_text(
        report.model_dump_json(indent=2),
        encoding="utf-8",
    )
    (path / "host-hypotheses.md").write_text(
        render_host_hypotheses_markdown(report),
        encoding="utf-8",
    )


def write_fleet_report_outputs(
    report: FleetReport,
    output_dir: str | Path,
    *,
    report_format: str = "both",
) -> None:
    path = Path(output_dir)
    path.mkdir(parents=True, exist_ok=True)
    format_name = report_format.lower()
    if format_name in {"json", "both"}:
        (path / "fleet-report.json").write_text(
            report.model_dump_json(indent=2),
            encoding="utf-8",
        )
    if format_name in {"markdown", "md", "both"}:
        (path / "fleet-report.md").write_text(
            render_fleet_markdown(report),
            encoding="utf-8",
        )


def render_host_markdown(report: HostPostureReport) -> str:
    lines = [
        "# Piranesi Host Posture Report",
        "",
        f"- Target: `{report.target}`",
        f"- Generated: `{report.generated_at}`",
        f"- Analysis modes: {', '.join(report.analysis_modes)}",
        f"- Posture score: **{report.posture_score}/100**",
        f"- Findings: **{report.summary.get('findings_total', 0)}**",
        "",
        "## Decision Summary",
        "",
    ]
    lines.extend(_decision_summary_lines(report))
    lines.extend(["", "## Host Metadata", ""])
    lines.extend(_host_metadata_lines(report))
    lines.extend(["", "## Top Actions", ""])
    if not report.top_actions:
        lines.append("No priority actions were identified.")
    for action in report.top_actions:
        lines.extend(_top_action_lines(action))
    lines.extend(["", "## Control Summary", ""])
    lines.extend(_control_summary_lines(report))
    if report.policy_profile is not None:
        lines.extend(["", "## Policy", ""])
        lines.extend(_policy_lines(report))
    lines.extend(["", "## Evidence Inventory", ""])
    for key, count in sorted(report.evidence_inventory.items()):
        lines.append(f"- {key}: {count}")

    auth_metadata = {
        k: v
        for k, v in report.host_metadata.items()
        if k in {"active_sessions_count", "auth_event_summary_count", "failed_ssh_attempt_count"}
    }
    if any(v for v in auth_metadata.values() if isinstance(v, int) and v > 0):
        lines.extend(["", "## Auth Evidence", ""])
        lines.append(f"- Active sessions: {auth_metadata.get('active_sessions_count', 0)}")
        lines.append(f"- Auth event summaries: {auth_metadata.get('auth_event_summary_count', 0)}")
        lines.append(f"- Failed SSH attempts: {auth_metadata.get('failed_ssh_attempt_count', 0)}")

    if report.llm_redaction is not None:
        lines.extend(["", "## LLM Redaction", ""])
        lines.append(f"- Applied: `{str(report.llm_redaction.applied).lower()}`")
        lines.append(f"- Mode: `{report.llm_redaction.mode}`")
        lines.append(f"- Redacted values: {report.llm_redaction.redacted_value_count}")
        if report.llm_redaction.categories:
            rendered = ", ".join(
                f"{key}={value}" for key, value in sorted(report.llm_redaction.categories.items())
            )
            lines.append(f"- Categories: {rendered}")

    if report.collection_health is not None:
        lines.extend(["", "## Collection Health", ""])
        status_counts = report.collection_health.status_counts
        rendered_counts = ", ".join(
            f"{key}={value}" for key, value in sorted(status_counts.items()) if value
        )
        lines.append(f"- Command statuses: {rendered_counts or 'none recorded'}")
        for name, health in sorted(report.collection_health.required.items()):
            lines.append(f"- Required `{name}`: `{health.status}` - {health.message}")
            if health.remediation:
                lines.append(f"  remediation: {health.remediation}")
        for name, health in sorted(report.collection_health.optional.items()):
            lines.append(f"- Optional `{name}`: `{health.status}` - {health.message}")
            if health.remediation:
                lines.append(f"  remediation: {health.remediation}")
    lines.extend(["", "## Findings", ""])
    if not report.findings:
        lines.append("No host posture findings were identified.")
    for finding in report.findings:
        lines.extend(_finding_lines(finding))
    lines.extend(["", "## Known Limitations", ""])
    for limitation in report.known_limitations:
        lines.append(f"- {limitation}")
    return "\n".join(lines).rstrip() + "\n"


def render_host_terminal_summary(report: HostPostureReport) -> str:
    top_finding = _top_risk_finding(report)
    top_action = _first_top_action(report)
    lines = [
        "Piranesi host posture summary",
        f"target: {report.target}",
        f"score: {report.posture_score}/100",
        f"findings: {_active_finding_count(report)}{_severity_summary_suffix(report)}",
        f"decision: {_decision_statement(report)}",
    ]
    if top_finding is not None:
        lines.append(f"top risk: {_risk_text(top_finding)} {top_finding.title}".rstrip())
    if top_action is not None:
        lines.append(f"first action: {top_action}")
    return "\n".join(lines).rstrip() + "\n"


def _decision_summary_lines(report: HostPostureReport) -> list[str]:
    lines = [
        f"- Decision: {_decision_statement(report)}",
        (
            f"- Score: `{report.posture_score}/100` with "
            f"**{_active_finding_count(report)}** active finding(s)"
            f"{_severity_summary_suffix(report)}."
        ),
    ]
    top_finding = _top_risk_finding(report)
    if top_finding is not None:
        lines.append(f"- Top risk: `{_risk_text(top_finding)}` {top_finding.title}")
    top_action = _first_top_action(report)
    if top_action is not None:
        lines.append(f"- First action: {top_action}")
    lines.append(f"- Evidence basis: {_evidence_basis(report)}")
    evidence_gaps = _evidence_gaps(report)
    if evidence_gaps:
        lines.append(f"- Evidence gaps to close: {', '.join(evidence_gaps)}")
    return lines


def _decision_statement(report: HostPostureReport) -> str:
    active_findings = _active_findings(report)
    if not active_findings:
        return (
            "No immediate host posture action is required from supplied evidence; "
            "keep evidence collection current."
        )
    highest = _highest_severity(active_findings)
    max_risk = _max_risk_total(active_findings)
    if highest in {"critical", "high"} or max_risk >= 70:
        return "Prioritize remediation before treating this host as production-ready."
    if highest == "medium" or max_risk >= 50:
        return "Review and schedule remediation; risk is material but bounded by supplied evidence."
    return (
        "Track low-risk cleanup and close evidence gaps before relying on this "
        "as a clean bill of health."
    )


def _active_finding_count(report: HostPostureReport) -> int:
    return len(_active_findings(report))


def _severity_summary_suffix(report: HostPostureReport) -> str:
    counts: dict[str, int] = {}
    for finding in _active_findings(report):
        counts[finding.severity] = counts.get(finding.severity, 0) + 1
    parts = [
        f"{severity}={counts[severity]}"
        for severity in sorted(counts, key=_severity_rank, reverse=True)
        if counts[severity] > 0
    ]
    return "" if not parts else f" ({', '.join(parts)})"


def _active_findings(report: HostPostureReport) -> list[HostFinding]:
    return [finding for finding in report.findings if not finding.suppressed]


def _top_risk_finding(report: HostPostureReport) -> HostFinding | None:
    findings = _active_findings(report)
    if not findings:
        return None
    return sorted(
        findings,
        key=lambda finding: (
            0.0 if finding.risk is None else finding.risk.total,
            _severity_rank(finding.severity),
            finding.confidence,
        ),
        reverse=True,
    )[0]


def _first_top_action(report: HostPostureReport) -> str | None:
    if not report.top_actions:
        return None
    action = report.top_actions[0].get("action")
    return action if isinstance(action, str) and action else None


def _max_risk_total(findings: list[HostFinding]) -> float:
    risk_scores = [finding.risk.total for finding in findings if finding.risk is not None]
    return max(risk_scores, default=0.0)


def _highest_severity(findings: list[HostFinding]) -> str | None:
    if not findings:
        return None
    return max((finding.severity for finding in findings), key=_severity_rank)


def _severity_rank(severity: str) -> int:
    order = {
        "informational": 0,
        "low": 1,
        "medium": 2,
        "high": 3,
        "critical": 4,
    }
    return order.get(severity, -1)


def _risk_text(finding: HostFinding) -> str:
    if finding.risk is None:
        return "n/a"
    return f"{finding.risk.total:.1f}/100"


def _evidence_basis(report: HostPostureReport) -> str:
    metadata = report.host_metadata
    tools = metadata.get("tools")
    rendered_tools = (
        ", ".join(str(item) for item in tools)
        if isinstance(tools, list) and tools
        else "supplied snapshot"
    )
    evidence_present = _evidence_present(report)
    if not evidence_present:
        return rendered_tools
    return f"{rendered_tools}; present evidence: {', '.join(evidence_present)}"


def _evidence_present(report: HostPostureReport) -> list[str]:
    completeness = report.host_metadata.get("evidence_completeness")
    if not isinstance(completeness, dict):
        return []
    return [str(key) for key, value in sorted(completeness.items()) if value]


def _evidence_gaps(report: HostPostureReport) -> list[str]:
    completeness = report.host_metadata.get("evidence_completeness")
    if not isinstance(completeness, dict):
        return []
    return [str(key) for key, value in sorted(completeness.items()) if not value][:6]


def render_fleet_markdown(report: FleetReport) -> str:
    summary = report.summary
    lines = [
        "# Piranesi Fleet Report",
        "",
        f"- Generated: `{report.generated_at}`",
        f"- Hosts: **{report.host_count}**",
        f"- Successful hosts: **{report.success_count}**",
        f"- Failed hosts: **{report.failure_count}**",
        f"- Findings: **{summary.get('findings_total', 0)}**",
        "",
        "## Severity Summary",
        "",
    ]
    by_severity = summary.get("by_severity")
    if isinstance(by_severity, dict) and by_severity:
        for severity, count in by_severity.items():
            lines.append(f"- {severity}: {count}")
    else:
        lines.append("No findings were identified.")
    if report.policy_profile is not None:
        lines.extend(["", "## Policy", ""])
        lines.extend(_fleet_policy_lines(report))
    lines.extend(["", "## Worst Hosts", ""])
    worst_hosts = summary.get("worst_hosts")
    if isinstance(worst_hosts, list) and worst_hosts:
        for host in worst_hosts:
            if isinstance(host, dict):
                lines.append(
                    "- "
                    f"`{host.get('target')}` score={host.get('posture_score')}/100 "
                    f"findings={host.get('findings_total')}"
                )
    else:
        lines.append("No successful hosts were assessed.")
    lines.extend(["", "## Highest-Risk Findings", ""])
    high_risks = summary.get("highest_risk_findings")
    if isinstance(high_risks, list) and high_risks:
        for finding in high_risks:
            if isinstance(finding, dict):
                lines.append(
                    "- "
                    f"`{finding.get('target')}` "
                    f"{float(finding.get('risk_total') or 0.0):.1f}/100 "
                    f"`{finding.get('severity')}` {finding.get('title')}"
                )
    else:
        lines.append("No unsuppressed findings were identified.")
    lines.extend(["", "## Hosts", ""])
    for host in report.hosts:
        lines.extend(_fleet_host_lines(host))
    failures = [host for host in report.hosts if host.status == "error"]
    if failures:
        lines.extend(["", "## Failed Hosts", ""])
        for host in failures:
            lines.append(f"- `{host.target}`: {host.error}")
    return "\n".join(lines).rstrip() + "\n"


def _policy_lines(report: HostPostureReport) -> list[str]:
    summary = report.policy_summary
    lines = [
        f"- Profile: `{report.policy_profile}`",
        f"- Passed: `{str(summary.get('passed', False)).lower()}`",
        f"- Failed gates: {summary.get('failed_gate_count', 0)}",
        f"- Warnings: {summary.get('warning_count', 0)}",
        "",
        "### Gate Results",
        "",
    ]
    if report.policy_gate_results:
        for gate in report.policy_gate_results:
            lines.append(f"- `{gate.get('status')}` `{gate.get('gate_id')}`: {gate.get('message')}")
            finding_ids = gate.get("finding_ids")
            if isinstance(finding_ids, list) and finding_ids:
                lines.append(f"  findings: {', '.join(str(item) for item in finding_ids)}")
    else:
        lines.append("No policy gates were configured.")
    lines.extend(["", "### Required Evidence", ""])
    if report.required_evidence_status:
        for evidence in report.required_evidence_status:
            lines.append(
                f"- `{evidence.get('status')}` `{evidence.get('name')}`: {evidence.get('message')}"
            )
    else:
        lines.append("No required evidence checks were configured.")
    return lines


def _fleet_policy_lines(report: FleetReport) -> list[str]:
    summary = report.policy_summary
    lines = [
        f"- Profile: `{report.policy_profile}`",
        f"- Passed: `{str(summary.get('passed', False)).lower()}`",
        f"- Host policy failures: {summary.get('host_policy_failures', 0)}",
        f"- Failed gates: {summary.get('failed_gate_count', 0)}",
        f"- Warnings: {summary.get('warning_count', 0)}",
        "",
        "### Fleet Gate Results",
        "",
    ]
    if report.policy_gate_results:
        for gate in report.policy_gate_results:
            lines.append(f"- `{gate.get('status')}` `{gate.get('gate_id')}`: {gate.get('message')}")
    else:
        lines.append("No fleet policy gates were configured.")
    lines.extend(["", "### Required Evidence", ""])
    if report.required_evidence_status:
        for evidence in report.required_evidence_status:
            lines.append(
                f"- `{evidence.get('status')}` `{evidence.get('name')}`: {evidence.get('message')}"
            )
    else:
        lines.append("No required evidence checks were configured.")
    return lines


def render_fleet_terminal(report: FleetReport) -> str:
    summary = report.summary
    lines = [
        "Piranesi Fleet Report",
        f"hosts: {report.host_count} ok={report.success_count} failed={report.failure_count}",
        f"findings: {summary.get('findings_total', 0)}",
    ]
    worst_hosts = summary.get("worst_hosts")
    if isinstance(worst_hosts, list) and worst_hosts:
        lines.append("worst_hosts:")
        for host in worst_hosts[:5]:
            if isinstance(host, dict):
                lines.append(
                    f"  - {host.get('target')}: score={host.get('posture_score')}/100 "
                    f"findings={host.get('findings_total')}"
                )
    failures = [host for host in report.hosts if host.status == "error"]
    if failures:
        lines.append("failed_hosts:")
        for host in failures:
            lines.append(f"  - {host.target}: {host.error}")
    return "\n".join(lines).rstrip() + "\n"


def _fleet_host_lines(host: FleetHostSummary) -> list[str]:
    lines = [
        f"### {host.target}",
        "",
        f"- Status: `{host.status}`",
        f"- Evidence: `{host.evidence_path}`",
    ]
    if host.status == "ok":
        lines.extend(
            [
                f"- Report: `{host.report_path}`",
                f"- Posture score: `{host.posture_score}/100`",
                f"- Findings: `{host.findings_total}`",
            ]
        )
        if host.by_severity:
            rendered = ", ".join(
                f"{severity}={count}" for severity, count in host.by_severity.items()
            )
            lines.append(f"- Severity: {rendered}")
        if host.top_risks:
            lines.append("- Top risks: " + "; ".join(host.top_risks))
    elif host.error:
        lines.append(f"- Error: {host.error}")
    lines.append("")
    return lines


def render_host_hypotheses_markdown(report: HostHypothesisReport) -> str:
    lines = [
        "# Piranesi Host Hypothesis Report",
        "",
        f"- Target: `{report.target}`",
        f"- Generated: `{report.generated_at}`",
        f"- Analysis modes: {', '.join(report.analysis_modes)}",
        f"- Hypotheses: **{len(report.hypotheses)}**",
        "",
        "> Hypotheses are not confirmed findings. They do not affect findings_total, "
        "fail-severity, or posture score.",
        "",
    ]
    if report.llm_redaction is not None:
        lines.extend(["## LLM Redaction", ""])
        lines.append(f"- Applied: `{str(report.llm_redaction.applied).lower()}`")
        lines.append(f"- Mode: `{report.llm_redaction.mode}`")
        lines.append(f"- Redacted values: {report.llm_redaction.redacted_value_count}")
        if report.llm_redaction.categories:
            rendered = ", ".join(
                f"{key}={value}" for key, value in sorted(report.llm_redaction.categories.items())
            )
            lines.append(f"- Categories: {rendered}")
        lines.append("")
    lines.extend(["## Hypotheses", ""])
    if not report.hypotheses:
        lines.append("No evidence-bound host hypotheses were generated.")
    for hypothesis in report.hypotheses:
        lines.extend(_hypothesis_lines(hypothesis))
    return "\n".join(lines).rstrip() + "\n"


def _hypothesis_lines(hypothesis: HostHypothesis) -> list[str]:
    lines = [
        f"### {hypothesis.title}",
        "",
        f"- Type: `{hypothesis.hypothesis_type}`",
        f"- Severity if true: `{hypothesis.severity_if_true}`",
        f"- Confidence: `{hypothesis.confidence:.2f}`",
        "- Confirmed finding: `false`",
        "",
        "**Supporting Evidence**",
    ]
    if not hypothesis.supporting_evidence:
        lines.append("- none")
    for evidence_item in hypothesis.supporting_evidence:
        lines.append(f"- `{evidence_item.source}` `{evidence_item.key}`: {evidence_item.value}")
    lines.extend(["", "**Missing Evidence**"])
    if not hypothesis.missing_evidence:
        lines.append("- none")
    for missing_item in hypothesis.missing_evidence:
        lines.append(f"- {missing_item}")
    lines.extend(["", f"**Reasoning Summary:** {hypothesis.reasoning_summary}", ""])
    lines.append("**Suggested Follow-Up**")
    if not hypothesis.suggested_followup_probes:
        lines.append("- none")
    for probe in hypothesis.suggested_followup_probes:
        lines.append(f"- `{probe}`")
    if hypothesis.analyst_questions:
        lines.extend(["", "**Analyst Questions**"])
        for question in hypothesis.analyst_questions:
            lines.append(f"- {question}")
    lines.append("")
    return lines


def _host_metadata_lines(report: HostPostureReport) -> list[str]:
    metadata = report.host_metadata
    os_info = metadata.get("os")
    os_name = "unknown"
    if isinstance(os_info, dict):
        os_name = str(os_info.get("pretty_name") or os_info.get("name") or "unknown")
    ip_addresses = metadata.get("ip_addresses")
    rendered_ips = (
        ", ".join(str(item) for item in ip_addresses) if isinstance(ip_addresses, list) else ""
    )
    tools = metadata.get("tools")
    rendered_tools = ", ".join(str(item) for item in tools) if isinstance(tools, list) else ""
    lines = [
        f"- OS: `{os_name}`",
        f"- Kernel: `{metadata.get('kernel') or 'unknown'}`",
        f"- IP addresses: {rendered_ips or 'none recorded'}",
        f"- Collected tools: {rendered_tools or 'none recorded'}",
    ]
    completeness = metadata.get("evidence_completeness")
    if isinstance(completeness, dict):
        complete = [str(key) for key, value in sorted(completeness.items()) if value]
        missing = [str(key) for key, value in sorted(completeness.items()) if not value]
        lines.append(f"- Evidence present: {', '.join(complete) if complete else 'none'}")
        lines.append(f"- Evidence gaps: {', '.join(missing) if missing else 'none'}")
    return lines


def _top_action_lines(action: dict[str, object]) -> list[str]:
    category = str(action.get("category") or "action").title()
    summary = str(action.get("action") or "Review related findings.")
    severity = str(action.get("severity") or "informational")
    risk_total = action.get("risk_total")
    titles = action.get("finding_titles")
    lines = [f"### {category}", "", f"- Severity: `{severity}`", f"- Action: {summary}"]
    if isinstance(risk_total, int | float):
        lines.append(f"- Top risk: `{float(risk_total):.1f}/100`")
    if isinstance(titles, list) and titles:
        lines.append(f"- Related findings: {', '.join(str(title) for title in titles)}")
    lines.append("")
    return lines


def _finding_lines(finding: HostFinding) -> list[str]:
    lines = [
        f"### {finding.title}",
        "",
        f"- Severity: `{finding.severity}`",
        f"- Category: `{finding.category}`",
        f"- Confidence: `{finding.confidence:.2f}`",
        f"- Source: `{finding.source_tool}`",
    ]
    if finding.risk is not None:
        lines.append(f"- Risk score: `{finding.risk.total:.1f}/100`")
        lines.append(
            "- Risk dimensions: "
            f"severity={finding.risk.severity:.2f}, "
            f"exploitability={finding.risk.exploitability:.2f}, "
            f"blast_radius={finding.risk.blast_radius:.2f}, "
            f"urgency={finding.risk.remediation_urgency:.2f}, "
            f"evidence={finding.risk.evidence_quality:.2f}"
        )
    if finding.affected_component:
        lines.append(f"- Affected component: `{finding.affected_component}`")
    if finding.cve_ids:
        lines.append(f"- CVEs: {', '.join(finding.cve_ids)}")
    if finding.control_refs:
        lines.append(f"- Controls: {', '.join(finding.control_refs)}")
    if finding.structured_control_refs:
        lines.append("- Structured controls:")
        for control in finding.structured_control_refs:
            version = f" {control.version}" if control.version else ""
            lines.append(
                "  - "
                f"{control.framework}{version} `{control.control_id}` - "
                f"{control.title} "
                f"(confidence {control.mapping_confidence:.2f})"
            )
    if finding.suppressed:
        reason = finding.suppression_reason or "suppressed"
        lines.append(f"- Suppressed: yes ({reason})")
    lines.extend(["", "**Evidence**"])
    for evidence_item in finding.evidence:
        lines.append(f"- `{evidence_item.source}` `{evidence_item.key}`: {evidence_item.value}")
    if finding.rationale:
        lines.extend(["", f"**Rationale:** {finding.rationale}"])
    if finding.risk is not None and finding.risk.rationale:
        lines.extend(["", "**Risk Rationale**"])
        for rationale_item in finding.risk.rationale:
            lines.append(f"- {rationale_item}")
    lines.extend(["", f"**Remediation:** {finding.remediation}", ""])
    return lines


def host_report_payload(report: HostPostureReport) -> dict[str, object]:
    return cast(dict[str, object], json.loads(report.model_dump_json()))


def _control_summary_lines(report: HostPostureReport) -> list[str]:
    summary = report.control_summary
    frameworks = summary.get("frameworks")
    if not isinstance(frameworks, dict) or not frameworks:
        return ["No structured host control mappings were attached."]
    lines = [
        f"- Mapped findings: {summary.get('mapped_findings', 0)}",
        f"- Unmapped findings: {summary.get('unmapped_findings', 0)}",
    ]
    mapping_note = summary.get("mapping_note")
    if mapping_note:
        lines.append(f"- Note: {mapping_note}")
    lines.append("")
    for framework, payload in sorted(frameworks.items()):
        if not isinstance(payload, dict):
            continue
        lines.append(
            "- "
            f"{framework}: mapped_findings={payload.get('mapped_findings', 0)}, "
            f"mapped_controls={payload.get('mapped_controls', 0)}, "
            f"highest_severity={payload.get('highest_severity') or 'n/a'}, "
            f"average_confidence={float(payload.get('average_mapping_confidence') or 0.0):.3f}"
        )
    return lines


def render_host_pdf(report: HostPostureReport) -> bytes:
    lines = _pdf_report_lines(report)
    pages = _paginate_pdf_lines(lines)
    return _build_simple_pdf(pages)


def write_host_dashboard(report: HostPostureReport, output_dir: str | Path) -> None:
    dashboard_dir = Path(output_dir) / "host-dashboard"
    assets_dir = dashboard_dir / "assets"
    assets_dir.mkdir(parents=True, exist_ok=True)
    report_json = report.model_dump_json(indent=2)
    (dashboard_dir / "host-report.json").write_text(report_json, encoding="utf-8")
    (dashboard_dir / "index.html").write_text(_dashboard_html(report_json), encoding="utf-8")
    (assets_dir / "host-dashboard.css").write_text(_dashboard_css(), encoding="utf-8")
    (assets_dir / "host-dashboard.js").write_text(_dashboard_js(), encoding="utf-8")


def _pdf_report_lines(report: HostPostureReport) -> list[str]:
    summary = report.summary
    severity = summary.get("by_severity", {})
    metadata = report.host_metadata
    lines = [
        "Piranesi Host Posture Report",
        "",
        f"Target: {report.target}",
        f"Generated: {report.generated_at}",
        f"Analysis modes: {', '.join(report.analysis_modes)}",
        f"Posture score: {report.posture_score}/100",
        f"Findings: {summary.get('findings_total', 0)}",
        f"Severity summary: {json.dumps(severity, sort_keys=True)}",
        "",
        "Host Metadata",
        f"OS: {_pdf_os_name(metadata)}",
        f"Kernel: {metadata.get('kernel') or 'unknown'}",
        f"IP addresses: {_render_value(metadata.get('ip_addresses'))}",
        f"Collected tools: {_render_value(metadata.get('tools'))}",
        "",
        "Top Actions",
    ]
    if report.top_actions:
        for action in report.top_actions:
            risk = action.get("risk_total")
            risk_text = f" risk={float(risk):.1f}/100" if isinstance(risk, int | float) else ""
            lines.append(
                f"- {action.get('category', 'action')}: {action.get('action', '')}{risk_text}"
            )
    else:
        lines.append("No priority actions were identified.")
    lines.extend(["", "Evidence Inventory"])
    for key, count in sorted(report.evidence_inventory.items()):
        lines.append(f"- {key}: {count}")
    lines.extend(["", "Control Summary"])
    for line in _control_summary_lines(report):
        lines.append(line)
    if report.collection_health is not None:
        lines.extend(["", "Collection Health"])
        for name, health in sorted(report.collection_health.required.items()):
            lines.append(f"- Required {name}: {health.status} - {health.message}")
        for name, health in sorted(report.collection_health.optional.items()):
            lines.append(f"- Optional {name}: {health.status} - {health.message}")
    if report.llm_redaction is not None:
        lines.extend(
            [
                "",
                "LLM Redaction",
                f"Applied: {report.llm_redaction.applied}",
                f"Mode: {report.llm_redaction.mode}",
                f"Redacted values: {report.llm_redaction.redacted_value_count}",
                f"Categories: {json.dumps(report.llm_redaction.categories, sort_keys=True)}",
            ]
        )
    lines.extend(["", "Findings"])
    if not report.findings:
        lines.append("No host posture findings were identified.")
    for finding in report.findings:
        lines.extend(
            [
                "",
                finding.title,
                f"Severity: {finding.severity}",
                f"Category: {finding.category}",
                f"Confidence: {finding.confidence:.2f}",
                f"Source: {finding.source_tool}",
                f"Risk score: {finding.risk.total:.1f}/100" if finding.risk else "Risk score: n/a",
                f"Component: {finding.affected_component or 'n/a'}",
                f"Remediation: {finding.remediation}",
            ]
        )
        for control in finding.structured_control_refs[:6]:
            version = f" {control.version}" if control.version else ""
            lines.append(
                "Structured control: "
                f"{control.framework}{version} {control.control_id} "
                f"confidence={control.mapping_confidence:.2f}"
            )
        if finding.risk is not None:
            for reason in finding.risk.rationale[:5]:
                lines.append(f"Risk rationale: {reason}")
        for item in finding.evidence:
            lines.append(f"Evidence: {item.source} {item.key}: {item.value}")
    lines.extend(["", "Known Limitations"])
    for limitation in report.known_limitations:
        lines.append(f"- {limitation}")
    return lines


def _pdf_os_name(metadata: dict[str, object]) -> str:
    os_info = metadata.get("os")
    if isinstance(os_info, dict):
        return str(os_info.get("pretty_name") or os_info.get("name") or "unknown")
    return "unknown"


def _render_value(value: object) -> str:
    if isinstance(value, list):
        return ", ".join(str(item) for item in value) or "none recorded"
    if value is None:
        return "none recorded"
    return str(value)


def _paginate_pdf_lines(
    lines: list[str], *, width: int = 92, per_page: int = 48
) -> list[list[str]]:
    wrapped: list[str] = []
    for line in lines:
        wrapped.extend(_wrap_pdf_line(line, width=width))
    return [wrapped[index : index + per_page] for index in range(0, len(wrapped), per_page)] or [[]]


def _wrap_pdf_line(line: str, *, width: int) -> list[str]:
    if not line:
        return [""]
    words = line.split()
    if not words:
        return [""]
    result: list[str] = []
    current = words[0]
    for word in words[1:]:
        if len(current) + 1 + len(word) <= width:
            current = f"{current} {word}"
        else:
            result.append(current)
            current = word
    result.append(current)
    return result


def _build_simple_pdf(pages: list[list[str]]) -> bytes:
    objects: list[bytes] = []
    page_object_numbers: list[int] = []
    content_object_numbers: list[int] = []
    next_object = 4
    for _page in pages:
        page_object_numbers.append(next_object)
        content_object_numbers.append(next_object + 1)
        next_object += 2
    objects.append(b"<< /Type /Catalog /Pages 2 0 R >>")
    kids = " ".join(f"{number} 0 R" for number in page_object_numbers)
    objects.append(f"<< /Type /Pages /Kids [{kids}] /Count {len(page_object_numbers)} >>".encode())
    objects.append(b"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
    for page_number, content_number, page_lines in zip(
        page_object_numbers,
        content_object_numbers,
        pages,
        strict=True,
    ):
        _ = page_number
        content = _pdf_page_content(page_lines)
        objects.append(
            (
                f"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "
                f"/Resources << /Font << /F1 3 0 R >> >> /Contents {content_number} 0 R >>"
            ).encode()
        )
        objects.append(
            b"<< /Length "
            + str(len(content)).encode()
            + b" >>\nstream\n"
            + content
            + b"\nendstream"
        )
    return _assemble_pdf(objects)


def _pdf_page_content(lines: list[str]) -> bytes:
    rendered = ["BT", "/F1 10 Tf", "72 750 Td", "14 TL"]
    for line in lines:
        rendered.append(f"({_pdf_escape(line)}) Tj")
        rendered.append("T*")
    rendered.append("ET")
    return "\n".join(rendered).encode("latin-1", errors="replace")


def _pdf_escape(value: str) -> str:
    return value.replace("\\", "\\\\").replace("(", "\\(").replace(")", "\\)")


def _assemble_pdf(objects: list[bytes]) -> bytes:
    chunks = [b"%PDF-1.4\n%\xe2\xe3\xcf\xd3\n"]
    offsets: list[int] = []
    cursor = len(chunks[0])
    for index, payload in enumerate(objects, start=1):
        obj = f"{index} 0 obj\n".encode() + payload + b"\nendobj\n"
        offsets.append(cursor)
        chunks.append(obj)
        cursor += len(obj)
    xref_start = cursor
    xref = [f"xref\n0 {len(objects) + 1}\n0000000000 65535 f \n".encode()]
    xref.extend(f"{offset:010d} 00000 n \n".encode() for offset in offsets)
    trailer = (
        f"trailer\n<< /Size {len(objects) + 1} /Root 1 0 R >>\nstartxref\n{xref_start}\n%%EOF\n"
    ).encode()
    return b"".join([*chunks, *xref, trailer])


def _dashboard_html(report_json: str) -> str:
    embedded = report_json.replace("</", "<\\/")
    return f"""<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Piranesi Host Dashboard</title>
  <link rel="stylesheet" href="assets/host-dashboard.css">
</head>
<body>
  <header class="topbar">
    <div>
      <p class="eyebrow">Piranesi host posture</p>
      <h1 id="target">Host Dashboard</h1>
    </div>
    <div class="score"><span id="score">--</span><small>/100</small></div>
  </header>
  <main>
    <section class="metrics" id="metrics"></section>
    <section class="panel">
      <div class="panel-head">
        <h2>Findings</h2>
        <div class="filters">
          <select id="severityFilter" aria-label="Filter by severity"></select>
          <select id="categoryFilter" aria-label="Filter by category"></select>
        </div>
      </div>
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Severity</th>
              <th>Risk</th>
              <th>Category</th>
              <th>Finding</th>
              <th>Confidence</th>
            </tr>
          </thead>
          <tbody id="findingsTable"></tbody>
        </table>
      </div>
      <article id="findingDetail" class="detail"></article>
    </section>
    <section class="grid">
      <section class="panel"><h2>Top Actions</h2><div id="topActions"></div></section>
      <section class="panel"><h2>Evidence</h2><div id="evidence"></div></section>
      <section class="panel"><h2>Collection Health</h2><div id="collectionHealth"></div></section>
    </section>
  </main>
  <script>window.PIRANESI_HOST_REPORT = {embedded};</script>
  <script src="assets/host-dashboard.js"></script>
</body>
</html>
"""


def _dashboard_css() -> str:
    return """
:root {
  color-scheme: light;
  --bg: #f7f8fa;
  --panel: #ffffff;
  --text: #17202a;
  --muted: #64748b;
  --border: #d7dee8;
  --accent: #0f766e;
  --critical: #7f1d1d;
  --high: #b91c1c;
  --medium: #b45309;
  --low: #1d4ed8;
  --info: #475569;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  background: var(--bg);
  color: var(--text);
  font: 14px/1.45 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
}
.topbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 24px;
  padding: 28px 36px;
  background: #111827;
  color: white;
}
.eyebrow {
  margin: 0 0 4px;
  color: #9ca3af;
  text-transform: uppercase;
  font-size: 12px;
  letter-spacing: .08em;
}
h1, h2, h3, p { margin-top: 0; }
h1 { margin-bottom: 0; font-size: 28px; letter-spacing: 0; }
h2 { font-size: 17px; letter-spacing: 0; }
main { padding: 24px 36px 40px; }
.score {
  min-width: 118px;
  text-align: right;
  font-size: 44px;
  font-weight: 700;
}
.score small { font-size: 16px; color: #d1d5db; }
.metrics, .grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 14px;
  margin-bottom: 18px;
}
.metric, .panel {
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 16px;
}
.metric strong { display: block; font-size: 24px; }
.metric span, .muted { color: var(--muted); }
.panel { margin-bottom: 18px; }
.panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.filters { display: flex; gap: 8px; flex-wrap: wrap; }
select {
  border: 1px solid var(--border);
  border-radius: 6px;
  background: white;
  padding: 6px 8px;
}
.table-wrap { overflow-x: auto; }
table { width: 100%; border-collapse: collapse; min-width: 720px; }
th, td { padding: 10px 8px; border-bottom: 1px solid var(--border); text-align: left; }
tbody tr { cursor: pointer; }
tbody tr:hover { background: #f1f5f9; }
.severity { font-weight: 700; text-transform: capitalize; }
.critical { color: var(--critical); }
.high { color: var(--high); }
.medium { color: var(--medium); }
.low { color: var(--low); }
.informational { color: var(--info); }
.detail {
  margin-top: 14px;
  border-left: 3px solid var(--accent);
  padding: 10px 12px;
  background: #f8fafc;
}
.list { margin: 0; padding-left: 18px; }
code {
  background: #eef2f7;
  border-radius: 4px;
  padding: 1px 4px;
}
@media (max-width: 680px) {
  .topbar { padding: 22px; align-items: flex-start; }
  main { padding: 18px; }
  .score { font-size: 34px; }
  .panel-head { align-items: flex-start; flex-direction: column; }
}
"""


def _dashboard_js() -> str:
    return """
const severityOrder = ["critical", "high", "medium", "low", "informational"];
let report = null;
let visibleFindings = [];

async function loadReport() {
  try {
    const response = await fetch("host-report.json", { cache: "no-store" });
    if (response.ok) return await response.json();
  } catch (_) {
    // file:// dashboards may block fetch. The generated HTML embeds the same data.
  }
  return window.PIRANESI_HOST_REPORT;
}

function text(value) {
  if (value === null || value === undefined || value === "") return "none recorded";
  if (Array.isArray(value)) return value.length ? value.join(", ") : "none recorded";
  return String(value);
}

function optionList(select, values, label) {
  select.innerHTML = "";
  const all = document.createElement("option");
  all.value = "";
  all.textContent = label;
  select.appendChild(all);
  values.forEach((value) => {
    const option = document.createElement("option");
    option.value = value;
    option.textContent = value;
    select.appendChild(option);
  });
}

function renderMetrics() {
  const summary = report.summary || {};
  const metadata = report.host_metadata || {};
  const frameworks = Object.keys(report.control_summary?.frameworks || {});
  const metrics = [
    ["Findings", summary.findings_total || 0],
    ["Max risk", summary.risk?.max_total ?? 0],
    ["Public services", report.snapshot?.listening_ports?.length || 0],
    ["Packages", report.snapshot?.packages?.length || 0],
    ["Evidence classes", Object.keys(report.evidence_inventory || {}).length],
    ["Control frameworks", frameworks.length],
    ["Auth summaries", metadata.auth_event_summary_count || 0],
  ];
  document.getElementById("metrics").innerHTML = metrics.map(([label, value]) =>
    `<div class="metric"><strong>${value}</strong><span>${label}</span></div>`
  ).join("");
}

function renderFilters() {
  const findings = report.findings || [];
  const severities = severityOrder.filter((severity) => findings.some((f) => f.severity === severity));
  const categories = [...new Set(findings.map((f) => f.category).filter(Boolean))].sort();
  optionList(document.getElementById("severityFilter"), severities, "All severities");
  optionList(document.getElementById("categoryFilter"), categories, "All categories");
}

function filterFindings() {
  const severity = document.getElementById("severityFilter").value;
  const category = document.getElementById("categoryFilter").value;
  visibleFindings = (report.findings || []).filter((finding) =>
    (!severity || finding.severity === severity) && (!category || finding.category === category)
  );
  renderFindings();
}

function renderFindings() {
  const tbody = document.getElementById("findingsTable");
  tbody.innerHTML = "";
  if (!visibleFindings.length) {
    tbody.innerHTML = `<tr><td colspan="5" class="muted">No findings match the selected filters.</td></tr>`;
    document.getElementById("findingDetail").innerHTML = "";
    return;
  }
  visibleFindings.forEach((finding, index) => {
    const row = document.createElement("tr");
    row.innerHTML = `
      <td><span class="severity ${finding.severity}">${finding.severity}</span></td>
      <td>${Number(finding.risk?.total || 0).toFixed(1)}</td>
      <td>${text(finding.category)}</td>
      <td>${text(finding.title)}</td>
      <td>${Number(finding.confidence || 0).toFixed(2)}</td>
    `;
    row.addEventListener("click", () => renderDetail(finding));
    tbody.appendChild(row);
    if (index === 0) renderDetail(finding);
  });
}

function renderDetail(finding) {
  const evidence = finding.evidence || [];
  const risk = finding.risk;
  const controls = finding.structured_control_refs || [];
  document.getElementById("findingDetail").innerHTML = `
    <h3>${text(finding.title)}</h3>
    <p class="muted">${text(finding.affected_component)} | ${text(finding.source_tool)} | risk ${risk ? Number(risk.total).toFixed(1) + "/100" : "n/a"}</p>
    <p>${text(finding.remediation)}</p>
    ${risk ? `<p>${text((risk.rationale || []).slice(0, 3).join("; "))}</p>` : ""}
    ${controls.length ? `<h4>Structured Controls</h4><ul class="list">${
      controls.map((control) => `<li>${text(control.framework)} ${text(control.version || "")} <code>${text(control.control_id)}</code>: ${text(control.title)} (${Number(control.mapping_confidence || 0).toFixed(2)})</li>`).join("")
    }</ul>` : ""}
    <ul class="list">
      ${evidence.map((item) => `<li><code>${text(item.source)}</code> <code>${text(item.key)}</code>: ${text(item.value)}</li>`).join("")}
    </ul>
  `;
}

function renderTopActions() {
  const actions = report.top_actions || [];
  document.getElementById("topActions").innerHTML = actions.length
    ? `<ul class="list">${actions.map((action) => `<li><strong>${text(action.category)}</strong>: ${text(action.action)}</li>`).join("")}</ul>`
    : `<p class="muted">No priority actions were identified.</p>`;
}

function renderEvidence() {
  const inventory = report.evidence_inventory || {};
  document.getElementById("evidence").innerHTML = `<ul class="list">${
    Object.entries(inventory).sort().map(([key, value]) => `<li>${key}: ${value}</li>`).join("")
  }</ul>`;
}

function renderHealth() {
  const health = report.collection_health;
  if (!health) {
    document.getElementById("collectionHealth").innerHTML = `<p class="muted">No collection manifest was provided.</p>`;
    return;
  }
  const rows = [];
  for (const [name, item] of Object.entries(health.required || {})) rows.push(["Required", name, item]);
  for (const [name, item] of Object.entries(health.optional || {})) rows.push(["Optional", name, item]);
  document.getElementById("collectionHealth").innerHTML = `<ul class="list">${
    rows.map(([kind, name, item]) => `<li>${kind} <code>${name}</code>: ${item.status} - ${text(item.message)}</li>`).join("")
  }</ul>`;
}

loadReport().then((loaded) => {
  report = loaded || {};
  document.getElementById("target").textContent = report.target || "Host Dashboard";
  document.getElementById("score").textContent = report.posture_score ?? "--";
  renderMetrics();
  renderFilters();
  filterFindings();
  renderTopActions();
  renderEvidence();
  renderHealth();
  document.getElementById("severityFilter").addEventListener("change", filterFindings);
  document.getElementById("categoryFilter").addEventListener("change", filterFindings);
});
"""
