from __future__ import annotations

from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_slack_notification_threat_model_sets_required_boundaries() -> None:
    threat_model = (ROOT / "docs" / "slack-notification-threat-model.md").read_text(
        encoding="utf-8"
    )

    for required in [
        "summary-only by default",
        "Webhook URLs are bearer secrets",
        "Dry-run mode must produce",
        "Raw evidence snippets",
        "Notification failure does not change",
        "Uploading report or evidence files to Slack",
    ]:
        assert required in threat_model
