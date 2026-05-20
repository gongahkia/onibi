from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.adapters.nessus import NessusParseError, parse_nessus_file
from piranesi.cli import app
from piranesi.workspace import AUDIT_LOG_FILE, FINDINGS_FILE, file_sha256, load_workspace

NESSUS_FIXTURE = Path(__file__).parent / "fixtures" / "pentest" / "nessus" / "localhost-web.nessus"
runner = CliRunner()


def test_parse_nessus_fixture_preserves_metadata_and_evidence() -> None:
    digest = file_sha256(NESSUS_FIXTURE)

    result = parse_nessus_file(
        NESSUS_FIXTURE,
        input_sha256=digest,
        raw_path="raw/nessus/localhost-web.nessus",
    )

    assert result.metadata["valid_records"] == 1
    assert result.metadata["plugin_ids"] == ["11219"]
    finding = result.findings[0]
    assert finding.title == "Web Server Uses Plain Text HTTP"
    assert finding.severity == "medium"
    assert finding.confidence == "tool-observed"
    assert finding.asset == "127.0.0.1"
    assert finding.service is not None
    assert finding.service.name == "www"
    assert finding.service.protocol == "tcp"
    assert finding.service.port == 48766
    assert finding.weakness_ids == ["CVE-2026-0001", "CWE-319"]
    assert finding.references == ["https://www.tenable.com/plugins/nessus/11219"]
    assert "nessus-plugin-11219" in finding.tags
    assert {item.kind for item in finding.evidence} == {
        "nessus-plugin-output",
        "nessus-report-item",
    }
    plugin_output = next(item for item in finding.evidence if item.kind == "nessus-plugin-output")
    assert plugin_output.redacted is True
    assert "HTTP/1.0 200 OK" in plugin_output.value


def test_parse_nessus_rejects_empty_or_invalid_inputs(tmp_path: Path) -> None:
    empty = tmp_path / "empty.nessus"
    empty.write_text("<NessusClientData_v2/>", encoding="utf-8")
    invalid = tmp_path / "invalid.nessus"
    invalid.write_text("<issues/>", encoding="utf-8")

    for path, expected in [
        (empty, "empty Nessus XML"),
        (invalid, "unsupported Nessus XML"),
    ]:
        try:
            parse_nessus_file(
                path,
                input_sha256=file_sha256(path),
                raw_path=f"raw/nessus/{path.name}",
            )
        except NessusParseError as exc:
            assert expected in str(exc)
        else:
            raise AssertionError("expected NessusParseError")


def test_ingest_nessus_cli_creates_findings_and_audit_event(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"

    result = runner.invoke(
        app,
        [
            "ingest",
            "nessus",
            "--input",
            str(NESSUS_FIXTURE),
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
    assert {item.tool for item in state.workspace.tool_inputs} == {"nessus"}
    finding = state.findings.findings[0]
    assert finding.provenance["tool"] == "nessus"
    assert finding.provenance["plugin_id"] == "11219"

    audit_events = [
        json.loads(line)
        for line in (workspace / AUDIT_LOG_FILE).read_text(encoding="utf-8").splitlines()
    ]
    assert audit_events[-1]["command"] == "ingest nessus"
    assert audit_events[-1]["input_sha256"] == file_sha256(NESSUS_FIXTURE)
    assert audit_events[-1]["output_path"] == FINDINGS_FILE
    assert audit_events[-1]["summary"]["records"] == 1


def test_ingest_nessus_cli_reports_missing_files(tmp_path: Path) -> None:
    result = runner.invoke(
        app,
        [
            "ingest",
            "nessus",
            "--input",
            str(tmp_path / "missing.nessus"),
            "--workspace",
            str(tmp_path / "ws"),
        ],
    )

    assert result.exit_code == 2
    assert "input file does not exist" in result.output
