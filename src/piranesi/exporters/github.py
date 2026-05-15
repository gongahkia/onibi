from __future__ import annotations

import os
from collections.abc import Callable
from pathlib import Path
from typing import Any

import requests

from piranesi.exporters.common import (
    ExporterError,
    ExportResult,
    iter_export_findings,
    load_report,
    preview_items,
    redacted_target,
    write_export_audit,
)

PostCallable = Callable[..., Any]


def build_github_issue(finding: Any) -> dict[str, Any]:
    title = f"[Piranesi] {finding.severity.upper()}: {finding.title}"
    evidence = "\n".join(f"- {item['source']}:{item['key']}" for item in finding.evidence[:5])
    body = "\n".join(
        [
            f"Finding ID: `{finding.finding_id}`",
            f"Dedupe key: `{finding.dedupe_key}`",
            f"Target: `{redacted_target(finding.target)}`",
            f"Severity: `{finding.severity}`",
            f"Risk score: `{finding.risk_score if finding.risk_score is not None else 'n/a'}`",
            "",
            "Evidence summary:",
            evidence or "- No structured evidence captured.",
            "",
            "Remediation:",
            finding.remediation,
            "",
            f"Report path: `{finding.report_path or 'n/a'}`",
        ]
    )
    return {
        "title": title,
        "body": body,
        "labels": ["piranesi", f"severity:{finding.severity}"],
    }


def export_github_issues(
    report_path: str | Path,
    *,
    repo: str | None = None,
    dry_run: bool = True,
    yes: bool = False,
    token: str | None = None,
    requester: PostCallable | None = None,
    audit_dir: str | Path | None = None,
) -> ExportResult:
    if not dry_run and not yes:
        raise ExporterError("GitHub issue creation requires --yes")
    report = load_report(report_path)
    findings = iter_export_findings(report, report_path=report_path)
    issues = [build_github_issue(finding) for finding in findings]
    created = False
    if not dry_run:
        if not repo:
            raise ExporterError("GitHub issue creation requires --repo owner/name")
        auth_token = token or os.getenv("GITHUB_TOKEN")
        if not auth_token:
            raise ExporterError("GitHub issue creation requires GITHUB_TOKEN")
        post = requester or requests.post
        for issue in issues:
            response = post(
                f"https://api.github.com/repos/{repo}/issues",
                headers={
                    "Accept": "application/vnd.github+json",
                    "Authorization": f"Bearer {auth_token}",
                },
                json=issue,
                timeout=15,
            )
            response.raise_for_status()
        created = bool(issues)
    output_dir = Path(audit_dir) if audit_dir is not None else Path(report_path).parent
    audit_path = write_export_audit(
        output_dir=output_dir,
        integration="github-issues",
        dry_run=dry_run,
        details={
            "repo": repo,
            "report_path": str(Path(report_path).expanduser().resolve(strict=False)),
            "item_count": len(issues),
        },
    )
    return ExportResult(
        integration="github-issues",
        dry_run=dry_run,
        audit_log_path=str(audit_path),
        item_count=len(issues),
        created=created,
        status="dry-run" if dry_run else "created",
        preview=preview_items(findings),
    )


__all__ = ["build_github_issue", "export_github_issues"]
