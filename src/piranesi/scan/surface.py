from __future__ import annotations

import json
import re
from collections import defaultdict
from collections.abc import Sequence
from contextlib import suppress
from dataclasses import dataclass, field
from pathlib import Path

from piranesi.detect.flows import extract_candidate_findings
from piranesi.models import (
    AttackSurfaceNode,
    CandidateFinding,
    EntryPoint,
    ScanMetadata,
    ScannedFunction,
    ScanResult,
)
from piranesi.models.taint import SourceLocation
from piranesi.scan.framework import detect_frameworks, discover_nextjs_routes
from piranesi.scan.joern import JoernServer
from piranesi.scan.queries import QueryNode, execute_json_query, execute_source_query
from piranesi.scan.specs import SanitizerSpec, SinkSpec, SourceSpec, get_source_specs
from piranesi.scan.transpile import SourceMap

_EXPRESS_ROUTE_CALLS_QUERY = (
    'cpg.call.name("get|post|put|delete|patch").filter(c => c.argument.size >= 2)'
)
_ENTRY_POINT_CALLS_QUERY = f"{_EXPRESS_ROUTE_CALLS_QUERY}.toJsonPretty"
_ENTRY_POINT_HANDLER_CODES_QUERY = (
    f"{_EXPRESS_ROUTE_CALLS_QUERY}.map(c => c.argument(2).code).toJsonPretty"
)
_ENTRY_POINT_ROUTE_PATTERNS_QUERY = (
    f"{_EXPRESS_ROUTE_CALLS_QUERY}.map(c => c.argument(1).code).toJsonPretty"
)
_METHOD_REFS_QUERY = (
    "cpg.methodRef.map(m => "
    'Map("code" -> m.code, "methodFullName" -> m.methodFullName)).toJsonPretty'
)
_CALL_GRAPH_QUERY = (
    'cpg.method.map(m => Map("method" -> m.fullName, '
    '"calls" -> m.callOut.map(c => c.methodFullName).dedup.l.mkString("||"))).toJsonPretty'
)
_FUNCTIONS_QUERY = (
    "cpg.method.map(m => Map("
    '"name" -> m.name, '
    '"fullName" -> m.fullName, '
    '"file" -> m.file.name.headOption.getOrElse(""), '
    '"lineNumber" -> m.lineNumber, '
    '"columnNumber" -> m.columnNumber, '
    '"code" -> m.code'
    ")).toJsonPretty"
)
_FILES_SCANNED_QUERY = "cpg.file.name.toJsonPretty"
_NEXTJS_HTTP_METHODS = frozenset({"GET", "HEAD", "OPTIONS", "POST", "PUT", "PATCH", "DELETE"})


class SurfaceMappingError(RuntimeError):
    """Raised when Joern returns an unexpected attack-surface payload."""


@dataclass(slots=True)
class _NodeResolver:
    server: JoernServer
    joern_project_root: Path
    source_map: SourceMap | None = None
    _file_cache: dict[int, Path | None] = field(default_factory=dict)
    _method_cache: dict[int, str | None] = field(default_factory=dict)
    _parameter_cache: dict[str, tuple[str, ...]] = field(default_factory=dict)

    def file_for(self, node: QueryNode) -> Path | None:
        if node.node_id < 0:
            return None
        if node.node_id not in self._file_cache:
            payload = execute_json_query(
                self.server,
                f"cpg.id({node.node_id}L).file.name.toJsonPretty",
            )
            self._file_cache[node.node_id] = _first_path(
                payload,
                joern_project_root=self.joern_project_root,
                source_map=self.source_map,
            )
        return self._file_cache[node.node_id]

    def method_for_call(self, node: QueryNode) -> str | None:
        if node.node_id < 0:
            return None
        if node.node_id not in self._method_cache:
            payload = execute_json_query(
                self.server,
                f"cpg.call.id({node.node_id}L).method.fullName.toJsonPretty",
            )
            self._method_cache[node.node_id] = _first_string(payload)
        return self._method_cache[node.node_id]

    def parameters_for_method(self, function_id: str) -> tuple[str, ...]:
        if function_id not in self._parameter_cache:
            payload = execute_json_query(
                self.server,
                f"cpg.method.fullNameExact({json.dumps(function_id)}).parameter.name.toJsonPretty",
            )
            raw_parameters = payload if isinstance(payload, list) else []
            self._parameter_cache[function_id] = tuple(
                parameter
                for parameter in raw_parameters
                if isinstance(parameter, str) and parameter and parameter != "this"
            )
        return self._parameter_cache[function_id]

    def location_for(self, node: QueryNode) -> SourceLocation:
        generated_file = self.file_for(node)
        line_number = node.line_number or 1
        resolved_file = generated_file
        resolved_line = line_number
        if self.source_map is not None and generated_file is not None:
            with suppress(KeyError):
                resolved_file, resolved_line = self.source_map.resolve(generated_file, line_number)

        return SourceLocation(
            file=str(resolved_file) if resolved_file is not None else "<unknown>",
            line=resolved_line,
            column=node.column_number or 0,
            snippet=node.code,
        )


@dataclass(frozen=True, slots=True)
class _MethodEntryPointSummary:
    name: str
    full_name: str
    file_name: str
    line_number: int | None
    column_number: int | None
    code: str


def build_scan_result(
    server: JoernServer,
    *,
    project_root: str | Path,
    metadata: ScanMetadata,
    joern_project_root: str | Path | None = None,
    source_map: SourceMap | None = None,
    source_specs: Sequence[SourceSpec] | None = None,
    sink_specs: Sequence[SinkSpec] | None = None,
    sanitizer_specs: Sequence[SanitizerSpec] | None = None,
    candidate_findings: Sequence[CandidateFinding] | None = None,
    frameworks: Sequence[str] | None = None,
) -> ScanResult:
    resolved_project_root = Path(project_root).resolve(strict=False)
    resolved_joern_root = (
        Path(joern_project_root).resolve(strict=False)
        if joern_project_root is not None
        else resolved_project_root
    )
    resolved_frameworks = tuple(frameworks or detect_frameworks(resolved_project_root))
    resolved_source_specs = tuple(source_specs or get_source_specs(frameworks=resolved_frameworks))
    resolver = _NodeResolver(
        server=server,
        joern_project_root=resolved_joern_root,
        source_map=source_map,
    )

    findings = tuple(
        candidate_findings
        or extract_candidate_findings(
            server,
            joern_project_root=resolved_joern_root,
            source_map=source_map,
            source_specs=resolved_source_specs,
            sink_specs=sink_specs,
            sanitizer_specs=sanitizer_specs,
            frameworks=resolved_frameworks,
        )
    )
    entry_points = extract_entry_points(
        server,
        resolver=resolver,
        project_root=resolved_project_root,
        frameworks=resolved_frameworks,
    )
    attack_surface = extract_attack_surface(
        server,
        resolver=resolver,
        source_specs=resolved_source_specs,
        entry_points=entry_points,
        candidate_findings=findings,
    )
    return ScanResult(
        project_root=str(resolved_project_root),
        files_scanned=collect_files_scanned(
            server,
            joern_project_root=resolved_joern_root,
            source_map=source_map,
        ),
        call_graph=collect_call_graph(server),
        functions=collect_functions(
            server,
            joern_project_root=resolved_joern_root,
            source_map=source_map,
        ),
        entry_points=entry_points,
        attack_surface=attack_surface,
        metadata=metadata,
    )


def collect_files_scanned(
    server: JoernServer,
    *,
    joern_project_root: Path,
    source_map: SourceMap | None,
) -> list[str]:
    payload = execute_json_query(server, _FILES_SCANNED_QUERY)
    if not isinstance(payload, list):
        raise SurfaceMappingError(f"Unexpected file listing payload: {payload!r}")

    files: list[str] = []
    for raw_name in payload:
        if not isinstance(raw_name, str) or not raw_name or raw_name.startswith("<"):
            continue
        resolved = _first_path(
            [raw_name],
            joern_project_root=joern_project_root,
            source_map=source_map,
        )
        if resolved is None:
            continue
        if source_map is not None:
            with suppress(KeyError):
                resolved, _ = source_map.resolve(resolved, 1)
        rendered = str(resolved)
        if rendered not in files:
            files.append(rendered)
    return files


def collect_call_graph(server: JoernServer) -> dict[str, list[str]]:
    payload = execute_json_query(server, _CALL_GRAPH_QUERY)
    if not isinstance(payload, list):
        raise SurfaceMappingError(f"Unexpected call graph payload: {payload!r}")

    call_graph: dict[str, list[str]] = {}
    for item in payload:
        if not isinstance(item, dict):
            raise SurfaceMappingError(f"Unexpected call graph entry: {item!r}")
        method = item.get("method")
        raw_calls = item.get("calls")
        if not isinstance(method, str) or not method:
            continue
        if not isinstance(raw_calls, str):
            call_graph[method] = []
            continue
        call_graph[method] = [call for call in raw_calls.split("||") if call]
    return call_graph


def collect_functions(
    server: JoernServer,
    *,
    joern_project_root: Path,
    source_map: SourceMap | None,
) -> list[ScannedFunction]:
    payload = execute_json_query(server, _FUNCTIONS_QUERY)
    if not isinstance(payload, list):
        raise SurfaceMappingError(f"Unexpected functions payload: {payload!r}")

    functions: list[ScannedFunction] = []
    seen_function_ids: set[str] = set()
    for item in payload:
        if not isinstance(item, dict):
            raise SurfaceMappingError(f"Unexpected function entry: {item!r}")
        function_id = item.get("fullName")
        function_name = item.get("name")
        file_name = item.get("file")
        if (
            not isinstance(function_id, str)
            or not function_id
            or not isinstance(function_name, str)
            or not function_name
            or not isinstance(file_name, str)
            or not file_name
        ):
            continue
        resolved_file = _first_path(
            [file_name],
            joern_project_root=joern_project_root,
            source_map=source_map,
        )
        if resolved_file is None:
            continue
        raw_line = item.get("lineNumber")
        resolved_line = raw_line if isinstance(raw_line, int) else 1
        if source_map is not None:
            with suppress(KeyError):
                resolved_file, resolved_line = source_map.resolve(resolved_file, resolved_line)
        raw_column = item.get("columnNumber")
        raw_code = item.get("code")
        resolved_column = raw_column if isinstance(raw_column, int) else 0
        resolved_snippet = raw_code if isinstance(raw_code, str) else function_name

        if function_id in seen_function_ids:
            continue
        functions.append(
            ScannedFunction(
                function_id=function_id,
                name=function_name,
                location=SourceLocation(
                    file=str(resolved_file),
                    line=resolved_line,
                    column=resolved_column,
                    snippet=resolved_snippet,
                ),
            )
        )
        seen_function_ids.add(function_id)
    return functions


def extract_entry_points(
    server: JoernServer,
    *,
    resolver: _NodeResolver,
    project_root: Path | None = None,
    frameworks: Sequence[str] | None = None,
) -> list[EntryPoint]:
    normalized_frameworks = {framework.lower() for framework in frameworks or ()}
    entry_points: list[EntryPoint] = []
    if not normalized_frameworks or normalized_frameworks & {"express", "fastify", "koa", "nestjs"}:
        entry_points.extend(_extract_express_entry_points(server, resolver=resolver))
    if project_root is not None and "nextjs" in normalized_frameworks:
        entry_points.extend(
            _extract_nextjs_entry_points(server, resolver=resolver, project_root=project_root)
        )
    return _dedupe_entry_points(entry_points)


def _extract_express_entry_points(
    server: JoernServer,
    *,
    resolver: _NodeResolver,
) -> list[EntryPoint]:
    raw_calls = execute_json_query(server, _ENTRY_POINT_CALLS_QUERY)
    handler_codes = execute_json_query(server, _ENTRY_POINT_HANDLER_CODES_QUERY)
    route_patterns = execute_json_query(server, _ENTRY_POINT_ROUTE_PATTERNS_QUERY)
    method_refs = execute_json_query(server, _METHOD_REFS_QUERY)

    if not isinstance(raw_calls, list) or not all(isinstance(item, dict) for item in raw_calls):
        raise SurfaceMappingError(f"Unexpected entry-point calls payload: {raw_calls!r}")
    if not isinstance(handler_codes, list) or not all(
        isinstance(item, str) for item in handler_codes
    ):
        raise SurfaceMappingError(f"Unexpected handler-code payload: {handler_codes!r}")
    if not isinstance(route_patterns, list) or not all(
        isinstance(item, str) for item in route_patterns
    ):
        raise SurfaceMappingError(f"Unexpected route-pattern payload: {route_patterns!r}")
    if not isinstance(method_refs, list) or not all(isinstance(item, dict) for item in method_refs):
        raise SurfaceMappingError(f"Unexpected method-ref payload: {method_refs!r}")
    if not (len(raw_calls) == len(handler_codes) == len(route_patterns)):
        raise SurfaceMappingError("Entry-point Joern queries returned mismatched result lengths")

    method_ref_lookup = {
        item["code"]: item["methodFullName"]
        for item in method_refs
        if isinstance(item.get("code"), str) and isinstance(item.get("methodFullName"), str)
    }

    entry_points: list[EntryPoint] = []
    for raw_call, handler_code, raw_route_pattern in zip(
        raw_calls,
        handler_codes,
        route_patterns,
        strict=True,
    ):
        call_node = QueryNode.from_json(raw_call)
        function_id = method_ref_lookup.get(
            handler_code,
            handler_code or call_node.method_full_name or call_node.code,
        )
        entry_points.append(
            EntryPoint(
                function_id=function_id,
                location=resolver.location_for(call_node),
                kind="route_handler",
                http_method=(call_node.name or "").upper() or None,
                route_pattern=_normalize_route_pattern(raw_route_pattern),
                parameters=list(resolver.parameters_for_method(function_id)) if function_id else [],
            )
        )
    return entry_points


def _extract_nextjs_entry_points(
    server: JoernServer,
    *,
    resolver: _NodeResolver,
    project_root: Path,
) -> list[EntryPoint]:
    entry_points: list[EntryPoint] = []

    for route in discover_nextjs_routes(project_root):
        transpiled_relative_file = (
            route.file.relative_to(project_root).with_suffix(".js").as_posix()
        )
        method_summaries = _query_methods_for_file(server, transpiled_relative_file)
        if route.kind == "pages_router":
            method_summary = _select_pages_router_method(method_summaries)
            if method_summary is None:
                continue
            entry_points.append(
                _entry_point_from_method_summary(
                    method_summary,
                    resolver=resolver,
                    kind="route_handler",
                    route_pattern=route.route_pattern,
                    http_method=None,
                )
            )
            continue

        if route.kind == "app_router":
            for method_summary in method_summaries:
                if method_summary.name.upper() not in _NEXTJS_HTTP_METHODS:
                    continue
                entry_points.append(
                    _entry_point_from_method_summary(
                        method_summary,
                        resolver=resolver,
                        kind="route_handler",
                        route_pattern=route.route_pattern,
                        http_method=method_summary.name.upper(),
                    )
                )
            continue

        if route.kind == "server_action":
            for method_summary in method_summaries:
                if method_summary.name == ":program":
                    continue
                entry_points.append(
                    _entry_point_from_method_summary(
                        method_summary,
                        resolver=resolver,
                        kind="server_action",
                        route_pattern=route.route_pattern,
                        http_method=None,
                    )
                )

    return entry_points


def extract_attack_surface(
    server: JoernServer,
    *,
    resolver: _NodeResolver,
    source_specs: Sequence[SourceSpec],
    entry_points: Sequence[EntryPoint],
    candidate_findings: Sequence[CandidateFinding],
) -> list[AttackSurfaceNode]:
    entry_point_ids = {entry_point.function_id for entry_point in entry_points}
    finding_index = _index_findings_by_source(candidate_findings)
    nodes_by_key: dict[tuple[str, str, int, str], AttackSurfaceNode] = {}

    for source_spec in source_specs:
        for node in execute_source_query(server, source_spec):
            function_id = resolver.method_for_call(node)
            if function_id is None or function_id not in entry_point_ids:
                continue

            location = resolver.location_for(node)
            attack_key = (function_id, location.file, location.line, source_spec.source_type.value)
            findings_for_source = finding_index.get(
                (location.file, location.line, location.column, source_spec.source_type.value),
                (),
            )
            data_flow_to = sorted(
                {finding.sink.api_name for finding in findings_for_source if finding.sink.api_name}
            )
            sanitizers_on_path = sorted(
                {
                    step.sanitizer_applied
                    for finding in findings_for_source
                    for step in finding.taint_path
                    if step.sanitizer_applied is not None
                }
            )
            nodes_by_key[attack_key] = AttackSurfaceNode(
                function_id=function_id,
                location=location,
                source_type=source_spec.source_type.value,
                data_flow_to=data_flow_to,
                sanitizers_on_path=sanitizers_on_path,
            )

    return list(nodes_by_key.values())


def _index_findings_by_source(
    candidate_findings: Sequence[CandidateFinding],
) -> dict[tuple[str, int, int, str], tuple[CandidateFinding, ...]]:
    indexed: defaultdict[tuple[str, int, int, str], list[CandidateFinding]] = defaultdict(list)
    for finding in candidate_findings:
        key = (
            finding.source.location.file,
            finding.source.location.line,
            finding.source.location.column,
            finding.source.source_type,
        )
        indexed[key].append(finding)
    return {key: tuple(values) for key, values in indexed.items()}


def _query_methods_for_file(
    server: JoernServer,
    transpiled_relative_file: str,
) -> tuple[_MethodEntryPointSummary, ...]:
    file_pattern = f".*{re.escape(transpiled_relative_file)}"
    payload = execute_json_query(
        server,
        "cpg.file.name("
        f"{json.dumps(file_pattern)}"
        ").method.map(m => Map("
        '"name" -> m.name, '
        '"fullName" -> m.fullName, '
        '"file" -> m.file.name.headOption.getOrElse(""), '
        '"lineNumber" -> m.lineNumber, '
        '"columnNumber" -> m.columnNumber, '
        '"code" -> m.code'
        ")).toJsonPretty",
    )
    if not isinstance(payload, list) or not all(isinstance(item, dict) for item in payload):
        raise SurfaceMappingError(f"Unexpected Next.js method payload: {payload!r}")

    summaries: list[_MethodEntryPointSummary] = []
    for item in payload:
        name = item.get("name")
        full_name = item.get("fullName")
        file_name = item.get("file")
        if (
            not isinstance(name, str)
            or not isinstance(full_name, str)
            or not isinstance(file_name, str)
        ):
            continue
        summaries.append(
            _MethodEntryPointSummary(
                name=name,
                full_name=full_name,
                file_name=file_name,
                line_number=item.get("lineNumber")
                if isinstance(item.get("lineNumber"), int)
                else None,
                column_number=item.get("columnNumber")
                if isinstance(item.get("columnNumber"), int)
                else None,
                code=item.get("code") if isinstance(item.get("code"), str) else name,
            )
        )
    return tuple(summaries)


def _select_pages_router_method(
    method_summaries: Sequence[_MethodEntryPointSummary],
) -> _MethodEntryPointSummary | None:
    candidates = [summary for summary in method_summaries if summary.name != ":program"]
    if not candidates:
        return None
    for preferred_name in ("handler", "default_1", "default"):
        for summary in candidates:
            if summary.name == preferred_name:
                return summary
    return candidates[0]


def _entry_point_from_method_summary(
    summary: _MethodEntryPointSummary,
    *,
    resolver: _NodeResolver,
    kind: str,
    route_pattern: str,
    http_method: str | None,
) -> EntryPoint:
    location = _location_for_method_summary(summary, resolver=resolver)
    return EntryPoint(
        function_id=summary.full_name,
        location=location,
        kind=kind,
        http_method=http_method,
        route_pattern=route_pattern,
        parameters=list(resolver.parameters_for_method(summary.full_name)),
    )


def _location_for_method_summary(
    summary: _MethodEntryPointSummary,
    *,
    resolver: _NodeResolver,
) -> SourceLocation:
    generated_file = _first_path(
        [summary.file_name],
        joern_project_root=resolver.joern_project_root,
        source_map=resolver.source_map,
    )
    resolved_file = generated_file
    resolved_line = summary.line_number or 1
    if resolver.source_map is not None and generated_file is not None:
        with suppress(KeyError):
            resolved_file, resolved_line = resolver.source_map.resolve(
                generated_file,
                summary.line_number or 1,
            )
    return SourceLocation(
        file=str(resolved_file) if resolved_file is not None else "<unknown>",
        line=resolved_line,
        column=summary.column_number or 0,
        snippet=summary.code,
    )


def _dedupe_entry_points(entry_points: Sequence[EntryPoint]) -> list[EntryPoint]:
    deduped: dict[tuple[str, str, str | None, str | None], EntryPoint] = {}
    for entry_point in entry_points:
        key = (
            entry_point.function_id,
            entry_point.kind,
            entry_point.http_method,
            entry_point.route_pattern,
        )
        deduped.setdefault(key, entry_point)
    return list(deduped.values())


def _first_string(payload: object) -> str | None:
    if not isinstance(payload, list) or not payload:
        return None
    first = payload[0]
    if not isinstance(first, str) or not first:
        return None
    return first


def _first_path(
    payload: object,
    *,
    joern_project_root: Path,
    source_map: SourceMap | None,
) -> Path | None:
    raw_name = _first_string(payload)
    if raw_name is None:
        return None
    candidate = Path(raw_name)
    if candidate.is_absolute():
        return candidate.resolve(strict=False)

    rooted_candidate = (joern_project_root / candidate).resolve(strict=False)
    if rooted_candidate.exists():
        return rooted_candidate

    if source_map is not None:
        generated_paths = tuple(source_map._generated_lines.keys())
        exact_name_matches = [path for path in generated_paths if path.name == candidate.name]
        if len(exact_name_matches) == 1:
            return exact_name_matches[0]

        suffix_matches = [path for path in generated_paths if str(path).endswith(raw_name)]
        if len(suffix_matches) == 1:
            return suffix_matches[0]

    return rooted_candidate


def _normalize_route_pattern(raw_route_pattern: str) -> str | None:
    normalized = raw_route_pattern.strip()
    if not normalized:
        return None
    if normalized in {"app", "router"}:
        return None
    if normalized.startswith(('"', "'")) and normalized.endswith(('"', "'")):
        return normalized[1:-1]
    return normalized


__all__ = [
    "SurfaceMappingError",
    "build_scan_result",
    "collect_call_graph",
    "collect_files_scanned",
    "collect_functions",
    "extract_attack_surface",
    "extract_entry_points",
]
