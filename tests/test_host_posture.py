from __future__ import annotations

import json
import subprocess
from pathlib import Path

import pytest
from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.doctor import build_doctor_report
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


def test_raw_bundle_normalizes_real_vm_posture_evidence(tmp_path: Path) -> None:
    raw_osquery = tmp_path / "raw" / "osquery"
    raw_commands = tmp_path / "raw" / "commands"
    raw_trivy = tmp_path / "raw" / "trivy"
    raw_osquery.mkdir(parents=True)
    raw_commands.mkdir(parents=True)
    raw_trivy.mkdir(parents=True)
    (raw_osquery / "system_info.json").write_text(
        json.dumps([{"hostname": "real-vm", "uuid": "real-vm-id"}]),
        encoding="utf-8",
    )
    (raw_osquery / "interface_addresses.json").write_text(
        json.dumps(
            [
                {"interface": "lo", "address": "127.0.0.1", "mask": "255.0.0.0", "type": "ipv4"},
                {
                    "interface": "eth0",
                    "address": "10.42.0.9",
                    "mask": "255.255.0.0",
                    "type": "ipv4",
                },
            ]
        ),
        encoding="utf-8",
    )
    (raw_osquery / "processes.json").write_text(
        json.dumps([{"pid": "944", "name": "redis-server", "user": "redis"}]),
        encoding="utf-8",
    )
    (raw_osquery / "listening_ports.json").write_text(
        json.dumps(
            [{"protocol": "tcp", "address": "0.0.0.0", "port": "6379", "pid": "944"}]  # noqa: S104
        ),
        encoding="utf-8",
    )
    (raw_osquery / "users.json").write_text(
        json.dumps(
            [
                {"username": "root", "uid": "0", "gid": "0", "groups": "root"},
                {"username": "deployer", "uid": "1001", "gid": "1001", "groups": "sudo"},
            ]
        ),
        encoding="utf-8",
    )
    (raw_osquery / "deb_packages.json").write_text(
        json.dumps([{"name": "openssl", "version": "1.1.1f-1ubuntu2.16"}]),
        encoding="utf-8",
    )
    (raw_osquery / "sshd_config.json").write_text(
        json.dumps([{"key": "PermitEmptyPasswords", "value": "yes"}]),
        encoding="utf-8",
    )
    (raw_commands / "ufw_status.json").write_text(
        json.dumps({"stdout": "Status: inactive\n", "stderr": ""}),
        encoding="utf-8",
    )
    (raw_commands / "apt_upgradable.json").write_text(
        json.dumps(
            {
                "stdout": (
                    "Listing...\n"
                    "openssl/jammy-security 1.1.1f-1ubuntu2.17 amd64 "
                    "[upgradable from: 1.1.1f-1ubuntu2.16]\n"
                ),
                "stderr": "",
            }
        ),
        encoding="utf-8",
    )
    (raw_trivy / "results.json").write_text(json.dumps({"Results": []}), encoding="utf-8")

    snapshot = load_host_input(tmp_path)
    report = analyze_snapshot(snapshot)

    titles = {finding.title for finding in report.findings}
    assert snapshot.identity.ip_addresses == ["10.42.0.9"]
    assert snapshot.listening_ports[0].process == "redis-server"
    assert snapshot.config["firewall"] == {
        "ufw_status": "inactive",
        "active": False,
        "sources": ["ufw_status"],
    }
    assert "Redis is listening on a public interface" in titles
    assert "Firewall appears inactive while public services are exposed" in titles
    assert "Security package updates are pending" in titles
    assert "Automatic security updates are not installed" in titles
    assert "SSH permits empty passwords" in titles
    assert report.host_metadata["ip_addresses"] == ["10.42.0.9"]
    assert {action["category"] for action in report.top_actions} >= {
        "exposure",
        "patching",
        "identity",
    }


def test_unattended_upgrades_absent_with_apt_evidence_triggers_finding(
    tmp_path: Path,
) -> None:
    _write_minimal_raw_bundle(
        tmp_path,
        packages=[{"name": "openssl", "version": "1.1.1f-1ubuntu2.16"}],
        commands={
            "apt_upgradable": {
                "stdout": (
                    "Listing...\n"
                    "openssl/jammy-security 1.1.1f-1ubuntu2.17 amd64 "
                    "[upgradable from: 1.1.1f-1ubuntu2.16]\n"
                ),
                "stderr": "",
            }
        },
    )

    report = analyze_snapshot(load_host_input(tmp_path))

    titles = {finding.title for finding in report.findings}
    assert "Security package updates are pending" in titles
    assert "Automatic security updates are not installed" in titles


def test_unattended_upgrades_installed_suppresses_finding(tmp_path: Path) -> None:
    _write_minimal_raw_bundle(
        tmp_path,
        packages=[
            {"name": "openssl", "version": "1.1.1f-1ubuntu2.16"},
            {"name": "unattended-upgrades", "version": "2.9.1+nmu3ubuntu1"},
        ],
        commands={
            "apt_upgradable": {
                "stdout": (
                    "Listing...\n"
                    "openssl/jammy-security 1.1.1f-1ubuntu2.17 amd64 "
                    "[upgradable from: 1.1.1f-1ubuntu2.16]\n"
                ),
                "stderr": "",
            }
        },
    )

    report = analyze_snapshot(load_host_input(tmp_path))

    titles = {finding.title for finding in report.findings}
    assert "Security package updates are pending" in titles
    assert "Automatic security updates are not installed" not in titles


def test_sysctl_command_evidence_triggers_only_insecure_value_findings(
    tmp_path: Path,
) -> None:
    _write_minimal_raw_bundle(
        tmp_path,
        commands={
            "sysctl_net_ipv4_ip_forward": {"stdout": "1\n", "stderr": ""},
            "sysctl_net_ipv6_conf_all_forwarding": {"stdout": "0\n", "stderr": ""},
            "sysctl_kernel_unprivileged_bpf_disabled": {"stdout": "0\n", "stderr": ""},
            "sysctl_kernel_kptr_restrict": {"stdout": "0\n", "stderr": ""},
        },
    )

    snapshot = load_host_input(tmp_path)
    report = analyze_snapshot(snapshot)

    titles = {finding.title for finding in report.findings}
    assert snapshot.config["sysctl"] == {
        "values": {
            "net.ipv4.ip_forward": "1",
            "net.ipv6.conf.all.forwarding": "0",
            "kernel.unprivileged_bpf_disabled": "0",
            "kernel.kptr_restrict": "0",
        },
        "sources": [
            "sysctl_net_ipv4_ip_forward",
            "sysctl_net_ipv6_conf_all_forwarding",
            "sysctl_kernel_unprivileged_bpf_disabled",
            "sysctl_kernel_kptr_restrict",
        ],
    }
    assert "IPv4 packet forwarding is enabled" in titles
    assert "IPv6 packet forwarding is enabled" not in titles
    assert "Unprivileged BPF is enabled" in titles
    assert "Kernel pointer exposure is unrestricted" in titles


def test_missing_sysctl_evidence_does_not_create_sysctl_findings(tmp_path: Path) -> None:
    _write_minimal_raw_bundle(tmp_path)

    report = analyze_snapshot(load_host_input(tmp_path))

    assert "sysctl" not in report.snapshot.config
    assert not any(
        finding.affected_component
        and finding.affected_component.startswith(("net.", "kernel."))
        for finding in report.findings
    )


def test_smoke_style_bundle_writes_report_metadata_and_top_actions(tmp_path: Path) -> None:
    evidence_dir = tmp_path / "evidence"
    report_dir = tmp_path / "report"
    _write_minimal_raw_bundle(
        evidence_dir,
        packages=[{"name": "openssl", "version": "1.1.1f-1ubuntu2.16"}],
        commands={"apt_upgradable": {"stdout": "Listing...\n", "stderr": ""}},
    )

    report = analyze_snapshot(load_host_input(evidence_dir))
    write_host_report_outputs(report, report_dir, report_format="both")

    payload = json.loads((report_dir / "host-report.json").read_text(encoding="utf-8"))
    assert payload["host_metadata"]["hostname"] == "minimal-vm"
    assert payload["top_actions"]
    assert payload["snapshot"]["identity"]["hostname"] == "minimal-vm"


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
    assert result.snapshot.identity.ip_addresses == ["10.0.0.20"]
    assert result.snapshot.listening_ports[0].process == "sshd"

    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    assert manifest["tool_versions"]["osquery"] == "osqueryi version 5.12.0"
    assert any(
        command["tool"] == "trivy" and command["status"] == "missing"
        for command in manifest["commands"]
    )
    assert any(
        command["tool"] == "system" and command["status"] == "missing"
        for command in manifest["commands"]
    )

    assessed = analyze_snapshot(load_host_input(tmp_path))
    assert assessed.collection_health is not None
    assert assessed.collection_health.status_counts["missing"] >= 1
    assert assessed.collection_health.required["osquery"].status == "ok"
    assert assessed.collection_health.optional["trivy"].status == "warn"
    assert not any(
        finding.title == "Missing Trivy vulnerability evidence"
        for finding in assessed.findings
    )


def test_manifest_missing_optional_commands_are_health_warnings_not_findings(
    tmp_path: Path,
) -> None:
    _write_minimal_raw_bundle(tmp_path)
    _write_manifest(
        tmp_path,
        [
            {"tool": "osquery", "name": "deb_packages", "status": "ok"},
            {"tool": "osquery", "name": "system_info", "status": "ok"},
            {"tool": "trivy", "name": "filesystem_scan", "status": "missing"},
            {"tool": "system", "name": "ufw_status", "status": "missing"},
            {"tool": "system", "name": "iptables_rules", "status": "missing"},
            {"tool": "system", "name": "nft_ruleset", "status": "missing"},
            {"tool": "system", "name": "apt_upgradable", "status": "missing"},
            {"tool": "system", "name": "sshd_effective_config", "status": "missing"},
            {"tool": "system", "name": "group_sudo", "status": "missing"},
            {"tool": "system", "name": "group_admin", "status": "missing"},
            {"tool": "system", "name": "group_wheel", "status": "missing"},
            {"tool": "system", "name": "sysctl_net_ipv4_ip_forward", "status": "missing"},
            {
                "tool": "system",
                "name": "sysctl_net_ipv6_conf_all_forwarding",
                "status": "missing",
            },
            {
                "tool": "system",
                "name": "sysctl_kernel_unprivileged_bpf_disabled",
                "status": "missing",
            },
            {"tool": "system", "name": "sysctl_kernel_kptr_restrict", "status": "missing"},
        ],
    )

    report = analyze_snapshot(load_host_input(tmp_path))

    assert report.collection_health is not None
    assert report.collection_health.optional["firewall"].status == "warn"
    assert report.collection_health.optional["sysctl"].status == "warn"
    assert "firewall" not in report.snapshot.config
    assert "sysctl" not in report.snapshot.config
    titles = {finding.title for finding in report.findings}
    assert "Missing Trivy vulnerability evidence" not in titles
    assert "Firewall appears inactive while public services are exposed" not in titles
    assert not any(title.startswith("IPv") for title in titles)


def test_failed_and_timeout_commands_are_grouped_by_capability(tmp_path: Path) -> None:
    _write_minimal_raw_bundle(tmp_path)
    _write_manifest(
        tmp_path,
        [
            {"tool": "osquery", "name": "deb_packages", "status": "ok"},
            {"tool": "system", "name": "ufw_status", "status": "failed"},
            {"tool": "system", "name": "iptables_rules", "status": "timeout"},
            {"tool": "system", "name": "nft_ruleset", "status": "missing"},
            {"tool": "system", "name": "apt_upgradable", "status": "failed"},
        ],
    )

    health = analyze_snapshot(load_host_input(tmp_path)).collection_health

    assert health is not None
    assert health.status_counts["failed"] == 2
    assert health.status_counts["timeout"] == 1
    assert health.optional["firewall"].commands_by_status == {
        "missing": 1,
        "failed": 1,
        "timeout": 1,
    }
    assert health.optional["apt_updates"].status == "warn"


def test_collect_optional_command_failures_do_not_fail_collection(tmp_path: Path) -> None:
    result = collect_host_evidence(
        tmp_path,
        include_trivy=False,
        executable_lookup=_fake_lookup_with_failing_ufw,
        command_runner=_fake_runner_with_failing_ufw,
    )

    manifest = json.loads((tmp_path / "collection-manifest.json").read_text(encoding="utf-8"))
    assert result.snapshot.identity.hostname == "collector-vm-01"
    assert any(
        command["name"] == "ufw_status" and command["status"] == "failed"
        for command in manifest["commands"]
    )
    assert any(
        command["name"] == "filesystem_scan" and command["status"] == "skipped"
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


def test_host_doctor_reports_full_readiness_when_tools_exist(tmp_path: Path) -> None:
    report = build_doctor_report(
        tmp_path,
        executable_lookup=_fake_doctor_lookup_all_tools,
        command_runner=_fake_doctor_runner,
    )

    assert report.assess_ready is True
    assert report.collect_ready is True
    assert report.required_tools["osquery"] == "ok"
    assert report.optional_tools["trivy"] == "ok"
    assert report.optional_tools["sysctl"] == "ok"


def test_host_doctor_treats_trivy_as_optional(tmp_path: Path) -> None:
    report = build_doctor_report(
        tmp_path,
        executable_lookup=_fake_doctor_lookup_osquery_only,
        command_runner=_fake_doctor_runner,
    )

    assert report.assess_ready is True
    assert report.collect_ready is True
    assert report.optional_tools["trivy"] == "warn"
    assert report.optional_tools["ufw"] == "warn"


def test_host_doctor_marks_collection_not_ready_without_osquery(tmp_path: Path) -> None:
    report = build_doctor_report(
        tmp_path,
        executable_lookup=lambda _name: None,
        command_runner=_fake_doctor_runner,
    )

    assert report.assess_ready is True
    assert report.collect_ready is False
    assert report.required_tools["osquery"] == "fail"
    assert any("osquery" in step for step in report.next_steps)


def test_llm_mode_without_provider_reports_coverage() -> None:
    snapshot = load_host_input(FIXTURES / "debian-vulnerable")

    report = analyze_snapshot(snapshot, analysis="llm", provider=None)

    assert report.analysis_modes == ["llm"]
    assert [finding.title for finding in report.findings] == ["LLM host analysis was not completed"]


def _write_minimal_raw_bundle(
    root: Path,
    *,
    packages: list[dict[str, str]] | None = None,
    commands: dict[str, object] | None = None,
) -> None:
    raw_osquery = root / "raw" / "osquery"
    raw_commands = root / "raw" / "commands"
    raw_osquery.mkdir(parents=True)
    raw_commands.mkdir(parents=True)
    (raw_osquery / "system_info.json").write_text(
        json.dumps([{"hostname": "minimal-vm", "uuid": "minimal-vm-id"}]),
        encoding="utf-8",
    )
    (raw_osquery / "deb_packages.json").write_text(
        json.dumps(packages or [{"name": "openssl", "version": "1.1.1f-1ubuntu2.16"}]),
        encoding="utf-8",
    )
    for name, payload in (commands or {}).items():
        (raw_commands / f"{name}.json").write_text(json.dumps(payload), encoding="utf-8")


def _write_manifest(root: Path, commands: list[dict[str, str]]) -> None:
    (root / "collection-manifest.json").write_text(
        json.dumps(
            {
                "schema_version": 1,
                "output_dir": str(root),
                "raw_dir": str(root / "raw"),
                "commands": commands,
            }
        ),
        encoding="utf-8",
    )


def _fake_lookup_without_trivy(name: str) -> str | None:
    if name == "osqueryi":
        return "/usr/local/bin/osqueryi"
    return None


def _fake_lookup_with_failing_ufw(name: str) -> str | None:
    if name == "ufw":
        return "/usr/sbin/ufw"
    return _fake_lookup_without_trivy(name)


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
    elif "from interface_addresses" in query:
        payload = [
            {"interface": "lo", "address": "127.0.0.1", "mask": "255.0.0.0", "type": "ipv4"},
            {
                "interface": "ens3",
                "address": "10.0.0.20",
                "mask": "255.255.255.0",
                "type": "ipv4",
            },
        ]
    elif "from deb_packages" in query:
        payload = [{"name": "openssh-server", "version": "1:9.6p1", "arch": "amd64"}]
    elif "from listening_ports" in query:
        payload = [{"protocol": "tcp", "address": "127.0.0.1", "port": "22", "pid": "100"}]
    elif "from processes" in query:
        payload = [{"pid": "100", "name": "sshd", "path": "/usr/sbin/sshd", "user": "root"}]
    elif "from users" in query:
        payload = [{"username": "root", "uid": "0", "gid": "0", "shell": "/bin/bash"}]
    elif "from systemd_units" in query:
        payload = [{"name": "ssh.service", "active_state": "active", "unit_file_state": "enabled"}]
    else:
        payload = []
    return subprocess.CompletedProcess(command, 0, stdout=json.dumps(payload), stderr="")


def _fake_runner_with_failing_ufw(
    args: object,
    *,
    capture_output: bool,
    text: bool,
    timeout: int,
) -> subprocess.CompletedProcess[str]:
    command = list(args) if isinstance(args, list | tuple) else [str(args)]
    if command[0] == "/usr/sbin/ufw":
        return subprocess.CompletedProcess(command, 1, stdout="", stderr="permission denied")
    return _fake_osquery_runner(
        command,
        capture_output=capture_output,
        text=text,
        timeout=timeout,
    )


def _fake_doctor_lookup_all_tools(name: str) -> str | None:
    if name in {
        "osqueryi",
        "trivy",
        "ufw",
        "iptables",
        "nft",
        "apt",
        "sshd",
        "getent",
        "sysctl",
    }:
        return f"/usr/local/bin/{name}"
    return None


def _fake_doctor_lookup_osquery_only(name: str) -> str | None:
    if name == "osqueryi":
        return "/usr/local/bin/osqueryi"
    return None


def _fake_doctor_runner(
    args: object,
    *,
    check: bool,
    capture_output: bool,
    text: bool,
    timeout: int,
) -> subprocess.CompletedProcess[str]:
    assert check is False
    assert capture_output is True
    assert text is True
    assert timeout == 5
    command = list(args) if isinstance(args, list | tuple) else [str(args)]
    executable = Path(str(command[0])).name
    if executable == "osqueryi":
        return subprocess.CompletedProcess(
            command,
            0,
            stdout="osqueryi version 5.12.0\n",
            stderr="",
        )
    if executable == "trivy":
        return subprocess.CompletedProcess(command, 0, stdout="Version: 0.50.0\n", stderr="")
    if executable in {"ufw", "iptables", "nft", "apt", "sshd", "getent", "sysctl"}:
        return subprocess.CompletedProcess(command, 0, stdout=f"{executable} ok\n", stderr="")
    return subprocess.CompletedProcess(command, 1, stdout="", stderr="unexpected")
