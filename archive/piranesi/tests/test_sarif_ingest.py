from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.adapters.sarif import SarifParseError, parse_sarif_file
from piranesi.cli import app
from piranesi.workspace import AUDIT_LOG_FILE, FINDINGS_FILE, file_sha256, load_workspace

SARIF_FIXTURE = Path(__file__).parent / "fixtures" / "pentest" / "sarif" / "local-sast.sarif.json"
runner = CliRunner()


def test_parse_sarif_fixture_preserves_rule_metadata_and_location() -> None:
    digest = file_sha256(SARIF_FIXTURE)

    result = parse_sarif_file(
        SARIF_FIXTURE,
        input_sha256=digest,
        raw_path="raw/sarif/local-sast.sarif.json",
    )

    assert result.metadata["valid_records"] == 1
    assert result.metadata["rules"] == ["PY-SQLI-001"]
    finding = result.findings[0]
    assert finding.title == "SQL injection candidate"
    assert finding.severity == "high"
    assert finding.confidence == "tool-observed"
    assert finding.asset == "src/app.py"
    assert finding.weakness_ids == ["CWE-89"]
    assert finding.references == ["https://example.com/rules/py-sqli-001"]
    assert "sarif-tool-piranesilocalsast" in finding.tags
    assert finding.evidence[0].kind == "sarif-result"
    assert "src/app.py:42" in finding.evidence[0].value
    assert finding.affected_instances[0].location == "src/app.py:42"


def test_parse_sarif_rejects_empty_or_invalid_inputs(tmp_path: Path) -> None:
    empty = tmp_path / "empty.sarif.json"
    empty.write_text('{"version":"2.1.0","runs":[]}', encoding="utf-8")
    invalid = tmp_path / "invalid.sarif.json"
    invalid.write_text('{"version":"1.0.0","runs":[]}', encoding="utf-8")

    for path, expected in [
        (empty, "empty SARIF"),
        (invalid, "unsupported SARIF version"),
    ]:
        try:
            parse_sarif_file(
                path,
                input_sha256=file_sha256(path),
                raw_path=f"raw/sarif/{path.name}",
            )
        except SarifParseError as exc:
            assert expected in str(exc)
        else:
            raise AssertionError("expected SarifParseError")


def test_ingest_sarif_cli_creates_findings_and_audit_event(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"

    result = runner.invoke(
        app,
        [
            "ingest",
            "sarif",
            "--input",
            str(SARIF_FIXTURE),
            "--workspace",
            str(workspace),
            "--json",
        ],
    )

    assert result.exit_code == 0, result.output
    summary = json.loads(result.stdout)
    assert summary["created"] == 1
    assert summary["records"] == 1
    assert summary["findings"] == 1
    assert summary["warnings"] == []

    state = load_workspace(workspace)
    assert {item.tool for item in state.workspace.tool_inputs} == {"sarif"}
    finding = state.findings.findings[0]
    assert finding.provenance["tool"] == "sarif"
    assert finding.provenance["rule_id"] == "PY-SQLI-001"

    audit_events = [
        json.loads(line)
        for line in (workspace / AUDIT_LOG_FILE).read_text(encoding="utf-8").splitlines()
    ]
    assert audit_events[-1]["command"] == "ingest sarif"
    assert audit_events[-1]["input_sha256"] == file_sha256(SARIF_FIXTURE)
    assert audit_events[-1]["output_path"] == FINDINGS_FILE
    assert audit_events[-1]["summary"]["records"] == 1


def test_ingest_sarif_cli_reports_missing_files(tmp_path: Path) -> None:
    result = runner.invoke(
        app,
        [
            "ingest",
            "sarif",
            "--input",
            str(tmp_path / "missing.sarif.json"),
            "--workspace",
            str(tmp_path / "ws"),
        ],
    )

    assert result.exit_code == 2
    assert "input file does not exist" in result.output
