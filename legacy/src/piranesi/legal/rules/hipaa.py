from __future__ import annotations

from pathlib import Path

from piranesi.legal.engine import Rule
from piranesi.legal.rules.common import (
    RegulatoryRuleSpec,
    compile_rule_specs,
    default_rules_path,
    extract_thresholds,
    load_rule_specs,
)

HIPAA_RULES_PATH = default_rules_path("hipaa.toml")


def load_hipaa_rule_specs(path: Path | None = None) -> list[RegulatoryRuleSpec]:
    return load_rule_specs(path or HIPAA_RULES_PATH)


def load_hipaa_rules(path: Path | None = None) -> list[Rule]:
    return compile_rule_specs(load_hipaa_rule_specs(path))


def hipaa_thresholds(path: Path | None = None) -> tuple[int, ...]:
    return extract_thresholds(load_hipaa_rule_specs(path))


__all__ = [
    "HIPAA_RULES_PATH",
    "hipaa_thresholds",
    "load_hipaa_rule_specs",
    "load_hipaa_rules",
]
