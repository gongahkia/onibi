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
    return f"({sink_spec.pattern}).reachableByFlows({source_spec.pattern}).toJsonPretty"


def execute_source_query(server: JoernServer, source_spec: SourceSpec) -> tuple[QueryNode, ...]:
    return _execute_nodes_query(server, source_spec.pattern)


def execute_sink_query(server: JoernServer, sink_spec: SinkSpec) -> tuple[QueryNode, ...]:
    return _execute_nodes_query(server, sink_spec.pattern)


def execute_sanitizer_query(
    server: JoernServer,
    sanitizer_spec: SanitizerSpec,
) -> tuple[QueryNode, ...]:
    return _execute_nodes_query(server, sanitizer_spec.pattern)


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
        flows.append(FlowPath(source_spec=source_spec, sink_spec=sink_spec, elements=elements))
    return tuple(flows)


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


def _extract_json_payload(stdout: str) -> str:
    triple_quoted_match = _TRIPLE_QUOTED_STRING_PATTERN.search(stdout)
    if triple_quoted_match is not None:
        return triple_quoted_match.group("payload")

    quoted_match = _QUOTED_STRING_PATTERN.search(stdout)
    if quoted_match is not None:
        encoded_payload = quoted_match.group("payload")
        return json.loads(f'"{encoded_payload}"')

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
]
