from __future__ import annotations

from pathlib import Path

from piranesi.legal.engine import Rule
from piranesi.legal.rules.common import (
    RegulatoryRuleSpec,
    compile_rule_specs,
    default_rules_path,
    load_rule_specs,
)

EU_AI_ACT_RULES_PATH = default_rules_path("eu_ai_act.toml")


def load_eu_ai_act_rule_specs(path: Path | None = None) -> list[RegulatoryRuleSpec]:
    return load_rule_specs(path or EU_AI_ACT_RULES_PATH)


def load_eu_ai_act_rules(path: Path | None = None) -> list[Rule]:
    return compile_rule_specs(load_eu_ai_act_rule_specs(path))


__all__ = [
    "EU_AI_ACT_RULES_PATH",
    "load_eu_ai_act_rule_specs",
    "load_eu_ai_act_rules",
]
