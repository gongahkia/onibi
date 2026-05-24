from __future__ import annotations

from pathlib import Path

import pytest

from piranesi.detect.flows import _DEFAULT_CONFIDENCE, extract_candidate_findings
from piranesi.detect.sanitizer_validation import PARTIAL_CONFIDENCE_REDUCTION
from piranesi.scan.framework import detect_frameworks, resolve_frameworks
from piranesi.scan.joern import JoernServer, is_joern_installed
from piranesi.scan.specs import get_sanitizer_specs, get_sink_specs, get_source_specs
from piranesi.scan.transpile import (
    TranspiledProject,
    TypeScriptCompilerNotFoundError,
    transpile_project,
)

FASTIFY_APP_DIR = Path(__file__).resolve().parents[1] / "fixtures" / "typescript" / "fastify_app"


def _transpile_or_skip(project_dir: Path) -> TranspiledProject:
    try:
        return transpile_project(project_dir)
    except TypeScriptCompilerNotFoundError as exc:  # pragma: no cover - environment dependent
        pytest.skip(str(exc))


@pytest.mark.joern
@pytest.mark.integration
def test_fastify_fixture_detects_sources_sinks_and_schema_validation() -> None:
    if not is_joern_installed():
        pytest.skip("Joern is not installed in PATH")

    assert detect_frameworks(FASTIFY_APP_DIR) == ("fastify",)
    frameworks = resolve_frameworks(FASTIFY_APP_DIR, ("auto",))
    source_specs = get_source_specs(frameworks=frameworks)
    sink_specs = get_sink_specs(frameworks=frameworks)
    sanitizer_specs = get_sanitizer_specs(frameworks=frameworks)

    transpiled = _transpile_or_skip(FASTIFY_APP_DIR)
    try:
        with JoernServer(
            port=8124,
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
            )
    finally:
        transpiled.cleanup()

    echo_query = next(
        finding
        for finding in findings
        if finding.source.source_type == "url_param"
        and finding.source.parameter_name == "name"
        and finding.sink.api_name == "reply.send"
    )
    redirect_param = next(
        finding
        for finding in findings
        if finding.source.source_type == "request_param"
        and finding.source.parameter_name == "next"
        and finding.sink.api_name == "reply.header"
    )
    validated_body = next(
        finding
        for finding in findings
        if finding.source.source_type == "request_body"
        and finding.source.parameter_name == "name"
        and finding.sink.api_name == "reply.send"
    )
    header_echo = next(
        finding
        for finding in findings
        if finding.source.source_type == "header"
        and finding.source.parameter_name == "x-forwarded-host"
        and finding.sink.api_name == "reply.send"
    )

    assert echo_query.confidence == pytest.approx(_DEFAULT_CONFIDENCE)
    assert redirect_param.vuln_class == "CWE-113"
    assert redirect_param.sink.sink_type == "header_injection"
    assert validated_body.confidence == pytest.approx(
        _DEFAULT_CONFIDENCE - PARTIAL_CONFIDENCE_REDUCTION
    )
    assert any(
        step.sanitizer_applied == "fastify_schema_validation" for step in validated_body.taint_path
    )
    assert header_echo.confidence == pytest.approx(_DEFAULT_CONFIDENCE)
