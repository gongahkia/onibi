from __future__ import annotations

import json
from email import policy
from email.parser import BytesParser
from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app

runner = CliRunner()


def test_email_handoff_draft_references_artifacts_without_sensitive_content(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    secret = tmp_path / "secret.txt"
    secret.write_text("token=super-secret-value\n", encoding="utf-8")
    add_secret = runner.invoke(
        app,
        [
            "evidence",
            "add",
            "--workspace",
            str(workspace),
            "--file",
            str(secret),
            "--kind",
            "payload",
            "--title",
            "Sensitive payload",
            "--sensitivity",
            "secret",
            "--json",
        ],
    )
    assert add_secret.exit_code == 0, add_secret.output
    report = runner.invoke(
        app,
        [
            "report",
            "--workspace",
            str(workspace),
            "--type",
            "red-team",
            "--format",
            "archive",
            "--json",
        ],
    )
    assert report.exit_code == 0, report.output

    result = runner.invoke(
        app,
        [
            "integrations",
            "email-handoff",
            "--workspace",
            str(workspace),
            "--to",
            "client@example.com",
            "--json",
        ],
    )

    assert result.exit_code == 0, result.output
    payload = json.loads(result.stdout)
    assert payload["sent"] is False
    assert payload["recipients"] == ["client@example.com"]
    draft_path = Path(payload["path"])
    manifest_path = Path(payload["manifest_path"])
    message = BytesParser(policy=policy.default).parsebytes(draft_path.read_bytes())
    body = message.get_content()
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    assert message["To"] == "client@example.com"
    assert "red-team-handoff-archive.zip" in body
    assert "Sensitive evidence content and raw artifacts are not embedded" in body
    assert "super-secret-value" not in body
    assert manifest["schema_version"] == "piranesi.handoff-manifest.v1"
    assert manifest["delivery_channel"] == "email-draft"
    assert manifest["sent"] is False
    assert manifest["recipients"] == ["client@example.com"]
    assert manifest["draft"]["path"] == "reports/email-handoff-draft.eml"
    assert manifest["draft"]["sha256"]
    assert manifest["artifacts"] == payload["artifact_references"]
    assert manifest["sensitive_content_embedded"] is False


def test_email_handoff_requires_recipient(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    init = runner.invoke(app, ["ingest", "init", "--workspace", str(workspace)])
    assert init.exit_code == 0, init.output

    result = runner.invoke(
        app,
        ["integrations", "email-handoff", "--workspace", str(workspace), "--json-errors"],
    )

    assert result.exit_code == 2
    payload = json.loads(result.stdout)
    assert "at least one --to recipient is required" in payload["error"]
