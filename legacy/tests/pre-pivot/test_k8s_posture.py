from __future__ import annotations

from pathlib import Path

from piranesi.infrastructure import assess_kubernetes_snapshot, load_kubernetes_snapshot

FIXTURES = Path(__file__).parent / "fixtures" / "k8s"


def test_kubernetes_manifest_parsing() -> None:
    snapshot = load_kubernetes_snapshot(FIXTURES)

    assert len(snapshot.workloads) == 1
    assert len(snapshot.services) == 1
    assert snapshot.workloads[0].containers[0].name == "web"
    assert snapshot.workloads[0].containers[0].env_secret_refs == ["db-secret"]


def test_privileged_kubernetes_container_finding() -> None:
    snapshot = load_kubernetes_snapshot(FIXTURES)
    report = assess_kubernetes_snapshot(snapshot)
    rule_ids = {finding.rule_id for finding in report.findings}

    assert "k8s.workload.privileged_container" in rule_ids
    assert "k8s.workload.host_network" in rule_ids
    assert "k8s.workload.runs_as_root" in rule_ids
    assert "k8s.workload.missing_resource_limits" in rule_ids
    assert "k8s.workload.env_secret_ref" in rule_ids


def test_public_service_finding() -> None:
    snapshot = load_kubernetes_snapshot(FIXTURES)
    report = assess_kubernetes_snapshot(snapshot)

    public = [
        finding for finding in report.findings if finding.rule_id == "k8s.service.public_exposure"
    ]
    assert len(public) == 1
    assert public[0].affected_resource == "default/Service/risky-web"


def test_fixture_tests_require_no_external_cluster() -> None:
    snapshot = load_kubernetes_snapshot(FIXTURES)

    assert snapshot.source.endswith("tests/fixtures/k8s")
    assert snapshot.raw_evidence["resource_count"] == 2
