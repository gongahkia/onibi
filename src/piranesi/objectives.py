from __future__ import annotations

import json
from pathlib import Path
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from piranesi.evidence import load_evidence_index
from piranesi.timeline import load_timeline_events
from piranesi.workspace import (
    OBJECTIVES_FILE,
    PROCEDURES_FILE,
    WorkspaceState,
    deterministic_finding_id,
    workspace_path,
)

OBJECTIVES_SCHEMA_VERSION: Literal["piranesi.objectives.v1"] = "piranesi.objectives.v1"
PROCEDURES_SCHEMA_VERSION: Literal["piranesi.procedures.v1"] = "piranesi.procedures.v1"
ObjectiveStatus = Literal["planned", "in-progress", "achieved", "blocked", "deferred"]


class ObjectiveError(ValueError):
    """Raised when objective or procedure records are invalid."""


class _StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class ObjectiveRecord(_StrictModel):
    id: str
    title: str
    status: ObjectiveStatus = "planned"
    owner: str | None = None
    target_assets: list[str] = Field(default_factory=list)
    success_criteria: list[str] = Field(default_factory=list)
    evidence_ids: list[str] = Field(default_factory=list)
    timeline_event_ids: list[str] = Field(default_factory=list)
    tags: list[str] = Field(default_factory=list)
    notes: str | None = None


class ProcedureRecord(_StrictModel):
    id: str
    summary: str
    tactic: str | None = None
    technique_id: str | None = None
    technique_name: str | None = None
    command: str | None = None
    evidence_ids: list[str] = Field(default_factory=list)
    timeline_event_ids: list[str] = Field(default_factory=list)
    finding_ids: list[str] = Field(default_factory=list)
    objective_ids: list[str] = Field(default_factory=list)
    tags: list[str] = Field(default_factory=list)
    notes: str | None = None


class ObjectivesDocument(_StrictModel):
    schema_version: Literal["piranesi.objectives.v1"] = OBJECTIVES_SCHEMA_VERSION
    objectives: list[ObjectiveRecord] = Field(default_factory=list)


class ProceduresDocument(_StrictModel):
    schema_version: Literal["piranesi.procedures.v1"] = PROCEDURES_SCHEMA_VERSION
    procedures: list[ProcedureRecord] = Field(default_factory=list)


def load_objectives(root: Path | str) -> ObjectivesDocument:
    path = workspace_path(root, OBJECTIVES_FILE, allowed_roots=("objectives",))
    if not path.exists():
        return ObjectivesDocument()
    payload = _load_json(path)
    version = payload.get("schema_version")
    if version != OBJECTIVES_SCHEMA_VERSION:
        raise ObjectiveError(
            f"unsupported objectives schema version {version!r}; "
            f"expected {OBJECTIVES_SCHEMA_VERSION!r}"
        )
    try:
        return ObjectivesDocument.model_validate(payload)
    except ValidationError as exc:
        raise ObjectiveError(f"invalid objectives schema: {exc}") from exc


def save_objectives(root: Path | str, document: ObjectivesDocument) -> Path:
    path = workspace_path(root, OBJECTIVES_FILE, allowed_roots=("objectives",))
    _write_json(path, document.model_dump(mode="json"))
    return path


def add_objective(
    state: WorkspaceState,
    *,
    title: str,
    status: ObjectiveStatus = "planned",
    owner: str | None = None,
    target_assets: list[str] | None = None,
    success_criteria: list[str] | None = None,
    evidence_ids: list[str] | None = None,
    timeline_event_ids: list[str] | None = None,
    tags: list[str] | None = None,
    notes: str | None = None,
) -> tuple[ObjectivesDocument, ObjectiveRecord]:
    if not title.strip():
        raise ObjectiveError("objective title cannot be empty")
    record = ObjectiveRecord(
        id=deterministic_finding_id("objective", title, prefix="objective"),
        title=title,
        status=status,
        owner=owner,
        target_assets=sorted(set(target_assets or [])),
        success_criteria=success_criteria or [],
        evidence_ids=sorted(set(evidence_ids or [])),
        timeline_event_ids=sorted(set(timeline_event_ids or [])),
        tags=sorted(set(tags or [])),
        notes=notes,
    )
    _validate_common_references(state, record.evidence_ids, record.timeline_event_ids, [])
    document = load_objectives(state.root)
    records = [existing for existing in document.objectives if existing.id != record.id]
    records.append(record)
    updated = ObjectivesDocument(objectives=sorted(records, key=lambda item: item.id))
    save_objectives(state.root, updated)
    return updated, record


def load_procedures(root: Path | str) -> ProceduresDocument:
    path = workspace_path(root, PROCEDURES_FILE, allowed_roots=("procedures",))
    if not path.exists():
        return ProceduresDocument()
    payload = _load_json(path)
    version = payload.get("schema_version")
    if version != PROCEDURES_SCHEMA_VERSION:
        raise ObjectiveError(
            f"unsupported procedures schema version {version!r}; "
            f"expected {PROCEDURES_SCHEMA_VERSION!r}"
        )
    try:
        return ProceduresDocument.model_validate(payload)
    except ValidationError as exc:
        raise ObjectiveError(f"invalid procedures schema: {exc}") from exc


def save_procedures(root: Path | str, document: ProceduresDocument) -> Path:
    path = workspace_path(root, PROCEDURES_FILE, allowed_roots=("procedures",))
    _write_json(path, document.model_dump(mode="json"))
    return path


def add_procedure(
    state: WorkspaceState,
    *,
    summary: str,
    tactic: str | None = None,
    technique_id: str | None = None,
    technique_name: str | None = None,
    command: str | None = None,
    evidence_ids: list[str] | None = None,
    timeline_event_ids: list[str] | None = None,
    finding_ids: list[str] | None = None,
    objective_ids: list[str] | None = None,
    tags: list[str] | None = None,
    notes: str | None = None,
) -> tuple[ProceduresDocument, ProcedureRecord]:
    if not summary.strip():
        raise ObjectiveError("procedure summary cannot be empty")
    record = ProcedureRecord(
        id=deterministic_finding_id("procedure", summary, technique_id, prefix="procedure"),
        summary=summary,
        tactic=tactic,
        technique_id=technique_id,
        technique_name=technique_name,
        command=command,
        evidence_ids=sorted(set(evidence_ids or [])),
        timeline_event_ids=sorted(set(timeline_event_ids or [])),
        finding_ids=sorted(set(finding_ids or [])),
        objective_ids=sorted(set(objective_ids or [])),
        tags=sorted(set(tags or [])),
        notes=notes,
    )
    _validate_common_references(
        state,
        record.evidence_ids,
        record.timeline_event_ids,
        record.finding_ids,
    )
    if record.objective_ids:
        known_objective_ids = {objective.id for objective in load_objectives(state.root).objectives}
        missing = sorted(set(record.objective_ids) - known_objective_ids)
        if missing:
            raise ObjectiveError(f"unknown objective id(s): {', '.join(missing)}")
    document = load_procedures(state.root)
    records = [existing for existing in document.procedures if existing.id != record.id]
    records.append(record)
    updated = ProceduresDocument(procedures=sorted(records, key=lambda item: item.id))
    save_procedures(state.root, updated)
    return updated, record


def _validate_common_references(
    state: WorkspaceState,
    evidence_ids: list[str],
    timeline_event_ids: list[str],
    finding_ids: list[str],
) -> None:
    if evidence_ids:
        known = {record.id for record in load_evidence_index(state.root).evidence}
        missing = sorted(set(evidence_ids) - known)
        if missing:
            raise ObjectiveError(f"unknown evidence id(s): {', '.join(missing)}")
    if timeline_event_ids:
        known = {event.id for event in load_timeline_events(state.root)}
        missing = sorted(set(timeline_event_ids) - known)
        if missing:
            raise ObjectiveError(f"unknown timeline event id(s): {', '.join(missing)}")
    if finding_ids:
        known = {finding.id for finding in state.findings.findings}
        missing = sorted(set(finding_ids) - known)
        if missing:
            raise ObjectiveError(f"unknown finding id(s): {', '.join(missing)}")


def _load_json(path: Path) -> dict[str, object]:
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise ObjectiveError(f"invalid JSON in {path}: {exc.msg}") from exc
    if not isinstance(payload, dict):
        raise ObjectiveError(f"expected JSON object in {path}")
    return payload


def _write_json(path: Path, payload: object) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")


__all__ = [
    "OBJECTIVES_SCHEMA_VERSION",
    "PROCEDURES_SCHEMA_VERSION",
    "ObjectiveError",
    "ObjectiveRecord",
    "ObjectiveStatus",
    "ObjectivesDocument",
    "ProcedureRecord",
    "ProceduresDocument",
    "add_objective",
    "add_procedure",
    "load_objectives",
    "load_procedures",
]
