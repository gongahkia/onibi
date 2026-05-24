from __future__ import annotations

from collections import Counter
from dataclasses import dataclass
from datetime import UTC, date, datetime
from pathlib import Path

from pydantic import BaseModel, ConfigDict, Field, ValidationError
from rich import box
from rich.console import Console
from rich.table import Table

from piranesi.diff import BaselineArtifact
from piranesi.report.cwe import extract_cwe_id

_SEVERITY_ORDER = ("critical", "high", "medium", "low", "informational")
_SPARKLINE_GLYPHS = "▁▂▃▄▅▆▇█"


@dataclass(frozen=True, slots=True)
class HistoricalScan:
    path: Path
    created_at: datetime
    artifact: BaselineArtifact
    fingerprints: frozenset[str]


class TrendPeriod(BaseModel):
    model_config = ConfigDict(extra="forbid")

    start: str
    end: str


class TrendSeries(BaseModel):
    model_config = ConfigDict(extra="forbid")

    scan_dates: list[str] = Field(default_factory=list)
    total_findings: list[int] = Field(default_factory=list)
    by_severity: dict[str, list[int]] = Field(default_factory=dict)
    by_cwe: dict[str, list[int]] = Field(default_factory=dict)
    fix_rate: list[int] = Field(default_factory=list)
    mean_time_to_fix_days: list[float | None] = Field(default_factory=list)
    new_finding_velocity: list[int] = Field(default_factory=list)


class TrendSummary(BaseModel):
    model_config = ConfigDict(extra="forbid")

    total_reduction: float | None = None
    avg_fix_rate: float = 0.0
    mean_time_to_fix_days: float | None = None
    avg_new_finding_velocity: float = 0.0
    alerts: list[str] = Field(default_factory=list)


class TrendReport(BaseModel):
    model_config = ConfigDict(extra="forbid")

    period: TrendPeriod
    scans: int
    series: TrendSeries
    summary: TrendSummary


def build_trend_report(
    output_dir: Path,
    *,
    since: date | None = None,
    until: date | None = None,
) -> TrendReport:
    history = load_baseline_history(output_dir, since=since, until=until)
    return compute_trend_report(history)


def load_baseline_history(
    output_dir: Path,
    *,
    since: date | None = None,
    until: date | None = None,
) -> list[HistoricalScan]:
    if since is not None and until is not None and since > until:
        raise ValueError(
            f"--since {since.isoformat()} must be on or before --until {until.isoformat()}"
        )

    if not output_dir.exists():
        raise ValueError(f"{output_dir} does not exist")
    if not output_dir.is_dir():
        raise ValueError(f"{output_dir} is not a directory")

    history: list[HistoricalScan] = []
    for candidate in sorted(output_dir.rglob("*.json")):
        if not candidate.is_file():
            continue
        artifact = _try_load_baseline_artifact(candidate)
        if artifact is None:
            continue

        created_at = _parse_timestamp(artifact.created_at, source=candidate)
        created_on = created_at.date()
        if since is not None and created_on < since:
            continue
        if until is not None and created_on > until:
            continue

        history.append(
            HistoricalScan(
                path=candidate,
                created_at=created_at,
                artifact=artifact,
                fingerprints=frozenset(finding.stable_fingerprint for finding in artifact.findings),
            )
        )

    history.sort(key=lambda scan: (scan.created_at, scan.path.as_posix()))
    if not history:
        raise ValueError(_missing_baseline_message(output_dir, since=since, until=until))
    return history


def compute_trend_report(history: list[HistoricalScan]) -> TrendReport:
    if not history:
        raise ValueError("at least one baseline artifact is required to compute trends")

    scan_dates: list[str] = []
    total_findings: list[int] = []
    by_severity: dict[str, list[int]] = {severity: [] for severity in _SEVERITY_ORDER}
    cwe_counts_per_scan: list[Counter[str]] = []
    cwe_totals: Counter[str] = Counter()
    fix_rate: list[int] = []
    mean_time_to_fix_days: list[float | None] = []
    new_finding_velocity: list[int] = []
    alerts: list[str] = []

    active_since: dict[str, datetime] = {}
    resolved_durations: list[float] = []

    for index, scan in enumerate(history):
        findings = scan.artifact.findings
        scan_dates.append(scan.created_at.date().isoformat())
        total_findings.append(len(findings))

        severity_counts = Counter(finding.severity.lower() for finding in findings)
        for severity in _SEVERITY_ORDER:
            by_severity[severity].append(severity_counts.get(severity, 0))

        cwe_counts = Counter(extract_cwe_id(finding.vuln_class) for finding in findings)
        cwe_counts_per_scan.append(cwe_counts)
        cwe_totals.update(cwe_counts)

        if index == 0:
            fix_rate.append(0)
            mean_time_to_fix_days.append(None)
            new_finding_velocity.append(0)
            for fingerprint in scan.fingerprints:
                active_since[fingerprint] = scan.created_at
            continue

        previous_scan = history[index - 1]
        fixed = previous_scan.fingerprints - scan.fingerprints
        introduced = scan.fingerprints - previous_scan.fingerprints

        fix_rate.append(len(fixed))
        new_finding_velocity.append(len(introduced))

        previous_total = len(previous_scan.artifact.findings)
        current_total = len(findings)
        if previous_total > 0:
            change_pct = ((current_total - previous_total) / previous_total) * 100
            if change_pct > 20:
                alerts.append(
                    f"{scan.created_at.date().isoformat()}: finding count increased "
                    f"{change_pct:.1f}% ({previous_total} -> {current_total})"
                )

        resolved_this_scan: list[float] = []
        for fingerprint in fixed:
            started_at = active_since.pop(fingerprint, previous_scan.created_at)
            duration_days = (scan.created_at - started_at).total_seconds() / 86_400
            resolved_this_scan.append(duration_days)
            resolved_durations.append(duration_days)

        mean_time_to_fix_days.append(
            _rounded(sum(resolved_this_scan) / len(resolved_this_scan))
            if resolved_this_scan
            else None
        )

        for fingerprint in introduced:
            active_since[fingerprint] = scan.created_at

    ordered_cwes = sorted(cwe_totals, key=lambda cwe: (-cwe_totals[cwe], cwe))
    by_cwe = {cwe: [counts.get(cwe, 0) for counts in cwe_counts_per_scan] for cwe in ordered_cwes}

    transition_count = max(len(history) - 1, 1)
    return TrendReport(
        period=TrendPeriod(
            start=history[0].created_at.date().isoformat(),
            end=history[-1].created_at.date().isoformat(),
        ),
        scans=len(history),
        series=TrendSeries(
            scan_dates=scan_dates,
            total_findings=total_findings,
            by_severity=by_severity,
            by_cwe=by_cwe,
            fix_rate=fix_rate,
            mean_time_to_fix_days=mean_time_to_fix_days,
            new_finding_velocity=new_finding_velocity,
        ),
        summary=TrendSummary(
            total_reduction=_percent_change(total_findings[0], total_findings[-1]),
            avg_fix_rate=(
                _rounded(sum(fix_rate[1:]) / transition_count) if len(history) > 1 else 0.0
            ),
            mean_time_to_fix_days=(
                _rounded(sum(resolved_durations) / len(resolved_durations))
                if resolved_durations
                else None
            ),
            avg_new_finding_velocity=(
                _rounded(sum(new_finding_velocity[1:]) / transition_count)
                if len(history) > 1
                else 0.0
            ),
            alerts=alerts,
        ),
    )


def write_trend_report(report: TrendReport, destination: Path) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(report.model_dump_json(indent=2), encoding="utf-8")


def render_terminal_trends(report: TrendReport, *, console: Console | None = None) -> None:
    render_console = console or Console()
    render_console.print(
        "[bold]Piranesi Trend Report[/bold] "
        f"({report.scans} scans, {report.period.start} -> {report.period.end})"
    )

    table = Table(box=box.ROUNDED)
    table.add_column("Metric", style="bold")
    table.add_column("Trend")
    table.add_column("Latest", justify="right")
    table.add_column("Change", justify="right")

    _add_series_row(table, "Findings", report.series.total_findings)
    for severity in _SEVERITY_ORDER:
        values = report.series.by_severity.get(severity, [])
        if any(values):
            _add_series_row(table, severity.title(), values)

    for cwe, values in list(report.series.by_cwe.items())[:3]:
        _add_series_row(table, cwe, values)

    render_console.print(table)

    summary = Table(box=None, show_header=False, pad_edge=False)
    summary.add_column("Metric", style="bold")
    summary.add_column("Value")
    summary.add_row("Fix rate", f"{report.summary.avg_fix_rate:.2f} findings/scan")
    summary.add_row(
        "MTTF",
        (
            f"{report.summary.mean_time_to_fix_days:.2f} days"
            if report.summary.mean_time_to_fix_days is not None
            else "n/a"
        ),
    )
    summary.add_row(
        "New velocity",
        f"{report.summary.avg_new_finding_velocity:.2f} findings/scan",
    )
    render_console.print(summary)

    for alert in report.summary.alerts:
        render_console.print(f"[yellow]warning:[/] {alert}")


def _add_series_row(table: Table, label: str, values: list[int]) -> None:
    latest = str(values[-1]) if values else "0"
    table.add_row(label, _sparkline(values), latest, _format_change(values))


def _sparkline(values: list[int]) -> str:
    if not values:
        return ""
    if max(values) == min(values):
        glyph = _SPARKLINE_GLYPHS[4] if values[0] > 0 else _SPARKLINE_GLYPHS[0]
        return glyph * len(values)

    lower_bound = min(values)
    scale = (len(_SPARKLINE_GLYPHS) - 1) / (max(values) - lower_bound)
    return "".join(_SPARKLINE_GLYPHS[round((value - lower_bound) * scale)] for value in values)


def _format_change(values: list[int]) -> str:
    if not values:
        return "n/a"
    start = values[0]
    end = values[-1]
    if start == 0:
        if end == 0:
            return "0%"
        return "new"
    return f"{((end - start) / start) * 100:+.0f}%"


def _percent_change(start: int, end: int) -> float | None:
    if start == 0:
        return 0.0 if end == 0 else None
    return _rounded(((end - start) / start) * 100)


def _rounded(value: float) -> float:
    return round(value, 2)


def _try_load_baseline_artifact(path: Path) -> BaselineArtifact | None:
    try:
        payload = path.read_text(encoding="utf-8")
    except OSError as exc:
        raise ValueError(f"failed to read baseline artifact {path}: {exc}") from exc

    try:
        return BaselineArtifact.model_validate_json(payload)
    except ValidationError:
        return None


def _parse_timestamp(value: str, *, source: Path) -> datetime:
    normalized = value.strip()
    if normalized.endswith("Z"):
        normalized = f"{normalized[:-1]}+00:00"
    try:
        parsed = datetime.fromisoformat(normalized)
    except ValueError as exc:
        raise ValueError(f"baseline artifact {source} has invalid created_at {value!r}") from exc

    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=UTC)
    return parsed.astimezone(UTC)


def _missing_baseline_message(
    output_dir: Path,
    *,
    since: date | None,
    until: date | None,
) -> str:
    filters: list[str] = []
    if since is not None:
        filters.append(f"since {since.isoformat()}")
    if until is not None:
        filters.append(f"until {until.isoformat()}")
    suffix = f" matching {' and '.join(filters)}" if filters else ""
    return f"no baseline artifacts found in {output_dir}{suffix}"


__all__ = [
    "HistoricalScan",
    "TrendPeriod",
    "TrendReport",
    "TrendSeries",
    "TrendSummary",
    "build_trend_report",
    "compute_trend_report",
    "load_baseline_history",
    "render_terminal_trends",
    "write_trend_report",
]
