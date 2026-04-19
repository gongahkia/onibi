from __future__ import annotations

from pathlib import Path

from piranesi.launcher_tui import (
    _autocomplete_directory_candidates,
    _display_path,
    _resolve_input_directory,
)


def test_resolve_input_directory_handles_relative_path(tmp_path: Path) -> None:
    resolved = _resolve_input_directory("src", cwd=tmp_path)
    assert resolved == (tmp_path / "src").resolve(strict=False)


def test_autocomplete_directory_candidates_filters_prefix(tmp_path: Path) -> None:
    (tmp_path / "apps").mkdir()
    (tmp_path / "apple").mkdir()
    (tmp_path / "docs").mkdir()
    (tmp_path / "README.md").write_text("x", encoding="utf-8")

    matches = _autocomplete_directory_candidates(str(tmp_path / "ap"), cwd=tmp_path)
    names = [path.name for path in matches]
    assert names == ["apple", "apps"]


def test_display_path_prefers_relative_from_cwd(tmp_path: Path) -> None:
    nested = (tmp_path / "examples" / "vuln-express").resolve(strict=False)
    rendered = _display_path(nested, cwd=tmp_path)
    assert rendered == "examples/vuln-express"
