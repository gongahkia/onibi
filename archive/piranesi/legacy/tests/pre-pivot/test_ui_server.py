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
        path = parsed.path
        if parsed.query:
            path = f"{path}?{parsed.query}"
        connection.request("POST", path, body=body, headers=headers)
        response = connection.getresponse()
        payload = json.loads(response.read().decode("utf-8"))
        assert response.status == expected_status, payload
        return payload
    finally:
        connection.close()


def _delete(url: str, *, expected_status: int = 200) -> dict[str, object]:
    parsed = urlparse(url)
    assert parsed.scheme == "http"
    assert parsed.hostname is not None
    connection = http.client.HTTPConnection(parsed.hostname, parsed.port, timeout=5)
    try:
        path = parsed.path
        if parsed.query:
            path = f"{path}?{parsed.query}"
        connection.request("DELETE", path)
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


def _json_body(payload: dict[str, object]) -> tuple[bytes, dict[str, str]]:
    body = json.dumps(payload).encode("utf-8")
    return body, {
        "Content-Type": "application/json",
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


def test_ui_artifacts_and_handoff_preview_are_local_dry_run() -> None:
    server = run_ui_server(
        UiServerOptions(report_path=REPORT_FIXTURE, port=0),
        block=False,
    )
    try:
        url = f"http://{server.server_address[0]}:{server.server_address[1]}"
        summary = _get_json(f"{url}/api/report")
        csv_status, csv_headers, csv_body = _get_raw(f"{url}/api/artifacts/csv")
        pdf_status, pdf_headers, pdf_body = _get_raw(f"{url}/api/artifacts/pdf")
        preview = _get_json(f"{url}/api/handoff/preview?integration=github")
        blocked = _post(
            f"{url}/api/handoff/send?integration=github",
            b"",
            {},
            expected_status=409,
        )

        assert summary["artifacts"]["report_json"] == "/api/artifacts/report-json"
        assert csv_status == 200
        assert csv_headers["content-type"] == "text/csv; charset=utf-8"
        assert b"finding_id" in csv_body
        assert pdf_status == 200
        assert pdf_headers["content-type"] == "application/pdf"
        assert pdf_body.startswith(b"%PDF")
        assert preview["dry_run"] is True
        assert preview["confirmation_required"] is True
        assert preview["preview"]
        assert "confirm=true" in str(blocked["error"])
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
        assert "preflightPanel" in text
        assert "sampleGallery" in text
        assert "recentScans" in text
        assert "Run bundled ZIP demo" in text
        assert "Container" in text
        assert "Privacy defaults" in text
        summary = _get_json(f"{url}/api/report")
        preflight = _get_json(f"{url}/api/preflight")

        assert summary["type"] == "workbench"
        assert summary["title"] == "Piranesi Local Evidence Workbench"
        assert preflight["mode"] == "workbench"
        assert preflight["ui"]["workbench_enabled"] is True
        assert any(check["name"] == "python" for check in preflight["checks"])
    finally:
        server.shutdown()
        server.server_close()


def test_workbench_sample_gallery_lists_downloadable_app_zip(tmp_path: Path) -> None:
    server = run_ui_server(
        UiServerOptions(workbench=True, jobs_dir=tmp_path / "jobs", port=0),
        block=False,
    )
    try:
        url = f"http://{server.server_address[0]}:{server.server_address[1]}"

        gallery = _get_json(f"{url}/api/samples")
        samples = {str(sample["id"]): sample for sample in gallery["samples"]}

        assert samples["host-demo"]["command"] == "piranesi demo --output piranesi-demo-output"
        assert samples["app-vuln-express"]["download_url"] == ("/api/samples/app-vuln-express.zip")

        status, headers, body = _get_raw(f"{url}/api/samples/app-vuln-express.zip")
        assert status == 200
        assert headers["content-type"] == "application/zip"
        with zipfile.ZipFile(io.BytesIO(body)) as archive:
            names = set(archive.namelist())
        assert "vuln-express/package.json" in names
        assert "vuln-express/app.js" in names
    finally:
        server.shutdown()
        server.server_close()


def test_workbench_runs_bundled_sample_with_mocked_runner(tmp_path: Path) -> None:
    def fake_runner(job: Any, _workbench: Any) -> None:
        assert job.input_kind == "sample"
        assert (job.project_dir / "app.js").is_file()
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

        created = _post(f"{url}/api/app-scans/sample/app-vuln-express", b"", {})
        job = _wait_for_job(url, str(created["job_id"]))
        report = _get_json(f"{url}/api/app-scans/{created['job_id']}/report")

        assert job["status"] == "succeeded"
        assert job["input_kind"] == "sample"
        assert report["type"] == "source"
    finally:
        server.shutdown()
        server.server_close()
        if thread is not None:
            thread.join(timeout=2)


def test_workbench_rejects_unsupported_url_import(tmp_path: Path) -> None:
    server = run_ui_server(
        UiServerOptions(workbench=True, jobs_dir=tmp_path / "jobs", port=0),
        block=False,
    )
    try:
        url = f"http://{server.server_address[0]}:{server.server_address[1]}"
        body, headers = _json_body({"url": "https://example.com/app.zip"})

        payload = _post(f"{url}/api/app-scans/import-url", body, headers, expected_status=400)

        assert "only public https://github.com/owner/repo" in str(payload["error"])
    finally:
        server.shutdown()
        server.server_close()


def test_workbench_imports_mocked_github_repo(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    def fake_git(command: list[str], **_kwargs: object) -> subprocess.CompletedProcess[str]:
        assert command[:6] == ["git", "clone", "--depth", "1", "--single-branch", "--no-tags"]
        destination = Path(command[-1])
        destination.mkdir(parents=True)
        (destination / "package.json").write_text("{}", encoding="utf-8")
        (destination / "index.ts").write_text("export const value = 1;\n", encoding="utf-8")
        (destination / ".git").mkdir()
        return subprocess.CompletedProcess(command, 0, stdout="cloned", stderr="")

    def fake_runner(job: Any, _workbench: Any) -> None:
        assert job.input_kind == "github"
        assert job.target_name == "octo/demo"
        assert (job.project_dir / "index.ts").is_file()
        assert not (job.project_dir / ".git").exists()
        _write_source_report(job.output_dir, job.project_dir)

    monkeypatch.setattr(ui_server_module.subprocess, "run", fake_git)
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
        body, headers = _json_body({"url": "https://github.com/octo/demo"})

        created = _post(f"{url}/api/app-scans/import-url", body, headers)
        job = _wait_for_job(url, str(created["job_id"]))
        report = _get_json(f"{url}/api/app-scans/{created['job_id']}/report")

        assert job["status"] == "succeeded"
        assert job["input_kind"] == "github"
        assert report["type"] == "source"
    finally:
        server.shutdown()
        server.server_close()
        if thread is not None:
            thread.join(timeout=2)


def test_source_finding_detail_payload_contains_analyst_context(tmp_path: Path) -> None:
    target_dir = tmp_path / "app"
    target_dir.mkdir()
    _write_source_report(tmp_path, target_dir)

    server = run_ui_server(
        UiServerOptions(report_path=tmp_path, port=0),
        block=False,
    )
    try:
        url = f"http://{server.server_address[0]}:{server.server_address[1]}"
        findings = _get_json(f"{url}/api/findings")
        finding = findings["findings"][0]

        assert finding["risk_rationale"]
        assert finding["confidence_notes"]
        assert finding["verification"]["state"] == "verified_confirmed"
        assert "parameterized queries" in finding["remediation"]
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
        sarif = _get_json(f"{url}/api/app-scans/{job_id}/artifacts/sarif")
        csv_status, _csv_headers, csv_body = _get_raw(f"{url}/api/app-scans/{job_id}/artifacts/csv")
        preview = _get_json(f"{url}/api/app-scans/{job_id}/handoff/preview?integration=slack")
        blocked = _post(
            f"{url}/api/app-scans/{job_id}/handoff/send?integration=slack",
            b"",
            {},
            expected_status=409,
        )

        assert job["status"] == "succeeded"
        assert report["type"] == "source"
        assert findings["findings"][0]["title"] == "SQL Injection"
        assert "Piranesi Security Analysis Report" in markdown
        assert sarif["version"] == "2.1.0"
        assert csv_status == 200
        assert b"cwe_id" in csv_body
        assert preview["integration"] == "slack"
        assert preview["dry_run"] is True
        assert "confirm=true" in str(blocked["error"])
    finally:
        server.shutdown()
        server.server_close()
        if thread is not None:
            thread.join(timeout=2)


def test_workbench_persists_reopens_and_deletes_job_records(tmp_path: Path) -> None:
    jobs_dir = tmp_path / "jobs"

    def fake_runner(job: Any, _workbench: Any) -> None:
        _write_source_report(job.output_dir, job.project_dir)

    server = create_ui_server(
        workbench=True,
        jobs_dir=jobs_dir,
        port=0,
        scan_runner=fake_runner,
    )
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        url = f"http://{server.server_address[0]}:{server.server_address[1]}"
        body, headers = _multipart_zip("app.zip", _zip_bytes({"app/index.ts": "x"}))
        created = _post(f"{url}/api/app-scans", body, headers)
        job_id = str(created["job_id"])
        job = _wait_for_job(url, job_id)
        assert job["status"] == "succeeded"
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=2)

    index_payload = json.loads((jobs_dir / "jobs-index.json").read_text(encoding="utf-8"))
    assert index_payload["jobs"][0]["job_id"] == job_id

    reopened = create_ui_server(
        workbench=True,
        jobs_dir=jobs_dir,
        port=0,
        scan_runner=fake_runner,
    )
    reopened_thread = threading.Thread(target=reopened.serve_forever, daemon=True)
    reopened_thread.start()
    try:
        url = f"http://{reopened.server_address[0]}:{reopened.server_address[1]}"
        jobs = _get_json(f"{url}/api/app-scans")
        report = _get_json(f"{url}/api/app-scans/{job_id}/report")
        deleted = _delete(f"{url}/api/app-scans/{job_id}")

        assert jobs["jobs"][0]["job_id"] == job_id
        assert report["type"] == "source"
        assert deleted["deleted"] is True
        assert not (jobs_dir / job_id).exists()
    finally:
        reopened.shutdown()
        reopened.server_close()
        reopened_thread.join(timeout=2)


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
        index_payload = _wait_for_job_index_error(tmp_path / "jobs", "scanner exploded")
        assert index_payload["jobs"][0]["error"] == "scanner exploded"
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


def _wait_for_job_index_error(jobs_dir: Path, expected_error: str) -> dict[str, object]:
    import time

    index_path = jobs_dir / "jobs-index.json"
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        payload = json.loads(index_path.read_text(encoding="utf-8"))
        if payload["jobs"][0]["error"] == expected_error:
            return payload
        time.sleep(0.05)
    raise AssertionError("job index did not record failed scan error")
