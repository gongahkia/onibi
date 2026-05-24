from __future__ import annotations

from collections import Counter

from pydantic import BaseModel, ConfigDict, Field

from piranesi.intel.schema import NormalizationBundle


class EnrichmentReportSummary(BaseModel):
    model_config = ConfigDict(extra="forbid")

    source_name: str
    tool: str
    trust_level: str
    trust_score: float
    staleness_score: float
    findings_total: int
    findings_by_severity: dict[str, int] = Field(default_factory=dict)
    top_cwe_ids: list[str] = Field(default_factory=list)
    diagnostics: list[str] = Field(default_factory=list)


def build_enrichment_summary(bundle: NormalizationBundle) -> EnrichmentReportSummary:
    severity_counts = Counter(item.severity for item in bundle.findings)
    cwe_counts = Counter(cwe for finding in bundle.findings for cwe in finding.cwe_ids)
    trust_score = bundle.findings[0].trust_score if bundle.findings else 0.0
    stale_score = bundle.findings[0].staleness_score if bundle.findings else 0.0
    return EnrichmentReportSummary(
        source_name=bundle.source.source_name,
        tool=bundle.source.tool,
        trust_level=bundle.source.trust_level,
        trust_score=round(trust_score, 3),
        staleness_score=round(stale_score, 3),
        findings_total=len(bundle.findings),
        findings_by_severity=dict(sorted(severity_counts.items())),
        top_cwe_ids=[entry for entry, _count in cwe_counts.most_common(5)],
        diagnostics=list(bundle.diagnostics),
    )
