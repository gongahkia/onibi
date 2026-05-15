from __future__ import annotations

import http.client
import io
import json
import subprocess
import threading
import zipfile
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

import pytest

import piranesi.ui_server.server as ui_server_module
from piranesi.report.renderer import build_report, write_report_outputs
from piranesi.ui_server import (
    UiServerError,
    UiServerOptions,
    create_ui_server,
    load_report_state,
    run_ui_server,
)
from tests._pipeline_fixtures import fixture_artifacts

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


def _post(
    url: str,
    body: bytes,
    headers: dict[str, str],
    *,
    expected_status: int = 202,
) -> dict[str, object]:
    parsed = urlparse(url)
    assert parsed.scheme == "http"
    assert parsed.hostname is not None
    connection = http.client.HTTPConnection(parsed.hostname, parsed.port, timeout=5)
    try:
        connection.request("POST", parsed.path, body=body, headers=headers)
        response = connection.getresponse()
        payload = json.loads(response.read().decode("utf-8"))
        assert response.status == expected_status, payload
        return payload
    finally:
        connection.close()


def _zip_bytes(files: dict[str, str]) -> bytes:
    buffer = io.BytesIO()
    with zipfile.ZipFile(buffer, mode="w") as archive:
        for name, body in files.items():
            archive.writestr(name, body)
    return buffer.getvalue()


def _multipart_zip(filename: str, payload: bytes) -> tuple[bytes, dict[str, str]]:
    boundary = "----piranesi-test-boundary"
    body = b"".join(
        [
            f"--{boundary}\r\n".encode(),
            (f'Content-Disposition: form-data; name="archive"; filename="{filename}"\r\n').encode(),
            b"Content-Type: application/zip\r\n\r\n",
            payload,
            f"\r\n--{boundary}--\r\n".encode(),
        ]
    )
    return body, {
        "Content-Type": f"multipart/form-data; boundary={boundary}",
        "Content-Length": str(len(body)),
    }


def _write_source_report(output_dir: Path, target_dir: Path) -> None:
    artifacts = fixture_artifacts(target_dir)
    report = build_report(
        scan_result=artifacts["scan"],  # type: ignore[arg-type]
        detected_findings=artifacts["detect"].findings,  # type: ignore[attr-defined]
        confirmed_findings=artifacts["verify"].findings,  # type: ignore[attr-defined]
        legal_assessments=artifacts["legal"].assessments,  # type: ignore[attr-defined]
        patch_results=artifacts["patch"].patches,  # type: ignore[attr-defined]
        target_dir=target_dir,
        total_llm_cost_usd=0.0,
        duration_s=1.0,
        stage_timings_s={"scan": 1.0, "report": 0.1},
    )
    write_report_outputs(report, output_dir)


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


def test_server_loads_source_report(tmp_path: Path) -> None:
    target_dir = tmp_path / "app"
    target_dir.mkdir()
    _write_source_report(tmp_path, target_dir)

    server = run_ui_server(
        UiServerOptions(report_path=tmp_path, port=0),
        block=False,
    )
    try:
        url = f"http://{server.server_address[0]}:{server.server_address[1]}"

        summary = _get_json(f"{url}/api/report")
        findings = _get_json(f"{url}/api/findings")
        markdown = _get_text(f"{url}/api/artifacts/report-md")

        assert summary["type"] == "source"
        assert summary["artifacts"]["report_md"] == "/api/artifacts/report-md"
        assert summary["files_scanned"] == 1
        assert findings["findings"][0]["title"] == "SQL Injection"
        assert findings["findings"][0]["evidence_status"] == "confirmed"
        assert "Piranesi Security Analysis Report" in markdown
    finally:
        server.shutdown()
        server.server_close()


def test_workbench_loads_without_report_path(tmp_path: Path) -> None:
    server = run_ui_server(
        UiServerOptions(workbench=True, jobs_dir=tmp_path / "jobs", port=0),
        block=False,
    )
    try:
        url = f"http://{server.server_address[0]}:{server.server_address[1]}"

        text = _get_text(url)
        assert "Upload a web app ZIP" in text
        assert "Privacy defaults" in text
        summary = _get_json(f"{url}/api/report")

        assert summary["type"] == "workbench"
        assert summary["title"] == "Piranesi Local Evidence Workbench"
    finally:
        server.shutdown()
        server.server_close()


def test_workbench_rejects_non_zip_upload(tmp_path: Path) -> None:
    server = run_ui_server(
        UiServerOptions(workbench=True, jobs_dir=tmp_path / "jobs", port=0),
        block=False,
    )
    try:
        url = f"http://{server.server_address[0]}:{server.server_address[1]}"
        body, headers = _multipart_zip("app.txt", b"not a zip")

        payload = _post(f"{url}/api/app-scans", body, headers, expected_status=400)

        assert "must use a .zip" in str(payload["error"])
    finally:
        server.shutdown()
        server.server_close()


def test_workbench_rejects_zip_slip(tmp_path: Path) -> None:
    server = run_ui_server(
        UiServerOptions(workbench=True, jobs_dir=tmp_path / "jobs", port=0),
        block=False,
    )
    try:
        url = f"http://{server.server_address[0]}:{server.server_address[1]}"
        body, headers = _multipart_zip("app.zip", _zip_bytes({"../escape.txt": "x"}))

        payload = _post(f"{url}/api/app-scans", body, headers, expected_status=400)

        assert "unsafe ZIP member" in str(payload["error"])
    finally:
        server.shutdown()
        server.server_close()


def test_workbench_runs_mocked_scan_job(tmp_path: Path) -> None:
    def fake_runner(job: Any, _workbench: Any) -> None:
        output_dir = job.output_dir
        project_dir = job.project_dir
        _write_source_report(output_dir, project_dir)

    server = create_ui_server(
        workbench=True,
        jobs_dir=tmp_path / "jobs",
        port=0,
        scan_runner=fake_runner,
    )
    thread = None
    try:
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        url = f"http://{server.server_address[0]}:{server.server_address[1]}"
        body, headers = _multipart_zip(
            "app.zip",
            _zip_bytes({"app/package.json": "{}", "app/src/routes/login.ts": "x"}),
        )

        created = _post(f"{url}/api/app-scans", body, headers)
        job_id = str(created["job_id"])
        job = _wait_for_job(url, job_id)
        report = _get_json(f"{url}/api/app-scans/{job_id}/report")
        findings = _get_json(f"{url}/api/app-scans/{job_id}/findings")
        markdown = _get_text(f"{url}/api/app-scans/{job_id}/artifacts/report-md")

        assert job["status"] == "succeeded"
        assert report["type"] == "source"
        assert findings["findings"][0]["title"] == "SQL Injection"
        assert "Piranesi Security Analysis Report" in markdown
    finally:
        server.shutdown()
        server.server_close()
        if thread is not None:
            thread.join(timeout=2)


def test_workbench_surfaces_failed_scan_job(tmp_path: Path) -> None:
    def fake_runner(_job: Any, _workbench: Any) -> None:
        raise RuntimeError("scanner exploded")

    server = create_ui_server(
        workbench=True,
        jobs_dir=tmp_path / "jobs",
        port=0,
        scan_runner=fake_runner,
    )
    thread = None
    try:
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        url = f"http://{server.server_address[0]}:{server.server_address[1]}"
        body, headers = _multipart_zip("app.zip", _zip_bytes({"app/index.ts": "x"}))

        created = _post(f"{url}/api/app-scans", body, headers)
        job = _wait_for_job(url, str(created["job_id"]))

        assert job["status"] == "failed"
        assert job["error"] == "scanner exploded"
    finally:
        server.shutdown()
        server.server_close()
        if thread is not None:
            thread.join(timeout=2)


def test_workbench_allows_one_active_scan(tmp_path: Path) -> None:
    release = threading.Event()

    def fake_runner(job: Any, _workbench: Any) -> None:
        release.wait(timeout=5)
        _write_source_report(job.output_dir, job.project_dir)

    server = create_ui_server(
        workbench=True,
        jobs_dir=tmp_path / "jobs",
        port=0,
        scan_runner=fake_runner,
    )
    thread = None
    try:
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        url = f"http://{server.server_address[0]}:{server.server_address[1]}"
        body, headers = _multipart_zip("app.zip", _zip_bytes({"app/index.ts": "x"}))
        first = _post(f"{url}/api/app-scans", body, headers)

        second_body, second_headers = _multipart_zip(
            "second.zip",
            _zip_bytes({"app/index.ts": "x"}),
        )
        second = _post(
            f"{url}/api/app-scans",
            second_body,
            second_headers,
            expected_status=409,
        )
        release.set()
        job = _wait_for_job(url, str(first["job_id"]))

        assert "already running" in str(second["error"])
        assert job["status"] == "succeeded"
    finally:
        release.set()
        server.shutdown()
        server.server_close()
        if thread is not None:
            thread.join(timeout=2)


def test_default_workbench_runner_uses_existing_job_config(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    job_dir = tmp_path / "job"
    project_dir = job_dir / "source"
    output_dir = job_dir / "output"
    project_dir.mkdir(parents=True)
    output_dir.mkdir()
    now = "2026-05-14T00:00:00Z"
    job = ui_server_module.LocalScanJob(
        job_id="job-1",
        target_name="app",
        job_dir=job_dir,
        upload_path=job_dir / "upload.zip",
        extract_dir=project_dir,
        project_dir=project_dir,
        output_dir=output_dir,
        log_path=job_dir / "scan.log",
        created_at=now,
        updated_at=now,
    )
    workbench = ui_server_module.WorkbenchState(
        jobs_dir=tmp_path,
        max_upload_bytes=1024,
        max_extracted_bytes=1024,
        max_extracted_files=10,
        scan_timeout_seconds=30,
        scan_runner=ui_server_module._default_scan_runner,
    )

    def fake_run(command: list[str], **_kwargs: object) -> subprocess.CompletedProcess[str]:
        config_index = command.index("--config") + 1
        assert Path(command[config_index]).is_file()
        return subprocess.CompletedProcess(command, 0, stdout="", stderr="")

    monkeypatch.setattr(ui_server_module.subprocess, "run", fake_run)

    ui_server_module._default_scan_runner(job, workbench)

    assert (job_dir / "piranesi.toml").read_text(encoding="utf-8") == ""
    assert "$ " in job.log_path.read_text(encoding="utf-8")


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
    with pytest.raises(UiServerError, match=r"failed to load report\.json"):
        load_report_state(wrong_file)


def _wait_for_job(url: str, job_id: str) -> dict[str, object]:
    import time

    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        job = _get_json(f"{url}/api/app-scans/{job_id}")
        if job["status"] in {"succeeded", "failed"}:
            return job
        time.sleep(0.05)
    raise AssertionError("job did not finish")
