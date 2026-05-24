from __future__ import annotations

from pathlib import Path

from piranesi.host import analyze_snapshot, load_host_input

FIXTURES = Path(__file__).parent / "fixtures" / "host"


def test_platform_detection_for_rhel_alpine_and_amazon() -> None:
    rhel = analyze_snapshot(load_host_input(FIXTURES / "rhel-vulnerable"))
    alpine = analyze_snapshot(load_host_input(FIXTURES / "alpine-minimal"))
    amazon = analyze_snapshot(load_host_input(FIXTURES / "amazon-linux"))

    assert rhel.host_metadata["platform_family"] == "rhel"
    assert rhel.host_metadata["package_manager"] == "rpm"
    assert "firewalld" in rhel.host_metadata["supported_checks"]
    assert alpine.host_metadata["platform_family"] == "alpine"
    assert alpine.host_metadata["package_manager"] == "apk"
    assert amazon.host_metadata["platform_family"] == "amazon"
    assert amazon.host_metadata["package_manager"] == "rpm"


def test_rpm_and_apk_package_normalization_with_provenance() -> None:
    rhel_snapshot = load_host_input(FIXTURES / "rhel-vulnerable")
    alpine_snapshot = load_host_input(FIXTURES / "alpine-minimal")

    redis = next(package for package in rhel_snapshot.packages if package.name == "redis")
    openssl = next(package for package in alpine_snapshot.packages if package.name == "openssl")

    assert redis.version == "6.2.7-1.el9"
    assert redis.package_manager == "rpm"
    assert redis.source == "osquery"
    assert openssl.version == "3.1.4-r6"
    assert openssl.package_manager == "apk"


def test_platform_update_evidence_parsing() -> None:
    rhel_snapshot = load_host_input(FIXTURES / "rhel-vulnerable")
    alpine_snapshot = load_host_input(FIXTURES / "alpine-minimal")
    amazon_snapshot = load_host_input(FIXTURES / "amazon-linux")

    assert rhel_snapshot.config["updates"]["source"] == "dnf_security_updates"
    assert rhel_snapshot.config["updates"]["security_count"] == 2
    assert alpine_snapshot.config["updates"]["source"] == "apk_version_outdated"
    assert alpine_snapshot.config["updates"]["package_manager"] == "apk"
    assert amazon_snapshot.config["updates"]["source"] == "yum_security_updates"
    assert amazon_snapshot.config["updates"]["security_count"] == 1

    rhel_report = analyze_snapshot(rhel_snapshot)
    titles = {finding.title for finding in rhel_report.findings}
    assert "Security package updates are pending" in titles


def test_firewall_helper_selection_and_selinux_parsing() -> None:
    rhel_snapshot = load_host_input(FIXTURES / "rhel-vulnerable")
    amazon_snapshot = load_host_input(FIXTURES / "amazon-linux")

    assert rhel_snapshot.config["firewall"]["sources"] == ["firewalld_state"]
    assert rhel_snapshot.config["firewall"]["active"] is False
    assert rhel_snapshot.config["selinux"]["state"] == "Permissive"
    assert amazon_snapshot.config["firewall"]["active"] is True

    rhel_report = analyze_snapshot(rhel_snapshot)
    assert any(
        finding.title == "Firewall appears inactive while public services are exposed"
        for finding in rhel_report.findings
    )


def test_unsupported_checks_become_health_warnings_not_false_findings() -> None:
    alpine_report = analyze_snapshot(load_host_input(FIXTURES / "alpine-minimal"))

    assert alpine_report.collection_health is not None
    assert any(
        "unattended_upgrades is not supported" in warning
        for warning in alpine_report.collection_health.warnings
    )
    assert not any(
        finding.rule_id == "host.updates.unattended_upgrades_missing"
        for finding in alpine_report.findings
    )
    assert not any(
        finding.title == "Automatic security updates are not installed"
        for finding in alpine_report.findings
    )
