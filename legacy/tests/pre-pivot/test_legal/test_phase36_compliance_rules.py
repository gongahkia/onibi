from __future__ import annotations

from piranesi.legal.engine import ForwardChainingEngine
from piranesi.legal.rules import (
    add_finding_facts,
    load_cis_rule_specs,
    load_cis_rules,
    load_iso27001_rule_specs,
    load_iso27001_rules,
    load_nist_csf_rule_specs,
    load_nist_csf_rules,
    query_obligations,
)


def test_iso27001_toml_loads_expected_rule_specs() -> None:
    rule_specs = load_iso27001_rule_specs()

    assert [rule_spec.rule_id for rule_spec in rule_specs] == [
        "iso27001_a8_3_auth_bypass",
        "iso27001_a8_3_idor",
        "iso27001_a8_3_access_control",
        "iso27001_a8_4_hardcoded_secrets",
        "iso27001_a8_4_source_exposure",
        "iso27001_a8_7_deserialization",
        "iso27001_a8_7_code_injection",
        "iso27001_a8_8_known_cve",
        "iso27001_a8_8_high_severity",
        "iso27001_a8_9_misconfig",
        "iso27001_a8_9_cookie_flags",
        "iso27001_a8_12_cleartext",
        "iso27001_a8_12_path_traversal",
        "iso27001_a8_12_info_exposure",
        "iso27001_a8_24_weak_crypto",
        "iso27001_a8_24_weak_prng",
        "iso27001_a8_25_secure_sdl",
        "iso27001_a8_28_injection",
    ]


def test_nist_csf_toml_loads_expected_rule_specs() -> None:
    rule_specs = load_nist_csf_rule_specs()

    assert [rule_spec.rule_id for rule_spec in rule_specs] == [
        "nist_csf_pr_aa_1_auth",
        "nist_csf_pr_aa_1_secrets",
        "nist_csf_pr_aa_1_password",
        "nist_csf_pr_aa_2_session",
        "nist_csf_pr_ds_1_storage",
        "nist_csf_pr_ds_2_transit",
        "nist_csf_pr_ds_10_in_use",
        "nist_csf_pr_ps_1_config",
        "nist_csf_id_ra_1_vuln",
        "nist_csf_id_ra_2_cve",
        "nist_csf_de_cm_6_deps",
        "nist_csf_pr_ip_injection",
        "nist_csf_pr_ip_input_val",
        "nist_csf_pr_ac_idor",
        "nist_csf_pr_ac_escalation",
    ]


def test_cis_toml_loads_expected_rule_specs() -> None:
    rule_specs = load_cis_rule_specs()

    assert [rule_spec.rule_id for rule_spec in rule_specs] == [
        "cis_16_1_sdl_evidence",
        "cis_16_3_root_cause",
        "cis_16_4_sca_inventory",
        "cis_16_5_outdated_deps",
        "cis_16_5_known_cve",
        "cis_16_6_severity_applied",
        "cis_16_9_training_gap",
        "cis_16_12_sast_evidence",
    ]


def test_iso27001_auth_maps_a8_3() -> None:
    obligations = _run_rules(load_iso27001_rules(), vuln_class="CWE-287")
    assert _obligation_ids(obligations) >= {"iso27001_a8_3_auth_bypass"}


def test_iso27001_secrets_maps_a8_4() -> None:
    obligations = _run_rules(load_iso27001_rules(), vuln_class="CWE-798")
    assert _obligation_ids(obligations) >= {"iso27001_a8_4_hardcoded_secrets"}


def test_iso27001_deserialization_maps_a8_7() -> None:
    obligations = _run_rules(load_iso27001_rules(), vuln_class="CWE-502")
    assert _obligation_ids(obligations) >= {"iso27001_a8_7_deserialization"}


def test_iso27001_cve_maps_a8_8() -> None:
    obligations = _run_rules(
        load_iso27001_rules(),
        vuln_class="CWE-1395",
        boolean_facts={"has_cve": True},
    )
    assert _obligation_ids(obligations) >= {"iso27001_a8_8_known_cve"}


def test_iso27001_misconfig_maps_a8_9() -> None:
    obligations = _run_rules(load_iso27001_rules(), vuln_class="CWE-942")
    assert _obligation_ids(obligations) >= {"iso27001_a8_9_misconfig"}


def test_iso27001_cleartext_maps_a8_12() -> None:
    obligations = _run_rules(load_iso27001_rules(), vuln_class="CWE-319")
    assert _obligation_ids(obligations) >= {"iso27001_a8_12_cleartext"}


def test_iso27001_weak_crypto_maps_a8_24() -> None:
    obligations = _run_rules(load_iso27001_rules(), vuln_class="CWE-327")
    assert _obligation_ids(obligations) >= {"iso27001_a8_24_weak_crypto"}


def test_iso27001_injection_maps_a8_28() -> None:
    obligations = _run_rules(load_iso27001_rules(), vuln_class="CWE-89")
    assert _obligation_ids(obligations) >= {"iso27001_a8_28_injection"}


def test_nist_csf_auth_maps_pr_aa_1() -> None:
    obligations = _run_rules(load_nist_csf_rules(), vuln_class="CWE-287")
    assert _obligation_ids(obligations) >= {"nist_csf_pr_aa_1_auth"}


def test_nist_csf_cleartext_maps_pr_ds_2() -> None:
    obligations = _run_rules(load_nist_csf_rules(), vuln_class="CWE-319")
    assert _obligation_ids(obligations) >= {"nist_csf_pr_ds_2_transit"}


def test_nist_csf_crypto_maps_pr_ds_1() -> None:
    obligations = _run_rules(load_nist_csf_rules(), vuln_class="CWE-327")
    assert _obligation_ids(obligations) >= {"nist_csf_pr_ds_1_storage"}


def test_nist_csf_injection_maps_pr_ip() -> None:
    obligations = _run_rules(load_nist_csf_rules(), vuln_class="CWE-89")
    assert _obligation_ids(obligations) >= {"nist_csf_pr_ip_injection"}


def test_nist_csf_cve_maps_id_ra_2() -> None:
    obligations = _run_rules(
        load_nist_csf_rules(),
        vuln_class="CWE-1395",
        boolean_facts={"has_cve": True},
    )
    assert _obligation_ids(obligations) >= {"nist_csf_id_ra_2_cve"}


def test_cis_16_5_outdated_dep() -> None:
    obligations = _run_rules(
        load_cis_rules(),
        vuln_class="CWE-1395",
        boolean_facts={"outdated_dependency": True},
    )
    assert _obligation_ids(obligations) >= {"cis_16_5_outdated_deps"}


def test_cis_16_5_known_cve() -> None:
    obligations = _run_rules(
        load_cis_rules(),
        vuln_class="CWE-1395",
        boolean_facts={"has_cve": True},
    )
    assert _obligation_ids(obligations) >= {"cis_16_5_known_cve"}


def test_cis_16_9_training_gap() -> None:
    obligations = _run_rules(
        load_cis_rules(),
        vuln_class="CWE-89",
        boolean_facts={"repeated_cwe_pattern": True},
    )
    assert _obligation_ids(obligations) >= {"cis_16_9_training_gap"}


def test_cis_16_12_meta_evidence() -> None:
    obligations = _run_rules(
        load_cis_rules(),
        vuln_class="CWE-89",
        boolean_facts={"scan_executed": True},
    )
    assert _obligation_ids(obligations) >= {"cis_16_12_sast_evidence"}


def _run_rules(
    rules: list[object],
    *,
    vuln_class: str,
    severity: str = "high",
    boolean_facts: dict[str, bool] | None = None,
) -> list[object]:
    engine = ForwardChainingEngine()
    for rule in rules:
        engine.add_rule(rule)

    add_finding_facts(
        engine,
        finding_id="finding-1",
        vuln_class=vuln_class,
        data_categories=[],
        severity=severity,
        boolean_facts=boolean_facts,
    )
    engine.run()
    return query_obligations(engine, finding_id="finding-1")


def _obligation_ids(obligations: list[object]) -> set[str]:
    return {str(obligation.args["rule_id"]) for obligation in obligations}
