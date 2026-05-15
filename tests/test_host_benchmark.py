from __future__ import annotations

import csv
import json
from pathlib import Path

import pytest

from piranesi.host import HostFinding, HostIdentity, HostSnapshot, analyze_snapshot
from piranesi.host.community import (
    HostCommunityError,
    validate_host_benchmark_submission,
    validate_host_fixture,
)
from piranesi.host.eval import (
    HostGroundTruth,
    HostGroundTruthMatcher,
    build_host_benchmark_report,
    evaluate_host_findings,
    load_host_ground_truth,
    matches_ground_truth,
    render_host_benchmark_markdown,
    write_host_benchmark_outputs,
)

FIXTURES = Path(__file__).parent / "fixtures" / "host"
COMMUNITY_FIXTURE = FIXTURES / "debian-vulnerable"


def _finding(
    rule_id: str,
    instance_key: str,
    *,
    title: str | None = None,
    category: str = "misconfiguration",
    severity: str = "high",
    source_tool: str = "piranesi",
) -> HostFinding:
    return HostFinding(
        id=f"{rule_id}:{instance_key}",
        rule_id=rule_id,
        instance_key=instance_key,
        title=title or f"{rule_id} {instance_key}",
        category=category,
        severity=severity,  # type: ignore[arg-type]
        confidence=0.9,
        remediation="Apply the benchmark fixture remediation.",
        source_tool=source_tool,
    )


def _report():
    return analyze_snapshot(HostSnapshot(identity=HostIdentity(hostname="benchmark-test")))


def test_ground_truth_parser_and_rule_instance_matching(tmp_path: Path) -> None:
    ground_truth_path = tmp_path / "ground_truth.json"
    ground_truth_path.write_text(
        json.dumps(
            {
                "schema_version": 1,
                "expected_findings": [
                    {
                        "id": "redis-public",
                        "rule_id": "host.listener.high_risk_service",
                        "instance_key": "tcp:redis-server",
                        "title_contains": "Redis",
                    }
                ],
            }
        ),
        encoding="utf-8",
    )

    ground_truth = load_host_ground_truth(ground_truth_path)
    matcher = ground_truth.expected_findings[0]

    assert matcher.id == "redis-public"
    assert matches_ground_truth(
        _finding(
            "host.listener.high_risk_service",
            "tcp:redis-server",
            title="Redis is listening on a public interface",
        ),
        matcher,
    )
    assert not matches_ground_truth(
        _finding(
            "host.listener.high_risk_service",
            "tcp:mysql",
            title="Redis is listening on a public interface",
        ),
        matcher,
    )


def test_precision_recall_f1_false_positives_and_false_negatives() -> None:
    ground_truth = HostGroundTruth(
        expected_findings=[
            HostGroundTruthMatcher(id="expected-a", rule_id="host.a", instance_key="a"),
            HostGroundTruthMatcher(id="expected-b", rule_id="host.b", instance_key="b"),
        ],
        expected_absent=[HostGroundTruthMatcher(id="absent-c", rule_id="host.c", instance_key="c")],
        allowed_extra=[HostGroundTruthMatcher(id="allowed-d", rule_id="host.d", instance_key="d")],
    )

    result = evaluate_host_findings(
        fixture="synthetic",
        target="synthetic",
        baseline="piranesi_deterministic",
        ground_truth=ground_truth,
        findings=[
            _finding("host.a", "a"),
            _finding("host.x", "x"),
            _finding("host.d", "d"),
        ],
        report=_report(),
    )

    assert result.metrics is not None
    assert result.metrics.true_positives == 1
    assert result.metrics.false_positives == 1
    assert result.metrics.false_negatives == 1
    assert result.metrics.allowed_extra_count == 1
    assert result.metrics.precision == pytest.approx(0.5)
    assert result.metrics.recall == pytest.approx(0.5)
    assert result.metrics.f1 == pytest.approx(0.5)
    assert {row.status for row in result.matrix} >= {
        "true_positive",
        "false_positive",
        "false_negative",
        "allowed_extra",
        "expected_absent_pass",
    }


def test_clean_fixture_coverage_findings_are_skipped_not_false_positive() -> None:
    ground_truth = HostGroundTruth(
        clean_fixture=True,
        expected_findings=[],
        expected_absent=[HostGroundTruthMatcher(id="absent-a", rule_id="host.a")],
    )

    result = evaluate_host_findings(
        fixture="clean",
        target="clean",
        baseline="piranesi_deterministic",
        ground_truth=ground_truth,
        findings=[
            _finding(
                "host.coverage.missing_evidence",
                "packages",
                category="coverage",
                severity="informational",
            )
        ],
        report=_report(),
    )

    assert result.metrics is not None
    assert result.metrics.false_positives == 0
    assert result.metrics.skipped_count == 1
    assert result.metrics.precision == pytest.approx(1.0)
    assert result.metrics.recall == pytest.approx(1.0)


def test_benchmark_records_optional_baseline_skips() -> None:
    report = build_host_benchmark_report(FIXTURES)
    baselines = {baseline.name: baseline for baseline in report.baselines}

    assert report.fixture_count == 4
    assert report.metrics.precision == pytest.approx(1.0)
    assert report.metrics.recall == pytest.approx(1.0)
    assert report.metrics.f1 == pytest.approx(1.0)
    assert baselines["trivy_only"].skipped_fixture_count >= 1
    assert baselines["lynis_only"].skipped_fixture_count >= 1
    assert baselines["openscap_only"].skipped_fixture_count >= 1
    assert baselines["piranesi_deterministic_llm"].status == "skipped"
    assert "LLM baseline disabled" in (baselines["piranesi_deterministic_llm"].skip_reason or "")


def test_report_rendering_and_output_files(tmp_path: Path) -> None:
    report = build_host_benchmark_report(FIXTURES)

    write_host_benchmark_outputs(report, tmp_path)

    payload = json.loads((tmp_path / "host_benchmark.json").read_text(encoding="utf-8"))
    markdown = (tmp_path / "host_benchmark.md").read_text(encoding="utf-8")
    with (tmp_path / "findings_matrix.csv").open(encoding="utf-8", newline="") as handle:
        rows = list(csv.DictReader(handle))

    assert set(payload) == {
        "schema_version",
        "generated_at",
        "fixtures_root",
        "fixture_count",
        "primary_baseline",
        "metrics",
        "baselines",
        "findings_matrix",
        "notes",
    }
    assert "Triage-speed metrics in this report are proxies" in markdown
    assert "not measured human analyst time" in markdown
    assert rows
    assert {"true_positive", "allowed_extra", "skipped"} <= {row["status"] for row in rows}


def test_markdown_renderer_lists_false_positive_and_false_negative() -> None:
    ground_truth = HostGroundTruth(
        expected_findings=[HostGroundTruthMatcher(id="expected-a", rule_id="host.a")],
    )
    result = evaluate_host_findings(
        fixture="synthetic",
        target="synthetic",
        baseline="piranesi_deterministic",
        ground_truth=ground_truth,
        findings=[_finding("host.extra", "x")],
        report=_report(),
    )
    report = build_host_benchmark_report(FIXTURES).model_copy(
        update={
            "fixture_count": 1,
            "metrics": result.metrics,
            "findings_matrix": result.matrix,
        }
    )

    markdown = render_host_benchmark_markdown(report)

    assert "Primary False Positives" in markdown
    assert "host.extra" in markdown
    assert "Primary False Negatives" in markdown
    assert "host.a" in markdown


def test_fixture_validation_for_host_bundle() -> None:
    result = validate_host_fixture(COMMUNITY_FIXTURE)

    assert result.status == "ok"
    assert result.has_ground_truth is True
    assert result.expected_findings == 6
    assert result.evidence_inventory["packages"] == 2


def test_fixture_validation_reports_invalid_bundle(tmp_path: Path) -> None:
    result = validate_host_fixture(tmp_path / "missing")

    assert result.status == "error"
    assert result.errors


def test_benchmark_metadata_validation() -> None:
    submission = validate_host_benchmark_submission(COMMUNITY_FIXTURE)

    assert submission.schema_version == 1
    assert submission.target == "debian-vm-01"
    assert submission.platform_family == "debian"
    assert submission.expected_findings == 6
    assert submission.expected_absent == 1


def test_benchmark_submission_requires_ground_truth(tmp_path: Path) -> None:
    fixture = tmp_path / "fixture"
    fixture.mkdir()
    (fixture / "host_snapshot.json").write_text(
        """
{
  "schema_version": 1,
  "identity": {"hostname": "community-fixture", "host_id": null, "ip_addresses": []},
  "os": {"name": "unknown", "version": null, "id": null, "version_id": null, "pretty_name": null},
  "packages": [],
  "network_interfaces": [],
  "listening_ports": [],
  "processes": [],
  "services": [],
  "users": [],
  "baseline_checks": [],
  "login_sessions": [],
  "auth_event_summaries": [],
  "config": {},
  "tool_provenance": {},
  "raw_evidence": {}
}
""".strip(),
        encoding="utf-8",
    )

    with pytest.raises(HostCommunityError, match="ground_truth"):
        validate_host_benchmark_submission(fixture)
