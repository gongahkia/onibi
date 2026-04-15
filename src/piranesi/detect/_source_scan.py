from __future__ import annotations

from bisect import bisect_right
from collections.abc import Sequence
from dataclasses import dataclass
from pathlib import Path

from piranesi.models import SourceLocation
from piranesi.scan.transpile import SourceMap

_SOURCE_FILE_EXTENSIONS = frozenset({".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs"})
_IGNORED_PATH_SEGMENTS = frozenset(
    {
        "node_modules",
        ".next",
        "dist",
        "build",
        "coverage",
        ".git",
        "__pycache__",
        # piranesi output dirs
        "piranesi-output",
        ".piranesi-cache",
        ".piranesi-out",
    }
)
_PIRANESI_TRACE_PREFIX = ".piranesi-trace"


@dataclass(frozen=True, slots=True)
class ScannedSourceFile:
    path: Path
    root: Path
    text: str
    lines: tuple[str, ...]
    line_starts: tuple[int, ...]

    @classmethod
    def load(cls, path: Path, *, root: Path) -> ScannedSourceFile | None:
        try:
            text = path.read_text(encoding="utf-8")
        except OSError:
            return None
        return cls(
            path=path.resolve(strict=False),
            root=root.resolve(strict=False),
            text=text,
            lines=tuple(text.splitlines()),
            line_starts=_line_starts(text),
        )

    @property
    def relative_path(self) -> str:
        try:
            return str(self.path.relative_to(self.root))
        except ValueError:
            return str(self.path)

    def location_for_index(self, index: int, *, snippet: str | None = None) -> SourceLocation:
        line_number = bisect_right(self.line_starts, index)
        line_start = self.line_starts[max(0, line_number - 1)]
        return SourceLocation(
            file=str(self.path),
            line=max(1, line_number),
            column=max(1, index - line_start + 1),
            snippet=snippet or self._line_text(max(1, line_number)),
        )

    def location_for_line(
        self,
        line_number: int,
        *,
        column: int = 1,
        snippet: str | None = None,
    ) -> SourceLocation:
        return SourceLocation(
            file=str(self.path),
            line=max(1, line_number),
            column=max(1, column),
            snippet=snippet or self._line_text(max(1, line_number)),
        )

    def _line_text(self, line_number: int) -> str:
        if 1 <= line_number <= len(self.lines):
            return self.lines[line_number - 1]
        return ""


def iter_scanned_source_files(
    project_root: str | Path,
    *,
    source_map: SourceMap | None = None,
    files: Sequence[Path] | None = None,
) -> tuple[ScannedSourceFile, ...]:
    root = Path(project_root).resolve(strict=False)
    candidates = _candidate_files(root, source_map=source_map, files=files)
    scanned: list[ScannedSourceFile] = []
    for path in candidates:
        if path.suffix.lower() not in _SOURCE_FILE_EXTENSIONS:
            continue
        if _is_ignored_path(path):
            continue
        scanned_file = ScannedSourceFile.load(path, root=root)
        if scanned_file is not None:
            scanned.append(scanned_file)
    return tuple(scanned)


def _candidate_files(
    project_root: Path,
    *,
    source_map: SourceMap | None,
    files: Sequence[Path] | None,
) -> tuple[Path, ...]:
    if files is not None:
        return tuple(path.resolve(strict=False) for path in files if path.exists())
    if source_map is not None:
        return source_map.original_files()
    return tuple(
        path.resolve(strict=False) for path in sorted(project_root.rglob("*")) if path.is_file()
    )


def _is_ignored_path(path: Path) -> bool:
    return any(
        part in _IGNORED_PATH_SEGMENTS or part.startswith(_PIRANESI_TRACE_PREFIX)
        for part in path.parts
    )


def _line_starts(text: str) -> tuple[int, ...]:
    starts = [0]
    for index, char in enumerate(text):
        if char == "\n":
            starts.append(index + 1)
    return tuple(starts)


__all__ = ["ScannedSourceFile", "iter_scanned_source_files"]
