# Contributing Regulatory Rules

Guide for adding new regulatory frameworks to Piranesi.

## TOML Schema Reference

Each rule file contains a `[[rules]]` array. Every entry must conform to `RegulatoryRuleSpec`:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `rule_id` | string | yes | Unique identifier, e.g. `ccpa_1798_100_disclosure` |
| `framework` | string | yes | Framework key used for grouping, e.g. `CCPA`, `GDPR` |
| `section` | string | yes | Statutory section reference |
| `obligation_text` | string | yes | Full obligation description citing the statute |
| `consequences` | list[string] | yes | Actions: `remediate`, `document`, `review`, `notify_regulator`, `notify_individuals` |
| `penalty_range` | string | yes | Statutory penalty range with currency and amounts |
| `notification_timeline` | string | no | Time-bound notification requirement, e.g. `72 hours` |
| `enforcement_precedents` | list[string] | no | Notable enforcement actions or case references |
| `cross_references` | list[string] | no | Other `rule_id`s this rule relates to |
| `severity_modifier` | string | no | `standard` or `aggravated` |
| `vuln_classes` | list[string] | no | CWE IDs that trigger this rule, e.g. `["CWE-89", "CWE-78"]` |
| `data_categories` | list[string] | no | Piranesi data categories, e.g. `["health", "biometric"]` |
| `requires_rule_ids` | list[string] | no | Rule IDs that must fire first (chaining) |
| `requires_boolean_facts` | list[string] | no | ALL of these boolean facts must be true |
| `requires_any_boolean_facts` | list[string] | no | ANY of these boolean facts must be true |
| `affected_individuals_gte` | int | no | Minimum affected individuals to trigger |

### Available boolean facts

`is_healthcare_entity`, `is_high_risk_ai`, `is_essential_entity`, `is_important_entity`,
`willful_violation`, `no_encryption_at_rest`, `third_party_processor`, `cross_border`,
`high_risk_to_individuals`, `likely_risk_to_rights`, `basic_processing_principle_violation`

### Available data categories

`name`, `nric`, `fin`, `dob`, `nationality`, `race`, `religion`, `biometric`, `health`,
`genetic`, `criminal`, `financial`, `financial_bank`, `financial_credit_card`,
`contact_email`, `contact_phone`, `contact_address`, `password`, `session_token`

## Step-by-step Guide

1. **Copy the template**: `cp rules/community/_template.toml rules/community/{framework}.toml`
2. **Define your rules**: Fill in each `[[rules]]` entry with accurate statutory references
3. **Add framework label** (optional): If you want a human-readable label in memos, add an entry to `_FRAMEWORK_LABELS` in `src/piranesi/legal/memo.py`
4. **Add framework ordering** (optional): Add an entry to `_FRAMEWORK_ORDER` in the same file
5. **Run validation**: `python -c "from piranesi.legal.rules.common import load_rule_specs; from pathlib import Path; load_rule_specs(Path('rules/community/{framework}.toml'))"`
6. **Run the full test suite**: `pytest tests/test_legal/ -v`

## Testing Requirements

- Add a test to `tests/test_legal/test_regulatory_rules.py` that loads your rule file and asserts the expected rule IDs
- Add at least one integration test that asserts the rules fire correctly when the right facts are present
- Verify no regressions: `pytest tests/ -x`

## Legal Review Process

Before merge, a community rule PR must have:

1. **Statutory citation check** — every `obligation_text` and `penalty_range` must cite a specific section of the statute
2. **Domain expert review** — at least one reviewer with familiarity in the regulatory domain
3. **Cross-reference validation** — all `cross_references` point to valid `rule_id`s
4. **Category coverage** — `data_categories` accurately reflect what the statute protects

## PR Checklist

```
- [ ] TOML file placed in `rules/community/`
- [ ] All rules validate against `RegulatoryRuleSpec` (no extra fields)
- [ ] Each rule has a unique `rule_id`
- [ ] `obligation_text` cites specific statutory provisions
- [ ] `penalty_range` references the statute
- [ ] Tests added to `tests/test_legal/test_regulatory_rules.py`
- [ ] Integration test confirms rules fire with correct facts
- [ ] `pytest tests/ -x` passes
- [ ] Legal accuracy reviewed by domain expert
```
