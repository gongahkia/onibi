from __future__ import annotations

from typing import Any

from typer.main import get_command

from piranesi.cli import app
from piranesi.plugin import plugin_api_manifest
from piranesi.report.renderer import KnownLimitation, PiranesiReport


def build_contract_snapshot() -> dict[str, Any]:
    root = get_command(app)
    groups: dict[str, list[str]] = {}
    for name, command in sorted(root.commands.items()):
        if hasattr(command, "commands"):
            groups[name] = sorted(command.commands.keys())
    return {
        "snapshot_version": 1,
        "cli": {
            "root_commands": sorted(root.commands.keys()),
            "groups": groups,
        },
        "plugin_api_manifest": plugin_api_manifest(),
        "report_contract": {
            "piranesi_report_fields": sorted(PiranesiReport.model_fields.keys()),
            "known_limitation_fields": sorted(KnownLimitation.model_fields.keys()),
        },
    }


__all__ = ["build_contract_snapshot"]
