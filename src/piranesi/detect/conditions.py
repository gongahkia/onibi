from __future__ import annotations

import ast
import json
import re
from collections.abc import Callable, Sequence
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from piranesi.models import PathCondition, SourceLocation
from piranesi.scan.joern import JoernServer
from piranesi.scan.queries import QueryNode, execute_json_query

JsonDict = dict[str, Any]
LocationResolver = Callable[[QueryNode], SourceLocation]

_VARIABLE_PATTERN = r"[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*|\[['\"][^'\"]+['\"]\])*"
_STRING_LITERAL_PATTERN = r"(?:\"(?:\\.|[^\"\\])*\"|'(?:\\.|[^'\\])*')"
_TYPEOF_PATTERN = re.compile(
    rf"^\s*typeof\s+(?P<var>{_VARIABLE_PATTERN})\s*(?P<op>===|==|!==|!=)\s*"
    rf"(?P<literal>{_STRING_LITERAL_PATTERN})\s*$"
)
_STRING_LENGTH_PATTERN = re.compile(
    rf"^\s*(?P<var>{_VARIABLE_PATTERN})\.length\s*(?P<op>===|==|!==|!=|>=|>|<=|<)\s*"
    r"(?P<n>-?\d+)\s*$"
)
_STRING_CONTAINS_PATTERN = re.compile(
    rf"^\s*(?P<neg>!)?(?P<var>{_VARIABLE_PATTERN})\.includes\("
    rf"(?P<substr>{_STRING_LITERAL_PATTERN})\)\s*$"
)
_STRING_EQ_PATTERN = re.compile(
    rf"^\s*(?P<var>{_VARIABLE_PATTERN})\s*(?P<op>===|==|!==|!=)\s*"
    rf"(?P<literal>{_STRING_LITERAL_PATTERN})\s*$"
)
_INT_BOUND_PATTERN = re.compile(
    rf"^\s*(?P<var>{_VARIABLE_PATTERN})\s*(?P<op>===|==|!==|!=|>=|>|<=|<)\s*"
    r"(?P<n>-?\d+)\s*$"
)
_IN_OPERATOR_PATTERN = re.compile(
    rf"^\s*(?P<neg>!)?\s*(?P<key>{_STRING_LITERAL_PATTERN})\s+in\s+"
    rf"(?P<var>{_VARIABLE_PATTERN})\s*$"
)
_ARRAY_ISARRAY_PATTERN = re.compile(
    rf"^\s*(?P<neg>!)?\s*Array\.isArray\(\s*(?P<var>{_VARIABLE_PATTERN})\s*\)\s*$"
)
_INSTANCEOF_PATTERN = re.compile(
    rf"^\s*(?P<var>{_VARIABLE_PATTERN})\s+instanceof\s+(?P<cls>[A-Za-z_$][\w$.]*)\s*$"
)
_NULL_CHECK_PATTERN = re.compile(
    rf"^\s*(?P<var>{_VARIABLE_PATTERN})\s*(?P<op>===|==|!==|!=)\s*"
    r"(?P<lit>null|undefined)\s*$"
)
_SWITCH_PATTERN = re.compile(r"^\s*switch\s*\((?P<expr>.+?)\)\s*$")
_CASE_PATTERN = re.compile(r"^\s*case\s+(?P<label>.+?)\s*:\s*$")
_DEFAULT_PATTERN = re.compile(r"^\s*default\s*:\s*$")
_OPERATOR_NAME_MAP = {
    ">": "gt",
    ">=": "gte",
    "<": "lt",
    "<=": "lte",
    "==": "eq",
    "===": "eq",
    "!=": "neq",
    "!==": "neq",
}
_NEGATED_EQUALITY_OPERATORS = {"!=", "!=="}


class ConditionExtractionError(RuntimeError):
    """Raised when Joern returns an unexpected control-flow payload."""


@dataclass(frozen=True, slots=True)
class _ControlSummary:
    control_id: int
    method_name: str | None
    method_full_name: str | None

    @classmethod
    def from_json(cls, payload: JsonDict) -> _ControlSummary:
        return cls(
            control_id=_coerce_int(payload.get("_id"), default=-1),
            method_name=_coerce_optional_str(payload.get("methodName")),
            method_full_name=_coerce_optional_str(payload.get("methodFullName")),
        )


@dataclass(frozen=True, slots=True)
class _ControlStructure:
    control_id: int
    code: str
    condition_code: str
    control_type: str | None
    line_number: int | None
    column_number: int | None

    @classmethod
    def from_json(cls, payload: JsonDict) -> _ControlStructure:
        return cls(
            control_id=_coerce_int(payload.get("_id"), default=-1),
            code=_coerce_optional_str(payload.get("code")) or "",
            condition_code=_coerce_optional_str(payload.get("condition")) or "",
            control_type=_coerce_optional_str(payload.get("controlStructureType")),
            line_number=_coerce_optional_int(payload.get("lineNumber")),
            column_number=_coerce_optional_int(payload.get("columnNumber")),
        )

    def to_query_node(self) -> QueryNode:
        return QueryNode(
            node_id=self.control_id,
            name=None,
            code=self.code,
            node_type="CONTROL_STRUCTURE",
            line_number=self.line_number,
            column_number=self.column_number,
            method_full_name=None,
        )


@dataclass(frozen=True, slots=True)
class _ParsedConstraint:
    condition_type: str
    symbolic_constraint: str
    required_value: bool


@dataclass(frozen=True, slots=True)
class _ResolvedBranchCondition:
    expression: str
    location: SourceLocation
    required_value: bool


@dataclass(slots=True)
class _ControlOccurrence:
    control_id: int
    first_step_index: int
    method_name: str | None = None
    method_full_name: str | None = None
    nodes: list[QueryNode] = field(default_factory=list)


class PathConditionExtractor:
    """Extract path conditions for a Joern data-flow path."""

    def __init__(
        self,
        server: JoernServer,
        *,
        location_for_node: LocationResolver,
    ) -> None:
        self._server = server
        self._location_for_node = location_for_node
        self._controls_for_node_cache: dict[int, tuple[_ControlSummary, ...]] = {}
        self._controls_for_method_cache: dict[
            tuple[str | None, str | None],
            dict[int, _ControlStructure],
        ] = {}
        self._control_by_id_cache: dict[int, _ControlStructure | None] = {}
        self._branch_ast_cache: dict[tuple[int, int], frozenset[int]] = {}
        self._source_lines_cache: dict[Path, tuple[str, ...]] = {}

    def extract(self, flow: Sequence[QueryNode]) -> list[PathCondition]:
        occurrences = self._collect_occurrences(flow)
        if not occurrences:
            return []

        resolved: list[tuple[_ControlOccurrence, _ControlStructure]] = []
        for occurrence in occurrences.values():
            control = self._resolve_control_structure(
                occurrence.control_id,
                method_name=occurrence.method_name,
                method_full_name=occurrence.method_full_name,
            )
            if control is None:
                continue
            resolved.append((occurrence, control))

        resolved.sort(
            key=lambda item: (
                item[0].first_step_index,
                item[1].line_number if item[1].line_number is not None else 10**9,
                item[1].control_id,
            )
        )

        return [
            self._build_path_condition(control, occurrence.nodes)
            for occurrence, control in resolved
        ]

    def _collect_occurrences(self, flow: Sequence[QueryNode]) -> dict[int, _ControlOccurrence]:
        occurrences: dict[int, _ControlOccurrence] = {}
        for step_index, node in enumerate(flow):
            if node.node_id < 0:
                continue
            for summary in self._controls_for_node(node):
                if summary.control_id < 0:
                    continue
                occurrence = occurrences.get(summary.control_id)
                if occurrence is None:
                    occurrence = _ControlOccurrence(
                        control_id=summary.control_id,
                        first_step_index=step_index,
                        method_name=summary.method_name,
                        method_full_name=summary.method_full_name,
                    )
                    occurrences[summary.control_id] = occurrence
                occurrence.nodes.append(node)
                occurrence.method_name = occurrence.method_name or summary.method_name
                occurrence.method_full_name = (
                    occurrence.method_full_name or summary.method_full_name
                )
        return occurrences

    def _controls_for_node(self, node: QueryNode) -> tuple[_ControlSummary, ...]:
        if node.node_id in self._controls_for_node_cache:
            return self._controls_for_node_cache[node.node_id]

        query = _controlled_by_query(node)
        if query is None:
            self._controls_for_node_cache[node.node_id] = ()
            return ()

        payload = execute_json_query(self._server, query)
        if not isinstance(payload, list) or not all(isinstance(item, dict) for item in payload):
            raise ConditionExtractionError(
                f"Unexpected controlledBy payload for node {node.node_id}: {payload!r}"
            )
        controls = tuple(_ControlSummary.from_json(item) for item in payload)
        self._controls_for_node_cache[node.node_id] = controls
        return controls

    def _resolve_control_structure(
        self,
        control_id: int,
        *,
        method_name: str | None,
        method_full_name: str | None,
    ) -> _ControlStructure | None:
        if control_id in self._control_by_id_cache:
            return self._control_by_id_cache[control_id]

        by_method = self._controls_for_method(
            method_name=method_name,
            method_full_name=method_full_name,
        )
        if control_id in by_method:
            control = by_method[control_id]
            self._control_by_id_cache[control_id] = control
            return control

        payload = execute_json_query(self._server, _control_by_id_query(control_id))
        if not isinstance(payload, list) or not all(isinstance(item, dict) for item in payload):
            raise ConditionExtractionError(
                f"Unexpected control structure payload for control {control_id}: {payload!r}"
            )
        resolved: _ControlStructure | None = (
            _ControlStructure.from_json(payload[0]) if payload else None
        )
        self._control_by_id_cache[control_id] = resolved
        return resolved

    def _controls_for_method(
        self,
        *,
        method_name: str | None,
        method_full_name: str | None,
    ) -> dict[int, _ControlStructure]:
        cache_key = (method_name, method_full_name)
        if cache_key in self._controls_for_method_cache:
            return self._controls_for_method_cache[cache_key]

        if method_name is None and method_full_name is None:
            self._controls_for_method_cache[cache_key] = {}
            return {}

        payload = execute_json_query(
            self._server,
            _method_controls_query(method_name=method_name, method_full_name=method_full_name),
        )
        if not isinstance(payload, list) or not all(isinstance(item, dict) for item in payload):
            raise ConditionExtractionError(
                "Unexpected method control payload for "
                f"{method_full_name or method_name}: {payload!r}"
            )

        controls = {
            control.control_id: control
            for control in (_ControlStructure.from_json(item) for item in payload)
            if control.control_id >= 0
        }
        self._controls_for_method_cache[cache_key] = controls
        return controls

    def _build_path_condition(
        self,
        control: _ControlStructure,
        affected_nodes: Sequence[QueryNode],
    ) -> PathCondition:
        resolved = self._resolve_branch_condition(control, affected_nodes)
        return parse_condition_text(
            resolved.expression,
            location=resolved.location,
            required_value=resolved.required_value,
        )

    def _resolve_branch_condition(
        self,
        control: _ControlStructure,
        affected_nodes: Sequence[QueryNode],
    ) -> _ResolvedBranchCondition:
        if _is_switch(control):
            switch_condition = self._resolve_switch_case(control, affected_nodes)
            if switch_condition is not None:
                return switch_condition

        expression = control.condition_code.strip() or control.code.strip()
        location = self._location_for_node(control.to_query_node())
        affected_node_ids = {node.node_id for node in affected_nodes if node.node_id >= 0}
        true_ids = self._branch_ast_ids(control.control_id, order=2)
        false_ids = self._branch_ast_ids(control.control_id, order=3)
        required_value = True
        if affected_node_ids & false_ids and not affected_node_ids & true_ids:
            required_value = False
        return _ResolvedBranchCondition(
            expression=expression,
            location=location,
            required_value=required_value,
        )

    def _branch_ast_ids(self, control_id: int, *, order: int) -> frozenset[int]:
        cache_key = (control_id, order)
        if cache_key in self._branch_ast_cache:
            return self._branch_ast_cache[cache_key]

        payload = execute_json_query(self._server, _branch_ast_ids_query(control_id, order))
        if not isinstance(payload, list):
            raise ConditionExtractionError(
                f"Unexpected branch AST payload for control {control_id} order {order}: {payload!r}"
            )
        node_ids = frozenset(_coerce_int(item, default=-1) for item in payload)
        cleaned = frozenset(node_id for node_id in node_ids if node_id >= 0)
        self._branch_ast_cache[cache_key] = cleaned
        return cleaned

    def _resolve_switch_case(
        self,
        control: _ControlStructure,
        affected_nodes: Sequence[QueryNode],
    ) -> _ResolvedBranchCondition | None:
        location = self._location_for_node(control.to_query_node())
        if not location.file or location.file.startswith("<"):
            return None

        source_path = Path(location.file)
        if not source_path.exists():
            return None

        switch_expr = control.condition_code.strip() or _switch_expression_from_code(control.code)
        if not switch_expr:
            return None

        target_node = next(iter(affected_nodes), None)
        if target_node is None:
            return None
        target_line = self._location_for_node(target_node).line

        branch = _find_switch_branch(
            self._source_lines(source_path),
            switch_line=location.line,
            target_line=target_line,
        )
        if branch is None:
            return None

        if branch.label is None:
            return _ResolvedBranchCondition(
                expression=f"default({switch_expr})",
                location=SourceLocation(
                    file=location.file,
                    line=branch.label_line,
                    column=0,
                    snippet=f"default({switch_expr})",
                ),
                required_value=False,
            )

        expression = f"{switch_expr} === {branch.label}"
        return _ResolvedBranchCondition(
            expression=expression,
            location=SourceLocation(
                file=location.file,
                line=branch.label_line,
                column=0,
                snippet=expression,
            ),
            required_value=True,
        )

    def _source_lines(self, source_path: Path) -> tuple[str, ...]:
        if source_path not in self._source_lines_cache:
            self._source_lines_cache[source_path] = tuple(source_path.read_text().splitlines())
        return self._source_lines_cache[source_path]


def extract_path_conditions(
    server: JoernServer,
    flow: Sequence[QueryNode],
    *,
    location_for_node: LocationResolver,
) -> list[PathCondition]:
    return PathConditionExtractor(server, location_for_node=location_for_node).extract(flow)


def parse_condition_text(
    expression: str,
    *,
    location: SourceLocation,
    required_value: bool,
) -> PathCondition:
    parsed = _parse_constraint(expression, required_value=required_value)
    if parsed is None:
        return PathCondition(
            location=location,
            condition_type="branch",
            expression=expression,
            required_value=required_value,
            symbolic_constraint=None,
        )

    return PathCondition(
        location=location,
        condition_type=parsed.condition_type,
        expression=expression,
        required_value=parsed.required_value,
        symbolic_constraint=parsed.symbolic_constraint,
    )


@dataclass(frozen=True, slots=True)
class _SwitchBranch:
    label: str | None
    label_line: int


def _parse_constraint(expression: str, *, required_value: bool) -> _ParsedConstraint | None:
    typeof_match = _TYPEOF_PATTERN.match(expression)
    if typeof_match is not None:
        type_name = _decode_string_literal(typeof_match.group("literal"))
        normalized_required = required_value
        if typeof_match.group("op") in _NEGATED_EQUALITY_OPERATORS:
            normalized_required = not normalized_required
        return _ParsedConstraint(
            condition_type="type_check",
            symbolic_constraint=_symbolic_call(
                "TypeCheck",
                var=typeof_match.group("var"),
                type=type_name,
            ),
            required_value=normalized_required,
        )

    length_match = _STRING_LENGTH_PATTERN.match(expression)
    if length_match is not None:
        return _ParsedConstraint(
            condition_type="string_length",
            symbolic_constraint=_symbolic_call(
                "StringLength",
                var=length_match.group("var"),
                op=_operator_name(length_match.group("op")),
                n=int(length_match.group("n")),
            ),
            required_value=required_value,
        )

    contains_match = _STRING_CONTAINS_PATTERN.match(expression)
    if contains_match is not None:
        normalized_required = required_value
        if contains_match.group("neg") is not None:
            normalized_required = not normalized_required
        return _ParsedConstraint(
            condition_type="string_contains",
            symbolic_constraint=_symbolic_call(
                "StringContains",
                var=contains_match.group("var"),
                substr=_decode_string_literal(contains_match.group("substr")),
            ),
            required_value=normalized_required,
        )

    string_eq_match = _STRING_EQ_PATTERN.match(expression)
    if string_eq_match is not None:
        normalized_required = required_value
        if string_eq_match.group("op") in _NEGATED_EQUALITY_OPERATORS:
            normalized_required = not normalized_required
        return _ParsedConstraint(
            condition_type="string_eq",
            symbolic_constraint=_symbolic_call(
                "StringEq",
                var=string_eq_match.group("var"),
                val=_decode_string_literal(string_eq_match.group("literal")),
            ),
            required_value=normalized_required,
        )

    int_bound_match = _INT_BOUND_PATTERN.match(expression)
    if int_bound_match is not None:
        return _ParsedConstraint(
            condition_type="int_bound",
            symbolic_constraint=_symbolic_call(
                "IntBound",
                var=int_bound_match.group("var"),
                op=_operator_name(int_bound_match.group("op")),
                n=int(int_bound_match.group("n")),
            ),
            required_value=required_value,
        )

    in_match = _IN_OPERATOR_PATTERN.match(expression)
    if in_match is not None:
        normalized_required = required_value
        if in_match.group("neg") is not None:
            normalized_required = not normalized_required
        return _ParsedConstraint(
            condition_type="key_in_object",
            symbolic_constraint=_symbolic_call(
                "KeyIn",
                var=in_match.group("var"),
                key=_decode_string_literal(in_match.group("key")),
            ),
            required_value=normalized_required,
        )

    isarray_match = _ARRAY_ISARRAY_PATTERN.match(expression)
    if isarray_match is not None:
        normalized_required = required_value
        if isarray_match.group("neg") is not None:
            normalized_required = not normalized_required
        return _ParsedConstraint(
            condition_type="array_check",
            symbolic_constraint=_symbolic_call(
                "ArrayIsArray",
                var=isarray_match.group("var"),
            ),
            required_value=normalized_required,
        )

    instanceof_match = _INSTANCEOF_PATTERN.match(expression)
    if instanceof_match is not None:
        return _ParsedConstraint(
            condition_type="instanceof",
            symbolic_constraint=_symbolic_call(
                "InstanceOf",
                var=instanceof_match.group("var"),
                cls=instanceof_match.group("cls"),
            ),
            required_value=required_value,
        )

    null_match = _NULL_CHECK_PATTERN.match(expression)
    if null_match is not None:
        normalized_required = required_value
        if null_match.group("op") in _NEGATED_EQUALITY_OPERATORS:
            normalized_required = not normalized_required
        return _ParsedConstraint(
            condition_type="null_check",
            symbolic_constraint=_symbolic_call(
                "NullCheck",
                var=null_match.group("var"),
                lit=null_match.group("lit"),
            ),
            required_value=normalized_required,
        )

    return None


def _find_switch_branch(
    lines: Sequence[str],
    *,
    switch_line: int,
    target_line: int,
) -> _SwitchBranch | None:
    start_index = max(switch_line - 1, 0)
    if start_index >= len(lines):
        return None

    header_started = False
    brace_depth = 0
    current_label: str | None = None
    current_label_line: int | None = None
    current_start_line: int | None = None
    branches: list[tuple[int, int, str | None, int]] = []

    for index in range(start_index, len(lines)):
        line = lines[index]
        line_number = index + 1

        if not header_started:
            if "{" not in line:
                continue
            header_started = True

        if header_started and brace_depth == 1:
            case_match = _CASE_PATTERN.match(line)
            if case_match is not None:
                if current_label_line is not None and current_start_line is not None:
                    branches.append(
                        (current_start_line, line_number - 1, current_label, current_label_line)
                    )
                current_label = case_match.group("label").strip()
                current_label_line = line_number
                current_start_line = line_number + 1
            elif _DEFAULT_PATTERN.match(line) is not None:
                if current_label_line is not None and current_start_line is not None:
                    branches.append(
                        (current_start_line, line_number - 1, current_label, current_label_line)
                    )
                current_label = None
                current_label_line = line_number
                current_start_line = line_number + 1

        brace_depth += line.count("{")
        brace_depth -= line.count("}")

        if header_started and brace_depth == 0:
            if current_label_line is not None and current_start_line is not None:
                branches.append(
                    (current_start_line, line_number - 1, current_label, current_label_line)
                )
            break

    for start_line, end_line, label, label_line in branches:
        if start_line <= target_line <= max(start_line, end_line):
            return _SwitchBranch(label=label, label_line=label_line)
    return None


def _switch_expression_from_code(code: str) -> str:
    match = _SWITCH_PATTERN.match(code.strip())
    if match is None:
        return ""
    return match.group("expr").strip()


def _is_switch(control: _ControlStructure) -> bool:
    if control.control_type is not None and control.control_type.upper() == "SWITCH":
        return True
    return control.code.lstrip().startswith("switch")


def _symbolic_call(name: str, **fields: object) -> str:
    rendered_fields: list[str] = []
    for key, value in fields.items():
        if isinstance(value, str):
            rendered_fields.append(f"{key}={json.dumps(value)}")
        else:
            rendered_fields.append(f"{key}={value}")
    return f"{name}({', '.join(rendered_fields)})"


def _operator_name(operator: str) -> str:
    try:
        return _OPERATOR_NAME_MAP[operator]
    except KeyError as exc:
        raise ConditionExtractionError(f"Unsupported operator: {operator}") from exc


def _decode_string_literal(value: str) -> str:
    decoded = ast.literal_eval(value)
    if not isinstance(decoded, str):
        raise ConditionExtractionError(f"Expected string literal, got {value!r}")
    return decoded


def _controlled_by_query(node: QueryNode) -> str | None:
    node_root = _node_query_root(node)
    if node_root is None:
        return None
    return (
        f"{node_root}.controlledBy.map(c => Map("
        '"_id" -> c.id, '
        '"methodName" -> c.method.name, '
        '"methodFullName" -> c.method.fullName'
        ")).toJsonPretty"
    )


def _method_controls_query(
    *,
    method_name: str | None,
    method_full_name: str | None,
) -> str:
    if method_full_name:
        selector = f"cpg.method.fullNameExact({json.dumps(method_full_name)})"
    elif method_name:
        selector = f"cpg.method.name({json.dumps(re.escape(method_name))})"
    else:
        raise ConditionExtractionError("Method selector requires a name or full name")
    return (
        f"{selector}.ast.isControlStructure.map(c => Map("
        '"_id" -> c.id, '
        '"code" -> c.code, '
        '"lineNumber" -> c.lineNumber, '
        '"columnNumber" -> c.columnNumber, '
        '"controlStructureType" -> c.controlStructureType, '
        '"condition" -> c.condition.code'
        ")).toJsonPretty"
    )


def _control_by_id_query(control_id: int) -> str:
    return (
        f"cpg.id({control_id}L).isControlStructure.map(c => Map("
        '"_id" -> c.id, '
        '"code" -> c.code, '
        '"lineNumber" -> c.lineNumber, '
        '"columnNumber" -> c.columnNumber, '
        '"controlStructureType" -> c.controlStructureType, '
        '"condition" -> c.condition.code'
        ")).toJsonPretty"
    )


def _branch_ast_ids_query(control_id: int, order: int) -> str:
    return f"cpg.id({control_id}L).astChildren.order({order}).ast.id.toJsonPretty"


def _node_query_root(node: QueryNode) -> str | None:
    step_name = {
        "CALL": "call",
        "IDENTIFIER": "identifier",
        "METHOD_PARAMETER_IN": "parameter",
        "RETURN": "methodReturn",
        "FIELD_IDENTIFIER": "fieldIdentifier",
        "LITERAL": "literal",
    }.get(node.node_type)
    if step_name is None:
        return None
    return f"cpg.{step_name}.id({node.node_id}L)"


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
    "ConditionExtractionError",
    "PathConditionExtractor",
    "extract_path_conditions",
    "parse_condition_text",
]
