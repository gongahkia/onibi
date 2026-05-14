# ruff: noqa: E501

from __future__ import annotations

import io
import json
import os
import shutil
import subprocess
import sys
import threading
import uuid
import webbrowser
import zipfile
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import UTC, datetime
from email import policy
from email.parser import BytesParser
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path, PurePosixPath
from typing import Any
from urllib.parse import parse_qs, urlparse

from pydantic import ValidationError

from piranesi.host.api import load_host_report
from piranesi.host.fleet import load_fleet_report
from piranesi.host.models import FleetReport, HostFinding, HostPostureReport
from piranesi.report.renderer import PiranesiReport


class UiServerError(RuntimeError):
    """Raised when the local UI cannot safely load a report root."""


@dataclass(frozen=True, slots=True)
class UiServerOptions:
    report_path: Path | None = None
    host: str = "127.0.0.1"
    port: int = 8765
    watch: bool = False
    open_browser: bool = False
    workbench: bool = False
    jobs_dir: Path | None = None
    max_upload_mb: int = 100
    scan_timeout_seconds: int = 900


@dataclass(slots=True)
class UiServerState:
    root: Path | None
    report_path: Path | None
    report_type: str
    report: HostPostureReport | FleetReport | PiranesiReport | None
    watch: bool = False
    workbench: WorkbenchState | None = None

    def reload(self) -> None:
        if self.root is None:
            return
        loaded = load_report_state(self.root, watch=self.watch)
        self.report_path = loaded.report_path
        self.report_type = loaded.report_type
        self.report = loaded.report


@dataclass(slots=True)
class LocalScanJob:
    job_id: str
    target_name: str
    job_dir: Path
    upload_path: Path
    extract_dir: Path
    project_dir: Path
    output_dir: Path
    log_path: Path
    created_at: str
    updated_at: str
    input_kind: str = "zip"
    status: str = "queued"
    current_stage: str = "Upload"
    report_path: Path | None = None
    markdown_path: Path | None = None
    error: str | None = None
    return_code: int | None = None


ScanRunner = Callable[[LocalScanJob, "WorkbenchState"], None]


@dataclass(slots=True)
class WorkbenchState:
    jobs_dir: Path
    max_upload_bytes: int
    max_extracted_bytes: int
    max_extracted_files: int
    scan_timeout_seconds: int
    scan_runner: ScanRunner
    jobs: dict[str, LocalScanJob] = field(default_factory=dict)
    lock: threading.Lock = field(default_factory=threading.Lock)
    active_job_id: str | None = None


def load_report_state(path: str | Path, *, watch: bool = False) -> UiServerState:
    root = Path(path).expanduser().resolve(strict=False)
    if root.is_file():
        report_path = root
        root_dir = root.parent
    elif root.is_dir():
        root_dir = root
        host_report = root / "host-report.json"
        fleet_report = root / "fleet-report.json"
        source_report = root / "report.json"
        if host_report.is_file():
            report_path = host_report
        elif fleet_report.is_file():
            report_path = fleet_report
        elif source_report.is_file():
            report_path = source_report
        else:
            raise UiServerError(
                f"{root} must contain host-report.json, fleet-report.json, or report.json"
            )
    else:
        raise UiServerError(f"report path does not exist: {root}")

    if not _is_safe_report_path(report_path, root_dir):
        raise UiServerError(f"unsafe report path: {report_path}")
    report: HostPostureReport | FleetReport | PiranesiReport
    try:
        if report_path.name == "host-report.json":
            report = load_host_report(report_path)
            report_type = "host"
        elif report_path.name == "fleet-report.json":
            report = load_fleet_report(report_path)
            report_type = "fleet"
        elif report_path.name == "report.json":
            report = PiranesiReport.model_validate_json(report_path.read_text(encoding="utf-8"))
            report_type = "source"
        else:
            raise UiServerError(
                "report file must be host-report.json, fleet-report.json, or report.json"
            )
    except (OSError, ValueError, ValidationError) as exc:
        raise UiServerError(f"failed to load {report_path.name}: {exc}") from exc
    return UiServerState(
        root=root_dir,
        report_path=report_path,
        report_type=report_type,
        report=report,
        watch=watch,
    )


def create_ui_server(
    report_path: str | Path | None = None,
    *,
    host: str = "127.0.0.1",
    port: int = 8765,
    watch: bool = False,
    workbench: bool = False,
    jobs_dir: str | Path | None = None,
    max_upload_bytes: int = 100 * 1024 * 1024,
    scan_timeout_seconds: int = 900,
    scan_runner: ScanRunner | None = None,
) -> ThreadingHTTPServer:
    if report_path is None:
        if not workbench:
            raise UiServerError("report_path is required unless workbench=True")
        state = _create_workbench_state(
            jobs_dir=jobs_dir,
            max_upload_bytes=max_upload_bytes,
            scan_timeout_seconds=scan_timeout_seconds,
            scan_runner=scan_runner,
        )
    else:
        state = load_report_state(report_path, watch=watch)
        if workbench:
            state.workbench = _create_workbench(
                jobs_dir=jobs_dir,
                max_upload_bytes=max_upload_bytes,
                scan_timeout_seconds=scan_timeout_seconds,
                scan_runner=scan_runner,
            )

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
        workbench=options.workbench,
        jobs_dir=options.jobs_dir,
        max_upload_bytes=options.max_upload_mb * 1024 * 1024,
        scan_timeout_seconds=options.scan_timeout_seconds,
    )
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


def _create_workbench_state(
    *,
    jobs_dir: str | Path | None,
    max_upload_bytes: int,
    scan_timeout_seconds: int,
    scan_runner: ScanRunner | None,
) -> UiServerState:
    return UiServerState(
        root=None,
        report_path=None,
        report_type="workbench",
        report=None,
        workbench=_create_workbench(
            jobs_dir=jobs_dir,
            max_upload_bytes=max_upload_bytes,
            scan_timeout_seconds=scan_timeout_seconds,
            scan_runner=scan_runner,
        ),
    )


def _create_workbench(
    *,
    jobs_dir: str | Path | None,
    max_upload_bytes: int,
    scan_timeout_seconds: int,
    scan_runner: ScanRunner | None,
) -> WorkbenchState:
    resolved_jobs_dir = (
        Path(jobs_dir).expanduser().resolve(strict=False)
        if jobs_dir is not None
        else (Path.home() / ".piranesi" / "ui-jobs").resolve(strict=False)
    )
    resolved_jobs_dir.mkdir(parents=True, exist_ok=True)
    return WorkbenchState(
        jobs_dir=resolved_jobs_dir,
        max_upload_bytes=max_upload_bytes,
        max_extracted_bytes=500 * 1024 * 1024,
        max_extracted_files=10_000,
        scan_timeout_seconds=scan_timeout_seconds,
        scan_runner=scan_runner or _default_scan_runner,
    )


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
        if parsed.path == "/api/artifacts/report-md":
            try:
                self._send_text(
                    _state_markdown(self.server_state),
                    content_type="text/markdown; charset=utf-8",
                )
            except UiServerError as exc:
                self._send_json({"error": str(exc)}, status=HTTPStatus.NOT_FOUND)
            return
        if parsed.path.startswith("/api/app-scans/"):
            self._handle_app_scan_get(parsed.path, parse_qs(parsed.query))
            return
        self._send_json({"error": "not found"}, status=HTTPStatus.NOT_FOUND)

    def do_POST(self) -> None:
        parsed = urlparse(self.path)
        if parsed.path == "/api/app-scans":
            self._handle_app_scan_post()
            return
        self._send_json({"error": "not found"}, status=HTTPStatus.NOT_FOUND)

    def _handle_app_scan_get(
        self,
        path: str,
        query: dict[str, list[str]],
    ) -> None:
        workbench = self.server_state.workbench
        if workbench is None:
            self._send_json({"error": "workbench is not enabled"}, status=HTTPStatus.NOT_FOUND)
            return
        parts = path.strip("/").split("/")
        if len(parts) < 2 or parts[0] != "api" or parts[1] != "app-scans":
            self._send_json({"error": "not found"}, status=HTTPStatus.NOT_FOUND)
            return
        if len(parts) == 2:
            with workbench.lock:
                jobs = [_job_payload(job) for job in workbench.jobs.values()]
            self._send_json({"jobs": jobs})
            return
        job = _get_job(workbench, parts[2])
        if job is None:
            self._send_json({"error": "job not found"}, status=HTTPStatus.NOT_FOUND)
            return
        if len(parts) == 3:
            self._send_json(_job_payload(job))
            return
        if len(parts) == 4 and parts[3] == "report":
            try:
                self._send_json(_job_report_payload(job))
            except UiServerError as exc:
                self._send_json({"error": str(exc)}, status=HTTPStatus.CONFLICT)
            return
        if len(parts) == 4 and parts[3] == "findings":
            try:
                self._send_json(_job_findings_payload(job, query))
            except UiServerError as exc:
                self._send_json({"error": str(exc)}, status=HTTPStatus.CONFLICT)
            return
        if len(parts) == 5 and parts[3] == "artifacts" and parts[4] == "report-md":
            try:
                self._send_text(
                    _read_job_markdown(job),
                    content_type="text/markdown; charset=utf-8",
                )
            except UiServerError as exc:
                self._send_json({"error": str(exc)}, status=HTTPStatus.CONFLICT)
            return
        self._send_json({"error": "not found"}, status=HTTPStatus.NOT_FOUND)

    def _handle_app_scan_post(self) -> None:
        workbench = self.server_state.workbench
        if workbench is None:
            self._send_json({"error": "workbench is not enabled"}, status=HTTPStatus.NOT_FOUND)
            return
        content_length = self.headers.get("Content-Length")
        if content_length is None:
            self._send_json({"error": "Content-Length is required"}, status=HTTPStatus.LENGTH_REQUIRED)
            return
        try:
            length = int(content_length)
        except ValueError:
            self._send_json({"error": "invalid Content-Length"}, status=HTTPStatus.BAD_REQUEST)
            return
        if length <= 0:
            self._send_json({"error": "upload body is empty"}, status=HTTPStatus.BAD_REQUEST)
            return
        if length > workbench.max_upload_bytes:
            self._send_json(
                {"error": f"upload exceeds {workbench.max_upload_bytes // (1024 * 1024)} MB"},
                status=HTTPStatus.REQUEST_ENTITY_TOO_LARGE,
            )
            return
        with workbench.lock:
            active = workbench.active_job_id
            if active is not None and workbench.jobs.get(active, None) is not None:
                active_job = workbench.jobs[active]
                if active_job.status in {"queued", "running"}:
                    self._send_json(
                        {"error": "a scan is already running", "active_job_id": active},
                        status=HTTPStatus.CONFLICT,
                    )
                    return

        body = self.rfile.read(length)
        try:
            upload = _parse_zip_upload(
                content_type=self.headers.get("Content-Type", ""),
                body=body,
            )
            job = _create_scan_job(workbench, upload)
        except UiServerError as exc:
            self._send_json({"error": str(exc)}, status=HTTPStatus.BAD_REQUEST)
            return

        with workbench.lock:
            active = workbench.active_job_id
            if active is not None and workbench.jobs.get(active, None) is not None:
                active_job = workbench.jobs[active]
                if active_job.status in {"queued", "running"}:
                    shutil.rmtree(job.job_dir, ignore_errors=True)
                    self._send_json(
                        {"error": "a scan is already running", "active_job_id": active},
                        status=HTTPStatus.CONFLICT,
                    )
                    return
            workbench.jobs[job.job_id] = job
            workbench.active_job_id = job.job_id
        thread = threading.Thread(target=_run_scan_job, args=(job, workbench), daemon=True)
        thread.start()
        self._send_json(_job_payload(job), status=HTTPStatus.ACCEPTED)

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
    if state.report_type == "workbench":
        return {
            "type": "workbench",
            "title": "Piranesi Local Workbench",
            "workbench": True,
        }
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
    if state.report_type == "source":
        report = state.report
        assert isinstance(report, PiranesiReport)
        return _source_report_summary(report, report_dir=state.root)
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
    if state.report_type == "source":
        report = state.report
        assert isinstance(report, PiranesiReport)
        return _source_findings_payload(report, query)
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


def _source_report_summary(report: PiranesiReport, *, report_dir: Path | None = None) -> dict[str, Any]:
    summary = report.executive_summary
    target = _source_target_label(report.target)
    report_md_available = bool(report_dir is not None and (report_dir / "report.md").is_file())
    return {
        "type": "source",
        "target": target,
        "generated_at": report.generated_at,
        "files_scanned": len(report.files_scanned),
        "summary": {
            "findings_total": summary.findings_detected,
            "findings_confirmed": summary.findings_confirmed,
            "suppressed_findings": summary.suppressed_findings,
            "reachable_findings": summary.reachable_findings,
            "unreachable_findings": summary.unreachable_findings,
            "severity_breakdown": summary.severity_breakdown,
            "status_breakdown": summary.status_breakdown,
        },
        "highest_risk": {
            "score": summary.highest_composite_risk_score,
            "band": summary.highest_composite_risk_band,
            "finding_id": summary.highest_composite_risk_finding_id,
        },
        "scan_metadata": report.scan_metadata.model_dump(mode="json"),
        "known_limitations": [
            limitation.model_dump(mode="json") for limitation in report.known_limitations
        ],
        "artifacts": {
            "report_json": "/api/report",
            "report_md": "/api/artifacts/report-md" if report_md_available else None,
        },
    }


def _source_findings_payload(
    report: PiranesiReport,
    query: dict[str, list[str]],
) -> dict[str, Any]:
    severity = _first_query(query, "severity")
    category = _first_query(query, "category")
    suppressed = _first_query(query, "suppressed")
    status = _first_query(query, "status")
    findings = _source_findings(report)
    if severity:
        findings = [finding for finding in findings if finding["severity"] == severity]
    if category:
        findings = [finding for finding in findings if finding["category"] == category]
    if suppressed in {"true", "false"}:
        want = suppressed == "true"
        findings = [finding for finding in findings if finding["suppressed"] is want]
    if status:
        findings = [finding for finding in findings if finding["evidence_status"] == status]
    return {"findings": findings}


def _source_findings(report: PiranesiReport) -> list[dict[str, Any]]:
    payloads: list[dict[str, Any]] = []
    seen: set[str] = set()
    for finding in [
        *report.findings,
        *report.active_findings,
        *report.unreachable_findings,
        *report.suppressed_findings,
    ]:
        finding_id = str(getattr(finding, "finding_id", ""))
        if finding_id in seen:
            continue
        seen.add(finding_id)
        payloads.append(_source_finding_payload(finding))
    return sorted(
        payloads,
        key=lambda item: (
            _status_rank(str(item["evidence_status"])),
            -float(item["risk"]["total"]),
            str(item["title"]),
        ),
    )


def _source_finding_payload(finding: Any) -> dict[str, Any]:
    source_location = getattr(finding, "source_location", None)
    sink_location = getattr(finding, "sink_location", None)
    cwe = str(getattr(finding, "cwe", "") or "unknown")
    status = str(getattr(finding, "evidence_status", "") or "static_candidate")
    evidence = [
        _source_evidence("source", source_location),
        _source_evidence("sink", sink_location),
    ]
    evidence.extend(
        _source_evidence(f"path.{index + 1}", getattr(step, "location", None), step)
        for index, step in enumerate(getattr(finding, "taint_path", []) or [])
    )
    return {
        "id": str(getattr(finding, "finding_id", "")),
        "finding_id": str(getattr(finding, "finding_id", "")),
        "title": str(getattr(finding, "title", "") or "Untitled finding"),
        "severity": str(getattr(finding, "severity", "") or "informational").lower(),
        "category": cwe,
        "cwe": cwe,
        "risk": {
            "total": float(getattr(finding, "composite_risk_score", 0.0) or 0.0),
            "band": getattr(finding, "composite_risk_band", None),
        },
        "confidence": float(getattr(finding, "confidence", 0.0) or 0.0),
        "evidence_status": status,
        "suppressed": status == "suppressed",
        "rule_id": cwe,
        "affected_component": _format_location(sink_location),
        "source_location": _location_payload(source_location),
        "sink_location": _location_payload(sink_location),
        "taint_source": str(getattr(finding, "taint_source", "") or ""),
        "taint_sink": str(getattr(finding, "taint_sink", "") or ""),
        "taint_path": [
            _taint_step_payload(step) for step in (getattr(finding, "taint_path", []) or [])
        ],
        "remediation": _source_remediation(finding),
        "evidence": [item for item in evidence if item is not None],
        "verification_method": getattr(finding, "verification_method", None),
        "verified": bool(getattr(finding, "verified", False)),
        "package_name": getattr(finding, "package_name", None),
    }


def _source_evidence(prefix: str, location: Any, step: Any | None = None) -> dict[str, str] | None:
    payload = _location_payload(location)
    if payload is None:
        return None
    operation = ""
    if step is not None:
        operation = f" operation={getattr(step, 'operation', '')}"
    return {
        "source": "source",
        "key": prefix,
        "value": _redact_text(
            f"{payload['file']}:{payload['line']} {payload['snippet']}{operation}".strip()
        ),
    }


def _location_payload(location: Any) -> dict[str, Any] | None:
    if location is None:
        return None
    return {
        "file": str(getattr(location, "file", "") or ""),
        "line": int(getattr(location, "line", 0) or 0),
        "column": int(getattr(location, "column", 0) or 0),
        "snippet": _redact_text(str(getattr(location, "snippet", "") or "")),
    }


def _taint_step_payload(step: Any) -> dict[str, Any]:
    return {
        "operation": str(getattr(step, "operation", "") or ""),
        "taint_state": str(getattr(step, "taint_state", "") or ""),
        "location": _location_payload(getattr(step, "location", None)),
    }


def _format_location(location: Any) -> str:
    if location is None:
        return "unknown"
    file_name = str(getattr(location, "file", "") or "unknown")
    line = int(getattr(location, "line", 0) or 0)
    return f"{file_name}:{line}" if line else file_name


def _source_remediation(finding: Any) -> str:
    patch_explanation = getattr(finding, "patch_explanation", None)
    if patch_explanation:
        return str(patch_explanation)
    cwe = str(getattr(finding, "cwe", "") or "")
    if "89" in cwe:
        return "Use parameterized queries and keep untrusted input out of SQL strings."
    if "78" in cwe:
        return "Avoid shell execution with untrusted input; use argument arrays and allowlists."
    if "918" in cwe:
        return "Restrict outbound requests to trusted destinations and block internal network targets."
    return "Review the source-to-sink path and add framework-appropriate validation, encoding, or access control."


def _status_rank(status: str) -> int:
    order = {
        "confirmed": 0,
        "triaged_active_candidate": 1,
        "static_candidate": 2,
        "unreachable_candidate": 3,
        "suppressed": 4,
    }
    return order.get(status, 99)


def _source_target_label(target: str) -> str:
    path = Path(target)
    name = path.name
    return name or "uploaded app"


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
    return (
        report_path.name in {"host-report.json", "fleet-report.json", "report.json"}
        and report_path.is_file()
    )


def _parse_zip_upload(*, content_type: str, body: bytes) -> tuple[str, bytes]:
    if content_type.startswith("application/zip"):
        return ("upload.zip", body)
    if "multipart/form-data" not in content_type:
        raise UiServerError("upload must be multipart/form-data with a ZIP file")
    message = BytesParser(policy=policy.default).parsebytes(
        f"Content-Type: {content_type}\r\nMIME-Version: 1.0\r\n\r\n".encode()
        + body
    )
    if not message.is_multipart():
        raise UiServerError("upload body is not multipart")
    for part in message.iter_parts():
        filename = part.get_filename()
        if not filename:
            continue
        payload = part.get_payload(decode=True)
        if payload is None:
            continue
        if not isinstance(payload, bytes):
            continue
        if not filename.lower().endswith(".zip"):
            raise UiServerError("uploaded file must use a .zip extension")
        return (Path(filename).name, payload)
    raise UiServerError("multipart upload must include a ZIP file field")


def _create_scan_job(workbench: WorkbenchState, upload: tuple[str, bytes]) -> LocalScanJob:
    filename, payload = upload
    if not zipfile.is_zipfile(io.BytesIO(payload)):
        raise UiServerError("uploaded file is not a valid ZIP archive")
    job_id = uuid.uuid4().hex[:12]
    job_dir = workbench.jobs_dir / job_id
    upload_path = job_dir / "upload.zip"
    extract_dir = job_dir / "source"
    output_dir = job_dir / "output"
    log_path = job_dir / "scan.log"
    job_dir.mkdir(parents=True, exist_ok=False)
    extract_dir.mkdir()
    output_dir.mkdir()
    upload_path.write_bytes(payload)
    try:
        _safe_extract_zip(upload_path, extract_dir, workbench)
    except Exception:
        shutil.rmtree(job_dir, ignore_errors=True)
        raise
    project_dir = _detect_project_dir(extract_dir)
    now = _now_iso()
    return LocalScanJob(
        job_id=job_id,
        target_name=_target_name(filename, project_dir),
        job_dir=job_dir,
        upload_path=upload_path,
        extract_dir=extract_dir,
        project_dir=project_dir,
        output_dir=output_dir,
        log_path=log_path,
        created_at=now,
        updated_at=now,
    )


def _safe_extract_zip(
    upload_path: Path,
    extract_dir: Path,
    workbench: WorkbenchState,
) -> None:
    total_size = 0
    file_count = 0
    with zipfile.ZipFile(upload_path) as archive:
        for info in archive.infolist():
            if _unsafe_zip_member(info):
                raise UiServerError(f"unsafe ZIP member: {info.filename}")
            if info.is_dir():
                continue
            file_count += 1
            if file_count > workbench.max_extracted_files:
                raise UiServerError(
                    f"ZIP contains more than {workbench.max_extracted_files} files"
                )
            total_size += info.file_size
            if total_size > workbench.max_extracted_bytes:
                limit_mb = workbench.max_extracted_bytes // (1024 * 1024)
                raise UiServerError(f"ZIP extracts to more than {limit_mb} MB")

        for info in archive.infolist():
            if info.is_dir():
                continue
            relative = PurePosixPath(info.filename)
            destination = (extract_dir / Path(*relative.parts)).resolve(strict=False)
            try:
                destination.relative_to(extract_dir.resolve(strict=False))
            except ValueError as exc:
                raise UiServerError(f"unsafe ZIP member: {info.filename}") from exc
            destination.parent.mkdir(parents=True, exist_ok=True)
            with archive.open(info) as source, destination.open("wb") as target:
                shutil.copyfileobj(source, target)


def _unsafe_zip_member(info: zipfile.ZipInfo) -> bool:
    name = info.filename
    mode = info.external_attr >> 16
    is_symlink = (mode & 0o170000) == 0o120000
    if is_symlink:
        return True
    if not name or name.startswith(("/", "\\")) or "\\" in name or ":" in name:
        return True
    relative = PurePosixPath(name)
    return any(part in {"", ".", ".."} for part in relative.parts)


def _detect_project_dir(extract_dir: Path) -> Path:
    children = [
        child
        for child in extract_dir.iterdir()
        if child.name not in {"__MACOSX", ".DS_Store"}
    ]
    if len(children) == 1 and children[0].is_dir():
        return children[0]
    return extract_dir


def _target_name(filename: str, project_dir: Path) -> str:
    stem = Path(filename).stem.strip()
    return stem or project_dir.name or "uploaded app"


def _run_scan_job(job: LocalScanJob, workbench: WorkbenchState) -> None:
    _update_job(job, status="running", current_stage="Scan")
    try:
        workbench.scan_runner(job, workbench)
        report_path = job.output_dir / "report.json"
        markdown_path = job.output_dir / "report.md"
        if not report_path.is_file():
            raise UiServerError("scan completed without report.json")
        _update_job(
            job,
            status="succeeded",
            current_stage="Report",
            report_path=report_path,
            markdown_path=markdown_path if markdown_path.is_file() else None,
        )
    except subprocess.TimeoutExpired:
        _update_job(
            job,
            status="failed",
            current_stage="Report",
            error=f"scan exceeded {workbench.scan_timeout_seconds}s timeout",
        )
    except Exception as exc:
        _update_job(job, status="failed", current_stage="Report", error=str(exc))
    finally:
        with workbench.lock:
            if workbench.active_job_id == job.job_id:
                workbench.active_job_id = None


def _default_scan_runner(job: LocalScanJob, workbench: WorkbenchState) -> None:
    config_path = job.job_dir / "piranesi.toml"
    config_path.write_text("", encoding="utf-8")
    command = [
        sys.executable,
        "-c",
        "from piranesi.cli import app; app()",
        "run",
        str(job.project_dir),
        "--output",
        str(job.output_dir),
        "--config",
        str(config_path),
        "--no-execute",
        "--no-fail",
        "--format",
        "both",
        "--authorized",
        "--yes",
        "--quiet",
    ]
    result = subprocess.run(
        command,
        cwd=job.job_dir,
        env=_scan_env(),
        text=True,
        capture_output=True,
        timeout=workbench.scan_timeout_seconds,
        check=False,
    )
    job.return_code = result.returncode
    job.log_path.write_text(
        "\n".join(
            [
                "$ " + " ".join(command),
                "",
                "[stdout]",
                result.stdout,
                "",
                "[stderr]",
                result.stderr,
            ]
        ),
        encoding="utf-8",
    )
    if result.returncode != 0:
        stderr_tail = "\n".join(result.stderr.strip().splitlines()[-8:])
        raise UiServerError(stderr_tail or f"scan exited with code {result.returncode}")


def _scan_env() -> dict[str, str]:
    env = dict(os.environ)
    for key in list(env):
        lowered = key.lower()
        if lowered.endswith("_api_key") or lowered in {
            "openai_api_key",
            "anthropic_api_key",
            "gemini_api_key",
            "google_api_key",
            "mistral_api_key",
            "deepseek_api_key",
            "openrouter_api_key",
            "xai_api_key",
        }:
            env.pop(key, None)
    env["PYTHONUNBUFFERED"] = "1"
    return env


def _update_job(job: LocalScanJob, **updates: Any) -> None:
    for key, value in updates.items():
        setattr(job, key, value)
    job.updated_at = _now_iso()


def _get_job(workbench: WorkbenchState, job_id: str) -> LocalScanJob | None:
    with workbench.lock:
        return workbench.jobs.get(job_id)


def _job_payload(job: LocalScanJob) -> dict[str, Any]:
    return {
        "id": job.job_id,
        "job_id": job.job_id,
        "target_name": job.target_name,
        "input_kind": job.input_kind,
        "status": job.status,
        "current_stage": job.current_stage,
        "created_at": job.created_at,
        "updated_at": job.updated_at,
        "error": job.error,
        "return_code": job.return_code,
        "artifacts": {
            "report": f"/api/app-scans/{job.job_id}/report"
            if job.report_path is not None
            else None,
            "findings": f"/api/app-scans/{job.job_id}/findings"
            if job.report_path is not None
            else None,
            "report_md": f"/api/app-scans/{job.job_id}/artifacts/report-md"
            if job.markdown_path is not None
            else None,
        },
    }


def _job_report_payload(job: LocalScanJob) -> dict[str, Any]:
    report = _load_job_report(job)
    payload = _source_report_summary(report, report_dir=job.output_dir)
    payload["job"] = _job_payload(job)
    payload["artifacts"] = {
        "report_json": f"/api/app-scans/{job.job_id}/report",
        "report_md": f"/api/app-scans/{job.job_id}/artifacts/report-md"
        if job.markdown_path is not None
        else None,
    }
    return payload


def _job_findings_payload(job: LocalScanJob, query: dict[str, list[str]]) -> dict[str, Any]:
    return _source_findings_payload(_load_job_report(job), query)


def _load_job_report(job: LocalScanJob) -> PiranesiReport:
    report_path = job.report_path or (job.output_dir / "report.json")
    if not report_path.is_file():
        raise UiServerError("report is not ready")
    return PiranesiReport.model_validate_json(report_path.read_text(encoding="utf-8"))


def _read_job_markdown(job: LocalScanJob) -> str:
    markdown_path = job.markdown_path or (job.output_dir / "report.md")
    if not markdown_path.is_file():
        raise UiServerError("report.md is not ready")
    return markdown_path.read_text(encoding="utf-8")


def _state_markdown(state: UiServerState) -> str:
    if state.report_type != "source" or state.root is None:
        raise UiServerError("Markdown artifact is only available for source reports")
    markdown_path = state.root / "report.md"
    if not markdown_path.is_file():
        raise UiServerError("report.md is not available")
    return markdown_path.read_text(encoding="utf-8")


def _now_iso() -> str:
    return datetime.now(UTC).isoformat().replace("+00:00", "Z")


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
      <p id="eyebrow">Piranesi local review</p>
      <h1 id="title">Review Workbench</h1>
      <span id="subtitle"></span>
    </div>
    <strong id="score"></strong>
  </header>
  <main>
    <section id="workbench" class="workbench" hidden>
      <div class="prompt-card">
        <h2>What do you want to review?</h2>
        <form id="uploadForm">
          <label id="dropzone" class="dropzone" for="archive">
            <input id="archive" name="archive" type="file" accept=".zip,application/zip">
            <span id="archiveLabel">Upload a web app ZIP</span>
            <small>Runs locally. Source code stays on this machine.</small>
          </label>
          <div class="prompt-row">
            <input id="urlInput" disabled value="URL import is planned for the next roadmap item">
            <button id="runButton" type="submit">Run review</button>
          </div>
        </form>
        <ol id="steps" class="steps">
          <li data-step="Upload">Upload</li>
          <li data-step="Extract">Extract</li>
          <li data-step="Scan">Scan</li>
          <li data-step="Detect">Detect</li>
          <li data-step="Report">Report</li>
        </ol>
        <p id="workbenchStatus" class="muted"></p>
      </div>
    </section>
    <section id="reportView">
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
      <section class="grid" id="supportGrid">
        <section class="panel"><h2 id="supportAHeading">Top Actions</h2><div id="actions"></div></section>
        <section class="panel"><h2 id="supportBHeading">Evidence Inventory</h2><div id="evidence"></div></section>
        <section class="panel"><h2 id="supportCHeading">Collection Health</h2><div id="health"></div></section>
        <section class="panel"><h2 id="supportDHeading">Suppression Review</h2><div id="suppression"></div></section>
      </section>
    </section>
  </main>
  <script src="/app.js"></script>
</body>
</html>
"""

_APP_CSS = """
:root { --bg:#f7f8fa; --panel:#fff; --border:#d7dee8; --text:#111827; --muted:#667085; --accent:#0f766e; --dark:#101827; }
* { box-sizing:border-box; }
[hidden] { display:none !important; }
body { margin:0; background:var(--bg); color:var(--text); font:14px/1.45 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif; }
header { display:flex; justify-content:space-between; align-items:flex-start; gap:20px; padding:24px 32px; background:var(--dark); color:white; }
header p { margin:0 0 4px; color:#a9b3c1; text-transform:uppercase; font-size:12px; letter-spacing:0; }
header span { color:#a9b3c1; }
h1,h2,h3,h4,p { margin-top:0; letter-spacing:0; }
main { padding:22px 32px 40px; }
.grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(190px,1fr)); gap:14px; margin-bottom:16px; }
.metric,.panel,.prompt-card { background:var(--panel); border:1px solid var(--border); border-radius:8px; padding:15px; }
.metric strong { display:block; font-size:24px; word-break:break-word; }
.metric span,.muted { color:var(--muted); }
.panel { margin-bottom:16px; }
.panel-head { display:flex; justify-content:space-between; gap:12px; align-items:center; }
.workbench { min-height:calc(100vh - 150px); display:grid; place-items:center; }
.prompt-card { width:min(760px,100%); padding:26px; box-shadow:0 12px 30px rgba(15,23,42,.08); }
.prompt-card h2 { font-size:32px; text-align:center; margin-bottom:22px; }
.dropzone { display:flex; flex-direction:column; justify-content:center; min-height:130px; border:1px solid var(--border); border-radius:8px; padding:20px; cursor:pointer; background:#fbfcfe; }
.dropzone input { display:none; }
.dropzone span { font-size:18px; }
.dropzone small { color:var(--muted); margin-top:7px; }
.dropzone.dragover { border-color:var(--accent); background:#f0fdfa; }
.prompt-row { display:grid; grid-template-columns:1fr auto; gap:10px; margin-top:12px; }
input,select,button { font:inherit; }
input,select { padding:8px 10px; border:1px solid var(--border); border-radius:6px; background:white; min-width:0; }
input:disabled { color:var(--muted); background:#f8fafc; }
button { border:1px solid #111827; border-radius:6px; background:#111827; color:white; padding:8px 13px; cursor:pointer; }
button:disabled { opacity:.55; cursor:not-allowed; }
.steps { display:grid; grid-template-columns:repeat(5,1fr); gap:8px; list-style:none; padding:0; margin:18px 0 0; }
.steps li { border:1px solid var(--border); border-radius:6px; padding:8px; color:var(--muted); text-align:center; }
.steps li.active { border-color:var(--accent); color:var(--accent); background:#f0fdfa; }
.steps li.done { color:#14532d; background:#f0fdf4; }
table { width:100%; border-collapse:collapse; }
th,td { padding:9px 7px; border-bottom:1px solid var(--border); text-align:left; vertical-align:top; }
tbody tr { cursor:pointer; }
tbody tr:hover { background:#f1f5f9; }
.severity { font-weight:700; text-transform:capitalize; }
.critical,.high { color:#b91c1c; } .medium { color:#b45309; } .low { color:#1d4ed8; } .informational { color:#475569; }
.detail { margin-top:12px; border-left:3px solid var(--accent); background:#f8fafc; padding:12px; }
.detail-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(240px,1fr)); gap:10px; }
.artifact-link { display:inline-block; margin-right:10px; }
ul { margin:0; padding-left:18px; }
code { background:#eef2f7; border-radius:4px; padding:1px 4px; word-break:break-word; }
@media (max-width:700px) { header,main { padding-left:18px; padding-right:18px; } .panel-head,.prompt-row { align-items:stretch; grid-template-columns:1fr; flex-direction:column; } table { font-size:13px; } .steps { grid-template-columns:1fr; } .prompt-card h2 { font-size:26px; } }
"""

_APP_JS = """
let report = null;
let findings = [];
let allFindings = [];
let findingsEndpoint = "/api/findings";
let activeJob = null;

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
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(payload.error || `request failed: ${response.status}`);
  return payload;
}

function metric(label, value) {
  return `<div class="metric"><strong>${text(value)}</strong><span>${label}</span></div>`;
}

function renderList(items) {
  if (!items || !items.length) return '<p class="muted">None recorded.</p>';
  return `<ul>${items.map((item) => `<li>${text(item)}</li>`).join("")}</ul>`;
}

async function load() {
  const initial = await getJson("/api/report");
  if (initial.type === "workbench") {
    renderWorkbench();
    return;
  }
  await renderReport(initial, "/api/findings");
}

function renderWorkbench() {
  $("reportView").hidden = true;
  $("workbench").hidden = false;
  $("eyebrow").textContent = "Piranesi local workbench";
  $("title").textContent = "What do you want to review?";
  $("subtitle").textContent = "Upload a web app ZIP and Piranesi will produce a local risk report.";
  $("score").textContent = "";
}

async function renderReport(nextReport, nextFindingsEndpoint) {
  report = nextReport;
  findingsEndpoint = nextFindingsEndpoint;
  $("workbench").hidden = true;
  $("reportView").hidden = false;
  $("fleetPanel").hidden = true;
  $("eyebrow").textContent = "Piranesi local review";
  $("title").textContent = report.type === "fleet" ? "Fleet Review" : report.type === "source" ? "Application Review" : "Host Review";
  $("subtitle").textContent = report.type === "source" ? "Static source review generated by the local workbench." : "";
  $("score").textContent = report.posture_score !== undefined ? `${report.posture_score}/100` : report.highest_risk?.score ? `${Number(report.highest_risk.score).toFixed(1)} risk` : "";
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
    clearSupport();
    return;
  }
  if (report.type === "source") {
    $("overview").innerHTML = [
      metric("Target", report.target),
      metric("Files scanned", report.files_scanned || 0),
      metric("Findings", report.summary?.findings_total || 0),
      metric("Confirmed", report.summary?.findings_confirmed || 0),
    ].join("");
    $("supportAHeading").textContent = "Scan Metadata";
    $("supportBHeading").textContent = "Severity";
    $("supportCHeading").textContent = "Artifacts";
    $("supportDHeading").textContent = "Known Limitations";
    $("actions").innerHTML = renderList(Object.entries(report.scan_metadata || {}).map(([k,v]) => `${k}: ${v}`));
    $("evidence").innerHTML = renderList(Object.entries(report.summary?.severity_breakdown || {}).map(([k,v]) => `${k}: ${v}`));
    $("health").innerHTML = artifactLinks();
    $("suppression").innerHTML = renderList((report.known_limitations || []).map((item) => `${item.title}: ${item.status}`));
    return;
  }
  $("overview").innerHTML = [
    metric("Target", report.target),
    metric("Findings", report.summary?.findings_total || 0),
    metric("Posture", `${report.posture_score}/100`),
    metric("Evidence", Object.keys(report.evidence_inventory || {}).length),
  ].join("");
  $("supportAHeading").textContent = "Top Actions";
  $("supportBHeading").textContent = "Evidence Inventory";
  $("supportCHeading").textContent = "Collection Health";
  $("supportDHeading").textContent = "Suppression Review";
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

function clearSupport() {
  for (const id of ["actions", "evidence", "health", "suppression"]) $(id).innerHTML = "";
}

function artifactLinks() {
  const reportMd = report.artifacts?.report_md;
  if (!reportMd) return '<p class="muted">JSON report is available through the local API.</p>';
  return `<a class="artifact-link" href="${text(reportMd)}" target="_blank" rel="noreferrer">Open Markdown report</a>`;
}

function renderFleet() {
  $("fleetPanel").hidden = false;
  $("fleet").innerHTML = renderList((report.hosts || []).map((h) => `${h.target}: ${h.status}, score ${h.posture_score}, findings ${h.findings_total}`));
}

async function loadFindings() {
  if (!allFindings.length) allFindings = (await getJson(findingsEndpoint)).findings || [];
  const params = new URLSearchParams();
  if ($("severity").value) params.set("severity", $("severity").value);
  if ($("category").value) params.set("category", $("category").value);
  findings = (await getJson(`${findingsEndpoint}?${params.toString()}`)).findings || [];
  renderFilters();
  renderFindings();
}

function renderFilters() {
  const severities = [...new Set(allFindings.map((f) => f.severity).filter(Boolean))].sort();
  const categories = [...new Set(allFindings.map((f) => f.category).filter(Boolean))].sort();
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
      <td><span class="severity ${text(f.severity)}">${text(f.severity)}</span></td>
      <td>${Number(f.risk?.total || 0).toFixed(1)}</td>
      <td>${text(f.category)}</td>
      <td>${text(f.title)}</td>
      <td>${text(f.evidence_status || (f.suppressed ? "suppressed" : "active"))}</td>
    </tr>`).join("");
  document.querySelectorAll("tbody tr").forEach((row) => row.addEventListener("click", () => detail(Number(row.dataset.index))));
  detail(0);
}

function detail(index) {
  const f = findings[index];
  if (!f) return;
  if (report.type === "source") {
    $("detail").innerHTML = sourceDetail(f);
    return;
  }
  $("detail").innerHTML = `
    <h3>${text(f.title)}</h3>
    <p><strong>Rule:</strong> <code>${text(f.rule_id)}</code> <strong>Component:</strong> ${text(f.affected_component)}</p>
    <p><strong>Remediation:</strong> ${text(f.remediation)}</p>
    <h4>Evidence</h4>
    ${renderList((f.evidence || []).map((e) => `${e.source}.${e.key}: ${e.value}`))}
  `;
}

function sourceDetail(f) {
  return `
    <h3>${text(f.title)}</h3>
    <p><strong>CWE:</strong> <code>${text(f.cwe)}</code> <strong>Status:</strong> ${text(f.evidence_status)} <strong>Confidence:</strong> ${Number(f.confidence || 0).toFixed(2)}</p>
    <div class="detail-grid">
      ${locationBlock("Source", f.source_location)}
      ${locationBlock("Sink", f.sink_location)}
    </div>
    <p><strong>Remediation:</strong> ${text(f.remediation)}</p>
    <h4>Evidence</h4>
    ${renderList((f.evidence || []).map((e) => `${e.key}: ${e.value}`))}
  `;
}

function locationBlock(label, location) {
  if (!location) return `<p class="muted">${label}: none</p>`;
  return `<p><strong>${label}</strong><br><code>${text(location.file)}:${text(location.line)}</code><br>${text(location.snippet)}</p>`;
}

function setSteps(stage, status) {
  const order = ["Upload", "Extract", "Scan", "Detect", "Report"];
  const activeIndex = Math.max(0, order.indexOf(stage));
  document.querySelectorAll("#steps li").forEach((item, index) => {
    item.classList.toggle("done", status === "succeeded" || index < activeIndex);
    item.classList.toggle("active", status !== "succeeded" && index === activeIndex);
  });
}

async function submitUpload(event) {
  event.preventDefault();
  const file = $("archive").files[0];
  if (!file) {
    $("workbenchStatus").textContent = "Choose a ZIP file first.";
    return;
  }
  $("runButton").disabled = true;
  $("workbenchStatus").textContent = "Uploading ZIP...";
  setSteps("Upload", "running");
  const form = new FormData();
  form.append("archive", file);
  try {
    activeJob = await fetch("/api/app-scans", { method: "POST", body: form }).then(async (response) => {
      const payload = await response.json();
      if (!response.ok) throw new Error(payload.error || `request failed: ${response.status}`);
      return payload;
    });
    pollJob(activeJob.job_id);
  } catch (error) {
    $("runButton").disabled = false;
    $("workbenchStatus").textContent = error.message || String(error);
    setSteps("Upload", "failed");
  }
}

async function pollJob(jobId) {
  try {
    const job = await getJson(`/api/app-scans/${jobId}`);
    $("workbenchStatus").textContent = job.error || `${job.status}: ${job.current_stage}`;
    setSteps(job.current_stage || "Scan", job.status);
    if (job.status === "succeeded") {
      allFindings = [];
      await renderReport(await getJson(`/api/app-scans/${jobId}/report`), `/api/app-scans/${jobId}/findings`);
      return;
    }
    if (job.status === "failed") {
      $("runButton").disabled = false;
      return;
    }
    setTimeout(() => pollJob(jobId), 1200);
  } catch (error) {
    $("runButton").disabled = false;
    $("workbenchStatus").textContent = error.message || String(error);
  }
}

$("severity").addEventListener("change", () => { findings = []; loadFindings(); });
$("category").addEventListener("change", () => { findings = []; loadFindings(); });
$("uploadForm").addEventListener("submit", submitUpload);
$("archive").addEventListener("change", () => {
  const file = $("archive").files[0];
  $("archiveLabel").textContent = file ? file.name : "Upload a web app ZIP";
});
for (const eventName of ["dragenter", "dragover"]) {
  $("dropzone").addEventListener(eventName, (event) => { event.preventDefault(); $("dropzone").classList.add("dragover"); });
}
for (const eventName of ["dragleave", "drop"]) {
  $("dropzone").addEventListener(eventName, () => $("dropzone").classList.remove("dragover"));
}
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
