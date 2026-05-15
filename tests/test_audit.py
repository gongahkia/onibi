from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.audit import append_audit_event
from piranesi.cli import app

runner = CliRunner()


def _load_audit_events(path: Path) -> list[dict[str, object]]:
    events: list[dict[str, object]] = []
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        text = raw_line.strip()
        if not text:
            continue
        payload = json.loads(text)
        if isinstance(payload, dict):
            events.append(payload)
    return events


def test_append_audit_event_writes_jsonl_payload(tmp_path: Path) -> None:
    output_dir = tmp_path / "piranesi-output"

    log_path = append_audit_event(
        output_dir=output_dir,
        event_type="policy_override_applied",
        stage="verify",
        approved=False,
        details={
            "config_path": tmp_path / "piranesi.toml",
            "overrides": {"verify.proof_mode": "unsafe"},
        },
    )

    assert log_path.exists()
    events = _load_audit_events(log_path)
    assert len(events) == 1
    event = events[0]
    assert event["event_type"] == "policy_override_applied"
    assert event["stage"] == "verify"
    assert event["approved"] is False
    details = event["details"]
    assert isinstance(details, dict)
    assert details["overrides"] == {"verify.proof_mode": "unsafe"}


def test_suppress_command_writes_audit_log(tmp_path: Path) -> None:
    project_root = tmp_path / "project"
    project_root.mkdir(parents=True, exist_ok=True)

    result = runner.invoke(
        app,
        [
            "suppress",
            "finding-123",
            "--reason",
            "accepted risk",
            "--owner",
            "security-team",
            "--scope",
            "id",
            "--project-root",
            str(project_root),
        ],
    )

    assert result.exit_code == 0
    audit_path = project_root / "piranesi-output" / "audit-log.jsonl"
    events = _load_audit_events(audit_path)
    assert events
    latest = events[-1]
    assert latest["event_type"] == "suppression_created"
    details = latest["details"]
    assert isinstance(details, dict)
    assert details["finding_id"] == "finding-123"
    assert details["owner"] == "security-team"


def test_run_policy_overrides_write_audit_event(tmp_path: Path) -> None:
    config_path = tmp_path / "piranesi.toml"
    config_path.write_text("", encoding="utf-8")
    output_dir = tmp_path / "output"
    target_dir = tmp_path / "target"
    target_dir.mkdir(parents=True, exist_ok=True)

    result = runner.invoke(
        app,
        [
            "run",
            str(target_dir),
            "--config",
            str(config_path),
            "--output",
            str(output_dir),
            "--proof-mode",
            "unsafe",
            "--dry-run",
            "--authorized",
            "--yes",
        ],
    )

    assert result.exit_code == 0
    audit_path = output_dir / "audit-log.jsonl"
    events = _load_audit_events(audit_path)
    override_events = [
        event for event in events if event.get("event_type") == "policy_override_applied"
    ]
    assert override_events
    details = override_events[-1]["details"]
    assert isinstance(details, dict)
    overrides = details.get("overrides")
    assert isinstance(overrides, dict)
    assert overrides.get("verify.proof_mode") == "unsafe"
