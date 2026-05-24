from __future__ import annotations

import json
from pathlib import Path

from tests._pipeline_fixtures import fixture_artifacts
from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.legal.maturity import TrendSignals, build_maturity_history, compute_maturity_level
from piranesi.report.compliance import render_compliance_summary
from piranesi.report.renderer import PiranesiReport, build_report

runner = CliRunner()


def test_maturity_level_1_many_criticals() -> None:
    assert (
        compute_maturity_level(
            scan_active=True,
            critical_findings=15,
            high_findings=0,
        )
        == 1
    )


def test_maturity_level_2_no_baseline() -> None:
    assert (
        compute_maturity_level(
            scan_active=True,
            critical_findings=5,
            high_findings=2,
        )
        == 2
    )


def test_maturity_level_3_with_trends() -> None:
    assert (
        compute_maturity_level(
            scan_active=True,
            critical_findings=3,
            high_findings=2,
            trend_signals=TrendSignals(has_baseline=True, has_trend=True, avg_fix_rate=0.0),
        )
        == 3
    )


def test_maturity_level_4_no_critical() -> None:
    assert (
        compute_maturity_level(
            scan_active=True,
            critical_findings=0,
            high_findings=3,
            trend_signals=TrendSignals(has_baseline=True, has_trend=True, avg_fix_rate=1.0),
        )
        == 4
    )


def test_maturity_level_5_clean() -> None:
    assert (
        compute_maturity_level(
            scan_active=True,
            critical_findings=0,
            high_findings=0,
            incremental=True,
        )
        == 5
    )


def test_maturity_regression_alert() -> None:
    history = build_maturity_history(
        scan_dates=["2026-01-01", "2026-01-02"],
        by_framework={"ISO_27001": [4, 3]},
    )

    assert history.regressions == [
        "2026-01-02: ISO_27001 maturity regressed 4 -> 3",
    ]


def test_render_compliance_summary_includes_standards_frameworks(tmp_path: Path) -> None:
    report = _build_report(tmp_path)

    rendered = render_compliance_summary(report, include_all=True)

    assert "Frameworks assessed:  12" in rendered
    assert "ISO 27001" in rendered
    assert "NIST CSF" in rendered
    assert "CIS v8" in rendered
    assert "Top 3 Remediation Priorities:" in rendered


def test_compliance_maturity_cli_emits_json(tmp_path: Path) -> None:
    report = _build_report(tmp_path)
    output_dir = tmp_path / "out"
    output_dir.mkdir()
    (output_dir / "report.json").write_text(report.model_dump_json(indent=2), encoding="utf-8")

    result = runner.invoke(
        app,
        ["compliance", "maturity", str(output_dir), "--framework", "iso27001", "--format", "json"],
    )

    assert result.exit_code == 0
    payload = json.loads(result.stdout)
    assert payload["assessments"][0]["framework"] == "ISO_27001"
    assert payload["assessments"][0]["score"] >= 2


def test_compliance_summary_cli_includes_all_framework_groups(tmp_path: Path) -> None:
    report = _build_report(tmp_path)
    output_dir = tmp_path / "out"
    output_dir.mkdir()
    (output_dir / "report.json").write_text(report.model_dump_json(indent=2), encoding="utf-8")

    result = runner.invoke(app, ["compliance", "summary", str(output_dir), "--all"])

    assert result.exit_code == 0
    assert "Privacy:" in result.stdout
    assert "Financial:" in result.stdout
    assert "Cyber:" in result.stdout
    assert "Standards:" in result.stdout


def _build_report(tmp_path: Path) -> PiranesiReport:
    artifacts = fixture_artifacts(tmp_path)
    return build_report(
        scan_result=artifacts["scan"],  # type: ignore[arg-type]
        detected_findings=artifacts["detect"].findings,  # type: ignore[attr-defined]
        confirmed_findings=artifacts["verify"].findings,  # type: ignore[attr-defined]
        legal_assessments=artifacts["legal"].assessments,  # type: ignore[attr-defined]
        patch_results=artifacts["patch"].patches,  # type: ignore[attr-defined]
        target_dir=tmp_path,
        total_llm_cost_usd=0.0,
        duration_s=1.0,
        stage_timings_s={},
    )
