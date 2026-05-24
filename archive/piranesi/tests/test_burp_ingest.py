from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.adapters.burp import BurpParseError, parse_burp_xml_file
from piranesi.cli import app
from piranesi.workspace import AUDIT_LOG_FILE, FINDINGS_FILE, file_sha256, load_workspace

BURP_FIXTURE = Path(__file__).parent / "fixtures" / "pentest" / "burp" / "lab-issues.xml"
runner = CliRunner()


def test_parse_real_burp_fixture_preserves_metadata_and_evidence() -> None:
    digest = file_sha256(BURP_FIXTURE)

    result = parse_burp_xml_file(
        BURP_FIXTURE,
        input_sha256=digest,
        raw_path="raw/burp/lab-issues.xml",
    )

    assert result.metadata["valid_records"] == 1
    assert result.metadata["burp_version"] == "2026.4.1"
    finding = result.findings[0]
    assert finding.title == "Cross-site scripting reflected"
    assert finding.severity == "high"
    assert finding.confidence == "tool-observed"
    assert finding.asset == "lab.example.test"
    assert finding.service is not None
    assert finding.service.protocol == "https"
    assert finding.service.port == 443
    assert finding.weakness_ids == ["CWE-79"]
    assert finding.references == [
        "https://portswigger.net/web-security/cross-site-scripting/reflected"
    ]
    assert "burp" in finding.tags
    assert {item.kind for item in finding.evidence} == {
        "burp-issue",
        "burp-request",
        "burp-response",
    }
    request = next(item for item in finding.evidence if item.kind == "burp-request")
    assert request.redacted is True
    assert "GET /account HTTP/1.1" in request.value


def test_parse_burp_rejects_empty_or_invalid_inputs(tmp_path: Path) -> None:
    empty = tmp_path / "empty.xml"
    empty.write_text("<issues/>", encoding="utf-8")
    invalid = tmp_path / "invalid.xml"
    invalid.write_text("<nmaprun/>", encoding="utf-8")

    for path, expected in [
        (empty, "empty Burp Issues XML"),
        (invalid, "unsupported Burp Issues XML"),
    ]:
        try:
            parse_burp_xml_file(
                path,
                input_sha256=file_sha256(path),
                raw_path=f"raw/burp/{path.name}",
            )
        except BurpParseError as exc:
            assert expected in str(exc)
        else:
            raise AssertionError("expected BurpParseError")


def test_ingest_burp_cli_creates_findings_and_report_evidence(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"

    result = runner.invoke(
        app,
        [
            "ingest",
            "burp",
            "--input",
            str(BURP_FIXTURE),
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
    assert {item.tool for item in state.workspace.tool_inputs} == {"burp"}
    finding = state.findings.findings[0]
    assert finding.provenance["tool"] == "burp"
    assert finding.provenance["burp_confidence"] == "Certain"

    report = runner.invoke(
        app,
        [
            "report",
            "--workspace",
            str(workspace),
            "--format",
            "json",
            "--include-sensitive-evidence",
            "--json",
        ],
    )
    assert report.exit_code == 0, report.output
    report_path = Path(json.loads(report.stdout)["path"])
    report_payload = json.loads(report_path.read_text(encoding="utf-8"))
    report_finding = report_payload["findings"][0]
    assert report_finding["confidence"] == "tool-observed"
    assert any(item["kind"] == "burp-request" for item in report_finding["evidence"])

    audit_events = [
        json.loads(line)
        for line in (workspace / AUDIT_LOG_FILE).read_text(encoding="utf-8").splitlines()
    ]
    assert audit_events[-1]["command"] == "ingest burp"
    assert audit_events[-1]["input_sha256"] == file_sha256(BURP_FIXTURE)
    assert audit_events[-1]["output_path"] == FINDINGS_FILE
    assert audit_events[-1]["summary"]["records"] == 1


def test_ingest_burp_cli_reports_missing_files(tmp_path: Path) -> None:
    result = runner.invoke(
        app,
        [
            "ingest",
            "burp",
            "--input",
            str(tmp_path / "missing.xml"),
            "--workspace",
            str(tmp_path / "ws"),
        ],
    )

    assert result.exit_code == 2
    assert "input file does not exist" in result.output
