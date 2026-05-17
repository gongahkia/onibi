from __future__ import annotations

from pathlib import Path

import pytest

from piranesi.adapters import parse_external_tool_file
from piranesi.intel.normalize import normalize_adapter_result
from piranesi.intel.reporting import build_enrichment_summary
from piranesi.intel.schema import IntelSourceProvenance


@pytest.mark.parametrize(
    ("tool", "fixture_name", "expected_severity", "expected_cwe"),
    [
        ("sarif", "sample-sarif.json", "critical", "CWE-89"),
        ("codeql_sarif", "sample-sarif.json", "critical", "CWE-89"),
        ("semgrep", "sample-semgrep.json", "critical", "CWE-89"),
        ("trivy", "sample-trivy.json", "high", "CWE-1321"),
        ("zap", "sample-zap.json", "high", "CWE-89"),
    ],
)
def test_external_adapter_parsers_first_wave(
    fixtures_dir: Path,
    tool: str,
    fixture_name: str,
    expected_severity: str,
    expected_cwe: str,
) -> None:
    snapshot = fixtures_dir / "intel" / fixture_name

    result = parse_external_tool_file(tool=tool, input_path=snapshot)

    assert result.tool == tool
    assert len(result.findings) == 1
    finding = result.findings[0]
    assert finding.severity == expected_severity
    assert expected_cwe in finding.cwe_ids


def test_normalization_bundle_preserves_provenance_and_confidence(fixtures_dir: Path) -> None:
    snapshot = fixtures_dir / "intel" / "sample-semgrep.json"
    parsed = parse_external_tool_file(tool="semgrep", input_path=snapshot)
    provenance = IntelSourceProvenance.from_snapshot(
        source_name="semgrep-ci",
        tool="semgrep",
        snapshot_path=snapshot,
        trust_level="verified",
        stale_after_hours=72,
    )

    bundle = normalize_adapter_result(parse_result=parsed, source=provenance)

    assert bundle.source.snapshot_sha256
    assert len(bundle.findings) == 1
    finding = bundle.findings[0]
    assert finding.source_name == "semgrep-ci"
    assert finding.trust_score > 0.9
    assert 0.0 <= finding.confidence <= 1.0
    assert finding.finding_id.startswith("intel-")


def test_enrichment_summary_reports_severity_and_cwe_counts(fixtures_dir: Path) -> None:
    snapshot = fixtures_dir / "intel" / "sample-trivy.json"
    parsed = parse_external_tool_file(tool="trivy", input_path=snapshot)
    provenance = IntelSourceProvenance.from_snapshot(
        source_name="trivy-bom",
        tool="trivy",
        snapshot_path=snapshot,
        trust_level="trusted",
        stale_after_hours=168,
    )
    bundle = normalize_adapter_result(parse_result=parsed, source=provenance)

    summary = build_enrichment_summary(bundle)

    assert summary.findings_total == 1
    assert summary.findings_by_severity["high"] == 1
    assert "CWE-1321" in summary.top_cwe_ids
