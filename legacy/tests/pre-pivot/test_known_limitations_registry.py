from __future__ import annotations

import json
from pathlib import Path

from scripts.check_known_limitations import collect_known_limitations_errors


def test_known_limitations_registry_is_valid() -> None:
    root = Path(__file__).resolve().parents[1]

    errors = collect_known_limitations_errors(root)

    assert errors == []


def test_known_limitations_registry_reports_schema_violations(tmp_path: Path) -> None:
    docs_dir = tmp_path / "docs"
    docs_dir.mkdir(parents=True, exist_ok=True)
    (docs_dir / "known-limitations.json").write_text(
        json.dumps(
            {
                "schema_version": "1.0",
                "limitations": [
                    {
                        "id": "invalid-id",
                        "title": "",
                        "affected_feature": "",
                        "severity": "urgent",
                        "impact": "",
                        "workaround": "",
                        "status": "unknown",
                        "introduced_version": "latest",
                        "last_reviewed": "2026/04/16",
                    }
                ],
            }
        ),
        encoding="utf-8",
    )

    errors = collect_known_limitations_errors(tmp_path)

    assert errors
    assert any("must match KL-###" in error for error in errors)
    assert any("severity must be one of" in error for error in errors)
    assert any("last_reviewed must use YYYY-MM-DD" in error for error in errors)
