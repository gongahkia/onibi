from __future__ import annotations

import logging
import subprocess
import time
from collections.abc import Callable, Generator, Iterable, Sequence
from contextlib import nullcontext
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from rich.console import Group
from rich.live import Live
from rich.panel import Panel
from rich.table import Table

from piranesi.config import PiranesiConfig
from piranesi.diff import Finding, diff_findings
from piranesi.llm.cost import CostTracker
from piranesi.llm.provider import LLMProvider
from piranesi.llm.router import ModelRouter
from piranesi.llm.trace import TraceLogger
from piranesi.models import CandidateFinding, ScanResult
from piranesi.pipeline import (
    DetectArtifact,
    IncrementalState,
    PipelineContext,
    _effective_scan_globs,
    _matches_patterns,
    _run_detect_stage,
    _run_scan_stage,
    prepare_incremental_state,
)
from piranesi.scan.incremental import IncrementalResult
from piranesi.trace import TraceWriter
from piranesi.ui import console

logger = logging.getLogger("piranesi.watch")
_SEVERITY_ORDER = ("critical", "high", "medium", "low", "informational")


class WatchModeError(RuntimeError):
    """Raised when watch mode cannot be started or configured."""


class WatchDependencyError(WatchModeError):
    """Raised when an optional watch-mode dependency is unavailable."""


@dataclass(frozen=True, slots=True)
class WatchModeSummary:
    scans: int
    findings_remaining: int
    fixed_total: int
    exit_code: int


@dataclass(slots=True)
class _WatchDisplayState:
    status: str = "starting"
    scan_count: int = 0
    findings_total: int = 0
    severity_breakdown: dict[str, int] = field(default_factory=dict)
    last_scan_time: str | None = None
    last_scan_duration_s: float | None = None
    last_new: int = 0
    last_fixed: int = 0
    total_fixed: int = 0
    changed_files: list[str] = field(default_factory=list)
    highest_severity: str | None = None
    last_error: str | None = None


def run_watch_mode(
    target_dir: Path,
    *,
    config: PiranesiConfig,
    output_dir: Path,
    debounce_ms: int = 500,
    filter_glob: str | None = None,
    on_finding: str | None = None,
    fail_severity: str = "low",
    max_scans: int | None = None,
    use_cache: bool = True,
    max_parallel: int | None = None,
    render_ui: bool = True,
) -> WatchModeSummary:
    normalized_target = target_dir.resolve(strict=False)
    normalized_output = output_dir.resolve(strict=False)
    state = _WatchDisplayState()
    active_findings: list[CandidateFinding] = []

    cost_tracker = CostTracker()
    trace_writer = TraceWriter(config.trace, config.budget)
    router = ModelRouter(config, cost_tracker)
    trace_logger = TraceLogger(trace_writer, log_prompts=config.trace.log_prompts)
    provider = LLMProvider(trace_logger, cost_tracker, router=router)

    if not normalized_target.is_dir():
        raise WatchModeError(f"target directory does not exist: {normalized_target}")
    if max_scans is not None and max_scans < 1:
        raise WatchModeError("--max-scans must be >= 1")
    if debounce_ms < 0:
        raise WatchModeError("--debounce must be >= 0")
    _validate_watch_dependency()

    console.print(f"Watching {normalized_target} for changes...")
    live_context = (
        Live(_render_watch_state(state), console=console, refresh_per_second=4)
        if render_ui
        else nullcontext(None)
    )

    try:
        trace_writer.open()
        with live_context as live:
            state.status = "scanning"
            _refresh_live(live, state)
            _, detect_artifact, duration_s = _run_watch_scan(
                normalized_target,
                normalized_output,
                config=config,
                provider=provider,
                router=router,
                cost_tracker=cost_tracker,
                trace_writer=trace_writer,
                use_cache=use_cache,
                max_parallel=max_parallel,
                changed_batch=None,
            )
            active_findings = _active_findings(detect_artifact.findings)
            _apply_scan_update(
                state,
                findings=active_findings,
                changed_files=[],
                duration_s=duration_s,
                new_count=0,
                fixed_count=0,
            )
            _refresh_live(live, state)
            if max_scans is not None and state.scan_count >= max_scans:
                return _finalize_watch_summary(state, fail_severity=fail_severity)

            state.status = "idle"
            _refresh_live(live, state)
            for batch in _iter_watch_batches(
                normalized_target,
                config=config,
                filter_glob=filter_glob,
                debounce_ms=debounce_ms,
            ):
                changed_files = _display_paths(normalized_target, batch)
                state.status = "scanning"
                state.changed_files = changed_files
                state.last_error = None
                _refresh_live(live, state)

                _, detect_artifact, duration_s = _run_watch_scan(
                    normalized_target,
                    normalized_output,
                    config=config,
                    provider=provider,
                    router=router,
                    cost_tracker=cost_tracker,
                    trace_writer=trace_writer,
                    use_cache=use_cache,
                    max_parallel=max_parallel,
                    changed_batch=batch,
                )
                current_findings = _active_findings(detect_artifact.findings)
                diff_result = diff_findings(
                    [Finding.from_candidate(finding) for finding in active_findings],
                    [Finding.from_candidate(finding) for finding in current_findings],
                )
                active_findings = current_findings
                _apply_scan_update(
                    state,
                    findings=current_findings,
                    changed_files=changed_files,
                    duration_s=duration_s,
                    new_count=len(diff_result.new),
                    fixed_count=len(diff_result.fixed),
                )
                if on_finding is not None and diff_result.new:
                    _run_on_finding_hook(
                        on_finding,
                        count=len(current_findings),
                        new_count=len(diff_result.new),
                        fixed_count=len(diff_result.fixed),
                        highest_severity=state.highest_severity,
                    )
                state.status = "idle"
                _refresh_live(live, state)
                if max_scans is not None and state.scan_count >= max_scans:
                    break
    except KeyboardInterrupt:
        logger.info("watch mode interrupted by user")
    except Exception as exc:
        state.status = "error"
        state.last_error = str(exc)
        raise
    finally:
        trace_writer.close()

    return _finalize_watch_summary(state, fail_severity=fail_severity)


def _run_watch_scan(
    target_dir: Path,
    output_dir: Path,
    *,
    config: PiranesiConfig,
    provider: LLMProvider,
    router: ModelRouter,
    cost_tracker: CostTracker,
    trace_writer: TraceWriter,
    use_cache: bool,
    max_parallel: int | None,
    changed_batch: set[Path] | None,
) -> tuple[ScanResult, DetectArtifact, float]:
    incremental = prepare_incremental_state(
        target_dir,
        output_dir,
        manifest_write_stage="watch",
    )
    incremental = _narrow_incremental_state_for_batch(
        incremental=incremental,
        target_dir=target_dir,
        changed_batch=changed_batch,
    )
    context = PipelineContext(
        target_dir=target_dir,
        output_dir=output_dir,
        provider=provider,
        router=router,
        cost_tracker=cost_tracker,
        trace_writer=trace_writer,
        use_cache=use_cache,
        incremental=incremental,
        max_parallel=max_parallel,
        render_ui=False,
    )
    scan_result = _run_scan_stage(context, config, None)
    context.stage_outputs["scan"] = scan_result.artifact
    detect_result = _run_detect_stage(context, config, None)
    context.stage_outputs["detect"] = detect_result.artifact
    _persist_watch_artifacts(
        output_dir,
        scan_artifact=scan_result.artifact,
        detect_artifact=detect_result.artifact,
    )
    return (
        scan_result.artifact,
        detect_result.artifact,
        scan_result.elapsed_s + detect_result.elapsed_s,
    )


def _persist_watch_artifacts(
    output_dir: Path,
    *,
    scan_artifact: ScanResult,
    detect_artifact: DetectArtifact,
) -> None:
    output_dir.mkdir(parents=True, exist_ok=True)
    (output_dir / "scan.json").write_text(scan_artifact.model_dump_json(indent=2), encoding="utf-8")
    (output_dir / "detect.json").write_text(
        detect_artifact.model_dump_json(indent=2),
        encoding="utf-8",
    )
    # Watch mode manages its own manifest lifecycle so tests can stub pipeline stages safely.
    from piranesi.scan.incremental import write_manifest

    write_manifest(Path(scan_artifact.project_root), output_dir)


def _narrow_incremental_state_for_batch(
    *,
    incremental: IncrementalState,
    target_dir: Path,
    changed_batch: set[Path] | None,
) -> IncrementalState:
    if changed_batch is None or incremental.previous_manifest is None:
        return incremental

    relative_batch: set[Path] = set()
    for changed_path in changed_batch:
        resolved = changed_path.resolve(strict=False)
        try:
            relative_batch.add(resolved.relative_to(target_dir))
        except ValueError:
            continue
    if not relative_batch:
        return incremental

    previous_paths = {Path(path) for path in incremental.previous_manifest.files}
    current_paths = {Path(path) for path in incremental.current_manifest.files}
    added = {
        path for path in relative_batch if path in current_paths and path not in previous_paths
    }
    modified = {path for path in relative_batch if path in current_paths and path in previous_paths}
    deleted = {
        path for path in relative_batch if path not in current_paths and path in previous_paths
    }
    if not (added or modified or deleted):
        return incremental

    unchanged = {path for path in current_paths if path not in added and path not in modified}
    return IncrementalState(
        previous_manifest=incremental.previous_manifest,
        current_manifest=incremental.current_manifest,
        diff=IncrementalResult(
            added=added,
            modified=modified,
            deleted=deleted,
            unchanged=unchanged,
        ),
        manifest_write_stage=incremental.manifest_write_stage,
    )


def _apply_scan_update(
    state: _WatchDisplayState,
    *,
    findings: Sequence[CandidateFinding],
    changed_files: list[str],
    duration_s: float,
    new_count: int,
    fixed_count: int,
) -> None:
    state.scan_count += 1
    state.findings_total = len(findings)
    state.severity_breakdown = _severity_breakdown(findings)
    state.last_scan_time = time.strftime("%H:%M:%S")
    state.last_scan_duration_s = duration_s
    state.last_new = new_count
    state.last_fixed = fixed_count
    state.total_fixed += fixed_count
    state.changed_files = changed_files
    state.highest_severity = _highest_severity(findings)
    state.last_error = None


def _active_findings(findings: Sequence[CandidateFinding]) -> list[CandidateFinding]:
    return [finding for finding in findings if not finding.suppressed]


def _severity_breakdown(findings: Sequence[CandidateFinding]) -> dict[str, int]:
    counts = dict.fromkeys(_SEVERITY_ORDER, 0)
    for finding in findings:
        severity = finding.severity.lower()
        counts[severity] = counts.get(severity, 0) + 1
    return {severity: count for severity, count in counts.items() if count > 0}


def _highest_severity(findings: Sequence[CandidateFinding]) -> str | None:
    present = {finding.severity.lower() for finding in findings}
    for severity in _SEVERITY_ORDER:
        if severity in present:
            return severity
    return None


def _finalize_watch_summary(
    state: _WatchDisplayState,
    *,
    fail_severity: str,
) -> WatchModeSummary:
    summary = WatchModeSummary(
        scans=state.scan_count,
        findings_remaining=state.findings_total,
        fixed_total=state.total_fixed,
        exit_code=_summary_exit_code(
            findings_total=state.findings_total,
            severity_breakdown=state.severity_breakdown,
            fail_severity=fail_severity,
        ),
    )
    console.print(
        f"Summary: {summary.scans} scans, "
        f"{summary.findings_remaining} findings remaining, "
        f"{summary.fixed_total} fixed"
    )
    return summary


def _summary_exit_code(
    *,
    findings_total: int,
    severity_breakdown: dict[str, int],
    fail_severity: str,
) -> int:
    threshold = _severity_rank(fail_severity)
    findings_at_or_above_threshold = sum(
        count
        for severity, count in severity_breakdown.items()
        if _severity_rank(severity) >= threshold
    )
    if findings_total == 0:
        return 0
    return 1 if findings_at_or_above_threshold > 0 else 0


def _severity_rank(severity: str) -> int:
    normalized = severity.lower()
    if normalized == "low":
        return 0
    if normalized == "medium":
        return 1
    if normalized == "high":
        return 2
    if normalized == "critical":
        return 3
    return -1


def _render_watch_state(state: _WatchDisplayState) -> Panel:
    summary_table = Table.grid(padding=(0, 1))
    summary_table.add_column(style="bold cyan", no_wrap=True)
    summary_table.add_column()
    summary_table.add_row("Status", state.status)
    summary_table.add_row("Scans", str(state.scan_count))
    summary_table.add_row(
        "Findings",
        f"{state.findings_total} total | {_format_severity_breakdown(state.severity_breakdown)}",
    )
    summary_table.add_row(
        "Last scan",
        _format_last_scan(state.last_scan_time, state.last_scan_duration_s),
    )
    summary_table.add_row("Delta", f"{state.last_new} new, {state.last_fixed} fixed")

    changed_table = Table.grid()
    changed_table.add_column()
    if state.changed_files:
        for changed_file in state.changed_files[:8]:
            changed_table.add_row(changed_file)
        remaining = len(state.changed_files) - 8
        if remaining > 0:
            changed_table.add_row(f"... +{remaining} more")
    else:
        changed_table.add_row("(none)")

    renderables: list[Any] = [
        summary_table,
        Panel(changed_table, title="Changed Files", border_style="blue"),
    ]
    if state.last_error is not None:
        renderables.append(Panel(state.last_error, title="Error", border_style="red"))
    return Panel(Group(*renderables), title="Piranesi Watch", border_style="green")


def _format_severity_breakdown(severity_breakdown: dict[str, int]) -> str:
    if not severity_breakdown:
        return "none"
    parts = [f"{severity} {severity_breakdown[severity]}" for severity in _SEVERITY_ORDER]
    return ", ".join(part for part in parts if not part.endswith(" 0"))


def _format_last_scan(last_scan_time: str | None, duration_s: float | None) -> str:
    if last_scan_time is None:
        return "-"
    if duration_s is None:
        return last_scan_time
    return f"{last_scan_time} ({duration_s:.2f}s)"


def _refresh_live(live: Live | None, state: _WatchDisplayState) -> None:
    if live is None:
        return
    live.update(_render_watch_state(state))


def _iter_watch_batches(
    target_dir: Path,
    *,
    config: PiranesiConfig,
    filter_glob: str | None,
    debounce_ms: int,
) -> Generator[set[Path], None, None]:
    watch, default_filter_factory = _load_watchfiles()
    default_filter = default_filter_factory()

    def _watch_filter(change: object, raw_path: str) -> bool:
        return default_filter(change, raw_path) and _should_track_path(
            target_dir,
            Path(raw_path),
            config=config,
            filter_glob=filter_glob,
        )

    for changes in watch(
        str(target_dir),
        watch_filter=_watch_filter,
        debounce=debounce_ms,
        raise_interrupt=False,
        ignore_permission_denied=True,
    ):
        batch = {Path(raw_path).resolve(strict=False) for _change, raw_path in changes}
        if batch:
            yield batch


def _should_track_path(
    target_dir: Path,
    changed_path: Path,
    *,
    config: PiranesiConfig,
    filter_glob: str | None,
) -> bool:
    candidate = changed_path.resolve(strict=False)
    try:
        relative = candidate.relative_to(target_dir).as_posix()
    except ValueError:
        return False

    include_patterns, exclude_patterns = _effective_scan_globs(target_dir, config)
    if not _matches_patterns(relative, include_patterns):
        return False
    if _matches_patterns(relative, exclude_patterns):
        return False
    if filter_glob is None:
        return True
    return _matches_patterns(relative, [filter_glob])


def _display_paths(target_dir: Path, paths: Iterable[Path]) -> list[str]:
    display: list[str] = []
    for path in sorted(paths):
        candidate = path.resolve(strict=False)
        try:
            display.append(candidate.relative_to(target_dir).as_posix())
        except ValueError:
            display.append(str(candidate))
    return display


def _run_on_finding_hook(
    command_template: str,
    *,
    count: int,
    new_count: int,
    fixed_count: int,
    highest_severity: str | None,
) -> None:
    try:
        command = command_template.format(
            count=count,
            new=new_count,
            fixed=fixed_count,
            severity=highest_severity or "none",
        )
    except KeyError as exc:
        logger.warning("failed to format --on-finding hook: %s", exc)
        return

    result = subprocess.run(command, shell=True, check=False)  # noqa: S602
    if result.returncode != 0:
        logger.warning("on-finding hook exited with status %s", result.returncode)


def _load_watchfiles() -> tuple[
    Callable[..., Iterable[set[tuple[object, str]]]],
    Callable[[], Callable[[object, str], bool]],
]:
    try:
        from watchfiles import DefaultFilter, watch
    except ImportError as exc:
        raise WatchDependencyError(
            "watch mode requires the optional 'watchfiles' dependency. "
            "Install with `pip install piranesi[watch]`."
        ) from exc
    return watch, DefaultFilter


def _validate_watch_dependency() -> None:
    _load_watchfiles()


__all__ = ["WatchDependencyError", "WatchModeError", "WatchModeSummary", "run_watch_mode"]
