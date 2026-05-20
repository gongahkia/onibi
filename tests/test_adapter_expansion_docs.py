from __future__ import annotations

from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_adapter_expansion_docs_preserve_real_fixture_gate() -> None:
    text = (ROOT / "docs" / "adapter-expansion.md").read_text(encoding="utf-8")

    assert "OWASP ZAP" in text
    assert "Nessus `.nessus`" in text
    assert "No adapter should merge without real exported tool output." in text
    assert "Synthetic or hand-authored files" in text
    assert "Child Issue Template" in text
