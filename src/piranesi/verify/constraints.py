from __future__ import annotations

import ast
import json
import re
from collections.abc import Iterable, Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import Literal

from piranesi.models import (
    CandidateFinding,
)
from piranesi.models import (
    PathCondition as RawPathCondition,
)
from piranesi.verify.sandbox import PayloadEncoding

ConstraintOperator = Literal["eq", "lt", "le", "gt", "ge"]
ConstraintValueType = Literal["string", "int", "float", "bool"]
PayloadCarrier = Literal["body", "query", "path", "header"]

_MAX_DISJUNCTS = 10
_FIELD_SEGMENT_PATTERN = re.compile(r"\.([A-Za-z_$][\w$]*)|\[['\"]([^'\"]+)['\"]\]")
_ROUTE_CALL_PATTERN = re.compile(
    r"\b(?:app|router)\.(?P<method>get|post|put|delete|patch|options|head|use)\s*"
    r"\(\s*(?P<quote>['\"`])(?P<path>.+?)(?P=quote)"
)
_ROUTE_CHAIN_PATTERN = re.compile(
    r"\b(?:app|router)\.route\(\s*(?P<quote>['\"`])(?P<path>.+?)(?P=quote)\s*\)"
    r"\.(?P<method>get|post|put|delete|patch|options|head|use)\b"
)
_TYPEOF_PATTERN = re.compile(
    r"^typeof\s+(?P<var>.+?)\s*(?:===|==)\s*(?P<quote>['\"])(?P<type>[^'\"]+)(?P=quote)$"
)
_LENGTH_PATTERN = re.compile(
    r"^(?P<var>.+?)\.length\s*(?P<op>===|==|!==|!=|>=|>|<=|<)\s*(?P<n>-?\d+)$"
)
_INCLUDES_PATTERN = re.compile(
    r"^(?P<var>.+?)\.(?:includes|contains)\(\s*(?P<quote>['\"`])(?P<value>.*?)(?P=quote)\s*\)$"
)
_COMPARISON_PATTERN = re.compile(
    r"^(?P<left>.+?)\s*(?P<op>===|==|!==|!=|>=|>|<=|<)\s*(?P<right>.+)$"
)
_TYPE_ALIASES = {
    "number": "float",
    "boolean": "bool",
}


@dataclass(frozen=True, slots=True)
class StringEq:
    var: str
    val: str


@dataclass(frozen=True, slots=True)
class StringContains:
    var: str
    substr: str


@dataclass(frozen=True, slots=True)
class StringLength:
    var: str
    op: ConstraintOperator
    n: int


@dataclass(frozen=True, slots=True)
class IntBound:
    var: str
    op: ConstraintOperator
    n: int


@dataclass(frozen=True, slots=True)
class TypeCheck:
    var: str
    type_name: ConstraintValueType


@dataclass(frozen=True, slots=True)
class LogicalAnd:
    children: tuple[VerifierConstraint, ...]


@dataclass(frozen=True, slots=True)
class LogicalOr:
    children: tuple[VerifierConstraint, ...]


@dataclass(frozen=True, slots=True)
class LogicalNot:
    child: VerifierConstraint


type VerifierConstraint = (
    StringEq
    | StringContains
    | StringLength
    | IntBound
    | TypeCheck
    | LogicalAnd
    | LogicalOr
    | LogicalNot
)
type AtomicConstraint = StringEq | StringContains | StringLength | IntBound | TypeCheck


@dataclass(frozen=True, slots=True)
class PayloadSlot:
    name: str
    carrier: PayloadCarrier
    field_path: tuple[str, ...]
    source: str
    encoding: PayloadEncoding

    @property
    def request_key(self) -> str:
        return ".".join(self.field_path) if self.field_path else self.name


@dataclass(slots=True)
class ExploitTemplate:
    vuln_class: str
    http_method: str
    endpoint: str
    payload_slots: tuple[PayloadSlot, ...]
    path_conditions: tuple[VerifierConstraint, ...]
    constraint_sets: tuple[tuple[VerifierConstraint, ...], ...]
    unsat_reason: str | None = None


def extract_exploit_template(finding: CandidateFinding) -> ExploitTemplate:
    payload_slots = _extract_payload_slots(finding)
    alias_map = _build_alias_map(finding, payload_slots)

    parsed_conditions = tuple(
        condition
        for raw_condition in finding.path_conditions
        for condition in [_parse_and_canonicalize(raw_condition, alias_map)]
        if condition is not None
    )

    constraint_sets = tuple(_expand_path_conditions(parsed_conditions))
    if not constraint_sets:
        constraint_sets = ((),)

    normalized_sets = tuple(
        normalized
        for constraint_set in constraint_sets
        for normalized in [normalize_constraint_set(constraint_set)]
        if normalized is not None
    )

    unsat_reason = None
    if constraint_sets and not normalized_sets:
        unsat_reason = "CONSTRAINTS_UNSATISFIABLE"
        normalized_sets = ()
    elif not normalized_sets:
        normalized_sets = ((),)

    http_method, endpoint = _infer_route(finding, payload_slots[0] if payload_slots else None)
    return ExploitTemplate(
        vuln_class=finding.vuln_class,
        http_method=http_method,
        endpoint=endpoint,
        payload_slots=payload_slots,
        path_conditions=parsed_conditions,
        constraint_sets=normalized_sets,
        unsat_reason=unsat_reason,
    )


def parse_path_condition(raw_condition: RawPathCondition) -> VerifierConstraint | None:
    parsed = _parse_constraint_text(raw_condition.symbolic_constraint or raw_condition.expression)
    if parsed is None:
        return None
    return parsed if raw_condition.required_value else negate_constraint(parsed)


def negate_constraint(constraint: VerifierConstraint) -> VerifierConstraint:
    if isinstance(constraint, LogicalNot):
        return constraint.child
    if isinstance(constraint, LogicalAnd):
        return LogicalOr(tuple(negate_constraint(child) for child in constraint.children))
    if isinstance(constraint, LogicalOr):
        return LogicalAnd(tuple(negate_constraint(child) for child in constraint.children))
    return LogicalNot(constraint)


def normalize_constraint_set(
    constraints: Sequence[VerifierConstraint],
) -> tuple[VerifierConstraint, ...] | None:
    flattened = tuple(_flatten_conjunction(constraints))
    without_tautologies = tuple(
        constraint for constraint in flattened if not _is_tautology(constraint)
    )
    deduplicated = tuple(dict.fromkeys(without_tautologies))
    simplified = _drop_subsumed_contains(deduplicated)
    return None if _has_contradiction(simplified) else simplified


def expand_constraint_sets(
    constraints: Sequence[VerifierConstraint],
    *,
    max_disjuncts: int = _MAX_DISJUNCTS,
) -> tuple[tuple[VerifierConstraint, ...], ...]:
    return tuple(_expand_path_conditions(constraints, max_disjuncts=max_disjuncts))


def _parse_and_canonicalize(
    raw_condition: RawPathCondition,
    alias_map: dict[str, str],
) -> VerifierConstraint | None:
    parsed = parse_path_condition(raw_condition)
    if parsed is None:
        return None
    return _canonicalize_constraint(parsed, alias_map)


def _parse_constraint_text(raw_text: str | None) -> VerifierConstraint | None:
    if raw_text is None:
        return None
    text = raw_text.strip()
    if not text:
        return None

    for parser in (_parse_json_constraint, _parse_python_call_constraint, _parse_js_expression):
        parsed = parser(text)
        if parsed is not None:
            return parsed
    return None


def _parse_json_constraint(text: str) -> VerifierConstraint | None:
    try:
        payload = json.loads(text)
    except json.JSONDecodeError:
        return None
    return _constraint_from_structured_value(payload)


def _parse_python_call_constraint(text: str) -> VerifierConstraint | None:
    try:
        node = ast.parse(text, mode="eval").body
    except SyntaxError:
        return None
    return _constraint_from_ast(node)


def _constraint_from_ast(node: ast.AST) -> VerifierConstraint | None:
    if not isinstance(node, ast.Call) or not isinstance(node.func, ast.Name):
        return None
    name = node.func.id

    if name in {"LogicalAnd", "LogicalOr"}:
        children = tuple(
            child
            for arg in _iter_call_arguments(node, keyword_name="children")
            for child in [_constraint_from_ast(arg)]
            if child is not None
        )
        if not children:
            return None
        return LogicalAnd(children) if name == "LogicalAnd" else LogicalOr(children)

    if name == "LogicalNot":
        child_node = _first_call_argument(node, keyword_name="child")
        if child_node is None:
            return None
        child = _constraint_from_ast(child_node)
        return LogicalNot(child) if child is not None else None

    values = {
        keyword.arg: _literal_from_ast(keyword.value)
        for keyword in node.keywords
        if keyword.arg is not None
    }
    positional = tuple(_literal_from_ast(arg) for arg in node.args)
    if name == "StringEq":
        var = _value_at(values, positional, 0, "var")
        val = _value_at(values, positional, 1, "val")
        return StringEq(var=str(var), val=str(val)) if var is not None and val is not None else None
    if name == "StringContains":
        var = _value_at(values, positional, 0, "var")
        substr = _value_at(values, positional, 1, "substr")
        return (
            StringContains(var=str(var), substr=str(substr))
            if var is not None and substr is not None
            else None
        )
    if name == "StringLength":
        var = _value_at(values, positional, 0, "var")
        op = _value_at(values, positional, 1, "op")
        n = _value_at(values, positional, 2, "n")
        return _length_constraint(var, op, n)
    if name == "IntBound":
        var = _value_at(values, positional, 0, "var")
        op = _value_at(values, positional, 1, "op")
        n = _value_at(values, positional, 2, "n")
        return _int_constraint(var, op, n)
    if name == "TypeCheck":
        var = _value_at(values, positional, 0, "var")
        type_name = _value_at(values, positional, 1, "type")
        return _type_constraint(var, type_name)
    return None


def _iter_call_arguments(node: ast.Call, *, keyword_name: str) -> tuple[ast.AST, ...]:
    if node.args:
        return tuple(node.args)
    for keyword in node.keywords:
        if keyword.arg == keyword_name and isinstance(keyword.value, (ast.List, ast.Tuple)):
            return tuple(keyword.value.elts)
    return ()


def _first_call_argument(node: ast.Call, *, keyword_name: str) -> ast.AST | None:
    if node.args:
        return node.args[0]
    for keyword in node.keywords:
        if keyword.arg == keyword_name:
            return keyword.value
    return None


def _literal_from_ast(node: ast.AST) -> object:
    return ast.literal_eval(node)


def _value_at(
    values: dict[str, object],
    positional: Sequence[object],
    index: int,
    key: str,
) -> object | None:
    if key in values:
        return values[key]
    return positional[index] if index < len(positional) else None


def _constraint_from_structured_value(value: object) -> VerifierConstraint | None:
    if not isinstance(value, dict):
        return None
    kind_raw = value.get("kind") or value.get("type") or value.get("constraint")
    if not isinstance(kind_raw, str):
        return None

    kind = kind_raw.strip()
    if kind == "StringEq":
        var = value.get("var")
        val = value.get("val")
        return StringEq(var=str(var), val=str(val)) if var is not None and val is not None else None
    if kind == "StringContains":
        var = value.get("var")
        substr = value.get("substr")
        return (
            StringContains(var=str(var), substr=str(substr))
            if var is not None and substr is not None
            else None
        )
    if kind == "StringLength":
        return _length_constraint(value.get("var"), value.get("op"), value.get("n"))
    if kind == "IntBound":
        return _int_constraint(value.get("var"), value.get("op"), value.get("n"))
    if kind == "TypeCheck":
        return _type_constraint(value.get("var"), value.get("type"))
    if kind in {"LogicalAnd", "LogicalOr"}:
        children = tuple(
            child
            for item in value.get("children", [])
            for child in [_constraint_from_structured_value(item)]
            if child is not None
        )
        if not children:
            return None
        return LogicalAnd(children) if kind == "LogicalAnd" else LogicalOr(children)
    if kind == "LogicalNot":
        child = _constraint_from_structured_value(value.get("child"))
        return LogicalNot(child) if child is not None else None
    return None


def _length_constraint(var: object, op: object, n: object) -> StringLength | None:
    normalized_op = _normalize_constraint_operator(op)
    if var is None or normalized_op is None or not isinstance(n, int):
        return None
    return StringLength(var=str(var), op=normalized_op, n=n)


def _int_constraint(var: object, op: object, n: object) -> IntBound | None:
    normalized_op = _normalize_constraint_operator(op)
    if var is None or normalized_op is None or not isinstance(n, int):
        return None
    return IntBound(var=str(var), op=normalized_op, n=n)


def _type_constraint(var: object, type_name: object) -> TypeCheck | None:
    if var is None or not isinstance(type_name, str):
        return None
    normalized = _normalize_type_name(type_name)
    return TypeCheck(var=str(var), type_name=normalized) if normalized is not None else None


def _parse_js_expression(text: str) -> VerifierConstraint | None:
    expression = _strip_outer_parens(text.strip())
    if not expression:
        return None

    or_parts = _split_top_level(expression, "||")
    if len(or_parts) > 1:
        children = tuple(
            child
            for part in or_parts
            for child in [_parse_js_expression(part)]
            if child is not None
        )
        return LogicalOr(children) if children else None

    and_parts = _split_top_level(expression, "&&")
    if len(and_parts) > 1:
        children = tuple(
            child
            for part in and_parts
            for child in [_parse_js_expression(part)]
            if child is not None
        )
        return LogicalAnd(children) if children else None

    if expression.startswith("!") and not expression.startswith("!="):
        child = _parse_js_expression(expression[1:].strip())
        return LogicalNot(child) if child is not None else None

    if typeof_match := _TYPEOF_PATTERN.match(expression):
        return _type_constraint(
            typeof_match.group("var"),
            typeof_match.group("type"),
        )

    if length_match := _LENGTH_PATTERN.match(expression):
        constraint = _length_constraint(
            length_match.group("var"),
            _normalize_js_operator(length_match.group("op")),
            int(length_match.group("n")),
        )
        if constraint is None:
            return None
        return (
            negate_constraint(constraint)
            if length_match.group("op") in {"!=", "!=="}
            else constraint
        )

    if includes_match := _INCLUDES_PATTERN.match(expression):
        return StringContains(
            var=includes_match.group("var"),
            substr=includes_match.group("value"),
        )

    if comparison_match := _COMPARISON_PATTERN.match(expression):
        return _comparison_constraint(
            left=comparison_match.group("left"),
            operator=comparison_match.group("op"),
            right=comparison_match.group("right"),
        )
    return None


def _comparison_constraint(
    *,
    left: str,
    operator: str,
    right: str,
) -> VerifierConstraint | None:
    normalized_operator = _normalize_js_operator(operator)
    if normalized_operator is None:
        return None

    left_text = _strip_outer_parens(left.strip())
    right_text = _strip_outer_parens(right.strip())
    left_string = _string_literal(left_text)
    right_string = _string_literal(right_text)

    if right_string is not None:
        constraint = StringEq(var=left_text, val=right_string)
        return negate_constraint(constraint) if operator in {"!=", "!=="} else constraint
    if left_string is not None:
        constraint = StringEq(var=right_text, val=left_string)
        return negate_constraint(constraint) if operator in {"!=", "!=="} else constraint

    left_int = _int_literal(left_text)
    right_int = _int_literal(right_text)
    if right_int is not None:
        constraint = _int_constraint(left_text, normalized_operator, right_int)
        return negate_constraint(constraint) if operator in {"!=", "!=="} else constraint
    if left_int is not None:
        reversed_operator = _reverse_operator(normalized_operator)
        constraint = _int_constraint(right_text, reversed_operator, left_int)
        return negate_constraint(constraint) if operator in {"!=", "!=="} else constraint
    return None


def _canonicalize_constraint(
    constraint: VerifierConstraint,
    alias_map: dict[str, str],
) -> VerifierConstraint:
    if isinstance(constraint, StringEq):
        return StringEq(var=_canonicalize_var(constraint.var, alias_map), val=constraint.val)
    if isinstance(constraint, StringContains):
        return StringContains(
            var=_canonicalize_var(constraint.var, alias_map),
            substr=constraint.substr,
        )
    if isinstance(constraint, StringLength):
        return StringLength(
            var=_canonicalize_var(constraint.var, alias_map),
            op=constraint.op,
            n=constraint.n,
        )
    if isinstance(constraint, IntBound):
        return IntBound(
            var=_canonicalize_var(constraint.var, alias_map),
            op=constraint.op,
            n=constraint.n,
        )
    if isinstance(constraint, TypeCheck):
        return TypeCheck(
            var=_canonicalize_var(constraint.var, alias_map),
            type_name=constraint.type_name,
        )
    if isinstance(constraint, LogicalAnd):
        return LogicalAnd(
            tuple(_canonicalize_constraint(child, alias_map) for child in constraint.children)
        )
    if isinstance(constraint, LogicalOr):
        return LogicalOr(
            tuple(_canonicalize_constraint(child, alias_map) for child in constraint.children)
        )
    return LogicalNot(_canonicalize_constraint(constraint.child, alias_map))


def _canonicalize_var(var: str, alias_map: dict[str, str]) -> str:
    normalized = _normalize_var(var)
    if normalized in alias_map:
        return alias_map[normalized]
    last_segment = _last_field_segment(normalized)
    if last_segment in alias_map:
        return alias_map[last_segment]
    return normalized


def _flatten_conjunction(
    constraints: Sequence[VerifierConstraint],
) -> Iterable[VerifierConstraint]:
    for constraint in constraints:
        if isinstance(constraint, LogicalAnd):
            yield from _flatten_conjunction(constraint.children)
        else:
            yield constraint


def _drop_subsumed_contains(
    constraints: Sequence[VerifierConstraint],
) -> tuple[VerifierConstraint, ...]:
    eq_values = {
        constraint.var: constraint.val
        for constraint in constraints
        if isinstance(constraint, StringEq)
    }
    result: list[VerifierConstraint] = []
    for constraint in constraints:
        if isinstance(constraint, StringContains):
            eq_value = eq_values.get(constraint.var)
            if eq_value is not None and constraint.substr in eq_value:
                continue
        result.append(constraint)
    return tuple(result)


def _has_contradiction(constraints: Sequence[VerifierConstraint]) -> bool:
    positives: set[AtomicConstraint] = set()
    negatives: set[AtomicConstraint] = set()

    for constraint in constraints:
        if isinstance(constraint, LogicalNot) and isinstance(constraint.child, _atomic_types()):
            negatives.add(constraint.child)
            continue
        if isinstance(constraint, _atomic_types()):
            positives.add(constraint)

    if positives & negatives:
        return True

    if _type_contradiction(positives):
        return True
    if _string_eq_contradiction(positives):
        return True
    if _bounds_contradiction(positives, StringLength):
        return True
    if _bounds_contradiction(positives, IntBound):
        return True
    if _eq_and_length_contradiction(positives):
        return True
    return _eq_and_contains_contradiction(positives, negatives)


def _type_contradiction(constraints: set[AtomicConstraint]) -> bool:
    type_checks: dict[str, str] = {}
    for constraint in constraints:
        if not isinstance(constraint, TypeCheck):
            continue
        existing = type_checks.get(constraint.var)
        if existing is not None and existing != constraint.type_name:
            return True
        type_checks[constraint.var] = constraint.type_name
    return False


def _string_eq_contradiction(constraints: set[AtomicConstraint]) -> bool:
    values: dict[str, str] = {}
    for constraint in constraints:
        if not isinstance(constraint, StringEq):
            continue
        existing = values.get(constraint.var)
        if existing is not None and existing != constraint.val:
            return True
        values[constraint.var] = constraint.val
    return False


def _bounds_contradiction(
    constraints: set[AtomicConstraint],
    bound_type: type[StringLength] | type[IntBound],
) -> bool:
    eq_value: dict[str, int] = {}
    lower: dict[str, tuple[int, bool]] = {}
    upper: dict[str, tuple[int, bool]] = {}

    for constraint in constraints:
        if not isinstance(constraint, bound_type):
            continue
        if constraint.op == "eq":
            existing_eq = eq_value.get(constraint.var)
            if existing_eq is not None and existing_eq != constraint.n:
                return True
            eq_value[constraint.var] = constraint.n
            continue
        if constraint.op in {"gt", "ge"}:
            inclusive = constraint.op == "ge"
            prior = lower.get(constraint.var)
            candidate = (constraint.n, inclusive)
            lower[constraint.var] = _max_lower(prior, candidate)
            continue
        inclusive = constraint.op == "le"
        prior = upper.get(constraint.var)
        candidate = (constraint.n, inclusive)
        upper[constraint.var] = _min_upper(prior, candidate)

    for var, value in eq_value.items():
        if var in lower and not _satisfies_lower(value, lower[var]):
            return True
        if var in upper and not _satisfies_upper(value, upper[var]):
            return True

    for var in set(lower) | set(upper):
        if var not in lower or var not in upper:
            continue
        lower_value, lower_inclusive = lower[var]
        upper_value, upper_inclusive = upper[var]
        if lower_value > upper_value:
            return True
        if lower_value == upper_value and not (lower_inclusive and upper_inclusive):
            return True
    return False


def _eq_and_length_contradiction(constraints: set[AtomicConstraint]) -> bool:
    string_eq = {
        constraint.var: constraint.val
        for constraint in constraints
        if isinstance(constraint, StringEq)
    }
    for constraint in constraints:
        if not isinstance(constraint, StringLength):
            continue
        value = string_eq.get(constraint.var)
        if value is None:
            continue
        if not _compare_int(len(value), constraint.op, constraint.n):
            return True
    return False


def _eq_and_contains_contradiction(
    positives: set[AtomicConstraint],
    negatives: set[AtomicConstraint],
) -> bool:
    string_eq = {
        constraint.var: constraint.val
        for constraint in positives
        if isinstance(constraint, StringEq)
    }
    for constraint in positives:
        if isinstance(constraint, StringContains):
            value = string_eq.get(constraint.var)
            if value is not None and constraint.substr not in value:
                return True
    for constraint in negatives:
        if isinstance(constraint, StringContains):
            value = string_eq.get(constraint.var)
            if value is not None and constraint.substr in value:
                return True
    return False


def _is_tautology(constraint: VerifierConstraint) -> bool:
    return isinstance(constraint, StringLength) and constraint.op == "ge" and constraint.n <= 0


def _expand_path_conditions(
    constraints: Sequence[VerifierConstraint],
    *,
    max_disjuncts: int = _MAX_DISJUNCTS,
) -> list[tuple[VerifierConstraint, ...]]:
    expanded_sets: list[tuple[VerifierConstraint, ...]] = [()]
    for constraint in constraints:
        child_sets = _expand_constraint(constraint)
        next_sets: list[tuple[VerifierConstraint, ...]] = []
        for existing in expanded_sets:
            for child_set in child_sets:
                next_sets.append(existing + child_set)
                if len(next_sets) >= max_disjuncts:
                    return next_sets[:max_disjuncts]
        expanded_sets = next_sets
    return expanded_sets[:max_disjuncts]


def _expand_constraint(constraint: VerifierConstraint) -> list[tuple[VerifierConstraint, ...]]:
    if isinstance(constraint, LogicalAnd):
        return _expand_path_conditions(constraint.children)
    if isinstance(constraint, LogicalOr):
        disjuncts: list[tuple[VerifierConstraint, ...]] = []
        for child in constraint.children:
            disjuncts.extend(_expand_constraint(child))
            if len(disjuncts) >= _MAX_DISJUNCTS:
                break
        return disjuncts[:_MAX_DISJUNCTS]
    return [(constraint,)]


def _extract_payload_slots(finding: CandidateFinding) -> tuple[PayloadSlot, ...]:
    source_expression = _source_expression(finding)
    carrier = _infer_payload_carrier(finding, source_expression)
    field_path = _field_path_for_source(finding, source_expression)
    default_name = field_path[-1] if field_path else (finding.source.parameter_name or "payload")
    encoding = _infer_encoding(finding, carrier)

    return (
        PayloadSlot(
            name=default_name,
            carrier=carrier,
            field_path=field_path or (default_name,),
            source=source_expression,
            encoding=encoding,
        ),
    )


def _build_alias_map(
    finding: CandidateFinding,
    payload_slots: Sequence[PayloadSlot],
) -> dict[str, str]:
    alias_map: dict[str, str] = {}
    source_expression = _source_expression(finding)
    for slot in payload_slots:
        for alias in {
            slot.name,
            slot.request_key,
            source_expression,
            finding.source.parameter_name or "",
            _last_field_segment(source_expression),
        }:
            normalized = _normalize_var(alias)
            if normalized:
                alias_map[normalized] = slot.name
    return alias_map


def _source_expression(finding: CandidateFinding) -> str:
    source_type = finding.source.source_type.strip()
    if source_type.startswith("req.") or source_type.startswith("process.env"):
        return source_type
    snippet = finding.source.location.snippet
    match = re.search(r"req\.(?:body|query|params|headers)\S*", snippet)
    if match is not None:
        return match.group(0).rstrip(";),")
    return source_type or (finding.source.parameter_name or "payload")


def _infer_payload_carrier(
    finding: CandidateFinding,
    source_expression: str,
) -> PayloadCarrier:
    raw_candidates = (
        source_expression,
        finding.source.source_type,
        finding.source.location.snippet,
    )
    raw = " ".join(candidate.lower() for candidate in raw_candidates)
    if "req.body" in raw or finding.source.source_type == "request_body":
        return "body"
    if "req.params" in raw:
        return "path"
    if "req.headers" in raw or finding.source.source_type == "header":
        return "header"
    return "query"


def _field_path_for_source(
    finding: CandidateFinding,
    source_expression: str,
) -> tuple[str, ...]:
    if finding.source.parameter_name:
        return (finding.source.parameter_name,)

    base = source_expression
    for prefix in ("req.body", "req.query", "req.params", "req.headers", "process.env"):
        if base.startswith(prefix):
            suffix = base[len(prefix) :]
            segments = tuple(
                segment_a or segment_b
                for segment_a, segment_b in _FIELD_SEGMENT_PATTERN.findall(suffix)
            )
            if segments:
                return segments
    last_segment = _last_field_segment(base)
    return (last_segment,) if last_segment else ("payload",)


def _infer_encoding(finding: CandidateFinding, carrier: PayloadCarrier) -> PayloadEncoding:
    if carrier == "query":
        return "query"
    if carrier == "path":
        return "path"
    if carrier == "header":
        return "json"

    file_text = _read_source_file(finding.source.location.file)
    lowered = file_text.lower()
    if "urlencoded(" in lowered and "json(" not in lowered:
        return "urlencoded"
    if "application/x-www-form-urlencoded" in lowered:
        return "urlencoded"
    return "json"


def _infer_route(
    finding: CandidateFinding,
    payload_slot: PayloadSlot | None,
) -> tuple[str, str]:
    fallback_method = (
        "POST" if payload_slot is not None and payload_slot.carrier == "body" else "GET"
    )
    file_path = Path(finding.source.location.file)
    if not file_path.exists():
        return fallback_method, "/"

    try:
        lines = file_path.read_text(encoding="utf-8").splitlines()
    except OSError:
        return fallback_method, "/"

    search_end = min(max(finding.source.location.line, 1), len(lines))
    for line_number in range(search_end - 1, -1, -1):
        line = lines[line_number]
        for pattern in (_ROUTE_CALL_PATTERN, _ROUTE_CHAIN_PATTERN):
            match = pattern.search(line)
            if match is not None:
                return match.group("method").upper(), match.group("path")
    return fallback_method, "/"


def _read_source_file(file_path: str) -> str:
    path = Path(file_path)
    if not path.exists():
        return ""
    try:
        return path.read_text(encoding="utf-8")
    except OSError:
        return ""


def _split_top_level(text: str, separator: str) -> list[str]:
    parts: list[str] = []
    depth = 0
    quote: str | None = None
    escaped = False
    start = 0
    index = 0

    while index < len(text):
        char = text[index]
        if quote is not None:
            if escaped:
                escaped = False
            elif char == "\\" and quote != "`":
                escaped = True
            elif char == quote:
                quote = None
            index += 1
            continue

        if char in {"'", '"', "`"}:
            quote = char
            index += 1
            continue
        if char in "([{":
            depth += 1
            index += 1
            continue
        if char in ")]}":
            depth = max(0, depth - 1)
            index += 1
            continue
        if depth == 0 and text.startswith(separator, index):
            parts.append(text[start:index].strip())
            index += len(separator)
            start = index
            continue
        index += 1

    parts.append(text[start:].strip())
    return [part for part in parts if part]


def _strip_outer_parens(text: str) -> str:
    stripped = text.strip()
    while stripped.startswith("(") and stripped.endswith(")") and _balanced_outer_parens(stripped):
        stripped = stripped[1:-1].strip()
    return stripped


def _balanced_outer_parens(text: str) -> bool:
    depth = 0
    quote: str | None = None
    escaped = False
    for index, char in enumerate(text):
        if quote is not None:
            if escaped:
                escaped = False
            elif char == "\\" and quote != "`":
                escaped = True
            elif char == quote:
                quote = None
            continue
        if char in {"'", '"', "`"}:
            quote = char
            continue
        if char == "(":
            depth += 1
        elif char == ")":
            depth -= 1
            if depth == 0 and index != len(text) - 1:
                return False
    return depth == 0


def _string_literal(text: str) -> str | None:
    if len(text) < 2 or text[0] not in {"'", '"', "`"} or text[-1] != text[0]:
        return None
    if text[0] == "`":
        return text[1:-1]
    try:
        value = ast.literal_eval(text)
    except (SyntaxError, ValueError):
        return None
    return value if isinstance(value, str) else None


def _int_literal(text: str) -> int | None:
    try:
        return int(text)
    except ValueError:
        return None


def _normalize_js_operator(operator: str) -> ConstraintOperator | None:
    return {
        "==": "eq",
        "===": "eq",
        "!=": "eq",
        "!==": "eq",
        ">": "gt",
        ">=": "ge",
        "<": "lt",
        "<=": "le",
    }.get(operator)


def _normalize_constraint_operator(operator: object) -> ConstraintOperator | None:
    if not isinstance(operator, str):
        return None
    return {
        "eq": "eq",
        "lt": "lt",
        "le": "le",
        "gt": "gt",
        "ge": "ge",
    }.get(operator)


def _normalize_type_name(type_name: str) -> ConstraintValueType | None:
    normalized = _TYPE_ALIASES.get(type_name.strip().lower(), type_name.strip().lower())
    return normalized if normalized in {"string", "int", "float", "bool"} else None


def _reverse_operator(operator: ConstraintOperator) -> ConstraintOperator:
    return {
        "eq": "eq",
        "lt": "gt",
        "le": "ge",
        "gt": "lt",
        "ge": "le",
    }[operator]


def _normalize_var(value: str) -> str:
    stripped = _strip_outer_parens(value.strip())
    stripped = re.sub(r"\s+as\s+[\w.<>[\]|]+$", "", stripped)
    return stripped


def _last_field_segment(value: str) -> str:
    matches = tuple(
        segment_a or segment_b
        for segment_a, segment_b in _FIELD_SEGMENT_PATTERN.findall(value)
    )
    if matches:
        return matches[-1]
    if "." in value:
        return value.rsplit(".", 1)[-1]
    return value


def _compare_int(left: int, operator: ConstraintOperator, right: int) -> bool:
    if operator == "eq":
        return left == right
    if operator == "lt":
        return left < right
    if operator == "le":
        return left <= right
    if operator == "gt":
        return left > right
    return left >= right


def _max_lower(
    prior: tuple[int, bool] | None,
    candidate: tuple[int, bool],
) -> tuple[int, bool]:
    if prior is None:
        return candidate
    if candidate[0] > prior[0]:
        return candidate
    if candidate[0] < prior[0]:
        return prior
    return (prior[0], prior[1] and candidate[1])


def _min_upper(
    prior: tuple[int, bool] | None,
    candidate: tuple[int, bool],
) -> tuple[int, bool]:
    if prior is None:
        return candidate
    if candidate[0] < prior[0]:
        return candidate
    if candidate[0] > prior[0]:
        return prior
    return (prior[0], prior[1] and candidate[1])


def _satisfies_lower(value: int, lower: tuple[int, bool]) -> bool:
    bound, inclusive = lower
    return value >= bound if inclusive else value > bound


def _satisfies_upper(value: int, upper: tuple[int, bool]) -> bool:
    bound, inclusive = upper
    return value <= bound if inclusive else value < bound


def _atomic_types() -> tuple[type[StringEq], ...]:
    return (StringEq, StringContains, StringLength, IntBound, TypeCheck)


__all__ = [
    "AtomicConstraint",
    "ConstraintOperator",
    "ConstraintValueType",
    "ExploitTemplate",
    "IntBound",
    "LogicalAnd",
    "LogicalNot",
    "LogicalOr",
    "PayloadCarrier",
    "PayloadSlot",
    "StringContains",
    "StringEq",
    "StringLength",
    "TypeCheck",
    "VerifierConstraint",
    "expand_constraint_sets",
    "extract_exploit_template",
    "negate_constraint",
    "normalize_constraint_set",
    "parse_path_condition",
]
