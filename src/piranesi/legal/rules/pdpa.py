from __future__ import annotations

from pathlib import Path

from piranesi.legal.engine import Rule
from piranesi.legal.rules.common import (
    RegulatoryRuleSpec,
    compile_rule_specs,
    default_rules_path,
    load_rule_specs,
)

PDPA_RULES_PATH = default_rules_path("pdpa.toml")


def load_pdpa_rule_specs(path: Path | None = None) -> list[RegulatoryRuleSpec]:
    return load_rule_specs(path or PDPA_RULES_PATH)


def load_pdpa_rules(path: Path | None = None) -> list[Rule]:
    return compile_rule_specs(load_pdpa_rule_specs(path))


def pdpa_thresholds(path: Path | None = None) -> tuple[int, ...]:
    return tuple(
        sorted(
            {
                rule_spec.affected_individuals_gte
                for rule_spec in load_pdpa_rule_specs(path)
                if rule_spec.affected_individuals_gte is not None
            }
        )
    )


__all__ = [
    "PDPA_RULES_PATH",
    "load_pdpa_rule_specs",
    "load_pdpa_rules",
    "pdpa_thresholds",
]
