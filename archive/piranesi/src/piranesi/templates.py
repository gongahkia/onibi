from __future__ import annotations

import json
from pathlib import Path
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, ValidationError

TEMPLATE_LIBRARY_SCHEMA_VERSION: Literal["piranesi.template-library.v1"] = (
    "piranesi.template-library.v1"
)
TemplateKind = Literal["methodology", "remediation", "section"]


class TemplateError(ValueError):
    """Raised when a local report template library is invalid."""


class _StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class ReportTemplate(_StrictModel):
    id: str
    kind: TemplateKind
    title: str
    body: str
    version: str = "v1"


class TemplateLibrary(_StrictModel):
    schema_version: Literal["piranesi.template-library.v1"] = TEMPLATE_LIBRARY_SCHEMA_VERSION
    templates: list[ReportTemplate] = Field(default_factory=list)


def load_template_library(path: Path | str) -> TemplateLibrary:
    template_path = Path(path).expanduser().resolve(strict=False)
    try:
        payload = json.loads(template_path.read_text(encoding="utf-8"))
        return TemplateLibrary.model_validate(payload)
    except OSError as exc:
        raise TemplateError(f"cannot read template library {template_path}: {exc}") from exc
    except json.JSONDecodeError as exc:
        raise TemplateError(f"invalid template library JSON: {exc.msg}") from exc
    except ValidationError as exc:
        raise TemplateError(f"invalid template library schema: {exc}") from exc


def select_templates(library: TemplateLibrary, template_ids: list[str]) -> list[ReportTemplate]:
    by_id = {template.id: template for template in library.templates}
    selected: list[ReportTemplate] = []
    missing = [template_id for template_id in template_ids if template_id not in by_id]
    if missing:
        raise TemplateError("unknown template id(s): " + ", ".join(sorted(missing)))
    for template_id in template_ids:
        selected.append(by_id[template_id])
    return selected


__all__ = [
    "TEMPLATE_LIBRARY_SCHEMA_VERSION",
    "ReportTemplate",
    "TemplateError",
    "TemplateKind",
    "TemplateLibrary",
    "load_template_library",
    "select_templates",
]
