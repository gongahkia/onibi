from __future__ import annotations

from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_ai_operator_policy_sets_phase_6_boundaries() -> None:
    text = (ROOT / "docs" / "ai-operator-control-policy.md").read_text(
        encoding="utf-8"
    )

    for required in [
        "must never be treated as confirmed evidence",
        "Autonomous testing, exploitation, scanning",
        "Cloud providers require explicit bring-your-own-key configuration",
        "External model calls are disabled when privacy mode is enabled",
        "Every prompt payload must pass through the redaction-before-prompt contract",
        "Until accepted, it must not alter normalized findings",
    ]:
        assert required in text
