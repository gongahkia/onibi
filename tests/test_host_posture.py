from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.host import analyze_snapshot, load_host_input, write_host_report_outputs

FIXTURES = Path(__file__).parent / "fixtures" / "host"


def test_load_raw_osquery_trivy_bundle_and_analyze() -> None:
    snapshot = load_host_input(FIXTURES / "debian-vulnerable")
    report = analyze_snapshot(snapshot)

    titles = {finding.title for finding in report.findings}

    assert snapshot.identity.hostname == "debian-vm-01"
    assert len(snapshot.packages) == 2
    assert "Redis is listening on a public interface" in titles
    assert "SSH root login is allowed" in titles
    assert "SSH password authentication is enabled" in titles
    assert "Privileged local account present: deployer" in titles
    assert any(finding.cve_ids == ["CVE-2023-0464"] for finding in report.findings)
    assert report.posture_score < 100


def test_load_canonical_snapshot_and_write_reports(tmp_path: Path) -> None:
    snapshot = load_host_input(FIXTURES / "debian-clean" / "host_snapshot.json")
    report = analyze_snapshot(snapshot)

    write_host_report_outputs(report, tmp_path, report_format="both")

    payload = json.loads((tmp_path / "host-report.json").read_text(encoding="utf-8"))
    markdown = (tmp_path / "host-report.md").read_text(encoding="utf-8")

    assert payload["target"] == "debian-clean-01"
    assert payload["findings"] == []
    assert "Piranesi Host Posture Report" in markdown


def test_assess_cli_writes_host_reports(tmp_path: Path) -> None:
    runner = CliRunner()
    output_dir = tmp_path / "out"

    result = runner.invoke(
        app,
        [
            "assess",
            str(FIXTURES / "debian-vulnerable"),
            "--output",
            str(output_dir),
            "--format",
            "both",
        ],
    )

    assert result.exit_code == 0, result.stdout
    assert (output_dir / "host-report.json").is_file()
    assert (output_dir / "host-report.md").is_file()

    payload = json.loads((output_dir / "host-report.json").read_text(encoding="utf-8"))
    assert payload["target"] == "debian-vm-01"
    assert payload["summary"]["findings_total"] >= 5


def test_llm_mode_without_provider_reports_coverage() -> None:
    snapshot = load_host_input(FIXTURES / "debian-vulnerable")

    report = analyze_snapshot(snapshot, analysis="llm", provider=None)

    assert report.analysis_modes == ["llm"]
    assert [finding.title for finding in report.findings] == ["LLM host analysis was not completed"]
