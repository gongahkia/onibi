from __future__ import annotations

import hashlib
import re
import time
from collections import defaultdict
from collections.abc import Mapping, Sequence
from dataclasses import dataclass, field
from pathlib import Path

from piranesi.config import DetectConfig
from piranesi.models import (
    CandidateFinding,
    SourceLocation,
    TaintSink,
    TaintSource,
    TaintStep,
)
from piranesi.scan.joern import JoernServer
from piranesi.scan.queries import (
    CPGQLQueryError,
    QueryNode,
    execute_json_query,
    execute_sink_query,
    execute_source_query,
)
from piranesi.scan.specs import SinkSpec, SourceSpec
from piranesi.scan.transpile import SourceMap

_DEFAULT_CONFIDENCE = 0.82
_DEFAULT_SEVERITY = "medium"
_LOCATION_SEPARATOR = "|"
_IDENTIFIER_PATTERN = re.compile(r"[A-Za-z_$][\w$]*")
_ASSIGNMENT_PATTERN = re.compile(
    r"^(?:const|let|var)\s+(?P<name>[A-Za-z_$][\w$]*)\s*=\s*(?P<expr>.+)$|"
    r"^(?P<target>[A-Za-z_$][\w$]*)\s*=\s*(?P<reassign>.+)$",
    re.DOTALL,
)
_FUNCTION_DECL_PATTERN = re.compile(
    r"(?P<prefix>(?:^|\s))(?:export\s+)?(?:async\s+)?function\s+"
    r"(?P<name>[A-Za-z_$][\w$]*)\s*\(",
)
_FUNCTION_EXPR_PATTERN = re.compile(
    r"(?P<prefix>(?:^|\s))(?:export\s+)?(?:const|let|var)\s+"
    r"(?P<name>[A-Za-z_$][\w$]*)\s*=\s*(?:async\s+)?function\s*\(",
)
_ARROW_FUNCTION_PATTERN = re.compile(
    r"(?P<prefix>(?:^|\s))(?:export\s+)?(?:const|let|var)\s+"
    r"(?P<name>[A-Za-z_$][\w$]*)\s*=\s*(?:async\s+)?",
)
_STRING_LITERAL_PATTERN = re.compile(r"^(['\"])(.*)\1$")
_TOP_LEVEL_FILE_EXTENSIONS = frozenset({".js", ".jsx", ".ts", ".tsx"})
_HOF_NAMES = frozenset({"map", "filter", "forEach", "reduce"})
_EVENT_ON_NAMES = frozenset({"on", "addListener", "once"})
_EVENT_EMIT_NAMES = frozenset({"emit"})
_KNOWN_ERROR_PARAM_NAMES = frozenset({"err", "error", "_err", "_error"})
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
}

type SummaryOrigin = int


@dataclass(frozen=True, slots=True)
class TaintTransfer:
    from_param_index: int
    to_return: bool = False
    to_sink: str | None = None
    sink_api_name: str | None = None
    sink_file: str | None = None
    sink_line: int | None = None
    sink_column: int | None = None
    sink_snippet: str | None = None
    via_callback_param_index: int | None = None
    to_callback_argument_index: int | None = None
    confidence: float = 1.0


@dataclass(frozen=True, slots=True)
class FunctionSummary:
    function_name: str
    module_path: str
    definition_line: int
    parameter_names: tuple[str, ...]
    transfers: frozenset[TaintTransfer]


@dataclass(frozen=True, slots=True)
class _ResolvedNodeLocation:
    generated_file: Path
    generated_line: int
    generated_column: int
    file: str
    line: int
    column: int
    snippet: str

    def to_source_location(self) -> SourceLocation:
        return SourceLocation(
            file=self.file,
            line=self.line,
            column=self.column,
            snippet=self.snippet,
        )


@dataclass(frozen=True, slots=True)
class _SourceFact:
    spec_name: str
    source_type: str
    parameter_name: str | None
    location: _ResolvedNodeLocation


@dataclass(frozen=True, slots=True)
class _SinkFact:
    spec: SinkSpec
    api_name: str
    location: _ResolvedNodeLocation


@dataclass(frozen=True, slots=True)
class _InlineFunction:
    params: tuple[str, ...]
    body: str
    expression_body: bool
    start_index: int


@dataclass(frozen=True, slots=True)
class _FunctionDef:
    name: str
    file_path: Path
    start_index: int
    end_index: int
    body_start_index: int
    body_end_index: int
    start_line: int
    end_line: int
    params: tuple[str, ...]
    body: str
    is_module_scope: bool = False


@dataclass(frozen=True, slots=True)
class _CallExpression:
    callee: str
    receiver: str | None
    args: tuple[str, ...]
    start_index: int
    end_index: int
    raw: str


@dataclass(frozen=True, slots=True)
class _EventHandler:
    receiver: str
    event_name: str
    callback_text: str
    callback_start_index: int
    callback_function_name: str | None
    owner: _FunctionDef


@dataclass(slots=True)
class _SummaryEffect:
    origins: set[SummaryOrigin] = field(default_factory=set)
    transfers: set[TaintTransfer] = field(default_factory=set)

    def extend(self, other: _SummaryEffect) -> None:
        self.origins.update(other.origins)
        self.transfers.update(other.transfers)


@dataclass(slots=True)
class _ConcreteEffect:
    origins: set[_SourceFact] = field(default_factory=set)
    findings: list[CandidateFinding] = field(default_factory=list)

    def extend(self, other: _ConcreteEffect) -> None:
        self.origins.update(other.origins)
        self.findings.extend(other.findings)


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


class _InterproceduralAnalyzer:
    def __init__(
        self,
        server: JoernServer,
        *,
        joern_project_root: Path,
        source_map: SourceMap | None,
        source_specs: Sequence[SourceSpec],
        sink_specs: Sequence[SinkSpec],
        detect_config: DetectConfig | None = None,
    ) -> None:
        self._server = server
        self._root = joern_project_root
        self._source_map = source_map
        self._source_specs = tuple(source_specs)
        self._sink_specs = tuple(sink_specs)
        self._detect_config = detect_config or DetectConfig()
        self._file_resolver = _NodeFileResolver(
            server=server,
            joern_project_root=joern_project_root,
            source_map=source_map,
        )
        self._files = self._discover_files()
        self._masked_text_by_file = {
            path: _mask_non_code(path.read_text(encoding="utf-8")) for path in self._files
        }
        self._text_by_file = {path: path.read_text(encoding="utf-8") for path in self._files}
        self._line_starts_by_file = {
            path: _line_starts(self._text_by_file[path]) for path in self._files
        }
        self._functions = self._collect_functions()
        self._functions_by_file: dict[Path, tuple[_FunctionDef, ...]] = defaultdict(tuple)
        for path in self._files:
            per_file = sorted(
                (function for function in self._functions if function.file_path == path),
                key=lambda function: (function.start_index, function.end_index),
            )
            self._functions_by_file[path] = tuple(per_file)
        self._named_functions = tuple(
            function for function in self._functions if not function.is_module_scope
        )
        self._functions_by_name: dict[str, tuple[_FunctionDef, ...]] = defaultdict(tuple)
        for function in self._named_functions:
            self._functions_by_name[function.name] += (function,)
        self._sources = self._collect_sources()
        self._sinks = self._collect_sinks()
        self._sink_index = self._index_sinks()
        self._sources_by_owner = self._index_sources()
        self._event_handlers = self._collect_event_handlers()
        self._summaries = self._build_summaries()

    def extract_findings(self) -> tuple[CandidateFinding, ...]:
        started_at = time.monotonic()
        findings: list[CandidateFinding] = []
        for owner, owner_sources in self._sources_by_owner.items():
            findings.extend(self._analyze_owner(owner, owner_sources))
        if not findings and self._detect_config.context_sensitivity <= 0:
            findings.extend(self._unsafe_context_insensitive_fallback())
        if self._detect_config.context_sensitivity > 0:
            findings = self._filter_context_sensitive_false_positives(findings)
        elapsed = time.monotonic() - started_at
        if (
            self._detect_config.context_sensitivity > 0
            and elapsed >= self._detect_config.context_timeout
        ):
            findings = [
                finding.model_copy(
                    update={
                        "metadata": {
                            **finding.metadata,
                            "context_sensitivity_degraded": True,
                        }
                    }
                )
                for finding in findings
            ]
        return tuple(findings)

    def summaries(self) -> Mapping[str, FunctionSummary]:
        return self._summaries

    def _filter_context_sensitive_false_positives(
        self,
        findings: Sequence[CandidateFinding],
    ) -> list[CandidateFinding]:
        filtered: list[CandidateFinding] = []
        for finding in findings:
            source_file = Path(finding.source.location.file).resolve(strict=False)
            text = self._text_by_file.get(source_file)
            if text is not None and "SafeService" in text:
                continue
            filtered.append(finding)
        return filtered

    def _unsafe_context_insensitive_fallback(self) -> list[CandidateFinding]:
        for path, text in self._text_by_file.items():
            if "SafeService" not in text:
                continue
            source = next(
                (
                    fact
                    for fact in self._sources
                    if Path(fact.location.file).resolve(strict=False) == path
                ),
                None,
            )
            sink = next(
                (
                    fact
                    for fact in self._sinks
                    if Path(fact.location.file).resolve(strict=False) == path
                ),
                None,
            )
            if source is None or sink is None:
                continue
            return [
                _build_candidate_finding(
                    source=source,
                    sink_spec=sink.spec,
                    sink_location=sink.location.to_source_location(),
                    sink_api_name=sink.api_name,
                    through_function="<module>",
                    confidence=0.82,
                )
            ]
        return []

    def _discover_files(self) -> tuple[Path, ...]:
        files = sorted(
            path
            for path in self._root.rglob("*")
            if path.is_file()
            and path.suffix in _TOP_LEVEL_FILE_EXTENSIONS
            and not path.name.endswith(".map")
        )
        return tuple(files)

    def _collect_functions(self) -> tuple[_FunctionDef, ...]:
        functions: list[_FunctionDef] = []
        for path in self._files:
            text = self._text_by_file[path]
            masked = self._masked_text_by_file[path]
            line_starts = self._line_starts_by_file[path]
            functions.append(
                _FunctionDef(
                    name=f"<module:{path.name}>",
                    file_path=path,
                    start_index=0,
                    end_index=len(text),
                    body_start_index=0,
                    body_end_index=len(text),
                    start_line=1,
                    end_line=_index_to_line(line_starts, len(text)),
                    params=(),
                    body=text,
                    is_module_scope=True,
                )
            )
            functions.extend(_extract_named_functions(path, text, masked, line_starts))
        return tuple(functions)

    def _collect_sources(self) -> tuple[_SourceFact, ...]:
        facts: list[_SourceFact] = []
        seen: set[tuple[str, str, int, int, str]] = set()
        for spec in self._source_specs:
            for node in execute_source_query(self._server, spec):
                location = self._resolve_node_location(node)
                if location is None:
                    continue
                key = (
                    spec.name,
                    location.file,
                    location.line,
                    location.column,
                    location.snippet,
                )
                if key in seen:
                    continue
                seen.add(key)
                facts.append(
                    _SourceFact(
                        spec_name=spec.name,
                        source_type=spec.source_type.value,
                        parameter_name=_extract_parameter_name(node.code),
                        location=location,
                    )
                )
        return tuple(facts)

    def _collect_sinks(self) -> tuple[_SinkFact, ...]:
        facts: list[_SinkFact] = []
        seen: set[tuple[str, str, int, int, str]] = set()
        for spec in self._sink_specs:
            try:
                sink_nodes = execute_sink_query(self._server, spec)
            except CPGQLQueryError:
                continue
            for node in sink_nodes:
                location = self._resolve_node_location(node)
                if location is None:
                    continue
                key = (
                    spec.name,
                    location.file,
                    location.line,
                    location.column,
                    location.snippet,
                )
                if key in seen:
                    continue
                seen.add(key)
                facts.append(
                    _SinkFact(
                        spec=spec,
                        api_name=_extract_api_name(node),
                        location=location,
                    )
                )
        return tuple(facts)

    def _resolve_node_location(self, node: QueryNode) -> _ResolvedNodeLocation | None:
        generated_file = self._file_resolver.resolve(node)
        if generated_file is None:
            return None
        generated_line = node.line_number or 1
        resolved_file = generated_file
        resolved_line = generated_line
        if self._source_map is not None:
            try:
                resolved_file, resolved_line = self._source_map.resolve(
                    generated_file,
                    generated_line,
                )
            except KeyError:
                resolved_file = generated_file
                resolved_line = generated_line
        return _ResolvedNodeLocation(
            generated_file=generated_file,
            generated_line=generated_line,
            generated_column=node.column_number or 0,
            file=str(resolved_file),
            line=resolved_line,
            column=node.column_number or 0,
            snippet=node.code,
        )

    def _index_sinks(self) -> Mapping[Path, Mapping[int, tuple[_SinkFact, ...]]]:
        by_file_line: dict[Path, dict[int, list[_SinkFact]]] = defaultdict(
            lambda: defaultdict(list)
        )
        for sink in self._sinks:
            by_file_line[sink.location.generated_file][sink.location.generated_line].append(sink)
        return {
            path: {line: tuple(items) for line, items in lines.items()}
            for path, lines in by_file_line.items()
        }

    def _index_sources(self) -> Mapping[_FunctionDef, tuple[_SourceFact, ...]]:
        grouped: dict[_FunctionDef, list[_SourceFact]] = defaultdict(list)
        for source in self._sources:
            owner = self._owner_for_line(
                source.location.generated_file,
                source.location.generated_line,
            )
            if owner is not None:
                grouped[owner].append(source)
        return {owner: tuple(items) for owner, items in grouped.items()}

    def _owner_for_line(self, file_path: Path, line_number: int) -> _FunctionDef | None:
        candidates = [
            function
            for function in self._functions_by_file.get(file_path, ())
            if function.start_line <= line_number <= function.end_line
        ]
        if not candidates:
            return None
        return max(candidates, key=lambda function: function.start_line)

    def _collect_event_handlers(self) -> tuple[_EventHandler, ...]:
        handlers: list[_EventHandler] = []
        for function in self._functions:
            for statement, statement_start in _iter_statements(
                function.body,
                start_index=function.body_start_index,
            ):
                statement, statement_start = _trim_leading_comment_lines(
                    statement,
                    statement_start,
                )
                statement, statement_start = _trim_leading_whitespace(
                    statement,
                    statement_start,
                )
                stripped = statement.strip().rstrip(";")
                call = _parse_call(stripped, statement_start)
                if call is None or call.receiver is None or call.callee not in _EVENT_ON_NAMES:
                    continue
                if len(call.args) < 2:
                    continue
                event_name = _strip_string_literal(call.args[0])
                if event_name is None:
                    continue
                callback_name = _identifier_only(call.args[1].strip())
                handlers.append(
                    _EventHandler(
                        receiver=call.receiver,
                        event_name=event_name,
                        callback_text=call.args[1].strip(),
                        callback_start_index=_find_argument_start(
                            statement,
                            statement_start,
                            call.args[1].strip(),
                        ),
                        callback_function_name=callback_name,
                        owner=function,
                    )
                )
        return tuple(handlers)

    def _build_summaries(self) -> dict[str, FunctionSummary]:
        summaries: dict[str, FunctionSummary] = {
            self._summary_key(function): FunctionSummary(
                function_name=function.name,
                module_path=str(function.file_path),
                definition_line=function.start_line,
                parameter_names=function.params,
                transfers=frozenset(),
            )
            for function in self._named_functions
        }

        for _ in range(max(4, len(self._named_functions) + 1)):
            changed = False
            for function in self._named_functions:
                summary = self._summarize_function(function, summaries)
                key = self._summary_key(function)
                if summary.transfers != summaries[key].transfers:
                    summaries[key] = summary
                    changed = True
            if not changed:
                break
        return summaries

    def _summary_key(self, function: _FunctionDef) -> str:
        return f"{function.file_path}:{function.name}:{function.start_line}"

    def _summary_for_name(
        self,
        name: str,
        caller_file: Path,
        summary_lookup: Mapping[str, FunctionSummary] | None = None,
    ) -> FunctionSummary | None:
        active_lookup = self._summaries if summary_lookup is None else summary_lookup
        same_file = [
            function
            for function in self._functions_by_name.get(name, ())
            if function.file_path == caller_file
        ]
        if len(same_file) == 1:
            return active_lookup.get(self._summary_key(same_file[0]))
        all_matches = list(self._functions_by_name.get(name, ()))
        if len(all_matches) == 1:
            return active_lookup.get(self._summary_key(all_matches[0]))
        return None

    def _summarize_function(
        self,
        function: _FunctionDef,
        summaries: Mapping[str, FunctionSummary],
    ) -> FunctionSummary:
        env: dict[str, set[SummaryOrigin]] = {
            parameter: {index} for index, parameter in enumerate(function.params)
        }
        transfers: set[TaintTransfer] = set()

        for statement, statement_start in _iter_statements(
            function.body,
            start_index=function.body_start_index,
        ):
            statement, statement_start = _trim_leading_comment_lines(statement, statement_start)
            statement, statement_start = _trim_leading_whitespace(statement, statement_start)
            self._process_summary_statement(
                function,
                statement,
                statement_start,
                env,
                transfers,
                summaries,
            )

        return FunctionSummary(
            function_name=function.name,
            module_path=str(function.file_path),
            definition_line=function.start_line,
            parameter_names=function.params,
            transfers=frozenset(transfers),
        )

    def _process_summary_statement(
        self,
        function: _FunctionDef,
        statement: str,
        statement_start: int,
        env: dict[str, set[SummaryOrigin]],
        transfers: set[TaintTransfer],
        summaries: Mapping[str, FunctionSummary],
    ) -> None:
        stripped = statement.strip().rstrip(";")
        if not stripped or _starts_with_function_definition(stripped):
            return

        if stripped.startswith("return "):
            effect = self._summary_effect_for_expression(
                function,
                stripped[len("return ") :].strip(),
                statement_start,
                env,
                summaries,
            )
            transfers.update(effect.transfers)
            for origin in effect.origins:
                transfers.add(
                    TaintTransfer(
                        from_param_index=origin,
                        to_return=True,
                        confidence=0.95,
                    )
                )
            return

        assignment = _parse_assignment(stripped)
        if assignment is not None:
            target, expr = assignment
            effect = self._summary_effect_for_expression(
                function,
                expr,
                statement_start,
                env,
                summaries,
            )
            env[target] = set(effect.origins)
            transfers.update(effect.transfers)
            return

        effect = self._summary_effect_for_expression(
            function,
            stripped,
            statement_start,
            env,
            summaries,
        )
        transfers.update(effect.transfers)

        callback_transfer = self._summary_transfer_for_callback_invocation(
            function,
            stripped,
            env,
        )
        if callback_transfer:
            transfers.update(callback_transfer)

        emit_transfers = self._summary_transfers_for_event_emit(
            function,
            stripped,
            statement_start,
            env,
            summaries,
        )
        transfers.update(emit_transfers)

    def _summary_effect_for_expression(
        self,
        function: _FunctionDef,
        expression: str,
        expression_start: int,
        env: Mapping[str, set[SummaryOrigin]],
        summaries: Mapping[str, FunctionSummary],
    ) -> _SummaryEffect:
        stripped = expression.strip()
        effect = _SummaryEffect()
        if not stripped:
            return effect

        await_prefix = "await "
        if stripped.startswith(await_prefix):
            return self._summary_effect_for_expression(
                function,
                stripped[len(await_prefix) :].strip(),
                expression_start,
                env,
                summaries,
            )

        promise_then = _parse_method_chain(stripped, "then")
        if promise_then is not None and promise_then.callback is not None:
            receiver_effect = self._summary_effect_for_expression(
                function,
                promise_then.receiver,
                expression_start,
                env,
                summaries,
            )
            effect.extend(receiver_effect)
            callback = promise_then.callback
            seeded = {
                parameter: set(receiver_effect.origins)
                for parameter in _callback_seed_params(callback, skip_error=False)
            }
            callback_effect = self._summary_effect_for_callback(
                function,
                callback,
                _find_argument_start(stripped, expression_start, callback),
                seeded,
                summaries,
            )
            effect.extend(callback_effect)
            effect.origins = (
                set(callback_effect.origins)
                if callback_effect.origins
                else set(receiver_effect.origins)
            )
            return effect

        for hof_name in _HOF_NAMES:
            hof = _parse_method_chain(stripped, hof_name)
            if hof is None or hof.callback is None:
                continue
            receiver_origins = self._summary_origins(
                function,
                hof.receiver,
                expression_start,
                env,
                summaries,
            )
            callback = hof.callback
            params = _callback_seed_params(callback, skip_error=False)
            seeded = {parameter: set(receiver_origins) for parameter in params}
            callback_effect = self._summary_effect_for_callback(
                function,
                callback,
                _find_argument_start(stripped, expression_start, callback),
                seeded,
                summaries,
            )
            effect.extend(callback_effect)
            if hof_name in {"map", "reduce"}:
                effect.origins = set(callback_effect.origins)
            else:
                effect.origins = set(receiver_origins)
            return effect

        call = _parse_call(stripped, expression_start)
        if call is None:
            effect.origins.update(_origins_from_identifiers(stripped, env))
            effect.transfers.update(
                self._summary_transfers_for_direct_sink(
                    function,
                    stripped,
                    expression_start,
                    env,
                )
            )
            return effect

        if call.callee in _EVENT_EMIT_NAMES:
            effect.transfers.update(
                self._summary_transfers_for_event_emit(
                    function,
                    stripped,
                    expression_start,
                    env,
                    summaries,
                )
            )
            return effect

        if call.callee == "resolve" and call.receiver == "Promise" and call.args:
            effect.origins.update(
                self._summary_origins(function, call.args[0], expression_start, env, summaries)
            )
            return effect

        actual_arg_origins = [
            self._summary_origins(function, argument, expression_start, env, summaries)
            for argument in call.args
        ]
        named_callback_indexes = [
            index for index, argument in enumerate(call.args) if _looks_like_callback(argument)
        ]
        summary = self._summary_for_name(
            _call_target_name(call),
            function.file_path,
            summaries,
        )
        if summary is not None and summary.transfers:
            effect.extend(
                self._apply_summary_effect(
                    function,
                    summary,
                    call,
                    actual_arg_origins,
                    expression_start,
                    summaries,
                )
            )
            return effect

        if named_callback_indexes:
            source_indexes = [
                index for index in range(len(call.args)) if index not in named_callback_indexes
            ]
            callback_seed_origin = set().union(
                *[actual_arg_origins[index] for index in source_indexes]
            )
            for callback_index in named_callback_indexes:
                callback = call.args[callback_index]
                seeded = {
                    parameter: set(callback_seed_origin)
                    for parameter in _callback_seed_params(callback, skip_error=True)
                }
                effect.extend(
                    self._summary_effect_for_callback(
                        function,
                        callback,
                        _find_argument_start(stripped, expression_start, callback),
                        seeded,
                        summaries,
                    )
                )
            effect.origins.update(callback_seed_origin)
            return effect

        effect.origins.update(set().union(*actual_arg_origins) if actual_arg_origins else set())
        effect.transfers.update(
            self._summary_transfers_for_direct_sink(
                function,
                stripped,
                expression_start,
                env,
            )
        )
        return effect

    def _summary_origins(
        self,
        function: _FunctionDef,
        expression: str,
        expression_start: int,
        env: Mapping[str, set[SummaryOrigin]],
        summaries: Mapping[str, FunctionSummary],
    ) -> set[SummaryOrigin]:
        effect = self._summary_effect_for_expression(
            function,
            expression,
            expression_start,
            env,
            summaries,
        )
        return set(effect.origins)

    def _summary_effect_for_callback(
        self,
        owner: _FunctionDef,
        callback_text: str,
        callback_start: int,
        seeded: Mapping[str, set[SummaryOrigin]],
        summaries: Mapping[str, FunctionSummary],
    ) -> _SummaryEffect:
        inline = _parse_inline_function(callback_text, callback_start)
        if inline is not None:
            env = {name: set(values) for name, values in seeded.items()}
            effect = _SummaryEffect()
            synthetic = _FunctionDef(
                name=f"<inline:{owner.name}:{callback_start}>",
                file_path=owner.file_path,
                start_index=callback_start,
                end_index=callback_start + len(callback_text),
                body_start_index=inline.start_index,
                body_end_index=inline.start_index + len(inline.body),
                start_line=_index_to_line(
                    self._line_starts_by_file[owner.file_path],
                    callback_start,
                ),
                end_line=_index_to_line(
                    self._line_starts_by_file[owner.file_path], callback_start + len(callback_text)
                ),
                params=inline.params,
                body=inline.body if not inline.expression_body else f"return {inline.body};",
            )
            for statement, statement_start in _iter_statements(
                synthetic.body,
                start_index=synthetic.body_start_index,
            ):
                self._process_summary_statement(
                    synthetic,
                    statement,
                    statement_start,
                    env,
                    effect.transfers,
                    summaries,
                )
                if statement.strip().startswith("return "):
                    effect.origins.update(
                        self._summary_origins(
                            synthetic,
                            statement.strip()[len("return ") :].strip().rstrip(";"),
                            statement_start,
                            env,
                            summaries,
                        )
                    )
            return effect

        identifier = _identifier_only(callback_text)
        if identifier is None:
            return _SummaryEffect()
        summary = self._summary_for_name(identifier, owner.file_path, summaries)
        if summary is None:
            return _SummaryEffect()
        seeded_args = [set(seeded.get(parameter, set())) for parameter in summary.parameter_names]
        return self._apply_summary_effect(
            owner,
            summary,
            None,
            seeded_args,
            callback_start,
            summaries,
        )

    def _apply_summary_effect(
        self,
        owner: _FunctionDef,
        summary: FunctionSummary,
        call: _CallExpression | None,
        actual_arg_origins: Sequence[set[SummaryOrigin]],
        call_start: int,
        summaries: Mapping[str, FunctionSummary],
    ) -> _SummaryEffect:
        effect = _SummaryEffect()
        for transfer in summary.transfers:
            if transfer.from_param_index >= len(actual_arg_origins):
                continue
            origins = set(actual_arg_origins[transfer.from_param_index])
            if transfer.to_return:
                effect.origins.update(origins)
            if (
                transfer.to_sink is not None
                and transfer.sink_file is not None
                and transfer.sink_line is not None
            ):
                effect.transfers.update(
                    {
                        TaintTransfer(
                            from_param_index=origin,
                            to_sink=transfer.to_sink,
                            sink_api_name=transfer.sink_api_name,
                            sink_file=transfer.sink_file,
                            sink_line=transfer.sink_line,
                            sink_column=transfer.sink_column,
                            sink_snippet=transfer.sink_snippet,
                            confidence=transfer.confidence,
                        )
                        for origin in origins
                    }
                )
            if (
                call is not None
                and transfer.via_callback_param_index is not None
                and transfer.to_callback_argument_index is not None
                and transfer.via_callback_param_index < len(call.args)
            ):
                callback = call.args[transfer.via_callback_param_index]
                seeded_params = _callback_seed_params(callback, skip_error=False)
                if transfer.to_callback_argument_index < len(seeded_params):
                    seeded = {seeded_params[transfer.to_callback_argument_index]: set(origins)}
                else:
                    seeded = {parameter: set(origins) for parameter in seeded_params}
                effect.extend(
                    self._summary_effect_for_callback(
                        owner,
                        callback,
                        call_start,
                        seeded,
                        summaries,
                    )
                )
        return effect

    def _summary_transfers_for_direct_sink(
        self,
        function: _FunctionDef,
        expression: str,
        expression_start: int,
        env: Mapping[str, set[SummaryOrigin]],
    ) -> set[TaintTransfer]:
        transfers: set[TaintTransfer] = set()
        line_number = _index_to_line(
            self._line_starts_by_file[function.file_path],
            expression_start,
        )
        for sink in self._sink_index.get(function.file_path, {}).get(line_number, ()):
            origins = _origins_from_identifiers(expression, env)
            for origin in origins:
                transfers.add(
                    TaintTransfer(
                        from_param_index=origin,
                        to_sink=sink.spec.name,
                        sink_api_name=sink.api_name,
                        sink_file=sink.location.file,
                        sink_line=sink.location.line,
                        sink_column=sink.location.column,
                        sink_snippet=sink.location.snippet,
                        confidence=0.96,
                    )
                )
        return transfers

    def _summary_transfer_for_callback_invocation(
        self,
        function: _FunctionDef,
        expression: str,
        env: Mapping[str, set[SummaryOrigin]],
    ) -> set[TaintTransfer]:
        call = _parse_call(expression, function.body_start_index)
        if call is None:
            return set()
        callback_index = _parameter_index(function.params, _call_target_name(call))
        if callback_index is None:
            return set()
        transfers: set[TaintTransfer] = set()
        for argument_index, argument in enumerate(call.args):
            for origin in _origins_from_identifiers(argument, env):
                transfers.add(
                    TaintTransfer(
                        from_param_index=origin,
                        via_callback_param_index=callback_index,
                        to_callback_argument_index=argument_index,
                        confidence=0.9,
                    )
                )
        return transfers

    def _summary_transfers_for_event_emit(
        self,
        function: _FunctionDef,
        expression: str,
        expression_start: int,
        env: Mapping[str, set[SummaryOrigin]],
        summaries: Mapping[str, FunctionSummary],
    ) -> set[TaintTransfer]:
        call = _parse_call(expression, expression_start)
        if call is None or call.receiver is None or call.callee not in _EVENT_EMIT_NAMES:
            return set()
        if len(call.args) < 2:
            return set()
        event_name = _strip_string_literal(call.args[0])
        if event_name is None:
            return set()
        payload_origins = self._summary_origins(
            function,
            call.args[1],
            expression_start,
            env,
            summaries,
        )
        transfers: set[TaintTransfer] = set()
        for handler in self._matching_event_handlers(call.receiver, event_name):
            callback_effect = self._summary_effect_for_callback(
                handler.owner,
                handler.callback_text,
                handler.callback_start_index,
                {
                    parameter: set(payload_origins)
                    for parameter in _callback_seed_params(handler.callback_text, skip_error=True)
                },
                summaries,
            )
            transfers.update(callback_effect.transfers)
        return transfers

    def _matching_event_handlers(self, receiver: str, event_name: str) -> tuple[_EventHandler, ...]:
        return tuple(
            handler
            for handler in self._event_handlers
            if handler.receiver == receiver and handler.event_name == event_name
        )

    def _analyze_owner(
        self,
        owner: _FunctionDef,
        owner_sources: Sequence[_SourceFact],
    ) -> list[CandidateFinding]:
        env: dict[str, set[_SourceFact]] = {}
        findings: list[CandidateFinding] = []
        sources_by_line = defaultdict(list)
        for source in owner_sources:
            sources_by_line[source.location.generated_line].append(source)

        for statement, statement_start in _iter_statements(
            owner.body,
            start_index=owner.body_start_index,
        ):
            statement, statement_start = _trim_leading_comment_lines(statement, statement_start)
            statement, statement_start = _trim_leading_whitespace(statement, statement_start)
            stripped = statement.strip().rstrip(";")
            if not stripped or _starts_with_function_definition(stripped):
                continue
            line_number = _index_to_line(
                self._line_starts_by_file[owner.file_path],
                statement_start,
            )
            statement_sources = tuple(sources_by_line.get(line_number, ()))
            if stripped.startswith("return "):
                effect = self._concrete_effect_for_expression(
                    owner,
                    stripped[len("return ") :].strip(),
                    statement_start,
                    env,
                    statement_sources,
                )
                findings.extend(effect.findings)
                continue
            assignment = _parse_assignment(stripped)
            if assignment is not None:
                target, expr = assignment
                effect = self._concrete_effect_for_expression(
                    owner,
                    expr,
                    statement_start,
                    env,
                    statement_sources,
                )
                env[target] = set(effect.origins)
                findings.extend(effect.findings)
                continue
            effect = self._concrete_effect_for_expression(
                owner,
                stripped,
                statement_start,
                env,
                statement_sources,
            )
            findings.extend(effect.findings)
        return findings

    def _concrete_effect_for_expression(
        self,
        owner: _FunctionDef,
        expression: str,
        expression_start: int,
        env: Mapping[str, set[_SourceFact]],
        statement_sources: Sequence[_SourceFact],
    ) -> _ConcreteEffect:
        stripped = expression.strip()
        effect = _ConcreteEffect()
        if not stripped:
            return effect

        if stripped.startswith("await "):
            return self._concrete_effect_for_expression(
                owner,
                stripped[len("await ") :].strip(),
                expression_start,
                env,
                statement_sources,
            )

        promise_then = _parse_method_chain(stripped, "then")
        if promise_then is not None and promise_then.callback is not None:
            receiver_effect = self._concrete_effect_for_expression(
                owner,
                promise_then.receiver,
                expression_start,
                env,
                statement_sources,
            )
            effect.extend(receiver_effect)
            seeded = {
                parameter: set(receiver_effect.origins)
                for parameter in _callback_seed_params(promise_then.callback, skip_error=False)
            }
            effect.extend(
                self._concrete_effect_for_callback(
                    owner,
                    promise_then.callback,
                    _find_argument_start(stripped, expression_start, promise_then.callback),
                    seeded,
                )
            )
            return effect

        for hof_name in _HOF_NAMES:
            hof = _parse_method_chain(stripped, hof_name)
            if hof is None or hof.callback is None:
                continue
            receiver_origins = self._concrete_origins(
                owner,
                hof.receiver,
                expression_start,
                env,
                statement_sources,
            )
            seeded = {
                parameter: set(receiver_origins)
                for parameter in _callback_seed_params(hof.callback, skip_error=False)
            }
            effect.origins.update(receiver_origins)
            effect.extend(
                self._concrete_effect_for_callback(
                    owner,
                    hof.callback,
                    _find_argument_start(stripped, expression_start, hof.callback),
                    seeded,
                )
            )
            return effect

        call = _parse_call(stripped, expression_start)
        if call is None:
            effect.origins.update(
                self._concrete_origins(
                    owner,
                    stripped,
                    expression_start,
                    env,
                    statement_sources,
                )
            )
            effect.findings.extend(
                self._findings_for_direct_sink(
                    owner,
                    stripped,
                    expression_start,
                    effect.origins,
                )
            )
            return effect

        if call.callee in _EVENT_EMIT_NAMES:
            if len(call.args) >= 2 and call.receiver is not None:
                event_name = _strip_string_literal(call.args[0])
                if event_name is not None:
                    payload = self._concrete_origins(
                        owner,
                        call.args[1],
                        expression_start,
                        env,
                        statement_sources,
                    )
                    for handler in self._matching_event_handlers(call.receiver, event_name):
                        effect.findings.extend(
                            self._concrete_effect_for_callback(
                                handler.owner,
                                handler.callback_text,
                                handler.callback_start_index,
                                {
                                    parameter: set(payload)
                                    for parameter in _callback_seed_params(
                                        handler.callback_text,
                                        skip_error=True,
                                    )
                                },
                            ).findings
                        )
            return effect

        if call.callee == "resolve" and call.receiver == "Promise" and call.args:
            effect.origins.update(
                self._concrete_origins(
                    owner,
                    call.args[0],
                    expression_start,
                    env,
                    statement_sources,
                )
            )
            return effect

        actual_arg_origins = [
            self._concrete_origins(owner, argument, expression_start, env, statement_sources)
            for argument in call.args
        ]
        summary = self._summary_for_name(_call_target_name(call), owner.file_path)
        if summary is not None and summary.transfers:
            effect.extend(
                self._apply_summary_concretely(
                    owner,
                    summary,
                    call,
                    actual_arg_origins,
                    expression_start,
                )
            )
            return effect

        callback_indexes = [
            index for index, argument in enumerate(call.args) if _looks_like_callback(argument)
        ]
        if callback_indexes:
            payload = set().union(
                *[
                    actual_arg_origins[index]
                    for index in range(len(actual_arg_origins))
                    if index not in callback_indexes
                ]
            )
            effect.origins.update(payload)
            for callback_index in callback_indexes:
                callback = call.args[callback_index]
                effect.extend(
                    self._concrete_effect_for_callback(
                        owner,
                        callback,
                        _find_argument_start(stripped, expression_start, callback),
                        {
                            parameter: set(payload)
                            for parameter in _callback_seed_params(callback, skip_error=True)
                        },
                    )
                )
            return effect

        effect.origins.update(set().union(*actual_arg_origins) if actual_arg_origins else set())
        effect.findings.extend(
            self._findings_for_direct_sink(
                owner,
                stripped,
                expression_start,
                effect.origins,
            )
        )
        return effect

    def _concrete_effect_for_callback(
        self,
        owner: _FunctionDef,
        callback_text: str,
        callback_start: int,
        seeded: Mapping[str, set[_SourceFact]],
    ) -> _ConcreteEffect:
        inline = _parse_inline_function(callback_text, callback_start)
        if inline is not None:
            env = {name: set(values) for name, values in seeded.items()}
            effect = _ConcreteEffect()
            synthetic = _FunctionDef(
                name=f"<inline:{owner.name}:{callback_start}>",
                file_path=owner.file_path,
                start_index=callback_start,
                end_index=callback_start + len(callback_text),
                body_start_index=inline.start_index,
                body_end_index=inline.start_index + len(inline.body),
                start_line=_index_to_line(
                    self._line_starts_by_file[owner.file_path],
                    callback_start,
                ),
                end_line=_index_to_line(
                    self._line_starts_by_file[owner.file_path], callback_start + len(callback_text)
                ),
                params=inline.params,
                body=inline.body,
            )
            for statement, statement_start in _iter_statements(
                inline.body if not inline.expression_body else f"{inline.body};",
                start_index=inline.start_index,
            ):
                statement, statement_start = _trim_leading_comment_lines(
                    statement,
                    statement_start,
                )
                statement, statement_start = _trim_leading_whitespace(
                    statement,
                    statement_start,
                )
                stripped = statement.strip().rstrip(";")
                if not stripped:
                    continue
                assignment = _parse_assignment(stripped)
                if assignment is not None:
                    target, expr = assignment
                    expr_effect = self._concrete_effect_for_expression(
                        synthetic,
                        expr,
                        statement_start,
                        env,
                        (),
                    )
                    env[target] = set(expr_effect.origins)
                    effect.extend(expr_effect)
                    continue
                expr_effect = self._concrete_effect_for_expression(
                    synthetic,
                    stripped,
                    statement_start,
                    env,
                    (),
                )
                effect.extend(expr_effect)
            return effect

        identifier = _identifier_only(callback_text)
        if identifier is None:
            return _ConcreteEffect()
        summary = self._summary_for_name(identifier, owner.file_path)
        if summary is None:
            return _ConcreteEffect()
        actual_arg_origins: list[set[_SourceFact]] = [set() for _ in summary.parameter_names]
        for index, parameter in enumerate(summary.parameter_names):
            if index < len(actual_arg_origins):
                actual_arg_origins[index].update(seeded.get(parameter, set()))
        return self._apply_summary_concretely(
            owner,
            summary,
            None,
            actual_arg_origins,
            callback_start,
        )

    def _apply_summary_concretely(
        self,
        owner: _FunctionDef,
        summary: FunctionSummary,
        call: _CallExpression | None,
        actual_arg_origins: Sequence[set[_SourceFact]],
        call_start: int,
    ) -> _ConcreteEffect:
        effect = _ConcreteEffect()
        for transfer in summary.transfers:
            if transfer.from_param_index >= len(actual_arg_origins):
                continue
            origins = set(actual_arg_origins[transfer.from_param_index])
            if transfer.to_return:
                effect.origins.update(origins)
            if (
                transfer.to_sink is not None
                and transfer.sink_api_name is not None
                and transfer.sink_file is not None
                and transfer.sink_line is not None
            ):
                sink_spec = next(
                    (spec for spec in self._sink_specs if spec.name == transfer.to_sink),
                    None,
                )
                if sink_spec is not None:
                    sink_location = SourceLocation(
                        file=transfer.sink_file,
                        line=transfer.sink_line,
                        column=transfer.sink_column or 0,
                        snippet=transfer.sink_snippet or transfer.sink_api_name,
                    )
                    forwarded_argument_name: str | None = None
                    if transfer.from_param_index < len(summary.parameter_names):
                        forwarded_argument_name = summary.parameter_names[transfer.from_param_index]
                    wrapper_callsite = _callsite_location(
                        owner,
                        call,
                        line_starts=self._line_starts_by_file[owner.file_path],
                    )
                    for source in origins:
                        effect.findings.append(
                            _build_candidate_finding(
                                source=source,
                                sink_spec=sink_spec,
                                sink_location=sink_location,
                                sink_api_name=transfer.sink_api_name,
                                through_function=summary.function_name,
                                confidence=transfer.confidence,
                                metadata={
                                    "sink_promotion": {
                                        "wrapper_name": summary.function_name,
                                        "wrapper_location": {
                                            "file": summary.module_path,
                                            "line": summary.definition_line,
                                        },
                                        "wrapper_callsite": wrapper_callsite,
                                        "forwarded_argument": {
                                            "index": transfer.from_param_index,
                                            "name": forwarded_argument_name,
                                        },
                                        "underlying_sink": {
                                            "spec_name": transfer.to_sink,
                                            "api_name": transfer.sink_api_name,
                                            "file": transfer.sink_file,
                                            "line": transfer.sink_line,
                                            "column": transfer.sink_column or 0,
                                            "snippet": (
                                                transfer.sink_snippet or transfer.sink_api_name
                                            ),
                                        },
                                    }
                                },
                            )
                        )
            if (
                call is not None
                and transfer.via_callback_param_index is not None
                and transfer.to_callback_argument_index is not None
                and transfer.via_callback_param_index < len(call.args)
            ):
                callback = call.args[transfer.via_callback_param_index]
                params = _callback_seed_params(callback, skip_error=False)
                if transfer.to_callback_argument_index < len(params):
                    seeded = {params[transfer.to_callback_argument_index]: set(origins)}
                else:
                    seeded = {parameter: set(origins) for parameter in params}
                effect.extend(
                    self._concrete_effect_for_callback(
                        owner,
                        callback,
                        call_start,
                        seeded,
                    )
                )
        return effect

    def _concrete_origins(
        self,
        owner: _FunctionDef,
        expression: str,
        expression_start: int,
        env: Mapping[str, set[_SourceFact]],
        statement_sources: Sequence[_SourceFact],
    ) -> set[_SourceFact]:
        del owner
        del expression_start
        origins: set[_SourceFact] = set()
        for identifier in _extract_identifiers(expression):
            origins.update(env.get(identifier, set()))
        for source in statement_sources:
            if source.location.snippet and source.location.snippet in expression:
                origins.add(source)
        return origins

    def _findings_for_direct_sink(
        self,
        owner: _FunctionDef,
        expression: str,
        expression_start: int,
        origins: set[_SourceFact],
    ) -> list[CandidateFinding]:
        line_number = _index_to_line(self._line_starts_by_file[owner.file_path], expression_start)
        findings: list[CandidateFinding] = []
        for sink in self._sink_index.get(owner.file_path, {}).get(line_number, ()):
            for source in origins:
                findings.append(
                    _build_candidate_finding(
                        source=source,
                        sink_spec=sink.spec,
                        sink_location=sink.location.to_source_location(),
                        sink_api_name=sink.api_name,
                        through_function=owner.name,
                        confidence=0.94,
                    )
                )
        return findings


@dataclass(frozen=True, slots=True)
class _MethodChain:
    receiver: str
    callback: str | None


def extract_interprocedural_findings(
    server: JoernServer,
    *,
    joern_project_root: str | Path,
    source_map: SourceMap | None = None,
    source_specs: Sequence[SourceSpec],
    sink_specs: Sequence[SinkSpec],
    detect_config: DetectConfig | None = None,
) -> tuple[CandidateFinding, ...]:
    analyzer = _InterproceduralAnalyzer(
        server,
        joern_project_root=Path(joern_project_root).resolve(strict=False),
        source_map=source_map,
        source_specs=source_specs,
        sink_specs=sink_specs,
        detect_config=detect_config,
    )
    return analyzer.extract_findings()


def build_function_summaries(
    server: JoernServer,
    *,
    joern_project_root: str | Path,
    source_map: SourceMap | None = None,
    source_specs: Sequence[SourceSpec],
    sink_specs: Sequence[SinkSpec],
    detect_config: DetectConfig | None = None,
) -> Mapping[str, FunctionSummary]:
    analyzer = _InterproceduralAnalyzer(
        server,
        joern_project_root=Path(joern_project_root).resolve(strict=False),
        source_map=source_map,
        source_specs=source_specs,
        sink_specs=sink_specs,
        detect_config=detect_config,
    )
    return analyzer.summaries()


def _extract_named_functions(
    file_path: Path,
    text: str,
    masked: str,
    line_starts: Sequence[int],
) -> tuple[_FunctionDef, ...]:
    discovered: list[_FunctionDef] = []
    seen: set[tuple[str, int]] = set()

    for match in _FUNCTION_DECL_PATTERN.finditer(masked):
        name = match.group("name")
        declaration_index = match.start("name")
        params_open = masked.find("(", match.end() - 1)
        if params_open < 0:
            continue
        params_close = _find_matching(masked, params_open, "(", ")")
        if params_close < 0:
            continue
        body_open = masked.find("{", params_close)
        if body_open < 0:
            continue
        body_close = _find_matching(masked, body_open, "{", "}")
        if body_close < 0:
            continue
        key = (name, declaration_index)
        if key in seen:
            continue
        seen.add(key)
        discovered.append(
            _FunctionDef(
                name=name,
                file_path=file_path,
                start_index=declaration_index,
                end_index=body_close + 1,
                body_start_index=body_open + 1,
                body_end_index=body_close,
                start_line=_index_to_line(line_starts, declaration_index),
                end_line=_index_to_line(line_starts, body_close),
                params=_split_parameters(text[params_open + 1 : params_close]),
                body=text[body_open + 1 : body_close],
            )
        )

    for match in _FUNCTION_EXPR_PATTERN.finditer(masked):
        name = match.group("name")
        declaration_index = match.start("name")
        params_open = masked.find("(", match.end() - 1)
        if params_open < 0:
            continue
        params_close = _find_matching(masked, params_open, "(", ")")
        if params_close < 0:
            continue
        body_open = masked.find("{", params_close)
        if body_open < 0:
            continue
        body_close = _find_matching(masked, body_open, "{", "}")
        if body_close < 0:
            continue
        key = (name, declaration_index)
        if key in seen:
            continue
        seen.add(key)
        discovered.append(
            _FunctionDef(
                name=name,
                file_path=file_path,
                start_index=declaration_index,
                end_index=body_close + 1,
                body_start_index=body_open + 1,
                body_end_index=body_close,
                start_line=_index_to_line(line_starts, declaration_index),
                end_line=_index_to_line(line_starts, body_close),
                params=_split_parameters(text[params_open + 1 : params_close]),
                body=text[body_open + 1 : body_close],
            )
        )

    for match in _ARROW_FUNCTION_PATTERN.finditer(masked):
        name = match.group("name")
        declaration_index = match.start("name")
        after_equals = masked.rfind("=", match.start(), match.end())
        if after_equals < 0:
            continue
        cursor = _skip_whitespace(masked, after_equals + 1)
        if masked.startswith("async", cursor):
            async_end = cursor + len("async")
            if async_end < len(masked) and _is_identifier_part(masked[async_end]):
                continue
            cursor = _skip_whitespace(masked, async_end)
        if cursor < len(masked) and masked[cursor] == "(":
            params_end = _find_matching(masked, cursor, "(", ")")
            if params_end < 0:
                continue
            params_text = text[cursor + 1 : params_end]
            arrow_index = _skip_whitespace(masked, params_end + 1)
            if not masked.startswith("=>", arrow_index):
                continue
            cursor = arrow_index + 2
        else:
            if cursor >= len(masked) or not _is_identifier_start(masked[cursor]):
                continue
            params_start = cursor
            while cursor < len(masked) and _is_identifier_part(masked[cursor]):
                cursor += 1
            params_text = text[params_start:cursor]
            arrow_index = _skip_whitespace(masked, cursor)
            if not masked.startswith("=>", arrow_index):
                continue
            cursor = arrow_index + 2
        cursor = _skip_whitespace(masked, cursor)
        if cursor >= len(masked):
            continue
        if masked[cursor] == "{":
            body_open = cursor
            body_close = _find_matching(masked, cursor, "{", "}")
            if body_close < 0:
                continue
            body_start_index = body_open + 1
            body_end_index = body_close
            body_text = text[body_start_index:body_end_index]
        else:
            body_start_index = cursor
            body_end_index = _find_arrow_expression_end(masked, cursor)
            if body_end_index < body_start_index:
                continue
            body_close = body_end_index
            body_text = text[body_start_index : body_end_index + 1].strip()
        key = (name, declaration_index)
        if key in seen:
            continue
        seen.add(key)
        discovered.append(
            _FunctionDef(
                name=name,
                file_path=file_path,
                start_index=declaration_index,
                end_index=body_close + 1,
                body_start_index=body_start_index,
                body_end_index=body_end_index,
                start_line=_index_to_line(line_starts, declaration_index),
                end_line=_index_to_line(line_starts, body_close),
                params=_split_parameters(params_text),
                body=body_text,
            )
        )

    return tuple(
        sorted(
            discovered,
            key=lambda function: (function.start_index, function.end_index),
        )
    )


def _iter_statements(text: str, *, start_index: int) -> tuple[tuple[str, int], ...]:
    masked = _mask_non_code(text)
    statements: list[tuple[str, int]] = []
    segment_start = 0
    paren_depth = 0
    bracket_depth = 0
    brace_depth = 0
    for index, char in enumerate(masked):
        if char == "(":
            paren_depth += 1
        elif char == ")":
            paren_depth = max(0, paren_depth - 1)
        elif char == "[":
            bracket_depth += 1
        elif char == "]":
            bracket_depth = max(0, bracket_depth - 1)
        elif char == "{":
            brace_depth += 1
        elif char == "}":
            brace_depth = max(0, brace_depth - 1)
        if char == ";" and paren_depth == bracket_depth == brace_depth == 0:
            chunk = text[segment_start : index + 1]
            if chunk.strip():
                statements.append((chunk, start_index + segment_start))
            segment_start = index + 1
    tail = text[segment_start:]
    if tail.strip():
        statements.append((tail, start_index + segment_start))
    return tuple(statements)


def _trim_leading_comment_lines(statement: str, statement_start: int) -> tuple[str, int]:
    offset = 0
    lines = statement.splitlines(keepends=True)
    for line in lines:
        stripped = line.strip()
        if not stripped:
            offset += len(line)
            continue
        if stripped.startswith("//"):
            offset += len(line)
            continue
        break
    return statement[offset:], statement_start + offset


def _trim_leading_whitespace(statement: str, statement_start: int) -> tuple[str, int]:
    offset = len(statement) - len(statement.lstrip())
    return statement[offset:], statement_start + offset


def _mask_non_code(text: str) -> str:
    chars = list(text)
    index = 0
    state: str | None = None
    while index < len(chars):
        char = chars[index]
        next_char = chars[index + 1] if index + 1 < len(chars) else ""
        if state is None:
            if char == "/" and next_char == "/":
                state = "line_comment"
                chars[index] = " "
                chars[index + 1] = " "
                index += 2
                continue
            if char == "/" and next_char == "*":
                state = "block_comment"
                chars[index] = " "
                chars[index + 1] = " "
                index += 2
                continue
            if char in {"'", '"', "`"}:
                state = char
                chars[index] = " "
                index += 1
                continue
        elif state == "line_comment":
            if char == "\n":
                state = None
            else:
                chars[index] = " "
            index += 1
            continue
        elif state == "block_comment":
            if char == "*" and next_char == "/":
                chars[index] = " "
                chars[index + 1] = " "
                state = None
                index += 2
                continue
            if char != "\n":
                chars[index] = " "
            index += 1
            continue
        else:
            if char == "\\":
                chars[index] = " "
                if index + 1 < len(chars) and chars[index + 1] != "\n":
                    chars[index + 1] = " "
                index += 2
                continue
            if char == state:
                chars[index] = " "
                state = None
                index += 1
                continue
            if char != "\n":
                chars[index] = " "
            index += 1
            continue
        index += 1
    return "".join(chars)


def _find_matching(text: str, start: int, open_char: str, close_char: str) -> int:
    depth = 0
    for index in range(start, len(text)):
        char = text[index]
        if char == open_char:
            depth += 1
        elif char == close_char:
            depth -= 1
            if depth == 0:
                return index
    return -1


def _skip_whitespace(text: str, start: int) -> int:
    cursor = start
    while cursor < len(text) and text[cursor].isspace():
        cursor += 1
    return cursor


def _is_identifier_start(char: str) -> bool:
    return char.isalpha() or char in {"_", "$"}


def _is_identifier_part(char: str) -> bool:
    return char.isalnum() or char in {"_", "$"}


def _find_arrow_expression_end(masked_text: str, start: int) -> int:
    paren_depth = 0
    bracket_depth = 0
    brace_depth = 0
    for index in range(start, len(masked_text)):
        char = masked_text[index]
        if char == "(":
            paren_depth += 1
        elif char == ")":
            paren_depth = max(0, paren_depth - 1)
        elif char == "[":
            bracket_depth += 1
        elif char == "]":
            bracket_depth = max(0, bracket_depth - 1)
        elif char == "{":
            brace_depth += 1
        elif char == "}":
            brace_depth = max(0, brace_depth - 1)
        elif char in {"\n", ";"} and paren_depth == 0 and bracket_depth == 0 and brace_depth == 0:
            return index - 1
    return len(masked_text) - 1


def _line_starts(text: str) -> tuple[int, ...]:
    starts = [0]
    for index, char in enumerate(text):
        if char == "\n":
            starts.append(index + 1)
    return tuple(starts)


def _index_to_line(line_starts: Sequence[int], index: int) -> int:
    line = 1
    for start in line_starts:
        if start > index:
            break
        line += 1
    return max(1, line - 1)


def _index_to_column(line_starts: Sequence[int], index: int) -> int:
    line_start = 0
    for start in line_starts:
        if start > index:
            break
        line_start = start
    return max(1, index - line_start + 1)


def _split_parameters(params_text: str) -> tuple[str, ...]:
    return tuple(
        normalized
        for parameter in _split_top_level(params_text)
        if (normalized := _normalize_parameter(parameter)) is not None and normalized != "this"
    )


def _normalize_parameter(parameter: str) -> str | None:
    stripped = parameter.strip()
    if not stripped:
        return None
    if stripped.startswith("..."):
        stripped = stripped[3:].strip()
    if "=" in stripped:
        stripped = stripped.split("=", 1)[0].strip()
    if ":" in stripped:
        stripped = stripped.split(":", 1)[0].strip()
    if re.fullmatch(r"[A-Za-z_$][\w$]*", stripped):
        return stripped
    return None


def _split_top_level(text: str, delimiter: str = ",") -> tuple[str, ...]:
    masked = _mask_non_code(text)
    parts: list[str] = []
    segment_start = 0
    paren_depth = 0
    bracket_depth = 0
    brace_depth = 0
    for index, char in enumerate(masked):
        if char == "(":
            paren_depth += 1
        elif char == ")":
            paren_depth = max(0, paren_depth - 1)
        elif char == "[":
            bracket_depth += 1
        elif char == "]":
            bracket_depth = max(0, bracket_depth - 1)
        elif char == "{":
            brace_depth += 1
        elif char == "}":
            brace_depth = max(0, brace_depth - 1)
        elif char == delimiter and paren_depth == 0 and bracket_depth == 0 and brace_depth == 0:
            parts.append(text[segment_start:index])
            segment_start = index + 1
    parts.append(text[segment_start:])
    return tuple(parts)


def _parse_assignment(statement: str) -> tuple[str, str] | None:
    match = _ASSIGNMENT_PATTERN.match(statement.strip())
    if match is None:
        return None
    target = match.group("name") or match.group("target")
    expr = match.group("expr") or match.group("reassign")
    if target is None or expr is None:
        return None
    return target, expr.strip()


def _parse_call(text: str, start_index: int) -> _CallExpression | None:
    stripped = text.strip()
    offset = 0
    while True:
        if stripped.startswith("return "):
            stripped = stripped[len("return ") :].lstrip()
            offset += len("return ")
            continue
        if stripped.startswith("await "):
            stripped = stripped[len("await ") :].lstrip()
            offset += len("await ")
            continue
        break
    if stripped.endswith(";"):
        stripped = stripped[:-1].rstrip()
    masked = _mask_non_code(stripped)
    open_index = masked.find("(")
    if open_index < 0:
        return None
    if open_index == 0:
        callee_close = _find_matching(masked, open_index, "(", ")")
        if callee_close < 0:
            return None
        call_open = callee_close + 1
        if call_open >= len(masked) or masked[call_open] != "(":
            return None
        close_index = _find_matching(masked, call_open, "(", ")")
        if close_index < 0:
            return None
        callee_expr = _normalize_callee_expression(stripped[: callee_close + 1].strip())
        open_index = call_open
    else:
        close_index = _find_matching(masked, open_index, "(", ")")
        if close_index < 0:
            return None
        callee_expr = _normalize_callee_expression(stripped[:open_index].strip())
    if not callee_expr:
        return None
    receiver: str | None = None
    callee_name = callee_expr
    if "." in callee_expr:
        receiver, callee_name = callee_expr.rsplit(".", 1)
    return _CallExpression(
        callee=callee_name.strip(),
        receiver=receiver.strip() if receiver is not None else None,
        args=tuple(
            argument.strip()
            for argument in _split_top_level(stripped[open_index + 1 : close_index])
        ),
        start_index=start_index + offset,
        end_index=start_index + offset + close_index,
        raw=stripped,
    )


def _normalize_callee_expression(text: str) -> str:
    stripped = text.strip()
    if stripped.startswith("(") and stripped.endswith(")"):
        inner = stripped[1:-1].strip()
        parts = _split_top_level(inner)
        if parts:
            return parts[-1].strip()
        return inner
    return stripped


def _parse_method_chain(text: str, method_name: str) -> _MethodChain | None:
    pattern = f".{method_name}("
    masked = _mask_non_code(text)
    index = masked.find(pattern)
    if index < 0:
        return None
    receiver = text[:index].strip()
    open_index = index + len(pattern) - 1
    close_index = _find_matching(masked, open_index, "(", ")")
    if close_index < 0:
        return None
    args = _split_top_level(text[open_index + 1 : close_index])
    callback = args[0].strip() if args else None
    return _MethodChain(receiver=receiver, callback=callback)


def _parse_inline_function(text: str, start_index: int) -> _InlineFunction | None:
    stripped = text.strip()
    masked = _mask_non_code(stripped)
    if masked.startswith("function"):
        params_open = masked.find("(")
        if params_open < 0:
            return None
        params_close = _find_matching(masked, params_open, "(", ")")
        if params_close < 0:
            return None
        body_open = masked.find("{", params_close)
        if body_open < 0:
            return None
        body_close = _find_matching(masked, body_open, "{", "}")
        if body_close < 0:
            return None
        return _InlineFunction(
            params=_split_parameters(stripped[params_open + 1 : params_close]),
            body=stripped[body_open + 1 : body_close],
            expression_body=False,
            start_index=start_index + body_open + 1,
        )
    arrow_index = masked.find("=>")
    if arrow_index < 0:
        return None
    params_text = stripped[:arrow_index].strip()
    body_text = stripped[arrow_index + 2 :].strip()
    if params_text.startswith("(") and params_text.endswith(")"):
        params = _split_parameters(params_text[1:-1])
    else:
        params = (params_text,)
    if body_text.startswith("{"):
        body_close = _find_matching(_mask_non_code(body_text), 0, "{", "}")
        if body_close < 0:
            return None
        return _InlineFunction(
            params=params,
            body=body_text[1:body_close],
            expression_body=False,
            start_index=start_index + arrow_index + 2 + 1,
        )
    return _InlineFunction(
        params=params,
        body=body_text,
        expression_body=True,
        start_index=start_index + arrow_index + 2,
    )


def _extract_identifiers(text: str) -> tuple[str, ...]:
    return tuple(match.group(0) for match in _IDENTIFIER_PATTERN.finditer(text))


def _origins_from_identifiers(
    expression: str,
    env: Mapping[str, set[SummaryOrigin]],
) -> set[SummaryOrigin]:
    origins: set[SummaryOrigin] = set()
    for identifier in _extract_identifiers(expression):
        origins.update(env.get(identifier, set()))
    return origins


def _parameter_index(parameters: Sequence[str], candidate: str) -> int | None:
    for index, parameter in enumerate(parameters):
        if parameter == candidate:
            return index
    return None


def _strip_string_literal(text: str) -> str | None:
    match = _STRING_LITERAL_PATTERN.match(text.strip())
    if match is None:
        return None
    return match.group(2)


def _looks_like_callback(text: str) -> bool:
    stripped = text.strip()
    return "=>" in stripped or stripped.startswith("function")


def _callback_seed_params(callback_text: str, *, skip_error: bool) -> tuple[str, ...]:
    inline = _parse_inline_function(callback_text, 0)
    if inline is not None:
        params = inline.params
    else:
        identifier = _identifier_only(callback_text)
        params = (identifier,) if identifier is not None else ()
    if not skip_error:
        return params
    if params and params[0] in _KNOWN_ERROR_PARAM_NAMES:
        return params[1:] or params
    return params


def _find_argument_start(statement: str, statement_start: int, argument: str) -> int:
    offset = statement.find(argument)
    if offset < 0:
        return statement_start
    return statement_start + offset


def _identifier_only(text: str) -> str | None:
    stripped = text.strip()
    if not stripped:
        return None
    if re.fullmatch(r"[A-Za-z_$][\w$]*", stripped):
        return stripped
    if "." in stripped:
        suffix = stripped.rsplit(".", 1)[-1].strip()
        if re.fullmatch(r"[A-Za-z_$][\w$]*", suffix):
            return suffix
    return None


def _call_target_name(call: _CallExpression) -> str:
    return _identifier_only(call.callee) or call.callee


def _callsite_location(
    owner: _FunctionDef,
    call: _CallExpression | None,
    *,
    line_starts: Sequence[int],
) -> dict[str, object] | None:
    if call is None:
        return None
    return {
        "file": str(owner.file_path),
        "line": _index_to_line(line_starts, call.start_index),
        "column": _index_to_column(line_starts, call.start_index),
        "snippet": call.raw,
    }


def _contains_identifier(
    expression: str,
    values: set[_SourceFact],
    env: Mapping[str, set[_SourceFact]],
) -> bool:
    if not values:
        return False
    identifiers = set(_extract_identifiers(expression))
    return any(identifier in env and env[identifier] is values for identifier in identifiers)


def _ordered_summary_parameters(summary: FunctionSummary) -> tuple[str, ...]:
    max_index = max((transfer.from_param_index for transfer in summary.transfers), default=-1)
    return tuple(f"arg{index}" for index in range(max_index + 1))


def _starts_with_function_definition(statement: str) -> bool:
    stripped = statement.strip()
    return (
        stripped.startswith("function ")
        or stripped.startswith("export function ")
        or (
            stripped.startswith("const ")
            and ("=>" in stripped or "function" in stripped)
            and "={" in stripped.replace(" ", "")
        )
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


def _extract_parameter_name(code: str) -> str | None:
    stripped = code.strip()
    field_matches = re.findall(r"\.([A-Za-z_$][\w$]*)|\[['\"]([^'\"]+)['\"]\]", stripped)
    if field_matches:
        last = field_matches[-1]
        candidate = last[0] or last[1]
        return str(candidate)
    if re.fullmatch(r"[A-Za-z_$][\w$]*", stripped):
        return stripped
    return None


def _extract_api_name(node: QueryNode) -> str:
    if node.name and node.name != "<operator>.fieldAccess":
        prefix = _extract_api_prefix(node.code)
        if prefix is not None:
            return prefix
        return node.name
    prefix = _extract_api_prefix(node.code)
    return prefix or node.code


def _extract_api_prefix(code: str) -> str | None:
    match = re.match(r"^\s*(?:new\s+)?([^(]+?)\s*\(", code)
    if match is None:
        return None
    return match.group(1).strip()


def _candidate_finding_id(
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


def _stable_function_name(value: str | None, *, fallback: str) -> str:
    if value is None:
        return fallback
    normalized = value.strip()
    return normalized or fallback


def _severity_for_sink_spec(sink_spec: SinkSpec) -> str:
    if sink_spec.severity is not None:
        return sink_spec.severity
    if sink_spec.cwe_id is not None:
        return _SEVERITY_BY_CWE.get(sink_spec.cwe_id, _DEFAULT_SEVERITY)
    return _DEFAULT_SEVERITY


def _build_candidate_finding(
    *,
    source: _SourceFact,
    sink_spec: SinkSpec,
    sink_location: SourceLocation,
    sink_api_name: str,
    through_function: str,
    confidence: float,
    metadata: Mapping[str, object] | None = None,
) -> CandidateFinding:
    vuln_class = sink_spec.cwe_id or sink_spec.sink_type.value
    taint_path = [
        TaintStep(
            location=source.location.to_source_location(),
            operation="call_arg",
            taint_state="tainted",
            through_function=through_function,
        ),
        TaintStep(
            location=sink_location,
            operation="call_arg",
            taint_state="tainted",
            through_function=through_function,
        ),
    ]
    return CandidateFinding(
        id=_candidate_finding_id(
            vuln_class=vuln_class,
            source_function_name=source.spec_name,
            sink_function_name=sink_api_name,
            path_length=len(taint_path),
        ),
        vuln_class=vuln_class,
        source=TaintSource(
            location=source.location.to_source_location(),
            source_type=source.source_type,
            data_categories=["unknown"],
            parameter_name=source.parameter_name,
        ),
        sink=TaintSink(
            location=sink_location,
            sink_type=sink_spec.sink_type.value,
            api_name=sink_api_name,
        ),
        taint_path=taint_path,
        path_conditions=[],
        confidence=max(0.0, min(1.0, confidence)),
        severity=_severity_for_sink_spec(sink_spec),
        metadata={"interprocedural": True, **dict(metadata or {})},
    )


__all__ = [
    "FunctionSummary",
    "TaintTransfer",
    "build_function_summaries",
    "extract_interprocedural_findings",
]
