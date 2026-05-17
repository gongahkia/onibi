from __future__ import annotations

import json
import shutil
import threading
from collections import OrderedDict
from pathlib import Path
from types import SimpleNamespace
from typing import Any

import pytest
from typer.testing import CliRunner

import piranesi.pipeline as pipeline_module
from piranesi.cli import app
from piranesi.config import OutputConfig, PiranesiConfig
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
    run_pipeline,
)
from piranesi.report.renderer import PiranesiReport, build_report, write_report_outputs
from tests._pipeline_fixtures import fixture_artifacts

runner = CliRunner()


def test_run_executes_mocked_pipeline_and_writes_reports(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    config_path = tmp_path / "piranesi.toml"
    output_dir = tmp_path / "out"
    config_path.write_text("", encoding="utf-8")

    calls: list[str] = []

    def _registry(context: PipelineContext) -> OrderedDict[str, PipelineStage]:
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

    assert result.exit_code == 1
    assert calls == ["scan", "detect", "triage", "verify", "legal", "patch", "report"]
    assert "findings detected: 1 (confirmed: 1)" in result.stdout

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


def test_run_resume_skips_completed_stages(monkeypatch: pytest.MonkeyPatch, tmp_path: Path) -> None:
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

    def _registry(context: PipelineContext) -> OrderedDict[str, PipelineStage]:
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

    assert result.exit_code == 1
    assert calls == ["triage", "verify", "legal", "patch", "report"]


def test_run_returns_zero_when_report_is_clean(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    config_path = tmp_path / "piranesi.toml"
    output_dir = tmp_path / "out"
    config_path.write_text("", encoding="utf-8")

    calls: list[str] = []

    def _registry(context: PipelineContext) -> OrderedDict[str, PipelineStage]:
        artifacts = fixture_artifacts(context.target_dir)
        return _build_fake_registry(
            context,
            artifacts={
                **artifacts,
                "detect": artifacts["detect"].model_copy(update={"findings": []}),
                "triage": artifacts["triage"].model_copy(update={"findings": []}),
                "verify": artifacts["verify"].model_copy(update={"findings": []}),
                "legal": artifacts["legal"].model_copy(update={"assessments": []}),
                "patch": artifacts["patch"].model_copy(update={"patches": []}),
            },
            calls=calls,
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

    assert result.exit_code == 0
    assert calls == ["scan", "detect", "triage", "verify", "legal", "patch", "report"]


def test_run_fail_severity_high_allows_medium_findings(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    config_path = tmp_path / "piranesi.toml"
    output_dir = tmp_path / "out"
    config_path.write_text("", encoding="utf-8")

    def _registry(context: PipelineContext) -> OrderedDict[str, PipelineStage]:
        artifacts = fixture_artifacts(context.target_dir, severity="medium")
        return _build_fake_registry(context, artifacts=artifacts, calls=[])

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
            "--fail-severity",
            "high",
            "--authorized",
            "--yes",
        ],
    )

    assert result.exit_code == 0


def test_run_no_fail_returns_zero_even_with_findings(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    config_path = tmp_path / "piranesi.toml"
    output_dir = tmp_path / "out"
    config_path.write_text("", encoding="utf-8")

    def _registry(context: PipelineContext) -> OrderedDict[str, PipelineStage]:
        artifacts = fixture_artifacts(context.target_dir)
        return _build_fake_registry(context, artifacts=artifacts, calls=[])

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
            "--no-fail",
            "--authorized",
            "--yes",
        ],
    )

    assert result.exit_code == 0


def test_run_profile_prints_stage_breakdown(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    config_path = tmp_path / "piranesi.toml"
    output_dir = tmp_path / "out"
    config_path.write_text("", encoding="utf-8")

    def _registry(context: PipelineContext) -> OrderedDict[str, PipelineStage]:
        artifacts = fixture_artifacts(context.target_dir)
        return _build_fake_registry(context, artifacts=artifacts, calls=[])

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
            "--profile",
            "--authorized",
            "--yes",
        ],
    )

    assert result.exit_code == 1
    assert "Stage" in result.output
    assert "scan" in result.output
    assert "TOTAL" in result.output


def test_run_baseline_fail_on_new_returns_zero_for_unchanged_findings(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    config_path = tmp_path / "piranesi.toml"
    output_dir = tmp_path / "out"
    baseline_input_dir = tmp_path / "baseline-input"
    baseline_path = tmp_path / ".piranesi-baseline.json"
    config_path.write_text("", encoding="utf-8")

    artifacts = fixture_artifacts(tmp_path)
    baseline_input_dir.mkdir(parents=True, exist_ok=True)
    (baseline_input_dir / "detect.json").write_text(
        artifacts["detect"].model_dump_json(indent=2),
        encoding="utf-8",
    )

    save_result = runner.invoke(
        app,
        ["baseline", "save", "--from", str(baseline_input_dir), "--to", str(baseline_path)],
    )
    assert save_result.exit_code == 0

    def _registry(context: PipelineContext) -> OrderedDict[str, PipelineStage]:
        return _build_fake_registry(context, artifacts=artifacts, calls=[])

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
            "--baseline",
            str(baseline_path),
            "--fail-on-new",
            "--authorized",
            "--yes",
        ],
    )

    assert result.exit_code == 0
    assert "Summary: 0 new, 0 changed, 0 fixed, 1 existing" in result.stdout
    assert "baseline diff markdown:" in result.stdout
    assert "baseline diff json:" in result.stdout
    assert (output_dir / "baseline-diff.md").exists()
    assert (output_dir / "baseline-diff.json").exists()


def test_run_baseline_fail_on_new_exits_one_for_new_findings(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    config_path = tmp_path / "piranesi.toml"
    output_dir = tmp_path / "out"
    baseline_input_dir = tmp_path / "baseline-input"
    baseline_path = tmp_path / ".piranesi-baseline.json"
    config_path.write_text("", encoding="utf-8")

    artifacts = fixture_artifacts(tmp_path)
    baseline_input_dir.mkdir(parents=True, exist_ok=True)
    (baseline_input_dir / "detect.json").write_text(
        DetectArtifact(findings=[]).model_dump_json(indent=2),
        encoding="utf-8",
    )

    save_result = runner.invoke(
        app,
        ["baseline", "save", "--from", str(baseline_input_dir), "--to", str(baseline_path)],
    )
    assert save_result.exit_code == 0

    def _registry(context: PipelineContext) -> OrderedDict[str, PipelineStage]:
        return _build_fake_registry(context, artifacts=artifacts, calls=[])

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
            "--baseline",
            str(baseline_path),
            "--fail-on-new",
            "--authorized",
            "--yes",
        ],
    )

    assert result.exit_code == 1
    assert "NEW (1):" in result.stdout


def test_run_baseline_fail_on_new_respects_severity_threshold(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    config_path = tmp_path / "piranesi.toml"
    output_dir = tmp_path / "out"
    baseline_input_dir = tmp_path / "baseline-input"
    baseline_path = tmp_path / ".piranesi-baseline.json"
    config_path.write_text("", encoding="utf-8")

    artifacts = fixture_artifacts(tmp_path, severity="medium")
    baseline_input_dir.mkdir(parents=True, exist_ok=True)
    (baseline_input_dir / "detect.json").write_text(
        DetectArtifact(findings=[]).model_dump_json(indent=2),
        encoding="utf-8",
    )

    save_result = runner.invoke(
        app,
        ["baseline", "save", "--from", str(baseline_input_dir), "--to", str(baseline_path)],
    )
    assert save_result.exit_code == 0

    def _registry(context: PipelineContext) -> OrderedDict[str, PipelineStage]:
        return _build_fake_registry(context, artifacts=artifacts, calls=[])

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
            "--baseline",
            str(baseline_path),
            "--fail-on-new",
            "--fail-on-new-severity",
            "high",
            "--authorized",
            "--yes",
        ],
    )

    assert result.exit_code == 0


def test_run_saves_partial_results_when_stage_fails(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    config_path = tmp_path / "piranesi.toml"
    output_dir = tmp_path / "out"
    config_path.write_text("", encoding="utf-8")

    calls: list[str] = []

    def _registry(context: PipelineContext) -> OrderedDict[str, PipelineStage]:
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

    assert result.exit_code == 3
    assert "--resume" in result.stdout
    partial = json.loads((output_dir / "_partial.json").read_text(encoding="utf-8"))
    assert partial["failed"] == "verify"
    assert partial["completed"] == ["scan", "detect", "triage"]
    assert calls == ["scan", "detect", "triage", "verify"]


def test_run_missing_config_exits_two(tmp_path: Path) -> None:
    missing_config = tmp_path / "missing.toml"

    result = runner.invoke(
        app,
        [
            "run",
            str(tmp_path),
            "--config",
            str(missing_config),
            "--authorized",
            "--yes",
        ],
    )

    assert result.exit_code == 2


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


def test_scan_target_discovery_skips_piranesi_output_dirs_and_trace_files(
    tmp_path: Path,
) -> None:
    source_file = tmp_path / "src" / "index.ts"
    output_file = tmp_path / "piranesi-output" / "_cpg_cache" / "foo" / "transpiled" / "bar.ts"
    cache_file = tmp_path / ".piranesi-cache" / "cached.ts"
    out_file = tmp_path / ".piranesi-out" / "out.ts"
    trace_file = tmp_path / ".piranesi-trace.ts"

    for path in (source_file, output_file, cache_file, out_file, trace_file):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("export const value = 1;\n", encoding="utf-8")

    assert pipeline_module.discover_scan_targets(tmp_path, PiranesiConfig()) == [source_file]


def test_detect_artifact_round_trips_healthcare_entity_fact(tmp_path: Path) -> None:
    artifacts = fixture_artifacts(tmp_path)
    detect_artifact = artifacts["detect"]

    restored = DetectArtifact.model_validate_json(detect_artifact.model_dump_json())

    assert restored.findings[0].is_healthcare_entity is True


def test_scan_cache_hit_skips_transpile(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    target_dir = tmp_path / "target"
    target_dir.mkdir()
    source_file = target_dir / "src" / "app.ts"
    source_file.parent.mkdir(parents=True, exist_ok=True)
    source_file.write_text("console.log('cache');\n", encoding="utf-8")

    output_dir = tmp_path / "out"
    config = PiranesiConfig(output=OutputConfig(output_dir=str(output_dir)))
    context = PipelineContext(
        target_dir=target_dir,
        output_dir=output_dir,
        provider=None,  # type: ignore[arg-type]
        router=None,  # type: ignore[arg-type]
        cost_tracker=SimpleNamespace(total_usd=0.0),  # type: ignore[arg-type]
        trace_writer=None,  # type: ignore[arg-type]
    )

    fake_source_map = object()
    calls = {"transpile": 0, "import_project": 0, "import_cpg": 0}

    class FakeTranspiledProject:
        def __init__(self, workspace_root: Path) -> None:
            self._workspace_root = workspace_root
            self.out_dir = workspace_root / "out"
            self.out_dir.mkdir(parents=True, exist_ok=True)
            (self.out_dir / "app.js").write_text("console.log('cache');\n", encoding="utf-8")
            self.source_map = fake_source_map
            self.failed_files: tuple[Path, ...] = ()

        def cleanup(self) -> None:
            shutil.rmtree(self._workspace_root, ignore_errors=True)

    class FakeJoernServer:
        def __init__(self, *, config: Any | None = None) -> None:
            _ = config

        def __enter__(self) -> FakeJoernServer:
            return self

        def __exit__(self, exc_type: object, exc: object, traceback: object) -> None:
            _ = (exc_type, exc, traceback)

        def version(self) -> str:
            return "joern-test-1"

        def import_project(
            self,
            path: str | Path,
            *,
            language: str | None = None,
            project_name: str | None = None,
        ) -> dict[str, object]:
            _ = (path, language, project_name)
            calls["import_project"] += 1
            return {"success": True}

        def import_cpg(self, path: str | Path) -> dict[str, object]:
            _ = path
            calls["import_cpg"] += 1
            return {"success": True}

        def export_cpg(self, destination: str | Path, *, project_name: str) -> Path:
            _ = project_name
            destination_path = Path(destination)
            destination_path.mkdir(parents=True, exist_ok=True)
            cpg_path = destination_path / "cpg.bin"
            cpg_path.write_text("cached cpg", encoding="utf-8")
            return cpg_path

    def _transpile_project(
        target: Path,
        *,
        changed_files: set[Path] | None = None,
    ) -> FakeTranspiledProject:
        _ = (target, changed_files)
        calls["transpile"] += 1
        return FakeTranspiledProject(tmp_path / f"workspace-{calls['transpile']}")

    def _build_scan_result(
        server: Any,
        *,
        project_root: Path,
        metadata: Any,
        joern_project_root: Path,
        source_map: object | None = None,
        **_: Any,
    ) -> ScanResult:
        _ = (server, joern_project_root, source_map)
        return ScanResult(
            project_root=str(project_root),
            files_scanned=[str(source_file)],
            call_graph={},
            entry_points=[],
            attack_surface=[],
            metadata=metadata,
        )

    monkeypatch.setattr(pipeline_module, "transpile_project", _transpile_project)
    monkeypatch.setattr(pipeline_module, "JoernServer", FakeJoernServer)
    monkeypatch.setattr(pipeline_module, "build_scan_result", _build_scan_result)
    monkeypatch.setattr(pipeline_module, "resolve_frameworks", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "get_source_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "get_sink_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "get_sanitizer_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "_incremental_changed_files", lambda incremental: None)
    monkeypatch.setattr(
        pipeline_module.SourceMap,
        "from_directory",
        classmethod(lambda cls, path: fake_source_map),
    )

    first = pipeline_module._run_scan_stage(context, config, None)
    second = pipeline_module._run_scan_stage(context, config, None)

    assert first.cache_status == "MISS"
    assert second.cache_status == "HIT"
    assert calls == {"transpile": 1, "import_project": 1, "import_cpg": 1}


def test_scan_stage_skips_transpile_for_java_projects(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    target_dir = tmp_path / "spring-app"
    source_dir = target_dir / "src" / "main" / "java" / "com" / "example"
    test_dir = target_dir / "src" / "test" / "java" / "com" / "example"
    source_dir.mkdir(parents=True)
    test_dir.mkdir(parents=True)
    (target_dir / "pom.xml").write_text(
        "<project><dependencies><dependency><artifactId>spring-boot-starter-web</artifactId>"
        "</dependency></dependencies></project>",
        encoding="utf-8",
    )
    (source_dir / "App.java").write_text("class App {}\n", encoding="utf-8")
    (test_dir / "AppTest.java").write_text("class AppTest {}\n", encoding="utf-8")

    output_dir = tmp_path / "out"
    config = PiranesiConfig(output=OutputConfig(output_dir=str(output_dir)))
    context = PipelineContext(
        target_dir=target_dir,
        output_dir=output_dir,
        provider=None,  # type: ignore[arg-type]
        router=None,  # type: ignore[arg-type]
        cost_tracker=SimpleNamespace(total_usd=0.0),  # type: ignore[arg-type]
        trace_writer=None,  # type: ignore[arg-type]
        use_cache=False,
    )
    captured: dict[str, object] = {}

    class FakeJoernServer:
        def __init__(self, *, config: Any | None = None) -> None:
            _ = config

        def __enter__(self) -> FakeJoernServer:
            return self

        def __exit__(self, exc_type: object, exc: object, traceback: object) -> None:
            _ = (exc_type, exc, traceback)

        def version(self) -> str:
            return "joern-test-java"

        def import_project(
            self,
            path: str | Path,
            *,
            language: str | None = None,
            project_name: str | None = None,
            frontend_args: tuple[str, ...] | list[str] = (),
        ) -> dict[str, object]:
            captured["path"] = Path(path)
            captured["language"] = language
            captured["project_name"] = project_name
            captured["frontend_args"] = tuple(frontend_args)
            return {"success": True}

    def _transpile_project(*args: object, **kwargs: object) -> object:
        raise AssertionError("transpile_project should not run for Java projects")

    def _build_scan_result(
        server: Any,
        *,
        project_root: Path,
        metadata: Any,
        joern_project_root: Path,
        source_map: object | None = None,
        **_: Any,
    ) -> ScanResult:
        _ = (server, source_map)
        return ScanResult(
            project_root=str(project_root),
            files_scanned=[
                str(joern_project_root / "src" / "main" / "java" / "com" / "example" / "App.java")
            ],
            call_graph={},
            entry_points=[],
            attack_surface=[],
            metadata=metadata,
        )

    monkeypatch.setattr(pipeline_module, "JoernServer", FakeJoernServer)
    monkeypatch.setattr(pipeline_module, "transpile_project", _transpile_project)
    monkeypatch.setattr(pipeline_module, "build_scan_result", _build_scan_result)
    monkeypatch.setattr(
        pipeline_module, "resolve_frameworks", lambda *_args, **_kwargs: ("springboot",)
    )
    monkeypatch.setattr(pipeline_module, "get_source_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "get_sink_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "get_sanitizer_specs", lambda *_args, **_kwargs: [])

    result = pipeline_module._run_scan_stage(context, config, None)

    assert result.success is True
    assert result.cache_status == "BYPASS"
    assert captured == {
        "path": target_dir,
        "language": "java",
        "project_name": None,
        "frontend_args": (
            "--exclude",
            "piranesi-output",
            "--exclude",
            ".piranesi-cache",
            "--exclude",
            ".piranesi-out",
            "--exclude",
            ".piranesi-trace*",
            "--exclude",
            "src/test",
        ),
    }


def test_scan_stage_skips_transpile_for_go_projects(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    target_dir = tmp_path / "gin-app"
    vendor_dir = target_dir / "vendor" / "example.com" / "dep"
    target_dir.mkdir()
    vendor_dir.mkdir(parents=True)
    (target_dir / "go.mod").write_text(
        "\n".join(
            [
                "module example.com/app",
                "",
                "go 1.21",
                "",
                "require github.com/gin-gonic/gin v1.10.0",
            ]
        ),
        encoding="utf-8",
    )
    (target_dir / "main.go").write_text("package main\n", encoding="utf-8")
    (vendor_dir / "unsafe.go").write_text("package dep\n", encoding="utf-8")

    output_dir = tmp_path / "out"
    config = PiranesiConfig(output=OutputConfig(output_dir=str(output_dir)))
    context = PipelineContext(
        target_dir=target_dir,
        output_dir=output_dir,
        provider=None,  # type: ignore[arg-type]
        router=None,  # type: ignore[arg-type]
        cost_tracker=SimpleNamespace(total_usd=0.0),  # type: ignore[arg-type]
        trace_writer=None,  # type: ignore[arg-type]
        use_cache=False,
    )
    captured: dict[str, object] = {}

    class FakeJoernServer:
        def __init__(self, *, config: Any | None = None) -> None:
            _ = config

        def __enter__(self) -> FakeJoernServer:
            return self

        def __exit__(self, exc_type: object, exc: object, traceback: object) -> None:
            _ = (exc_type, exc, traceback)

        def version(self) -> str:
            return "joern-test-go"

        def import_project(
            self,
            path: str | Path,
            *,
            language: str | None = None,
            project_name: str | None = None,
            frontend_args: tuple[str, ...] | list[str] = (),
        ) -> dict[str, object]:
            captured["path"] = Path(path)
            captured["language"] = language
            captured["project_name"] = project_name
            captured["frontend_args"] = tuple(frontend_args)
            return {"success": True}

    def _transpile_project(*args: object, **kwargs: object) -> object:
        raise AssertionError("transpile_project should not run for Go projects")

    def _build_scan_result(
        server: Any,
        *,
        project_root: Path,
        metadata: Any,
        joern_project_root: Path,
        source_map: object | None = None,
        **_: Any,
    ) -> ScanResult:
        _ = (server, source_map)
        return ScanResult(
            project_root=str(project_root),
            files_scanned=[str(joern_project_root / "main.go")],
            call_graph={},
            entry_points=[],
            attack_surface=[],
            metadata=metadata,
        )

    monkeypatch.setattr(pipeline_module, "JoernServer", FakeJoernServer)
    monkeypatch.setattr(pipeline_module, "transpile_project", _transpile_project)
    monkeypatch.setattr(pipeline_module, "build_scan_result", _build_scan_result)
    monkeypatch.setattr(
        pipeline_module,
        "resolve_frameworks",
        lambda *_args, **_kwargs: ("gin", "go-stdlib"),
    )
    monkeypatch.setattr(pipeline_module, "get_source_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "get_sink_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "get_sanitizer_specs", lambda *_args, **_kwargs: [])

    result = pipeline_module._run_scan_stage(context, config, None)

    assert result.success is True
    assert result.cache_status == "BYPASS"
    assert captured == {
        "path": target_dir,
        "language": "go",
        "project_name": None,
        "frontend_args": (
            "--exclude",
            "piranesi-output",
            "--exclude",
            ".piranesi-cache",
            "--exclude",
            ".piranesi-out",
            "--exclude",
            ".piranesi-trace*",
            "--exclude",
            "vendor",
        ),
    }


def test_scan_stage_skips_transpile_and_excludes_python_virtualenvs(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    target_dir = tmp_path / "flask-app"
    target_dir.mkdir()
    app_file = target_dir / "app.py"
    app_file.write_text("from flask import Flask\napp = Flask(__name__)\n", encoding="utf-8")
    (target_dir / "requirements.txt").write_text("flask==3.0.0\n", encoding="utf-8")
    (target_dir / "venv" / "lib").mkdir(parents=True)
    (target_dir / "venv" / "lib" / "ignored.py").write_text("print('ignored')\n", encoding="utf-8")
    (target_dir / ".venv" / "lib").mkdir(parents=True)
    (target_dir / ".venv" / "lib" / "ignored.py").write_text("print('ignored')\n", encoding="utf-8")
    (target_dir / "site-packages").mkdir(parents=True)
    (target_dir / "site-packages" / "ignored.py").write_text("print('ignored')\n", encoding="utf-8")

    output_dir = tmp_path / "out"
    config = PiranesiConfig(output=OutputConfig(output_dir=str(output_dir)))
    context = PipelineContext(
        target_dir=target_dir,
        output_dir=output_dir,
        provider=None,  # type: ignore[arg-type]
        router=None,  # type: ignore[arg-type]
        cost_tracker=SimpleNamespace(total_usd=0.0),  # type: ignore[arg-type]
        trace_writer=None,  # type: ignore[arg-type]
        use_cache=False,
    )
    captured: dict[str, object] = {}

    class FakeJoernServer:
        def __init__(self, *, config: Any | None = None) -> None:
            _ = config

        def __enter__(self) -> FakeJoernServer:
            return self

        def __exit__(self, exc_type: object, exc: object, traceback: object) -> None:
            _ = (exc_type, exc, traceback)

        def version(self) -> str:
            return "joern-test-python"

        def import_project(
            self,
            path: str | Path,
            *,
            language: str | None = None,
            project_name: str | None = None,
            frontend_args: tuple[str, ...] | list[str] = (),
        ) -> dict[str, object]:
            workspace_path = Path(path)
            captured["path"] = workspace_path
            captured["language"] = language
            captured["project_name"] = project_name
            captured["frontend_args"] = tuple(frontend_args)
            captured["workspace_files"] = sorted(
                file.relative_to(workspace_path).as_posix()
                for file in workspace_path.rglob("*")
                if file.is_file()
            )
            return {"success": True}

    def _transpile_project(*args: object, **kwargs: object) -> object:
        raise AssertionError("transpile_project should not run for Python projects")

    def _build_scan_result(
        server: Any,
        *,
        project_root: Path,
        metadata: Any,
        joern_project_root: Path,
        source_map: object | None = None,
        **_: Any,
    ) -> ScanResult:
        _ = (server, source_map)
        return ScanResult(
            project_root=str(project_root),
            files_scanned=[str(joern_project_root / "app.py")],
            call_graph={},
            entry_points=[],
            attack_surface=[],
            metadata=metadata,
        )

    monkeypatch.setattr(pipeline_module, "JoernServer", FakeJoernServer)
    monkeypatch.setattr(pipeline_module, "transpile_project", _transpile_project)
    monkeypatch.setattr(pipeline_module, "build_scan_result", _build_scan_result)
    monkeypatch.setattr(pipeline_module, "resolve_frameworks", lambda *_args, **_kwargs: ("flask",))
    monkeypatch.setattr(pipeline_module, "get_source_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "get_sink_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "get_sanitizer_specs", lambda *_args, **_kwargs: [])

    result = pipeline_module._run_scan_stage(context, config, None)

    assert result.success is True
    assert result.cache_status == "BYPASS"
    assert captured["language"] == "python"
    assert captured["project_name"] is None
    assert captured["frontend_args"] == ()
    assert captured["workspace_files"] == ["app.py"]


def test_parallel_legal_patch_matches_sequential_results(tmp_path: Path) -> None:
    target_dir = tmp_path / "target"
    target_dir.mkdir()
    artifacts = fixture_artifacts(target_dir)
    config = PiranesiConfig(output=OutputConfig(output_dir=str(tmp_path / "parallel-out")))

    def _context(output_dir: Path) -> PipelineContext:
        return PipelineContext(
            target_dir=target_dir,
            output_dir=output_dir,
            provider=None,  # type: ignore[arg-type]
            router=None,  # type: ignore[arg-type]
            cost_tracker=SimpleNamespace(total_usd=0.0),  # type: ignore[arg-type]
            trace_writer=None,  # type: ignore[arg-type]
        )

    parallel_started = {
        "legal": threading.Event(),
        "patch": threading.Event(),
    }

    def _make_registry(
        context: PipelineContext,
        *,
        require_parallel_start: bool,
    ) -> OrderedDict[str, PipelineStage]:
        def _stage_runner(stage_name: str, artifact: Any) -> Any:
            def _run(config: Any, prev: StageResult | None) -> StageResult:
                _ = (config, prev)
                if stage_name == "legal":
                    parallel_started["legal"].set()
                    if require_parallel_start:
                        assert parallel_started["patch"].wait(1.0)
                if stage_name == "patch":
                    parallel_started["patch"].set()
                    if require_parallel_start:
                        assert parallel_started["legal"].wait(1.0)
                return StageResult(
                    stage=stage_name,
                    success=True,
                    artifact=artifact,
                    elapsed_s=0.05,
                )

            return _run

        def _report_runner(config: Any, prev: StageResult | None) -> StageResult:
            _ = (config, prev)
            report = build_report(
                scan_result=context.stage_outputs["scan"],
                detected_findings=context.stage_outputs["detect"].findings,
                confirmed_findings=context.stage_outputs["verify"].findings,
                legal_assessments=context.stage_outputs["legal"].assessments,
                patch_results=context.stage_outputs["patch"].patches,
                target_dir=context.target_dir,
                total_llm_cost_usd=0.0,
                duration_s=0.25,
                stage_timings_s={"scan": 0.05, "detect": 0.05, "triage": 0.05, "verify": 0.05},
            )
            write_report_outputs(report, context.output_dir)
            return StageResult(stage="report", success=True, artifact=report, elapsed_s=0.05)

        return OrderedDict(
            (
                (
                    "scan",
                    PipelineStage("scan", ScanResult, _stage_runner("scan", artifacts["scan"])),
                ),
                (
                    "detect",
                    PipelineStage(
                        "detect",
                        DetectArtifact,
                        _stage_runner("detect", artifacts["detect"]),
                    ),
                ),
                (
                    "triage",
                    PipelineStage(
                        "triage",
                        TriageArtifact,
                        _stage_runner("triage", artifacts["triage"]),
                    ),
                ),
                (
                    "verify",
                    PipelineStage(
                        "verify",
                        VerifyArtifact,
                        _stage_runner("verify", artifacts["verify"]),
                    ),
                ),
                (
                    "legal",
                    PipelineStage(
                        "legal",
                        LegalArtifact,
                        _stage_runner("legal", artifacts["legal"]),
                    ),
                ),
                (
                    "patch",
                    PipelineStage(
                        "patch",
                        PatchArtifact,
                        _stage_runner("patch", artifacts["patch"]),
                    ),
                ),
                ("report", PipelineStage("report", PiranesiReport, _report_runner)),
            )
        )

    parallel_context = _context(tmp_path / "parallel-out")
    parallel_result = run_pipeline(
        config,
        parallel_context,
        stage_registry=_make_registry(parallel_context, require_parallel_start=True),
    )
    assert parallel_result.failed_stage is None

    sequential_context = _context(tmp_path / "sequential-out")
    sequential_result = run_pipeline(
        config.model_copy(
            update={
                "output": config.output.model_copy(
                    update={"output_dir": str(sequential_context.output_dir)}
                )
            }
        ),
        sequential_context,
        stage_registry=_make_registry(sequential_context, require_parallel_start=False),
        resume=True,
    )
    assert sequential_result.failed_stage is None

    assert (
        parallel_context.stage_outputs["legal"].model_dump()
        == sequential_context.stage_outputs["legal"].model_dump()
    )
    assert (
        parallel_context.stage_outputs["patch"].model_dump()
        == sequential_context.stage_outputs["patch"].model_dump()
    )
    parallel_report = parallel_context.stage_outputs["report"]
    sequential_report = sequential_context.stage_outputs["report"]
    assert (
        parallel_report.executive_summary.model_dump()
        == sequential_report.executive_summary.model_dump()
    )
    assert parallel_report.findings[0].model_dump() == sequential_report.findings[0].model_dump()


def _build_fake_registry(
    context: PipelineContext,
    *,
    artifacts: dict[str, Any],
    calls: list[str],
    fail_stage: str | None = None,
) -> OrderedDict[str, PipelineStage]:
    def _runner(stage_name: str, artifact: Any) -> Any:
        def _run(config: Any, prev: Any) -> StageResult:
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
