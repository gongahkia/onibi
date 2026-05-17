# ruff: noqa: S104

from __future__ import annotations

import http.client
import json
from pathlib import Path
from urllib.parse import urlparse

from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.workspace_server import WorkspaceServeOptions, is_loopback_host, run_workspace_server

NMAP_FIXTURE = Path(__file__).parent / "fixtures" / "pentest" / "nmap" / "localhost-http.xml"
runner = CliRunner()


def _get_raw(url: str) -> tuple[int, dict[str, str], bytes]:
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
        body = response.read()
        headers = {key.lower(): value for key, value in response.getheaders()}
        return response.status, headers, body
    finally:
        connection.close()


def _get_json(url: str) -> dict[str, object]:
    status, _headers, body = _get_raw(url)
    assert status == 200, body.decode("utf-8")
    return json.loads(body.decode("utf-8"))


def test_workspace_server_renders_real_workspace_data(tmp_path: Path) -> None:
    workspace = _ingest_nmap(tmp_path / "workspace")
    server = run_workspace_server(WorkspaceServeOptions(workspace=workspace, port=0), block=False)
    try:
        url = f"http://{server.server_address[0]}:{server.server_address[1]}"
        html_status, _html_headers, html_body = _get_raw(url)
        payload = _get_json(f"{url}/api/workspace")

        assert html_status == 200
        html = html_body.decode("utf-8")
        assert "Piranesi Workspace Review" in html
        assert "workbench" not in html.lower()
        assert payload["type"] == "workspace"
        assert payload["executive_summary"]["finding_count"] == 2  # type: ignore[index]
        assert payload["chain_of_custody"]["manifest_status"] == "not-signed"  # type: ignore[index]
        assert {finding["asset"] for finding in payload["findings"]} == {  # type: ignore[index]
            "127.0.0.1"
        }
    finally:
        server.shutdown()
        server.server_close()


def test_workspace_server_report_preview_routes_and_path_safety(tmp_path: Path) -> None:
    workspace = _ingest_nmap(tmp_path / "workspace")
    server = run_workspace_server(WorkspaceServeOptions(workspace=workspace, port=0), block=False)
    try:
        url = f"http://{server.server_address[0]}:{server.server_address[1]}"
        md_status, md_headers, md_body = _get_raw(f"{url}/api/report/markdown")
        json_status, json_headers, json_body = _get_raw(f"{url}/api/report/json")
        pdf_status, pdf_headers, pdf_body = _get_raw(f"{url}/api/report/pdf?backend=reportlab")
        traversal_status, _traversal_headers, _traversal_body = _get_raw(
            f"{url}/api/report/../../workspace.json"
        )

        assert md_status == 200
        assert md_headers["content-type"] == "text/markdown; charset=utf-8"
        assert b"Piranesi Pentest Report" in md_body
        assert json_status == 200
        assert json_headers["content-type"] == "application/json; charset=utf-8"
        assert json.loads(json_body.decode("utf-8"))["schema_version"] == "piranesi.report.v1"
        assert pdf_status == 200
        assert pdf_headers["content-type"] == "application/pdf"
        assert pdf_body.startswith(b"%PDF")
        assert traversal_status == 404
    finally:
        server.shutdown()
        server.server_close()


def test_serve_cli_rejects_non_loopback_without_explicit_ack(tmp_path: Path) -> None:
    workspace = _ingest_nmap(tmp_path / "workspace")

    result = runner.invoke(
        app,
        ["serve", "--workspace", str(workspace), "--host", "0.0.0.0", "--port", "0"],
    )

    assert result.exit_code == 2
    assert "WARNING" in result.output
    assert "--unsafe-bind" in result.output


def test_serve_cli_allows_non_loopback_with_warning(
    tmp_path: Path,
    monkeypatch,
) -> None:
    workspace = _ingest_nmap(tmp_path / "workspace")

    class FakeServer:
        server_address = ("0.0.0.0", 9876)

        def serve_forever(self) -> None:
            return None

        def server_close(self) -> None:
            return None

    monkeypatch.setattr(
        "piranesi.cli.create_workspace_server", lambda *args, **kwargs: FakeServer()
    )

    result = runner.invoke(
        app,
        [
            "serve",
            "--workspace",
            str(workspace),
            "--host",
            "0.0.0.0",
            "--port",
            "0",
            "--unsafe-bind",
        ],
    )

    assert result.exit_code == 0, result.output
    assert "WARNING" in result.output
    assert "serve: http://0.0.0.0:9876" in result.output


def test_loopback_host_detection() -> None:
    assert is_loopback_host("127.0.0.1")
    assert is_loopback_host("::1")
    assert is_loopback_host("localhost")
    assert not is_loopback_host("0.0.0.0")
    assert not is_loopback_host("192.168.1.10")


def _ingest_nmap(workspace: Path) -> Path:
    result = runner.invoke(
        app,
        ["ingest", "nmap", "--input", str(NMAP_FIXTURE), "--workspace", str(workspace)],
    )
    assert result.exit_code == 0, result.output
    return workspace
