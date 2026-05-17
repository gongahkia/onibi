from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.adapters.nuclei import NucleiParseError, parse_nuclei_jsonl_file
from piranesi.cli import app
from piranesi.workspace import AUDIT_LOG_FILE, FINDINGS_FILE, file_sha256, load_workspace

NUCLEI_FIXTURE = Path(__file__).parent / "fixtures" / "pentest" / "nuclei" / "localhost-http.jsonl"
NMAP_FIXTURE = Path(__file__).parent / "fixtures" / "pentest" / "nmap" / "localhost-http.xml"
runner = CliRunner()


def test_parse_real_nuclei_fixture_preserves_metadata_and_evidence() -> None:
    digest = file_sha256(NUCLEI_FIXTURE)

    result = parse_nuclei_jsonl_file(
        NUCLEI_FIXTURE,
        input_sha256=digest,
        raw_path="raw/nuclei/localhost-http.jsonl",
    )

    assert result.metadata["valid_records"] == 1
    assert result.metadata["templates"] == ["piranesi-local-lab-header"]
    finding = result.findings[0]
    assert finding.title == "Piranesi Local Lab Header Disclosure"
    assert finding.severity == "low"
    assert finding.confidence == "confirmed"
    assert finding.asset == "127.0.0.1"
    assert finding.service is not None
    assert finding.service.protocol == "http"
    assert finding.service.port == 48766
    assert finding.weakness_ids == ["CWE-200"]
    assert finding.references == ["https://example.com/piranesi/local-lab-fixture"]
    assert "local-lab" in finding.tags
    assert {item.kind for item in finding.evidence} == {
        "nuclei-curl",
        "nuclei-extractor",
        "nuclei-match",
        "nuclei-request",
        "nuclei-response",
    }
    assert [item.redacted for item in finding.evidence if item.kind.endswith("response")] == [True]


def test_parse_nuclei_allows_partial_ingest_with_malformed_lines(tmp_path: Path) -> None:
    partial = tmp_path / "partial.jsonl"
    partial.write_text(
        NUCLEI_FIXTURE.read_text(encoding="utf-8") + "\n{not-json}\n[]\n",
        encoding="utf-8",
    )

    result = parse_nuclei_jsonl_file(
        partial,
        input_sha256=file_sha256(partial),
        raw_path="raw/nuclei/partial.jsonl",
    )

    assert len(result.findings) == 1
    assert result.metadata["malformed_records"] == 2
    assert result.warnings == [
        "line 3: invalid JSON (Expecting property name enclosed in double quotes)",
        "line 4: expected JSON object",
    ]


def test_parse_nuclei_rejects_empty_or_fully_invalid_inputs(tmp_path: Path) -> None:
    empty = tmp_path / "empty.jsonl"
    empty.write_text("\n", encoding="utf-8")
    invalid = tmp_path / "invalid.jsonl"
    invalid.write_text("{not-json}\n", encoding="utf-8")

    for path, expected in [
        (empty, "empty nuclei JSONL"),
        (invalid, "no valid records"),
    ]:
        try:
            parse_nuclei_jsonl_file(
                path,
                input_sha256=file_sha256(path),
                raw_path=f"raw/nuclei/{path.name}",
            )
        except NucleiParseError as exc:
            assert expected in str(exc)
        else:
            raise AssertionError("expected NucleiParseError")


def test_ingest_nuclei_cli_merges_with_existing_workspace_and_reports(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"

    nmap = runner.invoke(
        app,
        ["ingest", "nmap", "--input", str(NMAP_FIXTURE), "--workspace", str(workspace)],
    )
    assert nmap.exit_code == 0, nmap.output
    nuclei = runner.invoke(
        app,
        [
            "ingest",
            "nuclei",
            "--input",
            str(NUCLEI_FIXTURE),
            "--workspace",
            str(workspace),
            "--json",
        ],
    )
    assert nuclei.exit_code == 0, nuclei.output
    summary = json.loads(nuclei.stdout)
    assert summary["created"] == 1
    assert summary["records"] == 1
    assert summary["warnings"] == []

    state = load_workspace(workspace)
    assert {item.tool for item in state.workspace.tool_inputs} == {"nmap", "nuclei"}
    assert len(state.findings.findings) == 3
    nuclei_finding = next(
        finding for finding in state.findings.findings if finding.provenance["tool"] == "nuclei"
    )
    assert nuclei_finding.references == ["https://example.com/piranesi/local-lab-fixture"]

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
    report_finding = next(
        finding for finding in report_payload["findings"] if finding["id"] == nuclei_finding.id
    )
    assert report_finding["references"] == ["https://example.com/piranesi/local-lab-fixture"]
    assert any(item["kind"] == "nuclei-response" for item in report_finding["evidence"])

    audit_events = [
        json.loads(line)
        for line in (workspace / AUDIT_LOG_FILE).read_text(encoding="utf-8").splitlines()
    ]
    assert audit_events[-1]["command"] == "ingest nuclei"
    assert audit_events[-1]["input_sha256"] == file_sha256(NUCLEI_FIXTURE)
    assert audit_events[-1]["output_path"] == FINDINGS_FILE
    assert audit_events[-1]["summary"]["records"] == 1


def test_ingest_nuclei_deduplicates_repeated_records(tmp_path: Path) -> None:
    duplicate = tmp_path / "duplicate.jsonl"
    line = NUCLEI_FIXTURE.read_text(encoding="utf-8").strip()
    duplicate.write_text(f"{line}\n{line}\n", encoding="utf-8")
    workspace = tmp_path / "workspace"

    result = runner.invoke(
        app,
        ["ingest", "nuclei", "--input", str(duplicate), "--workspace", str(workspace), "--json"],
    )

    assert result.exit_code == 0, result.output
    summary = json.loads(result.stdout)
    assert summary["records"] == 2
    assert summary["findings"] == 1
    state = load_workspace(workspace)
    assert len(state.findings.findings) == 1
    assert len(state.findings.findings[0].affected_instances) == 1


def test_ingest_nuclei_cli_reports_missing_files(tmp_path: Path) -> None:
    result = runner.invoke(
        app,
        [
            "ingest",
            "nuclei",
            "--input",
            str(tmp_path / "missing.jsonl"),
            "--workspace",
            str(tmp_path / "ws"),
        ],
    )

    assert result.exit_code == 2
    assert "input file does not exist" in result.output
