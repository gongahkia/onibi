from __future__ import annotations

import json
from pathlib import Path

from piranesi.models import (
    QueryQualityMetrics,
    QuerySpecDescriptor,
    QuerySpecUsage,
    ReachabilityResult,
    ScannedFunction,
    SourceLocation,
)
from piranesi.report.renderer import build_report, write_report_outputs
from tests._pipeline_fixtures import fixture_artifacts


def test_report_renderer_writes_expected_structure(tmp_path: Path) -> None:
    artifacts = fixture_artifacts(tmp_path)
    report = build_report(
        scan_result=artifacts["scan"],  # type: ignore[arg-type]
        detected_findings=artifacts["detect"].findings,  # type: ignore[attr-defined]
        confirmed_findings=artifacts["verify"].findings,  # type: ignore[attr-defined]
        legal_assessments=artifacts["legal"].assessments,  # type: ignore[attr-defined]
        patch_results=artifacts["patch"].patches,  # type: ignore[attr-defined]
        target_dir=tmp_path,
        total_llm_cost_usd=0.73,
        duration_s=8.5,
        stage_timings_s={
            "scan": 1.0,
            "detect": 1.0,
            "triage": 2.0,
            "verify": 2.0,
            "legal": 1.0,
            "patch": 1.0,
            "report": 0.5,
        },
    )
    write_report_outputs(report, tmp_path)

    payload = json.loads((tmp_path / "report.json").read_text(encoding="utf-8"))
    assert payload["target"] == str(tmp_path.resolve())
    assert payload["executive_summary"]["total_llm_cost_usd"] == 0.73
    assert payload["findings"][0]["title"] == "SQL Injection"
    assert payload["findings"][0]["pr_body"]
    assert payload["findings"][0]["verification_template_id"] == "sqli-read-probe"
    assert (
        payload["findings"][0]["verification_template_reason"]
        == "matched finding CWE CWE-89 [carriers=body; route=POST /login]"
    )
    assert (
        payload["findings"][0]["explanation"]["verification_state"]["state"]
        == "verified_confirmed"
    )
    assert payload["findings"][0]["explanation"]["confidence"]["model_version"] == "v1"
    assert (
        payload["findings"][0]["explanation"]["confidence"]["final_confidence"]
        == payload["findings"][0]["confidence"]
    )

    markdown = (tmp_path / "report.md").read_text(encoding="utf-8")
    assert "## SQL Injection (`finding-001`)" in markdown
    assert "**Verification template:** `sqli-read-probe`" in markdown
    assert "**Template selection:** matched finding CWE CWE-89" in markdown
    assert "### Confidence Breakdown" in markdown
    assert "| PDPA | Section 24 | Notify the regulator of a notifiable breach." in markdown

    pr_body = (tmp_path / "pr_body.md").read_text(encoding="utf-8")
    assert "### Regulatory Impact" in pr_body
    assert "Switch to parameterized queries." not in pr_body


def test_report_renderer_includes_query_quality_metrics_from_scan(tmp_path: Path) -> None:
    artifacts = fixture_artifacts(tmp_path)
    query_quality = QueryQualityMetrics(
        loaded_source_specs=2,
        loaded_sink_specs=2,
        matched_source_specs=1,
        matched_sink_specs=1,
        noisy_candidate_threshold=5,
        source_specs=[
            QuerySpecUsage(
                spec=QuerySpecDescriptor(
                    spec_id="source:express_req_body",
                    name="express_req_body",
                    kind="source",
                    category="request_body",
                    definition_origin="builtin",
                    definition_file="src/piranesi/scan/specs.py",
                ),
                candidate_count=1,
                matched=True,
            ),
            QuerySpecUsage(
                spec=QuerySpecDescriptor(
                    spec_id="source:custom_source_1",
                    name="custom_source_1",
                    kind="source",
                    category="custom",
                    is_custom=True,
                    definition_origin="config",
                    definition_file="piranesi.toml",
                ),
                candidate_count=0,
                matched=False,
            ),
        ],
        sink_specs=[
            QuerySpecUsage(
                spec=QuerySpecDescriptor(
                    spec_id="sink:raw_sql_query",
                    name="raw_sql_query",
                    kind="sink",
                    category="sql_query",
                    cwe_id="CWE-89",
                    definition_origin="builtin",
                    definition_file="src/piranesi/scan/specs.py",
                ),
                candidate_count=1,
                matched=True,
            ),
            QuerySpecUsage(
                spec=QuerySpecDescriptor(
                    spec_id="sink:custom_sink_1",
                    name="custom_sink_1",
                    kind="sink",
                    category="custom",
                    cwe_id="CWE-20",
                    is_custom=True,
                    definition_origin="config",
                    definition_file="piranesi.toml",
                ),
                candidate_count=0,
                matched=False,
            ),
        ],
        unmatched_source_specs=[
            QuerySpecDescriptor(
                spec_id="source:custom_source_1",
                name="custom_source_1",
                kind="source",
                category="custom",
                is_custom=True,
                definition_origin="config",
                definition_file="piranesi.toml",
            )
        ],
        unmatched_sink_specs=[
            QuerySpecDescriptor(
                spec_id="sink:custom_sink_1",
                name="custom_sink_1",
                kind="sink",
                category="custom",
                cwe_id="CWE-20",
                is_custom=True,
                definition_origin="config",
                definition_file="piranesi.toml",
            )
        ],
        noisy_source_specs=[],
        noisy_sink_specs=[],
    )
    scan_with_quality = artifacts["scan"].model_copy(update={"query_quality": query_quality})  # type: ignore[attr-defined]

    report = build_report(
        scan_result=scan_with_quality,  # type: ignore[arg-type]
        detected_findings=artifacts["detect"].findings,  # type: ignore[attr-defined]
        confirmed_findings=artifacts["verify"].findings,  # type: ignore[attr-defined]
        legal_assessments=artifacts["legal"].assessments,  # type: ignore[attr-defined]
        patch_results=artifacts["patch"].patches,  # type: ignore[attr-defined]
        target_dir=tmp_path,
        total_llm_cost_usd=0.0,
        duration_s=1.0,
        stage_timings_s={"scan": 0.2, "detect": 0.2, "report": 0.1},
    )
    write_report_outputs(report, tmp_path)

    payload = json.loads((tmp_path / "report.json").read_text(encoding="utf-8"))
    assert payload["query_quality"]["loaded_source_specs"] == 2
    assert payload["query_quality"]["matched_sink_specs"] == 1
    assert payload["query_quality"]["unmatched_source_specs"][0]["name"] == "custom_source_1"


def test_report_renderer_separates_suppressed_findings(tmp_path: Path) -> None:
    artifacts = fixture_artifacts(tmp_path)
    active = artifacts["detect"].findings[0]  # type: ignore[attr-defined]
    suppressed = active.model_copy(
        update={
            "id": "finding-suppressed",
            "suppressed": True,
            "suppression_reason": "accepted risk",
        }
    )
    report = build_report(
        scan_result=artifacts["scan"],  # type: ignore[arg-type]
        detected_findings=[active, suppressed],
        confirmed_findings=artifacts["verify"].findings,  # type: ignore[attr-defined]
        legal_assessments=artifacts["legal"].assessments,  # type: ignore[attr-defined]
        patch_results=artifacts["patch"].patches,  # type: ignore[attr-defined]
        target_dir=tmp_path,
        total_llm_cost_usd=0.73,
        duration_s=8.5,
        stage_timings_s={"scan": 1.0, "detect": 1.0},
    )
    write_report_outputs(report, tmp_path)

    payload = json.loads((tmp_path / "report.json").read_text(encoding="utf-8"))
    assert payload["executive_summary"]["suppressed_findings"] == 1
    assert payload["suppressed_findings"][0]["finding_id"] == "finding-suppressed"

    markdown = (tmp_path / "report.md").read_text(encoding="utf-8")
    assert "2 findings (1 suppressed)" in markdown
    assert "## Suppressed Findings" in markdown
    assert "accepted risk" in markdown


def test_report_renderer_exposes_evidence_status_levels(tmp_path: Path) -> None:
    artifacts = fixture_artifacts(tmp_path)
    base = artifacts["detect"].findings[0]  # type: ignore[attr-defined]
    active = base.model_copy(
        update={
            "id": "finding-active",
            "metadata": {
                "source_spec_name": "express_req_body",
                "source_spec_category": "request_body",
                "source_spec_custom": False,
                "sink_spec_name": "raw_sql_query",
                "sink_spec_category": "sql_query",
                "sink_spec_cwe": "CWE-89",
                "sink_spec_custom": False,
                "sanitizer_effectiveness": {"escapeHtml": "partial"},
                "partial_sanitizers": ["escapeHtml"],
            },
            "taint_path": [
                base.taint_path[0].model_copy(update={"sanitizer_applied": "escapeHtml"})
            ],
        }
    )
    unreachable = base.model_copy(
        update={
            "id": "finding-unreachable",
            "severity": "informational",
            "reachability": "unreachable",
        }
    )
    suppressed = base.model_copy(
        update={
            "id": "finding-suppressed",
            "suppressed": True,
            "suppression_reason": "accepted risk",
        }
    )
    triaged_active = artifacts["triage"].findings[0].model_copy(  # type: ignore[attr-defined]
        update={
            "finding": active,
            "triage_verdict": "true_positive",
            "triage_mode": "llm",
        }
    )

    report = build_report(
        scan_result=artifacts["scan"],  # type: ignore[arg-type]
        detected_findings=[active, unreachable, suppressed],
        triaged_findings=[triaged_active],
        confirmed_findings=artifacts["verify"].findings,  # type: ignore[attr-defined]
        legal_assessments=artifacts["legal"].assessments,  # type: ignore[attr-defined]
        patch_results=artifacts["patch"].patches,  # type: ignore[attr-defined]
        target_dir=tmp_path,
        total_llm_cost_usd=0.0,
        duration_s=1.0,
        stage_timings_s={"scan": 0.1, "detect": 0.1, "report": 0.1},
    )
    write_report_outputs(report, tmp_path)

    payload = json.loads((tmp_path / "report.json").read_text(encoding="utf-8"))
    assert payload["findings"][0]["evidence_status"] == "confirmed"
    assert payload["active_findings"][0]["evidence_status"] == "triaged_active_candidate"
    assert payload["unreachable_findings"][0]["evidence_status"] == "unreachable_candidate"
    assert payload["suppressed_findings"][0]["evidence_status"] == "suppressed"
    assert payload["active_findings"][0]["explanation"]["matched_source_spec"]["spec_id"] == (
        "source:express_req_body"
    )
    assert payload["active_findings"][0]["explanation"]["matched_sink_spec"]["spec_id"] == (
        "sink:raw_sql_query"
    )
    assert "escapeHtml" in payload["active_findings"][0]["explanation"]["sanitizers_observed"]
    assert (
        payload["active_findings"][0]["explanation"]["confidence"]["triage_signal"]["score"] > 0.9
    )
    assert payload["executive_summary"]["status_breakdown"]["confirmed"] == 1
    assert payload["executive_summary"]["status_breakdown"]["triaged_active_candidate"] == 1
    assert payload["executive_summary"]["status_breakdown"]["unreachable_candidate"] == 1
    assert payload["executive_summary"]["status_breakdown"]["suppressed"] == 1

    markdown = (tmp_path / "report.md").read_text(encoding="utf-8")
    assert "## Evidence Status Legend" in markdown
    assert "`triaged_active_candidate`: Candidate retained after model-assisted triage." in markdown
    assert "- **Evidence:** `triaged_active_candidate`" in markdown
    assert "- **Evidence:** `unreachable_candidate`" in markdown
    assert "- **Evidence:** `suppressed`" in markdown
    assert "Evidence status breakdown:" in markdown


def test_report_renderer_keeps_deterministic_triage_as_static_candidate(tmp_path: Path) -> None:
    artifacts = fixture_artifacts(tmp_path)
    active = artifacts["detect"].findings[0].model_copy(update={"id": "finding-static"})  # type: ignore[attr-defined]
    deterministic_triaged = artifacts["triage"].findings[0].model_copy(  # type: ignore[attr-defined]
        update={
            "finding": active,
            "triage_verdict": "true_positive",
            "triage_mode": "deterministic",
        }
    )
    report = build_report(
        scan_result=artifacts["scan"],  # type: ignore[arg-type]
        detected_findings=[active],
        triaged_findings=[deterministic_triaged],
        confirmed_findings=[],
        legal_assessments=[],
        patch_results=[],
        target_dir=tmp_path,
        total_llm_cost_usd=0.0,
        duration_s=1.0,
        stage_timings_s={"scan": 0.1, "detect": 0.1, "report": 0.1},
    )

    assert report.active_findings[0].evidence_status == "static_candidate"
    assert report.executive_summary.status_breakdown["static_candidate"] == 1


def test_report_renderer_groups_package_and_cross_package_findings(tmp_path: Path) -> None:
    artifacts = fixture_artifacts(tmp_path)
    base_candidate = artifacts["detect"].findings[0]  # type: ignore[attr-defined]
    local_candidate = base_candidate.model_copy(
        update={"metadata": {"package": "@test/shared-lib"}}
    )
    cross_candidate = base_candidate.model_copy(
        update={
            "id": "finding-cross-package",
            "metadata": {
                "package": "@test/api",
                "cross_package": True,
                "source_package": "@test/api",
                "sink_package": "@test/shared-lib",
            },
        }
    )

    local_confirmed = (
        artifacts["verify"]
        .findings[0]
        .model_copy(  # type: ignore[attr-defined]
            update={
                "finding": artifacts["verify"]
                .findings[0]
                .finding.model_copy(  # type: ignore[attr-defined]
                    update={"finding": local_candidate}
                )
            }
        )
    )
    cross_confirmed = local_confirmed.model_copy(
        update={"finding": local_confirmed.finding.model_copy(update={"finding": cross_candidate})}
    )

    report = build_report(
        scan_result=artifacts["scan"],  # type: ignore[arg-type]
        detected_findings=[local_candidate, cross_candidate],
        confirmed_findings=[local_confirmed, cross_confirmed],
        legal_assessments=[],
        patch_results=[],
        target_dir=tmp_path,
        total_llm_cost_usd=0.0,
        duration_s=1.0,
        stage_timings_s={"scan": 0.2, "detect": 0.2, "report": 0.1},
    )
    write_report_outputs(report, tmp_path)

    assert "@test/shared-lib" in report.package_findings
    assert report.cross_package_findings[0].source_package == "@test/api"

    markdown = (tmp_path / "report.md").read_text(encoding="utf-8")
    assert "### Package: `@test/shared-lib`" in markdown
    assert "## Cross-Package Findings" in markdown
    assert "`@test/api` -> `@test/shared-lib`" in markdown


def test_report_renderer_clusters_related_active_findings(tmp_path: Path) -> None:
    artifacts = fixture_artifacts(tmp_path)
    first = artifacts["detect"].findings[0]  # type: ignore[attr-defined]
    second = first.model_copy(
        update={
            "id": "finding-002",
            "source": first.source.model_copy(
                update={
                    "parameter_name": "email",
                    "location": first.source.location.model_copy(
                        update={
                            "line": first.source.location.line + 1,
                            "snippet": "const email = req.body.email;",
                        }
                    ),
                }
            ),
            "confidence": 0.83,
        }
    )

    report = build_report(
        scan_result=artifacts["scan"],  # type: ignore[arg-type]
        detected_findings=[first, second],
        confirmed_findings=artifacts["verify"].findings,  # type: ignore[attr-defined]
        legal_assessments=artifacts["legal"].assessments,  # type: ignore[attr-defined]
        patch_results=artifacts["patch"].patches,  # type: ignore[attr-defined]
        target_dir=tmp_path,
        total_llm_cost_usd=0.0,
        duration_s=1.0,
        stage_timings_s={"scan": 0.1, "detect": 0.1, "report": 0.1},
    )
    write_report_outputs(report, tmp_path)

    payload = json.loads((tmp_path / "report.json").read_text(encoding="utf-8"))
    assert payload["executive_summary"]["finding_clusters"] == 1
    assert payload["finding_clusters"][0]["count"] == 2
    assert payload["finding_clusters"][0]["representative_finding_id"] == "finding-001"
    assert payload["active_findings"][0]["cluster_size"] == 2
    assert payload["active_findings"][1]["cluster_size"] == 2
    assert payload["findings"][0]["cluster_size"] == 2

    markdown = (tmp_path / "report.md").read_text(encoding="utf-8")
    assert "## Finding Clusters" in markdown
    assert "2 related findings" in markdown
    assert "Related findings:** 2" in markdown


def test_report_renderer_separates_unreachable_findings_and_dead_code(tmp_path: Path) -> None:
    artifacts = fixture_artifacts(tmp_path)
    reachable = artifacts["detect"].findings[0]  # type: ignore[attr-defined]
    unreachable = reachable.model_copy(
        update={
            "id": "finding-unreachable",
            "severity": "informational",
            "reachability": "unreachable",
            "metadata": {
                **reachable.metadata,
                "source_function_id": "src/app.ts::deadEntry",
                "reachability_original_severity": "high",
            },
        }
    )

    report = build_report(
        scan_result=artifacts["scan"],  # type: ignore[arg-type]
        detected_findings=[reachable, unreachable],
        confirmed_findings=artifacts["verify"].findings,  # type: ignore[attr-defined]
        legal_assessments=artifacts["legal"].assessments,  # type: ignore[attr-defined]
        patch_results=artifacts["patch"].patches,  # type: ignore[attr-defined]
        target_dir=tmp_path,
        total_llm_cost_usd=0.73,
        duration_s=8.5,
        stage_timings_s={"scan": 1.0, "detect": 1.0, "report": 0.2},
        reachability=ReachabilityResult(
            reachable_functions={"src/app.ts::reachable"},
            unreachable_functions={"src/app.ts::deadEntry"},
            entry_points={"src/app.ts::handler"},
            call_graph_edges=1,
            dead_code_functions=[
                ScannedFunction(
                    function_id="src/app.ts::deadEntry",
                    name="deadEntry",
                    location=SourceLocation(
                        file=str((tmp_path / "src" / "app.ts").resolve()),
                        line=42,
                        column=1,
                        snippet="function deadEntry() {}",
                    ),
                )
            ],
        ),
        dead_code_report=True,
    )
    write_report_outputs(report, tmp_path)

    payload = json.loads((tmp_path / "report.json").read_text(encoding="utf-8"))
    assert payload["executive_summary"]["reachable_findings"] == 1
    assert payload["executive_summary"]["unreachable_findings"] == 1
    assert payload["unreachable_findings"][0]["finding_id"] == "finding-unreachable"
    assert payload["dead_code_functions"][0]["name"] == "deadEntry"

    markdown = (tmp_path / "report.md").read_text(encoding="utf-8")
    assert "## Active Candidate Findings" in markdown
    assert "## Unreachable Candidate Findings" in markdown
    assert "Original Severity" in markdown
    assert "## Dead Code Report" in markdown
    assert "`deadEntry` (line 42)" in markdown
