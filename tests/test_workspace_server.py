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


def _post_json(url: str, payload: dict[str, object]) -> tuple[int, dict[str, object]]:
    parsed = urlparse(url)
    assert parsed.scheme == "http"
    assert parsed.hostname is not None
    connection = http.client.HTTPConnection(parsed.hostname, parsed.port, timeout=5)
    try:
        path = parsed.path or "/"
        body = json.dumps(payload).encode("utf-8")
        connection.request(
            "POST",
            path,
            body=body,
            headers={"Content-Type": "application/json"},
        )
        response = connection.getresponse()
        raw_body = response.read()
        return response.status, json.loads(raw_body.decode("utf-8"))
    finally:
        connection.close()


def _post_multipart(
    url: str,
    *,
    fields: dict[str, str],
    file_field: str,
    filename: str,
    file_body: bytes,
) -> tuple[int, dict[str, object]]:
    parsed = urlparse(url)
    assert parsed.scheme == "http"
    assert parsed.hostname is not None
    boundary = "----piranesi-test-boundary"
    parts: list[bytes] = []
    for key, value in fields.items():
        parts.extend(
            [
                f"--{boundary}\r\n".encode(),
                f'Content-Disposition: form-data; name="{key}"\r\n\r\n'.encode(),
                value.encode("utf-8"),
                b"\r\n",
            ]
        )
    parts.extend(
        [
            f"--{boundary}\r\n".encode(),
            (
                f'Content-Disposition: form-data; name="{file_field}"; filename="{filename}"\r\n'
            ).encode(),
            b"Content-Type: application/octet-stream\r\n\r\n",
            file_body,
            b"\r\n",
            f"--{boundary}--\r\n".encode(),
        ]
    )
    body = b"".join(parts)
    connection = http.client.HTTPConnection(parsed.hostname, parsed.port, timeout=5)
    try:
        path = parsed.path or "/"
        connection.request(
            "POST",
            path,
            body=body,
            headers={
                "Content-Type": f"multipart/form-data; boundary={boundary}",
                "Content-Length": str(len(body)),
            },
        )
        response = connection.getresponse()
        raw_body = response.read()
        return response.status, json.loads(raw_body.decode("utf-8"))
    finally:
        connection.close()


def test_workspace_server_opens_empty_workspace(tmp_path: Path) -> None:
    workspace = tmp_path / "empty-workspace"
    server = run_workspace_server(WorkspaceServeOptions(workspace=workspace, port=0), block=False)
    try:
        url = f"http://{server.server_address[0]}:{server.server_address[1]}"
        payload = _get_json(f"{url}/api/workspace")

        assert payload["initialized"] is True
        assert payload["workspace"] == str(workspace.resolve(strict=False))
        assert payload["empty_states"] == {
            "detections": True,
            "evidence": True,
            "findings": True,
            "objectives": True,
            "procedures": True,
            "reports": True,
            "signed": True,
            "signing": True,
            "timeline": True,
        }
        assert payload["evidence"] == []
        assert payload["timeline"] == []
        assert payload["objectives"] == []
        assert payload["procedures"] == []
        assert payload["findings"] == []
        assert (workspace / "workspace.json").is_file()
        assert (workspace / "evidence" / "index.json").is_file()
        assert (workspace / "timeline" / "events.jsonl").is_file()
    finally:
        server.shutdown()
        server.server_close()


def test_workspace_server_initializes_engagement_from_browser_flow(tmp_path: Path) -> None:
    workspace = tmp_path / "browser-workspace"
    server = run_workspace_server(WorkspaceServeOptions(workspace=workspace, port=0), block=False)
    try:
        url = f"http://{server.server_address[0]}:{server.server_address[1]}"
        status, payload = _post_json(
            f"{url}/api/workspace/init",
            {
                "client": "Acme",
                "project": "Q2 purple team",
                "scope": "app.acme.test, 10.0.0.0/24",
                "assessment_type": "red-team",
                "owner": "operator-1",
            },
        )

        assert status == 200
        assert payload["engagement"]["client"] == "Acme"  # type: ignore[index]
        assert payload["engagement"]["project"] == "Q2 purple team"  # type: ignore[index]
        assert payload["engagement"]["scope"] == [  # type: ignore[index]
            "app.acme.test",
            "10.0.0.0/24",
        ]
        assert payload["engagement"]["assessment_type"] == "red-team"  # type: ignore[index]
        assert payload["engagement"]["owner"] == "operator-1"  # type: ignore[index]
    finally:
        server.shutdown()
        server.server_close()


def test_workspace_server_adds_note_evidence_from_ui_flow(tmp_path: Path) -> None:
    workspace = tmp_path / "browser-workspace"
    server = run_workspace_server(WorkspaceServeOptions(workspace=workspace, port=0), block=False)
    try:
        url = f"http://{server.server_address[0]}:{server.server_address[1]}"
        status, payload = _post_json(
            f"{url}/api/evidence/note",
            {
                "title": "Initial access note",
                "tags": "initial-access, ui",
                "content": "Operator captured authorized lab login behavior.",
            },
        )

        assert status == 200
        assert payload["empty_states"]["evidence"] is False  # type: ignore[index]
        assert payload["evidence"][0]["title"] == "Initial access note"  # type: ignore[index]
        assert payload["evidence"][0]["kind"] == "note"  # type: ignore[index]
        assert payload["evidence"][0]["tags"] == ["initial-access", "ui"]  # type: ignore[index]
        raw_path = workspace / payload["evidence"][0]["raw_path"]  # type: ignore[index]
        assert raw_path.read_text(encoding="utf-8") == (
            "Operator captured authorized lab login behavior.\n"
        )
    finally:
        server.shutdown()
        server.server_close()


def test_workspace_server_uploads_evidence_file_from_ui_flow(tmp_path: Path) -> None:
    workspace = tmp_path / "browser-workspace"
    server = run_workspace_server(WorkspaceServeOptions(workspace=workspace, port=0), block=False)
    try:
        url = f"http://{server.server_address[0]}:{server.server_address[1]}"
        status, payload = _post_multipart(
            f"{url}/api/evidence/file",
            fields={
                "kind": "transcript",
                "title": "Operator terminal transcript",
                "tags": "terminal, ui",
                "source": "operator-1",
                "sensitivity": "internal",
                "notes": "Captured during authorized local lab validation.",
            },
            file_field="file",
            filename="../operator-terminal.txt",
            file_body=b"$ id\nuid=1000(operator)\n",
        )

        assert status == 200
        assert payload["empty_states"]["evidence"] is False  # type: ignore[index]
        assert payload["evidence"][0]["title"] == "Operator terminal transcript"  # type: ignore[index]
        assert payload["evidence"][0]["kind"] == "transcript"  # type: ignore[index]
        assert payload["evidence"][0]["source"] == "operator-1"  # type: ignore[index]
        assert payload["evidence"][0]["sensitivity"] == "internal"  # type: ignore[index]
        assert payload["evidence"][0]["tags"] == ["terminal", "ui"]  # type: ignore[index]
        raw_path = workspace / payload["evidence"][0]["raw_path"]  # type: ignore[index]
        assert raw_path.is_file()
        assert raw_path.name.endswith("-operator-terminal.txt")
        assert raw_path.read_bytes() == b"$ id\nuid=1000(operator)\n"
    finally:
        server.shutdown()
        server.server_close()


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
        assert "Add Evidence" in html
        assert "Upload File" in html
        assert "Engagement Flow" in html
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
