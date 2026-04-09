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


def test_assess_finding_renders_pdpa_and_mas_memo_for_fintech_sqli() -> None:
    finding = _build_confirmed_finding()
    assessment = assess_finding(finding, build_default_engine())

    obligation_sections = {(item.framework, item.section) for item in assessment.obligations}
    assert ("PDPA", "Section 24") in obligation_sections
    assert ("MAS_TRM", "Section 11.1 (System Reliability)") in obligation_sections

    pdpa_standard = next(
        item for item in assessment.obligations if item.rule_id == "pdpa_s24_standard"
    )
    assert pdpa_standard.penalty_range == "Up to $1,000,000"

    notification = next(
        item for item in assessment.obligations if item.rule_id == "pdpa_s26d_notification"
    )
    assert (
        notification.notification_timeline
        == "3 calendar days from assessment of breach as notifiable"
    )

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
    assert "Up to $1,000,000" in memo
    assert "3 calendar days from assessment of breach as notifiable" in memo
    assert "Section 11.1 (System Reliability)" in memo


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
            "curl -X POST http://localhost:3000/kyc "
            "-d 'nric=%27%20UNION%20SELECT...'"
        ),
        related_cves=[],
    )
