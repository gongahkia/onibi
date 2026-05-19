from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app

runner = CliRunner()


def test_red_team_report_renders_workspace_handoff_sections(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    evidence = tmp_path / "note.txt"
    evidence.write_text("operator note\n", encoding="utf-8")
    add_evidence = runner.invoke(
        app,
        [
            "evidence",
            "add",
            "--workspace",
            str(workspace),
            "--file",
            str(evidence),
            "--kind",
            "note",
            "--title",
            "Operator note",
            "--json",
        ],
    )
    assert add_evidence.exit_code == 0, add_evidence.output
    evidence_id = json.loads(add_evidence.stdout)["id"]
    add_timeline = runner.invoke(
        app,
        [
            "timeline",
            "add",
            "--workspace",
            str(workspace),
            "--summary",
            "Operator recorded initial activity",
            "--evidence-id",
            evidence_id,
            "--json",
        ],
    )
    assert add_timeline.exit_code == 0, add_timeline.output
    add_objective = runner.invoke(
        app,
        [
            "objectives",
            "add",
            "--workspace",
            str(workspace),
            "--title",
            "Demonstrate impact",
            "--status",
            "achieved",
            "--json",
        ],
    )
    assert add_objective.exit_code == 0, add_objective.output
    add_procedure = runner.invoke(
        app,
        [
            "procedures",
            "add",
            "--workspace",
            str(workspace),
            "--summary",
            "Reviewed accessible files",
            "--objective-id",
            json.loads(add_objective.stdout)["id"],
            "--json",
        ],
    )
    assert add_procedure.exit_code == 0, add_procedure.output
    add_ioc = runner.invoke(
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
        ],
    )
    assert add_ioc.exit_code == 0, add_ioc.output

    report = runner.invoke(
        app,
        [
            "report",
            "--workspace",
            str(workspace),
            "--type",
            "red-team",
            "--format",
            "json",
            "--json",
        ],
    )

    assert report.exit_code == 0, report.output
    path = Path(json.loads(report.stdout)["path"])
    payload = json.loads(path.read_text(encoding="utf-8"))
    assert payload["schema_version"] == "piranesi.red-team-report.v1"
    assert payload["executive_summary"]["evidence_count"] == 1
    assert payload["executive_summary"]["timeline_event_count"] == 1
    assert payload["executive_summary"]["objective_statuses"] == {"achieved": 1}
    assert payload["detections"]["iocs"][0]["value"] == "example.internal"


def test_red_team_report_markdown_empty_workspace(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    init = runner.invoke(app, ["ingest", "init", "--workspace", str(workspace)])
    assert init.exit_code == 0, init.output

    report = runner.invoke(
        app,
        [
            "report",
            "--workspace",
            str(workspace),
            "--type",
            "red-team",
            "--format",
            "md",
            "--json",
        ],
    )

    assert report.exit_code == 0, report.output
    path = Path(json.loads(report.stdout)["path"])
    markdown = path.read_text(encoding="utf-8")
    assert "# Piranesi Red-Team Handoff" in markdown
    assert "No objectives recorded" in markdown
