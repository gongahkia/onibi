from __future__ import annotations

from pathlib import Path

from piranesi.legal import assess_finding, build_default_engine
from piranesi.legal.rules import (
    detect_payment_processing_scope,
    load_pci_dss_rule_specs,
    load_soc2_rule_specs,
)
from piranesi.models import (
    ConfirmedFinding,
    SandboxResult,
    SourceLocation,
    TaintSink,
    TaintSource,
    TriagedFinding,
)
from piranesi.models.finding import CandidateFinding


def test_soc2_toml_loads_expected_rule_specs() -> None:
    rule_specs = load_soc2_rule_specs()

    assert [rule_spec.rule_id for rule_spec in rule_specs] == [
        "soc2_cc6_1_auth_bypass",
        "soc2_cc6_1_idor",
        "soc2_cc6_1_access_control",
        "soc2_cc6_6_sqli",
        "soc2_cc6_6_xss",
        "soc2_cc6_6_cmdi",
        "soc2_cc6_7_path_traversal",
        "soc2_cc6_7_ssrf",
        "soc2_cc6_8_deserialization",
        "soc2_cc6_8_code_injection",
        "soc2_cc7_1_hardcoded_secrets",
        "soc2_cc7_1_log_integrity",
        "soc2_cc7_2_info_leak",
        "soc2_cc8_1_vulnerable_deps",
        "soc2_cc8_1_outdated_deps",
    ]


def test_pci_dss_toml_loads_expected_rule_specs() -> None:
    rule_specs = load_pci_dss_rule_specs()

    assert [rule_spec.rule_id for rule_spec in rule_specs] == [
        "pci_dss_req_3_4_stored_data",
        "pci_dss_req_4_1_transit",
        "pci_dss_req_6_2_1_bespoke_security",
        "pci_dss_req_6_2_2_security_review",
        "pci_dss_req_6_3_1_known_vulnerabilities",
        "pci_dss_req_6_3_2_inventory",
        "pci_dss_req_6_4_1_public_web_protection",
        "pci_dss_req_6_4_2_automated_web_solution",
        "pci_dss_req_6_5_1_injection",
        "pci_dss_req_6_5_2_memory",
        "pci_dss_req_6_5_3_crypto",
        "pci_dss_req_6_5_4_communications",
        "pci_dss_req_6_5_5_error_handling",
        "pci_dss_req_6_5_6_high_risk",
        "pci_dss_req_8_3_1_authentication",
        "pci_dss_req_8_3_6_factor_complexity",
        "pci_dss_req_10_2_1_log_coverage",
        "pci_dss_req_10_2_2_log_integrity",
        "pci_dss_req_11_3_1_internal_scans",
        "pci_dss_req_11_3_2_external_scans",
    ]


def test_soc2_authentication_bypass_maps_to_cc6_1() -> None:
    assessment = _assess("CWE-287")
    assert _rule_ids(assessment, "SOC2") == {"soc2_cc6_1_auth_bypass"}


def test_soc2_command_injection_maps_to_cc6_6() -> None:
    assessment = _assess("CWE-78")
    assert _rule_ids(assessment, "SOC2") == {"soc2_cc6_6_cmdi"}


def test_soc2_ssrf_maps_to_cc6_7() -> None:
    assessment = _assess("CWE-918")
    assert _rule_ids(assessment, "SOC2") == {"soc2_cc6_7_ssrf"}


def test_soc2_deserialization_maps_to_cc6_8() -> None:
    assessment = _assess("CWE-502")
    assert _rule_ids(assessment, "SOC2") == {"soc2_cc6_8_deserialization"}


def test_soc2_log_integrity_maps_to_cc7_1() -> None:
    assessment = _assess("CWE-117")
    assert _rule_ids(assessment, "SOC2") == {"soc2_cc7_1_log_integrity"}


def test_soc2_information_leak_maps_to_cc7_2() -> None:
    assessment = _assess("CWE-209")
    assert _rule_ids(assessment, "SOC2") == {"soc2_cc7_2_info_leak"}


def test_soc2_known_vulnerable_dependency_maps_to_cc8_1() -> None:
    assessment = _assess(
        "CWE-1395",
        metadata={
            "package": "lodash",
            "package_version": "4.17.20",
            "patched_version": "4.17.21",
            "cve_id": "CVE-2024-0001",
        },
        dependency=True,
    )
    assert _rule_ids(assessment, "SOC2") == {"soc2_cc8_1_vulnerable_deps"}


def test_soc2_outdated_dependency_maps_to_cc8_1() -> None:
    assessment = _assess(
        "CWE-1395",
        metadata={
            "package": "legacy-payments",
            "package_version": "1.4.0",
            "patched_version": "4.0.0",
        },
        dependency=True,
    )
    assert _rule_ids(assessment, "SOC2") == {"soc2_cc8_1_outdated_deps"}


def test_pci_dss_scope_guard_blocks_rules_when_payment_scope_missing() -> None:
    assessment = _assess("CWE-319")
    assert _rule_ids(assessment, "PCI_DSS") == set()


def test_pci_dss_stored_data_rule_maps_req_3_4() -> None:
    assessment = _assess("CWE-327", extra_boolean_facts={"is_payment_processing": True})
    assert "pci_dss_req_3_4_stored_data" in _rule_ids(assessment, "PCI_DSS")


def test_pci_dss_transit_rule_maps_req_4_1_and_req_6_5_4() -> None:
    assessment = _assess("CWE-295", extra_boolean_facts={"is_payment_processing": True})
    assert _rule_ids(assessment, "PCI_DSS") == {
        "pci_dss_req_4_1_transit",
        "pci_dss_req_6_2_2_security_review",
        "pci_dss_req_6_5_4_communications",
    }


def test_pci_dss_high_severity_finding_maps_security_review() -> None:
    assessment = _assess(
        "CWE-200",
        severity="high",
        extra_boolean_facts={"is_payment_processing": True},
    )
    assert "pci_dss_req_6_2_2_security_review" in _rule_ids(assessment, "PCI_DSS")


def test_pci_dss_known_vulnerable_dependency_maps_req_6_3_1() -> None:
    assessment = _assess(
        "CWE-1395",
        metadata={
            "package": "lodash",
            "package_version": "4.17.20",
            "patched_version": "4.17.21",
            "cve_id": "CVE-2024-0001",
        },
        dependency=True,
        extra_boolean_facts={"is_payment_processing": True},
    )
    assert "pci_dss_req_6_3_1_known_vulnerabilities" in _rule_ids(assessment, "PCI_DSS")


def test_pci_dss_outdated_dependency_maps_req_6_3_2() -> None:
    assessment = _assess(
        "CWE-1395",
        metadata={
            "package": "legacy-gateway",
            "package_version": "1.0.0",
            "patched_version": "5.1.0",
        },
        dependency=True,
        extra_boolean_facts={"is_payment_processing": True},
    )
    assert "pci_dss_req_6_3_2_inventory" in _rule_ids(assessment, "PCI_DSS")


def test_pci_dss_public_facing_xss_maps_web_app_controls() -> None:
    assessment = _assess(
        "CWE-79",
        extra_boolean_facts={"is_payment_processing": True},
        source_type="req.body.checkout_session",
        file_name="src/routes/checkout.ts",
    )
    assert {
        "pci_dss_req_6_2_1_bespoke_security",
        "pci_dss_req_6_2_2_security_review",
        "pci_dss_req_6_4_1_public_web_protection",
        "pci_dss_req_6_4_2_automated_web_solution",
        "pci_dss_req_6_5_1_injection",
    }.issubset(_rule_ids(assessment, "PCI_DSS"))


def test_pci_dss_critical_finding_maps_req_6_5_6() -> None:
    assessment = _assess(
        "CWE-89",
        severity="critical",
        extra_boolean_facts={"is_payment_processing": True},
    )
    assert "pci_dss_req_6_5_6_high_risk" in _rule_ids(assessment, "PCI_DSS")


def test_pci_dss_authentication_rule_maps_req_8_3_1() -> None:
    assessment = _assess("CWE-287", extra_boolean_facts={"is_payment_processing": True})
    assert "pci_dss_req_8_3_1_authentication" in _rule_ids(assessment, "PCI_DSS")


def test_pci_dss_password_complexity_rule_maps_req_8_3_6() -> None:
    assessment = _assess("CWE-521", extra_boolean_facts={"is_payment_processing": True})
    assert "pci_dss_req_8_3_6_factor_complexity" in _rule_ids(assessment, "PCI_DSS")


def test_pci_dss_audit_log_coverage_rule_maps_req_10_2_1() -> None:
    assessment = _assess("CWE-778", extra_boolean_facts={"is_payment_processing": True})
    assert "pci_dss_req_10_2_1_log_coverage" in _rule_ids(assessment, "PCI_DSS")


def test_pci_dss_audit_log_integrity_rule_maps_req_10_2_2() -> None:
    assessment = _assess("CWE-117", extra_boolean_facts={"is_payment_processing": True})
    assert "pci_dss_req_10_2_2_log_integrity" in _rule_ids(assessment, "PCI_DSS")


def test_pci_dss_meta_scan_controls_do_not_attach_to_findings() -> None:
    assessment = _assess("CWE-79", extra_boolean_facts={"is_payment_processing": True})
    assert "pci_dss_req_11_3_1_internal_scans" not in _rule_ids(assessment, "PCI_DSS")
    assert "pci_dss_req_11_3_2_external_scans" not in _rule_ids(assessment, "PCI_DSS")


def test_payment_scope_detection_requires_two_real_code_hits(tmp_path: Path) -> None:
    payment_file = tmp_path / "src" / "checkout.ts"
    payment_file.parent.mkdir(parents=True)
    payment_file.write_text(
        "import Stripe from 'stripe';\n"
        "const checkout = '/checkout';\n",
        encoding="utf-8",
    )

    assessment = detect_payment_processing_scope(tmp_path, files=[str(payment_file)])

    assert assessment.is_payment_processing is True
    assert assessment.hit_count >= 2


def test_payment_scope_detection_ignores_comment_only_hits(tmp_path: Path) -> None:
    payment_file = tmp_path / "src" / "notes.ts"
    payment_file.parent.mkdir(parents=True)
    payment_file.write_text(
        "// stripe payment checkout billing\n"
        "const label = 'orders';\n",
        encoding="utf-8",
    )

    assessment = detect_payment_processing_scope(tmp_path, files=[str(payment_file)])

    assert assessment.is_payment_processing is False
    assert assessment.hit_count == 0


def _assess(
    cwe: str,
    *,
    severity: str = "high",
    source_type: str = "request.input",
    file_name: str = "src/app.ts",
    metadata: dict[str, object] | None = None,
    dependency: bool = False,
    extra_boolean_facts: dict[str, bool] | None = None,
) -> object:
    location = SourceLocation(
        file=file_name,
        line=10,
        column=1,
        snippet="placeholder",
    )
    candidate = CandidateFinding(
        id=f"finding-{cwe.lower()}",
        vuln_class=f"{cwe}: Example finding",
        source=TaintSource(
            location=location,
            source_type="dependency_manifest" if dependency else source_type,
            data_categories=[],
            parameter_name="payment" if not dependency else "package",
        ),
        sink=TaintSink(
            location=location,
            sink_type="dependency_vulnerability" if dependency else "sink",
            api_name=(metadata or {}).get("cve_id", "execute"),  # type: ignore[arg-type]
        ),
        taint_path=[],
        path_conditions=[],
        confidence=0.95,
        severity=severity,
        metadata=metadata or {},
    )
    confirmed = ConfirmedFinding(
        finding=TriagedFinding(
            finding=candidate,
            triage_verdict="true_positive",
            skeptic_analysis="test fixture",
            ensemble_score=0.99,
            escalated=False,
        ),
        exploit_payload="payload",
        exploit_constraints=[],
        sandbox_result=SandboxResult(
            container_id="sandbox",
            request={},
            response={},
            timing_ms=1,
            side_effects=[],
            container_diff=[],
            stdout="",
            stderr="",
            exit_code=0,
            network_isolated=True,
            confirmed=True,
        ),
        reproducer_script="echo test",
        related_cves=[],
    )
    return assess_finding(
        confirmed,
        build_default_engine(),
        extra_boolean_facts=extra_boolean_facts,
    )


def _rule_ids(assessment: object, framework: str) -> set[str]:
    obligations = getattr(assessment, "obligations")
    return {
        obligation.rule_id
        for obligation in obligations
        if obligation.framework == framework and obligation.rule_id is not None
    }
