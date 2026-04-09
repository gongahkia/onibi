from __future__ import annotations

from collections.abc import Mapping

from rich import box
from rich.console import Console
from rich.progress import BarColumn, Progress, SpinnerColumn, TextColumn, TimeElapsedColumn
from rich.status import Status
from rich.table import Table

console = Console(stderr=True)


def stage_header(name: str) -> None:
    console.rule(f"[bold]{name}[/bold]", style="dim")


def file_progress(total: int, description: str = "working") -> Progress:
    return Progress(
        SpinnerColumn(),
        TextColumn("[progress.description]{task.description}"),
        BarColumn(),
        TextColumn("{task.completed}/{task.total}"),
        TimeElapsedColumn(),
        console=console,
    )


def finding_spinner(finding_id: str, description: str) -> Status:
    return console.status(f"[{finding_id}] {description}", spinner="dots")


def print_summary_table(title: str, rows: Mapping[str, object]) -> None:
    table = Table(title=title, box=box.ROUNDED)
    table.add_column("Field", style="bold")
    table.add_column("Value")
    for key, value in rows.items():
        table.add_row(key, str(value))
    console.print(table)
