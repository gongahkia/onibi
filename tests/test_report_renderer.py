from __future__ import annotations

import json
from pathlib import Path

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
