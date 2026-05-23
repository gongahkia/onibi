from __future__ import annotations

import json
import shutil
import sys
from pathlib import Path

from typer.testing import CliRunner

from piranesi.agent_bridge import AGENT_RUN_SCHEMA_VERSION, load_agent_run_manifest
from piranesi.cli import app
from piranesi.evidence import load_evidence_index
from piranesi.pff import load_and_validate_pff_file
from piranesi.workspace import AUDIT_LOG_FILE, load_workspace

runner = CliRunner()
PFF_FIXTURE = Path("tests/fixtures/pff/workspace-findings-v0.json")


def test_agent_context_exports_scope_and_policy(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    init = runner.invoke(
        app,
        [
            "ingest",
            "init",
            "--workspace",
            str(workspace),
            "--client",
            "Example Client",
            "--project",
            "Agent Bridge",
            "--scope",
            "https://app.example.test",
        ],
    )
    assert init.exit_code == 0, init.output

    result = runner.invoke(app, ["agent", "context", "--workspace", str(workspace), "--json"])

    assert result.exit_code == 0, result.output
    payload = json.loads(result.stdout)
    context_path = Path(payload["path"])
    context = json.loads(context_path.read_text(encoding="utf-8"))
    assert context["schema_version"] == "piranesi.agent-context.v0"
    assert context["engagement"]["scope"] == ["https://app.example.test"]
    assert context["policy"]["required_run_manifest_schema"] == AGENT_RUN_SCHEMA_VERSION
    assert context["policy"]["findings_contract"] == "piranesi.pff.v0"

    audit_events = [
        json.loads(line)
        for line in (workspace / AUDIT_LOG_FILE).read_text(encoding="utf-8").splitlines()
    ]
    assert audit_events[-1]["command"] == "agent context"


def test_agent_import_run_imports_pff_findings_and_evidence(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    run_dir = tmp_path / "agent-run"
    run_dir.mkdir()
    shutil.copyfile(PFF_FIXTURE, run_dir / "findings.pff.json")
    (run_dir / "agent.log").write_text("triage completed\n", encoding="utf-8")
    manifest = run_dir / "agent-run.json"
    manifest.write_text(
        json.dumps(
            {
                "schema_version": AGENT_RUN_SCHEMA_VERSION,
                "run_id": "run-001",
                "agent": {
                    "name": "team-triage-agent",
                    "version": "1.4.0",
                    "kind": "external",
                },
                "authorization": {
                    "approved_by": "operator@example.test",
                    "approved_at": "2026-05-23T10:00:00Z",
                    "approval_reference": "ROE-1234",
                    "scope_acknowledged": True,
                },
                "mode": "triage",
                "scope": ["127.0.0.1"],
                "pff_path": "findings.pff.json",
                "artifacts": [
                    {
                        "path": "agent.log",
                        "kind": "transcript",
                        "title": "Agent execution log",
                        "sensitivity": "internal",
                        "tags": ["agent-log"],
                    }
                ],
                "commands": [
                    {
                        "argv": ["team-agent", "triage", "--target", "127.0.0.1"],
                        "exit_code": 0,
                        "output_path": "agent.log",
                    }
                ],
            }
        ),
        encoding="utf-8",
    )
    init = runner.invoke(
        app,
        ["ingest", "init", "--workspace", str(workspace), "--scope", "127.0.0.1"],
    )
    assert init.exit_code == 0, init.output

    validate = runner.invoke(app, ["agent", "validate-run", "--manifest", str(manifest), "--json"])
    result = runner.invoke(
        app,
        [
            "agent",
            "import-run",
            "--manifest",
            str(manifest),
            "--workspace",
            str(workspace),
            "--json",
        ],
    )

    assert validate.exit_code == 0, validate.output
    assert result.exit_code == 0, result.output
    payload = json.loads(result.stdout)
    assert payload["run_id"] == "run-001"
    assert payload["findings"] == 1
    assert payload["created"] == 1
    assert payload["evidence"] == 1
    assert payload["manifest_raw_path"].startswith("raw/agent-manifest/")
    assert payload["pff_raw_path"].startswith("raw/agent-pff/")

    state = load_workspace(workspace)
    assert len(state.findings.findings) == 1
    finding = state.findings.findings[0]
    assert finding.provenance["agent_run"]["run_id"] == "run-001"
    assert finding.provenance["agent_run"]["agent"]["name"] == "team-triage-agent"
    assert "agent-run" in finding.tags

    evidence_index = load_evidence_index(workspace)
    assert len(evidence_index.evidence) == 1
    assert evidence_index.evidence[0].source == "agent:team-triage-agent"
    assert "agent-log" in evidence_index.evidence[0].tags

    raw_pff = workspace / payload["pff_raw_path"]
    assert load_and_validate_pff_file(raw_pff)["schema_version"] == "piranesi.pff.v0"

    audit_events = [
        json.loads(line)
        for line in (workspace / AUDIT_LOG_FILE).read_text(encoding="utf-8").splitlines()
    ]
    assert audit_events[-1]["command"] == "agent import-run"
    assert audit_events[-1]["summary"]["agent"]["name"] == "team-triage-agent"


def test_agent_run_executes_command_and_imports_manifest(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    agent_script = tmp_path / "fake_agent.py"
    pff_fixture = json.dumps(str(PFF_FIXTURE.resolve()))
    agent_script.write_text(
        f"""
from __future__ import annotations

import argparse
import json
import os
import shutil
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("--context", required=True)
parser.add_argument("--manifest", required=True)
args = parser.parse_args()

context = json.loads(Path(args.context).read_text(encoding="utf-8"))
run_dir = Path(os.environ["PIRANESI_AGENT_RUN_DIR"])
shutil.copyfile(Path({pff_fixture}), run_dir / "findings.pff.json")
(run_dir / "agent.log").write_text("agent completed\\n", encoding="utf-8")
Path(args.manifest).write_text(
    json.dumps(
        {{
            "schema_version": "piranesi.agent-run.v0",
            "run_id": os.environ["PIRANESI_AGENT_RUN_ID"],
            "agent": {{
                "name": "fake-agent",
                "version": "0.1.0",
                "kind": "script",
            }},
            "authorization": {{
                "approved_by": os.environ["PIRANESI_AGENT_APPROVED_BY"],
                "approved_at": "2026-05-23T10:00:00Z",
                "approval_reference": os.environ["PIRANESI_AGENT_APPROVAL_REFERENCE"],
                "scope_acknowledged": True,
            }},
            "mode": os.environ["PIRANESI_AGENT_MODE"],
            "scope": context["engagement"]["scope"],
            "pff_path": "findings.pff.json",
            "artifacts": [
                {{
                    "path": "agent.log",
                    "kind": "transcript",
                    "title": "Agent execution log",
                    "sensitivity": "internal",
                }}
            ],
        }}
    ),
    encoding="utf-8",
)
print("fake agent complete")
""".lstrip(),
        encoding="utf-8",
    )
    init = runner.invoke(
        app,
        ["ingest", "init", "--workspace", str(workspace), "--scope", "127.0.0.1"],
    )
    assert init.exit_code == 0, init.output

    result = runner.invoke(
        app,
        [
            "agent",
            "run",
            "--workspace",
            str(workspace),
            "--run-id",
            "run-via-piranesi",
            "--approved-by",
            "operator@example.test",
            "--approval-reference",
            "ROE-1234",
            "--live",
            "--command",
            f"{sys.executable} {agent_script} --context {{context}} --manifest {{manifest}}",
            "--json",
        ],
    )

    assert result.exit_code == 0, result.output
    payload = json.loads(result.stdout)
    assert payload["run_id"] == "run-via-piranesi"
    assert payload["live"] is True
    assert payload["exit_code"] == 0
    assert Path(payload["context_path"]).is_file()
    assert Path(payload["manifest_path"]).is_file()
    assert Path(payload["stdout_path"]).read_text(encoding="utf-8") == "fake agent complete\n"
    assert payload["imported"]["findings"] == 1
    assert payload["imported"]["created"] == 1
    assert payload["imported"]["evidence"] == 1

    state = load_workspace(workspace)
    assert state.findings.findings[0].provenance["agent_run"]["agent"]["name"] == "fake-agent"


def test_agent_run_dry_run_prepares_context_without_executing(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    init = runner.invoke(
        app,
        ["ingest", "init", "--workspace", str(workspace), "--scope", "127.0.0.1"],
    )
    assert init.exit_code == 0, init.output

    result = runner.invoke(
        app,
        [
            "agent",
            "run",
            "--workspace",
            str(workspace),
            "--run-id",
            "dry-run",
            "--command",
            "missing-agent --token secret-value --context {context}",
            "--json",
        ],
    )

    assert result.exit_code == 0, result.output
    payload = json.loads(result.stdout)
    assert payload["live"] is False
    assert payload["exit_code"] is None
    assert payload["command"][2] == "[redacted]"
    assert Path(payload["context_path"]).is_file()
    assert not Path(payload["manifest_path"]).exists()


def test_agent_profile_onboarding_check_login_and_run(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    agent_script = tmp_path / "profile_agent.py"
    check_script = tmp_path / "profile_check.py"
    login_script = tmp_path / "profile_login.py"
    pff_fixture = json.dumps(str(PFF_FIXTURE.resolve()))
    check_script.write_text("print('agent ok')\n", encoding="utf-8")
    login_script.write_text("print('logged in')\n", encoding="utf-8")
    agent_script.write_text(
        f"""
from __future__ import annotations

import argparse
import json
import os
import shutil
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("--context", required=True)
parser.add_argument("--manifest", required=True)
args = parser.parse_args()

context = json.loads(Path(args.context).read_text(encoding="utf-8"))
run_dir = Path(os.environ["PIRANESI_AGENT_RUN_DIR"])
shutil.copyfile(Path({pff_fixture}), run_dir / "findings.pff.json")
(run_dir / "profile-agent.log").write_text("profile agent completed\\n", encoding="utf-8")
Path(args.manifest).write_text(
    json.dumps(
        {{
            "schema_version": "piranesi.agent-run.v0",
            "run_id": os.environ["PIRANESI_AGENT_RUN_ID"],
            "agent": {{
                "name": "profile-agent",
                "version": "0.1.0",
                "kind": "script",
            }},
            "authorization": {{
                "approved_by": os.environ["PIRANESI_AGENT_APPROVED_BY"],
                "approved_at": "2026-05-23T10:00:00Z",
                "scope_acknowledged": True,
            }},
            "mode": os.environ["PIRANESI_AGENT_MODE"],
            "scope": context["engagement"]["scope"],
            "pff_path": "findings.pff.json",
            "artifacts": [
                {{
                    "path": "profile-agent.log",
                    "kind": "transcript",
                    "title": "Profile agent log",
                    "sensitivity": "internal",
                }}
            ],
        }}
    ),
    encoding="utf-8",
)
""".lstrip(),
        encoding="utf-8",
    )
    init = runner.invoke(
        app,
        ["ingest", "init", "--workspace", str(workspace), "--scope", "127.0.0.1"],
    )
    assert init.exit_code == 0, init.output

    add = runner.invoke(
        app,
        [
            "agent",
            "add",
            "--workspace",
            str(workspace),
            "--name",
            "profile-agent",
            "--command",
            f"{sys.executable} {agent_script} --context {{context}} --manifest {{manifest}}",
            "--check-command",
            f"{sys.executable} {check_script}",
            "--login-command",
            f"{sys.executable} {login_script}",
            "--json",
        ],
    )
    listed = runner.invoke(app, ["agent", "list", "--workspace", str(workspace), "--json"])
    checked = runner.invoke(
        app, ["agent", "check", "--workspace", str(workspace), "--agent", "profile-agent", "--json"]
    )
    login = runner.invoke(
        app, ["agent", "login", "--workspace", str(workspace), "--agent", "profile-agent", "--json"]
    )
    run = runner.invoke(
        app,
        [
            "agent",
            "run",
            "--workspace",
            str(workspace),
            "--agent",
            "profile-agent",
            "--run-id",
            "profile-run",
            "--approved-by",
            "operator@example.test",
            "--live",
            "--json",
        ],
    )

    assert add.exit_code == 0, add.output
    add_payload = json.loads(add.stdout)
    assert add_payload["profile"]["name"] == "profile-agent"
    assert listed.exit_code == 0, listed.output
    list_payload = json.loads(listed.stdout)
    assert list_payload["count"] == 1
    assert list_payload["profiles"][0]["login_command"]
    assert checked.exit_code == 0, checked.output
    check_payload = json.loads(checked.stdout)
    assert check_payload["issues"] == []
    assert check_payload["check_exit_code"] == 0
    assert check_payload["login_configured"] is True
    assert login.exit_code == 0, login.output
    login_payload = json.loads(login.stdout)
    assert Path(login_payload["stdout_path"]).read_text(encoding="utf-8") == "logged in\n"
    assert run.exit_code == 0, run.output
    run_payload = json.loads(run.stdout)
    assert run_payload["imported"]["findings"] == 1
    assert run_payload["imported"]["evidence"] == 1


def test_agent_import_run_rejects_scope_outside_workspace(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    run_dir = tmp_path / "agent-run"
    run_dir.mkdir()
    shutil.copyfile(PFF_FIXTURE, run_dir / "findings.pff.json")
    manifest = run_dir / "agent-run.json"
    manifest.write_text(
        json.dumps(
            {
                "schema_version": AGENT_RUN_SCHEMA_VERSION,
                "run_id": "run-out-of-scope",
                "agent": {"name": "team-triage-agent", "version": "1.4.0"},
                "authorization": {
                    "approved_by": "operator@example.test",
                    "approved_at": "2026-05-23T10:00:00Z",
                },
                "mode": "triage",
                "scope": ["10.0.0.5"],
                "pff_path": "findings.pff.json",
            }
        ),
        encoding="utf-8",
    )
    init = runner.invoke(
        app,
        ["ingest", "init", "--workspace", str(workspace), "--scope", "127.0.0.1"],
    )
    assert init.exit_code == 0, init.output

    result = runner.invoke(
        app,
        [
            "agent",
            "import-run",
            "--manifest",
            str(manifest),
            "--workspace",
            str(workspace),
            "--json-errors",
        ],
    )

    assert result.exit_code == 2
    payload = json.loads(result.output)
    assert "outside workspace scope" in payload["error"]


def test_agent_manifest_rejects_path_traversal(tmp_path: Path) -> None:
    manifest = tmp_path / "agent-run.json"
    manifest.write_text(
        json.dumps(
            {
                "schema_version": AGENT_RUN_SCHEMA_VERSION,
                "run_id": "run-escape",
                "agent": {"name": "bad-agent", "version": "0.1.0"},
                "authorization": {
                    "approved_by": "operator",
                    "approved_at": "2026-05-23T10:00:00Z",
                },
                "mode": "triage",
                "scope": ["127.0.0.1"],
                "pff_path": "../findings.pff.json",
            }
        ),
        encoding="utf-8",
    )

    try:
        load_agent_run_manifest(manifest)
    except ValueError as exc:
        assert "traversal" in str(exc)
    else:
        raise AssertionError("expected traversal path to be rejected")
