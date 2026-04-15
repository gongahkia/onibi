from __future__ import annotations

from pathlib import Path

from piranesi.detect.sanitizer_discovery import (
    DEFAULT_DISCOVERED_CONFIDENCE,
    discover_custom_sanitizers,
)
from piranesi.scan.specs import SanitizerKind


def _write(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content)


def test_discovers_js_sanitizers(tmp_path: Path) -> None:
    _write(
        tmp_path / "a.js",
        "function sanitizeHtml(x) { return x.replace(/</g, '&lt;'); }\n"
        "const escapeShell = (s) => s.replace(/'/g, \"\\\\'\");\n"
        "function unrelated() { return 1; }\n",
    )
    specs = {s.name: s for s in discover_custom_sanitizers(tmp_path)}
    assert "discovered_sanitizeHtml" in specs
    assert specs["discovered_sanitizeHtml"].kind == SanitizerKind.SANITIZE
    assert specs["discovered_sanitizeHtml"].mitigates == ("CWE-79",)
    assert "discovered_escapeShell" in specs
    assert specs["discovered_escapeShell"].mitigates == ("CWE-78",)
    assert "discovered_unrelated" not in specs


def test_discovers_python_sanitizers(tmp_path: Path) -> None:
    _write(
        tmp_path / "b.py",
        "def sanitize_sql(x): return x.replace(\"'\", \"''\")\n"
        "def validate_url(u): return u.startswith('http')\n"
        "def unrelated(): pass\n",
    )
    specs = {s.name: s for s in discover_custom_sanitizers(tmp_path)}
    assert specs["discovered_sanitize_sql"].mitigates == ("CWE-89",)
    assert "CWE-601" in specs["discovered_validate_url"].mitigates
    assert "discovered_unrelated" not in specs


def test_confidence_and_blocks_flow(tmp_path: Path) -> None:
    _write(tmp_path / "c.js", "function sanitizeInput(x){ return x; }\n")
    specs = discover_custom_sanitizers(tmp_path)
    assert len(specs) == 1
    spec = specs[0]
    assert spec.confidence == DEFAULT_DISCOVERED_CONFIDENCE
    assert spec.blocks_flow is False


def test_skips_excluded_dirs(tmp_path: Path) -> None:
    _write(tmp_path / "node_modules" / "x.js", "function sanitizeHtml(x){return x;}\n")
    _write(tmp_path / "src" / "y.js", "function escapeHtml(x){return x;}\n")
    names = {s.name for s in discover_custom_sanitizers(tmp_path)}
    assert "discovered_escapeHtml" in names
    assert "discovered_sanitizeHtml" not in names
