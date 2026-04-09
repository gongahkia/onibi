from __future__ import annotations

import json
from pathlib import Path

import pytest

from piranesi.detect.flows import (
    _NodeFileResolver,
    candidate_finding_id,
    extract_candidate_findings,
    joern_flow_to_taint_steps,
)
from piranesi.scan.joern import JoernServer, is_joern_installed
from piranesi.scan.queries import QueryNode, build_flow_query, build_nodes_query
from piranesi.scan.specs import (
    SanitizerSpec,
    SinkSpec,
    SourceSpec,
    get_sanitizer_specs,
    get_sink_specs,
    get_source_specs,
)
from piranesi.scan.transpile import SourceMap

TAINT_APP_DIR = Path(__file__).resolve().parents[1] / "fixtures" / "typescript" / "taint_app"
TAINT_APP_TRANSPILED_DIR = TAINT_APP_DIR / "transpiled"


class FakeJoernServer:
    def __init__(self, responses: dict[str, dict[str, object]]) -> None:
        self.responses = responses
        self.queries: list[str] = []

    def query(self, cpgql: str) -> dict[str, object]:
        self.queries.append(cpgql)
        default_response = {"success": True, "stdout": 'val res0: String = "[]"'}
        return self.responses.get(cpgql, default_response).copy()


def _joern_json_stdout(payload: object) -> str:
    return f'val res0: String = """{json.dumps(payload, indent=2)}"""'


def _source_spec_by_name(name: str) -> SourceSpec:
    return next(spec for spec in get_source_specs() if spec.name == name)


def _sink_spec_by_name(name: str) -> SinkSpec:
    return next(spec for spec in get_sink_specs() if spec.name == name)


def _sanitizer_spec_by_name(name: str) -> SanitizerSpec:
    return next(spec for spec in get_sanitizer_specs() if spec.name == name)


def test_extract_candidate_findings_suppresses_sanitized_paths() -> None:
    source_map = SourceMap.from_directory(TAINT_APP_TRANSPILED_DIR)
    source_spec = _source_spec_by_name("express_req_body")
    sql_sink = _sink_spec_by_name("raw_sql_query")
    xss_sink = _sink_spec_by_name("response_output")
    sanitizer_spec = _sanitizer_spec_by_name("html_escape")

    sql_flow = [
        {
            "_id": 101,
            "_label": "CALL",
            "name": "<operator>.fieldAccess",
            "code": "req.body.user",
            "lineNumber": 8,
            "columnNumber": 17,
            "methodFullName": "<operator>.fieldAccess",
        },
        {
            "_id": 102,
            "_label": "IDENTIFIER",
            "name": "userId",
            "code": "userId",
            "lineNumber": 8,
            "columnNumber": 8,
        },
        {
            "_id": 103,
            "_label": "CALL",
            "name": "<operator>.addition",
            "code": "\"SELECT * FROM users WHERE id = '\" + userId + \"'\"",
            "lineNumber": 9,
            "columnNumber": 22,
            "methodFullName": "<operator>.addition",
        },
        {
            "_id": 104,
            "_label": "IDENTIFIER",
            "name": "unsafeQuery",
            "code": "unsafeQuery",
            "lineNumber": 10,
            "columnNumber": 11,
        },
        {
            "_id": 105,
            "_label": "CALL",
            "name": "query",
            "code": "db.query(unsafeQuery)",
            "lineNumber": 10,
            "columnNumber": 2,
            "methodFullName": "<unknownFullName>",
        },
    ]
    xss_flow = [
        {
            "_id": 201,
            "_label": "CALL",
            "name": "<operator>.fieldAccess",
            "code": "req.body.user",
            "lineNumber": 8,
            "columnNumber": 17,
            "methodFullName": "<operator>.fieldAccess",
        },
        {
            "_id": 202,
            "_label": "IDENTIFIER",
            "name": "userId",
            "code": "userId",
            "lineNumber": 8,
            "columnNumber": 8,
        },
        {
            "_id": 203,
            "_label": "CALL",
            "name": "escape",
            "code": "escape(userId)",
            "lineNumber": 11,
            "columnNumber": 22,
            "methodFullName": "escape",
        },
        {
            "_id": 204,
            "_label": "IDENTIFIER",
            "name": "safeMarkup",
            "code": "safeMarkup",
            "lineNumber": 11,
            "columnNumber": 8,
        },
        {
            "_id": 205,
            "_label": "CALL",
            "name": "send",
            "code": "res.send(safeMarkup)",
            "lineNumber": 12,
            "columnNumber": 2,
            "methodFullName": "<unknownFullName>",
        },
    ]

    responses: dict[str, dict[str, object]] = {
        build_flow_query(source_spec, sql_sink): {
            "success": True,
            "stdout": _joern_json_stdout([{"elements": sql_flow}]),
        },
        build_flow_query(source_spec, xss_sink): {
            "success": True,
            "stdout": _joern_json_stdout([{"elements": xss_flow}]),
        },
        build_nodes_query(sanitizer_spec.pattern): {
            "success": True,
            "stdout": _joern_json_stdout(
                [
                    {
                        "_id": 203,
                        "_label": "CALL",
                        "name": "escape",
                        "code": "escape(userId)",
                        "lineNumber": 11,
                        "columnNumber": 22,
                        "methodFullName": "escape",
                    }
                ]
            ),
        },
    }
    for node_id in [101, 102, 103, 104, 105, 201, 202, 203, 204, 205]:
        responses[f"cpg.id({node_id}L).file.name.toJsonPretty"] = {
            "success": True,
            "stdout": _joern_json_stdout(["app.js"]),
        }

    findings = extract_candidate_findings(
        FakeJoernServer(responses),
        joern_project_root=TAINT_APP_TRANSPILED_DIR,
        source_map=source_map,
        source_specs=(source_spec,),
        sink_specs=(sql_sink, xss_sink),
        sanitizer_specs=(sanitizer_spec,),
    )

    assert len(findings) == 1

    sql_finding = next(finding for finding in findings if finding.vuln_class == "CWE-89")
    assert sql_finding.source.location.file.endswith("app.ts")
    assert sql_finding.source.location.line == 8
    assert sql_finding.source.parameter_name == "user"
    assert sql_finding.sink.api_name == "db.query"
    assert sql_finding.severity == "high"
    assert [step.operation for step in sql_finding.taint_path] == [
        "call_arg",
        "assignment",
        "call_arg",
        "assignment",
        "call_arg",
    ]
    assert all(step.taint_state == "tainted" for step in sql_finding.taint_path)
    assert sql_finding.id == candidate_finding_id(
        vuln_class="CWE-89",
        source_location=sql_finding.source.location,
        sink_location=sql_finding.sink.location,
    )

    assert all(finding.vuln_class != "CWE-79" for finding in findings)


def test_joern_flow_to_taint_steps_marks_steps_after_sanitizer() -> None:
    source_map = SourceMap.from_directory(TAINT_APP_TRANSPILED_DIR)
    xss_flow = [
        {
            "_id": 201,
            "_label": "CALL",
            "name": "<operator>.fieldAccess",
            "code": "req.body.user",
            "lineNumber": 8,
            "columnNumber": 17,
            "methodFullName": "<operator>.fieldAccess",
        },
        {
            "_id": 202,
            "_label": "IDENTIFIER",
            "name": "userId",
            "code": "userId",
            "lineNumber": 8,
            "columnNumber": 8,
        },
        {
            "_id": 203,
            "_label": "CALL",
            "name": "escape",
            "code": "escape(userId)",
            "lineNumber": 11,
            "columnNumber": 22,
            "methodFullName": "escape",
        },
        {
            "_id": 204,
            "_label": "IDENTIFIER",
            "name": "safeMarkup",
            "code": "safeMarkup",
            "lineNumber": 11,
            "columnNumber": 8,
        },
        {
            "_id": 205,
            "_label": "CALL",
            "name": "send",
            "code": "res.send(safeMarkup)",
            "lineNumber": 12,
            "columnNumber": 2,
            "methodFullName": "<unknownFullName>",
        },
    ]
    responses: dict[str, dict[str, object]] = {}
    for node_id in [201, 202, 203, 204, 205]:
        responses[f"cpg.id({node_id}L).file.name.toJsonPretty"] = {
            "success": True,
            "stdout": _joern_json_stdout(["app.js"]),
        }

    server = FakeJoernServer(responses)
    file_resolver = _NodeFileResolver(
        server=server,
        joern_project_root=TAINT_APP_TRANSPILED_DIR,
        source_map=source_map,
    )
    steps = joern_flow_to_taint_steps(
        [QueryNode.from_json(node) for node in xss_flow],
        source_map=source_map,
        file_resolver=file_resolver,
        sanitizer_lookup={203: "escape"},
    )

    assert [step.taint_state for step in steps] == [
        "tainted",
        "tainted",
        "sanitized",
        "sanitized",
        "sanitized",
    ]
    assert [step.sanitizer_applied for step in steps][-3:] == [
        "escape",
        "escape",
        "escape",
    ]


@pytest.fixture(scope="module")
def joern_server() -> JoernServer:
    if not is_joern_installed():
        pytest.skip("Joern is not installed in PATH")

    with JoernServer(startup_timeout_seconds=30, query_timeout_seconds=30) as server:
        server.import_project(TAINT_APP_TRANSPILED_DIR)
        yield server


@pytest.mark.joern
@pytest.mark.integration
def test_extract_candidate_findings_with_real_joern(joern_server: JoernServer) -> None:
    source_map = SourceMap.from_directory(TAINT_APP_TRANSPILED_DIR)
    body_source = _source_spec_by_name("express_req_body")
    query_source = _source_spec_by_name("express_req_query")
    sql_sink = _sink_spec_by_name("raw_sql_query")
    xss_sink = _sink_spec_by_name("response_output")
    exec_sink = _sink_spec_by_name("child_process_exec")
    sanitizer_spec = _sanitizer_spec_by_name("html_escape")

    findings = extract_candidate_findings(
        joern_server,
        joern_project_root=TAINT_APP_TRANSPILED_DIR,
        source_map=source_map,
        source_specs=(body_source, query_source),
        sink_specs=(sql_sink, xss_sink, exec_sink),
        sanitizer_specs=(sanitizer_spec,),
    )

    assert len(findings) == 2

    sql_finding = next(finding for finding in findings if finding.vuln_class == "CWE-89")
    assert sql_finding.source.location.file.endswith("app.ts")
    assert sql_finding.source.location.line == 8
    assert sql_finding.sink.location.line == 10
    assert sql_finding.sink.api_name == "db.query"
    assert all(step.taint_state == "tainted" for step in sql_finding.taint_path)

    command_finding = next(finding for finding in findings if finding.vuln_class == "CWE-78")
    assert command_finding.source.parameter_name == "cmd"
    assert command_finding.source.location.line == 16
    assert command_finding.sink.api_name == "child.exec"
    assert command_finding.sink.location.line == 17
    assert all(finding.vuln_class != "CWE-79" for finding in findings)
