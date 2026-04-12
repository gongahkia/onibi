from __future__ import annotations

from collections.abc import Sequence
from dataclasses import dataclass
from typing import Protocol

from piranesi.scan.queries import QueryNode
from piranesi.scan.specs import SinkSpec, SinkType, SourceSpec

_NOSQL_OBJECT_SOURCES = frozenset(
    {
        "express_req_body",
        "fastify_request_body",
        "nextjs_pages_req_body",
        "nextjs_app_request_json",
        "nestjs_body",
        "nestjs_req",
        "spring_request_body",
        "flask_request_json",
        "django_request_body",
        "fastapi_body",
        "gin_bind_json",
    }
)
_NOSQL_BODY_MARKERS = (
    "req.body",
    "request.body",
    "request.json",
    "request.POST",
    "BindJSON",
    "ShouldBindJSON",
)
_NOSQL_DANGEROUS_TOKENS = ("$where", "$regex", "$ne", "$gt", "$lt", "$exists", "BasicQuery")
_TEMPLATE_SOURCE_TOKENS = (
    "compile(",
    "renderString(",
    "from_string(",
    "Template(",
    "process(",
    "evaluate(",
)
_TEMPLATE_CONTEXT_TOKENS = ("Context(", ".render(", "{", "name=", "context=")
_LDAP_FILTER_TOKENS = ("(uid=", "(cn=", "(ou=", "(dc=", "objectClass=", "sAMAccountName=")
_HEADER_VALUE_CALLS = ("setHeader(", ".set(", ".header(", ".writeHead(", "redirect(")
_EXPRESSION_CALLS = (
    "parseExpression(",
    "getValue(",
    "setValue(",
    "createValueExpression(",
    "createMethodExpression(",
    "MVEL.eval(",
)
_XPATH_CALLS = ("xpath(", "findall(", "find(", "iterfind(", "compile(", "evaluate(", "select(")


class _FlowStep(Protocol):
    @property
    def code(self) -> str: ...


@dataclass(frozen=True, slots=True)
class InjectionVariantDecision:
    report: bool
    reason: str


def should_report_injection_variant(
    *,
    source_spec: SourceSpec,
    sink_spec: SinkSpec,
    elements: Sequence[QueryNode],
) -> bool:
    decision = classify_injection_variant_flow(
        source_spec=source_spec,
        sink_spec=sink_spec,
        elements=elements,
    )
    return decision.report


def classify_injection_variant_flow(
    *,
    source_spec: SourceSpec,
    sink_spec: SinkSpec,
    elements: Sequence[QueryNode],
) -> InjectionVariantDecision:
    if sink_spec.sink_type is SinkType.NOSQL_QUERY:
        report = is_nosql_operator_position(elements, source_spec=source_spec, sink_spec=sink_spec)
        return InjectionVariantDecision(report=report, reason="nosql_operator_position")
    if sink_spec.sink_type is SinkType.TEMPLATE_INJECTION:
        report = is_template_source_position(elements)
        return InjectionVariantDecision(report=report, reason="template_source_position")
    if sink_spec.sink_type is SinkType.LDAP_QUERY:
        report = is_ldap_filter_concat(elements)
        return InjectionVariantDecision(report=report, reason="ldap_filter_position")
    if sink_spec.sink_type is SinkType.HEADER_INJECTION and sink_spec.cwe_id == "CWE-113":
        report = is_header_value_position(elements)
        return InjectionVariantDecision(report=report, reason="header_value_position")
    if sink_spec.sink_type is SinkType.EXPRESSION_INJECTION:
        report = is_expression_language_position(elements)
        return InjectionVariantDecision(report=report, reason="expression_language_position")
    if sink_spec.sink_type is SinkType.XPATH_QUERY:
        report = is_xpath_query_position(elements)
        return InjectionVariantDecision(report=report, reason="xpath_query_position")
    return InjectionVariantDecision(report=True, reason="default")


def is_nosql_operator_position(
    flow_path: Sequence[QueryNode],
    *,
    source_spec: SourceSpec | None = None,
    sink_spec: SinkSpec | None = None,
) -> bool:
    snippets = _flow_snippets(flow_path)
    if any(token in snippet for snippet in snippets for token in _NOSQL_DANGEROUS_TOKENS):
        return True
    if source_spec is not None and source_spec.name in _NOSQL_OBJECT_SOURCES:
        return True
    if any(marker in snippet for snippet in snippets for marker in _NOSQL_BODY_MARKERS):
        return True
    if sink_spec is not None and sink_spec.name in {
        "mongodb_collection_find",
        "mongodb_collection_update",
        "mongodb_collection_delete",
        "mongodb_collection_aggregate",
        "mongoose_model_find",
        "pymongo_find",
        "pymongo_update",
        "pymongo_aggregate",
        "go_mongo_find",
        "go_mongo_aggregate",
        "spring_mongo_template_find",
    }:
        return any(
            marker in snippet
            for snippet in snippets
            for marker in ("req.body", "request.json", "BasicQuery")
        )
    return False


def is_template_source_position(flow_path: Sequence[_FlowStep]) -> bool:
    snippets = _flow_snippets(flow_path)
    if not snippets:
        return False
    terminal = snippets[-1]
    if any(token in terminal for token in _TEMPLATE_SOURCE_TOKENS):
        return True
    render_index = terminal.find("render(")
    if render_index != -1:
        comma_index = terminal.find(",", render_index)
        source_slice = terminal if comma_index == -1 else terminal[render_index:comma_index]
        if any(
            marker in source_slice
            for marker in ("req.", "request.", ".template", '["tpl"]', "['tpl']")
        ):
            return True
    if terminal.strip().startswith("{") or "Context(" in terminal:
        return False
    has_source_marker = any(
        token in snippet for snippet in snippets for token in _TEMPLATE_SOURCE_TOKENS
    )
    has_context_marker = any(token in terminal for token in _TEMPLATE_CONTEXT_TOKENS)
    return has_source_marker and not has_context_marker


def is_ldap_filter_concat(flow_path: Sequence[_FlowStep]) -> bool:
    snippets = _flow_snippets(flow_path)
    if not snippets:
        return False
    return any(token in snippet for snippet in snippets for token in _LDAP_FILTER_TOKENS)


def is_header_value_position(flow_path: Sequence[_FlowStep]) -> bool:
    snippets = _flow_snippets(flow_path)
    if not snippets:
        return False
    terminal = snippets[-1]
    if terminal.startswith(('"X-', "'X-", '"Location', "'Location")):
        return False
    return any(token in terminal for token in _HEADER_VALUE_CALLS) or len(snippets) == 1


def is_expression_language_position(flow_path: Sequence[_FlowStep]) -> bool:
    snippets = _flow_snippets(flow_path)
    return any(token in snippet for snippet in snippets for token in _EXPRESSION_CALLS)


def is_xpath_query_position(flow_path: Sequence[_FlowStep]) -> bool:
    snippets = _flow_snippets(flow_path)
    return any(token in snippet for snippet in snippets for token in _XPATH_CALLS)


def _flow_snippets(flow_path: Sequence[_FlowStep | QueryNode]) -> tuple[str, ...]:
    snippets: list[str] = []
    for step in flow_path:
        snippet = _snippet_for_step(step)
        if snippet:
            snippets.append(snippet)
    return tuple(snippets)


def _snippet_for_step(step: _FlowStep | QueryNode) -> str:
    code = getattr(step, "code", None)
    if isinstance(code, str) and code.strip():
        return code.strip()
    location = getattr(step, "location", None)
    snippet = getattr(location, "snippet", None)
    if isinstance(snippet, str):
        return snippet.strip()
    return ""


__all__ = [
    "InjectionVariantDecision",
    "classify_injection_variant_flow",
    "is_expression_language_position",
    "is_header_value_position",
    "is_ldap_filter_concat",
    "is_nosql_operator_position",
    "is_template_source_position",
    "is_xpath_query_position",
    "should_report_injection_variant",
]
