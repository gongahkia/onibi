from __future__ import annotations

from hashlib import sha256

from piranesi.graph.model import GraphEdge, GraphNode, IntelligenceGraph
from piranesi.intel.schema import NormalizedExternalFinding


def build_graph_from_enrichment(
    *,
    source_name: str,
    findings: list[NormalizedExternalFinding],
) -> IntelligenceGraph:
    graph = IntelligenceGraph()

    asset_node_id = f"asset:{source_name}"
    graph.upsert_node(
        GraphNode(
            node_id=asset_node_id,
            node_type="asset",
            label=source_name,
        )
    )

    for finding in findings:
        finding_node_id = f"finding:{finding.finding_id}"
        graph.upsert_node(
            GraphNode(
                node_id=finding_node_id,
                node_type="finding",
                label=finding.title,
                metadata={
                    "severity": finding.severity,
                    "tool": finding.tool,
                    "trust_score": finding.trust_score,
                    "staleness_score": finding.staleness_score,
                },
            )
        )

        graph.upsert_edge(
            GraphEdge(
                edge_id=_edge_id(asset_node_id, finding_node_id, "affected_by"),
                edge_type="affected_by",
                source=asset_node_id,
                target=finding_node_id,
                metadata={"source": finding.source_name},
            )
        )

        if finding.package_name:
            pkg_node_id = f"package:{finding.package_name}"
            graph.upsert_node(
                GraphNode(
                    node_id=pkg_node_id,
                    node_type="package",
                    label=finding.package_name,
                )
            )
            graph.upsert_edge(
                GraphEdge(
                    edge_id=_edge_id(finding_node_id, pkg_node_id, "mentions"),
                    edge_type="mentions",
                    source=finding_node_id,
                    target=pkg_node_id,
                )
            )

        if finding.endpoint:
            endpoint_node_id = f"endpoint:{finding.endpoint}"
            graph.upsert_node(
                GraphNode(
                    node_id=endpoint_node_id,
                    node_type="endpoint",
                    label=finding.endpoint,
                )
            )
            graph.upsert_edge(
                GraphEdge(
                    edge_id=_edge_id(asset_node_id, endpoint_node_id, "hosts"),
                    edge_type="hosts",
                    source=asset_node_id,
                    target=endpoint_node_id,
                )
            )
            graph.upsert_edge(
                GraphEdge(
                    edge_id=_edge_id(endpoint_node_id, finding_node_id, "reachable_from"),
                    edge_type="reachable_from",
                    source=endpoint_node_id,
                    target=finding_node_id,
                )
            )

        for cwe_id in finding.cwe_ids:
            advisory_node_id = f"advisory:{cwe_id}"
            graph.upsert_node(
                GraphNode(
                    node_id=advisory_node_id,
                    node_type="advisory",
                    label=cwe_id,
                )
            )
            graph.upsert_edge(
                GraphEdge(
                    edge_id=_edge_id(finding_node_id, advisory_node_id, "mentions"),
                    edge_type="mentions",
                    source=finding_node_id,
                    target=advisory_node_id,
                )
            )

    return graph


def _edge_id(source: str, target: str, edge_type: str) -> str:
    payload = f"{source}|{target}|{edge_type}"
    return f"edge-{sha256(payload.encode('utf-8')).hexdigest()[:16]}"
