from __future__ import annotations

import re
from dataclasses import dataclass

_SOURCE_SECTION_TO_TYPE = {
    "body": "request_body",
    "query": "request_param",
    "params": "request_param",
    "headers": "header",
    "cookies": "cookie",
}

_DIRECT_SOURCE_PATTERN = re.compile(
    r"^(?P<root>(?:req|request)\.(?P<section>body|query|params|headers|cookies)|"
    r"(?:req|request)\[['\"](?P<section_bracket>body|query|params|headers|cookies)['\"]\])"
    r"(?:(?:\.(?P<field>[A-Za-z_$][\w$]*))|(?:\[['\"](?P<field_bracket>[^'\"]+)['\"]\]))?$"
)
_PROPERTY_REFERENCE_PATTERN = re.compile(
    r"^(?P<object>[A-Za-z_$][\w$]*)(?:\.(?P<field>[A-Za-z_$][\w$]*)|\[['\"](?P<field_bracket>[^'\"]+)['\"]\])$"
)
_IDENTIFIER_OR_PROPERTY_PATTERN = re.compile(
    r"[A-Za-z_$][\w$]*(?:\.(?:[A-Za-z_$][\w$]*))*|\b(?:req|request)\.(?:body|query|params|headers|cookies)(?:\.(?:[A-Za-z_$][\w$]*))?"
)
_MAGIC_CONSTRUCTOR_PROTOTYPE_PATTERN = re.compile(
    r"(?:constructor\s*(?:\]|\)|['\"])?\s*(?:\.|\[)\s*(?:['\"])?prototype\b)|"
    r"(?:prototype\s*(?:\]|\)|['\"])?\s*(?:\.|\[)\s*(?:['\"])?constructor\b)",
    re.IGNORECASE,
)


@dataclass(frozen=True, slots=True)
class JavaScriptSource:
    expression: str
    source_type: str
    parameter_name: str | None


def normalize_expression(text: str) -> str:
    normalized = text.strip().rstrip(";").strip()
    while normalized.startswith("(") and normalized.endswith(")"):
        inner = normalized[1:-1].strip()
        if not inner:
            break
        normalized = inner
    return normalized


def extract_user_controlled_source(expression: str) -> JavaScriptSource | None:
    normalized = normalize_expression(expression)
    match = _DIRECT_SOURCE_PATTERN.fullmatch(normalized)
    if match is None:
        return None
    section = match.group("section") or match.group("section_bracket")
    field = match.group("field") or match.group("field_bracket")
    if section is None:
        return None
    return JavaScriptSource(
        expression=normalized,
        source_type=_SOURCE_SECTION_TO_TYPE[section],
        parameter_name=field,
    )


def property_reference(expression: str) -> tuple[str, str] | None:
    normalized = normalize_expression(expression)
    match = _PROPERTY_REFERENCE_PATTERN.fullmatch(normalized)
    if match is None:
        return None
    field = match.group("field") or match.group("field_bracket")
    if field is None:
        return None
    return match.group("object"), field


def candidate_references(expression: str) -> tuple[str, ...]:
    normalized = normalize_expression(expression)
    return tuple(
        dict.fromkeys(
            match.group(0) for match in _IDENTIFIER_OR_PROPERTY_PATTERN.finditer(normalized)
        )
    )


def detect_magic_prototype_path(text: str) -> str | None:
    normalized = normalize_expression(text)
    if "__proto__" in normalized:
        return "__proto__"
    if _MAGIC_CONSTRUCTOR_PROTOTYPE_PATTERN.search(normalized):
        return "constructor.prototype"
    return None


__all__ = [
    "JavaScriptSource",
    "candidate_references",
    "detect_magic_prototype_path",
    "extract_user_controlled_source",
    "normalize_expression",
    "property_reference",
]
