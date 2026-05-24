from __future__ import annotations

import json
from pathlib import Path
from typing import Any

from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.github_export import export_findings_to_github_issues
from piranesi.workspace import load_workspace

FIXTURE_ROOT = Path(__file__).parent / "fixtures" / "pentest"
NMAP_FIXTURE = FIXTURE_ROOT / "nmap" / "localhost-http.xml"
runner = CliRunner()


class FakeGitHubClient:
    def __init__(self) -> None:
        self.payloads: list[dict[str, Any]] = []

    def create_issue(self, repo: str, payload: dict[str, Any]) -> str:
        self.payloads.append({"repo": repo, "payload": payload})
        return f"https://github.com/{repo}/issues/{len(self.payloads)}"


def test_github_issues_dry_run_redacts_assets_and_omits_evidence(tmp_path: Path) -> None:
    workspace = _workspace_with_nmap(tmp_path)
    finding_id = _first_finding_id(workspace)

    result = runner.invoke(
        app,
        [
            "integrations",
            "github-issues",
            "--workspace",
            str(workspace),
            "--repo",
            "owner/repo",
            "--finding-id",
            finding_id,
            "--dry-run",
            "--json",
        ],
    )

    assert result.exit_code == 0, result.output
    payload = json.loads(result.stdout)
    issue = payload["results"][0]
    assert payload["dry_run"] is True
    assert issue["issue_url"] is None
    assert "127.0.0.1" not in issue["body"]
    assert "Raw evidence is intentionally omitted" in issue["body"]
    assert "severity:" in " ".join(issue["labels"])


def test_github_issues_live_export_uses_client_without_raw_evidence(tmp_path: Path) -> None:
    workspace = _workspace_with_nmap(tmp_path)
    state = load_workspace(workspace)
    finding_id = state.findings.findings[0].id
    client = FakeGitHubClient()

    results = export_findings_to_github_issues(
        state,
        repo="owner/repo",
        finding_ids=[finding_id],
        dry_run=False,
        include_assets=True,
        client=client,
    )

    assert results[0].issue_url == "https://github.com/owner/repo/issues/1"
    assert client.payloads[0]["repo"] == "owner/repo"
    issue = client.payloads[0]["payload"]
    assert "127.0.0.1" in issue["body"]
    assert "Raw evidence is intentionally omitted" in issue["body"]
    assert all("value" not in key.lower() for key in issue)


def test_github_issues_requires_explicit_selection(tmp_path: Path) -> None:
    workspace = _workspace_with_nmap(tmp_path)

    result = runner.invoke(
        app,
        [
            "integrations",
            "github-issues",
            "--workspace",
            str(workspace),
            "--repo",
            "owner/repo",
            "--json-errors",
        ],
    )

    assert result.exit_code == 2
    payload = json.loads(result.stdout)
    assert "at least one --finding-id is required" in payload["error"]


def _workspace_with_nmap(tmp_path: Path) -> Path:
    workspace = tmp_path / "workspace"
    result = runner.invoke(
        app,
        ["ingest", "nmap", "--input", str(NMAP_FIXTURE), "--workspace", str(workspace)],
    )
    assert result.exit_code == 0, result.output
    return workspace


def _first_finding_id(workspace: Path) -> str:
    state = load_workspace(workspace)
    return state.findings.findings[0].id
