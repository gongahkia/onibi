from __future__ import annotations

import json
import logging
import os
import re
import shutil
import tempfile
import time
from collections import OrderedDict, defaultdict
from collections.abc import Callable, Mapping, Sequence
from concurrent.futures import ThreadPoolExecutor
from contextlib import contextmanager
from dataclasses import dataclass, field
from datetime import UTC, datetime
from fnmatch import fnmatch
from hashlib import sha256
from pathlib import Path
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from piranesi import __version__
from piranesi.advisory import advisory_db_path, get_advisory_db_status, parse_lockfiles
from piranesi.config import PiranesiConfig, config_hash
from piranesi.detect import (
    InlineSuppression,
    SuppressionLifecycleSummary,
    analyze_reachability,
    apply_suppressions_with_lifecycle,
    extract_auth_access_findings,
    extract_candidate_findings,
    extract_crypto_transport_findings,
    extract_misconfiguration_findings,
    extract_redos_findings,
    extract_secret_findings,
    load_ignore_file_with_diagnostics,
    parse_inline_suppressions,
    scan_dependency_findings,
)
from piranesi.detect.sanitizer_discovery import discover_custom_sanitizers
from piranesi.diff import stable_fingerprint
from piranesi.legal import assess_finding, build_default_engine
from piranesi.llm.cost import CostTracker
from piranesi.llm.provider import LLMProvider
from piranesi.llm.router import ModelRouter
from piranesi.models import (
    CandidateFinding,
    ConfirmedFinding,
    LegalAssessment,
    PackageScanResult,
    PatchResult,
    ReachabilityResult,
    ScanMetadata,
    ScannedFunction,
    ScanResult,
    SourceLocation,
    TaintSink,
    TaintSource,
    TaintStep,
    TriagedFinding,
)
from piranesi.models.finding import SandboxResult, VerificationAttempt
from piranesi.patch.generator import generate_patches
from piranesi.plugin import discover_rule_plugins
from piranesi.report.renderer import (
    PiranesiReport,
    build_report,
    update_report_metrics,
    write_report_outputs,
)
from piranesi.rules import (
    compile_rule,
    execute_custom_rules,
    filter_builtin_specs_for_custom_rules,
    load_rules,
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
from piranesi.scan.monorepo import (
    MonorepoManifest,
    WorkspacePackage,
    detect_monorepo,
    select_packages,
)
from piranesi.scan.specs import (
    SanitizerSpec,
    get_sanitizer_specs,
    get_sink_specs,
    get_source_specs,
)
from piranesi.scan.surface import build_scan_result
from piranesi.scan.transpile import SourceMap, transpile_project
from piranesi.trace import TraceWriter
from piranesi.triage import CalibratedEnsembleVoter, SkepticAgent
from piranesi.triage.ml_classifier import load_model, predict
from piranesi.ui import file_progress, stage_header
from piranesi.verify import (
    TargetLaunchProfile,
    build_baseline_payload,
    confirm_responses,
    extract_exploit_template,
    generate_reproducer_script,
    run_in_sandbox,
    solve_exploit_template,
)
from piranesi.verify.evidence import (
    build_verification_evidence,
    write_verification_evidence_artifact,
)
from piranesi.verify.preconditions import evaluate_verification_preconditions

STAGE_ORDER = ("scan", "detect", "triage", "verify", "legal", "patch", "report")
PARTIAL_SUMMARY_FILENAME = "_partial.json"
_CPG_CACHE_DIRNAME = "_cpg_cache"
_CPG_CACHE_METADATA_FILENAME = "metadata.json"
_CPG_CACHE_CPG_DIRNAME = "cpg"
_CPG_CACHE_TRANSPILED_DIRNAME = "transpiled"
_DEFAULT_SCAN_INCLUDE_PATTERNS = tuple(PiranesiConfig().scan.include_patterns)
_GO_INCLUDE_PATTERNS = ("**/*.go",)
_PYTHON_INCLUDE_PATTERNS = ("**/*.py",)
_PIRANESI_OUTPUT_EXCLUDE_PATTERNS = (
    # piranesi output dirs
    "**/piranesi-output/**",
    "**/.piranesi-cache/**",
    "**/.piranesi-out/**",
    "**/.piranesi-trace*",
)
_PYTHON_EXCLUDE_PATTERNS = (
    "**/__pycache__/**",
    "**/.venv/**",
    "**/venv/**",
    "**/site-packages/**",
    *_PIRANESI_OUTPUT_EXCLUDE_PATTERNS,
)
_GO_EXCLUDE_PATTERNS = ("**/vendor/**", *_PIRANESI_OUTPUT_EXCLUDE_PATTERNS)
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
    *_PIRANESI_OUTPUT_EXCLUDE_PATTERNS,
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

_logger = logging.getLogger("piranesi.pipeline")
VerificationStatus = Literal["confirmed", "skipped", "inconclusive", "error"]


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
    router: ModelRouter | None
    cost_tracker: CostTracker
    trace_writer: TraceWriter
    stage_outputs: dict[str, BaseModel] = field(default_factory=dict)
    stage_timings_s: dict[str, float] = field(default_factory=dict)
    resumed_cost_usd: float = 0.0
    apply_patches: bool = False
    no_execute: bool = False
    use_cache: bool = True
    incremental: IncrementalState | None = None
    monorepo_manifest: MonorepoManifest | None = None
    monorepo_package_name: str | None = None
    changed_packages_only: bool = False
    max_parallel: int | None = None
    selected_files: set[Path] | None = None
    render_ui: bool = False
    ui_progress: Any = None
    started_at: float = field(default_factory=time.monotonic)

    @property
    def total_cost_usd(self) -> float:
        return self.resumed_cost_usd + self.cost_tracker.total_usd


class DetectArtifact(BaseModel):
    model_config = ConfigDict(extra="forbid")

    findings: list[CandidateFinding] = Field(default_factory=list)
    reachability: ReachabilityResult | None = None
    suppression_lifecycle: SuppressionLifecycleSummary | None = None


class TriageArtifact(BaseModel):
    model_config = ConfigDict(extra="forbid")

    findings: list[TriagedFinding] = Field(default_factory=list)


class VerifyArtifact(BaseModel):
    model_config = ConfigDict(extra="forbid")

    findings: list[ConfirmedFinding] = Field(default_factory=list)
    attempts: list[VerificationAttempt] = Field(default_factory=list)


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


def discover_scan_targets(
    target_dir: Path,
    config: PiranesiConfig,
    *,
    candidate_paths: Sequence[Path] | None = None,
) -> list[Path]:
    target_root = target_dir.resolve(strict=False)
    include_patterns, exclude_patterns = _effective_scan_globs(target_root, config)
    files: list[Path] = []

    if candidate_paths is None:
        candidates = sorted(target_root.rglob("*"))
    else:
        seen: set[Path] = set()
        candidates = []
        for candidate_path in candidate_paths:
            candidate = (
                candidate_path.resolve(strict=False)
                if candidate_path.is_absolute()
                else (target_root / candidate_path).resolve(strict=False)
            )
            if candidate in seen:
                continue
            seen.add(candidate)
            candidates.append(candidate)
        candidates.sort()

    for path in candidates:
        if not path.is_file():
            continue
        try:
            relative = path.relative_to(target_root).as_posix()
        except ValueError:
            continue
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
    context.render_ui = render_ui
    if not _llm_is_configured():
        _logger.warning(
            "no LLM API key configured; running deterministic pipeline mode. "
            "Triage will pass reachable findings through and patch generation will be skipped. "
            "Set one of %s to enable LLM-assisted stages.",
            ", ".join(_LLM_API_ENV_VARS),
        )
    else:
        _logger.info("LLM provider configured — pipeline starting")
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
        context.ui_progress = progress

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
                failed_parallel: list[StageResult] = []
                for parallel_result in parallel_results:
                    if not parallel_result.success:
                        failed_parallel.append(parallel_result)
                        _logger.error(
                            "parallel stage '%s' failed: %s",
                            parallel_result.stage,
                            parallel_result.error,
                        )
                    else:
                        _record_stage_success(
                            context,
                            results,
                            parallel_result,
                            artifact_path=context.output_dir / f"{parallel_result.stage}.json",
                        )
                        prev_result = parallel_result
                        if progress is not None and task_id is not None:
                            progress.advance(task_id)
                if failed_parallel:
                    for fail_result in failed_parallel:
                        results.append(fail_result)
                    failed_stage = failed_parallel[0].stage
                    combined_error = "; ".join(f"{r.stage}: {r.error}" for r in failed_parallel)
                    partial_summary_path = _save_partial_summary(
                        context,
                        results,
                        StageResult(
                            stage=failed_stage,
                            success=False,
                            artifact=None,
                            elapsed_s=0.0,
                            error=combined_error,
                        ),
                    )
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
        context.ui_progress = None
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
    _logger.info("stage '%s' starting", stage.name)
    started_at = time.monotonic()
    try:
        result = stage.runner(config, prev_result)
    except Exception as exc:
        elapsed = time.monotonic() - started_at
        _logger.error("stage '%s' failed after %.1fs: %s", stage.name, elapsed, exc)
        return StageResult(
            stage=stage.name,
            success=False,
            artifact=None,
            elapsed_s=elapsed,
            error=str(exc),
        )

    if result.elapsed_s <= 0:
        result.elapsed_s = time.monotonic() - started_at
    _logger.info(
        "stage '%s' completed in %.1fs (success=%s)", stage.name, result.elapsed_s, result.success
    )
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
    _logger.info("stage '%s' artifact written to %s", result.stage, artifact_path)


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
    with _scan_session_for_target(
        context.target_dir,
        context.output_dir,
        config=config,
        use_cache=context.use_cache,
        frameworks=frameworks,
        changed_files=changed_files,
    ) as session:
        yield session


@contextmanager
def _scan_session_for_target(
    target_dir: Path,
    output_dir: Path,
    *,
    config: PiranesiConfig,
    use_cache: bool,
    frameworks: Sequence[str] = (),
    changed_files: set[Path] | None = None,
) -> Any:
    resolved_target = target_dir.resolve(strict=False)
    resolved_output = output_dir.resolve(strict=False)
    scan_language = _scan_language_for_project(resolved_target, frameworks=frameworks)
    selected_targets = discover_scan_targets(
        resolved_target,
        config,
        candidate_paths=None if changed_files is None else tuple(changed_files),
    )
    cache_key: str | None = None
    cache_entry_dir: Path | None = None
    if use_cache and changed_files is None and scan_language == "javascript":
        cache_key = cpg_cache_key(resolved_target, config)
        cache_entry_dir = _cache_entry_dir(resolved_output, cache_key)

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
            transpiled = transpile_project(resolved_target, changed_files=changed_files)
            try:
                project_name = _cache_project_name(
                    cache_key if cache_key is not None else sha256(os.urandom(16)).hexdigest()
                )
                server.import_project(transpiled.out_dir, project_name=project_name)
                cache_status = "MISS" if use_cache else "BYPASS"
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

        if changed_files is not None:
            direct_workspace = _prepare_direct_scan_workspace(
                resolved_target,
                selected_targets,
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
                        joern_project_root=direct_workspace.root_dir,
                        source_map=None,
                        cache_status="BYPASS",
                        failed_files=(),
                    ),
                )
            finally:
                direct_workspace.cleanup()
            return

        if scan_language != "python":
            server.import_project(
                resolved_target,
                language=scan_language,
                frontend_args=_joern_frontend_args_for_language(scan_language),
            )
            yield (
                server,
                _ScanSession(
                    joern_project_root=resolved_target,
                    source_map=None,
                    cache_status="BYPASS",
                    failed_files=(),
                ),
            )
            return

        direct_workspace = _prepare_direct_scan_workspace(
            resolved_target,
            selected_targets,
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
                    joern_project_root=resolved_target,
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


def _context_changed_files(context: PipelineContext) -> set[Path] | None:
    if context.selected_files is not None:
        return set(context.selected_files) or None
    return _incremental_changed_files(context.incremental)


def _apply_incremental_threshold(
    changed_files: set[Path] | None,
    *,
    config: PiranesiConfig,
    context: PipelineContext,
) -> set[Path] | None:
    if changed_files is None:
        return None
    if context.selected_files is not None:
        return changed_files
    threshold = config.scan.incremental_threshold
    if threshold > 0 and len(changed_files) >= threshold:
        return None
    return changed_files


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
    *,
    fail_on_invalid: bool,
    fail_on_expired: bool,
    fail_on_stale: bool,
) -> tuple[list[CandidateFinding], SuppressionLifecycleSummary]:
    validation = load_ignore_file_with_diagnostics(project_root)
    rules = validation.rules
    inline = _load_inline_suppressions(project_root, findings)
    outcome = apply_suppressions_with_lifecycle(
        findings,
        rules,
        inline,
        invalid_entries=validation.invalid_entries,
        evaluate_stale=True,
    )
    lifecycle = outcome.lifecycle
    if lifecycle.invalid_rules:
        message = (
            f"invalid suppression entries in {validation.path}: "
            f"{'; '.join(lifecycle.invalid_entries)}"
        )
        if fail_on_invalid:
            raise ValueError(message)
        _logger.warning("detect: %s", message)
    if lifecycle.expired_rules:
        message = (
            f"found {lifecycle.expired_rules} expired suppression rule(s): "
            f"{', '.join(lifecycle.expired_selectors)}"
        )
        if fail_on_expired:
            raise ValueError(f"detect: {message}")
        _logger.warning("detect: %s", message)
    if lifecycle.stale_rules:
        message = (
            f"found {lifecycle.stale_rules} stale suppression rule(s): "
            f"{', '.join(lifecycle.stale_selectors)}"
        )
        if fail_on_stale:
            raise ValueError(f"detect: {message}")
        _logger.warning("detect: %s", message)
    return outcome.findings, lifecycle


def _location_key(location: SourceLocation) -> tuple[Path, int]:
    return (Path(location.file).resolve(strict=False), location.line)


def _suppress_secret_findings_with_crypto(
    secret_findings: Sequence[CandidateFinding],
    crypto_findings: Sequence[CandidateFinding],
) -> list[CandidateFinding]:
    suppressed_locations = {
        _location_key(crypto.source.location)
        for crypto in crypto_findings
        if crypto.metadata.get("suppressed_cwe_798") is True
    }
    if not suppressed_locations:
        return list(secret_findings)

    filtered: list[CandidateFinding] = []
    for finding in secret_findings:
        if finding.vuln_class != "CWE-798":
            filtered.append(finding)
            continue
        if _location_key(finding.source.location) in suppressed_locations:
            continue
        filtered.append(finding)
    return filtered


def _annotate_reachability_for_findings(
    context: PipelineContext,
    config: PiranesiConfig,
    findings: Sequence[CandidateFinding],
) -> tuple[list[CandidateFinding], ReachabilityResult]:
    scan_artifact_raw = context.stage_outputs.get("scan")
    if scan_artifact_raw is None:
        return list(findings), ReachabilityResult()
    scan_artifact = _require_artifact(scan_artifact_raw, ScanResult, "scan")
    return analyze_reachability(
        scan_artifact,
        findings,
        project_root=context.target_dir,
        include_tests=config.scan.include_tests,
    )


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


_EXPORT_FUNCTION_PATTERN = re.compile(
    r"export\s+(?:async\s+)?function\s+(?P<name>[A-Za-z_$][\w$]*)\s*\((?P<params>[^)]*)\)"
)
_NAMED_IMPORT_PATTERN = re.compile(
    r'import\s*\{\s*(?P<imports>[^}]+)\s*\}\s*from\s*[\'"](?P<package>[^\'"]+)[\'"]'
)
_DESTRUCTURED_REQUIRE_PATTERN = re.compile(
    r'const\s*\{\s*(?P<imports>[^}]+)\s*\}\s*=\s*require\([\'"](?P<package>[^\'"]+)[\'"]\)'
)
_REQUEST_SOURCE_PATTERN = re.compile(r"req\.(?:body|query|params)(?:\.[A-Za-z_$][\w$]*)?")
_SQL_SINK_PATTERN = re.compile(r"(?P<api>[A-Za-z_$][\w$]*\.query|query|execute)\s*\(")
_COMMAND_SINK_PATTERN = re.compile(r"(?P<api>exec|execSync|spawn|spawnSync)\s*\(")
_WORKSPACE_SOURCE_EXCLUDE_PARTS = frozenset(
    {
        "node_modules",
        ".venv",
        "venv",
        "__pycache__",
        "dist",
        "build",
        "target",
        "vendor",
        # piranesi output dirs
        "piranesi-output",
        ".piranesi-cache",
        ".piranesi-out",
    }
)
_PIRANESI_TRACE_PREFIX = ".piranesi-trace"


def _monorepo_selection(
    context: PipelineContext,
    config: PiranesiConfig,
) -> tuple[MonorepoManifest, list[WorkspacePackage]] | None:
    if context.monorepo_manifest is None:
        context.monorepo_manifest = detect_monorepo(
            context.target_dir,
            requested_frameworks=config.scan.frameworks,
        )

    manifest = context.monorepo_manifest
    if manifest is None or len(manifest.packages) <= 1:
        return None

    selected_packages = select_packages(
        manifest,
        package_name=context.monorepo_package_name,
        changed_only=context.changed_packages_only,
    )
    if context.monorepo_package_name is not None and not selected_packages:
        raise ValueError(f"package '{context.monorepo_package_name}' not found in monorepo")
    return manifest, selected_packages


def _empty_scan_result(
    target_dir: Path,
    config: PiranesiConfig,
    *,
    detected_tool: str | None = None,
) -> ScanResult:
    return ScanResult(
        project_root=str(target_dir.resolve(strict=False)),
        files_scanned=[],
        call_graph={},
        functions=[],
        entry_points=[],
        attack_surface=[],
        dependency_findings=[],
        sbom_artifacts={},
        package_results=[],
        monorepo_detected_tool=detected_tool,
        metadata=ScanMetadata(
            timestamp=_utc_now(),
            duration_ms=0,
            tree_sitter_version="unknown",
            piranesi_version=__version__,
            files_parsed=0,
            parse_errors=0,
            config_hash=config_hash(config),
        ),
    )


def _package_changed_files(
    context: PipelineContext,
    package: WorkspacePackage,
) -> set[Path] | None:
    root_changed_files = _context_changed_files(context)
    if root_changed_files is None:
        return None

    package_root = package.path.resolve(strict=False)
    filtered: set[Path] = set()
    for changed_file in root_changed_files:
        candidate = (
            changed_file.resolve(strict=False)
            if changed_file.is_absolute()
            else (context.target_dir / changed_file).resolve(strict=False)
        )
        try:
            candidate.relative_to(package_root)
        except ValueError:
            continue
        else:
            filtered.add(candidate)
    return filtered or None


def _build_scan_artifact_for_target(
    context: PipelineContext,
    config: PiranesiConfig,
    target_dir: Path,
    *,
    changed_files: set[Path] | None = None,
) -> tuple[ScanResult, str, tuple[str, ...]]:
    started_at = time.monotonic()
    scanned_targets = discover_scan_targets(
        target_dir,
        config,
        candidate_paths=None if changed_files is None else tuple(changed_files),
    )
    frameworks = resolve_frameworks(target_dir, config.scan.frameworks)
    source_specs = get_source_specs(config.scan, frameworks=frameworks)
    sink_specs = get_sink_specs(config.scan, frameworks=frameworks)
    sanitizer_specs = _sanitizer_specs_for_target(target_dir, frameworks=frameworks)
    scan_session_cm = (
        _scan_session(
            context,
            config,
            frameworks=frameworks,
            changed_files=changed_files,
        )
        if target_dir.resolve(strict=False) == context.target_dir.resolve(strict=False)
        else _scan_session_for_target(
            target_dir,
            context.output_dir,
            config=config,
            use_cache=context.use_cache,
            frameworks=frameworks,
            changed_files=changed_files,
        )
    )

    with scan_session_cm as (server, scan_session):
        dependency_scan = scan_dependency_findings(
            target_dir,
            output_dir=context.output_dir,
            sbom_format=config.scan.sbom_format,
            changed_files=changed_files,
        )
        advisory_status = get_advisory_db_status(advisory_db_path(target_dir))
        dependency_context_present = bool(parse_lockfiles(target_dir))
        if dependency_context_present and advisory_status.warnings:
            _logger.warning(
                "advisory database freshness warning",
                extra={
                    "event": "advisory_db_freshness_warning",
                    "path": str(advisory_status.path),
                    "freshness": advisory_status.freshness,
                    "warnings": list(advisory_status.warnings),
                },
            )
        metadata = ScanMetadata(
            timestamp=_utc_now(),
            duration_ms=0,
            tree_sitter_version="unknown",
            piranesi_version=__version__,
            files_parsed=len(scanned_targets),
            parse_errors=len(scan_session.failed_files),
            config_hash=config_hash(config),
        )
        artifact = build_scan_result(
            server,
            project_root=target_dir,
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
    return (
        artifact.model_copy(
            update={
                "metadata": artifact.metadata.model_copy(
                    update={
                        "duration_ms": int(elapsed_s * 1000),
                        "files_parsed": len(artifact.files_scanned),
                    }
                )
            }
        ),
        scan_session.cache_status,
        frameworks,
    )


def _detect_findings_for_target(
    context: PipelineContext,
    config: PiranesiConfig,
    target_dir: Path,
    *,
    changed_files: set[Path] | None = None,
) -> list[CandidateFinding]:
    llm_configured = _llm_is_configured()
    category_provider = context.provider if llm_configured else None
    category_model = _resolve_stage_model(context, "detector") if llm_configured else None

    frameworks = resolve_frameworks(target_dir, config.scan.frameworks)
    local_rules = list(load_rules(target_dir / "rules"))
    disabled_plugins = frozenset(config.plugins.disabled)
    for rule_plugin in discover_rule_plugins(disabled=disabled_plugins):
        _logger.info("loading rules from plugin '%s'", rule_plugin.name())
        for rule_path in rule_plugin.rule_files():
            local_rules.extend(load_rules(rule_path))
    compiled_custom_rules = [compile_rule(rule) for rule in local_rules]
    source_specs = get_source_specs(config.scan, frameworks=frameworks)
    sink_specs = get_sink_specs(config.scan, frameworks=frameworks)
    sanitizer_specs = _sanitizer_specs_for_target(target_dir, frameworks=frameworks)
    source_specs, sink_specs, sanitizer_specs = filter_builtin_specs_for_custom_rules(
        compiled_custom_rules,
        source_specs=source_specs,
        sink_specs=sink_specs,
        sanitizer_specs=sanitizer_specs,
    )
    scanned_files = discover_scan_targets(
        target_dir,
        config,
        candidate_paths=None if changed_files is None else tuple(changed_files),
    )
    scan_session_cm = (
        _scan_session(
            context,
            config,
            frameworks=frameworks,
            changed_files=changed_files,
        )
        if target_dir.resolve(strict=False) == context.target_dir.resolve(strict=False)
        else _scan_session_for_target(
            target_dir,
            context.output_dir,
            config=config,
            use_cache=context.use_cache,
            frameworks=frameworks,
            changed_files=changed_files,
        )
    )

    with scan_session_cm as (server, scan_session):
        findings = list(
            extract_candidate_findings(
                server,
                joern_project_root=scan_session.joern_project_root,
                source_map=scan_session.source_map,
                source_specs=source_specs,
                sink_specs=sink_specs,
                sanitizer_specs=sanitizer_specs,
                category_provider=category_provider,
                category_model=category_model,
            )
        )
        findings.extend(
            execute_custom_rules(
                server,
                compiled_rules=compiled_custom_rules,
                project_root=target_dir,
                joern_project_root=scan_session.joern_project_root,
                source_map=scan_session.source_map,
                source_specs=source_specs,
                sink_specs=sink_specs,
                sanitizer_specs=sanitizer_specs,
                files=scanned_files,
                category_provider=category_provider,
                category_model=category_model,
            )
        )

    crypto_findings = list(
        extract_crypto_transport_findings(
            target_dir,
            frameworks=frameworks,
            files=scanned_files,
            include_tests=config.scan.include_tests,
        )
    )
    findings.extend(crypto_findings)
    secret_findings = list(
        extract_secret_findings(
            target_dir,
            include_tests=config.scan.include_tests,
            max_file_size=config.scan.max_file_size,
            changed_files=changed_files,
        )
    )
    findings.extend(_suppress_secret_findings_with_crypto(secret_findings, crypto_findings))
    findings.extend(
        extract_misconfiguration_findings(
            target_dir,
            frameworks=frameworks,
            files=scanned_files,
        )
    )
    findings.extend(extract_redos_findings(target_dir, files=scanned_files))
    findings.extend(
        extract_auth_access_findings(
            target_dir,
            frameworks=frameworks,
            files=scanned_files,
        )
    )
    findings.extend(
        scan_dependency_findings(
            target_dir,
            output_dir=context.output_dir,
            sbom_format=config.scan.sbom_format,
            changed_files=changed_files,
        ).findings
    )
    return findings


def _sanitizer_specs_for_target(
    target_dir: Path,
    *,
    frameworks: Sequence[str] | None = None,
) -> tuple[SanitizerSpec, ...]:
    return (
        *get_sanitizer_specs(frameworks=frameworks),
        *discover_custom_sanitizers(target_dir),
    )


def _package_scan_result(
    package: WorkspacePackage,
    artifact: ScanResult,
) -> PackageScanResult:
    return PackageScanResult(
        name=package.name,
        path=str(package.path),
        language=package.language,
        frameworks=list(package.frameworks),
        files_scanned=list(artifact.files_scanned),
        functions=list(artifact.functions),
        entry_points=list(artifact.entry_points),
        attack_surface=list(artifact.attack_surface),
        dependency_findings=[
            _tag_finding_for_package(finding, package) for finding in artifact.dependency_findings
        ],
    )


def _tag_finding_for_package(
    finding: CandidateFinding,
    package: WorkspacePackage,
) -> CandidateFinding:
    metadata = dict(finding.metadata)
    metadata.setdefault("package", package.name)
    metadata.setdefault("package_path", str(package.path))
    return finding.model_copy(update={"metadata": metadata})


def _merge_call_graphs(
    call_graphs: Sequence[dict[str, list[str]]],
) -> dict[str, list[str]]:
    merged: dict[str, list[str]] = {}
    for call_graph in call_graphs:
        for method_name, calls in call_graph.items():
            combined = set(merged.get(method_name, ()))
            combined.update(calls)
            merged[method_name] = sorted(combined)
    return merged


def _merge_functions(function_groups: Sequence[Sequence[ScannedFunction]]) -> list[ScannedFunction]:
    merged: dict[str, ScannedFunction] = {}
    for functions in function_groups:
        for function in functions:
            merged.setdefault(function.function_id, function)
    return sorted(
        merged.values(),
        key=lambda function: (
            function.location.file,
            function.location.line,
            function.location.column,
            function.name,
        ),
    )


def _dedupe_candidate_findings_by_fingerprint(
    findings: Sequence[CandidateFinding],
) -> list[CandidateFinding]:
    deduped: dict[str, CandidateFinding] = {}
    for finding in findings:
        deduped.setdefault(stable_fingerprint(finding), finding)
    return list(deduped.values())


def _merge_monorepo_scan_artifacts(
    context: PipelineContext,
    config: PiranesiConfig,
    manifest: MonorepoManifest,
    package_results: Sequence[tuple[WorkspacePackage, ScanResult]],
) -> ScanResult:
    merged_files: list[str] = []
    merged_entry_points = []
    merged_attack_surface = []
    merged_dependency_findings: list[CandidateFinding] = []
    merged_sbom_artifacts: dict[str, str] = {}

    for package, artifact in package_results:
        for file_name in artifact.files_scanned:
            if file_name not in merged_files:
                merged_files.append(file_name)
        merged_entry_points.extend(artifact.entry_points)
        merged_attack_surface.extend(artifact.attack_surface)
        merged_dependency_findings.extend(
            _tag_finding_for_package(finding, package) for finding in artifact.dependency_findings
        )
        for sbom_name, sbom_path in artifact.sbom_artifacts.items():
            merged_sbom_artifacts.setdefault(sbom_name, sbom_path)

    metadata = ScanMetadata(
        timestamp=_utc_now(),
        duration_ms=0,
        tree_sitter_version="unknown",
        piranesi_version=__version__,
        files_parsed=len(merged_files),
        parse_errors=sum(artifact.metadata.parse_errors for _, artifact in package_results),
        config_hash=config_hash(config),
    )

    return ScanResult(
        project_root=str(context.target_dir.resolve(strict=False)),
        files_scanned=merged_files,
        call_graph=_merge_call_graphs([artifact.call_graph for _, artifact in package_results]),
        functions=_merge_functions([artifact.functions for _, artifact in package_results]),
        entry_points=merged_entry_points,
        attack_surface=merged_attack_surface,
        dependency_findings=_dedupe_candidate_findings_by_fingerprint(merged_dependency_findings),
        sbom_artifacts=merged_sbom_artifacts,
        package_results=[
            _package_scan_result(package, artifact) for package, artifact in package_results
        ],
        monorepo_detected_tool=manifest.detected_tool,
        metadata=metadata,
    )


def _workspace_source_files(package_path: Path) -> list[Path]:
    files: list[Path] = []
    for path in sorted(package_path.rglob("*")):
        if not path.is_file() or path.suffix not in {".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"}:
            continue
        if any(
            part in _WORKSPACE_SOURCE_EXCLUDE_PARTS or part.startswith(_PIRANESI_TRACE_PREFIX)
            for part in path.parts
        ):
            continue
        files.append(path.resolve(strict=False))
    return files


def _exported_sink_findings(package: WorkspacePackage) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for source_file in _workspace_source_files(package.path):
        try:
            lines = source_file.read_text(encoding="utf-8").splitlines()
        except OSError:
            continue
        line_index = 0
        while line_index < len(lines):
            line = lines[line_index]
            match = _EXPORT_FUNCTION_PATTERN.search(line)
            if match is None:
                line_index += 1
                continue

            function_name = match.group("name")
            parameters = [
                parameter.strip().split("=", 1)[0].strip()
                for parameter in match.group("params").split(",")
                if parameter.strip()
            ]
            body_lines = [line]
            brace_balance = line.count("{") - line.count("}")
            body_index = line_index + 1
            saw_open_brace = "{" in line
            while body_index < len(lines):
                body_line = lines[body_index]
                body_lines.append(body_line)
                brace_balance += body_line.count("{") - body_line.count("}")
                saw_open_brace = saw_open_brace or "{" in body_line
                body_index += 1
                if saw_open_brace and brace_balance <= 0:
                    break

            body_text = "\n".join(body_lines)
            sink_match = _SQL_SINK_PATTERN.search(body_text) or _COMMAND_SINK_PATTERN.search(
                body_text
            )
            if sink_match is not None and any(
                parameter and parameter in body_text for parameter in parameters
            ):
                parameter_name = next(
                    (parameter for parameter in parameters if parameter and parameter in body_text),
                    "input",
                )
                vuln_class = (
                    "CWE-89: SQL Injection"
                    if sink_match.re is _SQL_SINK_PATTERN
                    else "CWE-78: Command Injection"
                )
                severity = "high" if sink_match.re is _SQL_SINK_PATTERN else "critical"
                sink_line_offset = next(
                    (
                        offset
                        for offset, body_line in enumerate(body_lines)
                        if sink_match.group("api") in body_line
                    ),
                    0,
                )
                sink_line_number = line_index + sink_line_offset + 1
                sink_line = body_lines[sink_line_offset]
                finding_id = sha256(
                    (
                        f"export|{package.name}|{source_file.as_posix()}|"
                        f"{function_name}|{sink_line_number}|{vuln_class}"
                    ).encode()
                ).hexdigest()[:16]
                source_location = SourceLocation(
                    file=str(source_file),
                    line=line_index + 1,
                    column=1,
                    snippet=line.strip(),
                )
                sink_location = SourceLocation(
                    file=str(source_file),
                    line=sink_line_number,
                    column=1,
                    snippet=sink_line.strip(),
                )
                findings.append(
                    CandidateFinding(
                        id=finding_id,
                        vuln_class=vuln_class,
                        source=TaintSource(
                            location=source_location,
                            source_type="exported_parameter",
                            data_categories=["user_input"],
                            parameter_name=parameter_name,
                        ),
                        sink=TaintSink(
                            location=sink_location,
                            sink_type="sql_query"
                            if sink_match.re is _SQL_SINK_PATTERN
                            else "command_execution",
                            api_name=sink_match.group("api"),
                        ),
                        taint_path=[
                            TaintStep(
                                location=sink_location,
                                operation="exported_function_summary",
                                taint_state="tainted",
                                through_function=function_name,
                            )
                        ],
                        path_conditions=[],
                        confidence=0.82,
                        severity=severity,
                        metadata={
                            "package": package.name,
                            "package_path": str(package.path),
                            "workspace_export": True,
                            "exported_function": function_name,
                        },
                    )
                )
            line_index = max(body_index, line_index + 1)
    return findings


def _parse_named_imports(source_text: str) -> list[tuple[str, dict[str, str]]]:
    parsed: list[tuple[str, dict[str, str]]] = []
    for pattern in (_NAMED_IMPORT_PATTERN, _DESTRUCTURED_REQUIRE_PATTERN):
        for match in pattern.finditer(source_text):
            raw_imports = match.group("imports")
            bindings: dict[str, str] = {}
            for raw_binding in raw_imports.split(","):
                binding = raw_binding.strip()
                if not binding:
                    continue
                if " as " in binding:
                    imported_name, local_name = [part.strip() for part in binding.split(" as ", 1)]
                elif ":" in binding:
                    imported_name, local_name = [part.strip() for part in binding.split(":", 1)]
                else:
                    imported_name = binding
                    local_name = binding
                bindings[local_name] = imported_name
            if bindings:
                parsed.append((match.group("package"), bindings))
    return parsed


def _cross_package_findings(
    manifest: MonorepoManifest,
    selected_packages: Sequence[WorkspacePackage],
    package_findings: Mapping[str, Sequence[CandidateFinding]],
) -> list[CandidateFinding]:
    export_summaries: dict[tuple[str, str], list[CandidateFinding]] = defaultdict(list)
    selected_names = {package.name for package in selected_packages}
    for package_name, package_candidate_findings in package_findings.items():
        if package_name not in selected_names:
            continue
        for finding in package_candidate_findings:
            exported_function = finding.metadata.get("exported_function")
            if not finding.metadata.get("workspace_export") or not isinstance(
                exported_function, str
            ):
                continue
            export_summaries[(package_name, exported_function)].append(finding)

    findings: list[CandidateFinding] = []
    for package in selected_packages:
        dependency_names = set(package.internal_deps)
        if not dependency_names:
            continue
        for source_file in _workspace_source_files(package.path):
            try:
                source_text = source_file.read_text(encoding="utf-8")
            except OSError:
                continue
            import_bindings = _parse_named_imports(source_text)
            if not import_bindings:
                continue
            lines = source_text.splitlines()
            for imported_package, bindings in import_bindings:
                dependency_package = next(
                    (
                        dependency_name
                        for dependency_name in dependency_names
                        if dependency_name == imported_package
                    ),
                    None,
                )
                if dependency_package is None:
                    continue
                for line_no, line in enumerate(lines, start=1):
                    source_match = _REQUEST_SOURCE_PATTERN.search(line)
                    if source_match is None:
                        continue
                    for local_name, imported_name in bindings.items():
                        call_pattern = re.compile(
                            rf"\b{re.escape(local_name)}\s*\((?P<args>[^)]*)\)"
                        )
                        call_match = call_pattern.search(line)
                        if call_match is None:
                            continue
                        summary_findings = export_summaries.get(
                            (dependency_package, imported_name), ()
                        )
                        for summary in summary_findings:
                            source_location = SourceLocation(
                                file=str(source_file),
                                line=line_no,
                                column=1,
                                snippet=line.strip(),
                            )
                            finding_id = sha256(
                                (
                                    f"xpkg|{package.name}|{dependency_package}|"
                                    f"{source_file.as_posix()}|{line_no}|{imported_name}|{summary.id}"
                                ).encode()
                            ).hexdigest()[:16]
                            findings.append(
                                CandidateFinding(
                                    id=finding_id,
                                    vuln_class=summary.vuln_class,
                                    source=TaintSource(
                                        location=source_location,
                                        source_type=source_match.group(0),
                                        data_categories=["user_input"],
                                        parameter_name=source_match.group(0),
                                    ),
                                    sink=summary.sink,
                                    taint_path=[
                                        TaintStep(
                                            location=source_location,
                                            operation="internal_dependency_call",
                                            taint_state="tainted",
                                            through_function=(
                                                f"{package.name} -> "
                                                f"{dependency_package}:{imported_name}"
                                            ),
                                        ),
                                        *summary.taint_path,
                                    ],
                                    path_conditions=[],
                                    confidence=max(summary.confidence, 0.74),
                                    severity=summary.severity,
                                    metadata={
                                        "package": package.name,
                                        "package_path": str(package.path),
                                        "cross_package": True,
                                        "source_package": package.name,
                                        "sink_package": dependency_package,
                                        "exported_function": imported_name,
                                    },
                                )
                            )
    return findings


def _run_scan_stage(
    context: PipelineContext,
    config: PiranesiConfig,
    prev_result: StageResult | None,
) -> StageResult:
    _ = prev_result
    started_at = time.monotonic()
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
        and not context.changed_packages_only
        and context.monorepo_package_name is None
        and context.selected_files is None
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

    monorepo_selection = _monorepo_selection(context, config)
    if monorepo_selection is not None:
        manifest, selected_packages = monorepo_selection
        if not selected_packages:
            artifact = _empty_scan_result(
                context.target_dir,
                config,
                detected_tool=manifest.detected_tool,
            )
            elapsed_s = time.monotonic() - started_at
            artifact = artifact.model_copy(
                update={
                    "metadata": artifact.metadata.model_copy(
                        update={"duration_ms": int(elapsed_s * 1000)}
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

        package_artifacts: list[tuple[WorkspacePackage, ScanResult]] = []
        cache_states: list[str] = []
        for package in selected_packages:
            package_artifact, cache_status, _ = _build_scan_artifact_for_target(
                context,
                config,
                package.path,
                changed_files=_package_changed_files(context, package),
            )
            package_artifacts.append((package, package_artifact))
            cache_states.append(cache_status)

        artifact = _merge_monorepo_scan_artifacts(context, config, manifest, package_artifacts)
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
            cache_status="HIT"
            if cache_states and all(state == "HIT" for state in cache_states)
            else "MISS"
            if cache_states and any(state == "MISS" for state in cache_states)
            else "BYPASS",
        )

    changed_files = _apply_incremental_threshold(
        _context_changed_files(context),
        config=config,
        context=context,
    )
    artifact, cache_status, _ = _build_scan_artifact_for_target(
        context,
        config,
        context.target_dir,
        changed_files=changed_files,
    )
    elapsed_s = time.monotonic() - started_at
    _finalize_incremental_manifest(context, "scan")
    return StageResult(
        stage="scan",
        success=True,
        artifact=artifact,
        elapsed_s=elapsed_s,
        cache_status=cache_status,
    )


def _run_detect_stage(
    context: PipelineContext,
    config: PiranesiConfig,
    prev_result: StageResult | None,
) -> StageResult:
    _ = prev_result
    started_at = time.monotonic()
    incremental = context.incremental
    previous_detect = (
        _load_artifact(context.output_dir / "detect.json", DetectArtifact)
        if incremental is not None
        else None
    )
    carried_findings = (
        _carry_forward_findings(previous_detect, incremental, context.target_dir)
        if (
            incremental is not None
            and not context.changed_packages_only
            and context.monorepo_package_name is None
            and context.selected_files is None
        )
        else []
    )
    if (
        incremental is not None
        and incremental.previous_manifest is not None
        and not incremental.diff.has_changes
        and previous_detect is not None
        and not context.changed_packages_only
        and context.monorepo_package_name is None
        and context.selected_files is None
    ):
        annotated_findings, reachability_result = _annotate_reachability_for_findings(
            context,
            config,
            carried_findings,
        )
        _finalize_incremental_manifest(context, "detect")
        suppressed_findings, suppression_lifecycle = _apply_project_suppressions(
            context.target_dir,
            annotated_findings,
            fail_on_invalid=config.suppression.fail_on_invalid,
            fail_on_expired=config.suppression.fail_on_expired,
            fail_on_stale=config.suppression.fail_on_stale,
        )
        return StageResult(
            stage="detect",
            success=True,
            artifact=DetectArtifact(
                findings=suppressed_findings,
                reachability=reachability_result,
                suppression_lifecycle=suppression_lifecycle,
            ),
            elapsed_s=time.monotonic() - started_at,
        )

    monorepo_selection = _monorepo_selection(context, config)
    if monorepo_selection is not None:
        manifest, selected_packages = monorepo_selection
        package_findings: dict[str, list[CandidateFinding]] = {}
        findings: list[CandidateFinding] = []
        for package in selected_packages:
            local_findings = [
                _tag_finding_for_package(finding, package)
                for finding in _detect_findings_for_target(
                    context,
                    config,
                    package.path,
                    changed_files=_package_changed_files(context, package),
                )
            ]
            exported_findings = _exported_sink_findings(package)
            package_findings[package.name] = [*local_findings, *exported_findings]
            findings.extend(package_findings[package.name])
        findings.extend(_cross_package_findings(manifest, selected_packages, package_findings))
        _finalize_incremental_manifest(context, "detect")
        merged_findings = _dedupe_candidate_findings_by_fingerprint([*carried_findings, *findings])
        annotated_findings, reachability_result = _annotate_reachability_for_findings(
            context,
            config,
            merged_findings,
        )
        suppressed_findings, suppression_lifecycle = _apply_project_suppressions(
            context.target_dir,
            annotated_findings,
            fail_on_invalid=config.suppression.fail_on_invalid,
            fail_on_expired=config.suppression.fail_on_expired,
            fail_on_stale=config.suppression.fail_on_stale,
        )
        return StageResult(
            stage="detect",
            success=True,
            artifact=DetectArtifact(
                findings=suppressed_findings,
                reachability=reachability_result,
                suppression_lifecycle=suppression_lifecycle,
            ),
            elapsed_s=time.monotonic() - started_at,
        )

    changed_files = _apply_incremental_threshold(
        _context_changed_files(context),
        config=config,
        context=context,
    )
    findings = _detect_findings_for_target(
        context,
        config,
        context.target_dir,
        changed_files=changed_files,
    )

    _finalize_incremental_manifest(context, "detect")
    merged_findings = _merge_candidate_findings(carried_findings, findings)
    annotated_findings, reachability_result = _annotate_reachability_for_findings(
        context,
        config,
        merged_findings,
    )
    suppressed_findings, suppression_lifecycle = _apply_project_suppressions(
        context.target_dir,
        annotated_findings,
        fail_on_invalid=config.suppression.fail_on_invalid,
        fail_on_expired=config.suppression.fail_on_expired,
        fail_on_stale=config.suppression.fail_on_stale,
    )
    return StageResult(
        stage="detect",
        success=True,
        artifact=DetectArtifact(
            findings=suppressed_findings,
            reachability=reachability_result,
            suppression_lifecycle=suppression_lifecycle,
        ),
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

    for pattern in _PIRANESI_OUTPUT_EXCLUDE_PATTERNS:
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
    output_excludes = (
        # piranesi output dirs
        "--exclude",
        "piranesi-output",
        "--exclude",
        ".piranesi-cache",
        "--exclude",
        ".piranesi-out",
        "--exclude",
        ".piranesi-trace*",
    )
    if language == "python":
        return ()
    if language == "go":
        return (*output_excludes, "--exclude", "vendor")
    if language == "java":
        return (*output_excludes, "--exclude", "src/test")
    return output_excludes


def _run_triage_stage(
    context: PipelineContext,
    config: PiranesiConfig,
    prev_result: StageResult | None,
) -> StageResult:
    detect_artifact = _extract_stage_artifact(prev_result, DetectArtifact, "detect")
    started_at = time.monotonic()
    active_findings = [finding for finding in detect_artifact.findings if not finding.suppressed]
    active_findings = [
        finding for finding in active_findings if finding.reachability == "reachable"
    ]
    if not _llm_is_configured():
        deterministic_findings = [
            TriagedFinding(
                finding=finding,
                triage_verdict="true_positive",
                triage_mode="deterministic",
                skeptic_analysis=(
                    "Deterministic mode: no LLM API key was configured, so Piranesi "
                    "preserved the reachable static finding without model-backed "
                    "false-positive discrimination."
                ),
                ensemble_score=finding.confidence,
                escalated=False,
            )
            for finding in active_findings
        ]
        return StageResult(
            stage="triage",
            success=True,
            artifact=TriageArtifact(findings=deterministic_findings),
            elapsed_s=time.monotonic() - started_at,
        )

    voter = CalibratedEnsembleVoter(provider=context.provider, router=context.router)
    skeptic = SkepticAgent(provider=context.provider, router=context.router)
    ml_scores: dict[str, float] = {}
    if config.triage.ml_prefilter and active_findings:
        classifier = load_model(config.triage.ml_model_path)
        for prediction in predict(
            active_findings,
            classifier=classifier,
            model_path=config.triage.ml_model_path,
        ):
            ml_scores[prediction.finding.id] = prediction.true_positive_probability

    findings: list[TriagedFinding] = []
    for finding in active_findings:
        if context.router is not None and context.router.remaining_tokens <= 64:
            remaining_findings = active_findings[len(findings) :]
            _logger.warning(
                "triage: token budget exhausted after %d processed finding(s); "
                "preserving %d remaining reachable finding(s) as deterministic true positives",
                len(findings),
                len(remaining_findings),
                extra={
                    "event": "triage_token_budget_degrade",
                    "processed_findings": len(findings),
                    "remaining_findings": len(remaining_findings),
                    "remaining_tokens": context.router.remaining_tokens,
                    "max_tokens": config.budget.max_tokens,
                },
            )
            for remaining in remaining_findings:
                fallback_score = ml_scores.get(remaining.id, remaining.confidence)
                findings.append(
                    TriagedFinding(
                        finding=remaining,
                        triage_verdict="true_positive",
                        triage_mode="deterministic",
                        skeptic_analysis=(
                            "Deterministic fallback: token budget exhausted before LLM "
                            "triage. Reachable candidate preserved for manual review."
                        ),
                        ensemble_score=fallback_score,
                        escalated=False,
                    )
                )
            break

        probability = ml_scores.get(finding.id)
        if (
            config.triage.ml_prefilter
            and probability is not None
            and probability < config.triage.ml_threshold
            and not config.triage.ml_conservative
        ):
            findings.append(
                TriagedFinding(
                    finding=finding,
                    triage_verdict="false_positive",
                    triage_mode="ml_prefilter",
                    skeptic_analysis=(
                        "ML pre-filter flagged likely false positive "
                        f"(p={probability:.2f} < threshold={config.triage.ml_threshold:.2f})."
                    ),
                    ensemble_score=probability,
                    escalated=False,
                )
            )
            continue
        findings.append(voter.triage_finding(finding, skeptic=skeptic))
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
    proof_mode = config.verify.proof_mode
    selected_profile = _resolve_selected_target_profile(config)
    triage_artifact = _extract_stage_artifact(prev_result, TriageArtifact, "triage")
    started_at = time.monotonic()
    confirmed_findings: list[ConfirmedFinding] = []
    verification_attempts: list[VerificationAttempt] = []

    for triaged in triage_artifact.findings:
        if triaged.triage_verdict == "false_positive":
            continue
        active_profile = _effective_target_profile(
            selected_profile=selected_profile,
            finding=triaged.finding,
        )
        template = extract_exploit_template(triaged.finding, proof_mode=proof_mode)
        precondition_eval = evaluate_verification_preconditions(
            finding=triaged.finding,
            template=template,
            target_dir=context.target_dir,
            proof_mode=proof_mode,
            target_profile_name=None if active_profile is None else active_profile.name,
            target_profile_base_url=None if active_profile is None else active_profile.base_url,
            no_execute=context.no_execute,
        )
        attempt_fields: dict[str, Any] = {
            "finding_id": triaged.finding.id,
            "proof_mode": proof_mode,
            "target_profile": None if active_profile is None else active_profile.name,
            "template_id": template.template_id,
            "template_reason": template.template_selection_reason,
            "preconditions": list(precondition_eval.preconditions),
        }

        def _append_attempt(
            *,
            status: VerificationStatus,
            reason: str,
            evidence: list[str] | None = None,
            payload: Any | None = None,
            baseline_capture: Any | None = None,
            exploit_capture: Any | None = None,
            launch_log_path: str | None = None,
            startup_error: str | None = None,
            error_text: str | None = None,
            _triaged: TriagedFinding = triaged,
            _template_id: str | None = template.template_id,
            _base_url: str | None = None if active_profile is None else active_profile.base_url,
            _attempt_fields: dict[str, Any] = attempt_fields,
        ) -> None:
            evidence_items = [] if evidence is None else list(evidence)
            baseline_response = (
                None
                if baseline_capture is None
                else getattr(baseline_capture, "http_response", None)
            )
            exploit_response = (
                None if exploit_capture is None else getattr(exploit_capture, "http_response", None)
            )
            (
                rich_evidence,
                sanitized_reason,
                sanitized_evidence,
                sanitized_error_text,
                evidence_artifact_payload,
            ) = build_verification_evidence(
                finding=_triaged.finding,
                template_id=_template_id,
                payload=payload,
                base_url=_base_url,
                baseline_response=baseline_response,
                exploit_response=exploit_response,
                baseline_capture=baseline_capture,
                exploit_capture=exploit_capture,
                reason=reason,
                evidence=evidence_items,
                error_text=startup_error if startup_error is not None else error_text,
            )
            evidence_artifact_payload["status"] = status
            evidence_artifact_path = write_verification_evidence_artifact(
                output_dir=context.output_dir,
                finding_id=_triaged.finding.id,
                payload=evidence_artifact_payload,
            )
            verification_attempts.append(
                VerificationAttempt(
                    **_attempt_fields,
                    status=status,
                    reason=sanitized_reason,
                    launch_log_path=launch_log_path,
                    startup_error=(
                        sanitized_error_text if startup_error is not None else startup_error
                    ),
                    evidence=sanitized_evidence,
                    rich_evidence=rich_evidence,
                    evidence_artifact_path=evidence_artifact_path,
                )
            )

        if precondition_eval.skip_reason is not None:
            _append_attempt(
                status="skipped",
                reason=precondition_eval.skip_reason,
            )
            continue

        solve_result = solve_exploit_template(
            template,
            allow_unsafe_payloads=proof_mode == "unsafe",
        )
        if solve_result.status != "SAT" or not solve_result.solutions:
            _append_attempt(
                status="inconclusive",
                reason=(
                    "verification inconclusive: solver did not produce an executable payload"
                    if solve_result.reason is None
                    else f"verification inconclusive: {solve_result.reason}"
                ),
                evidence=[] if solve_result.reason is None else [solve_result.reason],
                error_text=solve_result.reason,
            )
            continue
        payload = solve_result.solutions[0].payload

        baseline_payload = build_baseline_payload(payload, vuln_class=triaged.finding.vuln_class)
        try:
            captures = run_in_sandbox(
                str(context.target_dir),
                [baseline_payload, payload],
                target_profile=active_profile,
                logs_base_dir=context.output_dir,
            )
        except Exception as exc:
            _logger.warning(
                "verify: sandbox execution failed for finding %s: %s",
                triaged.finding.id,
                exc,
                exc_info=True,
            )
            _append_attempt(
                status="error",
                reason=f"verification error: sandbox execution failed ({exc})",
                evidence=[str(exc)],
                payload=payload,
                startup_error=str(exc),
                error_text=str(exc),
            )
            continue
        if len(captures) < 2:
            _append_attempt(
                status="inconclusive",
                reason=(
                    "verification inconclusive: sandbox did not return baseline+exploit captures"
                ),
                evidence=["MISSING_SANDBOX_CAPTURES"],
                payload=payload,
            )
            continue
        baseline_capture, exploit_capture = captures[0], captures[1]
        launch_profile = exploit_capture.launch_profile or baseline_capture.launch_profile
        launch_log_path = exploit_capture.launch_log_path or baseline_capture.launch_log_path
        startup_error = exploit_capture.startup_error or baseline_capture.startup_error
        if launch_profile and attempt_fields["target_profile"] is None:
            attempt_fields["target_profile"] = launch_profile
        if baseline_capture.error or exploit_capture.error:
            capture_error = (
                exploit_capture.error or baseline_capture.error or "UNKNOWN_SANDBOX_ERROR"
            )
            evidence = [capture_error]
            if launch_log_path:
                evidence.append(f"launch_logs:{launch_log_path}")
            _append_attempt(
                status="inconclusive",
                reason=f"verification inconclusive: sandbox capture error ({capture_error})",
                evidence=evidence,
                payload=payload,
                baseline_capture=baseline_capture,
                exploit_capture=exploit_capture,
                launch_log_path=launch_log_path,
                startup_error=startup_error,
                error_text=capture_error,
            )
            continue
        confirmation = confirm_responses(
            triaged.finding.vuln_class,
            payload,
            baseline_capture.http_response,
            exploit_capture.http_response,
            container_logs=exploit_capture.container_logs,
        )
        if confirmation.level != "CONFIRMED":
            evidence = [confirmation.evidence]
            if launch_log_path:
                evidence.append(f"launch_logs:{launch_log_path}")
            _append_attempt(
                status="inconclusive",
                reason=f"verification inconclusive: {confirmation.evidence}",
                evidence=evidence,
                payload=payload,
                baseline_capture=baseline_capture,
                exploit_capture=exploit_capture,
                launch_log_path=launch_log_path,
                startup_error=startup_error,
                error_text=confirmation.evidence,
            )
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
                verification_template_id=template.template_id,
                verification_template_reason=template.template_selection_reason,
                verification_template_risk_level=template.risk_level,
                verification_expected_evidence=list(template.expected_evidence),
                sandbox_result=_sandbox_result_from_capture(exploit_capture, confirmed=True),
                reproducer_script=generate_reproducer_script(
                    triaged.finding,
                    target_path=context.target_dir,
                    payload=payload,
                ),
                related_cves=[],
            )
        )
        _append_attempt(
            status="confirmed",
            reason=f"dynamic verification confirmed: {confirmation.evidence}",
            evidence=(
                [confirmation.evidence]
                if not launch_log_path
                else [confirmation.evidence, f"launch_logs:{launch_log_path}"]
            ),
            payload=payload,
            baseline_capture=baseline_capture,
            exploit_capture=exploit_capture,
            launch_log_path=launch_log_path,
            startup_error=startup_error,
            error_text=confirmation.evidence,
        )

    return StageResult(
        stage="verify",
        success=True,
        artifact=VerifyArtifact(findings=confirmed_findings, attempts=verification_attempts),
        elapsed_s=time.monotonic() - started_at,
    )


def _resolve_selected_target_profile(config: PiranesiConfig) -> TargetLaunchProfile | None:
    profile_name = config.verify.target_profile
    if profile_name is None:
        return None
    profile = config.verify.target_profiles.get(profile_name)
    if profile is None:
        available = ", ".join(sorted(config.verify.target_profiles)) or "none"
        raise ValueError(
            f"verify.target_profile '{profile_name}' is not defined in "
            f"[verify.target_profiles] (available: {available})"
        )
    return TargetLaunchProfile(
        name=profile_name,
        command=profile.command,
        cwd=profile.cwd,
        env=dict(profile.env),
        startup_timeout_seconds=profile.startup_timeout_seconds,
        readiness_url=profile.readiness_url,
        readiness_command=profile.readiness_command,
        base_url=profile.base_url,
        teardown=profile.teardown,
        logs_path=profile.logs_path,
    )


def _effective_target_profile(
    *,
    selected_profile: TargetLaunchProfile | None,
    finding: CandidateFinding,
) -> TargetLaunchProfile | None:
    if selected_profile is not None:
        return selected_profile
    target_url = finding.metadata.get("verification_target_url")
    if not isinstance(target_url, str) or not target_url.strip():
        return None
    return TargetLaunchProfile(
        name="metadata_target_url",
        base_url=target_url.strip(),
        startup_timeout_seconds=30,
        teardown="never",
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
    verify_artifact = _require_artifact(context.stage_outputs["verify"], VerifyArtifact, "verify")
    _ = prev_result
    started_at = time.monotonic()
    if not _llm_is_configured():
        _logger.warning(
            "patch: no LLM API key configured, skipping patch generation for %d finding(s)",
            len(verify_artifact.findings),
        )
        return StageResult(
            stage="patch",
            success=True,
            artifact=PatchArtifact(patches=[]),
            elapsed_s=time.monotonic() - started_at,
        )
    if context.router is not None and context.router.remaining_tokens <= 64:
        _logger.warning(
            "patch: token budget exhausted before patch generation; skipping %d finding(s)",
            len(verify_artifact.findings),
            extra={
                "event": "patch_token_budget_degrade",
                "remaining_tokens": context.router.remaining_tokens,
                "max_tokens": config.budget.max_tokens,
                "skipped_findings": len(verify_artifact.findings),
            },
        )
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
    _ = prev_result
    started_at = time.monotonic()
    scan_artifact = _require_artifact(context.stage_outputs["scan"], ScanResult, "scan")
    detect_artifact = _require_artifact(context.stage_outputs["detect"], DetectArtifact, "detect")
    verify_artifact = _require_artifact(context.stage_outputs["verify"], VerifyArtifact, "verify")
    legal_artifact = _require_artifact(context.stage_outputs["legal"], LegalArtifact, "legal")
    patch_artifact = _require_artifact(context.stage_outputs["patch"], PatchArtifact, "patch")
    triage_artifact = context.stage_outputs.get("triage")

    report = build_report(
        scan_result=scan_artifact,
        detected_findings=detect_artifact.findings,
        triaged_findings=(
            list(triage_artifact.findings) if isinstance(triage_artifact, TriageArtifact) else None
        ),
        confirmed_findings=verify_artifact.findings,
        verification_attempts=verify_artifact.attempts,
        legal_assessments=legal_artifact.assessments,
        patch_results=patch_artifact.patches,
        target_dir=context.target_dir,
        total_llm_cost_usd=context.total_cost_usd,
        duration_s=sum(context.stage_timings_s.values()),
        stage_timings_s=context.stage_timings_s,
        reachability=detect_artifact.reachability,
        include_unreachable=config.reachability.include_unreachable,
        dead_code_report=config.reachability.dead_code_report,
        suppression_lifecycle=detect_artifact.suppression_lifecycle,
        ownership_config=config.ownership,
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


def _load_artifact[T: BaseModel](path: Path, artifact_type: type[T]) -> T | None:
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
        launch_profile=getattr(capture, "launch_profile", None),
        launch_log_path=getattr(capture, "launch_log_path", None),
        startup_error=getattr(capture, "startup_error", None),
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


def _resolve_stage_model(context: PipelineContext, stage: str) -> str | None:
    if context.router is None:
        return None
    return context.router.resolve(stage)


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
