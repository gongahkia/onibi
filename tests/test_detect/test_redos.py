from __future__ import annotations

from pathlib import Path

from piranesi.detect.redos import analyze_regex_pattern, extract_redos_findings, scan_text_for_redos


def test_analyze_regex_pattern_detects_nested_quantifiers() -> None:
    assert analyze_regex_pattern("^(a+)+$") == ("nested_quantifier", 0.95)


def test_analyze_regex_pattern_detects_overlapping_alternation() -> None:
    result = analyze_regex_pattern("^(a|aa)+$")
    assert result is not None
    assert result[0] == "overlapping_alternation"


def test_analyze_regex_pattern_ignores_linear_regex() -> None:
    assert analyze_regex_pattern("^[a-z]+$") is None


def test_scan_text_for_redos_finds_literal_and_constructor_patterns(tmp_path: Path) -> None:
    fixture = tmp_path / "redos.ts"
    fixture.write_text(
        "\n".join(
            [
                "const a = /^(a+)+$/;",
                "const b = new RegExp('^(a|aa)+$');",
            ]
        ),
        encoding="utf-8",
    )

    findings = scan_text_for_redos(fixture.read_text(encoding="utf-8"), path=fixture)

    assert len(findings) == 2
    assert {finding.api_name for finding in findings} == {"regex_literal", "RegExp"}


def test_extract_redos_findings_builds_candidate_findings(tmp_path: Path) -> None:
    fixture = tmp_path / "bad.py"
    fixture.write_text('pattern = re.compile(r"^(a+)+$")\n', encoding="utf-8")

    findings = extract_redos_findings(tmp_path, files=[fixture])

    assert len(findings) == 1
    assert findings[0].vuln_class == "CWE-1333"
    assert findings[0].sink.api_name == "re.compile"
    assert findings[0].metadata["redos_variant"] == "nested_quantifier"
