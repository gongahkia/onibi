from __future__ import annotations

import json
import os
import shutil
import tempfile
import time
from collections import OrderedDict
from collections.abc import Callable, Mapping, Sequence
from concurrent.futures import ThreadPoolExecutor
from contextlib import contextmanager
from dataclasses import dataclass, field
from datetime import UTC, datetime
from fnmatch import fnmatch
from hashlib import sha256
from pathlib import Path
from typing import Any

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from piranesi import __version__
from piranesi.config import PiranesiConfig, config_hash
from piranesi.detect import (
    InlineSuppression,
    apply_suppressions,
    extract_candidate_findings,
    extract_misconfiguration_findings,
    extract_secret_findings,
    load_ignore_file,
    parse_inline_suppressions,
    scan_dependency_findings,
)
from piranesi.legal import assess_finding, build_default_engine
from piranesi.llm.cost import CostTracker
from piranesi.llm.provider import LLMProvider
from piranesi.llm.router import ModelRouter
from piranesi.models import (
    CandidateFinding,
    ConfirmedFinding,
    LegalAssessment,
    PatchResult,
    ScanMetadata,
    ScanResult,
    SourceLocation,
    TriagedFinding,
)
from piranesi.models.finding import SandboxResult
from piranesi.patch.generator import generate_patches
from piranesi.report.renderer import (
    PiranesiReport,
    build_report,
    update_report_metrics,
    write_report_outputs,
)
from piranesi.scan.framework import resolve_frameworks
from piranesi.scan.incremental import (
    FileManifest,
    IncrementalResult,
    build_manifest,
    diff_manifests,
    load_manifest,
    write_manifest,
)
from piranesi.scan.joern import JoernServer
from piranesi.scan.specs import get_sanitizer_specs, get_sink_specs, get_source_specs
from piranesi.scan.surface import build_scan_result
from piranesi.scan.transpile import SourceMap, transpile_project
from piranesi.trace import TraceWriter
from piranesi.triage import CalibratedEnsembleVoter, SkepticAgent
from piranesi.ui import file_progress, stage_header
from piranesi.verify import (
    build_baseline_payload,
    confirm_responses,
    extract_exploit_template,
    generate_reproducer_script,
    run_in_sandbox,
    solve_exploit_template,
)

STAGE_ORDER = ("scan", "detect", "triage", "verify", "legal", "patch", "report")
PARTIAL_SUMMARY_FILENAME = "_partial.json"
_CPG_CACHE_DIRNAME = "_cpg_cache"
_CPG_CACHE_METADATA_FILENAME = "metadata.json"
_CPG_CACHE_CPG_DIRNAME = "cpg"
_CPG_CACHE_TRANSPILED_DIRNAME = "transpiled"
_DEFAULT_SCAN_INCLUDE_PATTERNS = tuple(PiranesiConfig().scan.include_patterns)
_GO_INCLUDE_PATTERNS = ("**/*.go",)
_PYTHON_INCLUDE_PATTERNS = ("**/*.py",)
_PYTHON_EXCLUDE_PATTERNS = (
    "**/__pycache__/**",
    "**/.venv/**",
    "**/venv/**",
    "**/site-packages/**",
)
_GO_EXCLUDE_PATTERNS = ("**/vendor/**",)
_SOURCE_DISCOVERY_EXCLUDE_PATTERNS = (
    "**/__pycache__/**",
    "**/.venv/**",
    "**/venv/**",
    "**/site-packages/**",
    "**/node_modules/**",
    "**/dist/**",
    "**/build/**",
    "**/target/**",
    "**/vendor/**",
)
_LLM_API_ENV_VARS = (
    "OPENAI_API_KEY",
    "ANTHROPIC_API_KEY",
    "OPENROUTER_API_KEY",
    "AZURE_OPENAI_API_KEY",
    "GEMINI_API_KEY",
    "GOOGLE_API_KEY",
    "LITELLM_API_KEY",
)


@dataclass(slots=True)
class StageResult:
    stage: str
    success: bool
    artifact: Any
    elapsed_s: float
    error: str | None = None
    resumed: bool = False
    cache_status: str | None = None


StageFunc = Callable[[PiranesiConfig, StageResult | None], StageResult]


@dataclass(frozen=True, slots=True)
class PipelineStage:
    name: str
    artifact_type: type[BaseModel]
    runner: StageFunc


@dataclass(slots=True)
class PipelineContext:
    target_dir: Path
    output_dir: Path
    provider: LLMProvider
    router: ModelRouter
    cost_tracker: CostTracker
    trace_writer: TraceWriter
    stage_outputs: dict[str, BaseModel] = field(default_factory=dict)
    stage_timings_s: dict[str, float] = field(default_factory=dict)
    resumed_cost_usd: float = 0.0
    apply_patches: bool = False
    no_execute: bool = False
    use_cache: bool = True
    incremental: IncrementalState | None = None
    started_at: float = field(default_factory=time.monotonic)

    @property
    def total_cost_usd(self) -> float:
        return self.resumed_cost_usd + self.cost_tracker.total_usd


class DetectArtifact(BaseModel):
    model_config = ConfigDict(extra="forbid")

    findings: list[CandidateFinding] = Field(default_factory=list)


class TriageArtifact(BaseModel):
    model_config = ConfigDict(extra="forbid")

    findings: list[TriagedFinding] = Field(default_factory=list)


class VerifyArtifact(BaseModel):
    model_config = ConfigDict(extra="forbid")

    findings: list[ConfirmedFinding] = Field(default_factory=list)


@dataclass(frozen=True, slots=True)
class IncrementalState:
    previous_manifest: FileManifest | None
    current_manifest: FileManifest
    diff: IncrementalResult
    manifest_write_stage: str


class LegalArtifact(BaseModel):
    model_config = ConfigDict(extra="forbid")

    assessments: list[LegalAssessment] = Field(default_factory=list)


class PatchArtifact(BaseModel):
    model_config = ConfigDict(extra="forbid")

    patches: list[PatchResult] = Field(default_factory=list)


class PartialRunSummary(BaseModel):
    model_config = ConfigDict(extra="forbid")

    completed: list[str] = Field(default_factory=list)
    failed: str
    error: str
    stage_timings_s: dict[str, float] = Field(default_factory=dict)
    total_llm_cost_usd: float = 0.0


@dataclass(frozen=True, slots=True)
class _ScanSession:
    joern_project_root: Path
    source_map: SourceMap | None
    cache_status: str
    failed_files: tuple[Path, ...] = ()


@dataclass(frozen=True, slots=True)
class _DirectScanWorkspace:
    root_dir: Path

    def cleanup(self) -> None:
        shutil.rmtree(self.root_dir, ignore_errors=True)


@dataclass(slots=True)
class PipelineRunResult:
    results: list[StageResult]
    failed_stage: str | None = None
    partial_summary_path: Path | None = None

    @property
    def failed_result(self) -> StageResult | None:
        if self.failed_stage is None:
            return None
        for result in reversed(self.results):
            if result.stage == self.failed_stage:
                return result
        return None


def discover_scan_targets(target_dir: Path, config: PiranesiConfig) -> list[Path]:
    target_root = target_dir.resolve(strict=False)
    include_patterns, exclude_patterns = _effective_scan_globs(target_root, config)
    files: list[Path] = []

    for path in sorted(target_root.rglob("*")):
        if not path.is_file():
            continue
        relative = path.relative_to(target_root).as_posix()
        if not _matches_patterns(relative, include_patterns):
            continue
        if _matches_patterns(relative, exclude_patterns):
            continue
        if path.stat().st_size > config.scan.max_file_size:
            continue
        files.append(path)
    return files


def prepare_incremental_state(
    target_dir: Path,
    output_dir: Path,
    *,
    manifest_write_stage: str,
) -> IncrementalState:
    normalized_target = target_dir.resolve(strict=False)
    current_manifest = build_manifest(normalized_target)
    previous_manifest = load_manifest(output_dir, expected_target_dir=normalized_target)
    return IncrementalState(
        previous_manifest=previous_manifest,
        current_manifest=current_manifest,
        diff=diff_manifests(previous_manifest, current_manifest),
        manifest_write_stage=manifest_write_stage,
    )


def load_partial_summary(output_dir: Path) -> PartialRunSummary | None:
    path = output_dir / PARTIAL_SUMMARY_FILENAME
    if not path.exists():
        return None
    try:
        return PartialRunSummary.model_validate_json(path.read_text(encoding="utf-8"))
    except (OSError, ValidationError, json.JSONDecodeError):
        return None


def build_default_stage_registry(context: PipelineContext) -> OrderedDict[str, PipelineStage]:
    return OrderedDict(
        (
            (
                "scan",
                PipelineStage(
                    name="scan",
                    artifact_type=ScanResult,
                    runner=lambda config, prev: _run_scan_stage(context, config, prev),
                ),
            ),
            (
                "detect",
                PipelineStage(
                    name="detect",
                    artifact_type=DetectArtifact,
                    runner=lambda config, prev: _run_detect_stage(context, config, prev),
                ),
            ),
            (
                "triage",
                PipelineStage(
                    name="triage",
                    artifact_type=TriageArtifact,
                    runner=lambda config, prev: _run_triage_stage(context, config, prev),
                ),
            ),
            (
                "verify",
                PipelineStage(
                    name="verify",
                    artifact_type=VerifyArtifact,
                    runner=lambda config, prev: _run_verify_stage(context, config, prev),
                ),
            ),
            (
                "legal",
                PipelineStage(
                    name="legal",
                    artifact_type=LegalArtifact,
                    runner=lambda config, prev: _run_legal_stage(context, config, prev),
                ),
            ),
            (
                "patch",
                PipelineStage(
                    name="patch",
                    artifact_type=PatchArtifact,
                    runner=lambda config, prev: _run_patch_stage(context, config, prev),
                ),
            ),
            (
                "report",
                PipelineStage(
                    name="report",
                    artifact_type=PiranesiReport,
                    runner=lambda config, prev: _run_report_stage(context, config, prev),
                ),
            ),
        )
    )


def run_pipeline(
    config: PiranesiConfig,
    context: PipelineContext,
    *,
    stage_registry: Mapping[str, PipelineStage],
    resume: bool = False,
    render_ui: bool = False,
) -> PipelineRunResult:
    context.output_dir.mkdir(parents=True, exist_ok=True)
    results: list[StageResult] = []
    prev_result: StageResult | None = None
    failed_stage: str | None = None
    partial_summary_path: Path | None = None

    progress = None
    task_id: Any = None
    if render_ui:
        progress = file_progress(total=len(STAGE_ORDER), description="pipeline")
        progress.start()
        task_id = progress.add_task("pipeline", total=len(STAGE_ORDER))

    try:
        stage_index = 0
        while stage_index < len(STAGE_ORDER):
            stage_name = STAGE_ORDER[stage_index]
            stage = stage_registry[stage_name]
            artifact_path = context.output_dir / f"{stage_name}.json"
            if render_ui:
                stage_header(stage_name)
                assert progress is not None
                assert task_id is not None
                progress.update(task_id, description=f"{stage_name}")

            if (
                not resume
                and stage_name == "legal"
                and stage_index + 1 < len(STAGE_ORDER)
                and STAGE_ORDER[stage_index + 1] == "patch"
            ):
                parallel_results = _run_parallel_post_verify_stages(
                    context,
                    config,
                    stage_registry=stage_registry,
                    verify_result=prev_result,
                )
                for parallel_result in parallel_results:
                    if not parallel_result.success:
                        results.append(parallel_result)
                        failed_stage = parallel_result.stage
                        partial_summary_path = _save_partial_summary(
                            context,
                            results,
                            parallel_result,
                        )
                        break
                    _record_stage_success(
                        context,
                        results,
                        parallel_result,
                        artifact_path=context.output_dir / f"{parallel_result.stage}.json",
                    )
                    prev_result = parallel_result
                    if progress is not None and task_id is not None:
                        progress.advance(task_id)
                if failed_stage is not None:
                    break
                stage_index += 2
                continue

            if resume and artifact_path.exists():
                artifact = _load_artifact(artifact_path, stage.artifact_type)
                if artifact is not None:
                    elapsed_s = context.stage_timings_s.get(stage_name, 0.0)
                    prev_result = StageResult(
                        stage=stage_name,
                        success=True,
                        artifact=artifact,
                        elapsed_s=elapsed_s,
                        resumed=True,
                    )
                    context.stage_outputs[stage_name] = artifact
                    results.append(prev_result)
                    if progress is not None and task_id is not None:
                        progress.advance(task_id)
                    stage_index += 1
                    continue

            result = _execute_stage(stage, config, prev_result)
            if not result.success:
                results.append(result)
                failed_stage = stage_name
                partial_summary_path = _save_partial_summary(context, results, result)
                break

            _record_stage_success(context, results, result, artifact_path=artifact_path)
            prev_result = result
            if progress is not None and task_id is not None:
                progress.advance(task_id)
            stage_index += 1

        if failed_stage is None and "report" in context.stage_outputs:
            report = update_report_metrics(
                _require_artifact(context.stage_outputs["report"], PiranesiReport, "report"),
                total_llm_cost_usd=context.total_cost_usd,
                duration_s=sum(context.stage_timings_s.values()),
                stage_timings_s=context.stage_timings_s,
            )
            context.stage_outputs["report"] = report
            _write_artifact(context.output_dir / "report.json", report)
            write_report_outputs(
                report,
                context.output_dir,
                report_format=config.output.format,
            )
            for result in reversed(results):
                if result.stage == "report":
                    result.artifact = report
                    break
            partial_path = context.output_dir / PARTIAL_SUMMARY_FILENAME
            if partial_path.exists():
                partial_path.unlink()
    finally:
        if progress is not None:
            progress.stop()

    return PipelineRunResult(
        results=results,
        failed_stage=failed_stage,
        partial_summary_path=partial_summary_path,
    )


def _execute_stage(
    stage: PipelineStage,
    config: PiranesiConfig,
    prev_result: StageResult | None,
) -> StageResult:
    started_at = time.monotonic()
    try:
        result = stage.runner(config, prev_result)
    except Exception as exc:
        return StageResult(
            stage=stage.name,
            success=False,
            artifact=None,
            elapsed_s=time.monotonic() - started_at,
            error=str(exc),
        )

    if result.elapsed_s <= 0:
        result.elapsed_s = time.monotonic() - started_at
    return result


def _record_stage_success(
    context: PipelineContext,
    results: list[StageResult],
    result: StageResult,
    *,
    artifact_path: Path,
) -> None:
    context.stage_timings_s[result.stage] = result.elapsed_s
    context.stage_outputs[result.stage] = result.artifact
    _write_artifact(artifact_path, result.artifact)
    results.append(result)


def _run_parallel_post_verify_stages(
    context: PipelineContext,
    config: PiranesiConfig,
    *,
    stage_registry: Mapping[str, PipelineStage],
    verify_result: StageResult | None,
) -> list[StageResult]:
    legal_stage = stage_registry["legal"]
    patch_stage = stage_registry["patch"]

    with ThreadPoolExecutor(max_workers=2) as pool:
        legal_future = pool.submit(_execute_stage, legal_stage, config, verify_result)
        patch_future = pool.submit(_execute_stage, patch_stage, config, verify_result)
        legal_result = legal_future.result()
        patch_result = patch_future.result()

    return [legal_result, patch_result]


def cpg_cache_key(target_dir: Path, config: PiranesiConfig) -> str:
    target_root = target_dir.resolve(strict=False)
    file_hashes = [
        _source_file_state_hash(target_root, path)
        for path in discover_scan_targets(target_root, config)
    ]
    payload = "\n".join(sorted(file_hashes))
    payload = f"{payload}\n{config_hash(config)}"
    return sha256(payload.encode("utf-8")).hexdigest()


@contextmanager
def _scan_session(
    context: PipelineContext,
    config: PiranesiConfig,
    *,
    frameworks: Sequence[str] = (),
    changed_files: set[Path] | None = None,
) -> Any:
    scan_language = _scan_language_for_project(context.target_dir, frameworks=frameworks)
    cache_key: str | None = None
    cache_entry_dir: Path | None = None
    if context.use_cache and changed_files is None and scan_language == "javascript":
        cache_key = cpg_cache_key(context.target_dir, config)
        cache_entry_dir = _cache_entry_dir(context.output_dir, cache_key)

    with JoernServer(config=config.joern) as server:
        joern_version = server.version()
        cached_session = (
            _load_cached_scan_session(server, cache_entry_dir, joern_version)
            if cache_entry_dir is not None
            else None
        )
        if cached_session is not None:
            yield server, cached_session
            return

        if scan_language == "javascript":
            transpiled = transpile_project(context.target_dir, changed_files=changed_files)
            try:
                project_name = _cache_project_name(
                    cache_key if cache_key is not None else sha256(os.urandom(16)).hexdigest()
                )
                server.import_project(transpiled.out_dir, project_name=project_name)
                cache_status = "MISS" if context.use_cache else "BYPASS"
                if cache_entry_dir is not None:
                    cache_status = _write_scan_cache_entry(
                        server=server,
                        cache_entry_dir=cache_entry_dir,
                        project_name=project_name,
                        joern_version=joern_version,
                        transpiled_out_dir=transpiled.out_dir,
                    )
                yield (
                    server,
                    _ScanSession(
                        joern_project_root=transpiled.out_dir,
                        source_map=transpiled.source_map,
                        cache_status=cache_status,
                        failed_files=transpiled.failed_files,
                    ),
                )
            finally:
                transpiled.cleanup()
            return

        if scan_language != "python":
            server.import_project(
                context.target_dir,
                language=scan_language,
                frontend_args=_joern_frontend_args_for_language(scan_language),
            )
            yield (
                server,
                _ScanSession(
                    joern_project_root=context.target_dir,
                    source_map=None,
                    cache_status="BYPASS",
                    failed_files=(),
                ),
            )
            return

        direct_workspace = _prepare_direct_scan_workspace(
            context.target_dir,
            discover_scan_targets(context.target_dir, config),
        )
        try:
            server.import_project(
                direct_workspace.root_dir,
                language=scan_language,
                frontend_args=_joern_frontend_args_for_language(scan_language),
            )
            yield (
                server,
                _ScanSession(
                    joern_project_root=context.target_dir,
                    source_map=None,
                    cache_status="BYPASS",
                    failed_files=(),
                ),
            )
        finally:
            direct_workspace.cleanup()


def _source_file_state_hash(target_root: Path, path: Path) -> str:
    relative = path.resolve(strict=False).relative_to(target_root).as_posix()
    content_hash = sha256(path.read_bytes()).hexdigest()
    return sha256(f"{relative}:{content_hash}".encode()).hexdigest()


def _cache_entry_dir(output_dir: Path, cache_key: str) -> Path:
    return output_dir / _CPG_CACHE_DIRNAME / cache_key


def _cache_metadata_path(cache_entry_dir: Path) -> Path:
    return cache_entry_dir / _CPG_CACHE_METADATA_FILENAME


def _cache_project_name(cache_key: str) -> str:
    return f"piranesi-{cache_key[:16]}"


def _load_cached_scan_session(
    server: JoernServer,
    cache_entry_dir: Path,
    joern_version: str,
) -> _ScanSession | None:
    metadata = _load_scan_cache_metadata(cache_entry_dir)
    if metadata is None:
        return None

    if metadata.get("joern_version") != joern_version:
        return None

    cpg_file = metadata.get("cpg_file")
    if not isinstance(cpg_file, str) or not cpg_file:
        return None

    cpg_path = cache_entry_dir / _CPG_CACHE_CPG_DIRNAME / cpg_file
    transpiled_dir = cache_entry_dir / _CPG_CACHE_TRANSPILED_DIRNAME
    if not cpg_path.exists() or not transpiled_dir.exists():
        return None

    try:
        source_map = SourceMap.from_directory(transpiled_dir)
        server.import_cpg(cpg_path)
    except Exception:
        return None

    return _ScanSession(
        joern_project_root=transpiled_dir,
        source_map=source_map,
        cache_status="HIT",
    )


def _write_scan_cache_entry(
    *,
    server: JoernServer,
    cache_entry_dir: Path,
    project_name: str,
    joern_version: str,
    transpiled_out_dir: Path,
) -> str:
    try:
        shutil.rmtree(cache_entry_dir, ignore_errors=True)
        cache_entry_dir.mkdir(parents=True, exist_ok=True)
        cpg_path = server.export_cpg(
            cache_entry_dir / _CPG_CACHE_CPG_DIRNAME,
            project_name=project_name,
        )
        shutil.copytree(
            transpiled_out_dir,
            cache_entry_dir / _CPG_CACHE_TRANSPILED_DIRNAME,
            dirs_exist_ok=True,
        )
        _cache_metadata_path(cache_entry_dir).write_text(
            json.dumps(
                {
                    "joern_version": joern_version,
                    "cpg_file": cpg_path.name,
                },
                indent=2,
            ),
            encoding="utf-8",
        )
    except Exception:
        shutil.rmtree(cache_entry_dir, ignore_errors=True)
        return "BYPASS"
    return "MISS"


def _load_scan_cache_metadata(cache_entry_dir: Path) -> dict[str, Any] | None:
    metadata_path = _cache_metadata_path(cache_entry_dir)
    if not metadata_path.exists():
        return None
    try:
        payload = json.loads(metadata_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None
    return payload if isinstance(payload, dict) else None


def _finalize_incremental_manifest(context: PipelineContext, stage_name: str) -> None:
    if context.incremental is None or context.incremental.manifest_write_stage != stage_name:
        return
    write_manifest(context.target_dir, context.output_dir)


def _incremental_changed_files(incremental: IncrementalState | None) -> set[Path] | None:
    if incremental is None or incremental.previous_manifest is None:
        return None
    return set(incremental.diff.changed_files) or None


def _carry_forward_findings(
    previous_detect: DetectArtifact | None,
    incremental: IncrementalState,
    target_dir: Path,
) -> list[CandidateFinding]:
    if previous_detect is None:
        return []

    carried: list[CandidateFinding] = []
    unchanged_files = incremental.diff.unchanged
    for finding in previous_detect.findings:
        referenced_files = _candidate_finding_files(finding, target_dir)
        if referenced_files is None:
            continue
        if referenced_files and referenced_files <= unchanged_files:
            carried.append(finding)
    return carried


def _candidate_finding_files(
    finding: CandidateFinding,
    target_dir: Path,
) -> set[Path] | None:
    locations = [
        finding.source.location,
        finding.sink.location,
        *(step.location for step in finding.taint_path),
        *(condition.location for condition in finding.path_conditions),
    ]
    referenced_files: set[Path] = set()
    for location in locations:
        normalized = _normalize_target_relative_path(location.file, target_dir)
        if normalized is None:
            return None
        referenced_files.add(normalized)
    return referenced_files


def _merge_candidate_findings(
    carried_findings: Sequence[CandidateFinding],
    current_findings: Sequence[CandidateFinding],
) -> list[CandidateFinding]:
    merged: list[CandidateFinding] = []
    seen_ids: set[str] = set()
    for finding in [*carried_findings, *current_findings]:
        if finding.id in seen_ids:
            continue
        merged.append(finding)
        seen_ids.add(finding.id)
    return merged


def _normalize_target_relative_path(path_str: str, target_dir: Path) -> Path | None:
    candidate = Path(path_str)
    if not candidate.is_absolute():
        candidate = target_dir / candidate
    resolved = candidate.resolve(strict=False)
    try:
        return resolved.relative_to(target_dir)
    except ValueError:
        return None


def _apply_project_suppressions(
    project_root: Path,
    findings: Sequence[CandidateFinding],
) -> list[CandidateFinding]:
    rules = load_ignore_file(project_root)
    inline = _load_inline_suppressions(project_root, findings)
    return apply_suppressions(findings, rules, inline)


def _load_inline_suppressions(
    project_root: Path,
    findings: Sequence[CandidateFinding],
) -> list[InlineSuppression]:
    suppressions: list[InlineSuppression] = []
    for source_file in _finding_source_files(project_root, findings):
        suppressions.extend(parse_inline_suppressions(source_file))
    return suppressions


def _finding_source_files(project_root: Path, findings: Sequence[CandidateFinding]) -> list[Path]:
    files: set[Path] = set()
    for finding in findings:
        for location in _candidate_locations(finding):
            candidate = Path(location.file)
            if not candidate.is_absolute():
                candidate = project_root / candidate
            resolved = candidate.resolve(strict=False)
            if resolved.exists():
                files.add(resolved)
    return sorted(files)


def _candidate_locations(finding: CandidateFinding) -> tuple[SourceLocation, ...]:
    return (
        finding.source.location,
        finding.sink.location,
        *(step.location for step in finding.taint_path),
        *(condition.location for condition in finding.path_conditions),
    )


def _run_scan_stage(
    context: PipelineContext,
    config: PiranesiConfig,
    prev_result: StageResult | None,
) -> StageResult:
    _ = prev_result
    started_at = time.monotonic()
    frameworks = resolve_frameworks(context.target_dir, config.scan.frameworks)
    source_specs = get_source_specs(config.scan, frameworks=frameworks)
    sink_specs = get_sink_specs(config.scan, frameworks=frameworks)
    sanitizer_specs = get_sanitizer_specs(frameworks=frameworks)
    incremental = context.incremental
    previous_scan = (
        _load_artifact(context.output_dir / "scan.json", ScanResult)
        if incremental is not None
        else None
    )
    if (
        incremental is not None
        and incremental.previous_manifest is not None
        and not incremental.diff.has_changes
        and previous_scan is not None
    ):
        elapsed_s = time.monotonic() - started_at
        artifact = previous_scan.model_copy(
            update={
                "metadata": previous_scan.metadata.model_copy(
                    update={
                        "timestamp": _utc_now(),
                        "duration_ms": int(elapsed_s * 1000),
                        "config_hash": config_hash(config),
                        "files_parsed": len(previous_scan.files_scanned),
                    }
                )
            }
        )
        _finalize_incremental_manifest(context, "scan")
        return StageResult(
            stage="scan",
            success=True,
            artifact=artifact,
            elapsed_s=elapsed_s,
            cache_status="BYPASS",
        )

    changed_files = _incremental_changed_files(incremental)
    with _scan_session(
        context,
        config,
        frameworks=frameworks,
        changed_files=changed_files,
    ) as (server, scan_session):
        dependency_scan = scan_dependency_findings(
            context.target_dir,
            output_dir=context.output_dir,
            sbom_format=config.scan.sbom_format,
        )
        metadata = ScanMetadata(
            timestamp=_utc_now(),
            duration_ms=0,
            tree_sitter_version="unknown",
            piranesi_version=__version__,
            files_parsed=len(discover_scan_targets(context.target_dir, config)),
            parse_errors=len(scan_session.failed_files),
            config_hash=config_hash(config),
        )
        artifact = build_scan_result(
            server,
            project_root=context.target_dir,
            metadata=metadata,
            joern_project_root=scan_session.joern_project_root,
            source_map=scan_session.source_map,
            source_specs=source_specs,
            sink_specs=sink_specs,
            sanitizer_specs=sanitizer_specs,
        )
        artifact = artifact.model_copy(
            update={
                "dependency_findings": list(dependency_scan.findings),
                "sbom_artifacts": dict(dependency_scan.sbom_artifacts),
            }
        )

    elapsed_s = time.monotonic() - started_at
    artifact = artifact.model_copy(
        update={
            "metadata": artifact.metadata.model_copy(
                update={
                    "duration_ms": int(elapsed_s * 1000),
                    "files_parsed": len(artifact.files_scanned),
                }
            )
        }
    )
    _finalize_incremental_manifest(context, "scan")
    return StageResult(
        stage="scan",
        success=True,
        artifact=artifact,
        elapsed_s=elapsed_s,
        cache_status=scan_session.cache_status,
    )


def _run_detect_stage(
    context: PipelineContext,
    config: PiranesiConfig,
    prev_result: StageResult | None,
) -> StageResult:
    _ = prev_result
    started_at = time.monotonic()
    frameworks = resolve_frameworks(context.target_dir, config.scan.frameworks)
    source_specs = get_source_specs(config.scan, frameworks=frameworks)
    sink_specs = get_sink_specs(config.scan, frameworks=frameworks)
    sanitizer_specs = get_sanitizer_specs(frameworks=frameworks)
    incremental = context.incremental
    previous_detect = (
        _load_artifact(context.output_dir / "detect.json", DetectArtifact)
        if incremental is not None
        else None
    )
    carried_findings = (
        _carry_forward_findings(previous_detect, incremental, context.target_dir)
        if incremental is not None
        else []
    )
    if (
        incremental is not None
        and incremental.previous_manifest is not None
        and not incremental.diff.changed_files
        and previous_detect is not None
    ):
        _finalize_incremental_manifest(context, "detect")
        return StageResult(
            stage="detect",
            success=True,
            artifact=DetectArtifact(
                findings=_apply_project_suppressions(context.target_dir, carried_findings)
            ),
            elapsed_s=time.monotonic() - started_at,
        )

    changed_files = _incremental_changed_files(incremental)
    with _scan_session(
        context,
        config,
        frameworks=frameworks,
        changed_files=changed_files,
    ) as (server, scan_session):
        findings = list(
            extract_candidate_findings(
                server,
                joern_project_root=scan_session.joern_project_root,
                source_map=scan_session.source_map,
                source_specs=source_specs,
                sink_specs=sink_specs,
                sanitizer_specs=sanitizer_specs,
                category_provider=context.provider if _llm_is_configured() else None,
                category_model=context.router.resolve("detector") if _llm_is_configured() else None,
            )
        )

    findings.extend(
        extract_secret_findings(
            context.target_dir,
            include_tests=config.scan.include_tests,
            max_file_size=config.scan.max_file_size,
        )
    )
    findings.extend(
        extract_misconfiguration_findings(
            context.target_dir,
            frameworks=frameworks,
            files=discover_scan_targets(context.target_dir, config),
        )
    )
    findings.extend(_current_dependency_findings(context))

    _finalize_incremental_manifest(context, "detect")
    merged_findings = _merge_candidate_findings(carried_findings, findings)
    suppressed_findings = _apply_project_suppressions(context.target_dir, merged_findings)
    return StageResult(
        stage="detect",
        success=True,
        artifact=DetectArtifact(findings=suppressed_findings),
        elapsed_s=time.monotonic() - started_at,
    )


def _scan_language_for_project(project_root: Path, *, frameworks: Sequence[str]) -> str:
    has_javascript = _project_has_source_suffix(project_root, (".ts", ".tsx", ".js", ".jsx"))
    has_python = _project_has_source_suffix(project_root, (".py",))
    has_go = _project_has_source_suffix(project_root, (".go",))
    has_java = _project_has_source_suffix(project_root, (".java",))

    if has_javascript:
        return "javascript"

    normalized_frameworks = {framework.lower() for framework in frameworks}
    if "springboot" in normalized_frameworks or has_java:
        return "java"
    if normalized_frameworks & {"flask", "django", "fastapi"} or has_python:
        return "python"
    if normalized_frameworks & {"gin", "echo", "chi", "go-stdlib"} or has_go:
        return "go"
    return "javascript"


def _project_has_source_suffix(project_root: Path, suffixes: tuple[str, ...]) -> bool:
    for path in project_root.rglob("*"):
        if not path.is_file() or path.suffix not in suffixes:
            continue
        relative = path.relative_to(project_root).as_posix()
        if _matches_patterns(relative, _SOURCE_DISCOVERY_EXCLUDE_PATTERNS):
            continue
        return True
    return False


def _effective_scan_globs(target_root: Path, config: PiranesiConfig) -> tuple[list[str], list[str]]:
    include_patterns = _normalize_globs(config.scan.include_patterns)
    exclude_patterns = _normalize_globs(config.scan.exclude_patterns)
    frameworks = resolve_frameworks(target_root, config.scan.frameworks)
    scan_language = _scan_language_for_project(target_root, frameworks=frameworks)

    if scan_language == "python":
        default_includes = _normalize_globs(_DEFAULT_SCAN_INCLUDE_PATTERNS)
        if include_patterns == default_includes:
            include_patterns = _normalize_globs(_PYTHON_INCLUDE_PATTERNS)
        for pattern in _PYTHON_EXCLUDE_PATTERNS:
            if pattern not in exclude_patterns:
                exclude_patterns.append(pattern)
    elif scan_language == "go":
        default_includes = _normalize_globs(_DEFAULT_SCAN_INCLUDE_PATTERNS)
        if include_patterns == default_includes:
            include_patterns = _normalize_globs(_GO_INCLUDE_PATTERNS)
        for pattern in _GO_EXCLUDE_PATTERNS:
            if pattern not in exclude_patterns:
                exclude_patterns.append(pattern)

    return include_patterns, exclude_patterns


def _prepare_direct_scan_workspace(
    target_dir: Path,
    files: Sequence[Path],
) -> _DirectScanWorkspace:
    normalized_target = target_dir.resolve(strict=False)
    workspace_root = Path(tempfile.mkdtemp(prefix="piranesi-direct-scan-")).resolve(strict=False)
    for path in files:
        resolved_path = path.resolve(strict=False)
        relative = resolved_path.relative_to(normalized_target)
        destination = workspace_root / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.symlink_to(resolved_path)
    return _DirectScanWorkspace(root_dir=workspace_root)


def _joern_frontend_args_for_language(language: str) -> tuple[str, ...]:
    if language == "go":
        return ("--exclude", "vendor")
    if language == "java":
        return ("--exclude", "src/test")
    return ()


def _run_triage_stage(
    context: PipelineContext,
    config: PiranesiConfig,
    prev_result: StageResult | None,
) -> StageResult:
    _ = config
    detect_artifact = _extract_stage_artifact(prev_result, DetectArtifact, "detect")
    started_at = time.monotonic()
    active_findings = [finding for finding in detect_artifact.findings if not finding.suppressed]
    if not _llm_is_configured():
        findings = [
            TriagedFinding(
                finding=finding,
                triage_verdict="true_positive",
                skeptic_analysis="LLM triage skipped because no API key is configured.",
                ensemble_score=finding.confidence,
                escalated=False,
            )
            for finding in active_findings
        ]
        return StageResult(
            stage="triage",
            success=True,
            artifact=TriageArtifact(findings=findings),
            elapsed_s=time.monotonic() - started_at,
        )

    voter = CalibratedEnsembleVoter(provider=context.provider, router=context.router)
    skeptic = SkepticAgent(provider=context.provider, router=context.router)
    findings = [voter.triage_finding(finding, skeptic=skeptic) for finding in active_findings]
    return StageResult(
        stage="triage",
        success=True,
        artifact=TriageArtifact(findings=findings),
        elapsed_s=time.monotonic() - started_at,
    )


def _run_verify_stage(
    context: PipelineContext,
    config: PiranesiConfig,
    prev_result: StageResult | None,
) -> StageResult:
    _ = config
    triage_artifact = _extract_stage_artifact(prev_result, TriageArtifact, "triage")
    started_at = time.monotonic()
    confirmed_findings: list[ConfirmedFinding] = []

    for triaged in triage_artifact.findings:
        if triaged.triage_verdict == "false_positive":
            continue
        template = extract_exploit_template(triaged.finding)
        solve_result = solve_exploit_template(template)
        if solve_result.status != "SAT" or not solve_result.solutions:
            continue
        payload = solve_result.solutions[0].payload
        if context.no_execute:
            continue

        baseline_payload = build_baseline_payload(payload, vuln_class=triaged.finding.vuln_class)
        captures = run_in_sandbox(str(context.target_dir), [baseline_payload, payload])
        if len(captures) < 2:
            continue
        baseline_capture, exploit_capture = captures[0], captures[1]
        confirmation = confirm_responses(
            triaged.finding.vuln_class,
            payload,
            baseline_capture.http_response,
            exploit_capture.http_response,
            container_logs=exploit_capture.container_logs,
        )
        if confirmation.level != "CONFIRMED":
            continue

        confirmed_findings.append(
            ConfirmedFinding(
                finding=triaged,
                exploit_payload=_first_payload_value(payload),
                exploit_constraints=[
                    str(item)
                    for item in solve_result.solutions[0].model_values.values()
                    if isinstance(item, str)
                ],
                sandbox_result=_sandbox_result_from_capture(exploit_capture, confirmed=True),
                reproducer_script=generate_reproducer_script(
                    triaged.finding,
                    target_path=context.target_dir,
                    payload=payload,
                ),
                related_cves=[],
            )
        )

    return StageResult(
        stage="verify",
        success=True,
        artifact=VerifyArtifact(findings=confirmed_findings),
        elapsed_s=time.monotonic() - started_at,
    )


def _run_legal_stage(
    context: PipelineContext,
    config: PiranesiConfig,
    prev_result: StageResult | None,
) -> StageResult:
    _ = (context, config)
    verify_artifact = _extract_stage_artifact(prev_result, VerifyArtifact, "verify")
    started_at = time.monotonic()
    assessments = [
        assess_finding(finding, build_default_engine()) for finding in verify_artifact.findings
    ]
    return StageResult(
        stage="legal",
        success=True,
        artifact=LegalArtifact(assessments=assessments),
        elapsed_s=time.monotonic() - started_at,
    )


def _run_patch_stage(
    context: PipelineContext,
    config: PiranesiConfig,
    prev_result: StageResult | None,
) -> StageResult:
    _ = config
    verify_artifact = _require_artifact(context.stage_outputs["verify"], VerifyArtifact, "verify")
    _ = prev_result
    started_at = time.monotonic()
    if not _llm_is_configured():
        return StageResult(
            stage="patch",
            success=True,
            artifact=PatchArtifact(patches=[]),
            elapsed_s=time.monotonic() - started_at,
        )
    patches = generate_patches(
        findings=verify_artifact.findings,
        provider=context.provider,
        target_dir=context.target_dir,
    )
    return StageResult(
        stage="patch",
        success=True,
        artifact=PatchArtifact(patches=patches),
        elapsed_s=time.monotonic() - started_at,
    )


def _run_report_stage(
    context: PipelineContext,
    config: PiranesiConfig,
    prev_result: StageResult | None,
) -> StageResult:
    _ = (config, prev_result)
    started_at = time.monotonic()
    scan_artifact = _require_artifact(context.stage_outputs["scan"], ScanResult, "scan")
    detect_artifact = _require_artifact(context.stage_outputs["detect"], DetectArtifact, "detect")
    verify_artifact = _require_artifact(context.stage_outputs["verify"], VerifyArtifact, "verify")
    legal_artifact = _require_artifact(context.stage_outputs["legal"], LegalArtifact, "legal")
    patch_artifact = _require_artifact(context.stage_outputs["patch"], PatchArtifact, "patch")

    report = build_report(
        scan_result=scan_artifact,
        detected_findings=detect_artifact.findings,
        confirmed_findings=verify_artifact.findings,
        legal_assessments=legal_artifact.assessments,
        patch_results=patch_artifact.patches,
        target_dir=context.target_dir,
        total_llm_cost_usd=context.total_cost_usd,
        duration_s=sum(context.stage_timings_s.values()),
        stage_timings_s=context.stage_timings_s,
    )
    write_report_outputs(
        report,
        context.output_dir,
        report_format=config.output.format,
    )
    return StageResult(
        stage="report",
        success=True,
        artifact=report,
        elapsed_s=time.monotonic() - started_at,
    )


def _current_dependency_findings(context: PipelineContext) -> list[CandidateFinding]:
    scan_artifact = context.stage_outputs.get("scan")
    if isinstance(scan_artifact, ScanResult):
        return list(scan_artifact.dependency_findings)
    return list(scan_dependency_findings(context.target_dir).findings)


def _load_artifact(path: Path, artifact_type: type[BaseModel]) -> BaseModel | None:
    try:
        return artifact_type.model_validate_json(path.read_text(encoding="utf-8"))
    except (OSError, ValidationError, json.JSONDecodeError):
        return None


def _write_artifact(path: Path, artifact: BaseModel) -> None:
    path.write_text(artifact.model_dump_json(indent=2), encoding="utf-8")


def _save_partial_summary(
    context: PipelineContext,
    results: Sequence[StageResult],
    failed_result: StageResult,
) -> Path:
    completed = [result.stage for result in results if result.success]
    summary = PartialRunSummary(
        completed=completed,
        failed=failed_result.stage,
        error=failed_result.error or "unknown pipeline error",
        stage_timings_s=dict(context.stage_timings_s),
        total_llm_cost_usd=context.total_cost_usd,
    )
    path = context.output_dir / PARTIAL_SUMMARY_FILENAME
    path.write_text(summary.model_dump_json(indent=2), encoding="utf-8")
    return path


def _extract_stage_artifact[T: BaseModel](
    prev_result: StageResult | None,
    artifact_type: type[T],
    stage_name: str,
) -> T:
    if prev_result is None:
        raise ValueError(f"{stage_name} stage requires a prior stage artifact")
    return _require_artifact(prev_result.artifact, artifact_type, stage_name)


def _require_artifact[T: BaseModel](artifact: Any, artifact_type: type[T], stage_name: str) -> T:
    if isinstance(artifact, artifact_type):
        return artifact
    raise TypeError(
        f"{stage_name} stage expected {artifact_type.__name__}, got {type(artifact).__name__}"
    )


def _normalize_globs(patterns: Sequence[str]) -> list[str]:
    normalized: list[str] = []
    for pattern in patterns:
        for item in pattern.split(","):
            candidate = item.strip()
            if candidate:
                normalized.append(candidate)
    return normalized


def _matches_patterns(relative_path: str, patterns: Sequence[str]) -> bool:
    return any(
        fnmatch(relative_path, pattern)
        or (pattern.startswith("**/") and fnmatch(relative_path, pattern[3:]))
        for pattern in patterns
    )


def _utc_now() -> str:
    return datetime.now(UTC).isoformat(timespec="seconds").replace("+00:00", "Z")


def _sandbox_result_from_capture(capture: Any, *, confirmed: bool) -> SandboxResult:
    return SandboxResult(
        container_id=capture.container_id or "",
        request=dict(capture.http_response.request),
        response={
            "status": capture.http_response.status_code,
            "headers": dict(capture.http_response.headers),
            "body": capture.http_response.body,
            "error": capture.error,
        },
        timing_ms=int(capture.timing_ms),
        side_effects=list(capture.side_effects),
        container_diff=list(capture.filesystem_diff),
        stdout=capture.stdout or capture.container_logs,
        stderr=capture.stderr,
        exit_code=int(capture.exit_code or 0),
        network_isolated=bool(capture.network_isolated),
        confirmed=confirmed,
    )


def _first_payload_value(payload: Any) -> str:
    values = getattr(payload, "payload_values", {})
    if isinstance(values, Mapping):
        for value in values.values():
            if isinstance(value, str):
                return value
    return ""


def _llm_is_configured() -> bool:
    return any(os.getenv(name) for name in _LLM_API_ENV_VARS)


__all__ = [
    "PARTIAL_SUMMARY_FILENAME",
    "STAGE_ORDER",
    "DetectArtifact",
    "LegalArtifact",
    "PartialRunSummary",
    "PatchArtifact",
    "PipelineContext",
    "PipelineRunResult",
    "PipelineStage",
    "StageResult",
    "TriageArtifact",
    "VerifyArtifact",
    "build_default_stage_registry",
    "cpg_cache_key",
    "discover_scan_targets",
    "load_partial_summary",
    "run_pipeline",
]
