from __future__ import annotations

from pathlib import Path
from typing import Any

from piranesi import __version__
from piranesi.exporters.common import (
    ExportResult,
    ReportModel,
    iter_export_findings,
    load_report,
    write_export_audit,
    write_json,
)

_SARIF_SCHEMA_URI = (
    "https://docs.oasis-open.org/sarif/sarif/v2.1.0/errata01/os/schemas/sarif-schema-2.1.0.json"
)
_SEVERITY_LEVELS = {
    "critical": "error",
    "high": "error",
    "medium": "warning",
    "low": "note",
    "informational": "note",
}


def generate_sarif(
    report: ReportModel,
    *,
    report_path: str | Path | None = None,
    include_suppressed: bool = False,
) -> dict[str, Any]:
    findings = iter_export_findings(
        report,
        report_path=report_path,
        include_suppressed=include_suppressed,
    )
    rules: dict[str, dict[str, Any]] = {}
    results: list[dict[str, Any]] = []
    for finding in findings:
        rule_id = finding.rule_id or finding.category or "host-posture"
        rules.setdefault(
            rule_id,
            {
                "id": rule_id,
                "name": finding.category,
                "shortDescription": {"text": finding.title},
                "help": {"text": finding.remediation},
                "properties": {"category": finding.category},
            },
        )
        results.append(
            {
                "ruleId": rule_id,
                "level": _SEVERITY_LEVELS.get(finding.severity, "warning"),
                "message": {"text": finding.title},
                "locations": [
                    {
                        "physicalLocation": {
                            "artifactLocation": {
                                "uri": _artifact_uri(finding.report_path, report_path)
                            },
                            "region": {"startLine": 1},
                        },
                        "message": {"text": finding.target},
                    }
                ],
                "properties": {
                    "findingId": finding.finding_id,
                    "target": finding.target,
                    "severity": finding.severity,
                    "confidence": finding.confidence,
                    "category": finding.category,
                    "affectedComponent": finding.affected_component,
                    "cveIds": finding.cve_ids,
                    "evidence": finding.evidence,
                    "remediation": finding.remediation,
                    "riskScore": finding.risk_score,
                    "dedupeKey": finding.dedupe_key,
                },
            }
        )
    return {
        "$schema": _SARIF_SCHEMA_URI,
        "version": "2.1.0",
        "runs": [
            {
                "tool": {
                    "driver": {
                        "name": "Piranesi Host Posture",
                        "informationUri": "https://github.com/gongahkia/piranesi",
                        "version": __version__,
                        "rules": list(rules.values()),
                    }
                },
                "results": results,
                "properties": {"findingCount": len(results)},
            }
        ],
    }


def export_sarif(
    report_path: str | Path,
    *,
    output: str | Path,
    include_suppressed: bool = False,
) -> ExportResult:
    report = load_report(report_path)
    payload = generate_sarif(
        report,
        report_path=report_path,
        include_suppressed=include_suppressed,
    )
    output_path = write_json(output, payload)
    audit_path = write_export_audit(
        output_dir=output_path.parent,
        integration="sarif",
        dry_run=False,
        details={
            "report_path": str(Path(report_path).expanduser().resolve(strict=False)),
            "output": str(output_path),
            "item_count": len(payload["runs"][0]["results"]),
        },
    )
    return ExportResult(
        integration="sarif",
        output_path=str(output_path),
        audit_log_path=str(audit_path),
        item_count=len(payload["runs"][0]["results"]),
    )


def _artifact_uri(finding_report_path: str | None, fallback_path: str | Path | None) -> str:
    raw = finding_report_path or (
        str(fallback_path) if fallback_path is not None else "host-report.json"
    )
    return Path(raw).name


__all__ = ["export_sarif", "generate_sarif"]
