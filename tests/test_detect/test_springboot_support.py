from __future__ import annotations

from pathlib import Path

import pytest

from piranesi.detect.flows import extract_candidate_findings
from piranesi.scan.framework import detect_frameworks, resolve_frameworks
from piranesi.scan.joern import JoernServer, is_joern_installed
from piranesi.scan.queries import execute_sanitizer_query, execute_sink_query, execute_source_query
from piranesi.scan.specs import (
    SPRINGBOOT_SANITIZER_SPECS,
    SPRINGBOOT_SINK_SPECS,
    SPRINGBOOT_SOURCE_SPECS,
)
from piranesi.scan.surface import collect_files_scanned

SPRINGBOOT_APP_DIR = Path(__file__).resolve().parents[1] / "fixtures" / "java" / "springboot_app"


def _source_spec(name: str):
    return next(spec for spec in SPRINGBOOT_SOURCE_SPECS if spec.name == name)


def _sink_spec(name: str):
    return next(spec for spec in SPRINGBOOT_SINK_SPECS if spec.name == name)


def _sanitizer_spec(name: str):
    return next(spec for spec in SPRINGBOOT_SANITIZER_SPECS if spec.name == name)


def _codes(nodes: tuple[object, ...]) -> set[str]:
    return {node.code for node in nodes}  # type: ignore[attr-defined]


@pytest.mark.joern
@pytest.mark.integration
def test_springboot_fixture_detects_java_sources_sinks_and_excludes_tests() -> None:
    if not is_joern_installed():
        pytest.skip("Joern is not installed in PATH")

    assert detect_frameworks(SPRINGBOOT_APP_DIR) == ("springboot",)
    frameworks = resolve_frameworks(SPRINGBOOT_APP_DIR, ("auto",))
    assert frameworks == ("springboot",)

    with JoernServer(port=9012, startup_timeout_seconds=30, query_timeout_seconds=60) as server:
        server.import_project(
            SPRINGBOOT_APP_DIR,
            language="java",
            frontend_args=["--exclude", "src/test"],
        )

        request_body = execute_source_query(server, _source_spec("spring_request_body"))
        request_param = execute_source_query(server, _source_spec("spring_request_param"))
        jdbc_sinks = execute_sink_query(server, _sink_spec("spring_jdbc_query"))
        native_query_sinks = execute_sink_query(
            server, _sink_spec("spring_jpa_native_query_concat")
        )
        pre_authorize = execute_sanitizer_query(
            server,
            _sanitizer_spec("spring_pre_authorize_access_control"),
        )
        secured = execute_sanitizer_query(server, _sanitizer_spec("spring_secured_access_control"))
        findings = extract_candidate_findings(
            server,
            joern_project_root=SPRINGBOOT_APP_DIR,
            source_map=None,
            source_specs=SPRINGBOOT_SOURCE_SPECS,
            sink_specs=SPRINGBOOT_SINK_SPECS,
            sanitizer_specs=SPRINGBOOT_SANITIZER_SPECS,
            frameworks=frameworks,
        )
        files_scanned = collect_files_scanned(
            server,
            joern_project_root=SPRINGBOOT_APP_DIR,
            source_map=None,
        )

    assert _codes(request_body) == {"@RequestBody String email"}
    assert _codes(request_param) == {"@RequestParam String name"}
    assert {
        (
            'this.jdbcTemplate.query("SELECT name FROM users WHERE name = \'" '
            '+ name + "\'", USER_ROW_MAPPER)'
        ),
        'this.jdbcTemplate.queryForList("SELECT * FROM users WHERE email = \'" + email + "\'")',
    } <= _codes(jdbc_sinks)
    assert _codes(native_query_sinks) == {
        '@Query(value = "SELECT * " + "FROM users WHERE active = true", nativeQuery = true)'
    }
    assert _codes(pre_authorize) == {"@PreAuthorize(\"hasRole('ADMIN')\")"}
    assert _codes(secured) == {'@Secured("ROLE_WRITER")'}

    finding_keys = {
        (
            finding.vuln_class,
            finding.source.source_type,
            finding.source.location.snippet,
            finding.sink.api_name,
        )
        for finding in findings
    }
    assert finding_keys == {
        ("CWE-89", "request_body", "@RequestBody String email", "this.jdbcTemplate.queryForList"),
        ("CWE-89", "url_param", "@RequestParam String name", "this.jdbcTemplate.query"),
    }

    assert all("/src/test/" not in path for path in files_scanned)
    assert not any("UserControllerTest.java" in path for path in files_scanned)
