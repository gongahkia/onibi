from __future__ import annotations

from pathlib import Path

from piranesi.legal.engine import Rule
from piranesi.legal.rules.common import (
    RegulatoryRuleSpec,
    compile_rule_specs,
    default_rules_path,
    load_rule_specs,
)

ISO_27001_RULES_PATH = default_rules_path("iso27001.toml")


def load_iso27001_rule_specs(path: Path | None = None) -> list[RegulatoryRuleSpec]:
    return load_rule_specs(path or ISO_27001_RULES_PATH)


def load_iso27001_rules(path: Path | None = None) -> list[Rule]:
    return compile_rule_specs(load_iso27001_rule_specs(path))


__all__ = [
    "ISO_27001_RULES_PATH",
    "load_iso27001_rule_specs",
    "load_iso27001_rules",
]
