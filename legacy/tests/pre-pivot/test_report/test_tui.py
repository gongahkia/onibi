from __future__ import annotations

import io
from pathlib import Path

from tests._pipeline_fixtures import fixture_artifacts

from piranesi.report.renderer import PiranesiReport, build_report
from piranesi.report.tui import (
    ReportTUIController,
    ReportViewMode,
    dispatch_keybinding,
    display_report,
)


class _TTYStringIO(io.StringIO):
    def __init__(self, *, is_tty: bool) -> None:
        super().__init__()
        self._is_tty = is_tty

    def isatty(self) -> bool:
        return self._is_tty


def test_display_report_falls_back_to_markdown_for_non_tty(tmp_path: Path) -> None:
    report = _build_report(tmp_path)
    output = _TTYStringIO(is_tty=False)

    mode = display_report(report, output_dir=tmp_path, stdout=output)

    assert mode == ReportViewMode.MARKDOWN
    rendered = output.getvalue()
    assert "# Piranesi Security Analysis Report" in rendered
    assert "## Active Candidate Findings" in rendered


def test_display_report_falls_back_to_rich_when_textual_is_missing(
    monkeypatch,
    tmp_path: Path,
) -> None:
    report = _build_report(tmp_path)
    output = _TTYStringIO(is_tty=True)

    def _raise_import_error(controller: ReportTUIController) -> None:
        _ = controller
        raise ImportError("textual not installed")

    monkeypatch.setattr("piranesi.report.tui.create_textual_app", _raise_import_error)

    mode = display_report(report, output_dir=tmp_path, stdout=output)

    assert mode == ReportViewMode.RICH
    rendered = output.getvalue()
    assert "Piranesi Report" in rendered
    assert "Summary: 1/1 findings" in rendered


def test_report_controller_tracks_visible_finding_count(tmp_path: Path) -> None:
    report = _build_report(tmp_path)
    controller = ReportTUIController(report, output_dir=tmp_path)

    assert controller.total_findings == 1
    assert controller.visible_count == 1
    assert controller.summary_text().startswith("Summary: 1/1 findings")


def test_dispatch_keybinding_routes_to_textual_actions() -> None:
    calls: list[str] = []

    class MockTextualApp:
        def action_show_patch(self) -> None:
            calls.append("patch")

        def action_show_legal(self) -> None:
            calls.append("legal")

        def action_show_reproducer(self) -> None:
            calls.append("reproducer")

        def action_suppress_finding(self) -> None:
            calls.append("suppress")

        def action_export_markdown(self) -> None:
            calls.append("export")

        def action_cycle_filter(self) -> None:
            calls.append("filter")

    app = MockTextualApp()

    dispatch_keybinding(app, "p")
    dispatch_keybinding(app, "l")
    dispatch_keybinding(app, "r")
    dispatch_keybinding(app, "s")
    dispatch_keybinding(app, "e")
    dispatch_keybinding(app, "f")

    assert calls == ["patch", "legal", "reproducer", "suppress", "export", "filter"]


def _build_report(tmp_path: Path) -> PiranesiReport:
    artifacts = fixture_artifacts(tmp_path)
    return build_report(
        scan_result=artifacts["scan"],  # type: ignore[arg-type]
        detected_findings=artifacts["detect"].findings,  # type: ignore[attr-defined]
        confirmed_findings=artifacts["verify"].findings,  # type: ignore[attr-defined]
        legal_assessments=artifacts["legal"].assessments,  # type: ignore[attr-defined]
        patch_results=artifacts["patch"].patches,  # type: ignore[attr-defined]
        target_dir=tmp_path,
        total_llm_cost_usd=0.73,
        duration_s=8.5,
        stage_timings_s={
            "scan": 1.0,
            "detect": 1.0,
            "triage": 2.0,
            "verify": 2.0,
            "legal": 1.0,
            "patch": 1.0,
            "report": 0.5,
        },
    )
