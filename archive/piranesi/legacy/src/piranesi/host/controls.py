from __future__ import annotations

from collections import Counter, defaultdict

from piranesi.host.models import HostControlRef, HostFinding, Severity

_BROAD_CIS_RATIONALE = (
    "Broad CIS Ubuntu Linux family mapping only; no exact benchmark item is asserted."
)
_BROAD_NIST_CSF_RATIONALE = (
    "Broad NIST CSF 2.0 category mapping only; exact organizational profile control "
    "is not asserted."
)
_BROAD_NIST_800_53_RATIONALE = (
    "Broad NIST SP 800-53 Rev. 5 family mapping only; exact control selection depends "
    "on the system security plan."
)


HOST_CONTROL_NO_MAP_REASONS: dict[str, str] = {
    "host.coverage.llm_unavailable": (
        "LLM availability is an analysis-mode health signal rather than a host posture control."
    ),
}


def _cis(control_id: str, title: str, confidence: float) -> HostControlRef:
    return HostControlRef(
        framework="CIS Ubuntu Linux",
        version=None,
        control_id=control_id,
        title=title,
        mapping_confidence=confidence,
        rationale=_BROAD_CIS_RATIONALE,
    )


def _nist_csf(
    control_id: str,
    title: str,
    confidence: float,
    rationale_suffix: str,
) -> HostControlRef:
    return HostControlRef(
        framework="NIST CSF",
        version="2.0",
        control_id=control_id,
        title=title,
        mapping_confidence=confidence,
        rationale=f"{_BROAD_NIST_CSF_RATIONALE} Scope: {rationale_suffix}.",
    )


def _nist_800_53(
    control_id: str,
    title: str,
    confidence: float,
    rationale_suffix: str,
) -> HostControlRef:
    return HostControlRef(
        framework="NIST SP 800-53",
        version="Rev. 5",
        control_id=control_id,
        title=title,
        mapping_confidence=confidence,
        rationale=f"{_BROAD_NIST_800_53_RATIONALE} Scope: {rationale_suffix}.",
    )


HOST_CONTROL_MAPPINGS: dict[str, tuple[HostControlRef, ...]] = {
    "host.cve.trivy": (
        _nist_csf("ID.RA", "Risk Assessment", 0.62, "package vulnerability evidence"),
        _nist_csf("PR.PS", "Platform Security", 0.55, "platform vulnerability remediation"),
        _nist_800_53("RA family", "Risk Assessment", 0.5, "vulnerability identification"),
        _nist_800_53("SI family", "System and Information Integrity", 0.5, "flaw remediation"),
        _cis("vulnerability-management", "Vulnerability and patch management", 0.45),
    ),
    "host.listener.high_risk_service": (
        _nist_csf("PR.PS", "Platform Security", 0.62, "exposed service hardening"),
        _nist_csf("DE.CM", "Continuous Monitoring", 0.5, "network exposure visibility"),
        _nist_800_53("CM family", "Configuration Management", 0.52, "service exposure management"),
        _nist_800_53(
            "SC family", "System and Communications Protection", 0.5, "boundary protection family"
        ),
        _cis("network-service-hardening", "Network service hardening", 0.48),
    ),
    "host.listener.ssh_public": (
        _nist_csf(
            "PR.AA",
            "Identity Management, Authentication and Access Control",
            0.58,
            "remote administration exposure",
        ),
        _nist_csf("PR.PS", "Platform Security", 0.55, "platform service exposure"),
        _nist_800_53("AC family", "Access Control", 0.5, "remote access control family"),
        _cis("ssh-hardening", "SSH daemon hardening", 0.58),
    ),
    "host.ssh.permit_root_login": (
        _cis("ssh-root-login", "Ensure SSH root login is disabled", 0.74),
        _nist_csf(
            "PR.AA",
            "Identity Management, Authentication and Access Control",
            0.65,
            "privileged remote authentication",
        ),
        _nist_800_53("AC family", "Access Control", 0.58, "privileged access control family"),
        _nist_800_53(
            "IA family",
            "Identification and Authentication",
            0.56,
            "administrator authentication family",
        ),
    ),
    "host.ssh.password_authentication": (
        _cis("ssh-password-authentication", "Harden SSH password authentication", 0.68),
        _nist_csf(
            "PR.AA",
            "Identity Management, Authentication and Access Control",
            0.64,
            "remote authentication strength",
        ),
        _nist_800_53(
            "IA family",
            "Identification and Authentication",
            0.56,
            "authentication mechanism family",
        ),
    ),
    "host.ssh.permit_empty_passwords": (
        _cis("ssh-empty-passwords", "Ensure SSH empty passwords are disabled", 0.76),
        _nist_csf(
            "PR.AA",
            "Identity Management, Authentication and Access Control",
            0.68,
            "authentication credential strength",
        ),
        _nist_800_53(
            "IA family", "Identification and Authentication", 0.6, "authenticator management family"
        ),
    ),
    "host.firewall.inactive_public_services": (
        _nist_csf("PR.PS", "Platform Security", 0.66, "host firewall and platform hardening"),
        _nist_csf("DE.CM", "Continuous Monitoring", 0.54, "network exposure monitoring"),
        _nist_800_53(
            "SC family", "System and Communications Protection", 0.58, "boundary protection family"
        ),
        _cis("host-firewall", "Host firewall and network filtering", 0.58),
    ),
    "host.updates.security_pending": (
        _nist_csf("PR.PS", "Platform Security", 0.62, "platform update management"),
        _nist_800_53(
            "SI family", "System and Information Integrity", 0.56, "flaw remediation family"
        ),
        _cis("patch-management", "Security update management", 0.52),
    ),
    "host.updates.unattended_upgrades_missing": (
        _nist_csf("PR.PS", "Platform Security", 0.58, "platform update process"),
        _nist_800_53(
            "SI family", "System and Information Integrity", 0.52, "flaw remediation process family"
        ),
        _cis("patch-management", "Security update management", 0.5),
    ),
    "host.sysctl.net.ipv4.ip_forward": (
        _nist_csf("PR.PS", "Platform Security", 0.58, "kernel network hardening"),
        _nist_800_53("CM family", "Configuration Management", 0.5, "secure configuration family"),
        _cis("kernel-network-hardening", "Kernel network hardening", 0.52),
    ),
    "host.sysctl.net.ipv6.conf.all.forwarding": (
        _nist_csf("PR.PS", "Platform Security", 0.58, "kernel network hardening"),
        _nist_800_53("CM family", "Configuration Management", 0.5, "secure configuration family"),
        _cis("kernel-network-hardening", "Kernel network hardening", 0.52),
    ),
    "host.sysctl.kernel.unprivileged_bpf_disabled": (
        _nist_csf("PR.PS", "Platform Security", 0.56, "kernel hardening"),
        _nist_800_53("CM family", "Configuration Management", 0.48, "secure configuration family"),
        _cis("kernel-hardening", "Kernel hardening", 0.5),
    ),
    "host.sysctl.kernel.kptr_restrict": (
        _nist_csf("PR.PS", "Platform Security", 0.54, "kernel information exposure hardening"),
        _nist_800_53("CM family", "Configuration Management", 0.46, "secure configuration family"),
        _cis("kernel-hardening", "Kernel hardening", 0.48),
    ),
    "host.identity.privileged_user": (
        _nist_csf(
            "PR.AA",
            "Identity Management, Authentication and Access Control",
            0.64,
            "privileged account review",
        ),
        _nist_800_53("AC family", "Access Control", 0.58, "least privilege family"),
        _cis("privileged-account-management", "Privileged local account management", 0.5),
    ),
    "host.coverage.missing_evidence": (
        _nist_csf("ID.AM", "Asset Management", 0.5, "host inventory visibility"),
        _nist_csf("GV.OC", "Organizational Context", 0.42, "evidence governance visibility"),
        _nist_800_53(
            "CA family",
            "Assessment, Authorization, and Monitoring",
            0.42,
            "assessment evidence family",
        ),
    ),
    "host.coverage.missing_trivy": (
        _nist_csf("ID.RA", "Risk Assessment", 0.5, "vulnerability evidence coverage"),
        _nist_csf("DE.CM", "Continuous Monitoring", 0.42, "vulnerability monitoring visibility"),
        _nist_800_53(
            "RA family", "Risk Assessment", 0.44, "vulnerability scanning evidence family"
        ),
    ),
    "host.auth.ssh_failed_password_spike": (
        _nist_csf("DE.CM", "Continuous Monitoring", 0.62, "authentication event monitoring"),
        _nist_800_53("AU family", "Audit and Accountability", 0.56, "audit review family"),
        _cis("authentication-monitoring", "Authentication monitoring", 0.42),
    ),
    "host.auth.root_login_attempts": (
        _nist_csf("DE.CM", "Continuous Monitoring", 0.62, "privileged authentication monitoring"),
        _nist_csf(
            "PR.AA",
            "Identity Management, Authentication and Access Control",
            0.54,
            "privileged access monitoring",
        ),
        _nist_800_53("AU family", "Audit and Accountability", 0.56, "audit review family"),
    ),
    "host.auth.active_privileged_session": (
        _nist_csf(
            "PR.AA",
            "Identity Management, Authentication and Access Control",
            0.52,
            "privileged session review",
        ),
        _nist_800_53("AC family", "Access Control", 0.48, "privileged access family"),
    ),
    "host.auth.sudo_activity_present": (
        _nist_csf("DE.CM", "Continuous Monitoring", 0.5, "privileged command monitoring"),
        _nist_800_53("AU family", "Audit and Accountability", 0.5, "audit review family"),
    ),
    "host.auth.compound_ssh_brute_force": (
        _nist_csf(
            "PR.AA",
            "Identity Management, Authentication and Access Control",
            0.66,
            "remote privileged authentication risk",
        ),
        _nist_csf("DE.CM", "Continuous Monitoring", 0.62, "authentication attack monitoring"),
        _nist_800_53("AC family", "Access Control", 0.56, "remote access family"),
        _nist_800_53("AU family", "Audit and Accountability", 0.54, "audit review family"),
        _cis("ssh-hardening", "SSH daemon hardening", 0.52),
    ),
}


def apply_host_control_mappings(findings: list[HostFinding]) -> list[HostFinding]:
    return [
        finding.model_copy(
            update={
                "structured_control_refs": _dedupe_controls(
                    [
                        *finding.structured_control_refs,
                        *structured_controls_for_finding(finding),
                    ]
                )
            }
        )
        for finding in findings
    ]


def structured_controls_for_finding(finding: HostFinding) -> tuple[HostControlRef, ...]:
    controls = list(HOST_CONTROL_MAPPINGS.get(finding.rule_id or "", ()))
    if finding.rule_id in {"host.baseline.lynis", "host.baseline.openscap"}:
        controls.extend(_baseline_controls(finding))
    return tuple(_dedupe_controls(controls))


def control_summary_for_findings(findings: list[HostFinding]) -> dict[str, object]:
    active_findings = [finding for finding in findings if not finding.suppressed]
    frameworks: dict[str, dict[str, object]] = {}
    controls_by_framework: dict[str, set[str]] = defaultdict(set)
    confidence_by_framework: dict[str, list[float]] = defaultdict(list)
    severity_by_framework: dict[str, list[Severity]] = defaultdict(list)
    findings_by_framework: dict[str, set[str]] = defaultdict(set)
    unmapped: Counter[str] = Counter()

    for finding in active_findings:
        if not finding.structured_control_refs:
            key = finding.rule_id or "unknown"
            unmapped[key] += 1
            continue
        for control in finding.structured_control_refs:
            findings_by_framework[control.framework].add(finding.id)
            controls_by_framework[control.framework].add(control.control_id)
            confidence_by_framework[control.framework].append(control.mapping_confidence)
            severity_by_framework[control.framework].append(finding.severity)

    for framework in sorted(findings_by_framework):
        confidences = confidence_by_framework[framework]
        frameworks[framework] = {
            "mapped_findings": len(findings_by_framework[framework]),
            "mapped_controls": len(controls_by_framework[framework]),
            "highest_severity": _highest_severity(severity_by_framework[framework]),
            "average_mapping_confidence": round(sum(confidences) / len(confidences), 3)
            if confidences
            else 0.0,
        }

    return {
        "mapped_findings": len(
            {finding.id for finding in active_findings if finding.structured_control_refs}
        ),
        "unmapped_findings": sum(unmapped.values()),
        "frameworks": frameworks,
        "unmapped_rule_ids": dict(sorted(unmapped.items())),
        "mapping_note": (
            "Structured host controls are supporting mappings only. Broad CIS/NIST "
            "family mappings do not assert full compliance or exact profile coverage."
        ),
    }


def host_control_mapping_status(rule_id: str) -> str | None:
    if rule_id in HOST_CONTROL_MAPPINGS:
        return "mapped"
    if rule_id in {"host.baseline.lynis", "host.baseline.openscap"}:
        return "mapped"
    return HOST_CONTROL_NO_MAP_REASONS.get(rule_id)


def _baseline_controls(finding: HostFinding) -> list[HostControlRef]:
    controls: list[HostControlRef] = []
    if finding.source_tool == "lynis":
        check_id = _instance_suffix(finding.instance_key, "lynis")
        if check_id:
            controls.append(
                HostControlRef(
                    framework="Lynis",
                    version=None,
                    control_id=check_id,
                    title=finding.title,
                    mapping_confidence=0.82,
                    rationale="Lynis check identifier supplied by local baseline evidence.",
                )
            )
    if finding.source_tool == "openscap":
        check_id = _instance_suffix(finding.instance_key, "openscap")
        if check_id:
            controls.append(
                HostControlRef(
                    framework="OpenSCAP XCCDF",
                    version=None,
                    control_id=check_id,
                    title=finding.title,
                    mapping_confidence=0.86,
                    rationale="OpenSCAP XCCDF rule identifier supplied by local baseline evidence.",
                )
            )
        for ref in finding.control_refs:
            controls.append(_control_from_openscap_ref(ref, finding.title))
    return controls


def _control_from_openscap_ref(ref: str, title: str) -> HostControlRef:
    framework = "OpenSCAP"
    rationale = "Control reference supplied by OpenSCAP evidence."
    confidence = 0.78
    if ref.startswith("CCE-"):
        framework = "Common Configuration Enumeration"
        rationale = "CCE identifier supplied by OpenSCAP rule metadata."
        confidence = 0.82
    elif ref.startswith("CIS-"):
        framework = "CIS"
        rationale = "CIS reference supplied by OpenSCAP rule metadata."
        confidence = 0.78
    return HostControlRef(
        framework=framework,
        version=None,
        control_id=ref,
        title=title,
        mapping_confidence=confidence,
        rationale=rationale,
    )


def _dedupe_controls(controls: list[HostControlRef]) -> list[HostControlRef]:
    deduped: dict[tuple[str, str, str | None], HostControlRef] = {}
    for control in controls:
        key = (control.framework, control.control_id, control.version)
        existing = deduped.get(key)
        if existing is None or control.mapping_confidence > existing.mapping_confidence:
            deduped[key] = control
    return list(deduped.values())


def _instance_suffix(instance_key: str | None, prefix: str) -> str | None:
    if not instance_key:
        return None
    expected = f"{prefix}:"
    if instance_key.startswith(expected):
        return instance_key[len(expected) :]
    return None


def _highest_severity(values: list[Severity]) -> Severity | None:
    order: dict[str, int] = {
        "informational": 0,
        "low": 1,
        "medium": 2,
        "high": 3,
        "critical": 4,
    }
    if not values:
        return None
    return sorted(values, key=lambda item: order[item], reverse=True)[0]
