from __future__ import annotations

import json
from collections import OrderedDict
from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.models import ScanResult
from piranesi.pipeline import (
    DetectArtifact,
    LegalArtifact,
    PatchArtifact,
    PipelineStage,
    StageResult,
    TriageArtifact,
    VerifyArtifact,
)
from piranesi.report.renderer import PiranesiReport, build_report, write_report_outputs
from tests._pipeline_fixtures import fixture_artifacts

runner = CliRunner()


def test_run_executes_mocked_pipeline_and_writes_reports(
    monkeypatch,
    tmp_path: Path,
) -> None:
    config_path = tmp_path / "piranesi.toml"
    output_dir = tmp_path / "out"
    config_path.write_text("", encoding="utf-8")

    calls: list[str] = []

    def _registry(context):
        artifacts = fixture_artifacts(context.target_dir)
        return _build_fake_registry(context, artifacts=artifacts, calls=calls)

    monkeypatch.setattr("piranesi.cli.build_default_stage_registry", _registry)

    result = runner.invoke(
        app,
        [
            "run",
            str(tmp_path),
            "--config",
            str(config_path),
            "--output",
            str(output_dir),
            "--authorized",
            "--yes",
        ],
    )

    assert result.exit_code == 0
    assert calls == ["scan", "detect", "triage", "verify", "legal", "patch", "report"]

    report_payload = json.loads((output_dir / "report.json").read_text(encoding="utf-8"))
    assert report_payload["executive_summary"]["findings_detected"] == 1
    assert report_payload["executive_summary"]["findings_confirmed"] == 1
    assert report_payload["findings"][0]["cwe"] == "CWE-89"
    assert report_payload["findings"][0]["regulatory_obligations"][0]["framework"] == "PDPA"
    assert "--- a/src/routes/login.ts" in report_payload["findings"][0]["patch_diff"]

    markdown = (output_dir / "report.md").read_text(encoding="utf-8")
    assert "# Piranesi Security Analysis Report" in markdown
    assert "## Executive Summary" in markdown
    assert "### Patch" in markdown

    pr_body = (output_dir / "pr_body.md").read_text(encoding="utf-8")
    assert "## SQL Injection" in pr_body
    assert "```diff" in pr_body


def test_run_resume_skips_completed_stages(monkeypatch, tmp_path: Path) -> None:
    config_path = tmp_path / "piranesi.toml"
    output_dir = tmp_path / "out"
    config_path.write_text("", encoding="utf-8")
    output_dir.mkdir(parents=True, exist_ok=True)

    artifacts = fixture_artifacts(tmp_path)
    (output_dir / "scan.json").write_text(
        artifacts["scan"].model_dump_json(indent=2),
        encoding="utf-8",
    )
    (output_dir / "detect.json").write_text(
        artifacts["detect"].model_dump_json(indent=2),
        encoding="utf-8",
    )

    calls: list[str] = []

    def _registry(context):
        return _build_fake_registry(context, artifacts=artifacts, calls=calls)

    monkeypatch.setattr("piranesi.cli.build_default_stage_registry", _registry)

    result = runner.invoke(
        app,
        [
            "run",
            str(tmp_path),
            "--config",
            str(config_path),
            "--output",
            str(output_dir),
            "--resume",
            "--authorized",
            "--yes",
        ],
    )

    assert result.exit_code == 0
    assert calls == ["triage", "verify", "legal", "patch", "report"]


def test_run_saves_partial_results_when_stage_fails(monkeypatch, tmp_path: Path) -> None:
    config_path = tmp_path / "piranesi.toml"
    output_dir = tmp_path / "out"
    config_path.write_text("", encoding="utf-8")

    calls: list[str] = []

    def _registry(context):
        artifacts = fixture_artifacts(context.target_dir)
        return _build_fake_registry(
            context,
            artifacts=artifacts,
            calls=calls,
            fail_stage="verify",
        )

    monkeypatch.setattr("piranesi.cli.build_default_stage_registry", _registry)

    result = runner.invoke(
        app,
        [
            "run",
            str(tmp_path),
            "--config",
            str(config_path),
            "--output",
            str(output_dir),
            "--authorized",
            "--yes",
        ],
    )

    assert result.exit_code == 1
    assert "--resume" in result.stdout
    partial = json.loads((output_dir / "_partial.json").read_text(encoding="utf-8"))
    assert partial["failed"] == "verify"
    assert partial["completed"] == ["scan", "detect", "triage"]
    assert calls == ["scan", "detect", "triage", "verify"]


def test_run_dry_run_lists_matching_scan_targets(tmp_path: Path) -> None:
    config_path = tmp_path / "piranesi.toml"
    config_path.write_text("", encoding="utf-8")

    app_file = tmp_path / "app.ts"
    excluded_file = tmp_path / "node_modules" / "dep.ts"
    excluded_file.parent.mkdir(parents=True, exist_ok=True)
    app_file.write_text("console.log('ok')\n", encoding="utf-8")
    excluded_file.write_text("console.log('skip')\n", encoding="utf-8")

    result = runner.invoke(
        app,
        [
            "run",
            str(tmp_path),
            "--config",
            str(config_path),
            "--dry-run",
        ],
    )

    assert result.exit_code == 0
    assert str(app_file) in result.stdout
    assert str(excluded_file) not in result.stdout


def _build_fake_registry(
    context,
    *,
    artifacts: dict[str, object],
    calls: list[str],
    fail_stage: str | None = None,
) -> OrderedDict[str, PipelineStage]:
    def _runner(stage_name: str, artifact):
        def _run(config, prev):
            _ = (config, prev)
            calls.append(stage_name)
            if fail_stage == stage_name:
                raise RuntimeError(f"{stage_name} exploded")
            if stage_name == "report":
                report = build_report(
                    scan_result=artifacts["scan"],
                    detected_findings=artifacts["detect"].findings,
                    confirmed_findings=artifacts["verify"].findings,
                    legal_assessments=artifacts["legal"].assessments,
                    patch_results=artifacts["patch"].patches,
                    target_dir=context.target_dir,
                    total_llm_cost_usd=0.42,
                    duration_s=1.25,
                    stage_timings_s={"scan": 0.1, "detect": 0.1, "triage": 0.1},
                )
                write_report_outputs(report, context.output_dir)
                return StageResult(stage=stage_name, success=True, artifact=report, elapsed_s=0.05)
            return StageResult(stage=stage_name, success=True, artifact=artifact, elapsed_s=0.05)

        return _run

    return OrderedDict(
        (
            ("scan", PipelineStage("scan", ScanResult, _runner("scan", artifacts["scan"]))),
            ("detect", PipelineStage("detect", DetectArtifact, _runner("detect", artifacts["detect"]))),
            ("triage", PipelineStage("triage", TriageArtifact, _runner("triage", artifacts["triage"]))),
            ("verify", PipelineStage("verify", VerifyArtifact, _runner("verify", artifacts["verify"]))),
            ("legal", PipelineStage("legal", LegalArtifact, _runner("legal", artifacts["legal"]))),
            ("patch", PipelineStage("patch", PatchArtifact, _runner("patch", artifacts["patch"]))),
            ("report", PipelineStage("report", PiranesiReport, _runner("report", None))),
        )
    )
