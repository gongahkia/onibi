from __future__ import annotations

from pathlib import Path

from piranesi.models import (
    CandidateFinding,
    PathCondition,
    SourceLocation,
    TaintSink,
    TaintSource,
    TaintStep,
)
from piranesi.verify.constraints import (
    StringEq,
    TypeCheck,
    extract_exploit_template,
)


def test_extract_exploit_template_infers_route_and_normalizes_constraints(tmp_path: Path) -> None:
    app_file = tmp_path / "app.ts"
    app_file.write_text(
        "\n".join(
            [
                'app.get("/search", (req, res) => {',
                "  const q = req.query.q as string;",
                '  if (typeof q !== "string") {',
                "    return res.status(400).send('bad');",
                "  }",
                "  return res.send(q);",
                "});",
            ]
        ),
        encoding="utf-8",
    )
    finding = _candidate_finding(
        app_file,
        source_line=2,
        source_snippet="const q = req.query.q as string;",
        source_type="req.query.q",
        parameter_name="q",
        path_conditions=[
            _raw_condition(app_file, 'typeof q === "string"'),
            _raw_condition(app_file, "q.length >= 0"),
            _raw_condition(app_file, 'q.includes("admin")'),
            _raw_condition(app_file, 'q.includes("admin")'),
            _raw_condition(app_file, 'q === "superadmin"'),
        ],
    )

    template = extract_exploit_template(finding)

    assert template.http_method == "GET"
    assert template.endpoint == "/search"
    assert len(template.payload_slots) == 1
    assert template.payload_slots[0].carrier == "query"
    assert template.payload_slots[0].name == "q"
    assert template.payload_slots[0].encoding == "query"
    assert template.constraint_sets == (
        (
            TypeCheck(var="q", type_name="string"),
            StringEq(var="q", val="superadmin"),
        ),
    )
    assert template.unsat_reason is None


def test_extract_exploit_template_expands_disjunctions(tmp_path: Path) -> None:
    app_file = tmp_path / "app.ts"
    app_file.write_text(
        "\n".join(
            [
                'app.get("/search", (req, res) => {',
                "  const q = req.query.q;",
                "  return res.send(q);",
                "});",
            ]
        ),
        encoding="utf-8",
    )
    finding = _candidate_finding(
        app_file,
        source_line=2,
        source_snippet="const q = req.query.q;",
        source_type="req.query.q",
        parameter_name="q",
        path_conditions=[
            _raw_condition(app_file, 'q === "admin" || q === "root" || q === "guest"'),
        ],
    )

    template = extract_exploit_template(finding)

    assert template.constraint_sets == (
        (StringEq(var="q", val="admin"),),
        (StringEq(var="q", val="root"),),
        (StringEq(var="q", val="guest"),),
    )


def test_extract_exploit_template_marks_contradictions_unsat(tmp_path: Path) -> None:
    app_file = tmp_path / "app.ts"
    app_file.write_text(
        "\n".join(
            [
                'app.post("/login", (req, res) => {',
                "  const username = req.body.username;",
                "  return res.send(username);",
                "});",
            ]
        ),
        encoding="utf-8",
    )
    finding = _candidate_finding(
        app_file,
        source_line=2,
        source_snippet="const username = req.body.username;",
        source_type="req.body.username",
        parameter_name="username",
        path_conditions=[
            _raw_condition(app_file, 'username === "alice"'),
            _raw_condition(app_file, 'username === "bob"'),
        ],
    )

    template = extract_exploit_template(finding)

    assert template.http_method == "POST"
    assert template.endpoint == "/login"
    assert template.payload_slots[0].carrier == "body"
    assert template.payload_slots[0].encoding == "json"
    assert template.constraint_sets == ()
    assert template.unsat_reason == "CONSTRAINTS_UNSATISFIABLE"


def _candidate_finding(
    file_path: Path,
    *,
    source_line: int,
    source_snippet: str,
    source_type: str,
    parameter_name: str,
    path_conditions: list[PathCondition],
    vuln_class: str = "CWE-79",
) -> CandidateFinding:
    source_location = SourceLocation(
        file=str(file_path),
        line=source_line,
        column=11,
        snippet=source_snippet,
    )
    sink_location = SourceLocation(
        file=str(file_path),
        line=source_line + 1,
        column=9,
        snippet="res.send(q);",
    )
    step_location = SourceLocation(
        file=str(file_path),
        line=source_line + 1,
        column=9,
        snippet="return res.send(q);",
    )
    return CandidateFinding(
        id="finding-verify",
        vuln_class=vuln_class,
        source=TaintSource(
            location=source_location,
            source_type=source_type,
            data_categories=["identifier"],
            parameter_name=parameter_name,
        ),
        sink=TaintSink(
            location=sink_location,
            sink_type="html_output",
            api_name="res.send",
        ),
        taint_path=[
            TaintStep(
                location=step_location,
                operation="call_arg",
                taint_state="tainted",
            )
        ],
        path_conditions=path_conditions,
        confidence=0.9,
        severity="medium",
    )


def _raw_condition(
    file_path: Path,
    expression: str,
    *,
    required_value: bool = True,
) -> PathCondition:
    return PathCondition(
        location=SourceLocation(
            file=str(file_path),
            line=1,
            column=1,
            snippet=expression,
        ),
        condition_type="branch",
        expression=expression,
        required_value=required_value,
        symbolic_constraint=None,
    )
