from __future__ import annotations

from collections.abc import Sequence
from enum import Flag, auto

from piranesi.models import CandidateFinding
from piranesi.report.cwe import extract_cwe_id


class StrideCategory(Flag):
    SPOOFING = auto()
    TAMPERING = auto()
    REPUDIATION = auto()
    INFORMATION_DISCLOSURE = auto()
    DENIAL_OF_SERVICE = auto()
    ELEVATION_OF_PRIVILEGE = auto()


STRIDE_ORDER = (
    StrideCategory.SPOOFING,
    StrideCategory.TAMPERING,
    StrideCategory.REPUDIATION,
    StrideCategory.INFORMATION_DISCLOSURE,
    StrideCategory.DENIAL_OF_SERVICE,
    StrideCategory.ELEVATION_OF_PRIVILEGE,
)

STRIDE_LABELS: dict[StrideCategory, str] = {
    StrideCategory.SPOOFING: "Spoofing",
    StrideCategory.TAMPERING: "Tampering",
    StrideCategory.REPUDIATION: "Repudiation",
    StrideCategory.INFORMATION_DISCLOSURE: "Information Disclosure",
    StrideCategory.DENIAL_OF_SERVICE: "Denial of Service",
    StrideCategory.ELEVATION_OF_PRIVILEGE: "Elevation of Privilege",
}

CWE_STRIDE_MAP: dict[str, StrideCategory] = {
    "CWE-22": StrideCategory.INFORMATION_DISCLOSURE,
    "CWE-78": StrideCategory.TAMPERING | StrideCategory.ELEVATION_OF_PRIVILEGE,
    "CWE-79": StrideCategory.TAMPERING,
    "CWE-89": StrideCategory.TAMPERING | StrideCategory.INFORMATION_DISCLOSURE,
    "CWE-94": StrideCategory.TAMPERING | StrideCategory.ELEVATION_OF_PRIVILEGE,
    "CWE-269": StrideCategory.ELEVATION_OF_PRIVILEGE,
    "CWE-287": StrideCategory.SPOOFING,
    "CWE-295": StrideCategory.INFORMATION_DISCLOSURE | StrideCategory.SPOOFING,
    "CWE-306": StrideCategory.SPOOFING,
    "CWE-319": StrideCategory.INFORMATION_DISCLOSURE,
    "CWE-327": StrideCategory.INFORMATION_DISCLOSURE,
    "CWE-328": StrideCategory.INFORMATION_DISCLOSURE,
    "CWE-338": StrideCategory.INFORMATION_DISCLOSURE | StrideCategory.SPOOFING,
    "CWE-345": StrideCategory.SPOOFING,
    "CWE-347": StrideCategory.INFORMATION_DISCLOSURE | StrideCategory.SPOOFING,
    "CWE-352": StrideCategory.SPOOFING | StrideCategory.TAMPERING,
    "CWE-384": StrideCategory.SPOOFING | StrideCategory.REPUDIATION,
    "CWE-400": StrideCategory.DENIAL_OF_SERVICE,
    "CWE-434": StrideCategory.TAMPERING | StrideCategory.ELEVATION_OF_PRIVILEGE,
    "CWE-502": StrideCategory.ELEVATION_OF_PRIVILEGE | StrideCategory.TAMPERING,
    "CWE-611": StrideCategory.TAMPERING | StrideCategory.INFORMATION_DISCLOSURE,
    "CWE-639": StrideCategory.ELEVATION_OF_PRIVILEGE,
    "CWE-770": StrideCategory.DENIAL_OF_SERVICE,
    "CWE-778": StrideCategory.REPUDIATION,
    "CWE-798": StrideCategory.SPOOFING,
    "CWE-835": StrideCategory.DENIAL_OF_SERVICE,
    "CWE-862": StrideCategory.ELEVATION_OF_PRIVILEGE,
    "CWE-863": StrideCategory.ELEVATION_OF_PRIVILEGE,
    "CWE-915": StrideCategory.TAMPERING,
    "CWE-918": StrideCategory.INFORMATION_DISCLOSURE,
    "CWE-1333": StrideCategory.DENIAL_OF_SERVICE,
}

_SINK_HEURISTICS: dict[str, StrideCategory] = {
    "command_execution": StrideCategory.TAMPERING | StrideCategory.ELEVATION_OF_PRIVILEGE,
    "file_write": StrideCategory.TAMPERING,
    "http_response": StrideCategory.INFORMATION_DISCLOSURE,
    "html_output": StrideCategory.INFORMATION_DISCLOSURE,
    "response_write": StrideCategory.INFORMATION_DISCLOSURE,
    "sql_query": StrideCategory.TAMPERING | StrideCategory.INFORMATION_DISCLOSURE,
}


def classify_stride(finding: CandidateFinding) -> StrideCategory:
    cwe_id = extract_cwe_id(finding.vuln_class)
    mapped = CWE_STRIDE_MAP.get(cwe_id)
    if mapped is not None:
        return mapped

    if finding.source.source_type == "dependency_manifest":
        categories = _classify_dependency_finding(finding)
        if categories is not None:
            return categories

    sink_type = finding.sink.sink_type.lower()
    heuristic = _SINK_HEURISTICS.get(sink_type)
    if heuristic is not None:
        return heuristic

    return StrideCategory.INFORMATION_DISCLOSURE


def classify_all(findings: Sequence[CandidateFinding]) -> dict[str, StrideCategory]:
    return {finding.id: classify_stride(finding) for finding in findings}


def stride_breakdown(
    classifications: dict[str, StrideCategory],
) -> dict[StrideCategory, list[str]]:
    breakdown: dict[StrideCategory, list[str]] = {category: [] for category in STRIDE_ORDER}
    for finding_id, classification in classifications.items():
        for category in stride_members(classification):
            breakdown[category].append(finding_id)
    return {category: sorted(ids) for category, ids in breakdown.items()}


def stride_members(categories: StrideCategory) -> tuple[StrideCategory, ...]:
    return tuple(category for category in STRIDE_ORDER if bool(categories & category))


def stride_label(category: StrideCategory) -> str:
    return STRIDE_LABELS[category]


def _classify_dependency_finding(finding: CandidateFinding) -> StrideCategory | None:
    raw_cwe_ids = finding.metadata.get("cwe_ids")
    if not isinstance(raw_cwe_ids, Sequence) or isinstance(raw_cwe_ids, str):
        return None

    categories = StrideCategory(0)
    for raw_cwe_id in raw_cwe_ids:
        if not isinstance(raw_cwe_id, str):
            continue
        categories |= CWE_STRIDE_MAP.get(extract_cwe_id(raw_cwe_id), StrideCategory(0))
    return categories or None


__all__ = [
    "CWE_STRIDE_MAP",
    "STRIDE_LABELS",
    "STRIDE_ORDER",
    "StrideCategory",
    "classify_all",
    "classify_stride",
    "stride_breakdown",
    "stride_label",
    "stride_members",
]
