from __future__ import annotations

import json
from pathlib import Path

from piranesi.workspace import (
    DeliveryMetadata,
    EngagementMetadata,
    EngagementMilestone,
    RetestRound,
    create_workspace,
    load_workspace,
)


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
