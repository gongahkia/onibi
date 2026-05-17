from __future__ import annotations

import json
import time
from collections import OrderedDict
from pathlib import Path
from subprocess import CompletedProcess
from typing import Any

import pytest
from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.config import PiranesiConfig, ScanConfig
from piranesi.hooks.pre_commit import discover_staged_files
from piranesi.models import ScanResult
from piranesi.pipeline import (
    DetectArtifact,
    LegalArtifact,
    PatchArtifact,
    PipelineContext,
    PipelineStage,
    StageResult,
    TriageArtifact,
    VerifyArtifact,
)
from piranesi.report.renderer import PiranesiReport, build_report, write_report_outputs
from tests._pipeline_fixtures import fixture_artifacts

runner = CliRunner()


def test_discover_staged_files_filters_to_supported_scan_targets(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    staged_source = tmp_path / "src" / "app.js"
    ignored_doc = tmp_path / "docs" / "readme.md"
    ignored_text = tmp_path / "src" / "notes.txt"
    staged_source.parent.mkdir(parents=True, exist_ok=True)
    ignored_doc.parent.mkdir(parents=True, exist_ok=True)
    staged_source.write_text("console.log('ok')\n", encoding="utf-8")
    ignored_doc.write_text("# docs\n", encoding="utf-8")
    ignored_text.write_text("notes\n", encoding="utf-8")

    def _fake_git_root(_start_dir: Path) -> Path:
        return tmp_path

    def _fake_run_subprocess(*args: Any, **kwargs: Any) -> CompletedProcess[str]:
        _ = (args, kwargs)
        return CompletedProcess(
            args=["git", "diff"],
            returncode=0,
            stdout="src/app.js\ndocs/readme.md\nsrc/notes.txt\n",
            stderr="",
        )

    monkeypatch.setattr("piranesi.hooks.pre_commit.git_repo_root", _fake_git_root)
    monkeypatch.setattr("piranesi.hooks.pre_commit.run_subprocess", _fake_run_subprocess)

    config = PiranesiConfig(
        scan=ScanConfig(
            include_patterns=["**/*.js"],
            exclude_patterns=[],
        )
    )

    staged_files = discover_staged_files(tmp_path, config)

    assert staged_files == [staged_source.resolve(strict=False)]


def test_hook_install_uninstall_commands_write_and_remove_managed_hook(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    config_path = tmp_path / "piranesi.toml"
    hook_path = tmp_path / ".git" / "hooks" / "pre-commit"
    hook_path.parent.mkdir(parents=True, exist_ok=True)
    config_path.write_text(
        "\n".join(
            [
                "[hooks]",
                'fail_severity = "critical"',
                "timeout = 15",
            ]
        ),
        encoding="utf-8",
    )

    monkeypatch.setattr(
        "piranesi.hooks.pre_commit.pre_commit_hook_path",
        lambda _start_dir: hook_path,
    )

    install_result = runner.invoke(app, ["hook", "install", "--config", str(config_path)])

    assert install_result.exit_code == 0
    payload = hook_path.read_text(encoding="utf-8")
    assert "--staged-only" in payload
    assert "--fail-severity critical" in payload
    assert "--hook-timeout 15" in payload

    status_result = runner.invoke(app, ["hook", "status"])

    assert status_result.exit_code == 0
    assert "installed" in status_result.stdout
    assert str(hook_path) in status_result.stdout

    uninstall_result = runner.invoke(app, ["hook", "uninstall"])

    assert uninstall_result.exit_code == 0
    assert not hook_path.exists()


def test_run_staged_only_uses_only_staged_files(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    config_path = tmp_path / "piranesi.toml"
    output_dir = tmp_path / "out"
    staged_file = tmp_path / "src" / "staged.py"
    unstaged_file = tmp_path / "src" / "unstaged.py"
    staged_file.parent.mkdir(parents=True, exist_ok=True)
    staged_file.write_text("print('staged')\n", encoding="utf-8")
    unstaged_file.write_text("print('unstaged')\n", encoding="utf-8")
    config_path.write_text('[scan]\ninclude_patterns = ["**/*.py"]\n', encoding="utf-8")

    seen: dict[str, Any] = {}

    def _registry(context: PipelineContext) -> OrderedDict[str, PipelineStage]:
        selected = sorted(str(path) for path in context.selected_files or set())
        seen["selected_files"] = selected
        artifacts = fixture_artifacts(context.target_dir)
        artifacts["scan"] = artifacts["scan"].model_copy(
            update={
                "files_scanned": selected,
                "metadata": artifacts["scan"].metadata.model_copy(
                    update={"files_parsed": len(selected)}
                ),
            }
        )
        return _build_fake_registry(context, artifacts=artifacts, calls=[])

    monkeypatch.setattr("piranesi.cli.build_default_stage_registry", _registry)
    monkeypatch.setattr(
        "piranesi.cli.discover_staged_files",
        lambda _target_dir, _config: [staged_file.resolve(strict=False)],
    )

    result = runner.invoke(
        app,
        [
            "run",
            str(tmp_path),
            "--config",
            str(config_path),
            "--output",
            str(output_dir),
            "--staged-only",
            "--authorized",
            "--yes",
        ],
    )

    assert result.exit_code == 1
    assert seen["selected_files"] == [str(staged_file.resolve(strict=False))]
    scan_payload = json.loads((output_dir / "scan.json").read_text(encoding="utf-8"))
    assert scan_payload["files_scanned"] == [str(staged_file.resolve(strict=False))]


def test_run_staged_only_timeout_exits_zero(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    config_path = tmp_path / "piranesi.toml"
    staged_file = tmp_path / "src" / "staged.py"
    staged_file.parent.mkdir(parents=True, exist_ok=True)
    staged_file.write_text("print('staged')\n", encoding="utf-8")
    config_path.write_text('[scan]\ninclude_patterns = ["**/*.py"]\n', encoding="utf-8")

    monkeypatch.setattr(
        "piranesi.cli.discover_staged_files",
        lambda _target_dir, _config: [staged_file.resolve(strict=False)],
    )

    def _slow_run_pipeline(*args: Any, **kwargs: Any) -> Any:
        _ = (args, kwargs)
        time.sleep(2)
        pytest.fail("run_pipeline should have timed out before completion")

    monkeypatch.setattr("piranesi.cli.run_pipeline", _slow_run_pipeline)

    result = runner.invoke(
        app,
        [
            "run",
            str(tmp_path),
            "--config",
            str(config_path),
            "--staged-only",
            "--hook-timeout",
            "1",
            "--authorized",
            "--yes",
        ],
    )

    assert result.exit_code == 0
    assert "skipping staged pre-commit scan" in result.stdout


def _build_fake_registry(
    context: PipelineContext,
    *,
    artifacts: dict[str, Any],
    calls: list[str],
) -> OrderedDict[str, PipelineStage]:
    def _runner(stage_name: str, artifact: Any) -> Any:
        def _run(config: Any, prev: Any) -> StageResult:
            _ = (config, prev)
            calls.append(stage_name)
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
            (
                "detect",
                PipelineStage("detect", DetectArtifact, _runner("detect", artifacts["detect"])),
            ),
            (
                "triage",
                PipelineStage("triage", TriageArtifact, _runner("triage", artifacts["triage"])),
            ),
            (
                "verify",
                PipelineStage("verify", VerifyArtifact, _runner("verify", artifacts["verify"])),
            ),
            ("legal", PipelineStage("legal", LegalArtifact, _runner("legal", artifacts["legal"]))),
            ("patch", PipelineStage("patch", PatchArtifact, _runner("patch", artifacts["patch"]))),
            ("report", PipelineStage("report", PiranesiReport, _runner("report", None))),
        )
    )
