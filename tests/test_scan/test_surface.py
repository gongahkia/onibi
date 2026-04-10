from __future__ import annotations

from collections.abc import Generator
from pathlib import Path

import pytest

from piranesi.detect.flows import extract_candidate_findings
from piranesi.models import ScanMetadata
from piranesi.scan.joern import JoernServer, is_joern_installed
from piranesi.scan.specs import (
    SanitizerSpec,
    SinkSpec,
    SourceSpec,
    get_sanitizer_specs,
    get_sink_specs,
    get_source_specs,
)
from piranesi.scan.surface import build_scan_result
from piranesi.scan.transpile import SourceMap

TAINT_APP_DIR = Path(__file__).resolve().parents[1] / "fixtures" / "typescript" / "taint_app"
TAINT_APP_TRANSPILED_DIR = TAINT_APP_DIR / "transpiled"


def _source_spec_by_name(name: str) -> SourceSpec:
    return next(spec for spec in get_source_specs() if spec.name == name)


def _sink_spec_by_name(name: str) -> SinkSpec:
    return next(spec for spec in get_sink_specs() if spec.name == name)


def _sanitizer_spec_by_name(name: str) -> SanitizerSpec:
    return next(spec for spec in get_sanitizer_specs() if spec.name == name)


@pytest.fixture(scope="module")
def joern_server() -> Generator[JoernServer, None, None]:
    if not is_joern_installed():
        pytest.skip("Joern is not installed in PATH")

    with JoernServer(port=8128, startup_timeout_seconds=30, query_timeout_seconds=30) as server:
        server.import_project(TAINT_APP_TRANSPILED_DIR)
        yield server


@pytest.mark.joern
@pytest.mark.integration
def test_build_scan_result_maps_entry_points_and_attack_surface(joern_server: JoernServer) -> None:
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

    result = build_scan_result(
        joern_server,
        project_root=TAINT_APP_DIR,
        joern_project_root=TAINT_APP_TRANSPILED_DIR,
        source_map=source_map,
        source_specs=(body_source, query_source),
        candidate_findings=findings,
        metadata=ScanMetadata(
            timestamp="2026-04-09T00:00:00Z",
            duration_ms=42,
            tree_sitter_version="n/a",
            piranesi_version="0.1.0",
            files_parsed=1,
            parse_errors=0,
            config_hash="test-config",
        ),
    )

    assert result.project_root == str(TAINT_APP_DIR.resolve())
    assert result.files_scanned == [str((TAINT_APP_DIR / "app.ts").resolve())]
    assert "app.js::program:userHandler" in result.call_graph
    assert "app.js::program:commandHandler" in result.call_graph

    entry_points = {entry_point.route_pattern: entry_point for entry_point in result.entry_points}
    assert set(entry_points) == {"/users", "/cmd"}
    assert entry_points["/users"].http_method == "POST"
    assert entry_points["/users"].function_id == "app.js::program:userHandler"
    assert entry_points["/users"].parameters == ["req", "res", "db"]
    assert entry_points["/users"].location.file.endswith("app.ts")
    assert entry_points["/cmd"].http_method == "GET"
    assert entry_points["/cmd"].function_id == "app.js::program:commandHandler"
    assert entry_points["/cmd"].parameters == ["req", "child"]

    attack_surface = {(node.function_id, node.source_type): node for node in result.attack_surface}
    user_surface = attack_surface[("app.js::program:userHandler", "request_body")]
    assert user_surface.location.file.endswith("app.ts")
    assert user_surface.location.line == 8
    assert user_surface.data_flow_to == ["db.query", "res.send"]
    assert user_surface.sanitizers_on_path == ["escape"]

    cmd_surface = attack_surface[("app.js::program:commandHandler", "request_param")]
    assert cmd_surface.location.line == 16
    assert cmd_surface.data_flow_to == ["child.exec"]
    assert cmd_surface.sanitizers_on_path == []
