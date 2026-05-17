from __future__ import annotations

from pathlib import Path

from piranesi.detect.reachability import analyze_reachability
from piranesi.models import (
    EntryPoint,
    ReachabilityResult,
    ScanMetadata,
    ScannedFunction,
    ScanResult,
)
from piranesi.models.finding import CandidateFinding
from piranesi.models.taint import SourceLocation, TaintSink, TaintSource, TaintStep

FIXTURE_DIR = Path(__file__).resolve().parents[1] / "fixtures" / "typescript" / "reachability_app"


def test_analyze_reachability_prunes_unreachable_findings() -> None:
    scan_result = _scan_result()
    findings = _candidate_findings()

    annotated, reachability = analyze_reachability(
        scan_result,
        findings,
        project_root=FIXTURE_DIR,
    )

    findings_by_id = {finding.id: finding for finding in annotated}
    assert findings_by_id["route-reachable"].reachability == "reachable"
    assert findings_by_id["route-reachable"].severity == "high"
    assert findings_by_id["exported-reachable"].reachability == "reachable"
    assert findings_by_id["cli-reachable"].reachability == "reachable"

    assert findings_by_id["dead-route"].reachability == "unreachable"
    assert findings_by_id["dead-route"].severity == "informational"
    assert findings_by_id["dead-route"].metadata["reachability_original_severity"] == "high"
    assert findings_by_id["never-called"].reachability == "unreachable"
    assert findings_by_id["never-called"].severity == "informational"

    assert reachability.entry_points == {
        "app.js::program:reachableHandler",
        "index.js::program:publicApi",
        "cli.js::program",
    }
    assert reachability.unreachable_functions == {
        "app.js::program:deadEntry",
        "app.js::program:deadQuery",
        "app.js::program:neverCalled",
    }
    assert [function.name for function in reachability.dead_code_functions] == [
        "deadEntry",
        "deadQuery",
        "neverCalled",
    ]


def test_reachability_result_serializes_with_detect_artifacts() -> None:
    _, reachability = analyze_reachability(
        _scan_result(),
        _candidate_findings(),
        project_root=FIXTURE_DIR,
    )

    restored = ReachabilityResult.model_validate_json(reachability.model_dump_json())

    assert restored.entry_points == reachability.entry_points
    assert restored.dead_code_functions[0].name == "deadEntry"


def _scan_result() -> ScanResult:
    app_file = FIXTURE_DIR / "src" / "app.ts"
    index_file = FIXTURE_DIR / "src" / "index.ts"
    cli_file = FIXTURE_DIR / "src" / "cli.ts"
    return ScanResult(
        project_root=str(FIXTURE_DIR),
        files_scanned=[str(app_file), str(index_file), str(cli_file)],
        call_graph={
            "app.js::program:reachableHandler": ["app.js::program:dangerousQuery"],
            "app.js::program:dangerousQuery": [],
            "app.js::program:deadEntry": ["app.js::program:deadQuery"],
            "app.js::program:deadQuery": [],
            "app.js::program:neverCalled": [],
            "index.js::program:publicApi": ["index.js::program:exportedQuery"],
            "index.js::program:exportedQuery": [],
            "cli.js::program": ["cli.js::program:runCli"],
            "cli.js::program:runCli": ["cli.js::program:cliQuery"],
            "cli.js::program:cliQuery": [],
        },
        functions=[
            _function("app.js::program:reachableHandler", "reachableHandler", app_file, 5),
            _function("app.js::program:dangerousQuery", "dangerousQuery", app_file, 10),
            _function("app.js::program:deadEntry", "deadEntry", app_file, 14),
            _function("app.js::program:deadQuery", "deadQuery", app_file, 19),
            _function("app.js::program:neverCalled", "neverCalled", app_file, 23),
            _function("index.js::program:publicApi", "publicApi", index_file, 1),
            _function("index.js::program:exportedQuery", "exportedQuery", index_file, 5),
            _function("cli.js::program", ":program", cli_file, 1),
            _function("cli.js::program:runCli", "runCli", cli_file, 5),
            _function("cli.js::program:cliQuery", "cliQuery", cli_file, 9),
        ],
        entry_points=[
            EntryPoint(
                function_id="app.js::program:reachableHandler",
                location=_location(app_file, 27, 'app.post("/users", reachableHandler);'),
                kind="route_handler",
                http_method="POST",
                route_pattern="/users",
                parameters=["req", "res"],
            )
        ],
        attack_surface=[],
        metadata=ScanMetadata(
            timestamp="2026-04-10T00:00:00Z",
            duration_ms=10,
            tree_sitter_version="test",
            piranesi_version="0.1.0",
            files_parsed=3,
            parse_errors=0,
            config_hash="test",
        ),
    )


def _candidate_findings() -> list[CandidateFinding]:
    app_file = FIXTURE_DIR / "src" / "app.ts"
    index_file = FIXTURE_DIR / "src" / "index.ts"
    cli_file = FIXTURE_DIR / "src" / "cli.ts"
    return [
        _finding(
            finding_id="route-reachable",
            source_file=app_file,
            source_line=6,
            source_snippet="const name = req.body.name;",
            sink_file=app_file,
            sink_line=11,
            sink_snippet='return db.query("SELECT * FROM users WHERE name = \'" + input + "\'");',
            source_type="request_body",
            sink_api="db.query",
            through_functions=(
                "app.js::program:reachableHandler",
                "app.js::program:dangerousQuery",
            ),
        ),
        _finding(
            finding_id="dead-route",
            source_file=app_file,
            source_line=15,
            source_snippet="const orphan = req.body.name;",
            sink_file=app_file,
            sink_line=20,
            sink_snippet='return db.query("SELECT * FROM legacy WHERE name = \'" + input + "\'");',
            source_type="request_body",
            sink_api="db.query",
            through_functions=("app.js::program:deadEntry", "app.js::program:deadQuery"),
        ),
        _finding(
            finding_id="never-called",
            source_file=app_file,
            source_line=23,
            source_snippet="function neverCalled(input) {",
            sink_file=app_file,
            sink_line=24,
            sink_snippet='return db.query("SELECT * FROM ghosts WHERE name = \'" + input + "\'");',
            source_type="local_parameter",
            sink_api="db.query",
            through_functions=("app.js::program:neverCalled",),
        ),
        _finding(
            finding_id="exported-reachable",
            source_file=index_file,
            source_line=1,
            source_snippet="export function publicApi(input) {",
            sink_file=index_file,
            sink_line=6,
            sink_snippet='return db.query("SELECT * FROM exports WHERE name = \'" + input + "\'");',
            source_type="exported_parameter",
            sink_api="db.query",
            through_functions=("index.js::program:publicApi", "index.js::program:exportedQuery"),
        ),
        _finding(
            finding_id="cli-reachable",
            source_file=cli_file,
            source_line=5,
            source_snippet="function runCli(input) {",
            sink_file=cli_file,
            sink_line=10,
            sink_snippet='return db.query("SELECT * FROM cli WHERE name = \'" + input + "\'");',
            source_type="cli_argument",
            sink_api="db.query",
            through_functions=("cli.js::program:runCli", "cli.js::program:cliQuery"),
        ),
    ]


def _function(function_id: str, name: str, file_path: Path, line: int) -> ScannedFunction:
    return ScannedFunction(
        function_id=function_id,
        name=name,
        location=_location(file_path, line, f"function {name}"),
    )


def _finding(
    *,
    finding_id: str,
    source_file: Path,
    source_line: int,
    source_snippet: str,
    sink_file: Path,
    sink_line: int,
    sink_snippet: str,
    source_type: str,
    sink_api: str,
    through_functions: tuple[str, ...],
) -> CandidateFinding:
    source_location = _location(source_file, source_line, source_snippet)
    sink_location = _location(sink_file, sink_line, sink_snippet)
    taint_path = [
        TaintStep(
            location=source_location if index == 0 else sink_location,
            operation="call_arg",
            taint_state="tainted",
            through_function=through_function,
        )
        for index, through_function in enumerate(through_functions)
    ]
    return CandidateFinding(
        id=finding_id,
        vuln_class="CWE-89: SQL Injection",
        source=TaintSource(
            location=source_location,
            source_type=source_type,
            data_categories=["user_input"],
            parameter_name="input",
        ),
        sink=TaintSink(
            location=sink_location,
            sink_type="sql_query",
            api_name=sink_api,
        ),
        taint_path=taint_path,
        path_conditions=[],
        confidence=0.9,
        severity="high",
    )


def _location(file_path: Path, line: int, snippet: str) -> SourceLocation:
    return SourceLocation(
        file=str(file_path.resolve(strict=False)),
        line=line,
        column=1,
        snippet=snippet,
    )
