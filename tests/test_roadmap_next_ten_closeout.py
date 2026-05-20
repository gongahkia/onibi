from __future__ import annotations

from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_roadmap_next_ten_closeout_records_follow_up_queue() -> None:
    text = (ROOT / "docs" / "roadmap-next-ten-closeout.md").read_text(encoding="utf-8")

    for issue in range(126, 136):
        assert f"#{issue}" in text
    assert "second queue implemented" in text
    assert "separate rollback-friendly commits" in text
    assert "not by pretending unsupported features exist" in text
