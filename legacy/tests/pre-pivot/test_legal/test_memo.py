from __future__ import annotations

from piranesi.legal.memo import DISCLAIMER_TEXT, assess_finding, build_default_engine
from piranesi.models import (
    ConfirmedFinding,
    SandboxResult,
    SourceLocation,
    TaintSink,
    TaintSource,
    TaintStep,
    TriagedFinding,
)
from piranesi.models.finding import CandidateFinding


def test_assess_finding_renders_pdpa_mas_and_ccpa_memo_for_fintech_sqli() -> None:
    finding = _build_confirmed_finding()
    assessment = assess_finding(finding, build_default_engine())

    obligation_sections = {(item.framework, item.section) for item in assessment.obligations}
    assert ("PDPA", "Section 24") in obligation_sections
    assert ("MAS_TRM", "Section 11.1 (System Reliability)") in obligation_sections
    assert ("CCPA", "Cal. Civ. Code Section 1798.100") in obligation_sections

    pdpa_standard = next(
        item for item in assessment.obligations if item.rule_id == "pdpa_s24_standard"
    )
    assert pdpa_standard.penalty_range == "Up to $1,000,000"
    assert pdpa_standard.evidence_role == "compliance_support"
    assert pdpa_standard.mapping_metadata is not None
    assert pdpa_standard.mapping_metadata.control_id == pdpa_standard.section
    assert pdpa_standard.mapping_metadata.framework_name.startswith("Personal Data Protection Act")
    assert pdpa_standard.mapping_metadata.last_reviewed == "2026-04-16"
    assert pdpa_standard.mapping_metadata.reviewer == "Piranesi compliance maintainers"
    assert pdpa_standard.mapping_metadata.source is not None
    assert 0.0 <= pdpa_standard.mapping_metadata.confidence <= 1.0

    notification = next(
        item for item in assessment.obligations if item.rule_id == "pdpa_s26d_notification"
    )
    assert (
        notification.notification_timeline
        == "3 calendar days from assessment of breach as notifiable"
    )

    ccpa_disclosure = next(
        item for item in assessment.obligations if item.rule_id == "ccpa_1798_100_disclosure"
    )
    assert "Section 1798.155" in ccpa_disclosure.penalty_range

    memo = assessment.memo_markdown
    assert DISCLAIMER_TEXT in memo
    assert "# Regulatory Impact Assessment" in memo
    assert "## Finding Reference" in memo
    assert "## Regulatory Frameworks" in memo
    assert "## Risk Assessment" in memo
    assert "## Recommended Actions" in memo
    assert "PIRANESI-2026-0042" in memo
    assert "| Exploit Confirmation | CONFIRMED |" in memo
    assert "Personal Data Protection Act 2012 (PDPA)" in memo
    assert "MAS Technology Risk Management Guidelines (MAS TRM)" in memo
    assert "California Consumer Privacy Act / California Privacy Rights Act (CCPA/CPRA)" in memo
    assert "Up to $1,000,000" in memo
    assert "Up to $2,500 per violation" in memo
    assert "3 calendar days from assessment of breach as notifiable" in memo
    assert "Section 11.1 (System Reliability)" in memo
    assert "Evidence role:** compliance support mapping (not certification evidence)" in memo


def test_assess_finding_renders_hipaa_memo_for_healthcare_phi_exposure() -> None:
    finding = _build_healthcare_confirmed_finding()

    assessment = assess_finding(finding, build_default_engine())

    obligation_sections = {(item.framework, item.section) for item in assessment.obligations}
    assert ("HIPAA", "45 CFR 164.312(a)") in obligation_sections
    assert ("HIPAA", "45 CFR 164.312(b)") in obligation_sections
    assert ("HIPAA", "45 CFR 164.312(e)") in obligation_sections

    hipaa_access = next(
        item for item in assessment.obligations if item.rule_id == "hipaa_164_312_access_control"
    )
    assert "Tiered civil monetary penalties" in hipaa_access.penalty_range

    memo = assessment.memo_markdown
    assert "Health Insurance Portability and Accountability Act (HIPAA)" in memo
    assert "45 CFR 164.312(e)" in memo
    assert "Tiered civil monetary penalties" in memo


def test_assess_finding_renders_gdpr_memo_for_special_category_breach() -> None:
    finding = _build_gdpr_confirmed_finding()

    assessment = assess_finding(finding, build_default_engine())

    obligation_ids = {item.rule_id for item in assessment.obligations}
    assert {
        "gdpr_art32_security",
        "gdpr_art32_encryption",
        "gdpr_art33_notification",
        "gdpr_art34_communication",
        "gdpr_art83_standard",
        "gdpr_art83_aggravated",
        "gdpr_art83_special",
    }.issubset(obligation_ids)

    communication = next(
        item for item in assessment.obligations if item.rule_id == "gdpr_art34_communication"
    )
    assert communication.notification_timeline == "Without undue delay"

    memo = assessment.memo_markdown
    assert "General Data Protection Regulation (GDPR)" in memo
    assert "72 hours after becoming aware of the personal data breach" in memo
    assert "Up to EUR 20,000,000 or 4% of total worldwide annual turnover" in memo


def test_assess_finding_renders_nis2_memo_for_essential_cross_border_incident() -> None:
    finding = _build_nis2_confirmed_finding()

    assessment = assess_finding(finding, build_default_engine())

    obligation_ids = {item.rule_id for item in assessment.obligations}
    assert {
        "nis2_art21_risk_management",
        "nis2_art21_supply_chain",
        "nis2_art23_early_warning",
        "nis2_art23_incident_notification",
        "nis2_art23_cross_border",
        "nis2_art34_essential_penalties",
    } == obligation_ids

    early_warning = next(
        item for item in assessment.obligations if item.rule_id == "nis2_art23_early_warning"
    )
    assert early_warning.notification_timeline == (
        "24 hours from awareness of the significant incident"
    )

    essential_penalty = next(
        item for item in assessment.obligations if item.rule_id == "nis2_art34_essential_penalties"
    )
    assert essential_penalty.penalty_range == (
        "Up to EUR 10,000,000 or 2% of total worldwide annual turnover, whichever is higher"
    )

    memo = assessment.memo_markdown
    assert "NIS2 Directive (Directive (EU) 2022/2555)" in memo
    assert "Article 23 (72h Incident Notification)" in memo
    assert "24 hours from awareness of the significant incident" in memo
    assert "Up to EUR 10,000,000 or 2% of total worldwide annual turnover" in memo


def _build_confirmed_finding() -> ConfirmedFinding:
    source_location = SourceLocation(
        file="src/routes/kyc.ts",
        line=42,
        column=18,
        snippet="const nric = req.body.nric;\n",
    )
    step_location = SourceLocation(
        file="src/routes/kyc.ts",
        line=84,
        column=9,
        snippet='const sql = `SELECT * FROM customers WHERE nric = "${nric}"`;\n',
    )
    sink_location = SourceLocation(
        file="src/routes/kyc.ts",
        line=87,
        column=5,
        snippet="await db.query(sql);\n",
    )

    candidate = CandidateFinding(
        id="PIRANESI-2026-0042",
        vuln_class="CWE-89: SQL Injection",
        source=TaintSource(
            location=source_location,
            source_type="req.body.nric",
            data_categories=["nric", "financial_bank", "name"],
            parameter_name="nric",
        ),
        sink=TaintSink(
            location=sink_location,
            sink_type="sql",
            api_name="db.query",
        ),
        taint_path=[
            TaintStep(
                location=step_location,
                operation="build_sql",
                taint_state="tainted",
                through_function="lookupCustomerByNric",
            )
        ],
        path_conditions=[],
        confidence=0.99,
        severity="critical",
        affected_individuals_estimate=2000,
    )
    triaged = TriagedFinding(
        finding=candidate,
        triage_verdict="confirmed_true_positive",
        skeptic_analysis="Exploit evidence matches an injectable SQL sink.",
        ensemble_score=0.98,
        escalated=False,
    )
    sandbox_result = SandboxResult(
        container_id="sandbox-42",
        request={"path": "/kyc", "method": "POST"},
        response={"status": 500, "body": "SQL syntax error near UNION SELECT"},
        timing_ms=18,
        side_effects=[],
        container_diff=[],
        stdout="",
        stderr="",
        exit_code=0,
        network_isolated=True,
        confirmed=True,
    )
    return ConfirmedFinding(
        finding=triaged,
        exploit_payload="' UNION SELECT nric,full_name FROM customers --",
        exploit_constraints=["input reaches db.query unsanitized"],
        sandbox_result=sandbox_result,
        reproducer_script=(
            "curl -X POST http://localhost:3000/kyc -d 'nric=%27%20UNION%20SELECT...'"
        ),
        related_cves=[],
    )


def _build_healthcare_confirmed_finding() -> ConfirmedFinding:
    source_location = SourceLocation(
        file="src/routes/patient-portal.ts",
        line=18,
        column=16,
        snippet="const note = req.query.note;\n",
    )
    step_location = SourceLocation(
        file="src/routes/patient-portal.ts",
        line=31,
        column=9,
        snippet="const html = `<div>${note}</div>`;\n",
    )
    sink_location = SourceLocation(
        file="src/routes/patient-portal.ts",
        line=32,
        column=5,
        snippet="res.send(html);\n",
    )

    candidate = CandidateFinding(
        id="PIRANESI-2026-0043",
        vuln_class="CWE-79: Cross-Site Scripting",
        source=TaintSource(
            location=source_location,
            source_type="req.query.note",
            data_categories=["health"],
            parameter_name="note",
        ),
        sink=TaintSink(
            location=sink_location,
            sink_type="html_response",
            api_name="res.send",
        ),
        taint_path=[
            TaintStep(
                location=step_location,
                operation="template_render",
                taint_state="tainted",
                through_function="renderPatientPortal",
            )
        ],
        path_conditions=[],
        confidence=0.97,
        severity="high",
        affected_individuals_estimate=400,
        is_healthcare_entity=True,
    )
    triaged = TriagedFinding(
        finding=candidate,
        triage_verdict="confirmed_true_positive",
        skeptic_analysis="Browser-rendered PHI is reflected without output encoding.",
        ensemble_score=0.95,
        escalated=False,
    )
    sandbox_result = SandboxResult(
        container_id="sandbox-43",
        request={"path": "/portal?note=%3Cscript%3Ealert(1)%3C/script%3E", "method": "GET"},
        response={"status": 200, "body": "<div><script>alert(1)</script></div>"},
        timing_ms=12,
        side_effects=[],
        container_diff=[],
        stdout="",
        stderr="",
        exit_code=0,
        network_isolated=True,
        confirmed=True,
    )
    return ConfirmedFinding(
        finding=triaged,
        exploit_payload="<script>alert(1)</script>",
        exploit_constraints=["note reaches res.send without output encoding"],
        sandbox_result=sandbox_result,
        reproducer_script=(
            "curl 'http://localhost:3000/portal?note=%3Cscript%3Ealert(1)%3C/script%3E'"
        ),
        related_cves=[],
    )


def _build_gdpr_confirmed_finding() -> ConfirmedFinding:
    source_location = SourceLocation(
        file="src/routes/profile.ts",
        line=12,
        column=20,
        snippet="const orientation = req.body.sexualOrientation;\n",
    )
    step_location = SourceLocation(
        file="src/routes/profile.ts",
        line=27,
        column=9,
        snippet='const sql = `SELECT * FROM profiles WHERE orientation = "${orientation}"`;\n',
    )
    sink_location = SourceLocation(
        file="src/routes/profile.ts",
        line=30,
        column=5,
        snippet="await db.query(sql);\n",
    )

    candidate = CandidateFinding(
        id="PIRANESI-2026-0044",
        vuln_class="CWE-89: SQL Injection",
        source=TaintSource(
            location=source_location,
            source_type="req.body.sexualOrientation",
            data_categories=["sexual_orientation", "political", "name"],
            parameter_name="sexualOrientation",
        ),
        sink=TaintSink(
            location=sink_location,
            sink_type="sql",
            api_name="db.query",
        ),
        taint_path=[
            TaintStep(
                location=step_location,
                operation="build_sql",
                taint_state="tainted",
                through_function="lookupSensitiveProfile",
            )
        ],
        path_conditions=[],
        confidence=0.98,
        severity="critical",
        affected_individuals_estimate=800,
        no_encryption_at_rest=True,
        likely_risk_to_rights=True,
        high_risk_to_individuals=True,
        basic_processing_principle_violation=True,
    )
    triaged = TriagedFinding(
        finding=candidate,
        triage_verdict="confirmed_true_positive",
        skeptic_analysis="Special-category profile data reaches a raw SQL sink.",
        ensemble_score=0.97,
        escalated=False,
    )
    sandbox_result = SandboxResult(
        container_id="sandbox-44",
        request={"path": "/profile", "method": "POST"},
        response={"status": 500, "body": "syntax error at or near UNION"},
        timing_ms=21,
        side_effects=[],
        container_diff=[],
        stdout="",
        stderr="",
        exit_code=0,
        network_isolated=True,
        confirmed=True,
    )
    return ConfirmedFinding(
        finding=triaged,
        exploit_payload="' UNION SELECT orientation,political_view FROM profiles --",
        exploit_constraints=["special-category profile data reaches db.query unsanitized"],
        sandbox_result=sandbox_result,
        reproducer_script=(
            "curl -X POST http://localhost:3000/profile -d 'sexualOrientation=%27%20UNION...'"
        ),
        related_cves=[],
    )


def _build_nis2_confirmed_finding() -> ConfirmedFinding:
    source_location = SourceLocation(
        file="src/routes/operations.ts",
        line=14,
        column=16,
        snippet="const deviceStatus = req.body.deviceStatus;\n",
    )
    step_location = SourceLocation(
        file="src/routes/operations.ts",
        line=28,
        column=9,
        snippet='const sql = `SELECT * FROM assets WHERE status = "${deviceStatus}"`;\n',
    )
    sink_location = SourceLocation(
        file="src/routes/operations.ts",
        line=31,
        column=5,
        snippet="await db.query(sql);\n",
    )

    candidate = CandidateFinding(
        id="PIRANESI-2026-0045",
        vuln_class="CWE-89: SQL Injection",
        source=TaintSource(
            location=source_location,
            source_type="req.body.deviceStatus",
            data_categories=[],
            parameter_name="deviceStatus",
        ),
        sink=TaintSink(
            location=sink_location,
            sink_type="sql",
            api_name="db.query",
        ),
        taint_path=[
            TaintStep(
                location=step_location,
                operation="build_sql",
                taint_state="tainted",
                through_function="listOperationalAssets",
            )
        ],
        path_conditions=[],
        confidence=0.98,
        severity="critical",
        affected_individuals_estimate=1200,
        cross_border=True,
        is_essential_entity=True,
        third_party_processor=True,
    )
    triaged = TriagedFinding(
        finding=candidate,
        triage_verdict="confirmed_true_positive",
        skeptic_analysis="Operational asset status input reaches db.query unsanitized.",
        ensemble_score=0.97,
        escalated=False,
    )
    sandbox_result = SandboxResult(
        container_id="sandbox-45",
        request={"path": "/operations/assets", "method": "POST"},
        response={"status": 500, "body": "syntax error near UNION"},
        timing_ms=16,
        side_effects=[],
        container_diff=[],
        stdout="",
        stderr="",
        exit_code=0,
        network_isolated=True,
        confirmed=True,
    )
    return ConfirmedFinding(
        finding=triaged,
        exploit_payload="' UNION SELECT hostname,status FROM assets --",
        exploit_constraints=["deviceStatus reaches db.query unsanitized"],
        sandbox_result=sandbox_result,
        reproducer_script=(
            "curl -X POST http://localhost:3000/operations/assets -d 'deviceStatus=%27%20UNION...'"
        ),
        related_cves=[],
    )
