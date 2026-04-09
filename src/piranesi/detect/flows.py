from __future__ import annotations

import hashlib
import re
from collections.abc import Mapping, Sequence
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from piranesi.detect.categories import classify_candidate_findings
from piranesi.detect.conditions import ConditionExtractionError, PathConditionExtractor
from piranesi.models import (
    CandidateFinding,
    PathCondition,
    SourceLocation,
    TaintSink,
    TaintSource,
    TaintStep,
)
from piranesi.scan.joern import JoernServer
from piranesi.scan.queries import (
    CPGQLQueryError,
    QueryNode,
    build_flow_query,
    execute_json_query,
    execute_sanitizer_query,
)
from piranesi.scan.specs import (
    SanitizerSpec,
    SinkSpec,
    SinkType,
    SourceSpec,
    get_sanitizer_specs,
    get_sink_specs,
    get_source_specs,
)
from piranesi.scan.transpile import SourceMap

JsonDict = dict[str, Any]

_DEFAULT_DATA_CATEGORIES = ["unknown"]
_DEFAULT_CONFIDENCE = 0.7
_DEFAULT_SEVERITY = "medium"
_LOCATION_SEPARATOR = "|"
_FIELD_SEGMENT_PATTERN = re.compile(r"\.([A-Za-z_$][\w$]*)|\[['\"]([^'\"]+)['\"]\]")
_CALL_PREFIX_PATTERN = re.compile(r"^\s*(?:new\s+)?([^(]+?)\s*\(")
_SQL_PLACEHOLDER_PATTERN = re.compile(r"\$(?:\d+)\b|\?")
_MULTI_ARG_QUERY_PATTERN = re.compile(r"\bquery\s*\([^,]+,\s*(?:\[|[A-Za-z_$])", re.DOTALL)

_OPERATION_BY_NODE_TYPE = {
    "CALL": "call_arg",
    "IDENTIFIER": "assignment",
    "METHOD_PARAMETER_IN": "call_arg",
    "RETURN": "return",
    "FIELD_IDENTIFIER": "property_access",
    "LITERAL": "assignment",
}
_SEVERITY_BY_CWE = {
    "CWE-78": "critical",
    "CWE-89": "high",
    "CWE-94": "critical",
    "CWE-918": "high",
    "CWE-79": "medium",
    "CWE-22": "medium",
}


class FlowExtractionError(RuntimeError):
    """Raised when Joern returns an unexpected data-flow payload."""


@dataclass(slots=True)
class _NodeFileResolver:
    server: JoernServer
    joern_project_root: Path
    source_map: SourceMap | None = None
    _cache: dict[int, Path | None] = field(default_factory=dict)

    def resolve(self, node: QueryNode) -> Path | None:
        if node.node_id < 0:
            return None
        if node.node_id not in self._cache:
            self._cache[node.node_id] = self._lookup(node.node_id)
        return self._cache[node.node_id]

    def _lookup(self, node_id: int) -> Path | None:
        payload = execute_json_query(
            self.server,
            f"cpg.id({node_id}L).file.name.toJsonPretty",
        )
        if not isinstance(payload, list) or not payload:
            return None
        first = payload[0]
        if not isinstance(first, str) or not first:
            return None
        return _resolve_joern_file(
            first,
            joern_project_root=self.joern_project_root,
            source_map=self.source_map,
        )


def extract_candidate_findings(
    server: JoernServer,
    *,
    joern_project_root: str | Path,
    source_map: SourceMap | None = None,
    source_specs: Sequence[SourceSpec] | None = None,
    sink_specs: Sequence[SinkSpec] | None = None,
    sanitizer_specs: Sequence[SanitizerSpec] | None = None,
    route_patterns_by_finding_id: Mapping[str, str | None] | None = None,
    category_provider: Any | None = None,
    category_model: str | None = None,
) -> tuple[CandidateFinding, ...]:
    root = Path(joern_project_root).resolve(strict=False)
    resolved_source_specs = tuple(source_specs or get_source_specs())
    resolved_sink_specs = tuple(sink_specs or get_sink_specs())
    resolved_sanitizer_specs = tuple(sanitizer_specs or get_sanitizer_specs())
    sanitizer_lookup = _collect_sanitizer_lookup(server, resolved_sanitizer_specs)
    file_resolver = _NodeFileResolver(server=server, joern_project_root=root, source_map=source_map)
    condition_extractor = PathConditionExtractor(
        server,
        location_for_node=lambda node: _source_location_for_node(
            node,
            source_map=source_map,
            file_resolver=file_resolver,
        ),
    )

    findings: list[CandidateFinding] = []
    for source_spec in resolved_source_specs:
        for sink_spec in resolved_sink_specs:
            findings.extend(
                _extract_findings_for_pair(
                    server,
                    source_spec=source_spec,
                    sink_spec=sink_spec,
                    source_map=source_map,
                    file_resolver=file_resolver,
                    sanitizer_lookup=sanitizer_lookup,
                    condition_extractor=condition_extractor,
                )
            )
    return classify_candidate_findings(
        findings,
        route_patterns_by_finding_id=route_patterns_by_finding_id,
        provider=category_provider,
        model=category_model,
    )


def joern_flow_to_taint_steps(
    flow: Sequence[QueryNode],
    *,
    source_map: SourceMap | None,
    file_resolver: _NodeFileResolver,
    sanitizer_lookup: dict[int, str],
) -> list[TaintStep]:
    steps: list[TaintStep] = []
    active_sanitizer: str | None = None

    for node in flow:
        sanitizer_name = sanitizer_lookup.get(node.node_id)
        if sanitizer_name is not None:
            active_sanitizer = sanitizer_name

        location = _source_location_for_node(
            node,
            source_map=source_map,
            file_resolver=file_resolver,
        )
        steps.append(
            TaintStep(
                location=location,
                operation=classify_operation(node.node_type),
                taint_state="sanitized" if active_sanitizer is not None else "tainted",
                through_function=node.method_full_name,
                sanitizer_applied=active_sanitizer,
            )
        )

    return steps


def classify_operation(node_type: str) -> str:
    return _OPERATION_BY_NODE_TYPE.get(node_type, "assignment")


def severity_for_cwe(cwe_id: str | None) -> str:
    if cwe_id is None:
        return _DEFAULT_SEVERITY
    return _SEVERITY_BY_CWE.get(cwe_id, _DEFAULT_SEVERITY)


def candidate_finding_id(
    *,
    vuln_class: str,
    source_location: SourceLocation,
    sink_location: SourceLocation,
) -> str:
    material = _LOCATION_SEPARATOR.join(
        [
            vuln_class,
            _location_key(source_location),
            _location_key(sink_location),
        ]
    )
    return hashlib.sha256(material.encode("utf-8")).hexdigest()


def _extract_findings_for_pair(
    server: JoernServer,
    *,
    source_spec: SourceSpec,
    sink_spec: SinkSpec,
    source_map: SourceMap | None,
    file_resolver: _NodeFileResolver,
    sanitizer_lookup: dict[int, str],
    condition_extractor: PathConditionExtractor,
) -> list[CandidateFinding]:
    payload = execute_json_query(server, build_flow_query(source_spec, sink_spec))
    if not isinstance(payload, list):
        raise FlowExtractionError(
            "Expected list payload from Joern flow query for "
            f"{source_spec.name}->{sink_spec.name}, got {payload!r}"
        )

    findings: list[CandidateFinding] = []
    for raw_path in payload:
        if not isinstance(raw_path, dict):
            raise FlowExtractionError(f"Unexpected flow payload element: {raw_path!r}")
        raw_elements = raw_path.get("elements")
        if not isinstance(raw_elements, list) or not all(
            isinstance(node, dict) for node in raw_elements
        ):
            raise FlowExtractionError(f"Unexpected flow elements payload: {raw_path!r}")

        elements = tuple(QueryNode.from_json(node) for node in raw_elements)
        if not elements:
            continue
        if _path_contains_sanitizer(elements, sanitizer_lookup):
            continue

        source_node = elements[0]
        sink_node = elements[-1]
        source_location = _source_location_for_node(
            source_node,
            source_map=source_map,
            file_resolver=file_resolver,
        )
        sink_location = _source_location_for_node(
            sink_node,
            source_map=source_map,
            file_resolver=file_resolver,
        )
        if _is_parameterized_query_sink(elements, sink_spec):
            continue

        try:
            path_conditions = condition_extractor.extract(elements)
        except (ConditionExtractionError, CPGQLQueryError):
            path_conditions = []
        if _path_is_unreachable(path_conditions):
            continue

        vuln_class = sink_spec.cwe_id or sink_spec.sink_type.value
        findings.append(
            CandidateFinding(
                id=candidate_finding_id(
                    vuln_class=vuln_class,
                    source_location=source_location,
                    sink_location=sink_location,
                ),
                vuln_class=vuln_class,
                source=TaintSource(
                    location=source_location,
                    source_type=source_spec.source_type.value,
                    data_categories=list(_DEFAULT_DATA_CATEGORIES),
                    parameter_name=_extract_parameter_name(source_node.code),
                ),
                sink=TaintSink(
                    location=sink_location,
                    sink_type=sink_spec.sink_type.value,
                    api_name=_extract_api_name(sink_node),
                ),
                taint_path=joern_flow_to_taint_steps(
                    elements,
                    source_map=source_map,
                    file_resolver=file_resolver,
                    sanitizer_lookup=sanitizer_lookup,
                ),
                path_conditions=path_conditions,
                confidence=_DEFAULT_CONFIDENCE,
                severity=severity_for_cwe(sink_spec.cwe_id),
            )
        )

    return findings


def _path_contains_sanitizer(
    elements: Sequence[QueryNode],
    sanitizer_lookup: Mapping[int, str],
) -> bool:
    return any(node.node_id in sanitizer_lookup for node in elements if node.node_id >= 0)


def _is_parameterized_query_sink(
    elements: Sequence[QueryNode],
    sink_spec: SinkSpec,
) -> bool:
    if sink_spec.sink_type is not SinkType.SQL_QUERY or not elements:
        return False

    sink_node = elements[-1]
    code = sink_node.code
    return bool(
        code
        and _SQL_PLACEHOLDER_PATTERN.search(code)
        and _MULTI_ARG_QUERY_PATTERN.search(code)
    )


def _path_is_unreachable(path_conditions: Sequence[PathCondition]) -> bool:
    for condition in path_conditions:
        expression = condition.expression.strip().lower()
        if condition.required_value and expression == "false":
            return True
        if not condition.required_value and expression == "true":
            return True
    return False


def _collect_sanitizer_lookup(
    server: JoernServer,
    sanitizer_specs: Sequence[SanitizerSpec],
) -> dict[int, str]:
    sanitizer_lookup: dict[int, str] = {}
    for sanitizer_spec in sanitizer_specs:
        for node in execute_sanitizer_query(server, sanitizer_spec):
            if node.node_id < 0:
                continue
            sanitizer_lookup[node.node_id] = _sanitizer_name(node, fallback=sanitizer_spec.name)
    return sanitizer_lookup


def _source_location_for_node(
    node: QueryNode,
    *,
    source_map: SourceMap | None,
    file_resolver: _NodeFileResolver,
) -> SourceLocation:
    line_number = node.line_number or 1
    generated_file = file_resolver.resolve(node)
    resolved_file = generated_file
    resolved_line = line_number

    if source_map is not None and generated_file is not None:
        try:
            resolved_file, resolved_line = source_map.resolve(generated_file, line_number)
        except KeyError:
            resolved_file = generated_file
            resolved_line = line_number

    file_path = str(resolved_file) if resolved_file is not None else "<unknown>"
    return SourceLocation(
        file=file_path,
        line=resolved_line,
        column=node.column_number or 0,
        snippet=node.code,
    )


def _resolve_joern_file(
    raw_filename: str,
    *,
    joern_project_root: Path,
    source_map: SourceMap | None,
) -> Path:
    candidate = Path(raw_filename)
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

        suffix_matches = [path for path in generated_paths if str(path).endswith(raw_filename)]
        if len(suffix_matches) == 1:
            return suffix_matches[0]

    return rooted_candidate


def _extract_api_name(node: QueryNode) -> str:
    if node.name is not None and node.name != "<operator>.fieldAccess":
        if "." in node.code:
            prefix = _extract_api_prefix(node.code)
            if prefix is not None:
                return prefix
        return node.name

    prefix = _extract_api_prefix(node.code)
    if prefix is not None:
        return prefix
    return node.code


def _extract_api_prefix(code: str) -> str | None:
    match = _CALL_PREFIX_PATTERN.match(code)
    if match is None:
        return None
    return match.group(1).strip()


def _extract_parameter_name(code: str) -> str | None:
    matches = list(_FIELD_SEGMENT_PATTERN.finditer(code))
    if not matches:
        return None
    last_match = matches[-1]
    return last_match.group(1) or last_match.group(2)


def _sanitizer_name(node: QueryNode, *, fallback: str) -> str:
    if node.name and not node.name.startswith("<operator>"):
        return node.name
    prefix = _extract_api_prefix(node.code)
    if prefix is not None:
        return prefix
    return fallback


def _location_key(location: SourceLocation) -> str:
    return _LOCATION_SEPARATOR.join(
        [
            location.file,
            str(location.line),
            str(location.column),
        ]
    )


__all__ = [
    "FlowExtractionError",
    "candidate_finding_id",
    "classify_operation",
    "extract_candidate_findings",
    "joern_flow_to_taint_steps",
    "severity_for_cwe",
]
