# Community Rules

This directory contains community-contributed regulatory rule files for Piranesi.

## How to contribute

1. Copy `_template.toml` to `{framework_name}.toml` in this directory
2. Fill in all required fields following the `RegulatoryRuleSpec` schema
3. Run the test suite to validate your rules: `pytest tests/test_legal/test_regulatory_rules.py -v`
4. Open a PR against `main` with the completed checklist from `docs/contributing-rules.md`

## Auto-discovery

Rule files placed here are automatically discovered and loaded by the Piranesi legal engine.
Files prefixed with `_` (like `_template.toml`) are excluded from auto-discovery.

## Requirements

- Each rule must cite a specific statutory provision
- Penalty ranges must reference the statute
- At least one integration test per framework
- Legal accuracy review by a domain expert before merge
