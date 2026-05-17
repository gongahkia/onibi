from __future__ import annotations

from pathlib import Path

from piranesi.infrastructure import (
    assess_container_image,
    assess_running_containers,
    load_container_image_snapshot,
    load_running_container_snapshots,
    parse_docker_inspect,
)

FIXTURES = Path(__file__).parent / "fixtures" / "container"


def test_trivy_image_json_ingestion() -> None:
    snapshot = load_container_image_snapshot(FIXTURES / "trivy-image.json")

    assert snapshot.image_ref == "registry.example/app:vulnerable"
    assert snapshot.packages[0].name == "openssl"
    assert snapshot.packages[0].vulnerability_id == "CVE-2024-0001"


def test_container_image_vulnerability_finding() -> None:
    snapshot = load_container_image_snapshot(FIXTURES / "trivy-image.json")
    report = assess_container_image(snapshot)

    assert report.surface == "container"
    assert "container.image.vulnerable_package" in {finding.rule_id for finding in report.findings}
    assert report.evidence_inventory["image_packages"] == 1


def test_docker_inspect_parsing() -> None:
    snapshots = load_running_container_snapshots(FIXTURES / "docker-inspect.json")
    parsed = parse_docker_inspect(snapshots[0].raw_evidence["docker_inspect"])

    assert snapshots[0].name == "risky-web"
    assert snapshots[0].privileged is True
    assert snapshots[0].network_mode == "host"
    assert parsed.mounts[0].destination == "/var/run/docker.sock"


def test_docker_container_list_parsing() -> None:
    snapshots = load_running_container_snapshots(FIXTURES / "docker-list.json")

    assert snapshots[0].container_id == "abcdef012345"
    assert snapshots[0].name == "risky-web"
    assert snapshots[0].ports == ["0.0.0.0:8080->8080/tcp"]


def test_privileged_container_finding() -> None:
    snapshots = load_running_container_snapshots(FIXTURES / "docker-inspect.json")
    report = assess_running_containers(snapshots)
    rule_ids = {finding.rule_id for finding in report.findings}

    assert "container.runtime.privileged" in rule_ids
    assert "container.runtime.host_network" in rule_ids
    assert "container.runtime.dangerous_mount" in rule_ids
    assert "container.runtime.runs_as_root" in rule_ids
