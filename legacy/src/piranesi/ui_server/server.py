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
from importlib import resources
from pathlib import Path, PurePosixPath
from typing import Any
from urllib.parse import parse_qs, urlparse

from pydantic import ValidationError

from piranesi.host.api import load_host_report
from piranesi.host.fleet import load_fleet_report
from piranesi.host.models import FleetReport, HostFinding, HostPostureReport
from piranesi.preflight import PreflightMode, build_preflight_report
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
    workbench = WorkbenchState(
        jobs_dir=resolved_jobs_dir,
        max_upload_bytes=max_upload_bytes,
        max_extracted_bytes=500 * 1024 * 1024,
        max_extracted_files=10_000,
        scan_timeout_seconds=scan_timeout_seconds,
        scan_runner=scan_runner or _default_scan_runner,
    )
    _load_job_index(workbench)
    return workbench


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
        if parsed.path == "/api/preflight":
            self._send_json(_preflight_payload(self.server_state))
            return
        if parsed.path == "/api/samples":
            self._send_json(_sample_gallery_payload(self.server_state))
            return
        if parsed.path == "/api/samples/app-vuln-express.zip":
            try:
                self._send_bytes(
                    _sample_app_zip(),
                    content_type="application/zip",
                    filename="piranesi-vuln-express.zip",
                )
            except UiServerError as exc:
                self._send_json({"error": str(exc)}, status=HTTPStatus.NOT_FOUND)
            return
        if parsed.path == "/api/findings":
            self._send_json(_findings_payload(self.server_state, parse_qs(parsed.query)))
            return
        if parsed.path == "/api/handoff/preview":
            try:
                self._send_json(_handoff_preview_payload(self.server_state, parse_qs(parsed.query)))
            except UiServerError as exc:
                self._send_json({"error": str(exc)}, status=HTTPStatus.BAD_REQUEST)
            return
        if parsed.path.startswith("/api/artifacts/"):
            try:
                body, content_type, filename = _state_artifact(
                    self.server_state,
                    parsed.path.rsplit("/", maxsplit=1)[-1],
                )
                self._send_bytes(body, content_type=content_type, filename=filename)
            except UiServerError as exc:
                self._send_json({"error": str(exc)}, status=HTTPStatus.NOT_FOUND)
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
        if parsed.path == "/api/app-scans" or parsed.path.startswith("/api/app-scans/"):
            self._handle_app_scan_get(parsed.path, parse_qs(parsed.query))
            return
        self._send_json({"error": "not found"}, status=HTTPStatus.NOT_FOUND)

    def do_POST(self) -> None:
        parsed = urlparse(self.path)
        if parsed.path == "/api/app-scans/sample/app-vuln-express":
            self._handle_sample_app_scan_post()
            return
        if parsed.path == "/api/app-scans/import-url":
            self._handle_url_import_post()
            return
        if parsed.path == "/api/handoff/send":
            self._handle_handoff_send(parse_qs(parsed.query))
            return
        if parsed.path == "/api/app-scans":
            self._handle_app_scan_post()
            return
        if parsed.path.startswith("/api/app-scans/") and parsed.path.endswith("/handoff/send"):
            self._handle_handoff_send(parse_qs(parsed.query))
            return
        self._send_json({"error": "not found"}, status=HTTPStatus.NOT_FOUND)

    def do_DELETE(self) -> None:
        parsed = urlparse(self.path)
        if parsed.path.startswith("/api/app-scans/"):
            self._handle_app_scan_delete(parsed.path)
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
                jobs = [
                    _job_payload(job)
                    for job in sorted(
                        workbench.jobs.values(),
                        key=lambda item: item.created_at,
                        reverse=True,
                    )
                ]
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
                body, content_type, filename = _job_artifact(job, parts[4])
                self._send_bytes(body, content_type=content_type, filename=filename)
            except UiServerError as exc:
                self._send_json({"error": str(exc)}, status=HTTPStatus.CONFLICT)
            return
        if len(parts) == 5 and parts[3] == "artifacts":
            try:
                body, content_type, filename = _job_artifact(job, parts[4])
                self._send_bytes(body, content_type=content_type, filename=filename)
            except UiServerError as exc:
                self._send_json({"error": str(exc)}, status=HTTPStatus.CONFLICT)
            return
        if len(parts) == 5 and parts[3] == "handoff" and parts[4] == "preview":
            try:
                self._send_json(_job_handoff_preview_payload(job, query))
            except UiServerError as exc:
                self._send_json({"error": str(exc)}, status=HTTPStatus.BAD_REQUEST)
            return
        self._send_json({"error": "not found"}, status=HTTPStatus.NOT_FOUND)

    def _handle_app_scan_delete(self, path: str) -> None:
        workbench = self.server_state.workbench
        if workbench is None:
            self._send_json({"error": "workbench is not enabled"}, status=HTTPStatus.NOT_FOUND)
            return
        parts = path.strip("/").split("/")
        if len(parts) != 3 or parts[0] != "api" or parts[1] != "app-scans":
            self._send_json({"error": "not found"}, status=HTTPStatus.NOT_FOUND)
            return
        with workbench.lock:
            job = workbench.jobs.get(parts[2])
            if job is None:
                self._send_json({"error": "job not found"}, status=HTTPStatus.NOT_FOUND)
                return
            if job.status in {"queued", "running"}:
                self._send_json(
                    {"error": "cannot delete a running scan"},
                    status=HTTPStatus.CONFLICT,
                )
                return
            if not _is_job_dir_safe(workbench, job.job_dir):
                self._send_json({"error": "unsafe job directory"}, status=HTTPStatus.CONFLICT)
                return
            workbench.jobs.pop(job.job_id, None)
            if workbench.active_job_id == job.job_id:
                workbench.active_job_id = None
            _persist_job_index(workbench)
        shutil.rmtree(job.job_dir, ignore_errors=True)
        self._send_json({"deleted": True, "job_id": job.job_id})

    def _handle_app_scan_post(self) -> None:
        workbench = self.server_state.workbench
        if workbench is None:
            self._send_json({"error": "workbench is not enabled"}, status=HTTPStatus.NOT_FOUND)
            return
        content_length = self.headers.get("Content-Length")
        if content_length is None:
            self._send_json(
                {"error": "Content-Length is required"}, status=HTTPStatus.LENGTH_REQUIRED
            )
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
        active_error = _active_scan_error(workbench)
        if active_error is not None:
            self._send_json(active_error, status=HTTPStatus.CONFLICT)
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

        self._enqueue_scan_job(workbench, job)

    def _handle_sample_app_scan_post(self) -> None:
        workbench = self.server_state.workbench
        if workbench is None:
            self._send_json({"error": "workbench is not enabled"}, status=HTTPStatus.NOT_FOUND)
            return
        active_error = _active_scan_error(workbench)
        if active_error is not None:
            self._send_json(active_error, status=HTTPStatus.CONFLICT)
            return
        try:
            job = _create_scan_job(
                workbench,
                ("piranesi-vuln-express.zip", _sample_app_zip()),
                input_kind="sample",
            )
            job.target_name = "Vulnerable Express demo"
        except UiServerError as exc:
            self._send_json({"error": str(exc)}, status=HTTPStatus.BAD_REQUEST)
            return
        self._enqueue_scan_job(workbench, job)

    def _handle_url_import_post(self) -> None:
        workbench = self.server_state.workbench
        if workbench is None:
            self._send_json({"error": "workbench is not enabled"}, status=HTTPStatus.NOT_FOUND)
            return
        active_error = _active_scan_error(workbench)
        if active_error is not None:
            self._send_json(active_error, status=HTTPStatus.CONFLICT)
            return
        content_length = self.headers.get("Content-Length")
        if content_length is None:
            self._send_json(
                {"error": "Content-Length is required"}, status=HTTPStatus.LENGTH_REQUIRED
            )
            return
        try:
            length = int(content_length)
        except ValueError:
            self._send_json({"error": "invalid Content-Length"}, status=HTTPStatus.BAD_REQUEST)
            return
        if length <= 0 or length > 16 * 1024:
            self._send_json({"error": "invalid import body size"}, status=HTTPStatus.BAD_REQUEST)
            return
        try:
            payload = json.loads(self.rfile.read(length).decode("utf-8"))
            raw_url = str(payload.get("url") or "")
            job = _create_github_import_job(workbench, raw_url)
        except (ValueError, UnicodeDecodeError, UiServerError) as exc:
            self._send_json({"error": str(exc)}, status=HTTPStatus.BAD_REQUEST)
            return
        self._enqueue_scan_job(workbench, job)

    def _handle_handoff_send(self, query: dict[str, list[str]]) -> None:
        confirm = (_first_query(query, "confirm") or "").lower() in {"1", "true", "yes"}
        if not confirm:
            self._send_json(
                {
                    "error": "externally visible handoff requires confirm=true",
                    "dry_run_available": True,
                },
                status=HTTPStatus.CONFLICT,
            )
            return
        self._send_json(
            {
                "error": "external handoff sends are intentionally not available from the local UI",
                "use_cli": "piranesi export ... --create/--send --yes",
            },
            status=HTTPStatus.NOT_IMPLEMENTED,
        )

    def _enqueue_scan_job(self, workbench: WorkbenchState, job: LocalScanJob) -> None:
        with workbench.lock:
            active = workbench.active_job_id
            if (
                active is not None
                and (active_job := workbench.jobs.get(active)) is not None
                and active_job.status in {"queued", "running"}
            ):
                shutil.rmtree(job.job_dir, ignore_errors=True)
                self._send_json(
                    {"error": "a scan is already running", "active_job_id": active},
                    status=HTTPStatus.CONFLICT,
                )
                return
            workbench.jobs[job.job_id] = job
            workbench.active_job_id = job.job_id
            _persist_job_index(workbench)
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


def _report_summary(state: UiServerState) -> dict[str, Any]:
    if state.report_type == "workbench":
        return {
            "type": "workbench",
            "title": "Piranesi Local Evidence Workbench",
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
            "artifacts": _artifact_catalog("fleet", "/api/artifacts"),
            "handoff": _handoff_links("/api/handoff"),
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
        "artifacts": _artifact_catalog("host", "/api/artifacts"),
        "handoff": _handoff_links("/api/handoff"),
    }


def _preflight_payload(state: UiServerState) -> dict[str, Any]:
    mode: PreflightMode = "workbench" if state.report_type == "workbench" else "all"
    payload = build_preflight_report(mode=mode).model_dump(mode="json")
    payload["ui"] = {
        "host": "local",
        "report_type": state.report_type,
        "workbench_enabled": state.workbench is not None,
        "max_upload_mb": (
            state.workbench.max_upload_bytes // (1024 * 1024)
            if state.workbench is not None
            else None
        ),
        "scan_timeout_seconds": (
            state.workbench.scan_timeout_seconds if state.workbench is not None else None
        ),
    }
    return payload


def _artifact_catalog(report_type: str, base_path: str) -> dict[str, str | None]:
    common: dict[str, str | None] = {
        "report_json": f"{base_path}/report-json",
        "report_md": f"{base_path}/report-md",
        "sarif": f"{base_path}/sarif",
        "csv": f"{base_path}/csv",
        "pdf": None,
    }
    if report_type == "host":
        common["pdf"] = f"{base_path}/pdf"
    return common


def _handoff_links(base_path: str) -> dict[str, str]:
    return {
        "preview": f"{base_path}/preview",
        "send": f"{base_path}/send",
    }


def _state_artifact(state: UiServerState, kind: str) -> tuple[bytes, str, str]:
    if state.report_type == "workbench" or state.report is None:
        raise UiServerError("no report is loaded")
    if state.report_type == "source":
        report = state.report
        assert isinstance(report, PiranesiReport)
        return _source_artifact(
            report,
            report_path=state.report_path,
            report_dir=state.root,
            kind=kind,
        )
    if state.report_type == "host":
        report = state.report
        assert isinstance(report, HostPostureReport)
        return _host_artifact(
            report, report_path=state.report_path, report_dir=state.root, kind=kind
        )
    report = state.report
    assert isinstance(report, FleetReport)
    return _fleet_artifact(report, report_path=state.report_path, report_dir=state.root, kind=kind)


def _job_artifact(job: LocalScanJob, kind: str) -> tuple[bytes, str, str]:
    return _source_artifact(
        _load_job_report(job),
        report_path=job.report_path or (job.output_dir / "report.json"),
        report_dir=job.output_dir,
        kind=kind,
    )


def _source_artifact(
    report: PiranesiReport,
    *,
    report_path: Path | None,
    report_dir: Path | None,
    kind: str,
) -> tuple[bytes, str, str]:
    if kind == "report-json":
        path = report_path or (report_dir / "report.json" if report_dir is not None else None)
        if path is not None and path.is_file():
            return path.read_bytes(), "application/json; charset=utf-8", "report.json"
        return (
            report.model_dump_json(indent=2).encode("utf-8"),
            "application/json; charset=utf-8",
            "report.json",
        )
    if kind == "report-md":
        path = report_dir / "report.md" if report_dir is not None else None
        if path is None or not path.is_file():
            raise UiServerError("report.md is not available")
        return path.read_bytes(), "text/markdown; charset=utf-8", "report.md"
    if kind == "sarif":
        from piranesi.report.sarif import generate_sarif as generate_source_sarif

        body = json.dumps(generate_source_sarif(report), indent=2).encode("utf-8")
        return body, "application/json; charset=utf-8", "report.sarif.json"
    if kind == "csv":
        from piranesi.report.csv import generate_csv as generate_source_csv

        return (
            generate_source_csv(report).encode("utf-8"),
            "text/csv; charset=utf-8",
            "findings.csv",
        )
    raise UiServerError(f"artifact is not available for source reports: {kind}")


def _host_artifact(
    report: HostPostureReport,
    *,
    report_path: Path | None,
    report_dir: Path | None,
    kind: str,
) -> tuple[bytes, str, str]:
    if kind == "report-json":
        path = report_path or (report_dir / "host-report.json" if report_dir is not None else None)
        if path is not None and path.is_file():
            return path.read_bytes(), "application/json; charset=utf-8", "host-report.json"
        return (
            report.model_dump_json(indent=2).encode("utf-8"),
            "application/json; charset=utf-8",
            "host-report.json",
        )
    if kind == "report-md":
        from piranesi.host.report import render_host_markdown

        path = report_dir / "host-report.md" if report_dir is not None else None
        if path is not None and path.is_file():
            return path.read_bytes(), "text/markdown; charset=utf-8", "host-report.md"
        return (
            render_host_markdown(report).encode("utf-8"),
            "text/markdown; charset=utf-8",
            "host-report.md",
        )
    if kind == "pdf":
        from piranesi.host.report import render_host_pdf

        path = report_dir / "host-report.pdf" if report_dir is not None else None
        if path is not None and path.is_file():
            return path.read_bytes(), "application/pdf", "host-report.pdf"
        return render_host_pdf(report), "application/pdf", "host-report.pdf"
    if kind == "sarif":
        from piranesi.exporters.sarif import generate_sarif as generate_host_sarif

        body = json.dumps(
            generate_host_sarif(report, report_path=report_path),
            indent=2,
        ).encode("utf-8")
        return body, "application/json; charset=utf-8", "host-report.sarif.json"
    if kind == "csv":
        from piranesi.exporters.csv import generate_csv as generate_host_csv

        body = generate_host_csv(report, report_path=report_path).encode("utf-8")
        return body, "text/csv; charset=utf-8", "host-findings.csv"
    raise UiServerError(f"artifact is not available for host reports: {kind}")


def _fleet_artifact(
    report: FleetReport,
    *,
    report_path: Path | None,
    report_dir: Path | None,
    kind: str,
) -> tuple[bytes, str, str]:
    if kind == "report-json":
        path = report_path or (report_dir / "fleet-report.json" if report_dir is not None else None)
        if path is not None and path.is_file():
            return path.read_bytes(), "application/json; charset=utf-8", "fleet-report.json"
        return (
            report.model_dump_json(indent=2).encode("utf-8"),
            "application/json; charset=utf-8",
            "fleet-report.json",
        )
    if kind == "report-md":
        from piranesi.host.report import render_fleet_markdown

        path = report_dir / "fleet-report.md" if report_dir is not None else None
        if path is not None and path.is_file():
            return path.read_bytes(), "text/markdown; charset=utf-8", "fleet-report.md"
        return (
            render_fleet_markdown(report).encode("utf-8"),
            "text/markdown; charset=utf-8",
            "fleet-report.md",
        )
    if kind == "sarif":
        from piranesi.exporters.sarif import generate_sarif as generate_host_sarif

        body = json.dumps(
            generate_host_sarif(report, report_path=report_path),
            indent=2,
        ).encode("utf-8")
        return body, "application/json; charset=utf-8", "fleet-report.sarif.json"
    if kind == "csv":
        from piranesi.exporters.csv import generate_csv as generate_host_csv

        body = generate_host_csv(report, report_path=report_path).encode("utf-8")
        return body, "text/csv; charset=utf-8", "fleet-findings.csv"
    raise UiServerError(f"artifact is not available for fleet reports: {kind}")


def _handoff_preview_payload(
    state: UiServerState,
    query: dict[str, list[str]],
) -> dict[str, Any]:
    if state.report_type == "workbench" or state.report is None:
        raise UiServerError("no report is loaded")
    integration = _handoff_integration(query)
    if state.report_type == "source":
        report = state.report
        assert isinstance(report, PiranesiReport)
        return _source_handoff_preview(report, integration=integration)
    report = state.report
    assert isinstance(report, HostPostureReport | FleetReport)
    return _host_handoff_preview(
        report,
        report_path=state.report_path,
        integration=integration,
        query=query,
    )


def _job_handoff_preview_payload(job: LocalScanJob, query: dict[str, list[str]]) -> dict[str, Any]:
    return _source_handoff_preview(
        _load_job_report(job),
        integration=_handoff_integration(query),
    )


def _handoff_integration(query: dict[str, list[str]]) -> str:
    integration = (_first_query(query, "integration") or "webhook").lower()
    if integration not in {"github", "jira", "webhook", "slack"}:
        raise UiServerError(f"unsupported handoff integration: {integration}")
    return integration


def _host_handoff_preview(
    report: HostPostureReport | FleetReport,
    *,
    report_path: Path | None,
    integration: str,
    query: dict[str, list[str]],
) -> dict[str, Any]:
    from piranesi.exporters.common import iter_export_findings

    findings = iter_export_findings(report, report_path=report_path)
    if integration == "github":
        from piranesi.exporters.github import build_github_issue

        preview: Any = [build_github_issue(finding) for finding in findings[:3]]
    elif integration == "jira":
        from piranesi.exporters.jira import build_jira_issue

        project = _first_query(query, "project") or "SEC"
        preview = [build_jira_issue(finding, project=project) for finding in findings[:3]]
    else:
        from piranesi.exporters.webhook import build_webhook_payload

        preview = build_webhook_payload(report, report_path=report_path, redact=True)
        if integration == "slack":
            preview = {
                "text": "Piranesi dry-run finding handoff",
                "payload": preview,
            }
    return _handoff_response(integration=integration, item_count=len(findings), preview=preview)


def _source_handoff_preview(report: PiranesiReport, *, integration: str) -> dict[str, Any]:
    findings = _source_findings(report)
    compact = [
        {
            "finding_id": finding["id"],
            "title": finding["title"],
            "severity": finding["severity"],
            "risk": finding["risk"],
            "status": finding["evidence_status"],
            "remediation": finding["remediation"],
        }
        for finding in findings[:5]
    ]
    if integration == "github":
        preview: Any = [
            {
                "title": f"[Piranesi] {str(item['severity']).upper()}: {item['title']}",
                "body": (
                    f"Finding ID: `{item['finding_id']}`\n"
                    f"Status: `{item['status']}`\n\n"
                    f"Remediation:\n{item['remediation']}"
                ),
                "labels": ["piranesi", f"severity:{item['severity']}"],
            }
            for item in compact
        ]
    elif integration == "jira":
        preview = [
            {
                "fields": {
                    "project": {"key": "SEC"},
                    "summary": f"[Piranesi] {str(item['severity']).upper()}: {item['title']}",
                    "issuetype": {"name": "Task"},
                    "labels": ["piranesi", f"severity-{item['severity']}"],
                }
            }
            for item in compact
        ]
    else:
        preview = {
            "schema_version": 1,
            "integration": "webhook" if integration == "webhook" else "slack",
            "target": _source_target_label(report.target),
            "summary": report.executive_summary.model_dump(mode="json"),
            "findings": compact,
        }
    return _handoff_response(integration=integration, item_count=len(findings), preview=preview)


def _handoff_response(*, integration: str, item_count: int, preview: Any) -> dict[str, Any]:
    return {
        "integration": integration,
        "dry_run": True,
        "confirmation_required": True,
        "external_send_available": False,
        "item_count": item_count,
        "privacy_warning": (
            "Preview is redacted and local. External sends can expose finding, target, "
            "and remediation metadata and must be confirmed from the CLI."
        ),
        "preview": _json_safe(preview),
    }


def _json_safe(value: Any) -> Any:
    return json.loads(json.dumps(value, default=str))


def _sample_gallery_payload(state: UiServerState) -> dict[str, Any]:
    workbench_enabled = state.workbench is not None
    return {
        "samples": [
            {
                "id": "host-demo",
                "title": "Bundled Debian host posture demo",
                "kind": "host",
                "deterministic": True,
                "requires_optional_tools": [],
                "available_in_workbench": False,
                "command": "piranesi demo --output piranesi-demo-output",
                "expected_artifacts": ["host-report.json", "host-report.md"],
                "docs": "docs/sample-gallery.md#bundled-host-posture-demo",
            },
            {
                "id": "app-vuln-express",
                "title": "Vulnerable Express ZIP demo",
                "kind": "application",
                "deterministic": True,
                "requires_optional_tools": ["joern", "java", "node", "npm"],
                "available_in_workbench": workbench_enabled,
                "download_url": "/api/samples/app-vuln-express.zip" if workbench_enabled else None,
                "expected_artifacts": ["report.json", "report.md"],
                "docs": "docs/sample-gallery.md#vulnerable-express-zip-demo",
            },
            {
                "id": "container-trivy-fixture",
                "title": "Container Trivy image fixture",
                "kind": "container",
                "deterministic": True,
                "requires_optional_tools": [],
                "available_in_workbench": False,
                "fixture_path": "tests/fixtures/container/trivy-image.json",
                "command": (
                    "piranesi container assess --image "
                    "tests/fixtures/container/trivy-image.json --output piranesi-container-output"
                ),
                "expected_artifacts": ["container-report.json", "container-report.md"],
                "docs": "docs/sample-gallery.md#container-and-kubernetes-fixtures",
            },
            {
                "id": "k8s-risky-workload-fixture",
                "title": "Risky Kubernetes workload fixture",
                "kind": "kubernetes",
                "deterministic": True,
                "requires_optional_tools": [],
                "available_in_workbench": False,
                "fixture_path": "tests/fixtures/k8s/risky-workload.yaml",
                "command": "piranesi k8s assess tests/fixtures/k8s --output piranesi-k8s-output",
                "expected_artifacts": ["k8s-report.json", "k8s-report.md"],
                "docs": "docs/sample-gallery.md#container-and-kubernetes-fixtures",
            },
        ]
    }


def _sample_app_zip() -> bytes:
    sample_root = resources.files("piranesi").joinpath("fixtures/app/vuln-express")
    try:
        with resources.as_file(sample_root) as root:
            if not root.is_dir():
                raise UiServerError("bundled app sample is not available")
            buffer = io.BytesIO()
            with zipfile.ZipFile(buffer, mode="w", compression=zipfile.ZIP_DEFLATED) as archive:
                for path in sorted(item for item in root.rglob("*") if item.is_file()):
                    archive.write(path, Path("vuln-express") / path.relative_to(root))
            return buffer.getvalue()
    except OSError as exc:
        raise UiServerError(f"failed to load bundled app sample: {exc}") from exc


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


def _source_report_summary(
    report: PiranesiReport, *, report_dir: Path | None = None
) -> dict[str, Any]:
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
            "report_json": "/api/artifacts/report-json",
            "report_md": "/api/artifacts/report-md" if report_md_available else None,
            "sarif": "/api/artifacts/sarif",
            "csv": "/api/artifacts/csv",
            "pdf": None,
        },
        "handoff": _handoff_links("/api/handoff"),
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
    explanation = getattr(finding, "explanation", None)
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
        "risk_rationale": _source_risk_rationale(finding),
        "confidence_notes": _source_confidence_notes(explanation),
        "verification": _source_verification_payload(explanation),
        "controls": [
            _redact_text(f"{item.framework} {item.section}: {item.obligation_text}")
            for item in (getattr(finding, "regulatory_obligations", []) or [])
        ],
        "verification_method": getattr(finding, "verification_method", None),
        "verified": bool(getattr(finding, "verified", False)),
        "package_name": getattr(finding, "package_name", None),
    }


def _source_risk_rationale(finding: Any) -> list[str]:
    composite = getattr(finding, "composite_risk", None)
    if composite is None:
        return []
    return [
        f"{name}: {component.rationale}"
        for name in (
            "severity",
            "confidence",
            "source_exposure",
            "sink_criticality",
            "ownership_signal",
            "verification_signal",
            "exploitability_signal",
            "reachable_path_signal",
            "suppression_signal",
        )
        if (component := getattr(composite, name, None)) is not None
    ]


def _source_confidence_notes(explanation: Any | None) -> list[str]:
    confidence = getattr(explanation, "confidence", None)
    if confidence is None:
        return []
    return [
        f"{name}: {component.rationale}"
        for name in (
            "static_reachability",
            "source_quality",
            "sink_quality",
            "sanitizer_signal",
            "triage_signal",
            "verification_signal",
            "suppression_signal",
        )
        if (component := getattr(confidence, name, None)) is not None
    ]


def _source_verification_payload(explanation: Any | None) -> dict[str, Any] | None:
    state = getattr(explanation, "verification_state", None)
    if state is None:
        return None
    return {
        "state": state.state,
        "outcome": state.outcome,
        "reason": _redact_text(state.reason or ""),
        "evidence": [_redact_text(item) for item in state.evidence],
        "missing_preconditions": list(state.missing_preconditions),
        "next_steps": list(state.actionable_next_steps),
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
        return (
            "Restrict outbound requests to trusted destinations and block internal network targets."
        )
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
        f"Content-Type: {content_type}\r\nMIME-Version: 1.0\r\n\r\n".encode() + body
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


def _create_scan_job(
    workbench: WorkbenchState,
    upload: tuple[str, bytes],
    *,
    input_kind: str = "zip",
) -> LocalScanJob:
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
        input_kind=input_kind,
    )


def _create_github_import_job(workbench: WorkbenchState, raw_url: str) -> LocalScanJob:
    clone_url, target_name = _github_clone_url(raw_url)
    job_id = uuid.uuid4().hex[:12]
    job_dir = workbench.jobs_dir / job_id
    source_dir = job_dir / "source"
    project_dir = source_dir / "repo"
    output_dir = job_dir / "output"
    log_path = job_dir / "scan.log"
    url_path = job_dir / "source-url.txt"
    job_dir.mkdir(parents=True, exist_ok=False)
    source_dir.mkdir()
    output_dir.mkdir()
    url_path.write_text(clone_url + "\n", encoding="utf-8")
    now = _now_iso()
    return LocalScanJob(
        job_id=job_id,
        target_name=target_name,
        job_dir=job_dir,
        upload_path=url_path,
        extract_dir=source_dir,
        project_dir=project_dir,
        output_dir=output_dir,
        log_path=log_path,
        created_at=now,
        updated_at=now,
        input_kind="github",
        current_stage="Clone",
    )


def _github_clone_url(raw_url: str) -> tuple[str, str]:
    value = raw_url.strip()
    if not value:
        raise UiServerError("URL is required")
    parsed = urlparse(value)
    if parsed.scheme != "https" or parsed.netloc.lower() != "github.com":
        raise UiServerError("only public https://github.com/owner/repo URLs are supported")
    if parsed.username or parsed.password or parsed.params or parsed.query or parsed.fragment:
        raise UiServerError("GitHub import URL must not include credentials, params, or fragments")
    parts = [part for part in parsed.path.split("/") if part]
    if len(parts) != 2:
        raise UiServerError("GitHub import URL must be https://github.com/owner/repo")
    owner, repo = parts
    repo = repo.removesuffix(".git")
    if not owner or not repo or owner.startswith(".") or repo.startswith("."):
        raise UiServerError("GitHub import URL must include owner and repository")
    safe_chars = set("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-")
    if any(set(part) - safe_chars for part in (owner, repo)):
        raise UiServerError(
            "GitHub owner and repository may only use letters, numbers, dot, dash, or underscore"
        )
    return f"https://github.com/{owner}/{repo}.git", f"{owner}/{repo}"


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
                raise UiServerError(f"ZIP contains more than {workbench.max_extracted_files} files")
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
        child for child in extract_dir.iterdir() if child.name not in {"__MACOSX", ".DS_Store"}
    ]
    if len(children) == 1 and children[0].is_dir():
        return children[0]
    return extract_dir


def _target_name(filename: str, project_dir: Path) -> str:
    stem = Path(filename).stem.strip()
    return stem or project_dir.name or "uploaded app"


def _run_scan_job(job: LocalScanJob, workbench: WorkbenchState) -> None:
    _update_job(
        job,
        workbench,
        status="running",
        current_stage="Clone" if job.input_kind == "github" else "Scan",
    )
    try:
        if job.input_kind == "github":
            _clone_github_job(job, workbench)
            _update_job(job, workbench, current_stage="Scan")
        workbench.scan_runner(job, workbench)
        report_path = job.output_dir / "report.json"
        markdown_path = job.output_dir / "report.md"
        if not report_path.is_file():
            raise UiServerError("scan completed without report.json")
        _update_job(
            job,
            workbench,
            status="succeeded",
            current_stage="Report",
            report_path=report_path,
            markdown_path=markdown_path if markdown_path.is_file() else None,
        )
    except subprocess.TimeoutExpired:
        _update_job(
            job,
            workbench,
            status="failed",
            current_stage="Report",
            error=f"scan exceeded {workbench.scan_timeout_seconds}s timeout",
        )
    except Exception as exc:
        _update_job(job, workbench, status="failed", current_stage="Report", error=str(exc))
    finally:
        with workbench.lock:
            if workbench.active_job_id == job.job_id:
                workbench.active_job_id = None
            _persist_job_index(workbench)


def _clone_github_job(job: LocalScanJob, workbench: WorkbenchState) -> None:
    clone_url = job.upload_path.read_text(encoding="utf-8").strip()
    if job.project_dir.exists():
        shutil.rmtree(job.project_dir)
    command = [
        "git",
        "clone",
        "--depth",
        "1",
        "--single-branch",
        "--no-tags",
        clone_url,
        str(job.project_dir),
    ]
    result = subprocess.run(
        command,
        cwd=job.job_dir,
        text=True,
        capture_output=True,
        timeout=min(120, max(10, workbench.scan_timeout_seconds)),
        check=False,
    )
    job.log_path.write_text(
        "\n".join(
            [
                "$ " + " ".join([*command[:-2], "[redacted-github-url]", str(job.project_dir)]),
                "",
                "[stdout]",
                result.stdout,
                "",
                "[stderr]",
                _redact_text(result.stderr),
            ]
        ),
        encoding="utf-8",
    )
    if result.returncode != 0:
        stderr_tail = "\n".join(_redact_text(result.stderr).strip().splitlines()[-8:])
        raise UiServerError(stderr_tail or f"git clone exited with code {result.returncode}")
    shutil.rmtree(job.project_dir / ".git", ignore_errors=True)
    _validate_imported_tree(job.project_dir, workbench)


def _validate_imported_tree(project_dir: Path, workbench: WorkbenchState) -> None:
    total_size = 0
    file_count = 0
    for path in project_dir.rglob("*"):
        if path.is_dir():
            continue
        file_count += 1
        if file_count > workbench.max_extracted_files:
            raise UiServerError(
                f"repository contains more than {workbench.max_extracted_files} files"
            )
        try:
            total_size += path.stat().st_size
        except OSError as exc:
            raise UiServerError(f"failed to inspect imported repository: {exc}") from exc
        if total_size > workbench.max_extracted_bytes:
            limit_mb = workbench.max_extracted_bytes // (1024 * 1024)
            raise UiServerError(f"repository is larger than {limit_mb} MB")


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


def _update_job(job: LocalScanJob, workbench: WorkbenchState | None = None, **updates: Any) -> None:
    for key, value in updates.items():
        setattr(job, key, value)
    job.updated_at = _now_iso()
    if workbench is not None:
        _persist_job_index(workbench)


def _active_scan_error(workbench: WorkbenchState) -> dict[str, Any] | None:
    with workbench.lock:
        active = workbench.active_job_id
        if active is None:
            return None
        active_job = workbench.jobs.get(active)
        if active_job is None or active_job.status not in {"queued", "running"}:
            return None
        return {"error": "a scan is already running", "active_job_id": active}


def _job_index_path(workbench: WorkbenchState) -> Path:
    return workbench.jobs_dir / "jobs-index.json"


def _persist_job_index(workbench: WorkbenchState) -> None:
    index_path = _job_index_path(workbench)
    payload = {
        "schema_version": 1,
        "updated_at": _now_iso(),
        "jobs": [
            _job_index_record(job, workbench.jobs_dir)
            for job in sorted(workbench.jobs.values(), key=lambda item: item.created_at)
        ],
    }
    tmp_path = index_path.with_suffix(".json.tmp")
    tmp_path.write_text(json.dumps(payload, indent=2, sort_keys=True), encoding="utf-8")
    tmp_path.replace(index_path)


def _load_job_index(workbench: WorkbenchState) -> None:
    index_path = _job_index_path(workbench)
    if not index_path.is_file():
        return
    try:
        payload = json.loads(index_path.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return
    jobs: dict[str, LocalScanJob] = {}
    for item in payload.get("jobs", []):
        if not isinstance(item, dict):
            continue
        job = _job_from_index_record(item, workbench.jobs_dir)
        if job is None:
            continue
        if job.status in {"queued", "running"}:
            job.status = "failed"
            job.current_stage = "Report"
            job.error = "workbench restarted before scan completed"
            job.updated_at = _now_iso()
        jobs[job.job_id] = job
    workbench.jobs = jobs
    workbench.active_job_id = None
    if jobs:
        _persist_job_index(workbench)


def _job_index_record(job: LocalScanJob, jobs_dir: Path) -> dict[str, Any]:
    return {
        "job_id": job.job_id,
        "target_name": job.target_name,
        "job_dir": _relative_job_path(job.job_dir, jobs_dir),
        "upload_path": _relative_job_path(job.upload_path, jobs_dir),
        "extract_dir": _relative_job_path(job.extract_dir, jobs_dir),
        "project_dir": _relative_job_path(job.project_dir, jobs_dir),
        "output_dir": _relative_job_path(job.output_dir, jobs_dir),
        "log_path": _relative_job_path(job.log_path, jobs_dir),
        "created_at": job.created_at,
        "updated_at": job.updated_at,
        "input_kind": job.input_kind,
        "status": job.status,
        "current_stage": job.current_stage,
        "report_path": _relative_job_path(job.report_path, jobs_dir)
        if job.report_path is not None
        else None,
        "markdown_path": _relative_job_path(job.markdown_path, jobs_dir)
        if job.markdown_path is not None
        else None,
        "error": job.error,
        "return_code": job.return_code,
    }


def _job_from_index_record(item: dict[str, Any], jobs_dir: Path) -> LocalScanJob | None:
    job_id = str(item.get("job_id") or "")
    if not job_id:
        return None
    try:
        job_dir = _resolve_job_index_path(jobs_dir, str(item["job_dir"]))
        upload_path = _resolve_job_index_path(jobs_dir, str(item["upload_path"]))
        extract_dir = _resolve_job_index_path(jobs_dir, str(item["extract_dir"]))
        project_dir = _resolve_job_index_path(jobs_dir, str(item["project_dir"]))
        output_dir = _resolve_job_index_path(jobs_dir, str(item["output_dir"]))
        log_path = _resolve_job_index_path(jobs_dir, str(item["log_path"]))
    except (KeyError, UiServerError):
        return None
    if not job_dir.exists():
        return None
    report_path = _optional_index_path(jobs_dir, item.get("report_path"))
    markdown_path = _optional_index_path(jobs_dir, item.get("markdown_path"))
    return LocalScanJob(
        job_id=job_id,
        target_name=str(item.get("target_name") or "local scan"),
        job_dir=job_dir,
        upload_path=upload_path,
        extract_dir=extract_dir,
        project_dir=project_dir,
        output_dir=output_dir,
        log_path=log_path,
        created_at=str(item.get("created_at") or _now_iso()),
        updated_at=str(item.get("updated_at") or _now_iso()),
        input_kind=str(item.get("input_kind") or "zip"),
        status=str(item.get("status") or "failed"),
        current_stage=str(item.get("current_stage") or "Report"),
        report_path=report_path if report_path is not None and report_path.is_file() else None,
        markdown_path=markdown_path
        if markdown_path is not None and markdown_path.is_file()
        else None,
        error=str(item["error"]) if item.get("error") else None,
        return_code=int(item["return_code"]) if item.get("return_code") is not None else None,
    )


def _optional_index_path(jobs_dir: Path, value: Any) -> Path | None:
    if not value:
        return None
    try:
        return _resolve_job_index_path(jobs_dir, str(value))
    except UiServerError:
        return None


def _relative_job_path(path: Path, jobs_dir: Path) -> str:
    try:
        return str(path.relative_to(jobs_dir))
    except ValueError:
        return str(path)


def _resolve_job_index_path(jobs_dir: Path, value: str) -> Path:
    if not value:
        raise UiServerError("empty job index path")
    relative = Path(value)
    if relative.is_absolute() or any(part == ".." for part in relative.parts):
        raise UiServerError(f"unsafe job index path: {value}")
    resolved = (jobs_dir / relative).resolve(strict=False)
    try:
        resolved.relative_to(jobs_dir.resolve(strict=False))
    except ValueError as exc:
        raise UiServerError(f"unsafe job index path: {value}") from exc
    return resolved


def _is_job_dir_safe(workbench: WorkbenchState, job_dir: Path) -> bool:
    try:
        job_dir.resolve(strict=False).relative_to(workbench.jobs_dir.resolve(strict=False))
    except ValueError:
        return False
    return job_dir != workbench.jobs_dir


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
        "report_json": f"/api/app-scans/{job.job_id}/artifacts/report-json",
        "report_md": f"/api/app-scans/{job.job_id}/artifacts/report-md"
        if job.markdown_path is not None
        else None,
        "sarif": f"/api/app-scans/{job.job_id}/artifacts/sarif",
        "csv": f"/api/app-scans/{job.job_id}/artifacts/csv",
        "pdf": None,
    }
    payload["handoff"] = _handoff_links(f"/api/app-scans/{job.job_id}/handoff")
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
      <p id="eyebrow">Piranesi local evidence</p>
      <h1 id="title">Review Workbench</h1>
      <span id="subtitle"></span>
    </div>
    <strong id="score"></strong>
  </header>
  <main>
    <section id="workbench" class="workbench" hidden>
      <div class="prompt-card">
        <h2>Review a local target</h2>
        <div class="intake-grid" aria-label="Intake modes">
          <div class="mode-card active"><strong>App ZIP</strong><span>Ready</span></div>
          <div class="mode-card"><strong>Existing report</strong><span>Open from CLI</span></div>
          <div class="mode-card"><strong>Host evidence</strong><span>Use collect or assess</span></div>
          <div class="mode-card disabled"><strong>Container</strong><span>CLI fixture path</span></div>
          <div class="mode-card disabled"><strong>Kubernetes</strong><span>CLI fixture path</span></div>
        </div>
        <div class="sample-actions">
          <button id="sampleDemoButton" type="button">Run bundled ZIP demo</button>
        </div>
        <form id="uploadForm">
          <label id="dropzone" class="dropzone" for="archive">
            <input id="archive" name="archive" type="file" accept=".zip,application/zip">
            <span id="archiveLabel">Upload a web app ZIP</span>
            <small>Runs locally. Evidence stays on this machine.</small>
          </label>
          <div class="prompt-row">
            <input id="urlInput" placeholder="https://github.com/owner/repo" autocomplete="off">
            <button id="importButton" type="button">Import repo</button>
            <button id="runButton" type="submit">Run review</button>
          </div>
        </form>
        <div id="recentScans" class="recent-scans" aria-label="Recent scans"></div>
        <ol id="steps" class="steps">
          <li data-step="Upload">Upload</li>
          <li data-step="Extract">Extract</li>
          <li data-step="Scan">Scan</li>
          <li data-step="Detect">Detect</li>
          <li data-step="Report">Report</li>
        </ol>
        <p id="workbenchStatus" class="muted"></p>
        <div id="sampleGallery" class="sample-gallery" aria-label="Sample gallery"></div>
        <div id="preflightPanel" class="readiness" aria-label="Workbench readiness"></div>
        <div class="privacy-summary" aria-label="Privacy and data handling summary">
          <strong>Privacy defaults</strong>
          <ul>
            <li>Runs on loopback and keeps reports, uploads, and extracted source local.</li>
            <li>Stores workbench jobs under <code>~/.piranesi/ui-jobs</code> unless <code>--jobs-dir</code> is set.</li>
            <li>Strips API-key-like environment variables before workbench ZIP scans.</li>
          </ul>
        </div>
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
        <section id="handoffPanel" class="handoff-panel"></section>
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
.intake-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(120px,1fr)); gap:8px; margin-bottom:12px; }
.mode-card { border:1px solid var(--border); border-radius:6px; padding:10px; background:white; min-height:70px; }
.mode-card strong,.mode-card span { display:block; }
.mode-card span { color:var(--muted); margin-top:4px; }
.mode-card.active { border-color:var(--accent); background:#f0fdfa; }
.mode-card.disabled { background:#f8fafc; color:#64748b; }
.sample-actions { display:flex; justify-content:center; margin-bottom:12px; }
.dropzone { display:flex; flex-direction:column; justify-content:center; min-height:130px; border:1px solid var(--border); border-radius:8px; padding:20px; cursor:pointer; background:#fbfcfe; }
.dropzone input { display:none; }
.dropzone span { font-size:18px; }
.dropzone small { color:var(--muted); margin-top:7px; }
.dropzone.dragover { border-color:var(--accent); background:#f0fdfa; }
.prompt-row { display:grid; grid-template-columns:1fr auto auto; gap:10px; margin-top:12px; }
input,select,button { font:inherit; }
input,select { padding:8px 10px; border:1px solid var(--border); border-radius:6px; background:white; min-width:0; }
input:disabled { color:var(--muted); background:#f8fafc; }
button { border:1px solid #111827; border-radius:6px; background:#111827; color:white; padding:8px 13px; cursor:pointer; }
button:disabled { opacity:.55; cursor:not-allowed; }
.steps { display:grid; grid-template-columns:repeat(5,1fr); gap:8px; list-style:none; padding:0; margin:18px 0 0; }
.steps li { border:1px solid var(--border); border-radius:6px; padding:8px; color:var(--muted); text-align:center; }
.steps li.active { border-color:var(--accent); color:var(--accent); background:#f0fdfa; }
.steps li.done { color:#14532d; background:#f0fdf4; }
.privacy-summary { margin-top:16px; padding-top:14px; border-top:1px solid var(--border); color:var(--muted); }
.privacy-summary strong { display:block; color:var(--text); margin-bottom:6px; }
.privacy-summary ul { margin:0; padding-left:18px; }
.readiness { margin-top:14px; border:1px solid var(--border); border-radius:8px; padding:12px; background:#f8fafc; }
.readiness h3 { margin-bottom:8px; }
.readiness-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(180px,1fr)); gap:8px; }
.check { border:1px solid var(--border); border-radius:6px; padding:8px; background:white; }
.check strong { display:block; }
.check.ok { border-color:#bbf7d0; } .check.missing { border-color:#fed7aa; } .check.error { border-color:#fecaca; }
.sample-gallery { margin-top:14px; border:1px solid var(--border); border-radius:8px; padding:12px; background:white; }
.sample-gallery h3 { margin-bottom:8px; }
.sample-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(220px,1fr)); gap:8px; }
.sample { border:1px solid var(--border); border-radius:6px; padding:10px; background:#fbfcfe; }
.sample strong { display:block; }
.sample a { color:var(--accent); font-weight:600; }
.recent-scans { margin-top:14px; border:1px solid var(--border); border-radius:8px; padding:12px; background:#fbfcfe; }
.recent-scans h3 { margin-bottom:8px; }
.job-row { display:grid; grid-template-columns:1fr auto auto; gap:8px; align-items:center; padding:8px 0; border-top:1px solid var(--border); }
.job-row:first-of-type { border-top:0; }
.job-row button { padding:6px 9px; }
table { width:100%; border-collapse:collapse; }
th,td { padding:9px 7px; border-bottom:1px solid var(--border); text-align:left; vertical-align:top; }
tbody tr { cursor:pointer; }
tbody tr:hover { background:#f1f5f9; }
.severity { font-weight:700; text-transform:capitalize; }
.critical,.high { color:#b91c1c; } .medium { color:#b45309; } .low { color:#1d4ed8; } .informational { color:#475569; }
.detail { margin-top:12px; border-left:3px solid var(--accent); background:#f8fafc; padding:12px; }
.detail-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(240px,1fr)); gap:10px; }
.detail-section { margin-top:12px; }
.handoff { white-space:pre-wrap; background:white; border:1px solid var(--border); border-radius:6px; padding:10px; overflow:auto; }
.handoff-panel { margin-top:12px; border-top:1px solid var(--border); padding-top:12px; }
.handoff-actions { display:flex; flex-wrap:wrap; gap:8px; margin:8px 0; }
.handoff-preview { max-height:260px; white-space:pre-wrap; background:#0f172a; color:#e2e8f0; border-radius:6px; padding:10px; overflow:auto; }
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
  $("eyebrow").textContent = "Piranesi local evidence";
  $("title").textContent = "Start a local evidence review";
  $("subtitle").textContent = "Upload a web app ZIP or open an existing host, fleet, or source report from the CLI.";
  $("score").textContent = "";
  loadSamples();
  loadJobs();
  loadPreflight();
}

async function loadSamples() {
  try {
    const payload = await getJson("/api/samples");
    renderSamples(payload.samples || []);
  } catch (error) {
    $("sampleGallery").innerHTML = `<p class="muted">Samples unavailable: ${text(error.message || error)}</p>`;
  }
}

function renderSamples(samples) {
  const appSample = samples.find((sample) => sample.id === "app-vuln-express");
  const hostSample = samples.find((sample) => sample.id === "host-demo");
  const sampleCards = [appSample, hostSample].filter(Boolean);
  $("sampleGallery").innerHTML = `
    <h3>Sample gallery</h3>
    <div class="sample-grid">
      ${sampleCards.map((sample) => `
        <div class="sample">
          <strong>${text(sample.title)}</strong>
          <p class="muted">${text(sample.kind)} · ${sample.deterministic ? "deterministic" : "tool-dependent"}</p>
          ${sample.download_url ? `<a href="${text(sample.download_url)}">Download ZIP</a>` : `<code>${text(sample.command)}</code>`}
        </div>
      `).join("")}
    </div>
  `;
}

async function loadJobs() {
  try {
    const payload = await getJson("/api/app-scans");
    renderJobs(payload.jobs || []);
  } catch (error) {
    $("recentScans").innerHTML = `<p class="muted">Scan history unavailable: ${text(error.message || error)}</p>`;
  }
}

function renderJobs(jobs) {
  if (!jobs.length) {
    $("recentScans").innerHTML = '<h3>Recent scans</h3><p class="muted">No local scans yet.</p>';
    return;
  }
  $("recentScans").innerHTML = `
    <h3>Recent scans</h3>
    ${jobs.map((job) => `
      <div class="job-row">
        <div><strong>${text(job.target_name)}</strong><br><span class="muted">${text(job.status)} · ${text(job.input_kind)} · ${text(job.updated_at)}</span></div>
        <button type="button" data-open-job="${text(job.job_id)}" ${job.artifacts?.report ? "" : "disabled"}>Open</button>
        <button type="button" data-delete-job="${text(job.job_id)}" ${["queued","running"].includes(job.status) ? "disabled" : ""}>Delete</button>
      </div>
    `).join("")}
  `;
  document.querySelectorAll("[data-open-job]").forEach((button) => button.addEventListener("click", () => openJob(button.dataset.openJob)));
  document.querySelectorAll("[data-delete-job]").forEach((button) => button.addEventListener("click", () => deleteJob(button.dataset.deleteJob)));
}

async function openJob(jobId) {
  if (!jobId) return;
  allFindings = [];
  await renderReport(await getJson(`/api/app-scans/${jobId}/report`), `/api/app-scans/${jobId}/findings`);
}

async function deleteJob(jobId) {
  if (!jobId) return;
  const response = await fetch(`/api/app-scans/${jobId}`, { method: "DELETE" });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) {
    $("workbenchStatus").textContent = payload.error || `delete failed: ${response.status}`;
    return;
  }
  await loadJobs();
}

async function loadPreflight() {
  try {
    const preflight = await getJson("/api/preflight");
    renderPreflight(preflight);
  } catch (error) {
    $("preflightPanel").innerHTML = `<p class="muted">Preflight unavailable: ${text(error.message || error)}</p>`;
  }
}

function renderPreflight(preflight) {
  const checks = preflight.checks || [];
  const requiredMissing = checks.filter((check) => check.required && check.status !== "ok");
  const optionalMissing = checks.filter((check) => !check.required && check.status === "missing");
  $("preflightPanel").innerHTML = `
    <h3>Readiness</h3>
    <p class="muted">${requiredMissing.length ? `${requiredMissing.length} required check(s) need attention.` : "Required local checks are ready."} ${optionalMissing.length} optional tool(s) are missing.</p>
    <div class="readiness-grid">
      ${checks.map((check) => `
        <div class="check ${text(check.status)}">
          <strong>${text(check.label)} ${check.required ? "(required)" : "(optional)"}</strong>
          <span>${text(check.status)}${check.version ? ` · ${text(check.version)}` : ""}</span>
          ${check.status === "ok" ? "" : `<small class="muted">${text(check.install_hint)}</small>`}
        </div>
      `).join("")}
    </div>
  `;
}

async function renderReport(nextReport, nextFindingsEndpoint) {
  report = nextReport;
  findingsEndpoint = nextFindingsEndpoint;
  $("workbench").hidden = true;
  $("reportView").hidden = false;
  $("fleetPanel").hidden = true;
  $("eyebrow").textContent = "Piranesi local evidence";
  $("title").textContent = report.type === "fleet" ? "Fleet Review" : report.type === "source" ? "Application Review" : "Host Review";
  $("subtitle").textContent = report.type === "source" ? "Evidence-bound application report generated locally." : "";
  $("score").textContent = report.posture_score !== undefined ? `${report.posture_score}/100` : report.highest_risk?.score ? `${Number(report.highest_risk.score).toFixed(1)} risk` : "";
  renderOverview();
  renderHandoffActions();
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
  const labels = { report_json: "JSON", report_md: "Markdown", sarif: "SARIF", csv: "CSV", pdf: "PDF" };
  const links = Object.entries(report.artifacts || {})
    .map(([key, href]) => href ? `<a class="artifact-link" href="${text(href)}" target="_blank" rel="noreferrer">${text(labels[key] || key)}</a>` : `<span class="muted artifact-link">${text(labels[key] || key)} unavailable</span>`);
  return links.length ? links.join("") : '<p class="muted">No downloadable artifacts recorded.</p>';
}

function renderHandoffActions() {
  const previewEndpoint = report.handoff?.preview;
  if (!previewEndpoint) {
    $("handoffPanel").innerHTML = "";
    return;
  }
  $("handoffPanel").innerHTML = `
    <h3>Artifacts and handoff</h3>
    <div>${artifactLinks()}</div>
    <div class="handoff-actions">
      ${["github","jira","slack","webhook"].map((integration) => `<button type="button" data-preview="${integration}">${integration}</button>`).join("")}
    </div>
    <p class="muted">Dry-run previews only. External sends require explicit CLI confirmation.</p>
    <pre id="handoffPreview" class="handoff-preview" hidden></pre>
  `;
  document.querySelectorAll("[data-preview]").forEach((button) => button.addEventListener("click", () => loadHandoffPreview(button.dataset.preview)));
}

async function loadHandoffPreview(integration) {
  if (!integration || !report.handoff?.preview) return;
  const payload = await getJson(`${report.handoff.preview}?integration=${encodeURIComponent(integration)}`);
  $("handoffPreview").hidden = false;
  $("handoffPreview").textContent = JSON.stringify(payload, null, 2);
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
  const riskRationale = [
    ...(f.risk?.rationale || []),
    ...(f.rationale ? [f.rationale] : []),
  ];
  $("detail").innerHTML = `
    <h3>${text(f.title)}</h3>
    <p><strong>Rule:</strong> <code>${text(f.rule_id)}</code> <strong>Component:</strong> ${text(f.affected_component)}</p>
    <p><strong>Status:</strong> ${text(f.suppressed ? "suppressed" : f.evidence_status || "active")} <strong>Confidence:</strong> ${Number(f.confidence || 0).toFixed(2)} <strong>Risk:</strong> ${Number(f.risk?.total || 0).toFixed(1)}</p>
    <div class="detail-section"><h4>Remediation</h4><p>${text(f.remediation)}</p></div>
    <div class="detail-section"><h4>Risk Rationale</h4>${renderList(riskRationale)}</div>
    <div class="detail-section"><h4>Related Controls</h4>${renderList((f.structured_control_refs || f.control_refs || []).map((control) => typeof control === "string" ? control : `${control.framework} ${control.control_id}: ${control.title}`))}</div>
    <div class="detail-section"><h4>Evidence</h4>
    ${renderList((f.evidence || []).map((e) => `${e.source}.${e.key}: ${e.value}`))}
    </div>
    ${handoffBlock(f)}
  `;
}

function sourceDetail(f) {
  return `
    <h3>${text(f.title)}</h3>
    <p><strong>CWE:</strong> <code>${text(f.cwe)}</code> <strong>Status:</strong> ${text(f.evidence_status)} <strong>Confidence:</strong> ${Number(f.confidence || 0).toFixed(2)}</p>
    <p><strong>Risk:</strong> ${Number(f.risk?.total || 0).toFixed(1)} ${text(f.risk?.band || "")}</p>
    <div class="detail-grid">
      ${locationBlock("Source", f.source_location)}
      ${locationBlock("Sink", f.sink_location)}
    </div>
    <div class="detail-section"><h4>Remediation</h4><p>${text(f.remediation)}</p></div>
    <div class="detail-section"><h4>Risk Rationale</h4>${renderList(f.risk_rationale || [])}</div>
    <div class="detail-section"><h4>Confidence Notes</h4>${renderList(f.confidence_notes || [])}</div>
    <div class="detail-section"><h4>Related Controls</h4>${renderList(f.controls || [])}</div>
    <div class="detail-section"><h4>Evidence</h4>
    ${renderList((f.evidence || []).map((e) => `${e.key}: ${e.value}`))}
    </div>
    <div class="detail-section"><h4>Path</h4>${renderList((f.taint_path || []).map((step) => `${step.operation || "flow"} at ${step.location?.file || "unknown"}:${step.location?.line || 0}`))}</div>
    ${handoffBlock(f)}
  `;
}

function handoffBlock(f) {
  const lines = [
    `${f.title}`,
    `status: ${f.evidence_status || (f.suppressed ? "suppressed" : "active")}`,
    `severity: ${f.severity}`,
    `risk: ${Number(f.risk?.total || 0).toFixed(1)}`,
    `remediation: ${f.remediation || "none"}`,
  ];
  return `<div class="detail-section"><h4>Analyst Handoff</h4><pre class="handoff">${text(lines.join("\\n"))}</pre></div>`;
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

async function startSampleDemo() {
  $("runButton").disabled = true;
  $("sampleDemoButton").disabled = true;
  $("workbenchStatus").textContent = "Starting bundled demo...";
  setSteps("Upload", "running");
  try {
    activeJob = await fetch("/api/app-scans/sample/app-vuln-express", { method: "POST" }).then(async (response) => {
      const payload = await response.json();
      if (!response.ok) throw new Error(payload.error || `request failed: ${response.status}`);
      return payload;
    });
    pollJob(activeJob.job_id);
  } catch (error) {
    $("runButton").disabled = false;
    $("sampleDemoButton").disabled = false;
    $("workbenchStatus").textContent = error.message || String(error);
    setSteps("Upload", "failed");
  }
}

async function importUrl() {
  const url = $("urlInput").value.trim();
  if (!url) {
    $("workbenchStatus").textContent = "Enter a public GitHub repository URL.";
    return;
  }
  $("runButton").disabled = true;
  $("importButton").disabled = true;
  $("sampleDemoButton").disabled = true;
  $("workbenchStatus").textContent = "Importing repository...";
  setSteps("Extract", "running");
  try {
    activeJob = await fetch("/api/app-scans/import-url", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url }),
    }).then(async (response) => {
      const payload = await response.json();
      if (!response.ok) throw new Error(payload.error || `request failed: ${response.status}`);
      return payload;
    });
    pollJob(activeJob.job_id);
  } catch (error) {
    $("runButton").disabled = false;
    $("importButton").disabled = false;
    $("sampleDemoButton").disabled = false;
    $("workbenchStatus").textContent = error.message || String(error);
    setSteps("Upload", "failed");
  }
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
      $("importButton").disabled = false;
      $("sampleDemoButton").disabled = false;
      loadJobs();
      return;
    }
    setTimeout(() => pollJob(jobId), 1200);
  } catch (error) {
    $("runButton").disabled = false;
    $("importButton").disabled = false;
    $("sampleDemoButton").disabled = false;
    $("workbenchStatus").textContent = error.message || String(error);
  }
}

$("severity").addEventListener("change", () => { findings = []; loadFindings(); });
$("category").addEventListener("change", () => { findings = []; loadFindings(); });
$("uploadForm").addEventListener("submit", submitUpload);
$("sampleDemoButton").addEventListener("click", startSampleDemo);
$("importButton").addEventListener("click", importUrl);
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
