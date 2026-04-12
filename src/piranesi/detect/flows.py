from __future__ import annotations

import contextlib
import hashlib
import json
import re
from collections.abc import Mapping, Sequence
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from piranesi.detect.alias import extract_alias_findings
from piranesi.detect.categories import classify_candidate_findings
from piranesi.detect.conditions import ConditionExtractionError, PathConditionExtractor
from piranesi.detect.field_taint import apply_field_sensitive_pruning
from piranesi.detect.injection_variants import should_report_injection_variant
from piranesi.detect.interprocedural import extract_interprocedural_findings
from piranesi.detect.prototype_pollution import extract_prototype_pollution_findings
from piranesi.detect.sanitizer_validation import (
    PARTIAL_CONFIDENCE_REDUCTION,
    SANITIZER_BYPASS_CONFIDENCE_BOOST,
    SanitizerEffectiveness,
    detect_sanitizer_bypass,
    validate_sanitizer_spec,
)
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
    normalize_flow_elements_for_sink_spec,
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
_VARIABLE_PATTERN = r"[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*|\[['\"][^'\"]+['\"]\])*"
_FIELD_SEGMENT_PATTERN = re.compile(r"\.([A-Za-z_$][\w$]*)|\[['\"]([^'\"]+)['\"]\]")
_CALL_PREFIX_PATTERN = re.compile(r"^\s*(?:new\s+)?([^(]+?)\s*\(")
_IDENTIFIER_SUFFIX_PATTERN = re.compile(r"[A-Za-z_$][\w$]*$")
_STATIC_FALSE_PATTERN = re.compile(
    r'^\s*(?:false|0|""|\'\'|\(\s*false\s*\)|\(\s*0\s*\)|\(\s*""\s*\)|\(\s*\'\'\s*\))\s*$',
    re.IGNORECASE,
)
_STATIC_TRUE_PATTERN = re.compile(
    r"^\s*(?:true|1|\(\s*true\s*\)|\(\s*1\s*\))\s*$",
    re.IGNORECASE,
)
_TYPEOF_NUMBER_PATTERN = re.compile(
    rf"^\s*typeof\s+(?P<var>{_VARIABLE_PATTERN})\s*(?P<op>===|==|!==|!=)\s*"
    r'(?P<quote>["\'])number(?P=quote)\s*$'
)
_INTEGER_GUARD_PATTERN = re.compile(
    rf"^\s*(?P<neg>!)?\s*(?:Number\.)?isInteger\(\s*(?P<var>{_VARIABLE_PATTERN})\s*\)\s*$"
)
_ALLOWLIST_GUARD_PATTERN = re.compile(
    rf"^\s*(?P<neg>!)?\s*(?P<receiver>{_VARIABLE_PATTERN})\.includes\(\s*"
    rf"(?P<argument>{_VARIABLE_PATTERN})\s*\)\s*$"
)
_STRING_EXPECTING_SQL_OPERATIONS = frozenset({"<operator>.addition", "<operator>.formatString"})

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
    "CWE-502": "high",
    "CWE-918": "high",
    "CWE-942": "high",
    "CWE-79": "medium",
    "CWE-22": "medium",
    "CWE-434": "high",
    "CWE-601": "medium",
    "CWE-1021": "medium",
    "CWE-693": "medium",
    "CWE-319": "medium",
    "CWE-614": "medium",
    "CWE-1004": "medium",
    "CWE-1321": "high",
}
_FIELD_SENSITIVE_PRUNING_CWES = frozenset({"CWE-79", "CWE-89"})
_GENERIC_SOURCE_PARAMETER_NAMES = frozenset(
    {
        "body",
        "query",
        "params",
        "param",
        "headers",
        "cookies",
        "request",
        "req",
        "payload",
        "input",
        "data",
    }
)


class FlowExtractionError(RuntimeError):
    """Raised when Joern returns an unexpected data-flow payload."""


@dataclass(frozen=True, slots=True)
class _MethodSummary:
    method_name: str | None
    method_full_name: str | None

    @classmethod
    def from_json(cls, payload: JsonDict) -> _MethodSummary:
        return cls(
            method_name=_coerce_optional_str(payload.get("name")),
            method_full_name=_coerce_optional_str(payload.get("fullName")),
        )


@dataclass(frozen=True, slots=True)
class _ControlStructureSummary:
    control_id: int
    code: str
    condition_code: str
    control_type: str | None
    line_number: int | None
    column_number: int | None

    @classmethod
    def from_json(cls, payload: JsonDict) -> _ControlStructureSummary:
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
class _TruthGuard:
    variable: str
    truth_means_match: bool
    requires_call: str | None = None


@dataclass(frozen=True, slots=True)
class _AllowlistGuard:
    receiver: str
    argument: str
    truth_means_allowed: bool


@dataclass(frozen=True, slots=True)
class _SanitizerObservation:
    spec_name: str
    display_name: str
    effectiveness: SanitizerEffectiveness


@dataclass(frozen=True, slots=True)
class _SanitizerPathAssessment:
    confidence: float
    suppressed: bool
    suppression_reason: str | None
    metadata: dict[str, object]


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


@dataclass(slots=True)
class _PathPruningAnalyzer:
    server: JoernServer
    source_map: SourceMap | None
    file_resolver: _NodeFileResolver
    _method_for_node_cache: dict[int, _MethodSummary | None] = field(default_factory=dict)
    _method_controls_cache: dict[str, tuple[_ControlStructureSummary, ...]] = field(
        default_factory=dict
    )
    _branch_ast_cache: dict[tuple[int, int], frozenset[int]] = field(default_factory=dict)
    _branch_return_cache: dict[tuple[int, int], bool] = field(default_factory=dict)
    _control_call_cache: dict[tuple[int, str], bool] = field(default_factory=dict)
    _control_location_cache: dict[int, SourceLocation] = field(default_factory=dict)
    _source_lines_cache: dict[Path, tuple[str, ...]] = field(default_factory=dict)

    def prune_for_type_narrowing(
        self,
        *,
        elements: Sequence[QueryNode],
        sink_spec: SinkSpec,
        sink_location: SourceLocation,
        path_conditions: Sequence[PathCondition],
    ) -> bool:
        if not _sink_expects_string(elements, sink_spec):
            return False

        flow_variables = _flow_variable_names(elements)
        if _path_conditions_narrow_to_number(path_conditions, flow_variables):
            return True

        sink_node = elements[-1]
        for control in self._controls_before_sink(sink_node, sink_location):
            guard = _parse_numeric_guard(control.condition_code or control.code)
            if guard is None or not _matches_flow_variable(guard.variable, flow_variables):
                continue
            if guard.requires_call is not None and not self._control_uses_call(
                control.control_id,
                guard.requires_call,
            ):
                continue
            if self._condition_holds_on_path(control.control_id, elements, guard.truth_means_match):
                return True
        return False

    def reduce_confidence_for_allowlist(
        self,
        base_confidence: float,
        *,
        elements: Sequence[QueryNode],
        sink_location: SourceLocation,
    ) -> float:
        flow_variables = _flow_variable_names(elements)
        sink_node = elements[-1]
        for control in self._controls_before_sink(sink_node, sink_location):
            guard = _parse_allowlist_guard(control.condition_code or control.code)
            if guard is None or not _matches_flow_variable(guard.argument, flow_variables):
                continue
            if not self._control_uses_call(control.control_id, "includes"):
                continue
            control_location = self._control_location(control)
            if not self._receiver_is_allowlist_array(guard.receiver, control_location):
                continue
            if self._condition_holds_on_path(
                control.control_id, elements, guard.truth_means_allowed
            ):
                return min(base_confidence, 0.1)
        return base_confidence

    def _controls_before_sink(
        self,
        sink_node: QueryNode,
        sink_location: SourceLocation,
    ) -> tuple[_ControlStructureSummary, ...]:
        method = self._method_for_node(sink_node)
        if method is None or not method.method_full_name:
            return ()

        relevant_controls: list[_ControlStructureSummary] = []
        for control in self._controls_for_method(method.method_full_name):
            control_location = self._control_location(control)
            if control_location.file != sink_location.file:
                continue
            if control_location.line >= sink_location.line:
                continue
            relevant_controls.append(control)
        return tuple(relevant_controls)

    def _method_for_node(self, node: QueryNode) -> _MethodSummary | None:
        if node.node_id < 0:
            return None
        if node.node_id in self._method_for_node_cache:
            return self._method_for_node_cache[node.node_id]

        query = _node_method_query(node)
        if query is None:
            self._method_for_node_cache[node.node_id] = None
            return None

        payload = execute_json_query(self.server, query)
        if not isinstance(payload, list) or not all(isinstance(item, dict) for item in payload):
            raise FlowExtractionError(
                f"Unexpected enclosing method payload for node {node.node_id}: {payload!r}"
            )

        resolved = _MethodSummary.from_json(payload[0]) if payload else None
        self._method_for_node_cache[node.node_id] = resolved
        return resolved

    def _controls_for_method(self, method_full_name: str) -> tuple[_ControlStructureSummary, ...]:
        if method_full_name in self._method_controls_cache:
            return self._method_controls_cache[method_full_name]

        payload = execute_json_query(self.server, _method_controls_query(method_full_name))
        if not isinstance(payload, list) or not all(isinstance(item, dict) for item in payload):
            raise FlowExtractionError(
                f"Unexpected method control payload for {method_full_name}: {payload!r}"
            )

        controls = tuple(
            control
            for control in (_ControlStructureSummary.from_json(item) for item in payload)
            if control.control_id >= 0
        )
        self._method_controls_cache[method_full_name] = controls
        return controls

    def _control_location(self, control: _ControlStructureSummary) -> SourceLocation:
        if control.control_id not in self._control_location_cache:
            self._control_location_cache[control.control_id] = _source_location_for_node(
                control.to_query_node(),
                source_map=self.source_map,
                file_resolver=self.file_resolver,
            )
        return self._control_location_cache[control.control_id]

    def _control_uses_call(self, control_id: int, call_name: str) -> bool:
        cache_key = (control_id, call_name)
        if cache_key in self._control_call_cache:
            return self._control_call_cache[cache_key]

        payload = execute_json_query(self.server, _control_call_query(control_id, call_name))
        if not isinstance(payload, list):
            raise FlowExtractionError(
                f"Unexpected control-call payload for control {control_id}: {payload!r}"
            )
        result = bool(payload)
        self._control_call_cache[cache_key] = result
        return result

    def _condition_holds_on_path(
        self,
        control_id: int,
        elements: Sequence[QueryNode],
        truth_means_match: bool,
    ) -> bool:
        outcome = self._branch_outcome_for_elements(control_id, elements)
        if outcome is None:
            outcome = self._fallthrough_outcome(control_id)
        return outcome is truth_means_match

    def _branch_outcome_for_elements(
        self,
        control_id: int,
        elements: Sequence[QueryNode],
    ) -> bool | None:
        affected_node_ids = {node.node_id for node in elements if node.node_id >= 0}
        if not affected_node_ids:
            return None

        true_ids = self._branch_ast_ids(control_id, order=2)
        false_ids = self._branch_ast_ids(control_id, order=3)
        in_true = bool(affected_node_ids & true_ids)
        in_false = bool(affected_node_ids & false_ids)
        if in_true and not in_false:
            return True
        if in_false and not in_true:
            return False
        return None

    def _branch_ast_ids(self, control_id: int, *, order: int) -> frozenset[int]:
        cache_key = (control_id, order)
        if cache_key in self._branch_ast_cache:
            return self._branch_ast_cache[cache_key]

        payload = execute_json_query(self.server, _branch_ast_ids_query(control_id, order))
        if not isinstance(payload, list):
            raise FlowExtractionError(
                f"Unexpected branch AST payload for control {control_id} order {order}: {payload!r}"
            )

        node_ids = frozenset(_coerce_int(item, default=-1) for item in payload)
        cleaned = frozenset(node_id for node_id in node_ids if node_id >= 0)
        self._branch_ast_cache[cache_key] = cleaned
        return cleaned

    def _fallthrough_outcome(self, control_id: int) -> bool | None:
        true_returns = self._branch_has_return(control_id, order=2)
        false_returns = self._branch_has_return(control_id, order=3)
        if true_returns == false_returns:
            return None
        return not true_returns

    def _branch_has_return(self, control_id: int, *, order: int) -> bool:
        cache_key = (control_id, order)
        if cache_key in self._branch_return_cache:
            return self._branch_return_cache[cache_key]

        payload = execute_json_query(self.server, _branch_return_query(control_id, order))
        if not isinstance(payload, list):
            raise FlowExtractionError(
                f"Unexpected branch return payload for control "
                f"{control_id} order {order}: {payload!r}"
            )

        result = bool(payload)
        self._branch_return_cache[cache_key] = result
        return result

    def _receiver_is_allowlist_array(
        self,
        receiver: str,
        control_location: SourceLocation,
    ) -> bool:
        if not control_location.file or control_location.file.startswith("<"):
            return False

        receiver_name = _identifier_suffix(receiver)
        if receiver_name is None:
            return False

        source_path = Path(control_location.file)
        if not source_path.exists():
            return False

        lines = self._source_lines(source_path)
        start_line = max(0, control_location.line - 13)
        pattern = re.compile(rf"\b(?:const|let|var)\s+{re.escape(receiver_name)}\s*=\s*\[")
        return any(pattern.search(line) for line in lines[start_line : control_location.line - 1])

    def _source_lines(self, source_path: Path) -> tuple[str, ...]:
        if source_path not in self._source_lines_cache:
            self._source_lines_cache[source_path] = tuple(
                source_path.read_text(encoding="utf-8").splitlines()
            )
        return self._source_lines_cache[source_path]


def extract_candidate_findings(
    server: JoernServer,
    *,
    joern_project_root: str | Path,
    source_map: SourceMap | None = None,
    source_specs: Sequence[SourceSpec] | None = None,
    sink_specs: Sequence[SinkSpec] | None = None,
    sanitizer_specs: Sequence[SanitizerSpec] | None = None,
    frameworks: Sequence[str] | None = None,
    route_patterns_by_finding_id: Mapping[str, str | None] | None = None,
    category_provider: Any | None = None,
    category_model: str | None = None,
    field_sensitive: bool = True,
) -> tuple[CandidateFinding, ...]:
    root = Path(joern_project_root).resolve(strict=False)
    resolved_source_specs = tuple(source_specs or get_source_specs(frameworks=frameworks))
    resolved_sink_specs = tuple(sink_specs or get_sink_specs(frameworks=frameworks))
    resolved_sanitizer_specs = tuple(sanitizer_specs or get_sanitizer_specs(frameworks=frameworks))
    flow_sink_specs = tuple(
        spec for spec in resolved_sink_specs if spec.sink_type is not SinkType.PROTOTYPE_POLLUTION
    )
    sanitizer_lookup = _collect_sanitizer_lookup(server, resolved_sanitizer_specs)
    file_resolver = _NodeFileResolver(server=server, joern_project_root=root, source_map=source_map)
    pruning_analyzer = _PathPruningAnalyzer(
        server=server,
        source_map=source_map,
        file_resolver=file_resolver,
    )
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
        for sink_spec in flow_sink_specs:
            findings.extend(
                _extract_findings_for_pair(
                    server,
                    source_spec=source_spec,
                    sink_spec=sink_spec,
                    source_map=source_map,
                    file_resolver=file_resolver,
                    sanitizer_lookup=sanitizer_lookup,
                    condition_extractor=condition_extractor,
                    pruning_analyzer=pruning_analyzer,
                )
            )
    if source_map is not None:
        findings.extend(
            extract_interprocedural_findings(
                server,
                joern_project_root=root,
                source_map=source_map,
                source_specs=resolved_source_specs,
                sink_specs=flow_sink_specs,
            )
        )
    findings.extend(
        extract_alias_findings(
            root,
            source_map=source_map,
            sink_specs=resolved_sink_specs,
            source_specs=resolved_source_specs,
        )
    )
    findings.extend(
        extract_prototype_pollution_findings(
            root,
            source_map=source_map,
            sink_specs=resolved_sink_specs,
        )
    )
    if field_sensitive:
        prunable = [
            finding for finding in findings if finding.vuln_class in _FIELD_SENSITIVE_PRUNING_CWES
        ]
        non_prunable = [
            finding
            for finding in findings
            if finding.vuln_class not in _FIELD_SENSITIVE_PRUNING_CWES
        ]
        if prunable:
            findings = [
                *apply_field_sensitive_pruning(
                    prunable,
                    server,
                    source_specs=resolved_source_specs,
                ),
                *non_prunable,
            ]
    findings = _suppress_interprocedural_generic_source_duplicates(findings)
    classified = classify_candidate_findings(
        findings,
        route_patterns_by_finding_id=route_patterns_by_finding_id,
        provider=category_provider,
        model=category_model,
    )
    return tuple(_dedupe_candidate_findings(classified))


def joern_flow_to_taint_steps(
    flow: Sequence[QueryNode],
    *,
    source_map: SourceMap | None,
    file_resolver: _NodeFileResolver,
    sanitizer_lookup: Mapping[int, SanitizerSpec],
    vuln_class: str | None = None,
) -> list[TaintStep]:
    steps: list[TaintStep] = []
    active_sanitizer: str | None = None

    for node in flow:
        sanitizer = _sanitizer_for_node(
            node,
            sanitizer_lookup=sanitizer_lookup,
            vuln_class=vuln_class,
        )
        if (
            sanitizer is not None
            and sanitizer.effectiveness is not SanitizerEffectiveness.INEFFECTIVE
        ):
            active_sanitizer = sanitizer.display_name

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


def _severity_for_sink_spec(sink_spec: SinkSpec) -> str:
    if sink_spec.severity is not None:
        return sink_spec.severity
    return severity_for_cwe(sink_spec.cwe_id)


def candidate_finding_id(
    *,
    vuln_class: str,
    source_function_name: str | None,
    sink_function_name: str | None,
    path_length: int,
) -> str:
    material = _LOCATION_SEPARATOR.join(
        [
            vuln_class,
            _stable_function_name(source_function_name, fallback="unknown_source"),
            _stable_function_name(sink_function_name, fallback="unknown_sink"),
            str(path_length),
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
    sanitizer_lookup: Mapping[int, SanitizerSpec],
    condition_extractor: PathConditionExtractor,
    pruning_analyzer: _PathPruningAnalyzer,
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

        elements = normalize_flow_elements_for_sink_spec(server, sink_spec, elements)
        if not elements:
            continue
        if not should_report_injection_variant(
            source_spec=source_spec,
            sink_spec=sink_spec,
            elements=elements,
        ):
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

        try:
            path_conditions = condition_extractor.extract(elements)
        except (ConditionExtractionError, CPGQLQueryError):
            path_conditions = []
        if _path_is_unreachable(path_conditions):
            continue
        try:
            if pruning_analyzer.prune_for_type_narrowing(
                elements=elements,
                sink_spec=sink_spec,
                sink_location=sink_location,
                path_conditions=path_conditions,
            ):
                continue
        except (CPGQLQueryError, FlowExtractionError):
            pass

        vuln_class = sink_spec.cwe_id or sink_spec.sink_type.value
        sanitizer_assessment = _assess_sanitizers_for_path(
            _DEFAULT_CONFIDENCE,
            elements=elements,
            sanitizer_lookup=sanitizer_lookup,
            vuln_class=sink_spec.cwe_id,
        )
        confidence = sanitizer_assessment.confidence
        with contextlib.suppress(CPGQLQueryError, FlowExtractionError):
            confidence = pruning_analyzer.reduce_confidence_for_allowlist(
                confidence,
                elements=elements,
                sink_location=sink_location,
            )
        findings.append(
            CandidateFinding(
                id=candidate_finding_id(
                    vuln_class=vuln_class,
                    source_function_name=source_node.method_full_name,
                    sink_function_name=sink_node.method_full_name,
                    path_length=len(elements),
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
                    vuln_class=sink_spec.cwe_id,
                ),
                path_conditions=path_conditions,
                confidence=confidence,
                severity=_severity_for_sink_spec(sink_spec),
                metadata=sanitizer_assessment.metadata,
                suppressed=sanitizer_assessment.suppressed,
                suppression_reason=sanitizer_assessment.suppression_reason,
            )
        )

    return findings


def _path_is_unreachable(path_conditions: Sequence[PathCondition]) -> bool:
    for condition in path_conditions:
        expression = condition.expression.strip()
        if condition.required_value and _is_statically_false(expression):
            return True
        if not condition.required_value and _is_statically_true(expression):
            return True
    return False


def _stable_function_name(value: str | None, *, fallback: str) -> str:
    if value is None:
        return fallback
    normalized = value.strip()
    return normalized or fallback


def _collect_sanitizer_lookup(
    server: JoernServer,
    sanitizer_specs: Sequence[SanitizerSpec],
) -> dict[int, SanitizerSpec]:
    sanitizer_lookup: dict[int, SanitizerSpec] = {}
    for sanitizer_spec in sanitizer_specs:
        for node in execute_sanitizer_query(server, sanitizer_spec):
            if node.node_id < 0:
                continue
            sanitizer_lookup[node.node_id] = sanitizer_spec
    return sanitizer_lookup


def _reduced_confidence_for_path(
    base_confidence: float,
    *,
    elements: Sequence[QueryNode],
    sanitizer_lookup: Mapping[int, SanitizerSpec],
    vuln_class: str | None,
) -> float:
    observations = _relevant_sanitizers_on_path(
        elements,
        sanitizer_lookup=sanitizer_lookup,
        vuln_class=vuln_class,
    )
    partial_count = sum(
        1 for sanitizer in observations if sanitizer.effectiveness is SanitizerEffectiveness.PARTIAL
    )
    confidence = base_confidence - (PARTIAL_CONFIDENCE_REDUCTION * partial_count)
    return max(0.0, min(1.0, confidence))


def _dedupe_candidate_findings(findings: Sequence[CandidateFinding]) -> list[CandidateFinding]:
    deduped: list[CandidateFinding] = []
    seen: set[tuple[object, ...]] = set()
    for finding in findings:
        key = (
            finding.vuln_class,
            finding.source.location.file,
            finding.source.location.line,
            finding.source.location.column,
            finding.source.parameter_name,
            finding.sink.location.file,
            finding.sink.location.line,
            finding.sink.location.column,
            finding.sink.api_name,
        )
        if key in seen:
            continue
        deduped.append(finding)
        seen.add(key)
    return deduped


def _suppress_interprocedural_generic_source_duplicates(
    findings: Sequence[CandidateFinding],
) -> list[CandidateFinding]:
    by_sink: dict[tuple[object, ...], list[CandidateFinding]] = {}
    for finding in findings:
        key = (
            finding.vuln_class,
            finding.sink.location.file,
            finding.sink.location.line,
            finding.sink.location.column,
            finding.sink.api_name,
        )
        by_sink.setdefault(key, []).append(finding)

    drop_instances: set[int] = set()
    for sink_findings in by_sink.values():
        has_specific_source = any(
            not _is_generic_source_parameter_name(finding.source.parameter_name)
            for finding in sink_findings
        )
        for finding in sink_findings:
            is_generic = _is_generic_source_parameter_name(finding.source.parameter_name)
            if has_specific_source and is_generic:
                drop_instances.add(id(finding))

    return [finding for finding in findings if id(finding) not in drop_instances]


def _is_generic_source_parameter_name(parameter_name: str | None) -> bool:
    if parameter_name is None:
        return False
    return parameter_name.strip().lower() in _GENERIC_SOURCE_PARAMETER_NAMES


def _relevant_sanitizers_on_path(
    elements: Sequence[QueryNode],
    *,
    sanitizer_lookup: Mapping[int, SanitizerSpec],
    vuln_class: str | None,
) -> tuple[_SanitizerObservation, ...]:
    relevant_sanitizers: list[_SanitizerObservation] = []
    seen_names: set[str] = set()

    for node in elements:
        sanitizer = _sanitizer_for_node(
            node,
            sanitizer_lookup=sanitizer_lookup,
            vuln_class=vuln_class,
        )
        if sanitizer is None or sanitizer.spec_name in seen_names:
            continue
        seen_names.add(sanitizer.spec_name)
        relevant_sanitizers.append(sanitizer)

    return tuple(relevant_sanitizers)


def _path_conditions_narrow_to_number(
    path_conditions: Sequence[PathCondition],
    flow_variables: frozenset[str],
) -> bool:
    for condition in path_conditions:
        guard = _parse_numeric_guard(condition.expression)
        if guard is None or not _matches_flow_variable(guard.variable, flow_variables):
            continue
        if condition.required_value == guard.truth_means_match:
            return True
    return False


def _sink_expects_string(elements: Sequence[QueryNode], sink_spec: SinkSpec) -> bool:
    if sink_spec.sink_type is SinkType.HTML_OUTPUT:
        return True
    if sink_spec.sink_type is not SinkType.SQL_QUERY:
        return False
    return any(node.name in _STRING_EXPECTING_SQL_OPERATIONS for node in elements)


def _parse_numeric_guard(expression: str) -> _TruthGuard | None:
    typeof_match = _TYPEOF_NUMBER_PATTERN.match(expression.strip())
    if typeof_match is not None:
        return _TruthGuard(
            variable=typeof_match.group("var"),
            truth_means_match=typeof_match.group("op") in {"==", "==="},
        )

    integer_match = _INTEGER_GUARD_PATTERN.match(expression.strip())
    if integer_match is not None:
        return _TruthGuard(
            variable=integer_match.group("var"),
            truth_means_match=integer_match.group("neg") is None,
            requires_call="isInteger",
        )

    return None


def _parse_allowlist_guard(expression: str) -> _AllowlistGuard | None:
    match = _ALLOWLIST_GUARD_PATTERN.match(expression.strip())
    if match is None:
        return None

    return _AllowlistGuard(
        receiver=match.group("receiver"),
        argument=match.group("argument"),
        truth_means_allowed=match.group("neg") is None,
    )


def _flow_variable_names(elements: Sequence[QueryNode]) -> frozenset[str]:
    names: set[str] = set()
    for node in elements:
        if node.name and not node.name.startswith("<operator>"):
            names.add(node.name)
        if node.code:
            names.add(node.code.strip())
            suffix = _identifier_suffix(node.code)
            if suffix is not None:
                names.add(suffix)
    return frozenset(name for name in names if name)


def _matches_flow_variable(candidate: str, flow_variables: frozenset[str]) -> bool:
    if candidate in flow_variables:
        return True
    suffix = _identifier_suffix(candidate)
    return suffix is not None and suffix in flow_variables


def _identifier_suffix(value: str) -> str | None:
    extracted = _extract_parameter_name(value)
    if extracted is not None:
        return extracted
    match = _IDENTIFIER_SUFFIX_PATTERN.search(value.strip())
    if match is None:
        return None
    return match.group(0)


def _is_statically_false(expression: str) -> bool:
    return _STATIC_FALSE_PATTERN.fullmatch(expression.strip()) is not None


def _is_statically_true(expression: str) -> bool:
    return _STATIC_TRUE_PATTERN.fullmatch(expression.strip()) is not None


def _sanitizer_for_node(
    node: QueryNode,
    *,
    sanitizer_lookup: Mapping[int, SanitizerSpec],
    vuln_class: str | None,
) -> _SanitizerObservation | None:
    if node.node_id < 0:
        return None
    sanitizer_spec = sanitizer_lookup.get(node.node_id)
    if sanitizer_spec is None:
        return None
    effectiveness = (
        SanitizerEffectiveness.EFFECTIVE
        if vuln_class is None
        else validate_sanitizer_spec(sanitizer_spec, vuln_class)
    )
    return _SanitizerObservation(
        spec_name=sanitizer_spec.name,
        display_name=_sanitizer_name(node, fallback=sanitizer_spec.name),
        effectiveness=effectiveness,
    )


def _assess_sanitizers_for_path(
    base_confidence: float,
    *,
    elements: Sequence[QueryNode],
    sanitizer_lookup: Mapping[int, SanitizerSpec],
    vuln_class: str | None,
) -> _SanitizerPathAssessment:
    observations = _relevant_sanitizers_on_path(
        elements,
        sanitizer_lookup=sanitizer_lookup,
        vuln_class=vuln_class,
    )
    if not observations:
        return _SanitizerPathAssessment(
            confidence=base_confidence,
            suppressed=False,
            suppression_reason=None,
            metadata={},
        )

    effectiveness_by_name = {
        sanitizer.spec_name: sanitizer.effectiveness.value for sanitizer in observations
    }
    bypass_patterns = detect_sanitizer_bypass(elements)
    if bypass_patterns:
        return _SanitizerPathAssessment(
            confidence=min(1.0, base_confidence + SANITIZER_BYPASS_CONFIDENCE_BOOST),
            suppressed=False,
            suppression_reason=None,
            metadata={
                "sanitizer_effectiveness": effectiveness_by_name,
                "sanitizer_bypassed": True,
                "sanitizer_bypass_patterns": list(bypass_patterns),
            },
        )

    effective = [
        sanitizer.display_name
        for sanitizer in observations
        if sanitizer.effectiveness is SanitizerEffectiveness.EFFECTIVE
    ]
    partial = [
        sanitizer.display_name
        for sanitizer in observations
        if sanitizer.effectiveness is SanitizerEffectiveness.PARTIAL
    ]
    ineffective = [
        sanitizer.display_name
        for sanitizer in observations
        if sanitizer.effectiveness is SanitizerEffectiveness.INEFFECTIVE
    ]

    metadata: dict[str, object] = {"sanitizer_effectiveness": effectiveness_by_name}
    if partial:
        metadata["partial_sanitizers"] = partial
    if ineffective:
        metadata["ineffective_sanitizers"] = ineffective
    if effective:
        metadata["effective_sanitizers"] = effective
        return _SanitizerPathAssessment(
            confidence=base_confidence,
            suppressed=True,
            suppression_reason=f"effective sanitizer for {vuln_class}: {', '.join(effective)}",
            metadata=metadata,
        )

    return _SanitizerPathAssessment(
        confidence=_reduced_confidence_for_path(
            base_confidence,
            elements=elements,
            sanitizer_lookup=sanitizer_lookup,
            vuln_class=vuln_class,
        ),
        suppressed=False,
        suppression_reason=None,
        metadata=metadata,
    )


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


def _node_method_query(node: QueryNode) -> str | None:
    node_root = _node_query_root(node)
    if node_root is None:
        return None
    return (
        f'{node_root}.method.map(m => Map("name" -> m.name, "fullName" -> m.fullName)).toJsonPretty'
    )


def _method_controls_query(method_full_name: str) -> str:
    method = json.dumps(method_full_name)
    return (
        f"cpg.method.fullNameExact({method})"
        ".ast.isControlStructure.map(c => Map("
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


def _branch_return_query(control_id: int, order: int) -> str:
    return f"cpg.id({control_id}L).astChildren.order({order}).ast.isReturn.id.toJsonPretty"


def _control_call_query(control_id: int, call_name: str) -> str:
    return f"cpg.id({control_id}L).condition.ast.isCallTo({json.dumps(call_name)}).id.toJsonPretty"


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
        stripped = code.strip()
        if _IDENTIFIER_SUFFIX_PATTERN.fullmatch(stripped) is not None:
            return stripped
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
    "FlowExtractionError",
    "candidate_finding_id",
    "classify_operation",
    "extract_candidate_findings",
    "joern_flow_to_taint_steps",
    "severity_for_cwe",
]
