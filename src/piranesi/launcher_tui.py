from __future__ import annotations

import importlib
import shlex
import subprocess
import threading
from dataclasses import dataclass
from enum import StrEnum
from pathlib import Path
from typing import Any, ClassVar, cast

_ASCII_BANNER = r"""
██████╗ ██╗██████╗  █████╗ ███╗   ██╗███████╗███████╗██╗
██╔══██╗██║██╔══██╗██╔══██╗████╗  ██║██╔════╝██╔════╝██║
██████╔╝██║██████╔╝███████║██╔██╗ ██║█████╗  ███████╗██║
██╔═══╝ ██║██╔══██╗██╔══██║██║╚██╗██║██╔══╝  ╚════██║██║
██║     ██║██║  ██║██║  ██║██║ ╚████║███████╗███████║██║
╚═╝     ╚═╝╚═╝  ╚═╝╚═╝  ╚═╝╚═╝  ╚═══╝╚══════╝╚══════╝╚═╝
"""


class LauncherAction(StrEnum):
    RUN = "run"
    REPORT_TUI = "report_tui"
    SUMMARY = "summary"
    DOCTOR = "doctor"
    QUIT = "quit"


@dataclass(frozen=True, slots=True)
class LauncherSelection:
    action: LauncherAction
    target_dir: Path
    output_dir: Path
    config_path: Path
    trace_path: Path
    resume: bool = False
    no_execute: bool = False


def launch_cli_tui(
    *,
    target_dir: Path,
    output_dir: Path,
    config_path: Path,
    trace_path: Path,
) -> LauncherSelection | None:
    app_module = importlib.import_module("textual.app")
    binding_module = importlib.import_module("textual.binding")
    containers_module = importlib.import_module("textual.containers")
    screen_module = importlib.import_module("textual.screen")
    widgets_module = importlib.import_module("textual.widgets")

    App = app_module.App
    Binding = binding_module.Binding
    Horizontal = containers_module.Horizontal
    Vertical = containers_module.Vertical
    ModalScreen = screen_module.ModalScreen
    DataTable = widgets_module.DataTable
    Footer = widgets_module.Footer
    Static = widgets_module.Static

    class DirectoryPickerScreen(ModalScreen[Path | None]):
        BINDINGS: ClassVar[list[Any]] = [
            Binding("up", "move_up", show=False),
            Binding("down", "move_down", show=False),
            Binding("k", "move_up", show=False),
            Binding("j", "move_down", show=False),
            Binding("left", "parent_dir", show=False),
            Binding("h", "parent_dir", show=False),
            Binding("right", "enter_dir", show=False),
            Binding("l", "enter_dir", show=False),
            Binding("enter", "enter_dir", show=False),
            Binding("space", "select_highlighted", "Select Highlighted", show=True),
            Binding("s", "select_current", "Select Current", show=True),
            Binding("escape", "cancel", show=False),
            Binding("q", "cancel", "Cancel", show=True),
        ]

        CSS = """
        DirectoryPickerScreen {
            align: center middle;
        }

        #picker_modal {
            width: 110;
            height: 30;
            border: round $accent;
            background: $surface;
            padding: 1;
            layout: vertical;
        }

        #picker_path {
            height: auto;
            padding: 0 1;
        }

        #picker_table {
            height: 1fr;
        }

        #picker_hint {
            height: auto;
            color: $text-muted;
            padding: 0 1;
        }
        """

        def __init__(self, *, start_dir: Path) -> None:
            super().__init__()
            self.current_dir = start_dir.resolve(strict=False)
            self._entries: list[Path] = []

        def compose(self) -> Any:
            with Vertical(id="picker_modal"):
                yield Static("Directory Picker", id="picker_title")
                yield Static("", id="picker_path")
                yield DataTable(id="picker_table")
                yield Static(
                    "Navigate: ↑↓/jk | Open: Enter/l | Parent: h/← | "
                    "Select: Space (highlighted), s (current) | q/Esc cancel",
                    id="picker_hint",
                )

        def on_mount(self) -> None:
            table = cast(Any, self.query_one("#picker_table"))
            table.cursor_type = "row"
            table.zebra_stripes = True
            table.add_columns("Folder", "Type")
            self._refresh_table()
            table.focus()

        def action_move_up(self) -> None:
            self._set_row(self._selected_row() - 1)

        def action_move_down(self) -> None:
            self._set_row(self._selected_row() + 1)

        def action_parent_dir(self) -> None:
            parent = self.current_dir.parent
            if parent == self.current_dir:
                return
            self.current_dir = parent
            self._refresh_table()

        def action_enter_dir(self) -> None:
            entry = self._selected_entry()
            if entry is None:
                return
            self.current_dir = entry.resolve(strict=False)
            self._refresh_table()

        def action_select_highlighted(self) -> None:
            entry = self._selected_entry()
            if entry is None:
                self.dismiss(None)
                return
            self.dismiss(entry.resolve(strict=False))

        def action_select_current(self) -> None:
            self.dismiss(self.current_dir.resolve(strict=False))

        def action_cancel(self) -> None:
            self.dismiss(None)

        def _refresh_table(self) -> None:
            path_label = cast(Any, self.query_one("#picker_path"))
            path_label.update(f"Current: {self.current_dir}")
            table = cast(Any, self.query_one("#picker_table"))
            table.clear(columns=False)
            self._entries = _picker_directory_entries(self.current_dir)
            if not self._entries:
                table.add_row("(no subdirectories)", "-")
                return
            for entry in self._entries:
                if entry == self.current_dir.parent and self.current_dir.parent != self.current_dir:
                    table.add_row("..", "parent")
                else:
                    table.add_row(entry.name, "dir")
            self._set_row(0)

        def _selected_row(self) -> int:
            table = cast(Any, self.query_one("#picker_table"))
            coordinate = getattr(table, "cursor_coordinate", None)
            row = getattr(coordinate, "row", 0) if coordinate is not None else 0
            if isinstance(row, int):
                return row
            try:
                return int(row)
            except (TypeError, ValueError):
                return 0

        def _set_row(self, row: int) -> None:
            if not self._entries:
                return
            table = cast(Any, self.query_one("#picker_table"))
            bounded = max(0, min(row, len(self._entries) - 1))
            table.cursor_coordinate = (bounded, 0)

        def _selected_entry(self) -> Path | None:
            if not self._entries:
                return None
            index = self._selected_row()
            if index < 0 or index >= len(self._entries):
                return None
            return self._entries[index]

    class PiranesiLauncherApp(App[LauncherSelection | None]):
        BINDINGS: ClassVar[list[Any]] = [
            Binding("f", "open_path_finder", "Path Finder", show=True),
            Binding("ctrl+o", "open_path_finder", show=False),
            Binding("r", "run_pipeline", "Run", show=True),
            Binding("t", "open_report", "Report", show=True),
            Binding("s", "show_summary", "Summary", show=True),
            Binding("d", "run_doctor", "Doctor", show=True),
            Binding("u", "toggle_resume", "Resume", show=True),
            Binding("n", "toggle_no_execute", "No Execute", show=True),
            Binding("c", "clear_output", "Clear Output", show=True),
            Binding("q", "quit_launcher", "Quit", show=True),
        ]

        CSS = """
        Screen {
            layout: vertical;
        }

        #banner {
            padding: 0 1;
            color: cyan;
        }

        #status {
            padding: 0 1;
            height: auto;
        }

        #body {
            height: 1fr;
        }

        #hint {
            width: 1fr;
            min-width: 42;
            padding: 1;
        }

        #run_output {
            width: 1fr;
            min-width: 42;
            height: 1fr;
            padding: 1;
            overflow: auto;
            border: round $accent;
            display: none;
        }
        """

        def __init__(
            self,
            *,
            start_dir: Path,
            output_dir: Path,
            config_path: Path,
            trace_path: Path,
        ) -> None:
            super().__init__()
            self.target_dir = start_dir
            self.output_dir = output_dir
            self.config_path = config_path
            self.trace_path = trace_path
            self.resume = False
            self.no_execute = False
            self.cwd = Path.cwd().resolve(strict=False)
            self.start_dir = start_dir
            self.target_dir: Path | None = None
            self._status_note = ""
            self._run_in_progress = False
            self._run_log_lines: list[str] = []

        def compose(self) -> Any:
            yield Static(_ASCII_BANNER, id="banner")
            yield Static("", id="status")
            with Horizontal(id="body"), Vertical():
                yield Static("", id="hint")
                yield Static("", id="run_output")
            yield Footer()

        def on_mount(self) -> None:
            self._status_note = "no target selected"
            self._refresh_status()

        def action_open_path_finder(self) -> None:
            start = self.target_dir if self.target_dir is not None else self.start_dir
            if not start.exists() or not start.is_dir():
                start = self.cwd
            self.push_screen(
                DirectoryPickerScreen(start_dir=start),
                self._on_path_finder_closed,
            )

        def action_toggle_resume(self) -> None:
            self.resume = not self.resume
            self._refresh_status()

        def action_toggle_no_execute(self) -> None:
            self.no_execute = not self.no_execute
            self._refresh_status()

        def action_run_pipeline(self) -> None:
            if self.target_dir is None:
                self._status_note = "select a target first (press f or Ctrl+O)"
                self._refresh_status()
                return
            if self._run_in_progress:
                self._status_note = "pipeline already running"
                self._refresh_status()
                return
            command = _build_pipeline_command(
                target_dir=self.target_dir,
                output_dir=self.output_dir,
                config_path=self.config_path,
                trace_path=self.trace_path,
                resume=self.resume,
                no_execute=self.no_execute,
            )
            self._run_in_progress = True
            self._status_note = "pipeline running"
            self._append_run_line(f"$ {shlex.join(command)}")
            self._append_run_line("")
            self._refresh_status()
            thread = threading.Thread(
                target=self._run_pipeline_thread,
                args=(command,),
                daemon=True,
            )
            thread.start()

        def action_open_report(self) -> None:
            self.exit(self._selection(LauncherAction.REPORT_TUI))

        def action_show_summary(self) -> None:
            self.exit(self._selection(LauncherAction.SUMMARY))

        def action_run_doctor(self) -> None:
            if self.target_dir is None:
                self._status_note = "select a target first (press f or Ctrl+O)"
                self._refresh_status()
                return
            self.exit(self._selection(LauncherAction.DOCTOR))

        def action_quit_launcher(self) -> None:
            self.exit(self._selection(LauncherAction.QUIT))

        def action_clear_output(self) -> None:
            self._run_log_lines.clear()
            self._refresh_run_output()

        def _selection(self, action: LauncherAction) -> LauncherSelection:
            return LauncherSelection(
                action=action,
                target_dir=self.target_dir or self.start_dir,
                output_dir=self.output_dir,
                config_path=self.config_path,
                trace_path=self.trace_path,
                resume=self.resume,
                no_execute=self.no_execute,
            )

        def _on_path_finder_closed(self, selected: Path | None) -> None:
            if selected is None:
                return
            resolved = selected.resolve(strict=False)
            self.target_dir = resolved
            self._status_note = "target selected via path finder"
            self._refresh_status()

        def _refresh_status(self) -> None:
            status = cast(Any, self.query_one("#status"))
            hint = cast(Any, self.query_one("#hint"))
            status.update(
                "\n".join(
                    (
                        f"Target: {self.target_dir or '(not selected)'}",
                        f"Output: {self.output_dir}",
                        f"Config: {self.config_path}",
                        f"Trace: {self.trace_path}",
                        (
                            "Options: "
                            f"resume={'on' if self.resume else 'off'} | "
                            f"no-execute={'on' if self.no_execute else 'off'}"
                        ),
                        f"Pipeline: {'running' if self._run_in_progress else 'idle'}",
                        f"Status: {self._status_note or 'ready'}",
                    )
                )
            )
            hint.update(
                "\n".join(
                    (
                        "Actions",
                        "  f / ctrl+o       open path finder overlay",
                        "  r run pipeline",
                        "  t open report viewer",
                        "  s show summary",
                        "  d run doctor",
                        "  u toggle resume",
                        "  n toggle no-execute",
                        "  c clear output",
                        "  q quit",
                    )
                )
            )
            self._refresh_run_output()

        def _refresh_run_output(self) -> None:
            output = cast(Any, self.query_one("#run_output"))
            has_output = self._run_in_progress or bool(self._run_log_lines)
            output.styles.display = "block" if has_output else "none"
            if not has_output:
                return
            output.update("\n".join(self._run_log_lines[-240:]))

        def _run_pipeline_thread(self, command: list[str]) -> None:
            try:
                process = subprocess.Popen(
                    command,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT,
                    text=True,
                    bufsize=1,
                )
            except OSError as exc:
                self.call_from_thread(self._on_pipeline_error, f"failed to launch: {exc}")
                return

            assert process.stdout is not None
            for line in process.stdout:
                self.call_from_thread(self._append_run_line, line.rstrip("\n"))
            return_code = process.wait()
            self.call_from_thread(self._on_pipeline_complete, return_code)

        def _append_run_line(self, line: str) -> None:
            self._run_log_lines.append(line)
            if len(self._run_log_lines) > 2000:
                self._run_log_lines = self._run_log_lines[-2000:]
            self._refresh_run_output()

        def _on_pipeline_complete(self, return_code: int) -> None:
            self._run_in_progress = False
            if return_code == 0:
                self._status_note = "pipeline completed successfully"
                self._append_run_line("")
                self._append_run_line("[piranesi] pipeline completed successfully")
            else:
                self._status_note = f"pipeline failed with exit code {return_code}"
                self._append_run_line("")
                self._append_run_line(
                    f"[piranesi] pipeline failed with exit code {return_code}"
                )
            self._refresh_status()

        def _on_pipeline_error(self, message: str) -> None:
            self._run_in_progress = False
            self._status_note = message
            self._append_run_line(f"[piranesi] {message}")
            self._refresh_status()

    app = PiranesiLauncherApp(
        start_dir=target_dir,
        output_dir=output_dir,
        config_path=config_path,
        trace_path=trace_path,
    )
    result = app.run()
    return result if isinstance(result, LauncherSelection) else None


def _picker_directory_entries(current_dir: Path) -> list[Path]:
    entries: list[Path] = []
    parent = current_dir.parent
    if parent != current_dir:
        entries.append(parent.resolve(strict=False))
    try:
        children = sorted(
            (path for path in current_dir.iterdir() if path.is_dir()),
            key=lambda path: path.name.casefold(),
        )
    except OSError:
        children = []
    entries.extend(path.resolve(strict=False) for path in children)
    return entries


def _build_pipeline_command(
    *,
    target_dir: Path,
    output_dir: Path,
    config_path: Path,
    trace_path: Path,
    resume: bool,
    no_execute: bool,
) -> list[str]:
    command = [
        "uv",
        "run",
        "piranesi",
        "pipeline",
        "run",
        str(target_dir),
        "-o",
        str(output_dir),
        "--config",
        str(config_path),
        "--trace",
        str(trace_path),
        "--authorized",
        "--yes",
    ]
    if resume:
        command.append("--resume")
    if no_execute:
        command.append("--no-execute")
    return command


__all__ = ["LauncherAction", "LauncherSelection", "launch_cli_tui"]
