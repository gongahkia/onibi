from __future__ import annotations

import json
from collections.abc import Mapping
from pathlib import Path
from typing import Any, Literal

from jsonschema import Draft202012Validator
from jsonschema.exceptions import SchemaError, ValidationError

from piranesi import __version__
from piranesi.workspace import NormalizedFinding, WorkspaceState, load_workspace

PFF_SCHEMA_VERSION: Literal["piranesi.pff.v0"] = "piranesi.pff.v0"
LATEST_PFF_SCHEMA_VERSION = PFF_SCHEMA_VERSION
SUPPORTED_PFF_SCHEMA_VERSIONS: tuple[str, ...] = (PFF_SCHEMA_VERSION,)
PFF_VERSION_HISTORY: dict[str, dict[str, str]] = {
    PFF_SCHEMA_VERSION: {
        "status": "current",
        "introduced": "0.2.0",
        "compatibility": "initial public PFF schema; no migration required",
    }
}
PFF_SCHEMA_PATH = Path(__file__).resolve().parents[2] / "docs" / "schemas" / "pff-v0.schema.json"


class PffValidationError(ValueError):
    """Raised when a PFF document fails schema validation."""


def build_pff_document(root_or_state: Path | str | WorkspaceState) -> dict[str, Any]:
    state = (
        root_or_state
        if isinstance(root_or_state, WorkspaceState)
        else load_workspace(root_or_state)
    )
    return {
        "schema_version": PFF_SCHEMA_VERSION,
        "producer": {
            "name": "piranesi",
            "version": __version__,
        },
        "engagement": state.workspace.engagement.model_dump(mode="json"),
        "findings": [_pff_finding(finding) for finding in state.findings.findings],
        "known_gaps": [],
    }


def load_pff_schema() -> dict[str, Any]:
    try:
        payload = json.loads(PFF_SCHEMA_PATH.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise PffValidationError(f"invalid PFF schema JSON: {exc.msg}") from exc
    except OSError as exc:
        raise PffValidationError(f"cannot read PFF schema: {exc}") from exc
    if not isinstance(payload, dict):
        raise PffValidationError("PFF schema must be a JSON object")
    try:
        Draft202012Validator.check_schema(payload)
    except SchemaError as exc:
        raise PffValidationError(f"invalid PFF JSON Schema: {exc.message}") from exc
    return payload


def validate_pff_document(document: Mapping[str, Any]) -> None:
    ensure_supported_pff_version(document)
    schema = load_pff_schema()
    validator = Draft202012Validator(schema)
    errors = sorted(validator.iter_errors(document), key=lambda item: list(item.path))
    if errors:
        joined = "; ".join(_format_validation_error(error) for error in errors[:5])
        remaining = len(errors) - 5
        suffix = f"; {remaining} more validation errors" if remaining > 0 else ""
        raise PffValidationError(f"PFF document is invalid: {joined}{suffix}")


def load_and_validate_pff_file(path: Path | str) -> dict[str, Any]:
    pff_path = Path(path)
    try:
        payload = json.loads(pff_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise PffValidationError(f"invalid PFF JSON: {exc.msg}") from exc
    except OSError as exc:
        raise PffValidationError(f"cannot read PFF document: {exc}") from exc
    if not isinstance(payload, dict):
        raise PffValidationError("PFF document must be a JSON object")
    validate_pff_document(payload)
    return payload


def pff_schema_version(document: Mapping[str, Any]) -> str:
    version = document.get("schema_version")
    if not isinstance(version, str) or not version:
        raise PffValidationError("PFF document is missing schema_version")
    return version


def ensure_supported_pff_version(document: Mapping[str, Any]) -> str:
    version = pff_schema_version(document)
    if version not in SUPPORTED_PFF_SCHEMA_VERSIONS:
        supported = ", ".join(SUPPORTED_PFF_SCHEMA_VERSIONS)
        raise PffValidationError(
            f"unsupported PFF schema version {version!r}; supported versions: {supported}"
        )
    return version


def migrate_pff_document(
    document: Mapping[str, Any],
    *,
    target_version: str = LATEST_PFF_SCHEMA_VERSION,
) -> dict[str, Any]:
    source_version = ensure_supported_pff_version(document)
    if target_version not in SUPPORTED_PFF_SCHEMA_VERSIONS:
        supported = ", ".join(SUPPORTED_PFF_SCHEMA_VERSIONS)
        raise PffValidationError(
            f"unsupported target PFF schema version {target_version!r}; "
            f"supported versions: {supported}"
        )
    if source_version == target_version:
        migrated = dict(document)
        validate_pff_document(migrated)
        return migrated
    raise PffValidationError(
        f"no migration path from PFF schema {source_version!r} to {target_version!r}"
    )


def _format_validation_error(error: ValidationError) -> str:
    path = "$"
    if error.path:
        path = "$." + ".".join(str(part) for part in error.path)
    return f"{path}: {error.message}"


def _pff_finding(finding: NormalizedFinding) -> dict[str, Any]:
    assets = [finding.asset] if finding.asset is not None else []
    return {
        "id": finding.id,
        "title": finding.title,
        "severity": finding.severity,
        "confidence": finding.confidence,
        "status": finding.status,
        "description": finding.description,
        "remediation": finding.remediation,
        "assets": assets,
        "service": finding.service.model_dump(mode="json") if finding.service else None,
        "affected_instances": [
            instance.model_dump(mode="json") for instance in finding.affected_instances
        ],
        "weakness_ids": finding.weakness_ids,
        "references": finding.references,
        "tags": finding.tags,
        "evidence": [item.model_dump(mode="json") for item in finding.evidence],
        "source_references": [
            reference.model_dump(mode="json") for reference in finding.source_references
        ],
        "provenance": finding.provenance,
        "chain_of_custody": None,
        "retest_status": finding.provenance.get("retest_status"),
    }


__all__ = [
    "LATEST_PFF_SCHEMA_VERSION",
    "PFF_SCHEMA_PATH",
    "PFF_SCHEMA_VERSION",
    "PFF_VERSION_HISTORY",
    "SUPPORTED_PFF_SCHEMA_VERSIONS",
    "PffValidationError",
    "build_pff_document",
    "ensure_supported_pff_version",
    "load_and_validate_pff_file",
    "load_pff_schema",
    "migrate_pff_document",
    "pff_schema_version",
    "validate_pff_document",
]
