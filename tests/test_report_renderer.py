from __future__ import annotations

import json
from pathlib import Path

from piranesi.report.renderer import build_report, write_report_outputs

from tests._pipeline_fixtures import fixture_artifacts


def test_report_renderer_writes_expected_structure(tmp_path: Path) -> None:
    artifacts = fixture_artifacts(tmp_path)
    report = build_report(
        scan_result=artifacts["scan"],
        detected_findings=artifacts["detect"].findings,
        confirmed_findings=artifacts["verify"].findings,
        legal_assessments=artifacts["legal"].assessments,
        patch_results=artifacts["patch"].patches,
        target_dir=tmp_path,
        total_llm_cost_usd=0.73,
        duration_s=8.5,
        stage_timings_s={"scan": 1.0, "detect": 1.0, "triage": 2.0, "verify": 2.0, "legal": 1.0, "patch": 1.0, "report": 0.5},
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
