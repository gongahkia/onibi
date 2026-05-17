from __future__ import annotations

import json
from collections import defaultdict
from collections.abc import Mapping, Sequence
from dataclasses import asdict, dataclass, field
from hashlib import sha256
from pathlib import Path
from typing import Literal

from piranesi.models import AttackSurfaceNode, CandidateFinding, EntryPoint, ScannedFunction

ElementType = Literal["external_entity", "process", "data_store", "trust_boundary"]
DfdFormat = Literal["json", "mermaid"]


@dataclass
class DfdElement:
    id: str
    label: str
    element_type: ElementType
    metadata: dict[str, str] = field(default_factory=dict)


@dataclass
class DfdFlow:
    source_id: str
    target_id: str
    label: str
    is_tainted: bool = False
    crosses_trust_boundary: bool = False


@dataclass
class DfdTrustBoundary:
    id: str
    label: str
    element_ids: list[str] = field(default_factory=list)
    metadata: dict[str, str] = field(default_factory=dict)


@dataclass
class DfdDiagram:
    elements: list[DfdElement] = field(default_factory=list)
    flows: list[DfdFlow] = field(default_factory=list)
    trust_boundaries: list[DfdTrustBoundary] = field(default_factory=list)


def extract_dfd(
    *,
    findings: Sequence[CandidateFinding],
    entry_points: Sequence[EntryPoint] | None = None,
    attack_surface: Sequence[AttackSurfaceNode] | None = None,
    call_graph: Mapping[str, Sequence[str]] | None = None,
    functions: Sequence[ScannedFunction] | None = None,
    taint_overlay: bool = True,
) -> DfdDiagram:
    indexed_functions = {function.function_id: function for function in functions or ()}
    entry_points = tuple(entry_points or ())
    attack_surface = tuple(attack_surface or ())

    elements_by_id: dict[str, DfdElement] = {}
    flows_by_key: dict[tuple[str, str, str], DfdFlow] = {}
    zone_members: defaultdict[str, list[str]] = defaultdict(list)
    specific_boundaries: dict[str, DfdTrustBoundary] = {}

    for entry_point in entry_points:
        external = _upsert_element(
            elements_by_id,
            DfdElement(
                id=_stable_id("ext", entry_point.function_id),
                label=_external_entity_label(entry_point),
                element_type="external_entity",
                metadata={"zone": "Public"},
            ),
        )
        process = _upsert_element(
            elements_by_id,
            DfdElement(
                id=_stable_id("proc", entry_point.function_id),
                label=_entry_point_label(entry_point),
                element_type="process",
                metadata={
                    "zone": "Application",
                    "function_id": entry_point.function_id,
                    "route_pattern": entry_point.route_pattern or "",
                },
            ),
        )
        _add_zone_member(zone_members, "Public", external.id)
        _add_zone_member(zone_members, "Application", process.id)
        _upsert_flow(
            flows_by_key,
            DfdFlow(
                source_id=external.id,
                target_id=process.id,
                label=_entry_input_label(entry_point, attack_surface),
            ),
        )
        for boundary in _specific_boundaries_for_entry_point(entry_point, process.id):
            specific_boundaries.setdefault(boundary.id, boundary)

    for finding in findings:
        source_process = _ensure_source_process(
            finding,
            entry_points=entry_points,
            attack_surface=attack_surface,
            indexed_functions=indexed_functions,
            elements_by_id=elements_by_id,
            zone_members=zone_members,
            flows_by_key=flows_by_key,
            taint_overlay=taint_overlay,
        )
        current_process_id = source_process
        for step in finding.taint_path:
            if step.through_function is None:
                continue
            process = _upsert_element(
                elements_by_id,
                DfdElement(
                    id=_stable_id("proc", step.through_function),
                    label=_function_label(step.through_function, indexed_functions),
                    element_type="process",
                    metadata={"zone": "Application", "function_id": step.through_function},
                ),
            )
            _add_zone_member(zone_members, "Application", process.id)
            _upsert_flow(
                flows_by_key,
                DfdFlow(
                    source_id=current_process_id,
                    target_id=process.id,
                    label=step.operation,
                    is_tainted=taint_overlay,
                ),
            )
            current_process_id = process.id

        sink_element = _ensure_sink_element(
            finding,
            elements_by_id=elements_by_id,
            zone_members=zone_members,
        )
        _upsert_flow(
            flows_by_key,
            DfdFlow(
                source_id=current_process_id,
                target_id=sink_element.id,
                label=finding.sink.api_name or finding.sink.sink_type,
                is_tainted=taint_overlay,
            ),
        )
        if finding.metadata.get("cross_package"):
            boundary = DfdTrustBoundary(
                id=_stable_id("tb", f"service:{finding.id}"),
                label="Service Boundary",
                element_ids=[current_process_id],
                metadata={"kind": "service"},
            )
            specific_boundaries.setdefault(boundary.id, boundary)

    for caller, callees in (call_graph or {}).items():
        caller_id = _stable_id("proc", caller)
        if caller_id not in elements_by_id:
            continue
        for callee in callees:
            callee_element = _upsert_element(
                elements_by_id,
                DfdElement(
                    id=_stable_id("proc", callee),
                    label=_function_label(callee, indexed_functions),
                    element_type="process",
                    metadata={"zone": "Application", "function_id": callee},
                ),
            )
            _add_zone_member(zone_members, "Application", callee_element.id)
            _upsert_flow(
                flows_by_key,
                DfdFlow(
                    source_id=caller_id,
                    target_id=callee_element.id,
                    label="calls",
                ),
            )

    trust_boundaries = _build_trust_boundaries(zone_members, elements_by_id, specific_boundaries)
    boundary_zone_by_element = _boundary_zone_by_element_id(trust_boundaries)
    flows = [
        flow
        if flow.crosses_trust_boundary
        else DfdFlow(
            source_id=flow.source_id,
            target_id=flow.target_id,
            label=flow.label,
            is_tainted=flow.is_tainted,
            crosses_trust_boundary=boundary_zone_by_element.get(flow.source_id)
            != boundary_zone_by_element.get(flow.target_id),
        )
        for flow in flows_by_key.values()
    ]

    for boundary in trust_boundaries:
        _upsert_element(
            elements_by_id,
            DfdElement(
                id=boundary.id,
                label=boundary.label,
                element_type="trust_boundary",
                metadata=dict(boundary.metadata),
            ),
        )

    return DfdDiagram(
        elements=list(elements_by_id.values()),
        flows=flows,
        trust_boundaries=trust_boundaries,
    )


def render_dfd(dfd: DfdDiagram, *, format: DfdFormat = "mermaid") -> str:
    if format == "json":
        return json.dumps(asdict(dfd), indent=2)
    return _render_mermaid(dfd)


def _ensure_source_process(
    finding: CandidateFinding,
    *,
    entry_points: Sequence[EntryPoint],
    attack_surface: Sequence[AttackSurfaceNode],
    indexed_functions: Mapping[str, ScannedFunction],
    elements_by_id: dict[str, DfdElement],
    zone_members: defaultdict[str, list[str]],
    flows_by_key: dict[tuple[str, str, str], DfdFlow],
    taint_overlay: bool,
) -> str:
    matched_surface = _match_attack_surface(finding, attack_surface)
    if matched_surface is not None:
        process_id = _stable_id("proc", matched_surface.function_id)
        process = _upsert_element(
            elements_by_id,
            DfdElement(
                id=process_id,
                label=_function_label(matched_surface.function_id, indexed_functions),
                element_type="process",
                metadata={"zone": "Application", "function_id": matched_surface.function_id},
            ),
        )
        _add_zone_member(zone_members, "Application", process.id)
        for entry_point in entry_points:
            if entry_point.function_id == matched_surface.function_id:
                external_id = _stable_id("ext", entry_point.function_id)
                if external_id in elements_by_id:
                    _upsert_flow(
                        flows_by_key,
                        DfdFlow(
                            source_id=external_id,
                            target_id=process.id,
                            label=_entry_input_label(entry_point, attack_surface),
                            is_tainted=taint_overlay,
                        ),
                    )
                return process.id

    process = _upsert_element(
        elements_by_id,
        DfdElement(
            id=_stable_id("proc", finding.id),
            label=_source_process_label(finding, indexed_functions),
            element_type="process",
            metadata={"zone": "Application"},
        ),
    )
    external = _upsert_element(
        elements_by_id,
        DfdElement(
            id=_stable_id("ext", finding.id),
            label=_external_source_label(finding.source.source_type),
            element_type="external_entity",
            metadata={"zone": "Public"},
        ),
    )
    _add_zone_member(zone_members, "Application", process.id)
    _add_zone_member(zone_members, "Public", external.id)
    _upsert_flow(
        flows_by_key,
        DfdFlow(
            source_id=external.id,
            target_id=process.id,
            label=finding.source.source_type,
            is_tainted=taint_overlay,
        ),
    )
    return process.id


def _ensure_sink_element(
    finding: CandidateFinding,
    *,
    elements_by_id: dict[str, DfdElement],
    zone_members: defaultdict[str, list[str]],
) -> DfdElement:
    sink_type = finding.sink.sink_type.lower()
    if sink_type in {"sql_query", "nosql_query"}:
        element = _upsert_element(
            elements_by_id,
            DfdElement(
                id=_stable_id("ds", sink_type),
                label="Database" if sink_type == "sql_query" else "NoSQL Store",
                element_type="data_store",
                metadata={"zone": "Data"},
            ),
        )
        _add_zone_member(zone_members, "Data", element.id)
        return element
    if sink_type in {"file_read", "file_write"}:
        element = _upsert_element(
            elements_by_id,
            DfdElement(
                id=_stable_id("ds", "filesystem"),
                label="File System",
                element_type="data_store",
                metadata={"zone": "Data"},
            ),
        )
        _add_zone_member(zone_members, "Data", element.id)
        return element
    if sink_type == "dependency_vulnerability":
        element = _upsert_element(
            elements_by_id,
            DfdElement(
                id=_stable_id("ds", "dependency-registry"),
                label="Package Registry",
                element_type="data_store",
                metadata={"zone": "Data"},
            ),
        )
        _add_zone_member(zone_members, "Data", element.id)
        return element

    element = _upsert_element(
        elements_by_id,
        DfdElement(
            id=_stable_id("proc", f"{finding.id}:{sink_type}"),
            label=finding.sink.api_name or finding.sink.sink_type,
            element_type="process",
            metadata={"zone": "Application"},
        ),
    )
    _add_zone_member(zone_members, "Application", element.id)
    return element


def _specific_boundaries_for_entry_point(
    entry_point: EntryPoint,
    process_id: str,
) -> list[DfdTrustBoundary]:
    boundaries: list[DfdTrustBoundary] = []
    middleware = " ".join(entry_point.middleware).lower()
    if any(marker in middleware for marker in ("auth", "authenticate", "passport", "jwt", "guard")):
        boundaries.append(
            DfdTrustBoundary(
                id=_stable_id("tb", f"auth:{entry_point.function_id}"),
                label="Authentication Boundary",
                element_ids=[process_id],
                metadata={"kind": "auth"},
            )
        )
    if any(marker in middleware for marker in ("validate", "validator", "joi", "zod", "schema")):
        boundaries.append(
            DfdTrustBoundary(
                id=_stable_id("tb", f"validation:{entry_point.function_id}"),
                label="Validation Boundary",
                element_ids=[process_id],
                metadata={"kind": "validation"},
            )
        )
    route_pattern = (entry_point.route_pattern or "").lower()
    if route_pattern.startswith("/api/internal") or "/internal/" in route_pattern:
        boundaries.append(
            DfdTrustBoundary(
                id=_stable_id("tb", f"network:{entry_point.function_id}"),
                label="Internal Network Boundary",
                element_ids=[process_id],
                metadata={"kind": "network"},
            )
        )
    return boundaries


def _build_trust_boundaries(
    zone_members: Mapping[str, Sequence[str]],
    elements_by_id: Mapping[str, DfdElement],
    specific_boundaries: Mapping[str, DfdTrustBoundary],
) -> list[DfdTrustBoundary]:
    boundaries: list[DfdTrustBoundary] = []
    for zone in ("Public", "Application", "Data"):
        members = [
            element_id
            for element_id in dict.fromkeys(zone_members.get(zone, ()))
            if element_id in elements_by_id
        ]
        if not members:
            continue
        boundaries.append(
            DfdTrustBoundary(
                id=_stable_id("tb", f"zone:{zone}"),
                label=f"Trust Zone: {zone}",
                element_ids=members,
                metadata={"kind": "zone", "zone": zone},
            )
        )
    boundaries.extend(specific_boundaries.values())
    return boundaries


def _boundary_zone_by_element_id(
    boundaries: Sequence[DfdTrustBoundary],
) -> dict[str, str]:
    mapping: dict[str, str] = {}
    for boundary in boundaries:
        zone = boundary.metadata.get("zone")
        if zone is None:
            continue
        for element_id in boundary.element_ids:
            mapping.setdefault(element_id, zone)
    return mapping


def _upsert_element(
    elements_by_id: dict[str, DfdElement],
    element: DfdElement,
) -> DfdElement:
    existing = elements_by_id.get(element.id)
    if existing is None:
        elements_by_id[element.id] = element
        return element
    merged_metadata = {**existing.metadata, **element.metadata}
    merged = DfdElement(
        id=existing.id,
        label=existing.label or element.label,
        element_type=existing.element_type,
        metadata=merged_metadata,
    )
    elements_by_id[element.id] = merged
    return merged


def _upsert_flow(
    flows_by_key: dict[tuple[str, str, str], DfdFlow],
    flow: DfdFlow,
) -> None:
    key = (flow.source_id, flow.target_id, flow.label)
    existing = flows_by_key.get(key)
    if existing is None:
        flows_by_key[key] = flow
        return
    flows_by_key[key] = DfdFlow(
        source_id=existing.source_id,
        target_id=existing.target_id,
        label=existing.label,
        is_tainted=existing.is_tainted or flow.is_tainted,
        crosses_trust_boundary=existing.crosses_trust_boundary or flow.crosses_trust_boundary,
    )


def _match_attack_surface(
    finding: CandidateFinding,
    attack_surface: Sequence[AttackSurfaceNode],
) -> AttackSurfaceNode | None:
    for node in attack_surface:
        if (
            node.location.file == finding.source.location.file
            and node.location.line == finding.source.location.line
            and node.source_type == finding.source.source_type
        ):
            return node
    return None


def _entry_input_label(entry_point: EntryPoint, attack_surface: Sequence[AttackSurfaceNode]) -> str:
    for node in attack_surface:
        if node.function_id == entry_point.function_id:
            return node.source_type
    return "request"


def _external_entity_label(entry_point: EntryPoint) -> str:
    route_pattern = (entry_point.route_pattern or "").lower()
    if route_pattern.startswith("/ws") or entry_point.kind == "websocket_handler":
        return "WebSocket Client"
    return "HTTP Client"


def _entry_point_label(entry_point: EntryPoint) -> str:
    route = entry_point.route_pattern or entry_point.function_id
    method = f"{entry_point.http_method} " if entry_point.http_method else ""
    return f"{method}{route}".strip()


def _source_process_label(
    finding: CandidateFinding,
    indexed_functions: Mapping[str, ScannedFunction],
) -> str:
    source_function_id = finding.metadata.get("source_function_id")
    if isinstance(source_function_id, str):
        return _function_label(source_function_id, indexed_functions)
    file_name = Path(finding.source.location.file).name
    return f"{file_name}:{finding.source.location.line}"


def _external_source_label(source_type: str) -> str:
    normalized = source_type.lower()
    if normalized == "dependency_manifest":
        return "Package Registry"
    if normalized == "cli_argument":
        return "CLI User"
    if normalized in {"header", "cookie", "request_body", "request_param", "url_param"}:
        return "HTTP Client"
    return "External Actor"


def _function_label(
    function_id: str,
    indexed_functions: Mapping[str, ScannedFunction],
) -> str:
    function = indexed_functions.get(function_id)
    if function is not None:
        return function.name
    return function_id.rsplit(":", maxsplit=1)[-1]


def _render_mermaid(dfd: DfdDiagram) -> str:
    lines = ["graph LR"]
    rendered_nodes: set[str] = set()
    zone_boundaries = [
        boundary for boundary in dfd.trust_boundaries if boundary.metadata.get("kind") == "zone"
    ]

    def render_node(element: DfdElement) -> str:
        label = _escape_mermaid_label(element.label)
        if element.element_type == "data_store":
            return f'    {element.id}[("{label}")]'
        return f'    {element.id}["{label}"]'

    elements_by_id = {element.id: element for element in dfd.elements}
    for boundary in zone_boundaries:
        lines.append(f'    subgraph "{_escape_mermaid_label(boundary.label)}"')
        for element_id in boundary.element_ids:
            element = elements_by_id.get(element_id)
            if element is None or element.element_type == "trust_boundary":
                continue
            lines.append(render_node(element))
            rendered_nodes.add(element.id)
        lines.append("    end")

    for element in dfd.elements:
        if element.id in rendered_nodes or element.element_type == "trust_boundary":
            continue
        lines.append(render_node(element))

    tainted_indexes: list[int] = []
    boundary_indexes: list[int] = []
    for index, flow in enumerate(dfd.flows):
        label = _escape_mermaid_label(flow.label)
        lines.append(f'    {flow.source_id} -->|"{label}"| {flow.target_id}')
        if flow.is_tainted:
            tainted_indexes.append(index)
        elif flow.crosses_trust_boundary:
            boundary_indexes.append(index)

    for index in tainted_indexes:
        lines.append(f"    linkStyle {index} stroke:red,stroke-width:3px")
    for index in boundary_indexes:
        lines.append(f"    linkStyle {index} stroke:orange,stroke-width:2px")
    return "\n".join(lines)


def _stable_id(prefix: str, value: str) -> str:
    digest = sha256(value.encode("utf-8")).hexdigest()[:10]
    return f"{prefix}_{digest}"


def _add_zone_member(zone_members: defaultdict[str, list[str]], zone: str, element_id: str) -> None:
    if element_id not in zone_members[zone]:
        zone_members[zone].append(element_id)


def _escape_mermaid_label(value: str) -> str:
    return value.replace('"', '\\"')


__all__ = [
    "DfdDiagram",
    "DfdElement",
    "DfdFlow",
    "DfdTrustBoundary",
    "extract_dfd",
    "render_dfd",
]
