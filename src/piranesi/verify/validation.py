from __future__ import annotations

import json
from pathlib import Path
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from piranesi.host import HostPostureReport

_SEVERITY_ORDER = ("informational", "low", "medium", "high", "critical")
_RISK_ORDER = ("low", "medium", "high", "critical")


class EvidenceValidationCheck(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    passed: bool
    detail: str


class FindingEvidenceValidation(BaseModel):
    model_config = ConfigDict(extra="forbid")

    finding_id: str
    valid: bool
    checks: list[EvidenceValidationCheck] = Field(default_factory=list)


class EvidenceValidationReport(BaseModel):
    model_config = ConfigDict(extra="forbid")

    output_dir: str
    valid: bool
    checked_findings: int
    valid_findings: int
    invalid_findings: int
    findings: list[FindingEvidenceValidation] = Field(default_factory=list)
    warnings: list[str] = Field(default_factory=list)


def validate_evidence_bundle(
    output_dir: str | Path,
    *,
    strict: bool = False,
) -> EvidenceValidationReport:
    root = Path(output_dir).expanduser().resolve(strict=False)
    host_report_payload = _load_json(root / "host-report.json")
    if host_report_payload is not None and not (root / "report.json").exists():
        return _validate_host_evidence_bundle(root, host_report_payload)

    report_payload = _load_json(root / "report.json")
    verify_payload = _load_json(root / "verify.json")
    warnings: list[str] = []
    if report_payload is None:
        warnings.append("report.json not found or invalid")
    if verify_payload is None:
        warnings.append("verify.json not found or invalid")

    attempts = _attempts_by_finding(verify_payload)
    report_findings = _report_findings(report_payload)
    validations = [
        _validate_report_finding(root, finding, attempts=attempts, strict=strict)
        for finding in report_findings
    ]

    valid_findings = sum(1 for finding in validations if finding.valid)
    invalid_findings = len(validations) - valid_findings
    return EvidenceValidationReport(
        output_dir=str(root),
        valid=not warnings and invalid_findings == 0,
        checked_findings=len(validations),
        valid_findings=valid_findings,
        invalid_findings=invalid_findings,
        findings=validations,
        warnings=warnings,
    )


def _validate_host_evidence_bundle(
    output_dir: Path,
    report_payload: Any,
) -> EvidenceValidationReport:
    checks: list[EvidenceValidationCheck] = []
    parsed_report: HostPostureReport | None = None
    schema_error: str | None = None
    try:
        parsed_report = HostPostureReport.model_validate(report_payload)
    except ValidationError as exc:
        schema_error = str(exc.errors()[0].get("msg", exc))
    checks.append(
        _check(
            "host_report_schema",
            parsed_report is not None,
            "host-report.json matches the host posture report schema"
            if schema_error is None
            else f"host-report.json schema validation failed: {schema_error}",
        )
    )
    snapshot = parsed_report.snapshot if parsed_report is not None else None
    checks.append(
        _check(
            "host_snapshot_identity",
            snapshot is not None and bool(snapshot.identity.hostname),
            "embedded snapshot identity is present",
        )
    )
    finding_count = len(parsed_report.findings) if parsed_report is not None else 0
    validations = [
        FindingEvidenceValidation(
            finding_id="host-report.json",
            valid=all(check.passed for check in checks),
            checks=checks,
        )
    ]
    if parsed_report is not None:
        validations.extend(_validate_host_finding(finding) for finding in parsed_report.findings)
    report_valid = validations[0].valid
    host_valid_findings = sum(1 for finding in validations[1:] if finding.valid)
    invalid_findings = (
        finding_count - host_valid_findings if report_valid else max(finding_count, 1)
    )
    return EvidenceValidationReport(
        output_dir=str(output_dir),
        valid=report_valid and invalid_findings == 0,
        checked_findings=finding_count,
        valid_findings=host_valid_findings if report_valid else 0,
        invalid_findings=invalid_findings,
        findings=validations,
    )


def _validate_host_finding(finding: Any) -> FindingEvidenceValidation:
    checks = [
        _check("host_finding_id", bool(finding.id), "finding id is present"),
        _check("host_finding_title", bool(finding.title), "finding title is present"),
        _check("host_finding_category", bool(finding.category), "finding category is present"),
        _check("host_finding_severity", bool(finding.severity), "finding severity is present"),
        _check(
            "host_finding_remediation",
            bool(finding.remediation),
            "finding remediation is present",
        ),
        _check("host_finding_source", bool(finding.source_tool), "finding source tool is present"),
    ]
    return FindingEvidenceValidation(
        finding_id=finding.id,
        valid=all(check.passed for check in checks),
        checks=checks,
    )


def render_evidence_validation_report(report: EvidenceValidationReport) -> str:
    lines = [
        "Piranesi evidence validation",
        f"Output: {report.output_dir}",
        f"Valid: {'yes' if report.valid else 'no'}",
        (
            f"Findings: {report.valid_findings} valid, "
            f"{report.invalid_findings} invalid, {report.checked_findings} checked"
        ),
    ]
    if report.warnings:
        lines.append("")
        lines.append("Warnings:")
        lines.extend(f"- {warning}" for warning in report.warnings)
    for finding in report.findings:
        lines.append("")
        lines.append(f"- {'PASS' if finding.valid else 'FAIL'} {finding.finding_id}")
        for check in finding.checks:
            lines.append(f"  - [{'PASS' if check.passed else 'FAIL'}] {check.name}: {check.detail}")
    return "\n".join(lines) + "\n"


def _validate_report_finding(
    output_dir: Path,
    finding: dict[str, Any],
    *,
    attempts: dict[str, list[dict[str, Any]]],
    strict: bool,
) -> FindingEvidenceValidation:
    finding_id = str(finding.get("finding_id") or finding.get("id") or "unknown")
    checks = [
        _severity_consistency_check(finding),
        _location_check(finding),
        _reproducer_check(finding),
        _attempt_check(finding, attempts.get(finding_id, []), strict=strict),
        _artifact_check(output_dir, finding, attempts.get(finding_id, []), strict=strict),
    ]
    return FindingEvidenceValidation(
        finding_id=finding_id,
        valid=all(check.passed for check in checks),
        checks=checks,
    )


def _severity_consistency_check(finding: dict[str, Any]) -> EvidenceValidationCheck:
    severity = str(finding.get("severity", "")).lower()
    if severity not in _SEVERITY_ORDER:
        return _check("severity_consistency", False, f"unknown severity {severity!r}")
    risk = str(finding.get("composite_risk_band", "")).lower()
    if risk in _RISK_ORDER and severity in {"critical", "high"} and risk == "low":
        return _check(
            "severity_consistency",
            False,
            f"{severity} severity contradicts low composite risk band",
        )
    return _check("severity_consistency", True, f"severity={severity}")


def _location_check(finding: dict[str, Any]) -> EvidenceValidationCheck:
    source = finding.get("source_location")
    sink = finding.get("sink_location")
    missing = [
        name
        for name, value in (("source_location", source), ("sink_location", sink))
        if not isinstance(value, dict) or not value.get("file") or not value.get("line")
    ]
    if missing:
        return _check("evidence_exists", False, f"missing {', '.join(missing)}")
    return _check("evidence_exists", True, "source and sink locations present")


def _reproducer_check(finding: dict[str, Any]) -> EvidenceValidationCheck:
    status = str(finding.get("evidence_status", ""))
    reproducer = finding.get("reproducer_script")
    if status == "confirmed":
        if not isinstance(reproducer, str) or not reproducer.strip():
            return _check("reproducer", False, "confirmed finding has no reproducer script")
        if "Only run against systems you own" not in reproducer:
            return _check("reproducer", False, "reproducer is missing authorization warning")
    return _check("reproducer", True, "reproducer requirement satisfied")


def _attempt_check(
    finding: dict[str, Any],
    attempts: list[dict[str, Any]],
    *,
    strict: bool,
) -> EvidenceValidationCheck:
    status = str(finding.get("evidence_status", ""))
    if not attempts:
        if strict or status == "confirmed":
            return _check("log_corroboration", False, "no verify attempt for finding")
        return _check("log_corroboration", True, "attempt not required for candidate finding")
    has_confirmed_attempt = any(attempt.get("status") == "confirmed" for attempt in attempts)
    if status == "confirmed" and not has_confirmed_attempt:
        return _check("log_corroboration", False, "confirmed report lacks confirmed attempt")
    return _check("log_corroboration", True, f"{len(attempts)} verify attempt(s) found")


def _artifact_check(
    output_dir: Path,
    finding: dict[str, Any],
    attempts: list[dict[str, Any]],
    *,
    strict: bool,
) -> EvidenceValidationCheck:
    paths = [
        path
        for path in [
            _nested_get(finding, "explanation", "verification_state", "evidence_artifact_path"),
            *[attempt.get("evidence_artifact_path") for attempt in attempts],
        ]
        if isinstance(path, str) and path.strip()
    ]
    if not paths:
        if strict or finding.get("evidence_status") == "confirmed":
            return _check("claims_vs_raw", False, "no evidence artifact path recorded")
        return _check("claims_vs_raw", True, "raw artifact not required for candidate finding")
    missing = [path for path in paths if not _resolve_artifact_path(output_dir, path).is_file()]
    if missing:
        return _check("claims_vs_raw", False, f"missing artifact(s): {', '.join(missing)}")
    return _check("claims_vs_raw", True, f"{len(paths)} evidence artifact path(s) exist")


def _report_findings(report_payload: dict[str, Any] | None) -> list[dict[str, Any]]:
    if not isinstance(report_payload, dict):
        return []
    findings: list[dict[str, Any]] = []
    for key in ("findings", "active_findings", "unreachable_findings", "suppressed_findings"):
        values = report_payload.get(key, [])
        if isinstance(values, list):
            findings.extend(value for value in values if isinstance(value, dict))
    return findings


def _attempts_by_finding(verify_payload: dict[str, Any] | None) -> dict[str, list[dict[str, Any]]]:
    if not isinstance(verify_payload, dict):
        return {}
    attempts: dict[str, list[dict[str, Any]]] = {}
    for attempt in verify_payload.get("attempts", []):
        if not isinstance(attempt, dict):
            continue
        finding_id = attempt.get("finding_id")
        if not isinstance(finding_id, str) or not finding_id:
            continue
        attempts.setdefault(finding_id, []).append(attempt)
    return attempts


def _load_json(path: Path) -> dict[str, Any] | None:
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None
    return payload if isinstance(payload, dict) else None


def _resolve_artifact_path(output_dir: Path, path: str) -> Path:
    candidate = Path(path).expanduser()
    if candidate.is_absolute():
        return candidate.resolve(strict=False)
    return (output_dir / candidate).resolve(strict=False)


def _nested_get(payload: dict[str, Any], *keys: str) -> object:
    current: object = payload
    for key in keys:
        if not isinstance(current, dict):
            return None
        current = current.get(key)
    return current


def _check(name: str, passed: bool, detail: str) -> EvidenceValidationCheck:
    return EvidenceValidationCheck(name=name, passed=passed, detail=detail)


EvidenceValidationFormat = Literal["text", "json"]


__all__ = [
    "EvidenceValidationCheck",
    "EvidenceValidationFormat",
    "EvidenceValidationReport",
    "FindingEvidenceValidation",
    "render_evidence_validation_report",
    "validate_evidence_bundle",
]
