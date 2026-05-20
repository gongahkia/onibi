from __future__ import annotations

import json
from pathlib import Path

from jsonschema import Draft202012Validator
from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.pff import (
    PFF_SCHEMA_PATH,
    PFF_SCHEMA_VERSION,
    PFF_VERSION_HISTORY,
    PffValidationError,
    build_pff_document,
    ensure_supported_pff_version,
    load_and_validate_pff_file,
    load_pff_schema,
    migrate_pff_document,
    pff_schema_version,
    validate_pff_document,
)

FIXTURE_ROOT = Path(__file__).parent / "fixtures" / "pentest"
NMAP_FIXTURE = FIXTURE_ROOT / "nmap" / "localhost-http.xml"
NUCLEI_FIXTURE = FIXTURE_ROOT / "nuclei" / "localhost-http.jsonl"
runner = CliRunner()


def test_pff_schema_is_valid_json_schema() -> None:
    schema = load_pff_schema()

    Draft202012Validator.check_schema(schema)


def test_pff_v0_represents_current_nmap_and_nuclei_findings(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    for tool, fixture in (("nmap", NMAP_FIXTURE), ("nuclei", NUCLEI_FIXTURE)):
        result = runner.invoke(
            app,
            ["ingest", tool, "--input", str(fixture), "--workspace", str(workspace)],
        )
        assert result.exit_code == 0, result.output
    document = build_pff_document(workspace)
    schema = json.loads(PFF_SCHEMA_PATH.read_text(encoding="utf-8"))

    Draft202012Validator(schema).validate(document)
    validate_pff_document(document)

    assert document["schema_version"] == PFF_SCHEMA_VERSION
    assert {finding["source_references"][0]["tool"] for finding in document["findings"]} == {
        "nmap",
        "nuclei",
    }
    assert all("provenance" in finding for finding in document["findings"])


def test_validate_pff_file_returns_valid_document(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    result = runner.invoke(
        app,
        ["ingest", "nmap", "--input", str(NMAP_FIXTURE), "--workspace", str(workspace)],
    )
    assert result.exit_code == 0, result.output
    document = build_pff_document(workspace)
    pff_path = tmp_path / "findings.pff.json"
    pff_path.write_text(json.dumps(document), encoding="utf-8")

    loaded = load_and_validate_pff_file(pff_path)
    cli_result = runner.invoke(app, ["pff", "validate", "--input", str(pff_path), "--json"])

    assert loaded["schema_version"] == PFF_SCHEMA_VERSION
    assert cli_result.exit_code == 0, cli_result.output
    summary = json.loads(cli_result.stdout)
    assert summary["valid"] is True
    assert summary["findings"] == len(document["findings"])


def test_pff_export_command_writes_valid_artifact_and_audit(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    ingest = runner.invoke(
        app,
        ["ingest", "nuclei", "--input", str(NUCLEI_FIXTURE), "--workspace", str(workspace)],
    )
    assert ingest.exit_code == 0, ingest.output

    result = runner.invoke(app, ["pff", "export", "--workspace", str(workspace), "--json"])

    assert result.exit_code == 0, result.output
    summary = json.loads(result.stdout)
    pff_path = Path(summary["path"])
    assert pff_path == workspace / "reports" / "findings.pff.json"
    document = load_and_validate_pff_file(pff_path)
    assert summary["sha256"]
    assert summary["findings"] == len(document["findings"])
    assert document["findings"][0]["source_references"][0]["tool"] == "nuclei"

    audit_events = [
        json.loads(line)
        for line in (workspace / "audit-log.jsonl").read_text(encoding="utf-8").splitlines()
    ]
    assert audit_events[-1]["command"] == "pff export"
    assert audit_events[-1]["output_sha256"] == summary["sha256"]


def test_validate_pff_document_reports_schema_errors() -> None:
    invalid = {"schema_version": PFF_SCHEMA_VERSION, "producer": {"name": "piranesi"}}

    try:
        validate_pff_document(invalid)
    except PffValidationError as exc:
        assert "$.producer" in str(exc) or "$" in str(exc)
    else:
        raise AssertionError("expected PffValidationError")


def test_pff_versioning_accepts_current_version_and_rejects_unknown() -> None:
    document = {
        "schema_version": PFF_SCHEMA_VERSION,
        "producer": {"name": "piranesi", "version": "0.2.0"},
        "findings": [],
    }

    assert pff_schema_version(document) == PFF_SCHEMA_VERSION
    assert ensure_supported_pff_version(document) == PFF_SCHEMA_VERSION
    assert PFF_VERSION_HISTORY[PFF_SCHEMA_VERSION]["status"] == "current"
    assert migrate_pff_document(document) == document

    unknown = {**document, "schema_version": "piranesi.pff.v99"}
    try:
        ensure_supported_pff_version(unknown)
    except PffValidationError as exc:
        assert "unsupported PFF schema version" in str(exc)
    else:
        raise AssertionError("expected PffValidationError")
