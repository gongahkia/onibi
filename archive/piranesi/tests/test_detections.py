from __future__ import annotations

import json
from pathlib import Path

import pytest
from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.detections import DetectionError, add_detection_note, load_detections
from piranesi.workspace import DETECTIONS_FILE, create_workspace

runner = CliRunner()


def test_cli_detection_ioc_add_and_list_json(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"

    add = runner.invoke(
        app,
        [
            "detections",
            "add-ioc",
            "--workspace",
            str(workspace),
            "--type",
            "domain",
            "--value",
            "example.internal",
            "--confidence",
            "high",
            "--tag",
            "c2",
            "--json",
        ],
    )

    assert add.exit_code == 0, add.output
    payload = json.loads(add.stdout)
    assert payload["type"] == "domain"
    assert payload["value"] == "example.internal"
    assert payload["confidence"] == "high"

    listed = runner.invoke(app, ["detections", "list", "--workspace", str(workspace), "--json"])
    assert listed.exit_code == 0, listed.output
    list_payload = json.loads(listed.stdout)
    assert list_payload["ioc_count"] == 1
    assert list_payload["iocs"][0]["tags"] == ["c2"]
    assert (workspace / DETECTIONS_FILE).is_file()


def test_cli_detection_note_links_procedure(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    objective = runner.invoke(
        app,
        [
            "objectives",
            "add",
            "--workspace",
            str(workspace),
            "--title",
            "Collect proof",
            "--json",
        ],
    )
    assert objective.exit_code == 0, objective.output
    procedure = runner.invoke(
        app,
        [
            "procedures",
            "add",
            "--workspace",
            str(workspace),
            "--summary",
            "Enumerated shares",
            "--objective-id",
            json.loads(objective.stdout)["id"],
            "--json",
        ],
    )
    assert procedure.exit_code == 0, procedure.output

    note = runner.invoke(
        app,
        [
            "detections",
            "add-note",
            "--workspace",
            str(workspace),
            "--title",
            "Detect share enumeration",
            "--body",
            "Look for repeated tree connect events from the same host.",
            "--procedure-id",
            json.loads(procedure.stdout)["id"],
            "--json",
        ],
    )

    assert note.exit_code == 0, note.output
    payload = json.loads(note.stdout)
    assert payload["procedure_ids"] == [json.loads(procedure.stdout)["id"]]

    detections = load_detections(workspace)
    assert detections.notes[0].id == payload["id"]


def test_detection_note_rejects_unknown_finding_id(tmp_path: Path) -> None:
    state = create_workspace(tmp_path / "workspace")

    with pytest.raises(DetectionError, match="unknown finding id"):
        add_detection_note(
            state,
            title="Invalid finding link",
            body="Invalid body",
            finding_ids=["finding:missing"],
        )
