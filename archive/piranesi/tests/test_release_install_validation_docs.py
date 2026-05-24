from __future__ import annotations

from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_release_install_validation_covers_external_install_smokes() -> None:
    text = (ROOT / "docs" / "release-install-validation.md").read_text(encoding="utf-8")
    readme = (ROOT / "README.md").read_text(encoding="utf-8")

    assert "Status: required before publishing release artifacts." in text
    assert "Source Checkout Smoke" in text
    assert "Wheel Smoke" in text
    assert "pipx Smoke" in text
    assert "does not upload artifacts to PyPI, GHCR, or any other registry" in text
    assert "built wheels include `piranesi` console script metadata" in text
    assert "docs/release-install-validation.md" in readme
