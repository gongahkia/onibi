from __future__ import annotations

from pathlib import Path

from piranesi.legal.engine import Rule
from piranesi.legal.rules.common import (
    RegulatoryRuleSpec,
    compile_rule_specs,
    default_rules_path,
    load_rule_specs,
)

MAS_TRM_RULES_PATH = default_rules_path("mas_trm.toml")


def load_mas_trm_rule_specs(path: Path | None = None) -> list[RegulatoryRuleSpec]:
    return load_rule_specs(path or MAS_TRM_RULES_PATH)


def load_mas_trm_rules(path: Path | None = None) -> list[Rule]:
    return compile_rule_specs(load_mas_trm_rule_specs(path))


__all__ = [
    "MAS_TRM_RULES_PATH",
    "load_mas_trm_rule_specs",
    "load_mas_trm_rules",
]
