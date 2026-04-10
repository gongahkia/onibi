from __future__ import annotations

from pathlib import Path

from piranesi.legal.engine import Rule
from piranesi.legal.rules.common import (
    RegulatoryRuleSpec,
    compile_rule_specs,
    default_rules_path,
    load_rule_specs,
)

GDPR_RULES_PATH = default_rules_path("gdpr.toml")


def load_gdpr_rule_specs(path: Path | None = None) -> list[RegulatoryRuleSpec]:
    return load_rule_specs(path or GDPR_RULES_PATH)


def load_gdpr_rules(path: Path | None = None) -> list[Rule]:
    return compile_rule_specs(load_gdpr_rule_specs(path))


__all__ = [
    "GDPR_RULES_PATH",
    "load_gdpr_rule_specs",
    "load_gdpr_rules",
]
