from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.adapters.metasploit import MetasploitParseError, parse_metasploit_json_file
from piranesi.cli import app
from piranesi.workspace import AUDIT_LOG_FILE, FINDINGS_FILE, file_sha256, load_workspace

METASPLOIT_FIXTURE = (
    Path(__file__).parent / "fixtures" / "pentest" / "metasploit" / "local-evidence.json"
)
runner = CliRunner()


def test_parse_metasploit_fixture_preserves_vuln_loot_and_session_evidence() -> None:
    digest = file_sha256(METASPLOIT_FIXTURE)

    result = parse_metasploit_json_file(
        METASPLOIT_FIXTURE,
        input_sha256=digest,
        raw_path="raw/metasploit/local-evidence.json",
    )

    assert result.metadata["valid_records"] == 3
    assert result.metadata["record_types"] == ["loot", "session", "vuln"]
    by_type = {finding.provenance["type"]: finding for finding in result.findings}
    vuln = by_type["vuln"]
    assert vuln.title == "Local lab service exposure"
    assert vuln.severity == "medium"
    assert vuln.asset == "127.0.0.1"
    assert vuln.service is not None
    assert vuln.service.port == 48766
    assert vuln.weakness_ids == ["CVE-2026-0002"]

    loot = by_type["loot"]
    assert loot.title == "Metasploit loot captured: HTTP proof marker"
    assert loot.severity == "info"
    loot_content = next(item for item in loot.evidence if item.kind == "metasploit-loot-content")
    assert loot_content.redacted is True

    session = by_type["session"]
    assert session.title == "Metasploit session observed: shell on 127.0.0.1"
    assert session.affected_instances[0].metadata["via_exploit"] == "exploit/multi/handler"


def test_parse_metasploit_rejects_empty_or_invalid_inputs(tmp_path: Path) -> None:
    empty = tmp_path / "empty.json"
    empty.write_text('{"vulns":[],"loot":[],"sessions":[]}', encoding="utf-8")
    invalid = tmp_path / "invalid.json"
    invalid.write_text("[]", encoding="utf-8")

    for path, expected in [
        (empty, "empty Metasploit JSON"),
        (invalid, "unsupported Metasploit JSON"),
    ]:
        try:
            parse_metasploit_json_file(
                path,
                input_sha256=file_sha256(path),
                raw_path=f"raw/metasploit/{path.name}",
            )
        except MetasploitParseError as exc:
            assert expected in str(exc)
        else:
            raise AssertionError("expected MetasploitParseError")


def test_ingest_metasploit_cli_creates_findings_and_audit_event(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"

    result = runner.invoke(
        app,
        [
            "ingest",
            "metasploit",
            "--input",
            str(METASPLOIT_FIXTURE),
            "--workspace",
            str(workspace),
            "--json",
        ],
    )

    assert result.exit_code == 0, result.output
    summary = json.loads(result.stdout)
    assert summary["created"] == 3
    assert summary["records"] == 3
    assert summary["findings"] == 3
    assert summary["warnings"] == []

    state = load_workspace(workspace)
    assert {item.tool for item in state.workspace.tool_inputs} == {"metasploit"}
    assert {finding.provenance["type"] for finding in state.findings.findings} == {
        "loot",
        "session",
        "vuln",
    }

    audit_events = [
        json.loads(line)
        for line in (workspace / AUDIT_LOG_FILE).read_text(encoding="utf-8").splitlines()
    ]
    assert audit_events[-1]["command"] == "ingest metasploit"
    assert audit_events[-1]["input_sha256"] == file_sha256(METASPLOIT_FIXTURE)
    assert audit_events[-1]["output_path"] == FINDINGS_FILE
    assert audit_events[-1]["summary"]["records"] == 3


def test_ingest_metasploit_cli_reports_missing_files(tmp_path: Path) -> None:
    result = runner.invoke(
        app,
        [
            "ingest",
            "metasploit",
            "--input",
            str(tmp_path / "missing.json"),
            "--workspace",
            str(tmp_path / "ws"),
        ],
    )

    assert result.exit_code == 2
    assert "input file does not exist" in result.output
