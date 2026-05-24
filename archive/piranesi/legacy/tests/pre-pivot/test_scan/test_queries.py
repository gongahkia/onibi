from __future__ import annotations

import json
import socket
from collections.abc import Generator
from pathlib import Path
from typing import Any

import pytest

from piranesi.config import load_config
from piranesi.scan.joern import JoernError, JoernServer, is_joern_installed
from piranesi.scan.queries import (
    CPGQLQueryError,
    build_flow_query,
    build_nodes_query,
    execute_flow_query,
    execute_sanitizer_query,
    execute_sink_query,
    execute_source_query,
)
from piranesi.scan.specs import (
    BUILTIN_SANITIZER_SPECS,
    SanitizerKind,
    SanitizerSpec,
    SinkSpec,
    SinkType,
    SourceSpec,
    SourceType,
    get_sanitizer_specs,
    get_sink_specs,
    get_source_specs,
)

SCAN_QUERY_FIXTURES_DIR = Path(__file__).resolve().parents[1] / "fixtures" / "scan_queries"
PYTHON_FLASK_FIXTURES_DIR = (
    Path(__file__).resolve().parents[1] / "fixtures" / "python" / "flask_app"
)
PYTHON_DJANGO_FIXTURES_DIR = (
    Path(__file__).resolve().parents[1] / "fixtures" / "python" / "django_app"
)


class FakeJoernServer:
    def __init__(self, responses: dict[str, dict[str, object]]) -> None:
        self.responses = responses
        self.queries: list[str] = []

    def query(self, cpgql: str) -> dict[str, object]:
        self.queries.append(cpgql)
        return self.responses.get(
            cpgql, {"success": True, "stdout": 'val res0: String = "[]"'}
        ).copy()


def _joern_json_stdout(payload: object) -> str:
    return f'val res0: String = """{json.dumps(payload, indent=2)}"""'


def _source_spec_by_name(specs: tuple[SourceSpec, ...], name: str) -> SourceSpec:
    return next(spec for spec in specs if spec.name == name)


def _sink_spec_by_name(specs: tuple[SinkSpec, ...], name: str) -> SinkSpec:
    return next(spec for spec in specs if spec.name == name)


def _sanitizer_spec_by_name(specs: tuple[SanitizerSpec, ...], name: str) -> SanitizerSpec:
    return next(spec for spec in specs if spec.name == name)


def _codes(elements: tuple[object, ...]) -> set[str]:
    return {element.code for element in elements}  # type: ignore[attr-defined]


def _find_free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def test_build_flow_query_uses_reachable_by_flows_core_pattern() -> None:
    source_spec = SourceSpec(
        name="test_source",
        pattern='cpg.call.name("source")',
        source_type=SourceType.CUSTOM,
        is_custom=True,
    )
    sink_spec = SinkSpec(
        name="test_sink",
        pattern='cpg.call.name("sink")',
        sink_type=SinkType.CUSTOM,
        cwe_id=None,
        is_custom=True,
    )

    assert build_nodes_query(source_spec.pattern) == '(cpg.call.name("source")).toJsonPretty'
    assert (
        build_flow_query(source_spec, sink_spec)
        == '(cpg.call.name("sink")).reachableByFlows(cpg.call.name("source")).toJsonPretty'
    )


def test_build_flow_query_prefers_sink_flow_pattern_when_present() -> None:
    source_spec = SourceSpec(
        name="test_source",
        pattern='cpg.call.name("source")',
        source_type=SourceType.CUSTOM,
        is_custom=True,
    )
    sink_spec = SinkSpec(
        name="ssrf_full_url",
        pattern='cpg.call.name("fetch")',
        sink_type=SinkType.HTTP_REQUEST,
        cwe_id="CWE-918",
        flow_pattern='cpg.call.name("fetch").argument(1)',
        flow_to_parent_call=True,
        is_custom=True,
    )

    assert build_flow_query(source_spec, sink_spec) == (
        '(cpg.call.name("fetch").argument(1)).reachableByFlows('
        'cpg.call.name("source")).toJsonPretty'
    )


def test_execute_flow_query_filters_paths_that_cross_sanitizers() -> None:
    source_spec = SourceSpec(
        name="custom_source",
        pattern='cpg.call.name("customInput")',
        source_type=SourceType.CUSTOM,
        is_custom=True,
    )
    sink_spec = SinkSpec(
        name="custom_sink",
        pattern='cpg.call.name("customDangerous")',
        sink_type=SinkType.CUSTOM,
        cwe_id=None,
        is_custom=True,
    )
    sanitizer_spec = SanitizerSpec(
        name="custom_escape",
        pattern='cpg.call.name("escape")',
        kind=SanitizerKind.ESCAPE,
    )
    flow_query = build_flow_query(source_spec, sink_spec)
    sanitizer_query = build_nodes_query(sanitizer_spec.pattern)
    server = FakeJoernServer(
        {
            flow_query: {
                "success": True,
                "stdout": _joern_json_stdout(
                    [
                        {
                            "elements": [
                                {
                                    "_id": 1,
                                    "_label": "CALL",
                                    "code": "customInput()",
                                    "name": "customInput",
                                },
                                {
                                    "_id": 2,
                                    "_label": "CALL",
                                    "code": "escape(taint)",
                                    "name": "escape",
                                },
                                {
                                    "_id": 3,
                                    "_label": "CALL",
                                    "code": "customDangerous(safe)",
                                    "name": "customDangerous",
                                },
                            ]
                        },
                        {
                            "elements": [
                                {
                                    "_id": 4,
                                    "_label": "CALL",
                                    "code": "customInput()",
                                    "name": "customInput",
                                },
                                {
                                    "_id": 5,
                                    "_label": "CALL",
                                    "code": "customDangerous(taint)",
                                    "name": "customDangerous",
                                },
                            ]
                        },
                    ]
                ),
            },
            sanitizer_query: {
                "success": True,
                "stdout": _joern_json_stdout(
                    [
                        {
                            "_id": 2,
                            "_label": "CALL",
                            "code": "escape(taint)",
                            "name": "escape",
                        }
                    ]
                ),
            },
        }
    )

    flows = execute_flow_query(server, source_spec, sink_spec, sanitizer_specs=(sanitizer_spec,))  # type: ignore[arg-type]

    assert len(flows) == 1
    assert [element.code for element in flows[0].elements] == [
        "customInput()",
        "customDangerous(taint)",
    ]
    assert server.queries == [sanitizer_query, flow_query]


def test_execute_source_query_raises_when_joern_rejects_generated_cpgql() -> None:
    source_spec = SourceSpec(
        name="broken_source",
        pattern='cpg.call.name("broken")',
        source_type=SourceType.CUSTOM,
        is_custom=True,
    )
    query = build_nodes_query(source_spec.pattern)
    server = FakeJoernServer(
        {
            query: {
                "success": True,
                "stdout": "-- [E008] Not Found Error:\n1 error found\n",
            }
        }
    )

    with pytest.raises(CPGQLQueryError, match="Joern rejected query"):
        execute_source_query(server, source_spec)  # type: ignore[arg-type]


def test_execute_flow_query_for_ssrf_full_url_uses_first_argument_and_parent_call() -> None:
    source_spec = SourceSpec(
        name="express_req_query",
        pattern='cpg.call.name("<operator>.fieldAccess").code("req[.]query.*")',
        source_type=SourceType.REQUEST_PARAM,
    )
    sink_spec = SinkSpec(
        name="ssrf_full_url",
        pattern='cpg.call.name("fetch")',
        sink_type=SinkType.HTTP_REQUEST,
        cwe_id="CWE-918",
        severity="high",
        flow_pattern='cpg.call.name("fetch").argument(1)',
        flow_to_parent_call=True,
    )
    noop_sanitizer = SanitizerSpec(
        name="noop",
        pattern='cpg.call.name("__never__")',
        kind=SanitizerKind.ESCAPE,
    )
    flow_query = build_flow_query(source_spec, sink_spec)
    sanitizer_query = build_nodes_query(noop_sanitizer.pattern)
    parent_query = "cpg.identifier.id(11L).astParent.toJsonPretty"
    server = FakeJoernServer(
        {
            sanitizer_query: {"success": True, "stdout": _joern_json_stdout([])},
            flow_query: {
                "success": True,
                "stdout": _joern_json_stdout(
                    [
                        {
                            "elements": [
                                {
                                    "_id": 1,
                                    "_label": "CALL",
                                    "code": "req.query.url",
                                    "name": "<operator>.fieldAccess",
                                },
                                {
                                    "_id": 10,
                                    "_label": "IDENTIFIER",
                                    "code": "url",
                                    "name": "url",
                                },
                                {
                                    "_id": 11,
                                    "_label": "IDENTIFIER",
                                    "code": "url",
                                    "name": "url",
                                },
                            ]
                        }
                    ]
                ),
            },
            parent_query: {
                "success": True,
                "stdout": _joern_json_stdout(
                    [
                        {
                            "_id": 12,
                            "_label": "CALL",
                            "code": "fetch(url)",
                            "name": "fetch",
                            "methodFullName": "fetch",
                        }
                    ]
                ),
            },
        }
    )

    flows = execute_flow_query(server, source_spec, sink_spec, sanitizer_specs=(noop_sanitizer,))  # type: ignore[arg-type]

    assert len(flows) == 1
    assert [element.code for element in flows[0].elements] == [
        "req.query.url",
        "url",
        "url",
        "fetch(url)",
    ]
    assert parent_query in server.queries


def test_execute_flow_query_reclassifies_hardcoded_base_templates_as_path_segment() -> None:
    source_spec = SourceSpec(
        name="express_req_query",
        pattern='cpg.call.name("<operator>.fieldAccess").code("req[.]query.*")',
        source_type=SourceType.REQUEST_PARAM,
    )
    full_url_sink = SinkSpec(
        name="ssrf_full_url",
        pattern='cpg.call.name("fetch")',
        sink_type=SinkType.HTTP_REQUEST,
        cwe_id="CWE-918",
        severity="high",
        flow_pattern='cpg.call.name("fetch").argument(1)',
        flow_to_parent_call=True,
    )
    path_segment_sink = SinkSpec(
        name="ssrf_path_segment",
        pattern='cpg.call.name("fetch")',
        sink_type=SinkType.HTTP_REQUEST,
        cwe_id="CWE-918",
        severity="medium",
        flow_pattern='cpg.call.name("fetch").argument(1)',
        flow_to_parent_call=True,
    )
    noop_sanitizer = SanitizerSpec(
        name="noop",
        pattern='cpg.call.name("__never__")',
        kind=SanitizerKind.ESCAPE,
    )
    flow_query = build_flow_query(source_spec, full_url_sink)
    sanitizer_query = build_nodes_query(noop_sanitizer.pattern)
    parent_query = "cpg.identifier.id(21L).astParent.toJsonPretty"
    payload = [
        {
            "elements": [
                {
                    "_id": 1,
                    "_label": "CALL",
                    "code": "req.query.userId",
                    "name": "<operator>.fieldAccess",
                },
                {
                    "_id": 20,
                    "_label": "IDENTIFIER",
                    "code": "endpoint",
                    "name": "endpoint",
                },
                {
                    "_id": 30,
                    "_label": "CALL",
                    "code": "`https://internal.service.local/api/users/${userId}`",
                    "name": "<operator>.formatString",
                },
                {
                    "_id": 21,
                    "_label": "IDENTIFIER",
                    "code": "endpoint",
                    "name": "endpoint",
                },
            ]
        }
    ]
    server = FakeJoernServer(
        {
            sanitizer_query: {"success": True, "stdout": _joern_json_stdout([])},
            flow_query: {"success": True, "stdout": _joern_json_stdout(payload)},
            parent_query: {
                "success": True,
                "stdout": _joern_json_stdout(
                    [
                        {
                            "_id": 22,
                            "_label": "CALL",
                            "code": "fetch(endpoint)",
                            "name": "fetch",
                            "methodFullName": "fetch",
                        }
                    ]
                ),
            },
        }
    )

    full_url_flows = execute_flow_query(
        server,  # type: ignore[arg-type]
        source_spec,
        full_url_sink,
        sanitizer_specs=(noop_sanitizer,),
    )
    path_segment_flows = execute_flow_query(
        server,  # type: ignore[arg-type]
        source_spec,
        path_segment_sink,
        sanitizer_specs=(noop_sanitizer,),
    )

    assert full_url_flows == ()
    assert len(path_segment_flows) == 1
    assert path_segment_flows[0].elements[-1].code == "fetch(endpoint)"


def test_execute_sink_query_filters_excluded_receivers() -> None:
    sink_spec = SinkSpec(
        name="receiver_filtered_sink",
        pattern='cpg.call.name("get|post")',
        sink_type=SinkType.HTTP_REQUEST,
        cwe_id="CWE-918",
        exclude_receivers=("app", "router"),
        is_custom=True,
    )
    sink_query = build_nodes_query(sink_spec.pattern)
    server = FakeJoernServer(
        {
            sink_query: {
                "success": True,
                "stdout": _joern_json_stdout(
                    [
                        {"_id": 1, "_label": "CALL", "code": "app.get(url)", "name": "get"},
                        {"_id": 2, "_label": "CALL", "code": "router.post(url)", "name": "post"},
                        {"_id": 3, "_label": "CALL", "code": "axios.get(url)", "name": "get"},
                        {"_id": 4, "_label": "CALL", "code": "http.get(url)", "name": "get"},
                    ]
                ),
            }
        }
    )

    sink_nodes = execute_sink_query(server, sink_spec)  # type: ignore[arg-type]

    assert _codes(sink_nodes) == {"axios.get(url)", "http.get(url)"}


def test_execute_sink_query_filters_included_receivers() -> None:
    sink_spec = SinkSpec(
        name="receiver_allowlist_sink",
        pattern='cpg.call.name("get")',
        sink_type=SinkType.HTTP_REQUEST,
        cwe_id="CWE-918",
        include_receivers=("axios",),
        is_custom=True,
    )
    sink_query = build_nodes_query(sink_spec.pattern)
    server = FakeJoernServer(
        {
            sink_query: {
                "success": True,
                "stdout": _joern_json_stdout(
                    [
                        {"_id": 1, "_label": "CALL", "code": "axios.get(url)", "name": "get"},
                        {"_id": 2, "_label": "CALL", "code": "http.get(url)", "name": "get"},
                        {"_id": 3, "_label": "CALL", "code": "fetch(url)", "name": "fetch"},
                    ]
                ),
            }
        }
    )

    sink_nodes = execute_sink_query(server, sink_spec)  # type: ignore[arg-type]

    assert _codes(sink_nodes) == {"axios.get(url)"}


def test_execute_flow_query_filters_excluded_receivers() -> None:
    source_spec = SourceSpec(
        name="custom_source",
        pattern='cpg.call.name("customInput")',
        source_type=SourceType.CUSTOM,
        is_custom=True,
    )
    sink_spec = SinkSpec(
        name="receiver_filtered_sink",
        pattern='cpg.call.name("get|post")',
        sink_type=SinkType.HTTP_REQUEST,
        cwe_id="CWE-918",
        exclude_receivers=("app", "router"),
        is_custom=True,
    )
    sanitizer_spec = SanitizerSpec(
        name="noop",
        pattern='cpg.call.name("__noop__")',
        kind=SanitizerKind.SANITIZE,
        blocks_flow=False,
    )
    flow_query = build_flow_query(source_spec, sink_spec)
    server = FakeJoernServer(
        {
            flow_query: {
                "success": True,
                "stdout": _joern_json_stdout(
                    [
                        {
                            "elements": [
                                {
                                    "_id": 1,
                                    "_label": "CALL",
                                    "code": "customInput()",
                                    "name": "customInput",
                                },
                                {"_id": 2, "_label": "CALL", "code": "app.get(url)", "name": "get"},
                            ]
                        },
                        {
                            "elements": [
                                {
                                    "_id": 3,
                                    "_label": "CALL",
                                    "code": "customInput()",
                                    "name": "customInput",
                                },
                                {
                                    "_id": 4,
                                    "_label": "CALL",
                                    "code": "router.post(url)",
                                    "name": "post",
                                },
                            ]
                        },
                        {
                            "elements": [
                                {
                                    "_id": 5,
                                    "_label": "CALL",
                                    "code": "customInput()",
                                    "name": "customInput",
                                },
                                {
                                    "_id": 6,
                                    "_label": "CALL",
                                    "code": "axios.get(url)",
                                    "name": "get",
                                },
                            ]
                        },
                    ]
                ),
            }
        }
    )

    flows = execute_flow_query(
        server,  # type: ignore[arg-type]
        source_spec,
        sink_spec,
        sanitizer_specs=(sanitizer_spec,),
    )

    assert len(flows) == 1
    assert [element.code for element in flows[0].elements] == ["customInput()", "axios.get(url)"]


def test_scan_specs_include_custom_patterns_from_piranesi_toml(config_file: Any) -> None:
    path = config_file(
        "\n".join(
            [
                "[scan.custom_sources]",
                "patterns = ['cpg.call.name(\"customInput\")']",
                "",
                "[scan.custom_sinks]",
                "patterns = ['cpg.call.name(\"customDangerous\")']",
                "sink_type = 'http_request'",
                "cwe_id = 'CWE-1234'",
                "include_receivers = ['axios', 'http']",
                "exclude_receivers = ['app', 'router']",
            ]
        )
    )

    config = load_config(path)
    source_specs = get_source_specs(config.scan)
    sink_specs = get_sink_specs(config.scan)

    custom_source = source_specs[-1]
    custom_sink = sink_specs[-1]

    assert custom_source.is_custom is True
    assert custom_source.pattern == 'cpg.call.name("customInput")'
    assert custom_source.source_type is SourceType.CUSTOM
    assert custom_sink.is_custom is True
    assert custom_sink.pattern == 'cpg.call.name("customDangerous")'
    assert custom_sink.sink_type is SinkType.HTTP_REQUEST
    assert custom_sink.cwe_id == "CWE-1234"
    assert custom_sink.include_receivers == ("axios", "http")
    assert custom_sink.exclude_receivers == ("app", "router")


def test_fastify_specs_are_appended_when_framework_selected() -> None:
    source_specs = get_source_specs(frameworks=("fastify",))
    sink_specs = get_sink_specs(frameworks=("fastify",))
    sanitizer_specs = get_sanitizer_specs(frameworks=("fastify",))

    assert (
        _source_spec_by_name(source_specs, "fastify_request_body").source_type
        is SourceType.REQUEST_BODY
    )
    assert (
        _source_spec_by_name(source_specs, "fastify_request_query").source_type
        is SourceType.URL_PARAM
    )
    assert _sink_spec_by_name(sink_specs, "fastify_reply_send").sink_type is SinkType.HTML_OUTPUT
    assert (
        _sink_spec_by_name(sink_specs, "fastify_reply_header").sink_type
        is SinkType.HEADER_INJECTION
    )
    assert (
        _sanitizer_spec_by_name(sanitizer_specs, "fastify_schema_validation").blocks_flow is False
    )


def test_nestjs_specs_are_appended_when_framework_selected() -> None:
    source_specs = get_source_specs(frameworks=("nestjs",))

    assert _source_spec_by_name(source_specs, "nestjs_body").source_type is SourceType.REQUEST_BODY
    assert (
        _source_spec_by_name(source_specs, "nestjs_param").source_type is SourceType.REQUEST_PARAM
    )
    assert _source_spec_by_name(source_specs, "nestjs_query").source_type is SourceType.URL_PARAM
    assert _source_spec_by_name(source_specs, "nestjs_headers").source_type is SourceType.HEADER
    assert _source_spec_by_name(source_specs, "nestjs_req").source_type is SourceType.REQUEST_BODY


@pytest.fixture(scope="module")
def joern_server() -> Generator[JoernServer, None, None]:
    if not is_joern_installed():
        pytest.skip("Joern is not installed in PATH")

    try:
        with JoernServer(port=8123, startup_timeout_seconds=30, query_timeout_seconds=30) as server:
            server.import_project(SCAN_QUERY_FIXTURES_DIR)
            yield server
    except JoernError as exc:
        pytest.skip(str(exc))


@pytest.fixture(scope="module")
def python_flask_joern_server() -> Generator[JoernServer, None, None]:
    if not is_joern_installed():
        pytest.skip("Joern is not installed in PATH")

    try:
        with JoernServer(
            port=_find_free_port(),
            startup_timeout_seconds=30,
            query_timeout_seconds=30,
        ) as server:
            server.import_project(
                PYTHON_FLASK_FIXTURES_DIR,
                language="python",
                project_name="python-flask-query-fixture",
            )
            yield server
    except JoernError as exc:
        pytest.skip(str(exc))


@pytest.fixture(scope="module")
def python_django_joern_server() -> Generator[JoernServer, None, None]:
    if not is_joern_installed():
        pytest.skip("Joern is not installed in PATH")

    try:
        with JoernServer(
            port=_find_free_port(),
            startup_timeout_seconds=30,
            query_timeout_seconds=30,
        ) as server:
            server.import_project(
                PYTHON_DJANGO_FIXTURES_DIR,
                language="python",
                project_name="python-django-query-fixture",
            )
            yield server
    except JoernError as exc:
        pytest.skip(str(exc))


@pytest.mark.joern
@pytest.mark.integration
def test_builtin_source_queries_detect_expected_patterns(joern_server: JoernServer) -> None:
    source_specs = get_source_specs()

    request_body = execute_source_query(
        joern_server,
        _source_spec_by_name(source_specs, "express_req_body"),
    )
    request_query = execute_source_query(
        joern_server,
        _source_spec_by_name(source_specs, "express_req_query"),
    )
    request_params = execute_source_query(
        joern_server,
        _source_spec_by_name(source_specs, "express_req_params"),
    )
    request_headers = execute_source_query(
        joern_server,
        _source_spec_by_name(source_specs, "express_req_headers"),
    )
    request_cookies = execute_source_query(
        joern_server,
        _source_spec_by_name(source_specs, "express_req_cookies"),
    )
    process_env = execute_source_query(
        joern_server,
        _source_spec_by_name(source_specs, "process_env"),
    )
    url_source = execute_source_query(
        joern_server,
        _source_spec_by_name(source_specs, "url_and_url_search_params"),
    )

    assert {"req.body.user"} <= _codes(request_body)
    assert {"req.query.id", "req.query.cmd"} <= _codes(request_query)
    assert {"req.params.slug", "req.params.file"} <= _codes(request_params)
    assert {'req.headers["authorization"]'} <= _codes(request_headers)
    assert {"req.cookies.sid"} <= _codes(request_cookies)
    assert {"process.env.TOKEN"} <= _codes(process_env)
    assert {"new URL(req.url)", "new URLSearchParams(req.query)"} <= _codes(url_source)


@pytest.mark.joern
@pytest.mark.integration
def test_builtin_fastify_source_queries_detect_expected_patterns(joern_server: JoernServer) -> None:
    source_specs = get_source_specs(frameworks=("fastify",))

    request_body = execute_source_query(
        joern_server,
        _source_spec_by_name(source_specs, "fastify_request_body"),
    )
    request_query = execute_source_query(
        joern_server,
        _source_spec_by_name(source_specs, "fastify_request_query"),
    )
    request_params = execute_source_query(
        joern_server,
        _source_spec_by_name(source_specs, "fastify_request_params"),
    )
    request_headers = execute_source_query(
        joern_server,
        _source_spec_by_name(source_specs, "fastify_request_headers"),
    )

    assert {"request.body.user"} <= _codes(request_body)
    assert {"request.query.id"} <= _codes(request_query)
    assert {"request.params.slug"} <= _codes(request_params)
    assert {'request.headers["authorization"]'} <= _codes(request_headers)


@pytest.mark.joern
@pytest.mark.integration
def test_builtin_nestjs_source_queries_detect_expected_patterns(joern_server: JoernServer) -> None:
    source_specs = get_source_specs(frameworks=("nestjs",))

    request_body = execute_source_query(
        joern_server,
        _source_spec_by_name(source_specs, "nestjs_body"),
    )
    request_param = execute_source_query(
        joern_server,
        _source_spec_by_name(source_specs, "nestjs_param"),
    )
    url_param = execute_source_query(
        joern_server,
        _source_spec_by_name(source_specs, "nestjs_query"),
    )
    header = execute_source_query(
        joern_server,
        _source_spec_by_name(source_specs, "nestjs_headers"),
    )
    request = execute_source_query(
        joern_server,
        _source_spec_by_name(source_specs, "nestjs_req"),
    )

    assert {"payload"} <= _codes(request_body)
    assert {"id"} <= _codes(request_param)
    assert {"term"} <= _codes(url_param)
    assert {"auth"} <= _codes(header)
    assert {"req"} <= _codes(request)


@pytest.mark.joern
@pytest.mark.integration
def test_builtin_sink_queries_detect_expected_patterns(joern_server: JoernServer) -> None:
    sink_specs = get_sink_specs()

    sink_expectations = {
        "raw_sql_query": {
            "db.query(userId)",
            "prisma.$queryRaw(userId)",
            "prisma.$executeRaw(userId)",
            "sql.raw(userId)",
        },
        "child_process_exec": {"child.exec(cmd)", "child.execSync(cmd)"},
        "child_process_spawn": {"child.spawn(cmd)", "child.spawnSync(cmd)"},
        "dynamic_eval": {"eval(script)", "Function(script)"},
        "dangerously_set_inner_html": {"dangerouslySetInnerHTML(markup)"},
        "response_output": {"res.send(markup)", "res.render(markup)", "res.write(markup)"},
        "filesystem_read": {"fs.readFile(pathValue)", "fs.readFileSync(pathValue)"},
        "filesystem_write": {
            "fs.writeFile(pathValue, markup)",
            "fs.writeFileSync(pathValue, markup)",
        },
        "ssrf_full_url": {
            "fetch(url)",
            "axios.get(url)",
            "axios.post(url)",
            "http.get(url)",
            "https.get(url)",
            "needle.get(url)",
            "request(url)",
            "request.get(url)",
        },
        "ssrf_path_segment": {
            "fetch(`https://internal.service.local/api/users/${userId}`)",
        },
    }

    for spec_name, expected_codes in sink_expectations.items():
        sink_nodes = execute_sink_query(joern_server, _sink_spec_by_name(sink_specs, spec_name))
        assert expected_codes <= _codes(sink_nodes)

    ssrf_full_url_nodes = execute_sink_query(
        joern_server, _sink_spec_by_name(sink_specs, "ssrf_full_url")
    )
    ssrf_path_nodes = execute_sink_query(
        joern_server, _sink_spec_by_name(sink_specs, "ssrf_path_segment")
    )
    assert 'app.get("/health", handlers.health)' not in _codes(ssrf_full_url_nodes)
    assert 'app.post("/users", handlers.createUser)' not in _codes(ssrf_full_url_nodes)
    assert 'router.get("/health", handlers.health)' not in _codes(ssrf_full_url_nodes)
    assert 'router.post("/users", handlers.createUser)' not in _codes(ssrf_full_url_nodes)
    assert 'app.get("/health", handlers.health)' not in _codes(ssrf_path_nodes)
    assert 'router.get("/health", handlers.health)' not in _codes(ssrf_path_nodes)


@pytest.mark.joern
@pytest.mark.integration
def test_builtin_fastify_sink_queries_detect_expected_patterns(joern_server: JoernServer) -> None:
    sink_specs = get_sink_specs(frameworks=("fastify",))

    reply_send = execute_sink_query(
        joern_server,
        _sink_spec_by_name(sink_specs, "fastify_reply_send"),
    )
    reply_header = execute_sink_query(
        joern_server,
        _sink_spec_by_name(sink_specs, "fastify_reply_header"),
    )

    assert {"reply.send(markup)"} <= _codes(reply_send)
    assert {'reply.header("Location", redirectTo)'} <= _codes(reply_header)


@pytest.mark.joern
@pytest.mark.integration
def test_builtin_python_sink_queries_detect_expected_patterns(
    python_flask_joern_server: JoernServer,
    python_django_joern_server: JoernServer,
) -> None:
    sink_specs = get_sink_specs(frameworks=("flask", "django"))
    sanitizer_specs = get_sanitizer_specs(frameworks=("flask",))

    sql_sink = _sink_spec_by_name(sink_specs, "python_sql_execute")
    system_sink = _sink_spec_by_name(sink_specs, "python_os_system")
    subprocess_sink = _sink_spec_by_name(sink_specs, "python_subprocess_run")
    eval_sink = _sink_spec_by_name(sink_specs, "python_eval")
    parameterized_query = _sanitizer_spec_by_name(sanitizer_specs, "python_parameterized_query")

    flask_sql = execute_sink_query(python_flask_joern_server, sql_sink)
    flask_system = execute_sink_query(python_flask_joern_server, system_sink)
    flask_subprocess = execute_sink_query(python_flask_joern_server, subprocess_sink)
    flask_eval = execute_sink_query(python_flask_joern_server, eval_sink)
    flask_parameterized = execute_sanitizer_query(python_flask_joern_server, parameterized_query)
    django_sql = execute_sink_query(python_django_joern_server, sql_sink)

    assert {"cursor.execute(f\"SELECT * FROM items WHERE name = '{q}'\")"} <= _codes(flask_sql)
    assert {"os.system(cmd)"} <= _codes(flask_system)
    assert {"subprocess.run(cmd, shell = True)"} <= _codes(flask_subprocess)
    assert 'subprocess.run(["echo", cmd], shell = False)' not in _codes(flask_subprocess)
    assert {"eval(expr)"} <= _codes(flask_eval)
    assert {'cursor.execute("SELECT * FROM items WHERE name = ?", (q,))'} <= _codes(
        flask_parameterized
    )
    assert {"User.objects.raw(f\"SELECT * FROM users WHERE name = '{q}'\")"} <= _codes(django_sql)
    assert {"User.objects.extra(where = [f\"name = '{q}'\"])"} <= _codes(django_sql)
    assert "User.objects.filter(name = q)" not in _codes(django_sql)
    assert "User.objects.filter(Q(name = q))" not in _codes(django_sql)
    assert 'User.objects.filter(score__gt = F("min_score"))' not in _codes(django_sql)


@pytest.mark.joern
@pytest.mark.integration
def test_ssrf_flow_queries_distinguish_full_url_from_hardcoded_base_templates(
    joern_server: JoernServer,
) -> None:
    source_specs = get_source_specs()
    sink_specs = get_sink_specs()

    request_query = _source_spec_by_name(source_specs, "express_req_query")
    request_body = _source_spec_by_name(source_specs, "express_req_body")
    full_url_sink = _sink_spec_by_name(sink_specs, "ssrf_full_url")
    path_segment_sink = _sink_spec_by_name(sink_specs, "ssrf_path_segment")

    full_url_flows = execute_flow_query(
        joern_server,
        request_query,
        full_url_sink,
        sanitizer_specs=BUILTIN_SANITIZER_SPECS,
    )
    path_segment_flows = execute_flow_query(
        joern_server,
        request_query,
        path_segment_sink,
        sanitizer_specs=BUILTIN_SANITIZER_SPECS,
    )
    forwarded_body_flows = execute_flow_query(
        joern_server,
        request_body,
        full_url_sink,
        sanitizer_specs=BUILTIN_SANITIZER_SPECS,
    )

    assert any(flow.elements[-1].code == "fetch(url)" for flow in full_url_flows)
    assert any(
        flow.elements[-1].code == "fetch(`https://internal.service.local/api/users/${userId}`)"
        for flow in path_segment_flows
    )
    assert any(flow.elements[-1].code == "axios.get(endpoint)" for flow in path_segment_flows)
    assert any(flow.elements[-1].code == "http.get(endpoint)" for flow in path_segment_flows)
    assert all(
        flow.elements[-1].code
        != 'axios.post("https://internal.service.local/api/users", req.body.payload)'
        for flow in forwarded_body_flows
    )


@pytest.mark.joern
@pytest.mark.integration
def test_flow_queries_filter_known_sanitizers_and_keep_unsafe_flows(
    joern_server: JoernServer,
) -> None:
    source_specs = get_source_specs()
    sink_specs = get_sink_specs()

    request_body = _source_spec_by_name(source_specs, "express_req_body")
    request_query = _source_spec_by_name(source_specs, "express_req_query")
    request_params = _source_spec_by_name(source_specs, "express_req_params")
    sql_sink = _sink_spec_by_name(sink_specs, "raw_sql_query")
    exec_sink = _sink_spec_by_name(sink_specs, "child_process_exec")
    file_read_sink = _sink_spec_by_name(sink_specs, "filesystem_read")

    sql_flows = execute_flow_query(
        joern_server,
        request_body,
        sql_sink,
        sanitizer_specs=BUILTIN_SANITIZER_SPECS,
    )
    exec_flows = execute_flow_query(
        joern_server,
        request_query,
        exec_sink,
        sanitizer_specs=BUILTIN_SANITIZER_SPECS,
    )
    file_read_flows = execute_flow_query(
        joern_server,
        request_params,
        file_read_sink,
        sanitizer_specs=BUILTIN_SANITIZER_SPECS,
    )

    assert any("db.query(queryText)" in _codes(flow.elements) for flow in sql_flows)
    assert all("escape(userId)" not in _codes(flow.elements) for flow in sql_flows)
    assert all("parameterize(userId)" not in _codes(flow.elements) for flow in sql_flows)
    assert any("child.exec(cmd)" in _codes(flow.elements) for flow in exec_flows)
    assert file_read_flows == ()


def test_execute_flow_query_keeps_paths_for_confidence_only_sanitizers() -> None:
    source_spec = SourceSpec(
        name="fastify_request_body",
        pattern='cpg.call.name("<operator>.fieldAccess").code("request[.]body.*")',
        source_type=SourceType.REQUEST_BODY,
    )
    sink_spec = SinkSpec(
        name="fastify_reply_send",
        pattern='cpg.call.name("send").code(".*reply[.]send[(].*")',
        sink_type=SinkType.HTML_OUTPUT,
        cwe_id="CWE-79",
    )
    sanitizer_spec = SanitizerSpec(
        name="fastify_schema_validation",
        pattern='cpg.call.name("__fastify_schema__")',
        kind=SanitizerKind.NORMALIZE,
        confidence=0.25,
        blocks_flow=False,
    )
    flow_query = build_flow_query(source_spec, sink_spec)
    sanitizer_query = build_nodes_query(sanitizer_spec.pattern)
    server = FakeJoernServer(
        {
            flow_query: {
                "success": True,
                "stdout": _joern_json_stdout(
                    [
                        {
                            "elements": [
                                {
                                    "_id": 1,
                                    "_label": "CALL",
                                    "code": "request.body.name",
                                    "name": "<operator>.fieldAccess",
                                },
                                {
                                    "_id": 2,
                                    "_label": "IDENTIFIER",
                                    "code": "name",
                                    "name": "name",
                                },
                                {
                                    "_id": 3,
                                    "_label": "CALL",
                                    "code": "reply.send(name)",
                                    "name": "send",
                                },
                            ]
                        }
                    ]
                ),
            },
            sanitizer_query: {
                "success": True,
                "stdout": _joern_json_stdout(
                    [
                        {
                            "_id": 1,
                            "_label": "CALL",
                            "code": "request.body.name",
                            "name": "<operator>.fieldAccess",
                        }
                    ]
                ),
            },
        }
    )

    flows = execute_flow_query(server, source_spec, sink_spec, sanitizer_specs=(sanitizer_spec,))  # type: ignore[arg-type]

    assert len(flows) == 1
    assert [element.code for element in flows[0].elements] == [
        "request.body.name",
        "name",
        "reply.send(name)",
    ]
    assert server.queries == [flow_query]


@pytest.mark.joern
@pytest.mark.integration
def test_custom_source_and_sink_patterns_from_config_find_custom_flow(
    joern_server: JoernServer,
    config_file: Any,
) -> None:
    path = config_file(
        "\n".join(
            [
                "[scan.custom_sources]",
                "patterns = ['cpg.call.name(\"customInput\")']",
                "",
                "[scan.custom_sinks]",
                "patterns = ['cpg.call.name(\"customDangerous\")']",
            ]
        )
    )

    config = load_config(path)
    custom_source = next(spec for spec in get_source_specs(config.scan) if spec.is_custom)
    custom_sink = next(spec for spec in get_sink_specs(config.scan) if spec.is_custom)

    flows = execute_flow_query(
        joern_server,
        custom_source,
        custom_sink,
        sanitizer_specs=BUILTIN_SANITIZER_SPECS,
    )

    assert len(flows) == 1
    codes = [element.code for element in flows[0].elements]
    assert codes[0] == "customInput()"
    assert "taint" in codes
    assert codes[-1] == "customDangerous(taint)"
