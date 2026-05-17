# ruff: noqa: E501

from __future__ import annotations

import json
import tempfile
import threading
import webbrowser
from dataclasses import dataclass
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from ipaddress import ip_address
from pathlib import Path
from typing import Any
from urllib.parse import parse_qs, urlparse

from piranesi.report.pentest import (
    PdfBackend,
    PentestReport,
    build_pentest_report,
    render_markdown,
    render_report_artifact,
)
from piranesi.workspace import WorkspaceError, load_workspace


class WorkspaceServerError(RuntimeError):
    """Raised when the local workspace preview server cannot start safely."""


@dataclass(frozen=True, slots=True)
class WorkspaceServeOptions:
    workspace: Path
    host: str = "127.0.0.1"
    port: int = 8765
    open_browser: bool = False


@dataclass(slots=True)
class WorkspaceServerState:
    workspace_root: Path


def is_loopback_host(host: str) -> bool:
    normalized = host.strip().lower()
    if normalized in {"localhost", "ip6-localhost"}:
        return True
    try:
        return ip_address(normalized).is_loopback
    except ValueError:
        return False


def create_workspace_server(
    workspace: str | Path,
    *,
    host: str = "127.0.0.1",
    port: int = 8765,
) -> ThreadingHTTPServer:
    try:
        state = load_workspace(workspace)
    except WorkspaceError as exc:
        raise WorkspaceServerError(str(exc)) from exc
    server_state = WorkspaceServerState(workspace_root=state.root)

    class PiranesiWorkspaceHandler(_WorkspaceRequestHandler):
        workspace_state = server_state

    server = ThreadingHTTPServer((host, port), PiranesiWorkspaceHandler)
    server.workspace_state = server_state  # type: ignore[attr-defined]
    return server


def run_workspace_server(
    options: WorkspaceServeOptions,
    *,
    block: bool = True,
) -> ThreadingHTTPServer:
    server = create_workspace_server(options.workspace, host=options.host, port=options.port)
    address = server.server_address[0]
    host_name = address.decode() if isinstance(address, bytes) else str(address)
    url = f"http://{host_name}:{server.server_address[1]}"
    if options.open_browser:
        webbrowser.open(url)
    if block:
        try:
            server.serve_forever()
        except KeyboardInterrupt:
            pass
        finally:
            server.server_close()
    else:
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
    return server


class _WorkspaceRequestHandler(BaseHTTPRequestHandler):
    workspace_state: WorkspaceServerState

    def log_message(self, _format: str, *_args: object) -> None:
        return

    def do_GET(self) -> None:
        parsed = urlparse(self.path)
        if parsed.path in {"/", "/index.html"}:
            self._send_text(_INDEX_HTML, content_type="text/html; charset=utf-8")
            return
        if parsed.path == "/app.css":
            self._send_text(_APP_CSS, content_type="text/css; charset=utf-8")
            return
        if parsed.path == "/app.js":
            self._send_text(_APP_JS, content_type="application/javascript; charset=utf-8")
            return
        if parsed.path == "/api/workspace":
            try:
                self._send_json(_workspace_payload(self.workspace_state.workspace_root))
            except WorkspaceError as exc:
                self._send_json({"error": str(exc)}, status=HTTPStatus.CONFLICT)
            return
        if parsed.path == "/api/report/json":
            try:
                payload = _report_payload(self.workspace_state.workspace_root)
            except WorkspaceError as exc:
                self._send_json({"error": str(exc)}, status=HTTPStatus.CONFLICT)
                return
            self._send_bytes(
                json.dumps(payload, indent=2, sort_keys=True).encode("utf-8") + b"\n",
                content_type="application/json; charset=utf-8",
                filename="pentest-report.json",
            )
            return
        if parsed.path == "/api/report/markdown":
            try:
                report = _report_model(self.workspace_state.workspace_root)
            except WorkspaceError as exc:
                self._send_json({"error": str(exc)}, status=HTTPStatus.CONFLICT)
                return
            self._send_text(
                render_markdown(report),
                content_type="text/markdown; charset=utf-8",
                filename="pentest-report.md",
            )
            return
        if parsed.path == "/api/report/pdf":
            backend = _pdf_backend(parse_qs(parsed.query))
            try:
                report = _report_model(self.workspace_state.workspace_root)
                with tempfile.TemporaryDirectory(prefix="piranesi-report-") as tmp:
                    path = render_report_artifact(
                        report,
                        output_dir=Path(tmp),
                        output_format="pdf",
                        pdf_backend=backend,
                    )
                    body = path.read_bytes()
            except Exception as exc:
                self._send_json({"error": str(exc)}, status=HTTPStatus.CONFLICT)
                return
            self._send_bytes(
                body,
                content_type="application/pdf",
                filename=f"pentest-report-{backend}.pdf",
            )
            return
        self._send_json({"error": "not found"}, status=HTTPStatus.NOT_FOUND)

    def _send_json(self, payload: dict[str, Any], *, status: HTTPStatus = HTTPStatus.OK) -> None:
        body = json.dumps(payload, sort_keys=True).encode("utf-8")
        self._send_bytes(body, content_type="application/json; charset=utf-8", status=status)

    def _send_text(
        self,
        body: str,
        *,
        content_type: str,
        filename: str | None = None,
        status: HTTPStatus = HTTPStatus.OK,
    ) -> None:
        self._send_bytes(
            body.encode("utf-8"),
            content_type=content_type,
            filename=filename,
            status=status,
        )

    def _send_bytes(
        self,
        body: bytes,
        *,
        content_type: str,
        filename: str | None = None,
        status: HTTPStatus = HTTPStatus.OK,
    ) -> None:
        self.send_response(status.value)
        self.send_header("Content-Type", content_type)
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(body)))
        if filename is not None:
            self.send_header("Content-Disposition", f'attachment; filename="{filename}"')
        self.end_headers()
        self.wfile.write(body)


def _workspace_payload(workspace_root: Path) -> dict[str, Any]:
    report = _report_model(workspace_root)
    payload = report.model_dump(mode="json")
    return {
        "type": "workspace",
        "workspace": str(workspace_root),
        "generated_at": report.generated_at,
        "engagement": payload["engagement"],
        "executive_summary": payload["executive_summary"],
        "severity_summary": payload["severity_summary"],
        "affected_assets": payload["affected_assets"],
        "findings": payload["findings"],
        "chain_of_custody": payload["chain_of_custody"],
        "artifacts": {
            "report_json": "/api/report/json",
            "report_markdown": "/api/report/markdown",
            "report_pdf": "/api/report/pdf?backend=reportlab",
        },
    }


def _report_payload(workspace_root: Path) -> dict[str, Any]:
    payload = _report_model(workspace_root).model_dump(mode="json")
    return dict(payload)


def _report_model(workspace_root: Path) -> PentestReport:
    state = load_workspace(workspace_root)
    return build_pentest_report(
        state,
        redact_sensitive_evidence=state.workspace.report_settings.redact_sensitive_evidence,
    )


def _pdf_backend(query: dict[str, list[str]]) -> PdfBackend:
    raw = (query.get("backend") or ["reportlab"])[0]
    if raw == "weasyprint":
        return "weasyprint"
    return "reportlab"


_INDEX_HTML = """<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Piranesi Workspace Review</title>
  <link rel="stylesheet" href="/app.css">
</head>
<body>
  <header>
    <h1>Piranesi Workspace Review</h1>
    <p id="project"></p>
  </header>
  <main>
    <section class="summary" id="summary"></section>
    <section>
      <div class="section-title">
        <h2>Findings</h2>
        <nav>
          <a href="/api/report/markdown">Markdown</a>
          <a href="/api/report/json">JSON</a>
          <a href="/api/report/pdf?backend=reportlab">PDF</a>
        </nav>
      </div>
      <div id="findings"></div>
    </section>
  </main>
  <script src="/app.js"></script>
</body>
</html>
"""

_APP_CSS = """
:root { color-scheme: light; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
body { margin: 0; color: #17202a; background: #f7f8fa; }
header { padding: 28px 32px 20px; border-bottom: 1px solid #d8dee7; background: #ffffff; }
h1 { margin: 0 0 6px; font-size: 26px; line-height: 1.2; font-weight: 720; letter-spacing: 0; }
h2 { margin: 0; font-size: 18px; letter-spacing: 0; }
p { margin: 0; color: #5b6676; }
main { max-width: 1120px; margin: 0 auto; padding: 24px 20px 48px; }
.summary { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 12px; margin-bottom: 24px; }
.metric, .finding { background: #ffffff; border: 1px solid #d8dee7; border-radius: 8px; padding: 14px; }
.metric b { display: block; font-size: 24px; margin-top: 4px; }
.section-title { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 12px; }
nav { display: flex; gap: 8px; flex-wrap: wrap; }
a { color: #075985; font-weight: 650; text-decoration: none; }
#findings { display: grid; gap: 12px; }
.finding h3 { margin: 0 0 8px; font-size: 16px; letter-spacing: 0; }
.meta { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 10px; }
.pill { border: 1px solid #b7c2d0; border-radius: 999px; padding: 3px 8px; font-size: 12px; color: #334155; background: #f8fafc; }
.evidence { color: #334155; font-size: 13px; line-height: 1.45; white-space: pre-wrap; }
"""

_APP_JS = """
function text(value) {
  return value === null || value === undefined || value === "" ? "not specified" : String(value);
}

function metric(label, value) {
  return `<div class="metric"><span>${label}</span><b>${value}</b></div>`;
}

function findingCard(finding) {
  const evidence = (finding.evidence || []).slice(0, 3).map((item) => {
    return `<div class="evidence">${text(item.kind)}: ${text(item.value)}</div>`;
  }).join("");
  const service = finding.service ? `${text(finding.service.protocol)}/${text(finding.service.port)}` : "not specified";
  return `<article class="finding">
    <h3>${text(finding.title)}</h3>
    <div class="meta">
      <span class="pill">${text(finding.severity)}</span>
      <span class="pill">${text(finding.status)}</span>
      <span class="pill">retest: ${text(finding.retest_status)}</span>
      <span class="pill">${text(finding.asset)}</span>
      <span class="pill">${service}</span>
    </div>
    ${evidence}
  </article>`;
}

fetch("/api/workspace")
  .then((response) => response.json())
  .then((data) => {
    const engagement = data.engagement || {};
    document.getElementById("project").textContent = `${text(engagement.client)} / ${text(engagement.project)}`;
    const summary = data.executive_summary || {};
    const chain = data.chain_of_custody || {};
    document.getElementById("summary").innerHTML = [
      metric("Findings", summary.finding_count || 0),
      metric("Affected assets", summary.affected_asset_count || 0),
      metric("Highest severity", text(summary.highest_severity)),
      metric("Manifest", text(chain.manifest_status))
    ].join("");
    document.getElementById("findings").innerHTML = (data.findings || []).map(findingCard).join("") || "<p>No findings imported.</p>";
  });
"""


__all__ = [
    "WorkspaceServeOptions",
    "WorkspaceServerError",
    "create_workspace_server",
    "is_loopback_host",
    "run_workspace_server",
]
