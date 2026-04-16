from __future__ import annotations

from pathlib import Path

import pytest

from piranesi.config import OutputConfig, PiranesiConfig, ScanConfig, TraceConfig
from piranesi.models import ScanMetadata, ScanResult
from piranesi.pipeline import DetectArtifact, StageResult
from piranesi.watch import run_watch_mode


def test_watch_mode_triggers_incremental_scan_for_changed_files(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    target_dir = tmp_path / "project"
    output_dir = tmp_path / "output"
    target_dir.mkdir()
    watched_file = target_dir / "app.ts"
    watched_file.write_text("export const value = 1;\n", encoding="utf-8")
    config = PiranesiConfig(
        output=OutputConfig(output_dir=str(output_dir)),
        trace=TraceConfig(enabled=False),
        scan=ScanConfig(incremental=True),
    )

    captured_changed_files: list[set[Path] | None] = []

    def _fake_iter_watch_batches(
        target_dir_arg: Path,
        *,
        config: PiranesiConfig,
        filter_glob: str | None,
        debounce_ms: int,
    ) -> object:
        _ = (target_dir_arg, config, filter_glob, debounce_ms)
        watched_file.write_text("export const value = 2;\n", encoding="utf-8")
        yield {watched_file}

    def _fake_scan_stage(
        context: object,
        config: PiranesiConfig,
        prev_result: object,
    ) -> StageResult:
        _ = (config, prev_result)
        from piranesi.pipeline import _incremental_changed_files

        incremental = context.incremental
        captured_changed_files.append(_incremental_changed_files(incremental))
        return StageResult(
            stage="scan",
            success=True,
            artifact=_scan_artifact(target_dir),
            elapsed_s=0.01,
        )

    def _fake_detect_stage(
        context: object,
        config: PiranesiConfig,
        prev_result: object,
    ) -> StageResult:
        _ = (context, config, prev_result)
        return StageResult(
            stage="detect",
            success=True,
            artifact=DetectArtifact(findings=[]),
            elapsed_s=0.01,
        )

    monkeypatch.setattr("piranesi.watch._iter_watch_batches", _fake_iter_watch_batches)
    monkeypatch.setattr("piranesi.watch._run_scan_stage", _fake_scan_stage)
    monkeypatch.setattr("piranesi.watch._run_detect_stage", _fake_detect_stage)
    monkeypatch.setattr("piranesi.watch._validate_watch_dependency", lambda: None)

    summary = run_watch_mode(
        target_dir,
        config=config,
        output_dir=output_dir,
        max_scans=2,
        render_ui=False,
    )

    assert summary.scans == 2
    assert summary.exit_code == 0
    assert captured_changed_files[0] is None
    assert captured_changed_files[1] == {Path("app.ts")}


def test_watch_mode_limits_invalidation_to_changed_batch(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    target_dir = tmp_path / "project"
    output_dir = tmp_path / "output"
    target_dir.mkdir()
    watched_file = target_dir / "app.ts"
    sibling_file = target_dir / "helper.ts"
    watched_file.write_text("export const value = 1;\n", encoding="utf-8")
    sibling_file.write_text("export const helper = 1;\n", encoding="utf-8")
    config = PiranesiConfig(
        output=OutputConfig(output_dir=str(output_dir)),
        trace=TraceConfig(enabled=False),
        scan=ScanConfig(incremental=True),
    )

    captured_changed_files: list[set[Path] | None] = []

    def _fake_iter_watch_batches(
        target_dir_arg: Path,
        *,
        config: PiranesiConfig,
        filter_glob: str | None,
        debounce_ms: int,
    ) -> object:
        _ = (target_dir_arg, config, filter_glob, debounce_ms)
        watched_file.write_text("export const value = 2;\n", encoding="utf-8")
        sibling_file.write_text("export const helper = 2;\n", encoding="utf-8")
        yield {watched_file}

    def _fake_scan_stage(
        context: object,
        config: PiranesiConfig,
        prev_result: object,
    ) -> StageResult:
        _ = (config, prev_result)
        from piranesi.pipeline import _incremental_changed_files

        incremental = context.incremental
        captured_changed_files.append(_incremental_changed_files(incremental))
        return StageResult(
            stage="scan",
            success=True,
            artifact=_scan_artifact(target_dir),
            elapsed_s=0.01,
        )

    def _fake_detect_stage(
        context: object,
        config: PiranesiConfig,
        prev_result: object,
    ) -> StageResult:
        _ = (context, config, prev_result)
        return StageResult(
            stage="detect",
            success=True,
            artifact=DetectArtifact(findings=[]),
            elapsed_s=0.01,
        )

    monkeypatch.setattr("piranesi.watch._iter_watch_batches", _fake_iter_watch_batches)
    monkeypatch.setattr("piranesi.watch._run_scan_stage", _fake_scan_stage)
    monkeypatch.setattr("piranesi.watch._run_detect_stage", _fake_detect_stage)
    monkeypatch.setattr("piranesi.watch._validate_watch_dependency", lambda: None)

    summary = run_watch_mode(
        target_dir,
        config=config,
        output_dir=output_dir,
        max_scans=2,
        render_ui=False,
    )

    assert summary.scans == 2
    assert summary.exit_code == 0
    assert captured_changed_files[0] is None
    assert captured_changed_files[1] == {Path("app.ts")}


def _scan_artifact(target_dir: Path) -> ScanResult:
    return ScanResult(
        project_root=str(target_dir.resolve(strict=False)),
        files_scanned=[],
        call_graph={},
        entry_points=[],
        attack_surface=[],
        metadata=ScanMetadata(
            timestamp="2026-04-10T00:00:00Z",
            duration_ms=10,
            tree_sitter_version="test",
            piranesi_version="test",
            files_parsed=0,
            parse_errors=0,
            config_hash="test",
        ),
    )
