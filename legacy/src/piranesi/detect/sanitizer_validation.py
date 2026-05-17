from __future__ import annotations

import re
from collections.abc import Sequence
from enum import StrEnum

from piranesi.scan.queries import QueryNode
from piranesi.scan.specs import SanitizerSpec

PARTIAL_CONFIDENCE_REDUCTION = 0.3
SANITIZER_BYPASS_CONFIDENCE_BOOST = 0.2


class SanitizerEffectiveness(StrEnum):
    EFFECTIVE = "effective"
    INEFFECTIVE = "ineffective"
    PARTIAL = "partial"


def _effectiveness_map(
    *,
    effective: Sequence[str] = (),
    partial: Sequence[str] = (),
) -> dict[str, SanitizerEffectiveness]:
    mapping: dict[str, SanitizerEffectiveness] = {}
    for cwe_id in effective:
        mapping[cwe_id] = SanitizerEffectiveness.EFFECTIVE
    for cwe_id in partial:
        mapping[cwe_id] = SanitizerEffectiveness.PARTIAL
    return mapping


_COMMON_INJECTION_CWES = ("CWE-89", "CWE-79", "CWE-78", "CWE-22", "CWE-918")

SANITIZER_EFFECTIVENESS: dict[str, dict[str, SanitizerEffectiveness]] = {
    "html_escape": _effectiveness_map(effective=("CWE-79",)),
    "validator_escape": _effectiveness_map(effective=("CWE-79",)),
    "sanitize_html": _effectiveness_map(effective=("CWE-79",)),
    "dompurify_sanitize": _effectiveness_map(effective=("CWE-79",)),
    "python_markupsafe_escape": _effectiveness_map(effective=("CWE-79",)),
    "python_bleach_clean": _effectiveness_map(effective=("CWE-79",)),
    "go_html_template_execute": _effectiveness_map(effective=("CWE-79",)),
    "parameterized_query": _effectiveness_map(effective=("CWE-89",)),
    "pg_parameterized_query": _effectiveness_map(effective=("CWE-89",)),
    "python_parameterized_query": _effectiveness_map(effective=("CWE-89",)),
    "go_parameterized_query": _effectiveness_map(effective=("CWE-89",)),
    "python_shlex_quote": _effectiveness_map(effective=("CWE-78",)),
    "uri_component_encoding": _effectiveness_map(partial=("CWE-79", "CWE-22")),
    "path_normalization": _effectiveness_map(partial=("CWE-22",)),
    "path_resolve_startswith": _effectiveness_map(effective=("CWE-22",)),
    "python_path_realpath_startswith": _effectiveness_map(effective=("CWE-22",)),
    "go_filepath_clean": _effectiveness_map(partial=("CWE-22",)),
    "go_filepath_clean_hasprefix": _effectiveness_map(effective=("CWE-22",)),
    "numeric_coercion": _effectiveness_map(partial=("CWE-89",)),
    "sqlstring_escape": _effectiveness_map(partial=("CWE-89",)),
    "url_origin_check": _effectiveness_map(effective=("CWE-601",)),
    "python_url_startswith_check": _effectiveness_map(effective=("CWE-601",)),
    "json_schema_validate": _effectiveness_map(
        effective=("CWE-502",),
        partial=_COMMON_INJECTION_CWES,
    ),
    "fastify_schema_validation": _effectiveness_map(partial=_COMMON_INJECTION_CWES),
    "spring_valid_annotation": _effectiveness_map(partial=("CWE-89", "CWE-79", "CWE-78")),
    "spring_security_context": _effectiveness_map(
        effective=("CWE-352",),
        partial=("CWE-79",),
    ),
    "python_yaml_safe_load": _effectiveness_map(effective=("CWE-502",)),
    "python_json_loads_schema": _effectiveness_map(effective=("CWE-502",)),
    "file_extension_check": _effectiveness_map(effective=("CWE-434",)),
    "multer_file_filter": _effectiveness_map(effective=("CWE-434",)),
}

_DOUBLE_ENCODED_LITERAL_PATTERN = re.compile(r"%25[0-9a-fA-F]{2}")
_DOUBLE_ENCODE_CALL_PATTERN = re.compile(
    r"(?:encodeURI(?:Component)?|quote)\s*\(\s*(?:encodeURI(?:Component)?|quote)\s*\(",
)
_JSON_HTML_LITERAL_PATTERN = re.compile(
    r'\{[^{}]*["\'][^"\']+["\']\s*:\s*["\'][^"\']*<[^>]+>[^"\']*["\']',
    re.IGNORECASE,
)
_HTML_TAG_PATTERN = re.compile(r"<\s*/?\s*[A-Za-z][^>]*>")
_NULL_BYTE_PATTERN = re.compile(r"(?:%00|\\0|\\x00|\\u0000)", re.IGNORECASE)
_CHARSET_TRICK_PATTERN = re.compile(r"(?:charset\s*=|utf-7|utf7|iso-2022|shift_jis)", re.IGNORECASE)
_HTML_TAG_CAPTURE_PATTERN = re.compile(r"<\s*/?\s*([A-Za-z][A-Za-z0-9]*)")
_DANGEROUS_HTML_TAGS = frozenset({"script", "svg", "iframe", "img", "body", "a"})
_JSON_TOKENS = ("JSON.stringify", "JSON.parse", "json.dumps", "json.loads")


def validate_sanitizer(
    sanitizer_name: str,
    cwe_id: str | None,
    *,
    fallback_mitigates: Sequence[str] = (),
    fallback_confidence: float = 1.0,
    blocks_flow: bool = True,
) -> SanitizerEffectiveness:
    """Return how effective a sanitizer is for a given CWE."""
    if cwe_id is None:
        return SanitizerEffectiveness.INEFFECTIVE

    matrix = SANITIZER_EFFECTIVENESS.get(sanitizer_name)
    if matrix is not None:
        return matrix.get(cwe_id, SanitizerEffectiveness.INEFFECTIVE)

    if cwe_id not in fallback_mitigates:
        return SanitizerEffectiveness.INEFFECTIVE

    if not blocks_flow or fallback_confidence < 0.8:
        return SanitizerEffectiveness.PARTIAL
    return SanitizerEffectiveness.EFFECTIVE


def validate_sanitizer_spec(
    sanitizer_spec: SanitizerSpec,
    cwe_id: str | None,
) -> SanitizerEffectiveness:
    return validate_sanitizer(
        sanitizer_spec.name,
        cwe_id,
        fallback_mitigates=sanitizer_spec.mitigates,
        fallback_confidence=sanitizer_spec.confidence,
        blocks_flow=sanitizer_spec.blocks_flow,
    )


def detect_sanitizer_bypass(elements: Sequence[QueryNode]) -> tuple[str, ...]:
    codes = tuple(node.code for node in elements if node.code)
    if not codes:
        return ()

    patterns: list[str] = []
    if _contains_double_encoding(codes):
        patterns.append("double_encoding")
    if _contains_nested_contexts(codes):
        patterns.append("nested_contexts")
    if _contains_charset_tricks(codes):
        patterns.append("charset_tricks")
    if _contains_case_variation(codes):
        patterns.append("case_variation")
    return tuple(patterns)


def _contains_double_encoding(codes: Sequence[str]) -> bool:
    return any(
        _DOUBLE_ENCODED_LITERAL_PATTERN.search(code) or _DOUBLE_ENCODE_CALL_PATTERN.search(code)
        for code in codes
    )


def _contains_nested_contexts(codes: Sequence[str]) -> bool:
    has_html = any(_HTML_TAG_PATTERN.search(code) for code in codes)
    has_json = any(token in candidate for candidate in codes for token in _JSON_TOKENS) or any(
        _JSON_HTML_LITERAL_PATTERN.search(code) for code in codes
    )
    return has_html and has_json


def _contains_charset_tricks(codes: Sequence[str]) -> bool:
    return any(
        _NULL_BYTE_PATTERN.search(code) or _CHARSET_TRICK_PATTERN.search(code) for code in codes
    )


def _contains_case_variation(codes: Sequence[str]) -> bool:
    for code in codes:
        for match in _HTML_TAG_CAPTURE_PATTERN.finditer(code):
            tag = match.group(1)
            lowered = tag.lower()
            if lowered not in _DANGEROUS_HTML_TAGS:
                continue
            if tag != lowered and tag != lowered.upper():
                return True
    return False


__all__ = [
    "PARTIAL_CONFIDENCE_REDUCTION",
    "SANITIZER_BYPASS_CONFIDENCE_BOOST",
    "SANITIZER_EFFECTIVENESS",
    "SanitizerEffectiveness",
    "detect_sanitizer_bypass",
    "validate_sanitizer",
    "validate_sanitizer_spec",
]
