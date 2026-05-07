from __future__ import annotations

import json
from collections import Counter
from datetime import UTC, datetime
from typing import Literal, cast

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from piranesi.host.models import (
    AnalysisMode,
    CollectionCapabilityHealth,
    CollectionHealth,
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
}
_CAPABILITY_REMEDIATION = {
    "osquery": "Install osquery and rerun `piranesi collect`.",
    "trivy": "Install Trivy or run collection with `--no-trivy` when CVE evidence is not needed.",
    "firewall": "Install or permit at least one firewall helper: ufw, iptables, or nft.",
    "apt_updates": "Ensure `apt list --upgradable` can run for Debian/Ubuntu patch evidence.",
    "sshd_config": "Ensure `sshd -T` or osquery sshd_config evidence can be collected.",
    "admin_groups": "Ensure `getent group` can run for sudo/admin/wheel group evidence.",
    "sysctl": "Ensure `sysctl -n` can run for kernel hardening evidence.",
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
    findings.extend(_firewall_findings(snapshot))
    findings.extend(_pending_security_update_findings(snapshot))
    findings.extend(_unattended_upgrade_findings(snapshot))
    findings.extend(_sysctl_findings(snapshot))
    findings.extend(_privileged_user_findings(snapshot))
    findings.extend(_missing_evidence_findings(snapshot))
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
        optional[name] = _capability_health(
            name=name,
            commands=[
                command
                for command in commands
                if _command_name(command) in command_names
                or (name == "trivy" and _command_tool(command) == "trivy")
            ],
            required=False,
            alternatives=name in {"firewall", "sshd_config"},
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
    if normalized.get("permitemptypasswords") == "yes":
        findings.append(
            HostFinding(
                id=host_finding_id("ssh", snapshot.identity.hostname, "permitemptypasswords"),
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


def _firewall_findings(snapshot: HostSnapshot) -> list[HostFinding]:
    public_ports = [port for port in snapshot.listening_ports if _is_public(port)]
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
                id=host_finding_id("sysctl", snapshot.identity.hostname, setting, value),
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
