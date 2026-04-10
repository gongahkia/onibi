from __future__ import annotations

from pathlib import Path

from piranesi.legal.engine import Rule
from piranesi.legal.rules.common import (
    RegulatoryRuleSpec,
    compile_rule_specs,
    default_rules_path,
    load_rule_specs,
)

NIS2_RULES_PATH = default_rules_path("nis2.toml")


def load_nis2_rule_specs(path: Path | None = None) -> list[RegulatoryRuleSpec]:
    return load_rule_specs(path or NIS2_RULES_PATH)


def load_nis2_rules(path: Path | None = None) -> list[Rule]:
    return compile_rule_specs(load_nis2_rule_specs(path))


__all__ = [
    "NIS2_RULES_PATH",
    "load_nis2_rule_specs",
    "load_nis2_rules",
]
