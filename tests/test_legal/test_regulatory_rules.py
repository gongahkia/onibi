from __future__ import annotations

from pathlib import Path

import pytest

from piranesi.legal.engine import ForwardChainingEngine
from piranesi.legal.rules import (
    add_finding_facts,
    discover_rule_files,
    hipaa_thresholds,
    load_all_rule_specs,
    load_ccpa_rule_specs,
    load_ccpa_rules,
    load_gdpr_rule_specs,
    load_gdpr_rules,
    load_hipaa_rule_specs,
    load_hipaa_rules,
    load_mas_trm_rule_specs,
    load_mas_trm_rules,
    load_nis2_rule_specs,
    load_nis2_rules,
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


def test_hipaa_toml_loads_expected_rule_specs() -> None:
    rule_specs = load_hipaa_rule_specs()

    assert [rule_spec.rule_id for rule_spec in rule_specs] == [
        "hipaa_164_312_access_control",
        "hipaa_164_312_audit",
        "hipaa_164_312_integrity",
        "hipaa_164_312_transmission",
        "hipaa_164_408_breach_notification",
    ]


def test_ccpa_toml_loads_expected_rule_specs() -> None:
    rule_specs = load_ccpa_rule_specs()

    assert [rule_spec.rule_id for rule_spec in rule_specs] == [
        "ccpa_1798_100_disclosure",
        "ccpa_1798_105_categories",
        "ccpa_1798_150_damages",
        "ccpa_1798_155_standard",
        "ccpa_1798_155_aggravated",
        "ccpa_1798_185_sensitive",
    ]


def test_gdpr_toml_loads_expected_rule_specs() -> None:
    rule_specs = load_gdpr_rule_specs()

    assert [rule_spec.rule_id for rule_spec in rule_specs] == [
        "gdpr_art32_security",
        "gdpr_art32_encryption",
        "gdpr_art33_notification",
        "gdpr_art34_communication",
        "gdpr_art83_standard",
        "gdpr_art83_aggravated",
        "gdpr_art83_special",
    ]


def test_nis2_toml_loads_expected_rule_specs() -> None:
    rule_specs = load_nis2_rule_specs()

    assert [rule_spec.rule_id for rule_spec in rule_specs] == [
        "nis2_art21_risk_management",
        "nis2_art21_supply_chain",
        "nis2_art23_early_warning",
        "nis2_art23_incident_notification",
        "nis2_art23_cross_border",
        "nis2_art34_essential_penalties",
        "nis2_art34_important_penalties",
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
    precedents = standard.args["enforcement_precedents"]
    assert isinstance(precedents, list)
    assert any("SingHealth" in item for item in precedents)
    assert any("Grab" in item for item in precedents)

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


def test_hipaa_rules_fire_for_healthcare_xss_breach() -> None:
    engine = ForwardChainingEngine()
    for rule in load_hipaa_rules():
        engine.add_rule(rule)

    add_finding_facts(
        engine,
        finding_id="finding-6",
        vuln_class="CWE-79",
        data_categories=["health"],
        affected_individuals=900,
        boolean_facts={"is_healthcare_entity": True},
        thresholds=hipaa_thresholds(),
    )

    engine.run()

    obligations = query_obligations(engine, finding_id="finding-6")
    obligation_ids = {fact.args["rule_id"] for fact in obligations}

    assert obligation_ids == {
        "hipaa_164_312_access_control",
        "hipaa_164_312_audit",
        "hipaa_164_312_transmission",
        "hipaa_164_408_breach_notification",
    }

    breach_notification = next(
        fact for fact in obligations if fact.args["rule_id"] == "hipaa_164_408_breach_notification"
    )
    assert breach_notification.args["framework"] == "HIPAA"
    assert (
        breach_notification.args["notification_timeline"]
        == "Without unreasonable delay and no later than 60 calendar days "
        "after discovery of the breach"
    )

    consequences = query_consequences(engine, finding_id="finding-6")
    consequence_pairs = {(fact.args["rule_id"], fact.args["action"]) for fact in consequences}
    assert ("hipaa_164_408_breach_notification", "notify_regulator") in consequence_pairs
    assert ("hipaa_164_408_breach_notification", "notify_individuals") in consequence_pairs


def test_hipaa_integrity_rule_fires_for_command_injection_in_healthcare_context() -> None:
    engine = ForwardChainingEngine()
    for rule in load_hipaa_rules():
        engine.add_rule(rule)

    add_finding_facts(
        engine,
        finding_id="finding-7",
        vuln_class="CWE-78",
        data_categories=["health"],
        boolean_facts={"is_healthcare_entity": True},
    )

    engine.run()

    obligation_ids = {
        fact.args["rule_id"] for fact in query_obligations(engine, finding_id="finding-7")
    }
    assert obligation_ids == {
        "hipaa_164_312_access_control",
        "hipaa_164_312_audit",
        "hipaa_164_312_integrity",
    }


def test_hipaa_rules_do_not_fire_without_healthcare_entity() -> None:
    engine = ForwardChainingEngine()
    for rule in load_hipaa_rules():
        engine.add_rule(rule)

    add_finding_facts(
        engine,
        finding_id="finding-8",
        vuln_class="CWE-79",
        data_categories=["health"],
        affected_individuals=900,
        thresholds=hipaa_thresholds(),
    )

    engine.run()

    assert query_obligations(engine, finding_id="finding-8") == []


def test_ccpa_rules_fire_for_sensitive_unencrypted_willful_breach() -> None:
    engine = ForwardChainingEngine()
    for rule in load_ccpa_rules():
        engine.add_rule(rule)

    add_finding_facts(
        engine,
        finding_id="finding-9",
        vuln_class="CWE-89",
        data_categories=["nric", "financial_credit_card"],
        boolean_facts={
            "no_encryption_at_rest": True,
            "willful_violation": True,
        },
    )

    engine.run()

    obligations = query_obligations(engine, finding_id="finding-9")
    obligation_ids = {fact.args["rule_id"] for fact in obligations}

    assert obligation_ids == {
        "ccpa_1798_100_disclosure",
        "ccpa_1798_105_categories",
        "ccpa_1798_150_damages",
        "ccpa_1798_155_standard",
        "ccpa_1798_155_aggravated",
        "ccpa_1798_185_sensitive",
    }

    damages = next(fact for fact in obligations if fact.args["rule_id"] == "ccpa_1798_150_damages")
    assert damages.args["framework"] == "CCPA"
    assert damages.args["penalty_range"] == (
        "$100-$750 per consumer per incident, or actual damages, whichever is greater"
    )

    aggravated = next(
        fact for fact in obligations if fact.args["rule_id"] == "ccpa_1798_155_aggravated"
    )
    assert aggravated.args["penalty_range"] == "Up to $7,500 per intentional violation"

    consequences = query_consequences(engine, finding_id="finding-9")
    consequence_pairs = {(fact.args["rule_id"], fact.args["action"]) for fact in consequences}
    assert ("ccpa_1798_150_damages", "notify_individuals") in consequence_pairs


def test_ccpa_contact_data_triggers_disclosure_but_not_sensitive_rules() -> None:
    engine = ForwardChainingEngine()
    for rule in load_ccpa_rules():
        engine.add_rule(rule)

    add_finding_facts(
        engine,
        finding_id="finding-10",
        vuln_class="CWE-79",
        data_categories=["contact_email"],
    )

    engine.run()

    obligation_ids = {
        fact.args["rule_id"] for fact in query_obligations(engine, finding_id="finding-10")
    }
    assert obligation_ids == {
        "ccpa_1798_100_disclosure",
        "ccpa_1798_155_standard",
    }


def test_ccpa_rules_do_not_fire_for_unsupported_vulnerability_class() -> None:
    engine = ForwardChainingEngine()
    for rule in load_ccpa_rules():
        engine.add_rule(rule)

    add_finding_facts(
        engine,
        finding_id="finding-11",
        vuln_class="CWE-918",
        data_categories=["nric"],
        boolean_facts={
            "no_encryption_at_rest": True,
            "willful_violation": True,
        },
    )

    engine.run()

    assert query_obligations(engine, finding_id="finding-11") == []


def test_gdpr_rules_fire_for_special_category_breach_with_risk_gates() -> None:
    engine = ForwardChainingEngine()
    for rule in load_gdpr_rules():
        engine.add_rule(rule)

    add_finding_facts(
        engine,
        finding_id="finding-12",
        vuln_class="CWE-89",
        data_categories=["health", "political", "name"],
        boolean_facts={
            "no_encryption_at_rest": True,
            "likely_risk_to_rights": True,
            "high_risk_to_individuals": True,
            "basic_processing_principle_violation": True,
        },
    )

    engine.run()

    obligations = query_obligations(engine, finding_id="finding-12")
    obligation_ids = {fact.args["rule_id"] for fact in obligations}

    assert obligation_ids == {
        "gdpr_art32_security",
        "gdpr_art32_encryption",
        "gdpr_art33_notification",
        "gdpr_art34_communication",
        "gdpr_art83_standard",
        "gdpr_art83_aggravated",
        "gdpr_art83_special",
    }

    notification = next(
        fact for fact in obligations if fact.args["rule_id"] == "gdpr_art33_notification"
    )
    assert notification.args["framework"] == "GDPR"
    assert notification.args["notification_timeline"] == (
        "72 hours after becoming aware of the personal data breach"
    )

    communication = next(
        fact for fact in obligations if fact.args["rule_id"] == "gdpr_art34_communication"
    )
    assert communication.args["notification_timeline"] == "Without undue delay"

    special = next(fact for fact in obligations if fact.args["rule_id"] == "gdpr_art83_special")
    assert (
        special.args["penalty_range"]
        == "Up to EUR 20,000,000 or 4% of total worldwide annual turnover"
    )

    consequences = query_consequences(engine, finding_id="finding-12")
    consequence_pairs = {(fact.args["rule_id"], fact.args["action"]) for fact in consequences}
    assert ("gdpr_art33_notification", "notify_regulator") in consequence_pairs
    assert ("gdpr_art34_communication", "notify_individuals") in consequence_pairs


def test_gdpr_notification_rules_do_not_fire_without_risk_facts() -> None:
    engine = ForwardChainingEngine()
    for rule in load_gdpr_rules():
        engine.add_rule(rule)

    add_finding_facts(
        engine,
        finding_id="finding-13",
        vuln_class="CWE-79",
        data_categories=["sexual_orientation", "name"],
        boolean_facts={"no_encryption_at_rest": True},
    )

    engine.run()

    obligation_ids = {
        fact.args["rule_id"] for fact in query_obligations(engine, finding_id="finding-13")
    }
    assert obligation_ids == {
        "gdpr_art32_security",
        "gdpr_art32_encryption",
        "gdpr_art83_standard",
        "gdpr_art83_special",
    }


def test_nis2_rules_fire_for_essential_entity_cross_border_supply_chain_incident() -> None:
    engine = ForwardChainingEngine()
    for rule in load_nis2_rules():
        engine.add_rule(rule)

    add_finding_facts(
        engine,
        finding_id="finding-14",
        vuln_class="CWE-89",
        data_categories=["public_info"],
        affected_individuals=900,
        boolean_facts={
            "cross_border": True,
            "is_essential_entity": True,
            "third_party_processor": True,
        },
        thresholds=(500,),
    )

    engine.run()

    obligations = query_obligations(engine, finding_id="finding-14")
    obligation_ids = {fact.args["rule_id"] for fact in obligations}

    assert obligation_ids == {
        "nis2_art21_risk_management",
        "nis2_art21_supply_chain",
        "nis2_art23_early_warning",
        "nis2_art23_incident_notification",
        "nis2_art23_cross_border",
        "nis2_art34_essential_penalties",
    }

    early_warning = next(
        fact for fact in obligations if fact.args["rule_id"] == "nis2_art23_early_warning"
    )
    assert early_warning.args["notification_timeline"] == (
        "24 hours from awareness of the significant incident"
    )

    notification = next(
        fact for fact in obligations if fact.args["rule_id"] == "nis2_art23_incident_notification"
    )
    assert notification.args["notification_timeline"] == (
        "72 hours from awareness of the significant incident"
    )

    essential_penalty = next(
        fact for fact in obligations if fact.args["rule_id"] == "nis2_art34_essential_penalties"
    )
    assert essential_penalty.args["penalty_range"] == (
        "Up to EUR 10,000,000 or 2% of total worldwide annual turnover, whichever is higher"
    )

    consequence_pairs = {
        (fact.args["rule_id"], fact.args["action"])
        for fact in query_consequences(engine, finding_id="finding-14")
    }
    assert ("nis2_art23_cross_border", "notify_regulator") in consequence_pairs
    assert ("nis2_art21_supply_chain", "review") in consequence_pairs


def test_nis2_rules_fire_for_important_entity_with_lower_penalty_tier() -> None:
    engine = ForwardChainingEngine()
    for rule in load_nis2_rules():
        engine.add_rule(rule)

    add_finding_facts(
        engine,
        finding_id="finding-15",
        vuln_class="CWE-78",
        data_categories=["public_info"],
        affected_individuals=700,
        boolean_facts={"is_important_entity": True},
        thresholds=(500,),
    )

    engine.run()

    obligation_ids = {
        fact.args["rule_id"] for fact in query_obligations(engine, finding_id="finding-15")
    }
    assert obligation_ids == {
        "nis2_art21_risk_management",
        "nis2_art23_early_warning",
        "nis2_art23_incident_notification",
        "nis2_art34_important_penalties",
    }


def test_nis2_rules_do_not_fire_without_entity_classification() -> None:
    engine = ForwardChainingEngine()
    for rule in load_nis2_rules():
        engine.add_rule(rule)

    add_finding_facts(
        engine,
        finding_id="finding-16",
        vuln_class="CWE-22",
        data_categories=["public_info"],
        affected_individuals=900,
        boolean_facts={
            "cross_border": True,
            "third_party_processor": True,
        },
        thresholds=(500,),
    )

    engine.run()

    assert query_obligations(engine, finding_id="finding-16") == []


def test_discover_rule_files_finds_community_rules(tmp_path: Path) -> None:
    core = tmp_path / "core.toml"
    core.write_text(
        "[[rules]]\n"
        'rule_id = "core_1"\nframework = "CORE"\n'
        'section = "S1"\nobligation_text = "test"\n'
        'consequences = ["remediate"]\npenalty_range = "N/A"\n'
    )
    community = tmp_path / "community"
    community.mkdir()
    contrib = community / "contrib.toml"
    contrib.write_text(
        "[[rules]]\n"
        'rule_id = "contrib_1"\nframework = "CONTRIB"\n'
        'section = "S1"\nobligation_text = "test"\n'
        'consequences = ["remediate"]\npenalty_range = "N/A"\n'
    )
    template = community / "_template.toml"
    template.write_text("# template, should be skipped\n")

    found = discover_rule_files(tmp_path)

    names = [p.name for p in found]
    assert "core.toml" in names
    assert "contrib.toml" in names
    assert "_template.toml" not in names


def test_discover_rule_files_loads_community_specs(tmp_path: Path) -> None:
    community = tmp_path / "community"
    community.mkdir()
    contrib = community / "example.toml"
    contrib.write_text(
        "[[rules]]\n"
        'rule_id = "example_1"\nframework = "EXAMPLE"\n'
        'section = "S1"\nobligation_text = "Example obligation"\n'
        'consequences = ["document"]\npenalty_range = "N/A"\n'
    )

    specs = load_all_rule_specs(tmp_path)

    assert len(specs) == 1
    assert specs[0].rule_id == "example_1"
    assert specs[0].framework == "EXAMPLE"


def test_discover_rule_files_skips_host_rule_schema(tmp_path: Path) -> None:
    host_rules = tmp_path / "community" / "host"
    host_rules.mkdir(parents=True)
    host_rule = host_rules / "ssh-password-authentication.toml"
    host_rule.write_text(
        "[rule]\n"
        'id = "community.ssh.password-authentication-enabled"\n'
        'title = "SSH password authentication should be disabled"\n'
        "[[match]]\n"
        'evidence = "config.ssh.PasswordAuthentication"\n'
        'equals = "yes"\n',
        encoding="utf-8",
    )

    assert host_rule not in discover_rule_files(tmp_path)
    assert load_all_rule_specs(tmp_path) == []


def test_malformed_toml_rejected_with_clear_error(tmp_path: Path) -> None:
    bad = tmp_path / "bad.toml"
    bad.write_text(
        '[[rules]]\nrule_id = "bad_1"\nframework = "BAD"\nextra_field_not_in_schema = true\n'
    )

    with pytest.raises(ValueError, match=r"Failed to load rule file bad\.toml"):
        load_all_rule_specs(tmp_path)


def test_malformed_toml_syntax_error_rejected(tmp_path: Path) -> None:
    bad = tmp_path / "broken.toml"
    bad.write_text("this is not valid toml [[[")

    with pytest.raises(ValueError, match=r"Failed to load rule file broken\.toml"):
        load_all_rule_specs(tmp_path)
