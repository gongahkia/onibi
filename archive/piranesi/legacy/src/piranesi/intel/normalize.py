from __future__ import annotations

from piranesi.adapters.models import AdapterParseResult
from piranesi.intel.schema import (
    IntelSourceProvenance,
    NormalizationBundle,
    NormalizedExternalFinding,
    normalized_finding_id,
)
from piranesi.intel.trust import source_quality_score


def normalize_adapter_result(
    *,
    parse_result: AdapterParseResult,
    source: IntelSourceProvenance,
) -> NormalizationBundle:
    normalized: list[NormalizedExternalFinding] = []

    quality, trust_score, stale_score = source_quality_score(
        trust_level=source.trust_level,
        collected_at=source.collected_at,
        stale_after_hours=source.stale_after_hours,
    )

    for raw in parse_result.findings:
        confidence = _normalize_confidence(raw.confidence, quality)
        normalized.append(
            NormalizedExternalFinding(
                finding_id=normalized_finding_id(raw, source.source_name),
                tool=raw.tool,
                source_name=source.source_name,
                external_id=raw.external_id,
                rule_id=raw.rule_id,
                title=raw.title,
                description=raw.description,
                severity=raw.severity,
                confidence=confidence,
                cwe_ids=list(raw.cwe_ids),
                category=raw.category,
                file_path=raw.file_path,
                line=raw.line,
                column=raw.column,
                package_name=raw.package_name,
                endpoint=raw.endpoint,
                provenance=source,
                trust_score=trust_score,
                staleness_score=stale_score,
                metadata={
                    **raw.metadata,
                    "source_quality_score": quality,
                },
            )
        )

    diagnostics = [f"{item.level}: {item.message}" for item in parse_result.diagnostics]
    return NormalizationBundle.create(source=source, findings=normalized, diagnostics=diagnostics)


def _normalize_confidence(raw_confidence: float | None, quality_score: float) -> float:
    if raw_confidence is None:
        return round(max(0.1, min(0.95, quality_score * 0.8)), 3)
    clamped = max(0.0, min(1.0, raw_confidence))
    return round((clamped * 0.75) + (quality_score * 0.25), 3)
