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


def test_bloodhound_import_gate_stays_parked_without_real_exports() -> None:
    text = (ROOT / "docs" / "bloodhound-import-gate.md").read_text(encoding="utf-8")
    readme = (ROOT / "README.md").read_text(encoding="utf-8")

    assert "Status: parked behind sanitized authorized collection exports." in text
    assert "BloodHound CE JSON files" in text
    assert "SharpHound ZIP output" in text
    assert "Relationship shape should be preserved" in text
    assert "Running SharpHound or any collector" in text
    assert "docs/bloodhound-import-gate.md" in readme


def test_netexec_crackmapexec_gate_blocks_live_credential_actions() -> None:
    text = (ROOT / "docs" / "netexec-crackmapexec-import-gate.md").read_text(
        encoding="utf-8"
    )
    readme = (ROOT / "README.md").read_text(encoding="utf-8")

    assert "Status: parked behind redacted real output fixtures." in text
    assert "real JSON output, structured log output, or terminal transcript" in text
    assert "hashes, passwords, tickets, tokens" in text
    assert "It must never run NetExec or CrackMapExec" in text
    assert "Credential validation or spraying" in text
    assert "docs/netexec-crackmapexec-import-gate.md" in readme


def test_known_limitations_link_deferred_adapter_expansion() -> None:
    payload = json.loads((ROOT / "docs" / "known-limitations.json").read_text(encoding="utf-8"))
    limitation = next(item for item in payload["limitations"] if item["id"] == "KL-001")

    assert "docs/deferred-adapter-expansion.md" in limitation["docs_refs"]
