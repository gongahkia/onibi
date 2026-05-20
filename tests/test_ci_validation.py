from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.ci_validation import CiValidationError, validate_report_bundle
from piranesi.cli import app

FIXTURE_ROOT = Path(__file__).parent / "fixtures" / "pentest"
NMAP_FIXTURE = FIXTURE_ROOT / "nmap" / "localhost-http.xml"
runner = CliRunner()


def test_ci_validation_commands_accept_pff_and_report_directory(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    ingest = runner.invoke(
        app,
        ["ingest", "nmap", "--input", str(NMAP_FIXTURE), "--workspace", str(workspace)],
    )
    assert ingest.exit_code == 0, ingest.output
    report = runner.invoke(app, ["report", "--workspace", str(workspace), "--format", "json"])
    assert report.exit_code == 0, report.output
    export = runner.invoke(app, ["pff", "export", "--workspace", str(workspace)])
    assert export.exit_code == 0, export.output

    pff_result = runner.invoke(
        app,
        [
            "ci",
            "validate-pff",
            "--input",
            str(workspace / "reports" / "findings.pff.json"),
            "--json",
        ],
    )
    reports_result = runner.invoke(
        app,
        ["ci", "validate-report-bundle", "--path", str(workspace / "reports"), "--json"],
    )

    assert pff_result.exit_code == 0, pff_result.output
    assert json.loads(pff_result.stdout)["findings"] == 2
    assert reports_result.exit_code == 0, reports_result.output
    report_summary = json.loads(reports_result.stdout)
    assert report_summary["valid"] is True
    assert report_summary["reports"][0]["schema_version"] == "piranesi.report.v1"


def test_ci_validation_accepts_red_team_archive(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    ingest = runner.invoke(
        app,
        ["ingest", "nmap", "--input", str(NMAP_FIXTURE), "--workspace", str(workspace)],
    )
    assert ingest.exit_code == 0, ingest.output
    archive = runner.invoke(
        app,
        [
            "report",
            "--workspace",
            str(workspace),
            "--type",
            "red-team",
            "--format",
            "archive",
        ],
    )
    assert archive.exit_code == 0, archive.output

    result = runner.invoke(
        app,
        [
            "ci",
            "validate-report-bundle",
            "--path",
            str(workspace / "reports" / "red-team-handoff-archive.zip"),
            "--json",
        ],
    )

    assert result.exit_code == 0, result.output
    summary = json.loads(result.stdout)
    assert summary["archives"][0]["schema_version"] == "piranesi.red-team-archive.v1"
    assert summary["archives"][0]["entries"] > 0


def test_ci_report_validation_rejects_unknown_schema(tmp_path: Path) -> None:
    report_path = tmp_path / "report.json"
    report_path.write_text(
        json.dumps({"schema_version": "piranesi.report.v99"}) + "\n",
        encoding="utf-8",
    )

    try:
        validate_report_bundle(report_path)
    except CiValidationError as exc:
        assert "unsupported report schema_version" in str(exc)
    else:
        raise AssertionError("expected CiValidationError")

    result = runner.invoke(
        app,
        ["ci", "validate-report-bundle", "--path", str(report_path), "--json-errors"],
    )
    assert result.exit_code == 1
    payload = json.loads(result.stdout)
    assert "unsupported report schema_version" in payload["error"]
