from __future__ import annotations

from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_plugin_api_security_model_defines_capabilities_and_forbidden_actions() -> None:
    security_model = (ROOT / "docs" / "plugin-api-security-model.md").read_text(encoding="utf-8")

    for required in [
        "Import adapters that read external tool exports and emit valid PFF",
        "Plugins are untrusted by default",
        "Run plugins out of process",
        "Network access is denied by default",
        "Plugins must not mutate `workspace.json`",
        "Autonomous testing, scanning, exploitation",
        "Disabling redaction, provenance, chain-of-custody, or validation checks",
    ]:
        assert required in security_model
