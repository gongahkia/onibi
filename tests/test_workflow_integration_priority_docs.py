from __future__ import annotations

from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_workflow_integration_priority_ranks_linear_before_jira() -> None:
    text = (ROOT / "docs" / "workflow-integration-priority.md").read_text(
        encoding="utf-8"
    )

    assert "Linear is the next ticketing candidate" in text
    assert "| 4 | Linear | next candidate |" in text
    assert "| 5 | Jira | defer |" in text
    assert "Create Linear implementation issues only after" in text
    assert "Create Jira implementation issues only after" in text
    assert "No bidirectional ticket sync" in text
