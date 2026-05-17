from __future__ import annotations

import csv
from io import StringIO
from pathlib import Path

from piranesi.exporters.common import (
    ExportResult,
    ReportModel,
    iter_export_findings,
    load_report,
    write_export_audit,
)

_COLUMNS = (
    "target",
    "finding_id",
    "dedupe_key",
    "title",
    "severity",
    "category",
    "rule_id",
    "affected_component",
    "risk_score",
    "confidence",
    "cve_ids",
    "report_path",
    "remediation",
)


def generate_csv(
    report: ReportModel,
    *,
    report_path: str | Path | None = None,
    include_suppressed: bool = False,
) -> str:
    buffer = StringIO()
    writer = csv.DictWriter(buffer, fieldnames=list(_COLUMNS))
    writer.writeheader()
    for finding in iter_export_findings(
        report,
        report_path=report_path,
        include_suppressed=include_suppressed,
    ):
        writer.writerow(
            {
                "target": finding.target,
                "finding_id": finding.finding_id,
                "dedupe_key": finding.dedupe_key,
                "title": finding.title,
                "severity": finding.severity,
                "category": finding.category,
                "rule_id": finding.rule_id or "",
                "affected_component": finding.affected_component or "",
                "risk_score": "" if finding.risk_score is None else f"{finding.risk_score:.1f}",
                "confidence": f"{finding.confidence:.2f}",
                "cve_ids": ";".join(finding.cve_ids),
                "report_path": finding.report_path or "",
                "remediation": finding.remediation,
            }
        )
    return buffer.getvalue()


def export_csv(
    report_path: str | Path,
    *,
    output: str | Path,
    include_suppressed: bool = False,
) -> ExportResult:
    report = load_report(report_path)
    body = generate_csv(
        report,
        report_path=report_path,
        include_suppressed=include_suppressed,
    )
    output_path = Path(output).expanduser().resolve(strict=False)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(body, encoding="utf-8")
    item_count = max(0, len(body.splitlines()) - 1)
    audit_path = write_export_audit(
        output_dir=output_path.parent,
        integration="csv",
        dry_run=False,
        details={
            "report_path": str(Path(report_path).expanduser().resolve(strict=False)),
            "output": str(output_path),
            "item_count": item_count,
        },
    )
    return ExportResult(
        integration="csv",
        output_path=str(output_path),
        audit_log_path=str(audit_path),
        item_count=item_count,
    )


__all__ = ["export_csv", "generate_csv"]
