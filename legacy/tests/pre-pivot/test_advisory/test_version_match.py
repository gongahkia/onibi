from __future__ import annotations

import pytest

from piranesi.advisory.version_match import is_vulnerable


@pytest.mark.parametrize(
    ("version", "range_str", "expected"),
    [
        ("1.2.3", ">=1.0.0 <1.3.0", True),
        ("1.3.0", ">=1.0.0 <1.3.0", False),
        ("2.0.0-alpha.1", ">=2.0.0-alpha.0 <2.0.0", True),
        ("0.0.0", ">=1.0.0", False),
        ("1.2.5", "^1.2.3", True),
        ("2.0.0", "^1.2.3", False),
    ],
)
def test_npm_semver_matching(version: str, range_str: str, expected: bool) -> None:
    assert is_vulnerable(version, [range_str], "npm") is expected


@pytest.mark.parametrize(
    ("version", "range_str", "expected"),
    [
        ("1.2.3", ">=1.0,<1.3", True),
        ("1.3.0", ">=1.0,<1.3", False),
        ("2.0.0a1", ">=2.0.0a0,<2.0.0", True),
    ],
)
def test_pep440_matching(version: str, range_str: str, expected: bool) -> None:
    assert is_vulnerable(version, [range_str], "pypi") is expected


@pytest.mark.parametrize(
    ("version", "range_str", "expected"),
    [
        ("v1.2.3", ">=1.0.0 <1.3.0", True),
        ("v1.3.0", ">=1.0.0 <1.3.0", False),
    ],
)
def test_go_version_matching(version: str, range_str: str, expected: bool) -> None:
    assert is_vulnerable(version, [range_str], "go") is expected


@pytest.mark.parametrize(
    ("version", "range_str", "expected"),
    [
        ("1.2.3", "[1.0,1.3)", True),
        ("1.3.0", "[1.0,1.3)", False),
        ("1.2.3", ">=1.0.0 <1.3.0", True),
    ],
)
def test_maven_version_matching(version: str, range_str: str, expected: bool) -> None:
    assert is_vulnerable(version, [range_str], "maven") is expected


@pytest.mark.parametrize(
    ("version", "range_str", "expected"),
    [
        ("2.2.4", "<2.2.6.2", True),
        ("2.2.6.2", "<2.2.6.2", False),
        ("6.0.3", "~> 6.0.3", True),
        ("6.0.4", "~> 6.0.3", True),
        ("6.1.0", "~> 6.0.3", False),
        ("6.0.3.5", "~> 6.0.3.1", True),
        ("6.0.4.0", "~> 6.0.3.1", False),
    ],
)
def test_rubygems_version_matching(version: str, range_str: str, expected: bool) -> None:
    assert is_vulnerable(version, [range_str], "rubygems") is expected
