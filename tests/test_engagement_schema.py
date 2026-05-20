from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.workspace import (
    AUDIT_LOG_FILE,
    DeliveryMetadata,
    EngagementMetadata,
    EngagementMilestone,
    RetestRound,
    create_workspace,
    load_workspace,
)

runner = CliRunner()


def test_workspace_tracks_solo_engagement_management_fields(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    create_workspace(
        workspace,
        engagement=EngagementMetadata(
            client="Example Client",
            project="Loopback Lab",
            scope=["127.0.0.1"],
            assessment_type="web",
            owner="solo@example.test",
            milestones=[
                EngagementMilestone(
                    id="milestone:kickoff",
                    title="Kickoff",
                    status="complete",
                    due_date="2026-05-20",
                )
            ],
            retest_rounds=[
                RetestRound(
                    id="retest:round-1",
                    title="Round 1",
                    baseline_workspace="workspace-before",
                )
            ],
            delivery=DeliveryMetadata(status="draft"),
        ),
    )

    state = load_workspace(workspace)

    assert state.workspace.engagement.client == "Example Client"
    assert state.workspace.engagement.milestones[0].status == "complete"
    assert state.workspace.engagement.retest_rounds[0].status == "planned"
    assert state.workspace.engagement.delivery.status == "draft"


def test_legacy_workspace_defaults_engagement_management_fields(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    create_workspace(workspace, engagement=EngagementMetadata(client="Legacy"))
    payload_path = workspace / "workspace.json"
    payload = json.loads(payload_path.read_text(encoding="utf-8"))
    payload["engagement"].pop("milestones", None)
    payload["engagement"].pop("retest_rounds", None)
    payload["engagement"].pop("delivery", None)
    payload_path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    state = load_workspace(workspace)

    assert state.workspace.engagement.milestones == []
    assert state.workspace.engagement.retest_rounds == []
    assert state.workspace.engagement.delivery.status == "draft"


def test_delivery_status_command_records_audit_and_report_state(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    create_workspace(workspace, engagement=EngagementMetadata(client="Example"))

    result = runner.invoke(
        app,
        [
            "engagement",
            "delivery",
            "--workspace",
            str(workspace),
            "--status",
            "ready-for-review",
            "--reviewer",
            "Solo Reviewer",
            "--note",
            "Initial report ready.",
            "--json",
        ],
    )

    assert result.exit_code == 0, result.output
    payload = json.loads(result.stdout)
    assert payload["status"] == "ready-for-review"
    assert payload["reviewer"] == "Solo Reviewer"
    assert payload["reviewer_notes"] == ["Initial report ready."]
    state = load_workspace(workspace)
    assert state.workspace.engagement.delivery.status == "ready-for-review"
    audit_events = [
        json.loads(line)
        for line in (workspace / AUDIT_LOG_FILE).read_text(encoding="utf-8").splitlines()
    ]
    assert audit_events[-1]["command"] == "engagement delivery"
    assert audit_events[-1]["summary"]["status"] == "ready-for-review"

    report = runner.invoke(
        app,
        ["report", "--workspace", str(workspace), "--format", "md", "--json"],
    )
    assert report.exit_code == 0, report.output
    report_path = Path(json.loads(report.stdout)["path"])
    markdown = report_path.read_text(encoding="utf-8")
    assert "- Delivery status: ready-for-review" in markdown
    assert "- Reviewer: Solo Reviewer" in markdown
