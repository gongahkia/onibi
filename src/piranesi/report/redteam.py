from __future__ import annotations

import json
from collections import Counter
from pathlib import Path
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

from piranesi import __version__
from piranesi.detections import load_detections
from piranesi.evidence import load_evidence_index
from piranesi.objectives import load_objectives, load_procedures
from piranesi.timeline import load_timeline_events
from piranesi.workspace import WorkspaceState, utc_now

RED_TEAM_REPORT_SCHEMA_VERSION: Literal["piranesi.red-team-report.v1"] = (
    "piranesi.red-team-report.v1"
)
RedTeamReportFormat = Literal["json", "md"]


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
) -> Path:
    output_dir.mkdir(parents=True, exist_ok=True)
    if output_format == "json":
        path = output_dir / "red-team-report.json"
        path.write_text(
            json.dumps(report.model_dump(mode="json"), indent=2, sort_keys=True) + "\n",
            encoding="utf-8",
        )
        return path
    path = output_dir / "red-team-report.md"
    path.write_text(render_red_team_markdown(report), encoding="utf-8")
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


__all__ = [
    "RED_TEAM_REPORT_SCHEMA_VERSION",
    "RedTeamReport",
    "RedTeamReportError",
    "RedTeamReportFormat",
    "build_red_team_report",
    "render_red_team_markdown",
    "render_red_team_report_artifact",
]
