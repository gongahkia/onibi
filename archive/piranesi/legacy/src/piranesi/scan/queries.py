from __future__ import annotations

import json
import re
from collections.abc import Iterable, Sequence
from dataclasses import dataclass
from typing import Any

from piranesi.scan.joern import JoernServer
from piranesi.scan.specs import SanitizerSpec, SinkSpec, SourceSpec, get_sanitizer_specs

JsonDict = dict[str, Any]

_ANSI_ESCAPE_PATTERN = re.compile(r"\x1B\[[0-?]*[ -/]*[@-~]")
_TRIPLE_QUOTED_STRING_PATTERN = re.compile(r'=\s*"""(?P<payload>.*)"""\s*$', re.DOTALL)
_QUOTED_STRING_PATTERN = re.compile(r'=\s*"(?P<payload>(?:\\.|[^"\\])*)"\s*$', re.DOTALL)
_SSRF_PATH_SEGMENT_SINK_NAME = "ssrf_path_segment"
_SSRF_FULL_URL_SINK_NAME = "ssrf_full_url"
_PYTHON_SQL_SINK_NAME = "python_sql_execute"
_PYTHON_OS_SYSTEM_SINK_NAME = "python_os_system"
_PYTHON_SUBPROCESS_RUN_SINK_NAME = "python_subprocess_run"
_PYTHON_EVAL_SINK_NAME = "python_eval"
_PYTHON_PARAMETERIZED_QUERY_SANITIZER_NAME = "python_parameterized_query"
_PYTHON_SQL_SINK_PATTERN = (
    'cpg.call.name("execute|raw|extra").filter(c => '
    'c.code.contains("cursor.execute(") || '
    'c.code.contains(".objects.raw(") || '
    'c.code.contains(".objects.extra("))'
)
_PYTHON_OS_SYSTEM_SINK_PATTERN = (
    'cpg.call.name("system").filter(c => c.code.startsWith("os.system("))'
)
_PYTHON_SUBPROCESS_RUN_SINK_PATTERN = (
    'cpg.call.name("run").filter(c => '
    'c.code.startsWith("subprocess.run(") && '
    '(c.code.contains("shell = True") || c.code.contains("shell=True")))'
)
_PYTHON_EVAL_SINK_PATTERN = 'cpg.call.name("eval").filter(c => c.code.startsWith("eval("))'
_PYTHON_PARAMETERIZED_QUERY_SANITIZER_PATTERN = (
    'cpg.call.name("execute").filter(c => '
    'c.code.contains("cursor.execute(") && c.argument.size >= 2)'
)
_HARDCODED_BASE_URL_TEMPLATE_PATTERN = re.compile(
    r"^`https?://[^/$?`]+(?::\d+)?[/?#][^`]*\$\{[^}]+\}[^`]*`$"
)
_HARDCODED_BASE_URL_FORMAT_STRING_PATTERN = re.compile(
    r'^<operator>[.]formatString\("https?://[^/$?"]+(?::\d+)?[/?#][^"]*", .+\)$'
)
_DOTTED_CALLEE_PATTERN = re.compile(r"(?P<callee>[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)+)\s*\(")
_PARENT_CALL_QUERY_BY_NODE_TYPE = {
    "CALL": "cpg.call.id({node_id}L).astParent.toJsonPretty",
    "IDENTIFIER": "cpg.identifier.id({node_id}L).astParent.toJsonPretty",
    "LITERAL": "cpg.literal.id({node_id}L).astParent.toJsonPretty",
}
_SINK_NODES_PATTERN_OVERRIDES: dict[str, str] = {
    _PYTHON_SQL_SINK_NAME: _PYTHON_SQL_SINK_PATTERN,
    _PYTHON_OS_SYSTEM_SINK_NAME: _PYTHON_OS_SYSTEM_SINK_PATTERN,
    _PYTHON_SUBPROCESS_RUN_SINK_NAME: _PYTHON_SUBPROCESS_RUN_SINK_PATTERN,
    _PYTHON_EVAL_SINK_NAME: _PYTHON_EVAL_SINK_PATTERN,
}
_SINK_FLOW_PATTERN_OVERRIDES: dict[str, str] = {
    _PYTHON_SQL_SINK_NAME: f"{_PYTHON_SQL_SINK_PATTERN}.argument(1)",
    _PYTHON_SUBPROCESS_RUN_SINK_NAME: f"{_PYTHON_SUBPROCESS_RUN_SINK_PATTERN}.argument(1)",
}
_SANITIZER_NODES_PATTERN_OVERRIDES: dict[str, str] = {
    _PYTHON_PARAMETERIZED_QUERY_SANITIZER_NAME: _PYTHON_PARAMETERIZED_QUERY_SANITIZER_PATTERN,
}
_SINKS_USING_PARENT_CALL = frozenset(
    {
        _SSRF_FULL_URL_SINK_NAME,
        _SSRF_PATH_SEGMENT_SINK_NAME,
        _PYTHON_SQL_SINK_NAME,
        _PYTHON_SUBPROCESS_RUN_SINK_NAME,
    }
)


class CPGQLQueryError(RuntimeError):
    """Raised when Joern rejects a generated CPGQL query or returns invalid data."""


@dataclass(frozen=True, slots=True)
class QueryNode:
    node_id: int
    name: str | None
    code: str
    node_type: str
    line_number: int | None
    column_number: int | None
    method_full_name: str | None

    @classmethod
    def from_json(cls, payload: JsonDict) -> QueryNode:
        return cls(
            node_id=_coerce_int(payload.get("_id"), default=-1),
            name=_coerce_optional_str(payload.get("name")),
            code=_coerce_optional_str(payload.get("code")) or "",
            node_type=_coerce_optional_str(payload.get("_label")) or "",
            line_number=_coerce_optional_int(payload.get("lineNumber")),
            column_number=_coerce_optional_int(payload.get("columnNumber")),
            method_full_name=_coerce_optional_str(payload.get("methodFullName")),
        )


@dataclass(frozen=True, slots=True)
class FlowPath:
    source_spec: SourceSpec
    sink_spec: SinkSpec
    elements: tuple[QueryNode, ...]


def build_nodes_query(pattern: str) -> str:
    return f"({pattern}).toJsonPretty"


def build_flow_query(source_spec: SourceSpec, sink_spec: SinkSpec) -> str:
    sink_flow_pattern = _sink_flow_pattern(sink_spec)
    return f"({sink_flow_pattern}).reachableByFlows({source_spec.pattern}).toJsonPretty"


def execute_source_query(server: JoernServer, source_spec: SourceSpec) -> tuple[QueryNode, ...]:
    return _execute_nodes_query(server, source_spec.pattern)


def execute_sink_query(server: JoernServer, sink_spec: SinkSpec) -> tuple[QueryNode, ...]:
    nodes = _execute_nodes_query(server, _sink_nodes_pattern(sink_spec))
    return tuple(node for node in nodes if _node_matches_sink_receiver_constraints(node, sink_spec))


def execute_sanitizer_query(
    server: JoernServer,
    sanitizer_spec: SanitizerSpec,
) -> tuple[QueryNode, ...]:
    return _execute_nodes_query(server, _sanitizer_nodes_pattern(sanitizer_spec))


def execute_flow_query(
    server: JoernServer,
    source_spec: SourceSpec,
    sink_spec: SinkSpec,
    *,
    sanitizer_specs: Sequence[SanitizerSpec] | None = None,
) -> tuple[FlowPath, ...]:
    sanitizer_node_ids = collect_sanitizer_node_ids(server, sanitizer_specs=sanitizer_specs)
    return _execute_flow_query_with_sanitizers(
        server,
        source_spec,
        sink_spec,
        sanitizer_node_ids=sanitizer_node_ids,
    )


def find_flows(
    server: JoernServer,
    *,
    source_specs: Sequence[SourceSpec],
    sink_specs: Sequence[SinkSpec],
    sanitizer_specs: Sequence[SanitizerSpec] | None = None,
) -> tuple[FlowPath, ...]:
    sanitizer_node_ids = collect_sanitizer_node_ids(server, sanitizer_specs=sanitizer_specs)
    flows: list[FlowPath] = []
    for source_spec in source_specs:
        for sink_spec in sink_specs:
            flows.extend(
                _execute_flow_query_with_sanitizers(
                    server,
                    source_spec,
                    sink_spec,
                    sanitizer_node_ids=sanitizer_node_ids,
                )
            )
    return tuple(flows)


def collect_sanitizer_node_ids(
    server: JoernServer,
    *,
    sanitizer_specs: Sequence[SanitizerSpec] | None = None,
) -> frozenset[int]:
    resolved_specs = tuple(sanitizer_specs or get_sanitizer_specs())
    node_ids: set[int] = set()
    for sanitizer_spec in resolved_specs:
        if not sanitizer_spec.blocks_flow:
            continue
        for node in execute_sanitizer_query(server, sanitizer_spec):
            if node.node_id >= 0:
                node_ids.add(node.node_id)
    return frozenset(node_ids)


def _execute_nodes_query(server: JoernServer, pattern: str) -> tuple[QueryNode, ...]:
    payload = _ensure_list_of_dicts(
        execute_json_query(server, build_nodes_query(pattern)),
        cpgql=build_nodes_query(pattern),
    )
    return tuple(QueryNode.from_json(node) for node in payload)


def _execute_flow_query_with_sanitizers(
    server: JoernServer,
    source_spec: SourceSpec,
    sink_spec: SinkSpec,
    *,
    sanitizer_node_ids: frozenset[int],
) -> tuple[FlowPath, ...]:
    payload = _ensure_list_of_dicts(
        execute_json_query(server, build_flow_query(source_spec, sink_spec)),
        cpgql=build_flow_query(source_spec, sink_spec),
    )
    flows: list[FlowPath] = []
    for raw_path in payload:
        elements = tuple(QueryNode.from_json(node) for node in _read_path_elements(raw_path))
        if _path_contains_sanitizer(elements, sanitizer_node_ids):
            continue
        normalized_elements = normalize_flow_elements_for_sink_spec(server, sink_spec, elements)
        if not normalized_elements:
            continue
        if not _node_matches_sink_receiver_constraints(normalized_elements[-1], sink_spec):
            continue
        flows.append(
            FlowPath(
                source_spec=source_spec,
                sink_spec=sink_spec,
                elements=normalized_elements,
            )
        )
    return tuple(flows)


def normalize_flow_elements_for_sink_spec(
    server: JoernServer,
    sink_spec: SinkSpec,
    elements: Sequence[QueryNode],
) -> tuple[QueryNode, ...]:
    if not elements:
        return ()
    if sink_spec.name in {_SSRF_FULL_URL_SINK_NAME, _SSRF_PATH_SEGMENT_SINK_NAME}:
        is_path_segment = _flow_uses_hardcoded_base_template(elements)
        if sink_spec.name == _SSRF_FULL_URL_SINK_NAME and is_path_segment:
            return ()
        if sink_spec.name == _SSRF_PATH_SEGMENT_SINK_NAME and not is_path_segment:
            return ()

    if not _sink_requires_parent_call(sink_spec):
        return tuple(elements)

    parent_call = _resolve_parent_call_node(server, elements[-1])
    if parent_call is None:
        return ()
    return (*elements, parent_call)


def _sink_nodes_pattern(sink_spec: SinkSpec) -> str:
    return _SINK_NODES_PATTERN_OVERRIDES.get(sink_spec.name, sink_spec.pattern)


def _sink_flow_pattern(sink_spec: SinkSpec) -> str:
    return _SINK_FLOW_PATTERN_OVERRIDES.get(
        sink_spec.name,
        sink_spec.flow_pattern or _sink_nodes_pattern(sink_spec),
    )


def _sanitizer_nodes_pattern(sanitizer_spec: SanitizerSpec) -> str:
    return _SANITIZER_NODES_PATTERN_OVERRIDES.get(sanitizer_spec.name, sanitizer_spec.pattern)


def _sink_requires_parent_call(sink_spec: SinkSpec) -> bool:
    return sink_spec.flow_to_parent_call or sink_spec.name in _SINKS_USING_PARENT_CALL


def _node_matches_sink_receiver_constraints(node: QueryNode, sink_spec: SinkSpec) -> bool:
    if not sink_spec.include_receivers and not sink_spec.exclude_receivers:
        return True

    receiver = _extract_call_receiver(node.code)
    if sink_spec.include_receivers and not _receiver_matches_any(
        receiver,
        sink_spec.include_receivers,
    ):
        return False
    return not (
        sink_spec.exclude_receivers and _receiver_matches_any(receiver, sink_spec.exclude_receivers)
    )


def _extract_call_receiver(code: str) -> str | None:
    match = _DOTTED_CALLEE_PATTERN.search(code)
    if match is None:
        return None
    callee = match.group("callee")
    receiver, _, _method = callee.rpartition(".")
    return receiver or None


def _receiver_matches_any(receiver: str | None, patterns: Sequence[str]) -> bool:
    if receiver is None:
        return False
    normalized = receiver.strip()
    if not normalized:
        return False
    for pattern in patterns:
        candidate = pattern.strip()
        if not candidate:
            continue
        if normalized == candidate or normalized.startswith(f"{candidate}."):
            return True
    return False


def execute_json_query(server: JoernServer, cpgql: str) -> object:
    response = server.query(cpgql)
    if response.get("success") is not True:
        raise CPGQLQueryError(f"Joern query failed: {response}")

    stdout = _strip_ansi(str(response.get("stdout", ""))).strip()
    if _looks_like_joern_error(stdout):
        raise CPGQLQueryError(f"Joern rejected query {cpgql!r}: {stdout}")

    json_payload = _extract_json_payload(stdout)
    return json.loads(json_payload)


def _ensure_list_of_dicts(payload: object, *, cpgql: str) -> list[JsonDict]:
    if not isinstance(payload, list):
        raise CPGQLQueryError(f"Expected list payload from Joern query {cpgql!r}, got: {payload!r}")
    if not all(isinstance(item, dict) for item in payload):
        raise CPGQLQueryError(
            f"Expected list[dict] payload from Joern query {cpgql!r}, got: {payload!r}"
        )
    return [item for item in payload if isinstance(item, dict)]


def _resolve_parent_call_node(server: JoernServer, node: QueryNode) -> QueryNode | None:
    if node.node_id < 0:
        return None
    parent_query = _PARENT_CALL_QUERY_BY_NODE_TYPE.get(node.node_type)
    if parent_query is None:
        return None
    payload = execute_json_query(server, parent_query.format(node_id=node.node_id))
    if not isinstance(payload, list):
        return None
    for raw_parent in payload:
        if not isinstance(raw_parent, dict):
            continue
        parent = QueryNode.from_json(raw_parent)
        if parent.node_type == "CALL":
            return parent
    return None


def _flow_uses_hardcoded_base_template(elements: Sequence[QueryNode]) -> bool:
    return any(
        _HARDCODED_BASE_URL_TEMPLATE_PATTERN.match(node.code)
        or _HARDCODED_BASE_URL_FORMAT_STRING_PATTERN.match(node.code)
        for node in elements
        if node.code
    )


def _extract_json_payload(stdout: str) -> str:
    triple_quoted_match = _TRIPLE_QUOTED_STRING_PATTERN.search(stdout)
    if triple_quoted_match is not None:
        return triple_quoted_match.group("payload")

    quoted_match = _QUOTED_STRING_PATTERN.search(stdout)
    if quoted_match is not None:
        encoded_payload = quoted_match.group("payload")
        return str(json.loads(f'"{encoded_payload}"'))

    if stdout.startswith("[") and stdout.endswith("]"):
        return stdout

    raise CPGQLQueryError(f"Unable to extract JSON payload from Joern stdout: {stdout}")


def _looks_like_joern_error(stdout: str) -> bool:
    if stdout.startswith("-- "):
        return True
    return re.search(r"(^|\n)\d+ error[s]? found\s*$", stdout) is not None


def _path_contains_sanitizer(
    elements: Iterable[QueryNode],
    sanitizer_node_ids: frozenset[int],
) -> bool:
    if not sanitizer_node_ids:
        return False
    return any(element.node_id in sanitizer_node_ids for element in elements)


def _read_path_elements(payload: JsonDict) -> list[JsonDict]:
    raw_elements = payload.get("elements", [])
    if not isinstance(raw_elements, list) or not all(
        isinstance(node, dict) for node in raw_elements
    ):
        raise CPGQLQueryError(f"Unexpected path payload from Joern: {payload!r}")
    return [node for node in raw_elements if isinstance(node, dict)]


def _strip_ansi(value: str) -> str:
    return _ANSI_ESCAPE_PATTERN.sub("", value)


def _coerce_int(value: Any, *, default: int) -> int:
    coerced = _coerce_optional_int(value)
    return default if coerced is None else coerced


def _coerce_optional_int(value: Any) -> int | None:
    if value is None:
        return None
    if isinstance(value, int):
        return value
    if isinstance(value, str) and value.isdigit():
        return int(value)
    return None


def _coerce_optional_str(value: Any) -> str | None:
    if value is None:
        return None
    if isinstance(value, str):
        return value
    return str(value)


__all__ = [
    "CPGQLQueryError",
    "FlowPath",
    "QueryNode",
    "build_flow_query",
    "build_nodes_query",
    "collect_sanitizer_node_ids",
    "execute_flow_query",
    "execute_json_query",
    "execute_sanitizer_query",
    "execute_sink_query",
    "execute_source_query",
    "find_flows",
    "normalize_flow_elements_for_sink_spec",
]
