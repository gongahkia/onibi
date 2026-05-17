from __future__ import annotations

from pydantic import BaseModel, ConfigDict


class EnrichmentPrioritySignal(BaseModel):
    model_config = ConfigDict(extra="forbid")

    base_score: float
    adjustment: float
    adjusted_score: float
    trust_score: float
    staleness_score: float
    graph_boost: float
    rationale: str


def enrichment_priority_signal(
    *,
    base_score: float,
    trust_score: float,
    staleness_score: float,
    graph_boost: float = 0.0,
) -> EnrichmentPrioritySignal:
    bounded_graph = max(-5.0, min(5.0, graph_boost))
    quality_delta = ((trust_score - 0.5) * 10.0) + ((staleness_score - 0.5) * 6.0)
    raw_adjustment = quality_delta + bounded_graph
    adjustment = max(-10.0, min(10.0, raw_adjustment))
    adjusted = max(0.0, min(100.0, base_score + adjustment))
    rationale = (
        "enrichment affects ranking only; base evidence remains authoritative "
        f"(trust={trust_score:.2f}, stale={staleness_score:.2f}, graph={bounded_graph:.2f})"
    )
    return EnrichmentPrioritySignal(
        base_score=round(base_score, 3),
        adjustment=round(adjustment, 3),
        adjusted_score=round(adjusted, 3),
        trust_score=round(trust_score, 3),
        staleness_score=round(staleness_score, 3),
        graph_boost=round(bounded_graph, 3),
        rationale=rationale,
    )
