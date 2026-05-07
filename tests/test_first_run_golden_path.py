from __future__ import annotations

import json
from pathlib import Path

import pytest
from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.doctor import DoctorReport
from piranesi.pipeline import PipelineRunResult, StageResult
from piranesi.report.renderer import build_report, write_report_outputs
from tests._pipeline_fixtures import fixture_artifacts

runner = CliRunner()


def test_first_run_golden_path_init_doctor_run_and_explain(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    project_root = tmp_path / "golden-first-run"
    project_root.mkdir(parents=True, exist_ok=True)
    (project_root / "package.json").write_text(
        json.dumps({"dependencies": {"express": "^5.1.0"}}),
        encoding="utf-8",
    )
    (project_root / "app.js").write_text(
        "const express = require('express');\n"
        "const app = express();\n"
        "app.use(express.json());\n"
        "app.post('/login', (req, res) => {\n"
        "  const sql = `SELECT * FROM users WHERE username='${req.body.username}'`;\n"
        "  res.send(sql);\n"
        "});\n"
        "module.exports = app;\n",
        encoding="utf-8",
    )
    monkeypatch.chdir(project_root)

    init_result = runner.invoke(app, ["init"])
    assert init_result.exit_code == 0
    assert (project_root / "piranesi.toml").exists()
    assert (project_root / ".piranesi-ignore").exists()

    doctor_report = DoctorReport(
        piranesi_version="0.2.0",
        target=str(project_root),
        config_path=str(project_root / "piranesi.toml"),
        ready=True,
        collect_ready=False,
        assess_ready=True,
    )
    monkeypatch.setattr("piranesi.cli.build_doctor_report", lambda *_args, **_kwargs: doctor_report)

    doctor_result = runner.invoke(app, ["doctor", "."])
    assert doctor_result.exit_code == 0
    assert "Host assessment ready: yes" in doctor_result.stdout

    output_dir = project_root / ".piranesi-out"

    def fake_run_pipeline(*_args, **kwargs) -> PipelineRunResult:  # type: ignore[no-untyped-def]
        context = kwargs.get("context") or _args[1]
        context.output_dir.mkdir(parents=True, exist_ok=True)
        artifacts = fixture_artifacts(project_root)
        report = build_report(
            scan_result=artifacts["scan"],  # type: ignore[arg-type]
            detected_findings=artifacts["detect"].findings,  # type: ignore[attr-defined]
            triaged_findings=artifacts["triage"].findings,  # type: ignore[attr-defined]
            confirmed_findings=artifacts["verify"].findings,  # type: ignore[attr-defined]
            legal_assessments=artifacts["legal"].assessments,  # type: ignore[attr-defined]
            patch_results=artifacts["patch"].patches,  # type: ignore[attr-defined]
            target_dir=project_root,
            total_llm_cost_usd=0.0,
            duration_s=1.0,
            stage_timings_s={
                "scan": 0.1,
                "detect": 0.1,
                "triage": 0.1,
                "verify": 0.1,
                "legal": 0.1,
                "patch": 0.1,
                "report": 0.1,
            },
        )
        write_report_outputs(report, context.output_dir)
        for stage_name in ("scan", "detect", "triage", "verify", "legal", "patch"):
            stage_artifact = artifacts[stage_name]
            context.stage_outputs[stage_name] = stage_artifact
            (context.output_dir / f"{stage_name}.json").write_text(
                stage_artifact.model_dump_json(indent=2),
                encoding="utf-8",
            )
        context.stage_outputs["report"] = report
        return PipelineRunResult(
            results=[
                StageResult(
                    stage="scan",
                    success=True,
                    artifact=artifacts["scan"],
                    elapsed_s=0.1,
                ),
                StageResult(
                    stage="detect",
                    success=True,
                    artifact=artifacts["detect"],
                    elapsed_s=0.1,
                ),
                StageResult(
                    stage="triage",
                    success=True,
                    artifact=artifacts["triage"],
                    elapsed_s=0.1,
                ),
                StageResult(
                    stage="verify",
                    success=True,
                    artifact=artifacts["verify"],
                    elapsed_s=0.1,
                ),
                StageResult(
                    stage="legal",
                    success=True,
                    artifact=artifacts["legal"],
                    elapsed_s=0.1,
                ),
                StageResult(
                    stage="patch",
                    success=True,
                    artifact=artifacts["patch"],
                    elapsed_s=0.1,
                ),
                StageResult(stage="report", success=True, artifact=report, elapsed_s=0.1),
            ]
        )

    monkeypatch.setattr("piranesi.cli.run_pipeline", fake_run_pipeline)

    run_result = runner.invoke(
        app,
        [
            "run",
            ".",
            "--authorized",
            "--yes",
            "--no-execute",
            "--no-fail",
            "--output",
            str(output_dir),
        ],
    )

    assert run_result.exit_code == 0
    assert (output_dir / "report.json").exists()
    assert (output_dir / "report.md").exists()

    report_payload = json.loads((output_dir / "report.json").read_text(encoding="utf-8"))
    finding_id = report_payload["findings"][0]["finding_id"]

    explain_result = runner.invoke(app, ["explain", finding_id, "--output", str(output_dir)])

    assert explain_result.exit_code == 0
    assert finding_id in explain_result.stdout
    assert "Piranesi Finding Explanation" in explain_result.stdout
