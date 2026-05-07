from __future__ import annotations

import json
import subprocess
from pathlib import Path

import pytest
from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.host import (
    HostCollectionError,
    analyze_snapshot,
    collect_host_evidence,
    load_host_input,
    write_host_report_outputs,
)

FIXTURES = Path(__file__).parent / "fixtures" / "host"


def test_load_raw_osquery_trivy_bundle_and_analyze() -> None:
    snapshot = load_host_input(FIXTURES / "debian-vulnerable")
    report = analyze_snapshot(snapshot)

    titles = {finding.title for finding in report.findings}

    assert snapshot.identity.hostname == "debian-vm-01"
    assert len(snapshot.packages) == 2
    assert "Redis is listening on a public interface" in titles
    assert "SSH root login is allowed" in titles
    assert "SSH password authentication is enabled" in titles
    assert "Privileged local account present: deployer" in titles
    assert any(finding.cve_ids == ["CVE-2023-0464"] for finding in report.findings)
    assert report.posture_score < 100


def test_load_canonical_snapshot_and_write_reports(tmp_path: Path) -> None:
    snapshot = load_host_input(FIXTURES / "debian-clean" / "host_snapshot.json")
    report = analyze_snapshot(snapshot)

    write_host_report_outputs(report, tmp_path, report_format="both")

    payload = json.loads((tmp_path / "host-report.json").read_text(encoding="utf-8"))
    markdown = (tmp_path / "host-report.md").read_text(encoding="utf-8")

    assert payload["target"] == "debian-clean-01"
    assert payload["findings"] == []
    assert "Piranesi Host Posture Report" in markdown


def test_assess_cli_writes_host_reports(tmp_path: Path) -> None:
    runner = CliRunner()
    output_dir = tmp_path / "out"

    result = runner.invoke(
        app,
        [
            "assess",
            str(FIXTURES / "debian-vulnerable"),
            "--output",
            str(output_dir),
            "--format",
            "both",
        ],
    )

    assert result.exit_code == 0, result.stdout
    assert (output_dir / "host-report.json").is_file()
    assert (output_dir / "host-report.md").is_file()

    payload = json.loads((output_dir / "host-report.json").read_text(encoding="utf-8"))
    assert payload["target"] == "debian-vm-01"
    assert payload["summary"]["findings_total"] >= 5


def test_load_collector_raw_layout_without_snapshot(tmp_path: Path) -> None:
    raw_osquery = tmp_path / "raw" / "osquery"
    raw_trivy = tmp_path / "raw" / "trivy"
    raw_osquery.mkdir(parents=True)
    raw_trivy.mkdir(parents=True)
    (raw_osquery / "system_info.json").write_text(
        json.dumps([{"hostname": "collected-vm"}]),
        encoding="utf-8",
    )
    (raw_osquery / "os_version.json").write_text(
        json.dumps([{"name": "Ubuntu", "version": "24.04", "id": "ubuntu"}]),
        encoding="utf-8",
    )
    (raw_trivy / "results.json").write_text(json.dumps({"Results": []}), encoding="utf-8")

    snapshot = load_host_input(tmp_path)

    assert snapshot.identity.hostname == "collected-vm"
    assert snapshot.tool_provenance["osquery"] == str(raw_osquery)
    assert "trivy" in snapshot.raw_evidence


def test_collect_host_evidence_writes_snapshot_manifest_and_raw_layout(tmp_path: Path) -> None:
    result = collect_host_evidence(
        tmp_path,
        executable_lookup=_fake_lookup_without_trivy,
        command_runner=_fake_osquery_runner,
    )

    snapshot_path = tmp_path / "host_snapshot.json"
    manifest_path = tmp_path / "collection-manifest.json"

    assert snapshot_path.is_file()
    assert manifest_path.is_file()
    assert (tmp_path / "raw" / "osquery" / "system_info.json").is_file()
    assert result.snapshot.identity.hostname == "collector-vm-01"

    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    assert manifest["tool_versions"]["osquery"] == "osqueryi version 5.12.0"
    assert any(
        command["tool"] == "trivy" and command["status"] == "missing"
        for command in manifest["commands"]
    )


def test_collect_host_evidence_requires_osquery(tmp_path: Path) -> None:
    with pytest.raises(HostCollectionError, match="osqueryi was not found"):
        collect_host_evidence(tmp_path, executable_lookup=lambda _name: None)

    manifest = json.loads((tmp_path / "collection-manifest.json").read_text(encoding="utf-8"))
    assert manifest["commands"][0]["status"] == "missing"


def test_collect_cli_reports_collection_errors(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    def _raise_collection_error(*_args: object, **_kwargs: object) -> None:
        raise HostCollectionError("osqueryi was not found on PATH")

    monkeypatch.setattr("piranesi.cli.collect_host_evidence", _raise_collection_error)

    result = CliRunner().invoke(app, ["collect", "--output", str(tmp_path)])

    assert result.exit_code == 2


def test_llm_mode_without_provider_reports_coverage() -> None:
    snapshot = load_host_input(FIXTURES / "debian-vulnerable")

    report = analyze_snapshot(snapshot, analysis="llm", provider=None)

    assert report.analysis_modes == ["llm"]
    assert [finding.title for finding in report.findings] == ["LLM host analysis was not completed"]


def _fake_lookup_without_trivy(name: str) -> str | None:
    if name == "osqueryi":
        return "/usr/local/bin/osqueryi"
    return None


def _fake_osquery_runner(
    args: object,
    *,
    capture_output: bool,
    text: bool,
    timeout: int,
) -> subprocess.CompletedProcess[str]:
    assert capture_output is True
    assert text is True
    assert timeout > 0
    command = list(args) if isinstance(args, list | tuple) else [str(args)]
    if command[-1] == "--version":
        return subprocess.CompletedProcess(
            command,
            0,
            stdout="osqueryi version 5.12.0\n",
            stderr="",
        )
    query = command[-1]
    if "from system_info" in query:
        payload = [{"hostname": "collector-vm-01", "uuid": "collector-uuid"}]
    elif "from os_version" in query:
        payload = [{"name": "Ubuntu", "version": "24.04", "id": "ubuntu"}]
    elif "from kernel_info" in query:
        payload = [{"version": "6.8.0-31-generic"}]
    elif "from deb_packages" in query:
        payload = [{"name": "openssh-server", "version": "1:9.6p1", "arch": "amd64"}]
    elif "from listening_ports" in query:
        payload = [{"protocol": "tcp", "address": "127.0.0.1", "port": "22", "pid": "100"}]
    elif "from users" in query:
        payload = [{"username": "root", "uid": "0", "gid": "0", "shell": "/bin/bash"}]
    elif "from systemd_units" in query:
        payload = [{"name": "ssh.service", "active_state": "active", "unit_file_state": "enabled"}]
    else:
        payload = []
    return subprocess.CompletedProcess(command, 0, stdout=json.dumps(payload), stderr="")
