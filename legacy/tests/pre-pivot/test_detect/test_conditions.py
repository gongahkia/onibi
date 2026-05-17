from __future__ import annotations

import json
import re
from pathlib import Path

import pytest

from piranesi.detect.conditions import parse_condition_text
from piranesi.detect.flows import extract_candidate_findings
from piranesi.models import SourceLocation
from piranesi.scan.queries import build_flow_query
from piranesi.scan.specs import SinkSpec, SinkType, SourceSpec, SourceType

FIXTURE_PATH = Path(__file__).resolve().parents[1] / "fixtures" / "scan_queries" / "conditions.js"


class FakeJoernServer:
    def __init__(
        self,
        *,
        exact_payloads: dict[str, object] | None = None,
        controlled_by: dict[int, list[dict[str, object]]] | None = None,
        method_controls: dict[str, list[dict[str, object]]] | None = None,
        branch_ast_ids: dict[tuple[int, int], list[int]] | None = None,
        control_by_id: dict[int, list[dict[str, object]]] | None = None,
    ) -> None:
        self.exact_payloads = exact_payloads or {}
        self.controlled_by = controlled_by or {}
        self.method_controls = method_controls or {}
        self.branch_ast_ids = branch_ast_ids or {}
        self.control_by_id = control_by_id or {}
        self.queries: list[str] = []

    def query(self, cpgql: str) -> dict[str, object]:
        self.queries.append(cpgql)
        payload = self._payload_for(cpgql)
        return {"success": True, "stdout": _joern_json_stdout(payload)}

    def _payload_for(self, cpgql: str) -> object:
        if cpgql in self.exact_payloads:
            return self.exact_payloads[cpgql]

        controlled_by_match = re.fullmatch(
            r"cpg\.(?:call|identifier|parameter|returns|fieldIdentifier|literal)\.id\((?P<node_id>\d+)L\)"
            r'\.controlledBy\.map\(c => Map\("'
            r'_id" -> c\.id, "methodName" -> c\.method\.name, '
            r'"methodFullName" -> c\.method\.fullName\)\)\.toJsonPretty',
            cpgql,
        )
        if controlled_by_match is not None:
            node_id = int(controlled_by_match.group("node_id"))
            return self.controlled_by.get(node_id, [])

        method_controls_match = re.fullmatch(
            r'cpg\.method\.(?:fullNameExact|name)\((?P<method>"(?:\\.|[^"\\])*")\)'
            r'\.ast\.isControlStructure\.map\(c => Map\("'
            r'_id" -> c\.id, "code" -> c\.code, "lineNumber" -> c\.lineNumber, '
            r'"columnNumber" -> c\.columnNumber, "controlStructureType" -> '
            r'c\.controlStructureType, "condition" -> c\.condition\.code\)\)\.toJsonPretty',
            cpgql,
        )
        if method_controls_match is not None:
            method_key = json.loads(method_controls_match.group("method"))
            return self.method_controls.get(method_key, [])

        branch_ast_match = re.fullmatch(
            r"cpg\.id\((?P<control_id>\d+)L\)\.astChildren\.order\((?P<order>\d+)\)\.ast\.id\.toJsonPretty",
            cpgql,
        )
        if branch_ast_match is not None:
            key = (int(branch_ast_match.group("control_id")), int(branch_ast_match.group("order")))
            return self.branch_ast_ids.get(key, [])

        control_by_id_match = re.fullmatch(
            r'cpg\.id\((?P<control_id>\d+)L\)\.map\(c => Map\("'
            r'_id" -> c\.id, "code" -> c\.code, "lineNumber" -> c\.lineNumber, '
            r'"columnNumber" -> c\.columnNumber, "controlStructureType" -> '
            r'c\.controlStructureType, "condition" -> c\.condition\.code\)\)\.toJsonPretty',
            cpgql,
        )
        if control_by_id_match is not None:
            control_id = int(control_by_id_match.group("control_id"))
            return self.control_by_id.get(control_id, [])

        return []


def _joern_json_stdout(payload: object) -> str:
    return f'val res0: String = """{json.dumps(payload, indent=2)}"""'


@pytest.mark.parametrize(
    (
        "expression",
        "required_value",
        "condition_type",
        "symbolic_constraint",
        "normalized_required",
    ),
    [
        (
            'typeof input === "string"',
            True,
            "type_check",
            'TypeCheck(var="input", type="string")',
            True,
        ),
        (
            "input.length > 5",
            True,
            "string_length",
            'StringLength(var="input", op="gt", n=5)',
            True,
        ),
        (
            'input.includes("admin")',
            True,
            "string_contains",
            'StringContains(var="input", substr="admin")',
            True,
        ),
        (
            'input === "expected"',
            True,
            "string_eq",
            'StringEq(var="input", val="expected")',
            True,
        ),
        (
            "input > 0",
            True,
            "int_bound",
            'IntBound(var="input", op="gt", n=0)',
            True,
        ),
        (
            "checkAccess(input, role)",
            True,
            "branch",
            None,
            True,
        ),
    ],
)
def test_parse_condition_text_builds_expected_models(
    expression: str,
    required_value: bool,
    condition_type: str,
    symbolic_constraint: str | None,
    normalized_required: bool,
) -> None:
    location = SourceLocation(
        file=str(FIXTURE_PATH),
        line=1,
        column=0,
        snippet=expression,
    )

    condition = parse_condition_text(
        expression,
        location=location,
        required_value=required_value,
    )

    assert condition.condition_type == condition_type
    assert condition.symbolic_constraint == symbolic_constraint
    assert condition.required_value == normalized_required
    assert condition.expression == expression


def test_extract_candidate_findings_populates_nested_path_conditions() -> None:
    source_spec = SourceSpec(
        name="custom_source",
        pattern='cpg.call.name("customSource")',
        source_type=SourceType.CUSTOM,
    )
    sink_spec = SinkSpec(
        name="custom_sink",
        pattern='cpg.call.name("sink")',
        sink_type=SinkType.CUSTOM,
        cwe_id="CWE-79",
    )
    flow = [
        {
            "_id": 101,
            "_label": "METHOD_PARAMETER_IN",
            "name": "input",
            "code": "input",
            "lineNumber": 1,
            "columnNumber": 17,
            "methodFullName": "conditions.handle",
        },
        {
            "_id": 102,
            "_label": "IDENTIFIER",
            "name": "value",
            "code": "value",
            "lineNumber": 2,
            "columnNumber": 9,
            "methodFullName": "conditions.handle",
        },
        {
            "_id": 103,
            "_label": "IDENTIFIER",
            "name": "value",
            "code": "value",
            "lineNumber": 8,
            "columnNumber": 18,
            "methodFullName": "conditions.handle",
        },
        {
            "_id": 104,
            "_label": "CALL",
            "name": "sink",
            "code": "sink(value)",
            "lineNumber": 8,
            "columnNumber": 13,
            "methodFullName": "sink",
        },
    ]
    controls = [
        {
            "_id": 900,
            "code": 'if (typeof value === "string")',
            "lineNumber": 3,
            "columnNumber": 3,
            "controlStructureType": "IF",
            "condition": 'typeof value === "string"',
        },
        {
            "_id": 901,
            "code": "if (value.length > 5)",
            "lineNumber": 4,
            "columnNumber": 5,
            "controlStructureType": "IF",
            "condition": "value.length > 5",
        },
        {
            "_id": 902,
            "code": 'if (value.includes("admin"))',
            "lineNumber": 5,
            "columnNumber": 7,
            "controlStructureType": "IF",
            "condition": 'value.includes("admin")',
        },
        {
            "_id": 903,
            "code": "switch (role)",
            "lineNumber": 6,
            "columnNumber": 9,
            "controlStructureType": "SWITCH",
            "condition": "role",
        },
    ]
    controlled_by = {
        101: [],
        102: [],
        103: [
            {"_id": 900, "methodName": "handle", "methodFullName": "conditions.handle"},
            {"_id": 901, "methodName": "handle", "methodFullName": "conditions.handle"},
            {"_id": 902, "methodName": "handle", "methodFullName": "conditions.handle"},
            {"_id": 903, "methodName": "handle", "methodFullName": "conditions.handle"},
        ],
        104: [
            {"_id": 900, "methodName": "handle", "methodFullName": "conditions.handle"},
            {"_id": 901, "methodName": "handle", "methodFullName": "conditions.handle"},
            {"_id": 902, "methodName": "handle", "methodFullName": "conditions.handle"},
            {"_id": 903, "methodName": "handle", "methodFullName": "conditions.handle"},
        ],
    }
    branch_ast_ids = {
        (900, 2): [103, 104],
        (900, 3): [],
        (901, 2): [103, 104],
        (901, 3): [],
        (902, 2): [103, 104],
        (902, 3): [],
    }
    exact_payloads: dict[str, object] = {
        build_flow_query(source_spec, sink_spec): [{"elements": flow}],
    }
    for node_id in [101, 102, 103, 104, 900, 901, 902, 903]:
        exact_payloads[f"cpg.id({node_id}L).file.name.toJsonPretty"] = [str(FIXTURE_PATH)]

    server = FakeJoernServer(
        exact_payloads=exact_payloads,
        controlled_by=controlled_by,
        method_controls={"conditions.handle": controls},
        branch_ast_ids=branch_ast_ids,
    )

    findings = extract_candidate_findings(
        server,  # type: ignore[arg-type]
        joern_project_root=FIXTURE_PATH.parent,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
        sanitizer_specs=(),
    )

    assert len(findings) == 1
    finding = findings[0]
    assert [condition.condition_type for condition in finding.path_conditions] == [
        "type_check",
        "string_length",
        "string_contains",
        "string_eq",
    ]
    assert [condition.expression for condition in finding.path_conditions] == [
        'typeof value === "string"',
        "value.length > 5",
        'value.includes("admin")',
        'role === "admin"',
    ]
    assert all(condition.required_value is True for condition in finding.path_conditions)
    assert [condition.symbolic_constraint for condition in finding.path_conditions] == [
        'TypeCheck(var="value", type="string")',
        'StringLength(var="value", op="gt", n=5)',
        'StringContains(var="value", substr="admin")',
        'StringEq(var="role", val="admin")',
    ]
    assert finding.path_conditions[-1].location.line == 7


def test_extract_candidate_findings_handles_ternary_conditions() -> None:
    source_spec = SourceSpec(
        name="custom_source_ternary",
        pattern='cpg.call.name("customSourceTernary")',
        source_type=SourceType.CUSTOM,
    )
    sink_spec = SinkSpec(
        name="custom_sink_ternary",
        pattern='cpg.call.name("sink")',
        sink_type=SinkType.CUSTOM,
        cwe_id="CWE-79",
    )
    flow = [
        {
            "_id": 201,
            "_label": "METHOD_PARAMETER_IN",
            "name": "input",
            "code": "input",
            "lineNumber": 1,
            "columnNumber": 17,
            "methodFullName": "conditions.handle",
        },
        {
            "_id": 202,
            "_label": "IDENTIFIER",
            "name": "value",
            "code": "value",
            "lineNumber": 20,
            "columnNumber": 30,
            "methodFullName": "conditions.handle",
        },
        {
            "_id": 203,
            "_label": "CALL",
            "name": "sink",
            "code": "sink(value)",
            "lineNumber": 20,
            "columnNumber": 24,
            "methodFullName": "sink",
        },
    ]
    controls = [
        {
            "_id": 904,
            "code": 'value === "expected" ? sink(value) : sinkFallback(value)',
            "lineNumber": 20,
            "columnNumber": 3,
            "controlStructureType": "IF",
            "condition": 'value === "expected"',
        }
    ]
    controlled_by = {
        201: [],
        202: [{"_id": 904, "methodName": "handle", "methodFullName": "conditions.handle"}],
        203: [{"_id": 904, "methodName": "handle", "methodFullName": "conditions.handle"}],
    }
    branch_ast_ids = {
        (904, 2): [202, 203],
        (904, 3): [],
    }
    exact_payloads: dict[str, object] = {
        build_flow_query(source_spec, sink_spec): [{"elements": flow}],
    }
    for node_id in [201, 202, 203, 904]:
        exact_payloads[f"cpg.id({node_id}L).file.name.toJsonPretty"] = [str(FIXTURE_PATH)]

    server = FakeJoernServer(
        exact_payloads=exact_payloads,
        controlled_by=controlled_by,
        method_controls={"conditions.handle": controls},
        branch_ast_ids=branch_ast_ids,
    )

    findings = extract_candidate_findings(
        server,  # type: ignore[arg-type]
        joern_project_root=FIXTURE_PATH.parent,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
        sanitizer_specs=(),
    )

    assert len(findings) == 1
    assert len(findings[0].path_conditions) == 1
    condition = findings[0].path_conditions[0]
    assert condition.condition_type == "string_eq"
    assert condition.expression == 'value === "expected"'
    assert condition.required_value is True
    assert condition.symbolic_constraint == 'StringEq(var="value", val="expected")'
    assert condition.location.line == 20


def test_parse_in_operator_narrowing() -> None:
    loc = SourceLocation(file="a.ts", line=1, column=1, snippet="")
    cond = parse_condition_text("'admin' in user", location=loc, required_value=True)
    assert cond.condition_type == "key_in_object"
    assert "KeyIn" in (cond.symbolic_constraint or "")


def test_parse_array_isarray() -> None:
    loc = SourceLocation(file="a.ts", line=1, column=1, snippet="")
    cond = parse_condition_text("Array.isArray(input)", location=loc, required_value=True)
    assert cond.condition_type == "array_check"
    assert "ArrayIsArray" in (cond.symbolic_constraint or "")


def test_parse_instanceof() -> None:
    loc = SourceLocation(file="a.ts", line=1, column=1, snippet="")
    cond = parse_condition_text("err instanceof HttpError", location=loc, required_value=True)
    assert cond.condition_type == "instanceof"
    assert "HttpError" in (cond.symbolic_constraint or "")


def test_parse_null_check() -> None:
    loc = SourceLocation(file="a.ts", line=1, column=1, snippet="")
    cond = parse_condition_text("value !== null", location=loc, required_value=True)
    assert cond.condition_type == "null_check"
    # negated op flips required_value
    assert cond.required_value is False
    cond_eq = parse_condition_text("value === undefined", location=loc, required_value=True)
    assert cond_eq.condition_type == "null_check"
    assert cond_eq.required_value is True
