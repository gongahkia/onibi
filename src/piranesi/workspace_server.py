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
from typing import Any, cast
from urllib.parse import parse_qs, urlparse

from piranesi.detections import load_detections
from piranesi.evidence import (
    EvidenceError,
    EvidenceSensitivity,
    add_evidence_file,
    load_evidence_index,
)
from piranesi.objectives import load_objectives, load_procedures
from piranesi.report.pentest import (
    PdfBackend,
    PentestReport,
    build_pentest_report,
    render_markdown,
    render_report_artifact,
)
from piranesi.timeline import load_timeline_events
from piranesi.workspace import (
    EVIDENCE_FILE,
    AuditEvent,
    EngagementMetadata,
    WorkspaceError,
    append_audit_event,
    create_workspace,
    file_sha256,
    load_workspace,
    utc_now,
)


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
        state = create_workspace(workspace)
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

    def do_POST(self) -> None:
        parsed = urlparse(self.path)
        if parsed.path == "/api/workspace/init":
            try:
                payload = self._read_json_body()
                engagement = EngagementMetadata(
                    client=_optional_string(payload.get("client")),
                    project=_optional_string(payload.get("project")),
                    scope=_string_list(payload.get("scope")),
                    assessment_type=_optional_string(payload.get("assessment_type")),
                    owner=_optional_string(payload.get("owner")),
                )
                create_workspace(self.workspace_state.workspace_root, engagement=engagement)
                self._send_json(_workspace_payload(self.workspace_state.workspace_root))
            except (WorkspaceError, WorkspaceServerError, ValueError) as exc:
                self._send_json({"error": str(exc)}, status=HTTPStatus.BAD_REQUEST)
            return
        if parsed.path == "/api/evidence/note":
            try:
                payload = self._read_json_body()
                state = create_workspace(self.workspace_state.workspace_root)
                title = _optional_string(payload.get("title")) or "Operator note"
                content = _required_string(payload.get("content"), "content")
                with tempfile.TemporaryDirectory(prefix="piranesi-note-") as tmp:
                    note_path = Path(tmp) / "operator-note.md"
                    note_path.write_text(content.rstrip() + "\n", encoding="utf-8")
                    _index, record = add_evidence_file(
                        state.root,
                        file_path=note_path,
                        kind="note",
                        title=title,
                        source=_optional_string(payload.get("source")) or "piranesi-ui",
                        sensitivity=_evidence_sensitivity(payload.get("sensitivity")),
                        tags=_string_list(payload.get("tags")),
                        notes=_optional_string(payload.get("notes")) or content,
                    )
                output_digest = file_sha256(state.root / EVIDENCE_FILE)
                append_audit_event(
                    state,
                    AuditEvent(
                        timestamp=utc_now(),
                        command="web evidence note",
                        input_path=record.raw_path,
                        input_sha256=record.sha256,
                        output_path=EVIDENCE_FILE,
                        output_sha256=output_digest,
                        summary={
                            "evidence_id": record.id,
                            "kind": record.kind,
                            "title": record.title,
                        },
                    ),
                )
                self._send_json(_workspace_payload(self.workspace_state.workspace_root))
            except (EvidenceError, WorkspaceError, WorkspaceServerError, ValueError) as exc:
                self._send_json({"error": str(exc)}, status=HTTPStatus.BAD_REQUEST)
            return
        self._send_json({"error": "not found"}, status=HTTPStatus.NOT_FOUND)

    def _read_json_body(self) -> dict[str, Any]:
        raw_length = self.headers.get("Content-Length") or "0"
        try:
            length = int(raw_length)
        except ValueError as exc:
            raise WorkspaceServerError("invalid Content-Length") from exc
        body = self.rfile.read(length)
        if not body:
            return {}
        payload = json.loads(body.decode("utf-8"))
        if not isinstance(payload, dict):
            raise WorkspaceServerError("expected JSON object")
        return payload

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
    state = load_workspace(workspace_root)
    report = _report_model(workspace_root)
    payload = report.model_dump(mode="json")
    evidence = load_evidence_index(workspace_root)
    timeline = load_timeline_events(workspace_root)
    objectives = load_objectives(workspace_root)
    procedures = load_procedures(workspace_root)
    detections = load_detections(workspace_root)
    report_artifacts = _report_artifacts(workspace_root)
    return {
        "type": "workspace",
        "workspace": str(workspace_root),
        "initialized": True,
        "generated_at": report.generated_at,
        "engagement": payload["engagement"],
        "executive_summary": payload["executive_summary"],
        "severity_summary": payload["severity_summary"],
        "affected_assets": payload["affected_assets"],
        "findings": payload["findings"],
        "evidence": [record.model_dump(mode="json") for record in evidence.evidence],
        "timeline": [event.model_dump(mode="json") for event in timeline],
        "objectives": [objective.model_dump(mode="json") for objective in objectives.objectives],
        "procedures": [procedure.model_dump(mode="json") for procedure in procedures.procedures],
        "detections": {
            "iocs": [ioc.model_dump(mode="json") for ioc in detections.iocs],
            "notes": [note.model_dump(mode="json") for note in detections.notes],
        },
        "report_artifacts": report_artifacts,
        "empty_states": {
            "evidence": len(evidence.evidence) == 0,
            "timeline": len(timeline) == 0,
            "objectives": len(objectives.objectives) == 0,
            "procedures": len(procedures.procedures) == 0,
            "detections": not detections.iocs and not detections.notes,
            "findings": len(state.findings.findings) == 0,
            "reports": len(report_artifacts) == 0,
            "signing": payload["chain_of_custody"]["manifest_status"] != "available",
            "signed": payload["chain_of_custody"]["manifest_status"] != "available",
        },
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


def _report_artifacts(workspace_root: Path) -> list[dict[str, str]]:
    reports_root = workspace_root / "reports"
    if not reports_root.is_dir():
        return []
    artifacts: list[dict[str, str]] = []
    for path in sorted(item for item in reports_root.rglob("*") if item.is_file()):
        artifacts.append(
            {
                "path": path.relative_to(workspace_root).as_posix(),
                "sha256": file_sha256(path),
            }
        )
    return artifacts


def _optional_string(value: object) -> str | None:
    if isinstance(value, str) and value.strip():
        return value.strip()
    return None


def _required_string(value: object, field_name: str) -> str:
    if isinstance(value, str) and value.strip():
        return value.strip()
    raise WorkspaceServerError(f"{field_name} is required")


def _string_list(value: object) -> list[str]:
    if value is None:
        return []
    if isinstance(value, str):
        return [item.strip() for item in value.split(",") if item.strip()]
    if isinstance(value, list):
        return [item.strip() for item in value if isinstance(item, str) and item.strip()]
    raise WorkspaceServerError("scope must be a string or string list")


def _evidence_sensitivity(value: object) -> EvidenceSensitivity:
    if value in {"public", "internal", "sensitive", "secret"}:
        return cast(EvidenceSensitivity, value)
    return "sensitive"


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
    <section class="setup">
      <div>
        <h2>Engagement</h2>
        <p id="workspace-path"></p>
      </div>
      <form id="setup-form">
        <input name="client" placeholder="Client">
        <input name="project" placeholder="Project">
        <input name="scope" placeholder="Scope, comma separated">
        <button type="submit">Save</button>
      </form>
    </section>
    <section class="entry">
      <div>
        <h2>Add Note Evidence</h2>
        <p id="evidence-status">No note queued.</p>
      </div>
      <form id="evidence-form" class="note-form">
        <input name="title" placeholder="Title">
        <input name="tags" placeholder="Tags, comma separated">
        <textarea name="content" placeholder="Operator note or transcript excerpt" required></textarea>
        <button type="submit">Add Evidence</button>
      </form>
    </section>
    <section class="summary" id="summary"></section>
    <section>
      <div class="section-title">
        <h2>Engagement Flow</h2>
      </div>
      <div class="flow" id="flow"></div>
    </section>
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
.setup, .entry, .metric, .finding, .step { background: #ffffff; border: 1px solid #d8dee7; border-radius: 8px; padding: 14px; }
.setup, .entry { display: grid; grid-template-columns: minmax(220px, 1fr) 2fr; gap: 16px; align-items: start; margin-bottom: 16px; }
form { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 8px; }
input, textarea { min-width: 0; border: 1px solid #b7c2d0; border-radius: 6px; padding: 8px 10px; font: inherit; }
textarea { min-height: 84px; resize: vertical; }
button { border: 1px solid #075985; border-radius: 6px; padding: 8px 10px; color: #ffffff; background: #075985; font-weight: 650; }
.note-form { grid-template-columns: 1fr 1fr auto; }
.note-form textarea { grid-column: 1 / -1; }
.metric b { display: block; font-size: 24px; margin-top: 4px; }
.section-title { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 12px; }
nav { display: flex; gap: 8px; flex-wrap: wrap; }
a { color: #075985; font-weight: 650; text-decoration: none; }
.flow { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 12px; margin-bottom: 24px; }
.step h3 { margin: 0 0 8px; font-size: 15px; letter-spacing: 0; }
.step p { font-size: 13px; line-height: 1.45; }
#findings { display: grid; gap: 12px; }
.finding h3 { margin: 0 0 8px; font-size: 16px; letter-spacing: 0; }
.meta { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 10px; }
.pill { border: 1px solid #b7c2d0; border-radius: 999px; padding: 3px 8px; font-size: 12px; color: #334155; background: #f8fafc; }
.evidence { color: #334155; font-size: 13px; line-height: 1.45; white-space: pre-wrap; }
@media (max-width: 720px) {
  .setup, .entry, form, .note-form { grid-template-columns: 1fr; }
  .note-form textarea { grid-column: auto; }
}
"""

_APP_JS = """
function display(value) {
  return value === null || value === undefined || value === "" ? "not specified" : String(value);
}

function html(value) {
  return display(value).replace(/[&<>"']/g, (character) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#39;"
  })[character]);
}

function metric(label, value) {
  return `<div class="metric"><span>${html(label)}</span><b>${html(value)}</b></div>`;
}

function step(label, count, emptyText) {
  const state = count > 0 ? `${count} recorded` : emptyText;
  return `<article class="step"><h3>${html(label)}</h3><p>${html(state)}</p></article>`;
}

function findingCard(finding) {
  const evidence = (finding.evidence || []).slice(0, 3).map((item) => {
    return `<div class="evidence">${html(item.kind)}: ${html(item.value)}</div>`;
  }).join("");
  const service = finding.service ? `${display(finding.service.protocol)}/${display(finding.service.port)}` : "not specified";
  return `<article class="finding">
    <h3>${html(finding.title)}</h3>
    <div class="meta">
      <span class="pill">${html(finding.severity)}</span>
      <span class="pill">${html(finding.status)}</span>
      <span class="pill">retest: ${html(finding.retest_status)}</span>
      <span class="pill">${html(finding.asset)}</span>
      <span class="pill">${html(service)}</span>
    </div>
    ${evidence}
  </article>`;
}

fetch("/api/workspace")
  .then((response) => response.json())
  .then(renderWorkspace);

document.getElementById("setup-form").addEventListener("submit", (event) => {
  event.preventDefault();
  const form = new FormData(event.currentTarget);
  fetch("/api/workspace/init", {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify({
      client: form.get("client"),
      project: form.get("project"),
      scope: form.get("scope")
    })
  })
    .then((response) => response.json())
    .then(renderWorkspace);
});

document.getElementById("evidence-form").addEventListener("submit", (event) => {
  event.preventDefault();
  const form = new FormData(event.currentTarget);
  fetch("/api/evidence/note", {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify({
      title: form.get("title"),
      tags: form.get("tags"),
      content: form.get("content")
    })
  })
    .then((response) => response.json())
    .then((data) => {
      if (data.error) {
        document.getElementById("evidence-status").textContent = data.error;
        return;
      }
      event.currentTarget.reset();
      document.getElementById("evidence-status").textContent = "Evidence saved.";
      renderWorkspace(data);
    });
});

function renderWorkspace(data) {
  const engagement = data.engagement || {};
  document.getElementById("project").textContent = `${display(engagement.client)} / ${display(engagement.project)}`;
  document.getElementById("workspace-path").textContent = display(data.workspace);
  const summary = data.executive_summary || {};
  const chain = data.chain_of_custody || {};
  document.getElementById("summary").innerHTML = [
    metric("Evidence", (data.evidence || []).length),
    metric("Timeline", (data.timeline || []).length),
    metric("Objectives", (data.objectives || []).length),
    metric("Findings", summary.finding_count || 0),
    metric("IOCs", ((data.detections || {}).iocs || []).length),
    metric("Reports", (data.report_artifacts || []).length),
    metric("Manifest", display(chain.manifest_status))
  ].join("");
  document.getElementById("flow").innerHTML = [
    step("1. Scope", (engagement.scope || []).length, "Define scope and rules."),
    step("2. Evidence", (data.evidence || []).length, "Add notes, screenshots, logs, and transcripts."),
    step("3. Timeline", (data.timeline || []).length, "Record operator activity."),
    step("4. Objectives", (data.objectives || []).length, "Track goals and outcomes."),
    step("5. Findings", (data.findings || []).length, "Import scanner or manual findings."),
    step("6. Handoff", ((data.detections || {}).iocs || []).length + ((data.detections || {}).notes || []).length, "Prepare detection notes and IOCs."),
    step("7. Report", (data.report_artifacts || []).length, "Generate handoff artifacts."),
    step("8. Sign", chain.manifest_status === "available" ? 1 : 0, "Create a custody manifest.")
  ].join("");
  document.getElementById("findings").innerHTML = (data.findings || []).map(findingCard).join("") || "<p>No findings imported.</p>";
}
"""


__all__ = [
    "WorkspaceServeOptions",
    "WorkspaceServerError",
    "create_workspace_server",
    "is_loopback_host",
    "run_workspace_server",
]
