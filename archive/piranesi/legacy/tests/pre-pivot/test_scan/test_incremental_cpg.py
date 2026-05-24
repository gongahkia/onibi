from __future__ import annotations

from pathlib import Path
from subprocess import CompletedProcess
from types import SimpleNamespace

import pytest

from piranesi.config import OutputConfig, PiranesiConfig, ScanConfig, TraceConfig
from piranesi.llm.cost import CostTracker
from piranesi.models import (
    ScanMetadata,
    ScannedFunction,
    ScanResult,
    SourceLocation,
    TaintSink,
    TaintSource,
    TaintStep,
)
from piranesi.models.finding import CandidateFinding
from piranesi.pipeline import (
    PipelineContext,
    _run_detect_stage,
    _run_scan_stage,
    prepare_incremental_state,
)
from piranesi.scan.cpg_cache import clear_cache, load_cached_cpg


@pytest.fixture()
def incremental_cpg_project(tmp_path: Path) -> tuple[Path, list[Path]]:
    target_dir = tmp_path / "project"
    target_dir.mkdir()
    files: list[Path] = []
    for index in range(25):
        path = target_dir / f"file_{index}.ts"
        path.write_text(
            "\n".join(
                [
                    f"export function handler_{index}(value) {{",
                    f"  const local_{index} = value;",
                    f"  return `file_{index}:${{local_{index}}}`;",
                    "}",
                    "",
                ]
            ),
            encoding="utf-8",
        )
        files.append(path)
    return target_dir.resolve(strict=False), files


def test_cpg_cache_round_trip_preserves_graph(
    incremental_cpg_project: tuple[Path, list[Path]],
    tmp_path: Path,
) -> None:
    target_dir, files = incremental_cpg_project
    config = PiranesiConfig(
        output=OutputConfig(output_dir=str(tmp_path / "out")),
        trace=TraceConfig(enabled=False),
        scan=ScanConfig(incremental=True),
    )
    project_hash, cpg = load_cached_cpg(target_dir, config, joern_version="test")
    assert project_hash
    assert cpg is None

    from piranesi.scan.cpg_cache import write_cached_cpg
    from piranesi.scan.cpg_graph import apply_findings_to_cpg, build_cpg_from_scan_result

    file_a = files[0]
    file_b = files[1]
    function_a = f"{file_a.relative_to(target_dir).as_posix()}::handler_0(value)"
    function_b = f"{file_b.relative_to(target_dir).as_posix()}::handler_1(value)"
    scan_artifact = ScanResult(
        project_root=str(target_dir),
        files_scanned=[str(file_a), str(file_b)],
        call_graph={function_a: [function_b], function_b: []},
        functions=[
            ScannedFunction(
                function_id=function_a,
                name="handler_0",
                location=SourceLocation(
                    file=str(file_a),
                    line=1,
                    column=1,
                    snippet="export function handler_0(value) {",
                ),
                parameters=["value"],
            ),
            ScannedFunction(
                function_id=function_b,
                name="handler_1",
                location=SourceLocation(
                    file=str(file_b),
                    line=1,
                    column=1,
                    snippet="export function handler_1(value) {",
                ),
                parameters=["value"],
            ),
        ],
        entry_points=[],
        attack_surface=[],
        metadata=ScanMetadata(
            timestamp="2026-04-11T00:00:00+00:00",
            duration_ms=1,
            tree_sitter_version="test",
            piranesi_version="0.2.0",
            files_parsed=2,
            parse_errors=0,
            config_hash="cfg",
        ),
    )
    finding = CandidateFinding(
        id="finding-1",
        vuln_class="CWE-79",
        source=TaintSource(
            location=SourceLocation(
                file=str(file_a),
                line=1,
                column=1,
                snippet="handler_0(value)",
            ),
            source_type="req.body",
            data_categories=["identifier"],
            parameter_name="value",
        ),
        sink=TaintSink(
            location=SourceLocation(
                file=str(file_b),
                line=1,
                column=1,
                snippet="handler_1(value)",
            ),
            sink_type="html_output",
            api_name="res.send",
        ),
        taint_path=[
            TaintStep(
                location=SourceLocation(
                    file=str(file_a),
                    line=1,
                    column=1,
                    snippet="handler_0(value)",
                ),
                operation="call_arg",
                taint_state="tainted",
                through_function=function_a,
            ),
            TaintStep(
                location=SourceLocation(
                    file=str(file_b),
                    line=1,
                    column=1,
                    snippet="handler_1(value)",
                ),
                operation="call_arg",
                taint_state="tainted",
                through_function=function_b,
            ),
        ],
        path_conditions=[],
        confidence=0.9,
        severity="medium",
    )

    cpg = build_cpg_from_scan_result(
        scan_artifact,
        project_root=target_dir,
        piranesi_version="0.2.0",
        joern_version="test",
        config_hash="cfg",
    )
    apply_findings_to_cpg(cpg, [finding], affected_function_ids=set(cpg.functions))
    write_cached_cpg(target_dir, config, cpg, joern_version="test")

    _, loaded = load_cached_cpg(target_dir, config, joern_version="test")
    assert loaded is not None
    assert set(loaded.functions) == {function_a, function_b}
    assert {(edge.caller_id, edge.callee_id) for edge in loaded.call_edges} == {
        (function_a, function_b)
    }
    assert [flow.finding_id for flow in loaded.taint_flows] == ["finding-1"]
    clear_cache(target_dir)


@pytest.mark.parametrize(
    ("changed_count", "budget_seconds"),
    [(1, 3.0), (5, 10.0), (20, 25.0)],
)
def test_incremental_cpg_matches_full_scan_results_for_1_5_and_20_file_changes(
    incremental_cpg_project: tuple[Path, list[Path]],
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
    changed_count: int,
    budget_seconds: float,
) -> None:
    target_dir, source_files = incremental_cpg_project
    output_dir = tmp_path / "incremental-out"
    full_output_dir = tmp_path / "full-out"
    config = PiranesiConfig(
        output=OutputConfig(output_dir=str(output_dir)),
        trace=TraceConfig(enabled=False),
        scan=ScanConfig(incremental=True, incremental_threshold=20),
    )
    full_config = config.model_copy(
        update={
            "output": OutputConfig(output_dir=str(full_output_dir)),
            "scan": ScanConfig(incremental=False, incremental_threshold=20),
        }
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

    def _function_id_for_path(path: Path) -> str:
        relative = path.relative_to(target_dir).as_posix()
        stem = path.stem.split("_")[-1]
        return f"{relative}::handler_{stem}(value)"

    def _candidate_for_path(path: Path) -> CandidateFinding:
        function_id = _function_id_for_path(path)
        rendered = path.relative_to(target_dir).as_posix()
        location = SourceLocation(
            file=str(path),
            line=1,
            column=1,
            snippet=f"handler({rendered})",
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
            taint_path=[
                TaintStep(
                    location=location,
                    operation="call_arg",
                    taint_state="tainted",
                    through_function=function_id,
                )
            ],
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
        active_paths = selected_files[-1]
        functions = [
            ScannedFunction(
                function_id=_function_id_for_path(path),
                name=f"handler_{path.stem.split('_')[-1]}",
                location=SourceLocation(
                    file=str(path),
                    line=1,
                    column=1,
                    snippet=path.read_text(encoding="utf-8").splitlines()[0],
                ),
                parameters=["value"],
            )
            for path in active_paths
        ]
        call_graph = {function.function_id: [] for function in functions}
        return ScanResult(
            project_root=str(Path(project_root).resolve(strict=False)),
            files_scanned=[str(path) for path in active_paths],
            call_graph=call_graph,
            functions=functions,
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
    monkeypatch.setattr("piranesi.pipeline.load_rules", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(
        "piranesi.pipeline.scan_dependency_findings",
        lambda *_args, **_kwargs: SimpleNamespace(findings=[], sbom_artifacts={}),
    )
    monkeypatch.setattr(
        "piranesi.pipeline.extract_crypto_transport_findings",
        lambda *_args, **_kwargs: [],
    )
    monkeypatch.setattr("piranesi.pipeline.extract_secret_findings", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(
        "piranesi.pipeline.extract_misconfiguration_findings",
        lambda *_args, **_kwargs: [],
    )
    monkeypatch.setattr("piranesi.pipeline.extract_redos_findings", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(
        "piranesi.pipeline.extract_auth_access_findings",
        lambda *_args, **_kwargs: [],
    )

    def _context(output_root: Path, *, incremental_enabled: bool) -> PipelineContext:
        return PipelineContext(
            target_dir=target_dir,
            output_dir=output_root,
            provider=SimpleNamespace(),
            router=SimpleNamespace(resolve=lambda _stage: None),
            cost_tracker=CostTracker(),
            trace_writer=SimpleNamespace(),
            use_cache=False,
            incremental=(
                prepare_incremental_state(
                    target_dir,
                    output_root,
                    manifest_write_stage="detect",
                )
                if incremental_enabled
                else None
            ),
        )

    baseline_context = _context(output_dir, incremental_enabled=True)
    baseline_scan = _run_scan_stage(baseline_context, config, None)
    baseline_context.stage_outputs["scan"] = baseline_scan.artifact
    baseline_detect = _run_detect_stage(baseline_context, config, None)
    output_dir.mkdir(parents=True, exist_ok=True)
    (output_dir / "scan.json").write_text(
        baseline_scan.artifact.model_dump_json(indent=2),
        encoding="utf-8",
    )
    (output_dir / "detect.json").write_text(
        baseline_detect.artifact.model_dump_json(indent=2),
        encoding="utf-8",
    )

    modified = source_files[:changed_count]
    for index, path in enumerate(modified):
        path.write_text(
            "\n".join(
                [
                    f"export function handler_{path.stem.split('_')[-1]}(value) {{",
                    f"  const changed_{index} = value;",
                    f"  return `changed_{index}:${{changed_{index}}}`;",
                    "}",
                    "",
                ]
            ),
            encoding="utf-8",
        )

    incremental_context = _context(output_dir, incremental_enabled=True)
    incremental_scan = _run_scan_stage(incremental_context, config, None)
    incremental_context.stage_outputs["scan"] = incremental_scan.artifact
    incremental_detect = _run_detect_stage(incremental_context, config, None)

    full_context = _context(full_output_dir, incremental_enabled=False)
    full_scan = _run_scan_stage(full_context, full_config, None)
    full_context.stage_outputs["scan"] = full_scan.artifact
    full_detect = _run_detect_stage(full_context, full_config, None)

    assert sorted(finding.id for finding in incremental_detect.artifact.findings) == sorted(
        finding.id for finding in full_detect.artifact.findings
    )
    assert (incremental_scan.elapsed_s + incremental_detect.elapsed_s) < budget_seconds
    expected_transpile_selection = (
        None
        if changed_count >= config.scan.incremental_threshold
        else {path.relative_to(target_dir) for path in modified}
    )
    assert transpile_calls[2] == expected_transpile_selection
    assert transpile_calls[3] == expected_transpile_selection
