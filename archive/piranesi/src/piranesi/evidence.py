from __future__ import annotations

import json
import shutil
from pathlib import Path
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from piranesi.workspace import (
    EVIDENCE_FILE,
    EVIDENCE_SCHEMA_VERSION,
    deterministic_finding_id,
    file_sha256,
    utc_now,
    workspace_path,
)

EvidenceKind = Literal[
    "screenshot",
    "c2-log",
    "transcript",
    "payload",
    "detection",
    "scanner",
    "note",
    "other",
]
EvidenceSensitivity = Literal["public", "internal", "sensitive", "secret"]


class EvidenceError(ValueError):
    """Raised when red-team evidence cannot be added or loaded safely."""


class _StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class EvidenceRecord(_StrictModel):
    id: str
    kind: EvidenceKind
    title: str
    raw_path: str
    sha256: str
    added_at: str
    observed_at: str | None = None
    source: str | None = None
    sensitivity: EvidenceSensitivity = "sensitive"
    tags: list[str] = Field(default_factory=list)
    notes: str | None = None


class EvidenceIndexDocument(_StrictModel):
    schema_version: Literal["piranesi.evidence.v1"] = EVIDENCE_SCHEMA_VERSION
    evidence: list[EvidenceRecord] = Field(default_factory=list)


def load_evidence_index(root: Path | str) -> EvidenceIndexDocument:
    path = workspace_path(root, EVIDENCE_FILE, allowed_roots=("evidence",))
    if not path.exists():
        return EvidenceIndexDocument()
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise EvidenceError(f"invalid JSON in {path}: {exc.msg}") from exc
    if not isinstance(payload, dict):
        raise EvidenceError(f"expected JSON object in {path}")
    version = payload.get("schema_version")
    if version != EVIDENCE_SCHEMA_VERSION:
        raise EvidenceError(
            f"unsupported evidence schema version {version!r}; expected {EVIDENCE_SCHEMA_VERSION!r}"
        )
    try:
        return EvidenceIndexDocument.model_validate(payload)
    except ValidationError as exc:
        raise EvidenceError(f"invalid evidence schema: {exc}") from exc


def save_evidence_index(root: Path | str, index: EvidenceIndexDocument) -> Path:
    path = workspace_path(root, EVIDENCE_FILE, allowed_roots=("evidence",))
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(index.model_dump(mode="json"), indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    return path


def add_evidence_file(
    root: Path | str,
    *,
    file_path: Path | str,
    kind: EvidenceKind,
    title: str | None = None,
    observed_at: str | None = None,
    source: str | None = None,
    sensitivity: EvidenceSensitivity = "sensitive",
    tags: list[str] | None = None,
    notes: str | None = None,
) -> tuple[EvidenceIndexDocument, EvidenceRecord]:
    source_path = Path(file_path).expanduser().resolve(strict=True)
    if not source_path.is_file():
        raise EvidenceError(f"evidence file does not exist: {file_path}")
    digest = file_sha256(source_path)
    raw_rel = Path("raw") / kind / f"{digest[:16]}-{_safe_filename(source_path.name)}"
    destination = workspace_path(root, raw_rel, allowed_roots=("raw",))
    destination.parent.mkdir(parents=True, exist_ok=True)
    if not destination.exists():
        shutil.copyfile(source_path, destination)

    record = EvidenceRecord(
        id=deterministic_finding_id("evidence", kind, digest, prefix="evidence"),
        kind=kind,
        title=title or source_path.name,
        raw_path=raw_rel.as_posix(),
        sha256=digest,
        added_at=utc_now(),
        observed_at=observed_at,
        source=source,
        sensitivity=sensitivity,
        tags=sorted(set(tags or [])),
        notes=notes,
    )
    index = load_evidence_index(root)
    records = [existing for existing in index.evidence if existing.id != record.id]
    records.append(record)
    updated = EvidenceIndexDocument(evidence=sorted(records, key=lambda item: item.id))
    save_evidence_index(root, updated)
    return updated, record


def _safe_filename(name: str) -> str:
    cleaned = "".join(char if char.isalnum() or char in {".", "-", "_"} else "-" for char in name)
    cleaned = cleaned.strip(".-")
    return cleaned or "evidence"


__all__ = [
    "EvidenceError",
    "EvidenceIndexDocument",
    "EvidenceKind",
    "EvidenceRecord",
    "EvidenceSensitivity",
    "add_evidence_file",
    "load_evidence_index",
    "save_evidence_index",
]
