from __future__ import annotations

import json
from pathlib import Path
from typing import Any

from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.slack_notify import send_slack_notification
from piranesi.workspace import load_workspace

FIXTURE_ROOT = Path(__file__).parent / "fixtures" / "pentest"
NMAP_FIXTURE = FIXTURE_ROOT / "nmap" / "localhost-http.xml"
runner = CliRunner()


class FakeSlackClient:
    def __init__(self) -> None:
        self.payloads: list[dict[str, Any]] = []

    def post(self, payload: dict[str, Any]) -> None:
        self.payloads.append(payload)


def test_slack_notify_dry_run_is_summary_only_and_redacted(tmp_path: Path) -> None:
    workspace = _workspace_with_nmap(tmp_path)

    result = runner.invoke(
        app,
        [
            "integrations",
            "slack-notify",
            "--workspace",
            str(workspace),
            "--event",
            "report-ready",
            "--dry-run",
            "--json",
        ],
    )

    assert result.exit_code == 0, result.output
    payload = json.loads(result.stdout)
    encoded = json.dumps(payload)
    assert payload["sent"] is False
    assert payload["payload"]["text"].startswith("Piranesi report-ready")
    assert "[redacted engagement]" in encoded
    assert "127.0.0.1" not in encoded
    assert "Summary-only notification" in encoded
    assert "hooks.slack.com" not in encoded


def test_slack_notify_live_uses_client_without_webhook_echo(tmp_path: Path) -> None:
    workspace = _workspace_with_nmap(tmp_path)
    state = load_workspace(workspace)
    client = FakeSlackClient()

    result = send_slack_notification(
        state,
        event="delivered",
        dry_run=False,
        include_engagement=True,
        client=client,
    )

    assert result.sent is True
    assert len(client.payloads) == 1
    assert "delivered" in client.payloads[0]["text"]
    assert "hooks.slack.com" not in json.dumps(result.as_payload())


def _workspace_with_nmap(tmp_path: Path) -> Path:
    workspace = tmp_path / "workspace"
    init = runner.invoke(
        app,
        [
            "ingest",
            "init",
            "--workspace",
            str(workspace),
            "--client",
            "Client token=should-redact",
            "--project",
            "Slack lab",
        ],
    )
    assert init.exit_code == 0, init.output
    result = runner.invoke(
        app,
        ["ingest", "nmap", "--input", str(NMAP_FIXTURE), "--workspace", str(workspace)],
    )
    assert result.exit_code == 0, result.output
    return workspace
