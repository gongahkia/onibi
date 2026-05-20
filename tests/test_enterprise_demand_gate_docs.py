from __future__ import annotations

from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_enterprise_demand_intake_template_keeps_implementation_parked() -> None:
    text = (ROOT / "docs" / "enterprise-demand-intake-template.md").read_text(
        encoding="utf-8"
    )
    readme = (ROOT / "README.md").read_text(encoding="utf-8")

    assert "Status: template only; enterprise implementation remains parked." in text
    assert "Do not open implementation issues until the demand gate" in text
    assert "Why signed local exports or one-way handoff are insufficient" in text
    assert "hosted SaaS" in text
    assert "customer-managed on-prem" in text
    assert "air-gapped" in text
    assert "Gate decision: pass, park, or reject." in text
    assert "docs/enterprise-demand-intake-template.md" in readme


def test_enterprise_demand_gate_still_requires_real_customer_evidence() -> None:
    text = (ROOT / "docs" / "enterprise-demand-gate.md").read_text(encoding="utf-8")

    assert "At least one design partner or customer" in text
    assert "implementation owner for secure operation" in text
    assert "Phase 5 remains parked" in text
