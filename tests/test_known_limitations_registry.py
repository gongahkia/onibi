from __future__ import annotations

import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_known_limitations_have_current_review_metadata() -> None:
    payload = json.loads((ROOT / "docs" / "known-limitations.json").read_text(encoding="utf-8"))

    assert payload["last_updated"] == "2026-05-20"
    assert payload["schema_version"] == "1.0"
    assert payload["limitations"]
    for limitation in payload["limitations"]:
        assert limitation["id"].startswith("KL-")
        assert limitation["title"]
        assert limitation["status"] in {"open", "monitoring", "resolved", "parked"}
        assert limitation["severity"] in {"low", "medium", "high"}
        assert limitation["last_reviewed"] == "2026-05-20"
        assert limitation["review_state"] == "reviewed"
        assert limitation["docs_refs"]


def test_known_limitations_do_not_reintroduce_legacy_host_scope() -> None:
    payload = json.loads((ROOT / "docs" / "known-limitations.json").read_text(encoding="utf-8"))
    titles = {item["title"] for item in payload["limitations"]}

    assert "Live SSH collection is not implemented" not in titles
    assert "Container or Kubernetes scanning is not implemented" not in titles
