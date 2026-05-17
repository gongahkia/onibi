from __future__ import annotations

from pathlib import Path

from piranesi.adapters import parse_external_tool_file
from piranesi.graph import build_graph_from_enrichment
from piranesi.intel.normalize import normalize_adapter_result
from piranesi.intel.prioritize import enrichment_priority_signal
from piranesi.intel.schema import IntelSourceProvenance


def test_graph_builder_creates_nodes_and_valid_edges(fixtures_dir: Path) -> None:
    snapshot = fixtures_dir / "intel" / "sample-zap.json"
    parsed = parse_external_tool_file(tool="zap", input_path=snapshot)
    provenance = IntelSourceProvenance.from_snapshot(
        source_name="zap-snapshot",
        tool="zap",
        snapshot_path=snapshot,
        trust_level="trusted",
        stale_after_hours=24,
    )
    bundle = normalize_adapter_result(parse_result=parsed, source=provenance)

    graph = build_graph_from_enrichment(
        source_name=bundle.source.source_name,
        findings=list(bundle.findings),
    )

    assert len(graph.nodes) >= 3
    assert len(graph.edges) >= 2
    assert graph.validate_edges() == []


def test_enrichment_priority_signal_bounds_adjustment() -> None:
    signal = enrichment_priority_signal(
        base_score=72.0,
        trust_score=1.0,
        staleness_score=0.95,
        graph_boost=12.0,
    )

    assert signal.adjustment <= 10.0
    assert 0.0 <= signal.adjusted_score <= 100.0
    assert "base evidence remains authoritative" in signal.rationale
