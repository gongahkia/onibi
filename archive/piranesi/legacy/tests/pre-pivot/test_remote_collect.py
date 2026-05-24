from __future__ import annotations

import json
from collections.abc import Sequence
from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.host.remote import (
    RemoteCommandResult,
    RemoteHostTarget,
    collect_remote_host,
    collect_remote_hosts,
)

runner = CliRunner()


class FakeSSHTransport:
    def __init__(self, *, fail_hosts: set[str] | None = None) -> None:
        self.fail_hosts = fail_hosts or set()
        self.commands: list[list[str]] = []

    def run(
        self,
        target: RemoteHostTarget,
        command: Sequence[str],
        *,
        timeout: int,
    ) -> RemoteCommandResult:
        assert not isinstance(command, str)
        rendered = list(command)
        self.commands.append(rendered)
        if target.host in self.fail_hosts:
            return RemoteCommandResult(
                command=rendered,
                returncode=255,
                stderr="Permission denied for password hunter2",
            )
        if rendered == ["osqueryi", "--version"]:
            return RemoteCommandResult(command=rendered, returncode=0, stdout="osqueryi 5.12.0\n")
        if rendered[:2] == ["osqueryi", "--json"]:
            return RemoteCommandResult(
                command=rendered,
                returncode=0,
                stdout=json.dumps(_osquery_payload(rendered[2])),
            )
        if rendered == ["trivy", "fs", "--format", "json", "--quiet", "--scanners", "vuln", "/"]:
            return RemoteCommandResult(
                command=rendered,
                returncode=0,
                stdout=json.dumps({"Results": []}),
            )
        return RemoteCommandResult(command=rendered, returncode=0, stdout="")


def test_single_remote_host_collection_writes_expected_layout(tmp_path: Path) -> None:
    transport = FakeSSHTransport()
    target = RemoteHostTarget(host="vm-001", user="ubuntu")

    result = collect_remote_host(
        target,
        tmp_path / "vm-001",
        transport=transport,
        include_trivy=False,
    )

    assert result.status == "ok"
    assert (tmp_path / "vm-001" / "host_snapshot.json").is_file()
    assert (tmp_path / "vm-001" / "collection-manifest.json").is_file()
    assert (tmp_path / "vm-001" / "raw" / "osquery" / "system_info.json").is_file()
    manifest = json.loads(
        (tmp_path / "vm-001" / "collection-manifest.json").read_text(encoding="utf-8")
    )
    assert manifest["commands"]
    assert all(
        isinstance(item["command"], list) for item in manifest["commands"] if item["command"]
    )
    assert any(item["status"] == "skipped" for item in manifest["commands"])


def test_multi_host_collection_continues_after_failure(tmp_path: Path) -> None:
    transport = FakeSSHTransport(fail_hosts={"vm-bad"})
    targets = [
        RemoteHostTarget(host="vm-good"),
        RemoteHostTarget(host="vm-bad"),
        RemoteHostTarget(host="vm-other"),
    ]

    summary = collect_remote_hosts(
        targets,
        tmp_path / "fleet-evidence",
        transport=transport,
        include_trivy=False,
    )

    assert summary.host_count == 3
    assert summary.success_count == 2
    assert summary.failure_count == 1
    assert (tmp_path / "fleet-evidence" / "vm-good" / "host_snapshot.json").is_file()
    assert (tmp_path / "fleet-evidence" / "vm-other" / "host_snapshot.json").is_file()
    assert (tmp_path / "fleet-evidence" / "vm-bad" / "collection-manifest.json").is_file()
    assert (tmp_path / "fleet-evidence" / "remote-collection-summary.json").is_file()


def test_dry_run_writes_no_evidence(tmp_path: Path) -> None:
    transport = FakeSSHTransport()

    result = collect_remote_host(
        RemoteHostTarget(host="vm-001"),
        tmp_path / "vm-001",
        transport=transport,
        dry_run=True,
    )

    assert result.status == "dry_run"
    assert result.planned_commands
    assert transport.commands == []
    assert not (tmp_path / "vm-001").exists()


def test_invalid_sudo_mode_fails_validation(tmp_path: Path) -> None:
    result = runner.invoke(
        app,
        [
            "remote",
            "collect",
            "--host",
            "vm-001",
            "--output",
            str(tmp_path / "out"),
            "--sudo-mode",
            "sometimes",
            "--dry-run",
        ],
    )

    assert result.exit_code == 2


def test_command_execution_is_not_shell_string_based(tmp_path: Path) -> None:
    transport = FakeSSHTransport()

    collect_remote_host(
        RemoteHostTarget(host="vm-001"),
        tmp_path / "vm-001",
        transport=transport,
        include_trivy=False,
    )

    assert transport.commands
    assert all(isinstance(command, list) for command in transport.commands)
    assert all(command[0] != "sh" for command in transport.commands)


def _osquery_payload(query: str) -> list[dict[str, object]]:
    if "from system_info" in query:
        return [{"hostname": "vm-001", "uuid": "remote-uuid"}]
    if "from os_version" in query:
        return [{"name": "Ubuntu", "version": "22.04", "id": "ubuntu"}]
    if "from kernel_info" in query:
        return [{"version": "5.15.0"}]
    if "from deb_packages" in query:
        return [{"name": "openssl", "version": "3.0.2", "arch": "amd64"}]
    if "from interface_addresses" in query:
        return [{"interface": "eth0", "address": "10.0.0.10", "type": "inet"}]
    return []
