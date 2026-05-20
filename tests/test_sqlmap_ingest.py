from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.adapters.sqlmap import SqlmapParseError, parse_sqlmap_file
from piranesi.cli import app
from piranesi.workspace import AUDIT_LOG_FILE, FINDINGS_FILE, file_sha256, load_workspace

SQLMAP_FIXTURE = Path(__file__).parent / "fixtures" / "pentest" / "sqlmap" / "localhost-sqli.json"
runner = CliRunner()


def test_parse_sqlmap_fixture_preserves_payload_as_redacted_evidence() -> None:
    digest = file_sha256(SQLMAP_FIXTURE)

    result = parse_sqlmap_file(
        SQLMAP_FIXTURE,
        input_sha256=digest,
        raw_path="raw/sqlmap/localhost-sqli.json",
    )

    assert result.metadata["valid_records"] == 1
    finding = result.findings[0]
    assert finding.title == "sqlmap reported SQL injection in GET id"
    assert finding.severity == "high"
    assert finding.confidence == "tool-observed"
    assert finding.asset == "127.0.0.1"
    assert finding.service is not None
    assert finding.service.protocol == "http"
    assert finding.service.port == 48766
    assert finding.weakness_ids == ["CWE-89"]
    assert finding.references == ["https://cwe.mitre.org/data/definitions/89.html"]
    assert set(finding.tags) == {"cwe-89", "sql-injection", "sqlmap"}
    payload = next(item for item in finding.evidence if item.kind == "sqlmap-payload")
    assert payload.redacted is True
    assert "9277=9277" in payload.value


def test_parse_sqlmap_rejects_empty_or_invalid_inputs(tmp_path: Path) -> None:
    empty = tmp_path / "empty.json"
    empty.write_text("{}", encoding="utf-8")
    invalid = tmp_path / "invalid.json"
    invalid.write_text("[]", encoding="utf-8")

    for path, expected in [
        (empty, "no vulnerability records"),
        (invalid, "no vulnerability records"),
    ]:
        try:
            parse_sqlmap_file(
                path,
                input_sha256=file_sha256(path),
                raw_path=f"raw/sqlmap/{path.name}",
            )
        except SqlmapParseError as exc:
            assert expected in str(exc)
        else:
            raise AssertionError("expected SqlmapParseError")


def test_ingest_sqlmap_cli_creates_findings_and_audit_event(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"

    result = runner.invoke(
        app,
        [
            "ingest",
            "sqlmap",
            "--input",
            str(SQLMAP_FIXTURE),
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
    assert {item.tool for item in state.workspace.tool_inputs} == {"sqlmap"}
    finding = state.findings.findings[0]
    assert finding.provenance["tool"] == "sqlmap"
    assert finding.provenance["dbms"] == "SQLite"

    audit_events = [
        json.loads(line)
        for line in (workspace / AUDIT_LOG_FILE).read_text(encoding="utf-8").splitlines()
    ]
    assert audit_events[-1]["command"] == "ingest sqlmap"
    assert audit_events[-1]["input_sha256"] == file_sha256(SQLMAP_FIXTURE)
    assert audit_events[-1]["output_path"] == FINDINGS_FILE
    assert audit_events[-1]["summary"]["records"] == 1


def test_ingest_sqlmap_cli_reports_missing_files(tmp_path: Path) -> None:
    result = runner.invoke(
        app,
        [
            "ingest",
            "sqlmap",
            "--input",
            str(tmp_path / "missing.json"),
            "--workspace",
            str(tmp_path / "ws"),
        ],
    )

    assert result.exit_code == 2
    assert "input file does not exist" in result.output
