from __future__ import annotations

from pathlib import Path

from piranesi.legal.engine import Rule
from piranesi.legal.rules.common import (
    RegulatoryRuleSpec,
    compile_rule_specs,
    default_rules_path,
    load_rule_specs,
)

CCPA_RULES_PATH = default_rules_path("ccpa.toml")

CCPA_CATEGORY_DEFINITIONS: dict[str, str] = {
    "nric": "government_id",
    "fin": "government_id",
    "biometric": "biometric_information",
    "health": "health_information",
    "financial": "financial_information",
    "financial_bank": "financial_information",
    "financial_credit_card": "financial_information",
    "financial_income": "financial_information",
    "contact": "personal_information",
    "contact_phone": "personal_information",
    "contact_email": "personal_information",
    "contact_address": "personal_information",
    "race": "sensitive_personal_information",
    "religion": "sensitive_personal_information",
    "dob": "personal_information",
    "name": "personal_information",
    "username": "personal_information",
}

CCPA_PERSONAL_INFORMATION_DEFINITIONS = {
    "government_id",
    "biometric_information",
    "health_information",
    "financial_information",
    "personal_information",
    "sensitive_personal_information",
}
CCPA_SENSITIVE_PERSONAL_INFORMATION_DEFINITIONS = {
    "government_id",
    "biometric_information",
    "health_information",
    "financial_information",
    "sensitive_personal_information",
}
CCPA_PERSONAL_INFORMATION_CATEGORIES = tuple(
    category
    for category, definition in CCPA_CATEGORY_DEFINITIONS.items()
    if definition in CCPA_PERSONAL_INFORMATION_DEFINITIONS
)
CCPA_SENSITIVE_PERSONAL_INFORMATION_CATEGORIES = tuple(
    category
    for category, definition in CCPA_CATEGORY_DEFINITIONS.items()
    if definition in CCPA_SENSITIVE_PERSONAL_INFORMATION_DEFINITIONS
)


def ccpa_definition_for_category(category: str) -> str | None:
    return CCPA_CATEGORY_DEFINITIONS.get(category.strip().lower())


def load_ccpa_rule_specs(path: Path | None = None) -> list[RegulatoryRuleSpec]:
    return load_rule_specs(path or CCPA_RULES_PATH)


def load_ccpa_rules(path: Path | None = None) -> list[Rule]:
    return compile_rule_specs(load_ccpa_rule_specs(path))


__all__ = [
    "CCPA_CATEGORY_DEFINITIONS",
    "CCPA_PERSONAL_INFORMATION_CATEGORIES",
    "CCPA_RULES_PATH",
    "CCPA_SENSITIVE_PERSONAL_INFORMATION_CATEGORIES",
    "ccpa_definition_for_category",
    "load_ccpa_rule_specs",
    "load_ccpa_rules",
]
