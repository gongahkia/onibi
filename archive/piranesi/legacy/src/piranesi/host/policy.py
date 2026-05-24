from __future__ import annotations

import tomllib
from collections import Counter
from pathlib import Path
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, ValidationError, field_validator, model_validator

from piranesi.host.models import FleetReport, HostFinding, HostPostureReport, Severity

PolicyAction = Literal["warn", "fail"]
PolicyStatus = Literal["pass", "warn", "fail"]
PolicyWhen = Literal["always", "public_ssh", "public_listener"]

_SEVERITY_RANK: dict[str, int] = {
    "informational": 0,
    "low": 1,
    "medium": 2,
    "high": 3,
    "critical": 4,
}


class HostPolicyError(ValueError):
    """Raised when a host policy file is invalid."""


class HostPolicyGate(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    rule_id: str | None = None
    category: str | None = None
    when: PolicyWhen = "always"
    max_severity: Severity | None = None
    max_risk: float | None = Field(default=None, ge=0.0, le=100.0)
    action: PolicyAction = "fail"

    @model_validator(mode="after")
    def _requires_matcher(self) -> HostPolicyGate:
        if self.rule_id is None and self.category is None and self.when == "always":
            raise ValueError("gate must set rule_id, category, or a non-default when")
        return self


class HostRequiredEvidence(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    required: bool = True
    action: PolicyAction = "warn"


class HostAllowedExposure(BaseModel):
    model_config = ConfigDict(extra="forbid")

    port: int | None = Field(default=None, ge=1, le=65535)
    service: str | None = None
    protocol: str | None = "tcp"
    reason: str | None = None

    @model_validator(mode="after")
    def _requires_port_or_service(self) -> HostAllowedExposure:
        if self.port is None and not self.service:
            raise ValueError("allowed exposure must set port or service")
        return self


class FleetPolicy(BaseModel):
    model_config = ConfigDict(extra="forbid")

    max_failed_hosts: int | None = Field(default=0, ge=0)
    minimum_passing_hosts_percent: float | None = Field(default=None, ge=0.0, le=100.0)
    max_policy_failures: int | None = Field(default=0, ge=0)


class HostPolicy(BaseModel):
    model_config = ConfigDict(extra="forbid")

    profile: str
    minimum_score: int | None = Field(default=None, ge=0, le=100)
    max_severity: Severity | None = None
    max_risk: float | None = Field(default=None, ge=0.0, le=100.0)
    allow_suppressed: bool = True
    suppression_expiry_required: bool = False
    gates: list[HostPolicyGate] = Field(default_factory=list)
    required_evidence: list[HostRequiredEvidence] = Field(default_factory=list)
    allowed_exposure: list[HostAllowedExposure] = Field(default_factory=list)
    fleet: FleetPolicy = Field(default_factory=FleetPolicy)

    @field_validator("profile")
    @classmethod
    def _profile_not_blank(cls, value: str) -> str:
        cleaned = value.strip()
        if not cleaned:
            raise ValueError("profile must not be blank")
        return cleaned


class HostPolicyGateResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    gate_id: str
    status: PolicyStatus
    action: PolicyAction
    message: str
    finding_ids: list[str] = Field(default_factory=list)
    matched_count: int = 0


class HostPolicyRequiredEvidenceResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    required: bool
    available: bool
    status: PolicyStatus
    action: PolicyAction
    message: str


class HostPolicyResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    profile: str
    passed: bool
    summary: dict[str, object]
    gate_results: list[HostPolicyGateResult] = Field(default_factory=list)
    required_evidence: list[HostPolicyRequiredEvidenceResult] = Field(default_factory=list)


def load_host_policy(path: str | Path) -> HostPolicy:
    policy_path = Path(path).expanduser().resolve(strict=False)
    try:
        payload = tomllib.loads(policy_path.read_text(encoding="utf-8"))
    except OSError as exc:
        raise HostPolicyError(f"could not read policy file {policy_path}: {exc}") from exc
    except tomllib.TOMLDecodeError as exc:
        raise HostPolicyError(f"invalid TOML in policy file {policy_path}: {exc}") from exc
    try:
        policy_payload = payload["host"]["policy"]
    except KeyError as exc:
        raise HostPolicyError("policy file must contain [host.policy]") from exc
    try:
        return HostPolicy.model_validate(policy_payload)
    except ValidationError as exc:
        detail = "; ".join(error["msg"] for error in exc.errors())
        raise HostPolicyError(f"invalid host policy: {detail}") from exc


def evaluate_host_policy(report: HostPostureReport, policy: HostPolicy) -> HostPolicyResult:
    evidence_results = [
        _evaluate_required_evidence(report, item) for item in policy.required_evidence
    ]
    gate_results: list[HostPolicyGateResult] = []
    gate_results.extend(_global_gate_results(report, policy))
    gate_results.extend(_rule_gate_results(report, policy))
    all_results: list[HostPolicyGateResult | HostPolicyRequiredEvidenceResult] = [
        *gate_results,
        *evidence_results,
    ]
    failed = [result for result in all_results if result.status == "fail"]
    warnings = [result for result in all_results if result.status == "warn"]
    summary: dict[str, object] = {
        "passed": not failed,
        "failed_gate_count": sum(1 for result in gate_results if result.status == "fail"),
        "warning_count": len(warnings),
        "required_evidence_failures": sum(
            1 for result in evidence_results if result.status == "fail"
        ),
        "required_evidence_warnings": sum(
            1 for result in evidence_results if result.status == "warn"
        ),
    }
    return HostPolicyResult(
        profile=policy.profile,
        passed=not failed,
        summary=summary,
        gate_results=gate_results,
        required_evidence=evidence_results,
    )


def apply_host_policy(report: HostPostureReport, policy: HostPolicy) -> HostPostureReport:
    result = evaluate_host_policy(report, policy)
    return report.model_copy(
        update={
            "policy_profile": result.profile,
            "policy_summary": result.summary,
            "policy_gate_results": [gate.model_dump(mode="json") for gate in result.gate_results],
            "required_evidence_status": [
                evidence.model_dump(mode="json") for evidence in result.required_evidence
            ],
        }
    )


def apply_fleet_policy(
    report: FleetReport,
    host_reports: dict[str, HostPostureReport],
    policy: HostPolicy,
) -> FleetReport:
    host_failures = [
        host_report.target
        for host_report in host_reports.values()
        if host_report.policy_summary.get("passed") is False
    ]
    gate_results = _fleet_gate_results(report, host_reports, policy, host_failures)
    required_evidence = _fleet_required_evidence_results(host_reports, policy)
    all_results: list[HostPolicyGateResult | HostPolicyRequiredEvidenceResult] = [
        *gate_results,
        *required_evidence,
    ]
    failed = [result for result in all_results if result.status == "fail"]
    warnings = [result for result in all_results if result.status == "warn"]
    summary = dict(report.summary)
    summary["policy"] = {
        "passed": not failed,
        "host_policy_failures": len(host_failures),
        "failed_gate_count": sum(1 for result in gate_results if result.status == "fail"),
        "warning_count": len(warnings),
    }
    return report.model_copy(
        update={
            "summary": summary,
            "policy_profile": policy.profile,
            "policy_summary": summary["policy"],
            "policy_gate_results": [gate.model_dump(mode="json") for gate in gate_results],
            "required_evidence_status": [
                evidence.model_dump(mode="json") for evidence in required_evidence
            ],
        }
    )


def policy_failed(report: HostPostureReport | FleetReport) -> bool:
    return report.policy_summary.get("passed") is False


def render_policy_validation(policy: HostPolicy) -> str:
    return (
        f"policy: {policy.profile}\n"
        f"gates: {len(policy.gates)}\n"
        f"required_evidence: {len(policy.required_evidence)}\n"
        f"allowed_exposure: {len(policy.allowed_exposure)}\n"
    )


def _global_gate_results(
    report: HostPostureReport,
    policy: HostPolicy,
) -> list[HostPolicyGateResult]:
    results: list[HostPolicyGateResult] = []
    if policy.minimum_score is not None:
        failed = report.posture_score < policy.minimum_score
        results.append(
            HostPolicyGateResult(
                gate_id="minimum-score",
                status="fail" if failed else "pass",
                action="fail",
                message=(
                    f"posture score {report.posture_score} is below minimum {policy.minimum_score}"
                    if failed
                    else (
                        f"posture score {report.posture_score} meets minimum {policy.minimum_score}"
                    )
                ),
            )
        )
    eligible_findings = _eligible_findings(report, policy)
    if policy.max_severity is not None:
        violations = [
            finding
            for finding in eligible_findings
            if _severity_gt(finding.severity, policy.max_severity)
        ]
        results.append(
            _finding_threshold_result(
                gate_id="max-severity",
                action="fail",
                violations=violations,
                message_prefix=f"findings above {policy.max_severity} severity",
            )
        )
    if policy.max_risk is not None:
        violations = [
            finding for finding in eligible_findings if _risk_total(finding) > policy.max_risk
        ]
        results.append(
            _finding_threshold_result(
                gate_id="max-risk",
                action="fail",
                violations=violations,
                message_prefix=f"findings above {policy.max_risk:g} risk",
            )
        )
    if not policy.allow_suppressed:
        suppressed = [finding for finding in report.findings if finding.suppressed]
        results.append(
            _finding_threshold_result(
                gate_id="suppressed-findings-disallowed",
                action="fail",
                violations=suppressed,
                message_prefix="suppressed findings are not allowed by this policy",
            )
        )
    return results


def _rule_gate_results(
    report: HostPostureReport,
    policy: HostPolicy,
) -> list[HostPolicyGateResult]:
    results: list[HostPolicyGateResult] = []
    for gate in policy.gates:
        matched = [
            finding
            for finding in _eligible_findings(report, policy)
            if _finding_matches_gate(finding, gate, report)
            and not _finding_matches_allowed_exposure(finding, policy)
        ]
        if gate.max_severity is not None:
            violations = [
                finding for finding in matched if _severity_gt(finding.severity, gate.max_severity)
            ]
        elif gate.max_risk is not None:
            violations = [finding for finding in matched if _risk_total(finding) > gate.max_risk]
        else:
            violations = matched
        status: PolicyStatus = "pass"
        if violations:
            status = gate.action
        message = (
            f"{len(violations)} finding(s) violated gate {gate.id}"
            if violations
            else f"gate {gate.id} passed"
        )
        results.append(
            HostPolicyGateResult(
                gate_id=gate.id,
                status=status,
                action=gate.action,
                message=message,
                finding_ids=[finding.id for finding in violations],
                matched_count=len(matched),
            )
        )
    return results


def _evaluate_required_evidence(
    report: HostPostureReport,
    item: HostRequiredEvidence,
) -> HostPolicyRequiredEvidenceResult:
    available = _evidence_available(report, item.name)
    status: PolicyStatus = "pass"
    if item.required and not available:
        status = item.action
    return HostPolicyRequiredEvidenceResult(
        name=item.name,
        required=item.required,
        available=available,
        status=status,
        action=item.action,
        message=(
            f"required evidence `{item.name}` is available"
            if available
            else f"required evidence `{item.name}` is missing"
        ),
    )


def _fleet_gate_results(
    report: FleetReport,
    host_reports: dict[str, HostPostureReport],
    policy: HostPolicy,
    host_failures: list[str],
) -> list[HostPolicyGateResult]:
    results: list[HostPolicyGateResult] = []
    max_policy_failures = policy.fleet.max_policy_failures
    if max_policy_failures is not None:
        failed = len(host_failures) > max_policy_failures
        results.append(
            HostPolicyGateResult(
                gate_id="fleet-host-policy-failures",
                status="fail" if failed else "pass",
                action="fail",
                message=(
                    f"{len(host_failures)} host(s) failed policy; maximum is {max_policy_failures}"
                ),
                finding_ids=[],
                matched_count=len(host_failures),
            )
        )
    if policy.fleet.max_failed_hosts is not None:
        failed = report.failure_count > policy.fleet.max_failed_hosts
        results.append(
            HostPolicyGateResult(
                gate_id="fleet-failed-hosts",
                status="fail" if failed else "pass",
                action="fail",
                message=(
                    f"{report.failure_count} host(s) failed assessment; maximum is "
                    f"{policy.fleet.max_failed_hosts}"
                ),
                matched_count=report.failure_count,
            )
        )
    if policy.fleet.minimum_passing_hosts_percent is not None:
        total = max(1, len(host_reports))
        passing = sum(
            1
            for host_report in host_reports.values()
            if host_report.policy_summary.get("passed") is not False
        )
        percent = passing / total * 100
        failed = percent < policy.fleet.minimum_passing_hosts_percent
        results.append(
            HostPolicyGateResult(
                gate_id="fleet-passing-host-percent",
                status="fail" if failed else "pass",
                action="fail",
                message=(
                    f"{percent:.1f}% of assessed hosts passed policy; minimum is "
                    f"{policy.fleet.minimum_passing_hosts_percent:.1f}%"
                ),
                matched_count=passing,
            )
        )
    return results


def _fleet_required_evidence_results(
    host_reports: dict[str, HostPostureReport],
    policy: HostPolicy,
) -> list[HostPolicyRequiredEvidenceResult]:
    results: list[HostPolicyRequiredEvidenceResult] = []
    for item in policy.required_evidence:
        missing = [
            report.target
            for report in host_reports.values()
            if not _evidence_available(report, item.name)
        ]
        status: PolicyStatus = "pass"
        if item.required and missing:
            status = item.action
        results.append(
            HostPolicyRequiredEvidenceResult(
                name=item.name,
                required=item.required,
                available=not missing,
                status=status,
                action=item.action,
                message=(
                    f"required evidence `{item.name}` is available on all assessed hosts"
                    if not missing
                    else (f"required evidence `{item.name}` is missing on {len(missing)} host(s)")
                ),
            )
        )
    return results


def _eligible_findings(report: HostPostureReport, policy: HostPolicy) -> list[HostFinding]:
    if policy.allow_suppressed:
        return [finding for finding in report.findings if not finding.suppressed]
    return list(report.findings)


def _finding_matches_gate(
    finding: HostFinding,
    gate: HostPolicyGate,
    report: HostPostureReport,
) -> bool:
    if gate.rule_id is not None and finding.rule_id != gate.rule_id:
        return False
    if gate.category is not None and finding.category != gate.category:
        return False
    if gate.when == "public_ssh":
        return _report_has_public_ssh(report)
    if gate.when == "public_listener":
        return _finding_is_public_listener(finding)
    return True


def _finding_matches_allowed_exposure(finding: HostFinding, policy: HostPolicy) -> bool:
    if not _finding_is_public_listener(finding):
        return False
    component = finding.affected_component or ""
    title = finding.title.lower()
    for allowed in policy.allowed_exposure:
        if allowed.port is not None and f"/{allowed.port}" in component:
            return True
        if allowed.service and allowed.service.lower() in title:
            return True
    return False


def _finding_is_public_listener(finding: HostFinding) -> bool:
    return finding.rule_id in {
        "host.listener.high_risk_service",
        "host.listener.ssh_public",
    }


def _report_has_public_ssh(report: HostPostureReport) -> bool:
    return any(
        finding.rule_id == "host.listener.ssh_public" and not finding.suppressed
        for finding in report.findings
    )


def _finding_threshold_result(
    *,
    gate_id: str,
    action: PolicyAction,
    violations: list[HostFinding],
    message_prefix: str,
) -> HostPolicyGateResult:
    return HostPolicyGateResult(
        gate_id=gate_id,
        status=action if violations else "pass",
        action=action,
        message=(f"{len(violations)} {message_prefix}" if violations else f"no {message_prefix}"),
        finding_ids=[finding.id for finding in violations],
        matched_count=len(violations),
    )


def _evidence_available(report: HostPostureReport, name: str) -> bool:
    normalized = name.strip().lower().replace("-", "_")
    completeness = report.host_metadata.get("evidence_completeness")
    if isinstance(completeness, dict) and normalized in completeness:
        return completeness[normalized] is True
    raw_evidence = report.snapshot.raw_evidence
    if normalized in {"trivy", "commands", "lynis", "openscap"}:
        return normalized in raw_evidence
    inventory = report.evidence_inventory
    aliases = {
        "osquery": ["packages", "listening_ports", "users"],
        "network": ["network_interfaces", "ip_addresses"],
        "trivy": ["raw_tools"],
        "auth_evidence": ["login_sessions", "auth_event_summaries"],
    }
    if normalized in inventory:
        return int(inventory.get(normalized) or 0) > 0
    if normalized in aliases:
        return any(int(inventory.get(alias) or 0) > 0 for alias in aliases[normalized])
    health = report.collection_health
    if health is not None:
        capability = health.required.get(normalized) or health.optional.get(normalized)
        if capability is not None:
            return capability.status == "ok"
    return False


def _severity_gt(left: Severity, right: Severity) -> bool:
    return _SEVERITY_RANK[left] > _SEVERITY_RANK[right]


def _risk_total(finding: HostFinding) -> float:
    if finding.risk is None:
        return 0.0
    return finding.risk.total


def fleet_policy_status_counts(report: FleetReport) -> dict[str, int]:
    counter: Counter[str] = Counter()
    for result in [*report.policy_gate_results, *report.required_evidence_status]:
        status = result.get("status")
        if isinstance(status, str):
            counter[status] += 1
    return dict(counter)


__all__ = [
    "FleetPolicy",
    "HostAllowedExposure",
    "HostPolicy",
    "HostPolicyError",
    "HostPolicyGate",
    "HostPolicyGateResult",
    "HostPolicyRequiredEvidenceResult",
    "HostPolicyResult",
    "HostRequiredEvidence",
    "apply_fleet_policy",
    "apply_host_policy",
    "evaluate_host_policy",
    "fleet_policy_status_counts",
    "load_host_policy",
    "policy_failed",
    "render_policy_validation",
]
