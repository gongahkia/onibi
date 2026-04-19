from __future__ import annotations

import importlib
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
    widgets_module = importlib.import_module("textual.widgets")

    App = app_module.App
    Binding = binding_module.Binding
    Horizontal = containers_module.Horizontal
    DataTable = widgets_module.DataTable
    Footer = widgets_module.Footer
    Static = widgets_module.Static

    class PiranesiLauncherApp(App[LauncherSelection | None]):
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
            Binding("space", "select_target", "Select Target", show=True),
            Binding("r", "run_pipeline", "Run", show=True),
            Binding("t", "open_report", "Report", show=True),
            Binding("s", "show_summary", "Summary", show=True),
            Binding("d", "run_doctor", "Doctor", show=True),
            Binding("u", "toggle_resume", "Resume", show=True),
            Binding("n", "toggle_no_execute", "No Execute", show=True),
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

        #directories {
            width: 1fr;
            min-width: 52;
        }

        #hint {
            width: 1fr;
            min-width: 42;
            padding: 1;
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
            self.current_dir = start_dir
            self.target_dir = start_dir
            self.output_dir = output_dir
            self.config_path = config_path
            self.trace_path = trace_path
            self.resume = False
            self.no_execute = False
            self._entries: list[Path] = []

        def compose(self) -> Any:
            yield Static(_ASCII_BANNER, id="banner")
            yield Static("", id="status")
            with Horizontal(id="body"):
                yield DataTable(id="directories")
                yield Static("", id="hint")
            yield Footer()

        def on_mount(self) -> None:
            table = cast(Any, self.query_one("#directories"))
            table.cursor_type = "row"
            table.zebra_stripes = True
            table.add_columns("Directory", "Type")
            self._refresh_directory_table()
            self._refresh_status()
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
            self._refresh_directory_table()

        def action_enter_dir(self) -> None:
            entry = self._selected_entry()
            if entry is None:
                return
            self.current_dir = entry
            self._refresh_directory_table()

        def action_select_target(self) -> None:
            entry = self._selected_entry()
            self.target_dir = self.current_dir if entry is None else entry
            self._refresh_status()

        def action_toggle_resume(self) -> None:
            self.resume = not self.resume
            self._refresh_status()

        def action_toggle_no_execute(self) -> None:
            self.no_execute = not self.no_execute
            self._refresh_status()

        def action_run_pipeline(self) -> None:
            self.exit(self._selection(LauncherAction.RUN))

        def action_open_report(self) -> None:
            self.exit(self._selection(LauncherAction.REPORT_TUI))

        def action_show_summary(self) -> None:
            self.exit(self._selection(LauncherAction.SUMMARY))

        def action_run_doctor(self) -> None:
            self.exit(self._selection(LauncherAction.DOCTOR))

        def action_quit_launcher(self) -> None:
            self.exit(self._selection(LauncherAction.QUIT))

        def _selection(self, action: LauncherAction) -> LauncherSelection:
            return LauncherSelection(
                action=action,
                target_dir=self.target_dir,
                output_dir=self.output_dir,
                config_path=self.config_path,
                trace_path=self.trace_path,
                resume=self.resume,
                no_execute=self.no_execute,
            )

        def _refresh_directory_table(self) -> None:
            table = cast(Any, self.query_one("#directories"))
            table.clear(columns=False)
            self._entries = _directory_entries(self.current_dir)
            if not self._entries:
                table.add_row("(no subdirectories)", "-")
                self._refresh_status()
                return
            for entry in self._entries:
                if entry == self.current_dir.parent and self.current_dir.parent != self.current_dir:
                    table.add_row("..", "parent")
                else:
                    table.add_row(entry.name, "dir")
            self._set_row(0)
            self._refresh_status()

        def _refresh_status(self) -> None:
            status = cast(Any, self.query_one("#status"))
            hint = cast(Any, self.query_one("#hint"))
            status.update(
                "\n".join(
                    (
                        f"Current: {self.current_dir}",
                        f"Target: {self.target_dir}",
                        f"Output: {self.output_dir}",
                        f"Config: {self.config_path}",
                        f"Trace: {self.trace_path}",
                        (
                            "Options: "
                            f"resume={'on' if self.resume else 'off'} | "
                            f"no-execute={'on' if self.no_execute else 'off'}"
                        ),
                    )
                )
            )
            hint.update(
                "\n".join(
                    (
                        "Navigation",
                        "  up/down or j/k  move",
                        "  left/h           parent directory",
                        "  right/l/enter    enter directory",
                        "  space            set selected directory as target",
                        "",
                        "Actions",
                        "  r run pipeline",
                        "  t open report viewer",
                        "  s show summary",
                        "  d run doctor",
                        "  u toggle resume",
                        "  n toggle no-execute",
                        "  q quit",
                    )
                )
            )

        def _selected_row(self) -> int:
            table = cast(Any, self.query_one("#directories"))
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
            table = cast(Any, self.query_one("#directories"))
            bounded = max(0, min(row, len(self._entries) - 1))
            table.cursor_coordinate = (bounded, 0)

        def _selected_entry(self) -> Path | None:
            if not self._entries:
                return None
            index = self._selected_row()
            if index < 0 or index >= len(self._entries):
                return None
            return self._entries[index]

    app = PiranesiLauncherApp(
        start_dir=target_dir,
        output_dir=output_dir,
        config_path=config_path,
        trace_path=trace_path,
    )
    result = app.run()
    return result if isinstance(result, LauncherSelection) else None


def _directory_entries(current_dir: Path) -> list[Path]:
    entries: list[Path] = []
    parent = current_dir.parent
    if parent != current_dir:
        entries.append(parent)

    try:
        children = sorted(
            (path for path in current_dir.iterdir() if path.is_dir()),
            key=lambda path: path.name.casefold(),
        )
    except OSError:
        children = []
    entries.extend(children)
    return entries


__all__ = ["LauncherAction", "LauncherSelection", "launch_cli_tui"]
