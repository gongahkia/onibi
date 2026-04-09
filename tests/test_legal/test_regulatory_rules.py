from __future__ import annotations

from piranesi.legal.engine import ForwardChainingEngine
from piranesi.legal.rules import (
    add_finding_facts,
    load_mas_trm_rule_specs,
    load_mas_trm_rules,
    load_pdpa_rule_specs,
    load_pdpa_rules,
    pdpa_thresholds,
    query_consequences,
    query_obligations,
)


def test_pdpa_toml_loads_expected_rule_specs() -> None:
    rule_specs = load_pdpa_rule_specs()

    assert [rule_spec.rule_id for rule_spec in rule_specs] == [
        "pdpa_s24_standard",
        "pdpa_s24_aggravated_tier1",
        "pdpa_s24_no_encryption",
        "pdpa_s26d_notification",
        "pdpa_s24_s25_third_party",
    ]


def test_mas_trm_toml_loads_expected_rule_specs() -> None:
    rule_specs = load_mas_trm_rule_specs()

    assert [rule_spec.rule_id for rule_spec in rule_specs] == [
        "mas_trm_11_1_reliability",
        "mas_trm_11_2_recoverability",
        "mas_trm_11_0_5_controls",
    ]


def test_pdpa_rules_fire_for_tier_one_unencrypted_processor_breach() -> None:
    engine = ForwardChainingEngine()
    for rule in load_pdpa_rules():
        engine.add_rule(rule)

    add_finding_facts(
        engine,
        finding_id="finding-1",
        vuln_class="CWE-89",
        data_categories=["nric", "name"],
        affected_individuals=1200,
        boolean_facts={
            "no_encryption_at_rest": True,
            "third_party_processor": True,
        },
        thresholds=pdpa_thresholds(),
    )

    engine.run()

    obligations = query_obligations(engine, finding_id="finding-1")
    obligation_ids = {fact.args["rule_id"] for fact in obligations}

    assert obligation_ids == {
        "pdpa_s24_standard",
        "pdpa_s24_aggravated_tier1",
        "pdpa_s24_no_encryption",
        "pdpa_s26d_notification",
        "pdpa_s24_s25_third_party",
    }

    standard = next(fact for fact in obligations if fact.args["rule_id"] == "pdpa_s24_standard")
    assert standard.args["framework"] == "PDPA"
    assert "Section 24" in str(standard.args["section"])
    assert any("SingHealth" in item for item in standard.args["enforcement_precedents"])
    assert any("Grab" in item for item in standard.args["enforcement_precedents"])

    notification = next(
        fact for fact in obligations if fact.args["rule_id"] == "pdpa_s26d_notification"
    )
    assert (
        notification.args["notification_timeline"]
        == "3 calendar days from assessment of breach as notifiable"
    )

    consequences = query_consequences(engine, finding_id="finding-1")
    consequence_pairs = {(fact.args["rule_id"], fact.args["action"]) for fact in consequences}
    assert ("pdpa_s26d_notification", "notify_regulator") in consequence_pairs
    assert ("pdpa_s26d_notification", "notify_individuals") in consequence_pairs
    assert ("pdpa_s24_s25_third_party", "review") in consequence_pairs


def test_pdpa_rules_do_not_fire_without_personal_data() -> None:
    engine = ForwardChainingEngine()
    for rule in load_pdpa_rules():
        engine.add_rule(rule)

    add_finding_facts(
        engine,
        finding_id="finding-2",
        vuln_class="CWE-79",
        data_categories=[],
        affected_individuals=900,
        thresholds=pdpa_thresholds(),
    )

    engine.run()

    assert query_obligations(engine, finding_id="finding-2") == []


def test_mas_trm_rules_fire_for_command_injection_in_financial_system() -> None:
    engine = ForwardChainingEngine()
    for rule in load_mas_trm_rules():
        engine.add_rule(rule)

    add_finding_facts(
        engine,
        finding_id="finding-3",
        vuln_class="CWE-78",
        data_categories=["financial_bank"],
    )

    engine.run()

    obligations = query_obligations(engine, finding_id="finding-3")
    obligation_ids = {fact.args["rule_id"] for fact in obligations}

    assert obligation_ids == {
        "mas_trm_11_1_reliability",
        "mas_trm_11_2_recoverability",
        "mas_trm_11_0_5_controls",
    }

    for obligation in obligations:
        assert obligation.args["framework"] == "MAS_TRM"
        assert "supervisory action" in str(obligation.args["penalty_range"]).lower()
        assert "direct financial penalty" in str(obligation.args["penalty_range"]).lower()


def test_mas_trm_path_traversal_skips_injection_control_rule() -> None:
    engine = ForwardChainingEngine()
    for rule in load_mas_trm_rules():
        engine.add_rule(rule)

    add_finding_facts(
        engine,
        finding_id="finding-4",
        vuln_class="CWE-22",
        data_categories=["financial_credit_card"],
    )

    engine.run()

    obligation_ids = {
        fact.args["rule_id"] for fact in query_obligations(engine, finding_id="finding-4")
    }
    assert obligation_ids == {
        "mas_trm_11_1_reliability",
        "mas_trm_11_2_recoverability",
    }


def test_mixed_personal_and_financial_data_triggers_pdpa_and_mas_rules() -> None:
    engine = ForwardChainingEngine()
    for rule in [*load_pdpa_rules(), *load_mas_trm_rules()]:
        engine.add_rule(rule)

    add_finding_facts(
        engine,
        finding_id="finding-5",
        vuln_class="CWE-89",
        data_categories=["nric", "financial_credit_card", "name"],
        affected_individuals=1200,
        thresholds=pdpa_thresholds(),
    )

    engine.run()

    obligations = query_obligations(engine, finding_id="finding-5")
    obligation_ids = {fact.args["rule_id"] for fact in obligations}

    assert {
        "pdpa_s24_standard",
        "pdpa_s24_aggravated_tier1",
        "pdpa_s26d_notification",
        "mas_trm_11_1_reliability",
        "mas_trm_11_0_5_controls",
    }.issubset(obligation_ids)
