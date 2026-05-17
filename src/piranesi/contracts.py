from __future__ import annotations

from typing import Any

import click
from typer.main import get_command

from piranesi.cli import app
from piranesi.schema import available_schemas


def build_contract_snapshot() -> dict[str, Any]:
    root = get_command(app)
    if not isinstance(root, click.Group):
        raise TypeError("root CLI command must be a click group")
    groups: dict[str, list[str]] = {}
    for name, command in sorted(root.commands.items()):
        if isinstance(command, click.Group):
            groups[name] = sorted(command.commands.keys())
    return {
        "snapshot_version": 2,
        "cli": {
            "root_commands": sorted(root.commands.keys()),
            "groups": groups,
        },
        "workspace_contract": {
            "public_schemas": list(available_schemas()),
            "workspace_layout": [
                "workspace.json",
                "normalized/findings.json",
                "audit-log.jsonl",
                "raw/",
                "reports/",
                "signatures/",
            ],
        },
    }


__all__ = ["build_contract_snapshot"]
