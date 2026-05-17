from __future__ import annotations

import json
from pathlib import Path

from piranesi.detect.alias import extract_alias_findings
from piranesi.detect.flows import extract_candidate_findings
from piranesi.scan.queries import build_flow_query
from piranesi.scan.specs import get_sink_specs, get_source_specs

ROOT = Path(__file__).resolve().parents[2]
NODEGOAT_NOSQL_FIXTURE = ROOT / "eval" / "synthetic" / "nodegoat-nosqli-where.ts"
NODEGOAT_REDIRECT_FIXTURE = ROOT / "eval" / "synthetic" / "nodegoat-open-redirect.ts"


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
        "methodFullName": "nodegoat-regression.ts::program:handler",
    }


def test_nodegoat_nosql_where_query_param_flow_is_reported() -> None:
    source_spec = _source_spec("express_req_query")
    sink_spec = _sink_spec("mongodb_where_operator")

    flow = [
        _node(101, label="CALL", name="<operator>.fieldAccess", code="req.query.threshold", line=4),
        _node(102, label="IDENTIFIER", name="threshold", code="threshold", line=4),
        _node(103, label="IDENTIFIER", name="query", code="query", line=8),
    ]
    parent = _node(
        104,
        label="CALL",
        name="find",
        code="allocationsCollection.find(query).toArray()",
        line=8,
    )
    exact_payloads = {
        build_flow_query(source_spec, sink_spec): [{"elements": flow}],
        "cpg.identifier.id(103L).astParent.toJsonPretty": [parent],
        "cpg.id(101L).file.name.toJsonPretty": [str(NODEGOAT_NOSQL_FIXTURE)],
        "cpg.id(103L).file.name.toJsonPretty": [str(NODEGOAT_NOSQL_FIXTURE)],
        "cpg.id(104L).file.name.toJsonPretty": [str(NODEGOAT_NOSQL_FIXTURE)],
    }

    findings = extract_candidate_findings(
        _FakeJoernServer(exact_payloads=exact_payloads),  # type: ignore[arg-type]
        joern_project_root=NODEGOAT_NOSQL_FIXTURE.parent,
        source_map=None,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
        sanitizer_specs=(),
    )

    assert len(findings) == 1
    finding = findings[0]
    assert finding.vuln_class == "CWE-943"
    assert finding.sink.api_name == "allocationsCollection.find"
    assert finding.source.location.file == str(NODEGOAT_NOSQL_FIXTURE)


def test_nodegoat_nosql_safe_neighbor_is_not_reported() -> None:
    source_spec = _source_spec("express_req_query")
    sink_spec = _sink_spec("mongodb_collection_find")

    flow = [
        _node(
            201,
            label="CALL",
            name="<operator>.fieldAccess",
            code="req.query.threshold",
            line=13,
        ),
        _node(202, label="IDENTIFIER", name="thresholdValue", code="thresholdValue", line=13),
        _node(203, label="IDENTIFIER", name="query", code="query", line=14),
    ]
    parent = _node(
        204,
        label="CALL",
        name="find",
        code="allocationsCollection.find(query).toArray()",
        line=18,
    )
    exact_payloads = {
        build_flow_query(source_spec, sink_spec): [{"elements": flow}],
        "cpg.identifier.id(203L).astParent.toJsonPretty": [parent],
        "cpg.id(201L).file.name.toJsonPretty": [str(NODEGOAT_NOSQL_FIXTURE)],
        "cpg.id(203L).file.name.toJsonPretty": [str(NODEGOAT_NOSQL_FIXTURE)],
        "cpg.id(204L).file.name.toJsonPretty": [str(NODEGOAT_NOSQL_FIXTURE)],
    }

    findings = extract_candidate_findings(
        _FakeJoernServer(exact_payloads=exact_payloads),  # type: ignore[arg-type]
        joern_project_root=NODEGOAT_NOSQL_FIXTURE.parent,
        source_map=None,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
        sanitizer_specs=(),
    )

    assert not findings


def test_nodegoat_open_redirect_fixture_reports_only_vulnerable_redirect() -> None:
    sink_spec = _sink_spec("express_redirect")

    findings = extract_alias_findings(
        NODEGOAT_REDIRECT_FIXTURE.parent,
        sink_specs=(sink_spec,),
        files=(NODEGOAT_REDIRECT_FIXTURE,),
    )

    assert len(findings) == 1
    assert findings[0].vuln_class == "CWE-601"
    assert findings[0].sink.location.file == str(NODEGOAT_REDIRECT_FIXTURE)
    assert findings[0].sink.location.snippet.strip() == "return res.redirect(target);"
