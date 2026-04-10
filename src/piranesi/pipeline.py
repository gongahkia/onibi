from __future__ import annotations

import json
import os
import time
from collections import OrderedDict
from collections.abc import Callable, Mapping, Sequence
from dataclasses import dataclass, field
from datetime import UTC, datetime
from fnmatch import fnmatch
from pathlib import Path
from typing import Any

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from piranesi import __version__
from piranesi.config import PiranesiConfig, config_hash
from piranesi.detect import extract_candidate_findings
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
from piranesi.scan.joern import JoernServer
from piranesi.scan.specs import get_sanitizer_specs, get_sink_specs, get_source_specs
from piranesi.scan.surface import build_scan_result
from piranesi.scan.transpile import transpile_project
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
    include_patterns = _normalize_globs(config.scan.include_patterns)
    exclude_patterns = _normalize_globs(config.scan.exclude_patterns)
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
        for stage_name in STAGE_ORDER:
            stage = stage_registry[stage_name]
            artifact_path = context.output_dir / f"{stage_name}.json"
            if render_ui:
                stage_header(stage_name)
                assert progress is not None
                assert task_id is not None
                progress.update(task_id, description=f"{stage_name}")

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
                    continue

            started_at = time.monotonic()
            try:
                result = stage.runner(config, prev_result)
            except Exception as exc:
                elapsed_s = time.monotonic() - started_at
                result = StageResult(
                    stage=stage_name,
                    success=False,
                    artifact=None,
                    elapsed_s=elapsed_s,
                    error=str(exc),
                )
                results.append(result)
                failed_stage = stage_name
                partial_summary_path = _save_partial_summary(context, results, result)
                break

            if result.elapsed_s <= 0:
                result.elapsed_s = time.monotonic() - started_at
            context.stage_timings_s[stage_name] = result.elapsed_s
            context.stage_outputs[stage_name] = result.artifact
            _write_artifact(artifact_path, result.artifact)
            results.append(result)
            prev_result = result
            if progress is not None and task_id is not None:
                progress.advance(task_id)

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
    transpiled = transpile_project(context.target_dir)
    try:
        with JoernServer(config=config.joern) as server:
            server.import_project(transpiled.out_dir)
            metadata = ScanMetadata(
                timestamp=_utc_now(),
                duration_ms=0,
                tree_sitter_version="unknown",
                piranesi_version=__version__,
                files_parsed=len(discover_scan_targets(context.target_dir, config)),
                parse_errors=len(transpiled.failed_files),
                config_hash=config_hash(config),
            )
            artifact = build_scan_result(
                server,
                project_root=context.target_dir,
                metadata=metadata,
                joern_project_root=transpiled.out_dir,
                source_map=transpiled.source_map,
                source_specs=source_specs,
                sink_specs=sink_specs,
                sanitizer_specs=sanitizer_specs,
            )
    finally:
        transpiled.cleanup()

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
    return StageResult(stage="scan", success=True, artifact=artifact, elapsed_s=elapsed_s)


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
    transpiled = transpile_project(context.target_dir)
    try:
        with JoernServer(config=config.joern) as server:
            server.import_project(transpiled.out_dir)
            findings = extract_candidate_findings(
                server,
                joern_project_root=transpiled.out_dir,
                source_map=transpiled.source_map,
                source_specs=source_specs,
                sink_specs=sink_specs,
                sanitizer_specs=sanitizer_specs,
                category_provider=context.provider if _llm_is_configured() else None,
                category_model=context.router.resolve("detector") if _llm_is_configured() else None,
            )
    finally:
        transpiled.cleanup()

    return StageResult(
        stage="detect",
        success=True,
        artifact=DetectArtifact(findings=list(findings)),
        elapsed_s=time.monotonic() - started_at,
    )


def _run_triage_stage(
    context: PipelineContext,
    config: PiranesiConfig,
    prev_result: StageResult | None,
) -> StageResult:
    _ = config
    detect_artifact = _extract_stage_artifact(prev_result, DetectArtifact, "detect")
    started_at = time.monotonic()
    if not _llm_is_configured():
        findings = [
            TriagedFinding(
                finding=finding,
                triage_verdict="true_positive",
                skeptic_analysis="LLM triage skipped because no API key is configured.",
                ensemble_score=finding.confidence,
                escalated=False,
            )
            for finding in detect_artifact.findings
        ]
        return StageResult(
            stage="triage",
            success=True,
            artifact=TriageArtifact(findings=findings),
            elapsed_s=time.monotonic() - started_at,
        )

    voter = CalibratedEnsembleVoter(provider=context.provider, router=context.router)
    skeptic = SkepticAgent(provider=context.provider, router=context.router)
    findings = [
        voter.triage_finding(finding, skeptic=skeptic) for finding in detect_artifact.findings
    ]
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
    _ = _extract_stage_artifact(prev_result, LegalArtifact, "legal")
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
    "discover_scan_targets",
    "load_partial_summary",
    "run_pipeline",
]
