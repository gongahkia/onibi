from __future__ import annotations

import json
import re
from collections.abc import Iterable, Mapping, Sequence
from dataclasses import dataclass, field
from enum import StrEnum
from pathlib import Path

from piranesi.detect._javascript_taint import (
    candidate_references,
    extract_user_controlled_source,
    normalize_expression,
    property_reference,
)
from piranesi.models import CandidateFinding, TaintSource, TaintStep
from piranesi.scan.joern import JoernServer
from piranesi.scan.queries import execute_json_query

_IDENTIFIER_PATTERN = re.compile(r"^[A-Za-z_$][\w$]*$")
_VARIABLE_ASSIGNMENT_PATTERN = re.compile(
    r"^\s*(?:const|let|var)\s+(?P<name>[A-Za-z_$][\w$]*)\s*=\s*(?P<expr>.+?)\s*;?\s*$"
)
_REASSIGNMENT_PATTERN = re.compile(r"^\s*(?P<name>[A-Za-z_$][\w$]*)\s*=\s*(?P<expr>.+?)\s*;?\s*$")
_DESTRUCTURING_PATTERN = re.compile(
    r"^\s*(?:const|let|var)\s*{\s*(?P<bindings>[^}]+)\s*}\s*=\s*(?P<expr>.+?)\s*;?\s*$"
)
_SPREAD_ASSIGNMENT_PATTERN = re.compile(
    r"^\s*(?:const|let|var)\s+(?P<name>[A-Za-z_$][\w$]*)\s*=\s*{\s*(?P<body>.+)\s*}\s*;?\s*$"
)
_SPREAD_OPERAND_PATTERN = re.compile(r"\.\.\.(?P<expr>[A-Za-z_$][\w$.\[\]'\"]*)")
_OBJECT_LITERAL_KEY_PATTERN = re.compile(
    r"(?:^|,)\s*(?P<key>[A-Za-z_$][\w$]*|['\"][^'\"]+['\"])\s*:"
)
_PROPERTY_ASSIGNMENT_PATTERN = re.compile(
    r"^\s*(?P<object>[A-Za-z_$][\w$]*)(?:\.(?P<field>[A-Za-z_$][\w$]*)|\[['\"](?P<field_bracket>[^'\"]+)['\"]\])\s*=\s*(?P<expr>.+?)\s*;?\s*$"
)
_FIELD_ACCESS_PATTERN = re.compile(
    r"(?P<object>(?:(?:req|request)(?:\.(?:body|query|params|headers|cookies)|\[['\"](?:body|query|params|headers|cookies)['\"]\])|[A-Za-z_$][\w$]*)(?:\.(?:[A-Za-z_$][\w$]*))*)\.(?P<field>[A-Za-z_$][\w$]*)"
)
_COMPUTED_ACCESS_PATTERN = re.compile(
    r"(?P<object>(?:(?:req|request)(?:\.(?:body|query|params|headers|cookies)|\[['\"](?:body|query|params|headers|cookies)['\"]\])|[A-Za-z_$][\w$]*)(?:\.[A-Za-z_$][\w$]*)*)\[(?P<key>[^\]]+)\]"
)
_EFFECTIVE_SANITIZER_CALL_PATTERN = r"(?P<name>{name})\s*\((?P<args>.*)\)"
_GENERIC_SANITIZER_CALL_PATTERN = re.compile(
    r"^(?:escapeHtml|sanitizeHtml|parameterize|allowlistUrl|parseInt|Number|escape)\s*\(.+\)$"
)
_SANITIZER_CWE_MAP: dict[str, frozenset[str]] = {
    "Number": frozenset({"CWE-89"}),
    "allowlistUrl": frozenset({"CWE-601", "CWE-918"}),
    "escape": frozenset({"CWE-79"}),
    "escapeHtml": frozenset({"CWE-79"}),
    "parameterize": frozenset({"CWE-89"}),
    "parseInt": frozenset({"CWE-89"}),
    "sanitizeHtml": frozenset({"CWE-79"}),
}
_JSON_PARSE_PATTERN = re.compile(r"^JSON\.parse\(\s*(?P<arg>.+)\s*\)$")
_JSON_STRINGIFY_PATTERN = re.compile(r"^JSON\.stringify\(\s*(?P<arg>.+)\s*\)$")
_OBJECT_KEYS_PATTERN = re.compile(r"^Object\.(?:keys|values|entries)\(\s*(?P<arg>.+)\s*\)$")
_TEMPLATE_INTERPOLATION_PATTERN = re.compile(r"\$\{([^}]+)\}")
_MULTI_FIELD_SOURCE_PATTERN = re.compile(
    r"^(?:req|request)(?:\.(?:body|query)|\[['\"](?:body|query)['\"]\])$"
)
_FIELD_OPERATION_INDICATORS = re.compile(
    r"(?:const|let|var)\s*\{|"
    r"\.\.\.|"
    r"\.\w+\s*=|"
    r"\[[^\]]+\]|"
    r"\$\{"
)
_WHOLE_OBJECT_FIELD = "__whole__"


class FieldOp(StrEnum):
    PROPERTY_READ = "property_read"
    DESTRUCTURE = "destructure"
    SPREAD = "spread"
    COMPUTED = "computed"
    SANITIZER = "sanitizer"
    ASSIGNMENT = "assignment"
    UNKNOWN = "unknown"


@dataclass(frozen=True, slots=True)
class TaintLabel:
    source_id: str
    field_path: str
    confidence: float
    sanitized_for: frozenset[str] = frozenset()


@dataclass(slots=True)
class FieldTaintState:
    taint_labels: dict[str, TaintLabel] = field(default_factory=dict)
    object_labels: dict[str, dict[str, TaintLabel]] = field(default_factory=dict)
    object_safe_fields: dict[str, set[str]] = field(default_factory=dict)

    @property
    def labels(self) -> dict[str, TaintLabel]:
        return self.taint_labels


@dataclass(frozen=True, slots=True)
class StepClassification:
    op: FieldOp
    target: str | None = None
    source_expr: str | None = None
    field_name: str | None = None
    key_expression: str | None = None
    bindings: tuple[tuple[str, str], ...] = ()
    spread_sources: tuple[str, ...] = ()
    explicit_keys: frozenset[str] = frozenset()


@dataclass(frozen=True, slots=True)
class FieldTaintStep:
    original_step: TaintStep
    field_path: str | None
    operation_type: FieldOp
    narrowed: bool
    classification: StepClassification
    line_text: str


@dataclass(frozen=True, slots=True)
class _PropertyAccessNode:
    line_number: int
    code: str
    object_name: str
    field_name: str


@dataclass(frozen=True, slots=True)
class _IndexAccessNode:
    line_number: int
    object_name: str
    key_expression: str


@dataclass(frozen=True, slots=True)
class _AssignmentNode:
    line_number: int
    lhs: str
    rhs: str


@dataclass(frozen=True, slots=True)
class _LocalNode:
    line_number: int
    name: str
    code: str


@dataclass(frozen=True, slots=True)
class FieldMethodSummary:
    field_accesses_by_line: Mapping[int, tuple[_PropertyAccessNode, ...]]
    index_accesses_by_line: Mapping[int, tuple[_IndexAccessNode, ...]]
    assignments_by_line: Mapping[int, tuple[_AssignmentNode, ...]]
    locals_by_line: Mapping[int, tuple[_LocalNode, ...]]


@dataclass(slots=True)
class FieldSummaryCache:
    _cache: dict[str, FieldMethodSummary] = field(default_factory=dict)
    _source_lines: dict[Path, tuple[str, ...]] = field(default_factory=dict)

    def get_or_query(self, method_full_name: str, server: JoernServer) -> FieldMethodSummary:
        if method_full_name not in self._cache:
            self._cache[method_full_name] = _query_field_ast_nodes(server, method_full_name)
        return self._cache[method_full_name]

    def read_source_line(self, file_name: str, line_number: int) -> str | None:
        path = Path(file_name)
        if not path.exists():
            return None
        if path not in self._source_lines:
            self._source_lines[path] = tuple(path.read_text(encoding="utf-8").splitlines())
        lines = self._source_lines[path]
        if line_number < 1 or line_number > len(lines):
            return None
        return lines[line_number - 1]


def classify_step_operation(snippet: str) -> StepClassification:
    normalized = normalize_expression(snippet)
    if not normalized:
        return StepClassification(op=FieldOp.UNKNOWN)

    destructuring_match = _DESTRUCTURING_PATTERN.match(snippet)
    if destructuring_match is not None:
        return StepClassification(
            op=FieldOp.DESTRUCTURE,
            source_expr=destructuring_match.group("expr"),
            bindings=_parse_destructuring_bindings(destructuring_match.group("bindings")),
        )

    spread_match = _SPREAD_ASSIGNMENT_PATTERN.match(snippet)
    if spread_match is not None and _SPREAD_OPERAND_PATTERN.search(spread_match.group("body")):
        return StepClassification(
            op=FieldOp.SPREAD,
            target=spread_match.group("name"),
            spread_sources=tuple(
                match.group("expr")
                for match in _SPREAD_OPERAND_PATTERN.finditer(spread_match.group("body"))
            ),
            explicit_keys=frozenset(_explicit_object_keys(spread_match.group("body"))),
        )

    property_assignment_match = _PROPERTY_ASSIGNMENT_PATTERN.match(normalized)
    if property_assignment_match is not None:
        field_name = property_assignment_match.group("field") or property_assignment_match.group(
            "field_bracket"
        )
        return StepClassification(
            op=FieldOp.ASSIGNMENT,
            target=(
                f"{property_assignment_match.group('object')}.{field_name}"
                if field_name is not None
                else property_assignment_match.group("object")
            ),
            source_expr=property_assignment_match.group("expr"),
            field_name=field_name,
        )

    variable_assignment_match = _VARIABLE_ASSIGNMENT_PATTERN.match(snippet)
    if variable_assignment_match is not None:
        rhs = variable_assignment_match.group("expr")
        computed_match = _COMPUTED_ACCESS_PATTERN.search(rhs)
        if computed_match is not None:
            return StepClassification(
                op=FieldOp.COMPUTED,
                target=variable_assignment_match.group("name"),
                source_expr=computed_match.group("object"),
                key_expression=computed_match.group("key"),
            )
        if _GENERIC_SANITIZER_CALL_PATTERN.fullmatch(normalize_expression(rhs)):
            return StepClassification(
                op=FieldOp.SANITIZER,
                target=variable_assignment_match.group("name"),
                source_expr=rhs,
            )
        if _JSON_PARSE_PATTERN.fullmatch(normalize_expression(rhs)) is not None:
            return StepClassification(
                op=FieldOp.ASSIGNMENT,
                target=variable_assignment_match.group("name"),
                source_expr=rhs,
            )
        if _JSON_STRINGIFY_PATTERN.fullmatch(normalize_expression(rhs)) is not None:
            return StepClassification(
                op=FieldOp.ASSIGNMENT,
                target=variable_assignment_match.group("name"),
                source_expr=rhs,
            )
        field_access_match = _FIELD_ACCESS_PATTERN.search(rhs)
        if field_access_match is not None:
            return StepClassification(
                op=FieldOp.PROPERTY_READ,
                target=variable_assignment_match.group("name"),
                source_expr=field_access_match.group("object"),
                field_name=field_access_match.group("field"),
            )
        return StepClassification(
            op=FieldOp.ASSIGNMENT,
            target=variable_assignment_match.group("name"),
            source_expr=rhs,
        )

    reassignment_match = _REASSIGNMENT_PATTERN.match(snippet)
    if reassignment_match is not None:
        if _GENERIC_SANITIZER_CALL_PATTERN.fullmatch(
            normalize_expression(reassignment_match.group("expr"))
        ):
            return StepClassification(
                op=FieldOp.SANITIZER,
                target=reassignment_match.group("name"),
                source_expr=reassignment_match.group("expr"),
            )
        return StepClassification(
            op=FieldOp.ASSIGNMENT,
            target=reassignment_match.group("name"),
            source_expr=reassignment_match.group("expr"),
        )

    computed_match = _COMPUTED_ACCESS_PATTERN.fullmatch(normalized)
    if computed_match is not None:
        return StepClassification(
            op=FieldOp.COMPUTED,
            source_expr=computed_match.group("object"),
            key_expression=computed_match.group("key"),
        )

    property_access_match = _FIELD_ACCESS_PATTERN.fullmatch(normalized)
    if property_access_match is not None:
        return StepClassification(
            op=FieldOp.PROPERTY_READ,
            source_expr=property_access_match.group("object"),
            field_name=property_access_match.group("field"),
        )

    return StepClassification(op=FieldOp.UNKNOWN)


def annotate_flow_with_fields(
    finding: CandidateFinding,
    joern_server: JoernServer,
    *,
    field_summary_cache: FieldSummaryCache | None = None,
) -> list[FieldTaintStep]:
    cache = field_summary_cache or FieldSummaryCache()
    field_steps: list[FieldTaintStep] = []

    for step in finding.taint_path:
        line_text = (
            cache.read_source_line(step.location.file, step.location.line) or step.location.snippet
        )
        summary = (
            cache.get_or_query(step.through_function, joern_server)
            if step.through_function
            else FieldMethodSummary({}, {}, {}, {})
        )
        classification = _classify_step_from_line(
            step=step,
            line_text=line_text,
            summary=summary,
            effective_sanitizers=frozenset(_effective_sanitizers(finding)),
        )
        field_steps.append(
            FieldTaintStep(
                original_step=step,
                field_path=_classification_field_path(step, classification),
                operation_type=classification.op,
                narrowed=classification.op in {FieldOp.PROPERTY_READ, FieldOp.DESTRUCTURE},
                classification=classification,
                line_text=line_text,
            )
        )

    return field_steps


def propagate_field_taint(
    flow_path: Sequence[FieldTaintStep],
    source: TaintSource,
    source_spec: str | None = None,
    *,
    vuln_class: str | None = None,
    effective_sanitizers: frozenset[str] = frozenset(),
) -> FieldTaintState:
    state = FieldTaintState()
    source_expr = normalize_expression(source.location.snippet)
    source_label = TaintLabel(
        source_id=source_spec or source_expr or source.source_type,
        field_path="",
        confidence=1.0,
        sanitized_for=frozenset(),
    )
    direct_source = extract_user_controlled_source(source_expr)
    if direct_source is not None and direct_source.parameter_name is None:
        _set_object_whole_label(state, source_expr, source_label)
        state.taint_labels[source_expr] = source_label
    elif direct_source is not None and direct_source.parameter_name is not None:
        object_name = _object_parent(source_expr) or source_expr
        narrowed = TaintLabel(
            source_id=source_label.source_id,
            field_path=direct_source.parameter_name,
            confidence=1.0,
            sanitized_for=frozenset(),
        )
        _set_object_field_label(state, object_name, direct_source.parameter_name, narrowed)
        state.taint_labels[source_expr] = narrowed
    else:
        state.taint_labels[source_expr] = source_label

    current_label = source_label

    for field_step in flow_path:
        step = field_step.original_step
        method = step.through_function
        snippet = normalize_expression(step.location.snippet)
        classification = field_step.classification

        if _IDENTIFIER_PATTERN.fullmatch(snippet) and snippet and current_label is not None:
            state.taint_labels.setdefault(_scoped_identifier(snippet, method), current_label)

        if classification.op is FieldOp.DESTRUCTURE and classification.source_expr is not None:
            for source_field, target_name in classification.bindings:
                label = _lookup_object_field(
                    state,
                    classification.source_expr,
                    source_field,
                    method=method,
                )
                if label is None:
                    _clear_identifier(state, target_name, method)
                    continue
                _set_identifier_label(state, target_name, label, method=method)
        elif classification.op is FieldOp.SPREAD and classification.target is not None:
            _clear_identifier(state, classification.target, method)
            _clear_object_state(state, classification.target, method)
            for spread_source in classification.spread_sources:
                _copy_object_expression(
                    state,
                    classification.target,
                    spread_source,
                    method=method,
                    sink_cwe=vuln_class,
                    effective_sanitizers=effective_sanitizers,
                )
            for explicit_key in classification.explicit_keys:
                _mark_safe_object_field(state, classification.target, explicit_key, method=method)
        elif (
            classification.op in {FieldOp.ASSIGNMENT, FieldOp.SANITIZER}
            and classification.target is not None
        ):
            _assign_target_from_expression(
                state,
                classification.target,
                classification.source_expr,
                method=method,
                sink_cwe=vuln_class,
                effective_sanitizers=effective_sanitizers,
            )
        elif classification.op in {FieldOp.PROPERTY_READ, FieldOp.COMPUTED}:
            expression = _classification_expression(classification, fallback=snippet)
            if classification.target is not None:
                _assign_target_from_expression(
                    state,
                    classification.target,
                    expression,
                    method=method,
                    sink_cwe=vuln_class,
                    effective_sanitizers=effective_sanitizers,
                )
            elif expression:
                label = _lookup_label_for_expression(
                    state,
                    expression,
                    method=method,
                    sink_cwe=vuln_class,
                    effective_sanitizers=effective_sanitizers,
                )
                if label is not None:
                    state.taint_labels[_expression_key(expression, method)] = label

        resolved = _lookup_label_for_expression(
            state,
            snippet,
            method=method,
            sink_cwe=vuln_class,
            effective_sanitizers=effective_sanitizers,
        )
        if resolved is not None:
            current_label = resolved

    return state


def prune_untainted_fields(
    finding: CandidateFinding,
    field_steps: Sequence[FieldTaintStep] | None = None,
    state: FieldTaintState | None = None,
    *,
    joern_server: JoernServer | None = None,
    field_summary_cache: FieldSummaryCache | None = None,
) -> CandidateFinding | None:
    metadata = dict(finding.metadata)
    if not _needs_field_analysis(finding, field_summary_cache=field_summary_cache):
        metadata["field_sensitive"] = False
        return finding.model_copy(update={"metadata": metadata})

    cache = field_summary_cache or FieldSummaryCache()
    if joern_server is None and field_steps is None:
        metadata["field_sensitive"] = False
        return finding.model_copy(update={"metadata": metadata})

    try:
        if field_steps is not None:
            resolved_steps = list(field_steps)
        else:
            assert joern_server is not None
            resolved_steps = list(
                annotate_flow_with_fields(
                    finding,
                    joern_server,
                    field_summary_cache=cache,
                )
            )
        resolved_state = state or propagate_field_taint(
            resolved_steps,
            finding.source,
            finding.source.location.snippet,
            vuln_class=finding.vuln_class,
            effective_sanitizers=frozenset(_effective_sanitizers(finding)),
        )
    except Exception:
        metadata["field_sensitive"] = False
        return finding.model_copy(update={"metadata": metadata})

    sink_labels = _labels_for_sink(
        finding,
        resolved_state,
        effective_sanitizers=frozenset(_effective_sanitizers(finding)),
    )
    if not sink_labels:
        return None
    if all(finding.vuln_class in label.sanitized_for for label in sink_labels):
        return None

    metadata["field_sensitive"] = True
    metadata["field_taint_sink_paths"] = sorted(
        {label.field_path for label in sink_labels if label.field_path}
    )
    return finding.model_copy(update={"metadata": metadata})


def apply_field_sensitive_pruning(
    findings: Sequence[CandidateFinding],
    joern_server: JoernServer,
    *,
    source_specs: Sequence[object] | None = None,
    field_summary_cache: FieldSummaryCache | None = None,
) -> list[CandidateFinding]:
    del source_specs
    cache = field_summary_cache or FieldSummaryCache()
    pruned: list[CandidateFinding] = []
    for finding in findings:
        kept = prune_untainted_fields(
            finding,
            joern_server=joern_server,
            field_summary_cache=cache,
        )
        if kept is not None:
            pruned.append(kept)
    return pruned


def _query_field_ast_nodes(server: JoernServer, method_full_name: str) -> FieldMethodSummary:
    return FieldMethodSummary(
        field_accesses_by_line=_index_by_line(_query_property_accesses(server, method_full_name)),
        index_accesses_by_line=_index_by_line(_query_index_accesses(server, method_full_name)),
        assignments_by_line=_index_by_line(_query_assignments(server, method_full_name)),
        locals_by_line=_index_by_line(_query_locals(server, method_full_name)),
    )


def _query_property_accesses(
    server: JoernServer,
    method_full_name: str,
) -> tuple[_PropertyAccessNode, ...]:
    method = json.dumps(method_full_name)
    payload = execute_json_query(
        server,
        (
            'cpg.call.name("<operator>.fieldAccess")'
            f".filter(_.method.fullName == {method})"
            '.map(c => Map("lineNumber" -> c.lineNumber, "code" -> c.code, '
            '"args" -> c.argument.code.l)).toJsonPretty'
        ),
    )
    if not isinstance(payload, list):
        return ()
    nodes: list[_PropertyAccessNode] = []
    for item in payload:
        if not isinstance(item, dict):
            continue
        line_number = _coerce_int(item.get("lineNumber"))
        args = item.get("args")
        if (
            line_number is None
            or not isinstance(args, list)
            or len(args) < 2
            or not isinstance(args[0], str)
            or not isinstance(args[1], str)
        ):
            continue
        nodes.append(
            _PropertyAccessNode(
                line_number=line_number,
                code=str(item.get("code") or ""),
                object_name=args[0],
                field_name=args[1],
            )
        )
    return tuple(nodes)


def _query_index_accesses(
    server: JoernServer,
    method_full_name: str,
) -> tuple[_IndexAccessNode, ...]:
    method = json.dumps(method_full_name)
    payload = execute_json_query(
        server,
        (
            'cpg.call.name("<operator>.indexAccess")'
            f".filter(_.method.fullName == {method})"
            '.map(c => Map("lineNumber" -> c.lineNumber, "obj" -> c.argument(1).code, '
            '"key" -> c.argument(2).code)).toJsonPretty'
        ),
    )
    if not isinstance(payload, list):
        return ()
    nodes: list[_IndexAccessNode] = []
    for item in payload:
        if not isinstance(item, dict):
            continue
        line_number = _coerce_int(item.get("lineNumber"))
        object_name = item.get("obj")
        key_expression = item.get("key")
        if (
            line_number is None
            or not isinstance(object_name, str)
            or not isinstance(key_expression, str)
        ):
            continue
        nodes.append(
            _IndexAccessNode(
                line_number=line_number,
                object_name=object_name,
                key_expression=key_expression,
            )
        )
    return tuple(nodes)


def _query_assignments(
    server: JoernServer,
    method_full_name: str,
) -> tuple[_AssignmentNode, ...]:
    method = json.dumps(method_full_name)
    payload = execute_json_query(
        server,
        (
            'cpg.call.name("<operator>.assignment")'
            f".filter(_.method.fullName == {method})"
            '.map(c => Map("lineNumber" -> c.lineNumber, "lhs" -> c.argument(1).code, '
            '"rhs" -> c.argument(2).code)).toJsonPretty'
        ),
    )
    if not isinstance(payload, list):
        return ()
    nodes: list[_AssignmentNode] = []
    for item in payload:
        if not isinstance(item, dict):
            continue
        line_number = _coerce_int(item.get("lineNumber"))
        lhs = item.get("lhs")
        rhs = item.get("rhs")
        if line_number is None or not isinstance(lhs, str) or not isinstance(rhs, str):
            continue
        nodes.append(_AssignmentNode(line_number=line_number, lhs=lhs, rhs=rhs))
    return tuple(nodes)


def _query_locals(server: JoernServer, method_full_name: str) -> tuple[_LocalNode, ...]:
    method = json.dumps(method_full_name)
    payload = execute_json_query(
        server,
        (
            "cpg.local"
            f".filter(_.method.fullName == {method})"
            '.map(l => Map("lineNumber" -> l.lineNumber, "name" -> l.name, "code" -> l.code))'
            ".toJsonPretty"
        ),
    )
    if not isinstance(payload, list):
        return ()
    nodes: list[_LocalNode] = []
    for item in payload:
        if not isinstance(item, dict):
            continue
        line_number = _coerce_int(item.get("lineNumber"))
        name = item.get("name")
        code = item.get("code")
        if line_number is None or not isinstance(name, str) or not isinstance(code, str):
            continue
        nodes.append(_LocalNode(line_number=line_number, name=name, code=code))
    return tuple(nodes)


def _classify_step_from_line(
    *,
    step: TaintStep,
    line_text: str,
    summary: FieldMethodSummary,
    effective_sanitizers: frozenset[str],
) -> StepClassification:
    line_number = step.location.line
    sanitized = (
        step.sanitizer_applied is not None and step.sanitizer_applied in effective_sanitizers
    )

    destructuring_match = _DESTRUCTURING_PATTERN.match(line_text)
    if destructuring_match is not None and summary.locals_by_line.get(line_number):
        return StepClassification(
            op=FieldOp.DESTRUCTURE,
            source_expr=destructuring_match.group("expr"),
            bindings=_parse_destructuring_bindings(destructuring_match.group("bindings")),
        )

    spread_match = _SPREAD_ASSIGNMENT_PATTERN.match(line_text)
    if spread_match is not None and _SPREAD_OPERAND_PATTERN.search(spread_match.group("body")):
        return StepClassification(
            op=FieldOp.SPREAD,
            target=spread_match.group("name"),
            spread_sources=tuple(
                match.group("expr")
                for match in _SPREAD_OPERAND_PATTERN.finditer(spread_match.group("body"))
            ),
            explicit_keys=frozenset(_explicit_object_keys(spread_match.group("body"))),
        )

    assignment = next(iter(summary.assignments_by_line.get(line_number, ())), None)
    if assignment is not None:
        if sanitized and _contains_effective_sanitizer_call(assignment.rhs, effective_sanitizers):
            return StepClassification(
                op=FieldOp.SANITIZER,
                target=assignment.lhs,
                source_expr=assignment.rhs,
            )
        if _COMPUTED_ACCESS_PATTERN.search(assignment.rhs) is not None:
            index_match = next(iter(summary.index_accesses_by_line.get(line_number, ())), None)
            if index_match is not None:
                return StepClassification(
                    op=FieldOp.COMPUTED,
                    target=assignment.lhs,
                    source_expr=index_match.object_name,
                    key_expression=index_match.key_expression,
                )
        field_access = next(iter(summary.field_accesses_by_line.get(line_number, ())), None)
        if field_access is not None:
            return StepClassification(
                op=FieldOp.PROPERTY_READ,
                target=assignment.lhs,
                source_expr=field_access.object_name,
                field_name=field_access.field_name,
            )
        return StepClassification(
            op=FieldOp.ASSIGNMENT,
            target=assignment.lhs,
            source_expr=assignment.rhs,
        )

    if summary.index_accesses_by_line.get(line_number):
        index_match = summary.index_accesses_by_line[line_number][0]
        return StepClassification(
            op=FieldOp.COMPUTED,
            source_expr=index_match.object_name,
            key_expression=index_match.key_expression,
        )

    if summary.field_accesses_by_line.get(line_number):
        field_access = summary.field_accesses_by_line[line_number][0]
        if sanitized and _contains_effective_sanitizer_call(line_text, effective_sanitizers):
            return StepClassification(
                op=FieldOp.SANITIZER,
                source_expr=field_access.code,
                field_name=field_access.field_name,
            )
        return StepClassification(
            op=FieldOp.PROPERTY_READ,
            source_expr=field_access.object_name,
            field_name=field_access.field_name,
        )

    return classify_step_operation(line_text)


def _classification_field_path(step: TaintStep, classification: StepClassification) -> str | None:
    if classification.op is FieldOp.DESTRUCTURE:
        snippet = normalize_expression(step.location.snippet)
        for source_field, target_name in classification.bindings:
            if snippet in {source_field, target_name}:
                return source_field
        return classification.bindings[0][0] if classification.bindings else None
    return classification.field_name


def _classification_expression(
    classification: StepClassification,
    *,
    fallback: str,
) -> str:
    if classification.op is FieldOp.PROPERTY_READ:
        if classification.source_expr and classification.field_name:
            return f"{classification.source_expr}.{classification.field_name}"
        return fallback
    if classification.op is FieldOp.COMPUTED:
        if classification.source_expr and classification.key_expression:
            return f"{classification.source_expr}[{classification.key_expression}]"
        return fallback
    return fallback


def _needs_field_analysis(
    finding: CandidateFinding,
    *,
    field_summary_cache: FieldSummaryCache | None,
) -> bool:
    if len(finding.taint_path) <= 2:
        return False
    source_expr = normalize_expression(finding.source.location.snippet)
    if not _MULTI_FIELD_SOURCE_PATTERN.fullmatch(source_expr):
        return False

    snippets = [
        finding.source.location.snippet,
        finding.sink.location.snippet,
        *(step.location.snippet for step in finding.taint_path),
    ]
    if any(_FIELD_OPERATION_INDICATORS.search(snippet) for snippet in snippets):
        return True

    cache = field_summary_cache or FieldSummaryCache()
    for step in finding.taint_path:
        line_text = cache.read_source_line(step.location.file, step.location.line)
        if line_text is not None and _FIELD_OPERATION_INDICATORS.search(line_text):
            return True
    return False


def _labels_for_sink(
    finding: CandidateFinding,
    state: FieldTaintState,
    *,
    effective_sanitizers: frozenset[str],
) -> list[TaintLabel]:
    sink_expr = finding.sink.location.snippet
    labels: list[TaintLabel] = []
    template_parts = _TEMPLATE_INTERPOLATION_PATTERN.findall(sink_expr)
    if template_parts:
        for part in template_parts:
            label = _lookup_label_for_expression(
                state,
                part,
                method=finding.taint_path[-1].through_function if finding.taint_path else None,
                sink_cwe=finding.vuln_class,
                effective_sanitizers=effective_sanitizers,
            )
            if label is not None:
                labels.append(label)
        return labels

    for reference in candidate_references(sink_expr):
        label = _lookup_label_for_expression(
            state,
            reference,
            method=finding.taint_path[-1].through_function if finding.taint_path else None,
            sink_cwe=finding.vuln_class,
            effective_sanitizers=effective_sanitizers,
        )
        if label is not None:
            labels.append(label)
    return labels


def _lookup_label_for_expression(
    state: FieldTaintState,
    expression: str | None,
    *,
    method: str | None,
    sink_cwe: str | None,
    effective_sanitizers: frozenset[str],
    visited: set[str] | None = None,
) -> TaintLabel | None:
    if expression is None:
        return None
    normalized = normalize_expression(expression)
    if not normalized:
        return None

    active = set() if visited is None else visited
    if normalized in active:
        return None
    active.add(normalized)

    direct_label = _lookup_direct_label(state, normalized, method=method)
    if direct_label is not None:
        return direct_label
    if _is_safe_literal(normalized):
        return None

    direct_source = extract_user_controlled_source(normalized)
    if direct_source is not None:
        if direct_source.parameter_name is None:
            return _lookup_object_field(state, normalized, _WHOLE_OBJECT_FIELD, method=method)
        object_name = _object_parent(normalized)
        if object_name is None:
            return TaintLabel(
                source_id=normalized,
                field_path=direct_source.parameter_name,
                confidence=1.0,
                sanitized_for=frozenset(),
            )
        label = _lookup_object_field(
            state,
            object_name,
            direct_source.parameter_name,
            method=method,
        )
        if label is not None:
            return label

    sanitizer_label = _lookup_sanitizer_result(
        state,
        normalized,
        method=method,
        sink_cwe=sink_cwe,
        effective_sanitizers=effective_sanitizers,
        visited=active,
    )
    if sanitizer_label is not None:
        return sanitizer_label

    json_parse_match = _JSON_PARSE_PATTERN.fullmatch(normalized)
    if json_parse_match is not None:
        base = _lookup_label_for_expression(
            state,
            json_parse_match.group("arg"),
            method=method,
            sink_cwe=sink_cwe,
            effective_sanitizers=effective_sanitizers,
            visited=active,
        )
        if base is None:
            return None
        return TaintLabel(
            source_id=base.source_id,
            field_path="",
            confidence=base.confidence * 0.95,
            sanitized_for=frozenset(),
        )

    json_stringify_match = _JSON_STRINGIFY_PATTERN.fullmatch(normalized)
    if json_stringify_match is not None:
        return _lookup_label_for_expression(
            state,
            json_stringify_match.group("arg"),
            method=method,
            sink_cwe=sink_cwe,
            effective_sanitizers=effective_sanitizers,
            visited=active,
        )

    object_keys_match = _OBJECT_KEYS_PATTERN.fullmatch(normalized)
    if object_keys_match is not None:
        return _lookup_label_for_expression(
            state,
            object_keys_match.group("arg"),
            method=method,
            sink_cwe=sink_cwe,
            effective_sanitizers=effective_sanitizers,
            visited=active,
        )

    property_match = property_reference(normalized)
    if property_match is not None:
        return _lookup_object_field(
            state,
            property_match[0],
            property_match[1],
            method=method,
        )

    computed_match = _COMPUTED_ACCESS_PATTERN.fullmatch(normalized)
    if computed_match is not None:
        key_expression = computed_match.group("key").strip()
        if key_expression.startswith(("'", '"')) and key_expression.endswith(("'", '"')):
            return _lookup_object_field(
                state,
                computed_match.group("object"),
                key_expression[1:-1],
                method=method,
            )
        return _lookup_all_object_fields(state, computed_match.group("object"), method=method)

    if _TEMPLATE_INTERPOLATION_PATTERN.search(normalized):
        labels = [
            label
            for label in (
                _lookup_label_for_expression(
                    state,
                    part,
                    method=method,
                    sink_cwe=sink_cwe,
                    effective_sanitizers=effective_sanitizers,
                    visited=active,
                )
                for part in _TEMPLATE_INTERPOLATION_PATTERN.findall(normalized)
            )
            if label is not None
        ]
        return _merge_labels(labels)

    labels = [
        label
        for label in (
            _lookup_label_for_expression(
                state,
                reference,
                method=method,
                sink_cwe=sink_cwe,
                effective_sanitizers=effective_sanitizers,
                visited=active,
            )
            for reference in candidate_references(normalized)
            if reference != normalized
        )
        if label is not None
    ]
    return _merge_labels(labels)


def _lookup_sanitizer_result(
    state: FieldTaintState,
    expression: str,
    *,
    method: str | None,
    sink_cwe: str | None,
    effective_sanitizers: frozenset[str],
    visited: set[str],
) -> TaintLabel | None:
    for sanitizer_name in sorted(effective_sanitizers, key=len, reverse=True):
        pattern = re.compile(
            _EFFECTIVE_SANITIZER_CALL_PATTERN.format(name=re.escape(sanitizer_name))
        )
        match = pattern.fullmatch(expression)
        if match is None:
            continue
        label = _lookup_label_for_expression(
            state,
            match.group("args"),
            method=method,
            sink_cwe=sink_cwe,
            effective_sanitizers=effective_sanitizers,
            visited=visited,
        )
        if label is None or sink_cwe is None:
            return label
        applicable_cwes = _SANITIZER_CWE_MAP.get(sanitizer_name)
        if applicable_cwes is not None and sink_cwe not in applicable_cwes:
            return label
        return TaintLabel(
            source_id=label.source_id,
            field_path=label.field_path,
            confidence=label.confidence,
            sanitized_for=label.sanitized_for | frozenset({sink_cwe}),
        )
    return None


def _assign_target_from_expression(
    state: FieldTaintState,
    target: str,
    source_expr: str | None,
    *,
    method: str | None,
    sink_cwe: str | None,
    effective_sanitizers: frozenset[str],
) -> None:
    normalized_target = normalize_expression(target)
    normalized_source = normalize_expression(source_expr or "")
    label = _lookup_label_for_expression(
        state,
        normalized_source,
        method=method,
        sink_cwe=sink_cwe,
        effective_sanitizers=effective_sanitizers,
    )

    property_target = property_reference(normalized_target)
    if property_target is not None:
        object_name, field_name = property_target
        if label is None:
            _mark_safe_object_field(state, object_name, field_name, method=method)
            return
        _set_object_field_label(state, object_name, field_name, label, method=method)
        return

    if label is None:
        _clear_identifier(state, normalized_target, method)
        return

    _set_identifier_label(state, normalized_target, label, method=method)
    if _expression_is_object_like(state, normalized_source, method=method):
        _copy_object_expression(
            state,
            normalized_target,
            normalized_source,
            method=method,
            sink_cwe=sink_cwe,
            effective_sanitizers=effective_sanitizers,
        )
    elif any(token in normalized_source for token in (".", "[")) or label.field_path == "":
        _set_object_whole_label(state, normalized_target, label, method=method)


def _copy_object_expression(
    state: FieldTaintState,
    target: str,
    source_expr: str,
    *,
    method: str | None,
    sink_cwe: str | None,
    effective_sanitizers: frozenset[str],
) -> None:
    source_object_key = _object_key(source_expr, method=method)
    if source_object_key in state.object_labels:
        state.object_labels[_object_key(target, method=method)] = dict(
            state.object_labels[source_object_key]
        )
        safe_fields = state.object_safe_fields.get(source_object_key)
        if safe_fields:
            state.object_safe_fields[_object_key(target, method=method)] = set(safe_fields)
        label = state.object_labels[source_object_key].get(_WHOLE_OBJECT_FIELD)
        if label is not None:
            _set_identifier_label(state, target, label, method=method)
        return

    label = _lookup_label_for_expression(
        state,
        source_expr,
        method=method,
        sink_cwe=sink_cwe,
        effective_sanitizers=effective_sanitizers,
    )
    if label is not None:
        _set_object_whole_label(state, target, label, method=method)
        _set_identifier_label(state, target, label, method=method)


def _lookup_direct_label(
    state: FieldTaintState,
    expression: str,
    *,
    method: str | None,
) -> TaintLabel | None:
    key = _expression_key(expression, method)
    if key in state.taint_labels:
        return state.taint_labels[key]
    if expression in state.taint_labels:
        return state.taint_labels[expression]
    if _IDENTIFIER_PATTERN.fullmatch(expression):
        return state.taint_labels.get(_scoped_identifier(expression, method))
    return None


def _lookup_object_field(
    state: FieldTaintState,
    object_name: str,
    field_name: str,
    *,
    method: str | None,
) -> TaintLabel | None:
    object_key = _object_key(object_name, method=method)
    object_labels = state.object_labels.get(object_key)
    if object_labels is None and object_name != object_key:
        object_labels = state.object_labels.get(object_name)
    if object_labels is None:
        return None
    if field_name != _WHOLE_OBJECT_FIELD and field_name in state.object_safe_fields.get(
        object_key, set()
    ):
        return None
    direct = object_labels.get(field_name)
    if direct is not None:
        return direct
    whole = object_labels.get(_WHOLE_OBJECT_FIELD)
    if whole is None:
        return None
    if field_name == _WHOLE_OBJECT_FIELD:
        return whole
    return _append_field_to_label(whole, field_name)


def _lookup_all_object_fields(
    state: FieldTaintState,
    object_name: str,
    *,
    method: str | None,
) -> TaintLabel | None:
    object_key = _object_key(object_name, method=method)
    object_labels = state.object_labels.get(object_key)
    if object_labels is None and object_name != object_key:
        object_labels = state.object_labels.get(object_name)
    if not object_labels:
        return None
    labels = [
        label
        for field_name, label in object_labels.items()
        if field_name == _WHOLE_OBJECT_FIELD
        or field_name not in state.object_safe_fields.get(object_key, set())
    ]
    return _merge_labels(labels)


def _set_identifier_label(
    state: FieldTaintState,
    identifier: str,
    label: TaintLabel,
    *,
    method: str | None,
) -> None:
    state.taint_labels[_scoped_identifier(identifier, method)] = label


def _set_object_whole_label(
    state: FieldTaintState,
    object_name: str,
    label: TaintLabel,
    *,
    method: str | None = None,
) -> None:
    state.object_labels.setdefault(_object_key(object_name, method=method), {})[
        _WHOLE_OBJECT_FIELD
    ] = label


def _set_object_field_label(
    state: FieldTaintState,
    object_name: str,
    field_name: str,
    label: TaintLabel,
    *,
    method: str | None = None,
) -> None:
    object_key = _object_key(object_name, method=method)
    state.object_labels.setdefault(object_key, {})[field_name] = label
    state.object_safe_fields.get(object_key, set()).discard(field_name)


def _mark_safe_object_field(
    state: FieldTaintState,
    object_name: str,
    field_name: str,
    *,
    method: str | None,
) -> None:
    object_key = _object_key(object_name, method=method)
    state.object_labels.setdefault(object_key, {}).pop(field_name, None)
    state.object_safe_fields.setdefault(object_key, set()).add(field_name)


def _clear_identifier(state: FieldTaintState, identifier: str, method: str | None) -> None:
    state.taint_labels.pop(_scoped_identifier(identifier, method), None)
    _clear_object_state(state, identifier, method)


def _clear_object_state(state: FieldTaintState, identifier: str, method: str | None) -> None:
    object_key = _object_key(identifier, method=method)
    state.object_labels.pop(object_key, None)
    state.object_safe_fields.pop(object_key, None)


def _expression_is_object_like(
    state: FieldTaintState,
    expression: str,
    *,
    method: str | None,
) -> bool:
    normalized = normalize_expression(expression)
    if _MULTI_FIELD_SOURCE_PATTERN.fullmatch(normalized):
        return True
    if _JSON_PARSE_PATTERN.fullmatch(normalized):
        return True
    return _object_key(normalized, method=method) in state.object_labels


def _parse_destructuring_bindings(bindings: str) -> tuple[tuple[str, str], ...]:
    resolved: list[tuple[str, str]] = []
    for raw_binding in bindings.split(","):
        binding = raw_binding.strip()
        if not binding or binding.startswith("..."):
            continue
        binding = binding.split("=", 1)[0].strip()
        if ":" in binding:
            source_name, _, target_name = binding.partition(":")
            resolved.append((source_name.strip(), target_name.strip()))
        else:
            resolved.append((binding, binding))
    return tuple(resolved)


def _explicit_object_keys(body: str) -> set[str]:
    keys: set[str] = set()
    for match in _OBJECT_LITERAL_KEY_PATTERN.finditer(body):
        key = match.group("key").strip()
        if key.startswith(("'", '"')) and key.endswith(("'", '"')):
            key = key[1:-1]
        keys.add(key)
    return keys


def _index_by_line[T](nodes: Sequence[T]) -> dict[int, tuple[T, ...]]:
    indexed: dict[int, list[T]] = {}
    for node in nodes:
        line_number = getattr(node, "line_number", None)
        if not isinstance(line_number, int):
            continue
        indexed.setdefault(line_number, []).append(node)
    return {line_number: tuple(items) for line_number, items in indexed.items()}


def _append_field_to_label(label: TaintLabel, field_name: str) -> TaintLabel:
    new_path = field_name if not label.field_path else f"{label.field_path}.{field_name}"
    return TaintLabel(
        source_id=label.source_id,
        field_path=new_path,
        confidence=label.confidence,
        sanitized_for=label.sanitized_for,
    )


def _merge_labels(labels: Iterable[TaintLabel]) -> TaintLabel | None:
    materialized = list(labels)
    if not materialized:
        return None
    first = materialized[0]
    field_paths = {label.field_path for label in materialized}
    sanitized_sets = [set(label.sanitized_for) for label in materialized]
    intersection = set.intersection(*sanitized_sets) if sanitized_sets else set()
    return TaintLabel(
        source_id=first.source_id,
        field_path=first.field_path if len(field_paths) == 1 else "",
        confidence=max(label.confidence for label in materialized),
        sanitized_for=frozenset(intersection),
    )


def _effective_sanitizers(finding: CandidateFinding) -> tuple[str, ...]:
    value = finding.metadata.get("effective_sanitizers")
    if not isinstance(value, list):
        return ()
    return tuple(item for item in value if isinstance(item, str))


def _contains_effective_sanitizer_call(text: str, effective_sanitizers: frozenset[str]) -> bool:
    return any(
        re.search(
            _EFFECTIVE_SANITIZER_CALL_PATTERN.format(name=re.escape(name)),
            text,
        )
        for name in effective_sanitizers
    )


def _scoped_identifier(identifier: str, method: str | None) -> str:
    normalized = normalize_expression(identifier)
    if method is None or not _IDENTIFIER_PATTERN.fullmatch(normalized):
        return normalized
    return f"{method}::{normalized}"


def _object_key(object_name: str, *, method: str | None) -> str:
    normalized = normalize_expression(object_name)
    if method is None or not _IDENTIFIER_PATTERN.fullmatch(normalized):
        return normalized
    return f"{method}::{normalized}"


def _expression_key(expression: str, method: str | None) -> str:
    normalized = normalize_expression(expression)
    if _IDENTIFIER_PATTERN.fullmatch(normalized):
        return _scoped_identifier(normalized, method)
    return normalized


def _object_parent(expression: str) -> str | None:
    property_match = property_reference(expression)
    if property_match is not None:
        return property_match[0]
    computed_match = _COMPUTED_ACCESS_PATTERN.fullmatch(expression)
    if computed_match is not None:
        return computed_match.group("object")
    return None


def _is_safe_literal(expression: str) -> bool:
    normalized = expression.strip()
    if not normalized:
        return True
    if normalized in {"true", "false", "null", "undefined"}:
        return True
    if normalized.startswith(("'", '"', "`")) and normalized.endswith(("'", '"', "`")):
        return not _TEMPLATE_INTERPOLATION_PATTERN.search(normalized)
    return bool(re.fullmatch(r"-?\d+(?:\.\d+)?", normalized))


def _coerce_int(value: object) -> int | None:
    if isinstance(value, int):
        return value
    if isinstance(value, str) and value.isdigit():
        return int(value)
    return None


__all__ = [
    "FieldMethodSummary",
    "FieldOp",
    "FieldSummaryCache",
    "FieldTaintState",
    "FieldTaintStep",
    "StepClassification",
    "TaintLabel",
    "annotate_flow_with_fields",
    "apply_field_sensitive_pruning",
    "classify_step_operation",
    "propagate_field_taint",
    "prune_untainted_fields",
]
