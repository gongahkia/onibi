from __future__ import annotations

from collections.abc import Sequence
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
    NOSQL_QUERY = "nosql_query"
    LDAP_QUERY = "ldap_query"
    XPATH_QUERY = "xpath_query"
    SHELL_EXEC = "shell_exec"
    EVAL = "eval"
    EXPRESSION_INJECTION = "expression_injection"
    HTML_OUTPUT = "html_output"
    HEADER_INJECTION = "header_injection"
    FILE_READ = "file_read"
    FILE_WRITE = "file_write"
    HTTP_REQUEST = "http_request"
    DESERIALIZATION = "deserialization"
    REDIRECT = "redirect"
    FILE_UPLOAD = "file_upload"
    ORM_WRITE = "orm_write"
    AUTH_SENSITIVE = "auth_sensitive"
    REGEX_INJECTION = "regex_injection"
    PROTOTYPE_POLLUTION = "prototype_pollution"
    STATE_CHANGE_HANDLER = "state_change_handler"
    COMMAND_EXECUTION = "command_execution"
    TEMPLATE_INJECTION = "template_injection"
    CUSTOM = "custom"


class SanitizerKind(StrEnum):
    ESCAPE = "escape"
    ENCODE = "encode"
    SANITIZE = "sanitize"
    PARAMETERIZE = "parameterize"
    NORMALIZE = "normalize"
    VALIDATE = "validate"


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
    severity: str | None = None
    flow_pattern: str | None = None
    flow_to_parent_call: bool = False
    is_custom: bool = False


@dataclass(frozen=True, slots=True)
class SanitizerSpec:
    name: str
    pattern: str
    kind: SanitizerKind
    mitigates: tuple[str, ...] = ()
    confidence: float = 1.0
    blocks_flow: bool = True

    def __post_init__(self) -> None:
        if not 0.0 <= self.confidence <= 1.0:
            raise ValueError("Sanitizer confidence must be between 0.0 and 1.0")


_EXPRESS_ACCESS_PATTERN = (
    'cpg.call.name("<operator>.fieldAccess|<operator>.indexAccess").code("{code}")'
)
_FASTIFY_ROUTE_CALL_PATTERN = 'cpg.call.name("get|post|put|delete|patch|options|head")'
_NEW_CALL_PATTERN = 'cpg.call.name("<operator>.new").code("{code}")'
_SCOPED_PATTERN = '({pattern}).where(_.file.name("{file_pattern}"))'
_HTTP_REQUEST_CALL_PATTERN = (
    "cpg.call.filter(c => ("
    'c.name == "fetch" || '
    'c.code.startsWith("axios.") || '
    'c.code.startsWith("http.") || '
    'c.code.startsWith("https.") || '
    'c.code.startsWith("needle.") || '
    'c.code.startsWith("got.") || '
    'c.code.startsWith("superagent.") || '
    'c.code.startsWith("undici.") || '
    'c.code.startsWith("request(") || '
    'c.code.startsWith("request.")'
    ')).filter(c => c.code.contains("("))'
)
_HTTP_REQUEST_URL_ARGUMENT_PATTERN = f"{_HTTP_REQUEST_CALL_PATTERN}.argument(1)"
_HTTP_REQUEST_INLINE_TEMPLATE_PREDICATE = (
    'c.argument(1).code.startsWith("<operator>.formatString(\\"http://") || '
    'c.argument(1).code.startsWith("<operator>.formatString(\\"https://") || '
    'c.argument(1).code.startsWith("`http://") || '
    'c.argument(1).code.startsWith("`https://")'
)
_HTTP_REQUEST_INLINE_TEMPLATE_PATTERN = (
    f"{_HTTP_REQUEST_CALL_PATTERN}.filter(c => {_HTTP_REQUEST_INLINE_TEMPLATE_PREDICATE})"
)
_HTTP_REQUEST_NON_TEMPLATE_PATTERN = (
    f"{_HTTP_REQUEST_CALL_PATTERN}.filter(c => !({_HTTP_REQUEST_INLINE_TEMPLATE_PREDICATE}))"
)
_FASTIFY_SCHEMA_BODY_ROUTE_PATTERN = (
    f'{_FASTIFY_ROUTE_CALL_PATTERN}.filter(c => c.argument(2).code.contains("schema") && '
    'c.argument(2).code.contains("body"))'
)
_FASTIFY_SCHEMA_VALIDATED_BODY_PATTERN = (
    f"{_FASTIFY_SCHEMA_BODY_ROUTE_PATTERN}.argument(3).code.flatMap(code => "
    "cpg.methodRef.codeExact(code).referencedMethod.ast.isCall"
    '.name("<operator>.fieldAccess|<operator>.indexAccess").code("request[.]body.*"))'
)
_NEXTJS_PAGES_API_FILE_PATTERN = ".*pages/api/.*[.]js"
_NEXTJS_APP_ROUTE_FILE_PATTERN = ".*app(?:/.*)?/route[.]js"
_NEXTJS_SERVER_ACTION_FILE_PATTERN = ".*app(?:/.*)?/actions[.]js"
_NESTJS_DECORATED_TYPE_NAME_EXPR = (
    'p.inAst.isCall.name("__decorate")'
    '.argument.order(4).code.mkString("")'
    '.replace(".prototype", "")'
)
_NESTJS_DECORATED_METHOD_NAME_EXPR = (
    'p.inAst.isCall.name("__decorate").argument.order(5).code.mkString("").replace("\\"", "")'
)
_NESTJS_DECORATED_PARAMETER_INDEX_EXPR = (
    'java.lang.Integer.parseInt(p.argument(1).code.mkString("")) + 1'
)

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
        name="express_req_origin_header",
        pattern=(
            'cpg.call.name("<operator>.fieldAccess|<operator>.indexAccess")'
            '.code("req[.]headers[.]origin|req[.]headers.*origin.*")'
        ),
        source_type=SourceType.HEADER,
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

FASTIFY_SOURCE_SPECS: tuple[SourceSpec, ...] = (
    SourceSpec(
        name="fastify_request_body",
        pattern=_EXPRESS_ACCESS_PATTERN.format(code="request[.]body.*"),
        source_type=SourceType.REQUEST_BODY,
    ),
    SourceSpec(
        name="fastify_request_params",
        pattern=_EXPRESS_ACCESS_PATTERN.format(code="request[.]params.*"),
        source_type=SourceType.REQUEST_PARAM,
    ),
    SourceSpec(
        name="fastify_request_query",
        pattern=_EXPRESS_ACCESS_PATTERN.format(code="request[.]query.*"),
        source_type=SourceType.URL_PARAM,
    ),
    SourceSpec(
        name="fastify_request_headers",
        pattern=_EXPRESS_ACCESS_PATTERN.format(code="request[.]headers.*"),
        source_type=SourceType.HEADER,
    ),
)

NEXTJS_SOURCE_SPECS: tuple[SourceSpec, ...] = (
    SourceSpec(
        name="nextjs_pages_req_body",
        pattern=_SCOPED_PATTERN.format(
            pattern=_EXPRESS_ACCESS_PATTERN.format(code="req[.]body.*"),
            file_pattern=_NEXTJS_PAGES_API_FILE_PATTERN,
        ),
        source_type=SourceType.REQUEST_BODY,
    ),
    SourceSpec(
        name="nextjs_pages_req_query",
        pattern=_SCOPED_PATTERN.format(
            pattern=_EXPRESS_ACCESS_PATTERN.format(code="req[.]query.*"),
            file_pattern=_NEXTJS_PAGES_API_FILE_PATTERN,
        ),
        source_type=SourceType.URL_PARAM,
    ),
    SourceSpec(
        name="nextjs_app_request_json",
        pattern=_SCOPED_PATTERN.format(
            pattern='cpg.call.name("json").code("request[.]json[(].*")',
            file_pattern=_NEXTJS_APP_ROUTE_FILE_PATTERN,
        ),
        source_type=SourceType.REQUEST_BODY,
    ),
    SourceSpec(
        name="nextjs_app_request_text",
        pattern=_SCOPED_PATTERN.format(
            pattern='cpg.call.name("text").code("request[.]text[(].*")',
            file_pattern=_NEXTJS_APP_ROUTE_FILE_PATTERN,
        ),
        source_type=SourceType.REQUEST_BODY,
    ),
    SourceSpec(
        name="nextjs_app_request_form_data",
        pattern=_SCOPED_PATTERN.format(
            pattern='cpg.call.name("formData").code("request[.]formData[(].*")',
            file_pattern=_NEXTJS_APP_ROUTE_FILE_PATTERN,
        ),
        source_type=SourceType.REQUEST_BODY,
    ),
    SourceSpec(
        name="nextjs_app_request_headers",
        pattern=_SCOPED_PATTERN.format(
            pattern=_EXPRESS_ACCESS_PATTERN.format(code="request[.]headers.*"),
            file_pattern=_NEXTJS_APP_ROUTE_FILE_PATTERN,
        ),
        source_type=SourceType.HEADER,
    ),
    SourceSpec(
        name="nextjs_app_nexturl_search_params",
        pattern=_SCOPED_PATTERN.format(
            pattern=_EXPRESS_ACCESS_PATTERN.format(code=".*nextUrl[.]searchParams.*"),
            file_pattern=_NEXTJS_APP_ROUTE_FILE_PATTERN,
        ),
        source_type=SourceType.URL_PARAM,
    ),
    SourceSpec(
        name="nextjs_server_action_formdata_get",
        pattern=_SCOPED_PATTERN.format(
            pattern='cpg.call.name("get").code("formData[.]get[(].*")',
            file_pattern=_NEXTJS_SERVER_ACTION_FILE_PATTERN,
        ),
        source_type=SourceType.REQUEST_BODY,
    ),
)


def _nestjs_decorated_parameter_pattern(decorator_name: str) -> str:
    # NestJS decorators are lowered by tsc into __decorate/__param helper calls.
    # Resolve those helper calls back to the original decorated parameter node so
    # data flow starts at the actual controller argument.
    return (
        f'cpg.call.name("__param").filter(p => p.code.contains("{decorator_name}(")).flatMap(p => '
        f"cpg.typeDecl.nameExact({_NESTJS_DECORATED_TYPE_NAME_EXPR}).method.nameExact({_NESTJS_DECORATED_METHOD_NAME_EXPR}).parameter.index("
        f"{_NESTJS_DECORATED_PARAMETER_INDEX_EXPR}))"
    )


NESTJS_SOURCE_SPECS: tuple[SourceSpec, ...] = (
    SourceSpec(
        name="nestjs_body",
        pattern=_nestjs_decorated_parameter_pattern("Body"),
        source_type=SourceType.REQUEST_BODY,
    ),
    SourceSpec(
        name="nestjs_param",
        pattern=_nestjs_decorated_parameter_pattern("Param"),
        source_type=SourceType.REQUEST_PARAM,
    ),
    SourceSpec(
        name="nestjs_query",
        pattern=_nestjs_decorated_parameter_pattern("Query"),
        source_type=SourceType.URL_PARAM,
    ),
    SourceSpec(
        name="nestjs_headers",
        pattern=_nestjs_decorated_parameter_pattern("Headers"),
        source_type=SourceType.HEADER,
    ),
    SourceSpec(
        name="nestjs_req",
        pattern=_nestjs_decorated_parameter_pattern("Req"),
        source_type=SourceType.REQUEST_BODY,
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
        name="mongodb_collection_find",
        pattern=(
            'cpg.call.name("find|findOne|findById|aggregate|updateOne|updateMany|deleteOne|deleteMany")'
            '.code(".*(?:mongo|mongoose|collection|Model|db)[.].*")'
        ),
        sink_type=SinkType.NOSQL_QUERY,
        cwe_id="CWE-943",
        severity="high",
        flow_pattern=(
            '(cpg.call.name("find|findOne|findById|aggregate|updateOne|updateMany|deleteOne|deleteMany")'
            '.code(".*(?:mongo|mongoose|collection|Model|db)[.].*")).argument(1)'
        ),
        flow_to_parent_call=True,
    ),
    SinkSpec(
        name="mongodb_where_operator",
        pattern=(
            'cpg.call.name("find|findOne|findById|aggregate|updateOne|updateMany|deleteOne|deleteMany|count|countDocuments")'
            '.code(".*[\\"\\\']\\\\$where[\\"\\\'].*")'
        ),
        sink_type=SinkType.NOSQL_QUERY,
        cwe_id="CWE-943",
        severity="critical",
        flow_pattern=(
            'cpg.call.name("find|findOne|findById|aggregate|updateOne|updateMany|deleteOne|deleteMany|count|countDocuments")'
            '.code(".*[\\"\\\']\\\\$where[\\"\\\'].*").argument(1)'
        ),
        flow_to_parent_call=True,
    ),
    SinkSpec(
        name="mongodb_where_chain",
        pattern=(
            'cpg.call.name("[$]where|where").code(".*[.][$]?where[(].*")'
        ),
        sink_type=SinkType.NOSQL_QUERY,
        cwe_id="CWE-943",
        severity="critical",
        flow_pattern='cpg.call.name("[$]where|where").code(".*[.][$]?where[(].*").argument(1)',
        flow_to_parent_call=True,
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
        name="cors_allow_origin_reflection",
        pattern=(
            'cpg.call.name("setHeader")'
            '.code(".*setHeader[(][\\"\\\']Access-Control-Allow-Origin[\\"\\\'].*,.*")'
        ),
        sink_type=SinkType.HEADER_INJECTION,
        cwe_id="CWE-942",
        severity="high",
        flow_pattern=(
            'cpg.call.name("setHeader")'
            '.code(".*setHeader[(][\\"\\\']Access-Control-Allow-Origin[\\"\\\'].*,.*")'
            ".argument(2)"
        ),
        flow_to_parent_call=True,
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
        name="ssrf_full_url",
        pattern=_HTTP_REQUEST_NON_TEMPLATE_PATTERN,
        sink_type=SinkType.HTTP_REQUEST,
        cwe_id="CWE-918",
        severity="high",
        flow_pattern=_HTTP_REQUEST_URL_ARGUMENT_PATTERN,
        flow_to_parent_call=True,
    ),
    SinkSpec(
        name="json_parse_user_input",
        pattern='cpg.call.name("parse").code("JSON[.]parse[(].*")',
        sink_type=SinkType.DESERIALIZATION,
        cwe_id="CWE-502",
        severity="medium",
    ),
    SinkSpec(
        name="express_redirect",
        pattern='cpg.call.name("redirect")',
        sink_type=SinkType.REDIRECT,
        cwe_id="CWE-601",
        severity="medium",
    ),
    SinkSpec(
        name="location_header_set",
        pattern=(
            'cpg.call.name("setHeader|writeHead")'
            '.code(".*(?:setHeader|writeHead)[(].*[Ll]ocation.*")'
        ),
        sink_type=SinkType.REDIRECT,
        cwe_id="CWE-601",
        severity="medium",
    ),
    SinkSpec(
        name="koa_ctx_redirect",
        pattern='cpg.call.name("redirect").code(".*ctx[.]redirect[(].*")',
        sink_type=SinkType.REDIRECT,
        cwe_id="CWE-601",
        severity="medium",
    ),
    SinkSpec(
        name="nestjs_res_redirect",
        pattern='cpg.call.name("redirect").code(".*(?:res|response)[.]redirect[(].*")',
        sink_type=SinkType.REDIRECT,
        cwe_id="CWE-601",
        severity="medium",
    ),
    SinkSpec(
        name="fastify_reply_redirect",
        pattern='cpg.call.name("redirect").code(".*reply[.]redirect[(].*")',
        sink_type=SinkType.REDIRECT,
        cwe_id="CWE-601",
        severity="medium",
    ),
    SinkSpec(
        name="multer_file_write",
        pattern='cpg.call.name("writeFile|writeFileSync").where(_.argument.code(".*(?:originalname|filename).*"))',
        sink_type=SinkType.FILE_UPLOAD,
        cwe_id="CWE-434",
        severity="high",
    ),
    SinkSpec(
        name="multer_file_path_concat",
        pattern=(
            'cpg.call.name("<operator>.addition|<operator>.formatString")'
            '.where(_.argument.code(".*(?:originalname|filename).*"))'
        ),
        sink_type=SinkType.FILE_UPLOAD,
        cwe_id="CWE-434",
        severity="high",
    ),
    SinkSpec(
        name="ssrf_path_segment",
        pattern=_HTTP_REQUEST_INLINE_TEMPLATE_PATTERN,
        sink_type=SinkType.HTTP_REQUEST,
        cwe_id="CWE-918",
        severity="medium",
        flow_pattern=_HTTP_REQUEST_URL_ARGUMENT_PATTERN,
        flow_to_parent_call=True,
    ),
    SinkSpec(
        name="prototype_pollution_object_assign",
        pattern='cpg.call.name("assign").code(".*Object[.]assign[(].*")',
        sink_type=SinkType.PROTOTYPE_POLLUTION,
        cwe_id="CWE-1321",
        severity="high",
    ),
    SinkSpec(
        name="prototype_pollution_lodash_merge",
        pattern='cpg.call.name("merge").code(".*(?:_|lodash)[.]merge[(].*")',
        sink_type=SinkType.PROTOTYPE_POLLUTION,
        cwe_id="CWE-1321",
        severity="high",
    ),
    SinkSpec(
        name="prototype_pollution_defaults_deep",
        pattern='cpg.call.name("defaultsDeep").code(".*(?:_|lodash)[.]defaultsDeep[(].*")',
        sink_type=SinkType.PROTOTYPE_POLLUTION,
        cwe_id="CWE-1321",
        severity="high",
    ),
    SinkSpec(
        name="prototype_pollution_custom_merge",
        pattern='cpg.call.name("merge|deepMerge|defaultsDeep")',
        sink_type=SinkType.PROTOTYPE_POLLUTION,
        cwe_id="CWE-1321",
        severity="high",
    ),
)

FASTIFY_SINK_SPECS: tuple[SinkSpec, ...] = (
    SinkSpec(
        name="fastify_reply_send",
        pattern='cpg.call.name("send").code(".*reply[.]send[(].*")',
        sink_type=SinkType.HTML_OUTPUT,
        cwe_id="CWE-79",
    ),
    SinkSpec(
        name="fastify_reply_header",
        pattern='cpg.call.name("header").code(".*reply[.]header[(][\\"\\\']Location[\\"\\\'].*,.*")',
        sink_type=SinkType.HEADER_INJECTION,
        cwe_id="CWE-113",
        severity="medium",
        flow_pattern='cpg.call.name("header").code(".*reply[.]header[(][\\"\\\']Location[\\"\\\'].*,.*").argument(2)',
        flow_to_parent_call=True,
    ),
)

BUILTIN_SANITIZER_SPECS: tuple[SanitizerSpec, ...] = (
    SanitizerSpec(
        name="html_escape",
        pattern='cpg.call.name("escape|escapeHtml|sanitize|encode")',
        kind=SanitizerKind.ESCAPE,
        mitigates=("CWE-79",),
        confidence=0.5,
    ),
    SanitizerSpec(
        name="parameterized_query",
        pattern='cpg.call.name("prepare|parameterize|[$]query")',
        kind=SanitizerKind.PARAMETERIZE,
        mitigates=("CWE-89",),
        confidence=0.9,
    ),
    SanitizerSpec(
        name="path_normalization",
        pattern='cpg.call.name("normalize|resolve")',
        kind=SanitizerKind.NORMALIZE,
        mitigates=("CWE-22",),
        confidence=0.4,
    ),
    SanitizerSpec(
        name="validator_escape",
        pattern='cpg.call.name("escape").code(".*validator[.]escape[(].*")',
        kind=SanitizerKind.ESCAPE,
        mitigates=("CWE-79",),
        confidence=0.7,
    ),
    SanitizerSpec(
        name="sanitize_html",
        pattern='cpg.call.name("sanitizeHtml")',
        kind=SanitizerKind.ESCAPE,
        mitigates=("CWE-79",),
        confidence=0.9,
    ),
    SanitizerSpec(
        name="dompurify_sanitize",
        pattern='cpg.call.name("sanitize").code(".*DOMPurify[.]sanitize[(].*")',
        kind=SanitizerKind.ESCAPE,
        mitigates=("CWE-79",),
        confidence=0.95,
    ),
    SanitizerSpec(
        name="sqlstring_escape",
        pattern='cpg.call.name("escape").code(".*(?:sqlstring|SqlString)[.]escape[(].*")',
        kind=SanitizerKind.ESCAPE,
        mitigates=("CWE-89",),
        confidence=0.75,
    ),
    SanitizerSpec(
        name="pg_parameterized_query",
        pattern='cpg.call.name("query").code(".*[$][0-9]+.*")',
        kind=SanitizerKind.PARAMETERIZE,
        mitigates=("CWE-89",),
        confidence=0.95,
    ),
    SanitizerSpec(
        name="path_resolve_startswith",
        pattern='cpg.call.name("resolve|startsWith").code(".*(?:path[.]resolve|[.]startsWith)[(].*")',
        kind=SanitizerKind.NORMALIZE,
        mitigates=("CWE-22",),
        confidence=0.9,
    ),
    SanitizerSpec(
        name="numeric_coercion",
        pattern='cpg.call.name("parseInt|Number")',
        kind=SanitizerKind.NORMALIZE,
        mitigates=("CWE-89",),
        confidence=0.6,
    ),
    SanitizerSpec(
        name="uri_component_encoding",
        pattern='cpg.call.name("encodeURIComponent")',
        kind=SanitizerKind.ESCAPE,
        mitigates=("CWE-79", "CWE-22"),
        confidence=0.55,
    ),
    SanitizerSpec(
        name="json_schema_validate",
        pattern='cpg.call.name("validate|ajv|Joi|yup|zod").code(".*(?:validate|parse|safeParse)[(].*")',
        kind=SanitizerKind.NORMALIZE,
        mitigates=("CWE-502",),
        confidence=0.9,
    ),
    SanitizerSpec(
        name="url_origin_check",
        pattern='cpg.call.name("startsWith|origin|hostname").code(".*(?:startsWith|origin|hostname).*")',
        kind=SanitizerKind.NORMALIZE,
        mitigates=("CWE-601",),
        confidence=0.7,
    ),
    SanitizerSpec(
        name="file_extension_check",
        pattern='cpg.call.name("endsWith|extname|mimetype").code(".*(?:endsWith|extname|mimetype).*")',
        kind=SanitizerKind.NORMALIZE,
        mitigates=("CWE-434",),
        confidence=0.8,
    ),
    SanitizerSpec(
        name="multer_file_filter",
        pattern='cpg.call.code(".*fileFilter.*")',
        kind=SanitizerKind.NORMALIZE,
        mitigates=("CWE-434",),
        confidence=0.85,
    ),
)

FASTIFY_SANITIZER_SPECS: tuple[SanitizerSpec, ...] = (
    SanitizerSpec(
        name="fastify_schema_validation",
        pattern=_FASTIFY_SCHEMA_VALIDATED_BODY_PATTERN,
        kind=SanitizerKind.NORMALIZE,
        confidence=0.25,
        blocks_flow=False,
    ),
)

# --- Java Spring Boot specs ---

_JAVA_ANNOTATION_PARAM_PATTERN = 'cpg.parameter.annotation.name("{annotation}").parameter'
_JAVA_METHOD_FULL_NAME_PATTERN = 'cpg.call.methodFullName(".*{class_pattern}[.]({method}).*")'
_JAVA_JDBC_TEMPLATE_CALL_PATTERN = _JAVA_METHOD_FULL_NAME_PATTERN.format(
    class_pattern="JdbcTemplate",
    method="query|queryForObject|queryForList|update",
)
_JAVA_STRING_CONCAT_ARGUMENT_PATTERN = (
    'c.argument(1).ast.isCall.name("<operator>.addition").nonEmpty'
)
_JAVA_NATIVE_QUERY_TRUE_PREDICATE = (
    '(a.code.contains("nativeQuery = true") || a.code.contains("nativeQuery=true"))'
)

SPRINGBOOT_SOURCE_SPECS: tuple[SourceSpec, ...] = (
    SourceSpec(
        name="spring_request_body",
        pattern=_JAVA_ANNOTATION_PARAM_PATTERN.format(annotation="RequestBody"),
        source_type=SourceType.REQUEST_BODY,
    ),
    SourceSpec(
        name="spring_request_param",
        pattern=_JAVA_ANNOTATION_PARAM_PATTERN.format(annotation="RequestParam"),
        source_type=SourceType.URL_PARAM,
    ),
    SourceSpec(
        name="spring_path_variable",
        pattern=_JAVA_ANNOTATION_PARAM_PATTERN.format(annotation="PathVariable"),
        source_type=SourceType.REQUEST_PARAM,
    ),
    SourceSpec(
        name="spring_request_header",
        pattern=_JAVA_ANNOTATION_PARAM_PATTERN.format(annotation="RequestHeader"),
        source_type=SourceType.HEADER,
    ),
    SourceSpec(
        name="spring_cookie_value",
        pattern=_JAVA_ANNOTATION_PARAM_PATTERN.format(annotation="CookieValue"),
        source_type=SourceType.COOKIE,
    ),
    SourceSpec(
        name="spring_servlet_get_parameter",
        pattern=_JAVA_METHOD_FULL_NAME_PATTERN.format(
            class_pattern="HttpServletRequest",
            method="getParameter",
        ),
        source_type=SourceType.URL_PARAM,
    ),
)

SPRINGBOOT_SINK_SPECS: tuple[SinkSpec, ...] = (
    SinkSpec(
        name="spring_jdbc_query",
        pattern=(
            f"{_JAVA_JDBC_TEMPLATE_CALL_PATTERN}.filter(c => "
            f"{_JAVA_STRING_CONCAT_ARGUMENT_PATTERN})"
        ),
        sink_type=SinkType.SQL_QUERY,
        cwe_id="CWE-89",
        severity="high",
        flow_pattern=(
            f"{_JAVA_JDBC_TEMPLATE_CALL_PATTERN}.filter(c => "
            f"{_JAVA_STRING_CONCAT_ARGUMENT_PATTERN}).argument(1)"
        ),
        flow_to_parent_call=True,
    ),
    SinkSpec(
        name="spring_jpa_native_query_concat",
        pattern=(
            'cpg.method.annotation.name("Query").filter(a => '
            f'{_JAVA_NATIVE_QUERY_TRUE_PREDICATE} && a.code.contains("+"))'
        ),
        sink_type=SinkType.SQL_QUERY,
        cwe_id="CWE-89",
        severity="high",
    ),
    SinkSpec(
        name="java_runtime_exec",
        pattern=_JAVA_METHOD_FULL_NAME_PATTERN.format(
            class_pattern="Runtime",
            method="exec",
        ),
        sink_type=SinkType.SHELL_EXEC,
        cwe_id="CWE-78",
        severity="high",
    ),
    SinkSpec(
        name="java_process_builder",
        pattern='cpg.call.name("<operator>.new").code("new ProcessBuilder.*")',
        sink_type=SinkType.SHELL_EXEC,
        cwe_id="CWE-78",
        severity="high",
    ),
    SinkSpec(
        name="java_new_file",
        pattern='cpg.call.name("<operator>.new").code("new File.*")',
        sink_type=SinkType.FILE_READ,
        cwe_id="CWE-22",
        severity="medium",
    ),
    SinkSpec(
        name="spring_rest_template",
        pattern=_JAVA_METHOD_FULL_NAME_PATTERN.format(
            class_pattern="RestTemplate",
            method="getForObject|getForEntity|postForObject|postForEntity|exchange",
        ),
        sink_type=SinkType.HTTP_REQUEST,
        cwe_id="CWE-918",
        severity="high",
    ),
    SinkSpec(
        name="spring_response_writer",
        pattern=_JAVA_METHOD_FULL_NAME_PATTERN.format(
            class_pattern="PrintWriter|ServletOutputStream",
            method="write|print|println",
        ),
        sink_type=SinkType.HTML_OUTPUT,
        cwe_id="CWE-79",
        severity="medium",
    ),
    SinkSpec(
        name="java_object_input_stream",
        pattern=_JAVA_METHOD_FULL_NAME_PATTERN.format(
            class_pattern="ObjectInputStream",
            method="readObject|readUnshared",
        ),
        sink_type=SinkType.DESERIALIZATION,
        cwe_id="CWE-502",
        severity="critical",
    ),
    SinkSpec(
        name="java_xml_decoder",
        pattern=_JAVA_METHOD_FULL_NAME_PATTERN.format(
            class_pattern="XMLDecoder",
            method="readObject",
        ),
        sink_type=SinkType.DESERIALIZATION,
        cwe_id="CWE-502",
        severity="critical",
    ),
    SinkSpec(
        name="java_yaml_load",
        pattern='cpg.call.name("load").code(".*(?:Yaml|SnakeYaml)[.]load[(].*")',
        sink_type=SinkType.DESERIALIZATION,
        cwe_id="CWE-502",
        severity="high",
    ),
    SinkSpec(
        name="spring_redirect",
        pattern='cpg.call.code(".*redirect:.*")',
        sink_type=SinkType.REDIRECT,
        cwe_id="CWE-601",
        severity="medium",
    ),
    SinkSpec(
        name="java_send_redirect",
        pattern=_JAVA_METHOD_FULL_NAME_PATTERN.format(
            class_pattern="HttpServletResponse",
            method="sendRedirect",
        ),
        sink_type=SinkType.REDIRECT,
        cwe_id="CWE-601",
        severity="medium",
    ),
)

SPRINGBOOT_SANITIZER_SPECS: tuple[SanitizerSpec, ...] = (
    SanitizerSpec(
        name="spring_security_context",
        pattern='cpg.dependency.name(".*spring-boot-starter-security.*")',
        kind=SanitizerKind.ESCAPE,
        mitigates=("CWE-79", "CWE-352"),
        confidence=0.3,
        blocks_flow=False,
    ),
    SanitizerSpec(
        name="spring_valid_annotation",
        pattern='cpg.method.parameter.where(_.annotation.name("Valid|Validated"))',
        kind=SanitizerKind.NORMALIZE,
        mitigates=("CWE-89", "CWE-79", "CWE-78"),
        confidence=0.4,
        blocks_flow=False,
    ),
    SanitizerSpec(
        name="spring_pre_authorize_access_control",
        pattern='cpg.method.annotation.name("PreAuthorize")',
        kind=SanitizerKind.NORMALIZE,
        confidence=0.0,
        blocks_flow=False,
    ),
    SanitizerSpec(
        name="spring_secured_access_control",
        pattern='cpg.method.annotation.name("Secured")',
        kind=SanitizerKind.NORMALIZE,
        confidence=0.0,
        blocks_flow=False,
    ),
)

# --- Python framework specs ---

_PY_FIELD_ACCESS_PATTERN = (
    'cpg.call.name("<operator>.fieldAccess|<operator>.indexAccess").code("{code}")'
)
_PY_CALL_PATTERN = 'cpg.call.name("{name}")'
_PY_CALL_CODE_PATTERN = 'cpg.call.name("{name}").code("{code}")'

FLASK_SOURCE_SPECS: tuple[SourceSpec, ...] = (
    SourceSpec(
        name="flask_request_form",
        pattern=_PY_FIELD_ACCESS_PATTERN.format(code="request[.]form.*"),
        source_type=SourceType.REQUEST_BODY,
    ),
    SourceSpec(
        name="flask_request_args",
        pattern=_PY_FIELD_ACCESS_PATTERN.format(code="request[.]args.*"),
        source_type=SourceType.URL_PARAM,
    ),
    SourceSpec(
        name="flask_request_json",
        pattern=_PY_FIELD_ACCESS_PATTERN.format(code="request[.]json.*"),
        source_type=SourceType.REQUEST_BODY,
    ),
    SourceSpec(
        name="flask_request_headers",
        pattern=_PY_FIELD_ACCESS_PATTERN.format(code="request[.]headers.*"),
        source_type=SourceType.HEADER,
    ),
    SourceSpec(
        name="flask_request_data",
        pattern=_PY_FIELD_ACCESS_PATTERN.format(code="request[.]data"),
        source_type=SourceType.REQUEST_BODY,
    ),
    SourceSpec(
        name="flask_request_cookies",
        pattern=_PY_FIELD_ACCESS_PATTERN.format(code="request[.]cookies.*"),
        source_type=SourceType.COOKIE,
    ),
)

DJANGO_SOURCE_SPECS: tuple[SourceSpec, ...] = (
    SourceSpec(
        name="django_request_post",
        pattern=_PY_FIELD_ACCESS_PATTERN.format(code="request[.]POST.*"),
        source_type=SourceType.REQUEST_BODY,
    ),
    SourceSpec(
        name="django_request_get",
        pattern=_PY_FIELD_ACCESS_PATTERN.format(code="request[.]GET.*"),
        source_type=SourceType.URL_PARAM,
    ),
    SourceSpec(
        name="django_request_body",
        pattern=_PY_FIELD_ACCESS_PATTERN.format(code="request[.]body"),
        source_type=SourceType.REQUEST_BODY,
    ),
    SourceSpec(
        name="django_request_headers",
        pattern=_PY_FIELD_ACCESS_PATTERN.format(code="request[.]headers.*"),
        source_type=SourceType.HEADER,
    ),
    SourceSpec(
        name="django_request_cookies",
        pattern=_PY_FIELD_ACCESS_PATTERN.format(code="request[.]COOKIES.*"),
        source_type=SourceType.COOKIE,
    ),
)

FASTAPI_SOURCE_SPECS: tuple[SourceSpec, ...] = (
    SourceSpec(
        name="fastapi_body",
        pattern=_PY_CALL_PATTERN.format(name="Body"),
        source_type=SourceType.REQUEST_BODY,
    ),
    SourceSpec(
        name="fastapi_query",
        pattern=_PY_CALL_PATTERN.format(name="Query"),
        source_type=SourceType.URL_PARAM,
    ),
    SourceSpec(
        name="fastapi_path",
        pattern=_PY_CALL_PATTERN.format(name="Path"),
        source_type=SourceType.REQUEST_PARAM,
    ),
    SourceSpec(
        name="fastapi_header",
        pattern=_PY_CALL_PATTERN.format(name="Header"),
        source_type=SourceType.HEADER,
    ),
    SourceSpec(
        name="fastapi_cookie",
        pattern=_PY_CALL_PATTERN.format(name="Cookie"),
        source_type=SourceType.COOKIE,
    ),
)

PYTHON_SINK_SPECS: tuple[SinkSpec, ...] = (
    SinkSpec(
        name="python_sql_execute",
        pattern=_PY_CALL_CODE_PATTERN.format(name="execute", code=".*cursor[.]execute[(].*"),
        sink_type=SinkType.SQL_QUERY,
        cwe_id="CWE-89",
    ),
    SinkSpec(
        name="python_os_system",
        pattern=_PY_CALL_PATTERN.format(name="system"),
        sink_type=SinkType.SHELL_EXEC,
        cwe_id="CWE-78",
    ),
    SinkSpec(
        name="python_subprocess_run",
        pattern=_PY_CALL_CODE_PATTERN.format(name="run", code=".*subprocess[.]run[(].*"),
        sink_type=SinkType.SHELL_EXEC,
        cwe_id="CWE-78",
    ),
    SinkSpec(
        name="python_subprocess_popen",
        pattern=_PY_CALL_CODE_PATTERN.format(name="Popen", code=".*subprocess[.]Popen[(].*"),
        sink_type=SinkType.SHELL_EXEC,
        cwe_id="CWE-78",
    ),
    SinkSpec(
        name="python_eval",
        pattern=_PY_CALL_PATTERN.format(name="eval"),
        sink_type=SinkType.EVAL,
        cwe_id="CWE-94",
    ),
    SinkSpec(
        name="python_open",
        pattern=_PY_CALL_PATTERN.format(name="open"),
        sink_type=SinkType.FILE_READ,
        cwe_id="CWE-22",
    ),
    SinkSpec(
        name="python_render_template_string",
        pattern=_PY_CALL_PATTERN.format(name="render_template_string"),
        sink_type=SinkType.HTML_OUTPUT,
        cwe_id="CWE-79",
    ),
    SinkSpec(
        name="python_markup",
        pattern=_PY_CALL_PATTERN.format(name="Markup"),
        sink_type=SinkType.HTML_OUTPUT,
        cwe_id="CWE-79",
    ),
    SinkSpec(
        name="python_requests_get",
        pattern=_PY_CALL_CODE_PATTERN.format(name="get", code=".*requests[.]get[(].*"),
        sink_type=SinkType.HTTP_REQUEST,
        cwe_id="CWE-918",
    ),
    SinkSpec(
        name="python_requests_post",
        pattern=_PY_CALL_CODE_PATTERN.format(name="post", code=".*requests[.]post[(].*"),
        sink_type=SinkType.HTTP_REQUEST,
        cwe_id="CWE-918",
    ),
    SinkSpec(
        name="python_pickle_loads",
        pattern=_PY_CALL_CODE_PATTERN.format(name="loads", code=".*pickle[.]loads[(].*"),
        sink_type=SinkType.DESERIALIZATION,
        cwe_id="CWE-502",
        severity="critical",
    ),
    SinkSpec(
        name="python_pickle_load",
        pattern=_PY_CALL_CODE_PATTERN.format(name="load", code=".*pickle[.]load[(].*"),
        sink_type=SinkType.DESERIALIZATION,
        cwe_id="CWE-502",
        severity="critical",
    ),
    SinkSpec(
        name="python_yaml_load_unsafe",
        pattern=_PY_CALL_CODE_PATTERN.format(name="load", code=".*yaml[.]load[(].*"),
        sink_type=SinkType.DESERIALIZATION,
        cwe_id="CWE-502",
        severity="high",
    ),
    SinkSpec(
        name="python_marshal_loads",
        pattern=_PY_CALL_CODE_PATTERN.format(name="loads", code=".*marshal[.]loads[(].*"),
        sink_type=SinkType.DESERIALIZATION,
        cwe_id="CWE-502",
        severity="critical",
    ),
    SinkSpec(
        name="python_redirect",
        pattern=_PY_CALL_PATTERN.format(name="redirect"),
        sink_type=SinkType.REDIRECT,
        cwe_id="CWE-601",
        severity="medium",
    ),
    SinkSpec(
        name="python_flask_make_response_location",
        pattern=_PY_CALL_CODE_PATTERN.format(
            name="__setitem__",
            code=".*headers\\[.*[Ll]ocation.*\\].*",
        ),
        sink_type=SinkType.REDIRECT,
        cwe_id="CWE-601",
        severity="medium",
    ),
    SinkSpec(
        name="python_django_http_response_redirect",
        pattern=_PY_CALL_PATTERN.format(name="HttpResponseRedirect"),
        sink_type=SinkType.REDIRECT,
        cwe_id="CWE-601",
        severity="medium",
    ),
    SinkSpec(
        name="python_django_permanent_redirect",
        pattern=_PY_CALL_PATTERN.format(name="HttpResponsePermanentRedirect"),
        sink_type=SinkType.REDIRECT,
        cwe_id="CWE-601",
        severity="medium",
    ),
    SinkSpec(
        name="python_pymongo_where_operator",
        pattern=_PY_CALL_CODE_PATTERN.format(
            name="find|find_one|count|count_documents|update_one|update_many|delete_one|delete_many|aggregate",
            code=".*[\\\"']\\$where[\\\"'].*",
        ),
        sink_type=SinkType.NOSQL_QUERY,
        cwe_id="CWE-943",
        severity="critical",
    ),
)

PYTHON_SANITIZER_SPECS: tuple[SanitizerSpec, ...] = (
    SanitizerSpec(
        name="python_parameterized_query",
        pattern=_PY_CALL_CODE_PATTERN.format(
            name="execute",
            code=".*cursor[.]execute[(].*,.*[(\\[].*[)\\]].*",
        ),
        kind=SanitizerKind.PARAMETERIZE,
        mitigates=("CWE-89",),
        confidence=0.95,
    ),
    SanitizerSpec(
        name="python_shlex_quote",
        pattern=_PY_CALL_CODE_PATTERN.format(name="quote", code=".*shlex[.]quote[(].*"),
        kind=SanitizerKind.ESCAPE,
        mitigates=("CWE-78",),
        confidence=0.9,
    ),
    SanitizerSpec(
        name="python_markupsafe_escape",
        pattern=_PY_CALL_CODE_PATTERN.format(name="escape", code=".*markupsafe[.]escape[(].*"),
        kind=SanitizerKind.ESCAPE,
        mitigates=("CWE-79",),
        confidence=0.9,
    ),
    SanitizerSpec(
        name="python_bleach_clean",
        pattern=_PY_CALL_CODE_PATTERN.format(name="clean", code=".*bleach[.]clean[(].*"),
        kind=SanitizerKind.ESCAPE,
        mitigates=("CWE-79",),
        confidence=0.85,
    ),
    SanitizerSpec(
        name="python_path_realpath_startswith",
        pattern='cpg.call.name("startswith|realpath").code(".*(?:os[.]path[.]realpath|[.]startswith)[(].*")',
        kind=SanitizerKind.NORMALIZE,
        mitigates=("CWE-22",),
        confidence=0.85,
    ),
    SanitizerSpec(
        name="python_yaml_safe_load",
        pattern=_PY_CALL_CODE_PATTERN.format(name="safe_load", code=".*yaml[.]safe_load[(].*"),
        kind=SanitizerKind.NORMALIZE,
        mitigates=("CWE-502",),
        confidence=0.95,
    ),
    SanitizerSpec(
        name="python_json_loads_schema",
        pattern=_PY_CALL_CODE_PATTERN.format(
            name="validate", code=".*(?:jsonschema[.]validate|pydantic)[(].*"
        ),
        kind=SanitizerKind.NORMALIZE,
        mitigates=("CWE-502",),
        confidence=0.9,
    ),
    SanitizerSpec(
        name="python_url_startswith_check",
        pattern=_PY_CALL_CODE_PATTERN.format(
            name="startswith", code=".*[.]startswith[(].*['\"/].*"
        ),
        kind=SanitizerKind.NORMALIZE,
        mitigates=("CWE-601",),
        confidence=0.7,
    ),
)

# --- Go framework specs ---

_GO_CALL_PATTERN = 'cpg.call.name("{method}")'
_GO_CALL_CODE_PATTERN = 'cpg.call.name("{method}").code("{code}")'
_GO_RECEIVER_CALL_PATTERN = 'cpg.call.code("{code}")'

GIN_SOURCE_SPECS: tuple[SourceSpec, ...] = (
    SourceSpec(
        name="gin_query",
        pattern=_GO_CALL_CODE_PATTERN.format(method="Query", code="c[.]Query[(].*"),
        source_type=SourceType.URL_PARAM,
    ),
    SourceSpec(
        name="gin_post_form",
        pattern=_GO_CALL_CODE_PATTERN.format(method="PostForm", code="c[.]PostForm[(].*"),
        source_type=SourceType.REQUEST_BODY,
    ),
    SourceSpec(
        name="gin_param",
        pattern=_GO_CALL_CODE_PATTERN.format(method="Param", code="c[.]Param[(].*"),
        source_type=SourceType.REQUEST_PARAM,
    ),
    SourceSpec(
        name="gin_get_header",
        pattern=_GO_CALL_CODE_PATTERN.format(method="GetHeader", code="c[.]GetHeader[(].*"),
        source_type=SourceType.HEADER,
    ),
    SourceSpec(
        name="gin_bind_json",
        pattern=_GO_CALL_CODE_PATTERN.format(
            method="BindJSON|ShouldBindJSON",
            code="c[.](?:Bind|ShouldBind)JSON[(].*",
        ),
        source_type=SourceType.REQUEST_BODY,
    ),
)

ECHO_SOURCE_SPECS: tuple[SourceSpec, ...] = (
    SourceSpec(
        name="echo_query_param",
        pattern=_GO_CALL_CODE_PATTERN.format(method="QueryParam", code="c[.]QueryParam[(].*"),
        source_type=SourceType.URL_PARAM,
    ),
    SourceSpec(
        name="echo_form_value",
        pattern=_GO_CALL_CODE_PATTERN.format(method="FormValue", code="c[.]FormValue[(].*"),
        source_type=SourceType.REQUEST_BODY,
    ),
    SourceSpec(
        name="echo_param",
        pattern=_GO_CALL_CODE_PATTERN.format(method="Param", code="c[.]Param[(].*"),
        source_type=SourceType.REQUEST_PARAM,
    ),
)

CHI_SOURCE_SPECS: tuple[SourceSpec, ...] = (
    SourceSpec(
        name="chi_url_param",
        pattern=_GO_CALL_CODE_PATTERN.format(method="URLParam", code="chi[.]URLParam[(].*"),
        source_type=SourceType.REQUEST_PARAM,
    ),
)

GO_STDLIB_SOURCE_SPECS: tuple[SourceSpec, ...] = (
    SourceSpec(
        name="go_url_query_get",
        pattern=_GO_RECEIVER_CALL_PATTERN.format(code="r[.]URL[.]Query[(][)][.]Get[(].*"),
        source_type=SourceType.URL_PARAM,
    ),
    SourceSpec(
        name="go_form_value",
        pattern=_GO_CALL_CODE_PATTERN.format(method="FormValue", code="r[.]FormValue[(].*"),
        source_type=SourceType.REQUEST_BODY,
    ),
    SourceSpec(
        name="go_header_get",
        pattern=_GO_RECEIVER_CALL_PATTERN.format(code="r[.]Header[.]Get[(].*"),
        source_type=SourceType.HEADER,
    ),
)

GO_SINK_SPECS: tuple[SinkSpec, ...] = (
    SinkSpec(
        name="go_sql_query_sprintf",
        pattern='cpg.call.name("Query|QueryRow|QueryContext").where(_.argument.isCall.name("Sprintf"))',
        sink_type=SinkType.SQL_QUERY,
        cwe_id="CWE-89",
    ),
    SinkSpec(
        name="go_sql_exec_sprintf",
        pattern='cpg.call.name("Exec|ExecContext").where(_.argument.isCall.name("Sprintf"))',
        sink_type=SinkType.SQL_QUERY,
        cwe_id="CWE-89",
    ),
    SinkSpec(
        name="go_exec_command",
        pattern=_GO_CALL_CODE_PATTERN.format(method="Command", code="exec[.]Command[(].*"),
        sink_type=SinkType.SHELL_EXEC,
        cwe_id="CWE-78",
    ),
    SinkSpec(
        name="go_exec_command_context",
        pattern=_GO_CALL_CODE_PATTERN.format(
            method="CommandContext",
            code="exec[.]CommandContext[(].*",
        ),
        sink_type=SinkType.SHELL_EXEC,
        cwe_id="CWE-78",
    ),
    SinkSpec(
        name="go_template_html",
        pattern=_GO_CALL_CODE_PATTERN.format(method="HTML", code="template[.]HTML[(].*"),
        sink_type=SinkType.HTML_OUTPUT,
        cwe_id="CWE-79",
    ),
    SinkSpec(
        name="go_os_open",
        pattern=_GO_CALL_CODE_PATTERN.format(
            method="Open|OpenFile", code="os[.]Open(?:File)?[(].*"
        ),
        sink_type=SinkType.FILE_READ,
        cwe_id="CWE-22",
    ),
    SinkSpec(
        name="go_http_get",
        pattern=_GO_CALL_CODE_PATTERN.format(method="Get", code="http[.]Get[(].*"),
        sink_type=SinkType.HTTP_REQUEST,
        cwe_id="CWE-918",
        severity="high",
    ),
    SinkSpec(
        name="go_http_newrequest",
        pattern=_GO_CALL_CODE_PATTERN.format(
            method="NewRequest|NewRequestWithContext",
            code="http[.]New(?:Request|RequestWithContext)[(].*",
        ),
        sink_type=SinkType.HTTP_REQUEST,
        cwe_id="CWE-918",
        severity="medium",
    ),
    SinkSpec(
        name="go_xml_unmarshal",
        pattern=_GO_CALL_CODE_PATTERN.format(method="Unmarshal", code="xml[.]Unmarshal[(].*"),
        sink_type=SinkType.DESERIALIZATION,
        cwe_id="CWE-502",
        severity="high",
    ),
    SinkSpec(
        name="go_json_unmarshal",
        pattern=_GO_CALL_CODE_PATTERN.format(method="Unmarshal", code="json[.]Unmarshal[(].*"),
        sink_type=SinkType.DESERIALIZATION,
        cwe_id="CWE-502",
        severity="medium",
    ),
    SinkSpec(
        name="go_gob_decode",
        pattern=_GO_CALL_CODE_PATTERN.format(method="Decode", code="gob[.].*Decode[(].*"),
        sink_type=SinkType.DESERIALIZATION,
        cwe_id="CWE-502",
        severity="high",
    ),
    SinkSpec(
        name="go_http_redirect",
        pattern=_GO_CALL_CODE_PATTERN.format(method="Redirect", code="http[.]Redirect[(].*"),
        sink_type=SinkType.REDIRECT,
        cwe_id="CWE-601",
        severity="medium",
    ),
)

GO_SANITIZER_SPECS: tuple[SanitizerSpec, ...] = (
    SanitizerSpec(
        name="go_parameterized_query",
        pattern='cpg.call.name("Query|QueryRow|Exec|QueryContext|ExecContext").where(_.argument.order(2).isLiteral)',
        kind=SanitizerKind.PARAMETERIZE,
        mitigates=("CWE-89",),
        confidence=0.95,
    ),
    SanitizerSpec(
        name="go_html_template_execute",
        pattern='cpg.call.name("Execute|ExecuteTemplate").where(_.file.name(".*[.]go"))',
        kind=SanitizerKind.ESCAPE,
        mitigates=("CWE-79",),
        confidence=0.85,
    ),
    SanitizerSpec(
        name="go_filepath_clean",
        pattern=_GO_CALL_CODE_PATTERN.format(method="Clean", code="filepath[.]Clean[(].*"),
        kind=SanitizerKind.NORMALIZE,
        mitigates=("CWE-22",),
        confidence=0.5,
    ),
    SanitizerSpec(
        name="go_filepath_clean_hasprefix",
        pattern='cpg.call.name("HasPrefix").where(_.argument.isCall.name("Clean"))',
        kind=SanitizerKind.NORMALIZE,
        mitigates=("CWE-22",),
        confidence=0.9,
    ),
    SanitizerSpec(
        name="go_strconv_atoi",
        pattern=_GO_CALL_CODE_PATTERN.format(method="Atoi", code="strconv[.]Atoi[(].*"),
        kind=SanitizerKind.NORMALIZE,
        mitigates=("CWE-89",),
        confidence=0.7,
    ),
)


def get_source_specs(
    scan_config: ScanConfig | None = None,
    *,
    frameworks: Sequence[str] | None = None,
    disabled_plugins: frozenset[str] = frozenset(),
) -> tuple[SourceSpec, ...]:
    from piranesi.plugin import collect_source_specs

    normalized_frameworks = frozenset(f.lower() for f in frameworks or ())
    active = normalized_frameworks or frozenset({"express"})
    specs = collect_source_specs(active, disabled=disabled_plugins)
    if scan_config is not None:
        specs.extend(_custom_source_specs(scan_config))
    return tuple(specs)


def get_sink_specs(
    scan_config: ScanConfig | None = None,
    *,
    frameworks: Sequence[str] | None = None,
    disabled_plugins: frozenset[str] = frozenset(),
) -> tuple[SinkSpec, ...]:
    from piranesi.plugin import collect_sink_specs

    normalized_frameworks = frozenset(f.lower() for f in frameworks or ())
    active = normalized_frameworks or frozenset({"express"})
    specs = collect_sink_specs(active, disabled=disabled_plugins)
    if scan_config is not None:
        specs.extend(_custom_sink_specs(scan_config))
    return tuple(specs)


def get_sanitizer_specs(
    *,
    frameworks: Sequence[str] | None = None,
    disabled_plugins: frozenset[str] = frozenset(),
) -> tuple[SanitizerSpec, ...]:
    from piranesi.plugin import collect_sanitizer_specs

    normalized_frameworks = frozenset(f.lower() for f in frameworks or ())
    active = normalized_frameworks or frozenset({"express"})
    return tuple(collect_sanitizer_specs(active, disabled=disabled_plugins))


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
    "CHI_SOURCE_SPECS",
    "CRYPTO_TRANSPORT_JWT_PUBLIC_KEY_HINTS",
    "CRYPTO_TRANSPORT_NON_SECURITY_HASH_HINTS",
    "CRYPTO_TRANSPORT_NON_SECURITY_PRNG_HINTS",
    "CRYPTO_TRANSPORT_SECURITY_CONTEXT_HINTS",
    "CRYPTO_TRANSPORT_SECURITY_IDENTIFIER_HINTS",
    "CRYPTO_TRANSPORT_WEAK_EC_CURVES",
    "DJANGO_SOURCE_SPECS",
    "ECHO_SOURCE_SPECS",
    "FASTAPI_SOURCE_SPECS",
    "FASTIFY_SANITIZER_SPECS",
    "FASTIFY_SINK_SPECS",
    "FASTIFY_SOURCE_SPECS",
    "FLASK_SOURCE_SPECS",
    "GIN_SOURCE_SPECS",
    "GO_SANITIZER_SPECS",
    "GO_SINK_SPECS",
    "GO_STDLIB_SOURCE_SPECS",
    "NESTJS_SOURCE_SPECS",
    "NEXTJS_SOURCE_SPECS",
    "PYTHON_SANITIZER_SPECS",
    "PYTHON_SINK_SPECS",
    "SPRINGBOOT_SANITIZER_SPECS",
    "SPRINGBOOT_SINK_SPECS",
    "SPRINGBOOT_SOURCE_SPECS",
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

PHP_SOURCE_SPECS: tuple[SourceSpec, ...] = (
    SourceSpec(name="php_get", pattern="$_GET", source_type=SourceType.REQUEST_PARAM),
    SourceSpec(name="php_post", pattern="$_POST", source_type=SourceType.REQUEST_BODY),
    SourceSpec(name="php_request", pattern="$_REQUEST", source_type=SourceType.REQUEST_PARAM),
    SourceSpec(name="php_cookie", pattern="$_COOKIE", source_type=SourceType.COOKIE),
    SourceSpec(name="php_server", pattern="$_SERVER", source_type=SourceType.HEADER),
)
PHP_SINK_SPECS: tuple[SinkSpec, ...] = (
    SinkSpec(
        name="php_mysqli_query",
        pattern="mysqli_query|PDO::query|PDO::exec",
        sink_type=SinkType.SQL_QUERY,
        cwe_id="CWE-89",
    ),
    SinkSpec(
        name="php_exec",
        pattern="exec|system|shell_exec|passthru",
        sink_type=SinkType.SHELL_EXEC,
        cwe_id="CWE-78",
    ),
    SinkSpec(
        name="php_echo", pattern="echo|print", sink_type=SinkType.HTML_OUTPUT, cwe_id="CWE-79"
    ),
    SinkSpec(
        name="php_include",
        pattern="include|require|include_once|require_once",
        sink_type=SinkType.FILE_READ,
        cwe_id="CWE-22",
    ),
    SinkSpec(
        name="php_unserialize",
        pattern="unserialize",
        sink_type=SinkType.DESERIALIZATION,
        cwe_id="CWE-502",
    ),
    SinkSpec(
        name="php_curl_exec", pattern="curl_exec", sink_type=SinkType.HTTP_REQUEST, cwe_id="CWE-918"
    ),
    SinkSpec(name="php_eval", pattern="eval", sink_type=SinkType.EVAL, cwe_id="CWE-94"),
)
PHP_SANITIZER_SPECS: tuple[SanitizerSpec, ...] = (
    SanitizerSpec(
        name="php_htmlspecialchars",
        pattern="htmlspecialchars|htmlentities",
        kind=SanitizerKind.ESCAPE,
        mitigates=("CWE-79",),
    ),
    SanitizerSpec(
        name="php_prepared",
        pattern="prepare|bindParam|bindValue",
        kind=SanitizerKind.PARAMETERIZE,
        mitigates=("CWE-89",),
    ),
)

LARAVEL_SOURCE_SPECS: tuple[SourceSpec, ...] = (
    *PHP_SOURCE_SPECS,
    SourceSpec(
        name="laravel_request_input",
        pattern="$request->input|request\\(\\)->input",
        source_type=SourceType.REQUEST_BODY,
    ),
    SourceSpec(
        name="laravel_request_all",
        pattern="$request->all|request\\(\\)->all",
        source_type=SourceType.REQUEST_BODY,
    ),
    SourceSpec(
        name="laravel_request_query",
        pattern="$request->query|request\\(\\)->query",
        source_type=SourceType.REQUEST_PARAM,
    ),
)
LARAVEL_SINK_SPECS: tuple[SinkSpec, ...] = (
    *PHP_SINK_SPECS,
    SinkSpec(
        name="laravel_db_raw",
        pattern="DB::raw|selectRaw|whereRaw|orderByRaw",
        sink_type=SinkType.SQL_QUERY,
        cwe_id="CWE-89",
    ),
    SinkSpec(
        name="laravel_db_select",
        pattern="DB::select|DB::statement",
        sink_type=SinkType.SQL_QUERY,
        cwe_id="CWE-89",
    ),
    SinkSpec(
        name="laravel_http_client",
        pattern="Http::get|Http::post|Http::send",
        sink_type=SinkType.HTTP_REQUEST,
        cwe_id="CWE-918",
    ),
    SinkSpec(
        name="laravel_redirect",
        pattern="redirect\\(|->to\\(|->away\\(|Redirect::to",
        sink_type=SinkType.REDIRECT,
        cwe_id="CWE-601",
        severity="medium",
    ),
)
LARAVEL_SANITIZER_SPECS: tuple[SanitizerSpec, ...] = (
    *PHP_SANITIZER_SPECS,
    SanitizerSpec(
        name="laravel_e", pattern="e\\(", kind=SanitizerKind.ESCAPE, mitigates=("CWE-79",)
    ),
    SanitizerSpec(
        name="laravel_binding",
        pattern="->where\\(|DB::select\\(",
        kind=SanitizerKind.PARAMETERIZE,
        mitigates=("CWE-89",),
    ),
)

SYMFONY_SOURCE_SPECS: tuple[SourceSpec, ...] = (
    *PHP_SOURCE_SPECS,
    SourceSpec(
        name="symfony_request_get", pattern="$request->get", source_type=SourceType.REQUEST_PARAM
    ),
    SourceSpec(
        name="symfony_request_content",
        pattern="$request->getContent",
        source_type=SourceType.REQUEST_BODY,
    ),
)
SYMFONY_SINK_SPECS: tuple[SinkSpec, ...] = (
    *PHP_SINK_SPECS,
    SinkSpec(
        name="symfony_execute_query",
        pattern="$connection->executeQuery|$connection->query",
        sink_type=SinkType.SQL_QUERY,
        cwe_id="CWE-89",
    ),
    SinkSpec(
        name="symfony_process",
        pattern="new Process\\(",
        sink_type=SinkType.SHELL_EXEC,
        cwe_id="CWE-78",
    ),
)
SYMFONY_SANITIZER_SPECS: tuple[SanitizerSpec, ...] = (
    *PHP_SANITIZER_SPECS,
    SanitizerSpec(
        name="symfony_twig_escape",
        pattern="twig_escape_filter",
        kind=SanitizerKind.ESCAPE,
        mitigates=("CWE-79",),
    ),
    SanitizerSpec(
        name="symfony_set_parameter",
        pattern="setParameter",
        kind=SanitizerKind.PARAMETERIZE,
        mitigates=("CWE-89",),
    ),
)

WORDPRESS_SOURCE_SPECS: tuple[SourceSpec, ...] = (
    *PHP_SOURCE_SPECS,
    SourceSpec(
        name="wordpress_rest_get_param",
        pattern="$request->get_param|WP_REST_Request",
        source_type=SourceType.REQUEST_PARAM,
    ),
    SourceSpec(name="wordpress_get", pattern="$_GET", source_type=SourceType.REQUEST_PARAM),
    SourceSpec(name="wordpress_post", pattern="$_POST", source_type=SourceType.REQUEST_BODY),
)
WORDPRESS_SINK_SPECS: tuple[SinkSpec, ...] = (
    *PHP_SINK_SPECS,
    SinkSpec(
        name="wordpress_wpdb_query",
        pattern="$wpdb->query|$wpdb->get_results",
        sink_type=SinkType.SQL_QUERY,
        cwe_id="CWE-89",
    ),
)
WORDPRESS_SANITIZER_SPECS: tuple[SanitizerSpec, ...] = (
    *PHP_SANITIZER_SPECS,
    SanitizerSpec(
        name="wordpress_esc_html",
        pattern="esc_html|esc_attr|wp_kses",
        kind=SanitizerKind.ESCAPE,
        mitigates=("CWE-79",),
    ),
    SanitizerSpec(
        name="wordpress_wpdb_prepare",
        pattern="$wpdb->prepare",
        kind=SanitizerKind.PARAMETERIZE,
        mitigates=("CWE-89",),
    ),
    SanitizerSpec(
        name="wordpress_wp_nonce_field",
        pattern="wp_nonce_field|check_admin_referer|wp_verify_nonce",
        kind=SanitizerKind.VALIDATE,
        mitigates=("CWE-352",),
    ),
)

RUBY_SOURCE_SPECS: tuple[SourceSpec, ...] = (
    SourceSpec(name="ruby_params", pattern="params", source_type=SourceType.REQUEST_PARAM),
    SourceSpec(
        name="ruby_request_body",
        pattern="request.body|request.raw_post",
        source_type=SourceType.REQUEST_BODY,
    ),
    SourceSpec(name="ruby_env", pattern="request.env|ENV\\[", source_type=SourceType.HEADER),
    SourceSpec(name="ruby_cookies", pattern="cookies", source_type=SourceType.COOKIE),
)
RUBY_SINK_SPECS: tuple[SinkSpec, ...] = (
    SinkSpec(
        name="ruby_active_record_string_query",
        pattern="ActiveRecord::Base.connection.execute|where\\(|order\\(|group\\(",
        sink_type=SinkType.SQL_QUERY,
        cwe_id="CWE-89",
    ),
    SinkSpec(
        name="ruby_find_by_sql",
        pattern="find_by_sql",
        sink_type=SinkType.SQL_QUERY,
        cwe_id="CWE-89",
    ),
    SinkSpec(
        name="ruby_raw_helper", pattern="\\braw\\b", sink_type=SinkType.HTML_OUTPUT, cwe_id="CWE-79"
    ),
    SinkSpec(
        name="ruby_html_safe",
        pattern="\\.html_safe\\b",
        sink_type=SinkType.HTML_OUTPUT,
        cwe_id="CWE-79",
    ),
    SinkSpec(
        name="ruby_system",
        pattern="\\bsystem\\b|IO\\.popen",
        sink_type=SinkType.SHELL_EXEC,
        cwe_id="CWE-78",
    ),
    SinkSpec(
        name="ruby_file_read", pattern="File\\.read", sink_type=SinkType.FILE_READ, cwe_id="CWE-22"
    ),
    SinkSpec(
        name="ruby_yaml_load",
        pattern="YAML\\.load|Marshal\\.load",
        sink_type=SinkType.DESERIALIZATION,
        cwe_id="CWE-502",
    ),
    SinkSpec(
        name="ruby_render_dynamic",
        pattern="\\brender\\b",
        sink_type=SinkType.TEMPLATE_INJECTION,
        cwe_id="CWE-1336",
    ),
    SinkSpec(
        name="ruby_redirect_to",
        pattern="\\bredirect_to\\b",
        sink_type=SinkType.REDIRECT,
        cwe_id="CWE-601",
        severity="medium",
    ),
)
RUBY_SANITIZER_SPECS: tuple[SanitizerSpec, ...] = (
    SanitizerSpec(
        name="ruby_sanitize",
        pattern="sanitize\\(",
        kind=SanitizerKind.ESCAPE,
        mitigates=("CWE-79",),
    ),
    SanitizerSpec(
        name="ruby_html_escape",
        pattern="ERB::Util.html_escape|CGI.escapeHTML",
        kind=SanitizerKind.ESCAPE,
        mitigates=("CWE-79",),
    ),
    SanitizerSpec(
        name="ruby_sanitize_sql",
        pattern="sanitize_sql|sanitize_sql_array",
        kind=SanitizerKind.PARAMETERIZE,
        mitigates=("CWE-89",),
    ),
    SanitizerSpec(
        name="ruby_permit", pattern="permit\\(", kind=SanitizerKind.VALIDATE, mitigates=("CWE-915",)
    ),
    SanitizerSpec(
        name="ruby_shellwords_escape",
        pattern="Shellwords\\.shellescape|Shellwords\\.escape",
        kind=SanitizerKind.ESCAPE,
        mitigates=("CWE-78",),
    ),
    SanitizerSpec(
        name="ruby_yaml_safe_load",
        pattern="YAML\\.safe_load",
        kind=SanitizerKind.VALIDATE,
        mitigates=("CWE-502",),
    ),
)

CRYPTO_TRANSPORT_JWT_PUBLIC_KEY_HINTS = (
    "publicKey",
    "public_key",
    "pubkey",
    "pub_key",
    "verifyKey",
    "verify_key",
    "signingKey",
    "signing_key",
    "jwtPublicKey",
    "jwt_public_key",
    "certificate",
    "cert",
    "rsaPublicKey",
    "rsa_public_key",
)
CRYPTO_TRANSPORT_NON_SECURITY_HASH_HINTS = (
    "checksum",
    "etag",
    "fingerprint",
    "cache_key",
    "dedup",
    "content_hash",
    "file_hash",
    "asset_hash",
    "build_hash",
)
CRYPTO_TRANSPORT_NON_SECURITY_PRNG_HINTS = (
    "shuffle",
    "sample",
    "jitter",
    "delay",
    "color",
    "placeholder",
    "animation",
    "style",
    "mock",
    "test",
    "seed",
    "demo",
    "example",
)
CRYPTO_TRANSPORT_SECURITY_CONTEXT_HINTS = (
    "password",
    "token",
    "secret",
    "credential",
    "auth",
    "session",
    "jwt",
    "api_key",
    "apiKey",
    "private_key",
    "privateKey",
)
CRYPTO_TRANSPORT_SECURITY_IDENTIFIER_HINTS = (
    "token",
    "session_token",
    "secret",
    "csrf_token",
    "auth_code",
    "reset_token",
    "verification_code",
    "userId",
    "user_id",
    "accountId",
    "account_id",
    "sessionId",
    "session_id",
    "transactionId",
    "transaction_id",
)
CRYPTO_TRANSPORT_WEAK_EC_CURVES = (
    "secp112r1",
    "secp128r1",
    "secp160r1",
    "prime192v1",
    "secp192r1",
    "prime192v2",
    "prime192v3",
    "secp192k1",
    "sect163k1",
)
