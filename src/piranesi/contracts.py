from __future__ import annotations

from typing import Any

import click
from typer.main import get_command

from piranesi.cli import app
from piranesi.plugin import plugin_api_manifest
from piranesi.report.renderer import KnownLimitation, PiranesiReport


def build_contract_snapshot() -> dict[str, Any]:
    root = get_command(app)
    if not isinstance(root, click.Group):
        raise TypeError("root CLI command must be a click group")
    groups: dict[str, list[str]] = {}
    for name, command in sorted(root.commands.items()):
        if isinstance(command, click.Group):
            groups[name] = sorted(command.commands.keys())
    return {
        "snapshot_version": 1,
        "cli": {
            "root_commands": sorted(root.commands.keys()),
            "groups": groups,
        },
        "plugin_api_manifest": plugin_api_manifest(),
        "stability_policy": {
            "document": "docs/stability.md",
            "public_modules": [
                "piranesi.host.api",
                "piranesi.host.models",
                "piranesi.schema",
            ],
            "public_schemas": ["host-report", "host-snapshot", "fleet-report"],
            "community_rule_formats": ["rules/community/host/*.toml"],
        },
        "report_contract": {
            "piranesi_report_fields": sorted(PiranesiReport.model_fields.keys()),
            "known_limitation_fields": sorted(KnownLimitation.model_fields.keys()),
        },
    }


__all__ = ["build_contract_snapshot"]
