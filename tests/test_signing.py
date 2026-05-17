from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.signing import audit_chain, verify_workspace

FIXTURE = Path(__file__).parent / "fixtures" / "pentest" / "nmap" / "localhost-http.xml"
runner = CliRunner()


def test_sign_workspace_is_stable_and_verifiable(tmp_path: Path) -> None:
    workspace = _workspace_with_report(tmp_path)

    first = runner.invoke(app, ["sign", "--workspace", str(workspace), "--json"])
    assert first.exit_code == 0, first.output
    first_payload = json.loads(first.stdout)

    second = runner.invoke(app, ["sign", "--workspace", str(workspace), "--json"])
    assert second.exit_code == 0, second.output
    second_payload = json.loads(second.stdout)

    assert second_payload["manifest_id"] == first_payload["manifest_id"]
    assert second_payload["sha256"] == first_payload["sha256"]
    manifest_path = Path(first_payload["path"])
    assert manifest_path.is_file()
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    assert manifest["schema_version"] == "piranesi.chain-of-custody.v1"
    assert {artifact["role"] for artifact in manifest["artifacts"]} >= {
        "workspace",
        "findings",
        "audit-log",
        "raw-input",
        "report",
    }
    assert manifest["tool_inputs"][0]["tool_version"] == "7.99"
    assert "nmap -sV" in manifest["tool_inputs"][0]["command_args"]

    verify = runner.invoke(app, ["sign", "--workspace", str(workspace), "--verify", "--json"])
    assert verify.exit_code == 0, verify.output
    assert json.loads(verify.stdout)["ok"] is True


def test_sign_verify_reports_precise_tamper_failure(tmp_path: Path) -> None:
    workspace = _workspace_with_report(tmp_path)
    sign = runner.invoke(app, ["sign", "--workspace", str(workspace), "--json"])
    assert sign.exit_code == 0, sign.output

    findings_path = workspace / "normalized" / "findings.json"
    findings_payload = json.loads(findings_path.read_text(encoding="utf-8"))
    findings_payload["findings"][0]["title"] = "tampered title"
    findings_path.write_text(json.dumps(findings_payload), encoding="utf-8")

    verify = runner.invoke(app, ["sign", "--workspace", str(workspace), "--verify", "--json"])
    assert verify.exit_code == 1, verify.output
    payload = json.loads(verify.stdout)
    assert payload["ok"] is False
    failure = next(
        item for item in payload["failures"] if item["path"] == "normalized/findings.json"
    )
    assert failure["message"] == "covered file digest mismatch"
    assert failure["expected_sha256"] != failure["actual_sha256"]


def test_audit_chain_detects_removed_reordered_or_edited_events(tmp_path: Path) -> None:
    workspace = _workspace_with_report(tmp_path)
    sign = runner.invoke(app, ["sign", "--workspace", str(workspace), "--json"])
    assert sign.exit_code == 0, sign.output

    audit_path = workspace / "audit-log.jsonl"
    original_chain = audit_chain(audit_path)
    lines = audit_path.read_text(encoding="utf-8").splitlines()
    assert len(lines) >= 1
    event = json.loads(lines[0])
    event["summary"]["created"] = 99
    lines[0] = json.dumps(event, sort_keys=True)
    audit_path.write_text("\n".join(lines) + "\n", encoding="utf-8")

    edited_chain = audit_chain(audit_path)
    assert edited_chain.head != original_chain.head
    result = verify_workspace(workspace)
    assert result.ok is False
    assert any(failure.path == "audit-log.jsonl" for failure in result.failures)


def test_report_references_latest_manifest(tmp_path: Path) -> None:
    workspace = _workspace_with_report(tmp_path)
    sign = runner.invoke(app, ["sign", "--workspace", str(workspace), "--json"])
    assert sign.exit_code == 0, sign.output
    manifest_id = json.loads(sign.stdout)["manifest_id"]

    report = runner.invoke(
        app,
        ["report", "--workspace", str(workspace), "--format", "json", "--json"],
    )
    assert report.exit_code == 0, report.output
    report_path = Path(json.loads(report.stdout)["path"])
    report_payload = json.loads(report_path.read_text(encoding="utf-8"))

    latest = report_payload["chain_of_custody"]["latest_manifest"]
    assert latest["manifest_id"] == manifest_id
    assert latest["path"] == f"signatures/manifest-{manifest_id}.json"
    assert report_payload["chain_of_custody"]["manifest_status"] == "available"


def _workspace_with_report(tmp_path: Path) -> Path:
    workspace = tmp_path / "workspace"
    ingest = runner.invoke(
        app,
        ["ingest", "nmap", "--input", str(FIXTURE), "--workspace", str(workspace)],
    )
    assert ingest.exit_code == 0, ingest.output
    report = runner.invoke(app, ["report", "--workspace", str(workspace), "--format", "json"])
    assert report.exit_code == 0, report.output
    return workspace
