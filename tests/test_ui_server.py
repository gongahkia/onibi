from __future__ import annotations

import http.client
import json
from pathlib import Path
from urllib.parse import urlparse

import pytest

from piranesi.ui_server import (
    UiServerError,
    UiServerOptions,
    create_ui_server,
    load_report_state,
    run_ui_server,
)

REPORT_FIXTURE = Path(__file__).parent / "fixtures" / "reports" / "host-report"


def _get_json(url: str) -> dict[str, object]:
    return json.loads(_request(url))


def _get_text(url: str) -> str:
    return _request(url)


def _request(url: str) -> str:
    parsed = urlparse(url)
    assert parsed.scheme == "http"
    assert parsed.hostname is not None
    connection = http.client.HTTPConnection(parsed.hostname, parsed.port, timeout=5)
    try:
        path = parsed.path or "/"
        if parsed.query:
            path = f"{path}?{parsed.query}"
        connection.request("GET", path)
        response = connection.getresponse()
        body = response.read().decode("utf-8")
        assert response.status == 200, body
        return body
    finally:
        connection.close()


def test_server_loads_host_report() -> None:
    server = run_ui_server(
        UiServerOptions(report_path=REPORT_FIXTURE, port=0),
        block=False,
    )
    try:
        url = f"http://{server.server_address[0]}:{server.server_address[1]}"

        assert "Piranesi Review Workbench" in _get_text(url)
        summary = _get_json(f"{url}/api/report")

        assert summary["type"] == "host"
        assert summary["posture_score"] == 68
    finally:
        server.shutdown()
        server.server_close()


def test_api_returns_redacted_summary() -> None:
    server = run_ui_server(
        UiServerOptions(report_path=REPORT_FIXTURE, port=0),
        block=False,
    )
    try:
        url = f"http://{server.server_address[0]}:{server.server_address[1]}"
        payload = _get_json(f"{url}/api/report")
        encoded = json.dumps(payload)

        assert payload["target"] == "[redacted-host]"
        assert "fixture-host" not in encoded
        assert "10.1.2.3" not in encoded
    finally:
        server.shutdown()
        server.server_close()


def test_finding_filters_work() -> None:
    server = run_ui_server(
        UiServerOptions(report_path=REPORT_FIXTURE, port=0),
        block=False,
    )
    try:
        url = f"http://{server.server_address[0]}:{server.server_address[1]}"

        high = _get_json(f"{url}/api/findings?severity=high")
        exposure = _get_json(f"{url}/api/findings?category=exposure")
        suppressed = _get_json(f"{url}/api/findings?suppressed=true")

        assert [finding["severity"] for finding in high["findings"]] == ["high"]
        assert [finding["category"] for finding in exposure["findings"]] == ["exposure"]
        assert [finding["suppressed"] for finding in suppressed["findings"]] == [True]
    finally:
        server.shutdown()
        server.server_close()


def test_localhost_binding_default() -> None:
    server = create_ui_server(REPORT_FIXTURE, port=0)
    try:
        assert server.server_address[0] == "127.0.0.1"
    finally:
        server.server_close()


def test_invalid_report_path_fails_safely(tmp_path: Path) -> None:
    missing_dir = tmp_path / "missing"
    empty_dir = tmp_path / "empty"
    empty_dir.mkdir()
    wrong_file = tmp_path / "report.json"
    wrong_file.write_text("{}", encoding="utf-8")

    with pytest.raises(UiServerError, match="does not exist"):
        load_report_state(missing_dir)
    with pytest.raises(UiServerError, match="must contain"):
        load_report_state(empty_dir)
    with pytest.raises(UiServerError, match="unsafe report path"):
        load_report_state(wrong_file)
