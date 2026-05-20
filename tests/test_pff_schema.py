from __future__ import annotations

import json
from pathlib import Path

from jsonschema import Draft202012Validator
from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.pff import PFF_SCHEMA_PATH, PFF_SCHEMA_VERSION, build_pff_document

FIXTURE_ROOT = Path(__file__).parent / "fixtures" / "pentest"
NMAP_FIXTURE = FIXTURE_ROOT / "nmap" / "localhost-http.xml"
NUCLEI_FIXTURE = FIXTURE_ROOT / "nuclei" / "localhost-http.jsonl"
runner = CliRunner()


def test_pff_schema_is_valid_json_schema() -> None:
    schema = json.loads(PFF_SCHEMA_PATH.read_text(encoding="utf-8"))

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

    assert document["schema_version"] == PFF_SCHEMA_VERSION
    assert {finding["source_references"][0]["tool"] for finding in document["findings"]} == {
        "nmap",
        "nuclei",
    }
    assert all("provenance" in finding for finding in document["findings"])
