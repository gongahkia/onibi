from __future__ import annotations

from dataclasses import dataclass
from enum import StrEnum

from piranesi.config import ScanConfig


class SourceType(StrEnum):
    REQUEST_BODY = "request_body"
    REQUEST_PARAM = "request_param"
    HEADER = "header"
    COOKIE = "cookie"
    ENV_VAR = "env_var"
    URL_PARAM = "url_param"
    CUSTOM = "custom"


class SinkType(StrEnum):
    SQL_QUERY = "sql_query"
    SHELL_EXEC = "shell_exec"
    EVAL = "eval"
    HTML_OUTPUT = "html_output"
    FILE_READ = "file_read"
    FILE_WRITE = "file_write"
    HTTP_REQUEST = "http_request"
    CUSTOM = "custom"


class SanitizerKind(StrEnum):
    ESCAPE = "escape"
    PARAMETERIZE = "parameterize"
    NORMALIZE = "normalize"


@dataclass(frozen=True, slots=True)
class SourceSpec:
    name: str
    pattern: str
    source_type: SourceType
    is_custom: bool = False


@dataclass(frozen=True, slots=True)
class SinkSpec:
    name: str
    pattern: str
    sink_type: SinkType
    cwe_id: str | None
    is_custom: bool = False


@dataclass(frozen=True, slots=True)
class SanitizerSpec:
    name: str
    pattern: str
    kind: SanitizerKind


_EXPRESS_ACCESS_PATTERN = 'cpg.call.name("<operator>.fieldAccess|<operator>.indexAccess").code("{code}")'
_NEW_CALL_PATTERN = 'cpg.call.name("<operator>.new").code("{code}")'

BUILTIN_SOURCE_SPECS: tuple[SourceSpec, ...] = (
    SourceSpec(
        name="express_req_body",
        pattern=_EXPRESS_ACCESS_PATTERN.format(code="req[.]body.*"),
        source_type=SourceType.REQUEST_BODY,
    ),
    SourceSpec(
        name="express_req_query",
        pattern=_EXPRESS_ACCESS_PATTERN.format(code="req[.]query.*"),
        source_type=SourceType.REQUEST_PARAM,
    ),
    SourceSpec(
        name="express_req_params",
        pattern=_EXPRESS_ACCESS_PATTERN.format(code="req[.]params.*"),
        source_type=SourceType.REQUEST_PARAM,
    ),
    SourceSpec(
        name="express_req_headers",
        pattern=_EXPRESS_ACCESS_PATTERN.format(code="req[.]headers.*"),
        source_type=SourceType.HEADER,
    ),
    SourceSpec(
        name="express_req_cookies",
        pattern=_EXPRESS_ACCESS_PATTERN.format(code="req[.]cookies.*"),
        source_type=SourceType.COOKIE,
    ),
    SourceSpec(
        name="process_env",
        pattern=_EXPRESS_ACCESS_PATTERN.format(code="process[.]env.*"),
        source_type=SourceType.ENV_VAR,
    ),
    SourceSpec(
        name="url_and_url_search_params",
        pattern=_NEW_CALL_PATTERN.format(code="new URL.*|new URLSearchParams.*"),
        source_type=SourceType.URL_PARAM,
    ),
)

BUILTIN_SINK_SPECS: tuple[SinkSpec, ...] = (
    SinkSpec(
        name="raw_sql_query",
        pattern='cpg.call.name("query|[$]queryRaw|[$]executeRaw|raw")',
        sink_type=SinkType.SQL_QUERY,
        cwe_id="CWE-89",
    ),
    SinkSpec(
        name="child_process_exec",
        pattern='cpg.call.name("exec|execSync")',
        sink_type=SinkType.SHELL_EXEC,
        cwe_id="CWE-78",
    ),
    SinkSpec(
        name="child_process_spawn",
        pattern='cpg.call.name("spawn|spawnSync")',
        sink_type=SinkType.SHELL_EXEC,
        cwe_id="CWE-78",
    ),
    SinkSpec(
        name="dynamic_eval",
        pattern='cpg.call.name("eval|Function")',
        sink_type=SinkType.EVAL,
        cwe_id="CWE-94",
    ),
    SinkSpec(
        name="dangerously_set_inner_html",
        pattern='cpg.call.name("dangerouslySetInnerHTML")',
        sink_type=SinkType.HTML_OUTPUT,
        cwe_id="CWE-79",
    ),
    SinkSpec(
        name="response_output",
        pattern='cpg.call.name("send|render|write")',
        sink_type=SinkType.HTML_OUTPUT,
        cwe_id="CWE-79",
    ),
    SinkSpec(
        name="filesystem_read",
        pattern='cpg.call.name("readFile|readFileSync")',
        sink_type=SinkType.FILE_READ,
        cwe_id="CWE-22",
    ),
    SinkSpec(
        name="filesystem_write",
        pattern='cpg.call.name("writeFile|writeFileSync")',
        sink_type=SinkType.FILE_WRITE,
        cwe_id="CWE-22",
    ),
    SinkSpec(
        name="http_request",
        pattern='cpg.call.name("fetch|get|post|request")',
        sink_type=SinkType.HTTP_REQUEST,
        cwe_id="CWE-918",
    ),
)

BUILTIN_SANITIZER_SPECS: tuple[SanitizerSpec, ...] = (
    SanitizerSpec(
        name="html_escape",
        pattern='cpg.call.name("escape|escapeHtml|sanitize|encode")',
        kind=SanitizerKind.ESCAPE,
    ),
    SanitizerSpec(
        name="parameterized_query",
        pattern='cpg.call.name("prepare|parameterize|[$]query")',
        kind=SanitizerKind.PARAMETERIZE,
    ),
    SanitizerSpec(
        name="path_normalization",
        pattern='cpg.call.name("normalize|resolve")',
        kind=SanitizerKind.NORMALIZE,
    ),
)


def get_source_specs(scan_config: ScanConfig | None = None) -> tuple[SourceSpec, ...]:
    if scan_config is None:
        return BUILTIN_SOURCE_SPECS
    return BUILTIN_SOURCE_SPECS + _custom_source_specs(scan_config)


def get_sink_specs(scan_config: ScanConfig | None = None) -> tuple[SinkSpec, ...]:
    if scan_config is None:
        return BUILTIN_SINK_SPECS
    return BUILTIN_SINK_SPECS + _custom_sink_specs(scan_config)


def get_sanitizer_specs() -> tuple[SanitizerSpec, ...]:
    return BUILTIN_SANITIZER_SPECS


def _custom_source_specs(scan_config: ScanConfig) -> tuple[SourceSpec, ...]:
    custom_type = _normalize_source_type(scan_config.custom_sources.source_type)
    return tuple(
        SourceSpec(
            name=f"custom_source_{index}",
            pattern=pattern,
            source_type=custom_type,
            is_custom=True,
        )
        for index, pattern in enumerate(scan_config.custom_sources.patterns, start=1)
    )


def _custom_sink_specs(scan_config: ScanConfig) -> tuple[SinkSpec, ...]:
    custom_type = _normalize_sink_type(scan_config.custom_sinks.sink_type)
    return tuple(
        SinkSpec(
            name=f"custom_sink_{index}",
            pattern=pattern,
            sink_type=custom_type,
            cwe_id=scan_config.custom_sinks.cwe_id,
            is_custom=True,
        )
        for index, pattern in enumerate(scan_config.custom_sinks.patterns, start=1)
    )


def _normalize_source_type(raw_value: str) -> SourceType:
    try:
        return SourceType(raw_value)
    except ValueError:
        return SourceType.CUSTOM


def _normalize_sink_type(raw_value: str) -> SinkType:
    try:
        return SinkType(raw_value)
    except ValueError:
        return SinkType.CUSTOM


__all__ = [
    "BUILTIN_SANITIZER_SPECS",
    "BUILTIN_SINK_SPECS",
    "BUILTIN_SOURCE_SPECS",
    "SanitizerKind",
    "SanitizerSpec",
    "SinkSpec",
    "SinkType",
    "SourceSpec",
    "SourceType",
    "get_sanitizer_specs",
    "get_sink_specs",
    "get_source_specs",
]
