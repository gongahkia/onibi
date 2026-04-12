from __future__ import annotations

import json
from pathlib import Path

from piranesi.models import ReachabilityResult, ScannedFunction, SourceLocation
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

    markdown = (tmp_path / "report.md").read_text(encoding="utf-8")
    assert "## SQL Injection (`finding-001`)" in markdown
    assert "| PDPA | Section 24 | Notify the regulator of a notifiable breach." in markdown

    pr_body = (tmp_path / "pr_body.md").read_text(encoding="utf-8")
    assert "### Regulatory Impact" in pr_body
    assert "Switch to parameterized queries." not in pr_body


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
    assert "## Active Findings" in markdown
    assert "## Unreachable Findings" in markdown
    assert "Original Severity" in markdown
    assert "## Dead Code Report" in markdown
    assert "`deadEntry` (line 42)" in markdown
