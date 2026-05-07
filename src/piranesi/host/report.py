from __future__ import annotations

import json
from pathlib import Path

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
        "## Evidence Inventory",
        "",
    ]
    for key, count in sorted(report.evidence_inventory.items()):
        lines.append(f"- {key}: {count}")
    lines.extend(["", "## Findings", ""])
    if not report.findings:
        lines.append("No host posture findings were identified.")
    for finding in report.findings:
        lines.extend(_finding_lines(finding))
    lines.extend(["", "## Known Limitations", ""])
    for limitation in report.known_limitations:
        lines.append(f"- {limitation}")
    return "\n".join(lines).rstrip() + "\n"


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
    return json.loads(report.model_dump_json())
