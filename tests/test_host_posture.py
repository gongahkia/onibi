from __future__ import annotations

import json
import subprocess
from pathlib import Path

import pytest
from typer.testing import CliRunner

from piranesi.cli import HostReportFormat, app
from piranesi.doctor import build_doctor_report
from piranesi.host import (
    HostCollectionError,
    HostIdentity,
    HostPackage,
    HostProcess,
    HostSnapshot,
    ListeningPort,
    NetworkInterface,
    OsRelease,
    ServiceState,
    UserAccount,
    analyze_snapshot,
    build_host_hypothesis_report,
    collect_host_evidence,
    load_host_input,
    write_host_hypothesis_outputs,
    write_host_report_outputs,
)

FIXTURES = Path(__file__).parent / "fixtures" / "host"
runner = CliRunner()


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
    assert not (tmp_path / "host-report.pdf").exists()
    assert not (tmp_path / "host-dashboard").exists()


def test_host_report_format_enum_accepts_pdf_dashboard_all() -> None:
    assert HostReportFormat("pdf") is HostReportFormat.PDF
    assert HostReportFormat("dashboard") is HostReportFormat.DASHBOARD
    assert HostReportFormat("all") is HostReportFormat.ALL


def test_host_report_writes_pdf_dashboard_and_all_outputs(tmp_path: Path) -> None:
    report = analyze_snapshot(load_host_input(FIXTURES / "debian-vulnerable"))

    pdf_dir = tmp_path / "pdf"
    write_host_report_outputs(report, pdf_dir, report_format="pdf")
    pdf_bytes = (pdf_dir / "host-report.pdf").read_bytes()
    assert pdf_bytes.startswith(b"%PDF")
    assert len(pdf_bytes) > 200
    assert not (pdf_dir / "host-report.json").exists()
    assert not (pdf_dir / "host-report.md").exists()

    dashboard_dir = tmp_path / "dashboard"
    write_host_report_outputs(report, dashboard_dir, report_format="dashboard")
    assert (dashboard_dir / "host-dashboard" / "index.html").is_file()
    assert (dashboard_dir / "host-dashboard" / "host-report.json").is_file()
    assert (dashboard_dir / "host-dashboard" / "assets" / "host-dashboard.css").is_file()
    assert (dashboard_dir / "host-dashboard" / "assets" / "host-dashboard.js").is_file()
    assert not (dashboard_dir / "host-report.json").exists()

    all_dir = tmp_path / "all"
    write_host_report_outputs(report, all_dir, report_format="all")
    assert (all_dir / "host-report.json").is_file()
    assert (all_dir / "host-report.md").is_file()
    assert (all_dir / "host-report.pdf").read_bytes().startswith(b"%PDF")
    assert (all_dir / "host-dashboard" / "index.html").is_file()


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
    assert "piranesi init" not in result.output


def test_assess_cli_writes_all_host_outputs(tmp_path: Path) -> None:
    output_dir = tmp_path / "out"

    result = runner.invoke(
        app,
        [
            "assess",
            str(FIXTURES / "debian-vulnerable"),
            "--output",
            str(output_dir),
            "--format",
            "all",
        ],
    )

    assert result.exit_code == 0, result.stdout
    assert (output_dir / "host-report.json").is_file()
    assert (output_dir / "host-report.md").is_file()
    assert (output_dir / "host-report.pdf").read_bytes().startswith(b"%PDF")
    assert (output_dir / "host-dashboard" / "index.html").is_file()
    assert (output_dir / "host-dashboard" / "assets" / "host-dashboard.css").is_file()
    assert (output_dir / "host-dashboard" / "assets" / "host-dashboard.js").is_file()


def test_deterministic_host_hypotheses_from_vulnerable_fixture() -> None:
    report = build_host_hypothesis_report(load_host_input(FIXTURES / "debian-vulnerable"))

    titles = {hypothesis.title for hypothesis in report.hypotheses}

    assert report.target == "debian-vm-01"
    assert any("Public SSH with password authentication" in title for title in titles)
    assert any("Public Redis exposure" in title for title in titles)
    assert any("CVE-2023-0464" in title for title in titles)
    assert all(hypothesis.must_not_treat_as_finding for hypothesis in report.hypotheses)
    assert all(hypothesis.supporting_evidence for hypothesis in report.hypotheses)
    assert all(hypothesis.missing_evidence for hypothesis in report.hypotheses)


def test_clean_fixture_produces_no_host_hypotheses() -> None:
    report = build_host_hypothesis_report(
        load_host_input(FIXTURES / "debian-clean" / "host_snapshot.json")
    )

    assert report.target == "debian-clean-01"
    assert report.hypotheses == []


def test_weak_kernel_hardening_hypothesis_requires_public_service_and_patch_gap() -> None:
    snapshot = HostSnapshot(
        identity=HostIdentity(hostname="kernel-gap-vm"),
        listening_ports=[
            ListeningPort(protocol="tcp", address="8.8.8.8", port=8080, process="app")
        ],
        config={"sysctl": {"values": {"kernel.kptr_restrict": "0"}}},
    )

    report = build_host_hypothesis_report(snapshot)

    assert any(
        "Weak kernel hardening" in hypothesis.title
        for hypothesis in report.hypotheses
    )
    hypothesis = next(
        item for item in report.hypotheses if "Weak kernel hardening" in item.title
    )
    assert hypothesis.hypothesis_type == "novel_attack_path"
    assert hypothesis.must_not_treat_as_finding is True


def test_llm_hypothesis_validation_rejects_missing_supporting_evidence() -> None:
    provider = _CapturingHostProvider(
        json.dumps(
            {
                "hypotheses": [
                    {
                        "title": "Unsupported hypothesis",
                        "hypothesis_type": "novel_attack_path",
                        "confidence": 0.6,
                        "severity_if_true": "high",
                        "missing_evidence": ["required evidence"],
                        "reasoning_summary": "This omits supporting evidence keys.",
                    }
                ]
            }
        )
    )

    report = build_host_hypothesis_report(
        _sensitive_llm_snapshot(),
        provider=provider,  # type: ignore[arg-type]
    )

    assert report.analysis_modes == ["deterministic", "llm"]
    assert report.hypotheses == []
    assert report.llm_redaction is not None
    assert report.llm_redaction.applied is True
    assert "prod-app-01" not in provider.user_prompt
    assert "deployer" not in provider.user_prompt
    assert "[HOSTNAME_1]" in provider.user_prompt
    assert "[USER_1]" in provider.user_prompt


def test_host_hypotheses_do_not_change_posture_scoring() -> None:
    snapshot = load_host_input(FIXTURES / "debian-vulnerable")

    posture_before = analyze_snapshot(snapshot)
    hypothesis_report = build_host_hypothesis_report(snapshot)
    posture_after = analyze_snapshot(snapshot)

    assert hypothesis_report.hypotheses
    assert posture_before.summary["findings_total"] == len(posture_before.findings)
    assert posture_after.summary["findings_total"] == posture_before.summary["findings_total"]
    assert posture_after.posture_score == posture_before.posture_score
    assert not hasattr(posture_after, "hypotheses")


def test_host_hypothesis_outputs_write_json_and_markdown(tmp_path: Path) -> None:
    report = build_host_hypothesis_report(load_host_input(FIXTURES / "debian-vulnerable"))

    write_host_hypothesis_outputs(report, tmp_path)

    payload = json.loads((tmp_path / "host-hypotheses.json").read_text(encoding="utf-8"))
    markdown = (tmp_path / "host-hypotheses.md").read_text(encoding="utf-8")

    assert payload["target"] == "debian-vm-01"
    assert payload["hypotheses"]
    assert "Hypotheses are not confirmed findings" in markdown
    assert "fail-severity" in markdown
    assert "Supporting Evidence" in markdown


def test_hypothesize_cli_writes_host_hypothesis_outputs(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    input_dir = (FIXTURES / "debian-vulnerable").resolve()
    for name in (
        "OPENAI_API_KEY",
        "ANTHROPIC_API_KEY",
        "OPENROUTER_API_KEY",
        "AZURE_OPENAI_API_KEY",
        "GEMINI_API_KEY",
        "GOOGLE_API_KEY",
        "LITELLM_API_KEY",
    ):
        monkeypatch.delenv(name, raising=False)
    monkeypatch.chdir(tmp_path)
    output_dir = tmp_path / "hypotheses"

    result = runner.invoke(
        app,
        [
            "hypothesize",
            str(input_dir),
            "--output",
            str(output_dir),
        ],
    )

    assert result.exit_code == 0, result.stdout
    assert (output_dir / "host-hypotheses.json").is_file()
    assert (output_dir / "host-hypotheses.md").is_file()
    payload = json.loads((output_dir / "host-hypotheses.json").read_text(encoding="utf-8"))
    assert payload["hypotheses"]
    assert "findings_impact: none" in result.output


def test_host_finding_ids_are_stable_for_service_port_and_sysctl_value_changes(
    tmp_path: Path,
) -> None:
    _write_minimal_raw_bundle(tmp_path)
    snapshot = load_host_input(tmp_path)
    redis_6379 = snapshot.model_copy(
        update={
            "listening_ports": [
                ListeningPort(
                    protocol="tcp",
                    address="0.0.0.0",
                    port=6379,
                    process="redis-server",
                    pid=944,
                )
            ],
            "config": {"sysctl": {"values": {"net.ipv4.ip_forward": "1"}}},
        },
        deep=True,
    )
    redis_6380 = redis_6379.model_copy(
        update={
            "listening_ports": [
                ListeningPort(
                    protocol="tcp",
                    address="0.0.0.0",
                    port=6380,
                    process="redis-server",
                    pid=944,
                )
            ],
            "config": {"sysctl": {"values": {"net.ipv4.ip_forward": "true"}}},
        },
        deep=True,
    )

    first = analyze_snapshot(redis_6379)
    second = analyze_snapshot(redis_6380)

    first_redis = next(finding for finding in first.findings if "Redis" in finding.title)
    second_redis = next(finding for finding in second.findings if "Redis" in finding.title)
    assert first_redis.id == second_redis.id
    assert first_redis.rule_id == "host.listener.high_risk_service"
    first_sysctl = next(
        finding
        for finding in first.findings
        if finding.affected_component == "net.ipv4.ip_forward"
    )
    assert first_sysctl.id == "host-" + first_sysctl.id.removeprefix("host-")
    assert first_sysctl.instance_key == "net.ipv4.ip_forward"


def test_host_cve_and_privileged_user_ids_are_scoped_by_instance() -> None:
    report = analyze_snapshot(load_host_input(FIXTURES / "debian-vulnerable"))

    cve_findings = [finding for finding in report.findings if finding.cve_ids]
    user_findings = [finding for finding in report.findings if finding.category == "identity"]

    assert len({finding.id for finding in cve_findings}) == len(cve_findings)
    assert all(finding.instance_key for finding in cve_findings)
    assert {finding.instance_key for finding in user_findings} == {"deployer"}


def test_assess_applies_host_id_suppression_and_fail_severity_ignores_it(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.chdir(tmp_path)
    fixture_report = analyze_snapshot(load_host_input(FIXTURES / "debian-vulnerable"))
    high_findings = [finding for finding in fixture_report.findings if finding.severity == "high"]
    (tmp_path / ".piranesi-ignore").write_text(
        "suppressions:\n"
        + "".join(
            f"  - id: {finding.id}\n    reason: accepted risk\n"
            for finding in high_findings
        ),
        encoding="utf-8",
    )
    output_dir = tmp_path / "out"

    result = runner.invoke(
        app,
        [
            "assess",
            str(FIXTURES / "debian-vulnerable"),
            "--output",
            str(output_dir),
            "--fail-severity",
            "high",
        ],
    )

    assert result.exit_code == 0, result.stdout
    payload = json.loads((output_dir / "host-report.json").read_text(encoding="utf-8"))
    suppressed = next(
        finding for finding in payload["findings"] if finding["id"] == high_findings[0].id
    )
    assert suppressed["suppressed"] is True
    assert suppressed["suppression_reason"] == "accepted risk"


def test_assess_fail_severity_and_no_fail_for_host_findings(tmp_path: Path) -> None:
    output_dir = tmp_path / "out"

    fail = runner.invoke(
        app,
        [
            "assess",
            str(FIXTURES / "debian-vulnerable"),
            "--output",
            str(output_dir),
            "--fail-severity",
            "high",
        ],
    )
    no_fail = runner.invoke(
        app,
        [
            "assess",
            str(FIXTURES / "debian-vulnerable"),
            "--output",
            str(output_dir),
            "--fail-severity",
            "high",
            "--no-fail",
        ],
    )

    assert fail.exit_code == 1
    assert no_fail.exit_code == 0, no_fail.stdout


def test_assess_invalid_host_suppression_yaml_exits_2(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.chdir(tmp_path)
    (tmp_path / ".piranesi-ignore").write_text("suppressions: [", encoding="utf-8")

    result = runner.invoke(app, ["assess", str(FIXTURES / "debian-clean" / "host_snapshot.json")])

    assert result.exit_code == 2


def test_public_listener_classification_defaults_and_private_override(tmp_path: Path) -> None:
    _write_minimal_raw_bundle(tmp_path)
    snapshot = load_host_input(tmp_path)
    addresses = [
        "127.0.0.1",
        "10.0.0.5",
        "172.16.0.5",
        "192.168.1.5",
        "169.254.1.5",
        "fc00::1",
        "0.0.0.0",
        "::",
        "8.8.8.8",
        "2001:4860:4860::8888",
    ]
    snapshot = snapshot.model_copy(
        update={
            "listening_ports": [
                ListeningPort(
                    protocol="tcp",
                    address=address,
                    port=6379,
                    process=f"redis-{index}",
                )
                for index, address in enumerate(addresses)
            ]
        },
        deep=True,
    )

    default_report = analyze_snapshot(snapshot)
    lab_report = analyze_snapshot(snapshot, treat_private_as_public=True)

    default_addresses = {
        finding.evidence[0].value.removeprefix("tcp/").split(":6379", 1)[0]
        for finding in default_report.findings
        if finding.title == "Redis is listening on a public interface"
    }
    lab_addresses = {
        finding.evidence[0].value.removeprefix("tcp/").split(":6379", 1)[0]
        for finding in lab_report.findings
        if finding.title == "Redis is listening on a public interface"
    }
    assert default_addresses == {"0.0.0.0", "::", "8.8.8.8", "2001:4860:4860::8888"}
    assert {"10.0.0.5", "172.16.0.5", "192.168.1.5", "fc00::1"} <= lab_addresses


def test_explain_and_validate_evidence_support_host_report(tmp_path: Path) -> None:
    output_dir = tmp_path / "out"
    report = analyze_snapshot(load_host_input(FIXTURES / "debian-vulnerable"))
    write_host_report_outputs(report, output_dir, report_format="json")
    finding_id = report.findings[0].id

    explain = runner.invoke(app, ["explain", finding_id, "--output", str(output_dir)])
    validate = runner.invoke(app, ["validate-evidence", str(output_dir)])

    assert explain.exit_code == 0, explain.stdout
    assert "Piranesi Host Finding Explanation" in explain.stdout
    assert validate.exit_code == 0, validate.stdout
    assert "Valid: yes" in validate.stdout


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
    assert not any(
        command["name"]
        in {"who_sessions", "last_logins", "lastb_failures", "journalctl_sshd_auth_summary"}
        for command in manifest["commands"]
    )


def test_collect_auth_evidence_is_opt_in_and_redacted(tmp_path: Path) -> None:
    result = collect_host_evidence(
        tmp_path,
        include_trivy=False,
        include_auth_evidence=True,
        executable_lookup=_fake_lookup_with_auth_tools,
        command_runner=_fake_runner_with_auth_evidence,
    )

    assert result.snapshot.auth_event_summaries
    manifest = json.loads((tmp_path / "collection-manifest.json").read_text(encoding="utf-8"))
    assert any(
        command["name"] == "journalctl_sshd_auth_summary" and command["status"] == "ok"
        for command in manifest["commands"]
    )
    payload = json.loads(
        (tmp_path / "raw" / "commands" / "journalctl_sshd_auth_summary.json").read_text(
            encoding="utf-8"
        )
    )
    assert "supersecret" not in payload["stdout"]
    assert "[REDACTED]" in payload["stdout"]
    assert len(payload["stdout"].splitlines()) <= 121


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
    assert report.llm_redaction is not None
    assert report.llm_redaction.applied is False


def test_host_llm_prompt_is_redacted_and_preserves_package_service_names() -> None:
    provider = _CapturingHostProvider(
        json.dumps(
            {
                "findings": [
                    {
                        "title": "Review privileged account exposure",
                        "category": "identity",
                        "severity": "low",
                        "confidence": 0.7,
                        "affected_component": "[USER_1]",
                        "evidence_keys": ["user:[USER_1]"],
                        "remediation": "Review account access.",
                        "rationale": "Evidence key references a redacted user placeholder.",
                    }
                ]
            }
        )
    )

    report = analyze_snapshot(
        _sensitive_llm_snapshot(),
        analysis="both",
        provider=provider,  # type: ignore[arg-type]
    )
    prompt = provider.user_prompt

    assert "prod-app-01" not in prompt
    assert "[HOSTNAME_1]" in prompt
    assert "deployer" not in prompt
    assert "[USER_1]" in prompt
    assert "10.0.0.7" not in prompt
    assert "8.8.8.8" not in prompt
    assert "[PRIVATE_IP_1]" in prompt
    assert "[PUBLIC_IP_1]" in prompt
    assert "aa:bb:cc:dd:ee:ff" not in prompt
    assert "[MAC_1]" in prompt
    assert "/home/deployer" not in prompt
    assert "[HOME_PATH_1]" in prompt
    assert "sk-prod-secret" not in prompt
    assert "[SECRET]" in prompt
    assert "openssl" in prompt
    assert "redis.service" in prompt

    assert report.llm_redaction is not None
    assert report.llm_redaction.applied is True
    assert report.llm_redaction.redacted_value_count >= 7
    assert report.llm_redaction.categories["hostname"] >= 1
    assert report.llm_redaction.categories["user"] >= 1
    assert report.llm_redaction.categories["private_ip"] >= 1
    assert report.llm_redaction.categories["public_ip"] >= 1
    assert report.llm_redaction.categories["secret"] >= 1

    llm_finding = next(finding for finding in report.findings if finding.source_tool == "llm")
    assert llm_finding.evidence[0].value == "deployer"


def test_deterministic_host_assess_does_not_attach_llm_redaction() -> None:
    report = analyze_snapshot(_sensitive_llm_snapshot())

    assert report.analysis_modes == ["deterministic"]
    assert report.llm_redaction is None


def test_host_llm_trace_contains_redacted_prompt_only(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    import litellm

    from piranesi.config import BudgetConfig, ModelsConfig, PiranesiConfig, TraceConfig
    from piranesi.llm.cost import CostTracker
    from piranesi.llm.provider import LLMProvider
    from piranesi.llm.router import ModelRouter
    from piranesi.llm.trace import TraceLogger
    from piranesi.trace import TraceWriter

    trace_path = tmp_path / "llm-trace.jsonl"
    config = PiranesiConfig(
        models=ModelsConfig(triage="openai/gpt-4o-mini"),
        trace=TraceConfig(file_path=str(trace_path), log_prompts=True),
        budget=BudgetConfig(max_cost_usd=5.0),
    )
    cost_tracker = CostTracker()
    router = ModelRouter(config, cost_tracker)
    writer = TraceWriter(config.trace, config.budget)
    provider = LLMProvider(TraceLogger(writer, log_prompts=True), cost_tracker, router=router)

    def _completion(*, model: str, messages: list[dict[str, str]], **kwargs: object) -> object:
        return litellm.mock_completion(
            model=model,
            messages=messages,
            mock_response='{"findings":[]}',
            **kwargs,
        )

    monkeypatch.setattr("piranesi.llm.provider.litellm.completion", _completion)
    try:
        analyze_snapshot(_sensitive_llm_snapshot(), analysis="llm", provider=provider)
    finally:
        writer.close()

    trace_prompt = json.loads(trace_path.read_text(encoding="utf-8").splitlines()[0])["prompt"]
    assert "prod-app-01" not in trace_prompt
    assert "deployer" not in trace_prompt
    assert "10.0.0.7" not in trace_prompt
    assert "sk-prod-secret" not in trace_prompt
    assert "[HOSTNAME_1]" in trace_prompt
    assert "[USER_1]" in trace_prompt
    assert "[SECRET]" in trace_prompt


class _FakeLlmResponse:
    def __init__(self, content: str) -> None:
        self.content = content


class _CapturingHostProvider:
    def __init__(self, content: str) -> None:
        self.content = content
        self.messages: list[list[dict[str, str]]] = []

    def complete(self, **kwargs: object) -> _FakeLlmResponse:
        messages = kwargs["messages"]
        assert isinstance(messages, list)
        self.messages.append(messages)
        return _FakeLlmResponse(self.content)

    @property
    def user_prompt(self) -> str:
        return self.messages[0][1]["content"]


def _sensitive_llm_snapshot() -> HostSnapshot:
    return HostSnapshot(
        identity=HostIdentity(
            hostname="prod-app-01",
            host_id="host-prod-id",
            ip_addresses=["10.0.0.7", "8.8.8.8"],
        ),
        os=OsRelease(name="Ubuntu", version="24.04", id="ubuntu"),
        kernel="6.8.0-prod",
        packages=[
            HostPackage(name="openssl", version="3.0.13-0ubuntu3.1", source="osquery"),
            HostPackage(name="openssh-server", version="1:9.6p1", source="osquery"),
        ],
        network_interfaces=[
            NetworkInterface(name="ens3", address="10.0.0.7", family="ipv4"),
            NetworkInterface(name="eth1", address="8.8.8.8", family="ipv4"),
        ],
        listening_ports=[
            ListeningPort(protocol="tcp", address="10.0.0.7", port=6379, process="redis-server"),
            ListeningPort(protocol="tcp", address="8.8.8.8", port=22, process="sshd"),
        ],
        processes=[
            HostProcess(
                pid=4242,
                name="custom-agent",
                path="/home/deployer/bin/custom-agent",
                cmdline=(
                    "/home/deployer/bin/custom-agent --api-key=sk-prod-secret "
                    "--remote 8.8.8.8 --mac aa:bb:cc:dd:ee:ff"
                ),
                user="deployer",
            )
        ],
        services=[
            ServiceState(name="redis.service", enabled=True, running=True, source="osquery"),
            ServiceState(name="ssh.service", enabled=True, running=True, source="osquery"),
        ],
        users=[
            UserAccount(username="root", uid=0, gid=0, shell="/bin/bash", groups=["root"]),
            UserAccount(
                username="deployer",
                uid=1001,
                gid=1001,
                shell="/bin/bash",
                groups=["sudo"],
            ),
        ],
    )


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


def _fake_lookup_with_auth_tools(name: str) -> str | None:
    if name in {"osqueryi", "who", "last", "lastb", "journalctl"}:
        return f"/usr/bin/{name}"
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


def _fake_runner_with_auth_evidence(
    args: object,
    *,
    capture_output: bool,
    text: bool,
    timeout: int,
) -> subprocess.CompletedProcess[str]:
    command = list(args) if isinstance(args, list | tuple) else [str(args)]
    executable = Path(str(command[0])).name
    if executable == "who":
        return subprocess.CompletedProcess(
            command,
            0,
            stdout="admin pts/0 2026-05-14 10:00 (192.0.2.10)\n",
            stderr="",
        )
    if executable == "last":
        return subprocess.CompletedProcess(
            command,
            0,
            stdout="admin pts/0 192.0.2.10 Thu May 14 10:00 still logged in\n",
            stderr="",
        )
    if executable == "lastb":
        return subprocess.CompletedProcess(
            command,
            0,
            stdout="root ssh:notty 203.0.113.20 Thu May 14 10:01 - 10:01 (00:00)\n",
            stderr="",
        )
    if executable == "journalctl":
        stdout = "\n".join(
            [
                (
                    "May 14 host sshd[1]: Failed password for root from 203.0.113.20 "
                    "port 22 ssh2 password=supersecret"
                )
                for _ in range(130)
            ]
        )
        return subprocess.CompletedProcess(command, 0, stdout=stdout, stderr="")
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


# ---------------------------------------------------------------------------
# Baseline (Lynis / OpenSCAP) tests
# ---------------------------------------------------------------------------

BASELINE_FIXTURES = FIXTURES / "baseline"


def test_load_lynis_only_evidence_and_analyze() -> None:
    snapshot = load_host_input(BASELINE_FIXTURES)
    lynis_checks = [c for c in snapshot.baseline_checks if c.source == "lynis"]

    assert len(lynis_checks) > 0
    assert any(c.check_id == "LYNIS-HARDENING-INDEX" for c in lynis_checks)
    assert any(c.result == "warn" for c in lynis_checks)
    assert any(c.result == "fail" for c in lynis_checks)  # suggestions -> fail

    report = analyze_snapshot(snapshot)
    titles = {f.title for f in report.findings}
    assert any("hardening index" in t.lower() for t in titles)


def test_lynis_hardening_index_allows_non_numeric_values(tmp_path: Path) -> None:
    lynis_dir = tmp_path / "lynis"
    lynis_dir.mkdir()
    (lynis_dir / "report.dat").write_text(
        "hardening_index=unknown\nwarning[]=AUTH-9328|SSH root login permitted|-\n",
        encoding="utf-8",
    )

    snapshot = load_host_input(tmp_path)
    hardening = next(
        check for check in snapshot.baseline_checks if check.check_id == "LYNIS-HARDENING-INDEX"
    )
    assert hardening.result == "unknown"
    report = analyze_snapshot(snapshot)
    assert any(finding.category == "baseline" for finding in report.findings)


def test_load_openscap_only_evidence_and_analyze(tmp_path: Path) -> None:
    osquery_dir = tmp_path / "osquery"
    openscap_dir = tmp_path / "openscap"
    osquery_dir.mkdir()
    openscap_dir.mkdir()
    (osquery_dir / "system_info.json").write_text(
        json.dumps([{"hostname": "scap-vm"}]), encoding="utf-8"
    )
    import shutil
    shutil.copy2(BASELINE_FIXTURES / "openscap" / "results.xml", openscap_dir / "results.xml")

    snapshot = load_host_input(tmp_path)
    scap_checks = [c for c in snapshot.baseline_checks if c.source == "openscap"]

    assert len(scap_checks) == 5
    assert any(c.result == "pass" for c in scap_checks)
    assert any(c.result == "fail" for c in scap_checks)
    assert any(c.result == "not_applicable" for c in scap_checks)


def test_combined_osquery_lynis_openscap_bundle() -> None:
    snapshot = load_host_input(BASELINE_FIXTURES)

    assert snapshot.identity.hostname == "baseline-vm"
    lynis_checks = [c for c in snapshot.baseline_checks if c.source == "lynis"]
    scap_checks = [c for c in snapshot.baseline_checks if c.source == "openscap"]
    assert len(lynis_checks) > 0
    assert len(scap_checks) > 0
    assert "lynis" in snapshot.raw_evidence
    assert "openscap" in snapshot.raw_evidence


def test_failed_baseline_checks_become_findings() -> None:
    snapshot = load_host_input(BASELINE_FIXTURES)
    report = analyze_snapshot(snapshot)

    baseline_findings = [f for f in report.findings if f.category == "baseline"]
    assert len(baseline_findings) > 0
    assert all(f.source_tool in {"lynis", "openscap"} for f in baseline_findings)
    assert all(f.evidence for f in baseline_findings)
    assert all(f.remediation for f in baseline_findings)


def test_passed_baseline_checks_do_not_become_findings() -> None:
    snapshot = load_host_input(BASELINE_FIXTURES)
    report = analyze_snapshot(snapshot)

    baseline_finding_ids = {
        f.instance_key for f in report.findings if f.category == "baseline"
    }
    passed_ids = {
        f"{c.source}:{c.check_id}"
        for c in snapshot.baseline_checks
        if c.result == "pass"
    }
    assert not (baseline_finding_ids & passed_ids)


def test_openscap_control_refs_preserved() -> None:
    snapshot = load_host_input(BASELINE_FIXTURES)
    report = analyze_snapshot(snapshot)

    scap_findings = [
        f for f in report.findings
        if f.source_tool == "openscap" and f.category == "baseline"
    ]
    refs_found = set()
    for f in scap_findings:
        refs_found.update(f.control_refs)
    assert any("CCE-" in r or "CIS-" in r for r in refs_found)


def test_missing_optional_baseline_tools_create_health_warnings(tmp_path: Path) -> None:
    _write_minimal_raw_bundle(tmp_path)
    _write_manifest(
        tmp_path,
        [
            {"tool": "osquery", "name": "system_info", "status": "ok"},
            {"tool": "lynis", "name": "audit_system", "status": "missing"},
            {"tool": "openscap", "name": "xccdf_eval", "status": "missing"},
        ],
    )

    report = analyze_snapshot(load_host_input(tmp_path))

    assert report.collection_health is not None
    assert "lynis" in report.collection_health.optional
    assert report.collection_health.optional["lynis"].status == "warn"
    assert "openscap" in report.collection_health.optional
    assert report.collection_health.optional["openscap"].status == "warn"
    # missing tools must NOT produce false baseline findings
    baseline_findings = [f for f in report.findings if f.category == "baseline"]
    assert len(baseline_findings) == 0


def test_baseline_top_actions_group() -> None:
    snapshot = load_host_input(BASELINE_FIXTURES)
    report = analyze_snapshot(snapshot)

    categories = {action["category"] for action in report.top_actions}
    assert "baseline" in categories


def test_assess_cli_baseline_fixture(tmp_path: Path) -> None:
    output_dir = tmp_path / "out"
    result = runner.invoke(
        app,
        [
            "assess",
            str(BASELINE_FIXTURES),
            "--output",
            str(output_dir),
            "--format",
            "both",
        ],
    )

    assert result.exit_code == 0, result.stdout
    assert (output_dir / "host-report.json").is_file()
    payload = json.loads((output_dir / "host-report.json").read_text(encoding="utf-8"))
    assert payload["target"] == "baseline-vm"
    assert any(
        f["category"] == "baseline" for f in payload["findings"]
    )


# ---------------------------------------------------------------------------
# Auth Evidence Tests
# ---------------------------------------------------------------------------

AUTH_FIXTURES = FIXTURES / "auth-evidence"

def test_auth_evidence_parsing_and_findings():
    from piranesi.host.analyze import deterministic_findings
    from piranesi.host.ingest import load_host_input

    snapshot = load_host_input(AUTH_FIXTURES)

    assert len(snapshot.login_sessions) == 1
    session = snapshot.login_sessions[0]
    assert session.username == "admin"
    assert session.source == "192.168.1.100"

    assert sum(e.count for e in snapshot.auth_event_summaries if e.event_type == "login_failure") == 51
    assert any(e.event_type == "ssh_root_login" for e in snapshot.auth_event_summaries)
    assert any(e.event_type == "sudo_command" for e in snapshot.auth_event_summaries)

    findings = deterministic_findings(snapshot)

    finding_ids = [f.rule_id for f in findings]
    assert "host.auth.ssh_failed_password_spike" in finding_ids
    assert "host.auth.root_login_attempts" in finding_ids
    assert "host.auth.active_privileged_session" in finding_ids
    assert "host.auth.compound_ssh_brute_force" in finding_ids
    assert "host.auth.sudo_activity_present" in finding_ids


def test_auth_redaction_removes_secrets():
    from piranesi.host.ingest import redact_auth_value

    sudo_log = "admin : TTY=pts/0 ; PWD=/home/admin ; USER=root ; COMMAND=/usr/bin/cat /etc/shadow password=mysecret"
    redacted = redact_auth_value(sudo_log)
    assert "mysecret" not in redacted
    assert "[REDACTED]" in redacted

    aws_key = "AWS_SECRET AWS_ACCESS_KEY_REDACTED"
    redacted = redact_auth_value(aws_key)
    assert "AWS_ACCESS_KEY_REDACTED" not in redacted
    assert "[REDACTED]" in redacted


def test_missing_auth_logs_creates_warning_not_finding():
    from piranesi.host.analyze import deterministic_findings
    from piranesi.host.ingest import load_host_input

    snapshot = load_host_input(AUTH_FIXTURES)

    # Simulate missing journalctl command
    snapshot.raw_evidence["commands"].pop("journalctl_sshd_auth_summary", None)

    # Simulate timeout status in manifest
    manifest = snapshot.raw_evidence.get("collection_manifest", {})
    manifest["commands"] = {"journalctl_sshd_auth_summary": "timeout"}

    findings = deterministic_findings(snapshot)

    # Missing auth logs should not create a finding
    assert not any(f.rule_id == "host.evidence.missing" and f.affected_component == "auth_evidence" for f in findings)


# ---------------------------------------------------------------------------
# Adaptive probing tests
# ---------------------------------------------------------------------------


def test_probe_plan_from_vulnerable_fixture() -> None:
    from piranesi.host.probe import generate_probe_plan

    snapshot = load_host_input(FIXTURES / "debian-vulnerable")
    report = analyze_snapshot(snapshot)
    plan = generate_probe_plan(snapshot, report.findings)

    assert plan.target == "debian-vm-01"
    assert len(plan.probes) > 0
    probe_ids = {p.id for p in plan.probes}
    # SSH exposure + password auth should suggest auth probes
    assert "followup.ssh.last_logins" in probe_ids
    assert "followup.ssh.lastb_failures" in probe_ids
    # Redis exposure should suggest service probes
    assert "followup.redis.process_detail" in probe_ids
    assert "followup.redis.service_unit" in probe_ids
    # Privileged user should suggest identity probes
    assert "followup.identity.sudoers" in probe_ids
    # All probes should reference finding IDs
    for p in plan.probes:
        assert len(p.finding_ids) > 0


def test_clean_fixture_produces_no_probes(tmp_path: Path) -> None:
    from piranesi.host.probe import generate_probe_plan

    osquery_dir = tmp_path / "osquery"
    osquery_dir.mkdir()
    (osquery_dir / "system_info.json").write_text(
        json.dumps([{"hostname": "clean-vm"}]), encoding="utf-8"
    )
    snapshot = load_host_input(tmp_path)
    report = analyze_snapshot(snapshot)
    plan = generate_probe_plan(snapshot, report.findings)

    assert plan.target == "clean-vm"
    # no SSH/Redis/firewall exposure findings -> minimal probes
    probe_ids = {p.id for p in plan.probes}
    assert "followup.ssh.last_logins" not in probe_ids
    assert "followup.redis.process_detail" not in probe_ids


def test_executor_rejects_unknown_probe_ids(tmp_path: Path) -> None:
    from piranesi.host.models import ProbePlan
    from piranesi.host.probe import execute_probe_plan

    plan = ProbePlan(
        target="test-vm",
        probes=[
            {
                "id": "evil.arbitrary.command",
                "reason": "Malicious probe",
                "capability": "exploit",
                "command": ["rm", "-rf", "/"],
                "output_name": "evil_output",
            }
        ],
    )
    result = execute_probe_plan(plan, tmp_path)

    assert result.rejected == 1
    assert result.executed == 0
    # verify manifest records the rejection
    assert any(
        "REJECTED" in (cmd.stderr or "")
        for cmd in result.manifest.commands
    )


def test_executor_rejects_modified_command(tmp_path: Path) -> None:
    from piranesi.host.models import ProbePlan
    from piranesi.host.probe import execute_probe_plan

    plan = ProbePlan(
        target="test-vm",
        probes=[
            {
                "id": "followup.ssh.last_logins",
                "reason": "tampered",
                "capability": "auth",
                "command": ["last", "-n", "9999", ";", "cat", "/etc/shadow"],
                "output_name": "last_logins",
            }
        ],
    )
    result = execute_probe_plan(plan, tmp_path)

    assert result.rejected == 1
    assert result.executed == 0


def test_executor_writes_manifest_and_followup_dir(tmp_path: Path) -> None:
    from piranesi.host.models import ProbePlan
    from piranesi.host.probe import execute_probe_plan

    def _fake_lookup(name: str) -> str | None:
        return f"/usr/bin/{name}"

    def _fake_runner(
        args: object,
        *,
        capture_output: bool,
        text: bool,
        timeout: int,
    ) -> subprocess.CompletedProcess[str]:
        command = list(args) if isinstance(args, (list, tuple)) else [str(args)]
        return subprocess.CompletedProcess(command, 0, stdout="test output\n", stderr="")

    plan = ProbePlan(
        target="test-vm",
        probes=[
            {
                "id": "followup.ssh.last_logins",
                "reason": "test",
                "capability": "auth",
                "command": ["last", "-n", "25"],
                "output_name": "last_logins",
            }
        ],
    )
    result = execute_probe_plan(
        plan, tmp_path, executable_lookup=_fake_lookup, command_runner=_fake_runner
    )

    assert result.executed == 1
    assert result.rejected == 0
    assert (tmp_path / "collection-manifest.json").is_file()
    assert (tmp_path / "raw" / "followup" / "probe-plan.json").is_file()
    assert (tmp_path / "raw" / "followup" / "probe-results.json").is_file()
    assert (tmp_path / "raw" / "followup" / "last_logins.json").is_file()
    # verify manifest has the executed command
    manifest = json.loads((tmp_path / "collection-manifest.json").read_text(encoding="utf-8"))
    assert any(cmd["name"] == "last_logins" and cmd["status"] == "ok" for cmd in manifest["commands"])


def test_probe_plan_is_json_serializable() -> None:
    from piranesi.host.probe import generate_probe_plan

    snapshot = load_host_input(FIXTURES / "debian-vulnerable")
    report = analyze_snapshot(snapshot)
    plan = generate_probe_plan(snapshot, report.findings)

    serialized = plan.model_dump_json(indent=2)
    roundtrip = json.loads(serialized)
    assert roundtrip["target"] == "debian-vm-01"
    assert len(roundtrip["probes"]) > 0
    assert roundtrip["schema_version"] == 1


def test_all_allowed_probes_have_command_or_osquery() -> None:
    from piranesi.host.probe import ALLOWED_PROBES

    for probe_id, probe in ALLOWED_PROBES.items():
        assert probe.command is not None or probe.osquery is not None, (
            f"probe {probe_id} has neither command nor osquery"
        )


def test_probe_cli_generates_plan(tmp_path: Path) -> None:
    plan_path = tmp_path / "plan.json"
    result = runner.invoke(
        app,
        [
            "probe",
            str(FIXTURES / "debian-vulnerable"),
            "--output",
            str(plan_path),
        ],
    )

    assert result.exit_code == 0, result.stdout
    assert plan_path.is_file()
    payload = json.loads(plan_path.read_text(encoding="utf-8"))
    assert payload["target"] == "debian-vm-01"
    assert len(payload["probes"]) > 0


def test_collect_followup_cli_rejects_bad_plan(tmp_path: Path) -> None:
    bad_plan = tmp_path / "bad.json"
    bad_plan.write_text('{"invalid": true}', encoding="utf-8")
    result = runner.invoke(
        app,
        [
            "collect-followup",
            str(bad_plan),
            "--output",
            str(tmp_path / "out"),
        ],
    )

    assert result.exit_code == 2


def test_collect_followup_output_can_be_reassessed(tmp_path: Path) -> None:
    from piranesi.host.models import ProbePlan

    plan_path = tmp_path / "probe-plan.json"
    followup_dir = tmp_path / "followup"
    assess_dir = tmp_path / "assessed"
    plan = ProbePlan(
        target="debian-vm-01",
        base_input=str((FIXTURES / "debian-vulnerable").resolve()),
        probes=[],
    )
    plan_path.write_text(plan.model_dump_json(indent=2), encoding="utf-8")

    collect_result = runner.invoke(
        app,
        [
            "collect-followup",
            str(plan_path),
            "--output",
            str(followup_dir),
        ],
    )
    assert collect_result.exit_code == 0, collect_result.stdout
    assert (followup_dir / "host_snapshot.json").is_file()
    assert (followup_dir / "raw" / "osquery" / "system_info.json").is_file()
    assert (followup_dir / "raw" / "followup" / "probe-plan.json").is_file()

    assess_result = runner.invoke(
        app,
        [
            "assess",
            str(followup_dir),
            "--output",
            str(assess_dir),
        ],
    )
    assert assess_result.exit_code == 0, assess_result.stdout
    assert (assess_dir / "host-report.json").is_file()
