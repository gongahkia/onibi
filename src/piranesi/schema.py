from __future__ import annotations

import json
from pathlib import Path
from typing import Any, Literal

from pydantic import BaseModel

from piranesi import __version__
from piranesi.evidence import EvidenceIndexDocument
from piranesi.report.pentest import PentestReport
from piranesi.retest import RetestResult
from piranesi.signing import ChainOfCustodyManifest
from piranesi.workspace import NormalizedFindingsDocument, WorkspaceDocument

SchemaName = Literal[
    "workspace",
    "evidence",
    "findings",
    "pentest-report",
    "chain-of-custody",
    "retest",
]

_SCHEMA_MODELS: dict[str, type[BaseModel]] = {
    "workspace": WorkspaceDocument,
    "evidence": EvidenceIndexDocument,
    "findings": NormalizedFindingsDocument,
    "pentest-report": PentestReport,
    "chain-of-custody": ChainOfCustodyManifest,
    "retest": RetestResult,
}


class SchemaExportError(ValueError):
    """Raised when a public schema name is unknown or cannot be exported."""


def available_schemas() -> tuple[str, ...]:
    return tuple(sorted(_SCHEMA_MODELS))


def build_schema(name: SchemaName | str) -> dict[str, Any]:
    """Return a JSON Schema dictionary for an active Phase 1 payload."""
    model = _SCHEMA_MODELS.get(name)
    if model is None:
        joined = ", ".join(available_schemas())
        raise SchemaExportError(f"unknown schema {name!r}; available schemas: {joined}")
    schema = model.model_json_schema()
    schema.setdefault("$schema", "https://json-schema.org/draft/2020-12/schema")
    schema.setdefault("x-piranesi-version", __version__)
    schema.setdefault("x-piranesi-schema-name", name)
    schema.setdefault("x-piranesi-compatibility", "phase-1-additive")
    return schema


def write_schema(name: SchemaName | str, output_path: str | Path) -> Path:
    """Write a public JSON Schema and return the resolved output path."""
    path = Path(output_path).expanduser().resolve(strict=False)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(build_schema(name), indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )
    return path
