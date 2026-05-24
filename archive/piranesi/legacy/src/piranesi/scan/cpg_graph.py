from __future__ import annotations

import copy
import dataclasses
import hashlib
import json
from dataclasses import dataclass, field
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

from piranesi.models import CandidateFinding, ScannedFunction, ScanResult
from piranesi.models.taint import SourceLocation
from piranesi.scan.cpg_diff import ParsedFunction, function_body_hash, parse_functions_from_file

try:  # pragma: no cover - exercised through round-trip tests when available.
    import msgpack  # type: ignore[import-not-found]
except ImportError:  # pragma: no cover - fallback is covered.
    msgpack = None


@dataclass(frozen=True, slots=True)
class SinkReference:
    sink_name: str
    sink_type: str
    cwe_id: str
    confidence: float


@dataclass(frozen=True, slots=True)
class TaintSummary:
    param_to_return: dict[int, float] = field(default_factory=dict)
    param_to_sink: dict[int, list[SinkReference]] = field(default_factory=dict)
    return_tainted_by: list[str] = field(default_factory=list)


@dataclass(frozen=True, slots=True)
class CallEdge:
    caller_id: str
    callee_id: str
    call_site_line: int
    argument_mapping: dict[int, int] | None = None


@dataclass(frozen=True, slots=True)
class TaintFlowRecord:
    flow_id: str
    source_function_id: str
    sink_function_id: str
    source_spec: str
    sink_spec: str
    intermediate_functions: list[str]
    confidence: float
    finding_id: str | None = None
    source_parameter_name: str | None = None


@dataclass(slots=True)
class CPGFunction:
    function_id: str
    joern_function_id: str | None
    name: str
    file_path: str
    line_start: int
    line_end: int
    parameters: list[str]
    body_hash: str
    is_entry_point: bool
    source_type: str | None
    contains_sinks: list[str]
    taint_summary: TaintSummary | None = None

    def to_parsed_function(self) -> ParsedFunction:
        return ParsedFunction(
            function_id=self.function_id,
            name=self.name,
            relative_path=self.file_path,
            line_start=self.line_start,
            line_end=self.line_end,
            parameters=tuple(self.parameters),
            parameter_signature=",".join(self.parameters),
            body_hash=self.body_hash,
            source="",
            anonymous=self.name == "<anonymous>",
        )


@dataclass(slots=True)
class PiranesiCPG:
    version: str
    joern_version: str
    config_hash: str
    project_root: str
    functions: dict[str, CPGFunction]
    call_edges: list[CallEdge]
    taint_flows: list[TaintFlowRecord]
    file_hashes: dict[str, str]
    created_at: str
    updated_at: str
    last_accessed: str | None = None
    _callers_of: dict[str, set[str]] = field(default_factory=dict)
    _callees_of: dict[str, set[str]] = field(default_factory=dict)
    _functions_by_file: dict[str, set[str]] = field(default_factory=dict)
    _flows_through: dict[str, list[int]] = field(default_factory=dict)

    def __post_init__(self) -> None:
        self.rebuild_indexes()

    def rebuild_indexes(self) -> None:
        self._callers_of = {}
        self._callees_of = {}
        self._functions_by_file = {}
        self._flows_through = {}

        for function_id, function in self.functions.items():
            self._functions_by_file.setdefault(function.file_path, set()).add(function_id)
            self._callers_of.setdefault(function_id, set())
            self._callees_of.setdefault(function_id, set())

        for edge in self.call_edges:
            self._callers_of.setdefault(edge.callee_id, set()).add(edge.caller_id)
            self._callees_of.setdefault(edge.caller_id, set()).add(edge.callee_id)

        for index, flow in enumerate(self.taint_flows):
            participants = {
                flow.source_function_id,
                flow.sink_function_id,
                *flow.intermediate_functions,
            }
            for function_id in participants:
                self._flows_through.setdefault(function_id, []).append(index)

    def clone(self) -> PiranesiCPG:
        return copy.deepcopy(self)

    def touch(self) -> None:
        self.last_accessed = _utc_now()
        self.updated_at = self.last_accessed

    def functions_by_file(self, file_path: str) -> set[str]:
        return set(self._functions_by_file.get(file_path, set()))

    def callers_of(self, function_id: str) -> set[str]:
        return set(self._callers_of.get(function_id, set()))

    def callees_of(self, function_id: str) -> set[str]:
        return set(self._callees_of.get(function_id, set()))

    def as_call_graph(self) -> dict[str, list[str]]:
        stable_to_joern = {
            function_id: function.joern_function_id
            for function_id, function in self.functions.items()
            if function.joern_function_id
        }
        call_graph: dict[str, set[str]] = {}
        for edge in self.call_edges:
            caller = stable_to_joern.get(edge.caller_id)
            callee = stable_to_joern.get(edge.callee_id)
            if caller is None or callee is None:
                continue
            call_graph.setdefault(caller, set()).add(callee)
        return {caller: sorted(callees) for caller, callees in sorted(call_graph.items())}


def build_cpg_from_scan_result(
    scan_result: ScanResult,
    *,
    project_root: Path,
    piranesi_version: str,
    joern_version: str,
    config_hash: str,
    previous: PiranesiCPG | None = None,
) -> PiranesiCPG:
    root = project_root.resolve(strict=False)
    parsed_by_file = _parse_scanned_files(scan_result, root)
    functions: dict[str, CPGFunction] = {}
    joern_to_stable: dict[str, str] = {}
    entry_point_ids = {entry_point.function_id for entry_point in scan_result.entry_points}
    source_types: dict[str, str] = {}
    for attack_surface in scan_result.attack_surface:
        source_types.setdefault(attack_surface.function_id, attack_surface.source_type)

    for scanned_function in scan_result.functions:
        matched = _match_parsed_function(scanned_function, parsed_by_file, root)
        if matched is None:
            matched = _fallback_parsed_function(scanned_function, root)
        if matched is None:
            continue
        existing = previous.functions.get(matched.function_id) if previous is not None else None
        functions[matched.function_id] = CPGFunction(
            function_id=matched.function_id,
            joern_function_id=scanned_function.function_id,
            name=scanned_function.name,
            file_path=matched.relative_path,
            line_start=matched.line_start,
            line_end=matched.line_end,
            parameters=list(matched.parameters or tuple(scanned_function.parameters)),
            body_hash=matched.body_hash,
            is_entry_point=scanned_function.function_id in entry_point_ids,
            source_type=source_types.get(scanned_function.function_id),
            contains_sinks=list(existing.contains_sinks) if existing is not None else [],
            taint_summary=existing.taint_summary if existing is not None else None,
        )
        joern_to_stable[scanned_function.function_id] = matched.function_id

    call_edges = _call_edges_from_scan_result(scan_result, joern_to_stable, functions)
    file_hashes = _collect_file_hashes(root, scan_result)
    now = _utc_now()
    cpg = PiranesiCPG(
        version=piranesi_version,
        joern_version=joern_version,
        config_hash=config_hash,
        project_root=str(root),
        functions=functions,
        call_edges=call_edges,
        taint_flows=[],
        file_hashes=file_hashes,
        created_at=previous.created_at if previous is not None else now,
        updated_at=now,
        last_accessed=now,
    )
    return cpg


def preserve_analysis_from_previous(
    rebuilt: PiranesiCPG,
    previous: PiranesiCPG | None,
    *,
    invalidated_function_ids: set[str],
) -> PiranesiCPG:
    if previous is None:
        rebuilt.rebuild_indexes()
        return rebuilt

    rebuilt.taint_flows = [
        flow
        for flow in previous.taint_flows
        if flow.source_function_id in rebuilt.functions
        and flow.sink_function_id in rebuilt.functions
        and flow.source_function_id not in invalidated_function_ids
        and flow.sink_function_id not in invalidated_function_ids
        and not set(flow.intermediate_functions) & invalidated_function_ids
        and all(function_id in rebuilt.functions for function_id in flow.intermediate_functions)
    ]

    for function_id, function in rebuilt.functions.items():
        previous_function = previous.functions.get(function_id)
        if previous_function is None or function_id in invalidated_function_ids:
            function.contains_sinks = []
            function.taint_summary = None
            continue
        if previous_function.body_hash == function.body_hash:
            function.contains_sinks = list(previous_function.contains_sinks)
            function.taint_summary = previous_function.taint_summary
        else:
            function.contains_sinks = []
            function.taint_summary = None

    rebuilt.rebuild_indexes()
    return rebuilt


def invalidate_direct(
    cpg: PiranesiCPG,
    changed_ids: set[str],
    *,
    drop_edges: bool = True,
) -> None:
    if drop_edges:
        cpg.call_edges = [
            edge
            for edge in cpg.call_edges
            if edge.caller_id not in changed_ids and edge.callee_id not in changed_ids
        ]
    cpg.taint_flows = [
        flow
        for flow in cpg.taint_flows
        if flow.source_function_id not in changed_ids
        and flow.sink_function_id not in changed_ids
        and not set(flow.intermediate_functions) & changed_ids
    ]
    for function_id in changed_ids:
        function = cpg.functions.get(function_id)
        if function is None:
            continue
        function.contains_sinks = []
        function.taint_summary = None
    cpg.rebuild_indexes()


def invalidate_transitively(
    cpg: PiranesiCPG,
    changed_ids: set[str],
    *,
    max_depth: int = 3,
    include_callees: bool = True,
) -> set[str]:
    visited = set(changed_ids)
    frontier = [(function_id, 0) for function_id in changed_ids]
    invalidated = set(changed_ids)

    while frontier:
        function_id, depth = frontier.pop(0)
        if depth >= max_depth:
            continue
        neighbors = set(cpg.callers_of(function_id))
        if include_callees:
            neighbors.update(cpg.callees_of(function_id))
        for neighbor in neighbors:
            if neighbor in visited:
                continue
            visited.add(neighbor)
            invalidated.add(neighbor)
            frontier.append((neighbor, depth + 1))
    return invalidated


def apply_findings_to_cpg(
    cpg: PiranesiCPG,
    findings: list[CandidateFinding],
    *,
    affected_function_ids: set[str] | None = None,
) -> None:
    affected = set(affected_function_ids or set())
    if affected:
        cpg.taint_flows = [
            flow
            for flow in cpg.taint_flows
            if flow.source_function_id not in affected
            and flow.sink_function_id not in affected
            and not set(flow.intermediate_functions) & affected
        ]
        for function_id in affected:
            function = cpg.functions.get(function_id)
            if function is not None:
                function.contains_sinks = []
                function.taint_summary = None

    joern_to_stable = {
        function.joern_function_id: function_id
        for function_id, function in cpg.functions.items()
        if function.joern_function_id
    }
    flows: dict[str, TaintFlowRecord] = {}
    sinks_by_function: dict[str, set[str]] = {}
    for finding in findings:
        participants = _flow_function_ids(cpg, finding, joern_to_stable=joern_to_stable)
        if len(participants) < 2:
            continue
        if affected and not set(participants) & affected:
            continue
        source_function_id = participants[0]
        sink_function_id = participants[-1]
        intermediate = participants[1:-1]
        sink_name = finding.sink.api_name or finding.sink.sink_type
        flow_id = hashlib.sha256(
            "|".join(
                [
                    source_function_id,
                    sink_function_id,
                    finding.source.source_type,
                    sink_name,
                    ",".join(intermediate),
                ]
            ).encode("utf-8")
        ).hexdigest()
        flows[flow_id] = TaintFlowRecord(
            flow_id=flow_id,
            source_function_id=source_function_id,
            sink_function_id=sink_function_id,
            source_spec=finding.source.source_type,
            sink_spec=sink_name,
            intermediate_functions=intermediate,
            confidence=finding.confidence,
            finding_id=finding.id,
            source_parameter_name=finding.source.parameter_name,
        )
        sinks_by_function.setdefault(sink_function_id, set()).add(sink_name)

    cpg.taint_flows.extend(flows.values())
    target_function_ids = affected or set(cpg.functions)
    for function_id in target_function_ids:
        function = cpg.functions.get(function_id)
        if function is None:
            continue
        function.contains_sinks = sorted(sinks_by_function.get(function.function_id, set()))
    recompute_taint_summaries(cpg, function_ids=affected or None)
    cpg.rebuild_indexes()


def recompute_taint_summaries(
    cpg: PiranesiCPG,
    *,
    function_ids: set[str] | None = None,
) -> None:
    target_ids = set(function_ids or cpg.functions.keys())
    flows_by_source: dict[str, list[TaintFlowRecord]] = {}
    for flow in cpg.taint_flows:
        flows_by_source.setdefault(flow.source_function_id, []).append(flow)

    for function_id in target_ids:
        function = cpg.functions.get(function_id)
        if function is None:
            continue
        param_to_sink: dict[int, list[SinkReference]] = {}
        return_tainted_by: list[str] = []
        for flow in flows_by_source.get(function_id, []):
            sink_reference = SinkReference(
                sink_name=flow.sink_spec,
                sink_type=flow.sink_spec,
                cwe_id=flow.sink_spec,
                confidence=flow.confidence,
            )
            parameter_index = _parameter_index(function.parameters, flow.source_parameter_name)
            if parameter_index is not None:
                param_to_sink.setdefault(parameter_index, []).append(sink_reference)
            if flow.sink_function_id != function_id:
                return_tainted_by.append(flow.source_spec)
        function.taint_summary = TaintSummary(
            param_to_return={},
            param_to_sink={
                index: _dedupe_sink_references(references)
                for index, references in sorted(param_to_sink.items())
            },
            return_tainted_by=sorted(set(return_tainted_by)),
        )


def serialize_cpg(cpg: PiranesiCPG, path: Path) -> str:
    payload = _to_serializable(cpg)
    if msgpack is not None:
        raw = msgpack.packb(payload, use_bin_type=True)
    else:
        raw = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode("utf-8")
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(raw)
    checksum = hashlib.sha256(raw).hexdigest()
    return checksum


def deserialize_cpg(path: Path) -> PiranesiCPG:
    raw = path.read_bytes()
    if msgpack is not None:
        payload = msgpack.unpackb(raw, raw=False)
    else:
        payload = json.loads(raw.decode("utf-8"))
    return _from_serializable(payload)


def _to_serializable(cpg: PiranesiCPG) -> dict[str, Any]:
    payload = dataclasses.asdict(cpg)
    for derived_field in ("_callers_of", "_callees_of", "_functions_by_file", "_flows_through"):
        payload.pop(derived_field, None)
    return payload


def _from_serializable(payload: dict[str, Any]) -> PiranesiCPG:
    functions: dict[str, CPGFunction] = {}
    for key, value in dict(payload.get("functions", {})).items():
        function_payload = dict(value)
        taint_summary_payload = function_payload.get("taint_summary")
        if isinstance(taint_summary_payload, dict):
            param_to_sink = {
                int(index): [SinkReference(**reference) for reference in references]
                for index, references in dict(
                    taint_summary_payload.get("param_to_sink", {})
                ).items()
            }
            function_payload["taint_summary"] = TaintSummary(
                param_to_return={
                    int(index): float(confidence)
                    for index, confidence in dict(
                        taint_summary_payload.get("param_to_return", {})
                    ).items()
                },
                param_to_sink=param_to_sink,
                return_tainted_by=list(taint_summary_payload.get("return_tainted_by", [])),
            )
        functions[key] = CPGFunction(**function_payload)
    call_edges = [CallEdge(**item) for item in list(payload.get("call_edges", []))]
    taint_flows = [TaintFlowRecord(**item) for item in list(payload.get("taint_flows", []))]
    return PiranesiCPG(
        version=str(payload.get("version", "")),
        joern_version=str(payload.get("joern_version", "")),
        config_hash=str(payload.get("config_hash", "")),
        project_root=str(payload.get("project_root", "")),
        functions=functions,
        call_edges=call_edges,
        taint_flows=taint_flows,
        file_hashes={
            str(key): str(value) for key, value in dict(payload.get("file_hashes", {})).items()
        },
        created_at=str(payload.get("created_at", "")),
        updated_at=str(payload.get("updated_at", "")),
        last_accessed=str(payload.get("last_accessed"))
        if payload.get("last_accessed") is not None
        else None,
    )


def _parse_scanned_files(
    scan_result: ScanResult,
    project_root: Path,
) -> dict[str, tuple[ParsedFunction, ...]]:
    parsed: dict[str, tuple[ParsedFunction, ...]] = {}
    for file_name in scan_result.files_scanned:
        file_path = Path(file_name).resolve(strict=False)
        try:
            relative_path = file_path.relative_to(project_root).as_posix()
        except ValueError:
            continue
        parsed[relative_path], _ = parse_functions_from_file(file_path, project_root)
    return parsed


def _match_parsed_function(
    scanned_function: ScannedFunction,
    parsed_by_file: dict[str, tuple[ParsedFunction, ...]],
    project_root: Path,
) -> ParsedFunction | None:
    file_path = Path(scanned_function.location.file).resolve(strict=False)
    try:
        relative_path = file_path.relative_to(project_root).as_posix()
    except ValueError:
        return None

    candidates = parsed_by_file.get(relative_path, ())
    if not candidates:
        return None
    same_name = [candidate for candidate in candidates if candidate.name == scanned_function.name]
    line = scanned_function.location.line
    containing = [
        candidate for candidate in same_name if candidate.line_start <= line <= candidate.line_end
    ]
    if containing:
        containing.sort(key=lambda candidate: abs(candidate.line_start - line))
        return containing[0]
    if same_name:
        same_name.sort(key=lambda candidate: abs(candidate.line_start - line))
        return same_name[0]
    candidates_list = list(candidates)
    candidates_list.sort(key=lambda candidate: abs(candidate.line_start - line))
    return candidates_list[0]


def _fallback_parsed_function(
    scanned_function: ScannedFunction,
    project_root: Path,
) -> ParsedFunction | None:
    file_path = Path(scanned_function.location.file).resolve(strict=False)
    try:
        relative_path = file_path.relative_to(project_root).as_posix()
    except ValueError:
        return None
    params = tuple(scanned_function.parameters)
    return ParsedFunction(
        function_id=f"{relative_path}::{scanned_function.name}({','.join(params)})",
        name=scanned_function.name,
        relative_path=relative_path,
        line_start=scanned_function.location.line,
        line_end=scanned_function.location.line,
        parameters=params,
        parameter_signature=",".join(params),
        body_hash=function_body_hash(scanned_function.location.snippet),
        source=scanned_function.location.snippet,
        anonymous=False,
    )


def _call_edges_from_scan_result(
    scan_result: ScanResult,
    joern_to_stable: dict[str, str],
    functions: dict[str, CPGFunction],
) -> list[CallEdge]:
    line_by_stable = {
        function_id: function.line_start for function_id, function in functions.items()
    }
    edges: dict[tuple[str, str], CallEdge] = {}
    for caller_joern_id, callees in scan_result.call_graph.items():
        caller_id = joern_to_stable.get(caller_joern_id)
        if caller_id is None:
            continue
        for callee_joern_id in callees:
            callee_id = joern_to_stable.get(callee_joern_id)
            if callee_id is None:
                continue
            key = (caller_id, callee_id)
            edges.setdefault(
                key,
                CallEdge(
                    caller_id=caller_id,
                    callee_id=callee_id,
                    call_site_line=line_by_stable.get(caller_id, 0),
                    argument_mapping=None,
                ),
            )
    return list(edges.values())


def _collect_file_hashes(project_root: Path, scan_result: ScanResult) -> dict[str, str]:
    hashes: dict[str, str] = {}
    for file_name in scan_result.files_scanned:
        file_path = Path(file_name).resolve(strict=False)
        if not file_path.exists():
            continue
        try:
            relative_path = file_path.relative_to(project_root).as_posix()
        except ValueError:
            continue
        hashes[relative_path] = hashlib.sha256(file_path.read_bytes()).hexdigest()
    return hashes


def _flow_function_ids(
    cpg: PiranesiCPG,
    finding: CandidateFinding,
    *,
    joern_to_stable: dict[str, str],
) -> list[str]:
    ordered: list[str] = []

    def _append(function_id: str | None) -> None:
        if function_id is None:
            return
        if function_id not in cpg.functions:
            return
        if not ordered or ordered[-1] != function_id:
            ordered.append(function_id)

    _append(_function_for_location(cpg, finding.source.location))
    for step in finding.taint_path:
        stable_id = joern_to_stable.get(step.through_function) if step.through_function else None
        _append(stable_id or _function_for_location(cpg, step.location))
    _append(_function_for_location(cpg, finding.sink.location))
    return ordered


def _function_for_location(cpg: PiranesiCPG, location: SourceLocation) -> str | None:
    location_path = Path(location.file)
    project_root = Path(cpg.project_root)
    try:
        relative_path = location_path.resolve(strict=False).relative_to(project_root).as_posix()
    except ValueError:
        return None
    candidates = [
        cpg.functions[function_id]
        for function_id in cpg.functions_by_file(relative_path)
        if function_id in cpg.functions
    ]
    matches = [
        function
        for function in candidates
        if function.line_start <= location.line <= function.line_end
    ]
    if matches:
        matches.sort(
            key=lambda function: (function.line_end - function.line_start, function.line_start)
        )
        return matches[0].function_id
    return None


def _parameter_index(parameters: list[str], parameter_name: str | None) -> int | None:
    if parameter_name is None:
        return None
    try:
        return parameters.index(parameter_name)
    except ValueError:
        return None


def _dedupe_sink_references(references: list[SinkReference]) -> list[SinkReference]:
    deduped: dict[tuple[str, str, str], SinkReference] = {}
    for reference in references:
        deduped[(reference.sink_name, reference.sink_type, reference.cwe_id)] = reference
    return list(deduped.values())


def _utc_now() -> str:
    return datetime.now(UTC).isoformat()


__all__ = [
    "CPGFunction",
    "CallEdge",
    "PiranesiCPG",
    "SinkReference",
    "TaintFlowRecord",
    "TaintSummary",
    "apply_findings_to_cpg",
    "build_cpg_from_scan_result",
    "deserialize_cpg",
    "invalidate_direct",
    "invalidate_transitively",
    "preserve_analysis_from_previous",
    "recompute_taint_summaries",
    "serialize_cpg",
]
