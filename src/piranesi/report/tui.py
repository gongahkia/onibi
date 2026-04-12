from __future__ import annotations

import sys
from dataclasses import dataclass
from enum import StrEnum
from pathlib import Path
from typing import Any, ClassVar, TextIO

from rich import box
from rich.console import Console, Group
from rich.panel import Panel
from rich.table import Table

from piranesi.models import SourceLocation, TaintStep
from piranesi.report.renderer import (
    CandidateReportFinding,
    CombinedFinding,
    PiranesiReport,
    render_markdown,
)

_SEVERITY_ORDER = ("critical", "high", "medium", "low", "informational")


class ReportViewMode(StrEnum):
    MARKDOWN = "markdown"
    RICH = "rich"
    TEXTUAL = "textual"


class FilterMode(StrEnum):
    NONE = "all"
    SEVERITY = "severity"
    CWE = "cwe"
    FILE = "file"


class DetailMode(StrEnum):
    FINDING = "finding"
    PATCH = "patch"
    LEGAL = "legal"
    REPRODUCER = "reproducer"
    MESSAGE = "message"


@dataclass(frozen=True)
class ReportFindingRecord:
    finding_id: str
    cwe: str
    title: str
    severity: str
    confidence: float
    taint_source: str
    taint_sink: str
    source_location: SourceLocation
    sink_location: SourceLocation
    taint_path: tuple[TaintStep, ...]
    confirmed: bool
    confirmation_status: str
    verification_method: str | None = None
    patch_diff: str | None = None
    legal_memo_markdown: str | None = None
    reproducer_script: str | None = None

    def list_location(self) -> str:
        source_name = Path(self.source_location.file).name
        sink_name = Path(self.sink_location.file).name
        return f"{source_name}:{self.source_location.line} -> {sink_name}:{self.sink_location.line}"

    def list_flow(self) -> str:
        return f"{self.taint_source} -> {self.taint_sink}"

    def filter_value(self, mode: FilterMode) -> str | None:
        if mode == FilterMode.SEVERITY:
            return self.severity.lower()
        if mode == FilterMode.CWE:
            return self.cwe
        if mode == FilterMode.FILE:
            return self.source_location.file
        return None

    def search_text(self, mode: FilterMode) -> str:
        if mode == FilterMode.SEVERITY:
            return self.severity
        if mode == FilterMode.CWE:
            return f"{self.cwe} {self.title}"
        if mode == FilterMode.FILE:
            return f"{self.source_location.file} {self.sink_location.file}"
        return " ".join(
            (
                self.finding_id,
                self.cwe,
                self.title,
                self.severity,
                self.taint_source,
                self.taint_sink,
                self.source_location.file,
                self.sink_location.file,
            )
        )


class ReportTUIController:
    def __init__(
        self,
        report: PiranesiReport,
        *,
        output_dir: Path | None = None,
    ) -> None:
        self.report = report
        self.output_dir = Path.cwd() if output_dir is None else output_dir
        self.findings = _finding_records(report)
        self.selected_index = 0
        self.search_query = ""
        self.filter_mode = FilterMode.NONE
        self.filter_value: str | None = None
        self.expanded = True
        self.detail_mode = DetailMode.FINDING
        self.message_title = ""
        self.message_body = ""
        self.last_export_path: Path | None = None
        self.awaiting_suppression_reason = False
        self._clamp_selection()

    @property
    def total_findings(self) -> int:
        return len(self.findings)

    @property
    def visible_findings(self) -> list[ReportFindingRecord]:
        findings = list(self.findings)
        if self.filter_mode != FilterMode.NONE and self.filter_value is not None:
            normalized_filter = self.filter_value.lower()
            findings = [
                finding
                for finding in findings
                if (finding.filter_value(self.filter_mode) or "").lower() == normalized_filter
            ]
        if self.search_query:
            query = self.search_query.lower()
            findings = [
                finding
                for finding in findings
                if query in finding.search_text(self.filter_mode).lower()
            ]
        return findings

    @property
    def visible_count(self) -> int:
        return len(self.visible_findings)

    @property
    def selected_finding(self) -> ReportFindingRecord | None:
        findings = self.visible_findings
        if not findings:
            return None
        self._clamp_selection()
        return findings[self.selected_index]

    def move_down(self) -> None:
        if self.visible_count == 0:
            return
        self.selected_index = min(self.selected_index + 1, self.visible_count - 1)
        self.detail_mode = DetailMode.FINDING

    def move_up(self) -> None:
        if self.visible_count == 0:
            return
        self.selected_index = max(self.selected_index - 1, 0)
        self.detail_mode = DetailMode.FINDING

    def toggle_expand(self) -> None:
        self.expanded = not self.expanded
        self.detail_mode = DetailMode.FINDING

    def set_search_query(self, query: str) -> None:
        self.search_query = query.strip()
        self.selected_index = 0
        self.detail_mode = DetailMode.FINDING
        self.awaiting_suppression_reason = False
        self._clamp_selection()

    def cycle_filter(self) -> str:
        current = self.selected_finding
        next_mode = {
            FilterMode.NONE: FilterMode.SEVERITY,
            FilterMode.SEVERITY: FilterMode.CWE,
            FilterMode.CWE: FilterMode.FILE,
            FilterMode.FILE: FilterMode.NONE,
        }[self.filter_mode]
        self.filter_mode = next_mode
        if next_mode == FilterMode.NONE or current is None:
            self.filter_value = None
        else:
            self.filter_value = current.filter_value(next_mode)
        self.selected_index = 0
        self.detail_mode = DetailMode.FINDING
        self._clamp_selection()
        return self.filter_description()

    def show_patch(self) -> str:
        finding = self.selected_finding
        if finding is None:
            return self._set_message("Patch", "No finding selected.")
        if finding.patch_diff is None:
            return self._set_message("Patch", "No patch generated for the selected finding.")
        self.detail_mode = DetailMode.PATCH
        return finding.patch_diff

    def show_legal(self) -> str:
        finding = self.selected_finding
        if finding is None:
            return self._set_message("Legal", "No finding selected.")
        if finding.legal_memo_markdown is None:
            return self._set_message("Legal", "No legal memo generated for the selected finding.")
        self.detail_mode = DetailMode.LEGAL
        return finding.legal_memo_markdown

    def show_reproducer(self) -> str:
        finding = self.selected_finding
        if finding is None:
            return self._set_message("Reproducer", "No finding selected.")
        if finding.reproducer_script is None:
            return self._set_message(
                "Reproducer",
                "No reproducer generated for the selected finding.",
            )
        self.detail_mode = DetailMode.REPRODUCER
        return finding.reproducer_script

    def suppress_finding(self) -> str:
        finding = self.selected_finding
        if finding is None:
            return self._set_message("Suppress", "No finding selected.")
        self.awaiting_suppression_reason = True
        return self._set_message(
            "Suppress",
            (
                f"Enter a suppression reason for {finding.finding_id} in the input box "
                "and press Enter."
            ),
        )

    def apply_suppression(self, reason: str) -> str:
        from piranesi.detect import append_ignore_file_suppression

        finding = self.selected_finding
        if finding is None:
            return self._set_message("Suppress", "No finding selected.")
        ignore_path = append_ignore_file_suppression(
            Path(self.report.target),
            finding_id=finding.finding_id,
            reason=reason.strip() or "suppressed via tui",
        )
        self.awaiting_suppression_reason = False
        return self._set_message(
            "Suppress",
            f"Added suppression for {finding.finding_id} to {ignore_path}",
        )

    def export_markdown(self) -> Path:
        export_path = self.output_dir / "report-view.md"
        export_path.write_text(self.render_current_markdown(), encoding="utf-8")
        self.last_export_path = export_path
        self._set_message("Export", f"Exported current view to {export_path}")
        return export_path

    def render_current_markdown(self) -> str:
        if (
            self.filter_mode == FilterMode.NONE
            and self.filter_value is None
            and not self.search_query
        ):
            return render_markdown(self.report)
        return render_markdown(self._filtered_report())

    def summary_text(self) -> str:
        severity_counts = _severity_breakdown(self.visible_findings)
        severity_parts = [
            f"{count} {severity.upper()}"
            for severity, count in severity_counts.items()
            if count > 0
        ]
        summary = f"Summary: {self.visible_count}/{self.total_findings} findings"
        if severity_parts:
            summary += f" ({', '.join(severity_parts)})"
        summary += f" | ${self.report.executive_summary.total_llm_cost_usd:.2f} LLM"
        if self.filter_mode != FilterMode.NONE and self.filter_value is not None:
            summary += f" | Filter: {self.filter_description()}"
        if self.search_query:
            summary += f" | Search: {self.search_query}"
        return summary

    def filter_description(self) -> str:
        if self.filter_mode == FilterMode.NONE or self.filter_value is None:
            return "all"
        return f"{self.filter_mode.value}={self.filter_value}"

    def detail_title(self) -> str:
        finding = self.selected_finding
        if self.detail_mode == DetailMode.PATCH:
            return "Patch"
        if self.detail_mode == DetailMode.LEGAL:
            return "Legal"
        if self.detail_mode == DetailMode.REPRODUCER:
            return "Reproducer"
        if self.detail_mode == DetailMode.MESSAGE:
            return self.message_title
        if finding is None:
            return "Finding Detail"
        return f"Finding {finding.finding_id}"

    def detail_body(self) -> str:
        if self.detail_mode == DetailMode.MESSAGE:
            return self.message_body
        finding = self.selected_finding
        if finding is None:
            return "No findings match the current view."
        if self.detail_mode == DetailMode.PATCH:
            return finding.patch_diff or "No patch generated for the selected finding."
        if self.detail_mode == DetailMode.LEGAL:
            return (
                finding.legal_memo_markdown or "No legal memo generated for the selected finding."
            )
        if self.detail_mode == DetailMode.REPRODUCER:
            return finding.reproducer_script or "No reproducer generated for the selected finding."
        return _finding_detail_text(finding, expanded=self.expanded)

    def _clamp_selection(self) -> None:
        findings = self.visible_findings
        if not findings:
            self.selected_index = 0
            return
        self.selected_index = max(0, min(self.selected_index, len(findings) - 1))

    def _filtered_report(self) -> PiranesiReport:
        visible_ids = {finding.finding_id for finding in self.visible_findings}
        active_findings = [
            finding for finding in self.report.active_findings if finding.finding_id in visible_ids
        ]
        confirmed_findings = [
            finding for finding in self.report.findings if finding.finding_id in visible_ids
        ]
        package_findings = {
            package_name: package_findings
            for package_name, package_findings in (
                (
                    package_name,
                    [finding for finding in findings if finding.finding_id in visible_ids],
                )
                for package_name, findings in self.report.package_findings.items()
            )
            if package_findings
        }
        summary = self.report.executive_summary.model_copy(
            update={
                "findings_detected": len(active_findings),
                "findings_confirmed": len(confirmed_findings),
                "severity_breakdown": _severity_breakdown(active_findings),
            }
        )
        return self.report.model_copy(
            update={
                "executive_summary": summary,
                "active_findings": active_findings,
                "findings": confirmed_findings,
                "package_findings": package_findings,
                "cross_package_findings": [
                    finding
                    for finding in self.report.cross_package_findings
                    if finding.finding_id in visible_ids
                ],
            }
        )

    def _set_message(self, title: str, body: str) -> str:
        self.detail_mode = DetailMode.MESSAGE
        self.message_title = title
        self.message_body = body
        return body


def dispatch_keybinding(app: object, key: str) -> Any:
    action_name = _KEY_ACTIONS.get(key.lower())
    if action_name is None:
        raise KeyError(f"unsupported keybinding: {key}")
    action = getattr(app, f"action_{action_name}", None)
    if not callable(action):
        raise AttributeError(f"app does not implement action_{action_name}")
    return action()


def display_report(
    report: PiranesiReport,
    *,
    output_dir: Path | None = None,
    stdout: TextIO | None = None,
) -> ReportViewMode:
    stream = sys.stdout if stdout is None else stdout
    controller = ReportTUIController(report, output_dir=output_dir)
    if not _is_tty(stream):
        markdown = controller.render_current_markdown()
        stream.write(markdown)
        if not markdown.endswith("\n"):
            stream.write("\n")
        return ReportViewMode.MARKDOWN

    try:
        app = create_textual_app(controller)
    except ImportError:
        render_rich_fallback(controller, stdout=stream)
        return ReportViewMode.RICH

    _run_textual_app(app)
    return ReportViewMode.TEXTUAL


def render_rich_fallback(
    controller: ReportTUIController,
    *,
    stdout: TextIO | None = None,
) -> None:
    stream = sys.stdout if stdout is None else stdout
    console = Console(file=stream, force_terminal=_is_tty(stream))
    table = Table(box=box.SIMPLE_HEAVY)
    table.add_column("", style="bold")
    table.add_column("Severity", style="bold")
    table.add_column("CWE")
    table.add_column("Location")
    table.add_column("Flow")
    table.add_column("Confirmed")

    visible_findings = controller.visible_findings
    if visible_findings:
        for index, finding in enumerate(visible_findings):
            marker = ">" if index == controller.selected_index else " "
            table.add_row(
                marker,
                finding.severity.upper(),
                finding.cwe,
                finding.list_location(),
                finding.list_flow(),
                "yes" if finding.confirmed else "no",
            )
    else:
        table.add_row("-", "-", "-", "No findings match the current view.", "-", "-")

    console.print(
        Group(
            Panel(controller.summary_text(), title="Piranesi Report", border_style="cyan"),
            Panel(controller.filter_description(), title="Filter", border_style="blue"),
            table,
            Panel(
                controller.detail_body(),
                title=controller.detail_title(),
                border_style="green",
            ),
        )
    )


def create_textual_app(controller: ReportTUIController) -> Any:
    from textual.app import App, ComposeResult
    from textual.binding import Binding
    from textual.containers import Horizontal
    from textual.widgets import DataTable, Footer, Input, Static

    class PiranesiReportTextualApp(App[None]):
        BINDINGS: ClassVar[list[Any]] = [
            Binding("j", "move_down", show=False),
            Binding("k", "move_up", show=False),
            Binding("down", "move_down", show=False),
            Binding("up", "move_up", show=False),
            Binding("enter", "toggle_expand", show=False),
            Binding("/", "focus_search", show=False),
            Binding("p", "show_patch", show=False),
            Binding("l", "show_legal", show=False),
            Binding("r", "show_reproducer", show=False),
            Binding("s", "suppress_finding", show=False),
            Binding("e", "export_markdown", show=False),
            Binding("f", "cycle_filter", show=False),
            Binding("q", "quit_viewer", show=False),
        ]

        CSS = """
        Screen {
            layout: vertical;
        }

        #summary {
            height: auto;
            padding: 0 1;
        }

        #search {
            margin: 0 1;
        }

        #body {
            height: 1fr;
        }

        #findings {
            width: 1fr;
            min-width: 48;
        }

        #detail {
            width: 1fr;
            padding: 1;
            overflow: auto;
        }
        """

        def __init__(self, state: ReportTUIController) -> None:
            super().__init__()
            self.state = state

        def compose(self) -> ComposeResult:
            yield Static(self.state.summary_text(), id="summary")
            yield Input(placeholder="Search findings", id="search")
            with Horizontal(id="body"):
                yield DataTable(id="findings")
                yield Static(self.state.detail_body(), id="detail")
            yield Footer()

        def on_mount(self) -> None:
            table = self.query_one("#findings", DataTable)
            table.cursor_type = "row"
            table.zebra_stripes = True
            table.add_columns("Severity", "CWE", "Location", "Flow", "Confirmed")
            self._refresh_view()
            table.focus()

        def action_move_down(self) -> None:
            self.state.move_down()
            self._refresh_view()

        def action_move_up(self) -> None:
            self.state.move_up()
            self._refresh_view()

        def action_toggle_expand(self) -> None:
            self.state.toggle_expand()
            self._refresh_view()

        def action_focus_search(self) -> None:
            self.query_one("#search", Input).focus()

        def action_show_patch(self) -> None:
            self.state.show_patch()
            self._refresh_view()

        def action_show_legal(self) -> None:
            self.state.show_legal()
            self._refresh_view()

        def action_show_reproducer(self) -> None:
            self.state.show_reproducer()
            self._refresh_view()

        def action_suppress_finding(self) -> None:
            self.state.suppress_finding()
            search = self.query_one("#search", Input)
            search.value = ""
            search.focus()
            self._refresh_view()

        def action_export_markdown(self) -> None:
            self.state.export_markdown()
            self._refresh_view()

        def action_cycle_filter(self) -> None:
            self.state.cycle_filter()
            self._refresh_view()

        def action_quit_viewer(self) -> None:
            self.exit()

        def on_input_changed(self, event: Input.Changed) -> None:
            if self.state.awaiting_suppression_reason:
                return
            self.state.set_search_query(event.value)
            self._refresh_view()

        def on_input_submitted(self, event: Input.Submitted) -> None:
            if not self.state.awaiting_suppression_reason:
                return
            self.state.apply_suppression(event.value)
            event.input.value = ""
            self._refresh_view()

        def _refresh_view(self) -> None:
            table = self.query_one("#findings", DataTable)
            table.clear(columns=False)
            for finding in self.state.visible_findings:
                table.add_row(
                    finding.severity.upper(),
                    finding.cwe,
                    finding.list_location(),
                    finding.list_flow(),
                    "yes" if finding.confirmed else "no",
                )
            summary = self.query_one("#summary", Static)
            detail = self.query_one("#detail", Static)
            summary.update(self.state.summary_text())
            detail.update(self.state.detail_body())
            if self.state.visible_findings:
                table.cursor_coordinate = (self.state.selected_index, 0)

    return PiranesiReportTextualApp(controller)


def _run_textual_app(app: Any) -> None:
    app.run()


def _finding_records(report: PiranesiReport) -> tuple[ReportFindingRecord, ...]:
    confirmed_by_id = {finding.finding_id: finding for finding in report.findings}
    return tuple(
        _finding_record(candidate, confirmed_by_id.get(candidate.finding_id))
        for candidate in report.active_findings
    )


def _finding_record(
    candidate: CandidateReportFinding,
    confirmed: CombinedFinding | None,
) -> ReportFindingRecord:
    return ReportFindingRecord(
        finding_id=candidate.finding_id,
        cwe=candidate.cwe,
        title=candidate.title,
        severity=candidate.severity,
        confidence=candidate.confidence,
        taint_source=candidate.taint_source,
        taint_sink=candidate.taint_sink,
        source_location=candidate.source_location,
        sink_location=candidate.sink_location,
        taint_path=tuple(() if confirmed is None else confirmed.taint_path),
        confirmed=confirmed is not None,
        confirmation_status="yes" if confirmed is not None else "no",
        verification_method=None if confirmed is None else confirmed.verification_method,
        patch_diff=None if confirmed is None else confirmed.patch_diff,
        legal_memo_markdown=None if confirmed is None else confirmed.legal_memo_markdown,
        reproducer_script=None if confirmed is None else confirmed.reproducer_script,
    )


def _severity_breakdown(
    findings: list[CandidateReportFinding] | list[ReportFindingRecord],
) -> dict[str, int]:
    counts = dict.fromkeys(_SEVERITY_ORDER, 0)
    for finding in findings:
        severity = finding.severity.lower()
        counts[severity] = counts.get(severity, 0) + 1
    return {severity: count for severity, count in counts.items() if count > 0}


def _finding_detail_text(finding: ReportFindingRecord, *, expanded: bool) -> str:
    lines = [
        f"{finding.cwe}: {finding.title}",
        f"Severity: {finding.severity.upper()}",
        f"Confidence: {finding.confidence:.2f}",
        (
            "Confirmed: "
            f"{finding.confirmation_status}"
            + (
                f" ({finding.verification_method})"
                if finding.verification_method is not None
                else ""
            )
        ),
        "",
        (
            "Source: "
            f"{finding.taint_source} "
            f"({finding.source_location.file}:{finding.source_location.line})"
        ),
    ]
    if expanded:
        for step in finding.taint_path:
            step_line = f"  -> {step.operation} ({step.location.file}:{step.location.line})"
            if step.sanitizer_applied:
                step_line += f" [{step.sanitizer_applied}]"
            lines.append(step_line)
    elif finding.taint_path:
        lines.append(f"  -> {len(finding.taint_path)} intermediate step(s)")
    lines.append(
        f"Sink: {finding.taint_sink} ({finding.sink_location.file}:{finding.sink_location.line})"
    )
    return "\n".join(lines)


def _is_tty(stream: TextIO) -> bool:
    isatty = getattr(stream, "isatty", None)
    return bool(isatty()) if callable(isatty) else False


_KEY_ACTIONS = {
    "j": "move_down",
    "k": "move_up",
    "down": "move_down",
    "up": "move_up",
    "enter": "toggle_expand",
    "/": "focus_search",
    "p": "show_patch",
    "l": "show_legal",
    "r": "show_reproducer",
    "s": "suppress_finding",
    "e": "export_markdown",
    "f": "cycle_filter",
    "q": "quit_viewer",
}


__all__ = [
    "FilterMode",
    "ReportTUIController",
    "ReportViewMode",
    "create_textual_app",
    "dispatch_keybinding",
    "display_report",
    "render_rich_fallback",
]
