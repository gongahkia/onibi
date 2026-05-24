from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.adapters.ffuf import FfufParseError, parse_ffuf_json_file
from piranesi.cli import app
from piranesi.workspace import AUDIT_LOG_FILE, FINDINGS_FILE, file_sha256, load_workspace

FFUF_FIXTURE = Path(__file__).parent / "fixtures" / "pentest" / "ffuf" / "localhost-discovery.json"
runner = CliRunner()


def test_parse_ffuf_fixture_preserves_discovery_metadata() -> None:
    digest = file_sha256(FFUF_FIXTURE)

    result = parse_ffuf_json_file(
        FFUF_FIXTURE,
        input_sha256=digest,
        raw_path="raw/ffuf/localhost-discovery.json",
    )

    assert result.metadata["valid_records"] == 1
    finding = result.findings[0]
    assert finding.title == "ffuf discovered HTTP 200 at /admin"
    assert finding.severity == "info"
    assert finding.confidence == "tool-observed"
    assert finding.asset == "127.0.0.1"
    assert finding.service is not None
    assert finding.service.protocol == "http"
    assert finding.service.port == 48766
    assert set(finding.tags) == {"content-discovery", "ffuf"}
    assert finding.evidence[0].kind == "ffuf-result"
    assert "status=200" in finding.evidence[0].value
    assert finding.source_references[0].metadata["length"] == 44


def test_parse_ffuf_rejects_empty_or_invalid_inputs(tmp_path: Path) -> None:
    empty = tmp_path / "empty.json"
    empty.write_text('{"results":[]}', encoding="utf-8")
    invalid = tmp_path / "invalid.json"
    invalid.write_text("[]", encoding="utf-8")

    for path, expected in [
        (empty, "empty ffuf JSON"),
        (invalid, "unsupported ffuf JSON"),
    ]:
        try:
            parse_ffuf_json_file(
                path,
                input_sha256=file_sha256(path),
                raw_path=f"raw/ffuf/{path.name}",
            )
        except FfufParseError as exc:
            assert expected in str(exc)
        else:
            raise AssertionError("expected FfufParseError")


def test_ingest_ffuf_cli_creates_findings_and_audit_event(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"

    result = runner.invoke(
        app,
        [
            "ingest",
            "ffuf",
            "--input",
            str(FFUF_FIXTURE),
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
    assert {item.tool for item in state.workspace.tool_inputs} == {"ffuf"}
    assert state.findings.findings[0].provenance["tool"] == "ffuf"

    audit_events = [
        json.loads(line)
        for line in (workspace / AUDIT_LOG_FILE).read_text(encoding="utf-8").splitlines()
    ]
    assert audit_events[-1]["command"] == "ingest ffuf"
    assert audit_events[-1]["input_sha256"] == file_sha256(FFUF_FIXTURE)
    assert audit_events[-1]["output_path"] == FINDINGS_FILE
    assert audit_events[-1]["summary"]["records"] == 1


def test_ingest_ffuf_cli_reports_missing_files(tmp_path: Path) -> None:
    result = runner.invoke(
        app,
        [
            "ingest",
            "ffuf",
            "--input",
            str(tmp_path / "missing.json"),
            "--workspace",
            str(tmp_path / "ws"),
        ],
    )

    assert result.exit_code == 2
    assert "input file does not exist" in result.output
