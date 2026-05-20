from __future__ import annotations

import html
import importlib
import json
import tempfile
import zipfile
from collections import Counter
from pathlib import Path
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

from piranesi import __version__
from piranesi.detections import load_detections
from piranesi.evidence import load_evidence_index
from piranesi.objectives import load_objectives, load_procedures
from piranesi.report.pentest import PdfBackend
from piranesi.timeline import load_timeline_events
from piranesi.workspace import (
    AUDIT_LOG_FILE,
    DETECTIONS_FILE,
    EVIDENCE_FILE,
    FINDINGS_FILE,
    OBJECTIVES_FILE,
    PROCEDURES_FILE,
    TIMELINE_FILE,
    WORKSPACE_FILE,
    WorkspaceState,
    file_sha256,
    utc_now,
    workspace_path,
)

RED_TEAM_REPORT_SCHEMA_VERSION: Literal["piranesi.red-team-report.v1"] = (
    "piranesi.red-team-report.v1"
)
RedTeamReportFormat = Literal["json", "md", "pdf", "archive"]


class RedTeamReportError(RuntimeError):
    """Raised when a red-team report cannot be rendered."""


class _StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class RedTeamReport(_StrictModel):
    schema_version: Literal["piranesi.red-team-report.v1"] = RED_TEAM_REPORT_SCHEMA_VERSION
    piranesi_version: str
    generated_at: str
    engagement: dict[str, Any]
    executive_summary: dict[str, Any]
    evidence: list[dict[str, Any]] = Field(default_factory=list)
    timeline: list[dict[str, Any]] = Field(default_factory=list)
    objectives: list[dict[str, Any]] = Field(default_factory=list)
    procedures: list[dict[str, Any]] = Field(default_factory=list)
    findings: list[dict[str, Any]] = Field(default_factory=list)
    detections: dict[str, Any] = Field(default_factory=dict)
    handoff: dict[str, Any] = Field(default_factory=dict)
    limitations: list[str] = Field(default_factory=list)


def build_red_team_report(
    state: WorkspaceState,
    *,
    redact_sensitive_evidence: bool,
) -> RedTeamReport:
    evidence = [
        _redacted_record(record.model_dump(mode="json"), redact_sensitive_evidence)
        for record in load_evidence_index(state.root).evidence
    ]
    timeline = [event.model_dump(mode="json") for event in load_timeline_events(state.root)]
    objectives = [
        objective.model_dump(mode="json") for objective in load_objectives(state.root).objectives
    ]
    procedures = [
        procedure.model_dump(mode="json") for procedure in load_procedures(state.root).procedures
    ]
    findings = [finding.model_dump(mode="json") for finding in state.findings.findings]
    detections_document = load_detections(state.root)
    detection_notes = [
        _redacted_record(note.model_dump(mode="json"), redact_sensitive_evidence)
        for note in detections_document.notes
    ]
    iocs = [
        _redacted_record(ioc.model_dump(mode="json"), redact_sensitive_evidence)
        for ioc in detections_document.iocs
    ]
    objective_statuses = Counter(item["status"] for item in objectives)
    return RedTeamReport(
        piranesi_version=__version__,
        generated_at=utc_now(),
        engagement=state.workspace.engagement.model_dump(mode="json"),
        executive_summary={
            "evidence_count": len(evidence),
            "timeline_event_count": len(timeline),
            "objective_count": len(objectives),
            "procedure_count": len(procedures),
            "finding_count": len(findings),
            "ioc_count": len(iocs),
            "detection_note_count": len(detection_notes),
            "objective_statuses": dict(sorted(objective_statuses.items())),
        },
        evidence=evidence,
        timeline=timeline,
        objectives=objectives,
        procedures=procedures,
        findings=findings,
        detections={"iocs": iocs, "notes": detection_notes},
        handoff={
            "report_sections": [
                "Executive summary",
                "Objective outcomes",
                "Operation timeline",
                "Procedures and ATT&CK mapping",
                "Findings",
                "Detection and IOC handoff",
                "Evidence appendix",
            ],
            "local_first": True,
        },
        limitations=[
            "This report only covers artifacts imported into the local workspace.",
            "Piranesi does not operate C2 infrastructure, run payloads, or perform exploitation.",
            "Sensitive evidence metadata may be redacted in generated report output.",
        ],
    )


def render_red_team_report_artifact(
    report: RedTeamReport,
    *,
    output_dir: Path,
    output_format: RedTeamReportFormat,
    pdf_backend: PdfBackend = "reportlab",
    workspace_root: Path | None = None,
    include_raw_evidence: bool = False,
    include_secret_raw_evidence: bool = False,
) -> Path:
    output_dir.mkdir(parents=True, exist_ok=True)
    if output_format == "json":
        path = output_dir / "red-team-report.json"
        path.write_text(
            json.dumps(report.model_dump(mode="json"), indent=2, sort_keys=True) + "\n",
            encoding="utf-8",
        )
        return path
    if output_format == "md":
        path = output_dir / "red-team-report.md"
        path.write_text(render_red_team_markdown(report), encoding="utf-8")
        return path
    if output_format == "pdf":
        path = output_dir / f"red-team-report-{pdf_backend}.pdf"
        if pdf_backend == "weasyprint":
            render_red_team_weasyprint_pdf(report, path)
        else:
            render_red_team_reportlab_pdf(report, path)
        return path
    if workspace_root is None:
        raise RedTeamReportError("workspace_root is required for red-team handoff archive export")
    path = output_dir / "red-team-handoff-archive.zip"
    render_red_team_archive(
        report,
        workspace_root=workspace_root,
        output_path=path,
        include_raw_evidence=include_raw_evidence,
        include_secret_raw_evidence=include_secret_raw_evidence,
    )
    return path


def render_red_team_markdown(report: RedTeamReport) -> str:
    summary = report.executive_summary
    lines = [
        "# Piranesi Red-Team Handoff",
        "",
        f"Generated: {report.generated_at}",
        f"Schema: {report.schema_version}",
        "",
        "## Executive Summary",
        "",
        f"- Evidence records: {summary['evidence_count']}",
        f"- Timeline events: {summary['timeline_event_count']}",
        f"- Objectives: {summary['objective_count']}",
        f"- Procedures: {summary['procedure_count']}",
        f"- Findings: {summary['finding_count']}",
        f"- IOCs: {summary['ioc_count']}",
        "",
        "## Objectives",
        "",
    ]
    lines.extend(_objective_lines(report.objectives))
    lines.extend(["", "## Timeline", ""])
    lines.extend(_timeline_lines(report.timeline))
    lines.extend(["", "## Procedures", ""])
    lines.extend(_procedure_lines(report.procedures))
    lines.extend(["", "## Findings", ""])
    lines.extend(_finding_lines(report.findings))
    lines.extend(["", "## Detection And IOC Handoff", ""])
    lines.extend(_detection_lines(report.detections))
    lines.extend(["", "## Evidence Appendix", ""])
    lines.extend(_evidence_lines(report.evidence))
    lines.extend(["", "## Limitations", ""])
    for limitation in report.limitations:
        lines.append(f"- {limitation}")
    lines.append("")
    return "\n".join(lines)


def render_red_team_weasyprint_pdf(report: RedTeamReport, output_path: Path) -> None:
    try:
        html_class = importlib.import_module("weasyprint").HTML
    except Exception as exc:
        raise RedTeamReportError(
            "WeasyPrint PDF rendering is unavailable. Install WeasyPrint system "
            "dependencies or rerun `piranesi report --type red-team --format pdf "
            "--pdf-backend reportlab` for the deterministic fallback."
        ) from exc
    try:
        html_class(string=render_red_team_html(report)).write_pdf(str(output_path))
    except Exception as exc:
        raise RedTeamReportError(
            "WeasyPrint failed to render the red-team PDF. Rerun with "
            "--pdf-backend reportlab for the deterministic fallback. "
            f"Underlying error: {exc}"
        ) from exc


def render_red_team_reportlab_pdf(report: RedTeamReport, output_path: Path) -> None:
    try:
        pagesizes = importlib.import_module("reportlab.lib.pagesizes")
        styles_module = importlib.import_module("reportlab.lib.styles")
        platypus = importlib.import_module("reportlab.platypus")
    except Exception as exc:
        raise RedTeamReportError("ReportLab PDF rendering is unavailable.") from exc

    letter = pagesizes.letter
    styles = styles_module.getSampleStyleSheet()
    doc = platypus.SimpleDocTemplate(str(output_path), pagesize=letter)
    summary = report.executive_summary
    story: list[Any] = [
        platypus.Paragraph("Piranesi Red-Team Handoff", styles["Title"]),
        platypus.Spacer(1, 12),
        platypus.Paragraph(f"Generated: {report.generated_at}", styles["Normal"]),
        platypus.Spacer(1, 12),
        platypus.Paragraph("Executive Summary", styles["Heading2"]),
        platypus.Paragraph(
            " | ".join(
                [
                    f"Evidence: {summary['evidence_count']}",
                    f"Timeline: {summary['timeline_event_count']}",
                    f"Objectives: {summary['objective_count']}",
                    f"Findings: {summary['finding_count']}",
                    f"IOCs: {summary['ioc_count']}",
                ]
            ),
            styles["Normal"],
        ),
        platypus.Spacer(1, 12),
    ]
    _append_pdf_section(story, platypus, styles, "Objectives", _objective_lines(report.objectives))
    _append_pdf_section(story, platypus, styles, "Timeline", _timeline_lines(report.timeline))
    _append_pdf_section(story, platypus, styles, "Procedures", _procedure_lines(report.procedures))
    _append_pdf_section(story, platypus, styles, "Findings", _finding_lines(report.findings))
    _append_pdf_section(
        story,
        platypus,
        styles,
        "Detection And IOC Handoff",
        _detection_lines(report.detections),
    )
    _append_pdf_section(
        story,
        platypus,
        styles,
        "Evidence Appendix",
        _evidence_lines(report.evidence),
    )
    doc.build(story)


def render_red_team_html(report: RedTeamReport) -> str:
    sections = "\n".join(
        [
            _html_list_section("Objectives", _objective_lines(report.objectives)),
            _html_list_section("Timeline", _timeline_lines(report.timeline)),
            _html_list_section("Procedures", _procedure_lines(report.procedures)),
            _html_list_section("Findings", _finding_lines(report.findings)),
            _html_list_section("Detection And IOC Handoff", _detection_lines(report.detections)),
            _html_list_section("Evidence Appendix", _evidence_lines(report.evidence)),
            _html_list_section("Limitations", [f"- {item}" for item in report.limitations]),
        ]
    )
    summary = report.executive_summary
    summary_html = (
        f'{summary["evidence_count"]} evidence records, '
        f'{summary["timeline_event_count"]} timeline events, '
        f'{summary["objective_count"]} objectives, '
        f'{summary["procedure_count"]} procedures, '
        f'{summary["finding_count"]} findings, and {summary["ioc_count"]} IOCs.'
    )
    return f"""<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <style>
    body {{ font-family: sans-serif; color: #1f2937; line-height: 1.45; }}
    h1, h2 {{ color: #111827; }}
    li {{ margin-bottom: 4px; }}
  </style>
</head>
<body>
  <h1>Piranesi Red-Team Handoff</h1>
  <p>Generated: {html.escape(report.generated_at)}</p>
  <h2>Executive Summary</h2>
  <p>{html.escape(summary_html)}</p>
  {sections}
</body>
</html>
"""


def render_red_team_archive(
    report: RedTeamReport,
    *,
    workspace_root: Path,
    output_path: Path,
    include_raw_evidence: bool = False,
    include_secret_raw_evidence: bool = False,
) -> None:
    output_path.parent.mkdir(parents=True, exist_ok=True)
    entries: list[dict[str, str]] = []
    with zipfile.ZipFile(output_path, mode="w", compression=zipfile.ZIP_DEFLATED) as archive:
        _archive_writestr(
            archive,
            entries,
            "reports/red-team-report.json",
            json.dumps(report.model_dump(mode="json"), indent=2, sort_keys=True) + "\n",
        )
        _archive_writestr(
            archive,
            entries,
            "reports/red-team-report.md",
            render_red_team_markdown(report),
        )
        with tempfile.TemporaryDirectory(prefix="piranesi-redteam-pdf-") as tmp:
            pdf_path = Path(tmp) / "red-team-report-reportlab.pdf"
            render_red_team_reportlab_pdf(report, pdf_path)
            _archive_write_file(archive, entries, pdf_path, "reports/red-team-report-reportlab.pdf")
        for relative in _handoff_workspace_files(workspace_root):
            path = workspace_path(workspace_root, relative)
            if path.is_file():
                _archive_write_file(archive, entries, path, relative)
        if include_raw_evidence:
            _archive_raw_evidence(
                archive,
                entries,
                report,
                workspace_root=workspace_root,
                include_secret_raw_evidence=include_secret_raw_evidence,
            )
        _archive_writestr(
            archive,
            entries,
            "archive-manifest.json",
            json.dumps(
                {
                    "schema_version": "piranesi.red-team-archive.v1",
                    "generated_at": utc_now(),
                    "include_raw_evidence": include_raw_evidence,
                    "include_secret_raw_evidence": include_secret_raw_evidence,
                    "entries": sorted(entries, key=lambda item: item["path"]),
                },
                indent=2,
                sort_keys=True,
            )
            + "\n",
        )


def _redacted_record(payload: dict[str, Any], redact: bool) -> dict[str, Any]:
    if not redact or payload.get("sensitivity") not in {"sensitive", "secret"}:
        return payload
    redacted = dict(payload)
    for key in ("notes", "body"):
        if redacted.get(key):
            redacted[key] = "[redacted]"
    return redacted


def _objective_lines(objectives: list[dict[str, Any]]) -> list[str]:
    if not objectives:
        return ["- No objectives recorded."]
    return [
        f"- {item['status']}: {item['title']} ({item['id']})"
        for item in sorted(objectives, key=lambda value: value["id"])
    ]


def _timeline_lines(events: list[dict[str, Any]]) -> list[str]:
    if not events:
        return ["- No timeline events recorded."]
    return [
        f"- {item['timestamp']}: {item['summary']} ({item.get('phase') or 'unphased'})"
        for item in events
    ]


def _procedure_lines(procedures: list[dict[str, Any]]) -> list[str]:
    if not procedures:
        return ["- No procedures recorded."]
    return [
        f"- {item.get('technique_id') or '-'}: {item['summary']} ({item['id']})"
        for item in sorted(procedures, key=lambda value: value["id"])
    ]


def _finding_lines(findings: list[dict[str, Any]]) -> list[str]:
    if not findings:
        return ["- No findings imported."]
    return [
        f"- {item['severity']}: {item['title']} ({item['id']})"
        for item in sorted(findings, key=lambda value: value["id"])
    ]


def _detection_lines(detections: dict[str, Any]) -> list[str]:
    iocs = detections.get("iocs") or []
    notes = detections.get("notes") or []
    lines: list[str] = []
    if iocs:
        for item in iocs:
            lines.append(f"- IOC {item['type']}: {item['value']} ({item['confidence']})")
    if notes:
        for item in notes:
            lines.append(f"- Note: {item['title']}")
    return lines or ["- No detection handoff records."]


def _evidence_lines(evidence: list[dict[str, Any]]) -> list[str]:
    if not evidence:
        return ["- No evidence records."]
    return [
        f"- {item['kind']}: {item['title']} ({item['raw_path']}, {item['sha256']})"
        for item in sorted(evidence, key=lambda value: value["id"])
    ]


def _append_pdf_section(
    story: list[Any],
    platypus: Any,
    styles: Any,
    title: str,
    lines: list[str],
) -> None:
    story.append(platypus.Paragraph(title, styles["Heading2"]))
    for line in lines:
        story.append(platypus.Paragraph(line.removeprefix("- "), styles["Normal"]))
    story.append(platypus.Spacer(1, 8))


def _html_list_section(title: str, lines: list[str]) -> str:
    items = "\n".join(
        f"<li>{html.escape(line.removeprefix('- '))}</li>"
        for line in lines
    )
    return f"<section><h2>{html.escape(title)}</h2><ul>{items}</ul></section>"


def _handoff_workspace_files(workspace_root: Path) -> list[str]:
    files = [
        WORKSPACE_FILE,
        EVIDENCE_FILE,
        TIMELINE_FILE,
        OBJECTIVES_FILE,
        PROCEDURES_FILE,
        DETECTIONS_FILE,
        FINDINGS_FILE,
        AUDIT_LOG_FILE,
    ]
    signatures_dir = workspace_root / "signatures"
    if signatures_dir.is_dir():
        files.extend(
            path.relative_to(workspace_root).as_posix()
            for path in sorted(signatures_dir.glob("manifest-*.json"))
        )
    return files


def _archive_raw_evidence(
    archive: zipfile.ZipFile,
    entries: list[dict[str, str]],
    report: RedTeamReport,
    *,
    workspace_root: Path,
    include_secret_raw_evidence: bool,
) -> None:
    for item in report.evidence:
        raw_path = item.get("raw_path")
        sensitivity = item.get("sensitivity")
        if not isinstance(raw_path, str):
            continue
        if sensitivity == "secret" and not include_secret_raw_evidence:
            continue
        path = workspace_path(workspace_root, raw_path, allowed_roots=("raw",))
        if path.is_file():
            _archive_write_file(archive, entries, path, raw_path)


def _archive_write_file(
    archive: zipfile.ZipFile,
    entries: list[dict[str, str]],
    source: Path,
    archive_path: str,
) -> None:
    archive.write(source, archive_path)
    entries.append({"path": archive_path, "sha256": file_sha256(source)})


def _archive_writestr(
    archive: zipfile.ZipFile,
    entries: list[dict[str, str]],
    archive_path: str,
    body: str,
) -> None:
    data = body.encode("utf-8")
    archive.writestr(archive_path, data)
    entries.append({"path": archive_path, "sha256": _sha256_bytes(data)})


def _sha256_bytes(data: bytes) -> str:
    import hashlib

    return hashlib.sha256(data).hexdigest()


__all__ = [
    "RED_TEAM_REPORT_SCHEMA_VERSION",
    "RedTeamReport",
    "RedTeamReportError",
    "RedTeamReportFormat",
    "build_red_team_report",
    "render_red_team_archive",
    "render_red_team_html",
    "render_red_team_markdown",
    "render_red_team_report_artifact",
    "render_red_team_reportlab_pdf",
    "render_red_team_weasyprint_pdf",
]
