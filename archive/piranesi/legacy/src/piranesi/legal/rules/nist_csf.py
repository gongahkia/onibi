from __future__ import annotations

from pathlib import Path

from piranesi.legal.engine import Rule
from piranesi.legal.rules.common import (
    RegulatoryRuleSpec,
    compile_rule_specs,
    default_rules_path,
    load_rule_specs,
)

NIST_CSF_RULES_PATH = default_rules_path("nist_csf.toml")


def load_nist_csf_rule_specs(path: Path | None = None) -> list[RegulatoryRuleSpec]:
    return load_rule_specs(path or NIST_CSF_RULES_PATH)


def load_nist_csf_rules(path: Path | None = None) -> list[Rule]:
    return compile_rule_specs(load_nist_csf_rule_specs(path))


__all__ = [
    "NIST_CSF_RULES_PATH",
    "load_nist_csf_rule_specs",
    "load_nist_csf_rules",
]
