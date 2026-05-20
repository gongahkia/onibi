from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.adapters.zap import ZapParseError, parse_zap_json_file
from piranesi.cli import app
from piranesi.workspace import AUDIT_LOG_FILE, FINDINGS_FILE, file_sha256, load_workspace

ZAP_FIXTURE = Path(__file__).parent / "fixtures" / "pentest" / "zap" / "localhost-alerts.json"
runner = CliRunner()


def test_parse_zap_fixture_preserves_metadata_and_evidence() -> None:
    digest = file_sha256(ZAP_FIXTURE)

    result = parse_zap_json_file(
        ZAP_FIXTURE,
        input_sha256=digest,
        raw_path="raw/zap/localhost-alerts.json",
    )

    assert result.metadata["valid_records"] == 1
    assert result.metadata["zap_version"] == "2.16.1"
    finding = result.findings[0]
    assert finding.title == "Content Security Policy Header Not Set"
    assert finding.severity == "medium"
    assert finding.confidence == "tool-observed"
    assert finding.asset == "127.0.0.1"
    assert finding.service is not None
    assert finding.service.protocol == "http"
    assert finding.service.port == 48766
    assert finding.weakness_ids == ["CWE-693"]
    assert finding.references == ["https://www.zaproxy.org/docs/alerts/10038/"]
    assert "zap-alert-10038" in finding.tags
    assert {item.kind for item in finding.evidence} == {
        "zap-alert",
        "zap-evidence",
        "zap-instance",
    }
    evidence = next(item for item in finding.evidence if item.kind == "zap-evidence")
    assert evidence.redacted is True
    assert "Content-Security-Policy header missing" in evidence.value


def test_parse_zap_rejects_empty_or_invalid_inputs(tmp_path: Path) -> None:
    empty = tmp_path / "empty.json"
    empty.write_text('{"site":[]}', encoding="utf-8")
    invalid = tmp_path / "invalid.json"
    invalid.write_text("[]", encoding="utf-8")

    for path, expected in [
        (empty, "empty ZAP JSON"),
        (invalid, "unsupported ZAP JSON"),
    ]:
        try:
            parse_zap_json_file(
                path,
                input_sha256=file_sha256(path),
                raw_path=f"raw/zap/{path.name}",
            )
        except ZapParseError as exc:
            assert expected in str(exc)
        else:
            raise AssertionError("expected ZapParseError")


def test_ingest_zap_cli_creates_findings_and_audit_event(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"

    result = runner.invoke(
        app,
        [
            "ingest",
            "zap",
            "--input",
            str(ZAP_FIXTURE),
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
    assert {item.tool for item in state.workspace.tool_inputs} == {"zap"}
    finding = state.findings.findings[0]
    assert finding.provenance["tool"] == "zap"
    assert finding.provenance["zap_alert_ref"] == "10038"

    audit_events = [
        json.loads(line)
        for line in (workspace / AUDIT_LOG_FILE).read_text(encoding="utf-8").splitlines()
    ]
    assert audit_events[-1]["command"] == "ingest zap"
    assert audit_events[-1]["input_sha256"] == file_sha256(ZAP_FIXTURE)
    assert audit_events[-1]["output_path"] == FINDINGS_FILE
    assert audit_events[-1]["summary"]["records"] == 1


def test_ingest_zap_cli_reports_missing_files(tmp_path: Path) -> None:
    result = runner.invoke(
        app,
        [
            "ingest",
            "zap",
            "--input",
            str(tmp_path / "missing.json"),
            "--workspace",
            str(tmp_path / "ws"),
        ],
    )

    assert result.exit_code == 2
    assert "input file does not exist" in result.output
