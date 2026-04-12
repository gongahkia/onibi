from __future__ import annotations

import io
import sys
from collections import Counter
from collections.abc import Iterable, Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import Any, ClassVar, TextIO

from rich import box
from rich.console import Console, Group
from rich.panel import Panel
from rich.table import Table

from piranesi.legal.frameworks import FRAMEWORK_BY_KEY, FRAMEWORKS, FrameworkSpec
from piranesi.models import RegulatoryObligation
from piranesi.report.renderer import CombinedFinding, PiranesiReport

_SEVERITY_ORDER = ("critical", "high", "medium", "low", "informational")
_SEVERITY_RANK = {severity: index for index, severity in enumerate(_SEVERITY_ORDER)}
_LANGUAGE_LABELS = {
    ".ts": "TypeScript",
    ".tsx": "TypeScript",
    ".js": "JavaScript",
    ".jsx": "JavaScript",
    ".py": "Python",
    ".go": "Go",
    ".java": "Java",
}
_CONSEQUENCE_ACTIONS = {
    "document": "Document the finding and the remediation decision in compliance records.",
    "notify_individuals": (
        "Prepare affected-individual communications where notification is required."
    ),
    "review": "Review adjacent systems and harden related preventive controls.",
    "remediate": "Remediate the vulnerable code path and verify the exploit is closed.",
    "notify_regulator": (
        "Prepare regulator notification materials for the applicable reporting window."
    ),
}


@dataclass(frozen=True)
class OwaspCategorySpec:
    key: str
    title: str
    cwes: tuple[str, ...]
    blind_spot_note: str


_FRAMEWORKS: tuple[FrameworkSpec, ...] = FRAMEWORKS
_FRAMEWORK_BY_KEY = FRAMEWORK_BY_KEY
_OWASP_TOP_10: tuple[OwaspCategorySpec, ...] = (
    OwaspCategorySpec(
        "A01", "Broken Access Control", ("CWE-22",), "review access-control coverage"
    ),
    OwaspCategorySpec("A02", "Cryptographic Failures", (), "no encoded detection rules"),
    OwaspCategorySpec(
        "A03",
        "Injection",
        ("CWE-78", "CWE-79", "CWE-89", "CWE-94", "CWE-1321"),
        "review rule coverage for additional injection classes",
    ),
    OwaspCategorySpec("A04", "Insecure Design", (), "architectural review required"),
    OwaspCategorySpec(
        "A05",
        "Security Misconfiguration",
        ("CWE-319", "CWE-693", "CWE-942", "CWE-1004", "CWE-1021", "CWE-614"),
        "no confirmed findings",
    ),
    OwaspCategorySpec(
        "A06",
        "Vulnerable and Outdated Components",
        ("CWE-1395",),
        "no confirmed SCA findings",
    ),
    OwaspCategorySpec(
        "A07", "Identification and Authentication Failures", (), "no encoded detection rules"
    ),
    OwaspCategorySpec(
        "A08",
        "Software and Data Integrity Failures",
        ("CWE-502", "CWE-1321", "CWE-1395"),
        "review integrity rule coverage",
    ),
    OwaspCategorySpec(
        "A09", "Security Logging and Monitoring Failures", (), "no encoded detection rules"
    ),
    OwaspCategorySpec("A10", "Server-Side Request Forgery", ("CWE-918",), "no confirmed findings"),
)


def render_compliance_report(report: PiranesiReport) -> str:
    buffer = io.StringIO()
    _console(buffer).print(_compliance_renderable(report))
    return buffer.getvalue()


def print_compliance_report(report: PiranesiReport, *, file: TextIO | None = None) -> None:
    target = sys.stdout if file is None else file
    _console(target).print(_compliance_renderable(report))


def render_compliance_summary(report: PiranesiReport, *, include_all: bool = False) -> str:
    summaries = _framework_summaries(report)
    all_frameworks = (
        _FRAMEWORKS
        if include_all
        else [fw for fw in _FRAMEWORKS if any(s.framework.key == fw.key for s in summaries)]
    )
    lines: list[str] = []
    lines.append(f"Frameworks assessed:  {len(all_frameworks)}")
    lines.append("")
    for fw in all_frameworks:
        matching = [s for s in summaries if s.framework.key == fw.key]
        count = matching[0].total_findings if matching else 0
        lines.append(f"  {fw.short_label}: {count} finding(s)")
    lines.append("")
    lines.append("Top 3 Remediation Priorities:")
    priority_findings = sorted(
        report.findings,
        key=lambda f: (_SEVERITY_RANK.get(f.severity.lower(), len(_SEVERITY_ORDER)), f.finding_id),
    )[:3]
    for i, finding in enumerate(priority_findings, 1):
        lines.append(f"  {i}. [{finding.severity.upper()}] {finding.cwe} — {finding.title}")
    return "\n".join(lines)


def render_attestation(report: PiranesiReport) -> str:
    file_count = report.scan_metadata.files_parsed or len(report.files_scanned)
    languages = ", ".join(_scan_languages(report)) or "Unknown"
    fixed_count = sum(1 for finding in report.findings if finding.patch_diff)
    lines = [
        "# Security Scan Attestation",
        "",
        f"**Project:** {_project_name(report.target)}",
        f"**Scan Date:** {report.scan_metadata.timestamp}",
        f"**Tool:** Piranesi v{report.appendix.piranesi_version}",
        f"**Scope:** {file_count} files across {languages}",
        "",
        "## Summary",
        f"- {report.executive_summary.findings_detected} findings detected",
        f"- {report.executive_summary.findings_confirmed} confirmed via exploit verification",
        f"- {report.executive_summary.suppressed_findings} suppressed (with documented rationale)",
        f"- {fixed_count} with auto-generated patches",
        "",
        "## Regulatory Coverage",
        *[f"- {framework.short_label}" for framework in _FRAMEWORKS],
        "",
        "## Limitations",
        "This scan covers static analysis of source code only. It does not assess:",
        "- Runtime configuration",
        "- Infrastructure security",
        "- Business logic flaws",
        "- Authentication/authorization design",
        "",
        "DISCLAIMER: This analysis is informational only. It is not legal advice.",
        "Consult qualified legal counsel for regulatory compliance decisions.",
        "",
    ]
    return "\n".join(lines)


def launch_compliance_tui(report: PiranesiReport) -> None:
    if not sys.stdout.isatty() or not sys.stdin.isatty():
        print_compliance_report(report)
        return

    try:
        import importlib

        app_module = importlib.import_module("textual.app")
        binding_module = importlib.import_module("textual.binding")
        containers_module = importlib.import_module("textual.containers")
        widgets_module = importlib.import_module("textual.widgets")
    except ImportError:
        print_compliance_report(report)
        return

    App = app_module.App
    Binding = binding_module.Binding
    Horizontal = containers_module.Horizontal
    Vertical = containers_module.Vertical
    DataTable = widgets_module.DataTable
    Footer = widgets_module.Footer
    Header = widgets_module.Header
    Label = widgets_module.Label
    ListItem = widgets_module.ListItem
    ListView = widgets_module.ListView
    Static = widgets_module.Static

    framework_summaries = _framework_summaries(report)
    if not framework_summaries:
        print_compliance_report(report)
        return

    class ComplianceDashboard(App):  # type: ignore[misc,valid-type]
        TITLE = "Piranesi Compliance Dashboard"
        BINDINGS: ClassVar[list[Any]] = [
            Binding("q", "quit", "Quit"),
            Binding("j", "cursor_down", "Down", show=False),
            Binding("k", "cursor_up", "Up", show=False),
        ]

        CSS = """
        Screen {
            layout: vertical;
        }
        #main {
            height: 1fr;
        }
        #frameworks {
            width: 28;
            border: round $accent;
        }
        #content {
            width: 1fr;
        }
        #summary {
            height: auto;
            border: round $accent;
            padding: 1;
        }
        #findings {
            height: 1fr;
            border: round $accent;
        }
        #details {
            height: 12;
            border: round $accent;
            padding: 1;
        }
        """

        def __init__(self, dashboard_report: PiranesiReport) -> None:
            super().__init__()
            self._report = dashboard_report
            self._current_framework = framework_summaries[0].framework.key
            self._findings_table: Any | None = None

        def compose(self) -> Iterable[Any]:
            yield Header()
            framework_items = [
                ListItem(
                    Label(summary.framework.short_label), id=f"framework-{summary.framework.key}"
                )
                for summary in framework_summaries
            ]
            with Horizontal(id="main"):
                yield ListView(*framework_items, id="frameworks")
                with Vertical(id="content"):
                    yield Static(id="summary")
                    yield DataTable(id="findings")
                    yield Static(id="details")
            yield Footer()

        def on_mount(self) -> None:
            list_view = self.query_one("#frameworks")
            list_view.index = 0
            findings_table = self.query_one("#findings")
            findings_table.add_columns("Finding", "Severity", "Obligations", "Timeline")
            self._findings_table = findings_table
            self._refresh_framework()

        def on_list_view_selected(self, event: Any) -> None:
            item_id = getattr(event.item, "id", "")
            framework_key = item_id.replace("framework-", "", 1)
            if framework_key in _FRAMEWORK_BY_KEY:
                self._current_framework = framework_key
                self._refresh_framework()

        def on_data_table_row_selected(self, event: Any) -> None:
            self._refresh_details(str(event.row_key.value))

        def _refresh_framework(self) -> None:
            summary = next(
                item
                for item in framework_summaries
                if item.framework.key == self._current_framework
            )
            summary_widget = self.query_one("#summary")
            summary_widget.update(
                "\n".join(
                    [
                        summary.framework.long_label,
                        f"Findings: {summary.total_findings}",
                        f"Severity: {_format_severity_breakdown(summary.severity_breakdown)}",
                        f"Timelines: {summary.notification_timelines or 'Not specified'}",
                        f"Penalty: {summary.penalty_exposure or 'Not specified'}",
                    ]
                )
            )
            assert self._findings_table is not None
            self._findings_table.clear()
            for finding in summary.findings:
                obligations = _framework_obligations_for_finding(finding, summary.framework.key)
                timelines = sorted(
                    {
                        obligation.notification_timeline
                        for obligation in obligations
                        if obligation.notification_timeline
                    }
                )
                self._findings_table.add_row(
                    finding.finding_id,
                    finding.severity.upper(),
                    ", ".join(sorted({obligation.section for obligation in obligations})) or "-",
                    ", ".join(timelines) or "-",
                    key=finding.finding_id,
                )
            if summary.findings:
                self._refresh_details(summary.findings[0].finding_id)
            else:
                self.query_one("#details").update("No findings for this framework.")

        def _refresh_details(self, finding_id: str) -> None:
            details_widget = self.query_one("#details")
            finding = next(
                (
                    item
                    for item in self._report.findings
                    if item.finding_id == finding_id
                    and any(
                        obligation.framework == self._current_framework
                        for obligation in item.regulatory_obligations
                    )
                ),
                None,
            )
            if finding is None:
                details_widget.update("No finding selected.")
                return
            obligations = _framework_obligations_for_finding(finding, self._current_framework)
            details_widget.update(
                "\n".join(
                    [
                        f"{finding.title} ({finding.finding_id})",
                        f"Location: {finding.source_location.file}:{finding.source_location.line}",
                        "Obligations:",
                        *[
                            f"- {obligation.section}: {obligation.obligation_text}"
                            for obligation in obligations
                        ],
                    ]
                )
            )

    ComplianceDashboard(report).run()


def _compliance_renderable(report: PiranesiReport) -> Group:
    renderables: list[Any] = [_coverage_matrix_table(report.findings)]
    framework_panels = _framework_panels(report)
    if framework_panels:
        renderables.extend(framework_panels)
    else:
        renderables.append(
            Panel(
                "No regulatory obligations were triggered for the confirmed findings "
                "in this report.",
                title="Per-Framework Summary",
                border_style="yellow",
            )
        )
    renderables.append(_owasp_gap_table(report))
    return Group(*renderables)


def _coverage_matrix_table(findings: Sequence[CombinedFinding]) -> Table:
    table = Table(title="Regulatory Coverage Matrix", box=box.ROUNDED, expand=True)
    table.add_column("Finding", style="bold")
    for framework_spec in _FRAMEWORKS:
        table.add_column(framework_spec.short_label, justify="center")

    affected_counts = {framework.key: 0 for framework in _FRAMEWORKS}
    for finding in findings:
        frameworks = {
            obligation.framework
            for obligation in finding.regulatory_obligations
            if obligation.framework in _FRAMEWORK_BY_KEY
        }
        for framework_key in frameworks:
            affected_counts[framework_key] += 1
        table.add_row(
            f"{finding.finding_id} ({finding.cwe})",
            *["*" if framework.key in frameworks else "-" for framework in _FRAMEWORKS],
        )

    if findings:
        table.add_section()
    table.add_row(
        "Affected",
        *[str(affected_counts[framework.key]) for framework in _FRAMEWORKS],
        style="bold",
    )
    return table


def _framework_panels(report: PiranesiReport) -> list[Panel]:
    panels: list[Panel] = []
    for summary in _framework_summaries(report):
        metrics = Table(box=box.SIMPLE_HEAVY, expand=True)
        metrics.add_column("Metric", style="bold")
        metrics.add_column("Value")
        metrics.add_row("Total findings", str(summary.total_findings))
        metrics.add_row(
            "Severity breakdown", _format_severity_breakdown(summary.severity_breakdown)
        )
        metrics.add_row("Obligations", summary.obligations or "None")
        metrics.add_row("Required actions", summary.required_actions or "None")
        metrics.add_row("Notification timelines", summary.notification_timelines or "Not specified")
        metrics.add_row("Penalty exposure", summary.penalty_exposure or "Not specified")
        metrics.add_row(
            "Enforcement precedents", summary.enforcement_precedents or "None specified"
        )
        panels.append(Panel(metrics, title=summary.framework.long_label, border_style="cyan"))
    return panels


def _owasp_gap_table(report: PiranesiReport) -> Table:
    table = Table(title="OWASP Top 10 2021 Coverage", box=box.ROUNDED, expand=True)
    table.add_column("Category", style="bold")
    table.add_column("Coverage")
    table.add_column("Blind Spot")

    counts = Counter(finding.cwe for finding in report.active_findings)
    for category in _OWASP_TOP_10:
        matched_cwes = [cwe for cwe in category.cwes if counts[cwe] > 0]
        findings_count = sum(counts[cwe] for cwe in category.cwes)
        if matched_cwes:
            coverage = f"{findings_count} findings ({', '.join(matched_cwes)})"
            blind_spot = "-"
        elif category.cwes:
            coverage = "0 findings"
            blind_spot = category.blind_spot_note
        else:
            coverage = "0 findings"
            blind_spot = category.blind_spot_note
        table.add_row(f"{category.key} {category.title}", coverage, blind_spot)
    return table


@dataclass(frozen=True)
class FrameworkSummary:
    framework: FrameworkSpec
    total_findings: int
    severity_breakdown: dict[str, int]
    obligations: str
    required_actions: str
    notification_timelines: str
    penalty_exposure: str
    enforcement_precedents: str
    findings: tuple[CombinedFinding, ...]


def _framework_summaries(report: PiranesiReport) -> list[FrameworkSummary]:
    summaries: list[FrameworkSummary] = []
    for framework in _FRAMEWORKS:
        findings = [
            finding
            for finding in report.findings
            if any(
                obligation.framework == framework.key
                for obligation in finding.regulatory_obligations
            )
        ]
        if not findings:
            continue
        obligations = [
            obligation
            for finding in findings
            for obligation in finding.regulatory_obligations
            if obligation.framework == framework.key
        ]
        severity_breakdown = Counter(
            sorted(
                (finding.severity.lower() for finding in findings),
                key=lambda severity: _SEVERITY_RANK.get(severity, len(_SEVERITY_ORDER)),
            )
        )
        summaries.append(
            FrameworkSummary(
                framework=framework,
                total_findings=len(findings),
                severity_breakdown=_ordered_severity_breakdown(severity_breakdown),
                obligations="\n".join(
                    f"{obligation.section}: {obligation.obligation_text}"
                    for obligation in _unique_obligations(obligations)
                ),
                required_actions="\n".join(_required_actions(obligations)),
                notification_timelines=", ".join(
                    sorted(
                        {
                            obligation.notification_timeline
                            for obligation in obligations
                            if obligation.notification_timeline
                        }
                    )
                ),
                penalty_exposure="\n".join(
                    sorted(
                        {
                            obligation.penalty_range
                            for obligation in obligations
                            if obligation.penalty_range
                        }
                    )
                ),
                enforcement_precedents="\n".join(
                    sorted(
                        {
                            precedent
                            for obligation in obligations
                            for precedent in obligation.enforcement_precedents
                        }
                    )
                ),
                findings=tuple(findings),
            )
        )
    return summaries


def _framework_obligations_for_finding(
    finding: CombinedFinding,
    framework_key: str,
) -> list[RegulatoryObligation]:
    return [
        obligation
        for obligation in finding.regulatory_obligations
        if obligation.framework == framework_key
    ]


def _required_actions(obligations: Sequence[RegulatoryObligation]) -> list[str]:
    actions: list[str] = []
    seen: set[str] = set()
    for obligation in obligations:
        for consequence in obligation.consequences:
            action = _CONSEQUENCE_ACTIONS.get(consequence)
            if action is not None and action not in seen:
                actions.append(action)
                seen.add(action)
    if not actions:
        actions.append(
            "Review the listed obligations, remediate the vulnerable control, and document closure."
        )
    return actions


def _unique_obligations(
    obligations: Sequence[RegulatoryObligation],
) -> list[RegulatoryObligation]:
    deduped: list[RegulatoryObligation] = []
    seen: set[tuple[str, str]] = set()
    for obligation in obligations:
        key = (obligation.section, obligation.obligation_text)
        if key in seen:
            continue
        seen.add(key)
        deduped.append(obligation)
    return deduped


def _ordered_severity_breakdown(counts: Counter[str]) -> dict[str, int]:
    ordered: dict[str, int] = {}
    for severity in _SEVERITY_ORDER:
        count = counts.get(severity)
        if count:
            ordered[severity] = count
    return ordered


def _format_severity_breakdown(breakdown: dict[str, int]) -> str:
    if not breakdown:
        return "None"
    return ", ".join(f"{severity.upper()} {count}" for severity, count in breakdown.items())


def _scan_languages(report: PiranesiReport) -> list[str]:
    candidates = list(report.files_scanned)
    if not candidates:
        candidates.extend(
            {
                finding.source_location.file
                for finding in report.findings
                if finding.source_location.file
            }
        )
        candidates.extend(
            {
                finding.source_location.file
                for finding in report.active_findings
                if finding.source_location.file
            }
        )
    labels: list[str] = []
    for path_str in candidates:
        suffix = Path(path_str).suffix.lower()
        label = _LANGUAGE_LABELS.get(suffix)
        if label is not None and label not in labels:
            labels.append(label)
    return labels


def _project_name(target: str) -> str:
    name = Path(target).name.strip()
    return name or target


def _console(file: TextIO) -> Console:
    return Console(file=file, force_terminal=False, color_system=None, soft_wrap=True)
