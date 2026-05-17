from __future__ import annotations

import pytest

from piranesi.detect.sanitizer_validation import (
    SANITIZER_EFFECTIVENESS,
    SanitizerEffectiveness,
    detect_sanitizer_bypass,
    validate_sanitizer,
    validate_sanitizer_spec,
)
from piranesi.scan.queries import QueryNode
from piranesi.scan.specs import SanitizerKind, SanitizerSpec


def _node(code: str) -> QueryNode:
    return QueryNode(
        node_id=1,
        name=None,
        code=code,
        node_type="CALL",
        line_number=1,
        column_number=1,
        method_full_name=None,
    )


@pytest.mark.parametrize(
    ("sanitizer_name", "cwe_id", "expected"),
    [
        ("html_escape", "CWE-79", SanitizerEffectiveness.EFFECTIVE),
        ("html_escape", "CWE-89", SanitizerEffectiveness.INEFFECTIVE),
        ("pg_parameterized_query", "CWE-89", SanitizerEffectiveness.EFFECTIVE),
        ("pg_parameterized_query", "CWE-79", SanitizerEffectiveness.INEFFECTIVE),
        ("uri_component_encoding", "CWE-22", SanitizerEffectiveness.PARTIAL),
        ("uri_component_encoding", "CWE-918", SanitizerEffectiveness.INEFFECTIVE),
        ("fastify_schema_validation", "CWE-79", SanitizerEffectiveness.PARTIAL),
    ],
)
def test_validate_sanitizer_matrix_is_context_sensitive(
    sanitizer_name: str,
    cwe_id: str,
    expected: SanitizerEffectiveness,
) -> None:
    assert validate_sanitizer(sanitizer_name, cwe_id) is expected
    if (
        sanitizer_name in SANITIZER_EFFECTIVENESS
        and cwe_id in SANITIZER_EFFECTIVENESS[sanitizer_name]
    ):
        assert SANITIZER_EFFECTIVENESS[sanitizer_name][cwe_id] is expected


def test_validate_sanitizer_spec_falls_back_to_effective_for_high_confidence_custom_sanitizer() -> (
    None
):
    sanitizer = SanitizerSpec(
        name="custom_safe_loader",
        pattern='cpg.call.name("safeLoad")',
        kind=SanitizerKind.NORMALIZE,
        mitigates=("CWE-502",),
        confidence=0.95,
    )

    assert validate_sanitizer_spec(sanitizer, "CWE-502") is SanitizerEffectiveness.EFFECTIVE


def test_validate_sanitizer_spec_falls_back_to_partial_for_low_confidence_custom_sanitizer() -> (
    None
):
    sanitizer = SanitizerSpec(
        name="custom_regex_guard",
        pattern='cpg.call.name("validate")',
        kind=SanitizerKind.NORMALIZE,
        mitigates=("CWE-89",),
        confidence=0.4,
        blocks_flow=False,
    )

    assert validate_sanitizer_spec(sanitizer, "CWE-89") is SanitizerEffectiveness.PARTIAL


def test_detect_sanitizer_bypass_patterns() -> None:
    patterns = detect_sanitizer_bypass(
        [
            _node('const payload = "%253Cscript%253Ealert(1)%253C/script%253E";'),
            _node('const wrapped = JSON.stringify({"html":"<ScRiPt>alert(1)</ScRiPt>"});'),
            _node('const path = "avatar%00.png";'),
        ]
    )

    assert set(patterns) >= {
        "double_encoding",
        "nested_contexts",
        "charset_tricks",
        "case_variation",
    }
