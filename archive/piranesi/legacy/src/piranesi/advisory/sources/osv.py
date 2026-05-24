from __future__ import annotations

from collections.abc import Mapping, Sequence

import requests

from piranesi.advisory.models import (
    Advisory,
    AffectedPackage,
    ExploitStatus,
    canonical_advisory_id,
    severity_rank,
)

OSV_INDEX_URL = "https://osv.dev/advisories/all.json"
OSV_ADVISORY_BASE_URL = "https://osv.dev/advisories"

_ECOSYSTEM_MAP = {
    "GO": "go",
    "MAVEN": "maven",
    "NPM": "npm",
    "PYPI": "pypi",
    "RUBYGEMS": "rubygems",
    "CRATES.IO": "crates",
    "CRATES": "crates",
    "PACKAGIST": "composer",
}


def fetch_osv_advisories(
    *,
    since: str | None = None,
    session: requests.Session | None = None,
    ecosystems: Sequence[str] | None = None,
    full: bool = False,
) -> tuple[list[Advisory], str | None]:
    http = session or requests.Session()
    response = http.get(OSV_INDEX_URL, timeout=60)
    response.raise_for_status()
    entries = response.json()
    if not isinstance(entries, list):
        return [], None

    normalized_ecosystems = {item.lower() for item in ecosystems or ()}
    advisories: list[Advisory] = []
    latest_cursor: str | None = None
    for entry in entries:
        if not isinstance(entry, Mapping):
            continue
        advisory_id = _string_value(entry.get("id"))
        modified = _string_value(entry.get("modified"))
        if advisory_id is None:
            continue
        if modified and (latest_cursor is None or modified > latest_cursor):
            latest_cursor = modified
        if since is not None and not full and modified is not None and modified <= since:
            continue
        advisory_response = http.get(f"{OSV_ADVISORY_BASE_URL}/{advisory_id}.json", timeout=60)
        advisory_response.raise_for_status()
        advisory_payload = advisory_response.json()
        advisory = parse_osv_advisory(advisory_payload, source="osv")
        if advisory is None:
            continue
        if normalized_ecosystems and not any(
            pkg.ecosystem in normalized_ecosystems for pkg in advisory.affected_packages
        ):
            continue
        advisories.append(advisory)
    return advisories, latest_cursor


def parse_osv_advisory(payload: object, *, source: str = "osv") -> Advisory | None:
    if not isinstance(payload, Mapping):
        return None
    advisory_id = _string_value(payload.get("id"))
    if advisory_id is None:
        return None
    aliases = payload.get("aliases")
    cve_id = _extract_alias(aliases, prefix="CVE-")
    ghsa_id = _extract_alias(aliases, prefix="GHSA-")
    affected_packages = _extract_affected_packages(payload.get("affected"))
    severity, cvss_score, cvss_vector = _extract_severity(payload.get("severity"))
    references = _extract_references(payload.get("references"))
    cwes = _extract_cwes(payload.get("database_specific"), payload.get("ecosystem_specific"))
    return Advisory(
        advisory_id=canonical_advisory_id(cve_id=cve_id, ghsa_id=ghsa_id, fallback=advisory_id),
        cve_id=cve_id,
        ghsa_id=ghsa_id,
        cwe_ids=tuple(sorted(cwes)),
        title=_string_value(payload.get("summary")) or advisory_id,
        description=_string_value(payload.get("details")) or "",
        affected_packages=tuple(affected_packages),
        severity=severity,
        cvss_score=cvss_score,
        cvss_vector=cvss_vector,
        epss_score=None,
        epss_percentile=None,
        exploit_status=ExploitStatus.NONE,
        exploit_sources=(),
        fix_available=any(pkg.fixed_versions for pkg in affected_packages),
        fix_version=_first_fix_version(affected_packages),
        published_date=_string_value(payload.get("published")),
        modified_date=_string_value(payload.get("modified")),
        sources=(source,),
        references=tuple(sorted(references)),
    )


def _extract_alias(aliases: object, *, prefix: str) -> str | None:
    if not isinstance(aliases, Sequence):
        return None
    for alias in aliases:
        if isinstance(alias, str) and alias.startswith(prefix):
            return alias
    return None


def _extract_affected_packages(affected: object) -> list[AffectedPackage]:
    if not isinstance(affected, Sequence):
        return []
    packages: dict[tuple[str, str], AffectedPackage] = {}
    for item in affected:
        if not isinstance(item, Mapping):
            continue
        package = item.get("package")
        if not isinstance(package, Mapping):
            continue
        name = _string_value(package.get("name"))
        ecosystem = _map_ecosystem(_string_value(package.get("ecosystem")))
        if name is None or ecosystem is None:
            continue
        vulnerable_ranges = _extract_ranges(item.get("ranges"))
        fixed_versions = _extract_fixed_versions(item.get("ranges"), item.get("versions"))
        key = (ecosystem, name)
        current = packages.get(key)
        if current is None:
            packages[key] = AffectedPackage(
                ecosystem=ecosystem,
                name=name,
                vulnerable_ranges=tuple(vulnerable_ranges),
                fixed_versions=tuple(fixed_versions),
            )
        else:
            packages[key] = AffectedPackage(
                ecosystem=ecosystem,
                name=name,
                vulnerable_ranges=tuple(
                    sorted(set(current.vulnerable_ranges) | set(vulnerable_ranges))
                ),
                fixed_versions=tuple(sorted(set(current.fixed_versions) | set(fixed_versions))),
            )
    return sorted(packages.values(), key=lambda item: (item.ecosystem, item.name))


def _extract_ranges(ranges: object) -> list[str]:
    if not isinstance(ranges, Sequence):
        return []
    collected: list[str] = []
    for range_item in ranges:
        if not isinstance(range_item, Mapping):
            continue
        events = range_item.get("events")
        if not isinstance(events, Sequence):
            continue
        introduced = "0"
        for event in events:
            if not isinstance(event, Mapping):
                continue
            if "introduced" in event:
                introduced = _string_value(event.get("introduced")) or introduced
            fixed = _string_value(event.get("fixed"))
            last_affected = _string_value(event.get("last_affected"))
            if fixed:
                lower = f">={introduced}" if introduced not in {"0", ""} else ">=0"
                collected.append(f"{lower} <{fixed}".strip())
            elif last_affected:
                lower = f">={introduced}" if introduced not in {"0", ""} else ">=0"
                collected.append(f"{lower} <={last_affected}".strip())
    return collected


def _extract_fixed_versions(ranges: object, versions: object) -> list[str]:
    fixed_versions: list[str] = []
    if isinstance(ranges, Sequence):
        for range_item in ranges:
            if not isinstance(range_item, Mapping):
                continue
            events = range_item.get("events")
            if not isinstance(events, Sequence):
                continue
            for event in events:
                if not isinstance(event, Mapping):
                    continue
                fixed = _string_value(event.get("fixed"))
                if fixed:
                    fixed_versions.append(fixed)
    if not fixed_versions and isinstance(versions, Sequence):
        for version in versions:
            if isinstance(version, str):
                fixed_versions.append(version)
    return fixed_versions


def _extract_severity(severity_items: object) -> tuple[str, float | None, str | None]:
    if not isinstance(severity_items, Sequence):
        return "medium", None, None
    best_severity = "medium"
    best_score: float | None = None
    best_vector: str | None = None
    for item in severity_items:
        if not isinstance(item, Mapping):
            continue
        if _string_value(item.get("type")) != "CVSS_V3":
            continue
        score, severity = _score_from_cvss_vector(_string_value(item.get("score")))
        if severity_rank(severity) >= severity_rank(best_severity):
            best_severity = severity
            best_score = score
            best_vector = _string_value(item.get("score"))
    return best_severity, best_score, best_vector


def _score_from_cvss_vector(vector: str | None) -> tuple[float | None, str]:
    if vector is None:
        return None, "medium"
    score_match = vector.rsplit("/", 1)
    if len(score_match) == 2 and score_match[-1].startswith("S:"):
        return None, "medium"
    return None, "medium"


def _extract_references(references: object) -> set[str]:
    if not isinstance(references, Sequence):
        return set()
    urls: set[str] = set()
    for item in references:
        if not isinstance(item, Mapping):
            continue
        url = _string_value(item.get("url"))
        if url:
            urls.add(url)
    return urls


def _extract_cwes(*payloads: object) -> set[str]:
    cwes: set[str] = set()
    for payload in payloads:
        if not isinstance(payload, Mapping):
            continue
        raw_cwes = payload.get("cwe_ids") or payload.get("cwes")
        if not isinstance(raw_cwes, Sequence):
            continue
        for cwe in raw_cwes:
            if isinstance(cwe, str) and cwe.startswith("CWE-"):
                cwes.add(cwe)
    return cwes


def _first_fix_version(packages: Sequence[AffectedPackage]) -> str | None:
    for package in packages:
        if package.fixed_versions:
            return package.fixed_versions[0]
    return None


def _map_ecosystem(value: str | None) -> str | None:
    if value is None:
        return None
    return _ECOSYSTEM_MAP.get(value.upper(), value.lower())


def _string_value(value: object) -> str | None:
    if not isinstance(value, str):
        return None
    stripped = value.strip()
    return stripped or None
