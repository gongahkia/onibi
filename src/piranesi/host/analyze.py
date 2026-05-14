from __future__ import annotations

import ipaddress
import json
from collections import Counter
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, Literal, cast

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from piranesi.host.models import (
    AnalysisMode,
    CollectionCapabilityHealth,
    CollectionHealth,
    EvidenceItem,
    HostFinding,
    HostHypothesis,
    HostHypothesisReport,
    HostPostureReport,
    HostSnapshot,
    HypothesisType,
    ListeningPort,
    RedactionStatus,
    Severity,
    UserAccount,
    host_finding_id,
)
from piranesi.host.redaction import redact_host_llm_payload
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
    "packages": (
        "Package inventory is required for CVE and patch posture. "
        "Collect it with `piranesi collect` osquery query `deb_packages`."
    ),
    "listening_ports": (
        "Listening port inventory is required for exposure analysis. "
        "Collect it with `piranesi collect` osquery query `listening_ports`."
    ),
    "users": (
        "User inventory is required for privilege posture. "
        "Collect it with `piranesi collect` osquery query `users`."
    ),
}
_COLLECTION_STATUSES = ("ok", "missing", "failed", "timeout", "skipped")
_CAPABILITY_COMMANDS: dict[str, tuple[str, ...]] = {
    "trivy": ("filesystem_scan",),
    "firewall": ("ufw_status", "iptables_rules", "nft_ruleset"),
    "apt_updates": ("apt_upgradable",),
    "sshd_config": ("sshd_config", "sshd_effective_config"),
    "admin_groups": ("group_sudo", "group_admin", "group_wheel"),
    "sysctl": (
        "sysctl_net_ipv4_ip_forward",
        "sysctl_net_ipv6_conf_all_forwarding",
        "sysctl_kernel_unprivileged_bpf_disabled",
        "sysctl_kernel_kptr_restrict",
    ),
    "auth_evidence": (
        "who_sessions",
        "last_logins",
        "lastb_failures",
        "journalctl_sshd_auth_summary",
    ),
}
_CAPABILITY_REMEDIATION = {
    "osquery": "Install osquery and rerun `piranesi collect`.",
    "trivy": "Install Trivy or run collection with `--no-trivy` when CVE evidence is not needed.",
    "firewall": "Install or permit at least one firewall helper: ufw, iptables, or nft.",
    "apt_updates": "Ensure `apt list --upgradable` can run for Debian/Ubuntu patch evidence.",
    "sshd_config": "Ensure `sshd -T` or osquery sshd_config evidence can be collected.",
    "admin_groups": "Ensure `getent group` can run for sudo/admin/wheel group evidence.",
    "sysctl": "Ensure `sysctl -n` can run for kernel hardening evidence.",
    "lynis": "Install Lynis and rerun `piranesi collect --lynis` for hardening baseline evidence.",
    "openscap": "Install OpenSCAP and rerun `piranesi collect --openscap` for compliance baseline evidence.",
    "auth_evidence": "Ensure `who`, `last`, `lastb`, and journalctl are available for auth evidence.",
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


class _LlmHostHypothesis(BaseModel):
    model_config = ConfigDict(extra="forbid")

    title: str
    hypothesis_type: HypothesisType
    confidence: float = Field(ge=0.0, le=1.0)
    severity_if_true: Severity
    supporting_evidence_keys: list[str] = Field(min_length=1)
    missing_evidence: list[str] = Field(min_length=1)
    reasoning_summary: str
    suggested_followup_probes: list[str] = Field(default_factory=list)
    analyst_questions: list[str] = Field(default_factory=list)


class _LlmHostHypothesisAnalysis(BaseModel):
    model_config = ConfigDict(extra="forbid")

    hypotheses: list[_LlmHostHypothesis] = Field(default_factory=list)


@dataclass(frozen=True, slots=True)
class _HostLlmFindingResult:
    findings: list[HostFinding]
    redaction_status: RedactionStatus


@dataclass(frozen=True, slots=True)
class _HostLlmHypothesisResult:
    hypotheses: list[HostHypothesis]
    redaction_status: RedactionStatus


@dataclass(frozen=True, slots=True)
class _HostLlmPrompt:
    prompt: str
    redaction_status: RedactionStatus
    evidence_key_map: dict[str, str]


def analyze_snapshot(
    snapshot: HostSnapshot,
    *,
    analysis: AnalysisSelection = "deterministic",
    provider: LLMProvider | None = None,
    treat_private_as_public: bool = False,
) -> HostPostureReport:
    deterministic = deterministic_findings(
        snapshot,
        treat_private_as_public=treat_private_as_public,
    )
    findings = list(deterministic)
    modes: list[AnalysisMode] = ["deterministic"]
    llm_redaction: RedactionStatus | None = None
    if analysis == "llm":
        modes = ["llm"]
        if provider:
            llm_result = _llm_findings_with_redaction(
                snapshot,
                findings=deterministic,
                provider=provider,
            )
            findings = llm_result.findings
            llm_redaction = llm_result.redaction_status
        else:
            findings = []
            llm_redaction = _llm_redaction_not_applied()
        if not findings:
            findings.append(_llm_unavailable_finding(snapshot))
    elif analysis == "both":
        modes = ["deterministic", "llm"]
        if provider is not None:
            llm_result = _llm_findings_with_redaction(
                snapshot,
                findings=deterministic,
                provider=provider,
            )
            findings.extend(llm_result.findings)
            llm_redaction = llm_result.redaction_status
        else:
            llm_redaction = _llm_redaction_not_applied()
            findings.append(_llm_unavailable_finding(snapshot))
    ranked = _rank_findings(_dedupe_findings(findings))
    evidence_inventory = _evidence_inventory(snapshot)
    collection_health = collection_health_from_snapshot(snapshot)
    return HostPostureReport(
        target=snapshot.identity.hostname,
        generated_at=datetime.now(UTC).isoformat(),
        analysis_modes=modes,
        posture_score=_posture_score(ranked),
        summary=_summary(ranked),
        host_metadata=_host_metadata(snapshot, evidence_inventory),
        top_actions=_top_actions(ranked),
        findings=ranked,
        evidence_inventory=evidence_inventory,
        collection_health=collection_health,
        llm_redaction=llm_redaction,
        known_limitations=[
            "Phase 1 supports Debian/Ubuntu-oriented host evidence only.",
            "Raw bundle ingestion is first-class for osquery and Trivy JSON outputs.",
            "LLM analysis is advisory and must remain tied to explicit snapshot evidence.",
        ],
        snapshot=snapshot,
    )


def deterministic_findings(
    snapshot: HostSnapshot,
    *,
    treat_private_as_public: bool = False,
) -> list[HostFinding]:
    findings: list[HostFinding] = []
    findings.extend(_trivy_vulnerability_findings(snapshot))
    findings.extend(
        _exposed_port_findings(
            snapshot,
            treat_private_as_public=treat_private_as_public,
        )
    )
    findings.extend(_ssh_config_findings(snapshot))
    findings.extend(
        _firewall_findings(
            snapshot,
            treat_private_as_public=treat_private_as_public,
        )
    )
    findings.extend(_pending_security_update_findings(snapshot))
    findings.extend(_unattended_upgrade_findings(snapshot))
    findings.extend(_sysctl_findings(snapshot))
    findings.extend(_privileged_user_findings(snapshot))
    findings.extend(_missing_evidence_findings(snapshot))
    findings.extend(_baseline_check_findings(snapshot))
    findings.extend(_auth_evidence_findings(snapshot))
    return findings


def collection_health_from_snapshot(snapshot: HostSnapshot) -> CollectionHealth | None:
    manifest = _collection_manifest(snapshot)
    if manifest is None:
        return None
    commands = _manifest_commands(manifest)
    status_counts = Counter(_command_status(command) for command in commands)
    required = {
        "osquery": _capability_health(
            name="osquery",
            commands=[
                command
                for command in commands
                if _command_tool(command) == "osquery" and _command_name(command) != "version"
            ],
            required=True,
            alternatives=False,
        )
    }
    optional: dict[str, CollectionCapabilityHealth] = {}
    for name, command_names in _CAPABILITY_COMMANDS.items():
        capability_commands = [
            command
            for command in commands
            if _command_name(command) in command_names
            or (name == "trivy" and _command_tool(command) == "trivy")
        ]
        if (
            name == "auth_evidence"
            and not capability_commands
            and not snapshot.login_sessions
            and not snapshot.auth_event_summaries
        ):
            continue
        optional[name] = _capability_health(
            name=name,
            commands=capability_commands,
            required=False,
            alternatives=name in {"firewall", "sshd_config"},
        )
    for baseline_tool in ("lynis", "openscap"):
        tool_commands = [
            command for command in commands if _command_tool(command) == baseline_tool
        ]
        if tool_commands or _manifest_has_tool(manifest, baseline_tool):
            optional[baseline_tool] = _capability_health(
                name=baseline_tool,
                commands=tool_commands,
                required=False,
                alternatives=False,
            )
        elif baseline_tool in (snapshot.raw_evidence if snapshot else {}):
            optional[baseline_tool] = CollectionCapabilityHealth(
                status="ok",
                required=False,
                message="baseline evidence is available from raw bundle",
            )
    warnings = [
        f"{name}: {health.message}"
        for name, health in optional.items()
        if health.status == "warn"
    ]
    if required["osquery"].status != "ok":
        warnings.insert(0, f"osquery: {required['osquery'].message}")
    return CollectionHealth(
        manifest_present=True,
        status_counts={status: status_counts.get(status, 0) for status in _COLLECTION_STATUSES},
        required=required,
        optional=optional,
        warnings=warnings,
    )


def llm_findings(
    snapshot: HostSnapshot,
    *,
    findings: list[HostFinding],
    provider: LLMProvider,
) -> list[HostFinding]:
    return _llm_findings_with_redaction(
        snapshot,
        findings=findings,
        provider=provider,
    ).findings


def build_host_hypothesis_report(
    snapshot: HostSnapshot,
    *,
    provider: LLMProvider | None = None,
) -> HostHypothesisReport:
    deterministic = deterministic_hypotheses(snapshot)
    hypotheses = list(deterministic)
    analysis_modes: list[AnalysisMode] = ["deterministic"]
    llm_redaction: RedactionStatus | None = None
    if provider is not None:
        analysis_modes.append("llm")
        llm_result = _llm_hypotheses_with_redaction(
            snapshot,
            deterministic=deterministic,
            provider=provider,
        )
        hypotheses.extend(llm_result.hypotheses)
        llm_redaction = llm_result.redaction_status
    return HostHypothesisReport(
        target=snapshot.identity.hostname,
        generated_at=datetime.now(UTC).isoformat(),
        analysis_modes=analysis_modes,
        hypotheses=_rank_hypotheses(_dedupe_hypotheses(hypotheses)),
        llm_redaction=llm_redaction,
    )


def deterministic_hypotheses(snapshot: HostSnapshot) -> list[HostHypothesis]:
    hypotheses: list[HostHypothesis] = []
    hypotheses.extend(_public_ssh_auth_gap_hypotheses(snapshot))
    hypotheses.extend(_public_database_evidence_gap_hypotheses(snapshot))
    hypotheses.extend(_package_cve_ambiguity_hypotheses(snapshot))
    hypotheses.extend(_kernel_hardening_patch_gap_hypotheses(snapshot))
    return hypotheses


def llm_hypotheses(
    snapshot: HostSnapshot,
    *,
    deterministic: list[HostHypothesis],
    provider: LLMProvider,
) -> list[HostHypothesis]:
    return _llm_hypotheses_with_redaction(
        snapshot,
        deterministic=deterministic,
        provider=provider,
    ).hypotheses


def _llm_findings_with_redaction(
    snapshot: HostSnapshot,
    *,
    findings: list[HostFinding],
    provider: LLMProvider,
) -> _HostLlmFindingResult:
    prompt = _redacted_llm_prompt(snapshot, findings)
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
                {"role": "user", "content": prompt.prompt},
            ],
            max_tokens=1200,
        )
    except TokenBudgetExceededError as exc:
        return _HostLlmFindingResult(
            findings=[_llm_unavailable_finding(snapshot, reason=str(exc))],
            redaction_status=prompt.redaction_status,
        )
    except Exception as exc:
        return _HostLlmFindingResult(
            findings=[_llm_unavailable_finding(snapshot, reason=f"LLM analysis failed: {exc}")],
            redaction_status=prompt.redaction_status,
        )
    try:
        payload = _LlmHostAnalysis.model_validate_json(response.content)
    except (ValidationError, ValueError):
        return _HostLlmFindingResult(
            findings=[
                _llm_unavailable_finding(
                    snapshot,
                    reason="LLM returned an invalid host analysis payload",
                )
            ],
            redaction_status=prompt.redaction_status,
        )

    evidence_by_key = _evidence_by_key(snapshot)
    rendered: list[HostFinding] = []
    for item in payload.findings:
        evidence = [
            evidence_by_key[prompt.evidence_key_map.get(key, key)]
            for key in item.evidence_keys
            if prompt.evidence_key_map.get(key, key) in evidence_by_key
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
    return _HostLlmFindingResult(
        findings=rendered,
        redaction_status=prompt.redaction_status,
    )


def _public_ssh_auth_gap_hypotheses(snapshot: HostSnapshot) -> list[HostHypothesis]:
    public_ssh_ports = [
        port
        for port in snapshot.listening_ports
        if port.port == 22 and _is_public(port)
    ]
    if not public_ssh_ports or not _ssh_password_auth_enabled(snapshot):
        return []
    privileged = _privileged_nonroot_users(snapshot)
    if not privileged or snapshot.login_sessions or snapshot.auth_event_summaries:
        return []
    evidence = [_port_evidence(public_ssh_ports[0]), _ssh_password_auth_evidence(snapshot)]
    evidence.extend(_user_privilege_evidence(user) for user in privileged[:5])
    return [
        HostHypothesis(
            id=host_finding_id(
                "hypothesis",
                "public_ssh_password_privileged_auth_gap",
                snapshot.identity.hostname,
            ),
            title=(
                "Public SSH with password authentication may expose privileged accounts, "
                "but auth evidence is missing"
            ),
            hypothesis_type="compound_misconfiguration",
            confidence=0.64,
            severity_if_true="high",
            supporting_evidence=evidence,
            missing_evidence=[
                "Redacted SSH authentication success and failure summaries",
                "`last` and `lastb` login history for privileged accounts",
                "Effective sshd access controls such as AllowUsers, AllowGroups, and MFA",
            ],
            reasoning_summary=(
                "The snapshot shows public SSH, password authentication, and privileged "
                "local accounts. There is no authentication evidence to confirm whether "
                "those accounts are being targeted or used."
            ),
            suggested_followup_probes=[
                "followup.ssh.last_logins",
                "followup.ssh.lastb_failures",
                "followup.ssh.auth_summary",
                "followup.ssh.sshd_effective_config",
            ],
            analyst_questions=[
                "Are privileged accounts allowed to authenticate over SSH?",
                "Is there compensating control such as source allowlisting or MFA?",
            ],
        )
    ]


def _public_database_evidence_gap_hypotheses(snapshot: HostSnapshot) -> list[HostHypothesis]:
    firewall = snapshot.config.get("firewall")
    if isinstance(firewall, dict):
        return []
    hypotheses: list[HostHypothesis] = []
    for port in snapshot.listening_ports:
        service = _database_service_name(port)
        if service is None or not _is_public(port) or not _service_config_unknown(snapshot, port):
            continue
        hypotheses.append(
            HostHypothesis(
                id=host_finding_id(
                    "hypothesis",
                    "public_database_missing_firewall_unknown_config",
                    snapshot.identity.hostname,
                    port.protocol,
                    port.port,
                    port.process,
                ),
                title=(
                    f"Public {service} exposure may depend on missing firewall "
                    "and service-configuration evidence"
                ),
                hypothesis_type="configuration_ambiguity",
                confidence=0.58,
                severity_if_true="high",
                supporting_evidence=[_port_evidence(port)],
                missing_evidence=[
                    "Host firewall rule inventory",
                    f"{service} effective bind/listen and authentication configuration",
                    "Cloud or upstream network ACL evidence",
                ],
                reasoning_summary=(
                    f"{service} appears reachable on a public interface, but the "
                    "snapshot lacks firewall and service-specific configuration evidence. "
                    "The report should not assume whether access is actually restricted."
                ),
                suggested_followup_probes=[
                    "followup.firewall.ufw_status",
                    "followup.firewall.iptables_rules",
                    _service_probe_id(service, "process_detail"),
                    _service_probe_id(service, "service_unit"),
                ],
                analyst_questions=[
                    f"Is {service} intentionally reachable from untrusted networks?",
                    "Where is source restriction enforced if the host firewall evidence is absent?",
                ],
            )
        )
    return hypotheses


def _package_cve_ambiguity_hypotheses(snapshot: HostSnapshot) -> list[HostHypothesis]:
    public_ports = [
        port
        for port in snapshot.listening_ports
        if _is_public(port)
    ]
    if not public_ports:
        return []
    hypotheses: list[HostHypothesis] = []
    for vuln in _trivy_vulnerabilities(snapshot):
        vuln_id = _string(vuln.get("VulnerabilityID"))
        pkg_name = _string(vuln.get("PkgName"))
        if not vuln_id or not pkg_name:
            continue
        installed = _string(vuln.get("InstalledVersion")) or "unknown"
        fixed = _string(vuln.get("FixedVersion"))
        severity = _normalize_severity(vuln.get("Severity"))
        evidence = [
            EvidenceItem(source="trivy", key="vulnerability", value=vuln_id),
            EvidenceItem(source="trivy", key="package", value=pkg_name),
            EvidenceItem(source="trivy", key="installed_version", value=installed),
        ]
        if fixed:
            evidence.append(EvidenceItem(source="trivy", key="fixed_version", value=fixed))
        evidence.extend(_port_evidence(port) for port in public_ports[:3])
        hypotheses.append(
            HostHypothesis(
                id=host_finding_id(
                    "hypothesis",
                    "package_cve_service_ambiguity",
                    snapshot.identity.hostname,
                    pkg_name,
                    vuln_id,
                ),
                title=(
                    f"{vuln_id} in {pkg_name} may matter more if exposed services "
                    "load the affected package"
                ),
                hypothesis_type="dependency_risk",
                confidence=0.46,
                severity_if_true=severity,
                supporting_evidence=evidence,
                missing_evidence=[
                    "Process-to-package or loaded-library linkage for public services",
                    "Service restart state after package updates",
                    "Whether the vulnerable code path is reachable in the observed workload",
                ],
                reasoning_summary=(
                    f"Trivy reports {vuln_id} for {pkg_name}, and public services are "
                    "present. The snapshot does not prove which service, if any, loads "
                    "or exposes the affected package, so this remains a dependency-risk "
                    "hypothesis rather than an additional finding."
                ),
                suggested_followup_probes=[
                    "followup.process.open_files",
                    "followup.process.loaded_libraries",
                    "followup.package.reverse_dependencies",
                    "followup.service.restart_status",
                ],
                analyst_questions=[
                    f"Which running services load {pkg_name} or link against it?",
                    "Has the service been restarted since the fixed package became available?",
                ],
            )
        )
    return hypotheses


def _kernel_hardening_patch_gap_hypotheses(snapshot: HostSnapshot) -> list[HostHypothesis]:
    public_ports = [
        port
        for port in snapshot.listening_ports
        if _is_public(port)
    ]
    if not public_ports or isinstance(snapshot.config.get("updates"), dict):
        return []
    weak_sysctls = _sysctl_findings(snapshot)
    if not weak_sysctls:
        return []
    evidence: list[EvidenceItem] = []
    evidence.extend(_port_evidence(port) for port in public_ports[:3])
    for finding in weak_sysctls[:4]:
        evidence.extend(finding.evidence)
    return [
        HostHypothesis(
            id=host_finding_id(
                "hypothesis",
                "weak_kernel_hardening_public_services_patch_gap",
                snapshot.identity.hostname,
            ),
            title=(
                "Weak kernel hardening could increase exposure impact when public "
                "services and patch evidence are present only partially"
            ),
            hypothesis_type="novel_attack_path",
            confidence=0.5,
            severity_if_true="medium",
            supporting_evidence=evidence,
            missing_evidence=[
                "Current kernel and package patch status",
                "Kernel CVE scan evidence",
                "Rationale for weak sysctl values on this workload",
            ],
            reasoning_summary=(
                "The snapshot shows public services and weak kernel hardening settings, "
                "but lacks patch evidence. This does not confirm exploitation risk; it "
                "identifies evidence to collect before assessing chained impact."
            ),
            suggested_followup_probes=[
                "followup.updates.apt_upgradable",
                "followup.sysctl.kernel_hardening",
                "followup.vulnerability.trivy_filesystem",
            ],
            analyst_questions=[
                "Are the weak sysctl values required for this host role?",
                "Is there a documented kernel patch process for exposed hosts?",
            ],
        )
    ]


def _llm_hypotheses_with_redaction(
    snapshot: HostSnapshot,
    *,
    deterministic: list[HostHypothesis],
    provider: LLMProvider,
) -> _HostLlmHypothesisResult:
    prompt = _redacted_hypothesis_llm_prompt(snapshot, deterministic)
    try:
        response = provider.complete(
            stage="triage",
            messages=[
                {
                    "role": "system",
                    "content": (
                        "You propose evidence-bound Linux host security hypotheses. "
                        "Hypotheses are not confirmed findings. Do not provide exploit "
                        "payloads or active exploitation steps."
                    ),
                },
                {"role": "user", "content": prompt.prompt},
            ],
            max_tokens=1400,
        )
    except TokenBudgetExceededError:
        return _HostLlmHypothesisResult(
            hypotheses=[],
            redaction_status=prompt.redaction_status,
        )
    except Exception:
        return _HostLlmHypothesisResult(
            hypotheses=[],
            redaction_status=prompt.redaction_status,
        )
    try:
        payload = _LlmHostHypothesisAnalysis.model_validate_json(response.content)
    except (ValidationError, ValueError):
        return _HostLlmHypothesisResult(
            hypotheses=[],
            redaction_status=prompt.redaction_status,
        )

    evidence_by_key = _evidence_by_key(snapshot)
    rendered: list[HostHypothesis] = []
    for item in payload.hypotheses:
        evidence = [
            evidence_by_key[prompt.evidence_key_map.get(key, key)]
            for key in item.supporting_evidence_keys
            if prompt.evidence_key_map.get(key, key) in evidence_by_key
        ]
        if not evidence:
            continue
        rendered.append(
            HostHypothesis(
                id=host_finding_id(
                    "hypothesis",
                    "llm",
                    snapshot.identity.hostname,
                    item.title,
                ),
                title=item.title,
                hypothesis_type=item.hypothesis_type,
                confidence=item.confidence,
                severity_if_true=item.severity_if_true,
                supporting_evidence=evidence,
                missing_evidence=item.missing_evidence,
                reasoning_summary=item.reasoning_summary,
                suggested_followup_probes=item.suggested_followup_probes,
                analyst_questions=item.analyst_questions,
            )
        )
    return _HostLlmHypothesisResult(
        hypotheses=rendered,
        redaction_status=prompt.redaction_status,
    )


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
                        rule_id="host.cve.trivy",
                        instance_key=f"{pkg_name}:{vuln_id}",
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


def _exposed_port_findings(
    snapshot: HostSnapshot,
    *,
    treat_private_as_public: bool = False,
) -> list[HostFinding]:
    findings: list[HostFinding] = []
    for port in snapshot.listening_ports:
        if not _is_public(port, treat_private_as_public=treat_private_as_public):
            continue
        instance_key = _listener_instance_key(port)
        service = _high_risk_service(port)
        if service is not None:
            findings.append(
                HostFinding(
                    id=host_finding_id(
                        "host.listener.high_risk_service",
                        snapshot.identity.hostname,
                        instance_key,
                    ),
                    rule_id="host.listener.high_risk_service",
                    instance_key=instance_key,
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
                        "host.listener.ssh_public",
                        snapshot.identity.hostname,
                        instance_key,
                    ),
                    rule_id="host.listener.ssh_public",
                    instance_key=instance_key,
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
                rule_id="host.ssh.permit_root_login",
                instance_key="permitrootlogin",
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
                rule_id="host.ssh.password_authentication",
                instance_key="passwordauthentication",
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
    if normalized.get("permitemptypasswords") == "yes":
        findings.append(
            HostFinding(
                id=host_finding_id("ssh", snapshot.identity.hostname, "permitemptypasswords"),
                rule_id="host.ssh.permit_empty_passwords",
                instance_key="permitemptypasswords",
                title="SSH permits empty passwords",
                category="misconfiguration",
                severity="critical",
                confidence=0.95,
                affected_component="sshd_config",
                control_refs=["CIS Ubuntu Linux: Disable SSH empty passwords"],
                evidence=[
                    EvidenceItem(
                        source="osquery",
                        key="ssh.PermitEmptyPasswords",
                        value=str(
                            ssh.get("PermitEmptyPasswords")
                            or ssh.get("permitemptypasswords")
                        ),
                    )
                ],
                remediation=(
                    "Set `PermitEmptyPasswords no`, lock any account with an empty "
                    "password, and restart sshd."
                ),
                source_tool="osquery",
            )
        )
    return findings


def _firewall_findings(
    snapshot: HostSnapshot,
    *,
    treat_private_as_public: bool = False,
) -> list[HostFinding]:
    public_ports = [
        port
        for port in snapshot.listening_ports
        if _is_public(port, treat_private_as_public=treat_private_as_public)
    ]
    if not public_ports:
        return []
    firewall = snapshot.config.get("firewall")
    if not isinstance(firewall, dict) or firewall.get("active") is not False:
        return []
    highest: Severity = (
        "high" if any(port.port in _HIGH_RISK_PORTS for port in public_ports) else "medium"
    )
    services = ", ".join(_port_label(port) for port in public_ports[:8])
    return [
        HostFinding(
            id=host_finding_id("firewall", snapshot.identity.hostname, "inactive"),
            rule_id="host.firewall.inactive_public_services",
            instance_key="inactive",
            title="Firewall appears inactive while public services are exposed",
            category="exposure",
            severity=highest,
            confidence=0.85,
            affected_component="firewall",
            evidence=[
                EvidenceItem(
                    source="system",
                    key="firewall.active",
                    value=str(firewall.get("active")),
                ),
                EvidenceItem(source="osquery", key="public_listeners", value=services),
            ],
            remediation=(
                "Enable a host firewall and allow only required source networks for "
                f"public listeners: {services}."
            ),
            source_tool="piranesi",
        )
    ]


def _pending_security_update_findings(snapshot: HostSnapshot) -> list[HostFinding]:
    updates = snapshot.config.get("updates")
    if not isinstance(updates, dict):
        return []
    raw_updates = updates.get("upgradable")
    if not isinstance(raw_updates, list):
        return []
    security_updates = [
        update
        for update in raw_updates
        if isinstance(update, dict) and update.get("security") is True
    ]
    if not security_updates:
        return []
    packages = [
        str(update.get("package"))
        for update in security_updates
        if update.get("package") is not None
    ]
    return [
        HostFinding(
            id=host_finding_id("updates", snapshot.identity.hostname, "security"),
            rule_id="host.updates.security_pending",
            instance_key="security",
            title="Security package updates are pending",
            category="patching",
            severity="high",
            confidence=0.9,
            affected_component="apt",
            evidence=[
                EvidenceItem(
                    source="system",
                    key="apt.security_updates",
                    value=", ".join(packages[:12]),
                ),
                EvidenceItem(
                    source="system",
                    key="apt.security_update_count",
                    value=str(len(security_updates)),
                ),
            ],
            remediation=(
                "Apply pending security updates with the approved Debian/Ubuntu "
                "patch workflow and reboot if kernel or core libraries changed."
            ),
            source_tool="piranesi",
        )
    ]


def _unattended_upgrade_findings(snapshot: HostSnapshot) -> list[HostFinding]:
    updates = snapshot.config.get("updates")
    if not isinstance(updates, dict) or updates.get("source") != "apt_upgradable":
        return []
    if not snapshot.packages:
        return []
    installed_packages = {package.name for package in snapshot.packages}
    if "unattended-upgrades" in installed_packages:
        return []
    return [
        HostFinding(
            id=host_finding_id("updates", snapshot.identity.hostname, "unattended-upgrades"),
            rule_id="host.updates.unattended_upgrades_missing",
            instance_key="unattended-upgrades",
            title="Automatic security updates are not installed",
            category="patching",
            severity="medium",
            confidence=0.8,
            affected_component="unattended-upgrades",
            evidence=[
                EvidenceItem(
                    source="osquery",
                    key="package_inventory",
                    value="unattended-upgrades absent",
                ),
                EvidenceItem(
                    source="system",
                    key="apt.update_evidence",
                    value=str(updates.get("source")),
                ),
            ],
            remediation=(
                "Install and configure `unattended-upgrades` or document an equivalent "
                "automated security patch process for this Debian/Ubuntu host."
            ),
            source_tool="piranesi",
            rationale=(
                "Package inventory and apt update evidence were present, but the "
                "unattended-upgrades package was not installed."
            ),
        )
    ]


def _sysctl_findings(snapshot: HostSnapshot) -> list[HostFinding]:
    sysctl = snapshot.config.get("sysctl")
    if not isinstance(sysctl, dict):
        return []
    values = sysctl.get("values")
    if not isinstance(values, dict):
        return []
    checks: tuple[tuple[str, str, str, Severity, str], ...] = (
        (
            "net.ipv4.ip_forward",
            "1",
            "IPv4 packet forwarding is enabled",
            "medium",
            "Disable IPv4 forwarding unless this VM is intentionally acting as a router.",
        ),
        (
            "net.ipv6.conf.all.forwarding",
            "1",
            "IPv6 packet forwarding is enabled",
            "medium",
            "Disable IPv6 forwarding unless this VM is intentionally acting as a router.",
        ),
        (
            "kernel.unprivileged_bpf_disabled",
            "0",
            "Unprivileged BPF is enabled",
            "medium",
            "Disable unprivileged BPF where compatible with workload requirements.",
        ),
        (
            "kernel.kptr_restrict",
            "0",
            "Kernel pointer exposure is unrestricted",
            "low",
            "Set `kernel.kptr_restrict` to `1` or stricter unless debugging requires it.",
        ),
    )
    findings: list[HostFinding] = []
    for setting, weak_value, title, severity, remediation in checks:
        value = _string(values.get(setting))
        if value != weak_value:
            continue
        findings.append(
            HostFinding(
                id=host_finding_id("sysctl", snapshot.identity.hostname, setting),
                rule_id=f"host.sysctl.{setting}",
                instance_key=setting,
                title=title,
                category="misconfiguration",
                severity=severity,
                confidence=0.85,
                affected_component=setting,
                evidence=[EvidenceItem(source="system", key=f"sysctl.{setting}", value=value)],
                remediation=remediation,
                source_tool="piranesi",
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
                    rule_id="host.identity.privileged_user",
                    instance_key=user.username,
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
    manifest = _collection_manifest(snapshot)
    for field_name, description in _REQUIRED_EVIDENCE.items():
        value = getattr(snapshot, field_name)
        if value:
            continue
        command_name = _required_evidence_command(field_name)
        command = _manifest_command_by_name(manifest, command_name) if manifest else None
        status = _command_status(command) if command is not None else None
        remediation = description
        evidence = [EvidenceItem(source="piranesi", key="missing", value=field_name)]
        if status and status != "ok":
            remediation = (
                f"Collector command `{command_name}` ended with status `{status}`. "
                "Review collection health and rerun collection after fixing the helper."
            )
            evidence.append(
                EvidenceItem(
                    source="collection_manifest",
                    key=command_name,
                    value=status,
                )
            )
        findings.append(
            HostFinding(
                id=host_finding_id("missing_evidence", snapshot.identity.hostname, field_name),
                rule_id="host.coverage.missing_evidence",
                instance_key=field_name,
                title=f"Missing host evidence: {field_name}",
                category="coverage",
                severity="informational",
                confidence=1.0,
                affected_component=field_name,
                evidence=evidence,
                remediation=remediation,
                source_tool="piranesi",
            )
        )
    if "trivy" not in snapshot.raw_evidence and not _manifest_has_tool(manifest, "trivy"):
        findings.append(
            HostFinding(
                id=host_finding_id("missing_evidence", snapshot.identity.hostname, "trivy"),
                rule_id="host.coverage.missing_trivy",
                instance_key="trivy",
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


def _baseline_check_findings(snapshot: HostSnapshot) -> list[HostFinding]:
    findings: list[HostFinding] = []
    confidence_by_source = {"openscap": 0.9, "lynis": 0.75}
    for check in snapshot.baseline_checks:
        if check.result in {"pass", "not_applicable"}:
            continue
        severity = check.severity or ("medium" if check.result == "fail" else "low")
        findings.append(
            HostFinding(
                id=host_finding_id(
                    "baseline", snapshot.identity.hostname, check.source, check.check_id
                ),
                rule_id=f"host.baseline.{check.source}",
                instance_key=f"{check.source}:{check.check_id}",
                title=check.title,
                category="baseline",
                severity=severity,
                confidence=confidence_by_source.get(check.source, 0.7),
                affected_component=check.check_id,
                control_refs=list(check.control_refs),
                evidence=list(check.evidence),
                remediation=check.remediation or f"Review {check.source} check {check.check_id}.",
                source_tool=check.source,
            )
        )
    return findings


def _auth_evidence_findings(snapshot: HostSnapshot) -> list[HostFinding]:
    findings: list[HostFinding] = []

    ssh_failures = [e for e in snapshot.auth_event_summaries if e.event_type in {"ssh_failed_password", "login_failure"}]
    total_ssh_failures = sum(e.count for e in ssh_failures)

    is_public_ssh = any(
        port.port == 22 and _is_public(port)
        for port in snapshot.listening_ports
    )

    ssh_config = snapshot.config.get("ssh")
    password_auth_enabled = False
    if isinstance(ssh_config, dict):
        normalized = {str(k).lower(): str(v).strip().lower() for k, v in ssh_config.items()}
        password_auth_enabled = normalized.get("passwordauthentication") == "yes"

    if total_ssh_failures > 50:
        findings.append(
            HostFinding(
                id=host_finding_id("auth", snapshot.identity.hostname, "ssh_failed_password_spike"),
                rule_id="host.auth.ssh_failed_password_spike",
                instance_key="ssh_failed_password",
                title=f"High volume of failed SSH password attempts ({total_ssh_failures})",
                category="auth",
                severity="medium" if (is_public_ssh and password_auth_enabled) else "low",
                confidence=0.9,
                affected_component="sshd",
                evidence=[EvidenceItem(source="auth_logs", key="failed_ssh_attempts", value=str(total_ssh_failures))],
                remediation="Review SSH exposure, configure fail2ban, or disable password authentication.",
                source_tool="auth_logs",
            )
        )

    root_attempts = [e for e in snapshot.auth_event_summaries if e.event_type == "ssh_root_login"]
    if root_attempts:
        total_root = sum(e.count for e in root_attempts)
        findings.append(
            HostFinding(
                id=host_finding_id("auth", snapshot.identity.hostname, "ssh_root_login"),
                rule_id="host.auth.root_login_attempts",
                instance_key="ssh_root_login",
                title="SSH root login attempts detected",
                category="auth",
                severity="high" if is_public_ssh else "medium",
                confidence=0.9,
                affected_component="sshd",
                evidence=[EvidenceItem(source="auth_logs", key="root_login_attempts", value=str(total_root))],
                remediation="Ensure PermitRootLogin is disabled in sshd_config.",
                source_tool="auth_logs",
            )
        )

    admin_users = set()
    for user in snapshot.users:
        if user.username == "root":
            continue
        if any(g in _ADMIN_GROUPS for g in user.groups):
            admin_users.add(user.username)

    active_privileged = [s for s in snapshot.login_sessions if s.username in admin_users]
    if active_privileged:
        usernames = sorted({s.username for s in active_privileged})
        findings.append(
            HostFinding(
                id=host_finding_id("auth", snapshot.identity.hostname, "active_privileged_session"),
                rule_id="host.auth.active_privileged_session",
                instance_key="active_privileged_session",
                title=f"Active privileged sessions: {', '.join(usernames)}",
                category="auth",
                severity="informational",
                confidence=0.9,
                affected_component="session",
                evidence=[EvidenceItem(source="auth_logs", key="active_privileged_sessions", value=str(len(active_privileged)))],
                remediation="Review active sessions for unauthorized access.",
                source_tool="auth_logs",
            )
        )

    if is_public_ssh and password_auth_enabled and total_ssh_failures > 0 and admin_users:
        findings.append(
            HostFinding(
                id=host_finding_id("auth", snapshot.identity.hostname, "compound_ssh_brute_force"),
                rule_id="host.auth.compound_ssh_brute_force",
                instance_key="compound_ssh_brute_force",
                title="Public SSH is exposed to brute-force attacks against privileged accounts",
                category="compound-risk",
                severity="high",
                confidence=0.95,
                affected_component="sshd",
                evidence=[
                    EvidenceItem(source="compound", key="public_ssh", value="true"),
                    EvidenceItem(source="compound", key="password_auth", value="yes"),
                    EvidenceItem(source="compound", key="failed_attempts", value=str(total_ssh_failures)),
                    EvidenceItem(source="compound", key="privileged_accounts", value=str(len(admin_users))),
                ],
                remediation="Disable password authentication immediately and enforce key-based auth.",
                source_tool="compound",
            )
        )

    sudo_events = [e for e in snapshot.auth_event_summaries if e.event_type == "sudo_command"]
    if sudo_events:
        total_sudo = sum(e.count for e in sudo_events)
        findings.append(
            HostFinding(
                id=host_finding_id("auth", snapshot.identity.hostname, "sudo_activity"),
                rule_id="host.auth.sudo_activity_present",
                instance_key="sudo_activity",
                title=f"Sudo activity detected ({total_sudo} events)",
                category="auth",
                severity="informational",
                confidence=0.9,
                affected_component="sudo",
                evidence=[EvidenceItem(source="auth_logs", key="sudo_events", value=str(total_sudo))],
                remediation="Review sudo logs for suspicious commands.",
                source_tool="auth_logs",
            )
        )

    return findings


def _capability_health(
    *,
    name: str,
    commands: list[dict[str, object]],
    required: bool,
    alternatives: bool,
) -> CollectionCapabilityHealth:
    status_counts = Counter(_command_status(command) for command in commands)
    command_names = sorted({_command_name(command) for command in commands})
    status = _capability_status(
        status_counts=status_counts,
        command_count=len(commands),
        required=required,
        alternatives=alternatives,
    )
    message = _capability_message(name=name, status=status, commands=commands)
    return CollectionCapabilityHealth(
        status=status,
        required=required,
        commands_by_status={
            item: status_counts.get(item, 0)
            for item in _COLLECTION_STATUSES
            if status_counts.get(item, 0)
        },
        command_names=command_names,
        message=message,
        remediation=None if status == "ok" else _CAPABILITY_REMEDIATION.get(name),
    )


def _capability_status(
    *,
    status_counts: Counter[str],
    command_count: int,
    required: bool,
    alternatives: bool,
) -> Literal["ok", "warn", "fail", "skipped"]:
    ok_count = status_counts.get("ok", 0)
    skipped_count = status_counts.get("skipped", 0)
    bad_count = (
        status_counts.get("missing", 0)
        + status_counts.get("failed", 0)
        + status_counts.get("timeout", 0)
    )
    if command_count == 0:
        return "fail" if required else "warn"
    if required:
        if ok_count == 0:
            return "fail"
        return "warn" if bad_count else "ok"
    if skipped_count == command_count:
        return "skipped"
    if alternatives:
        return "ok" if ok_count else "warn"
    if ok_count and bad_count == 0:
        return "ok"
    return "warn"


def _capability_message(
    *,
    name: str,
    status: Literal["ok", "warn", "fail", "skipped"],
    commands: list[dict[str, object]],
) -> str:
    if status == "ok":
        return "collection evidence is available"
    if status == "skipped":
        return "collection was skipped"
    if not commands:
        return "no manifest commands were recorded for this capability"
    grouped = Counter(_command_status(command) for command in commands)
    details = ", ".join(
        f"{status_name}={grouped[status_name]}"
        for status_name in _COLLECTION_STATUSES
        if grouped.get(status_name, 0)
    )
    prefix = (
        "required collection is incomplete"
        if name == "osquery"
        else "optional evidence is incomplete"
    )
    return f"{prefix} ({details})"


def _collection_manifest(snapshot: HostSnapshot) -> dict[str, object] | None:
    raw = snapshot.raw_evidence.get("collection_manifest")
    return raw if isinstance(raw, dict) else None


def _manifest_commands(manifest: dict[str, object]) -> list[dict[str, object]]:
    raw_commands = manifest.get("commands")
    if not isinstance(raw_commands, list):
        return []
    return [command for command in raw_commands if isinstance(command, dict)]


def _manifest_command_by_name(
    manifest: dict[str, object] | None,
    name: str,
) -> dict[str, object] | None:
    if manifest is None:
        return None
    for command in _manifest_commands(manifest):
        if _command_name(command) == name:
            return command
    return None


def _manifest_has_tool(manifest: dict[str, object] | None, tool: str) -> bool:
    if manifest is None:
        return False
    return any(_command_tool(command) == tool for command in _manifest_commands(manifest))


def _command_tool(command: dict[str, object]) -> str:
    return str(command.get("tool") or "")


def _command_name(command: dict[str, object]) -> str:
    return str(command.get("name") or "")


def _command_status(command: dict[str, object]) -> str:
    status = str(command.get("status") or "")
    return status if status in _COLLECTION_STATUSES else "failed"


def _required_evidence_command(field_name: str) -> str:
    commands = {
        "packages": "deb_packages",
        "listening_ports": "listening_ports",
        "users": "users",
    }
    return commands[field_name]


def _llm_unavailable_finding(
    snapshot: HostSnapshot,
    *,
    reason: str = "LLM provider was not configured for host posture analysis.",
) -> HostFinding:
    return HostFinding(
        id=host_finding_id("llm_unavailable", snapshot.identity.hostname, reason),
        rule_id="host.coverage.llm_unavailable",
        instance_key="llm",
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


def _ssh_password_auth_enabled(snapshot: HostSnapshot) -> bool:
    ssh = snapshot.config.get("ssh")
    if not isinstance(ssh, dict):
        return False
    normalized = {str(key).lower(): str(value).strip().lower() for key, value in ssh.items()}
    return normalized.get("passwordauthentication") == "yes"


def _ssh_password_auth_evidence(snapshot: HostSnapshot) -> EvidenceItem:
    ssh = snapshot.config.get("ssh")
    value = "unknown"
    if isinstance(ssh, dict):
        value = str(ssh.get("PasswordAuthentication") or ssh.get("passwordauthentication") or value)
    return EvidenceItem(source="osquery", key="ssh.PasswordAuthentication", value=value)


def _privileged_nonroot_users(snapshot: HostSnapshot) -> list[UserAccount]:
    users: list[UserAccount] = []
    for user in snapshot.users:
        if user.username == "root":
            continue
        groups = {group.lower() for group in user.groups}
        if user.uid == 0 or groups & _ADMIN_GROUPS:
            users.append(user)
    return users


def _user_privilege_evidence(user: UserAccount) -> EvidenceItem:
    groups = ", ".join(user.groups) or "unknown"
    return EvidenceItem(
        source="osquery",
        key="privileged_user",
        value=f"{user.username} groups={groups}",
    )


def _database_service_name(port: ListeningPort) -> str | None:
    service = _high_risk_service(port)
    if service in {"MySQL", "PostgreSQL", "Redis", "Elasticsearch", "Memcached", "MongoDB"}:
        return service
    return None


def _service_config_unknown(snapshot: HostSnapshot, port: ListeningPort) -> bool:
    service = _database_service_name(port)
    if service is None:
        return True
    service_key = service.lower()
    process = (port.process or "").lower()
    service_names = {item.name.lower() for item in snapshot.services}
    has_service_state = any(
        token in name
        for name in service_names
        for token in _service_match_tokens(service_key, process)
    )
    config = snapshot.config.get(service_key)
    has_config = isinstance(config, dict)
    return not (has_service_state and has_config)


def _service_match_tokens(service_key: str, process: str) -> set[str]:
    tokens = {service_key}
    if process:
        tokens.add(process)
        tokens.add(process.replace("-server", ""))
    if service_key == "postgresql":
        tokens.add("postgres")
    if service_key == "mongodb":
        tokens.add("mongo")
    return {token for token in tokens if token}


def _service_probe_id(service: str, suffix: str) -> str:
    normalized = service.lower()
    if normalized == "postgresql":
        normalized = "postgres"
    return f"followup.{normalized}.{suffix}"


def _trivy_vulnerabilities(snapshot: HostSnapshot) -> list[dict[str, object]]:
    raw = snapshot.raw_evidence.get("trivy")
    payloads = raw if isinstance(raw, dict) else {}
    vulnerabilities: list[dict[str, object]] = []
    for payload in payloads.values():
        for result in _trivy_results(payload):
            raw_vulns = result.get("Vulnerabilities")
            if isinstance(raw_vulns, list):
                vulnerabilities.extend(
                    item for item in raw_vulns if isinstance(item, dict)
                )
    return vulnerabilities


def _rank_hypotheses(hypotheses: list[HostHypothesis]) -> list[HostHypothesis]:
    return sorted(
        hypotheses,
        key=lambda item: (_SEVERITY_RANK[item.severity_if_true], item.confidence, item.title),
        reverse=True,
    )


def _dedupe_hypotheses(hypotheses: list[HostHypothesis]) -> list[HostHypothesis]:
    deduped: dict[str, HostHypothesis] = {}
    for hypothesis in hypotheses:
        existing = deduped.get(hypothesis.id)
        if (
            existing is None
            or _SEVERITY_RANK[hypothesis.severity_if_true]
            > _SEVERITY_RANK[existing.severity_if_true]
        ):
            deduped[hypothesis.id] = hypothesis
    return list(deduped.values())


def _trivy_results(payload: object) -> list[dict[str, object]]:
    if isinstance(payload, dict) and isinstance(payload.get("Results"), list):
        return [item for item in payload["Results"] if isinstance(item, dict)]
    if isinstance(payload, list):
        return [item for item in payload if isinstance(item, dict)]
    return []


def _is_public(port: ListeningPort, *, treat_private_as_public: bool = False) -> bool:
    address = port.address.strip().lower()
    if address in _PUBLIC_BIND_ADDRESSES:
        return True
    if address == "localhost":
        return False
    try:
        ip = ipaddress.ip_address(address.split("%", 1)[0])
    except ValueError:
        return False
    if ip.is_loopback or ip.is_link_local or ip.is_multicast:
        return False
    if ip.is_unspecified:
        return False
    if ip.is_private:
        return treat_private_as_public
    return ip.is_global


def _listener_instance_key(port: ListeningPort) -> str:
    if port.process:
        return f"{port.protocol}:{port.process.lower()}"
    if port.pid is not None:
        return f"{port.protocol}:pid:{port.pid}"
    return f"{port.protocol}:{port.port}"


def _high_risk_service(port: ListeningPort) -> str | None:
    if port.port in _HIGH_RISK_PORTS:
        return _HIGH_RISK_PORTS[port.port]
    process = (port.process or "").lower()
    if "redis" in process:
        return "Redis"
    if "mysql" in process or "mariadb" in process:
        return "MySQL"
    if "postgres" in process:
        return "PostgreSQL"
    if "mongo" in process:
        return "MongoDB"
    if "memcached" in process:
        return "Memcached"
    if "elasticsearch" in process:
        return "Elasticsearch"
    return None


def _port_evidence(port: ListeningPort) -> EvidenceItem:
    process = f" process={port.process}" if port.process else ""
    return EvidenceItem(
        source="osquery",
        key="listening_port",
        value=f"{port.protocol}/{port.address}:{port.port}{process}",
    )


def _port_label(port: ListeningPort) -> str:
    process = f" ({port.process})" if port.process else ""
    return f"{port.protocol}/{port.address}:{port.port}{process}"


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
    firewall = snapshot.config.get("firewall")
    updates = snapshot.config.get("updates")
    sysctl = snapshot.config.get("sysctl")
    return {
        "packages": len(snapshot.packages),
        "network_interfaces": len(snapshot.network_interfaces),
        "ip_addresses": len(snapshot.identity.ip_addresses),
        "listening_ports": len(snapshot.listening_ports),
        "processes": len(snapshot.processes),
        "services": len(snapshot.services),
        "users": len(snapshot.users),
        "config_sections": len(snapshot.config),
        "firewall_evidence": 1 if isinstance(firewall, dict) else 0,
        "update_evidence": 1 if isinstance(updates, dict) else 0,
        "sysctl_evidence": 1 if isinstance(sysctl, dict) else 0,
        "raw_tools": len(snapshot.raw_evidence),
        "login_sessions": len(snapshot.login_sessions),
        "auth_event_summaries": len(snapshot.auth_event_summaries),
        "failed_ssh_attempts": sum(
            1 for e in snapshot.auth_event_summaries
            if e.event_type in {"ssh_failed_password", "ssh_invalid_user"}
        ),
    }


def _host_metadata(snapshot: HostSnapshot, inventory: dict[str, int]) -> dict[str, object]:
    tools = sorted(key for key, value in snapshot.tool_provenance.items() if value)
    raw_tools = sorted(snapshot.raw_evidence)
    evidence_complete = {
        "packages": inventory["packages"] > 0,
        "network": inventory["network_interfaces"] > 0 or inventory["ip_addresses"] > 0,
        "listening_ports": inventory["listening_ports"] > 0,
        "processes": inventory["processes"] > 0,
        "users": inventory["users"] > 0,
        "firewall": inventory["firewall_evidence"] > 0,
        "updates": inventory["update_evidence"] > 0,
        "sysctl": inventory["sysctl_evidence"] > 0,
        "trivy": "trivy" in snapshot.raw_evidence,
    }
    return {
        "hostname": snapshot.identity.hostname,
        "host_id": snapshot.identity.host_id,
        "os": {
            "name": snapshot.os.name,
            "version": snapshot.os.version,
            "id": snapshot.os.id,
            "version_id": snapshot.os.version_id,
            "pretty_name": snapshot.os.pretty_name,
        },
        "kernel": snapshot.kernel,
        "ip_addresses": snapshot.identity.ip_addresses,
        "tools": tools,
        "raw_tools": raw_tools,
        "evidence_completeness": evidence_complete,
        "active_sessions_count": len(snapshot.login_sessions),
        "auth_event_summary_count": len(snapshot.auth_event_summaries),
        "failed_ssh_attempt_count": sum(
            1 for e in snapshot.auth_event_summaries
            if e.event_type in {"ssh_failed_password", "ssh_invalid_user"}
        ),
    }


def _top_actions(findings: list[HostFinding]) -> list[dict[str, object]]:
    groups = [
        (
            "exposure",
            "Reduce externally reachable services first.",
            {"exposure", "compound-risk"},
        ),
        (
            "patching",
            "Apply security updates and fixed package versions.",
            {"patching", "vulnerability"},
        ),
        (
            "identity",
            "Review privileged local accounts and SSH authentication.",
            {"identity", "misconfiguration"},
        ),
        ("coverage", "Collect missing evidence to improve confidence.", {"coverage"}),
        (
            "baseline",
            "Review and remediate failed hardening and compliance baseline checks.",
            {"baseline", "compliance"},
        ),
        (
            "auth",
            "Review authentication evidence for brute-force or unauthorized access patterns.",
            {"auth", "compound-risk"},
        ),
    ]
    actions: list[dict[str, object]] = []
    for category, action, categories in groups:
        matching = [finding for finding in findings if finding.category in categories]
        if not matching:
            continue
        highest = matching[0].severity
        actions.append(
            {
                "category": category,
                "action": action,
                "severity": highest,
                "finding_ids": [finding.id for finding in matching[:5]],
                "finding_titles": [finding.title for finding in matching[:5]],
            }
        )
    return actions


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
    for service in snapshot.services:
        key = f"service:{service.name}"
        state = []
        if service.enabled is not None:
            state.append(f"enabled={service.enabled}")
        if service.running is not None:
            state.append(f"running={service.running}")
        evidence[key] = EvidenceItem(
            source=service.source,
            key="service",
            value=f"{service.name} {' '.join(state)}".strip(),
        )
    for process in snapshot.processes:
        key = f"process:{process.pid}:{process.name}"
        evidence[key] = EvidenceItem(
            source="osquery",
            key="process",
            value=f"{process.name} pid={process.pid}",
        )
    return evidence


def _llm_redaction_not_applied() -> RedactionStatus:
    return RedactionStatus(
        applied=False,
        redacted_value_count=0,
        categories={},
        mode="strict",
    )


def _redacted_llm_prompt(snapshot: HostSnapshot, findings: list[HostFinding]) -> _HostLlmPrompt:
    payload = _host_llm_payload(snapshot, findings)
    redacted = redact_host_llm_payload(payload)
    redacted_payload = cast(dict[str, Any], redacted.payload)
    raw_keys = payload["available_evidence_keys"]
    rendered_keys = redacted_payload.get("available_evidence_keys", [])
    evidence_key_map = {
        str(redacted_key): str(raw_key)
        for raw_key, redacted_key in zip(raw_keys, rendered_keys, strict=False)
    }
    return _HostLlmPrompt(
        prompt=_llm_prompt_from_payload(redacted_payload),
        redaction_status=redacted.status,
        evidence_key_map=evidence_key_map,
    )


def _redacted_hypothesis_llm_prompt(
    snapshot: HostSnapshot,
    deterministic: list[HostHypothesis],
) -> _HostLlmPrompt:
    payload = _host_hypothesis_llm_payload(snapshot, deterministic)
    redacted = redact_host_llm_payload(payload)
    redacted_payload = cast(dict[str, Any], redacted.payload)
    raw_keys = payload["available_evidence_keys"]
    rendered_keys = redacted_payload.get("available_evidence_keys", [])
    evidence_key_map = {
        str(redacted_key): str(raw_key)
        for raw_key, redacted_key in zip(raw_keys, rendered_keys, strict=False)
    }
    return _HostLlmPrompt(
        prompt=_hypothesis_llm_prompt_from_payload(redacted_payload),
        redaction_status=redacted.status,
        evidence_key_map=evidence_key_map,
    )


def _host_llm_payload(snapshot: HostSnapshot, findings: list[HostFinding]) -> dict[str, Any]:
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
    return {
        "host": snapshot.identity.model_dump(mode="json"),
        "os": snapshot.os.model_dump(mode="json"),
        "kernel": snapshot.kernel,
        "evidence_inventory": _evidence_inventory(snapshot),
        "available_evidence_keys": evidence_keys[:200],
        "packages": [
            package.model_dump(mode="json")
            for package in snapshot.packages[:100]
        ],
        "services": [
            service.model_dump(mode="json")
            for service in snapshot.services[:100]
        ],
        "listening_ports": [
            port.model_dump(mode="json")
            for port in snapshot.listening_ports[:100]
        ],
        "processes": [
            process.model_dump(mode="json")
            for process in snapshot.processes[:100]
        ],
        "users": [
            user.model_dump(mode="json")
            for user in snapshot.users[:100]
        ],
        "login_sessions": [
            session.model_dump(mode="json")
            for session in snapshot.login_sessions[:100]
        ],
        "auth_event_summaries": [
            event.model_dump(mode="json")
            for event in snapshot.auth_event_summaries[:100]
        ],
        "deterministic_findings": deterministic,
    }


def _host_hypothesis_llm_payload(
    snapshot: HostSnapshot,
    deterministic: list[HostHypothesis],
) -> dict[str, Any]:
    deterministic_findings_summary = [
        {
            "title": finding.title,
            "severity": finding.severity,
            "category": finding.category,
            "component": finding.affected_component,
        }
        for finding in deterministic_findings(snapshot)[:25]
    ]
    deterministic_hypotheses_summary = [
        {
            "title": hypothesis.title,
            "hypothesis_type": hypothesis.hypothesis_type,
            "confidence": hypothesis.confidence,
            "severity_if_true": hypothesis.severity_if_true,
            "missing_evidence": hypothesis.missing_evidence,
        }
        for hypothesis in deterministic[:25]
    ]
    payload = _host_llm_payload(snapshot, deterministic_findings(snapshot))
    payload["available_evidence_keys"] = sorted(_evidence_by_key(snapshot))[:250]
    payload["confirmed_findings"] = deterministic_findings_summary
    payload["deterministic_hypotheses"] = deterministic_hypotheses_summary
    payload["hypothesis_instructions"] = {
        "status": "hypotheses_are_not_confirmed_findings",
        "allowed_output": "follow-up probes and analyst questions only",
        "disallowed_output": "exploit payloads, exploit code, or confirmed claims without evidence",
    }
    return payload


def _llm_prompt_from_payload(payload: dict[str, Any]) -> str:
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


def _hypothesis_llm_prompt_from_payload(payload: dict[str, Any]) -> str:
    schema = {
        "hypotheses": [
            {
                "title": "string",
                "hypothesis_type": (
                    "compound_misconfiguration|novel_attack_path|dependency_risk|"
                    "configuration_ambiguity"
                ),
                "confidence": 0.0,
                "severity_if_true": "informational|low|medium|high|critical",
                "supporting_evidence_keys": ["keys from available_evidence_keys"],
                "missing_evidence": ["specific evidence required before confirmation"],
                "reasoning_summary": "concise summary, no hidden chain-of-thought",
                "suggested_followup_probes": ["safe probe IDs or questions, no payloads"],
                "analyst_questions": ["questions for an operator or analyst"],
            }
        ]
    }
    return (
        "Generate Linux host security hypotheses from this redacted evidence. Return JSON only, "
        "matching this shape exactly:\n"
        f"{json.dumps(schema, indent=2)}\n\n"
        "Rules:\n"
        "- Every hypothesis must cite available supporting_evidence_keys.\n"
        "- Every hypothesis must list missing_evidence required before confirmation.\n"
        "- Hypotheses are not confirmed findings and must not be written as "
        "confirmed vulnerabilities.\n"
        "- Do not claim a vulnerability is confirmed unless it is already in confirmed_findings.\n"
        "- Suggest safe follow-up probes or analyst questions only; do not "
        "provide exploit payloads.\n"
        "- Use concise reasoning_summary text only.\n\n"
        "Host evidence:\n"
        f"{json.dumps(payload, indent=2, sort_keys=True)}"
    )


def _string(value: object) -> str | None:
    if value is None:
        return None
    rendered = str(value).strip()
    return rendered or None
