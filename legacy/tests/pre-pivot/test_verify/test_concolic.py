from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from textwrap import dedent

import pytest

from piranesi.models import (
    CandidateFinding,
    EntryPoint,
    SourceLocation,
    TaintSink,
    TaintSource,
    TaintStep,
)
from piranesi.verify.concolic import ConcolicInput, build_concolic_input, concolic_verify
from piranesi.verify.constraints import ExploitTemplate, PayloadSlot, StringEq
from piranesi.verify.solver import solve_exploit_template


@dataclass(frozen=True, slots=True)
class ConcolicCase:
    name: str
    source: str
    expected_status: str
    vuln_class: str = "CWE-89"
    source_type: str = "req.query.q"
    parameter_name: str = "q"
    api_name: str = "db.query"
    sink_snippet: str | None = None
    expected_payload_fragment: str | None = None
    expected_model_value: str | None = None
    loop_bound: int = 3
    max_paths: int = 100
    timeout_ms: int = 120_000


CASES = (
    ConcolicCase(
        name="string concat",
        source="""
        function handler(req, db) {
          const q = req.query.q;
          const sql = "SELECT * FROM users WHERE name = '" + q + "'";
          db.query(sql);
        }
        """,
        expected_status="SAT",
        expected_payload_fragment="'",
        sink_snippet="db.query(sql);",
    ),
    ConcolicCase(
        name="slice unsat",
        source="""
        function handler(req, db) {
          const q = req.query.q.slice(0, 0);
          db.query(q);
        }
        """,
        expected_status="UNSAT",
        sink_snippet="db.query(q);",
    ),
    ConcolicCase(
        name="indexOf branch",
        source="""
        function handler(req, db) {
          const q = req.query.q;
          if (q.indexOf("'") >= 0) {
            db.query(q);
          } else {
            db.query(q.slice(0, 0));
          }
        }
        """,
        expected_status="SAT",
        expected_payload_fragment="'",
        sink_snippet="db.query(q);",
    ),
    ConcolicCase(
        name="includes branch",
        source="""
        function handler(req, db) {
          const q = req.query.q;
          if (q.includes("'")) {
            db.query(q);
          } else {
            db.query(q.slice(0, 0));
          }
        }
        """,
        expected_status="SAT",
        expected_payload_fragment="'",
        sink_snippet="db.query(q);",
    ),
    ConcolicCase(
        name="startsWith xss",
        source="""
        function handler(req, res) {
          const q = req.query.q;
          if (q.startsWith("<script>")) {
            res.send(q);
          } else {
            res.send(q.slice(0, 0));
          }
        }
        """,
        expected_status="SAT",
        vuln_class="CWE-79",
        api_name="res.send",
        sink_snippet="res.send(q);",
        expected_payload_fragment="<script>",
    ),
    ConcolicCase(
        name="template literal xss",
        source="""
        function handler(req, res) {
          const html = `<div>${req.query.q}</div>`;
          res.send(html);
        }
        """,
        expected_status="SAT",
        vuln_class="CWE-79",
        api_name="res.send",
        sink_snippet="res.send(html);",
        expected_payload_fragment="<script>",
    ),
    ConcolicCase(
        name="parseInt condition",
        source="""
        function handler(req, db) {
          const count = req.query.count;
          if (parseInt(count) > 7) {
            db.query(count);
          }
        }
        """,
        expected_status="SAT",
        vuln_class="CWE-200",
        source_type="req.query.count",
        parameter_name="count",
        sink_snippet="db.query(count);",
        expected_model_value="8",
    ),
    ConcolicCase(
        name="loose equality",
        source="""
        function handler(req, db) {
          const count = req.query.count;
          if (count == 7) {
            db.query(count);
          }
        }
        """,
        expected_status="SAT",
        vuln_class="CWE-200",
        source_type="req.query.count",
        parameter_name="count",
        sink_snippet="db.query(count);",
        expected_model_value="7",
    ),
    ConcolicCase(
        name="destructuring",
        source="""
        function handler(req, db) {
          const { q } = req.query;
          db.query(q);
        }
        """,
        expected_status="SAT",
        expected_payload_fragment="'",
        sink_snippet="db.query(q);",
    ),
    ConcolicCase(
        name="spread",
        source="""
        function handler(req, db) {
          const merged = { ...req.query };
          db.query(merged.q);
        }
        """,
        expected_status="SAT",
        expected_payload_fragment="'",
        sink_snippet="db.query(merged.q);",
    ),
    ConcolicCase(
        name="computed property",
        source="""
        function handler(req, db) {
          const key = "q";
          const sql = req.query[key];
          db.query(sql);
        }
        """,
        expected_status="SAT",
        expected_payload_fragment="'",
        sink_snippet="db.query(sql);",
    ),
    ConcolicCase(
        name="function call",
        source="""
        function helper(q) {
          return q + "'";
        }

        function handler(req, db) {
          const sql = helper(req.query.q);
          db.query(sql);
        }
        """,
        expected_status="SAT",
        expected_payload_fragment="'",
        sink_snippet="db.query(sql);",
    ),
    ConcolicCase(
        name="for loop",
        source="""
        function handler(req, db) {
          let q = req.query.q;
          for (let i = 0; i < 2; i++) {
            q = q + "'";
          }
          db.query(q);
        }
        """,
        expected_status="SAT",
        expected_payload_fragment="'",
        sink_snippet="db.query(q);",
    ),
    ConcolicCase(
        name="while loop",
        source="""
        function handler(req, db) {
          let i = 0;
          let q = req.query.q;
          while (i < 2) {
            q = q + "'";
            i = i + 1;
          }
          db.query(q);
        }
        """,
        expected_status="SAT",
        expected_payload_fragment="'",
        sink_snippet="db.query(q);",
    ),
)


@pytest.mark.parametrize("case", CASES, ids=[case.name for case in CASES])
def test_concolic_cases(case: ConcolicCase) -> None:
    result = _run_case(case)
    assert result.status == case.expected_status, case.name
    if case.expected_payload_fragment is not None:
        assert result.model_values is not None, case.name
        assert case.parameter_name in result.model_values, case.name
        assert case.expected_payload_fragment in result.model_values[case.parameter_name], case.name
    if case.expected_model_value is not None:
        assert result.model_values is not None, case.name
        assert result.model_values[case.parameter_name] == case.expected_model_value, case.name


def test_concolic_times_out_when_budget_is_zero() -> None:
    result = _run_case(
        ConcolicCase(
            name="timeout",
            source="""
            function handler(req, db) {
              db.query(req.query.q);
            }
            """,
            expected_status="TIMEOUT",
            timeout_ms=0,
            sink_snippet="db.query(req.query.q);",
        )
    )

    assert result.status == "TIMEOUT"


def test_concolic_limits_branch_exploration() -> None:
    result = _run_case(
        ConcolicCase(
            name="max paths",
            source="""
            function handler(req, db) {
              const q = req.query.q;
              if (q.length > 10) {
                db.query(q.slice(0, 0));
              } else {
                db.query(q);
              }
            }
            """,
            expected_status="TIMEOUT",
            max_paths=1,
            sink_snippet="db.query(q);",
        )
    )

    assert result.status == "TIMEOUT"
    assert result.paths_explored == 1


def test_concolic_regex_alnum_validator_rejects_special_payload() -> None:
    result = _run_case(
        ConcolicCase(
            name="regex alnum validator",
            source="""
            function handler(req, db) {
              const q = req.query.q;
              if (/^[a-z0-9]+$/.test(q)) {
                db.query(q);
              }
            }
            """,
            expected_status="UNSAT",
            sink_snippet="db.query(q);",
        )
    )

    assert result.status == "UNSAT"


def test_concolic_regex_without_anchors_matches_substring() -> None:
    result = _run_case(
        ConcolicCase(
            name="regex substring",
            source="""
            function handler(req, db) {
              const q = req.query.q;
              if (q.match(/admin/) && q === "xadminy") {
                db.query(q);
              }
            }
            """,
            expected_status="SAT",
            vuln_class="CWE-200",
            sink_snippet="db.query(q);",
            expected_model_value="xadminy",
        )
    )

    assert result.status == "SAT"
    assert result.model_values is not None
    assert result.model_values["q"] == "xadminy"


def test_concolic_regex_numeric_validator_rejects_special_payload() -> None:
    result = _run_case(
        ConcolicCase(
            name="regex numeric validator",
            source=r"""
            function handler(req, db) {
              const q = req.query.q;
              if (/^\d+$/.test(q)) {
                db.query(q);
              }
            }
            """,
            expected_status="UNSAT",
            sink_snippet="db.query(q);",
        )
    )

    assert result.status == "UNSAT"


def test_concolic_unsupported_regex_falls_back_to_fresh_bool() -> None:
    result = _run_case(
        ConcolicCase(
            name="regex unsupported lookbehind",
            source="""
            function handler(req, db) {
              const q = req.query.q;
              if (q.search(/(?<=foo)bar/)) {
                db.query(q);
              }
            }
            """,
            expected_status="SAT",
            sink_snippet="db.query(q);",
            expected_payload_fragment="'",
        )
    )

    assert result.status == "SAT"
    assert result.model_values is not None
    assert "'" in result.model_values["q"]


def test_build_concolic_input_reads_source_file(tmp_path: Path) -> None:
    source_path = tmp_path / "app.js"
    source_path.write_text(
        dedent(
            """
            function handler(req, db) {
              const q = req.query.q;
              db.query(q);
            }
            """
        ).strip()
        + "\n",
        encoding="utf-8",
    )
    finding = _make_finding(
        source_type="req.query.q",
        parameter_name="q",
        api_name="db.query",
        sink_snippet="db.query(q);",
        source_file=source_path,
    )

    concolic_input = build_concolic_input(finding)

    assert concolic_input is not None
    assert concolic_input.entry_point is not None
    assert concolic_input.function_asts["handler"].startswith("function handler")


def test_solver_falls_back_to_concolic_for_unverifiable_template() -> None:
    concolic_input = _make_input(
        dedent(
            """
            function handler(req, db) {
              const q = req.query.q;
              db.query(q);
            }
            """
        ),
        sink_snippet="db.query(q);",
    )

    result = solve_exploit_template(_unverifiable_template("q"), concolic_input=concolic_input)

    assert result.status == "SAT"
    assert result.concolic_result is not None
    assert result.concolic_result.status == "SAT"
    assert result.solutions[0].payload.payload_values["q"] == "'"


def test_solver_auto_builds_concolic_input_from_finding(tmp_path: Path) -> None:
    source_path = tmp_path / "app.js"
    source_path.write_text(
        dedent(
            """
            function handler(req, db) {
              const q = req.query.q;
              db.query(q);
            }
            """
        ).strip()
        + "\n",
        encoding="utf-8",
    )
    finding = _make_finding(
        source_type="req.query.q",
        parameter_name="q",
        api_name="db.query",
        sink_snippet="db.query(q);",
        source_file=source_path,
    )

    result = solve_exploit_template(_unverifiable_template("q"), finding=finding)

    assert result.status == "SAT"
    assert result.concolic_result is not None
    assert result.concolic_result.status == "SAT"


def test_solver_keeps_unverifiable_when_concolic_proves_unsat() -> None:
    concolic_input = _make_input(
        dedent(
            """
            function handler(req, db) {
              const q = req.query.q.slice(0, 0);
              db.query(q);
            }
            """
        ),
        sink_snippet="db.query(q);",
    )

    result = solve_exploit_template(_unverifiable_template("q"), concolic_input=concolic_input)

    assert result.status == "UNVERIFIABLE"
    assert result.concolic_result is not None
    assert result.concolic_result.status == "UNSAT"
    assert result.reason == "path constraints unsatisfiable at sink"


def _run_case(case: ConcolicCase):
    concolic_input = _make_input(
        dedent(case.source),
        vuln_class=case.vuln_class,
        source_type=case.source_type,
        parameter_name=case.parameter_name,
        api_name=case.api_name,
        sink_snippet=case.sink_snippet,
    )
    return concolic_verify(
        concolic_input,
        max_paths=case.max_paths,
        timeout_ms=case.timeout_ms,
        loop_bound=case.loop_bound,
    )


def _make_input(
    source: str,
    *,
    vuln_class: str = "CWE-89",
    source_type: str = "req.query.q",
    parameter_name: str = "q",
    api_name: str = "db.query",
    sink_snippet: str | None = None,
) -> ConcolicInput:
    finding = _make_finding(
        source_type=source_type,
        parameter_name=parameter_name,
        api_name=api_name,
        sink_snippet=sink_snippet or f"{api_name}({parameter_name});",
    )
    return ConcolicInput(
        finding=finding.model_copy(update={"vuln_class": vuln_class}),
        taint_path=list(finding.taint_path),
        function_asts={"handler": source},
        call_graph={},
        entry_point=EntryPoint(
            function_id="handler",
            location=finding.source.location,
            kind="route_handler",
            parameters=["req", "db", "res"],
        ),
    )


def _make_finding(
    *,
    source_type: str,
    parameter_name: str,
    api_name: str,
    sink_snippet: str,
    source_file: Path | None = None,
) -> CandidateFinding:
    file_path = str(source_file or Path("app.js"))
    return CandidateFinding(
        id=f"finding-{parameter_name}",
        vuln_class="CWE-89",
        source=TaintSource(
            location=SourceLocation(
                file=file_path,
                line=1,
                column=1,
                snippet=f"const {parameter_name} = {source_type};",
            ),
            source_type=source_type,
            data_categories=["input"],
            parameter_name=parameter_name,
        ),
        sink=TaintSink(
            location=SourceLocation(
                file=file_path,
                line=99,
                column=1,
                snippet=sink_snippet,
            ),
            sink_type="sink",
            api_name=api_name,
        ),
        taint_path=[
            TaintStep(
                location=SourceLocation(
                    file=file_path,
                    line=1,
                    column=1,
                    snippet=sink_snippet,
                ),
                operation="call_arg",
                taint_state="tainted",
                through_function="handler",
            )
        ],
        path_conditions=[],
        confidence=0.9,
        severity="high",
    )


def _unverifiable_template(slot_name: str) -> ExploitTemplate:
    return ExploitTemplate(
        vuln_class="CWE-89",
        http_method="GET",
        endpoint="/",
        payload_slots=(
            PayloadSlot(
                name=slot_name,
                carrier="query",
                field_path=(slot_name,),
                source=f"req.query.{slot_name}",
                encoding="query",
            ),
        ),
        path_conditions=(),
        constraint_sets=(
            (
                StringEq(var=slot_name, val="alice"),
                StringEq(var=slot_name, val="bob"),
            ),
        ),
        unsat_reason=None,
    )
