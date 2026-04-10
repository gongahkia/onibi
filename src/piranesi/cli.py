from __future__ import annotations

import json
import logging
import sys
import time
from collections.abc import Mapping
from dataclasses import dataclass
from enum import StrEnum
from pathlib import Path
from typing import Annotated, Any

import typer
from pydantic import BaseModel

from piranesi import __version__
from piranesi.config import ConfigError, PiranesiConfig, load_config
from piranesi.detect import append_ignore_file_suppression
from piranesi.diff import build_baseline_artifact, diff_findings, load_findings, render_diff
from piranesi.llm.cost import CostTracker
from piranesi.llm.provider import LLMProvider
from piranesi.llm.router import ModelRouter
from piranesi.llm.trace import TraceLogger
from piranesi.models import ScanResult
from piranesi.observability import log_error_context, setup_logging
from piranesi.pipeline import (
    DetectArtifact,
    LegalArtifact,
    PatchArtifact,
    PipelineContext,
    StageResult,
    TriageArtifact,
    VerifyArtifact,
    build_default_stage_registry,
    discover_scan_targets,
    load_partial_summary,
    prepare_incremental_state,
    run_pipeline,
)
from piranesi.report.renderer import PiranesiReport
from piranesi.scaffold import scaffold_project
from piranesi.trace import TraceBudgetExceededError, TraceWriter
from piranesi.ui import console, print_summary_table, stage_header

_RUN_HELP = """Run the full Piranesi pipeline.

Exit codes:
  0 = no findings (or --no-fail)
  1 = findings at or above --fail-severity
  2 = configuration or required-flag error
  3 = runtime error
  4 = budget exceeded
"""

app = typer.Typer(
    add_completion=False,
    help="CLI-native cybersecurity analysis tool for TypeScript/JavaScript source code.",
    no_args_is_help=True,
)

plugins_app = typer.Typer(
    add_completion=False,
    help="Manage Piranesi plugins.",
    no_args_is_help=True,
)
baseline_app = typer.Typer(
    add_completion=False,
    help="Manage baseline artifacts.",
    no_args_is_help=True,
)
app.add_typer(plugins_app, name="plugins")
app.add_typer(baseline_app, name="baseline")


def _version_callback(value: bool) -> None:
    if not value:
        return
    typer.echo(f"piranesi {__version__}")
    raise typer.Exit()


TargetDirArg = Annotated[Path, typer.Argument(help="Target directory.")]
FindingsFileArg = Annotated[Path, typer.Argument(help="Findings artifact file.")]
ComparisonTargetArg = Annotated[
    Path,
    typer.Argument(help="Baseline artifact, findings artifact, or scan output directory."),
]

IncludeOption = Annotated[
    list[str] | None,
    typer.Option("--include", help="Glob patterns to include."),
]
ExcludeOption = Annotated[
    list[str] | None,
    typer.Option("--exclude", help="Glob patterns to exclude."),
]
SourcesOption = Annotated[Path | None, typer.Option("--sources", help="Custom sources file.")]
SinksOption = Annotated[Path | None, typer.Option("--sinks", help="Custom sinks file.")]
ModelOption = Annotated[str | None, typer.Option("--model", help="Override model.")]
TriageModelOption = Annotated[
    str | None,
    typer.Option("--triage-model", help="Override triage model."),
]
PatchModelOption = Annotated[
    str | None,
    typer.Option("--patch-model", help="Override patch model."),
]
ThresholdOption = Annotated[
    float | None,
    typer.Option("--threshold", help="Confidence threshold."),
]
DockerImageOption = Annotated[
    str | None,
    typer.Option("--docker-image", help="Sandbox Docker image."),
]
TimeoutOption = Annotated[
    int | None,
    typer.Option("--timeout", help="Sandbox timeout in seconds."),
]
NoExecuteOption = Annotated[
    bool,
    typer.Option("--no-execute", help="Generate only, do not execute."),
]
FrameworksOption = Annotated[
    str | None,
    typer.Option("--frameworks", help="Comma-separated frameworks."),
]
JurisdictionOption = Annotated[
    str | None,
    typer.Option("--jurisdiction", help="Jurisdiction override."),
]
ApplyOption = Annotated[bool, typer.Option("--apply", help="Apply the generated patch.")]


class ReportFormat(StrEnum):
    JSON = "json"
    MARKDOWN = "markdown"
    BOTH = "both"
    SARIF = "sarif"
    JUNIT = "junit"
    CSV = "csv"


class SbomFormat(StrEnum):
    SPDX = "spdx"
    CYCLONEDX = "cyclonedx"


class FailSeverity(StrEnum):
    LOW = "low"
    MEDIUM = "medium"
    HIGH = "high"
    CRITICAL = "critical"


FormatOption = Annotated[
    ReportFormat | None,
    typer.Option("--format", help="Report format.", case_sensitive=False),
]
SbomOption = Annotated[
    SbomFormat | None,
    typer.Option("--sbom", help="Generate an SBOM during scan.", case_sensitive=False),
]
TemplateOption = Annotated[
    Path | None,
    typer.Option("--template", help="Custom report template."),
]
ResumeOption = Annotated[
    bool,
    typer.Option("--resume", help="Resume from intermediate artifacts in the output directory."),
]
DryRunOption = Annotated[
    bool,
    typer.Option("--dry-run", help="Show what would be scanned without executing the pipeline."),
]
IncrementalOption = Annotated[
    bool | None,
    typer.Option(
        "--incremental/--no-incremental",
        help="Reuse the current output directory as a baseline and only rescan changed files.",
    ),
]
IncludeTestsOption = Annotated[
    bool,
    typer.Option(
        "--include-tests",
        help="Include test files during hardcoded secret detection.",
    ),
]
NoCacheOption = Annotated[
    bool,
    typer.Option("--no-cache", help="Disable CPG cache reuse and force a full re-scan."),
]
ProfileOption = Annotated[
    bool,
    typer.Option("--profile", help="Print a per-stage timing breakdown to stderr."),
]
BaselineOption = Annotated[
    Path | None,
    typer.Option(
        "--baseline",
        help="Baseline artifact or scan output directory to diff against after the run.",
    ),
]
FailOnNewOption = Annotated[
    bool,
    typer.Option(
        "--fail-on-new",
        help="Exit 1 only when the diff contains NEW findings.",
    ),
]
FailSeverityOption = Annotated[
    FailSeverity,
    typer.Option(
        "--fail-severity",
        help="Exit 1 only when unsuppressed findings at or above this severity exist.",
        case_sensitive=False,
    ),
]
NoFailOption = Annotated[
    bool,
    typer.Option(
        "--no-fail",
        help=(
            "Always exit 0 for findings; configuration and runtime errors still use "
            "non-zero codes."
        ),
    ),
]

ConfigOption = Annotated[
    Path,
    typer.Option("--config", "-c", help="Path to piranesi.toml."),
]
OutputOption = Annotated[
    Path,
    typer.Option("--output", "-o", help="Output directory."),
]
VerboseOption = Annotated[
    bool,
    typer.Option("--verbose", "-v", help="Enable verbose logging."),
]
QuietOption = Annotated[
    bool,
    typer.Option("--quiet", help="Only emit warnings and errors."),
]
DebugOption = Annotated[
    bool,
    typer.Option("--debug", help="Enable developer debug mode."),
]
JsonLogsOption = Annotated[
    bool,
    typer.Option("--json-logs", help="Emit JSONL logs to stderr."),
]
TraceOption = Annotated[
    Path,
    typer.Option("--trace", help="Trace file path."),
]
AuthorizedOption = Annotated[
    bool,
    typer.Option(
        "--authorized",
        help="Confirm authorization to test target code.",
    ),
]
YesOption = Annotated[
    bool,
    typer.Option("--yes", help="Skip authorization prompt."),
]
VersionOption = Annotated[
    bool,
    typer.Option(
        "--version",
        callback=_version_callback,
        expose_value=False,
        is_eager=True,
        help="Show the installed Piranesi version and exit.",
    ),
]


@dataclass(frozen=True)
class CommonOptions:
    config_path: Path
    output_dir: Path
    verbose: bool
    quiet: bool
    debug: bool
    json_logs: bool
    trace_path: Path
    authorized: bool
    assume_yes: bool
    no_cache: bool = False
    profile: bool = False


def _format_override(report_format: ReportFormat | None) -> str | None:
    if report_format is None:
        return None
    return report_format.value


def _sbom_override(sbom_format: SbomFormat | None) -> str | None:
    if sbom_format is None:
        return None
    return sbom_format.value


def _report_output_path(output_dir: Path, report_format: str) -> Path:
    format_name = report_format.lower()
    if format_name == ReportFormat.MARKDOWN.value:
        return output_dir / "report.md"
    if format_name == ReportFormat.SARIF.value:
        return output_dir / "report.sarif.json"
    if format_name == ReportFormat.JUNIT.value:
        return output_dir / "report.junit.xml"
    if format_name == ReportFormat.CSV.value:
        return output_dir / "findings.csv"
    return output_dir / "report.json"


def _run_stubbed_stage(
    stage: str,
    target: Path,
    *,
    options: CommonOptions,
    extra_cli_overrides: dict[str, Any] | None = None,
) -> None:
    setup_logging(
        verbose=options.verbose,
        quiet=options.quiet,
        debug=options.debug,
        json_logs=options.json_logs,
    )
    logger = logging.getLogger(f"piranesi.{stage}")
    cli_overrides: dict[str, Any] = {
        "output.output_dir": str(options.output_dir),
        "trace.file_path": str(options.trace_path),
    }
    if extra_cli_overrides is not None:
        cli_overrides.update(extra_cli_overrides)

    try:
        config = load_config(options.config_path, cli_overrides=cli_overrides)
    except ConfigError as exc:
        log_error_context(
            logger,
            event="config_load_failed",
            what="config_load",
            on_what=str(options.config_path),
            why=str(exc),
            next_step="exiting_with_code_2",
            debug="check TOML syntax and required fields",
        )
        raise typer.Exit(code=2) from exc

    if options.debug:
        config.trace.log_prompts = True

    logger.debug(
        "loaded config scanner=%s output_dir=%s trace_file=%s",
        config.models.scanner,
        config.output.output_dir,
        config.trace.file_path,
        extra={
            "event": "config_loaded",
            "scanner_model": config.models.scanner,
            "output_dir": config.output.output_dir,
            "trace_file": config.trace.file_path,
        },
    )

    trace_writer = TraceWriter(config.trace, config.budget)
    try:
        trace_writer.open()
        _validate_authorization(stage=stage, target=target, options=options, logger=logger)
        if sys.stderr.isatty() and not options.json_logs:
            stage_header(stage)
        logger.info(
            "stage initialized for %s",
            target,
            extra={
                "event": "stage_initialized",
                "stage": stage,
                "target": str(target),
                "trace_file": str(trace_writer.path),
            },
        )
        log_error_context(
            logger,
            event="stage_not_implemented",
            what=f"{stage}_pipeline",
            on_what=str(target),
            why="not implemented",
            next_step="exit_code_3",
            debug=f"trace_file={trace_writer.path}",
            stage=stage,
        )
        typer.echo("not implemented")
        if stage == "run" and sys.stderr.isatty() and not options.json_logs:
            print_summary_table(
                "Piranesi Run Summary",
                {
                    "Stage": stage,
                    "Target": target,
                    "Status": "not implemented",
                    "Trace": trace_writer.path,
                },
            )
        raise typer.Exit(code=3)
    except TraceBudgetExceededError as exc:
        log_error_context(
            logger,
            event="trace_budget_exceeded",
            what="trace_budget",
            on_what=str(trace_writer.path),
            why=str(exc),
            next_step="exiting_with_code_4",
            debug="reduce LLM usage or raise budget.max_cost_usd",
        )
        raise typer.Exit(code=4) from exc
    finally:
        trace_writer.close()


def _load_cli_config(
    *,
    stage: str,
    options: CommonOptions,
    extra_cli_overrides: Mapping[str, Any] | None = None,
) -> PiranesiConfig:
    cli_overrides: dict[str, Any] = {
        "output.output_dir": str(options.output_dir),
        "trace.file_path": str(options.trace_path),
    }
    if extra_cli_overrides is not None:
        cli_overrides.update(extra_cli_overrides)

    try:
        config = load_config(options.config_path, cli_overrides=cli_overrides)
    except ConfigError as exc:
        logger = logging.getLogger(f"piranesi.{stage}")
        log_error_context(
            logger,
            event="config_load_failed",
            what="config_load",
            on_what=str(options.config_path),
            why=str(exc),
            next_step="exiting_with_code_2",
            debug="check TOML syntax and required fields",
        )
        raise typer.Exit(code=2) from exc

    if options.debug:
        config.trace.log_prompts = True
    return config


_STAGE_ARTIFACT_TYPES: dict[str, type] = {
    "scan": ScanResult,
    "detect": DetectArtifact,
    "triage": TriageArtifact,
    "verify": VerifyArtifact,
    "legal": LegalArtifact,
    "patch": PatchArtifact,
    "report": PiranesiReport,
}
_STAGE_PREV: dict[str, str | None] = {  # stage -> which stage's artifact is passed as prev_result
    "scan": None,
    "detect": None,
    "triage": "detect",
    "verify": "triage",
    "legal": "verify",
    "patch": "legal",
    "report": None,
}
_STAGE_CONTEXT_DEPS: dict[str, tuple[str, ...]] = {  # stage -> context.stage_outputs keys needed
    "scan": (),
    "detect": (),
    "triage": (),
    "verify": (),
    "legal": (),
    "patch": ("verify",),
    "report": ("scan", "detect", "verify", "legal", "patch"),
}


def _load_artifact_file(path: Path, artifact_type: type[BaseModel]) -> Any:
    """Load a Pydantic artifact from a JSON file."""
    from pydantic import ValidationError

    try:
        return artifact_type.model_validate_json(path.read_text(encoding="utf-8"))
    except (OSError, ValidationError, json.JSONDecodeError) as exc:
        raise ValueError(f"failed to load {artifact_type.__name__} from {path}: {exc}") from exc


def _run_single_stage(
    stage_name: str,
    target: Path,
    *,
    options: CommonOptions,
    extra_cli_overrides: dict[str, Any] | None = None,
    is_dir_target: bool = False,
) -> None:
    """Run a single pipeline stage, replacing the old stub."""
    setup_logging(
        verbose=options.verbose,
        quiet=options.quiet,
        debug=options.debug,
        json_logs=options.json_logs,
    )
    logger = logging.getLogger(f"piranesi.{stage_name}")
    config_model = _load_cli_config(
        stage=stage_name,
        options=options,
        extra_cli_overrides=extra_cli_overrides,
    )
    cost_tracker = CostTracker()
    trace_writer = TraceWriter(config_model.trace, config_model.budget)
    router = ModelRouter(config_model, cost_tracker)
    trace_logger = TraceLogger(trace_writer, log_prompts=config_model.trace.log_prompts)
    provider = LLMProvider(trace_logger, cost_tracker, router=router)
    target_dir = target.resolve(strict=False) if is_dir_target else Path(".").resolve()
    try:
        trace_writer.open()
        _validate_authorization(stage=stage_name, target=target, options=options, logger=logger)
        if sys.stderr.isatty() and not options.json_logs:
            stage_header(stage_name)
        context = PipelineContext(
            target_dir=target_dir,
            output_dir=options.output_dir,
            provider=provider,
            router=router,
            cost_tracker=cost_tracker,
            trace_writer=trace_writer,
            use_cache=not options.no_cache,
            incremental=(
                prepare_incremental_state(
                    target_dir,
                    options.output_dir,
                    manifest_write_stage="scan" if stage_name == "scan" else "detect",
                )
                if config_model.scan.incremental and is_dir_target
                else None
            ),
        )
        registry = build_default_stage_registry(context)
        stage = registry[stage_name]
        prev_result: StageResult | None = None
        prev_stage_name = _STAGE_PREV.get(stage_name)
        if prev_stage_name is not None and not is_dir_target:
            prev_type = _STAGE_ARTIFACT_TYPES[prev_stage_name]
            artifact = _load_artifact_file(target, prev_type)
            prev_result = StageResult(
                stage=prev_stage_name,
                success=True,
                artifact=artifact,
                elapsed_s=0.0,
                resumed=True,
            )
        for dep in _STAGE_CONTEXT_DEPS.get(stage_name, ()):
            dep_path = options.output_dir / f"{dep}.json"
            if not dep_path.exists():
                log_error_context(
                    logger,
                    event="missing_prerequisite_artifact",
                    what=f"load_{dep}_artifact",
                    on_what=str(dep_path),
                    why=f"prerequisite artifact {dep}.json not found in output directory",
                    next_step="exiting_with_code_2",
                    debug=f"run 'piranesi {dep}' first or 'piranesi run' to generate all artifacts",
                )
                typer.echo(
                    f"error: prerequisite artifact '{dep}.json' not found in {options.output_dir}. "
                    f"Run 'piranesi {dep}' first or use 'piranesi run' for the full pipeline."
                )
                raise typer.Exit(code=1)
            dep_type = _STAGE_ARTIFACT_TYPES[dep]
            context.stage_outputs[dep] = _load_artifact_file(dep_path, dep_type)
        started_at = time.monotonic()
        try:
            result = stage.runner(config_model, prev_result)
        except Exception as exc:
            _ = time.monotonic() - started_at
            log_error_context(
                logger,
                event="stage_failed",
                what=f"{stage_name}_pipeline",
                on_what=str(target),
                why=str(exc),
                next_step="exiting_with_code_3",
                debug=f"trace_file={trace_writer.path}",
                stage=stage_name,
            )
            typer.echo(f"stage '{stage_name}' failed: {exc}")
            raise typer.Exit(code=3) from exc
        options.output_dir.mkdir(parents=True, exist_ok=True)
        artifact_path = options.output_dir / f"{stage_name}.json"
        artifact_path.write_text(
            result.artifact.model_dump_json(indent=2),
            encoding="utf-8",
        )
        public_output_path = (
            _report_output_path(options.output_dir, config_model.output.format)
            if stage_name == "report"
            else artifact_path
        )
        logger.info(
            "stage %s completed in %.2fs, artifact written to %s",
            stage_name,
            result.elapsed_s,
            public_output_path,
            extra={
                "event": "stage_completed",
                "stage": stage_name,
                "elapsed_s": result.elapsed_s,
                "artifact": str(public_output_path),
            },
        )
        if sys.stderr.isatty() and not options.json_logs:
            print_summary_table(
                f"Piranesi {stage_name.title()} Summary",
                {
                    "Status": "completed",
                    "Output": str(public_output_path),
                    "Elapsed": f"{result.elapsed_s:.2f}s",
                    "Trace": str(trace_writer.path),
                },
            )
    except TraceBudgetExceededError as exc:
        log_error_context(
            logger,
            event="trace_budget_exceeded",
            what="trace_budget",
            on_what=str(trace_writer.path),
            why=str(exc),
            next_step="exiting_with_code_4",
            debug="reduce LLM usage or raise budget.max_cost_usd",
        )
        raise typer.Exit(code=4) from exc
    finally:
        trace_writer.close()


def _final_report(results: list[StageResult]) -> PiranesiReport | None:
    for result in reversed(results):
        if result.stage == "report" and isinstance(result.artifact, PiranesiReport):
            return result.artifact
    return None


def _report_exit_code(
    report: PiranesiReport,
    *,
    fail_severity: FailSeverity = FailSeverity.LOW,
    no_fail: bool = False,
) -> int:
    if no_fail:
        return 0
    threshold = _severity_rank(fail_severity.value)
    findings_at_or_above_threshold = sum(
        count
        for severity, count in report.executive_summary.severity_breakdown.items()
        if _severity_rank(severity) >= threshold
    )
    return 1 if findings_at_or_above_threshold > 0 else 0


def _severity_rank(severity: str) -> int:
    normalized = severity.lower()
    if normalized == FailSeverity.LOW.value:
        return 0
    if normalized == FailSeverity.MEDIUM.value:
        return 1
    if normalized == FailSeverity.HIGH.value:
        return 2
    if normalized == FailSeverity.CRITICAL.value:
        return 3
    return -1


def _print_diff(
    baseline_path: Path,
    current_path: Path,
) -> tuple[int, int, int]:
    try:
        baseline_findings = load_findings(baseline_path)
        current_findings = load_findings(current_path)
    except ValueError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=1) from exc

    diff_result = diff_findings(baseline_findings, current_findings)
    typer.echo(f"Piranesi Diff: {baseline_path} -> {current_path}")
    typer.echo(render_diff(diff_result))
    return len(diff_result.new), len(diff_result.fixed), len(diff_result.unchanged)


def _print_profile_breakdown(results: list[StageResult]) -> None:
    results_by_stage = {result.stage: result for result in results}
    lines = [
        f"{'Stage':<10} {'Duration':<10} {'Findings':<10} {'Cache':<6}",
    ]
    for result in results:
        lines.append(
            f"{result.stage:<10} "
            f"{result.elapsed_s:>7.2f}s   "
            f"{_profile_findings_cell(result, results_by_stage):<10} "
            f"{(result.cache_status or '-'):>6}"
        )

    confirmed = _profile_confirmed_findings(results_by_stage)
    total_duration = sum(result.elapsed_s for result in results if result.success)
    lines.append(
        f"{'TOTAL':<10} {total_duration:>7.2f}s   {confirmed} confirmed".ljust(17) + "      -"
    )
    typer.echo("\n".join(lines), err=True)


def _profile_findings_cell(
    result: StageResult,
    results_by_stage: Mapping[str, StageResult],
) -> str:
    if result.stage == "scan":
        return "-"
    if result.stage == "detect" and isinstance(result.artifact, DetectArtifact):
        return str(len(result.artifact.findings))
    if result.stage == "triage" and isinstance(result.artifact, TriageArtifact):
        incoming = 0
        detect_result = results_by_stage.get("detect")
        if detect_result is not None and isinstance(detect_result.artifact, DetectArtifact):
            incoming = sum(
                1 for finding in detect_result.artifact.findings if not finding.suppressed
            )
        retained = sum(
            1 for finding in result.artifact.findings if finding.triage_verdict != "false_positive"
        )
        return _format_transition(incoming, retained)
    if result.stage == "verify" and isinstance(result.artifact, VerifyArtifact):
        incoming = 0
        triage_result = results_by_stage.get("triage")
        if triage_result is not None and isinstance(triage_result.artifact, TriageArtifact):
            incoming = sum(
                1
                for finding in triage_result.artifact.findings
                if finding.triage_verdict != "false_positive"
            )
        return _format_transition(incoming, len(result.artifact.findings))
    if result.stage == "legal" and isinstance(result.artifact, LegalArtifact):
        return str(len(result.artifact.assessments))
    if result.stage == "patch" and isinstance(result.artifact, PatchArtifact):
        return str(len(result.artifact.patches))
    return "-"


def _profile_confirmed_findings(results_by_stage: Mapping[str, StageResult]) -> int:
    report_result = results_by_stage.get("report")
    if report_result is not None and isinstance(report_result.artifact, PiranesiReport):
        return report_result.artifact.executive_summary.findings_confirmed
    verify_result = results_by_stage.get("verify")
    if verify_result is not None and isinstance(verify_result.artifact, VerifyArtifact):
        return len(verify_result.artifact.findings)
    return 0


def _format_transition(incoming: int, outgoing: int) -> str:
    if incoming <= 0:
        return str(outgoing)
    return f"{incoming}->{outgoing}" if incoming != outgoing else str(outgoing)


def _validate_authorization(
    *,
    stage: str,
    target: Path,
    options: CommonOptions,
    logger: logging.Logger,
) -> None:
    if not options.authorized:
        console.print(
            "[WARNING] Piranesi generates real exploits against the target codebase.",
            style="yellow",
        )
        console.print(
            "You must pass --authorized to confirm you have explicit permission to test this code.",
            style="yellow",
        )
        log_error_context(
            logger,
            event="authorization_missing",
            what="authorization_gate",
            on_what=str(target),
            why="--authorized flag not provided",
            next_step="exiting_with_code_2",
            debug=f"stage={stage}",
        )
        raise typer.Exit(code=2)

    if options.assume_yes:
        return

    console.print(
        "[WARNING] Piranesi generates real exploits against the target codebase.",
        style="yellow",
    )
    console.print("You must have explicit authorization to test this code.", style="yellow")
    try:
        response = console.input("Do you confirm you are authorized? [y/N]: ")
    except EOFError as exc:
        log_error_context(
            logger,
            event="authorization_prompt_failed",
            what="authorization_gate",
            on_what=str(target),
            why="interactive confirmation unavailable",
            next_step="exiting_with_code_2",
            debug="rerun with --authorized --yes for non-interactive usage",
        )
        raise typer.Exit(code=2) from exc

    if response.strip().lower() not in {"y", "yes"}:
        log_error_context(
            logger,
            event="authorization_declined",
            what="authorization_gate",
            on_what=str(target),
            why="confirmation not received",
            next_step="exiting_with_code_2",
            debug="rerun with --authorized and confirm the prompt",
        )
        raise typer.Exit(code=2)


def _common_options(
    *,
    config: Path,
    output: Path,
    verbose: bool,
    quiet: bool,
    debug: bool,
    json_logs: bool,
    trace: Path,
    authorized: bool,
    yes: bool,
    no_cache: bool = False,
    profile: bool = False,
) -> CommonOptions:
    return CommonOptions(
        config_path=config,
        output_dir=output,
        verbose=verbose,
        quiet=quiet,
        debug=debug,
        json_logs=json_logs,
        trace_path=trace,
        authorized=authorized,
        assume_yes=yes,
        no_cache=no_cache,
        profile=profile,
    )


def _defaults(
    *,
    config: Path = Path("./piranesi.toml"),
    output: Path = Path("./piranesi-output"),
    trace: Path = Path(".piranesi-trace.jsonl"),
) -> tuple[Path, Path, Path]:
    return config, output, trace


@app.callback()
def main(
    version: VersionOption = False,
) -> None:
    _ = version


@app.command("version")
def version_command() -> None:
    typer.echo(f"piranesi {__version__}")


@app.command(help="Scaffold piranesi.toml and .piranesi-ignore for the current project.")
def init(
    framework: Annotated[
        str | None,
        typer.Option("--framework", help="Framework to scaffold instead of auto-detecting."),
    ] = None,
) -> None:
    try:
        scaffold = scaffold_project(Path("."), requested_framework=framework)
    except ValueError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=2) from exc

    typer.echo(scaffold.detection_message)
    typer.echo(f"Created: {scaffold.config_path.name}")
    typer.echo(f"Created: {scaffold.ignore_path.name}")
    typer.echo("")
    typer.echo("Next steps:")
    typer.echo("  1. Review piranesi.toml and adjust the scan patterns if needed.")
    typer.echo("  2. Run `piranesi run . --authorized --yes` to scan.")


@app.command()
def scan(
    target_dir: TargetDirArg,
    include: IncludeOption = None,
    exclude: ExcludeOption = None,
    sbom: SbomOption = None,
    incremental: IncrementalOption = None,
    no_cache: NoCacheOption = False,
    config: ConfigOption = Path("./piranesi.toml"),
    output: OutputOption = Path("./piranesi-output"),
    verbose: VerboseOption = False,
    quiet: QuietOption = False,
    debug: DebugOption = False,
    json_logs: JsonLogsOption = False,
    trace: TraceOption = Path(".piranesi-trace.jsonl"),
    authorized: AuthorizedOption = False,
    yes: YesOption = False,
) -> None:
    _run_single_stage(
        "scan",
        target_dir,
        options=_common_options(
            config=config,
            output=output,
            verbose=verbose,
            quiet=quiet,
            debug=debug,
            json_logs=json_logs,
            trace=trace,
            authorized=authorized,
            yes=yes,
            no_cache=no_cache,
        ),
        extra_cli_overrides={
            "scan.include_patterns": include,
            "scan.exclude_patterns": exclude,
            "scan.sbom_format": _sbom_override(sbom),
            "scan.incremental": incremental,
        },
        is_dir_target=True,
    )


@app.command()
def detect(
    target_dir: TargetDirArg,
    sources: SourcesOption = None,
    sinks: SinksOption = None,
    include_tests: IncludeTestsOption = False,
    config: ConfigOption = Path("./piranesi.toml"),
    output: OutputOption = Path("./piranesi-output"),
    verbose: VerboseOption = False,
    quiet: QuietOption = False,
    debug: DebugOption = False,
    json_logs: JsonLogsOption = False,
    trace: TraceOption = Path(".piranesi-trace.jsonl"),
    authorized: AuthorizedOption = False,
    yes: YesOption = False,
) -> None:
    _ = (sources, sinks)
    _run_single_stage(
        "detect",
        target_dir,
        options=_common_options(
            config=config,
            output=output,
            verbose=verbose,
            quiet=quiet,
            debug=debug,
            json_logs=json_logs,
            trace=trace,
            authorized=authorized,
            yes=yes,
        ),
        extra_cli_overrides={"scan.include_tests": include_tests},
        is_dir_target=True,
    )


@app.command()
def triage(
    findings_file: FindingsFileArg,
    model: ModelOption = None,
    threshold: ThresholdOption = None,
    config: ConfigOption = Path("./piranesi.toml"),
    output: OutputOption = Path("./piranesi-output"),
    verbose: VerboseOption = False,
    quiet: QuietOption = False,
    debug: DebugOption = False,
    json_logs: JsonLogsOption = False,
    trace: TraceOption = Path(".piranesi-trace.jsonl"),
    authorized: AuthorizedOption = False,
    yes: YesOption = False,
) -> None:
    _ = threshold
    _run_single_stage(
        "triage",
        findings_file,
        options=_common_options(
            config=config,
            output=output,
            verbose=verbose,
            quiet=quiet,
            debug=debug,
            json_logs=json_logs,
            trace=trace,
            authorized=authorized,
            yes=yes,
        ),
        extra_cli_overrides={"models.triage": model},
    )


@app.command()
def verify(
    findings_file: FindingsFileArg,
    docker_image: DockerImageOption = None,
    timeout: TimeoutOption = None,
    no_execute: NoExecuteOption = False,
    config: ConfigOption = Path("./piranesi.toml"),
    output: OutputOption = Path("./piranesi-output"),
    verbose: VerboseOption = False,
    quiet: QuietOption = False,
    debug: DebugOption = False,
    json_logs: JsonLogsOption = False,
    trace: TraceOption = Path(".piranesi-trace.jsonl"),
    authorized: AuthorizedOption = False,
    yes: YesOption = False,
) -> None:
    _ = no_execute
    _run_single_stage(
        "verify",
        findings_file,
        options=_common_options(
            config=config,
            output=output,
            verbose=verbose,
            quiet=quiet,
            debug=debug,
            json_logs=json_logs,
            trace=trace,
            authorized=authorized,
            yes=yes,
        ),
        extra_cli_overrides={
            "sandbox.docker_image": docker_image,
            "sandbox.timeout_seconds": timeout,
        },
    )


@app.command()
def legal(
    findings_file: FindingsFileArg,
    frameworks: FrameworksOption = None,
    jurisdiction: JurisdictionOption = None,
    config: ConfigOption = Path("./piranesi.toml"),
    output: OutputOption = Path("./piranesi-output"),
    verbose: VerboseOption = False,
    quiet: QuietOption = False,
    debug: DebugOption = False,
    json_logs: JsonLogsOption = False,
    trace: TraceOption = Path(".piranesi-trace.jsonl"),
    authorized: AuthorizedOption = False,
    yes: YesOption = False,
) -> None:
    _ = (frameworks, jurisdiction)
    _run_single_stage(
        "legal",
        findings_file,
        options=_common_options(
            config=config,
            output=output,
            verbose=verbose,
            quiet=quiet,
            debug=debug,
            json_logs=json_logs,
            trace=trace,
            authorized=authorized,
            yes=yes,
        ),
    )


@app.command()
def patch(
    findings_file: FindingsFileArg,
    model: ModelOption = None,
    apply: ApplyOption = False,
    config: ConfigOption = Path("./piranesi.toml"),
    output: OutputOption = Path("./piranesi-output"),
    verbose: VerboseOption = False,
    quiet: QuietOption = False,
    debug: DebugOption = False,
    json_logs: JsonLogsOption = False,
    trace: TraceOption = Path(".piranesi-trace.jsonl"),
    authorized: AuthorizedOption = False,
    yes: YesOption = False,
) -> None:
    _ = apply
    _run_single_stage(
        "patch",
        findings_file,
        options=_common_options(
            config=config,
            output=output,
            verbose=verbose,
            quiet=quiet,
            debug=debug,
            json_logs=json_logs,
            trace=trace,
            authorized=authorized,
            yes=yes,
        ),
        extra_cli_overrides={"models.patcher": model},
    )


@app.command()
def report(
    findings_file: FindingsFileArg,
    format: FormatOption = None,
    template: TemplateOption = None,
    config: ConfigOption = Path("./piranesi.toml"),
    output: OutputOption = Path("./piranesi-output"),
    verbose: VerboseOption = False,
    quiet: QuietOption = False,
    debug: DebugOption = False,
    json_logs: JsonLogsOption = False,
    trace: TraceOption = Path(".piranesi-trace.jsonl"),
    authorized: AuthorizedOption = False,
    yes: YesOption = False,
) -> None:
    _ = template
    _run_single_stage(
        "report",
        findings_file,
        options=_common_options(
            config=config,
            output=output,
            verbose=verbose,
            quiet=quiet,
            debug=debug,
            json_logs=json_logs,
            trace=trace,
            authorized=authorized,
            yes=yes,
        ),
        extra_cli_overrides={"output.format": _format_override(format)},
    )


@app.command()
def suppress(
    finding_id: Annotated[str, typer.Argument(help="Finding fingerprint to suppress.")],
    reason: Annotated[str, typer.Option("--reason", help="Suppression rationale.")],
    ticket: Annotated[
        str | None, typer.Option("--ticket", help="Optional ticket reference.")
    ] = None,
    project_root: Annotated[
        Path,
        typer.Option("--project-root", help="Project root containing .piranesi-ignore."),
    ] = Path("."),
) -> None:
    ignore_path = append_ignore_file_suppression(
        project_root,
        finding_id=finding_id,
        reason=reason,
        ticket=ticket,
    )
    typer.echo(f"added suppression for {finding_id} to {ignore_path}")


@app.command("diff")
def diff_command(
    baseline_path: ComparisonTargetArg,
    current_path: ComparisonTargetArg,
    fail_on_new: FailOnNewOption = False,
) -> None:
    new_count, _, _ = _print_diff(baseline_path, current_path)
    if fail_on_new and new_count > 0:
        raise typer.Exit(code=1)


@baseline_app.command("save")
def baseline_save(
    from_results: Annotated[
        Path,
        typer.Option("--from", help="Scan output directory or findings artifact to save."),
    ],
    to: Annotated[
        Path,
        typer.Option("--to", help="Destination baseline JSON file."),
    ],
) -> None:
    try:
        baseline_artifact = build_baseline_artifact(from_results)
    except ValueError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=1) from exc

    to.parent.mkdir(parents=True, exist_ok=True)
    to.write_text(baseline_artifact.model_dump_json(indent=2), encoding="utf-8")
    typer.echo(f"saved baseline with {len(baseline_artifact.findings)} findings to {to}")


@plugins_app.command("list")
def plugins_list(
    config: ConfigOption = Path("./piranesi.toml"),
) -> None:
    from piranesi.plugin import (
        discover_framework_plugins,
        discover_reporter_plugins,
        discover_rule_plugins,
    )

    disabled: frozenset[str] = frozenset()
    config_path = Path(config)
    if config_path.exists():
        try:
            cfg = load_config(config_path)
            disabled = frozenset(cfg.plugins.disabled)
        except ConfigError:
            pass

    fw_plugins = discover_framework_plugins(disabled=frozenset())
    rule_plugins = discover_rule_plugins(disabled=frozenset())
    reporter_plugins = discover_reporter_plugins(disabled=frozenset())

    if not fw_plugins and not rule_plugins and not reporter_plugins:
        typer.echo("no plugins found")
        return

    for fw in fw_plugins:
        status = "disabled" if fw.name() in disabled else "enabled"
        typer.echo(f"framework  {fw.name():<20s} [{status}]")
    for rp in rule_plugins:
        status = "disabled" if rp.name() in disabled else "enabled"
        typer.echo(f"rule       {rp.name():<20s} [{status}]")
    for rep in reporter_plugins:
        status = "disabled" if rep.name() in disabled else "enabled"
        typer.echo(f"reporter   {rep.name():<20s} [{status}]")


@app.command(help=_RUN_HELP)
def run(
    target_dir: TargetDirArg,
    include: IncludeOption = None,
    exclude: ExcludeOption = None,
    sbom: SbomOption = None,
    include_tests: IncludeTestsOption = False,
    baseline: BaselineOption = None,
    fail_on_new: FailOnNewOption = False,
    fail_severity: FailSeverityOption = FailSeverity.LOW,
    no_fail: NoFailOption = False,
    incremental: IncrementalOption = None,
    sources: SourcesOption = None,
    sinks: SinksOption = None,
    triage_model: TriageModelOption = None,
    patch_model: PatchModelOption = None,
    threshold: ThresholdOption = None,
    docker_image: DockerImageOption = None,
    timeout: TimeoutOption = None,
    no_execute: NoExecuteOption = False,
    frameworks: FrameworksOption = None,
    jurisdiction: JurisdictionOption = None,
    apply: ApplyOption = False,
    format: FormatOption = None,
    template: TemplateOption = None,
    resume: ResumeOption = False,
    dry_run: DryRunOption = False,
    no_cache: NoCacheOption = False,
    profile: ProfileOption = False,
    config: ConfigOption = Path("./piranesi.toml"),
    output: OutputOption = Path("./piranesi-output"),
    verbose: VerboseOption = False,
    quiet: QuietOption = False,
    debug: DebugOption = False,
    json_logs: JsonLogsOption = False,
    trace: TraceOption = Path(".piranesi-trace.jsonl"),
    authorized: AuthorizedOption = False,
    yes: YesOption = False,
) -> None:
    _ = (sources, sinks, threshold, frameworks, jurisdiction, template)
    options = _common_options(
        config=config,
        output=output,
        verbose=verbose,
        quiet=quiet,
        debug=debug,
        json_logs=json_logs,
        trace=trace,
        authorized=authorized,
        yes=yes,
        no_cache=no_cache,
        profile=profile,
    )
    setup_logging(
        verbose=options.verbose,
        quiet=options.quiet,
        debug=options.debug,
        json_logs=options.json_logs,
    )
    logger = logging.getLogger("piranesi.run")
    config_model = _load_cli_config(
        stage="run",
        options=options,
        extra_cli_overrides={
            "scan.include_patterns": include,
            "scan.exclude_patterns": exclude,
            "scan.sbom_format": _sbom_override(sbom),
            "scan.include_tests": include_tests,
            "scan.incremental": incremental,
            "models.triage": triage_model,
            "models.patcher": patch_model,
            "sandbox.docker_image": docker_image,
            "sandbox.timeout_seconds": timeout,
            "output.format": _format_override(format),
        },
    )

    if dry_run:
        if sys.stderr.isatty() and not json_logs:
            stage_header("dry-run")
        scan_targets = discover_scan_targets(target_dir, config_model)
        for path in scan_targets:
            typer.echo(str(path))
        if sys.stderr.isatty() and not json_logs:
            print_summary_table(
                "Piranesi Dry Run",
                {
                    "Target": target_dir.resolve(strict=False),
                    "Files": len(scan_targets),
                    "Stages": "scan -> detect -> triage -> verify -> legal -> patch -> report",
                    "Output": options.output_dir,
                },
            )
        return

    _validate_authorization(stage="run", target=target_dir, options=options, logger=logger)

    partial_summary = load_partial_summary(options.output_dir) if resume else None
    cost_tracker = CostTracker()
    trace_writer = TraceWriter(config_model.trace, config_model.budget)
    router = ModelRouter(config_model, cost_tracker)
    trace_logger = TraceLogger(trace_writer, log_prompts=config_model.trace.log_prompts)
    provider = LLMProvider(trace_logger, cost_tracker, router=router)

    try:
        trace_writer.open()
        context = PipelineContext(
            target_dir=target_dir.resolve(strict=False),
            output_dir=options.output_dir,
            provider=provider,
            router=router,
            cost_tracker=cost_tracker,
            trace_writer=trace_writer,
            stage_timings_s={}
            if partial_summary is None
            else dict(partial_summary.stage_timings_s),
            resumed_cost_usd=0.0 if partial_summary is None else partial_summary.total_llm_cost_usd,
            apply_patches=apply,
            no_execute=no_execute,
            use_cache=not options.no_cache,
            incremental=(
                prepare_incremental_state(
                    target_dir.resolve(strict=False),
                    options.output_dir,
                    manifest_write_stage="detect",
                )
                if config_model.scan.incremental
                else None
            ),
        )
        pipeline_result = run_pipeline(
            config_model,
            context,
            stage_registry=build_default_stage_registry(context),
            resume=resume,
            render_ui=sys.stderr.isatty() and not json_logs,
        )
    except TraceBudgetExceededError as exc:
        log_error_context(
            logger,
            event="trace_budget_exceeded",
            what="trace_budget",
            on_what=str(trace_writer.path),
            why=str(exc),
            next_step="exiting_with_code_4",
            debug="reduce LLM usage or raise budget.max_cost_usd",
        )
        raise typer.Exit(code=4) from exc
    finally:
        trace_writer.close()

    if pipeline_result.failed_stage is not None:
        if options.profile:
            _print_profile_breakdown(pipeline_result.results)
        failed_result = pipeline_result.failed_result
        typer.echo(
            f"pipeline failed at stage '{pipeline_result.failed_stage}': "
            f"{failed_result.error if failed_result is not None else 'unknown error'}"
        )
        typer.echo(
            f"partial results were saved to {options.output_dir}. "
            "Rerun with `--resume` to continue from the last successful stage."
        )
        if sys.stderr.isatty() and not json_logs:
            print_summary_table(
                "Piranesi Run Summary",
                {
                    "Status": "failed",
                    "Failed stage": pipeline_result.failed_stage,
                    "Output": options.output_dir,
                    "Trace": trace_writer.path,
                },
            )
        raise typer.Exit(code=3)

    report = _final_report(pipeline_result.results)
    if options.profile:
        _print_profile_breakdown(pipeline_result.results)
    report_path = _report_output_path(options.output_dir, config_model.output.format)
    findings_detected = 0 if report is None else report.executive_summary.findings_detected
    findings_suppressed = 0 if report is None else report.executive_summary.suppressed_findings
    findings_confirmed = 0 if report is None else report.executive_summary.findings_confirmed
    if sys.stderr.isatty() and not json_logs:
        print_summary_table(
            "Piranesi Run Summary",
            {
                "Status": (
                    "completed"
                    if findings_detected - findings_suppressed == 0
                    else "findings_detected"
                ),
                "Stages": " -> ".join(result.stage for result in pipeline_result.results),
                "Findings detected": findings_detected,
                "Findings suppressed": findings_suppressed,
                "Findings confirmed": findings_confirmed,
                "Output": options.output_dir,
                "Report": report_path,
                "Trace": trace_writer.path,
            },
        )
    if baseline is not None:
        new_count, _, _ = _print_diff(baseline, options.output_dir)
        if fail_on_new and not no_fail:
            if new_count > 0:
                raise typer.Exit(code=1)
            return
    if (
        report is not None
        and _report_exit_code(
            report,
            fail_severity=fail_severity,
            no_fail=no_fail,
        )
        != 0
    ):
        if findings_suppressed:
            typer.echo(
                "findings detected: "
                f"{report.executive_summary.findings_detected} "
                f"({findings_suppressed} suppressed, "
                f"confirmed: {report.executive_summary.findings_confirmed})"
            )
        else:
            typer.echo(
                "findings detected: "
                f"{report.executive_summary.findings_detected} "
                f"(confirmed: {report.executive_summary.findings_confirmed})"
            )
        raise typer.Exit(code=1)
