from __future__ import annotations

import json
import logging
import re
import signal
import sys
import time
from collections.abc import Mapping
from contextlib import contextmanager
from dataclasses import dataclass
from datetime import UTC, date
from datetime import datetime as datetime_cls
from enum import StrEnum
from pathlib import Path
from typing import Annotated, Any, cast

import typer
from pydantic import BaseModel, ValidationError

from piranesi import __version__
from piranesi.adapters import parse_external_tool_file
from piranesi.audit import append_audit_event
from piranesi.config import ConfigError, PiranesiConfig, load_config
from piranesi.detect import (
    append_ignore_file_suppression,
    apply_suppressions_with_lifecycle,
    load_ignore_file_with_diagnostics,
    parse_inline_suppressions,
)
from piranesi.diff import (
    DiffResult,
    build_baseline_artifact,
    diff_findings,
    diff_result_payload,
    load_findings,
    new_findings_at_or_above,
    render_diff,
    render_diff_markdown,
)
from piranesi.doctor import build_doctor_report, render_doctor_report
from piranesi.graph import build_graph_from_enrichment
from piranesi.hooks.pre_commit import (
    HookError,
    discover_staged_files,
    install_pre_commit_hook,
    pre_commit_hook_status,
    uninstall_pre_commit_hook,
)
from piranesi.intel import build_enrichment_summary, normalize_adapter_result
from piranesi.intel.schema import IntelSourceProvenance, NormalizationBundle
from piranesi.launcher_tui import LauncherAction, LauncherSelection, launch_cli_tui
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
    CompositeRiskBreakdown,
    FindingExplanation,
    MatchedSpec,
    OwnershipMetadata,
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
_ADVISORY_PROJECT_ROOT_HELP = "Project root used to resolve the default advisory DB."

app = typer.Typer(
    add_completion=False,
    help="CLI-native cybersecurity analysis tool for TypeScript/JavaScript source code.",
    no_args_is_help=False,
    invoke_without_command=True,
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
advisory_app = typer.Typer(
    add_completion=False,
    help="Manage local advisory database workflows.",
    no_args_is_help=True,
)
baseline_app = typer.Typer(
    add_completion=False,
    help="Manage baseline artifacts.",
    no_args_is_help=True,
)
suppressions_app = typer.Typer(
    add_completion=False,
    help="Manage suppression lifecycle and validation.",
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
eval_app = typer.Typer(
    add_completion=False,
    help="Run evaluation harness commands.",
    no_args_is_help=True,
)
pipeline_app = typer.Typer(
    add_completion=False,
    help="Advanced stage-level pipeline controls (scan/detect/triage/verify/legal/patch/report).",
    no_args_is_help=True,
)
dev_app = typer.Typer(
    add_completion=False,
    help="Developer productivity workflows (watch and LSP).",
    no_args_is_help=True,
)
intel_app = typer.Typer(
    add_completion=False,
    help="Ingest and normalize offline external intelligence snapshots.",
    no_args_is_help=True,
)
app.add_typer(plugins_app, name="plugins")
app.add_typer(rules_app, name="rules")
app.add_typer(advisory_app, name="advisory")
app.add_typer(baseline_app, name="baseline")
app.add_typer(suppressions_app, name="suppressions")
app.add_typer(compliance_app, name="compliance")
app.add_typer(hook_app, name="hook")
app.add_typer(eval_app, name="eval")
app.add_typer(intel_app, name="intel")
app.add_typer(pipeline_app, name="pipeline")
app.add_typer(dev_app, name="dev")


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


class BaselineDiffFormat(StrEnum):
    TEXT = "text"
    MARKDOWN = "markdown"
    JSON = "json"


class SbomFormat(StrEnum):
    SPDX = "spdx"
    CYCLONEDX = "cyclonedx"


class ProofMode(StrEnum):
    SAFE = "safe"
    UNSAFE = "unsafe"


class FailSeverity(StrEnum):
    LOW = "low"
    MEDIUM = "medium"
    HIGH = "high"
    CRITICAL = "critical"


class AdvisoryTrustPolicy(StrEnum):
    PERMISSIVE = "permissive"
    VERIFIED_ONLY = "verified-only"


class AdvisoryPolicyAction(StrEnum):
    IGNORE = "ignore"
    WARN = "warn"
    FAIL = "fail"


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
BaselineDiffFormatOption = Annotated[
    BaselineDiffFormat,
    typer.Option("--format", help="Diff output format.", case_sensitive=False),
]
ComplianceFormatOption = Annotated[
    ComplianceFormat,
    typer.Option("--format", help="Compliance output format.", case_sensitive=False),
]
SbomOption = Annotated[
    SbomFormat | None,
    typer.Option("--sbom", help="Generate an SBOM during scan.", case_sensitive=False),
]
ProofModeOption = Annotated[
    ProofMode | None,
    typer.Option(
        "--proof-mode",
        help="Verification probe mode: safe (default) or unsafe (explicit opt-in).",
        case_sensitive=False,
    ),
]
TargetProfileOption = Annotated[
    str | None,
    typer.Option(
        "--target-profile",
        help="Reusable verification launch profile name from [verify.target_profiles].",
    ),
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
    bool | None,
    typer.Option(
        "--fail-on-new/--no-fail-on-new",
        help="Exit 1 only when baseline diff contains NEW findings.",
    ),
]
FailOnNewSeverityOption = Annotated[
    FailSeverity | None,
    typer.Option(
        "--fail-on-new-severity",
        help=(
            "Only count NEW findings at or above this severity when used with --fail-on-new "
            "or [baseline].fail_on_new."
        ),
        case_sensitive=False,
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


def _proof_mode_override(proof_mode: ProofMode | None) -> str | None:
    if proof_mode is None:
        return None
    return proof_mode.value


def _project_audit_output_dir(project_root: Path) -> Path:
    return project_root.resolve(strict=False) / "piranesi-output"


def _filtered_cli_overrides(extra_cli_overrides: Mapping[str, Any] | None) -> dict[str, Any]:
    if not extra_cli_overrides:
        return {}
    return {key: value for key, value in extra_cli_overrides.items() if value is not None}


def _write_audit_event(
    *,
    output_dir: Path,
    event_type: str,
    stage: str | None = None,
    approved: bool | None = None,
    details: Mapping[str, Any] | None = None,
) -> None:
    logger = logging.getLogger("piranesi.audit")
    try:
        append_audit_event(
            output_dir=output_dir,
            event_type=event_type,
            stage=stage,
            approved=approved,
            details=details,
        )
    except OSError as exc:
        logger.debug("failed to persist audit event %s: %s", event_type, exc)


def _record_policy_override_event(
    *,
    stage: str,
    options: CommonOptions,
    extra_cli_overrides: Mapping[str, Any] | None,
) -> None:
    overrides = _filtered_cli_overrides(extra_cli_overrides)
    if not overrides:
        return
    _write_audit_event(
        output_dir=options.output_dir,
        event_type="policy_override_applied",
        stage=stage,
        approved=options.authorized,
        details={
            "config_path": options.config_path.resolve(strict=False),
            "output_dir": options.output_dir.resolve(strict=False),
            "yes": options.assume_yes,
            "overrides": overrides,
        },
    )


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
            return _resolve_finding_status("confirmed", finding), finding
    for finding in report.active_findings:
        if finding.finding_id == finding_id:
            return _resolve_finding_status("static_candidate", finding), finding
    for finding in report.unreachable_findings:
        if finding.finding_id == finding_id:
            return _resolve_finding_status("unreachable_candidate", finding), finding
    for finding in report.suppressed_findings:
        if finding.finding_id == finding_id:
            return _resolve_finding_status("suppressed", finding), finding
    return None


def _render_finding_explanation(status: str, finding: ReportFindingMatch) -> str:
    evidence_label = _status_label(status)
    explanation = _finding_explanation_payload(finding)
    lines = [
        "# Piranesi Finding Explanation",
        "",
        f"ID: {finding.finding_id}",
        f"Status: {status.replace('_', ' ')}",
        f"Evidence: {evidence_label}",
        f"Title: {finding.title}",
        f"CWE: {finding.cwe}",
        f"Severity: {finding.severity.upper()}",
        f"Confidence: {finding.confidence:.2f}",
        (
            f"Composite risk: {finding.composite_risk_score:.1f}/100 "
            f"({finding.composite_risk_band})"
        ),
        (
            f"Source: {finding.source_location.file}:{finding.source_location.line} "
            f"({finding.taint_source})"
        ),
        (f"Sink: {finding.sink_location.file}:{finding.sink_location.line} ({finding.taint_sink})"),
    ]
    ownership = getattr(finding, "ownership", None)
    if isinstance(ownership, OwnershipMetadata):
        lines.extend(
            [
                "",
                "Ownership:",
                f"- Service: {ownership.service or 'n/a'}",
                f"- System: {ownership.system or 'n/a'}",
                f"- Team: {ownership.team or 'n/a'}",
                f"- Owner: {ownership.owner or 'n/a'}",
                f"- Repository: {ownership.repository or 'n/a'}",
                f"- Environment: {ownership.environment or 'n/a'}",
                f"- Control owner: {ownership.control_owner or 'n/a'}",
                f"- Package: {ownership.package or 'n/a'}",
            ]
        )
        if ownership.source_path is not None or ownership.sink_path is not None:
            lines.append(
                "- Paths: "
                f"source={ownership.source_path or 'n/a'}, "
                f"sink={ownership.sink_path or 'n/a'}"
            )
        if ownership.matched_package_mapping is not None:
            lines.append(f"- Package mapping: {ownership.matched_package_mapping}")
        if ownership.matched_path_mapping is not None:
            lines.append(f"- Path mapping: {ownership.matched_path_mapping}")
    if explanation is not None:
        lines.extend(
            [
                "",
                "What matched:",
                f"- Source spec: {_format_matched_spec('source', explanation.matched_source_spec)}",
                f"- Sink spec: {_format_matched_spec('sink', explanation.matched_sink_spec)}",
            ]
        )
        if explanation.sanitizers_considered:
            lines.append("- Sanitizers considered:")
            lines.extend(
                [
                    (
                        f"- {sanitizer.name}: {sanitizer.effectiveness or 'unknown'} "
                        f"(observed_on_path={'yes' if sanitizer.observed_on_path else 'no'})"
                    )
                    for sanitizer in explanation.sanitizers_considered
                ]
            )
        else:
            lines.append("- Sanitizers considered: none")
        if explanation.sanitizers_observed:
            lines.append(f"- Sanitizers observed: {', '.join(explanation.sanitizers_observed)}")
        else:
            lines.append("- Sanitizers observed: none")
        path = explanation.propagation_path
        operations = ", ".join(path.operation_sequence) if path.operation_sequence else "none"
        lines.extend(
            [
                "",
                "Propagation:",
                f"- Path: {path.source_to_sink}",
                f"- Nodes: {path.path_node_count} (edges: {path.path_edge_count})",
                f"- Operations: {operations}",
                (
                    "- Sanitizer steps on path: "
                    f"{'yes' if path.includes_sanitizer_steps else 'no'}"
                ),
                "",
                "Verification state:",
                f"- State: {explanation.verification_state.state}",
                (
                    "- Verified: "
                    f"{'yes' if explanation.verification_state.verified else 'no'}"
                ),
                (
                    "- Outcome: "
                    f"{explanation.verification_state.outcome or 'not_attempted'}"
                ),
                (
                    "- Proof mode: "
                    f"{explanation.verification_state.proof_mode or 'n/a'}"
                ),
                (
                    "- Target profile: "
                    f"{explanation.verification_state.target_profile or 'n/a'}"
                ),
                (
                    "- Verification method: "
                    f"{explanation.verification_state.verification_method or 'n/a'}"
                ),
                (
                    "- Verification reason: "
                    f"{explanation.verification_state.reason or 'n/a'}"
                ),
                (
                    "- Startup error: "
                    f"{explanation.verification_state.startup_error or 'n/a'}"
                ),
                (
                    "- Launch logs: "
                    f"{explanation.verification_state.launch_log_path or 'n/a'}"
                ),
                (
                    "- Triage: "
                    f"{explanation.verification_state.triage_verdict or 'n/a'} "
                    f"(mode={explanation.verification_state.triage_mode or 'n/a'})"
                ),
                (
                    "- Suppression reason: "
                    f"{explanation.verification_state.suppression_reason or 'n/a'}"
                ),
                (
                    "- Missing preconditions: "
                    f"{', '.join(explanation.verification_state.missing_preconditions) or 'none'}"
                ),
                (
                    "- Verification evidence: "
                    f"{' | '.join(explanation.verification_state.evidence) or 'none'}"
                ),
            ]
        )
        rich_evidence = explanation.verification_state.rich_evidence
        if rich_evidence is not None:
            request_target = rich_evidence.attempted_route or rich_evidence.attempted_url or "n/a"
            diff_summary = (
                None
                if rich_evidence.response_diff_summary is None
                else rich_evidence.response_diff_summary.summary
            )
            timing_summary = (
                "n/a"
                if rich_evidence.timing_summary is None
                else rich_evidence.timing_summary.model_dump_json()
            )
            lines.extend(
                [
                    f"- Verification request: {rich_evidence.method or 'n/a'} {request_target}",
                    f"- Verification status code: {rich_evidence.status_code or 'n/a'}",
                    f"- Payload class: {rich_evidence.payload_class or 'n/a'}",
                    f"- Template id: {rich_evidence.template_id or 'n/a'}",
                    f"- Response diff summary: {diff_summary or 'n/a'}",
                    f"- Timing summary: {timing_summary}",
                    f"- Error signature: {rich_evidence.error_signature or 'n/a'}",
                    f"- Body excerpt hash: {rich_evidence.body_excerpt.sha256 or 'n/a'}",
                    (
                        "- Redaction: "
                        f"{'applied' if rich_evidence.redaction_status.applied else 'none'} "
                        f"(count={rich_evidence.redaction_status.redacted_value_count})"
                    ),
                ]
            )
        lines.extend(
            [
                (
                    "- Evidence artifact: "
                    f"{explanation.verification_state.evidence_artifact_path or 'n/a'}"
                ),
                "",
                "Confidence contributors:",
            ]
        )
        if explanation.verification_state.actionable_next_steps:
            lines.extend(["Verification next steps:"])
            lines.extend(
                f"- {step}" for step in explanation.verification_state.actionable_next_steps
            )
        confidence = explanation.confidence
        lines.extend(_confidence_component_lines(confidence))
        lines.extend(
            [
                f"- Contextual confidence: {confidence.contextual_confidence:.3f}",
                f"- Final confidence: {confidence.final_confidence:.3f}",
                f"- Severity basis: {explanation.severity_basis}",
            ]
        )
    composite_risk = getattr(finding, "composite_risk", None)
    if isinstance(composite_risk, CompositeRiskBreakdown):
        lines.extend(["", "Composite risk contributors:"])
        lines.extend(_composite_risk_component_lines(composite_risk))
        lines.append(
            f"- Composite risk total: {composite_risk.total_score:.1f}/100 "
            f"(band={composite_risk.risk_band})"
        )
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


def _resolve_finding_status(default_status: str, finding: ReportFindingMatch) -> str:
    status = getattr(finding, "evidence_status", None)
    if isinstance(status, str) and status:
        return status
    return default_status


def _status_label(status: str) -> str:
    labels = {
        "confirmed": "Dynamically verified issue",
        "triaged_active_candidate": "LLM-triaged active candidate",
        "static_candidate": "Static candidate",
        "unreachable_candidate": "Unreachable candidate",
        "suppressed": "Suppressed finding",
    }
    return labels.get(status, status.replace("_", " "))


def _finding_explanation_payload(finding: ReportFindingMatch) -> FindingExplanation | None:
    explanation = getattr(finding, "explanation", None)
    if isinstance(explanation, FindingExplanation):
        return explanation
    return None


def _format_matched_spec(kind: str, spec: MatchedSpec) -> str:
    if spec.spec_id is not None:
        label = spec.spec_id
    elif spec.name is not None:
        label = f"{kind}:{spec.name}"
    else:
        return "n/a"
    details: list[str] = []
    if spec.category is not None:
        details.append(f"category={spec.category}")
    if spec.cwe is not None:
        details.append(f"cwe={spec.cwe}")
    if spec.is_custom is not None:
        details.append(f"custom={'yes' if spec.is_custom else 'no'}")
    return label if not details else f"{label} ({', '.join(details)})"


def _confidence_component_lines(confidence: object) -> list[str]:
    if not hasattr(confidence, "static_reachability"):
        return []
    components = [
        ("static_reachability", confidence.static_reachability),
        ("source_quality", confidence.source_quality),
        ("sink_quality", confidence.sink_quality),
        ("sanitizer_signal", confidence.sanitizer_signal),
        ("triage_signal", confidence.triage_signal),
        ("verification_signal", confidence.verification_signal),
        ("suppression_signal", confidence.suppression_signal),
    ]
    lines: list[str] = []
    for name, component in components:
        lines.append(
            f"- {name}: score={component.score:.3f}, weight={component.weight:.3f}, "
            f"weighted={component.weighted_score:.3f} — {component.rationale}"
        )
    return lines


def _composite_risk_component_lines(risk: CompositeRiskBreakdown) -> list[str]:
    components = [
        ("severity", risk.severity),
        ("confidence", risk.confidence),
        ("source_exposure", risk.source_exposure),
        ("sink_criticality", risk.sink_criticality),
        ("ownership_signal", risk.ownership_signal),
        ("verification_signal", risk.verification_signal),
        ("exploitability_signal", risk.exploitability_signal),
        ("advisory_signal", risk.advisory_signal),
        ("reachable_path_signal", risk.reachable_path_signal),
        ("suppression_signal", risk.suppression_signal),
    ]
    return [
        f"- {name}: points={component.points:.1f} — {component.rationale}"
        for name, component in components
    ]


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
    _record_policy_override_event(
        stage=stage,
        options=options,
        extra_cli_overrides=extra_cli_overrides,
    )
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
    no_execute: bool = False,
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
            no_execute=no_execute,
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
        if stage_name == "report" and "triage" not in context.stage_outputs:
            triage_path = options.output_dir / "triage.json"
            if triage_path.exists():
                try:
                    context.stage_outputs["triage"] = _load_artifact_file(
                        triage_path,
                        TriageArtifact,
                    )
                except ValueError:
                    logger.warning(
                        "unable to load optional triage artifact from %s; "
                        "continuing report generation without triage evidence",
                        triage_path,
                    )
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
    *,
    output_format: BaselineDiffFormat = BaselineDiffFormat.TEXT,
) -> DiffResult:
    try:
        baseline_findings = load_findings(baseline_path)
        current_findings = load_findings(current_path)
    except ValueError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=1) from exc

    diff_result = diff_findings(baseline_findings, current_findings)
    typer.echo(f"Piranesi Diff: {baseline_path} -> {current_path}")
    if output_format == BaselineDiffFormat.JSON:
        typer.echo(json.dumps(diff_result_payload(diff_result), indent=2))
    elif output_format == BaselineDiffFormat.MARKDOWN:
        typer.echo(render_diff_markdown(diff_result), nl=False)
    else:
        typer.echo(render_diff(diff_result))
    return diff_result


def _write_baseline_diff_artifacts(diff_result: DiffResult, output_dir: Path) -> tuple[Path, Path]:
    markdown_path = output_dir / "baseline-diff.md"
    json_path = output_dir / "baseline-diff.json"
    markdown_path.write_text(render_diff_markdown(diff_result), encoding="utf-8")
    json_path.write_text(json.dumps(diff_result_payload(diff_result), indent=2), encoding="utf-8")
    return markdown_path, json_path


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


def _slugify_rule_pack_name(raw: str) -> str:
    normalized = re.sub(r"[^a-z0-9._-]+", "-", raw.strip().lower())
    normalized = normalized.strip("-.")
    return normalized


def _rule_scaffold_template(slug: str) -> str:
    return (
        "[rule]\n"
        f'id = "{slug}"\n'
        f'name = "{slug.replace("-", " ").title()}"\n'
        'schema_version = "1"\n'
        'category = "injection"\n'
        'cwe_id = "CWE-89"\n'
        'severity = "high"\n'
        'description = "Example custom rule scaffold. Tune source, sink, and '
        'sanitizers for your project."\n'
        'author = "your-team"\n'
        'version = "0.1.0"\n'
        'tags = ["example", "custom"]\n'
        "\n"
        "[rule.source]\n"
        'pattern = "req\\\\.(?:body|query|params)\\\\.[A-Za-z_$][\\\\w$]*"\n'
        'type = "regex"\n'
        "\n"
        "[rule.sink]\n"
        'pattern = "db\\\\.query\\\\s*\\\\("\n'
        'type = "regex"\n'
        "\n"
        "[rule.sanitizers]\n"
        'patterns = ["sanitizeSql\\\\s*\\\\("]\n'
        'type = "regex"\n'
        "\n"
        "[rule.message]\n"
        'template = "Custom rule scaffold: user input from `{source}` reaches `{sink}`."\n'
        "\n"
        "[[tests]]\n"
        'fixture = "tests/fixtures/vulnerable.ts"\n'
        "expect_finding = true\n"
        'expect_cwe = "CWE-89"\n'
        "\n"
        "[[tests]]\n"
        'fixture = "tests/fixtures/safe.ts"\n'
        "expect_finding = false\n"
    )


def _resolve_advisory_db_path(project_root: Path, db_path: Path | None) -> Path:
    if db_path is not None:
        return db_path.expanduser().resolve(strict=False)
    from piranesi.advisory import advisory_db_path

    return advisory_db_path(project_root)


def _advisory_status_payload(status: Any) -> dict[str, object]:
    return {
        "path": str(status.path),
        "exists": status.exists,
        "schema_version": status.schema_version,
        "advisory_count": status.advisory_count,
        "affected_package_count": status.affected_package_count,
        "sources": list(status.sources),
        "last_updated": status.last_updated,
        "checksum_sha256": status.checksum_sha256,
        "freshness": status.freshness,
        "stale_after_days": status.stale_after_days,
        "age_days": status.age_days,
        "trust_state": status.trust_state,
        "provenance_verified": status.provenance_verified,
        "provenance_signature_scheme": status.provenance_signature_scheme,
        "provenance_signature_signer": status.provenance_signature_signer,
        "provenance_snapshot_sha256": status.provenance_snapshot_sha256,
        "provenance_manifest_sha256": status.provenance_manifest_sha256,
        "provenance_imported_at": status.provenance_imported_at,
        "warnings": list(status.warnings),
    }


def _advisory_policy_payload(outcome: Any) -> dict[str, object]:
    return {
        "mode": outcome.mode,
        "allowed": outcome.allowed,
        "freshness": outcome.freshness,
        "trust_state": outcome.trust_state,
        "violations": list(outcome.violations),
        "warnings": list(outcome.warnings),
    }


def _enforce_advisory_policy(
    *,
    status: Any,
    trust_policy: AdvisoryTrustPolicy,
    on_missing: AdvisoryPolicyAction,
    on_stale: AdvisoryPolicyAction,
    on_unsigned: AdvisoryPolicyAction,
) -> Any:
    from piranesi.advisory import evaluate_trust_policy

    return evaluate_trust_policy(
        status,
        mode=trust_policy.value,
        on_missing=on_missing.value,
        on_stale=on_stale.value,
        on_unsigned=on_unsigned.value,
    )


def _load_hook_cli_config(config_path: Path) -> PiranesiConfig:
    if not config_path.exists():
        return PiranesiConfig()
    try:
        return load_config(config_path)
    except ConfigError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=2) from exc


def _interactive_tty_available() -> bool:
    return sys.stdin.isatty() and sys.stderr.isatty()


def _render_latest_summary(output_dir: Path) -> None:
    report = _load_report_from_artifacts_dir(output_dir)
    summary = report.executive_summary
    print_summary_table(
        "Latest Piranesi Summary",
        {
            "Output": output_dir,
            "Findings detected": summary.findings_detected,
            "Findings suppressed": summary.suppressed_findings,
            "Findings confirmed": summary.findings_confirmed,
            "Reachable findings": summary.reachable_findings,
            "Unreachable findings": summary.unreachable_findings,
            "Top risk finding": summary.highest_composite_risk_finding_id or "n/a",
            "Top risk score": f"{summary.highest_composite_risk_score:.1f}",
            "Duration (s)": f"{summary.duration_s:.1f}",
            "LLM cost (USD)": f"{summary.total_llm_cost_usd:.4f}",
        },
    )

    combined = (
        list(report.findings) + list(report.active_findings) + list(report.unreachable_findings)
    )
    ranked = sorted(
        (
            (
                str(getattr(finding, "finding_id", "n/a")),
                float(getattr(finding, "composite_risk_score", 0.0) or 0.0),
                str(getattr(finding, "severity", "unknown")).upper(),
                str(getattr(finding, "title", "n/a")),
            )
            for finding in combined
        ),
        key=lambda item: item[1],
        reverse=True,
    )[:5]
    if not ranked:
        return
    typer.echo("top risk findings:")
    for finding_id, risk, severity, title in ranked:
        typer.echo(f"- [{severity}] {finding_id} risk={risk:.1f} :: {title}")


def _dispatch_launcher_selection(selection: LauncherSelection) -> None:
    if selection.action is LauncherAction.QUIT:
        return
    if selection.action is LauncherAction.RUN:
        run(
            target_dir=selection.target_dir,
            no_execute=selection.no_execute,
            resume=selection.resume,
            config=selection.config_path,
            output=selection.output_dir,
            trace=selection.trace_path,
            authorized=True,
            yes=True,
        )
        return
    if selection.action is LauncherAction.REPORT_TUI:
        report = _load_report_from_artifacts_dir(selection.output_dir)
        display_report(report, output_dir=selection.output_dir)
        return
    if selection.action is LauncherAction.SUMMARY:
        _render_latest_summary(selection.output_dir)
        return
    if selection.action is LauncherAction.DOCTOR:
        report = build_doctor_report(selection.target_dir, config_path=selection.config_path)
        typer.echo(render_doctor_report(report), nl=False)
        return


@app.callback()
def main(
    ctx: typer.Context,
    version: VersionOption = False,
) -> None:
    _ = version
    if ctx.invoked_subcommand is not None:
        return
    if not _interactive_tty_available():
        typer.echo(ctx.get_help())
        raise typer.Exit()
    ui(
        target_dir=Path("."),
        output=Path("./piranesi-output"),
        config=Path("./piranesi.toml"),
        trace=Path(".piranesi-trace.jsonl"),
    )
    raise typer.Exit()


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


@app.command(
    help=(
        "Launch the interactive launcher (arrow or hjkl navigation, "
        "ASCII banner, directory picker)."
    )
)
def ui(
    target_dir: Annotated[
        Path,
        typer.Option("--target", help="Default target directory for dashboard actions."),
    ] = Path("."),
    output: OutputOption = Path("./piranesi-output"),
    config: ConfigOption = Path("./piranesi.toml"),
    trace: TraceOption = Path(".piranesi-trace.jsonl"),
) -> None:
    if not _interactive_tty_available():
        typer.echo("error: `piranesi ui` requires an interactive TTY.")
        raise typer.Exit(code=2)
    try:
        selection = launch_cli_tui(
            target_dir=target_dir.expanduser().resolve(strict=False),
            output_dir=output.expanduser().resolve(strict=False),
            config_path=config.expanduser().resolve(strict=False),
            trace_path=trace.expanduser().resolve(strict=False),
        )
    except ImportError as exc:
        typer.echo(
            "error: Textual is required for launcher UI. "
            "Install extras with `uv sync --extra tui`."
        )
        raise typer.Exit(code=2) from exc
    if selection is None:
        return
    _dispatch_launcher_selection(selection)


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


@app.command(hidden=True)
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


@app.command(help="Watch a directory and run incremental scans on file changes.", hidden=True)
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


@app.command(help="Start the Piranesi LSP server.", hidden=True)
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


@app.command(hidden=True)
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


@app.command(hidden=True)
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


@app.command(hidden=True)
def verify(
    findings_file: FindingsFileArg,
    docker_image: DockerImageOption = None,
    timeout: TimeoutOption = None,
    proof_mode: ProofModeOption = None,
    target_profile: TargetProfileOption = None,
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
            "verify.proof_mode": _proof_mode_override(proof_mode),
            "verify.target_profile": target_profile,
        },
        no_execute=no_execute,
    )


@app.command(hidden=True)
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


@app.command(hidden=True)
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


@app.command(hidden=True)
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
        explanation = _finding_explanation_payload(finding)
        payload: dict[str, object] = {
            "status": status,
            "evidence": _status_label(status),
            "finding": finding.model_dump(mode="json"),
        }
        if explanation is not None:
            payload["explanation"] = explanation.model_dump(mode="json")
        typer.echo(
            json.dumps(payload, indent=2)
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

    _write_audit_event(
        output_dir=output,
        event_type="compliance_evidence_exported",
        stage="compliance",
        approved=True,
        details={
            "framework": framework,
            "artifacts_dir": artifacts_dir.resolve(strict=False),
            "output_dir": output.resolve(strict=False),
            "bundle_count": len(written),
        },
    )
    typer.echo(f"wrote {len(written)} evidence bundle(s) to {output}")


@compliance_app.command("bundle")
def compliance_bundle(
    framework: Annotated[
        str,
        typer.Option(
            "--framework",
            help="Compliance framework key (for example: soc2, pci_dss, all).",
        ),
    ],
    artifacts_dir: Annotated[
        Path,
        typer.Option(
            "--artifacts-dir",
            help="Directory containing scan.json and legal.json/report.json.",
        ),
    ],
    output: Annotated[
        Path,
        typer.Option("--output", "-o", help="Directory to write the compliance bundle."),
    ],
    redact: Annotated[
        bool,
        typer.Option(
            "--redact/--no-redact",
            help="Redact sensitive values in copied bundle artifacts.",
        ),
    ] = True,
    config_snapshot: Annotated[
        Path | None,
        typer.Option(
            "--config-snapshot",
            help="Optional explicit piranesi.toml path to include in the bundle.",
        ),
    ] = None,
) -> None:
    from piranesi.legal.evidence import build_compliance_evidence_bundle

    try:
        manifest = build_compliance_evidence_bundle(
            artifacts_dir=artifacts_dir,
            framework=framework,
            output_dir=output,
            redact=redact,
            config_path=config_snapshot,
        )
    except ValueError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=2) from exc

    _write_audit_event(
        output_dir=output,
        event_type="compliance_bundle_exported",
        stage="compliance",
        approved=True,
        details={
            "framework": framework,
            "artifacts_dir": artifacts_dir.resolve(strict=False),
            "output_dir": output.resolve(strict=False),
            "redact": redact,
            "manifest_path": output / manifest.checksum_manifest_path,
            "config_snapshot": (
                None if config_snapshot is None else config_snapshot.resolve(strict=False)
            ),
            "file_count": len(manifest.files),
        },
    )
    typer.echo(
        "wrote compliance bundle with "
        f"{len(manifest.files)} file(s) to {output}"
    )
    typer.echo(f"manifest: {output / manifest.checksum_manifest_path}")


@app.command(hidden=True)
def suppress(
    finding_id: Annotated[str, typer.Argument(help="Finding fingerprint to suppress.")],
    reason: Annotated[str, typer.Option("--reason", help="Suppression rationale.")],
    reason_code: Annotated[
        str | None,
        typer.Option(
            "--reason-code",
            help="Machine-readable reason code (for example: risk_accepted).",
        ),
    ] = None,
    owner: Annotated[
        str | None,
        typer.Option("--owner", help="Suppression owner (team or individual)."),
    ] = None,
    ticket: Annotated[
        str | None, typer.Option("--ticket", help="Optional ticket reference.")
    ] = None,
    reference: Annotated[
        str | None,
        typer.Option("--reference", help="Optional external reference or URL."),
    ] = None,
    created: Annotated[
        str | None,
        typer.Option("--created", help="Created date in YYYY-MM-DD."),
    ] = None,
    expires: Annotated[
        str | None,
        typer.Option("--expires", help="Expiry date in YYYY-MM-DD."),
    ] = None,
    scope: Annotated[
        str | None,
        typer.Option("--scope", help="Suppression scope label (for example: id, cwe_path)."),
    ] = "id",
    project_root: Annotated[
        Path,
        typer.Option("--project-root", help="Project root containing .piranesi-ignore."),
    ] = Path("."),
) -> None:
    created_date = _parse_date_option(created, option_name="--created")
    expires_date = _parse_date_option(expires, option_name="--expires")
    ignore_path = append_ignore_file_suppression(
        project_root,
        finding_id=finding_id,
        reason=reason,
        reason_code=reason_code,
        owner=owner,
        ticket=ticket,
        reference=reference,
        created=created_date,
        expires=expires_date,
        scope=scope,
    )
    _write_audit_event(
        output_dir=_project_audit_output_dir(project_root),
        event_type="suppression_created",
        stage="detect",
        approved=True,
        details={
            "project_root": project_root.resolve(strict=False),
            "ignore_path": ignore_path.resolve(strict=False),
            "finding_id": finding_id,
            "scope": scope,
            "owner": owner,
            "reason_code": reason_code,
            "ticket": ticket,
            "reference": reference,
            "created": None if created_date is None else created_date.isoformat(),
            "expires": None if expires_date is None else expires_date.isoformat(),
        },
    )
    typer.echo(f"added suppression for {finding_id} to {ignore_path}")


def _load_detect_findings_for_suppression_validation(path: Path) -> list[Any]:
    candidate_path = path
    if path.is_dir():
        candidate_path = path / "detect.json"
    if not candidate_path.exists():
        raise ValueError(
            "suppression validation requires detect findings; "
            f"missing artifact at {candidate_path}"
        )
    try:
        detect_artifact = DetectArtifact.model_validate_json(
            candidate_path.read_text(encoding="utf-8")
        )
    except (OSError, ValidationError) as exc:
        raise ValueError(f"invalid detect artifact at {candidate_path}: {exc}") from exc
    return list(detect_artifact.findings)


def _collect_inline_suppressions_from_findings(findings: list[Any]) -> list[Any]:
    source_files: set[Path] = set()
    for finding in findings:
        candidate = Path(finding.source.location.file)
        source_files.add(candidate.resolve(strict=False))
    inline: list[Any] = []
    for source_file in sorted(source_files):
        inline.extend(parse_inline_suppressions(source_file))
    return inline


@suppressions_app.command("list")
def suppressions_list(
    project_root: Annotated[
        Path,
        typer.Option("--project-root", help="Project root containing .piranesi-ignore."),
    ] = Path("."),
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Emit machine-readable JSON."),
    ] = False,
) -> None:
    validation = load_ignore_file_with_diagnostics(project_root)
    today = datetime_cls.now(UTC).date()
    rows = []
    for rule in validation.rules:
        expired = rule.expires is not None and rule.expires < today
        rows.append(
            {
                "selector": {
                    "id": rule.id,
                    "cwe": rule.cwe,
                    "path": rule.path,
                },
                "scope": rule.scope,
                "reason": rule.reason,
                "reason_code": rule.reason_code,
                "owner": rule.owner,
                "created": None if rule.created is None else rule.created.isoformat(),
                "expires": None if rule.expires is None else rule.expires.isoformat(),
                "ticket": rule.ticket,
                "reference": rule.reference,
                "status": "expired" if expired else "active",
            }
        )

    payload = {
        "path": validation.path,
        "rules": rows,
        "invalid_entries": validation.invalid_entries,
    }
    if json_output:
        typer.echo(json.dumps(payload, indent=2))
        return

    typer.echo(f"Suppression file: {validation.path}")
    typer.echo(f"Rules: {len(rows)}")
    for row in rows:
        selector = row["selector"]
        selector_parts = [
            f"id={selector['id']}" if selector["id"] else None,
            f"cwe={selector['cwe']}" if selector["cwe"] else None,
            f"path={selector['path']}" if selector["path"] else None,
        ]
        selector_text = ", ".join(part for part in selector_parts if part) or "<invalid>"
        typer.echo(
            f"- {selector_text} [{row['status']}] "
            f"owner={row['owner'] or 'n/a'} "
            f"expires={row['expires'] or 'n/a'} "
            f"reason={row['reason'] or 'n/a'}"
        )
    if validation.invalid_entries:
        typer.echo("Invalid entries:")
        for entry in validation.invalid_entries:
            typer.echo(f"- {entry}")


@suppressions_app.command("validate")
def suppressions_validate(
    project_root: Annotated[
        Path,
        typer.Option("--project-root", help="Project root containing .piranesi-ignore."),
    ] = Path("."),
    findings: Annotated[
        Path | None,
        typer.Option(
            "--findings",
            help="detect.json artifact or results directory used to evaluate stale suppressions.",
        ),
    ] = None,
    config_path: Annotated[
        Path,
        typer.Option("--config", help="Optional piranesi.toml to read suppression fail policy."),
    ] = Path("./piranesi.toml"),
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Emit machine-readable JSON."),
    ] = False,
) -> None:
    config_model = PiranesiConfig()
    if config_path.exists():
        try:
            config_model = load_config(config_path)
        except ConfigError as exc:
            typer.echo(f"error: {exc}")
            raise typer.Exit(code=2) from exc

    validation = load_ignore_file_with_diagnostics(project_root)
    detected_findings: list[Any] = []
    inline: list[Any] = []
    evaluate_stale = False
    if findings is not None:
        try:
            detected_findings = _load_detect_findings_for_suppression_validation(findings)
        except ValueError as exc:
            typer.echo(f"error: {exc}")
            raise typer.Exit(code=2) from exc
        inline = _collect_inline_suppressions_from_findings(detected_findings)
        evaluate_stale = True
    outcome = apply_suppressions_with_lifecycle(
        detected_findings,
        validation.rules,
        inline,
        invalid_entries=validation.invalid_entries,
        evaluate_stale=evaluate_stale,
    )
    lifecycle = outcome.lifecycle
    payload = {
        "path": validation.path,
        "summary": lifecycle.model_dump(mode="json"),
        "policy": {
            "fail_on_invalid": config_model.suppression.fail_on_invalid,
            "fail_on_expired": config_model.suppression.fail_on_expired,
            "fail_on_stale": config_model.suppression.fail_on_stale,
        },
    }
    _write_audit_event(
        output_dir=_project_audit_output_dir(project_root),
        event_type="suppression_validation_executed",
        stage="detect",
        approved=True,
        details={
            "project_root": project_root.resolve(strict=False),
            "config_path": config_path.resolve(strict=False),
            "findings": None if findings is None else findings.resolve(strict=False),
            "summary": lifecycle.model_dump(mode="json"),
            "policy": payload["policy"],
        },
    )
    if json_output:
        typer.echo(json.dumps(payload, indent=2))
    else:
        typer.echo(f"Suppression validation: {validation.path}")
        typer.echo(
            "Rules: "
            f"{lifecycle.total_rules} total, "
            f"{lifecycle.active_rules} active, "
            f"{lifecycle.expired_rules} expired, "
            f"{lifecycle.invalid_rules} invalid"
        )
        if lifecycle.stale_evaluated:
            typer.echo(f"Stale rules: {lifecycle.stale_rules}")
        else:
            typer.echo("Stale rules: not evaluated (pass --findings detect.json)")
        if lifecycle.expired_selectors:
            typer.echo(f"Expired selectors: {' | '.join(lifecycle.expired_selectors)}")
        if lifecycle.stale_selectors:
            typer.echo(f"Stale selectors: {' | '.join(lifecycle.stale_selectors)}")
        if lifecycle.invalid_entries:
            typer.echo(f"Invalid entries: {' | '.join(lifecycle.invalid_entries)}")

    fail_on_invalid = config_model.suppression.fail_on_invalid and lifecycle.invalid_rules > 0
    fail_on_expired = config_model.suppression.fail_on_expired and lifecycle.expired_rules > 0
    fail_on_stale = (
        config_model.suppression.fail_on_stale
        and lifecycle.stale_evaluated
        and lifecycle.stale_rules > 0
    )
    if fail_on_invalid or fail_on_expired or fail_on_stale:
        raise typer.Exit(code=1)


_INTEL_TOOLS = {"sarif", "codeql_sarif", "semgrep", "trivy", "zap"}
_INTEL_TRUST_LEVELS = {"verified", "trusted", "untrusted"}


def _resolve_intel_tool(raw_value: str) -> str:
    candidate = raw_value.strip().lower()
    if candidate not in _INTEL_TOOLS:
        supported = ", ".join(sorted(_INTEL_TOOLS))
        raise ValueError(f"unsupported intel tool '{raw_value}'. Supported values: {supported}")
    return candidate


def _resolve_intel_trust_level(raw_value: str) -> str:
    candidate = raw_value.strip().lower()
    if candidate not in _INTEL_TRUST_LEVELS:
        supported = ", ".join(sorted(_INTEL_TRUST_LEVELS))
        raise ValueError(
            f"unsupported intel trust level '{raw_value}'. Supported values: {supported}"
        )
    return candidate


def _load_normalization_bundle(path: Path) -> NormalizationBundle:
    try:
        return NormalizationBundle.model_validate_json(path.read_text(encoding="utf-8"))
    except (OSError, ValidationError) as exc:
        raise ValueError(f"invalid normalization bundle at {path}: {exc}") from exc


@intel_app.command("normalize")
def intel_normalize(
    input_path: Annotated[
        Path,
        typer.Argument(help="Path to external snapshot JSON file."),
    ],
    tool: Annotated[
        str,
        typer.Option(
            "--tool",
            help="External tool type (sarif, codeql_sarif, semgrep, trivy, zap).",
        ),
    ],
    output: Annotated[
        Path,
        typer.Option("--output", "-o", help="Destination JSON file for normalized bundle."),
    ],
    source_name: Annotated[
        str,
        typer.Option("--source-name", help="Human-readable source label."),
    ] = "external-snapshot",
    trust_level: Annotated[
        str,
        typer.Option("--trust-level", help="Source trust level (verified, trusted, untrusted)."),
    ] = "trusted",
    stale_after_hours: Annotated[
        int,
        typer.Option("--stale-after-hours", min=1, help="Staleness horizon in hours."),
    ] = 168,
    collected_at: Annotated[
        str | None,
        typer.Option(
            "--collected-at",
            help="Optional source collection timestamp in ISO-8601 format.",
        ),
    ] = None,
) -> None:
    if not input_path.exists():
        typer.echo(f"error: snapshot file not found: {input_path}")
        raise typer.Exit(code=2)

    try:
        tool_key = _resolve_intel_tool(tool)
        trust_key = _resolve_intel_trust_level(trust_level)
    except ValueError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=2) from exc

    parse_result = parse_external_tool_file(
        tool=cast(Any, tool_key),
        input_path=input_path,
    )
    source = IntelSourceProvenance.from_snapshot(
        source_name=source_name,
        tool=cast(Any, tool_key),
        snapshot_path=input_path,
        trust_level=cast(Any, trust_key),
        stale_after_hours=stale_after_hours,
        collected_at=collected_at,
    )
    bundle = normalize_adapter_result(parse_result=parse_result, source=source)

    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(bundle.model_dump_json(indent=2), encoding="utf-8")
    _write_audit_event(
        output_dir=output.parent,
        event_type="intel_snapshot_normalized",
        stage="intel",
        approved=True,
        details={
            "input_path": input_path.resolve(strict=False),
            "output_path": output.resolve(strict=False),
            "tool": tool_key,
            "source_name": source_name,
            "finding_count": len(bundle.findings),
            "diagnostics": list(bundle.diagnostics),
        },
    )
    typer.echo(
        f"normalized {len(bundle.findings)} finding(s) from {tool_key} snapshot to {output}"
    )


@intel_app.command("graph")
def intel_graph(
    normalized_bundle: Annotated[
        Path,
        typer.Option("--normalized", help="Normalization bundle JSON path."),
    ],
    output: Annotated[
        Path,
        typer.Option("--output", "-o", help="Destination graph snapshot JSON path."),
    ],
) -> None:
    try:
        bundle = _load_normalization_bundle(normalized_bundle)
    except ValueError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=2) from exc

    graph = build_graph_from_enrichment(
        source_name=bundle.source.source_name,
        findings=list(bundle.findings),
    )
    errors = graph.validate_edges()
    if errors:
        typer.echo("error: graph validation failed:")
        for error in errors:
            typer.echo(f"- {error}")
        raise typer.Exit(code=2)

    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(graph.model_dump_json(indent=2), encoding="utf-8")
    _write_audit_event(
        output_dir=output.parent,
        event_type="intel_graph_built",
        stage="intel",
        approved=True,
        details={
            "normalized_bundle": normalized_bundle.resolve(strict=False),
            "output_path": output.resolve(strict=False),
            "node_count": len(graph.nodes),
            "edge_count": len(graph.edges),
        },
    )
    typer.echo(f"wrote intelligence graph with {len(graph.nodes)} node(s) to {output}")


@intel_app.command("summary")
def intel_summary(
    normalized_bundle: Annotated[
        Path,
        typer.Option("--normalized", help="Normalization bundle JSON path."),
    ],
    output: Annotated[
        Path | None,
        typer.Option("--output", "-o", help="Optional output JSON path."),
    ] = None,
) -> None:
    try:
        bundle = _load_normalization_bundle(normalized_bundle)
    except ValueError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=2) from exc

    summary = build_enrichment_summary(bundle)
    payload = summary.model_dump_json(indent=2)
    if output is None:
        typer.echo(payload)
        return

    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(payload, encoding="utf-8")
    _write_audit_event(
        output_dir=output.parent,
        event_type="intel_summary_generated",
        stage="intel",
        approved=True,
        details={
            "normalized_bundle": normalized_bundle.resolve(strict=False),
            "output_path": output.resolve(strict=False),
            "findings_total": summary.findings_total,
        },
    )
    typer.echo(f"wrote enrichment summary to {output}")


@app.command("diff", hidden=True)
def diff_command(
    baseline_path: ComparisonTargetArg,
    current_path: ComparisonTargetArg,
    fail_on_new: FailOnNewOption = False,
    fail_on_new_severity: FailOnNewSeverityOption = None,
    format: BaselineDiffFormatOption = BaselineDiffFormat.TEXT,
) -> None:
    diff_result = _print_diff(
        baseline_path,
        current_path,
        output_format=format,
    )
    if fail_on_new is True and new_findings_at_or_above(
        diff_result,
        minimum_severity=(
            FailSeverity.LOW.value
            if fail_on_new_severity is None
            else fail_on_new_severity.value
        ),
    ):
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
        loaded_rules = load_rules(path)
    except RuleValidationError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=1) from exc

    if not loaded_rules:
        typer.echo(f"error: no custom rules found in {path}")
        raise typer.Exit(code=1)

    compiled_rules = []
    validation_errors: list[str] = []
    for rule in loaded_rules:
        try:
            compiled_rules.append(compile_rule(rule))
        except RuleValidationError as exc:
            validation_errors.append(str(exc))

    if validation_errors:
        typer.echo("error: rule validation failed")
        for message in validation_errors:
            typer.echo(f"- {message}")
        raise typer.Exit(code=1)

    typer.echo(f"validated {len(compiled_rules)} rule(s)")
    for rule in compiled_rules:
        typer.echo(f"{rule.id} [{rule.kind}] {rule.cwe_id} severity={rule.severity}")


@rules_app.command("scaffold")
def rules_scaffold(
    name: Annotated[str, typer.Argument(help="Rule pack name or identifier.")],
    output: Annotated[
        Path,
        typer.Option("--output", help="Directory where the rule pack scaffold is created."),
    ] = Path("./rules"),
) -> None:
    slug = _slugify_rule_pack_name(name)
    if not slug:
        typer.echo("error: rule pack name must contain at least one alphanumeric character")
        raise typer.Exit(code=1)

    pack_dir = output / slug
    if pack_dir.exists() and any(pack_dir.iterdir()):
        typer.echo(f"error: destination already exists and is not empty: {pack_dir}")
        raise typer.Exit(code=1)

    rules_dir = pack_dir / "rules"
    fixtures_dir = pack_dir / "tests" / "fixtures"
    rules_dir.mkdir(parents=True, exist_ok=True)
    fixtures_dir.mkdir(parents=True, exist_ok=True)

    rule_file = rules_dir / f"{slug}.toml"
    vulnerable_fixture = fixtures_dir / "vulnerable.ts"
    safe_fixture = fixtures_dir / "safe.ts"

    rule_file.write_text(_rule_scaffold_template(slug), encoding="utf-8")
    vulnerable_fixture.write_text(
        "export function vulnerable(req: any, db: any) {\n"
        "  db.query(req.query.id);\n"
        "}\n",
        encoding="utf-8",
    )
    safe_fixture.write_text(
        "export function safe(req: any, db: any) {\n"
        "  const id = Number.parseInt(String(req.query.id), 10);\n"
        "  db.query(id);\n"
        "}\n",
        encoding="utf-8",
    )

    typer.echo(f"created rule pack scaffold at {pack_dir}")
    typer.echo(f"- {rule_file}")
    typer.echo(f"- {vulnerable_fixture}")
    typer.echo(f"- {safe_fixture}")
    typer.echo(f"next: piranesi rules validate {rules_dir}")
    typer.echo(f"next: piranesi rules test-all --rules-dir {rules_dir}")


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


@advisory_app.command("sign-snapshot")
def advisory_sign_snapshot(
    snapshot_db: Annotated[
        Path,
        typer.Argument(help="Path to advisory DB snapshot file to sign."),
    ],
    manifest: Annotated[
        Path | None,
        typer.Option(
            "--manifest",
            help="Output path for detached snapshot manifest JSON.",
        ),
    ] = None,
    key_file: Annotated[
        Path | None,
        typer.Option(
            "--key-file",
            help="Shared trust key file used for HMAC signature generation.",
        ),
    ] = None,
    signer: Annotated[
        str | None,
        typer.Option("--signer", help="Signer label recorded in the manifest."),
    ] = None,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Emit machine-readable JSON."),
    ] = False,
) -> None:
    from piranesi.advisory import load_trust_key, write_snapshot_manifest

    snapshot_path = snapshot_db.expanduser().resolve(strict=False)
    if not snapshot_path.is_file():
        typer.echo(f"error: advisory DB snapshot not found: {snapshot_path}")
        raise typer.Exit(code=1)

    manifest_path = (
        manifest.expanduser().resolve(strict=False)
        if manifest is not None
        else snapshot_path.with_suffix(snapshot_path.suffix + ".manifest.json")
    )
    signing_key = None
    if key_file is not None:
        try:
            signing_key = load_trust_key(key_file.expanduser().resolve(strict=False))
        except (OSError, ValueError) as exc:
            typer.echo(f"error: failed to read trust key: {exc}")
            raise typer.Exit(code=1) from exc

    manifest_obj = write_snapshot_manifest(
        snapshot_path,
        manifest_path,
        signing_key=signing_key,
        signer=signer,
    )
    payload = {
        "snapshot_path": str(snapshot_path),
        "manifest_path": str(manifest_path),
        "snapshot_sha256": manifest_obj.snapshot_sha256,
        "file_size_bytes": manifest_obj.file_size_bytes,
        "signature": (
            None
            if manifest_obj.signature is None
            else {
                "scheme": manifest_obj.signature.scheme,
                "signer": manifest_obj.signature.signer,
                "value": manifest_obj.signature.value,
            }
        ),
    }
    if json_output:
        typer.echo(json.dumps(payload, indent=2))
        return
    typer.echo(f"snapshot: {snapshot_path}")
    typer.echo(f"manifest: {manifest_path}")
    typer.echo(f"snapshot_sha256: {manifest_obj.snapshot_sha256}")
    typer.echo("signed: yes" if manifest_obj.signature is not None else "signed: no")


@advisory_app.command("status")
def advisory_status(
    project_root: Annotated[
        Path,
        typer.Option("--project-root", help=_ADVISORY_PROJECT_ROOT_HELP),
    ] = Path("."),
    db: Annotated[
        Path | None,
        typer.Option("--db", help="Explicit advisory DB file path."),
    ] = None,
    stale_after_days: Annotated[
        int,
        typer.Option("--stale-after-days", help="Freshness warning threshold in days."),
    ] = 14,
    trust_policy: Annotated[
        AdvisoryTrustPolicy,
        typer.Option(
            "--trust-policy",
            help="Advisory trust policy mode.",
            case_sensitive=False,
        ),
    ] = AdvisoryTrustPolicy.PERMISSIVE,
    on_missing: Annotated[
        AdvisoryPolicyAction,
        typer.Option("--on-missing", help="Policy action when advisory DB is missing."),
    ] = AdvisoryPolicyAction.WARN,
    on_stale: Annotated[
        AdvisoryPolicyAction,
        typer.Option("--on-stale", help="Policy action when advisory DB is stale."),
    ] = AdvisoryPolicyAction.WARN,
    on_unsigned: Annotated[
        AdvisoryPolicyAction,
        typer.Option(
            "--on-unsigned",
            help="Policy action when advisory snapshot trust_state is unsigned/unverified.",
        ),
    ] = AdvisoryPolicyAction.WARN,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Emit machine-readable JSON."),
    ] = False,
) -> None:
    from piranesi.advisory import get_advisory_db_status

    db_path = _resolve_advisory_db_path(project_root, db)
    status = get_advisory_db_status(db_path, stale_after_days=stale_after_days)
    policy_outcome = _enforce_advisory_policy(
        status=status,
        trust_policy=trust_policy,
        on_missing=on_missing,
        on_stale=on_stale,
        on_unsigned=on_unsigned,
    )
    payload = _advisory_status_payload(status)
    payload["policy"] = _advisory_policy_payload(policy_outcome)

    if json_output:
        typer.echo(json.dumps(payload, indent=2))
        if not policy_outcome.allowed:
            raise typer.Exit(code=1)
        return

    typer.echo(f"path: {payload['path']}")
    typer.echo(f"exists: {payload['exists']}")
    typer.echo(f"schema_version: {payload['schema_version']}")
    typer.echo(f"advisories: {payload['advisory_count']}")
    typer.echo(f"affected_packages: {payload['affected_package_count']}")
    typer.echo(f"sources: {', '.join(payload['sources']) if payload['sources'] else 'none'}")
    typer.echo(f"last_updated: {payload['last_updated'] or 'unknown'}")
    typer.echo(f"checksum_sha256: {payload['checksum_sha256'] or 'n/a'}")
    typer.echo(f"freshness: {payload['freshness']}")
    typer.echo(f"trust_state: {payload['trust_state']}")
    if payload["age_days"] is not None:
        typer.echo(f"age_days: {payload['age_days']:.2f}")
    if payload["warnings"]:
        typer.echo("warnings:")
        for warning in payload["warnings"]:
            typer.echo(f"- {warning}")
    if policy_outcome.warnings:
        typer.echo("policy_warnings:")
        for warning in policy_outcome.warnings:
            typer.echo(f"- {warning}")
    if policy_outcome.violations:
        typer.echo("policy_violations:")
        for violation in policy_outcome.violations:
            typer.echo(f"- {violation}")
        raise typer.Exit(code=1)


@advisory_app.command("update")
def advisory_update(
    project_root: Annotated[
        Path,
        typer.Option("--project-root", help=_ADVISORY_PROJECT_ROOT_HELP),
    ] = Path("."),
    db: Annotated[
        Path | None,
        typer.Option("--db", help="Explicit advisory DB file path."),
    ] = None,
    source: Annotated[
        list[str] | None,
        typer.Option("--source", help="Advisory source to sync (repeatable)."),
    ] = None,
    full: Annotated[
        bool,
        typer.Option("--full", help="Force full sync instead of incremental cursor-based sync."),
    ] = False,
    ecosystem: Annotated[
        list[str] | None,
        typer.Option("--ecosystem", help="Restrict sync to ecosystem(s), e.g. npm, pypi."),
    ] = None,
    github_token: Annotated[
        str | None,
        typer.Option("--github-token", envvar="GITHUB_TOKEN", help="GitHub token for GHSA sync."),
    ] = None,
    nvd_api_key: Annotated[
        str | None,
        typer.Option("--nvd-api-key", envvar="NVD_API_KEY", help="NVD API key for NVD sync."),
    ] = None,
    stale_after_days: Annotated[
        int,
        typer.Option("--stale-after-days", help="Freshness warning threshold in days."),
    ] = 14,
    trust_policy: Annotated[
        AdvisoryTrustPolicy,
        typer.Option(
            "--trust-policy",
            help="Advisory trust policy mode.",
            case_sensitive=False,
        ),
    ] = AdvisoryTrustPolicy.PERMISSIVE,
    on_missing: Annotated[
        AdvisoryPolicyAction,
        typer.Option("--on-missing", help="Policy action when advisory DB is missing."),
    ] = AdvisoryPolicyAction.WARN,
    on_stale: Annotated[
        AdvisoryPolicyAction,
        typer.Option("--on-stale", help="Policy action when advisory DB is stale."),
    ] = AdvisoryPolicyAction.WARN,
    on_unsigned: Annotated[
        AdvisoryPolicyAction,
        typer.Option(
            "--on-unsigned",
            help="Policy action when advisory snapshot trust_state is unsigned/unverified.",
        ),
    ] = AdvisoryPolicyAction.WARN,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Emit machine-readable JSON."),
    ] = False,
) -> None:
    from piranesi.advisory import AdvisoryDB, get_advisory_db_status, sync_advisories

    db_path = _resolve_advisory_db_path(project_root, db)
    sources = tuple(source) if source else ("osv", "ghsa", "nvd", "go_vuln")
    ecosystems = tuple(ecosystem) if ecosystem else None
    try:
        with AdvisoryDB(db_path) as advisory_db:
            result = sync_advisories(
                advisory_db,
                sources=sources,
                full=full,
                ecosystems=ecosystems,
                github_token=github_token,
                nvd_api_key=nvd_api_key,
            )
    except Exception as exc:  # pragma: no cover - defensive against network stack/runtime errors.
        typer.echo(f"error: advisory update failed: {exc}")
        raise typer.Exit(code=1) from exc

    status = get_advisory_db_status(db_path, stale_after_days=stale_after_days)
    policy_outcome = _enforce_advisory_policy(
        status=status,
        trust_policy=trust_policy,
        on_missing=on_missing,
        on_stale=on_stale,
        on_unsigned=on_unsigned,
    )
    payload = {
        "db": _advisory_status_payload(status),
        "sync": {
            "sources": dict(result.source_counts),
            "total_upserted": result.total_upserted,
            "epss_updated": result.epss_updated,
            "exploit_updated": result.exploit_updated,
        },
        "policy": _advisory_policy_payload(policy_outcome),
    }
    if json_output:
        typer.echo(json.dumps(payload, indent=2))
        if not policy_outcome.allowed:
            raise typer.Exit(code=1)
        return

    typer.echo(f"synced sources: {', '.join(sources)}")
    typer.echo(f"upserted advisories: {result.total_upserted}")
    typer.echo(f"epss updates: {result.epss_updated}")
    typer.echo(f"exploit updates: {result.exploit_updated}")
    typer.echo(f"db freshness: {status.freshness}")
    if policy_outcome.warnings:
        typer.echo("policy_warnings:")
        for warning in policy_outcome.warnings:
            typer.echo(f"- {warning}")
    if policy_outcome.violations:
        typer.echo("policy_violations:")
        for violation in policy_outcome.violations:
            typer.echo(f"- {violation}")
        raise typer.Exit(code=1)
    if status.warnings:
        typer.echo("warnings:")
        for warning in status.warnings:
            typer.echo(f"- {warning}")


@advisory_app.command("import")
def advisory_import(
    source_db: Annotated[
        Path,
        typer.Argument(help="Path to advisory DB file to import."),
    ],
    project_root: Annotated[
        Path,
        typer.Option("--project-root", help=_ADVISORY_PROJECT_ROOT_HELP),
    ] = Path("."),
    db: Annotated[
        Path | None,
        typer.Option("--db", help="Explicit advisory DB file path."),
    ] = None,
    merge: Annotated[
        bool,
        typer.Option("--merge", help="Merge source DB into destination instead of replacing it."),
    ] = False,
    manifest: Annotated[
        Path | None,
        typer.Option(
            "--manifest",
            help="Detached snapshot manifest for source DB verification.",
        ),
    ] = None,
    trust_key: Annotated[
        Path | None,
        typer.Option(
            "--trust-key",
            help="Shared trust key used to verify manifest signatures.",
        ),
    ] = None,
    require_manifest: Annotated[
        bool,
        typer.Option(
            "--require-manifest",
            help="Fail when --manifest is not provided.",
        ),
    ] = False,
    require_verified_snapshot: Annotated[
        bool,
        typer.Option(
            "--require-verified-snapshot",
            help="Fail unless snapshot signature verifies successfully.",
        ),
    ] = False,
    stale_after_days: Annotated[
        int,
        typer.Option("--stale-after-days", help="Freshness warning threshold in days."),
    ] = 14,
    trust_policy: Annotated[
        AdvisoryTrustPolicy,
        typer.Option(
            "--trust-policy",
            help="Advisory trust policy mode.",
            case_sensitive=False,
        ),
    ] = AdvisoryTrustPolicy.PERMISSIVE,
    on_missing: Annotated[
        AdvisoryPolicyAction,
        typer.Option("--on-missing", help="Policy action when advisory DB is missing."),
    ] = AdvisoryPolicyAction.WARN,
    on_stale: Annotated[
        AdvisoryPolicyAction,
        typer.Option("--on-stale", help="Policy action when advisory DB is stale."),
    ] = AdvisoryPolicyAction.WARN,
    on_unsigned: Annotated[
        AdvisoryPolicyAction,
        typer.Option(
            "--on-unsigned",
            help="Policy action when advisory snapshot trust_state is unsigned/unverified.",
        ),
    ] = AdvisoryPolicyAction.WARN,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Emit machine-readable JSON."),
    ] = False,
) -> None:
    from piranesi.advisory import (
        AdvisoryDB,
        AdvisorySnapshotProvenance,
        get_advisory_db_status,
        load_trust_key,
        verify_snapshot_manifest,
    )
    from piranesi.advisory.db import utc_now
    from piranesi.advisory.trust import SnapshotVerificationResult, compute_sha256

    source_path = source_db.expanduser().resolve(strict=False)
    if not source_path.is_file():
        typer.echo(f"error: advisory DB source not found: {source_path}")
        raise typer.Exit(code=1)
    if require_manifest and manifest is None:
        typer.echo("error: --require-manifest was set but --manifest was not provided")
        raise typer.Exit(code=1)

    manifest_path = (
        manifest.expanduser().resolve(strict=False) if manifest is not None else None
    )
    verification_key: bytes | None = None
    if trust_key is not None:
        try:
            verification_key = load_trust_key(trust_key.expanduser().resolve(strict=False))
        except (OSError, ValueError) as exc:
            typer.echo(f"error: failed to read trust key: {exc}")
            raise typer.Exit(code=1) from exc

    verification_result = None
    if manifest_path is not None:
        try:
            verification_result = verify_snapshot_manifest(
                source_path,
                manifest_path,
                verification_key=verification_key,
            )
        except (OSError, ValueError, json.JSONDecodeError) as exc:
            typer.echo(f"error: snapshot manifest verification failed: {exc}")
            raise typer.Exit(code=1) from exc
    else:
        verification_result = SnapshotVerificationResult(
            verified=False,
            has_signature=False,
            tampered=False,
            reason="manifest not provided",
            snapshot_sha256=compute_sha256(source_path),
            manifest_sha256=None,
            signature_scheme=None,
            signature_signer=None,
            signature_value=None,
        )

    if require_verified_snapshot and (
        verification_result is None or not verification_result.verified
    ):
        reason = "unknown reason" if verification_result is None else verification_result.reason
        typer.echo(
            "error: snapshot verification policy failed: "
            f"require_verified_snapshot=true but verification did not pass ({reason})"
        )
        raise typer.Exit(code=1)

    db_path = _resolve_advisory_db_path(project_root, db)
    provenance = AdvisorySnapshotProvenance(
        source_path=str(source_path),
        snapshot_sha256=verification_result.snapshot_sha256 if verification_result else None,
        manifest_path=str(manifest_path) if manifest_path is not None else None,
        manifest_sha256=verification_result.manifest_sha256 if verification_result else None,
        signature_scheme=verification_result.signature_scheme if verification_result else None,
        signature_signer=verification_result.signature_signer if verification_result else None,
        signature_value=verification_result.signature_value if verification_result else None,
        verified=bool(verification_result and verification_result.verified),
        verification_reason=(
            None if verification_result is None else verification_result.reason
        ),
        imported_at=utc_now(),
    )
    with AdvisoryDB(db_path) as advisory_db:
        advisory_db.import_from(source_path, merge=merge, provenance=provenance)

    status = get_advisory_db_status(db_path, stale_after_days=stale_after_days)
    policy_outcome = _enforce_advisory_policy(
        status=status,
        trust_policy=trust_policy,
        on_missing=on_missing,
        on_stale=on_stale,
        on_unsigned=on_unsigned,
    )
    payload = _advisory_status_payload(status)
    payload["verification"] = {
        "manifest_path": str(manifest_path) if manifest_path is not None else None,
        "verified": None if verification_result is None else verification_result.verified,
        "has_signature": (
            None if verification_result is None else verification_result.has_signature
        ),
        "tampered": None if verification_result is None else verification_result.tampered,
        "reason": None if verification_result is None else verification_result.reason,
    }
    payload["policy"] = _advisory_policy_payload(policy_outcome)
    if json_output:
        typer.echo(json.dumps(payload, indent=2))
        if not policy_outcome.allowed:
            raise typer.Exit(code=1)
        return

    mode = "merged" if merge else "replaced"
    typer.echo(f"{mode} advisory DB from {source_path} into {db_path}")
    typer.echo(f"advisories: {status.advisory_count}")
    typer.echo(f"freshness: {status.freshness}")
    if policy_outcome.warnings:
        typer.echo("policy_warnings:")
        for warning in policy_outcome.warnings:
            typer.echo(f"- {warning}")
    if policy_outcome.violations:
        typer.echo("policy_violations:")
        for violation in policy_outcome.violations:
            typer.echo(f"- {violation}")
        raise typer.Exit(code=1)
    if status.warnings:
        typer.echo("warnings:")
        for warning in status.warnings:
            typer.echo(f"- {warning}")


@advisory_app.command("search")
def advisory_search(
    project_root: Annotated[
        Path,
        typer.Option("--project-root", help=_ADVISORY_PROJECT_ROOT_HELP),
    ] = Path("."),
    db: Annotated[
        Path | None,
        typer.Option("--db", help="Explicit advisory DB file path."),
    ] = None,
    query: Annotated[
        str | None,
        typer.Option("--query", help="Search text across advisory IDs, titles, and descriptions."),
    ] = None,
    ecosystem: Annotated[
        str | None,
        typer.Option("--ecosystem", help="Filter by ecosystem, e.g. npm, pypi, go."),
    ] = None,
    package: Annotated[
        str | None,
        typer.Option("--package", help="Filter by package name."),
    ] = None,
    limit: Annotated[
        int,
        typer.Option("--limit", help="Maximum advisory results to return."),
    ] = 20,
    stale_after_days: Annotated[
        int,
        typer.Option("--stale-after-days", help="Freshness warning threshold in days."),
    ] = 14,
    trust_policy: Annotated[
        AdvisoryTrustPolicy,
        typer.Option(
            "--trust-policy",
            help="Advisory trust policy mode.",
            case_sensitive=False,
        ),
    ] = AdvisoryTrustPolicy.PERMISSIVE,
    on_missing: Annotated[
        AdvisoryPolicyAction,
        typer.Option("--on-missing", help="Policy action when advisory DB is missing."),
    ] = AdvisoryPolicyAction.WARN,
    on_stale: Annotated[
        AdvisoryPolicyAction,
        typer.Option("--on-stale", help="Policy action when advisory DB is stale."),
    ] = AdvisoryPolicyAction.WARN,
    on_unsigned: Annotated[
        AdvisoryPolicyAction,
        typer.Option(
            "--on-unsigned",
            help="Policy action when advisory snapshot trust_state is unsigned/unverified.",
        ),
    ] = AdvisoryPolicyAction.WARN,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Emit machine-readable JSON."),
    ] = False,
) -> None:
    from piranesi.advisory import AdvisoryDB, get_advisory_db_status

    db_path = _resolve_advisory_db_path(project_root, db)
    status = get_advisory_db_status(db_path, stale_after_days=stale_after_days)
    if not status.exists:
        typer.echo(f"error: advisory database not found at {db_path}")
        raise typer.Exit(code=1)
    policy_outcome = _enforce_advisory_policy(
        status=status,
        trust_policy=trust_policy,
        on_missing=on_missing,
        on_stale=on_stale,
        on_unsigned=on_unsigned,
    )

    with AdvisoryDB(db_path) as advisory_db:
        rows = advisory_db.search_advisories(
            query=query,
            ecosystem=ecosystem,
            package_name=package,
            limit=limit,
        )

    if json_output:
        payload = {
            "status": _advisory_status_payload(status),
            "policy": _advisory_policy_payload(policy_outcome),
            "count": len(rows),
            "results": [
                {
                    "advisory_id": row.advisory.advisory_id,
                    "cve_id": row.advisory.cve_id,
                    "ghsa_id": row.advisory.ghsa_id,
                    "severity": row.advisory.severity,
                    "title": row.advisory.title,
                    "sources": list(row.advisory.sources),
                    "fix_version": row.advisory.fix_version,
                    "packages": [
                        {"ecosystem": pkg.ecosystem, "name": pkg.name}
                        for pkg in row.advisory.affected_packages
                    ],
                }
                for row in rows
            ],
        }
        typer.echo(json.dumps(payload, indent=2))
        if not policy_outcome.allowed:
            raise typer.Exit(code=1)
        return

    typer.echo(f"results: {len(rows)}")
    for row in rows:
        advisory = row.advisory
        packages = ", ".join(
            f"{pkg.ecosystem}:{pkg.name}" for pkg in advisory.affected_packages[:3]
        )
        if len(advisory.affected_packages) > 3:
            packages += ", ..."
        typer.echo(
            f"- {advisory.advisory_id} severity={advisory.severity} "
            f"fix={advisory.fix_version or 'unknown'} packages={packages or 'n/a'}"
        )
        typer.echo(f"  {advisory.title}")

    if status.warnings:
        typer.echo("warnings:")
        for warning in status.warnings:
            typer.echo(f"- {warning}")
    if policy_outcome.warnings:
        typer.echo("policy_warnings:")
        for warning in policy_outcome.warnings:
            typer.echo(f"- {warning}")
    if policy_outcome.violations:
        typer.echo("policy_violations:")
        for violation in policy_outcome.violations:
            typer.echo(f"- {violation}")
        raise typer.Exit(code=1)


def _run_eval_entrypoint(entrypoint: str, argv: list[str]) -> None:
    try:
        if entrypoint == "audit":
            from eval.ground_truth_audit import main as eval_main
        elif entrypoint == "enrich_ground_truth":
            from eval.ground_truth_enrich import main as eval_main
        elif entrypoint == "coverage_gaps":
            from eval.coverage_gap_report import main as eval_main
        elif entrypoint == "validate_all":
            from eval.validate_all import main as eval_main
        elif entrypoint == "compare_reports":
            from eval.compare_reports import main as eval_main
        else:  # pragma: no cover - defensive; entrypoint is fixed by command handlers.
            raise ValueError(f"unknown eval entrypoint: {entrypoint}")
    except ModuleNotFoundError as exc:
        typer.echo(
            "error: evaluation harness modules are unavailable. "
            "Use a source checkout that includes the eval/ directory."
        )
        raise typer.Exit(code=1) from exc
    except ValueError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=1) from exc

    try:
        exit_code = eval_main(argv)
    except ValueError as exc:
        typer.echo(f"error: {exc}")
        raise typer.Exit(code=1) from exc
    if exit_code != 0:
        raise typer.Exit(code=exit_code)


def _append_repeatable_flags(argv: list[str], flag: str, values: list[str]) -> None:
    for value in values:
        argv.extend([flag, value])


@eval_app.command("audit")
def eval_audit(
    gt_dir: Annotated[
        Path,
        typer.Option("--gt-dir", help="Ground-truth directory."),
    ] = Path("eval/ground_truth"),
    field: Annotated[
        list[str] | None,
        typer.Option("--field", help="Field to audit (repeatable)."),
    ] = None,
    required_field: Annotated[
        list[str] | None,
        typer.Option("--required-field", help="Field required to be present (repeatable)."),
    ] = None,
    filter_by: Annotated[
        list[str] | None,
        typer.Option("--filter", help="Filter entries by key=value (repeatable)."),
    ] = None,
    show_missing_limit: Annotated[
        int,
        typer.Option("--show-missing-limit", help="Maximum missing IDs to include per field."),
    ] = 10,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Emit machine-readable JSON."),
    ] = False,
    fail_on_missing: Annotated[
        bool,
        typer.Option("--fail-on-missing", help="Exit non-zero when required fields are missing."),
    ] = False,
) -> None:
    argv = [
        "--gt-dir",
        str(gt_dir),
        "--show-missing-limit",
        str(show_missing_limit),
    ]
    _append_repeatable_flags(argv, "--field", field or [])
    _append_repeatable_flags(argv, "--required-field", required_field or [])
    _append_repeatable_flags(argv, "--filter", filter_by or [])
    if json_output:
        argv.append("--json")
    if fail_on_missing:
        argv.append("--fail-on-missing")
    _run_eval_entrypoint("audit", argv)


@eval_app.command("validate-all")
def eval_validate_all(
    gt_dir: Annotated[
        Path,
        typer.Option("--gt-dir", help="Ground-truth directory."),
    ] = Path("eval/ground_truth"),
    fixtures_dir: Annotated[
        Path | None,
        typer.Option("--fixtures-dir", help="Optional base directory for relative fixture paths."),
    ] = None,
    output: Annotated[
        Path | None,
        typer.Option("--output", help="Write report to JSON."),
    ] = None,
    baseline_report: Annotated[
        Path | None,
        typer.Option("--baseline-report", help="Previous report JSON for delta comparisons."),
    ] = None,
    history_dir: Annotated[
        Path,
        typer.Option("--history-dir", help="Directory for validate_all history snapshots."),
    ] = Path("eval/history"),
    history_label: Annotated[
        str | None,
        typer.Option("--history-label", help="Optional label suffix for history snapshots."),
    ] = None,
    filter_by: Annotated[
        list[str] | None,
        typer.Option("--filter", help="Filter entries by key=value (repeatable)."),
    ] = None,
    group_by: Annotated[
        list[str] | None,
        typer.Option("--group-by", help="Group metric breakdown key (repeatable)."),
    ] = None,
    min_detection_rate: Annotated[
        float | None,
        typer.Option("--min-detection-rate", help="Minimum overall TP detection rate."),
    ] = None,
    min_fp_rate: Annotated[
        float | None,
        typer.Option("--min-fp-rate", help="Minimum overall FP suppression rate."),
    ] = None,
    min_detection_rate_delta: Annotated[
        float | None,
        typer.Option("--min-detection-rate-delta", help="Minimum overall detection-rate delta."),
    ] = None,
    min_fp_rate_delta: Annotated[
        float | None,
        typer.Option("--min-fp-rate-delta", help="Minimum overall FP-suppression-rate delta."),
    ] = None,
    min_group_detection_rate: Annotated[
        list[str] | None,
        typer.Option(
            "--min-group-detection-rate",
            help="Per-group detection threshold group=value:rate (repeatable).",
        ),
    ] = None,
    min_group_fp_rate: Annotated[
        list[str] | None,
        typer.Option(
            "--min-group-fp-rate",
            help="Per-group FP suppression threshold group=value:rate (repeatable).",
        ),
    ] = None,
    min_group_detection_delta: Annotated[
        list[str] | None,
        typer.Option(
            "--min-group-detection-delta",
            help="Per-group detection delta threshold group=value:delta (repeatable).",
        ),
    ] = None,
    min_group_fp_delta: Annotated[
        list[str] | None,
        typer.Option(
            "--min-group-fp-delta",
            help="Per-group FP suppression delta threshold group=value:delta (repeatable).",
        ),
    ] = None,
    keep_output: Annotated[
        bool,
        typer.Option("--keep-output", help="Keep per-fixture stage artifacts."),
    ] = False,
    no_history: Annotated[
        bool,
        typer.Option("--no-history", help="Disable history snapshot writing."),
    ] = False,
    verbose: Annotated[
        bool,
        typer.Option("--verbose", help="Stream Piranesi output while scanning."),
    ] = False,
) -> None:
    argv = ["--gt-dir", str(gt_dir), "--history-dir", str(history_dir)]
    if fixtures_dir is not None:
        argv.extend(["--fixtures-dir", str(fixtures_dir)])
    if output is not None:
        argv.extend(["--output", str(output)])
    if baseline_report is not None:
        argv.extend(["--baseline-report", str(baseline_report)])
    if history_label is not None:
        argv.extend(["--history-label", history_label])
    if min_detection_rate is not None:
        argv.extend(["--min-detection-rate", str(min_detection_rate)])
    if min_fp_rate is not None:
        argv.extend(["--min-fp-rate", str(min_fp_rate)])
    if min_detection_rate_delta is not None:
        argv.extend(["--min-detection-rate-delta", str(min_detection_rate_delta)])
    if min_fp_rate_delta is not None:
        argv.extend(["--min-fp-rate-delta", str(min_fp_rate_delta)])
    _append_repeatable_flags(argv, "--filter", filter_by or [])
    _append_repeatable_flags(argv, "--group-by", group_by or [])
    _append_repeatable_flags(argv, "--min-group-detection-rate", min_group_detection_rate or [])
    _append_repeatable_flags(argv, "--min-group-fp-rate", min_group_fp_rate or [])
    _append_repeatable_flags(argv, "--min-group-detection-delta", min_group_detection_delta or [])
    _append_repeatable_flags(argv, "--min-group-fp-delta", min_group_fp_delta or [])
    if keep_output:
        argv.append("--keep-output")
    if no_history:
        argv.append("--no-history")
    if verbose:
        argv.append("--verbose")
    _run_eval_entrypoint("validate_all", argv)


@eval_app.command("enrich-ground-truth")
def eval_enrich_ground_truth(
    gt_dir: Annotated[
        Path,
        typer.Option("--gt-dir", help="Ground-truth directory."),
    ] = Path("eval/ground_truth"),
    field: Annotated[
        list[str] | None,
        typer.Option("--field", help="Field to enrich (repeatable)."),
    ] = None,
    filter_by: Annotated[
        list[str] | None,
        typer.Option("--filter", help="Filter entries by key=value (repeatable)."),
    ] = None,
    show_limit: Annotated[
        int,
        typer.Option("--show-limit", help="Maximum unresolved IDs to include per field."),
    ] = 10,
    write: Annotated[
        bool,
        typer.Option("--write", help="Persist enriched values to YAML files."),
    ] = False,
    taint_field_candidates_only: Annotated[
        bool,
        typer.Option(
            "--taint-field-candidates-only",
            help="Only enforce taint_field_path enrichment for explicitly inferable sources.",
        ),
    ] = False,
    fail_on_unresolved: Annotated[
        bool,
        typer.Option(
            "--fail-on-unresolved",
            help="Exit non-zero if unresolved entries remain for selected fields.",
        ),
    ] = False,
    fail_on_updates: Annotated[
        bool,
        typer.Option(
            "--fail-on-updates",
            help="Exit non-zero if enrichment would update any fields in dry-run mode.",
        ),
    ] = False,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Emit machine-readable JSON."),
    ] = False,
) -> None:
    argv = [
        "--gt-dir",
        str(gt_dir),
        "--show-limit",
        str(show_limit),
    ]
    _append_repeatable_flags(argv, "--field", field or [])
    _append_repeatable_flags(argv, "--filter", filter_by or [])
    if write:
        argv.append("--write")
    if taint_field_candidates_only:
        argv.append("--taint-field-candidates-only")
    if fail_on_unresolved:
        argv.append("--fail-on-unresolved")
    if fail_on_updates:
        argv.append("--fail-on-updates")
    if json_output:
        argv.append("--json")
    _run_eval_entrypoint("enrich_ground_truth", argv)


@eval_app.command("coverage-gaps")
def eval_coverage_gaps(
    gt_dir: Annotated[
        Path,
        typer.Option("--gt-dir", help="Ground-truth directory."),
    ] = Path("eval/ground_truth"),
    dimension: Annotated[
        list[str] | None,
        typer.Option(
            "--dimension",
            help="Slice dimension, e.g. cwe+language (repeatable).",
        ),
    ] = None,
    filter_by: Annotated[
        list[str] | None,
        typer.Option("--filter", help="Filter entries by key=value (repeatable)."),
    ] = None,
    min_count: Annotated[
        int,
        typer.Option("--min-count", min=1, help="Minimum desired fixtures per slice."),
    ] = 8,
    max_results_per_dimension: Annotated[
        int,
        typer.Option(
            "--max-results-per-dimension",
            min=1,
            help="Maximum gap rows to emit per requested dimension.",
        ),
    ] = 20,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Emit machine-readable JSON."),
    ] = False,
) -> None:
    argv = [
        "--gt-dir",
        str(gt_dir),
        "--min-count",
        str(min_count),
        "--max-results-per-dimension",
        str(max_results_per_dimension),
    ]
    _append_repeatable_flags(argv, "--dimension", dimension or [])
    _append_repeatable_flags(argv, "--filter", filter_by or [])
    if json_output:
        argv.append("--json")
    _run_eval_entrypoint("coverage_gaps", argv)


@eval_app.command("compare-reports")
def eval_compare_reports(
    baseline_report: Annotated[
        Path | None,
        typer.Option("--baseline-report", help="Baseline validate_all report JSON path."),
    ] = None,
    current_report: Annotated[
        Path | None,
        typer.Option("--current-report", help="Current validate_all report JSON path."),
    ] = None,
    history_dir: Annotated[
        Path | None,
        typer.Option(
            "--history-dir",
            help=(
                "History directory with index.json. "
                "Use this instead of report paths to compare the latest two snapshots."
            ),
        ),
    ] = None,
    markdown_output: Annotated[
        Path | None,
        typer.Option("--markdown-output", help="Write markdown summary to this file."),
    ] = None,
    top: Annotated[
        int,
        typer.Option("--top", min=1, help="Top regressions per metric to include."),
    ] = 10,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Emit machine-readable JSON."),
    ] = False,
    min_detection_rate_delta: Annotated[
        float | None,
        typer.Option("--min-detection-rate-delta", help="Minimum overall detection-rate delta."),
    ] = None,
    min_fp_rate_delta: Annotated[
        float | None,
        typer.Option("--min-fp-rate-delta", help="Minimum overall FP-suppression-rate delta."),
    ] = None,
    min_group_detection_delta: Annotated[
        list[str] | None,
        typer.Option(
            "--min-group-detection-delta",
            help="Per-group detection delta threshold group=value:delta (repeatable).",
        ),
    ] = None,
    min_group_fp_delta: Annotated[
        list[str] | None,
        typer.Option(
            "--min-group-fp-delta",
            help="Per-group FP suppression delta threshold group=value:delta (repeatable).",
        ),
    ] = None,
) -> None:
    argv = ["--top", str(top)]
    if baseline_report is not None:
        argv.extend(["--baseline-report", str(baseline_report)])
    if current_report is not None:
        argv.extend(["--current-report", str(current_report)])
    if history_dir is not None:
        argv.extend(["--history-dir", str(history_dir)])
    if markdown_output is not None:
        argv.extend(["--markdown-output", str(markdown_output)])
    if json_output:
        argv.append("--json")
    if min_detection_rate_delta is not None:
        argv.extend(["--min-detection-rate-delta", str(min_detection_rate_delta)])
    if min_fp_rate_delta is not None:
        argv.extend(["--min-fp-rate-delta", str(min_fp_rate_delta)])
    _append_repeatable_flags(argv, "--min-group-detection-delta", min_group_detection_delta or [])
    _append_repeatable_flags(argv, "--min-group-fp-delta", min_group_fp_delta or [])
    _run_eval_entrypoint("compare_reports", argv)


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
    fail_on_new: FailOnNewOption = None,
    fail_on_new_severity: FailOnNewSeverityOption = None,
    fail_severity: FailSeverityOption = FailSeverity.LOW,
    no_fail: NoFailOption = False,
    staged_only: StagedOnlyOption = False,
    hook_timeout: HookTimeoutOption = None,
    incremental: IncrementalOption = None,
    triage_model: TriageModelOption = None,
    patch_model: PatchModelOption = None,
    docker_image: DockerImageOption = None,
    timeout: TimeoutOption = None,
    proof_mode: ProofModeOption = None,
    target_profile: TargetProfileOption = None,
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
            "verify.proof_mode": _proof_mode_override(proof_mode),
            "verify.target_profile": target_profile,
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
    effective_fail_on_new = (
        config_model.baseline.fail_on_new if fail_on_new is None else fail_on_new
    )
    effective_fail_on_new_severity = (
        config_model.baseline.fail_on_new_severity
        if fail_on_new_severity is None
        else fail_on_new_severity.value
    )
    if baseline is not None:
        diff_result = _print_diff(baseline, options.output_dir)
        markdown_path, json_path = _write_baseline_diff_artifacts(diff_result, options.output_dir)
        typer.echo(f"baseline diff markdown: {markdown_path}")
        typer.echo(f"baseline diff json: {json_path}")
        if effective_fail_on_new and not no_fail:
            failing_new = new_findings_at_or_above(
                diff_result,
                minimum_severity=effective_fail_on_new_severity,
            )
            if failing_new:
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


def _register_collapsed_command_aliases() -> None:
    """Register progressive-disclosure aliases without breaking compatibility."""

    pipeline_app.command("run")(run)
    pipeline_app.command("scan")(scan)
    pipeline_app.command("detect")(detect)
    pipeline_app.command("triage")(triage)
    pipeline_app.command("verify")(verify)
    pipeline_app.command("legal")(legal)
    pipeline_app.command("patch")(patch)
    pipeline_app.command("report")(report)

    dev_app.command("watch")(watch)
    dev_app.command("lsp")(lsp)

    baseline_app.command("diff")(diff_command)
    suppressions_app.command("add")(suppress)


_register_collapsed_command_aliases()
