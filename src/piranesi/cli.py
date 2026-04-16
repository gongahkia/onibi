from __future__ import annotations

import json
import logging
import signal
import sys
import time
from collections.abc import Mapping
from contextlib import contextmanager
from dataclasses import dataclass
from datetime import date
from enum import StrEnum
from pathlib import Path
from typing import Annotated, Any, cast

import typer
from pydantic import BaseModel, ValidationError

from piranesi import __version__
from piranesi.config import ConfigError, PiranesiConfig, load_config
from piranesi.detect import append_ignore_file_suppression
from piranesi.diff import build_baseline_artifact, diff_findings, load_findings, render_diff
from piranesi.doctor import build_doctor_report, render_doctor_report
from piranesi.hooks.pre_commit import (
    HookError,
    discover_staged_files,
    install_pre_commit_hook,
    pre_commit_hook_status,
    uninstall_pre_commit_hook,
)
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
from piranesi.report import launch_compliance_tui, print_compliance_report, render_attestation
from piranesi.report.renderer import (
    CandidateReportFinding,
    CombinedFinding,
    PiranesiReport,
    SuppressedFinding,
)
from piranesi.report.trends import build_trend_report, render_terminal_trends, write_trend_report
from piranesi.report.tui import display_report
from piranesi.scaffold import scaffold_project
from piranesi.scan.monorepo import detect_monorepo_manifest, select_packages
from piranesi.threat import build_threat_model
from piranesi.trace import TraceBudgetExceededError, TraceWriter
from piranesi.ui import console, print_summary_table, stage_header
from piranesi.watch import WatchDependencyError, WatchModeError, run_watch_mode

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
rules_app = typer.Typer(
    add_completion=False,
    help="Manage custom rules and rule repositories.",
    no_args_is_help=True,
)
baseline_app = typer.Typer(
    add_completion=False,
    help="Manage baseline artifacts.",
    no_args_is_help=True,
)
compliance_app = typer.Typer(
    add_completion=False,
    help="Generate compliance-focused artifacts.",
    no_args_is_help=True,
)
hook_app = typer.Typer(
    add_completion=False,
    help="Manage git hook integration.",
    no_args_is_help=True,
)
app.add_typer(plugins_app, name="plugins")
app.add_typer(rules_app, name="rules")
app.add_typer(baseline_app, name="baseline")
app.add_typer(compliance_app, name="compliance")
app.add_typer(hook_app, name="hook")


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
RulesPathArg = Annotated[Path, typer.Argument(help="Rule file or directory.")]

IncludeOption = Annotated[
    list[str] | None,
    typer.Option("--include", help="Glob patterns to include."),
]
ExcludeOption = Annotated[
    list[str] | None,
    typer.Option("--exclude", help="Glob patterns to exclude."),
]
ModelOption = Annotated[str | None, typer.Option("--model", help="Override model.")]
TriageModelOption = Annotated[
    str | None,
    typer.Option("--triage-model", help="Override triage model."),
]
PatchModelOption = Annotated[
    str | None,
    typer.Option("--patch-model", help="Override patch model."),
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
ApplyOption = Annotated[bool, typer.Option("--apply", help="Apply the generated patch.")]
FixtureDirOption = Annotated[
    Path,
    typer.Option("--fixture", help="Fixture directory."),
]


class ReportFormat(StrEnum):
    JSON = "json"
    MARKDOWN = "markdown"
    BOTH = "both"
    SARIF = "sarif"
    JUNIT = "junit"
    CSV = "csv"
    TUI = "tui"
    COMPLIANCE = "compliance"


class TrendFormat(StrEnum):
    JSON = "json"
    TERMINAL = "terminal"


class ComplianceFormat(StrEnum):
    JSON = "json"
    TERMINAL = "terminal"


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
AttestationOption = Annotated[
    bool,
    typer.Option(
        "--attestation",
        help="Emit a pre-filled Markdown compliance attestation to stdout.",
    ),
]
ComplianceTuiOption = Annotated[
    bool,
    typer.Option(
        "--tui",
        help="Launch the interactive compliance dashboard (requires piranesi[tui]).",
    ),
]
TrendFormatOption = Annotated[
    TrendFormat,
    typer.Option("--format", help="Trend output format.", case_sensitive=False),
]
ComplianceFormatOption = Annotated[
    ComplianceFormat,
    typer.Option("--format", help="Compliance output format.", case_sensitive=False),
]
SbomOption = Annotated[
    SbomFormat | None,
    typer.Option("--sbom", help="Generate an SBOM during scan.", case_sensitive=False),
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
IncludeUnreachableOption = Annotated[
    bool,
    typer.Option(
        "--include-unreachable",
        help="Include unreachable findings in the main report instead of appendix-only.",
    ),
]
DeadCodeReportOption = Annotated[
    bool,
    typer.Option(
        "--dead-code-report",
        help="Include a dead code report listing functions unreachable from entry points.",
    ),
]
NoCacheOption = Annotated[
    bool,
    typer.Option("--no-cache", help="Disable CPG cache reuse and force a full re-scan."),
]
PackageOption = Annotated[
    str | None,
    typer.Option("--package", help="Scan a single package inside a detected monorepo."),
]
ChangedPackagesOption = Annotated[
    bool,
    typer.Option(
        "--changed-packages",
        help="In a monorepo, scan only git-changed packages plus affected dependents.",
    ),
]
ProfileOption = Annotated[
    bool,
    typer.Option("--profile", help="Print a per-stage timing breakdown to stderr."),
]
MaxParallelOption = Annotated[
    int | None,
    typer.Option(
        "--max-parallel",
        min=1,
        help="Maximum number of workspace packages to analyze in parallel. Defaults to CPU count.",
    ),
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
            "Always exit 0 for findings; configuration and runtime errors still use non-zero codes."
        ),
    ),
]
StagedOnlyOption = Annotated[
    bool,
    typer.Option("--staged-only", help="Only scan files currently staged in git."),
]
HookTimeoutOption = Annotated[
    int | None,
    typer.Option(
        "--hook-timeout",
        min=1,
        help="Skip the run and exit 0 if a staged-only scan exceeds this many seconds.",
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
WatchFilterOption = Annotated[
    str | None,
    typer.Option("--filter", help="Only watch files matching this glob."),
]
DebounceOption = Annotated[
    int,
    typer.Option("--debounce", min=0, help="Debounce time in milliseconds."),
]
OnFindingOption = Annotated[
    str | None,
    typer.Option("--on-finding", help="Shell command to execute when new findings appear."),
]
MaxScansOption = Annotated[
    int | None,
    typer.Option("--max-scans", min=1, help="Exit after N completed scans."),
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
    max_parallel: int | None = None
    package_name: str | None = None
    changed_packages_only: bool = False


class HookTimeoutExceededError(RuntimeError):
    """Raised when a staged-only hook run exceeds its time budget."""


def _format_override(report_format: ReportFormat | None) -> str | None:
    if report_format is None:
        return None
    return report_format.value


@contextmanager
def _hook_timeout(seconds: int | None) -> Any:
    if seconds is None or not hasattr(signal, "SIGALRM") or not hasattr(signal, "setitimer"):
        yield
        return

    previous_handler = signal.getsignal(signal.SIGALRM)

    def _handle_timeout(_signum: int, _frame: Any) -> None:
        raise HookTimeoutExceededError()

    signal.signal(signal.SIGALRM, _handle_timeout)
    signal.setitimer(signal.ITIMER_REAL, float(seconds))
    try:
        yield
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0.0)
        signal.signal(signal.SIGALRM, previous_handler)


def _sbom_override(sbom_format: SbomFormat | None) -> str | None:
    if sbom_format is None:
        return None
    return sbom_format.value


def _parse_date_option(value: str | None, *, option_name: str) -> date | None:
    if value is None:
        return None
    try:
        return date.fromisoformat(value)
    except ValueError as exc:
        raise typer.BadParameter(
            f"{option_name} must use YYYY-MM-DD format",
            param_hint=option_name,
        ) from exc


def _report_output_path(output_dir: Path, report_format: str) -> Path:
    format_name = report_format.lower()
    if format_name == ReportFormat.MARKDOWN.value:
        return output_dir / "report.md"
    if format_name == ReportFormat.TUI.value:
        return output_dir / "report.md"
    if format_name == ReportFormat.SARIF.value:
        return output_dir / "report.sarif.json"
    if format_name == ReportFormat.JUNIT.value:
        return output_dir / "report.junit.xml"
    if format_name == ReportFormat.CSV.value:
        return output_dir / "findings.csv"
    return output_dir / "report.json"


def _validate_compliance_flags(
    report_format: str,
    *,
    attestation: bool,
    tui: bool,
) -> None:
    if attestation and tui:
        typer.echo("error: --attestation and --tui cannot be used together")
        raise typer.Exit(code=2)
    if not attestation and not tui:
        return
    if report_format.lower() == ReportFormat.COMPLIANCE.value:
        return
    typer.echo("error: --attestation and --tui require --format compliance")
    raise typer.Exit(code=2)


def _emit_compliance_output(
    report: PiranesiReport,
    *,
    attestation: bool,
    tui: bool,
) -> None:
    if attestation:
        typer.echo(render_attestation(report), nl=False)
        return
    if tui:
        launch_compliance_tui(report)
        return
    print_compliance_report(report)


def _load_report_from_artifacts_dir(artifacts_dir: Path) -> PiranesiReport:
    report_path = artifacts_dir / "report.json"
    if not report_path.exists():
        raise ValueError(f"report artifact not found: {report_path}")
    return cast(PiranesiReport, _load_artifact_file(report_path, PiranesiReport))


ReportFindingMatch = CombinedFinding | CandidateReportFinding | SuppressedFinding


def _find_report_finding(
    report: PiranesiReport,
    finding_id: str,
) -> tuple[str, ReportFindingMatch] | None:
    for finding in report.findings:
        if finding.finding_id == finding_id:
            return "confirmed", finding
    for finding in report.active_findings:
        if finding.finding_id == finding_id:
            return "active_candidate", finding
    for finding in report.unreachable_findings:
        if finding.finding_id == finding_id:
            return "unreachable_candidate", finding
    for finding in report.suppressed_findings:
        if finding.finding_id == finding_id:
            return "suppressed", finding
    return None


def _render_finding_explanation(status: str, finding: ReportFindingMatch) -> str:
    lines = [
        "# Piranesi Finding Explanation",
        "",
        f"ID: {finding.finding_id}",
        f"Status: {status.replace('_', ' ')}",
        f"Title: {finding.title}",
        f"CWE: {finding.cwe}",
        f"Severity: {finding.severity.upper()}",
        f"Confidence: {finding.confidence:.2f}",
        (
            f"Source: {finding.source_location.file}:{finding.source_location.line} "
            f"({finding.taint_source})"
        ),
        (f"Sink: {finding.sink_location.file}:{finding.sink_location.line} ({finding.taint_sink})"),
    ]
    if isinstance(finding, CandidateReportFinding):
        lines.extend(
            [
                f"Reachability: {finding.reachability}",
                f"Source function: {finding.source_function_id or 'n/a'}",
            ]
        )
    if isinstance(finding, CombinedFinding | CandidateReportFinding) and finding.cluster_id:
        representative = "yes" if finding.cluster_representative else "no"
        lines.append(
            f"Cluster: {finding.cluster_id} "
            f"(size={finding.cluster_size}, representative={representative})"
        )
    if isinstance(finding, SuppressedFinding):
        lines.append(f"Suppression reason: {finding.suppression_reason or 'n/a'}")
    if isinstance(finding, CombinedFinding):
        lines.extend(
            [
                f"Verified: {'yes' if finding.verified else 'no'} ({finding.verification_method})",
                f"Exploit payload: {finding.exploit_payload or 'n/a'}",
                f"Patch: {_patch_status(finding)}",
                f"Legal risk tier: {finding.legal_risk_tier or 'n/a'}",
            ]
        )
        if finding.taint_path:
            lines.extend(["", "Taint path:"])
            for step in finding.taint_path:
                sanitizer = f" sanitizer={step.sanitizer_applied}" if step.sanitizer_applied else ""
                lines.append(
                    f"- {step.location.file}:{step.location.line} "
                    f"{step.operation} state={step.taint_state}{sanitizer}"
                )
        if finding.regulatory_obligations:
            lines.extend(["", "Regulatory obligations:"])
            for obligation in finding.regulatory_obligations:
                deadline = (
                    f", deadline={obligation.notification_timeline}"
                    if obligation.notification_timeline
                    else ""
                )
                lines.append(
                    f"- {obligation.framework} {obligation.section}: "
                    f"{obligation.obligation_text}{deadline}"
                )
        if finding.reproducer_script:
            lines.extend(["", "Reproducer:", finding.reproducer_script])
    return "\n".join(lines).rstrip() + "\n"


def _patch_status(finding: CombinedFinding) -> str:
    if finding.patch_diff is None:
        return "not generated"
    if finding.patch_verified is True:
        return "generated and verified"
    if finding.patch_verified is False:
        return "generated, not verified"
    return "generated"


def _resolve_framework_keys(value: str | None) -> list[str] | None:
    if value is None:
        return None

    from piranesi.legal.frameworks import resolve_framework_key

    resolved: list[str] = []
    invalid: list[str] = []
    for raw_item in value.split(","):
        candidate = raw_item.strip()
        if not candidate:
            continue
        framework_key = resolve_framework_key(candidate)
        if framework_key is None:
            invalid.append(candidate)
            continue
        if framework_key not in resolved:
            resolved.append(framework_key)

    if invalid:
        raise ValueError(f"unsupported framework key(s): {', '.join(invalid)}")
    return resolved or None


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

    logger = logging.getLogger(f"piranesi.{stage}")
    if not options.config_path.exists():
        default_config_path = Path("./piranesi.toml").resolve(strict=False)
        requested_config_path = options.config_path.resolve(strict=False)
        if requested_config_path != default_config_path:
            log_error_context(
                logger,
                event="config_missing",
                what="config_path",
                on_what=str(options.config_path),
                why="configured file does not exist",
                next_step="exiting_with_code_2",
                debug="use an existing --config path or run without --config to use defaults",
            )
            raise typer.Exit(code=2)
        logger.warning(
            "config file %s not found — using defaults. "
            "run `piranesi init` to generate a configuration file.",
            options.config_path,
        )
        from piranesi.config import _apply_cli_overrides

        data = _apply_cli_overrides({}, cli_overrides)
        try:
            config = PiranesiConfig.model_validate(data)
        except ValidationError:
            config = PiranesiConfig()
    else:
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
) -> StageResult:
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
    monorepo_manifest = (
        detect_monorepo_manifest(target_dir, config_model.scan.frameworks)
        if is_dir_target
        else None
    )
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
            monorepo_manifest=monorepo_manifest,
            monorepo_package_name=options.package_name,
            changed_packages_only=options.changed_packages_only,
            max_parallel=options.max_parallel,
            render_ui=sys.stderr.isatty() and not options.json_logs,
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
        if (
            stage_name == "report"
            and isinstance(result.artifact, PiranesiReport)
            and config_model.output.format == ReportFormat.TUI.value
        ):
            display_report(result.artifact, output_dir=options.output_dir)
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
        return result
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


def _generate_threat_model_for_run(
    report: PiranesiReport,
    output_dir: Path,
    logger: logging.Logger,
) -> None:
    detect_path = output_dir / "detect.json"
    if not detect_path.exists():
        logger.debug("skipping threat model — detect.json not found in %s", output_dir)
        return
    try:
        detect_artifact = DetectArtifact.model_validate_json(
            detect_path.read_text(encoding="utf-8")
        )
        findings = list(detect_artifact.findings)
        if not findings:
            logger.info("no findings for threat model generation")
            return
        verify_path = output_dir / "verify.json"
        verification_results: dict[str, object] = {}
        if verify_path.exists():
            verify_artifact = VerifyArtifact.model_validate_json(
                verify_path.read_text(encoding="utf-8")
            )
            verification_results = {c.finding.finding.id: c for c in verify_artifact.findings}
        scan_path = output_dir / "scan.json"
        scan_result = None
        if scan_path.exists():
            scan_result = ScanResult.model_validate_json(scan_path.read_text(encoding="utf-8"))
        entry_points = scan_result.entry_points if scan_result else None
        attack_surface = scan_result.attack_surface if scan_result else None
        functions = scan_result.functions if scan_result else None
        model = build_threat_model(
            findings,
            entry_points=entry_points,
            attack_surface=attack_surface,
            functions=functions,
            verification_results=verification_results,
        )
        import json as _json

        threat_out = output_dir / "threat_model.json"
        from dataclasses import asdict

        threat_out.write_text(
            _json.dumps(asdict(model), indent=2, default=str),
            encoding="utf-8",
        )
        logger.info("threat model written to %s", threat_out)
    except Exception:
        logger.warning("threat model generation failed", exc_info=True)


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
                1
                for finding in detect_result.artifact.findings
                if not finding.suppressed and finding.reachability == "reachable"
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
    max_parallel: int | None = None,
    package_name: str | None = None,
    changed_packages_only: bool = False,
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
        max_parallel=max_parallel,
        package_name=package_name,
        changed_packages_only=changed_packages_only,
    )


def _defaults(
    *,
    config: Path = Path("./piranesi.toml"),
    output: Path = Path("./piranesi-output"),
    trace: Path = Path(".piranesi-trace.jsonl"),
) -> tuple[Path, Path, Path]:
    return config, output, trace


def _load_rules_cli_config(config_path: Path) -> PiranesiConfig:
    if not config_path.exists():
        return PiranesiConfig()
    try:
        return load_config(config_path)
    except ConfigError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=2) from exc


def _load_hook_cli_config(config_path: Path) -> PiranesiConfig:
    if not config_path.exists():
        return PiranesiConfig()
    try:
        return load_config(config_path)
    except ConfigError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=2) from exc


@app.callback()
def main(
    version: VersionOption = False,
) -> None:
    _ = version


@app.command("version")
def version_command() -> None:
    typer.echo(f"piranesi {__version__}")


@app.command(help="Diagnose local readiness and explain what Piranesi can run automatically.")
def doctor(
    target_dir: Annotated[
        Path,
        typer.Argument(help="Target directory to inspect."),
    ] = Path("."),
    config: ConfigOption = Path("./piranesi.toml"),
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Emit machine-readable JSON."),
    ] = False,
) -> None:
    report = build_doctor_report(target_dir, config_path=config)
    if json_output:
        typer.echo(report.model_dump_json(indent=2))
        return
    typer.echo(render_doctor_report(report), nl=False)
    if not report.ready:
        raise typer.Exit(code=1)


@hook_app.command("install")
def hook_install(
    config: ConfigOption = Path("./piranesi.toml"),
) -> None:
    config_model = _load_hook_cli_config(config)
    try:
        hook_path = install_pre_commit_hook(
            Path.cwd(),
            fail_severity=config_model.hooks.fail_severity,
            hook_timeout=config_model.hooks.timeout,
        )
    except HookError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=1) from exc

    typer.echo(f"installed pre-commit hook: {hook_path}")


@hook_app.command("uninstall")
def hook_uninstall() -> None:
    try:
        removed = uninstall_pre_commit_hook(Path.cwd())
    except HookError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=1) from exc

    if removed:
        typer.echo("removed pre-commit hook")
        return
    typer.echo("pre-commit hook not installed")


@hook_app.command("status")
def hook_status() -> None:
    try:
        installed, hook_path = pre_commit_hook_status(Path.cwd())
    except HookError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=1) from exc

    typer.echo(f"pre-commit hook: {'installed' if installed else 'not installed'}")
    typer.echo(f"path: {hook_path}")


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
    for index, step in enumerate(scaffold.next_steps, start=1):
        typer.echo(f"  {index}. {step}")


@app.command()
def scan(
    target_dir: TargetDirArg,
    include: IncludeOption = None,
    exclude: ExcludeOption = None,
    sbom: SbomOption = None,
    incremental: IncrementalOption = None,
    package_name: PackageOption = None,
    changed_packages: ChangedPackagesOption = False,
    max_parallel: MaxParallelOption = None,
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
            max_parallel=max_parallel,
            package_name=package_name,
            changed_packages_only=changed_packages,
        ),
        extra_cli_overrides={
            "scan.include_patterns": include,
            "scan.exclude_patterns": exclude,
            "scan.sbom_format": _sbom_override(sbom),
            "scan.incremental": incremental,
        },
        is_dir_target=True,
    )


@app.command(help="Watch a directory and run incremental scans on file changes.")
def watch(
    target_dir: TargetDirArg,
    filter_pattern: WatchFilterOption = None,
    debounce: DebounceOption = 500,
    on_finding: OnFindingOption = None,
    fail_severity: FailSeverityOption = FailSeverity.LOW,
    max_scans: MaxScansOption = None,
    max_parallel: MaxParallelOption = None,
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
        max_parallel=max_parallel,
    )
    setup_logging(
        verbose=options.verbose,
        quiet=options.quiet,
        debug=options.debug,
        json_logs=options.json_logs,
    )
    logger = logging.getLogger("piranesi.watch")
    config_model = _load_cli_config(
        stage="watch",
        options=options,
        extra_cli_overrides={"scan.incremental": True},
    )

    try:
        _validate_authorization(stage="watch", target=target_dir, options=options, logger=logger)
        summary = run_watch_mode(
            target_dir,
            config=config_model,
            output_dir=options.output_dir,
            debounce_ms=debounce,
            filter_glob=filter_pattern,
            on_finding=on_finding,
            fail_severity=fail_severity.value,
            max_scans=max_scans,
            use_cache=not options.no_cache,
            max_parallel=options.max_parallel,
            render_ui=sys.stderr.isatty() and not json_logs,
        )
    except WatchDependencyError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=2) from exc
    except WatchModeError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=2) from exc
    except TraceBudgetExceededError as exc:
        log_error_context(
            logger,
            event="trace_budget_exceeded",
            what="trace_budget",
            on_what=str(options.trace_path),
            why=str(exc),
            next_step="exiting_with_code_4",
            debug="reduce LLM usage or raise budget.max_cost_usd",
        )
        raise typer.Exit(code=4) from exc
    except Exception as exc:
        log_error_context(
            logger,
            event="watch_mode_failed",
            what="watch_mode",
            on_what=str(target_dir),
            why=str(exc),
            next_step="exiting_with_code_3",
            debug=f"output_dir={options.output_dir}",
        )
        typer.echo(f"watch mode failed: {exc}")
        raise typer.Exit(code=3) from exc

    if summary.exit_code != 0:
        raise typer.Exit(code=summary.exit_code)


@app.command(help="Start the Piranesi LSP server.")
def lsp(
    tcp: Annotated[
        bool,
        typer.Option("--tcp", help="Serve LSP over TCP instead of stdio."),
    ] = False,
    host: Annotated[
        str,
        typer.Option("--host", help="Host to bind when --tcp is enabled."),
    ] = "127.0.0.1",
    port: Annotated[
        int,
        typer.Option("--port", help="Port to bind when --tcp is enabled."),
    ] = 9257,
    log: Annotated[
        Path | None,
        typer.Option("--log", help="Write LSP logs to a file."),
    ] = None,
    config: ConfigOption = Path("./piranesi.toml"),
) -> None:
    if log is not None:
        log.parent.mkdir(parents=True, exist_ok=True)
        logging.basicConfig(
            filename=log,
            level=logging.DEBUG,
            format="%(asctime)s %(levelname)s %(name)s: %(message)s",
        )

    try:
        config_model = load_config(config)
    except ConfigError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=2) from exc

    if not config_model.lsp.enabled:
        typer.echo(f"error: LSP support is disabled in {config}")
        raise typer.Exit(code=2)

    try:
        from piranesi.lsp.server import serve
    except ImportError as exc:
        typer.echo("error: pygls is not installed. Install the optional extra: `piranesi[lsp]`.")
        raise typer.Exit(code=2) from exc

    try:
        serve(
            config_path=config.resolve(strict=False),
            tcp=tcp,
            host=host,
            port=port,
        )
    except Exception as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=3) from exc


@app.command()
def detect(
    target_dir: TargetDirArg,
    include_tests: IncludeTestsOption = False,
    package_name: PackageOption = None,
    changed_packages: ChangedPackagesOption = False,
    max_parallel: MaxParallelOption = None,
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
            max_parallel=max_parallel,
            package_name=package_name,
            changed_packages_only=changed_packages,
        ),
        extra_cli_overrides={"scan.include_tests": include_tests},
        is_dir_target=True,
    )


@app.command()
def triage(
    findings_file: FindingsFileArg,
    model: ModelOption = None,
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
    attestation: AttestationOption = False,
    tui: ComplianceTuiOption = False,
    include_unreachable: IncludeUnreachableOption = False,
    dead_code_report: DeadCodeReportOption = False,
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
    )
    extra_cli_overrides = {
        "output.format": _format_override(format),
        "reachability.include_unreachable": include_unreachable,
        "reachability.dead_code_report": dead_code_report,
    }
    config_model = _load_cli_config(
        stage="report",
        options=options,
        extra_cli_overrides=extra_cli_overrides,
    )
    _validate_compliance_flags(
        config_model.output.format,
        attestation=attestation,
        tui=tui,
    )
    result = _run_single_stage(
        "report",
        findings_file,
        options=options,
        extra_cli_overrides=extra_cli_overrides,
    )
    if config_model.output.format == ReportFormat.COMPLIANCE.value and isinstance(
        result.artifact, PiranesiReport
    ):
        _emit_compliance_output(result.artifact, attestation=attestation, tui=tui)


@app.command(help="Explain a single finding from a generated report.json artifact.")
def explain(
    finding_id: Annotated[str, typer.Argument(help="Finding id to explain.")],
    output: Annotated[
        Path,
        typer.Option("--output", "-o", help="Piranesi output directory containing report.json."),
    ] = Path("./piranesi-output"),
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Emit machine-readable JSON."),
    ] = False,
) -> None:
    try:
        report_model = _load_report_from_artifacts_dir(output)
    except ValueError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=2) from exc

    match = _find_report_finding(report_model, finding_id)
    if match is None:
        typer.echo(f"error: finding '{finding_id}' not found in {output / 'report.json'}")
        raise typer.Exit(code=1)

    status, finding = match
    if json_output:
        typer.echo(
            json.dumps(
                {"status": status, "finding": finding.model_dump(mode="json")},
                indent=2,
            )
        )
        return
    typer.echo(_render_finding_explanation(status, finding), nl=False)


@app.command(help="Compute historical finding trends from saved baseline artifacts.")
def trends(
    output_dir: Annotated[Path, typer.Argument(help="Directory containing baseline artifacts.")],
    since: Annotated[
        str | None,
        typer.Option("--since", help="Only include scans on or after YYYY-MM-DD."),
    ] = None,
    until: Annotated[
        str | None,
        typer.Option("--until", help="Only include scans on or before YYYY-MM-DD."),
    ] = None,
    format: TrendFormatOption = TrendFormat.TERMINAL,
) -> None:
    try:
        trend_report = build_trend_report(
            output_dir,
            since=_parse_date_option(since, option_name="--since"),
            until=_parse_date_option(until, option_name="--until"),
        )
    except ValueError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=1) from exc

    write_trend_report(trend_report, output_dir / "trends.json")
    if format == TrendFormat.JSON:
        typer.echo(trend_report.model_dump_json(indent=2))
        return
    render_terminal_trends(trend_report)


@compliance_app.command("maturity")
def compliance_maturity(
    artifacts_dir: Annotated[
        Path,
        typer.Argument(help="Directory containing report.json."),
    ],
    framework: Annotated[
        str | None,
        typer.Option(
            "--framework",
            help=(
                "Optional framework key or comma-separated framework keys "
                "(for example: iso27001,pci)."
            ),
        ),
    ] = None,
    format: ComplianceFormatOption = ComplianceFormat.TERMINAL,
) -> None:
    from piranesi.legal.maturity import assess_report_maturity, render_maturity_assessment

    try:
        report = _load_report_from_artifacts_dir(artifacts_dir)
        framework_keys = _resolve_framework_keys(framework)
    except ValueError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=2) from exc

    assessment = assess_report_maturity(report, framework_keys=framework_keys)
    if format == ComplianceFormat.JSON:
        typer.echo(assessment.model_dump_json(indent=2))
        return
    typer.echo(render_maturity_assessment(assessment), nl=False)


@compliance_app.command("summary")
def compliance_summary(
    artifacts_dir: Annotated[
        Path,
        typer.Argument(help="Directory containing report.json."),
    ],
    include_all: Annotated[
        bool,
        typer.Option("--all", help="Include all framework categories and frameworks."),
    ] = False,
) -> None:
    from piranesi.legal.frameworks import FRAMEWORK_BY_KEY, FRAMEWORK_CATEGORY_GROUPS
    from piranesi.report.compliance import render_compliance_summary

    try:
        report = _load_report_from_artifacts_dir(artifacts_dir)
    except ValueError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=2) from exc

    rendered = render_compliance_summary(report, include_all=include_all)
    if not include_all:
        typer.echo(rendered)
        return

    framework_counts = {framework.key: 0 for framework in FRAMEWORK_BY_KEY.values()}
    for finding in report.findings:
        obligations = getattr(finding, "regulatory_obligations", []) or []
        matched = {
            obligation.framework
            for obligation in obligations
            if getattr(obligation, "framework", None) in framework_counts
        }
        for framework_key in matched:
            framework_counts[framework_key] += 1

    lines = [rendered, "", "Framework Groups:"]
    for category, framework_keys in FRAMEWORK_CATEGORY_GROUPS:
        lines.append(f"{category}:")
        for framework_key in framework_keys:
            framework = FRAMEWORK_BY_KEY[framework_key]
            lines.append(f"  {framework.short_label}: {framework_counts[framework_key]} finding(s)")
        lines.append("")
    typer.echo("\n".join(lines).rstrip())


@compliance_app.command("evidence")
def compliance_evidence(
    framework: Annotated[
        str,
        typer.Option(
            "--framework", help="Compliance framework key (for example: soc2, pci_dss, all)."
        ),
    ],
    artifacts_dir: Annotated[
        Path,
        typer.Option(
            "--artifacts-dir", help="Directory containing scan.json and legal.json/report.json."
        ),
    ],
    output: Annotated[
        Path,
        typer.Option("--output", "-o", help="Directory to write evidence bundles."),
    ],
) -> None:
    from piranesi.legal.evidence import load_evidence_artifacts, write_evidence_bundles

    try:
        scan, assessments = load_evidence_artifacts(artifacts_dir)
        written = write_evidence_bundles(
            scan=scan,
            assessments=assessments,
            framework=framework,
            output_dir=output,
        )
    except ValueError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=2) from exc

    typer.echo(f"wrote {len(written)} evidence bundle(s) to {output}")


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


@rules_app.command("validate")
def rules_validate(
    path: RulesPathArg,
) -> None:
    from piranesi.rules.engine import RuleValidationError, compile_rule, load_rules

    try:
        compiled_rules = [compile_rule(rule) for rule in load_rules(path)]
    except RuleValidationError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=1) from exc

    if not compiled_rules:
        typer.echo(f"error: no custom rules found in {path}")
        raise typer.Exit(code=1)

    typer.echo(f"validated {len(compiled_rules)} rule(s)")
    for rule in compiled_rules:
        typer.echo(f"{rule.id} [{rule.kind}] {rule.cwe_id} severity={rule.severity}")


@rules_app.command("test")
def rules_test(
    path: RulesPathArg,
    fixture: FixtureDirOption,
) -> None:
    from piranesi.rules.engine import RuleValidationError, run_rules_against_fixture

    try:
        results = run_rules_against_fixture(path, fixture_dir=fixture)
    except RuleValidationError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=1) from exc

    if not results:
        typer.echo(f"error: no custom rules found in {path}")
        raise typer.Exit(code=1)

    total_matches = 0
    for result in results:
        suffix = "es" if len(result.findings) != 1 else ""
        typer.echo(f"{result.rule.id}: {len(result.findings)} match{suffix}")
        for finding in result.findings:
            total_matches += 1
            message = str(finding.metadata.get("custom_rule_message", "")).strip()
            location = (
                f"{finding.sink.location.file}:{finding.sink.location.line}:"
                f"{finding.sink.location.column}"
            )
            typer.echo(f"  {location} {message}")
    typer.echo(f"total matches: {total_matches}")


@rules_app.command("install")
def rules_install(
    git_url: Annotated[str, typer.Argument(help="Git URL of the rule repository.")],
    name: Annotated[
        str | None,
        typer.Option("--name", help="Override the installed rule set name."),
    ] = None,
    config: ConfigOption = Path("./piranesi.toml"),
) -> None:
    from piranesi.rules.registry import RuleRegistryError, install_rule_repository

    cfg = _load_rules_cli_config(config)
    try:
        installed = install_rule_repository(git_url, name=name, rules_config=cfg.rules)
    except RuleRegistryError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=1) from exc

    typer.echo(f"installed {installed.name} ({installed.rule_count} rules) to {installed.path}")


@rules_app.command("update")
def rules_update(
    name: Annotated[
        str | None,
        typer.Argument(help="Installed rule set name to update. Updates all when omitted."),
    ] = None,
    config: ConfigOption = Path("./piranesi.toml"),
) -> None:
    from piranesi.rules.registry import RuleRegistryError, update_rule_repositories

    cfg = _load_rules_cli_config(config)
    try:
        updated = update_rule_repositories(name=name, rules_config=cfg.rules)
    except RuleRegistryError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=1) from exc

    if not updated:
        typer.echo("no installed rule sets")
        return

    for rule_set in updated:
        remote = rule_set.remote_url or "unknown remote"
        typer.echo(f"updated {rule_set.name} ({rule_set.rule_count} rules) from {remote}")


@rules_app.command("remove")
def rules_remove(
    name: Annotated[str, typer.Argument(help="Installed rule set name.")],
    config: ConfigOption = Path("./piranesi.toml"),
) -> None:
    from piranesi.rules.registry import RuleRegistryError, remove_rule_repository

    _ = _load_rules_cli_config(config)
    try:
        removed_path = remove_rule_repository(name)
    except RuleRegistryError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=1) from exc

    typer.echo(f"removed {name} from {removed_path}")


@rules_app.command("list")
def rules_list(
    config: ConfigOption = Path("./piranesi.toml"),
) -> None:
    from piranesi.rules.registry import RuleRegistryError, list_installed_rule_sets

    cfg = _load_rules_cli_config(config)
    try:
        installed = list_installed_rule_sets(rules_config=cfg.rules)
    except RuleRegistryError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=1) from exc

    if not installed:
        typer.echo("no installed rule sets")
        return

    for rule_set in installed:
        version = rule_set.version or "unknown"
        remote = rule_set.remote_url or "unknown remote"
        line = (
            f"{rule_set.name:<20} {rule_set.rule_count:>3} rules  "
            f"version={version}  remote={remote}"
        )
        typer.echo(line)


@rules_app.command("test-all")
def rules_test_all(
    rules_dir: Annotated[
        Path | None,
        typer.Option("--rules-dir", help="Explicit rules file or directory to test."),
    ] = None,
    config: ConfigOption = Path("./piranesi.toml"),
) -> None:
    from piranesi.rules.engine import RuleValidationError
    from piranesi.rules.testing import render_rule_test_summary, run_all_rule_tests

    config_model = None if rules_dir is not None else _load_rules_cli_config(config)
    try:
        summary = run_all_rule_tests(
            rules_dir,
            rules_config=None if config_model is None else config_model.rules,
            config_path=config if config.exists() else None,
        )
    except (FileNotFoundError, ValueError, RuleValidationError) as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=1) from exc

    typer.echo(render_rule_test_summary(summary))
    if summary.total == 0 or summary.failed > 0:
        raise typer.Exit(code=1)


@rules_app.command("coverage")
def rules_coverage(
    rules_dir: Annotated[
        Path | None,
        typer.Option("--rules-dir", help="Explicit rules file or directory to inspect."),
    ] = None,
    ground_truth: Annotated[
        Path,
        typer.Option("--ground-truth", help="Ground truth directory for coverage reporting."),
    ] = Path("eval/ground_truth"),
    config: ConfigOption = Path("./piranesi.toml"),
) -> None:
    from piranesi.rules.engine import RuleValidationError
    from piranesi.rules.testing import build_rule_coverage_report, render_rule_coverage_report

    config_model = None if rules_dir is not None else _load_rules_cli_config(config)
    try:
        report = build_rule_coverage_report(
            rules_dir,
            rules_config=None if config_model is None else config_model.rules,
            config_path=config if config.exists() else None,
            ground_truth_dir=ground_truth,
        )
    except (FileNotFoundError, ValueError, RuleValidationError) as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=1) from exc

    typer.echo(render_rule_coverage_report(report))


@app.command(help=_RUN_HELP)
def run(
    target_dir: TargetDirArg,
    include: IncludeOption = None,
    exclude: ExcludeOption = None,
    sbom: SbomOption = None,
    include_tests: IncludeTestsOption = False,
    include_unreachable: IncludeUnreachableOption = False,
    dead_code_report: DeadCodeReportOption = False,
    package_name: PackageOption = None,
    changed_packages: ChangedPackagesOption = False,
    baseline: BaselineOption = None,
    fail_on_new: FailOnNewOption = False,
    fail_severity: FailSeverityOption = FailSeverity.LOW,
    no_fail: NoFailOption = False,
    staged_only: StagedOnlyOption = False,
    hook_timeout: HookTimeoutOption = None,
    incremental: IncrementalOption = None,
    triage_model: TriageModelOption = None,
    patch_model: PatchModelOption = None,
    docker_image: DockerImageOption = None,
    timeout: TimeoutOption = None,
    no_execute: NoExecuteOption = False,
    apply: ApplyOption = False,
    format: FormatOption = None,
    attestation: AttestationOption = False,
    tui: ComplianceTuiOption = False,
    resume: ResumeOption = False,
    dry_run: DryRunOption = False,
    max_parallel: MaxParallelOption = None,
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
        max_parallel=max_parallel,
        package_name=package_name,
        changed_packages_only=changed_packages,
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
            "reachability.include_unreachable": include_unreachable,
            "reachability.dead_code_report": dead_code_report,
            "models.triage": triage_model,
            "models.patcher": patch_model,
            "sandbox.docker_image": docker_image,
            "sandbox.timeout_seconds": timeout,
            "output.format": _format_override(format),
        },
    )
    _validate_compliance_flags(
        config_model.output.format,
        attestation=attestation,
        tui=tui,
    )
    selected_files: set[Path] | None = None
    if staged_only:
        try:
            staged_files = discover_staged_files(target_dir.resolve(strict=False), config_model)
        except HookError as exc:
            typer.echo(f"error: {exc}")
            raise typer.Exit(code=2) from exc
        if not staged_files:
            typer.echo("no staged files matched Piranesi scan patterns; skipping.")
            return
        selected_files = set(staged_files)
    active_hook_timeout = hook_timeout
    if active_hook_timeout is None and staged_only:
        active_hook_timeout = config_model.hooks.timeout

    if dry_run:
        if sys.stderr.isatty() and not json_logs:
            stage_header("dry-run")
        monorepo_manifest = detect_monorepo_manifest(
            target_dir.resolve(strict=False),
            config_model.scan.frameworks,
        )
        if monorepo_manifest is not None and (package_name is not None or changed_packages):
            scan_targets = [
                file_path
                for package in select_packages(
                    monorepo_manifest,
                    package_name=package_name,
                    changed_only=changed_packages,
                )
                for file_path in discover_scan_targets(package.path, config_model)
            ]
        else:
            scan_targets = discover_scan_targets(
                target_dir,
                config_model,
                candidate_paths=None if selected_files is None else tuple(selected_files),
            )
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
    monorepo_manifest = detect_monorepo_manifest(
        target_dir.resolve(strict=False),
        config_model.scan.frameworks,
    )

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
            monorepo_manifest=monorepo_manifest,
            monorepo_package_name=options.package_name,
            changed_packages_only=options.changed_packages_only,
            max_parallel=options.max_parallel,
            selected_files=selected_files,
            render_ui=sys.stderr.isatty() and not json_logs,
        )
        with _hook_timeout(active_hook_timeout):
            pipeline_result = run_pipeline(
                config_model,
                context,
                stage_registry=build_default_stage_registry(context),
                resume=resume,
                render_ui=sys.stderr.isatty() and not json_logs,
            )
    except HookTimeoutExceededError:
        typer.echo(f"scan exceeded {active_hook_timeout}s; skipping staged pre-commit scan.")
        return
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
    if report is not None:
        _generate_threat_model_for_run(report, options.output_dir, logger)
    if report is not None and config_model.output.format == ReportFormat.TUI.value:
        display_report(report, output_dir=options.output_dir)
    if report is not None and config_model.output.format == ReportFormat.COMPLIANCE.value:
        _emit_compliance_output(report, attestation=attestation, tui=tui)
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
                f"confirmed: {report.executive_summary.findings_confirmed})",
                err=config_model.output.format == ReportFormat.TUI.value,
            )
        else:
            typer.echo(
                "findings detected: "
                f"{report.executive_summary.findings_detected} "
                f"(confirmed: {report.executive_summary.findings_confirmed})",
                err=config_model.output.format == ReportFormat.TUI.value,
            )
        raise typer.Exit(code=1)
