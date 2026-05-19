from __future__ import annotations

import json
from pathlib import Path

import pytest
from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.timeline import TimelineError, append_timeline_event, load_timeline_events
from piranesi.workspace import AUDIT_LOG_FILE, TIMELINE_FILE, create_workspace

runner = CliRunner()


def test_append_timeline_event_links_evidence(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    note = tmp_path / "note.txt"
    note.write_text("operator note\n", encoding="utf-8")
    add = runner.invoke(
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
    assert add.exit_code == 0, add.output
    evidence_id = json.loads(add.stdout)["id"]

    result = runner.invoke(
        app,
        [
            "timeline",
            "add",
            "--workspace",
            str(workspace),
            "--summary",
            "Initial foothold observed",
            "--phase",
            "initial-access",
            "--actor",
            "operator",
            "--evidence-id",
            evidence_id,
            "--tag",
            "foothold",
            "--json",
        ],
    )

    assert result.exit_code == 0, result.output
    payload = json.loads(result.stdout)
    assert payload["summary"] == "Initial foothold observed"
    assert payload["evidence_ids"] == [evidence_id]

    events = load_timeline_events(workspace)
    assert len(events) == 1
    assert events[0].phase == "initial-access"

    audit_events = [
        json.loads(line)
        for line in (workspace / AUDIT_LOG_FILE).read_text(encoding="utf-8").splitlines()
    ]
    assert audit_events[-1]["command"] == "timeline add"
    assert audit_events[-1]["output_path"] == TIMELINE_FILE


def test_timeline_rejects_unknown_evidence_id(tmp_path: Path) -> None:
    state = create_workspace(tmp_path / "workspace")

    with pytest.raises(TimelineError, match="unknown evidence id"):
        append_timeline_event(
            state,
            summary="Invalid reference",
            evidence_ids=["evidence:missing"],
        )


def test_cli_timeline_list_json(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    add = runner.invoke(
        app,
        [
            "timeline",
            "add",
            "--workspace",
            str(workspace),
            "--timestamp",
            "2026-05-20T10:00:00+00:00",
            "--summary",
            "Operator started engagement",
        ],
    )
    assert add.exit_code == 0, add.output

    result = runner.invoke(app, ["timeline", "list", "--workspace", str(workspace), "--json"])

    assert result.exit_code == 0, result.output
    payload = json.loads(result.stdout)
    assert payload["count"] == 1
    assert payload["events"][0]["summary"] == "Operator started engagement"
