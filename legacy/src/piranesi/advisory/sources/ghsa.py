from __future__ import annotations

from collections.abc import Mapping, Sequence

import requests

from piranesi.advisory.models import (
    Advisory,
    AffectedPackage,
    ExploitStatus,
    canonical_advisory_id,
    normalize_severity,
    severity_rank,
)

GITHUB_GRAPHQL_URL = "https://api.github.com/graphql"
GHSA_QUERY = """
query AdvisorySync($first: Int!, $after: String, $updatedSince: DateTime) {
  securityAdvisories(
    first: $first
    after: $after
    updatedSince: $updatedSince
    orderBy: {field: UPDATED_AT, direction: ASC}
  ) {
    pageInfo {
      hasNextPage
      endCursor
    }
    nodes {
      ghsaId
      summary
      description
      severity
      publishedAt
      updatedAt
      identifiers {
        type
        value
      }
      references {
        url
      }
      cvss {
        score
        vectorString
      }
      cwes(first: 20) {
        nodes {
          cweId
        }
      }
      vulnerabilities(first: 100) {
        nodes {
          severity
          vulnerableVersionRange
          package {
            ecosystem
            name
          }
          firstPatchedVersion {
            identifier
          }
        }
      }
    }
  }
}
"""

_ECOSYSTEM_MAP = {
    "COMPOSER": "composer",
    "GO": "go",
    "MAVEN": "maven",
    "NPM": "npm",
    "NUGET": "nuget",
    "PIP": "pypi",
    "PYPI": "pypi",
    "RUBYGEMS": "rubygems",
    "RUST": "crates",
}


def fetch_ghsa_advisories(
    *,
    since: str | None = None,
    session: requests.Session | None = None,
    token: str | None,
    full: bool = False,
) -> tuple[list[Advisory], str | None]:
    if not token:
        return [], None
    http = session or requests.Session()
    advisories: list[Advisory] = []
    cursor: str | None = None
    while True:
        response = http.post(
            GITHUB_GRAPHQL_URL,
            json={
                "query": GHSA_QUERY,
                "variables": {
                    "first": 100,
                    "after": cursor,
                    "updatedSince": None if full else since,
                },
            },
            headers={
                "Accept": "application/vnd.github+json",
                "Authorization": f"Bearer {token}",
            },
            timeout=60,
        )
        response.raise_for_status()
        payload = response.json()
        page, cursor, has_next = parse_ghsa_response(payload)
        advisories.extend(page)
        if not has_next:
            break
    return advisories, cursor


def parse_ghsa_response(payload: object) -> tuple[list[Advisory], str | None, bool]:
    if not isinstance(payload, Mapping):
        return [], None, False
    data = payload.get("data")
    if not isinstance(data, Mapping):
        return [], None, False
    advisories_node = data.get("securityAdvisories")
    if not isinstance(advisories_node, Mapping):
        return [], None, False
    nodes = advisories_node.get("nodes")
    if not isinstance(nodes, Sequence):
        return [], None, False
    advisories: list[Advisory] = []
    for node in nodes:
        if not isinstance(node, Mapping):
            continue
        advisory = parse_ghsa_advisory(node)
        if advisory is not None:
            advisories.append(advisory)
    page_info = advisories_node.get("pageInfo")
    end_cursor: str | None = None
    has_next = False
    if isinstance(page_info, Mapping):
        end_cursor = _string_value(page_info.get("endCursor"))
        has_next = bool(page_info.get("hasNextPage"))
    return advisories, end_cursor, has_next


def parse_ghsa_advisory(payload: Mapping[str, object]) -> Advisory | None:
    ghsa_id = _string_value(payload.get("ghsaId"))
    if ghsa_id is None:
        return None
    cve_id = _extract_identifier(payload.get("identifiers"), "CVE")
    packages = _extract_packages(payload.get("vulnerabilities"))
    advisory_severity = normalize_severity(_string_value(payload.get("severity")))
    vuln_severity = max(
        (severity_rank(pkg[0]) for pkg in packages), default=severity_rank(advisory_severity)
    )
    severity = advisory_severity
    for candidate in ("critical", "high", "medium", "low"):
        if severity_rank(candidate) == vuln_severity:
            severity = candidate
            break
    affected_packages = tuple(pkg[1] for pkg in packages)
    return Advisory(
        advisory_id=canonical_advisory_id(cve_id=cve_id, ghsa_id=ghsa_id, fallback=ghsa_id),
        cve_id=cve_id,
        ghsa_id=ghsa_id,
        cwe_ids=tuple(sorted(_extract_cwes(payload.get("cwes")))),
        title=_string_value(payload.get("summary")) or ghsa_id,
        description=_string_value(payload.get("description")) or "",
        affected_packages=affected_packages,
        severity=severity,
        cvss_score=_nullable_float_from_mapping(payload.get("cvss"), "score"),
        cvss_vector=_nullable_text_from_mapping(payload.get("cvss"), "vectorString"),
        epss_score=None,
        epss_percentile=None,
        exploit_status=ExploitStatus.NONE,
        exploit_sources=(),
        fix_available=any(pkg.fixed_versions for pkg in affected_packages),
        fix_version=_first_fix_version(affected_packages),
        published_date=_string_value(payload.get("publishedAt")),
        modified_date=_string_value(payload.get("updatedAt")),
        sources=("ghsa",),
        references=tuple(sorted(_extract_references(payload.get("references")))),
    )


def _extract_identifier(identifiers: object, identifier_type: str) -> str | None:
    if not isinstance(identifiers, Sequence):
        return None
    for identifier in identifiers:
        if not isinstance(identifier, Mapping):
            continue
        if _string_value(identifier.get("type")) == identifier_type:
            return _string_value(identifier.get("value"))
    return None


def _extract_cwes(cwes: object) -> set[str]:
    if not isinstance(cwes, Mapping):
        return set()
    nodes = cwes.get("nodes")
    if not isinstance(nodes, Sequence):
        return set()
    results: set[str] = set()
    for node in nodes:
        if not isinstance(node, Mapping):
            continue
        cwe_id = _string_value(node.get("cweId"))
        if cwe_id:
            results.add(cwe_id)
    return results


def _extract_packages(vulnerabilities: object) -> list[tuple[str, AffectedPackage]]:
    if not isinstance(vulnerabilities, Mapping):
        return []
    nodes = vulnerabilities.get("nodes")
    if not isinstance(nodes, Sequence):
        return []
    packages: list[tuple[str, AffectedPackage]] = []
    for node in nodes:
        if not isinstance(node, Mapping):
            continue
        package = node.get("package")
        if not isinstance(package, Mapping):
            continue
        name = _string_value(package.get("name"))
        ecosystem = _map_ecosystem(_string_value(package.get("ecosystem")))
        if name is None or ecosystem is None:
            continue
        fixed_version = _nullable_text_from_mapping(node.get("firstPatchedVersion"), "identifier")
        vulnerable_range = _string_value(node.get("vulnerableVersionRange"))
        packages.append(
            (
                normalize_severity(_string_value(node.get("severity"))),
                AffectedPackage(
                    ecosystem=ecosystem,
                    name=name,
                    vulnerable_ranges=(vulnerable_range,) if vulnerable_range else (),
                    fixed_versions=(fixed_version,) if fixed_version else (),
                ),
            )
        )
    return packages


def _map_ecosystem(value: str | None) -> str | None:
    if value is None:
        return None
    return _ECOSYSTEM_MAP.get(value.upper(), value.lower())


def _extract_references(references: object) -> set[str]:
    if not isinstance(references, Sequence):
        return set()
    urls: set[str] = set()
    for reference in references:
        if not isinstance(reference, Mapping):
            continue
        url = _string_value(reference.get("url"))
        if url:
            urls.add(url)
    return urls


def _nullable_float_from_mapping(payload: object, key: str) -> float | None:
    if not isinstance(payload, Mapping):
        return None
    value = payload.get(key)
    try:
        return float(value) if value is not None else None
    except (TypeError, ValueError):
        return None


def _nullable_text_from_mapping(payload: object, key: str) -> str | None:
    if not isinstance(payload, Mapping):
        return None
    return _string_value(payload.get(key))


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
