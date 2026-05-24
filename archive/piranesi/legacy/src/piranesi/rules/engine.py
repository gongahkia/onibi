from __future__ import annotations

import contextlib
import hashlib
import re
import tomllib
from collections.abc import Iterable, Iterator, Mapping, Sequence
from dataclasses import dataclass, replace
from enum import StrEnum
from pathlib import Path
from typing import Any

from piranesi.config import PiranesiConfig
from piranesi.detect.flows import extract_candidate_findings
from piranesi.models import CandidateFinding, SourceLocation, TaintSink, TaintSource
from piranesi.scan.framework import resolve_frameworks
from piranesi.scan.joern import JoernError, JoernServer, is_joern_installed
from piranesi.scan.specs import (
    SanitizerKind,
    SanitizerSpec,
    SinkSpec,
    SinkType,
    SourceSpec,
    SourceType,
)
from piranesi.scan.transpile import SourceMap, TranspiledProject, transpile_project

_VALID_SEVERITIES = frozenset({"low", "medium", "high", "critical"})
_VALID_RULE_CATEGORIES = frozenset(
    {
        "authz",
        "crypto",
        "deserialization",
        "injection",
        "misconfiguration",
        "redirect",
        "secrets",
        "ssrf",
        "supply-chain",
        "traversal",
        "xss",
        "other",
    }
)
_SUPPORTED_RULE_SCHEMA_VERSIONS = frozenset({"1", "1.0"})
_DEFAULT_RULE_SCHEMA_VERSION = "1"
_CWE_ID_PATTERN = re.compile(r"^CWE-\d+$")
_CPGQL_DANGEROUS_TOKENS = (
    "workspace",
    "importCode",
    "importCpg",
    "runScript",
    "save(",
    "delete(",
    "overflowdb",
    "sys.process",
)
_TEXT_FILE_SUFFIXES = {
    ".c",
    ".cc",
    ".cpp",
    ".cs",
    ".go",
    ".h",
    ".hpp",
    ".html",
    ".java",
    ".js",
    ".json",
    ".jsx",
    ".kt",
    ".mjs",
    ".php",
    ".py",
    ".rb",
    ".rs",
    ".scala",
    ".sql",
    ".swift",
    ".toml",
    ".ts",
    ".tsx",
    ".txt",
    ".xml",
    ".yaml",
    ".yml",
}


class PatternKind(StrEnum):
    CPGQL = "cpgql"
    REGEX = "regex"


class RuleValidationError(RuntimeError):
    """Raised when a custom rule file is invalid."""


@dataclass(frozen=True, slots=True)
class CustomRule:
    id: str
    name: str | None
    cwe_id: str | None
    severity: str | None
    description: str | None
    source_pattern: str | None
    sink_pattern: str | None
    sanitizer_patterns: tuple[str, ...]
    message_template: str | None
    tags: tuple[str, ...]
    category: str | None
    schema_version: str | None
    author: str | None
    version: str | None
    source_pattern_type: PatternKind | None
    sink_pattern_type: PatternKind | None
    sanitizer_pattern_type: PatternKind | None
    extends: str | None = None
    override_severity: str | None = None
    additional_sanitizers: tuple[str, ...] = ()
    additional_sanitizer_type: PatternKind | None = None
    path: Path | None = None


@dataclass(frozen=True, slots=True)
class CompiledRule:
    id: str
    name: str
    cwe_id: str
    severity: str
    description: str
    message_template: str
    tags: tuple[str, ...]
    category: str | None
    schema_version: str
    author: str
    version: str
    kind: PatternKind
    source_pattern: str | None
    sink_pattern: str | None
    sanitizer_patterns: tuple[str, ...]
    extends: str | None
    path: Path | None
    compiled_source_regex: re.Pattern[str] | None = None
    compiled_sink_regex: re.Pattern[str] | None = None
    compiled_sanitizer_regexes: tuple[re.Pattern[str], ...] = ()


@dataclass(frozen=True, slots=True)
class RuleTestResult:
    rule: CompiledRule
    findings: tuple[CandidateFinding, ...]


@dataclass(frozen=True, slots=True)
class _BuiltinRuleDefinition:
    alias: str
    name: str
    cwe_id: str
    severity: str
    description: str
    message_template: str

    def resolve_sources(self, source_specs: Sequence[SourceSpec]) -> tuple[SourceSpec, ...]:
        return tuple(source_specs)

    def resolve_sinks(self, sink_specs: Sequence[SinkSpec]) -> tuple[SinkSpec, ...]:
        return tuple(spec for spec in sink_specs if spec.cwe_id == self.cwe_id)

    def resolve_sanitizers(
        self,
        sanitizer_specs: Sequence[SanitizerSpec],
    ) -> tuple[SanitizerSpec, ...]:
        return tuple(spec for spec in sanitizer_specs if self.cwe_id in spec.mitigates)


_BUILTIN_RULES: dict[str, _BuiltinRuleDefinition] = {
    "builtin:sqli": _BuiltinRuleDefinition(
        alias="builtin:sqli",
        name="SQL Injection",
        cwe_id="CWE-89",
        severity="high",
        description="Built-in SQL injection rule bundle.",
        message_template="User-controlled data reaches `{sink}` from `{source}`.",
    ),
}


def load_rules(rules_dir: str | Path) -> list[CustomRule]:
    path = Path(rules_dir).expanduser()
    if not path.exists():
        return []

    if path.is_file():
        return [_load_rule_file(path, strict=True)]

    rules: list[CustomRule] = []
    seen_ids: set[str] = set()
    for rule_path in sorted(candidate for candidate in path.rglob("*.toml") if candidate.is_file()):
        loaded = _try_load_rule_file(rule_path)
        if loaded is None:
            continue
        if loaded.id in seen_ids:
            raise RuleValidationError(f"duplicate custom rule id '{loaded.id}' in {rule_path}")
        seen_ids.add(loaded.id)
        rules.append(loaded)
    return rules


def compile_rule(rule: CustomRule) -> CompiledRule:
    errors: list[str] = []

    if not rule.id:
        errors.append("missing required field: rule.id")

    builtin = _BUILTIN_RULES.get(rule.extends) if rule.extends is not None else None
    if rule.extends is not None and builtin is None:
        errors.append(f"unsupported extends target: {rule.extends}")

    resolved_name = rule.name or builtin.name if builtin is not None else rule.name
    resolved_cwe_id = rule.cwe_id or builtin.cwe_id if builtin is not None else rule.cwe_id
    resolved_severity = (
        rule.override_severity or rule.severity or builtin.severity
        if builtin is not None
        else rule.severity
    )
    resolved_description = (
        rule.description or builtin.description if builtin is not None else rule.description
    )
    resolved_message = (
        rule.message_template or builtin.message_template
        if builtin is not None
        else rule.message_template
    )
    resolved_author = rule.author or ("piranesi" if builtin is not None else None)
    resolved_version = rule.version or ("builtin" if builtin is not None else None)
    resolved_schema_version = rule.schema_version or _DEFAULT_RULE_SCHEMA_VERSION

    if not resolved_name:
        errors.append("missing required field: rule.name")
    if not resolved_cwe_id:
        errors.append("missing required field: rule.cwe_id")
    elif not _CWE_ID_PATTERN.fullmatch(resolved_cwe_id):
        errors.append(f"invalid CWE id '{resolved_cwe_id}'")
    if not resolved_severity:
        errors.append("missing required field: rule.severity")
    elif resolved_severity.lower() not in _VALID_SEVERITIES:
        errors.append(f"invalid severity '{resolved_severity}'")
    if not resolved_description:
        errors.append("missing required field: rule.description")
    if not resolved_message:
        errors.append("missing required field: rule.message_template")
    if not resolved_author:
        errors.append("missing required field: rule.author")
    if not resolved_version:
        errors.append("missing required field: rule.version")
    if resolved_schema_version not in _SUPPORTED_RULE_SCHEMA_VERSIONS:
        supported_versions = ", ".join(sorted(_SUPPORTED_RULE_SCHEMA_VERSIONS))
        errors.append(
            "unsupported schema_version "
            f"'{resolved_schema_version}'; supported versions: {supported_versions}"
        )
    if rule.category is not None and rule.category not in _VALID_RULE_CATEGORIES:
        allowed_categories = ", ".join(sorted(_VALID_RULE_CATEGORIES))
        errors.append(f"unknown category '{rule.category}'; expected one of: {allowed_categories}")

    kinds = _rule_pattern_kinds(rule, builtin=builtin)
    if isinstance(kinds, RuleValidationError):
        errors.append(str(kinds))
        kinds_tuple: tuple[PatternKind, tuple[str, ...]] | None = None
    else:
        kinds_tuple = kinds

    compiled_source_regex: re.Pattern[str] | None = None
    compiled_sink_regex: re.Pattern[str] | None = None
    compiled_sanitizer_regexes: tuple[re.Pattern[str], ...] = ()
    effective_kind = PatternKind.CPGQL
    sanitizer_patterns = (*rule.sanitizer_patterns, *rule.additional_sanitizers)

    if kinds_tuple is not None:
        effective_kind, pattern_errors = kinds_tuple
        errors.extend(pattern_errors)
        if effective_kind is PatternKind.REGEX:
            compiled_source_regex, source_error = _compile_regex_pattern(
                rule.source_pattern,
                label="source_pattern",
            )
            if source_error is not None:
                errors.append(source_error)
            compiled_sink_regex, sink_error = _compile_regex_pattern(
                rule.sink_pattern,
                label="sink_pattern",
            )
            if sink_error is not None:
                errors.append(sink_error)

            sanitizer_regexes: list[re.Pattern[str]] = []
            for index, pattern in enumerate(sanitizer_patterns, start=1):
                compiled_regex, error = _compile_regex_pattern(
                    pattern,
                    label=f"sanitizer_patterns[{index}]",
                )
                if error is not None:
                    errors.append(error)
                    continue
                if compiled_regex is not None:
                    sanitizer_regexes.append(compiled_regex)
            compiled_sanitizer_regexes = tuple(sanitizer_regexes)
        else:
            for label, raw_pattern in (
                ("source_pattern", rule.source_pattern),
                ("sink_pattern", rule.sink_pattern),
            ):
                if raw_pattern is None:
                    continue
                cpgql_error = _validate_cpgql_pattern(raw_pattern, label=label)
                if cpgql_error is not None:
                    errors.append(cpgql_error)
            for index, pattern in enumerate(sanitizer_patterns, start=1):
                cpgql_error = _validate_cpgql_pattern(
                    pattern,
                    label=f"sanitizer_patterns[{index}]",
                )
                if cpgql_error is not None:
                    errors.append(cpgql_error)

    if errors:
        prefix = f"{rule.path}: " if rule.path is not None else ""
        raise RuleValidationError(prefix + "; ".join(errors))

    return CompiledRule(
        id=rule.id,
        name=resolved_name or "",
        cwe_id=resolved_cwe_id or "",
        severity=(resolved_severity or "").lower(),
        description=resolved_description or "",
        message_template=resolved_message or "",
        tags=rule.tags,
        category=rule.category,
        schema_version=resolved_schema_version,
        author=resolved_author or "",
        version=resolved_version or "",
        kind=effective_kind,
        source_pattern=rule.source_pattern,
        sink_pattern=rule.sink_pattern,
        sanitizer_patterns=tuple(sanitizer_patterns),
        extends=rule.extends,
        path=rule.path,
        compiled_source_regex=compiled_source_regex,
        compiled_sink_regex=compiled_sink_regex,
        compiled_sanitizer_regexes=compiled_sanitizer_regexes,
    )


def filter_builtin_specs_for_custom_rules(
    compiled_rules: Sequence[CompiledRule],
    *,
    source_specs: Sequence[SourceSpec],
    sink_specs: Sequence[SinkSpec],
    sanitizer_specs: Sequence[SanitizerSpec],
) -> tuple[tuple[SourceSpec, ...], tuple[SinkSpec, ...], tuple[SanitizerSpec, ...]]:
    overridden_aliases = {rule.extends for rule in compiled_rules if rule.extends is not None}
    filtered_sinks = tuple(sink_specs)
    filtered_sanitizers = tuple(sanitizer_specs)
    for alias in sorted(overridden_aliases):
        builtin = _BUILTIN_RULES.get(alias)
        if builtin is None:
            continue
        filtered_sinks = tuple(spec for spec in filtered_sinks if spec.cwe_id != builtin.cwe_id)
        filtered_sanitizers = tuple(
            spec for spec in filtered_sanitizers if builtin.cwe_id not in spec.mitigates
        )
    return tuple(source_specs), filtered_sinks, filtered_sanitizers


def execute_custom_rules(
    server: JoernServer,
    *,
    compiled_rules: Sequence[CompiledRule],
    project_root: Path,
    joern_project_root: Path,
    source_map: SourceMap | None,
    source_specs: Sequence[SourceSpec],
    sink_specs: Sequence[SinkSpec],
    sanitizer_specs: Sequence[SanitizerSpec],
    files: Sequence[Path] | None = None,
    category_provider: Any | None = None,
    category_model: str | None = None,
) -> tuple[CandidateFinding, ...]:
    findings: list[CandidateFinding] = []
    target_files = tuple(files or _discover_text_files(project_root))

    regex_rules = [rule for rule in compiled_rules if rule.kind is PatternKind.REGEX]
    if regex_rules:
        findings.extend(
            _execute_regex_rules(
                regex_rules,
                project_root=project_root,
                files=target_files,
            )
        )

    cpgql_rules = [rule for rule in compiled_rules if rule.kind is PatternKind.CPGQL]
    for rule in cpgql_rules:
        rule_sources, rule_sinks, rule_sanitizers = _resolve_cpgql_specs(
            rule,
            source_specs=source_specs,
            sink_specs=sink_specs,
            sanitizer_specs=sanitizer_specs,
        )
        if not rule_sources or not rule_sinks:
            continue
        extracted = extract_candidate_findings(
            server,
            joern_project_root=joern_project_root,
            source_map=source_map,
            source_specs=rule_sources,
            sink_specs=rule_sinks,
            sanitizer_specs=rule_sanitizers,
            category_provider=category_provider,
            category_model=category_model,
        )
        findings.extend(_decorate_custom_findings(rule, extracted))

    return tuple(findings)


def run_rules_against_fixture(
    rules_path: str | Path,
    *,
    fixture_dir: str | Path,
) -> list[RuleTestResult]:
    compiled_rules = [compile_rule(rule) for rule in load_rules(rules_path)]
    fixture_root = Path(fixture_dir).expanduser().resolve(strict=False)
    if not fixture_root.exists() or not fixture_root.is_dir():
        raise RuleValidationError(f"fixture directory not found: {fixture_root}")

    files = tuple(_discover_text_files(fixture_root))
    results: list[RuleTestResult] = []

    regex_rules = [rule for rule in compiled_rules if rule.kind is PatternKind.REGEX]
    for rule in regex_rules:
        findings = tuple(_execute_regex_rules((rule,), project_root=fixture_root, files=files))
        results.append(RuleTestResult(rule=rule, findings=findings))

    cpgql_rules = [rule for rule in compiled_rules if rule.kind is PatternKind.CPGQL]
    if cpgql_rules:
        if not is_joern_installed():
            raise RuleValidationError("Joern is required to test CPGQL rules")

        config = PiranesiConfig()
        frameworks = resolve_frameworks(fixture_root, config.scan.frameworks)
        source_specs, sink_specs, sanitizer_specs = _framework_specs(frameworks)
        source_specs, sink_specs, sanitizer_specs = filter_builtin_specs_for_custom_rules(
            cpgql_rules,
            source_specs=source_specs,
            sink_specs=sink_specs,
            sanitizer_specs=sanitizer_specs,
        )
        with _rule_test_scan_session(fixture_root, config=config, frameworks=frameworks) as (
            server,
            joern_project_root,
            source_map,
        ):
            for rule in cpgql_rules:
                findings = execute_custom_rules(
                    server,
                    compiled_rules=(rule,),
                    project_root=fixture_root,
                    joern_project_root=joern_project_root,
                    source_map=source_map,
                    source_specs=source_specs,
                    sink_specs=sink_specs,
                    sanitizer_specs=sanitizer_specs,
                    files=files,
                )
                results.append(RuleTestResult(rule=rule, findings=findings))

    return results


def _framework_specs(
    frameworks: Sequence[str],
) -> tuple[tuple[SourceSpec, ...], tuple[SinkSpec, ...], tuple[SanitizerSpec, ...]]:
    from piranesi.scan.specs import get_sanitizer_specs, get_sink_specs, get_source_specs

    return (
        get_source_specs(frameworks=frameworks),
        get_sink_specs(frameworks=frameworks),
        get_sanitizer_specs(frameworks=frameworks),
    )


@contextlib.contextmanager
def _rule_test_scan_session(
    fixture_root: Path,
    *,
    config: PiranesiConfig,
    frameworks: Sequence[str],
) -> Iterator[tuple[JoernServer, Path, SourceMap | None]]:
    language = _project_language_for_rule_testing(fixture_root, frameworks=frameworks)
    transpiled: TranspiledProject | None = None
    joern_project_root = fixture_root
    source_map: SourceMap | None = None

    try:
        if language in {"javascript", "typescript"}:
            transpiled = transpile_project(fixture_root)
            joern_project_root = transpiled.out_dir
            source_map = transpiled.source_map

        with JoernServer(config=config.joern) as server:
            response = server.import_project(joern_project_root, language=language)
            if response.get("success") is not True:
                raise RuleValidationError(
                    f"Joern import failed for {fixture_root}: {response.get('stderr') or response}"
                )
            yield server, joern_project_root, source_map
    except JoernError as exc:
        raise RuleValidationError(str(exc)) from exc
    finally:
        if transpiled is not None:
            transpiled.cleanup()


def _project_language_for_rule_testing(project_root: Path, *, frameworks: Sequence[str]) -> str:
    suffixes = {path.suffix for path in project_root.rglob("*") if path.is_file()}
    normalized_frameworks = {framework.lower() for framework in frameworks}

    if {".ts", ".tsx", ".js", ".jsx"} & suffixes:
        return "javascript"
    if ".py" in suffixes:
        return "python"
    if ".go" in suffixes:
        return "go"
    if ".java" in suffixes or "springboot" in normalized_frameworks:
        return "java"
    return "javascript"


def _execute_regex_rules(
    compiled_rules: Sequence[CompiledRule],
    *,
    project_root: Path,
    files: Sequence[Path],
) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for rule in compiled_rules:
        source_regex = rule.compiled_source_regex
        sink_regex = rule.compiled_sink_regex
        if source_regex is None or sink_regex is None:
            continue
        for file_path in files:
            text = _read_text_file(file_path)
            if text is None:
                continue
            source_matches = list(source_regex.finditer(text))
            sink_matches = list(sink_regex.finditer(text))
            if not source_matches or not sink_matches:
                continue
            sanitizer_matches = [
                match for regex in rule.compiled_sanitizer_regexes for match in regex.finditer(text)
            ]
            for sink_match in sink_matches:
                source_match = _nearest_source_before_sink(
                    source_matches, sink_start=sink_match.start()
                )
                if source_match is None:
                    continue
                if _path_sanitized(
                    source_start=source_match.start(),
                    sink_start=sink_match.start(),
                    sanitizer_matches=sanitizer_matches,
                ):
                    continue
                findings.append(
                    _build_regex_finding(
                        rule,
                        project_root=project_root,
                        file_path=file_path,
                        text=text,
                        source_match=source_match,
                        sink_match=sink_match,
                    )
                )
    return findings


def _build_regex_finding(
    rule: CompiledRule,
    *,
    project_root: Path,
    file_path: Path,
    text: str,
    source_match: re.Match[str],
    sink_match: re.Match[str],
) -> CandidateFinding:
    source_location = _location_from_match(file_path, text, source_match)
    sink_location = _location_from_match(file_path, text, sink_match)
    source_value = source_match.group(0)
    sink_value = sink_match.group(0)
    message = _render_message(rule.message_template, source_value, sink_value)
    relative_path = str(
        file_path.resolve(strict=False).relative_to(project_root.resolve(strict=False))
    )

    return CandidateFinding(
        id=_custom_rule_finding_id(
            rule_id=rule.id,
            relative_path=relative_path,
            source_location=source_location,
            sink_location=sink_location,
        ),
        vuln_class=rule.cwe_id,
        source=TaintSource(
            location=source_location,
            source_type="custom_rule_regex",
            data_categories=["unknown"],
            parameter_name=source_value,
        ),
        sink=TaintSink(
            location=sink_location,
            sink_type=SinkType.CUSTOM.value,
            api_name=_regex_api_name(sink_value),
        ),
        taint_path=[],
        path_conditions=[],
        confidence=0.75,
        severity=rule.severity,
        metadata=_custom_rule_metadata(rule, message=message),
    )


def _custom_rule_finding_id(
    *,
    rule_id: str,
    relative_path: str,
    source_location: SourceLocation,
    sink_location: SourceLocation,
) -> str:
    material = "|".join(
        (
            rule_id,
            relative_path,
            str(source_location.line),
            str(source_location.column),
            str(sink_location.line),
            str(sink_location.column),
        )
    )
    return hashlib.sha256(material.encode("utf-8")).hexdigest()


def _location_from_match(file_path: Path, text: str, match: re.Match[str]) -> SourceLocation:
    line = text.count("\n", 0, match.start()) + 1
    line_start = text.rfind("\n", 0, match.start()) + 1
    line_end = text.find("\n", match.start())
    if line_end == -1:
        line_end = len(text)
    snippet = text[line_start:line_end].strip()
    column = match.start() - line_start + 1
    return SourceLocation(
        file=str(file_path.resolve(strict=False)),
        line=line,
        column=column,
        end_line=line,
        end_column=column + len(match.group(0)),
        snippet=snippet,
    )


def _nearest_source_before_sink(
    source_matches: Sequence[re.Match[str]],
    *,
    sink_start: int,
) -> re.Match[str] | None:
    candidates = [match for match in source_matches if match.start() <= sink_start]
    if not candidates:
        return None
    return max(candidates, key=lambda candidate: candidate.start())


def _path_sanitized(
    *,
    source_start: int,
    sink_start: int,
    sanitizer_matches: Sequence[re.Match[str]],
) -> bool:
    return any(source_start <= match.start() <= sink_start for match in sanitizer_matches)


def _read_text_file(path: Path) -> str | None:
    try:
        return path.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError):
        return None


def _resolve_cpgql_specs(
    rule: CompiledRule,
    *,
    source_specs: Sequence[SourceSpec],
    sink_specs: Sequence[SinkSpec],
    sanitizer_specs: Sequence[SanitizerSpec],
) -> tuple[tuple[SourceSpec, ...], tuple[SinkSpec, ...], tuple[SanitizerSpec, ...]]:
    if rule.extends is not None:
        builtin = _BUILTIN_RULES[rule.extends]
        base_sources = builtin.resolve_sources(source_specs)
        base_sinks = tuple(
            replace(spec, severity=rule.severity) for spec in builtin.resolve_sinks(sink_specs)
        )
        base_sanitizers = list(builtin.resolve_sanitizers(sanitizer_specs))
        for index, pattern in enumerate(rule.sanitizer_patterns, start=1):
            base_sanitizers.append(
                SanitizerSpec(
                    name=f"{rule.id}_sanitizer_{index}",
                    pattern=pattern,
                    kind=SanitizerKind.NORMALIZE,
                    mitigates=(rule.cwe_id,),
                )
            )
        return base_sources, base_sinks, tuple(base_sanitizers)

    resolved_source = SourceSpec(
        name=f"{rule.id}_source",
        pattern=rule.source_pattern or "",
        source_type=SourceType.CUSTOM,
        is_custom=True,
    )
    resolved_sink = SinkSpec(
        name=f"{rule.id}_sink",
        pattern=rule.sink_pattern or "",
        sink_type=SinkType.CUSTOM,
        cwe_id=rule.cwe_id,
        severity=rule.severity,
        is_custom=True,
    )
    resolved_sanitizers = tuple(
        SanitizerSpec(
            name=f"{rule.id}_sanitizer_{index}",
            pattern=pattern,
            kind=SanitizerKind.NORMALIZE,
            mitigates=(rule.cwe_id,),
        )
        for index, pattern in enumerate(rule.sanitizer_patterns, start=1)
    )
    return (resolved_source,), (resolved_sink,), resolved_sanitizers


def _decorate_custom_findings(
    rule: CompiledRule,
    findings: Sequence[CandidateFinding],
) -> tuple[CandidateFinding, ...]:
    decorated: list[CandidateFinding] = []
    for finding in findings:
        message = _render_message(
            rule.message_template,
            finding.source.parameter_name or finding.source.location.snippet,
            finding.sink.api_name or finding.sink.location.snippet,
        )
        metadata = dict(finding.metadata)
        metadata.update(_custom_rule_metadata(rule, message=message))
        decorated.append(
            finding.model_copy(
                update={
                    "id": _custom_rule_finding_id(
                        rule_id=rule.id,
                        relative_path=Path(finding.sink.location.file).name,
                        source_location=finding.source.location,
                        sink_location=finding.sink.location,
                    ),
                    "vuln_class": rule.cwe_id,
                    "severity": rule.severity,
                    "metadata": metadata,
                }
            )
        )
    return tuple(decorated)


def _custom_rule_metadata(rule: CompiledRule, *, message: str) -> dict[str, object]:
    return {
        "custom_rule_id": rule.id,
        "custom_rule_name": rule.name,
        "custom_rule_author": rule.author,
        "custom_rule_version": rule.version,
        "custom_rule_schema_version": rule.schema_version,
        "custom_rule_category": rule.category,
        "custom_rule_tags": list(rule.tags),
        "custom_rule_message": message,
    }


def _render_message(template: str, source: str, sink: str) -> str:
    try:
        return template.format(source=source, sink=sink)
    except (KeyError, IndexError, ValueError):
        return template


def _regex_api_name(sink_text: str) -> str:
    match = re.search(r"([A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)?)\s*\(", sink_text)
    if match is not None:
        return match.group(1)
    return sink_text.strip()[:80]


def _compile_regex_pattern(
    pattern: str | None,
    *,
    label: str,
) -> tuple[re.Pattern[str] | None, str | None]:
    if pattern is None:
        return None, f"missing required field: {label}"
    try:
        return re.compile(pattern, re.MULTILINE), None
    except re.error as exc:
        return None, f"{label} failed to compile: {exc}"


def _validate_cpgql_pattern(pattern: str, *, label: str) -> str | None:
    normalized = pattern.strip()
    if not normalized:
        return f"{label} cannot be empty"
    if "cpg" not in normalized:
        return f"{label} does not look like CPGQL"
    lowered = normalized.lower()
    dangerous = next((token for token in _CPGQL_DANGEROUS_TOKENS if token.lower() in lowered), None)
    if dangerous is not None:
        return f"{label} contains disallowed token '{dangerous}'"
    if not _balanced_delimiters(normalized):
        return f"{label} has unbalanced delimiters"
    return None


def _balanced_delimiters(text: str) -> bool:
    stack: list[str] = []
    pairs = {")": "(", "]": "[", "}": "{"}
    in_single = False
    in_double = False
    escaped = False
    for char in text:
        if escaped:
            escaped = False
            continue
        if char == "\\":
            escaped = True
            continue
        if char == "'" and not in_double:
            in_single = not in_single
            continue
        if char == '"' and not in_single:
            in_double = not in_double
            continue
        if in_single or in_double:
            continue
        if char in "([{":
            stack.append(char)
        elif char in ")]}" and (not stack or stack.pop() != pairs[char]):
            return False
    return not stack and not in_single and not in_double


def _rule_pattern_kinds(
    rule: CustomRule,
    *,
    builtin: _BuiltinRuleDefinition | None,
) -> tuple[PatternKind, tuple[str, ...]] | RuleValidationError:
    errors: list[str] = []
    if builtin is not None:
        if rule.source_pattern is not None or rule.sink_pattern is not None:
            errors.append("extends rules inherit source_pattern and sink_pattern from the builtin")
        return PatternKind.CPGQL, tuple(errors)

    if rule.source_pattern is None:
        errors.append("missing required field: rule.source_pattern")
    if rule.sink_pattern is None:
        errors.append("missing required field: rule.sink_pattern")
    if errors:
        return RuleValidationError("; ".join(errors))

    source_kind = rule.source_pattern_type or _infer_pattern_kind(rule.source_pattern or "")
    sink_kind = rule.sink_pattern_type or _infer_pattern_kind(rule.sink_pattern or "")
    sanitizer_kind = (
        rule.sanitizer_pattern_type or source_kind
        if not rule.sanitizer_patterns
        else rule.sanitizer_pattern_type or _infer_pattern_kind(rule.sanitizer_patterns[0])
    )
    if source_kind != sink_kind:
        errors.append("source_pattern and sink_pattern must use the same pattern type")
    if rule.sanitizer_patterns and sanitizer_kind != source_kind:
        errors.append("sanitizer_patterns must use the same pattern type as the rule")
    if errors:
        return RuleValidationError("; ".join(errors))
    return source_kind, ()


def _infer_pattern_kind(pattern: str) -> PatternKind:
    normalized = pattern.strip()
    if (
        normalized.startswith("cpg.")
        or normalized.startswith("(cpg.")
        or ".reachableBy" in normalized
    ):
        return PatternKind.CPGQL
    return PatternKind.REGEX


def _try_load_rule_file(path: Path) -> CustomRule | None:
    with path.open("rb") as handle:
        try:
            document = tomllib.load(handle)
        except tomllib.TOMLDecodeError as exc:
            raise RuleValidationError(f"{path}: invalid TOML: {exc}") from exc
    if not isinstance(document, dict):
        raise RuleValidationError(f"{path}: expected a TOML table at the root")
    if "rule" not in document:
        return None
    return _parse_rule_document(document, path=path)


def _load_rule_file(path: Path, *, strict: bool) -> CustomRule:
    loaded = _try_load_rule_file(path)
    if loaded is None:
        if strict:
            raise RuleValidationError(f"{path}: expected a [rule] table")
        raise RuleValidationError(f"{path}: no custom rule found")
    return loaded


def _parse_rule_document(document: Mapping[str, Any], *, path: Path) -> CustomRule:
    _validate_document_shape(document, path=path)

    rule_section = document.get("rule")
    if not isinstance(rule_section, Mapping):
        raise RuleValidationError(f"{path}: [rule] must be a TOML table")

    source_section = _mapping(rule_section.get("source"))
    sink_section = _mapping(rule_section.get("sink"))
    sanitizers_section = _mapping(rule_section.get("sanitizers"))
    additional_sanitizers_section = _mapping(rule_section.get("additional_sanitizers"))
    message_section = _mapping(rule_section.get("message"))

    return CustomRule(
        id=_required_string(rule_section, "id", path=path),
        name=_optional_string(rule_section.get("name")),
        cwe_id=_optional_string(rule_section.get("cwe_id")),
        severity=_normalized_optional_string(rule_section.get("severity")),
        description=_optional_string(rule_section.get("description")),
        source_pattern=_optional_string(
            rule_section.get("source_pattern") or source_section.get("pattern")
        ),
        sink_pattern=_optional_string(
            rule_section.get("sink_pattern") or sink_section.get("pattern")
        ),
        sanitizer_patterns=_string_list(
            rule_section.get("sanitizer_patterns") or sanitizers_section.get("patterns")
        ),
        message_template=_optional_string(
            rule_section.get("message_template") or message_section.get("template")
        ),
        tags=_string_list(rule_section.get("tags")),
        category=_normalized_optional_string(rule_section.get("category")),
        schema_version=_optional_string(rule_section.get("schema_version")),
        author=_optional_string(rule_section.get("author")),
        version=_optional_string(rule_section.get("version")),
        source_pattern_type=_pattern_kind(
            rule_section.get("source_type") or source_section.get("type"),
            path=path,
            field="source_type",
        ),
        sink_pattern_type=_pattern_kind(
            rule_section.get("sink_type") or sink_section.get("type"),
            path=path,
            field="sink_type",
        ),
        sanitizer_pattern_type=_pattern_kind(
            rule_section.get("sanitizer_type") or sanitizers_section.get("type"),
            path=path,
            field="sanitizer_type",
        ),
        extends=_optional_string(rule_section.get("extends")),
        override_severity=_normalized_optional_string(rule_section.get("override_severity")),
        additional_sanitizers=_string_list(additional_sanitizers_section.get("patterns")),
        additional_sanitizer_type=_pattern_kind(
            additional_sanitizers_section.get("type"),
            path=path,
            field="additional_sanitizers.type",
        ),
        path=path,
    )


def _validate_document_shape(document: Mapping[str, Any], *, path: Path) -> None:
    top_level_unknown = _unknown_keys(document.keys(), {"rule", "tests"})
    if top_level_unknown:
        raise RuleValidationError(
            f"{path}: unknown top-level field(s): {', '.join(top_level_unknown)}"
        )

    rule_section = document.get("rule")
    if not isinstance(rule_section, Mapping):
        raise RuleValidationError(f"{path}: [rule] must be a TOML table")

    unknown_rule_fields = _unknown_keys(
        rule_section.keys(),
        {
            "id",
            "name",
            "cwe_id",
            "severity",
            "description",
            "source_pattern",
            "sink_pattern",
            "sanitizer_patterns",
            "message_template",
            "tags",
            "category",
            "schema_version",
            "author",
            "version",
            "source_type",
            "sink_type",
            "sanitizer_type",
            "extends",
            "override_severity",
            "source",
            "sink",
            "sanitizers",
            "additional_sanitizers",
            "message",
        },
    )
    if unknown_rule_fields:
        fields = ", ".join(f"rule.{field}" for field in unknown_rule_fields)
        raise RuleValidationError(f"{path}: unknown field(s): {fields}")

    for section_name, allowed_fields in (
        ("source", {"pattern", "type"}),
        ("sink", {"pattern", "type"}),
        ("sanitizers", {"patterns", "type"}),
        ("additional_sanitizers", {"patterns", "type"}),
        ("message", {"template"}),
    ):
        raw_section = rule_section.get(section_name)
        if raw_section is None:
            continue
        if not isinstance(raw_section, Mapping):
            raise RuleValidationError(f"{path}: [rule.{section_name}] must be a TOML table")
        unknown_section_fields = _unknown_keys(raw_section.keys(), allowed_fields)
        if unknown_section_fields:
            fields = ", ".join(f"rule.{section_name}.{field}" for field in unknown_section_fields)
            raise RuleValidationError(f"{path}: unknown field(s): {fields}")


def _unknown_keys(keys: Iterable[str], allowed: set[str]) -> tuple[str, ...]:
    return tuple(sorted(key for key in keys if key not in allowed))


def _mapping(value: Any) -> Mapping[str, Any]:
    return value if isinstance(value, Mapping) else {}


def _required_string(section: Mapping[str, Any], key: str, *, path: Path) -> str:
    value = _optional_string(section.get(key))
    if value is None:
        raise RuleValidationError(f"{path}: missing required field rule.{key}")
    return value


def _optional_string(value: Any) -> str | None:
    if value is None:
        return None
    if not isinstance(value, str):
        raise RuleValidationError(f"expected a string, got {type(value).__name__}")
    normalized = value.strip()
    return normalized or None


def _normalized_optional_string(value: Any) -> str | None:
    normalized = _optional_string(value)
    return normalized.lower() if normalized is not None else None


def _string_list(value: Any) -> tuple[str, ...]:
    if value is None:
        return ()
    if not isinstance(value, list) or not all(isinstance(item, str) for item in value):
        raise RuleValidationError("expected a list of strings")
    return tuple(item.strip() for item in value if item.strip())


def _pattern_kind(value: Any, *, path: Path, field: str) -> PatternKind | None:
    normalized = _optional_string(value)
    if normalized is None:
        return None
    try:
        return PatternKind(normalized.lower())
    except ValueError as exc:
        raise RuleValidationError(
            f"{path}: invalid pattern type for {field}: {normalized}"
        ) from exc


def _discover_text_files(root: Path) -> list[Path]:
    files: list[Path] = []
    for candidate in sorted(root.rglob("*")):
        if not candidate.is_file():
            continue
        if candidate.name.startswith("."):
            continue
        if candidate.suffix.lower() in _TEXT_FILE_SUFFIXES or not candidate.suffix:
            files.append(candidate)
    return files


__all__ = [
    "CompiledRule",
    "CustomRule",
    "PatternKind",
    "RuleTestResult",
    "RuleValidationError",
    "compile_rule",
    "execute_custom_rules",
    "filter_builtin_specs_for_custom_rules",
    "load_rules",
    "run_rules_against_fixture",
]
