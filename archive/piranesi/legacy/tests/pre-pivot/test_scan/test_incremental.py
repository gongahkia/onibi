from __future__ import annotations

from pathlib import Path
from subprocess import CompletedProcess
from types import SimpleNamespace

import pytest

from piranesi.config import OutputConfig, PiranesiConfig, ScanConfig, TraceConfig
from piranesi.llm.cost import CostTracker
from piranesi.models import ScanMetadata, ScanResult, SourceLocation, TaintSink, TaintSource
from piranesi.models.finding import CandidateFinding
from piranesi.pipeline import (
    PipelineContext,
    _run_detect_stage,
    _run_scan_stage,
    prepare_incremental_state,
)
from piranesi.scan.incremental import diff_manifests, write_manifest


@pytest.fixture()
def five_file_project(tmp_path: Path) -> tuple[Path, list[Path]]:
    target_dir = tmp_path / "project"
    target_dir.mkdir()
    files: list[Path] = []
    for index in range(5):
        path = target_dir / f"file_{index}.ts"
        path.write_text(f"export const value_{index} = {index};\n", encoding="utf-8")
        files.append(path)
    return target_dir.resolve(strict=False), files


def test_write_manifest_and_diff_manifests_classifies_changes(
    five_file_project: tuple[Path, list[Path]],
    tmp_path: Path,
) -> None:
    target_dir, files = five_file_project
    baseline_output = tmp_path / "baseline"
    current_output = tmp_path / "current"

    previous_manifest = write_manifest(target_dir, baseline_output)
    files[0].write_text("export const value_0 = 42;\n", encoding="utf-8")
    files[1].unlink()
    added_file = target_dir / "added.ts"
    added_file.write_text("export const added = true;\n", encoding="utf-8")

    current_manifest = write_manifest(target_dir, current_output)
    diff = diff_manifests(previous_manifest, current_manifest)

    assert diff.added == {Path("added.ts")}
    assert diff.modified == {Path("file_0.ts")}
    assert diff.deleted == {Path("file_1.ts")}
    assert Path("file_2.ts") in diff.unchanged


def test_incremental_scan_only_retranspiles_modified_file(
    five_file_project: tuple[Path, list[Path]],
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    target_dir, source_files = five_file_project
    output_dir = tmp_path / "out"
    config = PiranesiConfig(
        output=OutputConfig(output_dir=str(output_dir)),
        trace=TraceConfig(enabled=False),
        scan=ScanConfig(incremental=True),
    )

    transpile_calls: list[set[Path] | None] = []
    selected_files: list[list[Path]] = []

    class _FakeTranspiledProject:
        def __init__(self, index: int) -> None:
            self.target_dir = target_dir
            self.workspace = SimpleNamespace(out_dir=tmp_path / f"transpiled-{index}")
            self.workspace.out_dir.mkdir(parents=True, exist_ok=True)
            self.source_map = None
            self.failed_files = ()
            self.compiler_cmd = ("tsc",)
            self.initial_result = CompletedProcess(["tsc"], 0, stdout="", stderr="")
            self.retry_result = None

        @property
        def out_dir(self) -> Path:
            return self.workspace.out_dir

        def cleanup(self) -> None:
            return None

    def _resolve_selected(changed_files: set[Path] | None) -> list[Path]:
        if changed_files is None:
            return [path.resolve(strict=False) for path in source_files]
        resolved: list[Path] = []
        for path in changed_files:
            candidate = path if path.is_absolute() else target_dir / path
            resolved.append(candidate.resolve(strict=False))
        return sorted(resolved)

    def _fake_transpile_project(
        project_dir: Path,
        *,
        changed_files: set[Path] | None = None,
        timeout: int = 300,
        log: object | None = None,
    ) -> _FakeTranspiledProject:
        _ = (timeout, log)
        assert project_dir == target_dir
        transpile_calls.append(None if changed_files is None else set(changed_files))
        selected_files.append(_resolve_selected(changed_files))
        return _FakeTranspiledProject(len(selected_files))

    class _FakeJoernServer:
        def __init__(self, *args: object, **kwargs: object) -> None:
            _ = (args, kwargs)

        def __enter__(self) -> _FakeJoernServer:
            return self

        def __exit__(self, exc_type: object, exc: object, tb: object) -> None:
            _ = (exc_type, exc, tb)
            return None

        def version(self) -> str:
            return "test"

        def import_project(self, out_dir: Path, project_name: str | None = None) -> None:
            _ = (out_dir, project_name)

    def _candidate_for_path(path: Path) -> CandidateFinding:
        rendered = path.relative_to(target_dir).as_posix()
        location = SourceLocation(
            file=str(path),
            line=1,
            column=1,
            snippet="export const value = 1;",
        )
        return CandidateFinding(
            id=rendered,
            vuln_class="CWE-79: Cross-site Scripting",
            source=TaintSource(
                location=location,
                source_type="req.body",
                data_categories=["identifier"],
                parameter_name="value",
            ),
            sink=TaintSink(
                location=location,
                sink_type="http_response",
                api_name="res.send",
            ),
            taint_path=[],
            path_conditions=[],
            confidence=0.9,
            severity="medium",
        )

    def _fake_build_scan_result(
        server: object,
        *,
        project_root: str | Path,
        metadata: ScanMetadata,
        joern_project_root: str | Path | None = None,
        source_map: object | None = None,
        source_specs: object | None = None,
        sink_specs: object | None = None,
        sanitizer_specs: object | None = None,
        candidate_findings: object | None = None,
        frameworks: object | None = None,
    ) -> ScanResult:
        _ = (
            server,
            joern_project_root,
            source_map,
            source_specs,
            sink_specs,
            sanitizer_specs,
            candidate_findings,
            frameworks,
        )
        return ScanResult(
            project_root=str(Path(project_root).resolve(strict=False)),
            files_scanned=[str(path) for path in selected_files[-1]],
            call_graph={},
            entry_points=[],
            attack_surface=[],
            metadata=metadata,
        )

    def _fake_extract_candidate_findings(
        server: object,
        *,
        joern_project_root: str | Path,
        source_map: object | None,
        source_specs: object | None,
        sink_specs: object | None,
        sanitizer_specs: object | None,
        frameworks: object | None = None,
        category_provider: object | None = None,
        category_model: object | None = None,
    ) -> list[CandidateFinding]:
        _ = (
            server,
            joern_project_root,
            source_map,
            source_specs,
            sink_specs,
            sanitizer_specs,
            frameworks,
            category_provider,
            category_model,
        )
        return [_candidate_for_path(path) for path in selected_files[-1]]

    monkeypatch.setattr("piranesi.pipeline.transpile_project", _fake_transpile_project)
    monkeypatch.setattr("piranesi.pipeline.JoernServer", _FakeJoernServer)
    monkeypatch.setattr("piranesi.pipeline.build_scan_result", _fake_build_scan_result)
    monkeypatch.setattr(
        "piranesi.pipeline.extract_candidate_findings", _fake_extract_candidate_findings
    )
    monkeypatch.setattr("piranesi.pipeline.resolve_frameworks", lambda *_args, **_kwargs: [])
    monkeypatch.setattr("piranesi.pipeline.get_source_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr("piranesi.pipeline.get_sink_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr("piranesi.pipeline.get_sanitizer_specs", lambda *_args, **_kwargs: [])

    def _context() -> PipelineContext:
        return PipelineContext(
            target_dir=target_dir,
            output_dir=output_dir,
            provider=SimpleNamespace(),
            router=SimpleNamespace(resolve=lambda _stage: None),
            cost_tracker=CostTracker(),
            trace_writer=SimpleNamespace(),
            use_cache=False,
            incremental=prepare_incremental_state(
                target_dir,
                output_dir,
                manifest_write_stage="detect",
            ),
        )

    first_context = _context()
    first_scan = _run_scan_stage(first_context, config, None)
    first_detect = _run_detect_stage(first_context, config, None)
    output_dir.mkdir(parents=True, exist_ok=True)
    (output_dir / "scan.json").write_text(
        first_scan.artifact.model_dump_json(indent=2), encoding="utf-8"
    )
    (output_dir / "detect.json").write_text(
        first_detect.artifact.model_dump_json(indent=2),
        encoding="utf-8",
    )

    modified_file = source_files[3]
    modified_file.write_text("export const value_3 = 99;\n", encoding="utf-8")

    second_context = _context()
    second_scan = _run_scan_stage(second_context, config, None)
    second_detect = _run_detect_stage(second_context, config, None)

    assert transpile_calls[0] is None
    assert transpile_calls[1] is None
    assert transpile_calls[2] == {Path("file_3.ts")}
    assert transpile_calls[3] == {Path("file_3.ts")}
    assert second_scan.artifact.files_scanned == [str(modified_file.resolve(strict=False))]
    assert len(second_detect.artifact.findings) == 5
    assert sorted(finding.id for finding in second_detect.artifact.findings) == sorted(
        path.relative_to(target_dir).as_posix() for path in source_files
    )
