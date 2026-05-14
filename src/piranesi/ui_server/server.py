# ruff: noqa: E501

from __future__ import annotations

import json
import threading
import webbrowser
from dataclasses import dataclass
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any
from urllib.parse import parse_qs, urlparse

from piranesi.host.api import load_host_report
from piranesi.host.fleet import load_fleet_report
from piranesi.host.models import FleetReport, HostFinding, HostPostureReport


class UiServerError(RuntimeError):
    """Raised when the local UI cannot safely load a report root."""


@dataclass(frozen=True, slots=True)
class UiServerOptions:
    report_path: Path
    host: str = "127.0.0.1"
    port: int = 8765
    watch: bool = False
    open_browser: bool = False


@dataclass(slots=True)
class UiServerState:
    root: Path
    report_path: Path
    report_type: str
    report: HostPostureReport | FleetReport
    watch: bool = False

    def reload(self) -> None:
        loaded = load_report_state(self.root, watch=self.watch)
        self.report_path = loaded.report_path
        self.report_type = loaded.report_type
        self.report = loaded.report


def load_report_state(path: str | Path, *, watch: bool = False) -> UiServerState:
    root = Path(path).expanduser().resolve(strict=False)
    if root.is_file():
        report_path = root
        root_dir = root.parent
    elif root.is_dir():
        root_dir = root
        host_report = root / "host-report.json"
        fleet_report = root / "fleet-report.json"
        if host_report.is_file():
            report_path = host_report
        elif fleet_report.is_file():
            report_path = fleet_report
        else:
            raise UiServerError(
                f"{root} must contain host-report.json or fleet-report.json"
            )
    else:
        raise UiServerError(f"report path does not exist: {root}")

    if not _is_safe_report_path(report_path, root_dir):
        raise UiServerError(f"unsafe report path: {report_path}")
    if report_path.name == "host-report.json":
        report = load_host_report(report_path)
        report_type = "host"
    elif report_path.name == "fleet-report.json":
        report = load_fleet_report(report_path)
        report_type = "fleet"
    else:
        raise UiServerError("report file must be host-report.json or fleet-report.json")
    return UiServerState(
        root=root_dir,
        report_path=report_path,
        report_type=report_type,
        report=report,
        watch=watch,
    )


def create_ui_server(
    report_path: str | Path,
    *,
    host: str = "127.0.0.1",
    port: int = 8765,
    watch: bool = False,
) -> ThreadingHTTPServer:
    state = load_report_state(report_path, watch=watch)

    class PiranesiUiHandler(_UiRequestHandler):
        server_state = state

    server = ThreadingHTTPServer((host, port), PiranesiUiHandler)
    server.report_state = state  # type: ignore[attr-defined]
    return server


def run_ui_server(options: UiServerOptions, *, block: bool = True) -> ThreadingHTTPServer:
    server = create_ui_server(
        options.report_path,
        host=options.host,
        port=options.port,
        watch=options.watch,
    )
    url = f"http://{server.server_address[0]}:{server.server_address[1]}"
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


class _UiRequestHandler(BaseHTTPRequestHandler):
    server_state: UiServerState

    def log_message(self, _format: str, *_args: object) -> None:
        return

    def do_GET(self) -> None:
        parsed = urlparse(self.path)
        if self.server_state.watch:
            self.server_state.reload()
        if parsed.path in {"/", "/index.html"}:
            self._send_text(_INDEX_HTML, content_type="text/html; charset=utf-8")
            return
        if parsed.path == "/app.css":
            self._send_text(_APP_CSS, content_type="text/css; charset=utf-8")
            return
        if parsed.path == "/app.js":
            self._send_text(_APP_JS, content_type="application/javascript; charset=utf-8")
            return
        if parsed.path == "/api/report":
            self._send_json(_report_summary(self.server_state))
            return
        if parsed.path == "/api/findings":
            self._send_json(_findings_payload(self.server_state, parse_qs(parsed.query)))
            return
        self._send_json({"error": "not found"}, status=HTTPStatus.NOT_FOUND)

    def _send_json(self, payload: dict[str, Any], *, status: HTTPStatus = HTTPStatus.OK) -> None:
        body = json.dumps(payload, sort_keys=True).encode("utf-8")
        self.send_response(status.value)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _send_text(
        self,
        body: str,
        *,
        content_type: str,
        status: HTTPStatus = HTTPStatus.OK,
    ) -> None:
        encoded = body.encode("utf-8")
        self.send_response(status.value)
        self.send_header("Content-Type", content_type)
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)


def _report_summary(state: UiServerState) -> dict[str, Any]:
    if state.report_type == "fleet":
        fleet = state.report
        assert isinstance(fleet, FleetReport)
        return {
            "type": "fleet",
            "generated_at": fleet.generated_at,
            "summary": fleet.summary,
            "host_count": fleet.host_count,
            "success_count": fleet.success_count,
            "failure_count": fleet.failure_count,
            "hosts": [
                {
                    "target": _redacted_host(host.target),
                    "status": host.status,
                    "posture_score": host.posture_score,
                    "findings_total": host.findings_total,
                    "by_severity": host.by_severity,
                    "top_risks": host.top_risks,
                }
                for host in fleet.hosts
            ],
        }
    report = state.report
    assert isinstance(report, HostPostureReport)
    return {
        "type": "host",
        "target": _redacted_host(report.target),
        "generated_at": report.generated_at,
        "posture_score": report.posture_score,
        "summary": report.summary,
        "host_metadata": _redact_metadata(report.host_metadata),
        "evidence_inventory": report.evidence_inventory,
        "collection_health": (
            report.collection_health.model_dump(mode="json")
            if report.collection_health is not None
            else None
        ),
        "top_actions": report.top_actions,
        "suppression_review": _suppression_review(report.findings),
    }


def _findings_payload(
    state: UiServerState,
    query: dict[str, list[str]],
) -> dict[str, Any]:
    if state.report_type != "host":
        return {"findings": []}
    report = state.report
    assert isinstance(report, HostPostureReport)
    severity = _first_query(query, "severity")
    category = _first_query(query, "category")
    suppressed = _first_query(query, "suppressed")
    findings = report.findings
    if severity:
        findings = [finding for finding in findings if finding.severity == severity]
    if category:
        findings = [finding for finding in findings if finding.category == category]
    if suppressed in {"true", "false"}:
        want = suppressed == "true"
        findings = [finding for finding in findings if finding.suppressed is want]
    return {"findings": [_finding_payload(finding) for finding in findings]}


def _finding_payload(finding: HostFinding) -> dict[str, Any]:
    payload = finding.model_dump(mode="json")
    payload["evidence"] = [
        {
            "source": item.source,
            "key": item.key,
            "value": _redact_text(item.value),
        }
        for item in finding.evidence
    ]
    return payload


def _suppression_review(findings: list[HostFinding]) -> dict[str, Any]:
    suppressed = [finding for finding in findings if finding.suppressed]
    return {
        "suppressed_count": len(suppressed),
        "active_count": len(findings) - len(suppressed),
        "suppressed_findings": [
            {
                "id": finding.id,
                "title": finding.title,
                "severity": finding.severity,
                "reason": finding.suppression_reason,
            }
            for finding in suppressed
        ],
    }


def _redact_metadata(value: Any) -> Any:
    if isinstance(value, dict):
        return {str(key): _redact_metadata_by_key(str(key), item) for key, item in value.items()}
    if isinstance(value, list):
        return [_redact_metadata(item) for item in value]
    return value


def _redact_metadata_by_key(key: str, value: Any) -> Any:
    lowered = key.lower()
    if any(token in lowered for token in ("host", "ip", "user", "mac", "secret", "token")):
        if isinstance(value, list):
            return ["[redacted]" for _ in value]
        if value:
            return "[redacted]"
    return _redact_metadata(value)


def _redact_text(value: str) -> str:
    if not value:
        return value
    if any(token in value.lower() for token in ("password", "secret", "token")):
        return "[redacted]"
    return value


def _redacted_host(value: str) -> str:
    return "[redacted-host]" if value else "unknown"


def _first_query(query: dict[str, list[str]], name: str) -> str | None:
    values = query.get(name)
    if not values:
        return None
    return values[0] or None


def _is_safe_report_path(report_path: Path, root: Path) -> bool:
    resolved_report = report_path.resolve(strict=False)
    resolved_root = root.resolve(strict=False)
    try:
        resolved_report.relative_to(resolved_root)
    except ValueError:
        return False
    return report_path.name in {"host-report.json", "fleet-report.json"} and report_path.is_file()


_INDEX_HTML = """<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Piranesi Review Workbench</title>
  <link rel="stylesheet" href="/app.css">
</head>
<body>
  <header>
    <div>
      <p>Piranesi local review</p>
      <h1 id="title">Review Workbench</h1>
    </div>
    <strong id="score"></strong>
  </header>
  <main>
    <section id="overview" class="grid"></section>
    <section class="panel" id="fleetPanel" hidden>
      <h2>Fleet Summary</h2>
      <div id="fleet"></div>
    </section>
    <section class="panel">
      <div class="panel-head">
        <h2>Findings</h2>
        <div>
          <select id="severity"><option value="">All severities</option></select>
          <select id="category"><option value="">All categories</option></select>
        </div>
      </div>
      <table>
        <thead><tr><th>Severity</th><th>Risk</th><th>Category</th><th>Finding</th><th>Status</th></tr></thead>
        <tbody id="findings"></tbody>
      </table>
      <article id="detail" class="detail"></article>
    </section>
    <section class="grid">
      <section class="panel"><h2>Top Actions</h2><div id="actions"></div></section>
      <section class="panel"><h2>Evidence Inventory</h2><div id="evidence"></div></section>
      <section class="panel"><h2>Collection Health</h2><div id="health"></div></section>
      <section class="panel"><h2>Suppression Review</h2><div id="suppression"></div></section>
    </section>
  </main>
  <script src="/app.js"></script>
</body>
</html>
"""

_APP_CSS = """
:root { --bg:#f6f7f9; --panel:#fff; --border:#d8dee8; --text:#111827; --muted:#64748b; --accent:#0f766e; }
* { box-sizing:border-box; }
body { margin:0; background:var(--bg); color:var(--text); font:14px/1.45 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif; }
header { display:flex; justify-content:space-between; align-items:center; padding:24px 32px; background:#111827; color:white; }
header p { margin:0 0 4px; color:#9ca3af; text-transform:uppercase; font-size:12px; }
h1,h2,h3,p { margin-top:0; letter-spacing:0; }
main { padding:22px 32px 40px; }
.grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(190px,1fr)); gap:14px; margin-bottom:16px; }
.metric,.panel { background:var(--panel); border:1px solid var(--border); border-radius:8px; padding:15px; }
.metric strong { display:block; font-size:24px; }
.metric span,.muted { color:var(--muted); }
.panel { margin-bottom:16px; }
.panel-head { display:flex; justify-content:space-between; gap:12px; align-items:center; }
select { padding:6px 8px; border:1px solid var(--border); border-radius:6px; background:white; }
table { width:100%; border-collapse:collapse; }
th,td { padding:9px 7px; border-bottom:1px solid var(--border); text-align:left; }
tbody tr { cursor:pointer; }
tbody tr:hover { background:#f1f5f9; }
.severity { font-weight:700; text-transform:capitalize; }
.critical,.high { color:#b91c1c; } .medium { color:#b45309; } .low { color:#1d4ed8; } .informational { color:#475569; }
.detail { margin-top:12px; border-left:3px solid var(--accent); background:#f8fafc; padding:12px; }
ul { margin:0; padding-left:18px; }
code { background:#eef2f7; border-radius:4px; padding:1px 4px; }
@media (max-width:700px) { header,main { padding-left:18px; padding-right:18px; } .panel-head { align-items:flex-start; flex-direction:column; } table { font-size:13px; } }
"""

_APP_JS = """
let report = null;
let findings = [];

const $ = (id) => document.getElementById(id);
const escapeHtml = (value) => String(value)
  .replaceAll("&", "&amp;")
  .replaceAll("<", "&lt;")
  .replaceAll(">", "&gt;")
  .replaceAll('"', "&quot;")
  .replaceAll("'", "&#39;");
const text = (value) => value === null || value === undefined || value === "" ? "none" : escapeHtml(value);

async function getJson(url) {
  const response = await fetch(url, { cache: "no-store" });
  if (!response.ok) throw new Error(`request failed: ${response.status}`);
  return response.json();
}

function metric(label, value) {
  return `<div class="metric"><strong>${text(value)}</strong><span>${label}</span></div>`;
}

function renderList(items) {
  if (!items || !items.length) return '<p class="muted">None recorded.</p>';
  return `<ul>${items.map((item) => `<li>${text(item)}</li>`).join("")}</ul>`;
}

async function load() {
  report = await getJson("/api/report");
  $("title").textContent = report.type === "fleet" ? "Fleet Review" : "Host Review";
  $("score").textContent = report.posture_score !== undefined ? `${report.posture_score}/100` : "";
  renderOverview();
  if (report.type === "fleet") renderFleet();
  await loadFindings();
}

function renderOverview() {
  if (report.type === "fleet") {
    $("overview").innerHTML = [
      metric("Hosts", report.host_count),
      metric("Successful", report.success_count),
      metric("Failed", report.failure_count),
      metric("Findings", report.summary?.findings_total || 0),
    ].join("");
    return;
  }
  $("overview").innerHTML = [
    metric("Target", report.target),
    metric("Findings", report.summary?.findings_total || 0),
    metric("Posture", `${report.posture_score}/100`),
    metric("Evidence", Object.keys(report.evidence_inventory || {}).length),
  ].join("");
  $("actions").innerHTML = renderList((report.top_actions || []).map((a) => `${a.category}: ${a.action}`));
  $("evidence").innerHTML = renderList(Object.entries(report.evidence_inventory || {}).map(([k,v]) => `${k}: ${v}`));
  const health = report.collection_health;
  $("health").innerHTML = health ? renderList([
    ...Object.entries(health.required || {}).map(([k,v]) => `required ${k}: ${v.status}`),
    ...Object.entries(health.optional || {}).map(([k,v]) => `optional ${k}: ${v.status}`),
  ]) : '<p class="muted">No collection manifest present.</p>';
  const suppression = report.suppression_review || {};
  $("suppression").innerHTML = renderList([
    `active: ${suppression.active_count || 0}`,
    `suppressed: ${suppression.suppressed_count || 0}`,
  ]);
}

function renderFleet() {
  $("fleetPanel").hidden = false;
  $("fleet").innerHTML = renderList((report.hosts || []).map((h) => `${h.target}: ${h.status}, score ${h.posture_score}, findings ${h.findings_total}`));
}

async function loadFindings() {
  const params = new URLSearchParams();
  if ($("severity").value) params.set("severity", $("severity").value);
  if ($("category").value) params.set("category", $("category").value);
  findings = (await getJson(`/api/findings?${params.toString()}`)).findings || [];
  renderFilters();
  renderFindings();
}

function renderFilters() {
  const severities = [...new Set(findings.map((f) => f.severity).filter(Boolean))].sort();
  const categories = [...new Set(findings.map((f) => f.category).filter(Boolean))].sort();
  for (const [id, values, label] of [["severity", severities, "All severities"], ["category", categories, "All categories"]]) {
    const select = $(id);
    const current = select.value;
    select.innerHTML = `<option value="">${text(label)}</option>` + values.map((v) => `<option value="${text(v)}">${text(v)}</option>`).join("");
    select.value = current;
  }
}

function renderFindings() {
  if (!findings.length) {
    $("findings").innerHTML = '<tr><td colspan="5" class="muted">No matching findings.</td></tr>';
    $("detail").innerHTML = "";
    return;
  }
  $("findings").innerHTML = findings.map((f, i) => `
    <tr data-index="${i}">
      <td><span class="severity ${f.severity}">${f.severity}</span></td>
      <td>${Number(f.risk?.total || 0).toFixed(1)}</td>
      <td>${text(f.category)}</td>
      <td>${text(f.title)}</td>
      <td>${f.suppressed ? "suppressed" : "active"}</td>
    </tr>`).join("");
  document.querySelectorAll("tbody tr").forEach((row) => row.addEventListener("click", () => detail(Number(row.dataset.index))));
  detail(0);
}

function detail(index) {
  const f = findings[index];
  if (!f) return;
  $("detail").innerHTML = `
    <h3>${text(f.title)}</h3>
    <p><strong>Rule:</strong> <code>${text(f.rule_id)}</code> <strong>Component:</strong> ${text(f.affected_component)}</p>
    <p><strong>Remediation:</strong> ${text(f.remediation)}</p>
    <h4>Evidence</h4>
    ${renderList((f.evidence || []).map((e) => `${e.source}.${e.key}: ${e.value}`))}
  `;
}

$("severity").addEventListener("change", loadFindings);
$("category").addEventListener("change", loadFindings);
load().catch((error) => { document.body.innerHTML = `<main><h1>Unable to load report</h1><p>${text(error)}</p></main>`; });
"""

__all__ = [
    "UiServerError",
    "UiServerOptions",
    "UiServerState",
    "create_ui_server",
    "load_report_state",
    "run_ui_server",
]
