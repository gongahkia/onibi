from __future__ import annotations

from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_github_issues_export_threat_model_sets_required_boundaries() -> None:
    threat_model = (ROOT / "docs" / "github-issues-export-threat-model.md").read_text(
        encoding="utf-8"
    )

    for required in [
        "one-way handoff",
        "GitHub is an external system",
        "Piranesi does not store GitHub tokens",
        "Dry-run mode must never call GitHub",
        "Raw evidence upload or attachment",
        "Tests must cover redacted assets",
    ]:
        assert required in threat_model
