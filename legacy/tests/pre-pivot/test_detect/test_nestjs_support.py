from __future__ import annotations

import socket
from pathlib import Path

import pytest

from piranesi.detect.flows import _DEFAULT_CONFIDENCE, extract_candidate_findings
from piranesi.scan.framework import detect_frameworks, resolve_frameworks
from piranesi.scan.joern import JoernError, JoernServer, is_joern_installed
from piranesi.scan.specs import get_sanitizer_specs, get_sink_specs, get_source_specs
from piranesi.scan.transpile import (
    TranspiledProject,
    TypeScriptCompilerNotFoundError,
    transpile_project,
)

NESTJS_APP_DIR = Path(__file__).resolve().parents[1] / "fixtures" / "typescript" / "nestjs_app"


def _transpile_or_skip(project_dir: Path) -> TranspiledProject:
    try:
        return transpile_project(project_dir)
    except TypeScriptCompilerNotFoundError as exc:  # pragma: no cover - environment dependent
        pytest.skip(str(exc))


def _find_free_port() -> int:
    for port in range(8126, 8141):
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
            try:
                sock.bind(("127.0.0.1", port))
            except OSError:
                continue
        return port
    pytest.skip("No free high port available for NestJS Joern test")


@pytest.mark.joern
@pytest.mark.integration
def test_nestjs_fixture_detects_decorator_sources_and_service_flows() -> None:
    if not is_joern_installed():
        pytest.skip("Joern is not installed in PATH")

    assert detect_frameworks(NESTJS_APP_DIR) == ("nestjs",)
    frameworks = resolve_frameworks(NESTJS_APP_DIR, ("auto",))
    source_specs = get_source_specs(frameworks=frameworks)
    sink_specs = get_sink_specs(frameworks=frameworks)
    sanitizer_specs = get_sanitizer_specs(frameworks=frameworks)
    joern_port = _find_free_port()

    transpiled = _transpile_or_skip(NESTJS_APP_DIR)
    try:
        try:
            with JoernServer(
                port=joern_port,
                startup_timeout_seconds=30,
                query_timeout_seconds=30,
            ) as server:
                server.import_project(transpiled.out_dir)
                findings = extract_candidate_findings(
                    server,
                    joern_project_root=transpiled.out_dir,
                    source_map=transpiled.source_map,
                    source_specs=source_specs,
                    sink_specs=sink_specs,
                    sanitizer_specs=sanitizer_specs,
                    frameworks=frameworks,
                )
        except JoernError as exc:
            pytest.skip(str(exc))
    finally:
        transpiled.cleanup()

    body_sql = next(
        finding
        for finding in findings
        if finding.vuln_class == "CWE-89"
        and finding.source.source_type == "request_body"
        and finding.source.parameter_name == "name"
        and finding.sink.api_name.endswith(".query")
    )
    param_sql = next(
        finding
        for finding in findings
        if finding.vuln_class == "CWE-89"
        and finding.source.source_type == "request_param"
        and finding.source.parameter_name == "id"
        and finding.sink.api_name.endswith(".query")
    )
    query_xss = next(
        finding
        for finding in findings
        if finding.vuln_class == "CWE-79"
        and finding.source.source_type == "url_param"
        and finding.source.parameter_name == "term"
        and finding.sink.api_name == "res.send"
    )
    header_xss = next(
        finding
        for finding in findings
        if finding.vuln_class == "CWE-79"
        and finding.source.source_type == "header"
        and finding.source.parameter_name == "auth"
        and finding.sink.api_name == "res.send"
    )
    query_cmd = next(
        finding
        for finding in findings
        if finding.vuln_class == "CWE-78"
        and finding.source.source_type == "url_param"
        and finding.source.parameter_name == "cmd"
        and finding.sink.api_name == "exec"
    )
    request_cmd = next(
        finding
        for finding in findings
        if finding.vuln_class == "CWE-78"
        and finding.source.source_type == "request_body"
        and finding.source.parameter_name == "command"
        and finding.source.location.snippet == "req.body.command"
        and finding.sink.api_name == "exec"
    )

    assert body_sql.confidence == pytest.approx(_DEFAULT_CONFIDENCE)
    assert param_sql.confidence == pytest.approx(_DEFAULT_CONFIDENCE)
    assert query_xss.confidence == pytest.approx(_DEFAULT_CONFIDENCE)
    assert header_xss.confidence == pytest.approx(_DEFAULT_CONFIDENCE)
    assert query_cmd.confidence == pytest.approx(_DEFAULT_CONFIDENCE)
    assert request_cmd.confidence == pytest.approx(_DEFAULT_CONFIDENCE)
    assert query_cmd.taint_path[-1].location.file.endswith("app.ts")
    assert request_cmd.taint_path[-1].location.file.endswith("app.ts")
