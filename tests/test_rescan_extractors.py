from __future__ import annotations

import json
from pathlib import Path

import pytest
from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.rescan.extractors import ReplayExtractionError, extract_replay_specs

FIXTURE_ROOT = Path(__file__).parent / "fixtures" / "pentest"
NMAP_FIXTURE = FIXTURE_ROOT / "nmap" / "localhost-http.xml"
NUCLEI_FIXTURE = FIXTURE_ROOT / "nuclei" / "localhost-http.jsonl"
runner = CliRunner()


def test_extract_replay_specs_for_ingested_nmap_and_nuclei(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    nmap = runner.invoke(
        app,
        ["ingest", "nmap", "--input", str(NMAP_FIXTURE), "--workspace", str(workspace)],
    )
    assert nmap.exit_code == 0, nmap.output
    nuclei = runner.invoke(
        app,
        ["ingest", "nuclei", "--input", str(NUCLEI_FIXTURE), "--workspace", str(workspace)],
    )
    assert nuclei.exit_code == 0, nuclei.output

    result = extract_replay_specs(workspace)

    assert result.warnings == []
    specs = {spec.tool: spec for spec in result.specs}
    assert set(specs) == {"nmap", "nuclei"}
    assert specs["nmap"].confidence == "high"
    assert specs["nmap"].recovered_command[:2] == ["nmap", "-sV"]
    assert specs["nmap"].target_scope == ["127.0.0.1", "localhost"]
    assert specs["nuclei"].confidence == "medium"
    assert specs["nuclei"].target_scope == ["http://127.0.0.1:48766"]
    assert "-t" in specs["nuclei"].recovered_command
    assert specs["nuclei"].metadata["templates"] == ["piranesi-local-lab.yaml"]


def test_extract_replay_specs_reports_unsupported_workspace(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    init = runner.invoke(app, ["ingest", "init", "--workspace", str(workspace)])
    assert init.exit_code == 0, init.output

    result = extract_replay_specs(workspace)

    assert result.specs == []
    assert result.warnings == ["workspace has no supported nmap or nuclei baseline evidence"]


def test_nuclei_extractor_rejects_ambiguous_targets(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    first = json.loads(NUCLEI_FIXTURE.read_text(encoding="utf-8"))
    second = dict(first)
    second["url"] = "http://127.0.0.2:48766"
    second["matched-at"] = "http://127.0.0.2:48766/"
    ambiguous = tmp_path / "ambiguous.jsonl"
    ambiguous.write_text(
        json.dumps(first) + "\n" + json.dumps(second) + "\n",
        encoding="utf-8",
    )
    ingest = runner.invoke(
        app,
        ["ingest", "nuclei", "--input", str(ambiguous), "--workspace", str(workspace)],
    )
    assert ingest.exit_code == 0, ingest.output

    result = extract_replay_specs(workspace)

    assert result.specs == []
    assert "multiple targets" in result.warnings[0]


def test_extract_replay_specs_requires_workspace(tmp_path: Path) -> None:
    with pytest.raises(ReplayExtractionError, match=r"missing workspace\.json"):
        extract_replay_specs(tmp_path / "missing")
