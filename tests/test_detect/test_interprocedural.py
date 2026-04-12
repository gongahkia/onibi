from __future__ import annotations

import json
import re
from dataclasses import dataclass
from pathlib import Path

import pytest

from piranesi.detect.flows import extract_candidate_findings
from piranesi.detect.interprocedural import (
    TaintTransfer,
    build_function_summaries,
    extract_interprocedural_findings,
)
from piranesi.scan.joern import JoernServer, is_joern_installed
from piranesi.scan.queries import build_nodes_query
from piranesi.scan.specs import SinkSpec, SourceSpec, get_sink_specs, get_source_specs
from piranesi.scan.transpile import TypeScriptCompilerNotFoundError, transpile_project

FIXTURE_DIR = Path(__file__).resolve().parents[1] / "fixtures" / "typescript" / "phase22_cases"
EXPECT_PATTERN = re.compile(
    r"//\s*@piranesi-expect:\s*(?P<cwe>CWE-\d+),\s*source=(?P<source>[^,]+),\s*sink=(?P<sink>\S+)"
)


class FakeJoernServer:
    def __init__(self, *, exact_payloads: dict[str, object] | None = None) -> None:
        self.exact_payloads = exact_payloads or {}

    def query(self, cpgql: str) -> dict[str, object]:
        payload = self.exact_payloads.get(cpgql, [])
        return {"success": True, "stdout": _joern_json_stdout(payload)}


@dataclass(frozen=True, slots=True)
class _FixtureCase:
    name: str
    source_code: str
    source_line: int
    sink_code: str
    sink_line: int


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


@pytest.mark.parametrize(
    ("fixture", "source_line", "source_code", "sink_line", "sink_code"),
    [
        ("callback_chain.ts", 14, "req.body.sql", 15, "db.query(data)"),
        ("promise_chain.ts", 11, "req.body.sql", 12, "db.query(data)"),
        ("await_chain.ts", 12, "req.body.sql", 14, "db.query(data)"),
        ("event_emitter.ts", 22, "req.body.sql", 17, "db.query(payload)"),
        ("higher_order.ts", 8, "req.body.items", 11, "db.query(item)"),
        ("cross_module_entry.ts", 5, "req.body.sql", 8, "db.query(sql)"),
    ],
)
def test_extract_candidate_findings_detects_interprocedural_patterns(
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
    fixture_path = FIXTURE_DIR / fixture
    sink_file = (
        FIXTURE_DIR / "cross_module_helper.ts"
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

    findings = extract_interprocedural_findings(
        FakeJoernServer(exact_payloads=exact_payloads),  # type: ignore[arg-type]
        joern_project_root=FIXTURE_DIR,
        source_specs=(source_spec,),
        sink_specs=(sink_spec,),
    )

    assert len(findings) == 1
    finding = findings[0]
    assert finding.vuln_class == "CWE-89"
    assert finding.source.location.file == str(fixture_path)
    assert finding.source.location.line == source_line
    assert finding.sink.location.file == str(sink_file)
    assert finding.sink.location.line == sink_line
    assert finding.sink.api_name == "db.query"
    assert finding.metadata["interprocedural"] is True


def test_build_function_summaries_tracks_callback_parameter_flow(tmp_path: Path) -> None:
    fixture_path = tmp_path / "callback_wrapper.ts"
    fixture_path.write_text(
        ("export function forwardToCallback(sql, cb) {\n  cb(null, sql);\n}\n"),
        encoding="utf-8",
    )

    summaries = build_function_summaries(
        FakeJoernServer(exact_payloads={}),  # type: ignore[arg-type]
        joern_project_root=tmp_path,
        source_specs=(),
        sink_specs=(),
    )

    summary = next(
        value for key, value in summaries.items() if key.endswith(":forwardToCallback:1")
    )
    assert (
        TaintTransfer(
            from_param_index=0,
            via_callback_param_index=1,
            to_callback_argument_index=1,
            confidence=0.9,
        )
        in summary.transfers
    )


@pytest.mark.joern
@pytest.mark.integration
def test_phase22_fixture_annotations_match_real_findings() -> None:
    if not is_joern_installed():
        pytest.skip("Joern is not installed in PATH")

    body_source = _source_spec_by_name("express_req_body")
    sql_sink = _sink_spec_by_name("raw_sql_query")

    try:
        transpiled = transpile_project(FIXTURE_DIR)
    except TypeScriptCompilerNotFoundError as exc:  # pragma: no cover - environment dependent
        pytest.skip(str(exc))

    expected = _fixture_expectations(FIXTURE_DIR)
    try:
        with JoernServer(startup_timeout_seconds=30, query_timeout_seconds=30) as server:
            server.import_project(transpiled.out_dir)
            findings = extract_candidate_findings(
                server,
                joern_project_root=transpiled.out_dir,
                source_map=transpiled.source_map,
                source_specs=(body_source,),
                sink_specs=(sql_sink,),
                sanitizer_specs=(),
            )
    finally:
        transpiled.cleanup()

    actual = {
        (
            Path(finding.source.location.file).resolve(),
            finding.vuln_class,
            f"req.body.{finding.source.parameter_name}",
            finding.sink.api_name,
        )
        for finding in findings
    }
    assert actual == expected


def _fixture_expectations(root: Path) -> set[tuple[Path, str, str, str]]:
    expectations: set[tuple[Path, str, str, str]] = set()
    for fixture_path in sorted(root.glob("*.ts")):
        text = fixture_path.read_text(encoding="utf-8")
        for match in EXPECT_PATTERN.finditer(text):
            expectations.add(
                (
                    fixture_path.resolve(),
                    match.group("cwe"),
                    match.group("source"),
                    match.group("sink"),
                )
            )
    return expectations
