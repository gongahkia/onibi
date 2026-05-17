from __future__ import annotations

from pathlib import Path

from piranesi.launcher_tui import (
    _build_pipeline_command,
    _picker_directory_entries,
)


def test_build_pipeline_command_includes_flags(tmp_path: Path) -> None:
    command = _build_pipeline_command(
        target_dir=tmp_path / "target",
        output_dir=tmp_path / "out",
        config_path=tmp_path / "piranesi.toml",
        trace_path=tmp_path / ".trace.jsonl",
        resume=True,
        no_execute=True,
    )
    assert command[:6] == ["uv", "run", "piranesi", "pipeline", "run", str(tmp_path / "target")]
    assert "--authorized" in command
    assert "--yes" in command
    assert "--resume" in command
    assert "--no-execute" in command


def test_picker_directory_entries_include_parent_then_children(tmp_path: Path) -> None:
    current = tmp_path / "project"
    current.mkdir()
    (current / "b").mkdir()
    (current / "a").mkdir()

    entries = _picker_directory_entries(current)

    assert entries[0] == tmp_path.resolve(strict=False)
    assert [entry.name for entry in entries[1:]] == ["a", "b"]
