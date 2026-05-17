from __future__ import annotations

from pathlib import Path

from scripts.check_release_hygiene import collect_release_hygiene_errors


def test_release_hygiene_metadata_is_consistent() -> None:
    root = Path(__file__).resolve().parents[1]

    errors = collect_release_hygiene_errors(root)

    assert errors == []
