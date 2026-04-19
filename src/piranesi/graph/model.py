from __future__ import annotations

from datetime import UTC, datetime
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

NodeType = Literal[
    "asset",
    "domain",
    "repo",
    "package",
    "dependency",
    "endpoint",
    "finding",
    "advisory",
]
EdgeType = Literal[
    "depends_on",
    "hosts",
    "calls",
    "affected_by",
    "mentions",
    "reachable_from",
]


class GraphNode(BaseModel):
    model_config = ConfigDict(extra="forbid")

    node_id: str
    node_type: NodeType
    label: str
    metadata: dict[str, object] = Field(default_factory=dict)


class GraphEdge(BaseModel):
    model_config = ConfigDict(extra="forbid")

    edge_id: str
    edge_type: EdgeType
    source: str
    target: str
    metadata: dict[str, object] = Field(default_factory=dict)


class IntelligenceGraph(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: str = "1.0"
    generated_at: str = Field(default_factory=lambda: datetime.now(UTC).isoformat())
    nodes: list[GraphNode] = Field(default_factory=list)
    edges: list[GraphEdge] = Field(default_factory=list)

    def upsert_node(self, node: GraphNode) -> None:
        for index, existing in enumerate(self.nodes):
            if existing.node_id == node.node_id:
                self.nodes[index] = node
                return
        self.nodes.append(node)

    def upsert_edge(self, edge: GraphEdge) -> None:
        for index, existing in enumerate(self.edges):
            if existing.edge_id == edge.edge_id:
                self.edges[index] = edge
                return
        self.edges.append(edge)

    def validate_edges(self) -> list[str]:
        ids = {node.node_id for node in self.nodes}
        failures: list[str] = []
        for edge in self.edges:
            if edge.source not in ids:
                failures.append(f"edge {edge.edge_id} source '{edge.source}' missing")
            if edge.target not in ids:
                failures.append(f"edge {edge.edge_id} target '{edge.target}' missing")
        return failures
