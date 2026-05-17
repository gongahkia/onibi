from __future__ import annotations

from dataclasses import dataclass
from enum import StrEnum


class ExploitStatus(StrEnum):
    NONE = "none"
    POC_AVAILABLE = "poc_available"
    WEAPONIZED = "weaponized"
    IN_THE_WILD = "in_the_wild"


@dataclass(frozen=True)
class AffectedPackage:
    ecosystem: str
    name: str
    vulnerable_ranges: tuple[str, ...] = ()
    fixed_versions: tuple[str, ...] = ()


@dataclass(frozen=True)
class Advisory:
    advisory_id: str
    cve_id: str | None
    ghsa_id: str | None
    cwe_ids: tuple[str, ...]
    title: str
    description: str
    affected_packages: tuple[AffectedPackage, ...]
    severity: str
    cvss_score: float | None = None
    cvss_vector: str | None = None
    epss_score: float | None = None
    epss_percentile: float | None = None
    exploit_status: ExploitStatus = ExploitStatus.NONE
    exploit_sources: tuple[str, ...] = ()
    fix_available: bool = False
    fix_version: str | None = None
    published_date: str | None = None
    modified_date: str | None = None
    sources: tuple[str, ...] = ()
    references: tuple[str, ...] = ()


def canonical_advisory_id(*, cve_id: str | None, ghsa_id: str | None, fallback: str) -> str:
    if cve_id:
        return cve_id
    if ghsa_id:
        return ghsa_id
    return fallback


def normalize_severity(value: str | None, *, default: str = "medium") -> str:
    if value is None:
        return default
    normalized = value.strip().lower()
    if not normalized:
        return default
    mapping = {
        "critical": "critical",
        "high": "high",
        "moderate": "medium",
        "medium": "medium",
        "low": "low",
        "info": "low",
        "informational": "low",
        "none": "low",
    }
    return mapping.get(normalized, default)


def severity_rank(value: str) -> int:
    normalized = normalize_severity(value)
    if normalized == "critical":
        return 4
    if normalized == "high":
        return 3
    if normalized == "medium":
        return 2
    if normalized == "low":
        return 1
    return 0


def exploit_status_rank(value: ExploitStatus) -> int:
    if value is ExploitStatus.IN_THE_WILD:
        return 4
    if value is ExploitStatus.WEAPONIZED:
        return 3
    if value is ExploitStatus.POC_AVAILABLE:
        return 2
    return 1
