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


def test_adapter_intake_gate_requires_authorized_real_exports() -> None:
    text = (ROOT / "docs" / "adapter-intake.md").read_text(encoding="utf-8")
    readme = (ROOT / "README.md").read_text(encoding="utf-8")

    assert "Status: required before new adapter implementation." in text
    assert "real exported tool output from an authorized target" in text
    assert "Synthetic or hand-authored files are allowed only for negative tests" in text
    assert "raw/<tool>/" in text
    assert "do not imply scanning, exploitation, live credential validation" in text
    assert "docs/adapter-intake.md" in readme
