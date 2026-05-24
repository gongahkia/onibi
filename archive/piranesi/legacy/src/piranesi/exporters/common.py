from __future__ import annotations

import hashlib
import json
import re
from collections.abc import Iterable, Mapping
from pathlib import Path
from typing import Any, Protocol

from pydantic import BaseModel, ConfigDict, Field

from piranesi.audit import append_audit_event
from piranesi.host.api import load_host_report
from piranesi.host.fleet import load_fleet_report
from piranesi.host.models import FleetReport, HostFinding, HostPostureReport

ReportModel = HostPostureReport | FleetReport

_SENSITIVE_KEY_PATTERN = re.compile(
    r"(host(name|_id)?|ip(_address(es)?)?|mac|user(name)?|token|secret|key|password)",
    re.IGNORECASE,
)


class ExporterError(RuntimeError):
    """Raised when an integration export cannot be completed."""


class ExportedFinding(BaseModel):
    model_config = ConfigDict(extra="forbid")

    target: str
    finding_id: str
    rule_id: str | None = None
    title: str
    category: str
    severity: str
    confidence: float
    affected_component: str | None = None
    cve_ids: list[str] = Field(default_factory=list)
    evidence: list[dict[str, str]] = Field(default_factory=list)
    remediation: str
    risk_score: float | None = None
    report_path: str | None = None
    suppressed: bool = False
    suppression_reason: str | None = None
    dedupe_key: str


class ExportResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    integration: str
    dry_run: bool = False
    output_path: str | None = None
    audit_log_path: str | None = None
    item_count: int = 0
    sent: bool = False
    created: bool = False
    status: str = "ok"
    preview: list[dict[str, Any]] = Field(default_factory=list)


class FindingExporter(Protocol):
    def export(self, report: ReportModel) -> ExportResult: ...


def load_report(path: str | Path) -> ReportModel:
    report_path = Path(path).expanduser().resolve(strict=False)
    payload = json.loads(report_path.read_text(encoding="utf-8"))
    if not isinstance(payload, dict):
        raise ExporterError(f"report must be a JSON object: {report_path}")
    if "hosts" in payload and "findings" not in payload:
        return load_fleet_report(report_path)
    return load_host_report(report_path)


def iter_export_findings(
    report: ReportModel,
    *,
    report_path: str | Path | None = None,
    include_suppressed: bool = False,
) -> list[ExportedFinding]:
    if isinstance(report, HostPostureReport):
        resolved_report_path = _string_path(report_path)
        return [
            normalize_finding(
                finding,
                target=report.target,
                report_path=resolved_report_path,
            )
            for finding in report.findings
            if include_suppressed or not finding.suppressed
        ]

    root = _fleet_report_root(report_path)
    rows: list[ExportedFinding] = []
    for host in report.hosts:
        host_report_path = _resolve_host_report_path(root, host.report_path)
        if host_report_path is None:
            continue
        host_report = load_host_report(host_report_path)
        rows.extend(
            iter_export_findings(
                host_report,
                report_path=host_report_path,
                include_suppressed=include_suppressed,
            )
        )
    return rows


def normalize_finding(
    finding: HostFinding,
    *,
    target: str,
    report_path: str | None = None,
) -> ExportedFinding:
    risk_score = finding.risk.total if finding.risk is not None else None
    evidence = [
        {"source": item.source, "key": item.key, "value": _truncate(item.value)}
        for item in finding.evidence
    ]
    return ExportedFinding(
        target=target,
        finding_id=finding.id,
        rule_id=finding.rule_id,
        title=finding.title,
        category=finding.category,
        severity=finding.severity,
        confidence=finding.confidence,
        affected_component=finding.affected_component,
        cve_ids=list(finding.cve_ids),
        evidence=evidence,
        remediation=finding.remediation,
        risk_score=risk_score,
        report_path=report_path,
        suppressed=finding.suppressed,
        suppression_reason=finding.suppression_reason,
        dedupe_key=ticket_dedupe_key(target, finding.id),
    )


def ticket_dedupe_key(target: str, finding_id: str) -> str:
    material = f"{target.strip().lower()}|{finding_id.strip().lower()}"
    digest = hashlib.sha256(material.encode("utf-8")).hexdigest()[:20]
    return f"piranesi-{digest}"


def write_export_audit(
    *,
    output_dir: str | Path,
    integration: str,
    dry_run: bool,
    details: Mapping[str, Any] | None = None,
) -> Path:
    return append_audit_event(
        output_dir=Path(output_dir),
        event_type=f"integration.{integration}",
        stage="export",
        approved=not dry_run,
        details=dict(details or {}),
    )


def redact_metadata(value: Any) -> Any:
    return _redact_value(value, parent_key="")


def redacted_target(target: str) -> str:
    if not target:
        return "unknown"
    return "[redacted-host]"


def write_json(path: str | Path, payload: Mapping[str, Any]) -> Path:
    output = Path(path).expanduser().resolve(strict=False)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(payload, indent=2, sort_keys=True), encoding="utf-8")
    return output


def _redact_value(value: Any, *, parent_key: str) -> Any:
    if isinstance(value, Mapping):
        return {
            str(key): _redact_value(
                item,
                parent_key=str(key),
            )
            for key, item in value.items()
        }
    if isinstance(value, list):
        return [_redact_value(item, parent_key=parent_key) for item in value]
    if _SENSITIVE_KEY_PATTERN.search(parent_key):
        return "[redacted]"
    return value


def _fleet_report_root(report_path: str | Path | None) -> Path | None:
    if report_path is None:
        return None
    candidate = Path(report_path).expanduser().resolve(strict=False)
    if candidate.is_dir():
        return candidate
    return candidate.parent


def _resolve_host_report_path(root: Path | None, raw_path: str) -> Path | None:
    if not raw_path:
        return None
    report_path = Path(raw_path)
    if report_path.is_absolute():
        return report_path
    if root is None:
        return None
    return (root / report_path).resolve(strict=False)


def _string_path(path: str | Path | None) -> str | None:
    if path is None:
        return None
    return str(Path(path).expanduser().resolve(strict=False))


def _truncate(value: str, *, limit: int = 300) -> str:
    if len(value) <= limit:
        return value
    return f"{value[: limit - 3]}..."


def preview_items(findings: Iterable[ExportedFinding], *, limit: int = 10) -> list[dict[str, Any]]:
    rendered: list[dict[str, Any]] = []
    for finding in findings:
        if len(rendered) >= limit:
            break
        rendered.append(
            {
                "target": finding.target,
                "finding_id": finding.finding_id,
                "title": finding.title,
                "severity": finding.severity,
                "dedupe_key": finding.dedupe_key,
            }
        )
    return rendered


__all__ = [
    "ExportResult",
    "ExportedFinding",
    "ExporterError",
    "FindingExporter",
    "ReportModel",
    "iter_export_findings",
    "load_report",
    "normalize_finding",
    "preview_items",
    "redact_metadata",
    "redacted_target",
    "ticket_dedupe_key",
    "write_export_audit",
    "write_json",
]
