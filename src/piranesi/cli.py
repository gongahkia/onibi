from __future__ import annotations

import logging
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Annotated, Any, Mapping

import typer

from piranesi import __version__
from piranesi.config import ConfigError, PiranesiConfig, load_config
from piranesi.llm.cost import CostTracker
from piranesi.llm.provider import LLMProvider
from piranesi.llm.router import ModelRouter
from piranesi.llm.trace import TraceLogger
from piranesi.observability import log_error_context, setup_logging
from piranesi.pipeline import (
    PipelineContext,
    build_default_stage_registry,
    discover_scan_targets,
    load_partial_summary,
    run_pipeline,
)
from piranesi.trace import TraceBudgetExceededError, TraceWriter
from piranesi.ui import console, print_summary_table, stage_header

app = typer.Typer(
    add_completion=False,
    help="CLI-native cybersecurity analysis tool for TypeScript/JavaScript source code.",
    no_args_is_help=True,
)


def _version_callback(value: bool) -> None:
    if not value:
        return
    typer.echo(f"piranesi {__version__}")
    raise typer.Exit()

TargetDirArg = Annotated[Path, typer.Argument(help="Target directory.")]
FindingsFileArg = Annotated[Path, typer.Argument(help="Findings artifact file.")]

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
FormatOption = Annotated[str | None, typer.Option("--format", help="Report format.")]
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
            next_step="exiting_with_code_3",
            debug="check TOML syntax and required fields",
        )
        raise typer.Exit(code=3) from exc

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
            next_step="exit_code_1",
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
        raise typer.Exit(code=1)
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
            next_step="exiting_with_code_3",
            debug="check TOML syntax and required fields",
        )
        raise typer.Exit(code=3) from exc

    if options.debug:
        config.trace.log_prompts = True
    return config


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


@app.command()
def scan(
    target_dir: TargetDirArg,
    include: IncludeOption = None,
    exclude: ExcludeOption = None,
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
    _run_stubbed_stage(
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
        ),
        extra_cli_overrides={
            "scan.include_patterns": include,
            "scan.exclude_patterns": exclude,
        },
    )


@app.command()
def detect(
    target_dir: TargetDirArg,
    sources: SourcesOption = None,
    sinks: SinksOption = None,
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
    _run_stubbed_stage(
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
    _run_stubbed_stage(
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
    _run_stubbed_stage(
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
    _run_stubbed_stage(
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
    _run_stubbed_stage(
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
    _run_stubbed_stage(
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
        extra_cli_overrides={"output.format": format},
    )


@app.command()
def run(
    target_dir: TargetDirArg,
    include: IncludeOption = None,
    exclude: ExcludeOption = None,
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
            "models.triage": triage_model,
            "models.patcher": patch_model,
            "sandbox.docker_image": docker_image,
            "sandbox.timeout_seconds": timeout,
            "output.format": format,
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
        raise typer.Exit(code=1)

    report_path = options.output_dir / "report.json"
    if sys.stderr.isatty() and not json_logs:
        print_summary_table(
            "Piranesi Run Summary",
            {
                "Status": "completed",
                "Stages": " -> ".join(result.stage for result in pipeline_result.results),
                "Output": options.output_dir,
                "Report": report_path,
                "Trace": trace_writer.path,
            },
        )
