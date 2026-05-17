from __future__ import annotations

from pathlib import Path

import pytest

from piranesi.detect.flows import extract_candidate_findings
from piranesi.models import ScanMetadata
from piranesi.scan.framework import detect_frameworks, resolve_frameworks
from piranesi.scan.joern import JoernServer, is_joern_installed
from piranesi.scan.specs import get_sanitizer_specs, get_sink_specs, get_source_specs
from piranesi.scan.surface import build_scan_result
from piranesi.scan.transpile import (
    TranspiledProject,
    TypeScriptCompilerNotFoundError,
    transpile_project,
)

NEXTJS_APP_DIR = Path(__file__).resolve().parents[1] / "fixtures" / "typescript" / "nextjs_app"


def _transpile_or_skip(project_dir: Path) -> TranspiledProject:
    try:
        return transpile_project(project_dir)
    except TypeScriptCompilerNotFoundError as exc:  # pragma: no cover - environment dependent
        pytest.skip(str(exc))


@pytest.mark.joern
@pytest.mark.integration
def test_nextjs_fixture_detects_routes_sources_and_server_actions() -> None:
    if not is_joern_installed():
        pytest.skip("Joern is not installed in PATH")

    assert detect_frameworks(NEXTJS_APP_DIR) == ("nextjs",)
    frameworks = resolve_frameworks(NEXTJS_APP_DIR, ("auto",))
    source_specs = get_source_specs(frameworks=frameworks)
    sink_specs = get_sink_specs(frameworks=frameworks)
    sanitizer_specs = get_sanitizer_specs(frameworks=frameworks)

    assert {
        "nextjs_pages_req_body",
        "nextjs_pages_req_query",
        "nextjs_app_request_json",
        "nextjs_app_request_text",
        "nextjs_app_request_form_data",
        "nextjs_app_request_headers",
        "nextjs_app_nexturl_search_params",
        "nextjs_server_action_formdata_get",
    } <= {spec.name for spec in source_specs}

    transpiled = _transpile_or_skip(NEXTJS_APP_DIR)
    try:
        with JoernServer(port=8127, startup_timeout_seconds=30, query_timeout_seconds=30) as server:
            server.import_project(transpiled.out_dir)
            findings = extract_candidate_findings(
                server,
                joern_project_root=transpiled.out_dir,
                source_map=transpiled.source_map,
                source_specs=source_specs,
                sink_specs=sink_specs,
                sanitizer_specs=sanitizer_specs,
            )
            scan_result = build_scan_result(
                server,
                project_root=NEXTJS_APP_DIR,
                joern_project_root=transpiled.out_dir,
                source_map=transpiled.source_map,
                source_specs=source_specs,
                sink_specs=sink_specs,
                sanitizer_specs=sanitizer_specs,
                candidate_findings=findings,
                frameworks=frameworks,
                metadata=ScanMetadata(
                    timestamp="2026-04-10T00:00:00Z",
                    duration_ms=42,
                    tree_sitter_version="n/a",
                    piranesi_version="0.1.0",
                    files_parsed=3,
                    parse_errors=0,
                    config_hash="test-config",
                ),
            )
    finally:
        transpiled.cleanup()

    pages_sql = next(
        finding
        for finding in findings
        if finding.source.location.file.endswith("pages/api/report.ts")
        and finding.source.source_type == "request_body"
        and finding.sink.api_name == "db.query"
    )
    pages_path = next(
        finding
        for finding in findings
        if finding.source.location.file.endswith("pages/api/report.ts")
        and finding.source.source_type == "url_param"
        and finding.source.parameter_name == "file"
        and finding.sink.api_name == "fs.readFile"
    )
    pages_xss = next(
        finding
        for finding in findings
        if finding.source.location.file.endswith("pages/api/report.ts")
        and finding.source.source_type == "url_param"
        and finding.source.parameter_name == "preview"
        and finding.sink.api_name == "res.send"
    )
    app_sql = next(
        finding
        for finding in findings
        if finding.source.location.file.endswith("app/api/files/route.ts")
        and finding.source.location.snippet == "request.json()"
        and finding.sink.api_name == "db.query"
    )
    app_path = next(
        finding
        for finding in findings
        if finding.source.location.file.endswith("app/api/files/route.ts")
        and "nextUrl.searchParams" in finding.source.location.snippet
        and finding.sink.api_name == "fs.readFile"
    )
    action_sql = next(
        finding
        for finding in findings
        if finding.source.location.file.endswith("app/orders/actions.ts")
        and finding.source.location.snippet.startswith("formData.get(")
        and finding.sink.api_name == "db.query"
    )

    assert pages_sql.vuln_class == "CWE-89"
    assert pages_path.vuln_class == "CWE-22"
    assert pages_xss.vuln_class == "CWE-79"
    assert app_sql.vuln_class == "CWE-89"
    assert app_path.vuln_class == "CWE-22"
    assert action_sql.vuln_class == "CWE-89"

    entry_points = {
        (entry_point.route_pattern, entry_point.http_method, entry_point.kind): entry_point
        for entry_point in scan_result.entry_points
    }
    assert set(entry_points) == {
        ("/api/report", None, "route_handler"),
        ("/api/files", "GET", "route_handler"),
        ("/api/files", "POST", "route_handler"),
        ("/orders", None, "server_action"),
    }

    attack_surface = {
        (node.location.file, node.location.snippet, node.source_type): node
        for node in scan_result.attack_surface
    }
    pages_body_surface = attack_surface[
        (
            str((NEXTJS_APP_DIR / "pages/api/report.ts").resolve()),
            "req.body",
            "request_body",
        )
    ]
    app_json_surface = attack_surface[
        (
            str((NEXTJS_APP_DIR / "app/api/files/route.ts").resolve()),
            "request.json()",
            "request_body",
        )
    ]
    app_text_surface = attack_surface[
        (
            str((NEXTJS_APP_DIR / "app/api/files/route.ts").resolve()),
            "request.text()",
            "request_body",
        )
    ]
    app_form_data_surface = attack_surface[
        (
            str((NEXTJS_APP_DIR / "app/api/files/route.ts").resolve()),
            "request.formData()",
            "request_body",
        )
    ]
    app_header_surface = attack_surface[
        (
            str((NEXTJS_APP_DIR / "app/api/files/route.ts").resolve()),
            "request.headers",
            "header",
        )
    ]
    app_search_params_surface = next(
        node
        for (file_path, snippet, source_type), node in attack_surface.items()
        if file_path.endswith("app/api/files/route.ts")
        and source_type == "url_param"
        and "nextUrl.searchParams" in snippet
    )
    action_surface = next(
        node
        for (file_path, snippet, source_type), node in attack_surface.items()
        if file_path.endswith("app/orders/actions.ts")
        and source_type == "request_body"
        and snippet.startswith("formData.get(")
    )

    assert pages_body_surface.data_flow_to == ["db.query"]
    assert app_json_surface.data_flow_to == ["db.query"]
    assert app_text_surface.data_flow_to == []
    assert app_form_data_surface.data_flow_to == []
    assert app_header_surface.data_flow_to == []
    assert app_search_params_surface.data_flow_to == ["fs.readFile"]
    assert action_surface.data_flow_to == ["db.query"]
