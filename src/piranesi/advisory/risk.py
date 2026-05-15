from __future__ import annotations

from dataclasses import dataclass

from piranesi.advisory.models import Advisory, ExploitStatus, severity_rank


@dataclass(frozen=True)
class AdvisoryPrioritySignal:
    score: float
    tier: str
    precedence: str
    reason: str
    cvss_version: str | None
    epss_model_version: str
    kev_listed: bool


def advisory_priority_signal(advisory: Advisory) -> AdvisoryPrioritySignal:
    cvss_version = infer_cvss_version(advisory.cvss_vector)
    kev_listed = advisory.exploit_status is ExploitStatus.IN_THE_WILD or (
        "cisa_kev" in advisory.exploit_sources
    )
    if kev_listed:
        score = 10.0
        precedence = "kev"
        reason = "CISA KEV/in-the-wild exploit intelligence has highest precedence"
    elif advisory.exploit_status is ExploitStatus.WEAPONIZED:
        score = 9.0
        precedence = "exploit_intel"
        reason = "weaponized exploit telemetry outranks probability and base-score signals"
    elif advisory.exploit_status is ExploitStatus.POC_AVAILABLE:
        score = 7.5
        precedence = "exploit_intel"
        reason = "public PoC availability elevates exploitability risk"
    elif advisory.epss_score is not None:
        epss = float(advisory.epss_score)
        if epss >= 0.9:
            score = 8.5
        elif epss >= 0.7:
            score = 7.5
        elif epss >= 0.3:
            score = 6.0
        elif epss >= 0.1:
            score = 4.5
        else:
            score = 3.5
        precedence = "epss_v4"
        reason = f"EPSS v4 probability {epss:.3f} used as primary exploit-likelihood signal"
    elif advisory.cvss_score is not None:
        cvss = float(advisory.cvss_score)
        if cvss >= 9.0:
            score = 8.0
        elif cvss >= 7.0:
            score = 6.5
        elif cvss >= 4.0:
            score = 5.0
        else:
            score = 3.5
        precedence = "cvss_v4" if cvss_version == "4.0" else "cvss_legacy"
        reason = (
            f"CVSS {cvss_version or 'unknown'} base score {cvss:.1f} used as primary impact signal"
        )
    else:
        severity = advisory.severity
        if severity_rank(severity) >= severity_rank("critical"):
            score = 7.5
        elif severity_rank(severity) >= severity_rank("high"):
            score = 6.0
        elif severity_rank(severity) >= severity_rank("medium"):
            score = 4.5
        else:
            score = 3.0
        precedence = "severity"
        reason = (
            f"fallback to advisory severity '{severity}' due missing CVSS/EPSS/exploit telemetry"
        )
    return AdvisoryPrioritySignal(
        score=score,
        tier=_priority_tier(score),
        precedence=precedence,
        reason=reason,
        cvss_version=cvss_version,
        epss_model_version="v4",
        kev_listed=kev_listed,
    )


def infer_cvss_version(cvss_vector: str | None) -> str | None:
    if not cvss_vector:
        return None
    normalized = cvss_vector.strip().upper()
    if normalized.startswith("CVSS:4.0/"):
        return "4.0"
    if normalized.startswith("CVSS:3.1/"):
        return "3.1"
    if normalized.startswith("CVSS:3.0/"):
        return "3.0"
    if normalized.startswith("CVSS:2.0/") or normalized.startswith("CVSS2#"):
        return "2.0"
    return None


def _priority_tier(score: float) -> str:
    if score >= 8.0:
        return "critical"
    if score >= 6.0:
        return "high"
    if score >= 4.0:
        return "medium"
    return "low"
