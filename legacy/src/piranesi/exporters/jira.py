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


def build_jira_issue(finding: Any, *, project: str) -> dict[str, Any]:
    description = "\n".join(
        [
            f"Finding ID: {finding.finding_id}",
            f"Dedupe key: {finding.dedupe_key}",
            f"Target: {redacted_target(finding.target)}",
            f"Severity: {finding.severity}",
            f"Risk score: {finding.risk_score if finding.risk_score is not None else 'n/a'}",
            "",
            "Remediation:",
            finding.remediation,
            "",
            f"Report path: {finding.report_path or 'n/a'}",
        ]
    )
    return {
        "fields": {
            "project": {"key": project},
            "summary": f"[Piranesi] {finding.severity.upper()}: {finding.title}",
            "issuetype": {"name": "Task"},
            "description": _jira_doc(description),
            "labels": ["piranesi", f"severity-{finding.severity}"],
        }
    }


def export_jira_issues(
    report_path: str | Path,
    *,
    project: str,
    url: str | None = None,
    dry_run: bool = True,
    yes: bool = False,
    email: str | None = None,
    token: str | None = None,
    requester: PostCallable | None = None,
    audit_dir: str | Path | None = None,
) -> ExportResult:
    if not dry_run and not yes:
        raise ExporterError("Jira ticket creation requires --yes")
    report = load_report(report_path)
    findings = iter_export_findings(report, report_path=report_path)
    issues = [build_jira_issue(finding, project=project) for finding in findings]
    created = False
    if not dry_run:
        base_url = (url or os.getenv("JIRA_BASE_URL") or "").rstrip("/")
        jira_email = email or os.getenv("JIRA_EMAIL")
        jira_token = token or os.getenv("JIRA_API_TOKEN")
        if not base_url:
            raise ExporterError("Jira ticket creation requires --url or JIRA_BASE_URL")
        if not jira_email or not jira_token:
            raise ExporterError("Jira ticket creation requires JIRA_EMAIL and JIRA_API_TOKEN")
        post = requester or requests.post
        for issue in issues:
            response = post(
                f"{base_url}/rest/api/3/issue",
                auth=(jira_email, jira_token),
                json=issue,
                timeout=15,
            )
            response.raise_for_status()
        created = bool(issues)
    output_dir = Path(audit_dir) if audit_dir is not None else Path(report_path).parent
    audit_path = write_export_audit(
        output_dir=output_dir,
        integration="jira",
        dry_run=dry_run,
        details={
            "project": project,
            "url": url,
            "report_path": str(Path(report_path).expanduser().resolve(strict=False)),
            "item_count": len(issues),
        },
    )
    return ExportResult(
        integration="jira",
        dry_run=dry_run,
        audit_log_path=str(audit_path),
        item_count=len(issues),
        created=created,
        status="dry-run" if dry_run else "created",
        preview=preview_items(findings),
    )


def _jira_doc(text: str) -> dict[str, Any]:
    return {
        "type": "doc",
        "version": 1,
        "content": [
            {
                "type": "paragraph",
                "content": [{"type": "text", "text": line or " "}],
            }
            for line in text.splitlines()
        ],
    }


__all__ = ["build_jira_issue", "export_jira_issues"]
