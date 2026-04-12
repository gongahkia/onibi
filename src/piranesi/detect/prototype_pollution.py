from __future__ import annotations

import hashlib
import re
from collections.abc import Sequence
from dataclasses import dataclass
from pathlib import Path

from piranesi.detect._javascript_taint import (
    detect_magic_prototype_path,
    extract_user_controlled_source,
    normalize_expression,
)
from piranesi.detect._source_scan import ScannedSourceFile, iter_scanned_source_files
from piranesi.models import CandidateFinding, SourceLocation, TaintSink, TaintSource, TaintStep
from piranesi.scan.specs import SinkSpec
from piranesi.scan.transpile import SourceMap

_DEFAULT_DATA_CATEGORIES = ["unknown"]
_DEFAULT_CONFIDENCE = 0.85
_DEFAULT_SEVERITY = "high"

_VARIABLE_ASSIGNMENT_PATTERN = re.compile(
    r"^\s*(?:const|let|var)\s+(?P<name>[A-Za-z_$][\w$]*)\s*=\s*(?P<expr>.+?)\s*;?\s*$"
)
_INDEX_ASSIGNMENT_PATTERN = re.compile(
    r"^\s*(?P<object>[A-Za-z_$][\w$]*)(?:\[(?P<key_expr>[^\]]+)\])(?:\[(?P<nested_key>[^\]]+)\])?\s*=\s*(?P<value_expr>.+?)\s*;?\s*$"
)
_OBJECT_ASSIGN_PATTERN = re.compile(r"Object\.assign\((?P<args>.+)\)")
_LODASH_MERGE_PATTERN = re.compile(r"(?P<api>(?:_|lodash)\.merge)\((?P<args>.+)\)")
_DEFAULTS_DEEP_PATTERN = re.compile(r"(?P<api>(?:_|lodash)\.defaultsDeep)\((?P<args>.+)\)")
_CUSTOM_MERGE_CALL_PATTERN = re.compile(r"^\s*(?P<api>[A-Za-z_$][\w$]*)\((?P<args>.+)\)")
_FUNCTION_START_PATTERN = re.compile(r"^\s*(?:export\s+)?function\s+[A-Za-z_$][\w$]*\s*\(")


@dataclass(frozen=True, slots=True)
class _UserControlledOrigin:
    source_type: str
    parameter_name: str | None
    location: SourceLocation


@dataclass(frozen=True, slots=True)
class _PollutedObject:
    object_name: str
    origin: _UserControlledOrigin
    assignment_location: SourceLocation
    magic_path: str | None


@dataclass(frozen=True, slots=True)
class _MergeSinkMatch:
    sink_spec: SinkSpec
    api_name: str
    argument_name: str
    location: SourceLocation


def extract_prototype_pollution_findings(
    project_root: str | Path,
    *,
    source_map: SourceMap | None = None,
    sink_specs: Sequence[SinkSpec] | None = None,
    files: Sequence[Path] | None = None,
) -> tuple[CandidateFinding, ...]:
    active_sink_specs = tuple(spec for spec in sink_specs or () if spec.cwe_id == "CWE-1321")
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
    findings: list[CandidateFinding] = []
    tainted_variables: dict[str, _UserControlledOrigin] = {}
    tainted_objects: dict[str, _UserControlledOrigin] = {}
    polluted_objects: dict[str, _PollutedObject] = {}
    recursive_merge_names = _recursive_merge_functions(scanned_file)

    for line_number, line in enumerate(scanned_file.lines, start=1):
        stripped = line.strip()
        if not stripped or stripped.startswith("//"):
            continue
        if _FUNCTION_START_PATTERN.match(stripped):
            tainted_variables = {}
            tainted_objects = {}
            polluted_objects = {}
        location = scanned_file.location_for_line(line_number, snippet=line)
        _track_user_controlled_assignments(
            stripped,
            location=location,
            tainted_variables=tainted_variables,
            tainted_objects=tainted_objects,
        )
        polluted = _polluted_object_from_assignment(
            stripped,
            location=location,
            tainted_variables=tainted_variables,
        )
        if polluted is not None:
            polluted_objects[polluted.object_name] = polluted

        for sink_match in _merge_sink_matches(
            stripped,
            location=location,
            sink_specs_by_name=sink_specs_by_name,
            recursive_merge_names=recursive_merge_names,
        ):
            finding = _finding_for_sink(
                sink_match,
                polluted_objects=polluted_objects,
                tainted_objects=tainted_objects,
            )
            if finding is not None:
                findings.append(finding)

    return findings


def _track_user_controlled_assignments(
    line: str,
    *,
    location: SourceLocation,
    tainted_variables: dict[str, _UserControlledOrigin],
    tainted_objects: dict[str, _UserControlledOrigin],
) -> None:
    match = _VARIABLE_ASSIGNMENT_PATTERN.match(line)
    if match is None:
        return
    expression = normalize_expression(match.group("expr"))
    direct_source = extract_user_controlled_source(expression)
    if direct_source is None and expression in tainted_variables:
        tainted_variables[match.group("name")] = tainted_variables[expression]
        return
    if direct_source is None and expression in tainted_objects:
        tainted_objects[match.group("name")] = tainted_objects[expression]
        return
    if direct_source is None:
        tainted_variables.pop(match.group("name"), None)
        tainted_objects.pop(match.group("name"), None)
        return
    origin = _UserControlledOrigin(
        source_type=direct_source.source_type,
        parameter_name=direct_source.parameter_name,
        location=location,
    )
    if direct_source.parameter_name is None:
        tainted_objects[match.group("name")] = origin
    else:
        tainted_variables[match.group("name")] = origin


def _polluted_object_from_assignment(
    line: str,
    *,
    location: SourceLocation,
    tainted_variables: dict[str, _UserControlledOrigin],
) -> _PollutedObject | None:
    match = _INDEX_ASSIGNMENT_PATTERN.match(line)
    if match is None:
        return None
    key_expr = normalize_expression(match.group("key_expr"))
    direct_source = extract_user_controlled_source(key_expr)
    origin = (
        _UserControlledOrigin(
            source_type=direct_source.source_type,
            parameter_name=direct_source.parameter_name,
            location=location,
        )
        if direct_source is not None
        else tainted_variables.get(key_expr)
    )
    if origin is None:
        return None
    magic_path = detect_magic_prototype_path(line)
    if magic_path is None:
        literal_key = key_expr.strip("'\"")
        if literal_key in {"__proto__", "constructor", "prototype"}:
            magic_path = literal_key
    nested_key = match.group("nested_key")
    if nested_key is not None:
        nested_magic = detect_magic_prototype_path(nested_key)
        if nested_magic is not None:
            magic_path = nested_magic
        else:
            nested_literal = normalize_expression(nested_key).strip("'\"")
            if nested_literal == "__proto__":
                magic_path = "__proto__"
            elif nested_literal == "prototype":
                magic_path = "constructor.prototype"
            elif nested_literal == "constructor":
                magic_path = "constructor"
    return _PollutedObject(
        object_name=match.group("object"),
        origin=origin,
        assignment_location=location,
        magic_path=magic_path,
    )


def _merge_sink_matches(
    line: str,
    *,
    location: SourceLocation,
    sink_specs_by_name: dict[str, SinkSpec],
    recursive_merge_names: frozenset[str],
) -> tuple[_MergeSinkMatch, ...]:
    matches: list[_MergeSinkMatch] = []
    builtins = (
        ("prototype_pollution_object_assign", _OBJECT_ASSIGN_PATTERN, "Object.assign", 1),
        ("prototype_pollution_lodash_merge", _LODASH_MERGE_PATTERN, None, 1),
        ("prototype_pollution_defaults_deep", _DEFAULTS_DEEP_PATTERN, None, 1),
    )
    for sink_name, pattern, explicit_api, argument_index in builtins:
        sink_spec = sink_specs_by_name.get(sink_name)
        if sink_spec is None:
            continue
        match = pattern.search(line)
        if match is None:
            continue
        args = _split_args(match.group("args"))
        if len(args) <= argument_index:
            continue
        api_name = explicit_api or match.groupdict().get("api") or sink_name
        matches.append(
            _MergeSinkMatch(
                sink_spec=sink_spec,
                api_name=api_name,
                argument_name=normalize_expression(args[argument_index]),
                location=location,
            )
        )

    custom_spec = sink_specs_by_name.get("prototype_pollution_custom_merge")
    if custom_spec is not None:
        custom_match = _CUSTOM_MERGE_CALL_PATTERN.search(line)
        if custom_match is not None and custom_match.group("api") in recursive_merge_names:
            args = _split_args(custom_match.group("args"))
            if len(args) >= 2:
                matches.append(
                    _MergeSinkMatch(
                        sink_spec=custom_spec,
                        api_name=custom_match.group("api"),
                        argument_name=normalize_expression(args[1]),
                        location=location,
                    )
                )
    return tuple(matches)


def _finding_for_sink(
    sink_match: _MergeSinkMatch,
    *,
    polluted_objects: dict[str, _PollutedObject],
    tainted_objects: dict[str, _UserControlledOrigin],
) -> CandidateFinding | None:
    polluted = polluted_objects.get(sink_match.argument_name)
    direct_object = tainted_objects.get(sink_match.argument_name)
    direct_source = extract_user_controlled_source(sink_match.argument_name)
    if direct_object is None and direct_source is not None:
        direct_object = _UserControlledOrigin(
            source_type=direct_source.source_type,
            parameter_name=direct_source.parameter_name,
            location=sink_match.location.model_copy(update={"snippet": direct_source.expression}),
        )
    if polluted is None and direct_object is None:
        return None

    if polluted is not None:
        source_location = polluted.origin.location
        parameter_name = polluted.origin.parameter_name
        source_type = polluted.origin.source_type
        magic_path = polluted.magic_path
        taint_path = [
            TaintStep(
                location=source_location,
                operation="assignment",
                taint_state="tainted",
                through_function=None,
            ),
            TaintStep(
                location=polluted.assignment_location,
                operation="property_access",
                taint_state="tainted",
                through_function=None,
            ),
            TaintStep(
                location=sink_match.location,
                operation="call_arg",
                taint_state="tainted",
                through_function=None,
            ),
        ]
    else:
        assert direct_object is not None
        source_location = direct_object.location
        parameter_name = direct_object.parameter_name
        source_type = direct_object.source_type
        magic_path = detect_magic_prototype_path(sink_match.argument_name)
        taint_path = [
            TaintStep(
                location=source_location,
                operation="assignment",
                taint_state="tainted",
                through_function=None,
            ),
            TaintStep(
                location=sink_match.location,
                operation="call_arg",
                taint_state="tainted",
                through_function=None,
            ),
        ]

    finding_id = hashlib.sha256(
        (
            f"prototype-pollution|{source_location.file}:{source_location.line}:{source_location.column}|"
            f"{sink_match.location.file}:{sink_match.location.line}:{sink_match.location.column}|"
            f"{sink_match.api_name}"
        ).encode()
    ).hexdigest()

    return CandidateFinding(
        id=finding_id,
        vuln_class="CWE-1321",
        source=TaintSource(
            location=source_location,
            source_type=source_type,
            data_categories=list(_DEFAULT_DATA_CATEGORIES),
            parameter_name=parameter_name,
        ),
        sink=TaintSink(
            location=sink_match.location,
            sink_type=sink_match.sink_spec.sink_type.value,
            api_name=sink_match.api_name,
        ),
        taint_path=taint_path,
        path_conditions=[],
        confidence=_DEFAULT_CONFIDENCE,
        severity=sink_match.sink_spec.severity or _DEFAULT_SEVERITY,
        metadata={
            "detector": "prototype_pollution",
            "magic_property": magic_path,
            "argument_name": sink_match.argument_name,
        },
    )


def _recursive_merge_functions(scanned_file: ScannedSourceFile) -> frozenset[str]:
    names: set[str] = set()
    function_pattern = re.compile(r"^\s*function\s+(?P<name>[A-Za-z_$][\w$]*)\s*\(")
    for index, line in enumerate(scanned_file.lines):
        match = function_pattern.match(line)
        if match is None:
            continue
        name = match.group("name")
        window = "\n".join(scanned_file.lines[index : index + 12])
        if name in {"merge", "deepMerge", "defaultsDeep"} and (
            window.count(f"{name}(") > 1 and ("for (" in window or "Object.keys" in window)
        ):
            names.add(name)
    return frozenset(names)


def _split_args(args: str) -> tuple[str, ...]:
    result: list[str] = []
    current: list[str] = []
    depth = 0
    string_quote: str | None = None
    for char in args:
        if string_quote is not None:
            current.append(char)
            if char == string_quote:
                string_quote = None
            continue
        if char in {"'", '"'}:
            current.append(char)
            string_quote = char
            continue
        if char in "([{":
            depth += 1
            current.append(char)
            continue
        if char in ")]}":
            depth = max(0, depth - 1)
            current.append(char)
            continue
        if char == "," and depth == 0:
            result.append("".join(current).strip())
            current = []
            continue
        current.append(char)
    if current:
        result.append("".join(current).strip())
    return tuple(item for item in result if item)


def _dedupe_findings(findings: Sequence[CandidateFinding]) -> list[CandidateFinding]:
    deduped: list[CandidateFinding] = []
    seen: set[tuple[object, ...]] = set()
    for finding in findings:
        key = (
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


__all__ = ["extract_prototype_pollution_findings"]
