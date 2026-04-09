from __future__ import annotations

import json
from pathlib import Path

import pytest

from piranesi.config import load_config
from piranesi.scan.joern import JoernServer, is_joern_installed
from piranesi.scan.queries import (
    CPGQLQueryError,
    build_flow_query,
    build_nodes_query,
    execute_flow_query,
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
    get_sink_specs,
    get_source_specs,
)

SCAN_QUERY_FIXTURES_DIR = Path(__file__).resolve().parents[1] / "fixtures" / "scan_queries"


class FakeJoernServer:
    def __init__(self, responses: dict[str, dict[str, object]]) -> None:
        self.responses = responses
        self.queries: list[str] = []

    def query(self, cpgql: str) -> dict[str, object]:
        self.queries.append(cpgql)
        return self.responses.get(cpgql, {"success": True, "stdout": 'val res0: String = "[]"'}).copy()


def _joern_json_stdout(payload: object) -> str:
    return f'val res0: String = """{json.dumps(payload, indent=2)}"""'


def _source_spec_by_name(specs: tuple[SourceSpec, ...], name: str) -> SourceSpec:
    return next(spec for spec in specs if spec.name == name)


def _sink_spec_by_name(specs: tuple[SinkSpec, ...], name: str) -> SinkSpec:
    return next(spec for spec in specs if spec.name == name)


def _codes(elements: tuple[object, ...]) -> set[str]:
    return {getattr(element, "code") for element in elements}


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
                                {"_id": 1, "_label": "CALL", "code": "customInput()", "name": "customInput"},
                                {"_id": 2, "_label": "CALL", "code": "escape(taint)", "name": "escape"},
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
                                {"_id": 4, "_label": "CALL", "code": "customInput()", "name": "customInput"},
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

    flows = execute_flow_query(server, source_spec, sink_spec, sanitizer_specs=(sanitizer_spec,))

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
        execute_source_query(server, source_spec)


def test_scan_specs_include_custom_patterns_from_piranesi_toml(config_file) -> None:
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


@pytest.fixture(scope="module")
def joern_server() -> JoernServer:
    if not is_joern_installed():
        pytest.skip("Joern is not installed in PATH")

    with JoernServer(startup_timeout_seconds=30, query_timeout_seconds=30) as server:
        server.import_project(SCAN_QUERY_FIXTURES_DIR)
        yield server


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
        "filesystem_write": {"fs.writeFile(pathValue, markup)", "fs.writeFileSync(pathValue, markup)"},
        "http_request": {"fetch(url)", "axios.get(url)", "axios.post(url)", "request(url)"},
    }

    for spec_name, expected_codes in sink_expectations.items():
        sink_nodes = execute_sink_query(joern_server, _sink_spec_by_name(sink_specs, spec_name))
        assert expected_codes <= _codes(sink_nodes)


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


@pytest.mark.joern
@pytest.mark.integration
def test_custom_source_and_sink_patterns_from_config_find_custom_flow(
    joern_server: JoernServer,
    config_file,
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
