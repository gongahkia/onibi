from __future__ import annotations

import json
from collections import Counter
from datetime import UTC, datetime
from typing import Literal, cast

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from piranesi.host.models import (
    AnalysisMode,
    EvidenceItem,
    HostFinding,
    HostPostureReport,
    HostSnapshot,
    ListeningPort,
    Severity,
    host_finding_id,
)
from piranesi.llm.provider import LLMProvider
from piranesi.llm.router import TokenBudgetExceededError

AnalysisSelection = Literal["deterministic", "llm", "both"]

_SEVERITY_RANK: dict[str, int] = {
    "informational": 0,
    "low": 1,
    "medium": 2,
    "high": 3,
    "critical": 4,
}
_ANY_IPV4 = "0.0.0.0"  # noqa: S104
_PUBLIC_BIND_ADDRESSES = {_ANY_IPV4, "::", "", "*"}
_SEVERITY_PENALTIES = {
    "critical": 30,
    "high": 18,
    "medium": 8,
    "low": 3,
    "informational": 1,
}
_HIGH_RISK_PORTS = {
    21: "FTP",
    23: "Telnet",
    25: "SMTP",
    3306: "MySQL",
    5432: "PostgreSQL",
    6379: "Redis",
    9200: "Elasticsearch",
    11211: "Memcached",
    27017: "MongoDB",
}
_ADMIN_GROUPS = {"sudo", "admin", "wheel"}
_REQUIRED_EVIDENCE = {
    "packages": "Package inventory is required for CVE and patch posture.",
    "listening_ports": "Listening port inventory is required for exposure analysis.",
    "users": "User inventory is required for privilege posture.",
}


class _LlmHostFinding(BaseModel):
    model_config = ConfigDict(extra="forbid")

    title: str
    category: str
    severity: Severity
    confidence: float = Field(ge=0.0, le=1.0)
    affected_component: str | None = None
    evidence_keys: list[str] = Field(default_factory=list)
    remediation: str
    rationale: str


class _LlmHostAnalysis(BaseModel):
    model_config = ConfigDict(extra="forbid")

    findings: list[_LlmHostFinding] = Field(default_factory=list)


def analyze_snapshot(
    snapshot: HostSnapshot,
    *,
    analysis: AnalysisSelection = "deterministic",
    provider: LLMProvider | None = None,
) -> HostPostureReport:
    deterministic = deterministic_findings(snapshot)
    findings = list(deterministic)
    modes: list[AnalysisMode] = ["deterministic"]
    if analysis == "llm":
        modes = ["llm"]
        findings = (
            llm_findings(snapshot, findings=deterministic, provider=provider)
            if provider
            else []
        )
        if not findings:
            findings.append(_llm_unavailable_finding(snapshot))
    elif analysis == "both":
        modes = ["deterministic", "llm"]
        if provider is not None:
            findings.extend(llm_findings(snapshot, findings=deterministic, provider=provider))
        else:
            findings.append(_llm_unavailable_finding(snapshot))
    ranked = _rank_findings(_dedupe_findings(findings))
    return HostPostureReport(
        target=snapshot.identity.hostname,
        generated_at=datetime.now(UTC).isoformat(),
        analysis_modes=modes,
        posture_score=_posture_score(ranked),
        summary=_summary(ranked),
        findings=ranked,
        evidence_inventory=_evidence_inventory(snapshot),
        known_limitations=[
            "Phase 1 supports Debian/Ubuntu-oriented host evidence only.",
            "Raw bundle ingestion is first-class for osquery and Trivy JSON outputs.",
            "LLM analysis is advisory and must remain tied to explicit snapshot evidence.",
        ],
        snapshot=snapshot,
    )


def deterministic_findings(snapshot: HostSnapshot) -> list[HostFinding]:
    findings: list[HostFinding] = []
    findings.extend(_trivy_vulnerability_findings(snapshot))
    findings.extend(_exposed_port_findings(snapshot))
    findings.extend(_ssh_config_findings(snapshot))
    findings.extend(_privileged_user_findings(snapshot))
    findings.extend(_missing_evidence_findings(snapshot))
    return findings


def llm_findings(
    snapshot: HostSnapshot,
    *,
    findings: list[HostFinding],
    provider: LLMProvider,
) -> list[HostFinding]:
    prompt = _llm_prompt(snapshot, findings)
    try:
        response = provider.complete(
            stage="triage",
            messages=[
                {
                    "role": "system",
                    "content": (
                        "You analyze Linux VM security posture evidence. "
                        "Only report issues supported by the provided evidence. "
                        "Do not invent missing host facts."
                    ),
                },
                {"role": "user", "content": prompt},
            ],
            max_tokens=1200,
        )
    except TokenBudgetExceededError as exc:
        return [_llm_unavailable_finding(snapshot, reason=str(exc))]
    except Exception as exc:
        return [_llm_unavailable_finding(snapshot, reason=f"LLM analysis failed: {exc}")]
    try:
        payload = _LlmHostAnalysis.model_validate_json(response.content)
    except (ValidationError, ValueError):
        return [
            _llm_unavailable_finding(
                snapshot,
                reason="LLM returned an invalid host analysis payload",
            )
        ]

    evidence_by_key = _evidence_by_key(snapshot)
    rendered: list[HostFinding] = []
    for item in payload.findings:
        evidence = [
            evidence_by_key[key]
            for key in item.evidence_keys
            if key in evidence_by_key
        ]
        if not evidence:
            continue
        rendered.append(
            HostFinding(
                id=host_finding_id(
                    "llm",
                    snapshot.identity.hostname,
                    item.title,
                    item.affected_component,
                ),
                title=item.title,
                category=item.category,
                severity=item.severity,
                confidence=item.confidence,
                affected_component=item.affected_component,
                evidence=evidence,
                remediation=item.remediation,
                source_tool="llm",
                analysis_mode="llm",
                rationale=item.rationale,
            )
        )
    return rendered


def _trivy_vulnerability_findings(snapshot: HostSnapshot) -> list[HostFinding]:
    raw = snapshot.raw_evidence.get("trivy")
    payloads = raw if isinstance(raw, dict) else {}
    findings: list[HostFinding] = []
    for payload in payloads.values():
        for result in _trivy_results(payload):
            target = str(result.get("Target") or "host")
            vulnerabilities = result.get("Vulnerabilities")
            if not isinstance(vulnerabilities, list):
                continue
            for vuln in vulnerabilities:
                if not isinstance(vuln, dict):
                    continue
                vuln_id = _string(vuln.get("VulnerabilityID"))
                pkg_name = _string(vuln.get("PkgName"))
                installed = _string(vuln.get("InstalledVersion"))
                severity = _normalize_severity(vuln.get("Severity"))
                if not vuln_id or not pkg_name:
                    continue
                fixed = _string(vuln.get("FixedVersion"))
                title = _string(vuln.get("Title")) or f"{vuln_id} affects {pkg_name}"
                evidence = [
                    EvidenceItem(source="trivy", key="target", value=target),
                    EvidenceItem(source="trivy", key="package", value=pkg_name),
                    EvidenceItem(
                        source="trivy",
                        key="installed_version",
                        value=installed or "unknown",
                    ),
                    EvidenceItem(source="trivy", key="vulnerability", value=vuln_id),
                ]
                if fixed:
                    evidence.append(EvidenceItem(source="trivy", key="fixed_version", value=fixed))
                findings.append(
                    HostFinding(
                        id=host_finding_id("trivy", snapshot.identity.hostname, pkg_name, vuln_id),
                        title=title,
                        category="vulnerability",
                        severity=severity,
                        confidence=0.95,
                        affected_component=pkg_name,
                        cve_ids=[vuln_id] if vuln_id.startswith("CVE-") else [],
                        evidence=evidence,
                        remediation=(
                            f"Upgrade {pkg_name} to {fixed} or later."
                            if fixed
                            else (
                                f"Review vendor guidance for {pkg_name} and apply "
                                "available security updates."
                            )
                        ),
                        source_tool="trivy",
                    )
                )
    return findings


def _exposed_port_findings(snapshot: HostSnapshot) -> list[HostFinding]:
    findings: list[HostFinding] = []
    for port in snapshot.listening_ports:
        if not _is_public(port):
            continue
        if port.port in _HIGH_RISK_PORTS:
            service = _HIGH_RISK_PORTS[port.port]
            findings.append(
                HostFinding(
                    id=host_finding_id(
                        "port",
                        snapshot.identity.hostname,
                        port.protocol,
                        port.port,
                    ),
                    title=f"{service} is listening on a public interface",
                    category="exposure",
                    severity="high",
                    confidence=0.9,
                    affected_component=f"{port.protocol}/{port.port}",
                    evidence=[_port_evidence(port)],
                    remediation=(
                        f"Restrict {service} to a private interface, firewall it, "
                        "or disable the service if unused."
                    ),
                    source_tool="osquery",
                )
            )
        elif port.port == 22:
            findings.append(
                HostFinding(
                    id=host_finding_id(
                        "ssh_exposed",
                        snapshot.identity.hostname,
                        port.protocol,
                        port.port,
                    ),
                    title="SSH is reachable on a public interface",
                    category="exposure",
                    severity="medium",
                    confidence=0.85,
                    affected_component="ssh",
                    evidence=[_port_evidence(port)],
                    remediation=(
                        "Restrict SSH access to trusted networks and require "
                        "key-based authentication."
                    ),
                    source_tool="osquery",
                )
            )
    return findings


def _ssh_config_findings(snapshot: HostSnapshot) -> list[HostFinding]:
    ssh = snapshot.config.get("ssh")
    if not isinstance(ssh, dict):
        return []
    findings: list[HostFinding] = []
    normalized = {str(key).lower(): str(value).strip().lower() for key, value in ssh.items()}
    if normalized.get("permitrootlogin") in {"yes", "without-password", "prohibit-password"}:
        findings.append(
            HostFinding(
                id=host_finding_id("ssh", snapshot.identity.hostname, "permitrootlogin"),
                title="SSH root login is allowed",
                category="misconfiguration",
                severity="high",
                confidence=0.9,
                affected_component="sshd_config",
                control_refs=["CIS Ubuntu Linux: Disable SSH root login"],
                evidence=[
                    EvidenceItem(
                        source="osquery",
                        key="ssh.PermitRootLogin",
                        value=str(ssh.get("PermitRootLogin") or ssh.get("permitrootlogin")),
                    )
                ],
                remediation=(
                    "Set `PermitRootLogin no` and restart sshd after validating "
                    "administrative access."
                ),
                source_tool="osquery",
            )
        )
    if normalized.get("passwordauthentication") == "yes":
        findings.append(
            HostFinding(
                id=host_finding_id("ssh", snapshot.identity.hostname, "passwordauthentication"),
                title="SSH password authentication is enabled",
                category="misconfiguration",
                severity="medium",
                confidence=0.9,
                affected_component="sshd_config",
                control_refs=[
                    "CIS Ubuntu Linux: Disable SSH password authentication where possible"
                ],
                evidence=[
                    EvidenceItem(
                        source="osquery",
                        key="ssh.PasswordAuthentication",
                        value=str(
                            ssh.get("PasswordAuthentication")
                            or ssh.get("passwordauthentication")
                        ),
                    )
                ],
                remediation=(
                    "Prefer key-based authentication and set `PasswordAuthentication no` "
                    "where operationally feasible."
                ),
                source_tool="osquery",
            )
        )
    return findings


def _privileged_user_findings(snapshot: HostSnapshot) -> list[HostFinding]:
    findings: list[HostFinding] = []
    for user in snapshot.users:
        groups = {group.lower() for group in user.groups}
        if user.username == "root":
            continue
        if user.uid == 0 or groups & _ADMIN_GROUPS:
            findings.append(
                HostFinding(
                    id=host_finding_id(
                        "privileged_user",
                        snapshot.identity.hostname,
                        user.username,
                    ),
                    title=f"Privileged local account present: {user.username}",
                    category="identity",
                    severity="medium",
                    confidence=0.8,
                    affected_component=user.username,
                    evidence=[
                        EvidenceItem(source="osquery", key="user", value=user.username),
                        EvidenceItem(
                            source="osquery",
                            key="groups",
                            value=", ".join(user.groups) or "unknown",
                        ),
                    ],
                    remediation=(
                        "Review whether this account still requires administrator "
                        "privileges."
                    ),
                    source_tool="osquery",
                )
            )
    return findings


def _missing_evidence_findings(snapshot: HostSnapshot) -> list[HostFinding]:
    findings: list[HostFinding] = []
    for field_name, description in _REQUIRED_EVIDENCE.items():
        value = getattr(snapshot, field_name)
        if value:
            continue
        findings.append(
            HostFinding(
                id=host_finding_id("missing_evidence", snapshot.identity.hostname, field_name),
                title=f"Missing host evidence: {field_name}",
                category="coverage",
                severity="informational",
                confidence=1.0,
                affected_component=field_name,
                evidence=[EvidenceItem(source="piranesi", key="missing", value=field_name)],
                remediation=description,
                source_tool="piranesi",
            )
        )
    if "trivy" not in snapshot.raw_evidence:
        findings.append(
            HostFinding(
                id=host_finding_id("missing_evidence", snapshot.identity.hostname, "trivy"),
                title="Missing Trivy vulnerability evidence",
                category="coverage",
                severity="low",
                confidence=1.0,
                affected_component="trivy",
                evidence=[EvidenceItem(source="piranesi", key="missing", value="trivy")],
                remediation=(
                    "Include Trivy JSON output in the raw bundle to enable package CVE "
                    "prioritization."
                ),
                source_tool="piranesi",
            )
        )
    return findings


def _llm_unavailable_finding(
    snapshot: HostSnapshot,
    *,
    reason: str = "LLM provider was not configured for host posture analysis.",
) -> HostFinding:
    return HostFinding(
        id=host_finding_id("llm_unavailable", snapshot.identity.hostname, reason),
        title="LLM host analysis was not completed",
        category="coverage",
        severity="informational",
        confidence=1.0,
        evidence=[EvidenceItem(source="piranesi", key="analysis", value="llm")],
        remediation=reason,
        source_tool="piranesi",
        analysis_mode="llm",
        rationale=reason,
    )


def _trivy_results(payload: object) -> list[dict[str, object]]:
    if isinstance(payload, dict) and isinstance(payload.get("Results"), list):
        return [item for item in payload["Results"] if isinstance(item, dict)]
    if isinstance(payload, list):
        return [item for item in payload if isinstance(item, dict)]
    return []


def _is_public(port: ListeningPort) -> bool:
    address = port.address.strip().lower()
    return address in _PUBLIC_BIND_ADDRESSES or not address.startswith(("127.", "::1", "localhost"))


def _port_evidence(port: ListeningPort) -> EvidenceItem:
    process = f" process={port.process}" if port.process else ""
    return EvidenceItem(
        source="osquery",
        key="listening_port",
        value=f"{port.protocol}/{port.address}:{port.port}{process}",
    )


def _normalize_severity(value: object) -> Severity:
    normalized = str(value or "").strip().lower()
    if normalized in _SEVERITY_RANK:
        return cast(Severity, normalized)
    if normalized == "moderate":
        return "medium"
    return "medium"


def _rank_findings(findings: list[HostFinding]) -> list[HostFinding]:
    return sorted(
        findings,
        key=lambda item: (_SEVERITY_RANK[item.severity], item.confidence, item.title),
        reverse=True,
    )


def _dedupe_findings(findings: list[HostFinding]) -> list[HostFinding]:
    deduped: dict[str, HostFinding] = {}
    for finding in findings:
        existing = deduped.get(finding.id)
        if existing is None or _SEVERITY_RANK[finding.severity] > _SEVERITY_RANK[existing.severity]:
            deduped[finding.id] = finding
    return list(deduped.values())


def _posture_score(findings: list[HostFinding]) -> int:
    penalty = 0
    for finding in findings:
        if finding.category == "coverage":
            penalty += 2 if finding.severity == "low" else 1
            continue
        penalty += _SEVERITY_PENALTIES[finding.severity]
    return max(0, 100 - penalty)


def _summary(findings: list[HostFinding]) -> dict[str, object]:
    by_severity = Counter(finding.severity for finding in findings)
    by_category = Counter(finding.category for finding in findings)
    return {
        "findings_total": len(findings),
        "by_severity": dict(sorted(by_severity.items())),
        "by_category": dict(sorted(by_category.items())),
    }


def _evidence_inventory(snapshot: HostSnapshot) -> dict[str, int]:
    return {
        "packages": len(snapshot.packages),
        "listening_ports": len(snapshot.listening_ports),
        "processes": len(snapshot.processes),
        "services": len(snapshot.services),
        "users": len(snapshot.users),
        "config_sections": len(snapshot.config),
        "raw_tools": len(snapshot.raw_evidence),
    }


def _evidence_by_key(snapshot: HostSnapshot) -> dict[str, EvidenceItem]:
    evidence: dict[str, EvidenceItem] = {}
    for port in snapshot.listening_ports:
        key = f"port:{port.protocol}:{port.port}"
        evidence[key] = _port_evidence(port)
    for package in snapshot.packages:
        key = f"package:{package.name}"
        evidence[key] = EvidenceItem(
            source=package.source,
            key="package",
            value=f"{package.name}={package.version}",
        )
    for user in snapshot.users:
        key = f"user:{user.username}"
        evidence[key] = EvidenceItem(source="osquery", key="user", value=user.username)
    return evidence


def _llm_prompt(snapshot: HostSnapshot, findings: list[HostFinding]) -> str:
    evidence_keys = sorted(_evidence_by_key(snapshot))
    deterministic = [
        {
            "title": finding.title,
            "severity": finding.severity,
            "category": finding.category,
            "component": finding.affected_component,
        }
        for finding in findings[:25]
    ]
    payload = {
        "host": snapshot.identity.model_dump(mode="json"),
        "os": snapshot.os.model_dump(mode="json"),
        "kernel": snapshot.kernel,
        "evidence_inventory": _evidence_inventory(snapshot),
        "available_evidence_keys": evidence_keys[:200],
        "deterministic_findings": deterministic,
    }
    schema = {
        "findings": [
            {
                "title": "string",
                "category": (
                    "vulnerability|exposure|misconfiguration|identity|coverage|"
                    "compound-risk"
                ),
                "severity": "informational|low|medium|high|critical",
                "confidence": 0.0,
                "affected_component": "string or null",
                "evidence_keys": ["keys from available_evidence_keys"],
                "remediation": "string",
                "rationale": "string",
            }
        ]
    }
    return (
        "Analyze this Linux host posture snapshot. Return JSON only, matching this shape:\n"
        f"{json.dumps(schema, indent=2)}\n\n"
        "Only use evidence_keys listed in the input. Host evidence:\n"
        f"{json.dumps(payload, indent=2, sort_keys=True)}"
    )


def _string(value: object) -> str | None:
    if value is None:
        return None
    rendered = str(value).strip()
    return rendered or None
