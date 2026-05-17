"""Heuristic auto-discovery of project-local sanitizer functions.

Scans source files for functions whose names suggest a sanitizer role
(sanitize*, clean*, escape*, encode*, strip*, quote*, htmlspecialchars-like).
Registers them as SanitizerSpec with reduced confidence so taint analysis can
factor them in without over-trusting unverified user code.
"""

from __future__ import annotations

import re
from collections.abc import Iterable, Sequence
from pathlib import Path

from piranesi.scan.specs import SanitizerKind, SanitizerSpec

DEFAULT_DISCOVERED_CONFIDENCE = 0.5  # below builtin specs, above zero
MAX_SCAN_BYTES = 2_000_000  # skip very large files

# (regex capturing the function name, file suffixes it applies to)
_FN_PATTERNS: tuple[tuple[re.Pattern[str], tuple[str, ...]], ...] = (
    # js/ts: function sanitize_foo(...)
    (
        re.compile(r"\bfunction\s+([A-Za-z_][A-Za-z0-9_$]*)\s*\("),
        (".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs"),
    ),
    # js/ts: const sanitizeFoo = (...) => / function(...)
    (
        re.compile(
            r"\b(?:const|let|var)\s+([A-Za-z_][A-Za-z0-9_$]*)\s*=\s*"
            r"(?:async\s+)?(?:function\s*\(|\([^)]*\)\s*=>|[A-Za-z_][A-Za-z0-9_$]*\s*=>)"
        ),
        (".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs"),
    ),
    # python: def sanitize_foo(...)
    (
        re.compile(r"\bdef\s+([A-Za-z_][A-Za-z0-9_]*)\s*\("),
        (".py",),
    ),
    # go: func Sanitize(...) / func (r Recv) Sanitize(...)
    (
        re.compile(r"\bfunc\s+(?:\([^)]*\)\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\("),
        (".go",),
    ),
    # java: public ... Sanitize(...)
    (
        re.compile(
            r"\b(?:public|private|protected|static|final|\s)+[\w<>\[\],\s]+?\s+"
            r"([A-Za-z_][A-Za-z0-9_]*)\s*\([^)]*\)\s*\{"
        ),
        (".java",),
    ),
    # php: function sanitize_foo(...)
    (
        re.compile(r"\bfunction\s+([A-Za-z_][A-Za-z0-9_]*)\s*\("),
        (".php",),
    ),
    # ruby: def sanitize_foo
    (
        re.compile(r"\bdef\s+([A-Za-z_][A-Za-z0-9_?!]*)"),
        (".rb",),
    ),
)

# keyword-suffix -> (SanitizerKind, tuple[cwe])
_KEYWORD_MAP: tuple[tuple[re.Pattern[str], SanitizerKind, tuple[str, ...]], ...] = (
    (re.compile(r"(?i)html[_]?escape|escape[_]?html"), SanitizerKind.ESCAPE, ("CWE-79",)),
    (
        re.compile(r"(?i)escape[_]?shell|shell[_]?escape|shell[_]?quote"),
        SanitizerKind.ESCAPE,
        ("CWE-78",),
    ),
    (
        re.compile(r"(?i)escape[_]?sql|sql[_]?escape|quote[_]?sql"),
        SanitizerKind.ESCAPE,
        ("CWE-89",),
    ),
    (
        re.compile(r"(?i)sanitize[_]?html|clean[_]?html|strip[_]?tags|strip[_]?html"),
        SanitizerKind.SANITIZE,
        ("CWE-79",),
    ),
    (re.compile(r"(?i)sanitize[_]?sql|clean[_]?sql"), SanitizerKind.SANITIZE, ("CWE-89",)),
    (
        re.compile(r"(?i)sanitize[_]?path|normalize[_]?path|clean[_]?path"),
        SanitizerKind.NORMALIZE,
        ("CWE-22",),
    ),
    (
        re.compile(r"(?i)sanitize[_]?url|validate[_]?url|check[_]?url"),
        SanitizerKind.VALIDATE,
        ("CWE-601", "CWE-918"),
    ),
    (
        re.compile(r"(?i)validate[_]?input|check[_]?input|validate[_]?param"),
        SanitizerKind.VALIDATE,
        ("CWE-79", "CWE-89", "CWE-22"),
    ),
    (re.compile(r"(?i)^sanitize|^clean"), SanitizerKind.SANITIZE, ("CWE-79", "CWE-89", "CWE-78")),
    (re.compile(r"(?i)^escape|^encode"), SanitizerKind.ESCAPE, ("CWE-79",)),
    (re.compile(r"(?i)^strip"), SanitizerKind.SANITIZE, ("CWE-79",)),
    (re.compile(r"(?i)^quote"), SanitizerKind.ESCAPE, ("CWE-78", "CWE-89")),
    (re.compile(r"(?i)^filter"), SanitizerKind.VALIDATE, ("CWE-79", "CWE-89")),
)

_EXCLUDED_DIRS = frozenset(
    {
        "node_modules",
        ".git",
        ".venv",
        "venv",
        "__pycache__",
        ".pytest_cache",
        "dist",
        "build",
        ".next",
        "target",
        "vendor",
        ".piranesi-out",
        ".piranesi-cache",
        "piranesi-output",
    }
)
_PIRANESI_TRACE_PREFIX = ".piranesi-trace"


def _infer_kind(name: str) -> tuple[SanitizerKind, tuple[str, ...]] | None:
    for rx, kind, mitigates in _KEYWORD_MAP:
        if rx.search(name):
            return kind, mitigates
    return None


def _iter_source_files(roots: Iterable[Path]) -> Iterable[Path]:
    suffixes = {s for _, exts in _FN_PATTERNS for s in exts}
    for root in roots:
        if not root.exists():
            continue
        if root.is_file():
            if root.suffix in suffixes:
                yield root
            continue
        for path in root.rglob("*"):
            if not path.is_file():
                continue
            if path.suffix not in suffixes:
                continue
            if _is_excluded_path(path):
                continue
            yield path


def _is_excluded_path(path: Path) -> bool:
    return any(
        part in _EXCLUDED_DIRS or part.startswith(_PIRANESI_TRACE_PREFIX) for part in path.parts
    )


def _build_pattern_for_name(name: str, suffix: str) -> str:
    escaped = re.escape(name)
    if suffix == ".py":
        return rf"\b{escaped}\s*\("
    if suffix == ".rb":
        return rf"\b{escaped}\b"
    if suffix == ".php":
        return rf"\b{escaped}\s*\("
    return rf"\b{escaped}\s*\("


def discover_custom_sanitizers(
    roots: Sequence[Path] | Path | str,
    *,
    confidence: float = DEFAULT_DISCOVERED_CONFIDENCE,
    max_per_file: int = 50,
) -> tuple[SanitizerSpec, ...]:
    """Scan ``roots`` for likely-sanitizer functions, return SanitizerSpecs.

    Discovered specs have ``is_custom``-like semantics via reduced confidence
    and ``blocks_flow=False`` so they downgrade findings rather than suppress.
    """
    roots = [Path(roots)] if isinstance(roots, (str, Path)) else [Path(r) for r in roots]

    seen: dict[str, SanitizerSpec] = {}
    for path in _iter_source_files(roots):
        try:
            if path.stat().st_size > MAX_SCAN_BYTES:
                continue
            text = path.read_text(encoding="utf-8", errors="ignore")
        except OSError:
            continue
        matched = 0
        for rx, exts in _FN_PATTERNS:
            if path.suffix not in exts:
                continue
            for m in rx.finditer(text):
                if matched >= max_per_file:
                    break
                name = m.group(1)
                inferred = _infer_kind(name)
                if inferred is None:
                    continue
                kind, mitigates = inferred
                spec_name = f"discovered_{name}"
                if spec_name in seen:
                    continue
                pattern = _build_pattern_for_name(name, path.suffix)
                seen[spec_name] = SanitizerSpec(
                    name=spec_name,
                    pattern=pattern,
                    kind=kind,
                    mitigates=mitigates,
                    confidence=confidence,
                    blocks_flow=False,
                )
                matched += 1
    return tuple(seen.values())


__all__ = ["DEFAULT_DISCOVERED_CONFIDENCE", "discover_custom_sanitizers"]
