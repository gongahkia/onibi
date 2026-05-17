from __future__ import annotations

from collections.abc import Callable
from pathlib import Path
from typing import Any

import requests

from piranesi.exporters.common import (
    ExporterError,
    ExportResult,
    ReportModel,
    iter_export_findings,
    load_report,
    preview_items,
    redact_metadata,
    redacted_target,
    write_export_audit,
)
from piranesi.host.models import HostPostureReport

PostCallable = Callable[..., Any]


def build_webhook_payload(
    report: ReportModel,
    *,
    report_path: str | Path | None = None,
    include_raw_snapshot: bool = False,
    redact: bool = True,
) -> dict[str, Any]:
    findings = iter_export_findings(report, report_path=report_path)
    payload: dict[str, Any] = {
        "schema_version": 1,
        "integration": "webhook",
        "report_path": str(report_path) if report_path is not None else None,
        "summary": report.summary,
        "findings": [
            _webhook_finding_payload(finding.model_dump(mode="json"), redact=redact)
            for finding in findings
        ],
    }
    if isinstance(report, HostPostureReport):
        payload["target"] = redacted_target(report.target) if redact else report.target
        payload["host_metadata"] = (
            redact_metadata(report.host_metadata) if redact else report.host_metadata
        )
        if include_raw_snapshot:
            snapshot = report.snapshot.model_dump(mode="json")
            payload["snapshot"] = redact_metadata(snapshot) if redact else snapshot
    else:
        payload["target"] = "fleet"
        payload["hosts"] = [
            {
                "target": redacted_target(host.target) if redact else host.target,
                "status": host.status,
                "findings_total": host.findings_total,
                "report_path": host.report_path,
            }
            for host in report.hosts
        ]
    return payload


def _webhook_finding_payload(finding: dict[str, Any], *, redact: bool) -> dict[str, Any]:
    if not redact:
        return finding
    finding["target"] = redacted_target(str(finding.get("target") or ""))
    finding["evidence"] = [
        {**item, "value": "[redacted]"} if isinstance(item, dict) else item
        for item in finding.get("evidence", [])
    ]
    return finding


def export_webhook(
    report_path: str | Path,
    *,
    url: str,
    dry_run: bool = True,
    yes: bool = False,
    include_raw_snapshot: bool = False,
    redact: bool = True,
    requester: PostCallable | None = None,
    audit_dir: str | Path | None = None,
) -> ExportResult:
    if not dry_run and not yes:
        raise ExporterError("real webhook delivery requires --yes")
    report = load_report(report_path)
    findings = iter_export_findings(report, report_path=report_path)
    payload = build_webhook_payload(
        report,
        report_path=report_path,
        include_raw_snapshot=include_raw_snapshot,
        redact=redact,
    )
    item_count = len(findings)
    sent = False
    if not dry_run:
        post = requester or requests.post
        response = post(url, json=payload, timeout=15)
        response.raise_for_status()
        sent = True
    output_dir = Path(audit_dir) if audit_dir is not None else Path(report_path).parent
    audit_path = write_export_audit(
        output_dir=output_dir,
        integration="webhook",
        dry_run=dry_run,
        details={
            "url": url,
            "report_path": str(Path(report_path).expanduser().resolve(strict=False)),
            "item_count": item_count,
            "include_raw_snapshot": include_raw_snapshot,
            "redacted": redact,
        },
    )
    return ExportResult(
        integration="webhook",
        dry_run=dry_run,
        audit_log_path=str(audit_path),
        item_count=item_count,
        sent=sent,
        status="dry-run" if dry_run else "sent",
        preview=preview_items(findings),
    )


__all__ = ["build_webhook_payload", "export_webhook"]
