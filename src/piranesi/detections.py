from __future__ import annotations

import json
from pathlib import Path
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from piranesi.evidence import load_evidence_index
from piranesi.objectives import load_procedures
from piranesi.timeline import load_timeline_events
from piranesi.workspace import (
    DETECTIONS_FILE,
    WorkspaceState,
    deterministic_finding_id,
    workspace_path,
)

DETECTIONS_SCHEMA_VERSION: Literal["piranesi.detections.v1"] = "piranesi.detections.v1"
IOCType = Literal[
    "ip",
    "domain",
    "url",
    "hash",
    "email",
    "username",
    "file-path",
    "registry-key",
    "process",
    "other",
]
DetectionConfidence = Literal["low", "medium", "high", "confirmed"]
DetectionSensitivity = Literal["public", "internal", "sensitive", "secret"]


class DetectionError(ValueError):
    """Raised when detection handoff data cannot be stored or loaded safely."""


class _StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class IOCRecord(_StrictModel):
    id: str
    type: IOCType
    value: str
    first_observed: str | None = None
    last_observed: str | None = None
    evidence_ids: list[str] = Field(default_factory=list)
    timeline_event_ids: list[str] = Field(default_factory=list)
    procedure_ids: list[str] = Field(default_factory=list)
    sensitivity: DetectionSensitivity = "sensitive"
    confidence: DetectionConfidence = "medium"
    tags: list[str] = Field(default_factory=list)
    notes: str | None = None


class DetectionNote(_StrictModel):
    id: str
    title: str
    body: str
    evidence_ids: list[str] = Field(default_factory=list)
    timeline_event_ids: list[str] = Field(default_factory=list)
    procedure_ids: list[str] = Field(default_factory=list)
    finding_ids: list[str] = Field(default_factory=list)
    sensitivity: DetectionSensitivity = "sensitive"
    tags: list[str] = Field(default_factory=list)


class DetectionsDocument(_StrictModel):
    schema_version: Literal["piranesi.detections.v1"] = DETECTIONS_SCHEMA_VERSION
    iocs: list[IOCRecord] = Field(default_factory=list)
    notes: list[DetectionNote] = Field(default_factory=list)


def load_detections(root: Path | str) -> DetectionsDocument:
    path = workspace_path(root, DETECTIONS_FILE, allowed_roots=("detections",))
    if not path.exists():
        return DetectionsDocument()
    payload = _load_json(path)
    version = payload.get("schema_version")
    if version != DETECTIONS_SCHEMA_VERSION:
        raise DetectionError(
            f"unsupported detections schema version {version!r}; "
            f"expected {DETECTIONS_SCHEMA_VERSION!r}"
        )
    try:
        return DetectionsDocument.model_validate(payload)
    except ValidationError as exc:
        raise DetectionError(f"invalid detections schema: {exc}") from exc


def save_detections(root: Path | str, document: DetectionsDocument) -> Path:
    path = workspace_path(root, DETECTIONS_FILE, allowed_roots=("detections",))
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(document.model_dump(mode="json"), indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    return path


def add_ioc(
    state: WorkspaceState,
    *,
    ioc_type: IOCType,
    value: str,
    first_observed: str | None = None,
    last_observed: str | None = None,
    evidence_ids: list[str] | None = None,
    timeline_event_ids: list[str] | None = None,
    procedure_ids: list[str] | None = None,
    sensitivity: DetectionSensitivity = "sensitive",
    confidence: DetectionConfidence = "medium",
    tags: list[str] | None = None,
    notes: str | None = None,
) -> tuple[DetectionsDocument, IOCRecord]:
    if not value.strip():
        raise DetectionError("IOC value cannot be empty")
    record = IOCRecord(
        id=deterministic_finding_id("ioc", ioc_type, value, prefix="ioc"),
        type=ioc_type,
        value=value,
        first_observed=first_observed,
        last_observed=last_observed,
        evidence_ids=sorted(set(evidence_ids or [])),
        timeline_event_ids=sorted(set(timeline_event_ids or [])),
        procedure_ids=sorted(set(procedure_ids or [])),
        sensitivity=sensitivity,
        confidence=confidence,
        tags=sorted(set(tags or [])),
        notes=notes,
    )
    _validate_references(
        state,
        record.evidence_ids,
        record.timeline_event_ids,
        record.procedure_ids,
        [],
    )
    document = load_detections(state.root)
    iocs = [existing for existing in document.iocs if existing.id != record.id]
    iocs.append(record)
    updated = DetectionsDocument(iocs=sorted(iocs, key=lambda item: item.id), notes=document.notes)
    save_detections(state.root, updated)
    return updated, record


def add_detection_note(
    state: WorkspaceState,
    *,
    title: str,
    body: str,
    evidence_ids: list[str] | None = None,
    timeline_event_ids: list[str] | None = None,
    procedure_ids: list[str] | None = None,
    finding_ids: list[str] | None = None,
    sensitivity: DetectionSensitivity = "sensitive",
    tags: list[str] | None = None,
) -> tuple[DetectionsDocument, DetectionNote]:
    if not title.strip():
        raise DetectionError("detection note title cannot be empty")
    if not body.strip():
        raise DetectionError("detection note body cannot be empty")
    note = DetectionNote(
        id=deterministic_finding_id("detection-note", title, body, prefix="detection-note"),
        title=title,
        body=body,
        evidence_ids=sorted(set(evidence_ids or [])),
        timeline_event_ids=sorted(set(timeline_event_ids or [])),
        procedure_ids=sorted(set(procedure_ids or [])),
        finding_ids=sorted(set(finding_ids or [])),
        sensitivity=sensitivity,
        tags=sorted(set(tags or [])),
    )
    _validate_references(
        state,
        note.evidence_ids,
        note.timeline_event_ids,
        note.procedure_ids,
        note.finding_ids,
    )
    document = load_detections(state.root)
    notes = [existing for existing in document.notes if existing.id != note.id]
    notes.append(note)
    updated = DetectionsDocument(iocs=document.iocs, notes=sorted(notes, key=lambda item: item.id))
    save_detections(state.root, updated)
    return updated, note


def _validate_references(
    state: WorkspaceState,
    evidence_ids: list[str],
    timeline_event_ids: list[str],
    procedure_ids: list[str],
    finding_ids: list[str],
) -> None:
    if evidence_ids:
        known = {record.id for record in load_evidence_index(state.root).evidence}
        missing = sorted(set(evidence_ids) - known)
        if missing:
            raise DetectionError(f"unknown evidence id(s): {', '.join(missing)}")
    if timeline_event_ids:
        known = {event.id for event in load_timeline_events(state.root)}
        missing = sorted(set(timeline_event_ids) - known)
        if missing:
            raise DetectionError(f"unknown timeline event id(s): {', '.join(missing)}")
    if procedure_ids:
        known = {procedure.id for procedure in load_procedures(state.root).procedures}
        missing = sorted(set(procedure_ids) - known)
        if missing:
            raise DetectionError(f"unknown procedure id(s): {', '.join(missing)}")
    if finding_ids:
        known = {finding.id for finding in state.findings.findings}
        missing = sorted(set(finding_ids) - known)
        if missing:
            raise DetectionError(f"unknown finding id(s): {', '.join(missing)}")


def _load_json(path: Path) -> dict[str, object]:
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise DetectionError(f"invalid JSON in {path}: {exc.msg}") from exc
    if not isinstance(payload, dict):
        raise DetectionError(f"expected JSON object in {path}")
    return payload


__all__ = [
    "DETECTIONS_SCHEMA_VERSION",
    "DetectionConfidence",
    "DetectionError",
    "DetectionNote",
    "DetectionSensitivity",
    "DetectionsDocument",
    "IOCRecord",
    "IOCType",
    "add_detection_note",
    "add_ioc",
    "load_detections",
    "save_detections",
]
