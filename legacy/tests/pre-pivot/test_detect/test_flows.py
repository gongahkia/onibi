from __future__ import annotations

import json
import re
from collections.abc import Generator
from pathlib import Path

import pytest

from piranesi.detect.flows import (
    _DEFAULT_CONFIDENCE,
    _NodeFileResolver,
    candidate_finding_id,
    extract_candidate_findings,
    joern_flow_to_taint_steps,
)
from piranesi.detect.sanitizer_validation import (
    PARTIAL_CONFIDENCE_REDUCTION,
    SanitizerEffectiveness,
)
from piranesi.scan.joern import JoernError, JoernServer, is_joern_installed
from piranesi.scan.queries import QueryNode, build_flow_query, build_nodes_query
from piranesi.scan.specs import (
    SanitizerSpec,
    SinkSpec,
    SinkType,
    SourceSpec,
    SourceType,
    get_sanitizer_specs,
    get_sink_specs,
    get_source_specs,
)
from piranesi.scan.transpile import SourceMap

TAINT_APP_DIR = Path(__file__).resolve().parents[1] / "fixtures" / "typescript" / "taint_app"
TAINT_APP_TRANSPILED_DIR = TAINT_APP_DIR / "transpiled"


class FakeJoernServer:
    def __init__(
        self,
        responses: dict[str, dict[str, object]] | None = None,
        *,
        exact_payloads: dict[str, object] | None = None,
        controlled_by: dict[int, list[dict[str, object]]] | None = None,
        method_controls: dict[str, list[dict[str, object]]] | None = None,
        branch_ast_ids: dict[tuple[int, int], list[int]] | None = None,
        node_methods: dict[int, list[dict[str, object]]] | None = None,
        branch_returns: dict[tuple[int, int], list[int]] | None = None,
        control_calls: dict[tuple[int, str], list[int]] | None = None,
    ) -> None:
        self.responses = responses or {}
        self.exact_payloads = exact_payloads or {}
        self.controlled_by = controlled_by or {}
        self.method_controls = method_controls or {}
        self.branch_ast_ids = branch_ast_ids or {}
        self.node_methods = node_methods or {}
        self.branch_returns = branch_returns or {}
        self.control_calls = control_calls or {}
        self.queries: list[str] = []

    def query(self, cpgql: str) -> dict[str, object]:
        self.queries.append(cpgql)
        if cpgql in self.responses:
            return self.responses[cpgql].copy()
        return {"success": True, "stdout": _joern_json_stdout(self._payload_for(cpgql))}

    def _payload_for(self, cpgql: str) -> object:
        if cpgql in self.exact_payloads:
            return self.exact_payloads[cpgql]

        controlled_by_match = re.fullmatch(
            r"cpg\.(?:call|identifier|parameter|methodReturn|fieldIdentifier|literal)\.id\((?P<node_id>\d+)L\)"
            r'\.controlledBy\.map\(c => Map\("'
            r'_id" -> c\.id, "methodName" -> c\.method\.name, '
            r'"methodFullName" -> c\.method\.fullName\)\)\.toJsonPretty',
            cpgql,
        )
        if controlled_by_match is not None:
            node_id = int(controlled_by_match.group("node_id"))
            return self.controlled_by.get(node_id, [])

        method_controls_match = re.fullmatch(
            r'cpg\.method\.fullNameExact\((?P<method>"(?:\\.|[^"\\])*")\)'
            r'\.ast\.isControlStructure\.map\(c => Map\("'
            r'_id" -> c\.id, "code" -> c\.code, "lineNumber" -> c\.lineNumber, '
            r'"columnNumber" -> c\.columnNumber, "controlStructureType" -> '
            r'c\.controlStructureType, "condition" -> c\.condition\.code\)\)\.toJsonPretty',
            cpgql,
        )
        if method_controls_match is not None:
            return self.method_controls.get(json.loads(method_controls_match.group("method")), [])

        branch_ast_match = re.fullmatch(
            r"cpg\.id\((?P<control_id>\d+)L\)\.astChildren\.order\((?P<order>\d+)\)\.ast\.id\.toJsonPretty",
            cpgql,
        )
        if branch_ast_match is not None:
            key = (int(branch_ast_match.group("control_id")), int(branch_ast_match.group("order")))
            return self.branch_ast_ids.get(key, [])

        node_method_match = re.fullmatch(
            r"cpg\.(?:call|identifier|parameter|methodReturn|fieldIdentifier|literal)\.id\((?P<node_id>\d+)L\)"
            r'\.method\.map\(m => Map\("name" -> m\.name, '
            r'"fullName" -> m\.fullName\)\)\.toJsonPretty',
            cpgql,
        )
        if node_method_match is not None:
            return self.node_methods.get(int(node_method_match.group("node_id")), [])

        branch_return_match = re.fullmatch(
            r"cpg\.id\((?P<control_id>\d+)L\)\.astChildren\.order\((?P<order>\d+)\)\.ast\.isReturn\.id\.toJsonPretty",
            cpgql,
        )
        if branch_return_match is not None:
            key = (
                int(branch_return_match.group("control_id")),
                int(branch_return_match.group("order")),
            )
            return self.branch_returns.get(key, [])

        control_call_match = re.fullmatch(
            r'cpg\.id\((?P<control_id>\d+)L\)\.condition\.ast\.isCallTo\((?P<call>"(?:\\.|[^"\\])*")\)\.id\.toJsonPretty',
            cpgql,
        )
        if control_call_match is not None:
            key = (
                int(control_call_match.group("control_id")),
                json.loads(control_call_match.group("call")),
            )
            return self.control_calls.get(key, [])  # type: ignore[arg-type]

        return []


def _joern_json_stdout(payload: object) -> str:
    return f'val res0: String = """{json.dumps(payload, indent=2)}"""'


def _source_spec_by_name(name: str) -> SourceSpec:
    return next(
        spec for spec in get_source_specs(frameworks=("express", "fastify")) if spec.name == name
    )


def _sink_spec_by_name(name: str) -> SinkSpec:
    return next(
        spec for spec in get_sink_specs(frameworks=("express", "fastify")) if spec.name == name
    )


def _sanitizer_spec_by_name(name: str) -> SanitizerSpec:
    return next(
        spec for spec in get_sanitizer_specs(frameworks=("express", "fastify")) if spec.name == name
    )


def _custom_source_spec(name: str) -> SourceSpec:
    return SourceSpec(
        name=name,
        pattern=f'cpg.call.name("{name}")',
        source_type=SourceType.CUSTOM,
    )


def _custom_sink_spec(
    name: str,
    *,
    sink_type: SinkType,
    cwe_id: str,
) -> SinkSpec:
    return SinkSpec(
        name=name,
        pattern=f'cpg.call.name("{name}")',
        sink_type=sink_type,
        cwe_id=cwe_id,
    )


def _node(
    node_id: int,
    *,
    label: str,
    name: str,
    code: str,
    line: int,
    column: int,
    method_full_name: str | None = None,
) -> dict[str, object]:
    payload: dict[str, object] = {
        "_id": node_id,
        "_label": label,
        "name": name,
        "code": code,
        "lineNumber": line,
        "columnNumber": column,
    }
    if method_full_name is not None:
        payload["methodFullName"] = method_full_name
    return payload


def _file_lookup_responses(
    *node_ids: int, file_name: str = "app.js"
) -> dict[str, dict[str, object]]:
    return {
        f"cpg.id({node_id}L).file.name.toJsonPretty": {
            "success": True,
            "stdout": _joern_json_stdout([file_name]),
        }
        for node_id in node_ids
    }


def _register_file_queries(
    exact_payloads: dict[str, object], file_path: Path, *node_ids: int
) -> None:
    for node_id in node_ids:
        exact_payloads[f"cpg.id({node_id}L).file.name.toJsonPretty"] = [str(file_path)]


def test_extract_candidate_findings_detects_reflected_cors_origin() -> None:
    source_spec = _source_spec_by_name("express_req_origin_header")
    sink_spec = _sink_spec_by_name("cors_allow_origin_reflection")
    exact_payloads: dict[str, object] = {}
    app_file = TAINT_APP_DIR / "app.ts"

    flow = [
        _node(
            201,
            label="CALL",
            name="<operator>.fieldAccess",
            code="req.headers.origin",
            line=4,
            column=18,
            method_full_name="<operator>.fieldAccess",
        ),
        _node(
            202,
            label="IDENTIFIER",
            name="origin",
            code="origin",
            line=4,
            column=9,
            method_full_name="app.js::program:corsHandler",
        ),
        _node(
            203,
            label="IDENTIFIER",
            name="origin",
            code="origin",
            line=5,
            column=50,
            method_full_name="app.js::program:corsHandler",
        ),
    ]
    parent_call = _node(
        204,
        label="CALL",
        name="setHeader",
        code="res.setHeader('Access-Control-Allow-Origin', origin)",
        line=5,
        column=3,
        method_full_name="app.js::program:corsHandler",
    )

    exact_payloads[build_flow_query(source_spec, sink_spec)] = [{"elements": flow}]
    exact_payloads["cpg.identifier.id(203L).astParent.toJsonPretty"] = [parent_call]
    _register_file_queries(exact_payloads, app_file, 201, 203, 204)

    findings = extract_candidate_findings(
        FakeJoernServer(exact_payloads=exact_payloads),  # type: ignore[arg-type]
        joern_project_root=TAINT_APP_DIR,
        source_map=None,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
        sanitizer_specs=(),
    )

    assert len(findings) == 1
    finding = findings[0]
    assert finding.vuln_class == "CWE-942"
    assert finding.source.parameter_name == "origin"
    assert finding.sink.api_name == "res.setHeader"
    assert finding.sink.location.line == 5
    assert finding.severity == "high"


def test_extract_candidate_findings_detects_local_query_helper_sqli() -> None:
    source_spec = _source_spec_by_name("express_req_query")
    sink_spec = _sink_spec_by_name("raw_sql_query")
    exact_payloads: dict[str, object] = {}
    app_file = Path("examples/vuln-express/app.js").resolve()

    flow = [
        _node(
            301,
            label="CALL",
            name="<operator>.fieldAccess",
            code="req.query.name",
            line=34,
            column=16,
            method_full_name="<operator>.fieldAccess",
        ),
        _node(
            302,
            label="IDENTIFIER",
            name="name",
            code="name",
            line=34,
            column=9,
            method_full_name="app.js::program:usersHandler",
        ),
        _node(
            303,
            label="CALL",
            name="<operator>.formatString",
            code="`SELECT * FROM users WHERE name = '${name}'`",
            line=35,
            column=22,
            method_full_name="<operator>.formatString",
        ),
        _node(
            304,
            label="CALL",
            name="query",
            code="query(`SELECT * FROM users WHERE name = '${name}'`)",
            line=35,
            column=16,
            method_full_name="app.js::program:query",
        ),
    ]

    exact_payloads[build_flow_query(source_spec, sink_spec)] = [{"elements": flow}]
    _register_file_queries(exact_payloads, app_file, 301, 304)

    findings = extract_candidate_findings(
        FakeJoernServer(exact_payloads=exact_payloads),  # type: ignore[arg-type]
        joern_project_root=Path.cwd(),
        source_map=None,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
        sanitizer_specs=(),
    )

    assert len(findings) == 1
    finding = findings[0]
    assert finding.vuln_class == "CWE-89"
    assert finding.source.parameter_name == "name"
    assert finding.sink.api_name == "query"
    assert finding.sink.location.line == 35
    assert finding.severity == "high"


def test_extract_candidate_findings_reduces_confidence_for_sanitized_paths() -> None:
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
            "code": '"SELECT * FROM users WHERE id = \'" + userId + "\'"',
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
    responses.update(_file_lookup_responses(101, 102, 103, 104, 105, 201, 202, 203, 204, 205))

    findings = extract_candidate_findings(
        FakeJoernServer(responses),  # type: ignore[arg-type]
        joern_project_root=TAINT_APP_TRANSPILED_DIR,
        source_map=source_map,
        source_specs=(source_spec,),
        sink_specs=(sql_sink, xss_sink),
        sanitizer_specs=(sanitizer_spec,),
    )

    assert len(findings) == 2

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
        source_function_name=sql_finding.taint_path[0].through_function,
        sink_function_name=sql_finding.taint_path[-1].through_function,
        path_length=len(sql_finding.taint_path),
    )
    assert sql_finding.confidence == pytest.approx(_DEFAULT_CONFIDENCE)

    xss_finding = next(finding for finding in findings if finding.vuln_class == "CWE-79")
    assert xss_finding.sink.api_name == "res.send"
    assert xss_finding.confidence == pytest.approx(_DEFAULT_CONFIDENCE)
    assert xss_finding.suppressed is True
    assert xss_finding.suppression_reason == "effective sanitizer for CWE-79: escape"
    assert [step.taint_state for step in xss_finding.taint_path][-3:] == [
        "sanitized",
        "sanitized",
        "sanitized",
    ]
    assert [step.sanitizer_applied for step in xss_finding.taint_path][-3:] == [
        "escape",
        "escape",
        "escape",
    ]


def test_candidate_finding_id_is_stable_across_line_shifts() -> None:
    baseline = candidate_finding_id(
        vuln_class="CWE-89",
        source_function_name="routes.login",
        sink_function_name="db.query",
        path_length=4,
    )
    shifted = candidate_finding_id(
        vuln_class="CWE-89",
        source_function_name="routes.login",
        sink_function_name="db.query",
        path_length=4,
    )
    different_shape = candidate_finding_id(
        vuln_class="CWE-89",
        source_function_name="routes.login",
        sink_function_name="db.query",
        path_length=5,
    )

    assert baseline == shifted
    assert baseline != different_shape


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
        server=server,  # type: ignore[arg-type]
        joern_project_root=TAINT_APP_TRANSPILED_DIR,
        source_map=source_map,
    )
    steps = joern_flow_to_taint_steps(
        [QueryNode.from_json(node) for node in xss_flow],
        source_map=source_map,
        file_resolver=file_resolver,
        sanitizer_lookup={203: _sanitizer_spec_by_name("html_escape")},
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


@pytest.mark.parametrize(
    (
        "sanitizer_name",
        "source_name",
        "sink_name",
        "expected_effectiveness",
        "flow",
        "sanitizer_nodes",
    ),
    [
        (
            "validator_escape",
            "express_req_body",
            "response_output",
            SanitizerEffectiveness.EFFECTIVE,
            [
                _node(
                    1001,
                    label="CALL",
                    name="<operator>.fieldAccess",
                    code="req.body.comment",
                    line=4,
                    column=12,
                    method_full_name="<operator>.fieldAccess",
                ),
                _node(1002, label="IDENTIFIER", name="comment", code="comment", line=4, column=8),
                _node(
                    1003,
                    label="CALL",
                    name="escape",
                    code="validator.escape(comment)",
                    line=5,
                    column=20,
                    method_full_name="validator.escape",
                ),
                _node(
                    1004,
                    label="IDENTIFIER",
                    name="safeComment",
                    code="safeComment",
                    line=5,
                    column=8,
                ),
                _node(
                    1005,
                    label="CALL",
                    name="send",
                    code="res.send(safeComment)",
                    line=6,
                    column=2,
                    method_full_name="<unknownFullName>",
                ),
            ],
            [
                _node(
                    1003,
                    label="CALL",
                    name="escape",
                    code="validator.escape(comment)",
                    line=5,
                    column=20,
                    method_full_name="validator.escape",
                ),
            ],
        ),
        (
            "sanitize_html",
            "express_req_body",
            "response_output",
            SanitizerEffectiveness.EFFECTIVE,
            [
                _node(
                    1101,
                    label="CALL",
                    name="<operator>.fieldAccess",
                    code="req.body.comment",
                    line=4,
                    column=12,
                    method_full_name="<operator>.fieldAccess",
                ),
                _node(1102, label="IDENTIFIER", name="comment", code="comment", line=4, column=8),
                _node(
                    1103,
                    label="CALL",
                    name="sanitizeHtml",
                    code="sanitizeHtml(comment)",
                    line=5,
                    column=20,
                    method_full_name="sanitizeHtml",
                ),
                _node(
                    1104,
                    label="IDENTIFIER",
                    name="safeComment",
                    code="safeComment",
                    line=5,
                    column=8,
                ),
                _node(
                    1105,
                    label="CALL",
                    name="send",
                    code="res.send(safeComment)",
                    line=6,
                    column=2,
                    method_full_name="<unknownFullName>",
                ),
            ],
            [
                _node(
                    1103,
                    label="CALL",
                    name="sanitizeHtml",
                    code="sanitizeHtml(comment)",
                    line=5,
                    column=20,
                    method_full_name="sanitizeHtml",
                ),
            ],
        ),
        (
            "dompurify_sanitize",
            "express_req_body",
            "response_output",
            SanitizerEffectiveness.EFFECTIVE,
            [
                _node(
                    1201,
                    label="CALL",
                    name="<operator>.fieldAccess",
                    code="req.body.comment",
                    line=4,
                    column=12,
                    method_full_name="<operator>.fieldAccess",
                ),
                _node(1202, label="IDENTIFIER", name="comment", code="comment", line=4, column=8),
                _node(
                    1203,
                    label="CALL",
                    name="sanitize",
                    code="DOMPurify.sanitize(comment)",
                    line=5,
                    column=20,
                    method_full_name="DOMPurify.sanitize",
                ),
                _node(
                    1204,
                    label="IDENTIFIER",
                    name="safeComment",
                    code="safeComment",
                    line=5,
                    column=8,
                ),
                _node(
                    1205,
                    label="CALL",
                    name="send",
                    code="res.send(safeComment)",
                    line=6,
                    column=2,
                    method_full_name="<unknownFullName>",
                ),
            ],
            [
                _node(
                    1203,
                    label="CALL",
                    name="sanitize",
                    code="DOMPurify.sanitize(comment)",
                    line=5,
                    column=20,
                    method_full_name="DOMPurify.sanitize",
                ),
            ],
        ),
        (
            "sqlstring_escape",
            "express_req_body",
            "raw_sql_query",
            SanitizerEffectiveness.PARTIAL,
            [
                _node(
                    1301,
                    label="CALL",
                    name="<operator>.fieldAccess",
                    code="req.body.userId",
                    line=4,
                    column=12,
                    method_full_name="<operator>.fieldAccess",
                ),
                _node(1302, label="IDENTIFIER", name="userId", code="userId", line=4, column=8),
                _node(
                    1303,
                    label="CALL",
                    name="escape",
                    code="sqlstring.escape(userId)",
                    line=5,
                    column=20,
                    method_full_name="sqlstring.escape",
                ),
                _node(
                    1304, label="IDENTIFIER", name="safeUserId", code="safeUserId", line=5, column=8
                ),
                _node(
                    1305,
                    label="CALL",
                    name="query",
                    code='db.query("SELECT * FROM users WHERE id = \'" + safeUserId + "\'")',
                    line=6,
                    column=2,
                    method_full_name="<unknownFullName>",
                ),
            ],
            [
                _node(
                    1303,
                    label="CALL",
                    name="escape",
                    code="sqlstring.escape(userId)",
                    line=5,
                    column=20,
                    method_full_name="sqlstring.escape",
                ),
            ],
        ),
        (
            "pg_parameterized_query",
            "express_req_body",
            "raw_sql_query",
            SanitizerEffectiveness.EFFECTIVE,
            [
                _node(
                    1401,
                    label="CALL",
                    name="<operator>.fieldAccess",
                    code="req.body.userId",
                    line=4,
                    column=12,
                    method_full_name="<operator>.fieldAccess",
                ),
                _node(1402, label="IDENTIFIER", name="userId", code="userId", line=4, column=8),
                _node(
                    1403,
                    label="CALL",
                    name="query",
                    code='pool.query("SELECT * FROM users WHERE id = $1", [userId])',
                    line=6,
                    column=2,
                    method_full_name="<unknownFullName>",
                ),
            ],
            [
                _node(
                    1403,
                    label="CALL",
                    name="query",
                    code='pool.query("SELECT * FROM users WHERE id = $1", [userId])',
                    line=6,
                    column=2,
                    method_full_name="<unknownFullName>",
                ),
            ],
        ),
        (
            "pg_parameterized_query",
            "express_req_body",
            "response_output",
            SanitizerEffectiveness.INEFFECTIVE,
            [
                _node(
                    1451,
                    label="CALL",
                    name="<operator>.fieldAccess",
                    code="req.body.comment",
                    line=4,
                    column=12,
                    method_full_name="<operator>.fieldAccess",
                ),
                _node(1452, label="IDENTIFIER", name="comment", code="comment", line=4, column=8),
                _node(
                    1453,
                    label="CALL",
                    name="query",
                    code='pool.query("SELECT $1", [comment])',
                    line=5,
                    column=2,
                    method_full_name="<unknownFullName>",
                ),
                _node(
                    1454,
                    label="IDENTIFIER",
                    name="comment",
                    code="comment",
                    line=6,
                    column=8,
                ),
                _node(
                    1455,
                    label="CALL",
                    name="send",
                    code="res.send(comment)",
                    line=7,
                    column=2,
                    method_full_name="<unknownFullName>",
                ),
            ],
            [
                _node(
                    1453,
                    label="CALL",
                    name="query",
                    code='pool.query("SELECT $1", [comment])',
                    line=5,
                    column=2,
                    method_full_name="<unknownFullName>",
                ),
            ],
        ),
        (
            "path_resolve_startswith",
            "express_req_params",
            "filesystem_read",
            SanitizerEffectiveness.EFFECTIVE,
            [
                _node(
                    1501,
                    label="CALL",
                    name="<operator>.fieldAccess",
                    code="req.params.file",
                    line=4,
                    column=12,
                    method_full_name="<operator>.fieldAccess",
                ),
                _node(1502, label="IDENTIFIER", name="file", code="file", line=4, column=8),
                _node(
                    1503,
                    label="CALL",
                    name="resolve",
                    code="path.resolve(ROOT, file)",
                    line=5,
                    column=16,
                    method_full_name="path.resolve",
                ),
                _node(
                    1504,
                    label="CALL",
                    name="startsWith",
                    code="resolvedPath.startsWith(ROOT)",
                    line=6,
                    column=7,
                    method_full_name="startsWith",
                ),
                _node(
                    1505,
                    label="IDENTIFIER",
                    name="resolvedPath",
                    code="resolvedPath",
                    line=7,
                    column=8,
                ),
                _node(
                    1506,
                    label="CALL",
                    name="readFile",
                    code="fs.readFile(resolvedPath)",
                    line=8,
                    column=2,
                    method_full_name="<unknownFullName>",
                ),
            ],
            [
                _node(
                    1503,
                    label="CALL",
                    name="resolve",
                    code="path.resolve(ROOT, file)",
                    line=5,
                    column=16,
                    method_full_name="path.resolve",
                ),
                _node(
                    1504,
                    label="CALL",
                    name="startsWith",
                    code="resolvedPath.startsWith(ROOT)",
                    line=6,
                    column=7,
                    method_full_name="startsWith",
                ),
            ],
        ),
        (
            "numeric_coercion",
            "express_req_query",
            "raw_sql_query",
            SanitizerEffectiveness.PARTIAL,
            [
                _node(
                    1601,
                    label="CALL",
                    name="<operator>.fieldAccess",
                    code="req.query.id",
                    line=4,
                    column=12,
                    method_full_name="<operator>.fieldAccess",
                ),
                _node(1602, label="IDENTIFIER", name="rawId", code="rawId", line=4, column=8),
                _node(
                    1603,
                    label="CALL",
                    name="parseInt",
                    code="parseInt(rawId, 10)",
                    line=5,
                    column=12,
                    method_full_name="parseInt",
                ),
                _node(
                    1604, label="IDENTIFIER", name="numericId", code="numericId", line=5, column=8
                ),
                _node(
                    1605,
                    label="CALL",
                    name="query",
                    code='db.query("SELECT * FROM users WHERE id = " + numericId)',
                    line=6,
                    column=2,
                    method_full_name="<unknownFullName>",
                ),
            ],
            [
                _node(
                    1603,
                    label="CALL",
                    name="parseInt",
                    code="parseInt(rawId, 10)",
                    line=5,
                    column=12,
                    method_full_name="parseInt",
                ),
            ],
        ),
        (
            "uri_component_encoding",
            "express_req_query",
            "ssrf_full_url",
            SanitizerEffectiveness.INEFFECTIVE,
            [
                _node(
                    1701,
                    label="CALL",
                    name="<operator>.fieldAccess",
                    code="req.query.url",
                    line=4,
                    column=12,
                    method_full_name="<operator>.fieldAccess",
                ),
                _node(1702, label="IDENTIFIER", name="url", code="url", line=4, column=8),
                _node(
                    1703,
                    label="CALL",
                    name="encodeURIComponent",
                    code="encodeURIComponent(url)",
                    line=5,
                    column=16,
                    method_full_name="encodeURIComponent",
                ),
                _node(1704, label="IDENTIFIER", name="safeUrl", code="safeUrl", line=5, column=8),
                _node(
                    1705,
                    label="IDENTIFIER",
                    name="safeUrl",
                    code="safeUrl",
                    line=6,
                    column=8,
                ),
            ],
            [
                _node(
                    1703,
                    label="CALL",
                    name="encodeURIComponent",
                    code="encodeURIComponent(url)",
                    line=5,
                    column=16,
                    method_full_name="encodeURIComponent",
                ),
            ],
        ),
        (
            "uri_component_encoding",
            "express_req_query",
            "filesystem_read",
            SanitizerEffectiveness.PARTIAL,
            [
                _node(
                    1721,
                    label="CALL",
                    name="<operator>.fieldAccess",
                    code="req.query.file",
                    line=4,
                    column=12,
                    method_full_name="<operator>.fieldAccess",
                ),
                _node(1722, label="IDENTIFIER", name="file", code="file", line=4, column=8),
                _node(
                    1723,
                    label="CALL",
                    name="encodeURIComponent",
                    code="encodeURIComponent(file)",
                    line=5,
                    column=16,
                    method_full_name="encodeURIComponent",
                ),
                _node(1724, label="IDENTIFIER", name="safeFile", code="safeFile", line=5, column=8),
                _node(
                    1725,
                    label="CALL",
                    name="readFile",
                    code="fs.readFile(safeFile)",
                    line=6,
                    column=2,
                    method_full_name="<unknownFullName>",
                ),
            ],
            [
                _node(
                    1723,
                    label="CALL",
                    name="encodeURIComponent",
                    code="encodeURIComponent(file)",
                    line=5,
                    column=16,
                    method_full_name="encodeURIComponent",
                ),
            ],
        ),
        (
            "fastify_schema_validation",
            "fastify_request_body",
            "fastify_reply_send",
            SanitizerEffectiveness.PARTIAL,
            [
                _node(
                    1751,
                    label="CALL",
                    name="<operator>.fieldAccess",
                    code="request.body.name",
                    line=4,
                    column=15,
                    method_full_name="<operator>.fieldAccess",
                ),
                _node(1752, label="IDENTIFIER", name="name", code="name", line=4, column=9),
                _node(
                    1753,
                    label="CALL",
                    name="send",
                    code="reply.send(name)",
                    line=5,
                    column=2,
                    method_full_name="reply.send",
                ),
            ],
            [
                _node(
                    1751,
                    label="CALL",
                    name="<operator>.fieldAccess",
                    code="request.body.name",
                    line=4,
                    column=15,
                    method_full_name="<operator>.fieldAccess",
                ),
            ],
        ),
    ],
)
def test_extract_candidate_findings_reduces_confidence_for_framework_sanitizers(
    sanitizer_name: str,
    source_name: str,
    sink_name: str,
    expected_effectiveness: SanitizerEffectiveness,
    flow: list[dict[str, object]],
    sanitizer_nodes: list[dict[str, object]],
) -> None:
    source_spec = _source_spec_by_name(source_name)
    sink_spec = _sink_spec_by_name(sink_name)
    sanitizer_spec = _sanitizer_spec_by_name(sanitizer_name)

    responses: dict[str, dict[str, object]] = {
        build_flow_query(source_spec, sink_spec): {
            "success": True,
            "stdout": _joern_json_stdout([{"elements": flow}]),
        },
        build_nodes_query(sanitizer_spec.pattern): {
            "success": True,
            "stdout": _joern_json_stdout(sanitizer_nodes),
        },
    }
    responses.update(_file_lookup_responses(*(int(node["_id"]) for node in flow)))  # type: ignore[call-overload]
    if sink_name == "ssrf_full_url":
        responses["cpg.identifier.id(1705L).astParent.toJsonPretty"] = {
            "success": True,
            "stdout": _joern_json_stdout(
                [
                    _node(
                        1706,
                        label="CALL",
                        name="fetch",
                        code="fetch(safeUrl)",
                        line=6,
                        column=2,
                        method_full_name="fetch",
                    )
                ]
            ),
        }
        responses.update(_file_lookup_responses(1706))

    findings = extract_candidate_findings(
        FakeJoernServer(responses),  # type: ignore[arg-type]
        joern_project_root=TAINT_APP_TRANSPILED_DIR,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
        sanitizer_specs=(sanitizer_spec,),
    )

    assert len(findings) == 1
    finding = findings[0]
    assert finding.vuln_class == (sink_spec.cwe_id or sink_spec.sink_type.value)
    if expected_effectiveness is SanitizerEffectiveness.EFFECTIVE:
        assert finding.confidence == pytest.approx(_DEFAULT_CONFIDENCE)
        assert finding.suppressed is True
        assert any(step.taint_state == "sanitized" for step in finding.taint_path)
    elif expected_effectiveness is SanitizerEffectiveness.PARTIAL:
        assert finding.confidence == pytest.approx(
            _DEFAULT_CONFIDENCE - PARTIAL_CONFIDENCE_REDUCTION
        )
        assert finding.suppressed is False
        assert any(step.taint_state == "sanitized" for step in finding.taint_path)
    else:
        assert finding.confidence == pytest.approx(_DEFAULT_CONFIDENCE)
        assert finding.suppressed is False
        assert all(step.taint_state == "tainted" for step in finding.taint_path)


def test_extract_candidate_findings_keeps_bypassed_sanitizer_paths() -> None:
    source_spec = _source_spec_by_name("express_req_body")
    sink_spec = _sink_spec_by_name("response_output")
    sanitizer_spec = _sanitizer_spec_by_name("html_escape")
    flow = [
        _node(
            1761,
            label="CALL",
            name="<operator>.fieldAccess",
            code="req.body.comment",
            line=4,
            column=12,
            method_full_name="<operator>.fieldAccess",
        ),
        _node(1762, label="IDENTIFIER", name="comment", code="comment", line=4, column=8),
        _node(
            1763,
            label="CALL",
            name="escape",
            code='escape(JSON.stringify({"html":"<ScRiPt>alert(1)</ScRiPt>"}))',
            line=5,
            column=20,
            method_full_name="escape",
        ),
        _node(1764, label="IDENTIFIER", name="safeComment", code="safeComment", line=5, column=8),
        _node(
            1765,
            label="CALL",
            name="send",
            code="res.send(safeComment)",
            line=6,
            column=2,
            method_full_name="<unknownFullName>",
        ),
    ]

    responses: dict[str, dict[str, object]] = {
        build_flow_query(source_spec, sink_spec): {
            "success": True,
            "stdout": _joern_json_stdout([{"elements": flow}]),
        },
        build_nodes_query(sanitizer_spec.pattern): {
            "success": True,
            "stdout": _joern_json_stdout([flow[2]]),
        },
    }
    responses.update(_file_lookup_responses(*(int(node["_id"]) for node in flow)))  # type: ignore[call-overload]

    findings = extract_candidate_findings(
        FakeJoernServer(responses),  # type: ignore[arg-type]
        joern_project_root=TAINT_APP_TRANSPILED_DIR,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
        sanitizer_specs=(sanitizer_spec,),
    )

    assert len(findings) == 1
    finding = findings[0]
    assert finding.suppressed is False
    assert finding.confidence == pytest.approx(0.9)
    assert finding.metadata["sanitizer_bypassed"] is True
    assert set(finding.metadata["sanitizer_bypass_patterns"]) >= {
        "nested_contexts",
        "case_variation",
    }


def test_extract_candidate_findings_leaves_unsanitized_confidence_untouched() -> None:
    source_spec = _source_spec_by_name("express_req_body")
    sink_spec = _sink_spec_by_name("raw_sql_query")
    sanitizer_spec = _sanitizer_spec_by_name("sqlstring_escape")
    flow = [
        _node(
            1801,
            label="CALL",
            name="<operator>.fieldAccess",
            code="req.body.userId",
            line=4,
            column=12,
            method_full_name="<operator>.fieldAccess",
        ),
        _node(1802, label="IDENTIFIER", name="userId", code="userId", line=4, column=8),
        _node(
            1803,
            label="CALL",
            name="query",
            code='db.query("SELECT * FROM users WHERE id = \'" + userId + "\'")',
            line=5,
            column=2,
            method_full_name="<unknownFullName>",
        ),
    ]
    responses: dict[str, dict[str, object]] = {
        build_flow_query(source_spec, sink_spec): {
            "success": True,
            "stdout": _joern_json_stdout([{"elements": flow}]),
        },
        build_nodes_query(sanitizer_spec.pattern): {
            "success": True,
            "stdout": _joern_json_stdout([]),
        },
    }
    responses.update(_file_lookup_responses(1801, 1802, 1803))

    findings = extract_candidate_findings(
        FakeJoernServer(responses),  # type: ignore[arg-type]
        joern_project_root=TAINT_APP_TRANSPILED_DIR,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
        sanitizer_specs=(sanitizer_spec,),
    )

    assert len(findings) == 1
    finding = findings[0]
    assert finding.confidence == pytest.approx(_DEFAULT_CONFIDENCE)
    assert all(step.taint_state == "tainted" for step in finding.taint_path)


def test_extract_candidate_findings_sets_medium_severity_for_ssrf_path_segment() -> None:
    source_spec = _source_spec_by_name("express_req_query")
    sink_spec = _sink_spec_by_name("ssrf_path_segment")
    sanitizer_spec = SanitizerSpec(
        name="noop",
        pattern='cpg.call.name("__never__")',
        kind=_sanitizer_spec_by_name("html_escape").kind,
    )
    flow = [
        _node(
            1901,
            label="CALL",
            name="<operator>.fieldAccess",
            code="req.query.userId",
            line=4,
            column=12,
            method_full_name="<operator>.fieldAccess",
        ),
        _node(1902, label="IDENTIFIER", name="endpoint", code="endpoint", line=5, column=8),
        _node(
            1903,
            label="CALL",
            name="<operator>.formatString",
            code="`https://internal.service.local/api/users/${userId}`",
            line=5,
            column=20,
            method_full_name="<operator>.formatString",
        ),
        _node(1904, label="IDENTIFIER", name="endpoint", code="endpoint", line=6, column=14),
    ]
    responses: dict[str, dict[str, object]] = {
        build_flow_query(source_spec, sink_spec): {
            "success": True,
            "stdout": _joern_json_stdout([{"elements": flow}]),
        },
        build_nodes_query(sanitizer_spec.pattern): {
            "success": True,
            "stdout": _joern_json_stdout([]),
        },
        "cpg.identifier.id(1904L).astParent.toJsonPretty": {
            "success": True,
            "stdout": _joern_json_stdout(
                [
                    _node(
                        1905,
                        label="CALL",
                        name="fetch",
                        code="fetch(endpoint)",
                        line=6,
                        column=2,
                        method_full_name="fetch",
                    )
                ]
            ),
        },
    }
    responses.update(_file_lookup_responses(1901, 1902, 1903, 1904, 1905))

    findings = extract_candidate_findings(
        FakeJoernServer(responses),  # type: ignore[arg-type]
        joern_project_root=TAINT_APP_TRANSPILED_DIR,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
        sanitizer_specs=(sanitizer_spec,),
    )

    assert len(findings) == 1
    finding = findings[0]
    assert finding.vuln_class == "CWE-918"
    assert finding.severity == "medium"
    assert finding.sink.api_name == "fetch"
    assert finding.sink.location.line == 6


def test_extract_candidate_findings_prunes_dead_code_branch(tmp_path: Path) -> None:
    fixture_path = tmp_path / "dead_code.js"
    fixture_path.write_text(
        "function handle(input) {\n  if (0) {\n    sink(input);\n  }\n}\n",
        encoding="utf-8",
    )
    source_spec = _custom_source_spec("dead_code_source")
    sink_spec = _custom_sink_spec(
        "dead_code_sink",
        sink_type=SinkType.HTML_OUTPUT,
        cwe_id="CWE-79",
    )
    flow = [
        _node(2001, label="METHOD_PARAMETER_IN", name="input", code="input", line=1, column=17),
        _node(2002, label="IDENTIFIER", name="input", code="input", line=3, column=10),
        _node(
            2003,
            label="CALL",
            name="sink",
            code="sink(input)",
            line=3,
            column=5,
            method_full_name="<unknownFullName>",
        ),
    ]
    control = {
        "_id": 2900,
        "code": "if (0)",
        "lineNumber": 2,
        "columnNumber": 3,
        "controlStructureType": "IF",
        "condition": "0",
    }
    exact_payloads = {
        build_flow_query(source_spec, sink_spec): [{"elements": flow}],
    }
    _register_file_queries(exact_payloads, fixture_path, 2001, 2002, 2003, 2900)  # type: ignore[arg-type]

    findings = extract_candidate_findings(
        FakeJoernServer(
            exact_payloads=exact_payloads,  # type: ignore[arg-type]
            controlled_by={
                2001: [],
                2002: [{"_id": 2900, "methodName": "handle", "methodFullName": "fixtures.handle"}],
                2003: [{"_id": 2900, "methodName": "handle", "methodFullName": "fixtures.handle"}],
            },
            method_controls={"fixtures.handle": [control]},
            branch_ast_ids={(2900, 2): [2002, 2003], (2900, 3): []},
        ),
        joern_project_root=tmp_path,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
        sanitizer_specs=(),
    )

    assert findings == ()


def test_extract_candidate_findings_keeps_reachable_else_branch(tmp_path: Path) -> None:
    fixture_path = tmp_path / "reachable_else.js"
    fixture_path.write_text(
        "function handle(input) {\n  if (0) {\n    return;\n  }\n  sink(input);\n}\n",
        encoding="utf-8",
    )
    source_spec = _custom_source_spec("reachable_else_source")
    sink_spec = _custom_sink_spec(
        "reachable_else_sink",
        sink_type=SinkType.HTML_OUTPUT,
        cwe_id="CWE-79",
    )
    flow = [
        _node(2011, label="METHOD_PARAMETER_IN", name="input", code="input", line=1, column=17),
        _node(2012, label="IDENTIFIER", name="input", code="input", line=5, column=8),
        _node(
            2013,
            label="CALL",
            name="sink",
            code="sink(input)",
            line=5,
            column=3,
            method_full_name="<unknownFullName>",
        ),
    ]
    control = {
        "_id": 2901,
        "code": "if (0)",
        "lineNumber": 2,
        "columnNumber": 3,
        "controlStructureType": "IF",
        "condition": "0",
    }
    exact_payloads = {
        build_flow_query(source_spec, sink_spec): [{"elements": flow}],
    }
    _register_file_queries(exact_payloads, fixture_path, 2011, 2012, 2013, 2901)  # type: ignore[arg-type]

    findings = extract_candidate_findings(
        FakeJoernServer(
            exact_payloads=exact_payloads,  # type: ignore[arg-type]
            controlled_by={
                2011: [],
                2012: [{"_id": 2901, "methodName": "handle", "methodFullName": "fixtures.handle"}],
                2013: [{"_id": 2901, "methodName": "handle", "methodFullName": "fixtures.handle"}],
            },
            method_controls={"fixtures.handle": [control]},
            branch_ast_ids={(2901, 2): [], (2901, 3): [2012, 2013]},
        ),
        joern_project_root=tmp_path,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
        sanitizer_specs=(),
    )

    assert len(findings) == 1
    assert findings[0].confidence == pytest.approx(_DEFAULT_CONFIDENCE)


def test_extract_candidate_findings_prunes_typeof_number_guard_for_string_sink(
    tmp_path: Path,
) -> None:
    fixture_path = tmp_path / "typeof_number.js"
    fixture_path.write_text(
        "function handle(input) {\n"
        '  const query = "SELECT " + input;\n'
        '  if (typeof input === "number") {\n'
        "    sink(query);\n"
        "  }\n"
        "}\n",
        encoding="utf-8",
    )
    source_spec = _custom_source_spec("typeof_number_source")
    sink_spec = _custom_sink_spec(
        "typeof_number_sink",
        sink_type=SinkType.SQL_QUERY,
        cwe_id="CWE-89",
    )
    flow = [
        _node(2021, label="METHOD_PARAMETER_IN", name="input", code="input", line=1, column=17),
        _node(2022, label="IDENTIFIER", name="input", code="input", line=2, column=28),
        _node(
            2023,
            label="CALL",
            name="<operator>.addition",
            code='"SELECT " + input',
            line=2,
            column=17,
            method_full_name="<operator>.addition",
        ),
        _node(2024, label="IDENTIFIER", name="query", code="query", line=4, column=10),
        _node(
            2025,
            label="CALL",
            name="sink",
            code="sink(query)",
            line=4,
            column=5,
            method_full_name="<unknownFullName>",
        ),
    ]
    control = {
        "_id": 2902,
        "code": 'if (typeof input === "number")',
        "lineNumber": 3,
        "columnNumber": 3,
        "controlStructureType": "IF",
        "condition": 'typeof input === "number"',
    }
    exact_payloads = {
        build_flow_query(source_spec, sink_spec): [{"elements": flow}],
    }
    _register_file_queries(exact_payloads, fixture_path, 2021, 2022, 2023, 2024, 2025, 2902)  # type: ignore[arg-type]

    findings = extract_candidate_findings(
        FakeJoernServer(
            exact_payloads=exact_payloads,  # type: ignore[arg-type]
            controlled_by={
                2021: [],
                2022: [],
                2023: [],
                2024: [{"_id": 2902, "methodName": "handle", "methodFullName": "fixtures.handle"}],
                2025: [{"_id": 2902, "methodName": "handle", "methodFullName": "fixtures.handle"}],
            },
            method_controls={"fixtures.handle": [control]},
            branch_ast_ids={(2902, 2): [2024, 2025], (2902, 3): []},
        ),
        joern_project_root=tmp_path,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
        sanitizer_specs=(),
    )

    assert findings == ()


def test_extract_candidate_findings_prunes_number_is_integer_guard_for_string_sink(
    tmp_path: Path,
) -> None:
    fixture_path = tmp_path / "integer_guard.js"
    fixture_path.write_text(
        "function handle(input) {\n"
        "  if (!Number.isInteger(input)) return;\n"
        '  const query = "SELECT " + input;\n'
        "  sink(query);\n"
        "}\n",
        encoding="utf-8",
    )
    source_spec = _custom_source_spec("integer_guard_source")
    sink_spec = _custom_sink_spec(
        "integer_guard_sink",
        sink_type=SinkType.SQL_QUERY,
        cwe_id="CWE-89",
    )
    flow = [
        _node(2031, label="METHOD_PARAMETER_IN", name="input", code="input", line=1, column=17),
        _node(2032, label="IDENTIFIER", name="input", code="input", line=3, column=28),
        _node(
            2033,
            label="CALL",
            name="<operator>.addition",
            code='"SELECT " + input',
            line=3,
            column=17,
            method_full_name="<operator>.addition",
        ),
        _node(2034, label="IDENTIFIER", name="query", code="query", line=4, column=8),
        _node(
            2035,
            label="CALL",
            name="sink",
            code="sink(query)",
            line=4,
            column=3,
            method_full_name="<unknownFullName>",
        ),
    ]
    control = {
        "_id": 2903,
        "code": "if (!Number.isInteger(input)) return;",
        "lineNumber": 2,
        "columnNumber": 3,
        "controlStructureType": "IF",
        "condition": "!Number.isInteger(input)",
    }
    exact_payloads = {
        build_flow_query(source_spec, sink_spec): [{"elements": flow}],
    }
    _register_file_queries(exact_payloads, fixture_path, 2031, 2032, 2033, 2034, 2035, 2903)  # type: ignore[arg-type]

    findings = extract_candidate_findings(
        FakeJoernServer(
            exact_payloads=exact_payloads,  # type: ignore[arg-type]
            method_controls={"fixtures.handle": [control]},
            node_methods={2035: [{"name": "handle", "fullName": "fixtures.handle"}]},
            branch_ast_ids={(2903, 2): [], (2903, 3): []},
            branch_returns={(2903, 2): [2990], (2903, 3): []},
            control_calls={(2903, "isInteger"): [2991]},
        ),
        joern_project_root=tmp_path,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
        sanitizer_specs=(),
    )

    assert findings == ()


def test_extract_candidate_findings_keeps_numeric_guard_for_non_string_sink(tmp_path: Path) -> None:
    fixture_path = tmp_path / "numeric_non_string.js"
    fixture_path.write_text(
        'function handle(input) {\n  if (typeof input === "number") {\n    exec(input);\n  }\n}\n',
        encoding="utf-8",
    )
    source_spec = _custom_source_spec("numeric_non_string_source")
    sink_spec = _custom_sink_spec(
        "numeric_non_string_sink",
        sink_type=SinkType.SHELL_EXEC,
        cwe_id="CWE-78",
    )
    flow = [
        _node(2041, label="METHOD_PARAMETER_IN", name="input", code="input", line=1, column=17),
        _node(2042, label="IDENTIFIER", name="input", code="input", line=3, column=10),
        _node(
            2043,
            label="CALL",
            name="exec",
            code="exec(input)",
            line=3,
            column=5,
            method_full_name="<unknownFullName>",
        ),
    ]
    control = {
        "_id": 2904,
        "code": 'if (typeof input === "number")',
        "lineNumber": 2,
        "columnNumber": 3,
        "controlStructureType": "IF",
        "condition": 'typeof input === "number"',
    }
    exact_payloads = {
        build_flow_query(source_spec, sink_spec): [{"elements": flow}],
    }
    _register_file_queries(exact_payloads, fixture_path, 2041, 2042, 2043, 2904)  # type: ignore[arg-type]

    findings = extract_candidate_findings(
        FakeJoernServer(
            exact_payloads=exact_payloads,  # type: ignore[arg-type]
            controlled_by={
                2041: [],
                2042: [{"_id": 2904, "methodName": "handle", "methodFullName": "fixtures.handle"}],
                2043: [{"_id": 2904, "methodName": "handle", "methodFullName": "fixtures.handle"}],
            },
            method_controls={"fixtures.handle": [control]},
            branch_ast_ids={(2904, 2): [2042, 2043], (2904, 3): []},
        ),
        joern_project_root=tmp_path,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
        sanitizer_specs=(),
    )

    assert len(findings) == 1
    assert findings[0].vuln_class == "CWE-78"


def test_extract_candidate_findings_reduces_confidence_for_allowlist_guard(tmp_path: Path) -> None:
    fixture_path = tmp_path / "allowlist_guard.js"
    fixture_path.write_text(
        "function handle(input) {\n"
        '  const allowed = ["admin", "user"];\n'
        "  if (!allowed.includes(input)) return;\n"
        "  sink(input);\n"
        "}\n",
        encoding="utf-8",
    )
    source_spec = _custom_source_spec("allowlist_source")
    sink_spec = _custom_sink_spec(
        "allowlist_sink",
        sink_type=SinkType.HTML_OUTPUT,
        cwe_id="CWE-79",
    )
    flow = [
        _node(2051, label="METHOD_PARAMETER_IN", name="input", code="input", line=1, column=17),
        _node(2052, label="IDENTIFIER", name="input", code="input", line=4, column=8),
        _node(
            2053,
            label="CALL",
            name="sink",
            code="sink(input)",
            line=4,
            column=3,
            method_full_name="<unknownFullName>",
        ),
    ]
    control = {
        "_id": 2905,
        "code": "if (!allowed.includes(input)) return;",
        "lineNumber": 3,
        "columnNumber": 3,
        "controlStructureType": "IF",
        "condition": "!allowed.includes(input)",
    }
    exact_payloads = {
        build_flow_query(source_spec, sink_spec): [{"elements": flow}],
    }
    _register_file_queries(exact_payloads, fixture_path, 2051, 2052, 2053, 2905)  # type: ignore[arg-type]

    findings = extract_candidate_findings(
        FakeJoernServer(
            exact_payloads=exact_payloads,  # type: ignore[arg-type]
            method_controls={"fixtures.handle": [control]},
            node_methods={2053: [{"name": "handle", "fullName": "fixtures.handle"}]},
            branch_ast_ids={(2905, 2): [], (2905, 3): []},
            branch_returns={(2905, 2): [2992], (2905, 3): []},
            control_calls={(2905, "includes"): [2993]},
        ),
        joern_project_root=tmp_path,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
        sanitizer_specs=(),
    )

    assert len(findings) == 1
    assert findings[0].confidence == pytest.approx(0.1)


def test_extract_candidate_findings_keeps_default_confidence_for_non_allowlist_includes(
    tmp_path: Path,
) -> None:
    fixture_path = tmp_path / "non_allowlist_includes.js"
    fixture_path.write_text(
        "function handle(input) {\n"
        '  const prefix = "admin";\n'
        "  if (!prefix.includes(input)) return;\n"
        "  sink(input);\n"
        "}\n",
        encoding="utf-8",
    )
    source_spec = _custom_source_spec("non_allowlist_source")
    sink_spec = _custom_sink_spec(
        "non_allowlist_sink",
        sink_type=SinkType.HTML_OUTPUT,
        cwe_id="CWE-79",
    )
    flow = [
        _node(2061, label="METHOD_PARAMETER_IN", name="input", code="input", line=1, column=17),
        _node(2062, label="IDENTIFIER", name="input", code="input", line=4, column=8),
        _node(
            2063,
            label="CALL",
            name="sink",
            code="sink(input)",
            line=4,
            column=3,
            method_full_name="<unknownFullName>",
        ),
    ]
    control = {
        "_id": 2906,
        "code": "if (!prefix.includes(input)) return;",
        "lineNumber": 3,
        "columnNumber": 3,
        "controlStructureType": "IF",
        "condition": "!prefix.includes(input)",
    }
    exact_payloads = {
        build_flow_query(source_spec, sink_spec): [{"elements": flow}],
    }
    _register_file_queries(exact_payloads, fixture_path, 2061, 2062, 2063, 2906)  # type: ignore[arg-type]

    findings = extract_candidate_findings(
        FakeJoernServer(
            exact_payloads=exact_payloads,  # type: ignore[arg-type]
            method_controls={"fixtures.handle": [control]},
            node_methods={2063: [{"name": "handle", "fullName": "fixtures.handle"}]},
            branch_ast_ids={(2906, 2): [], (2906, 3): []},
            branch_returns={(2906, 2): [2994], (2906, 3): []},
            control_calls={(2906, "includes"): [2995]},
        ),
        joern_project_root=tmp_path,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
        sanitizer_specs=(),
    )

    assert len(findings) == 1
    assert findings[0].confidence == pytest.approx(_DEFAULT_CONFIDENCE)


@pytest.fixture(scope="module")
def joern_server() -> Generator[JoernServer, None, None]:
    if not is_joern_installed():
        pytest.skip("Joern is not installed in PATH")

    try:
        with JoernServer(port=8124, startup_timeout_seconds=30, query_timeout_seconds=30) as server:
            server.import_project(TAINT_APP_TRANSPILED_DIR)
            yield server
    except JoernError as exc:
        pytest.skip(str(exc))


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

    assert len(findings) == 3

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

    xss_finding = next(finding for finding in findings if finding.vuln_class == "CWE-79")
    assert xss_finding.sink.api_name == "res.send"
    assert xss_finding.sink.location.line == 12
    assert xss_finding.confidence == pytest.approx(_DEFAULT_CONFIDENCE)
    assert xss_finding.suppressed is True
    assert any(step.sanitizer_applied == "escape" for step in xss_finding.taint_path)
