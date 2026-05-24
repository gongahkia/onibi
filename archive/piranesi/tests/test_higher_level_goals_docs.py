from __future__ import annotations

from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

DETAIL_DOCS = [
    "detection-opportunity-matrix.md",
    "measurable-events-mode.md",
    "purple-team-handoff-pack.md",
    "attack-path-evidence-import.md",
    "operator-debrief-workflow.md",
    "report-qa-before-delivery.md",
    "client-outcome-view.md",
]


def test_higher_level_goals_links_all_detail_docs() -> None:
    text = (ROOT / "HIGHER-LEVEL-GOALS.md").read_text(encoding="utf-8")
    readme = (ROOT / "README.md").read_text(encoding="utf-8")

    assert "local evidence-to-detection handoff layer" in text
    assert "Recommended Build Order" in text
    assert "Report QA before delivery" in text
    assert "Higher-level goals" in readme
    for filename in DETAIL_DOCS:
        assert f"docs/{filename}" in text
        assert f"docs/{filename}" in readme


def test_detail_docs_are_implementation_ready() -> None:
    for filename in DETAIL_DOCS:
        text = (ROOT / "docs" / filename).read_text(encoding="utf-8")

        assert "## Goal" in text
        assert "## Approach" in text
        assert "## User Flow" in text
        assert "## Build Slices" in text
        assert "## Acceptance Criteria" in text
        assert "## Non-Goals" in text
