from __future__ import annotations

import shutil
import subprocess
from pathlib import Path
from types import SimpleNamespace

import pytest

import piranesi.pipeline as pipeline_module
from piranesi.config import OutputConfig, PiranesiConfig, TraceConfig
from piranesi.llm.cost import CostTracker
from piranesi.pipeline import PipelineContext, _run_detect_stage
from piranesi.scan.monorepo import detect_monorepo_manifest

_FIXTURE_DIR = Path(__file__).resolve().parents[1] / "fixtures" / "monorepo" / "npm_workspace"


@pytest.fixture()
def npm_workspace(tmp_path: Path) -> Path:
    workspace = tmp_path / "workspace"
    shutil.copytree(_FIXTURE_DIR, workspace)
    return workspace.resolve(strict=False)


def test_detect_monorepo_manifest_builds_workspace_graph(npm_workspace: Path) -> None:
    manifest = detect_monorepo_manifest(npm_workspace)

    assert manifest is not None
    assert manifest.detected_tool == "npm-workspaces"
    assert {package.name for package in manifest.packages} == {
        "@test/shared-lib",
        "@test/api",
        "@test/frontend",
    }

    packages_by_name = {package.name: package for package in manifest.packages}
    assert packages_by_name["@test/shared-lib"].internal_deps == []
    assert packages_by_name["@test/api"].internal_deps == ["@test/shared-lib"]
    assert packages_by_name["@test/frontend"].internal_deps == []
    assert ("@test/api", "@test/shared-lib") in manifest.dependency_edges


def test_monorepo_detect_stage_builds_package_and_cross_package_findings(
    monkeypatch: pytest.MonkeyPatch,
    npm_workspace: Path,
    tmp_path: Path,
) -> None:
    config = PiranesiConfig(
        output=OutputConfig(output_dir=str(tmp_path / "out")),
        trace=TraceConfig(enabled=False),
    )
    manifest = detect_monorepo_manifest(npm_workspace)
    assert manifest is not None

    monkeypatch.setattr(pipeline_module, "_detect_findings_for_target", lambda *args, **kwargs: [])

    context = PipelineContext(
        target_dir=npm_workspace,
        output_dir=tmp_path / "out",
        provider=SimpleNamespace(),
        router=SimpleNamespace(resolve=lambda _stage: None),
        cost_tracker=CostTracker(),
        trace_writer=SimpleNamespace(),
        monorepo_manifest=manifest,
    )

    result = _run_detect_stage(context, config, None)
    findings = result.artifact.findings

    package_findings = [
        finding
        for finding in findings
        if finding.metadata.get("package") == "@test/shared-lib"
        and finding.metadata.get("workspace_export")
    ]
    assert package_findings

    cross_package = next(
        finding for finding in findings if finding.metadata.get("cross_package") is True
    )
    assert cross_package.metadata["source_package"] == "@test/api"
    assert cross_package.metadata["sink_package"] == "@test/shared-lib"
    assert cross_package.vuln_class == "CWE-89: SQL Injection"
    assert any(step.operation == "internal_dependency_call" for step in cross_package.taint_path)
    assert not any(finding.metadata.get("package") == "@test/frontend" for finding in findings)


def test_monorepo_package_flag_scans_only_selected_package(
    monkeypatch: pytest.MonkeyPatch,
    npm_workspace: Path,
    tmp_path: Path,
) -> None:
    config = PiranesiConfig(
        output=OutputConfig(output_dir=str(tmp_path / "out")),
        trace=TraceConfig(enabled=False),
    )
    manifest = detect_monorepo_manifest(npm_workspace)
    assert manifest is not None

    scanned_targets: list[Path] = []

    def _fake_detect_findings(
        context: PipelineContext,
        config: PiranesiConfig,
        target_dir: Path,
        *,
        changed_files: set[Path] | None = None,
    ) -> list[object]:
        _ = (context, config, changed_files)
        scanned_targets.append(target_dir.resolve(strict=False))
        return []

    monkeypatch.setattr(pipeline_module, "_detect_findings_for_target", _fake_detect_findings)

    context = PipelineContext(
        target_dir=npm_workspace,
        output_dir=tmp_path / "out",
        provider=SimpleNamespace(),
        router=SimpleNamespace(resolve=lambda _stage: None),
        cost_tracker=CostTracker(),
        trace_writer=SimpleNamespace(),
        monorepo_manifest=manifest,
        monorepo_package_name="frontend",
    )

    result = _run_detect_stage(context, config, None)

    assert result.artifact.findings == []
    assert scanned_targets == [
        (npm_workspace / "packages" / "frontend").resolve(strict=False),
    ]


def test_monorepo_changed_packages_scans_only_changed_frontend_package(
    monkeypatch: pytest.MonkeyPatch,
    npm_workspace: Path,
    tmp_path: Path,
) -> None:
    _init_git_repo(npm_workspace)
    frontend_file = npm_workspace / "packages" / "frontend" / "src" / "index.js"
    frontend_file.write_text(
        "export function renderUser(id) {\n  return `<section>${id}</section>`;\n}\n",
        encoding="utf-8",
    )

    config = PiranesiConfig(
        output=OutputConfig(output_dir=str(tmp_path / "out")),
        trace=TraceConfig(enabled=False),
    )
    manifest = detect_monorepo_manifest(npm_workspace)
    assert manifest is not None

    scanned_targets: list[Path] = []

    def _fake_detect_findings(
        context: PipelineContext,
        config: PiranesiConfig,
        target_dir: Path,
        *,
        changed_files: set[Path] | None = None,
    ) -> list[object]:
        _ = (context, config, changed_files)
        scanned_targets.append(target_dir.resolve(strict=False))
        return []

    monkeypatch.setattr(pipeline_module, "_detect_findings_for_target", _fake_detect_findings)

    context = PipelineContext(
        target_dir=npm_workspace,
        output_dir=tmp_path / "out",
        provider=SimpleNamespace(),
        router=SimpleNamespace(resolve=lambda _stage: None),
        cost_tracker=CostTracker(),
        trace_writer=SimpleNamespace(),
        monorepo_manifest=manifest,
        changed_packages_only=True,
    )

    result = _run_detect_stage(context, config, None)

    assert result.artifact.findings == []
    assert scanned_targets == [
        (npm_workspace / "packages" / "frontend").resolve(strict=False),
    ]


def _init_git_repo(workspace: Path) -> None:
    for command in (
        ["git", "init"],
        ["git", "config", "user.email", "test@example.com"],
        ["git", "config", "user.name", "Piranesi Test"],
        ["git", "add", "."],
        ["git", "commit", "-m", "initial"],
    ):
        subprocess.run(
            command,
            cwd=workspace,
            capture_output=True,
            check=True,
            text=True,
        )
