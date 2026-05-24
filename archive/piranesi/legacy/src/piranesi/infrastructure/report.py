from __future__ import annotations

from pathlib import Path

from piranesi.infrastructure.models import InfrastructureFinding, InfrastructureReport


def write_infrastructure_report_outputs(
    report: InfrastructureReport,
    output_dir: str | Path,
    *,
    prefix: str,
) -> None:
    path = Path(output_dir)
    path.mkdir(parents=True, exist_ok=True)
    (path / f"{prefix}-report.json").write_text(
        report.model_dump_json(indent=2),
        encoding="utf-8",
    )
    (path / f"{prefix}-report.md").write_text(
        render_infrastructure_markdown(report),
        encoding="utf-8",
    )


def render_infrastructure_markdown(report: InfrastructureReport) -> str:
    title = "Container" if report.surface == "container" else "Kubernetes"
    lines = [
        f"# Piranesi {title} Posture Report",
        "",
        f"- Target: `{report.target}`",
        f"- Generated: `{report.generated_at}`",
        f"- Posture score: **{report.posture_score}/100**",
        f"- Findings: **{report.summary.get('findings_total', 0)}**",
        "",
        "## Top Actions",
        "",
    ]
    if not report.top_actions:
        lines.append("No priority actions were identified.")
    for action in report.top_actions:
        lines.append(
            "- "
            f"`{action.get('category')}` {action.get('action')} "
            f"({action.get('finding_count', 0)} finding(s))"
        )
    lines.extend(["", "## Evidence Inventory", ""])
    for key, value in sorted(report.evidence_inventory.items()):
        lines.append(f"- {key}: {value}")
    lines.extend(["", "## Collection Health", ""])
    if report.collection_health is None:
        lines.append("Collection health was not recorded.")
    else:
        status_counts = report.collection_health.status_counts
        if status_counts:
            rendered = ", ".join(
                f"{status}={count}" for status, count in sorted(status_counts.items())
            )
            lines.append(f"- Status counts: {rendered}")
        for name, health in sorted(report.collection_health.required.items()):
            lines.append(f"- Required `{name}`: {health.status} - {health.message}")
        for name, health in sorted(report.collection_health.optional.items()):
            lines.append(f"- Optional `{name}`: {health.status} - {health.message}")
    lines.extend(["", "## Findings", ""])
    if not report.findings:
        lines.append("No infrastructure posture findings were identified.")
    for finding in report.findings:
        lines.extend(_finding_lines(finding))
    lines.extend(["", "## Known Limitations", ""])
    for limitation in report.known_limitations:
        lines.append(f"- {limitation}")
    return "\n".join(lines).rstrip() + "\n"


def _finding_lines(finding: InfrastructureFinding) -> list[str]:
    lines = [
        f"### {finding.title}",
        "",
        f"- ID: `{finding.id}`",
        f"- Rule: `{finding.rule_id}`",
        f"- Severity: `{finding.severity}`",
        f"- Resource: `{finding.affected_resource}`",
        f"- Confidence: `{finding.confidence:.2f}`",
    ]
    if finding.risk is not None:
        lines.append(f"- Risk: `{finding.risk.total:.1f}/100`")
    lines.extend(["", "Evidence:"])
    for item in finding.evidence:
        lines.append(f"- `{item.source}.{item.key}`: {item.value}")
    lines.extend(["", f"Remediation: {finding.remediation}", ""])
    return lines
