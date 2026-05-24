from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.evidence import load_evidence_index
from piranesi.workspace import AUDIT_LOG_FILE, EVIDENCE_FILE, load_workspace

runner = CliRunner()


def test_cli_evidence_add_preserves_file_and_indexes_metadata(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    screenshot = tmp_path / "screenshot.png"
    screenshot.write_bytes(b"fake-png-bytes")

    result = runner.invoke(
        app,
        [
            "evidence",
            "add",
            "--file",
            str(screenshot),
            "--kind",
            "screenshot",
            "--workspace",
            str(workspace),
            "--title",
            "Initial access screenshot",
            "--source",
            "operator",
            "--sensitivity",
            "internal",
            "--tag",
            "initial-access",
            "--json",
        ],
    )

    assert result.exit_code == 0, result.output
    payload = json.loads(result.stdout)
    assert payload["kind"] == "screenshot"
    assert payload["title"] == "Initial access screenshot"
    assert payload["source"] == "operator"
    assert payload["sensitivity"] == "internal"
    assert payload["tags"] == ["initial-access"]

    state = load_workspace(workspace)
    raw_path = state.root / payload["raw_path"]
    assert raw_path.is_file()
    assert raw_path.read_bytes() == b"fake-png-bytes"

    index = load_evidence_index(workspace)
    assert len(index.evidence) == 1
    assert index.evidence[0].id == payload["id"]

    audit_events = [
        json.loads(line)
        for line in (workspace / AUDIT_LOG_FILE).read_text(encoding="utf-8").splitlines()
    ]
    assert audit_events[-1]["command"] == "evidence add"
    assert audit_events[-1]["output_path"] == EVIDENCE_FILE
    assert audit_events[-1]["summary"]["evidence_id"] == payload["id"]


def test_cli_evidence_list_json(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    transcript = tmp_path / "terminal.txt"
    transcript.write_text("id\nwhoami\n", encoding="utf-8")

    add = runner.invoke(
        app,
        [
            "evidence",
            "add",
            "--file",
            str(transcript),
            "--kind",
            "transcript",
            "--workspace",
            str(workspace),
        ],
    )
    assert add.exit_code == 0, add.output

    result = runner.invoke(app, ["evidence", "list", "--workspace", str(workspace), "--json"])

    assert result.exit_code == 0, result.output
    payload = json.loads(result.stdout)
    assert payload["count"] == 1
    assert payload["evidence"][0]["kind"] == "transcript"
