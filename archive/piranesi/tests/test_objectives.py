from __future__ import annotations

import json
from pathlib import Path

import pytest
from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.objectives import ObjectiveError, add_procedure, load_objectives, load_procedures
from piranesi.workspace import OBJECTIVES_FILE, PROCEDURES_FILE, create_workspace

runner = CliRunner()


def test_cli_objective_add_and_list_json(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"

    add = runner.invoke(
        app,
        [
            "objectives",
            "add",
            "--workspace",
            str(workspace),
            "--title",
            "Demonstrate domain user impact",
            "--status",
            "in-progress",
            "--target-asset",
            "corp.local",
            "--success-criterion",
            "Authenticated access is demonstrated",
            "--json",
        ],
    )

    assert add.exit_code == 0, add.output
    payload = json.loads(add.stdout)
    assert payload["title"] == "Demonstrate domain user impact"
    assert payload["status"] == "in-progress"

    listed = runner.invoke(app, ["objectives", "list", "--workspace", str(workspace), "--json"])
    assert listed.exit_code == 0, listed.output
    list_payload = json.loads(listed.stdout)
    assert list_payload["count"] == 1
    assert list_payload["objectives"][0]["target_assets"] == ["corp.local"]

    objective_doc = load_objectives(workspace)
    assert objective_doc.objectives[0].id == payload["id"]
    assert (workspace / OBJECTIVES_FILE).is_file()


def test_cli_procedure_add_links_objective_and_evidence(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    note = tmp_path / "note.txt"
    note.write_text("operator note\n", encoding="utf-8")
    evidence = runner.invoke(
        app,
        [
            "evidence",
            "add",
            "--file",
            str(note),
            "--kind",
            "note",
            "--workspace",
            str(workspace),
            "--json",
        ],
    )
    assert evidence.exit_code == 0, evidence.output
    objective = runner.invoke(
        app,
        [
            "objectives",
            "add",
            "--workspace",
            str(workspace),
            "--title",
            "Collect user proof",
            "--json",
        ],
    )
    assert objective.exit_code == 0, objective.output

    result = runner.invoke(
        app,
        [
            "procedures",
            "add",
            "--workspace",
            str(workspace),
            "--summary",
            "Enumerated accessible shares",
            "--tactic",
            "Discovery",
            "--technique-id",
            "T1083",
            "--technique-name",
            "File and Directory Discovery",
            "--evidence-id",
            json.loads(evidence.stdout)["id"],
            "--objective-id",
            json.loads(objective.stdout)["id"],
            "--json",
        ],
    )

    assert result.exit_code == 0, result.output
    payload = json.loads(result.stdout)
    assert payload["technique_id"] == "T1083"
    assert payload["objective_ids"] == [json.loads(objective.stdout)["id"]]

    procedures = load_procedures(workspace)
    assert procedures.procedures[0].id == payload["id"]
    assert (workspace / PROCEDURES_FILE).is_file()


def test_procedure_rejects_unknown_objective_id(tmp_path: Path) -> None:
    state = create_workspace(tmp_path / "workspace")

    with pytest.raises(ObjectiveError, match="unknown objective id"):
        add_procedure(
            state,
            summary="Invalid objective link",
            objective_ids=["objective:missing"],
        )
