from __future__ import annotations

import json
import shutil
from collections.abc import Sequence
from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.rescan.image_policy import AcceptedImage
from piranesi.rescan.runtime import ContainerRuntimeStatus
from piranesi.retest import build_retest_result
from piranesi.workspace import (
    AUDIT_LOG_FILE,
    NormalizedFindingsDocument,
    WorkspaceState,
    load_workspace,
    save_workspace,
)

OPEN_FIXTURE = Path(__file__).parent / "fixtures" / "pentest" / "nmap" / "localhost-http.xml"
CLOSED_FIXTURE = (
    Path(__file__).parent / "fixtures" / "pentest" / "nmap" / "localhost-http-closed.xml"
)
DIGEST = "sha256:" + "a" * 64
runner = CliRunner()


def test_retest_cli_classifies_closed_findings_from_real_nmap_snapshots(tmp_path: Path) -> None:
    baseline = _ingest_nmap(tmp_path / "baseline", OPEN_FIXTURE)
    current = _ingest_nmap(tmp_path / "current", CLOSED_FIXTURE)
    output = tmp_path / "retest.json"

    result = runner.invoke(
        app,
        [
            "retest",
            "--baseline",
            str(baseline),
            "--current",
            str(current),
            "--output",
            str(output),
            "--json",
        ],
    )

    assert result.exit_code == 0, result.output
    payload = json.loads(output.read_text(encoding="utf-8"))
    assert payload["summary"]["closed"] == 2
    assert payload["summary"]["new"] == 0
    assert {item["status"] for item in payload["findings"]} == {"closed"}
    audit_events = [
        json.loads(line)
        for line in (current / AUDIT_LOG_FILE).read_text(encoding="utf-8").splitlines()
    ]
    assert audit_events[-1]["command"] == "retest"
    assert audit_events[-1]["summary"]["statuses"]["closed"] == 2


def test_retest_cli_classifies_new_findings_from_real_nmap_snapshots(tmp_path: Path) -> None:
    baseline = _ingest_nmap(tmp_path / "baseline", CLOSED_FIXTURE)
    current = _ingest_nmap(tmp_path / "current", OPEN_FIXTURE)
    output = tmp_path / "retest.md"

    result = runner.invoke(
        app,
        [
            "retest",
            "--baseline",
            str(baseline),
            "--current",
            str(current),
            "--output",
            str(output),
        ],
    )

    assert result.exit_code == 0, result.output
    markdown = output.read_text(encoding="utf-8")
    assert "- new: 2" in markdown
    current_state = load_workspace(current)
    assert {finding.status for finding in current_state.findings.findings} == {"new"}
    assert {finding.provenance["retest_status"] for finding in current_state.findings.findings} == {
        "new"
    }


def test_retest_classifies_regressed_and_report_surfaces_status(tmp_path: Path) -> None:
    baseline = _ingest_nmap(tmp_path / "baseline", OPEN_FIXTURE)
    current = _ingest_nmap(tmp_path / "current", OPEN_FIXTURE)
    _mark_all_findings_closed(baseline)
    output = tmp_path / "retest.json"

    result = runner.invoke(
        app,
        [
            "retest",
            "--baseline",
            str(baseline),
            "--current",
            str(current),
            "--output",
            str(output),
        ],
    )
    assert result.exit_code == 0, result.output
    payload = json.loads(output.read_text(encoding="utf-8"))
    assert payload["summary"]["regressed"] == 2

    report = runner.invoke(
        app,
        ["report", "--workspace", str(current), "--format", "json", "--json"],
    )
    assert report.exit_code == 0, report.output
    report_path = Path(json.loads(report.stdout)["path"])
    report_payload = json.loads(report_path.read_text(encoding="utf-8"))
    assert {finding["retest_status"] for finding in report_payload["findings"]} == {"regressed"}

    markdown_report = runner.invoke(
        app,
        [
            "report",
            "--workspace",
            str(current),
            "--format",
            "md",
            "--output",
            str(tmp_path / "markdown-report"),
        ],
    )
    assert markdown_report.exit_code == 0, markdown_report.output
    markdown = (tmp_path / "markdown-report" / "pentest-report.md").read_text(encoding="utf-8")
    assert "- Retest: regressed" in markdown

    pdf_report = runner.invoke(
        app,
        [
            "report",
            "--workspace",
            str(current),
            "--format",
            "pdf",
            "--pdf-backend",
            "reportlab",
            "--output",
            str(tmp_path / "pdf-report"),
        ],
    )
    assert pdf_report.exit_code == 0, pdf_report.output
    assert (tmp_path / "pdf-report" / "pentest-report-reportlab.pdf").stat().st_size > 0


def test_retest_flags_ambiguous_fallback_matches(tmp_path: Path) -> None:
    baseline = _ingest_nmap(tmp_path / "baseline", OPEN_FIXTURE)
    current = _ingest_nmap(tmp_path / "current", OPEN_FIXTURE)
    baseline_state = load_workspace(baseline)
    current_state = load_workspace(current)

    duplicate = baseline_state.findings.findings[0].model_copy(
        update={"id": "finding:duplicate-fallback"}
    )
    baseline_state = WorkspaceState(
        root=baseline_state.root,
        workspace=baseline_state.workspace,
        findings=NormalizedFindingsDocument(
            findings=[baseline_state.findings.findings[0], duplicate]
        ),
    )
    current_finding = current_state.findings.findings[0].model_copy(
        update={"id": "finding:changed-id"}
    )
    current_state = WorkspaceState(
        root=current_state.root,
        workspace=current_state.workspace,
        findings=NormalizedFindingsDocument(findings=[current_finding]),
    )

    result = build_retest_result(baseline_state, current_state)

    assert result.summary["ambiguous"] == 1
    assert result.ambiguous_matches[0]["current_id"] == "finding:changed-id"
    assert set(result.ambiguous_matches[0]["candidate_baseline_ids"]) == {
        baseline_state.findings.findings[0].id,
        "finding:duplicate-fallback",
    }


def test_retest_can_generate_current_workspace_with_rescan(
    monkeypatch,
    tmp_path: Path,
) -> None:
    baseline = _ingest_nmap(tmp_path / "baseline", OPEN_FIXTURE)
    current = tmp_path / "current"
    output = tmp_path / "retest.json"
    monkeypatch.setattr(
        "piranesi.rescan.executor.ensure_container_runtime",
        lambda: ContainerRuntimeStatus(docker_python_available=True, docker_cli_path="/bin/docker"),
    )

    def fake_runner(
        _image: AcceptedImage,
        _command: Sequence[str],
        _host_output_dir: Path,
        host_output_path: Path,
        _timeout_seconds: int,
    ) -> None:
        shutil.copyfile(OPEN_FIXTURE, host_output_path)

    monkeypatch.setattr("piranesi.rescan.executor._run_replay_container", fake_runner)

    result = runner.invoke(
        app,
        [
            "retest",
            "--baseline",
            str(baseline),
            "--current",
            str(current),
            "--rescan",
            "--image",
            f"nmap=ghcr.io/acme/nmap:v1@{DIGEST}",
            "--allow-unenforced-network",
            "--output",
            str(output),
            "--json",
        ],
    )

    assert result.exit_code == 0, result.output
    payload = json.loads(result.stdout)
    assert payload["summary"]["open"] == 2
    assert payload["rescan"]["output_workspace"] == str(current.resolve())
    current_state = load_workspace(current)
    assert len(current_state.findings.findings) == 2
    assert {finding.provenance["retest_status"] for finding in current_state.findings.findings} == {
        "open"
    }


def _ingest_nmap(workspace: Path, fixture: Path) -> Path:
    result = runner.invoke(
        app,
        ["ingest", "nmap", "--input", str(fixture), "--workspace", str(workspace)],
    )
    assert result.exit_code == 0, result.output
    return workspace


def _mark_all_findings_closed(workspace: Path) -> None:
    state = load_workspace(workspace)
    updated = [
        finding.model_copy(update={"status": "closed"}) for finding in state.findings.findings
    ]
    save_workspace(
        WorkspaceState(
            root=state.root,
            workspace=state.workspace,
            findings=state.findings.model_copy(update={"findings": updated}),
        )
    )
