from __future__ import annotations

import socket
from pathlib import Path

import pytest

from piranesi.detect.flows import _DEFAULT_CONFIDENCE, extract_candidate_findings
from piranesi.scan.framework import detect_frameworks, resolve_frameworks
from piranesi.scan.joern import JoernError, JoernServer, is_joern_installed
from piranesi.scan.specs import get_sanitizer_specs, get_sink_specs, get_source_specs

GIN_APP_DIR = Path(__file__).resolve().parents[1] / "fixtures" / "go" / "gin_app"


def _find_free_port() -> int:
    for port in range(8141, 8156):
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
            try:
                sock.bind(("127.0.0.1", port))
            except OSError:
                continue
        return port
    pytest.skip("No free high port available for Go Joern test")


def test_gin_fixture_auto_detection_and_specs() -> None:
    frameworks = resolve_frameworks(GIN_APP_DIR, ("auto",))
    sink_names = {spec.name for spec in get_sink_specs(frameworks=frameworks)}

    assert "gin" in detect_frameworks(GIN_APP_DIR)
    assert "gin" in frameworks
    assert "go-stdlib" in frameworks
    assert "go_sql_query_sprintf" in sink_names
    assert "go_exec_command" in sink_names
    assert "go_template_html" in sink_names
    assert "go_os_open" in sink_names


@pytest.mark.joern
@pytest.mark.integration
def test_gin_fixture_detects_go_sources_sinks_and_excludes_vendor() -> None:
    if not is_joern_installed():
        pytest.skip("Joern is not installed in PATH")

    frameworks = resolve_frameworks(GIN_APP_DIR, ("auto",))
    source_specs = get_source_specs(frameworks=frameworks)
    sink_specs = get_sink_specs(frameworks=frameworks)
    sanitizer_specs = get_sanitizer_specs(frameworks=frameworks)

    try:
        with JoernServer(
            port=_find_free_port(),
            startup_timeout_seconds=30,
            query_timeout_seconds=30,
        ) as server:
            server.import_project(
                GIN_APP_DIR,
                language="go",
                frontend_args=("--exclude", "vendor"),
            )
            findings = extract_candidate_findings(
                server,
                joern_project_root=GIN_APP_DIR,
                source_map=None,
                source_specs=source_specs,
                sink_specs=sink_specs,
                sanitizer_specs=sanitizer_specs,
                frameworks=frameworks,
            )
    except JoernError as exc:
        pytest.skip(str(exc))

    sql_finding = next(
        finding
        for finding in findings
        if finding.vuln_class == "CWE-89"
        and finding.source.source_type == "url_param"
        and finding.source.parameter_name == "user"
        and finding.sink.api_name == "db.Query"
    )
    cmd_finding = next(
        finding
        for finding in findings
        if finding.vuln_class == "CWE-78"
        and finding.source.source_type == "url_param"
        and finding.source.parameter_name == "cmd"
        and finding.sink.api_name == "exec.Command"
    )
    html_finding = next(
        finding
        for finding in findings
        if finding.vuln_class == "CWE-79"
        and finding.source.source_type == "url_param"
        and finding.source.parameter_name == "markup"
        and finding.sink.api_name == "template.HTML"
    )
    file_finding = next(
        finding
        for finding in findings
        if finding.vuln_class == "CWE-22"
        and finding.source.source_type == "url_param"
        and finding.source.parameter_name == "file"
        and finding.sink.api_name == "os.Open"
    )

    assert sql_finding.confidence == pytest.approx(_DEFAULT_CONFIDENCE)
    assert cmd_finding.confidence == pytest.approx(_DEFAULT_CONFIDENCE)
    assert html_finding.confidence == pytest.approx(_DEFAULT_CONFIDENCE)
    assert file_finding.confidence == pytest.approx(_DEFAULT_CONFIDENCE)
    assert all("/vendor/" not in finding.source.location.file for finding in findings)
    assert all(finding.source.location.file.endswith("main.go") for finding in findings)
