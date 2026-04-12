from __future__ import annotations

from collections.abc import Mapping, Sequence
from datetime import UTC, datetime

import requests

from piranesi.advisory.models import (
    Advisory,
    AffectedPackage,
    ExploitStatus,
    canonical_advisory_id,
    normalize_severity,
)

NVD_API_URL = "https://services.nvd.nist.gov/rest/json/cves/2.0"


def fetch_nvd_advisories(
    *,
    since: str | None = None,
    session: requests.Session | None = None,
    api_key: str | None = None,
    full: bool = False,
) -> tuple[list[Advisory], str | None]:
    http = session or requests.Session()
    start_index = 0
    advisories: list[Advisory] = []
    latest_cursor: str | None = None
    headers = {"Accept": "application/json"}
    if api_key:
        headers["apiKey"] = api_key

    while True:
        params: dict[str, str | int] = {"startIndex": start_index}
        if since is not None and not full:
            params["lastModStartDate"] = since
            params["lastModEndDate"] = datetime.now(UTC).isoformat().replace("+00:00", "Z")
        response = http.get(NVD_API_URL, params=params, headers=headers, timeout=60)
        response.raise_for_status()
        payload = response.json()
        page = parse_nvd_response(payload)
        advisories.extend(page)
        vulnerabilities = payload.get("vulnerabilities")
        if isinstance(vulnerabilities, list):
            for item in vulnerabilities:
                if isinstance(item, Mapping):
                    cve = item.get("cve")
                    if isinstance(cve, Mapping):
                        modified = cve.get("lastModified")
                        if isinstance(modified, str) and (
                            latest_cursor is None or modified > latest_cursor
                        ):
                            latest_cursor = modified
        total_results = payload.get("totalResults")
        results_per_page = payload.get("resultsPerPage")
        if not isinstance(total_results, int) or not isinstance(results_per_page, int):
            break
        start_index += results_per_page
        if start_index >= total_results:
            break
    return advisories, latest_cursor


def parse_nvd_response(payload: object) -> list[Advisory]:
    if not isinstance(payload, Mapping):
        return []
    vulnerabilities = payload.get("vulnerabilities")
    if not isinstance(vulnerabilities, Sequence):
        return []
    advisories: list[Advisory] = []
    for item in vulnerabilities:
        if not isinstance(item, Mapping):
            continue
        cve = item.get("cve")
        if not isinstance(cve, Mapping):
            continue
        advisory = parse_nvd_cve_item(cve)
        if advisory is not None:
            advisories.append(advisory)
    return advisories


def parse_nvd_cve_item(cve: Mapping[str, object]) -> Advisory | None:
    cve_id = _string_value(cve.get("id"))
    if cve_id is None:
        return None
    severity, cvss_score, cvss_vector = _extract_cvss(cve.get("metrics"))
    return Advisory(
        advisory_id=canonical_advisory_id(cve_id=cve_id, ghsa_id=None, fallback=cve_id),
        cve_id=cve_id,
        ghsa_id=None,
        cwe_ids=tuple(sorted(_extract_cwes(cve.get("weaknesses")))),
        title=_english_description(cve.get("descriptions")) or cve_id,
        description=_english_description(cve.get("descriptions")) or "",
        affected_packages=tuple(_extract_affected_packages(cve.get("configurations"))),
        severity=severity,
        cvss_score=cvss_score,
        cvss_vector=cvss_vector,
        epss_score=None,
        epss_percentile=None,
        exploit_status=ExploitStatus.NONE,
        exploit_sources=(),
        fix_available=any(
            pkg.fixed_versions for pkg in _extract_affected_packages(cve.get("configurations"))
        ),
        fix_version=_first_fix_version(_extract_affected_packages(cve.get("configurations"))),
        published_date=_string_value(cve.get("published")),
        modified_date=_string_value(cve.get("lastModified")),
        sources=("nvd",),
        references=tuple(sorted(_extract_references(cve.get("references")))),
    )


def _extract_cvss(metrics: object) -> tuple[str, float | None, str | None]:
    if not isinstance(metrics, Mapping):
        return "medium", None, None
    for key in ("cvssMetricV31", "cvssMetricV30", "cvssMetricV2"):
        values = metrics.get(key)
        if not isinstance(values, Sequence) or not values:
            continue
        first = values[0]
        if not isinstance(first, Mapping):
            continue
        cvss_data = first.get("cvssData")
        if not isinstance(cvss_data, Mapping):
            continue
        score = cvss_data.get("baseScore")
        vector = _string_value(cvss_data.get("vectorString"))
        severity = _string_value(first.get("baseSeverity")) or _string_value(
            cvss_data.get("baseSeverity")
        )
        try:
            parsed_score = float(score) if score is not None else None
        except (TypeError, ValueError):
            parsed_score = None
        return normalize_severity(severity), parsed_score, vector
    return "medium", None, None


def _extract_cwes(weaknesses: object) -> set[str]:
    cwe_ids: set[str] = set()
    if not isinstance(weaknesses, Sequence):
        return cwe_ids
    for weakness in weaknesses:
        if not isinstance(weakness, Mapping):
            continue
        descriptions = weakness.get("description")
        if not isinstance(descriptions, Sequence):
            continue
        for description in descriptions:
            if not isinstance(description, Mapping):
                continue
            value = _string_value(description.get("value"))
            if value and value.startswith("CWE-"):
                cwe_ids.add(value)
    return cwe_ids


def _english_description(descriptions: object) -> str | None:
    if not isinstance(descriptions, Sequence):
        return None
    fallback: str | None = None
    for description in descriptions:
        if not isinstance(description, Mapping):
            continue
        value = _string_value(description.get("value"))
        if value is None:
            continue
        if fallback is None:
            fallback = value
        if _string_value(description.get("lang")) == "en":
            return value
    return fallback


def _extract_references(references: object) -> set[str]:
    urls: set[str] = set()
    if not isinstance(references, Sequence):
        return urls
    for item in references:
        if not isinstance(item, Mapping):
            continue
        url = _string_value(item.get("url"))
        if url:
            urls.add(url)
    return urls


def _extract_affected_packages(configurations: object) -> list[AffectedPackage]:
    collected: dict[tuple[str, str], AffectedPackage] = {}

    def visit_nodes(nodes: object) -> None:
        if not isinstance(nodes, Sequence):
            return
        for node in nodes:
            if not isinstance(node, Mapping):
                continue
            cpe_matches = node.get("cpeMatch")
            if isinstance(cpe_matches, Sequence):
                for match in cpe_matches:
                    if not isinstance(match, Mapping):
                        continue
                    criteria = _string_value(match.get("criteria"))
                    package_name = _package_name_from_cpe(criteria)
                    if package_name is None:
                        continue
                    vulnerable_ranges = _range_from_cpe_match(match)
                    fixed_versions = _fixed_versions_from_cpe_match(match)
                    key = ("generic", package_name)
                    current = collected.get(key)
                    if current is None:
                        collected[key] = AffectedPackage(
                            ecosystem="generic",
                            name=package_name,
                            vulnerable_ranges=tuple(vulnerable_ranges),
                            fixed_versions=tuple(fixed_versions),
                        )
                    else:
                        collected[key] = AffectedPackage(
                            ecosystem="generic",
                            name=package_name,
                            vulnerable_ranges=tuple(
                                sorted(set(current.vulnerable_ranges) | set(vulnerable_ranges))
                            ),
                            fixed_versions=tuple(
                                sorted(set(current.fixed_versions) | set(fixed_versions))
                            ),
                        )
            visit_nodes(node.get("nodes"))

    if isinstance(configurations, Sequence):
        for configuration in configurations:
            if not isinstance(configuration, Mapping):
                continue
            visit_nodes(configuration.get("nodes"))
    return sorted(collected.values(), key=lambda item: item.name)


def _package_name_from_cpe(criteria: str | None) -> str | None:
    if criteria is None:
        return None
    parts = criteria.split(":")
    if len(parts) < 5:
        return None
    product = parts[4].strip()
    return product or None


def _range_from_cpe_match(match: Mapping[str, object]) -> list[str]:
    start_including = _string_value(match.get("versionStartIncluding"))
    start_excluding = _string_value(match.get("versionStartExcluding"))
    end_including = _string_value(match.get("versionEndIncluding"))
    end_excluding = _string_value(match.get("versionEndExcluding"))
    parts: list[str] = []
    if start_including:
        parts.append(f">={start_including}")
    if start_excluding:
        parts.append(f">{start_excluding}")
    if end_including:
        parts.append(f"<={end_including}")
    if end_excluding:
        parts.append(f"<{end_excluding}")
    return [" ".join(parts)] if parts else []


def _fixed_versions_from_cpe_match(match: Mapping[str, object]) -> list[str]:
    end_excluding = _string_value(match.get("versionEndExcluding"))
    end_including = _string_value(match.get("versionEndIncluding"))
    if end_excluding:
        return [end_excluding]
    return [end_including] if end_including else []


def _first_fix_version(packages: Sequence[AffectedPackage]) -> str | None:
    for package in packages:
        if package.fixed_versions:
            return package.fixed_versions[0]
    return None


def _string_value(value: object) -> str | None:
    if not isinstance(value, str):
        return None
    stripped = value.strip()
    return stripped or None
