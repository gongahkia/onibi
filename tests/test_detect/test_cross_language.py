from __future__ import annotations

from pathlib import Path

from piranesi.detect.cross_language import (
    cross_language_findings,
    detect_cross_language_flows,
    extract_api_boundaries,
    match_api_boundaries,
)

FIXTURE_DIR = Path(__file__).resolve().parents[1] / "fixtures" / "cross_language"


def test_extract_api_boundaries_finds_client_fetches() -> None:
    boundaries = extract_api_boundaries(FIXTURE_DIR)
    clients = [b for b in boundaries if b.direction == "client"]
    client_urls = {b.url_path for b in clients}
    assert "/api/search" in client_urls
    assert "/api/exec" in client_urls
    assert "/api/login" in client_urls
    assert "/api/proxy" in client_urls
    assert "/api/eval" in client_urls


def test_extract_api_boundaries_finds_server_routes() -> None:
    boundaries = extract_api_boundaries(FIXTURE_DIR)
    servers = [b for b in boundaries if b.direction == "server"]
    server_urls = {b.url_path for b in servers}
    assert "/api/search" in server_urls
    assert "/api/exec" in server_urls
    assert "/api/login" in server_urls
    assert "/api/read" in server_urls
    assert "/api/proxy" in server_urls
    assert "/api/eval" in server_urls


def test_match_api_boundaries_cross_language_only() -> None:
    boundaries = extract_api_boundaries(FIXTURE_DIR)
    matched = match_api_boundaries(boundaries)
    assert len(matched) >= 5
    for client, server in matched:
        assert client.language != server.language  # cross-language
        assert client.direction == "client"
        assert server.direction == "server"


def test_detect_cross_language_flows_finds_sinks() -> None:
    flows = detect_cross_language_flows(FIXTURE_DIR)
    assert len(flows) >= 5
    vuln_classes = {f.vuln_class for f in flows}
    assert "sql_injection" in vuln_classes
    assert "command_injection" in vuln_classes


def test_cross_language_findings_creates_candidate_findings() -> None:
    findings = cross_language_findings(FIXTURE_DIR)
    assert len(findings) >= 5
    for f in findings:
        assert f.metadata.get("cross_language") is True
        assert f.metadata.get("client_language") == "typescript"
        assert f.metadata.get("server_language") == "python"
        assert f.metadata.get("api_path") is not None


def test_cross_language_finding_taint_path_has_api_step() -> None:
    findings = cross_language_findings(FIXTURE_DIR)
    for f in findings:
        ops = [step.operation for step in f.taint_path]
        assert "cross_language_api_call" in ops


def test_cross_language_finding_vuln_classes() -> None:
    findings = cross_language_findings(FIXTURE_DIR)
    classes = {f.vuln_class for f in findings}
    assert "sql_injection" in classes
    assert "command_injection" in classes


def test_cross_language_sqli_finding_details() -> None:
    findings = cross_language_findings(FIXTURE_DIR)
    sqli = [f for f in findings if f.vuln_class == "sql_injection"]
    assert len(sqli) >= 1
    f = sqli[0]
    assert f.severity == "high"
    assert "frontend" in f.source.location.file
    assert "backend" in f.sink.location.file
    assert f.confidence > 0


def test_cross_language_cmdi_finding_details() -> None:
    findings = cross_language_findings(FIXTURE_DIR)
    cmdi = [f for f in findings if f.vuln_class == "command_injection"]
    assert len(cmdi) >= 1
    f = cmdi[0]
    assert f.severity == "critical"


def test_cross_language_xss_finding() -> None:
    findings = cross_language_findings(FIXTURE_DIR)
    xss = [f for f in findings if f.vuln_class == "xss"]
    assert len(xss) >= 1
    assert xss[0].severity == "medium"


def test_cross_language_ssrf_finding() -> None:
    findings = cross_language_findings(FIXTURE_DIR)
    ssrf = [f for f in findings if f.vuln_class == "ssrf"]
    assert len(ssrf) >= 1
    assert ssrf[0].severity == "high"


def test_no_finding_for_safe_route() -> None:
    """safe_search uses parameterized query => no cross-language sink should match."""
    findings = cross_language_findings(FIXTURE_DIR)
    safe_ids = [f for f in findings if f.metadata.get("api_path") == "/api/safe_search"]
    assert len(safe_ids) == 0  # no frontend calls /api/safe_search in fixture


def test_finding_ids_are_unique() -> None:
    findings = cross_language_findings(FIXTURE_DIR)
    ids = [f.id for f in findings]
    assert len(ids) == len(set(ids))


def test_custom_boundaries_input() -> None:
    """Verify cross_language_findings accepts pre-extracted boundaries."""
    boundaries = extract_api_boundaries(FIXTURE_DIR)
    findings = cross_language_findings(FIXTURE_DIR, boundaries=boundaries)
    assert len(findings) >= 5
