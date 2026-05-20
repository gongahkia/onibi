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
    "PFF_SCHEMA_PATH",
    "PFF_SCHEMA_VERSION",
    "PffValidationError",
    "build_pff_document",
    "load_and_validate_pff_file",
    "load_pff_schema",
    "validate_pff_document",
]
