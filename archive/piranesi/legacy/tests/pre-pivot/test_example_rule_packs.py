from __future__ import annotations

from pathlib import Path

from piranesi.rules.engine import compile_rule, load_rules

EXAMPLE_RULE_PACKS_DIR = Path(__file__).resolve().parents[1] / "examples" / "rule-packs"
EXPECTED_RULE_IDS = {
    "example-node-express-open-redirect",
    "example-python-flask-ssti",
    "example-go-nethttp-header-injection",
    "example-php-laravel-sqli",
    "example-ruby-rails-command-injection",
}


def test_example_rule_packs_parse_and_compile() -> None:
    rules = load_rules(EXAMPLE_RULE_PACKS_DIR)

    assert {rule.id for rule in rules} == EXPECTED_RULE_IDS

    compiled = [compile_rule(rule) for rule in rules]
    assert {rule.id for rule in compiled} == EXPECTED_RULE_IDS
    assert all(rule.description for rule in compiled)
    assert all(rule.message_template for rule in compiled)
    assert all(rule.sanitizer_patterns for rule in compiled)
    assert all(rule.schema_version == "1" for rule in compiled)


def test_example_rule_packs_include_receiver_constrained_sinks() -> None:
    rules = {rule.id: rule for rule in load_rules(EXAMPLE_RULE_PACKS_DIR)}

    assert '.where(_.receiver.code("res|reply"))' in (
        rules["example-node-express-open-redirect"].sink_pattern or ""
    )
    assert "(?:w|rw|writer)" in (rules["example-go-nethttp-header-injection"].sink_pattern or "")
    assert "(?:Kernel\\.)?" in (rules["example-ruby-rails-command-injection"].sink_pattern or "")


def test_example_rule_packs_are_marked_as_examples() -> None:
    rules = load_rules(EXAMPLE_RULE_PACKS_DIR)

    assert all("example" in rule.tags for rule in rules)
    assert all(rule.author == "piranesi-example-packs" for rule in rules)
    assert all(rule.version == "0.1.0" for rule in rules)
    assert all(rule.category in {"redirect", "injection"} for rule in rules)
