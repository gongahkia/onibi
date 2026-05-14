from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.detect.suppression import SuppressionRule
from piranesi.host import (
    analyze_snapshot,
    apply_host_policy,
    apply_host_suppressions,
    assess_fleet_evidence,
    load_host_input,
    load_host_policy,
)
from piranesi.host.policy import HostPolicyError, evaluate_host_policy

FIXTURES = Path(__file__).parent / "fixtures" / "host"
FLEET_FIXTURES = Path(__file__).parent / "fixtures" / "fleet"
runner = CliRunner()


def test_policy_parser_validation(tmp_path: Path) -> None:
    policy = load_host_policy(Path("examples/policies/production-linux.toml"))

    assert policy.profile == "production-linux"
    assert policy.gates[0].id == "no-public-ssh-password-auth"
    assert policy.required_evidence[0].name == "trivy"

    invalid = tmp_path / "invalid-policy.toml"
    invalid.write_text(
        """
[host.policy]
profile = "broken"

[[host.policy.gates]]
id = "missing-matcher"
action = "fail"
""",
        encoding="utf-8",
    )

    try:
        load_host_policy(invalid)
    except HostPolicyError as exc:
        assert "gate must set" in str(exc)
    else:  # pragma: no cover - defensive guard.
        raise AssertionError("invalid policy parsed successfully")


def test_required_evidence_warning_does_not_fail_policy(tmp_path: Path) -> None:
    policy_path = tmp_path / "policy.toml"
    policy_path.write_text(
        """
[host.policy]
profile = "lab-evidence"

[[host.policy.required_evidence]]
name = "firewall"
required = true
action = "warn"
""",
        encoding="utf-8",
    )
    report = analyze_snapshot(load_host_input(FIXTURES / "debian-vulnerable"))

    result = evaluate_host_policy(report, load_host_policy(policy_path))

    assert result.passed is True
    assert result.required_evidence[0].name == "firewall"
    assert result.required_evidence[0].status == "warn"


def test_policy_fails_from_public_ssh_password_auth(tmp_path: Path) -> None:
    policy_path = tmp_path / "policy.toml"
    policy_path.write_text(
        """
[host.policy]
profile = "ssh-gate"

[[host.policy.gates]]
id = "no-public-ssh-password-auth"
rule_id = "host.ssh.password_authentication"
when = "public_ssh"
max_severity = "low"
action = "fail"
""",
        encoding="utf-8",
    )
    report = analyze_snapshot(load_host_input(FIXTURES / "debian-vulnerable"))

    policy_report = apply_host_policy(report, load_host_policy(policy_path))

    assert policy_report.policy_profile == "ssh-gate"
    assert policy_report.policy_summary["passed"] is False
    failed_gate = policy_report.policy_gate_results[0]
    assert failed_gate["gate_id"] == "no-public-ssh-password-auth"
    assert failed_gate["status"] == "fail"
    assert failed_gate["finding_ids"]


def test_suppressed_findings_do_not_fail_by_default(tmp_path: Path) -> None:
    policy_path = tmp_path / "policy.toml"
    policy_path.write_text(
        """
[host.policy]
profile = "suppression-aware"

[[host.policy.gates]]
id = "no-public-ssh-password-auth"
rule_id = "host.ssh.password_authentication"
when = "public_ssh"
max_severity = "low"
action = "fail"
""",
        encoding="utf-8",
    )
    report = analyze_snapshot(load_host_input(FIXTURES / "debian-vulnerable"))
    password_auth = next(
        finding
        for finding in report.findings
        if finding.rule_id == "host.ssh.password_authentication"
    )
    suppressed = apply_host_suppressions(
        report,
        [SuppressionRule(id=password_auth.id, reason="accepted lab exception")],
    )

    policy_report = apply_host_policy(suppressed, load_host_policy(policy_path))

    assert policy_report.policy_summary["passed"] is True
    assert policy_report.policy_gate_results[0]["status"] == "pass"


def test_fleet_level_policy_summary(tmp_path: Path) -> None:
    policy_path = tmp_path / "policy.toml"
    output_dir = tmp_path / "fleet-output"
    policy_path.write_text(
        """
[host.policy]
profile = "fleet-production"

[host.policy.fleet]
max_failed_hosts = 1
max_policy_failures = 0
minimum_passing_hosts_percent = 50

[[host.policy.gates]]
id = "no-public-ssh-password-auth"
rule_id = "host.ssh.password_authentication"
when = "public_ssh"
max_severity = "low"
action = "fail"
""",
        encoding="utf-8",
    )

    result = assess_fleet_evidence(
        FLEET_FIXTURES,
        output_dir,
        policy=load_host_policy(policy_path),
    )

    assert result.report.policy_profile == "fleet-production"
    assert result.report.policy_summary["passed"] is False
    assert result.report.policy_summary["host_policy_failures"] == 1
    assert any(
        gate["gate_id"] == "fleet-host-policy-failures" and gate["status"] == "fail"
        for gate in result.report.policy_gate_results
    )
    payload = json.loads((output_dir / "fleet-report.json").read_text(encoding="utf-8"))
    assert payload["policy_summary"]["host_policy_failures"] == 1


def test_policy_validate_cli_accepts_example_profile() -> None:
    result = runner.invoke(
        app,
        ["policy", "validate", "examples/policies/production-linux.toml"],
    )

    assert result.exit_code == 0
    assert "policy: production-linux" in result.stdout
