from __future__ import annotations

import hashlib
import re
from collections.abc import Sequence
from dataclasses import dataclass
from pathlib import Path

from piranesi.detect._source_scan import ScannedSourceFile
from piranesi.models import CandidateFinding, SourceLocation, TaintSink, TaintSource, TaintStep
from piranesi.scan.specs import SinkType
from piranesi.scan.transpile import SourceMap

_DEFAULT_DATA_CATEGORIES = ["unknown"]
_SEVERITY_BY_CWE = {
    "CWE-22": "medium",
    "CWE-78": "critical",
    "CWE-79": "medium",
    "CWE-89": "high",
    "CWE-352": "medium",
    "CWE-502": "high",
    "CWE-915": "high",
    "CWE-1336": "critical",
}
_RUBY_EXTENSIONS = frozenset({".rb", ".erb"})
_IGNORED_PATH_SEGMENTS = frozenset(
    {"vendor", "tmp", "log", ".bundle", ".git", "node_modules", "__pycache__"}
)
_SOURCE_PATTERNS: tuple[tuple[re.Pattern[str], str], ...] = (
    (re.compile(r"params(?:\[[^\]]+\]|\.\w+\([^)]*\)|\.\w+)?"), "request_param"),
    (re.compile(r"request\.body(?:\.read)?|request\.raw_post"), "request_body"),
    (re.compile(r"(?:request\.)?cookies(?:\[[^\]]+\])?"), "cookie"),
    (re.compile(r'ENV\[(?:"[^"]+"|\'[^\']+\'|:[A-Za-z_]\w*)\]'), "env_var"),
)
_SAFE_HTML_PATTERNS = (
    "sanitize(",
    "strip_tags(",
    "html_escape(",
    "ERB::Util.html_escape",
    "CGI.escapeHTML",
    "Rack::Utils.escape_html",
    " h(",
)
_SAFE_SQL_PATTERNS = ("sanitize_sql", "sanitize_sql_array")
_SAFE_SHELL_PATTERNS = ("Shellwords.shellescape", "Shellwords.escape")
_SAFE_PATH_PATTERNS = ("File.basename(", "File.expand_path(")

_MASS_ASSIGNMENT_RE = re.compile(
    r"\.(?P<api>new|create!?|update!?|update_attributes!?|assign_attributes)\s*\((?P<expr>.+)\)"
)
_PERMIT_BANG_RE = re.compile(r"\.permit!\b")
_QUERY_RE = re.compile(
    r"\.(?P<api>where|having|select|joins|from|order|group|pluck)\s*\((?P<expr>.+)\)"
)
_FIND_BY_SQL_RE = re.compile(r"\.(?P<api>find_by_sql)\s*\((?P<expr>.+)\)")
_RAW_CALL_RE = re.compile(r"\braw\b(?:\s*\((?P<expr_paren>.+)\)|\s+(?P<expr_bare>.+))")
_HTML_SAFE_RE = re.compile(r"(?P<expr>.+?)\.html_safe\b")
_SYSTEM_RE = re.compile(r"\b(?P<api>system|IO\.popen)\s*\((?P<expr>.+)\)")
_FILE_READ_RE = re.compile(r"\b(?P<api>File\.read)\s*\((?P<expr>.+)\)")
_DESERIALIZE_RE = re.compile(r"\b(?P<api>Marshal\.load|YAML\.load)\s*\((?P<expr>.+)\)")
_RENDER_RE = re.compile(r"\brender\s*(?:\(\s*)?(?P<expr>.+)")
_SKIP_CSRF_RE = re.compile(r"\bskip_before_action\s+:verify_authenticity_token\b")


@dataclass(frozen=True, slots=True)
class _SourceMatch:
    source_type: str
    parameter_name: str | None
    expression: str
    column: int


def extract_ruby_rails_findings(
    project_root: str | Path,
    *,
    source_map: SourceMap | None = None,
    files: Sequence[Path] | None = None,
) -> tuple[CandidateFinding, ...]:
    root = Path(project_root).resolve(strict=False)
    findings: list[CandidateFinding] = []
    for scanned_file in _iter_ruby_source_files(root, source_map=source_map, files=files):
        findings.extend(_scan_file(scanned_file))
    return tuple(_dedupe_findings(findings))


def _iter_ruby_source_files(
    project_root: Path,
    *,
    source_map: SourceMap | None,
    files: Sequence[Path] | None,
) -> tuple[ScannedSourceFile, ...]:
    if files is not None:
        candidates = tuple(path.resolve(strict=False) for path in files if path.exists())
    elif source_map is not None:
        candidates = source_map.original_files()
    else:
        candidates = tuple(path.resolve(strict=False) for path in sorted(project_root.rglob("*")))

    scanned: list[ScannedSourceFile] = []
    for path in candidates:
        if not path.is_file() or path.suffix.lower() not in _RUBY_EXTENSIONS:
            continue
        if any(part in _IGNORED_PATH_SEGMENTS for part in path.parts):
            continue
        loaded = ScannedSourceFile.load(path, root=project_root)
        if loaded is not None:
            scanned.append(loaded)
    return tuple(scanned)


def _scan_file(scanned_file: ScannedSourceFile) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    has_mass_assignment_on_line: set[int] = set()

    for line_number, line in enumerate(scanned_file.lines, start=1):
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue

        mass_assignment = _mass_assignment_finding(scanned_file, line_number, line)
        if mass_assignment is not None:
            findings.append(mass_assignment)
            has_mass_assignment_on_line.add(line_number)

        permit_bang = _permit_bang_finding(scanned_file, line_number, line)
        if permit_bang is not None and line_number not in has_mass_assignment_on_line:
            findings.append(permit_bang)

        query_finding = _query_finding(scanned_file, line_number, line)
        if query_finding is not None:
            findings.append(query_finding)

        raw_finding = _raw_html_finding(scanned_file, line_number, line)
        if raw_finding is not None:
            findings.append(raw_finding)

        shell_finding = _shell_finding(scanned_file, line_number, line)
        if shell_finding is not None:
            findings.append(shell_finding)

        file_read_finding = _file_read_finding(scanned_file, line_number, line)
        if file_read_finding is not None:
            findings.append(file_read_finding)

        deserialization_finding = _deserialization_finding(scanned_file, line_number, line)
        if deserialization_finding is not None:
            findings.append(deserialization_finding)

        render_finding = _render_finding(scanned_file, line_number, line)
        if render_finding is not None:
            findings.append(render_finding)

        csrf_skip_finding = _csrf_skip_finding(scanned_file, line_number, line)
        if csrf_skip_finding is not None:
            findings.append(csrf_skip_finding)

    protect_from_forgery = _missing_protect_from_forgery_finding(scanned_file)
    if protect_from_forgery is not None:
        findings.append(protect_from_forgery)
    return findings


def _mass_assignment_finding(
    scanned_file: ScannedSourceFile,
    line_number: int,
    line: str,
) -> CandidateFinding | None:
    match = _MASS_ASSIGNMENT_RE.search(line)
    if match is None:
        return None
    expr = match.group("expr").strip()
    if not expr.startswith("params"):
        return None
    if ".permit(" in expr and ".permit!" not in expr:
        return None

    source = _first_source(expr)
    if source is None:
        return None
    api_name = match.group("api")
    column = line.find(api_name)
    return _build_taint_finding(
        scanned_file=scanned_file,
        line_number=line_number,
        line=line,
        source=source,
        sink_type=SinkType.ORM_WRITE.value,
        api_name=api_name,
        cwe_id="CWE-915",
        sink_column=column + 1 if column >= 0 else 1,
        metadata={"ruby_pattern": "mass_assignment"},
    )


def _permit_bang_finding(
    scanned_file: ScannedSourceFile,
    line_number: int,
    line: str,
) -> CandidateFinding | None:
    match = _PERMIT_BANG_RE.search(line)
    if match is None:
        return None
    source = _first_source(line)
    if source is None:
        return None
    return _build_taint_finding(
        scanned_file=scanned_file,
        line_number=line_number,
        line=line,
        source=source,
        sink_type=SinkType.ORM_WRITE.value,
        api_name="permit!",
        cwe_id="CWE-915",
        sink_column=match.start() + 1,
        metadata={"ruby_pattern": "permit_bang"},
    )


def _query_finding(
    scanned_file: ScannedSourceFile,
    line_number: int,
    line: str,
) -> CandidateFinding | None:
    query_match = _QUERY_RE.search(line)
    if query_match is not None:
        expr = query_match.group("expr")
        source = _first_source(expr)
        if source is not None and _is_unsafe_sql_expression(expr):
            api_name = query_match.group("api")
            return _build_taint_finding(
                scanned_file=scanned_file,
                line_number=line_number,
                line=line,
                source=source,
                sink_type=SinkType.SQL_QUERY.value,
                api_name=api_name,
                cwe_id="CWE-89",
                sink_column=query_match.start("api") + 1,
                metadata={"ruby_pattern": "active_record_query"},
            )

    sql_match = _FIND_BY_SQL_RE.search(line)
    if sql_match is None:
        return None
    expr = sql_match.group("expr")
    source = _first_source(expr)
    if source is None or not _is_unsafe_sql_expression(expr):
        return None
    return _build_taint_finding(
        scanned_file=scanned_file,
        line_number=line_number,
        line=line,
        source=source,
        sink_type=SinkType.SQL_QUERY.value,
        api_name=sql_match.group("api"),
        cwe_id="CWE-89",
        sink_column=sql_match.start("api") + 1,
        metadata={"ruby_pattern": "find_by_sql"},
    )


def _raw_html_finding(
    scanned_file: ScannedSourceFile,
    line_number: int,
    line: str,
) -> CandidateFinding | None:
    if any(pattern in line for pattern in _SAFE_HTML_PATTERNS):
        return None

    raw_match = _RAW_CALL_RE.search(line)
    if raw_match is not None:
        expr = (raw_match.group("expr_paren") or raw_match.group("expr_bare") or "").strip()
        source = _first_source(expr)
        if source is not None:
            return _build_taint_finding(
                scanned_file=scanned_file,
                line_number=line_number,
                line=line,
                source=source,
                sink_type=SinkType.HTML_OUTPUT.value,
                api_name="raw",
                cwe_id="CWE-79",
                sink_column=raw_match.start() + 1,
                metadata={"ruby_pattern": "raw_helper"},
            )

    html_safe_match = _HTML_SAFE_RE.search(line)
    if html_safe_match is None:
        return None
    source = _first_source(html_safe_match.group("expr"))
    if source is None:
        return None
    return _build_taint_finding(
        scanned_file=scanned_file,
        line_number=line_number,
        line=line,
        source=source,
        sink_type=SinkType.HTML_OUTPUT.value,
        api_name="html_safe",
        cwe_id="CWE-79",
        sink_column=html_safe_match.start() + 1,
        metadata={"ruby_pattern": "html_safe"},
    )


def _shell_finding(
    scanned_file: ScannedSourceFile,
    line_number: int,
    line: str,
) -> CandidateFinding | None:
    match = _SYSTEM_RE.search(line)
    if match is None:
        return None
    expr = match.group("expr")
    source = _first_source(expr)
    if source is None or any(pattern in expr for pattern in _SAFE_SHELL_PATTERNS):
        return None
    if match.group("api") == "system" and re.match(r'\s*["\'][^"\']*["\']\s*,', expr):
        return None
    if match.group("api") == "IO.popen" and expr.lstrip().startswith("["):
        return None
    return _build_taint_finding(
        scanned_file=scanned_file,
        line_number=line_number,
        line=line,
        source=source,
        sink_type=SinkType.SHELL_EXEC.value,
        api_name=match.group("api"),
        cwe_id="CWE-78",
        sink_column=match.start("api") + 1,
        metadata={"ruby_pattern": "shell_exec"},
    )


def _file_read_finding(
    scanned_file: ScannedSourceFile,
    line_number: int,
    line: str,
) -> CandidateFinding | None:
    match = _FILE_READ_RE.search(line)
    if match is None:
        return None
    expr = match.group("expr")
    source = _first_source(expr)
    if source is None or any(pattern in expr for pattern in _SAFE_PATH_PATTERNS):
        return None
    return _build_taint_finding(
        scanned_file=scanned_file,
        line_number=line_number,
        line=line,
        source=source,
        sink_type=SinkType.FILE_READ.value,
        api_name=match.group("api"),
        cwe_id="CWE-22",
        sink_column=match.start("api") + 1,
        metadata={"ruby_pattern": "file_read"},
    )


def _deserialization_finding(
    scanned_file: ScannedSourceFile,
    line_number: int,
    line: str,
) -> CandidateFinding | None:
    match = _DESERIALIZE_RE.search(line)
    if match is None:
        return None
    if "YAML.safe_load" in line:
        return None
    expr = match.group("expr")
    source = _first_source(expr)
    if source is None:
        return None
    return _build_taint_finding(
        scanned_file=scanned_file,
        line_number=line_number,
        line=line,
        source=source,
        sink_type=SinkType.DESERIALIZATION.value,
        api_name=match.group("api"),
        cwe_id="CWE-502",
        sink_column=match.start("api") + 1,
        metadata={"ruby_pattern": "deserialization"},
    )


def _render_finding(
    scanned_file: ScannedSourceFile,
    line_number: int,
    line: str,
) -> CandidateFinding | None:
    match = _RENDER_RE.search(line)
    if match is None:
        return None
    expr = match.group("expr").strip()
    if not (
        expr.startswith("params")
        or "file:" in expr
        or "inline:" in expr
        or "template:" in expr
        or "action:" in expr
    ):
        return None
    source = _first_source(expr)
    if source is None:
        return None
    return _build_taint_finding(
        scanned_file=scanned_file,
        line_number=line_number,
        line=line,
        source=source,
        sink_type=SinkType.TEMPLATE_INJECTION.value,
        api_name="render",
        cwe_id="CWE-1336",
        sink_column=match.start() + 1,
        metadata={"ruby_pattern": "render"},
    )


def _csrf_skip_finding(
    scanned_file: ScannedSourceFile,
    line_number: int,
    line: str,
) -> CandidateFinding | None:
    match = _SKIP_CSRF_RE.search(line)
    if match is None:
        return None
    location = scanned_file.location_for_line(line_number, column=match.start() + 1, snippet=line)
    return _build_configuration_finding(
        location=location,
        cwe_id="CWE-352",
        sink_type="security_misconfiguration",
        api_name="skip_before_action",
        metadata={"ruby_pattern": "csrf_skip"},
    )


def _missing_protect_from_forgery_finding(
    scanned_file: ScannedSourceFile,
) -> CandidateFinding | None:
    normalized = scanned_file.relative_path.replace("\\", "/")
    if not normalized.endswith("app/controllers/application_controller.rb"):
        return None
    text = scanned_file.text
    if "ActionController::API" in text or "ActionController::Base" not in text:
        return None
    if "protect_from_forgery" in text:
        return None
    location = scanned_file.location_for_line(
        1,
        snippet=scanned_file.lines[0] if scanned_file.lines else "",
    )
    return _build_configuration_finding(
        location=location,
        cwe_id="CWE-352",
        sink_type="security_misconfiguration",
        api_name="protect_from_forgery",
        metadata={"ruby_pattern": "missing_protect_from_forgery"},
    )


def _is_unsafe_sql_expression(expression: str) -> bool:
    stripped = expression.strip()
    if any(pattern in stripped for pattern in _SAFE_SQL_PATTERNS):
        return False
    if re.search(r'["\'][^"\']*\?[^"\']*["\']\s*,', stripped):
        return False
    if stripped.startswith("{") or re.match(r"^[A-Za-z_]\w*:\s*", stripped):
        return False
    return "#{" in stripped or "+" in stripped or _first_source(stripped) is not None


def _first_source(text: str) -> _SourceMatch | None:
    best: _SourceMatch | None = None
    for pattern, source_type in _SOURCE_PATTERNS:
        for match in pattern.finditer(text):
            parameter_name = _parameter_name_from_source(match.group(0))
            candidate = _SourceMatch(
                source_type=source_type,
                parameter_name=parameter_name,
                expression=match.group(0),
                column=match.start() + 1,
            )
            if best is None or candidate.column < best.column:
                best = candidate
    return best


def _parameter_name_from_source(expression: str) -> str | None:
    index_match = re.search(
        r"\[\s*(?::(?P<symbol>[A-Za-z_]\w*)|['\"](?P<string>[^'\"]+)['\"])\s*\]",
        expression,
    )
    if index_match is not None:
        return index_match.group("symbol") or index_match.group("string")
    require_match = re.search(r"\.require\(\s*:(?P<symbol>[A-Za-z_]\w*)\s*\)", expression)
    if require_match is not None:
        return require_match.group("symbol")
    env_match = re.search(
        r'ENV\[(?:"(?P<double>[^"]+)"|\'(?P<single>[^\']+)\'|:(?P<symbol>[A-Za-z_]\w*))\]',
        expression,
    )
    if env_match is not None:
        return env_match.group("double") or env_match.group("single") or env_match.group("symbol")
    return None


def _build_taint_finding(
    *,
    scanned_file: ScannedSourceFile,
    line_number: int,
    line: str,
    source: _SourceMatch,
    sink_type: str,
    api_name: str,
    cwe_id: str,
    sink_column: int,
    metadata: dict[str, object],
) -> CandidateFinding:
    source_location = scanned_file.location_for_line(
        line_number,
        column=source.column,
        snippet=source.expression,
    )
    sink_location = scanned_file.location_for_line(
        line_number,
        column=sink_column,
        snippet=line,
    )
    finding_id = hashlib.sha256(
        (
            f"{cwe_id}:{source_location.file}:{source_location.line}:"
            f"{source_location.column}:{api_name}:{sink_location.line}"
        ).encode()
    ).hexdigest()[:16]
    return CandidateFinding(
        id=f"ruby-{finding_id}",
        vuln_class=cwe_id,
        source=TaintSource(
            location=source_location,
            source_type=source.source_type,
            data_categories=_DEFAULT_DATA_CATEGORIES,
            parameter_name=source.parameter_name,
        ),
        sink=TaintSink(
            location=sink_location,
            sink_type=sink_type,
            api_name=api_name,
        ),
        taint_path=[
            TaintStep(
                location=source_location,
                operation="assignment",
                taint_state="tainted",
                through_function=None,
            ),
            TaintStep(
                location=sink_location,
                operation="call_arg",
                taint_state="tainted",
                through_function=None,
            ),
        ],
        path_conditions=[],
        confidence=0.85,
        severity=_SEVERITY_BY_CWE[cwe_id],
        metadata=metadata,
    )


def _build_configuration_finding(
    *,
    location: SourceLocation,
    cwe_id: str,
    sink_type: str,
    api_name: str,
    metadata: dict[str, object],
) -> CandidateFinding:
    finding_id = hashlib.sha256(
        f"{cwe_id}:{location.file}:{location.line}:{api_name}".encode()
    ).hexdigest()[:16]
    return CandidateFinding(
        id=f"ruby-{finding_id}",
        vuln_class=cwe_id,
        source=TaintSource(
            location=location,
            source_type="security_configuration",
            data_categories=[],
            parameter_name=None,
        ),
        sink=TaintSink(
            location=location,
            sink_type=sink_type,
            api_name=api_name,
        ),
        taint_path=[],
        path_conditions=[],
        confidence=0.9,
        severity=_SEVERITY_BY_CWE[cwe_id],
        metadata=metadata,
    )


def _dedupe_findings(findings: Sequence[CandidateFinding]) -> list[CandidateFinding]:
    deduped: list[CandidateFinding] = []
    seen: set[tuple[object, ...]] = set()
    for finding in findings:
        key = (
            finding.vuln_class,
            finding.source.location.file,
            finding.source.location.line,
            finding.source.location.column,
            finding.sink.location.file,
            finding.sink.location.line,
            finding.sink.location.column,
            finding.sink.api_name,
        )
        if key in seen:
            continue
        seen.add(key)
        deduped.append(finding)
    return deduped


__all__ = ["extract_ruby_rails_findings"]
