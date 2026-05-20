from __future__ import annotations

import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_deferred_adapter_expansion_keeps_host_work_parked() -> None:
    text = (ROOT / "docs" / "deferred-adapter-expansion.md").read_text(encoding="utf-8")

    assert "parked behind real authorized fixture evidence" in text
    assert "BloodHound collection export" in text
    assert "Live SSH probing or fleet scanning" in text
    assert "Each accepted adapter must get its own GitHub issue" in text
    assert "Claiming adapter support from synthetic fixtures" in text


def test_known_limitations_link_deferred_adapter_expansion() -> None:
    payload = json.loads((ROOT / "docs" / "known-limitations.json").read_text(encoding="utf-8"))
    limitation = next(item for item in payload["limitations"] if item["id"] == "KL-001")

    assert "docs/deferred-adapter-expansion.md" in limitation["docs_refs"]
