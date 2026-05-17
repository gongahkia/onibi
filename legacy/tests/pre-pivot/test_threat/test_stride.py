from __future__ import annotations

import pytest
from tests.test_threat._helpers import make_finding

from piranesi.threat.stride import StrideCategory, classify_stride, stride_breakdown


@pytest.mark.parametrize(
    ("cwe", "expected_categories"),
    [
        ("CWE-89", StrideCategory.TAMPERING | StrideCategory.INFORMATION_DISCLOSURE),
        ("CWE-79", StrideCategory.TAMPERING),
        ("CWE-352", StrideCategory.SPOOFING | StrideCategory.TAMPERING),
        ("CWE-502", StrideCategory.ELEVATION_OF_PRIVILEGE | StrideCategory.TAMPERING),
        ("CWE-1333", StrideCategory.DENIAL_OF_SERVICE),
        ("CWE-639", StrideCategory.ELEVATION_OF_PRIVILEGE),
    ],
)
def test_cwe_stride_mapping(cwe: str, expected_categories: StrideCategory) -> None:
    finding = make_finding(vuln_class=cwe)
    assert classify_stride(finding) == expected_categories


def test_unknown_cwe_fallback() -> None:
    finding = make_finding(vuln_class="CWE-999999", sink_type="sql_query")
    result = classify_stride(finding)
    assert StrideCategory.TAMPERING in result


def test_stride_breakdown_groups_correctly() -> None:
    classifications = {
        "f1": StrideCategory.TAMPERING,
        "f2": StrideCategory.TAMPERING | StrideCategory.SPOOFING,
    }
    breakdown = stride_breakdown(classifications)
    assert "f1" in breakdown[StrideCategory.TAMPERING]
    assert "f2" in breakdown[StrideCategory.TAMPERING]
    assert "f2" in breakdown[StrideCategory.SPOOFING]
