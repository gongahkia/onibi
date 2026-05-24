from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.adapters.nmap import NmapParseError, parse_nmap_xml_file
from piranesi.cli import app
from piranesi.workspace import AUDIT_LOG_FILE, FINDINGS_FILE, file_sha256, load_workspace

FIXTURE = Path(__file__).parent / "fixtures" / "pentest" / "nmap" / "localhost-http.xml"
runner = CliRunner()


def test_parse_real_nmap_fixture_preserves_service_and_script_evidence() -> None:
    digest = file_sha256(FIXTURE)

    result = parse_nmap_xml_file(
        FIXTURE,
        input_sha256=digest,
        raw_path="raw/nmap/localhost-http.xml",
    )

    titles = {finding.title for finding in result.findings}
    assert "Open tcp/48765 http service" in titles
    assert "nmap http-title output for 127.0.0.1 tcp/48765" in titles
    assert result.metadata["nmap_version"] == "7.99"
    assert result.metadata["summary"] == {"hosts": 1, "findings": 2, "warnings": 0}
    service = next(finding for finding in result.findings if "Open tcp" in finding.title)
    assert service.confidence == "tool-observed"
    assert service.evidence[0].value.startswith("nmap observed http open")
    assert service.service is not None
    assert service.service.product == "SimpleHTTPServer"
    assert service.service.version == "0.6"


def test_parse_nmap_rejects_unsupported_xml_version(tmp_path: Path) -> None:
    scan = tmp_path / "scan.xml"
    scan.write_text(
        '<nmaprun scanner="nmap" xmloutputversion="9.99"><host /></nmaprun>',
        encoding="utf-8",
    )

    try:
        parse_nmap_xml_file(scan, input_sha256="a" * 64, raw_path="raw/nmap/scan.xml")
    except NmapParseError as exc:
        assert "unsupported nmap XML output version" in str(exc)
    else:
        raise AssertionError("expected NmapParseError")


def test_ingest_nmap_cli_creates_workspace_and_is_idempotent(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"

    first = runner.invoke(
        app,
        ["ingest", "nmap", "--input", str(FIXTURE), "--workspace", str(workspace), "--json"],
    )
    assert first.exit_code == 0, first.stdout
    first_summary = json.loads(first.stdout)
    assert first_summary["created"] == 2
    assert first_summary["updated"] == 0
    assert first_summary["findings"] == 2
    assert first_summary["warnings"] == []

    second = runner.invoke(
        app,
        ["ingest", "nmap", "--input", str(FIXTURE), "--workspace", str(workspace), "--json"],
    )
    assert second.exit_code == 0, second.stdout
    second_summary = json.loads(second.stdout)
    assert second_summary["created"] == 0
    assert second_summary["updated"] == 2

    state = load_workspace(workspace)
    assert len(state.workspace.tool_inputs) == 1
    assert len(state.findings.findings) == 2
    raw_path = workspace / state.workspace.tool_inputs[0].raw_path
    assert raw_path.is_file()

    audit_events = [
        json.loads(line)
        for line in (workspace / AUDIT_LOG_FILE).read_text(encoding="utf-8").splitlines()
    ]
    assert len(audit_events) == 2
    assert audit_events[-1]["command"] == "ingest nmap"
    assert audit_events[-1]["input_sha256"] == file_sha256(FIXTURE)
    assert audit_events[-1]["output_path"] == FINDINGS_FILE
    assert audit_events[-1]["summary"]["updated"] == 2


def test_ingest_nmap_cli_reports_invalid_xml(tmp_path: Path) -> None:
    bad_xml = tmp_path / "bad.xml"
    bad_xml.write_text("<not-xml", encoding="utf-8")

    result = runner.invoke(
        app,
        ["ingest", "nmap", "--input", str(bad_xml), "--workspace", str(tmp_path / "ws")],
    )

    assert result.exit_code == 2
    assert "invalid nmap XML" in result.output


def test_ingest_nmap_cli_reports_empty_scans(tmp_path: Path) -> None:
    empty_scan = tmp_path / "empty.xml"
    empty_scan.write_text(
        '<nmaprun scanner="nmap" xmloutputversion="1.05"></nmaprun>',
        encoding="utf-8",
    )

    result = runner.invoke(
        app,
        ["ingest", "nmap", "--input", str(empty_scan), "--workspace", str(tmp_path / "ws")],
    )

    assert result.exit_code == 2
    assert "empty nmap scan" in result.output


def test_ingest_nmap_cli_reports_missing_files(tmp_path: Path) -> None:
    result = runner.invoke(
        app,
        [
            "ingest",
            "nmap",
            "--input",
            str(tmp_path / "missing.xml"),
            "--workspace",
            str(tmp_path / "ws"),
        ],
    )

    assert result.exit_code == 2
    assert "input file does not exist" in result.output
