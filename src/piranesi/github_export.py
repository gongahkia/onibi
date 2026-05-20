from __future__ import annotations

import json
import os
import re
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Any, Protocol

from piranesi.workspace import NormalizedFinding, WorkspaceState

GITHUB_EXPORT_LABEL = "piranesi"
SECRET_PATTERN = re.compile(
    r"(?i)(token|secret|password|passwd|api[_-]?key|session|cookie)\s*[:=]\s*\S+"
)


class GitHubExportError(ValueError):
    """Raised when GitHub issue export cannot proceed safely."""


class GitHubIssueClient(Protocol):
    def create_issue(self, repo: str, payload: dict[str, Any]) -> str: ...


@dataclass(frozen=True, slots=True)
class GitHubIssueExportResult:
    finding_id: str
    title: str
    labels: list[str]
    body: str
    dry_run: bool
    issue_url: str | None = None
    error: str | None = None

    def as_payload(self) -> dict[str, Any]:
        return {
            "finding_id": self.finding_id,
            "title": self.title,
            "labels": self.labels,
            "body": self.body,
            "dry_run": self.dry_run,
            "issue_url": self.issue_url,
            "error": self.error,
        }


class GitHubApiIssueClient:
    def __init__(
        self,
        *,
        token: str,
        api_url: str = "https://api.github.com",
        timeout_seconds: int = 20,
    ) -> None:
        self._token = token
        self._api_url = api_url.rstrip("/")
        self._timeout_seconds = timeout_seconds

    def create_issue(self, repo: str, payload: dict[str, Any]) -> str:
        _validate_repo(repo)
        url = f"{self._api_url}/repos/{repo}/issues"
        request = urllib.request.Request(  # noqa: S310
            url,
            data=json.dumps(payload).encode("utf-8"),
            headers={
                "Accept": "application/vnd.github+json",
                "Authorization": f"Bearer {self._token}",
                "Content-Type": "application/json",
                "User-Agent": "piranesi-github-issues-export",
                "X-GitHub-Api-Version": "2022-11-28",
            },
            method="POST",
        )
        try:
            with urllib.request.urlopen(  # noqa: S310
                request,
                timeout=self._timeout_seconds,
            ) as response:
                response_payload = json.loads(response.read().decode("utf-8"))
        except urllib.error.HTTPError as exc:
            body = exc.read().decode("utf-8", errors="replace")
            raise GitHubExportError(f"GitHub API rejected issue create: {exc.code} {body}") from exc
        except urllib.error.URLError as exc:
            raise GitHubExportError(f"GitHub API request failed: {exc.reason}") from exc
        url_value = response_payload.get("html_url")
        if not isinstance(url_value, str) or not url_value:
            raise GitHubExportError("GitHub API response did not include html_url")
        return url_value


def export_findings_to_github_issues(
    state: WorkspaceState,
    *,
    repo: str,
    finding_ids: list[str],
    dry_run: bool = True,
    include_assets: bool = False,
    client: GitHubIssueClient | None = None,
) -> list[GitHubIssueExportResult]:
    _validate_repo(repo)
    selected = _select_findings(state, finding_ids)
    if not dry_run and client is None:
        token = github_token_from_environment()
        if token is None:
            raise GitHubExportError("live GitHub export requires GITHUB_TOKEN or GH_TOKEN")
        client = GitHubApiIssueClient(token=token)
    results: list[GitHubIssueExportResult] = []
    for finding in selected:
        issue = github_issue_payload(finding, include_assets=include_assets)
        if dry_run:
            results.append(
                GitHubIssueExportResult(
                    finding_id=finding.id,
                    title=issue["title"],
                    labels=list(issue["labels"]),
                    body=issue["body"],
                    dry_run=True,
                )
            )
            continue
        assert client is not None
        try:
            issue_url = client.create_issue(repo, issue)
            results.append(
                GitHubIssueExportResult(
                    finding_id=finding.id,
                    title=issue["title"],
                    labels=list(issue["labels"]),
                    body=issue["body"],
                    dry_run=False,
                    issue_url=issue_url,
                )
            )
        except GitHubExportError as exc:
            results.append(
                GitHubIssueExportResult(
                    finding_id=finding.id,
                    title=issue["title"],
                    labels=list(issue["labels"]),
                    body=issue["body"],
                    dry_run=False,
                    error=str(exc),
                )
            )
    return results


def github_issue_payload(
    finding: NormalizedFinding,
    *,
    include_assets: bool = False,
) -> dict[str, Any]:
    title = f"[{finding.severity}] {finding.title}"
    asset = finding.asset if include_assets else "[redacted]"
    source_tools = sorted({reference.tool for reference in finding.source_references})
    source_digests = sorted({reference.input_sha256 for reference in finding.source_references})
    body = "\n".join(
        [
            "Piranesi finding summary for one-way GitHub Issues handoff.",
            "",
            f"- Finding ID: `{finding.id}`",
            f"- Severity: `{finding.severity}`",
            f"- Confidence: `{finding.confidence}`",
            f"- Status: `{finding.status}`",
            f"- Affected asset: `{asset or '[redacted]'}`",
            "",
            "## Description",
            "",
            _redacted_text(finding.description) or "No description provided.",
            "",
            "## Remediation",
            "",
            _redacted_text(finding.remediation) or "No remediation guidance provided.",
            "",
            "## Provenance",
            "",
            f"- Source tools: {', '.join(source_tools) if source_tools else 'not specified'}",
            f"- Source digests: {', '.join(source_digests) if source_digests else 'not specified'}",
            "",
            (
                "Raw evidence is intentionally omitted. Review the local Piranesi "
                "workspace for evidence."
            ),
        ]
    )
    return {
        "title": title,
        "body": body,
        "labels": [
            GITHUB_EXPORT_LABEL,
            f"severity:{finding.severity}",
            f"status:{finding.status}",
        ],
    }


def github_token_from_environment() -> str | None:
    for name in ("GITHUB_TOKEN", "GH_TOKEN"):
        value = os.environ.get(name)
        if value:
            return value
    return None


def _select_findings(state: WorkspaceState, finding_ids: list[str]) -> list[NormalizedFinding]:
    if not finding_ids:
        raise GitHubExportError("at least one --finding-id is required")
    by_id = {finding.id: finding for finding in state.findings.findings}
    missing = sorted(set(finding_ids) - set(by_id))
    if missing:
        raise GitHubExportError(f"unknown finding id(s): {', '.join(missing)}")
    return [by_id[finding_id] for finding_id in finding_ids]


def _redacted_text(value: str | None) -> str | None:
    if value is None:
        return None
    return SECRET_PATTERN.sub(r"\1=[redacted]", value)


def _validate_repo(repo: str) -> None:
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repo):
        raise GitHubExportError("GitHub repository must be in owner/name form")


__all__ = [
    "GITHUB_EXPORT_LABEL",
    "GitHubApiIssueClient",
    "GitHubExportError",
    "GitHubIssueExportResult",
    "export_findings_to_github_issues",
    "github_issue_payload",
    "github_token_from_environment",
]
