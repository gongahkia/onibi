from __future__ import annotations

import json
import zipfile
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


def test_red_team_report_pdf_and_archive_exports(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    public_evidence = tmp_path / "screenshot.svg"
    secret_evidence = tmp_path / "secret.txt"
    public_evidence.write_text("<svg><text>portal</text></svg>\n", encoding="utf-8")
    secret_evidence.write_text("token=secret\n", encoding="utf-8")
    add_public = runner.invoke(
        app,
        [
            "evidence",
            "add",
            "--workspace",
            str(workspace),
            "--file",
            str(public_evidence),
            "--kind",
            "screenshot",
            "--title",
            "Portal screenshot",
            "--sensitivity",
            "internal",
            "--json",
        ],
    )
    assert add_public.exit_code == 0, add_public.output
    add_secret = runner.invoke(
        app,
        [
            "evidence",
            "add",
            "--workspace",
            str(workspace),
            "--file",
            str(secret_evidence),
            "--kind",
            "payload",
            "--title",
            "Secret payload note",
            "--sensitivity",
            "secret",
            "--json",
        ],
    )
    assert add_secret.exit_code == 0, add_secret.output

    pdf = runner.invoke(
        app,
        [
            "report",
            "--workspace",
            str(workspace),
            "--type",
            "red-team",
            "--format",
            "pdf",
            "--pdf-backend",
            "reportlab",
            "--json",
        ],
    )
    archive = runner.invoke(
        app,
        [
            "report",
            "--workspace",
            str(workspace),
            "--type",
            "red-team",
            "--format",
            "archive",
            "--include-raw-evidence",
            "--json",
        ],
    )

    assert pdf.exit_code == 0, pdf.output
    pdf_path = Path(json.loads(pdf.stdout)["path"])
    assert pdf_path.read_bytes().startswith(b"%PDF")
    assert archive.exit_code == 0, archive.output
    archive_path = Path(json.loads(archive.stdout)["path"])
    with zipfile.ZipFile(archive_path) as bundle:
        names = set(bundle.namelist())
        manifest = json.loads(bundle.read("archive-manifest.json").decode("utf-8"))

    assert "reports/red-team-report.json" in names
    assert "reports/red-team-report.md" in names
    assert "reports/red-team-report-reportlab.pdf" in names
    assert "evidence/index.json" in names
    assert "timeline/events.jsonl" in names
    assert any(name.startswith("raw/screenshot/") for name in names)
    assert not any(name.startswith("raw/payload/") for name in names)
    assert manifest["include_raw_evidence"] is True
    assert manifest["include_secret_raw_evidence"] is False
