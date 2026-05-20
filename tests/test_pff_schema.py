from __future__ import annotations

import json
from pathlib import Path

from jsonschema import Draft202012Validator
from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.pff import (
    PFF_SCHEMA_PATH,
    PFF_SCHEMA_VERSION,
    PffValidationError,
    build_pff_document,
    load_and_validate_pff_file,
    load_pff_schema,
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


def test_validate_pff_document_reports_schema_errors() -> None:
    invalid = {"schema_version": PFF_SCHEMA_VERSION, "producer": {"name": "piranesi"}}

    try:
        validate_pff_document(invalid)
    except PffValidationError as exc:
        assert "$.producer" in str(exc) or "$" in str(exc)
    else:
        raise AssertionError("expected PffValidationError")
