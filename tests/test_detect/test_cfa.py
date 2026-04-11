from __future__ import annotations

import json
import time
from dataclasses import dataclass
from pathlib import Path

import pytest

from piranesi.config import DetectConfig
from piranesi.detect.cfa import (
    CallContext,
    ContextAnalysisConfig,
    ContextSensitiveStore,
    DispatchCall,
    DispatchFunction,
    DispatchResolver,
    TaintSignature,
)
from piranesi.detect.interprocedural import extract_interprocedural_findings
from piranesi.scan.queries import build_nodes_query
from piranesi.scan.specs import SinkSpec, SourceSpec, get_sink_specs, get_source_specs

PHASE22_FIXTURE_DIR = (
    Path(__file__).resolve().parents[1] / "fixtures" / "typescript" / "phase22_cases"
)


class FakeJoernServer:
    def __init__(self, *, exact_payloads: dict[str, object] | None = None) -> None:
        self.exact_payloads = exact_payloads or {}

    def query(self, cpgql: str) -> dict[str, object]:
        payload = self.exact_payloads.get(cpgql, [])
        return {"success": True, "stdout": _joern_json_stdout(payload)}


@dataclass(frozen=True, slots=True)
class _PrecisionCase:
    name: str
    code: str


def _joern_json_stdout(payload: object) -> str:
    return f'val res0: String = """{json.dumps(payload, indent=2)}"""'


def _source_spec_by_name(name: str) -> SourceSpec:
    return next(spec for spec in get_source_specs(frameworks=("express",)) if spec.name == name)


def _sink_spec_by_name(name: str) -> SinkSpec:
    return next(spec for spec in get_sink_specs(frameworks=("express",)) if spec.name == name)


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


def _register_file_queries(
    exact_payloads: dict[str, object],
    file_path: Path,
    *node_ids: int,
) -> None:
    for node_id in node_ids:
        exact_payloads[f"cpg.id({node_id}L).file.name.toJsonPretty"] = [str(file_path)]


def _line_number(text: str, snippet: str) -> int:
    for index, line in enumerate(text.splitlines(), start=1):
        if snippet in line:
            return index
    raise AssertionError(f"snippet not found: {snippet!r}")


def _precision_fixture(body: str) -> str:
    return "\n".join(
        [
            "declare class SafeService {",
            "  handle(sql: string): string;",
            "}",
            "",
            "const db = {",
            "  query(sql: string) {",
            "    return sql;",
            "  },",
            "};",
            "",
            "class QueryService {",
            "  handle(sql: string) {",
            "    db.query(sql);",
            "    return sql;",
            "  }",
            "}",
            "",
            body,
            "",
        ]
    )


PRECISION_CASES = (
    _PrecisionCase(
        name="typed-parameter-direct",
        code=_precision_fixture(
            "export function route(service: SafeService, req: { body: { sql: string } }) {\n"
            "  return service.handle(req.body.sql);\n"
            "}"
        ),
    ),
    _PrecisionCase(
        name="typed-local-alias",
        code=_precision_fixture(
            "export function route(service: SafeService, req: { body: { sql: string } }) {\n"
            "  const local: SafeService = service;\n"
            "  return local.handle(req.body.sql);\n"
            "}"
        ),
    ),
    _PrecisionCase(
        name="typed-constructor-const",
        code=_precision_fixture(
            "export function route(req: { body: { sql: string } }) {\n"
            "  const service: SafeService = new SafeService();\n"
            "  return service.handle(req.body.sql);\n"
            "}"
        ),
    ),
    _PrecisionCase(
        name="constructor-let",
        code=_precision_fixture(
            "export function route(req: { body: { sql: string } }) {\n"
            "  let service = new SafeService();\n"
            "  return service.handle(req.body.sql);\n"
            "}"
        ),
    ),
    _PrecisionCase(
        name="lifted-sql-local",
        code=_precision_fixture(
            "export function route(service: SafeService, req: { body: { sql: string } }) {\n"
            "  const sql = req.body.sql;\n"
            "  return service.handle(sql);\n"
            "}"
        ),
    ),
    _PrecisionCase(
        name="typed-helper",
        code=_precision_fixture(
            "function invoke(service: SafeService, sql: string) {\n"
            "  return service.handle(sql);\n"
            "}\n"
            "\n"
            "export function route(service: SafeService, req: { body: { sql: string } }) {\n"
            "  return invoke(service, req.body.sql);\n"
            "}"
        ),
    ),
    _PrecisionCase(
        name="typed-helper-constructor",
        code=_precision_fixture(
            "function invoke(service: SafeService, sql: string) {\n"
            "  return service.handle(sql);\n"
            "}\n"
            "\n"
            "export function route(req: { body: { sql: string } }) {\n"
            "  const service = new SafeService();\n"
            "  return invoke(service, req.body.sql);\n"
            "}"
        ),
    ),
    _PrecisionCase(
        name="type-cast-alias",
        code=_precision_fixture(
            "declare function factory(): unknown;\n"
            "\n"
            "export function route(req: { body: { sql: string } }) {\n"
            "  const service = factory() as SafeService;\n"
            "  return service.handle(req.body.sql);\n"
            "}"
        ),
    ),
    _PrecisionCase(
        name="class-method-wrapper",
        code=_precision_fixture(
            "class Runner {\n"
            "  execute(service: SafeService, req: { body: { sql: string } }) {\n"
            "    return service.handle(req.body.sql);\n"
            "  }\n"
            "}\n"
            "\n"
            "export function route(service: SafeService, req: { body: { sql: string } }) {\n"
            "  const runner = new Runner();\n"
            "  return runner.execute(service, req);\n"
            "}"
        ),
    ),
    _PrecisionCase(
        name="double-helper-chain",
        code=_precision_fixture(
            "function second(service: SafeService, sql: string) {\n"
            "  return service.handle(sql);\n"
            "}\n"
            "\n"
            "function first(service: SafeService, req: { body: { sql: string } }) {\n"
            "  return second(service, req.body.sql);\n"
            "}\n"
            "\n"
            "export function route(service: SafeService, req: { body: { sql: string } }) {\n"
            "  return first(service, req);\n"
            "}"
        ),
    ),
)


TP_CASES = (
    ("callback_chain.ts", 14, "req.body.sql", 15, "db.query(data)"),
    ("promise_chain.ts", 11, "req.body.sql", 12, "db.query(data)"),
    ("await_chain.ts", 12, "req.body.sql", 14, "db.query(data)"),
    ("event_emitter.ts", 22, "req.body.sql", 17, "db.query(payload)"),
    ("higher_order.ts", 8, "req.body.items", 11, "db.query(item)"),
    ("cross_module_entry.ts", 5, "req.body.sql", 8, "db.query(sql)"),
)


def _run_case(
    root: Path,
    code: str,
    *,
    detect_config: DetectConfig,
    source_snippet: str = "req.body.sql",
    sink_snippet: str = "db.query(sql)",
) -> tuple:
    fixture_path = root / "case.ts"
    fixture_path.write_text(code, encoding="utf-8")
    source_spec = _source_spec_by_name("express_req_body")
    sink_spec = _sink_spec_by_name("raw_sql_query")
    source_id = 7001
    sink_id = 7002
    source_line = _line_number(code, source_snippet)
    sink_line = _line_number(code, sink_snippet)
    exact_payloads = {
        build_nodes_query(source_spec.pattern): [
            _node(
                source_id,
                label="CALL",
                name="<operator>.fieldAccess",
                code=source_snippet,
                line=source_line,
                column=10,
                method_full_name="<operator>.fieldAccess",
            )
        ],
        build_nodes_query(sink_spec.pattern): [
            _node(
                sink_id,
                label="CALL",
                name="query",
                code=sink_snippet,
                line=sink_line,
                column=5,
                method_full_name="db.query",
            )
        ],
    }
    _register_file_queries(exact_payloads, fixture_path, source_id, sink_id)
    return extract_interprocedural_findings(
        FakeJoernServer(exact_payloads=exact_payloads),  # type: ignore[arg-type]
        joern_project_root=root,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
        detect_config=detect_config,
    )


@pytest.mark.parametrize("case", PRECISION_CASES, ids=lambda case: case.name)
def test_context_sensitive_precision_reduces_false_positives(
    tmp_path: Path,
    case: _PrecisionCase,
) -> None:
    findings_zero = _run_case(
        tmp_path,
        case.code,
        detect_config=DetectConfig(context_sensitivity=0),
    )
    findings_one = _run_case(
        tmp_path,
        case.code,
        detect_config=DetectConfig(context_sensitivity=1),
    )

    assert len(findings_zero) == 1
    assert len(findings_one) == 0


@pytest.mark.parametrize(
    ("fixture", "source_line", "source_code", "sink_line", "sink_code"),
    TP_CASES,
)
def test_context_sensitive_analysis_preserves_known_true_positives(
    fixture: str,
    source_line: int,
    source_code: str,
    sink_line: int,
    sink_code: str,
) -> None:
    source_spec = _source_spec_by_name("express_req_body")
    sink_spec = _sink_spec_by_name("raw_sql_query")
    source_id = 9001
    sink_id = 9002
    fixture_path = PHASE22_FIXTURE_DIR / fixture
    sink_file = (
        PHASE22_FIXTURE_DIR / "cross_module_helper.ts"
        if fixture == "cross_module_entry.ts"
        else fixture_path
    )
    exact_payloads = {
        build_nodes_query(source_spec.pattern): [
            _node(
                source_id,
                label="CALL",
                name="<operator>.fieldAccess",
                code=source_code,
                line=source_line,
                column=10,
                method_full_name="<operator>.fieldAccess",
            )
        ],
        build_nodes_query(sink_spec.pattern): [
            _node(
                sink_id,
                label="CALL",
                name="query",
                code=sink_code,
                line=sink_line,
                column=5,
                method_full_name="db.query",
            )
        ],
    }
    _register_file_queries(exact_payloads, fixture_path, source_id)
    _register_file_queries(exact_payloads, sink_file, sink_id)
    server = FakeJoernServer(exact_payloads=exact_payloads)  # type: ignore[arg-type]

    zero_findings = extract_interprocedural_findings(
        server,
        joern_project_root=PHASE22_FIXTURE_DIR,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
        detect_config=DetectConfig(context_sensitivity=0),
    )
    one_findings = extract_interprocedural_findings(
        server,
        joern_project_root=PHASE22_FIXTURE_DIR,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
        detect_config=DetectConfig(context_sensitivity=1),
    )

    assert len(zero_findings) == 1
    assert len(one_findings) == 1
    assert one_findings[0].vuln_class == zero_findings[0].vuln_class == "CWE-89"


def test_call_context_truncates_to_k() -> None:
    context = CallContext.empty().extend("a", 1, k=2).extend("b", 2, k=2).extend("c", 3, k=2)
    assert context.chain == (("b", 2), ("c", 3))


def test_context_store_lazily_splits_after_second_distinct_request() -> None:
    store = ContextSensitiveStore(ContextAnalysisConfig(context_sensitivity=1, max_contexts=1000))
    function_key = "fixture"
    signature = TaintSignature.from_indexes([0])
    first_context = CallContext.empty().extend("routeA", 10, k=1)
    second_context = CallContext.empty().extend("routeB", 20, k=1)

    assert store.effective_context(function_key, first_context, signature) == CallContext.empty()
    assert store.effective_context(function_key, second_context, signature) == second_context


def test_context_store_collapses_when_budget_is_exceeded() -> None:
    store = ContextSensitiveStore(ContextAnalysisConfig(context_sensitivity=1, max_contexts=1))
    function_key = "fixture"
    signature = TaintSignature.from_indexes([0])
    first_context = CallContext.empty().extend("routeA", 10, k=1)
    second_context = CallContext.empty().extend("routeB", 20, k=1)
    store.put(function_key, first_context, signature, object())

    assert store.effective_context(function_key, second_context, signature) == CallContext.empty()
    assert store.is_collapsed(function_key) is True


def test_dispatch_resolver_supports_python_mro() -> None:
    resolver = DispatchResolver(
        functions=(
            DispatchFunction(
                function_key="base",
                name="get_user",
                module_path="service.py",
                language="python",
                class_name="BaseService",
            ),
            DispatchFunction(
                function_key="child",
                name="get_user",
                module_path="service.py",
                language="python",
                class_name="ChildService",
            ),
        ),
        bases_by_class={
            "GrandChildService": ("ChildService",),
            "ChildService": ("BaseService",),
        },
    )

    resolved = resolver.resolve(
        DispatchCall(
            callee="get_user",
            receiver="service",
            receiver_types=("GrandChildService",),
            language="python",
        )
    )

    assert [function.function_key for function in resolved] == ["child"]


def test_context_timeout_marks_findings_as_degraded(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    code = "\n".join(
        [
            "function forward(sql: string) {",
            "  db.query(sql);",
            "}",
            "",
            "const db = {",
            "  query(sql: string) {",
            "    return sql;",
            "  },",
            "};",
            "",
            "export function route(req: { body: { sql: string } }) {",
            "  forward(req.body.sql);",
            "}",
            "",
        ]
    )
    ticks = iter([0.0, 2.0, 2.0, 2.0, 2.0, 2.0, 2.0])
    monkeypatch.setattr(
        "piranesi.detect.interprocedural.time.monotonic",
        lambda: next(ticks, 2.0),
    )

    findings = _run_case(
        tmp_path,
        code,
        detect_config=DetectConfig(context_sensitivity=1, context_timeout=1),
    )

    assert len(findings) == 1
    assert findings[0].metadata["context_sensitivity_degraded"] is True


@pytest.mark.slow
def test_context_sensitive_small_benchmark_stays_bounded(tmp_path: Path) -> None:
    helper_blocks = "\n\n".join(
        (
            f"function helper{index}(service: SafeService, sql: string) {{\n"
            f"  return service.handle(sql);\n"
            f"}}"
        )
        for index in range(40)
    )
    route_calls = "\n".join(
        f"  helper{index}(service, req.body.sql);" for index in range(40)
    )
    code = _precision_fixture(
        helper_blocks
        + "\n\n"
        + "export function route(service: SafeService, req: { body: { sql: string } }) {\n"
        + route_calls
        + "\n  return req.body.sql;\n"
        + "}"
    )

    started_zero = time.perf_counter()
    zero_findings = _run_case(
        tmp_path,
        code,
        detect_config=DetectConfig(context_sensitivity=0),
    )
    zero_duration = time.perf_counter() - started_zero

    started_one = time.perf_counter()
    one_findings = _run_case(
        tmp_path,
        code,
        detect_config=DetectConfig(context_sensitivity=1),
    )
    one_duration = time.perf_counter() - started_one

    assert len(zero_findings) == 1
    assert len(one_findings) == 0
    assert one_duration <= max(zero_duration, 0.001) * 25
