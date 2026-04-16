from __future__ import annotations

import hashlib
import re
from collections.abc import Sequence
from dataclasses import dataclass
from pathlib import Path

from piranesi.detect._javascript_taint import (
    candidate_references,
    extract_user_controlled_source,
    normalize_expression,
    property_reference,
)
from piranesi.detect._source_scan import ScannedSourceFile, iter_scanned_source_files
from piranesi.models import CandidateFinding, SourceLocation, TaintSink, TaintSource, TaintStep
from piranesi.scan.specs import SinkSpec, SourceSpec
from piranesi.scan.transpile import SourceMap

_DEFAULT_DATA_CATEGORIES = ["unknown"]
_DEFAULT_CONFIDENCE = 0.7
_DEFAULT_SEVERITY = "medium"
_SEVERITY_BY_CWE = {
    "CWE-22": "medium",
    "CWE-78": "critical",
    "CWE-79": "medium",
    "CWE-89": "high",
    "CWE-94": "critical",
    "CWE-113": "medium",
    "CWE-601": "medium",
    "CWE-918": "high",
    "CWE-942": "high",
}

_VARIABLE_ASSIGNMENT_PATTERN = re.compile(
    r"^\s*(?:const|let|var)\s+(?P<name>[A-Za-z_$][\w$]*)\s*=\s*(?P<expr>.+?)\s*;?\s*$"
)
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
_FUNCTION_START_PATTERN = re.compile(r"^\s*(?:export\s+)?function\s+[A-Za-z_$][\w$]*\s*\(")


@dataclass(frozen=True, slots=True)
class _TraceStep:
    location: SourceLocation
    operation: str


@dataclass(frozen=True, slots=True)
class _TaintOrigin:
    source_type: str
    parameter_name: str | None
    source_location: SourceLocation
    trace: tuple[_TraceStep, ...]
    alias_related: bool

    def with_step(
        self,
        *,
        location: SourceLocation,
        operation: str,
        alias_related: bool = False,
    ) -> _TaintOrigin:
        return _TaintOrigin(
            source_type=self.source_type,
            parameter_name=self.parameter_name,
            source_location=self.source_location,
            trace=(*self.trace, _TraceStep(location=location, operation=operation)),
            alias_related=self.alias_related or alias_related,
        )

    def with_parameter(self, parameter_name: str | None) -> _TaintOrigin:
        return _TaintOrigin(
            source_type=self.source_type,
            parameter_name=parameter_name if parameter_name is not None else self.parameter_name,
            source_location=self.source_location,
            trace=self.trace,
            alias_related=self.alias_related,
        )


@dataclass(slots=True)
class _AliasState:
    variables: dict[str, _TaintOrigin]
    properties: dict[tuple[str, str], _TaintOrigin]
    objects: dict[str, _TaintOrigin]
    object_safe_properties: dict[str, set[str]]


@dataclass(frozen=True, slots=True)
class _SinkMatch:
    sink_spec: SinkSpec
    api_name: str
    expression: str
    location: SourceLocation


@dataclass(frozen=True, slots=True)
class _SinkPattern:
    sink_name: str
    pattern: re.Pattern[str]
    api_group: str
    expression_group: str


_SINK_PATTERNS: tuple[_SinkPattern, ...] = (
    _SinkPattern(
        sink_name="raw_sql_query",
        pattern=re.compile(
            r"(?P<api>(?:[A-Za-z_$][\w$]*\.)?(?:query|[$]queryRaw|[$]executeRaw|raw))\(\s*(?P<expr>[^,\n]+)"
        ),
        api_group="api",
        expression_group="expr",
    ),
    _SinkPattern(
        sink_name="child_process_exec",
        pattern=re.compile(
            r"(?P<api>(?:[A-Za-z_$][\w$]*\.)?(?:exec|execSync))\(\s*(?P<expr>[^,\n]+)"
        ),
        api_group="api",
        expression_group="expr",
    ),
    _SinkPattern(
        sink_name="child_process_spawn",
        pattern=re.compile(
            r"(?P<api>(?:[A-Za-z_$][\w$]*\.)?(?:spawn|spawnSync))\(\s*(?P<expr>[^,\n]+)"
        ),
        api_group="api",
        expression_group="expr",
    ),
    _SinkPattern(
        sink_name="dynamic_eval",
        pattern=re.compile(r"(?P<api>(?:eval|Function))\(\s*(?P<expr>[^,\n]+)"),
        api_group="api",
        expression_group="expr",
    ),
    _SinkPattern(
        sink_name="response_output",
        pattern=re.compile(
            r"(?P<api>[A-Za-z_$][\w$]*\.(?:send|render|write))\(\s*(?P<expr>[^,\n]+)"
        ),
        api_group="api",
        expression_group="expr",
    ),
    _SinkPattern(
        sink_name="fastify_reply_send",
        pattern=re.compile(r"(?P<api>[A-Za-z_$][\w$]*\.send)\(\s*(?P<expr>[^,\n]+)"),
        api_group="api",
        expression_group="expr",
    ),
    _SinkPattern(
        sink_name="filesystem_read",
        pattern=re.compile(
            r"(?P<api>(?:[A-Za-z_$][\w$]*\.)?(?:readFile|readFileSync))\(\s*(?P<expr>[^,\n]+)"
        ),
        api_group="api",
        expression_group="expr",
    ),
    _SinkPattern(
        sink_name="filesystem_write",
        pattern=re.compile(
            r"(?P<api>(?:[A-Za-z_$][\w$]*\.)?(?:writeFile|writeFileSync))\(\s*(?P<expr>[^,\n]+)"
        ),
        api_group="api",
        expression_group="expr",
    ),
    _SinkPattern(
        sink_name="ssrf_full_url",
        pattern=re.compile(
            r"(?P<api>(?:fetch|axios\.[A-Za-z_$][\w$]*|https?\.[A-Za-z_$][\w$]*|got(?:\.[A-Za-z_$][\w$]*)?|needle(?:\.[A-Za-z_$][\w$]*)?|superagent(?:\.[A-Za-z_$][\w$]*)?|undici(?:\.[A-Za-z_$][\w$]*)?))\(\s*(?P<expr>[^,\n]+)"
        ),
        api_group="api",
        expression_group="expr",
    ),
    _SinkPattern(
        sink_name="ssrf_path_segment",
        pattern=re.compile(
            r"(?P<api>(?:fetch|axios\.[A-Za-z_$][\w$]*|https?\.[A-Za-z_$][\w$]*|got(?:\.[A-Za-z_$][\w$]*)?|needle(?:\.[A-Za-z_$][\w$]*)?|superagent(?:\.[A-Za-z_$][\w$]*)?|undici(?:\.[A-Za-z_$][\w$]*)?))\(\s*(?P<expr>[^,\n]+)"
        ),
        api_group="api",
        expression_group="expr",
    ),
    _SinkPattern(
        sink_name="json_parse_user_input",
        pattern=re.compile(r"(?P<api>JSON\.parse)\(\s*(?P<expr>[^,\n]+)"),
        api_group="api",
        expression_group="expr",
    ),
    _SinkPattern(
        sink_name="express_redirect",
        pattern=re.compile(r"(?P<api>[A-Za-z_$][\w$]*\.redirect)\(\s*(?P<expr>[^,\n]+)"),
        api_group="api",
        expression_group="expr",
    ),
    _SinkPattern(
        sink_name="location_header_set",
        pattern=re.compile(
            r"(?P<api>[A-Za-z_$][\w$]*\.(?:setHeader|writeHead))\(\s*['\"]Location['\"]\s*,\s*(?P<expr>[^,\n]+)"
        ),
        api_group="api",
        expression_group="expr",
    ),
    _SinkPattern(
        sink_name="fastify_reply_header",
        pattern=re.compile(
            r"(?P<api>[A-Za-z_$][\w$]*\.header)\(\s*['\"]Location['\"]\s*,\s*(?P<expr>[^,\n]+)"
        ),
        api_group="api",
        expression_group="expr",
    ),
    _SinkPattern(
        sink_name="cors_allow_origin_reflection",
        pattern=re.compile(
            r"(?P<api>[A-Za-z_$][\w$]*\.setHeader)\(\s*['\"]Access-Control-Allow-Origin['\"]\s*,\s*(?P<expr>[^,\n]+)"
        ),
        api_group="api",
        expression_group="expr",
    ),
)


def extract_alias_findings(
    project_root: str | Path,
    *,
    source_map: SourceMap | None = None,
    sink_specs: Sequence[SinkSpec] | None = None,
    source_specs: Sequence[SourceSpec] | None = None,
    files: Sequence[Path] | None = None,
) -> tuple[CandidateFinding, ...]:
    del source_specs
    active_sink_specs = tuple(spec for spec in sink_specs or () if spec.cwe_id != "CWE-1321")
    if not active_sink_specs:
        return ()

    findings: list[CandidateFinding] = []
    for scanned_file in iter_scanned_source_files(project_root, source_map=source_map, files=files):
        findings.extend(_scan_file(scanned_file, sink_specs=active_sink_specs))
    return tuple(_dedupe_findings(findings))


def _scan_file(
    scanned_file: ScannedSourceFile, *, sink_specs: Sequence[SinkSpec]
) -> list[CandidateFinding]:
    sink_specs_by_name = {spec.name: spec for spec in sink_specs}
    state = _AliasState(variables={}, properties={}, objects={}, object_safe_properties={})
    findings: list[CandidateFinding] = []

    for line_number, line in enumerate(scanned_file.lines, start=1):
        stripped = line.strip()
        if not stripped or stripped.startswith("//"):
            continue
        if _FUNCTION_START_PATTERN.match(stripped):
            state = _AliasState(variables={}, properties={}, objects={}, object_safe_properties={})

        line_location = scanned_file.location_for_line(line_number, snippet=line)
        _process_destructuring_assignment(state, stripped, line_location)
        _process_spread_assignment(state, stripped, line_location)
        _process_variable_assignment(state, stripped, line_location)
        _process_property_assignment(state, stripped, line_location)

        for sink_match in _sink_matches(
            line,
            line_number=line_number,
            scanned_file=scanned_file,
            sink_specs_by_name=sink_specs_by_name,
        ):
            origin = _origin_for_expression(
                sink_match.expression,
                state=state,
                location=sink_match.location,
            )
            if origin is None or not origin.alias_related:
                continue
            findings.append(_build_finding(origin=origin, sink_match=sink_match))

    return findings


def _process_variable_assignment(
    state: _AliasState,
    line: str,
    location: SourceLocation,
) -> None:
    match = _VARIABLE_ASSIGNMENT_PATTERN.match(line)
    if match is None:
        return
    name = match.group("name")
    expr = normalize_expression(match.group("expr"))
    origin = _origin_for_expression(expr, state=state, location=location)
    if origin is not None:
        if origin.parameter_name is None:
            state.objects[name] = origin.with_step(location=location, operation="assignment")
            state.object_safe_properties.pop(name, None)
        else:
            state.variables[name] = origin.with_step(location=location, operation="assignment")
    else:
        state.variables.pop(name, None)
        state.objects.pop(name, None)
        state.object_safe_properties.pop(name, None)


def _process_destructuring_assignment(
    state: _AliasState,
    line: str,
    location: SourceLocation,
) -> None:
    match = _DESTRUCTURING_PATTERN.match(line)
    if match is None:
        return
    origin = _origin_for_expression(match.group("expr"), state=state, location=location)
    if origin is None:
        return
    for binding in _parse_destructuring_bindings(match.group("bindings")):
        parameter_name = binding[0]
        variable_name = binding[1]
        state.variables[variable_name] = origin.with_parameter(parameter_name).with_step(
            location=location,
            operation="assignment",
            alias_related=True,
        )


def _process_spread_assignment(
    state: _AliasState,
    line: str,
    location: SourceLocation,
) -> None:
    match = _SPREAD_ASSIGNMENT_PATTERN.match(line)
    if match is None:
        return
    spread_match = _SPREAD_OPERAND_PATTERN.search(match.group("body"))
    if spread_match is None:
        return
    origin = _origin_for_expression(spread_match.group("expr"), state=state, location=location)
    if origin is None:
        return
    object_name = match.group("name")
    state.objects[object_name] = origin.with_step(
        location=location,
        operation="assignment",
        alias_related=True,
    )
    state.object_safe_properties[object_name] = _explicit_object_keys(match.group("body"))


def _process_property_assignment(
    state: _AliasState,
    line: str,
    location: SourceLocation,
) -> None:
    match = _PROPERTY_ASSIGNMENT_PATTERN.match(line)
    if match is None:
        return
    object_name = match.group("object")
    field = match.group("field") or match.group("field_bracket")
    if field is None:
        return
    origin = _origin_for_expression(match.group("expr"), state=state, location=location)
    key = (object_name, field)
    if origin is not None:
        propagated_origin = (
            origin.with_parameter(field) if origin.parameter_name is None else origin
        )
        state.properties[key] = propagated_origin.with_step(
            location=location,
            operation="property_access",
            alias_related=True,
        )
        state.object_safe_properties.get(object_name, set()).discard(field)
    else:
        state.properties.pop(key, None)
        state.object_safe_properties.setdefault(object_name, set()).add(field)


def _origin_for_expression(
    expression: str,
    *,
    state: _AliasState,
    location: SourceLocation,
) -> _TaintOrigin | None:
    direct_source = extract_user_controlled_source(expression)
    if direct_source is not None:
        return _TaintOrigin(
            source_type=direct_source.source_type,
            parameter_name=direct_source.parameter_name,
            source_location=location.model_copy(update={"snippet": direct_source.expression}),
            trace=(),
            alias_related=False,
        )

    normalized = normalize_expression(expression)
    if normalized in state.variables:
        return state.variables[normalized]
    if normalized in state.objects:
        return state.objects[normalized]

    property_ref = property_reference(normalized)
    if property_ref is not None:
        object_name, field = property_ref
        property_origin = state.properties.get((object_name, field))
        if property_origin is not None:
            return property_origin
        if field in state.object_safe_properties.get(object_name, set()):
            return None
        object_origin = state.objects.get(object_name)
        if object_origin is not None:
            return object_origin.with_parameter(field).with_step(
                location=location,
                operation="property_access",
                alias_related=True,
            )

    for reference in candidate_references(normalized):
        if reference == normalized:
            continue
        nested_origin = _origin_for_expression(reference, state=state, location=location)
        if nested_origin is not None:
            return nested_origin
    return None


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


def _sink_matches(
    line: str,
    *,
    line_number: int,
    scanned_file: ScannedSourceFile,
    sink_specs_by_name: dict[str, SinkSpec],
) -> tuple[_SinkMatch, ...]:
    matches: list[_SinkMatch] = []
    for sink_pattern in _SINK_PATTERNS:
        sink_spec = sink_specs_by_name.get(sink_pattern.sink_name)
        if sink_spec is None:
            continue
        for match in sink_pattern.pattern.finditer(line):
            matches.append(
                _SinkMatch(
                    sink_spec=sink_spec,
                    api_name=match.group(sink_pattern.api_group),
                    expression=match.group(sink_pattern.expression_group),
                    location=scanned_file.location_for_index(
                        sum(len(current) + 1 for current in scanned_file.lines[: line_number - 1])
                        + match.start(sink_pattern.api_group),
                        snippet=line,
                    ),
                )
            )
    return tuple(matches)


def _build_finding(*, origin: _TaintOrigin, sink_match: _SinkMatch) -> CandidateFinding:
    source_location = origin.source_location
    vuln_class = sink_match.sink_spec.cwe_id or sink_match.sink_spec.sink_type.value
    finding_id = hashlib.sha256(
        (
            f"alias|{vuln_class}|{source_location.file}:{source_location.line}:{source_location.column}|"
            f"{sink_match.location.file}:{sink_match.location.line}:{sink_match.location.column}|"
            f"{sink_match.api_name}"
        ).encode()
    ).hexdigest()
    taint_path = [
        TaintStep(
            location=source_location,
            operation="assignment",
            taint_state="tainted",
            through_function=None,
        )
    ]
    for trace_step in origin.trace:
        taint_path.append(
            TaintStep(
                location=trace_step.location,
                operation=trace_step.operation,
                taint_state="tainted",
                through_function=None,
            )
        )
    taint_path.append(
        TaintStep(
            location=sink_match.location,
            operation="call_arg",
            taint_state="tainted",
            through_function=None,
        )
    )

    return CandidateFinding(
        id=finding_id,
        vuln_class=vuln_class,
        source=TaintSource(
            location=source_location,
            source_type=origin.source_type,
            data_categories=list(_DEFAULT_DATA_CATEGORIES),
            parameter_name=origin.parameter_name,
        ),
        sink=TaintSink(
            location=sink_match.location,
            sink_type=sink_match.sink_spec.sink_type.value,
            api_name=sink_match.api_name,
        ),
        taint_path=taint_path,
        path_conditions=[],
        confidence=_DEFAULT_CONFIDENCE,
        severity=sink_match.sink_spec.severity
        or _SEVERITY_BY_CWE.get(vuln_class, _DEFAULT_SEVERITY),
        metadata={
            "detector": "alias",
            "sink_spec_name": sink_match.sink_spec.name,
            "sink_spec_category": sink_match.sink_spec.sink_type.value,
            "sink_spec_cwe": sink_match.sink_spec.cwe_id,
            "sink_spec_custom": sink_match.sink_spec.is_custom,
        },
    )


def _dedupe_findings(findings: Sequence[CandidateFinding]) -> list[CandidateFinding]:
    deduped: list[CandidateFinding] = []
    seen: set[tuple[object, ...]] = set()
    for finding in findings:
        key = (
            finding.vuln_class,
            finding.source.location.file,
            finding.source.location.line,
            finding.source.parameter_name,
            finding.sink.location.file,
            finding.sink.location.line,
            finding.sink.api_name,
        )
        if key in seen:
            continue
        deduped.append(finding)
        seen.add(key)
    return deduped


__all__ = ["extract_alias_findings"]
