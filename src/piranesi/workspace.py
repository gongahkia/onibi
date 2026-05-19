from __future__ import annotations

import hashlib
import json
import shutil
from collections.abc import Iterable, Mapping, Sequence
from dataclasses import dataclass
from datetime import UTC
from datetime import datetime as datetime_cls
from pathlib import Path
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from piranesi import __version__

WORKSPACE_SCHEMA_VERSION: Literal["piranesi.workspace.v1"] = "piranesi.workspace.v1"
FINDINGS_SCHEMA_VERSION: Literal["piranesi.findings.v1"] = "piranesi.findings.v1"
AUDIT_EVENT_SCHEMA_VERSION: Literal["piranesi.audit-event.v1"] = "piranesi.audit-event.v1"
EVIDENCE_SCHEMA_VERSION: Literal["piranesi.evidence.v1"] = "piranesi.evidence.v1"

WORKSPACE_FILE = "workspace.json"
FINDINGS_FILE = "normalized/findings.json"
AUDIT_LOG_FILE = "audit-log.jsonl"
EVIDENCE_FILE = "evidence/index.json"
TIMELINE_FILE = "timeline/events.jsonl"
OBJECTIVES_FILE = "objectives/objectives.json"
PROCEDURES_FILE = "procedures/procedures.json"
DETECTIONS_FILE = "detections/detections.json"

WORKSPACE_DIRECTORIES = (
    "raw",
    "normalized",
    "reports",
    "signatures",
    "evidence",
    "timeline",
    "objectives",
    "procedures",
    "detections",
)

Severity = Literal["info", "low", "medium", "high", "critical"]
Confidence = Literal["info", "tool-observed", "low", "medium", "high", "confirmed"]
FindingStatus = Literal["new", "open", "closed", "changed", "regressed", "accepted-risk"]


class WorkspaceError(ValueError):
    """Raised when a pentest engagement workspace is invalid or unsafe."""


class _StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class EngagementMetadata(_StrictModel):
    client: str | None = None
    project: str | None = None
    scope: list[str] = Field(default_factory=list)
    assessment_type: str | None = None
    owner: str | None = None


class ReportSettings(_StrictModel):
    title: str = "Piranesi Pentest Report"
    default_format: Literal["pdf", "json", "md"] = "md"
    redact_sensitive_evidence: bool = True


class ToolInputRecord(_StrictModel):
    id: str
    tool: str
    raw_path: str
    sha256: str
    imported_at: str
    source_path: str | None = None
    metadata: dict[str, Any] = Field(default_factory=dict)


class WorkspaceDocument(_StrictModel):
    schema_version: Literal["piranesi.workspace.v1"] = WORKSPACE_SCHEMA_VERSION
    piranesi_version: str = __version__
    engagement: EngagementMetadata = Field(default_factory=EngagementMetadata)
    tool_inputs: list[ToolInputRecord] = Field(default_factory=list)
    report_settings: ReportSettings = Field(default_factory=ReportSettings)
    created_at: str
    updated_at: str


class ServiceContext(_StrictModel):
    port: int | None = None
    protocol: str | None = None
    name: str | None = None
    product: str | None = None
    version: str | None = None


class SourceReference(_StrictModel):
    tool: str
    input_sha256: str
    raw_path: str
    locator: str | None = None
    metadata: dict[str, Any] = Field(default_factory=dict)


class EvidenceSnippet(_StrictModel):
    kind: str
    value: str
    redacted: bool = False
    locator: str | None = None


class AffectedInstance(_StrictModel):
    asset: str
    service: ServiceContext | None = None
    location: str | None = None
    metadata: dict[str, Any] = Field(default_factory=dict)


class NormalizedFinding(_StrictModel):
    id: str
    title: str
    severity: Severity = "info"
    confidence: Confidence = "info"
    status: FindingStatus = "open"
    description: str | None = None
    remediation: str | None = None
    asset: str | None = None
    service: ServiceContext | None = None
    weakness_ids: list[str] = Field(default_factory=list)
    references: list[str] = Field(default_factory=list)
    tags: list[str] = Field(default_factory=list)
    evidence: list[EvidenceSnippet] = Field(default_factory=list)
    source_references: list[SourceReference] = Field(default_factory=list)
    affected_instances: list[AffectedInstance] = Field(default_factory=list)
    first_seen: str
    last_seen: str
    provenance: dict[str, Any] = Field(default_factory=dict)


class NormalizedFindingsDocument(_StrictModel):
    schema_version: Literal["piranesi.findings.v1"] = FINDINGS_SCHEMA_VERSION
    findings: list[NormalizedFinding] = Field(default_factory=list)


class AuditEvent(_StrictModel):
    schema_version: Literal["piranesi.audit-event.v1"] = AUDIT_EVENT_SCHEMA_VERSION
    timestamp: str
    command: str
    input_path: str | None = None
    input_sha256: str | None = None
    output_path: str | None = None
    output_sha256: str | None = None
    summary: dict[str, Any] = Field(default_factory=dict)


@dataclass(frozen=True)
class WorkspaceState:
    root: Path
    workspace: WorkspaceDocument
    findings: NormalizedFindingsDocument


def utc_now() -> str:
    return datetime_cls.now(UTC).isoformat()


def deterministic_finding_id(*parts: object, prefix: str = "finding") -> str:
    normalized = "\x1f".join(str(part).strip().lower() for part in parts if part is not None)
    digest = hashlib.sha256(normalized.encode("utf-8")).hexdigest()[:24]
    return f"{prefix}:{digest}"


def file_sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def create_workspace(
    root: Path | str,
    *,
    engagement: EngagementMetadata | None = None,
    report_settings: ReportSettings | None = None,
) -> WorkspaceState:
    workspace_root = _resolve_workspace_root(root)
    _ensure_workspace_directories(workspace_root)
    _ensure_red_team_documents(workspace_root)
    workspace_path = workspace_root / WORKSPACE_FILE
    findings_path = workspace_root / FINDINGS_FILE

    if workspace_path.exists():
        state = load_workspace(workspace_root)
        changed = False
        workspace = state.workspace
        if engagement is not None:
            workspace = workspace.model_copy(update={"engagement": engagement})
            changed = True
        if report_settings is not None:
            workspace = workspace.model_copy(update={"report_settings": report_settings})
            changed = True
        if changed:
            state = WorkspaceState(
                root=workspace_root,
                workspace=workspace.model_copy(update={"updated_at": utc_now()}),
                findings=state.findings,
            )
            save_workspace(state)
        return state

    now = utc_now()
    state = WorkspaceState(
        root=workspace_root,
        workspace=WorkspaceDocument(
            engagement=engagement or EngagementMetadata(),
            report_settings=report_settings or ReportSettings(),
            created_at=now,
            updated_at=now,
        ),
        findings=NormalizedFindingsDocument(),
    )
    _write_json(workspace_path, state.workspace.model_dump(mode="json"))
    _write_json(findings_path, state.findings.model_dump(mode="json"))
    (workspace_root / AUDIT_LOG_FILE).touch(exist_ok=True)
    return state


def load_workspace(root: Path | str) -> WorkspaceState:
    workspace_root = _resolve_workspace_root(root)
    _ensure_workspace_directories(workspace_root)
    workspace_path = workspace_root / WORKSPACE_FILE
    findings_path = workspace_root / FINDINGS_FILE

    if not workspace_path.is_file():
        raise WorkspaceError(f"missing {WORKSPACE_FILE} in workspace {workspace_root}")
    if not findings_path.is_file():
        raise WorkspaceError(f"missing {FINDINGS_FILE} in workspace {workspace_root}")

    workspace_payload = _load_json(workspace_path)
    findings_payload = _load_json(findings_path)

    workspace_version = workspace_payload.get("schema_version")
    if workspace_version != WORKSPACE_SCHEMA_VERSION:
        raise WorkspaceError(
            f"unsupported workspace schema version {workspace_version!r}; "
            f"expected {WORKSPACE_SCHEMA_VERSION!r}"
        )

    findings_version = findings_payload.get("schema_version")
    if findings_version != FINDINGS_SCHEMA_VERSION:
        raise WorkspaceError(
            f"unsupported findings schema version {findings_version!r}; "
            f"expected {FINDINGS_SCHEMA_VERSION!r}"
        )

    try:
        workspace = WorkspaceDocument.model_validate(workspace_payload)
        findings = NormalizedFindingsDocument.model_validate(findings_payload)
    except ValidationError as exc:
        raise WorkspaceError(f"invalid workspace schema: {exc}") from exc

    return WorkspaceState(root=workspace_root, workspace=workspace, findings=findings)


def save_workspace(state: WorkspaceState) -> None:
    _ensure_workspace_directories(state.root)
    workspace = state.workspace.model_copy(update={"updated_at": utc_now()})
    _write_json(state.root / WORKSPACE_FILE, workspace.model_dump(mode="json"))
    _write_json(state.root / FINDINGS_FILE, state.findings.model_dump(mode="json"))


def workspace_path(
    root: Path | str,
    relative_path: Path | str,
    *,
    allowed_roots: Sequence[str] | None = None,
) -> Path:
    workspace_root = _resolve_workspace_root(root)
    rel_path = Path(relative_path)
    if rel_path.is_absolute():
        raise WorkspaceError(f"workspace path must be relative: {relative_path}")
    if not rel_path.parts:
        raise WorkspaceError("workspace path cannot be empty")
    if any(part in {"", ".", ".."} for part in rel_path.parts):
        raise WorkspaceError(f"workspace path cannot contain traversal segments: {relative_path}")
    if allowed_roots is not None and rel_path.parts[0] not in set(allowed_roots):
        joined = ", ".join(sorted(allowed_roots))
        raise WorkspaceError(f"workspace path must be under one of: {joined}")

    candidate = (workspace_root / rel_path).resolve(strict=False)
    try:
        candidate.relative_to(workspace_root)
    except ValueError as exc:
        raise WorkspaceError(f"workspace path escapes workspace root: {relative_path}") from exc
    return candidate


def copy_tool_input(
    state: WorkspaceState,
    *,
    tool: str,
    input_path: Path | str,
    metadata: Mapping[str, Any] | None = None,
) -> tuple[WorkspaceState, ToolInputRecord]:
    source = Path(input_path).expanduser().resolve(strict=True)
    digest = file_sha256(source)
    safe_name = _safe_filename(source.name)
    raw_rel = Path("raw") / tool / f"{digest[:16]}-{safe_name}"
    destination = workspace_path(state.root, raw_rel, allowed_roots=("raw",))
    destination.parent.mkdir(parents=True, exist_ok=True)
    if not destination.exists():
        shutil.copyfile(source, destination)

    record = ToolInputRecord(
        id=f"{tool}:{digest[:16]}",
        tool=tool,
        raw_path=raw_rel.as_posix(),
        sha256=digest,
        imported_at=utc_now(),
        source_path=str(source),
        metadata=dict(metadata or {}),
    )
    records = [existing for existing in state.workspace.tool_inputs if existing.id != record.id]
    records.append(record)
    workspace = state.workspace.model_copy(update={"tool_inputs": records})
    new_state = WorkspaceState(root=state.root, workspace=workspace, findings=state.findings)
    save_workspace(new_state)
    return load_workspace(state.root), record


def upsert_findings(state: WorkspaceState, findings: Iterable[NormalizedFinding]) -> WorkspaceState:
    now = utc_now()
    by_id = {finding.id: finding for finding in state.findings.findings}
    for finding in findings:
        existing = by_id.get(finding.id)
        if existing is None:
            by_id[finding.id] = finding
            continue
        by_id[finding.id] = _merge_finding(existing, finding, last_seen=now)

    merged = NormalizedFindingsDocument(findings=sorted(by_id.values(), key=lambda item: item.id))
    new_state = WorkspaceState(root=state.root, workspace=state.workspace, findings=merged)
    save_workspace(new_state)
    return load_workspace(state.root)


def append_audit_event(state: WorkspaceState, event: AuditEvent) -> Path:
    log_path = state.root / AUDIT_LOG_FILE
    log_path.parent.mkdir(parents=True, exist_ok=True)
    with log_path.open("a", encoding="utf-8") as handle:
        handle.write(json.dumps(event.model_dump(mode="json"), sort_keys=True))
        handle.write("\n")
    return log_path


def _merge_finding(
    existing: NormalizedFinding,
    incoming: NormalizedFinding,
    *,
    last_seen: str,
) -> NormalizedFinding:
    return existing.model_copy(
        update={
            "last_seen": last_seen,
            "severity": _max_severity(existing.severity, incoming.severity),
            "confidence": _max_confidence(existing.confidence, incoming.confidence),
            "evidence": _dedupe_models(existing.evidence, incoming.evidence),
            "source_references": _dedupe_models(
                existing.source_references, incoming.source_references
            ),
            "affected_instances": _dedupe_models(
                existing.affected_instances, incoming.affected_instances
            ),
            "references": sorted(set(existing.references) | set(incoming.references)),
            "tags": sorted(set(existing.tags) | set(incoming.tags)),
        }
    )


def _dedupe_models[T: BaseModel](existing: list[T], incoming: list[T]) -> list[T]:
    seen: set[str] = set()
    merged: list[T] = []
    for item in [*existing, *incoming]:
        key = json.dumps(item.model_dump(mode="json"), sort_keys=True)
        if key in seen:
            continue
        seen.add(key)
        merged.append(item)
    return merged


def _max_severity(left: Severity, right: Severity) -> Severity:
    order: dict[Severity, int] = {
        "info": 0,
        "low": 1,
        "medium": 2,
        "high": 3,
        "critical": 4,
    }
    return left if order[left] >= order[right] else right


def _max_confidence(left: Confidence, right: Confidence) -> Confidence:
    order: dict[Confidence, int] = {
        "info": 0,
        "tool-observed": 1,
        "low": 2,
        "medium": 3,
        "high": 4,
        "confirmed": 5,
    }
    return left if order[left] >= order[right] else right


def _ensure_workspace_directories(root: Path) -> None:
    root.mkdir(parents=True, exist_ok=True)
    for directory in WORKSPACE_DIRECTORIES:
        (root / directory).mkdir(parents=True, exist_ok=True)


def _ensure_red_team_documents(root: Path) -> None:
    evidence_path = root / EVIDENCE_FILE
    if not evidence_path.exists():
        _write_json(
            evidence_path,
            {
                "schema_version": EVIDENCE_SCHEMA_VERSION,
                "evidence": [],
            },
        )
    (root / TIMELINE_FILE).touch(exist_ok=True)
    for path, schema_version, key in (
        (root / OBJECTIVES_FILE, "piranesi.objectives.v1", "objectives"),
        (root / PROCEDURES_FILE, "piranesi.procedures.v1", "procedures"),
    ):
        if not path.exists():
            _write_json(path, {"schema_version": schema_version, key: []})
    detections_path = root / DETECTIONS_FILE
    if not detections_path.exists():
        _write_json(
            detections_path,
            {
                "schema_version": "piranesi.detections.v1",
                "iocs": [],
                "notes": [],
            },
        )


def _resolve_workspace_root(root: Path | str) -> Path:
    return Path(root).expanduser().resolve(strict=False)


def _load_json(path: Path) -> dict[str, Any]:
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise WorkspaceError(f"invalid JSON in {path}: {exc.msg}") from exc
    if not isinstance(payload, dict):
        raise WorkspaceError(f"expected JSON object in {path}")
    return payload


def _write_json(path: Path, payload: Mapping[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def _safe_filename(name: str) -> str:
    cleaned = "".join(char if char.isalnum() or char in {".", "-", "_"} else "-" for char in name)
    cleaned = cleaned.strip(".-")
    return cleaned or "input"


__all__ = [
    "AUDIT_EVENT_SCHEMA_VERSION",
    "AUDIT_LOG_FILE",
    "DETECTIONS_FILE",
    "EVIDENCE_FILE",
    "EVIDENCE_SCHEMA_VERSION",
    "FINDINGS_FILE",
    "FINDINGS_SCHEMA_VERSION",
    "OBJECTIVES_FILE",
    "PROCEDURES_FILE",
    "TIMELINE_FILE",
    "WORKSPACE_FILE",
    "WORKSPACE_SCHEMA_VERSION",
    "AffectedInstance",
    "AuditEvent",
    "Confidence",
    "EngagementMetadata",
    "EvidenceSnippet",
    "FindingStatus",
    "NormalizedFinding",
    "NormalizedFindingsDocument",
    "ReportSettings",
    "ServiceContext",
    "Severity",
    "SourceReference",
    "ToolInputRecord",
    "WorkspaceDocument",
    "WorkspaceError",
    "WorkspaceState",
    "append_audit_event",
    "copy_tool_input",
    "create_workspace",
    "deterministic_finding_id",
    "file_sha256",
    "load_workspace",
    "save_workspace",
    "upsert_findings",
    "utc_now",
    "workspace_path",
]
