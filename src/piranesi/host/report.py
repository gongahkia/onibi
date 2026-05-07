from __future__ import annotations

import json
from pathlib import Path
from typing import cast

from piranesi.host.models import HostFinding, HostPostureReport


def write_host_report_outputs(
    report: HostPostureReport,
    output_dir: str | Path,
    *,
    report_format: str = "both",
) -> None:
    path = Path(output_dir)
    path.mkdir(parents=True, exist_ok=True)
    format_name = report_format.lower()
    if format_name in {"json", "both"}:
        (path / "host-report.json").write_text(report.model_dump_json(indent=2), encoding="utf-8")
    if format_name in {"markdown", "md", "both"}:
        (path / "host-report.md").write_text(render_host_markdown(report), encoding="utf-8")


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
        "## Host Metadata",
        "",
    ]
    lines.extend(_host_metadata_lines(report))
    lines.extend(["", "## Top Actions", ""])
    if not report.top_actions:
        lines.append("No priority actions were identified.")
    for action in report.top_actions:
        lines.extend(_top_action_lines(action))
    lines.extend(["", "## Evidence Inventory", ""])
    for key, count in sorted(report.evidence_inventory.items()):
        lines.append(f"- {key}: {count}")
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
    titles = action.get("finding_titles")
    lines = [f"### {category}", "", f"- Severity: `{severity}`", f"- Action: {summary}"]
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
    if finding.affected_component:
        lines.append(f"- Affected component: `{finding.affected_component}`")
    if finding.cve_ids:
        lines.append(f"- CVEs: {', '.join(finding.cve_ids)}")
    if finding.control_refs:
        lines.append(f"- Controls: {', '.join(finding.control_refs)}")
    lines.extend(["", "**Evidence**"])
    for item in finding.evidence:
        lines.append(f"- `{item.source}` `{item.key}`: {item.value}")
    if finding.rationale:
        lines.extend(["", f"**Rationale:** {finding.rationale}"])
    lines.extend(["", f"**Remediation:** {finding.remediation}", ""])
    return lines


def host_report_payload(report: HostPostureReport) -> dict[str, object]:
    return cast(dict[str, object], json.loads(report.model_dump_json()))
