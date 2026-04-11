from __future__ import annotations

import json
from pathlib import Path

from piranesi.detect.flows import extract_candidate_findings
from piranesi.detect.injection_variants import (
    is_header_value_position,
    is_template_source_position,
)
from piranesi.scan.queries import QueryNode, build_flow_query
from piranesi.scan.specs import get_sink_specs, get_source_specs


class _FakeJoernServer:
    def __init__(self, *, exact_payloads: dict[str, object] | None = None) -> None:
        self.exact_payloads = exact_payloads or {}

    def query(self, cpgql: str) -> dict[str, object]:
        payload = self.exact_payloads.get(cpgql, [])
        return {"success": True, "stdout": f'val res0: String = """{json.dumps(payload)}"""'}


def _source_spec(name: str):
    return next(spec for spec in get_source_specs(frameworks=("express",)) if spec.name == name)


def _sink_spec(name: str):
    return next(spec for spec in get_sink_specs(frameworks=("express",)) if spec.name == name)


def _node(
    node_id: int,
    *,
    label: str,
    name: str,
    code: str,
    line: int,
) -> dict[str, object]:
    return {
        "_id": node_id,
        "_label": label,
        "name": name,
        "code": code,
        "lineNumber": line,
        "columnNumber": 1,
        "methodFullName": "app.js::program:handler",
    }


def test_nosql_query_param_scalar_flow_is_filtered() -> None:
    source_spec = _source_spec("express_req_query")
    sink_spec = _sink_spec("mongodb_collection_find")
    project_root = Path("/tmp")
    flow = [
        _node(10, label="CALL", name="<operator>.fieldAccess", code="req.query.user", line=4),
        _node(11, label="IDENTIFIER", name="user", code="user", line=4),
        _node(12, label="IDENTIFIER", name="filter", code="{ username: user }", line=5),
    ]
    parent = _node(
        13,
        label="CALL",
        name="find",
        code="User.find({ username: user })",
        line=5,
    )
    exact_payloads = {
        build_flow_query(source_spec, sink_spec): [{"elements": flow}],
        "cpg.identifier.id(12L).astParent.toJsonPretty": [parent],
        "cpg.id(10L).file.name.toJsonPretty": [str(project_root / "app.ts")],
        "cpg.id(12L).file.name.toJsonPretty": [str(project_root / "app.ts")],
        "cpg.id(13L).file.name.toJsonPretty": [str(project_root / "app.ts")],
    }

    findings = extract_candidate_findings(
        _FakeJoernServer(exact_payloads=exact_payloads),  # type: ignore[arg-type]
        joern_project_root=project_root,
        source_map=None,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
        sanitizer_specs=(),
    )

    assert not findings


def test_nosql_request_body_object_flow_is_reported() -> None:
    source_spec = _source_spec("express_req_body")
    sink_spec = _sink_spec("mongodb_collection_find")
    project_root = Path("/tmp")
    flow = [
        _node(20, label="CALL", name="<operator>.fieldAccess", code="req.body.user", line=4),
        _node(21, label="IDENTIFIER", name="user", code="user", line=4),
        _node(22, label="IDENTIFIER", name="filter", code="{ username: user }", line=5),
    ]
    parent = _node(
        23,
        label="CALL",
        name="find",
        code="User.find({ username: user })",
        line=5,
    )
    exact_payloads = {
        build_flow_query(source_spec, sink_spec): [{"elements": flow}],
        "cpg.identifier.id(22L).astParent.toJsonPretty": [parent],
        "cpg.id(20L).file.name.toJsonPretty": [str(project_root / "app.ts")],
        "cpg.id(22L).file.name.toJsonPretty": [str(project_root / "app.ts")],
        "cpg.id(23L).file.name.toJsonPretty": [str(project_root / "app.ts")],
    }

    findings = extract_candidate_findings(
        _FakeJoernServer(exact_payloads=exact_payloads),  # type: ignore[arg-type]
        joern_project_root=project_root,
        source_map=None,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
        sanitizer_specs=(),
    )

    assert len(findings) == 1
    assert findings[0].vuln_class == "CWE-943"
    assert findings[0].sink.api_name == "User.find"


def test_template_source_position_distinguishes_context_payload() -> None:
    source_flow = [
        QueryNode(1, "render", "ejs.render(req.body.template, { name: 'test' })", "CALL", 1, 1, None)
    ]
    context_flow = [
        QueryNode(2, "identifier", "{ name: req.body.name }", "IDENTIFIER", 1, 1, None)
    ]

    assert is_template_source_position(source_flow) is True
    assert is_template_source_position(context_flow) is False


def test_header_value_position_rejects_header_name_literals() -> None:
    value_flow = [QueryNode(3, "identifier", "res.setHeader('X-Custom', value)", "CALL", 1, 1, None)]
    name_flow = [QueryNode(4, "literal", "'X-Custom'", "LITERAL", 1, 1, None)]

    assert is_header_value_position(value_flow) is True
    assert is_header_value_position(name_flow) is False
