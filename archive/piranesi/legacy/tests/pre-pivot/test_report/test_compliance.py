from __future__ import annotations

from pathlib import Path

from tests._pipeline_fixtures import fixture_artifacts

from piranesi.models import RegulatoryObligation
from piranesi.report.compliance import render_attestation, render_compliance_report
from piranesi.report.renderer import PiranesiReport, build_report


def test_render_compliance_report_includes_all_supported_frameworks(tmp_path: Path) -> None:
    report = _build_report(tmp_path)
    finding = report.findings[0]
    obligations = [
        RegulatoryObligation(
            framework=framework,
            section=section,
            obligation_text=f"Assess {framework} obligations for the confirmed finding.",
            data_categories_affected=["name"],
            penalty_range=penalty,
            notification_timeline=timeline,
            enforcement_precedents=[f"{framework} precedent"],
            consequences=["document", "remediate"],
        )
        for framework, section, timeline, penalty in (
            ("GDPR", "Art. 32", "72 hours", "Up to EUR 20M or 4% global annual turnover"),
            (
                "CCPA",
                "Section 1798.100",
                "Without unreasonable delay",
                "Up to $7,500 per intentional violation",
            ),
            (
                "HIPAA",
                "45 CFR 164.312(a)",
                "Without unreasonable delay",
                "Tiered civil monetary penalties",
            ),
            ("NIS2", "Article 23", "24 hours", "Up to EUR 10M or 2% global annual turnover"),
            ("PDPA", "Section 24", "3 calendar days", "Up to SGD 1M"),
            ("EU_AI_ACT", "Article 9", None, "Up to EUR 35M or 7% global annual turnover"),
            (
                "MAS_TRM",
                "Section 11.1",
                None,
                "Supervisory action and direct financial penalty exposure",
            ),
        )
    ]
    enriched_report = report.model_copy(
        update={
            "findings": [
                finding.model_copy(
                    update={
                        "regulatory_obligations": obligations,
                    }
                )
            ]
        }
    )

    rendered = render_compliance_report(enriched_report)

    assert "Compliance Claim Boundary" in rendered
    assert "supporting security evidence only" in rendered
    assert "Framework version" in rendered
    assert "Mapping confidence" in rendered
    for label in ("GDPR", "CCPA", "HIPAA", "NIS2", "PDPA", "EU AI", "MAS TRM"):
        assert label in rendered
    for framework_label in (
        "General Data Protection Regulation (GDPR)",
        "California Consumer Privacy Act / California Privacy Rights Act (CCPA/CPRA)",
        "Health Insurance Portability and Accountability Act (HIPAA)",
        "NIS2 Directive (Directive (EU) 2022/2555)",
        "Personal Data Protection Act 2012 (PDPA)",
        "EU Artificial Intelligence Act (EU AI Act)",
        "MAS Technology Risk Management Guidelines (MAS TRM)",
    ):
        assert framework_label in rendered


def test_render_attestation_includes_prefilled_metadata(tmp_path: Path) -> None:
    report = _build_report(tmp_path, include_suppressed=True)

    rendered = render_attestation(report)

    assert "# Security Scan Attestation" in rendered
    assert f"**Project:** {tmp_path.name}" in rendered
    assert f"**Scan Date:** {report.scan_metadata.timestamp}" in rendered
    assert f"**Tool:** Piranesi v{report.appendix.piranesi_version}" in rendered
    assert "**Scope:** 1 files across TypeScript" in rendered
    assert "- 2 findings detected" in rendered
    assert "- 1 confirmed via exploit verification" in rendered
    assert "- 1 suppressed (with documented rationale)" in rendered
    assert "- 1 with auto-generated patches" in rendered
    assert "This report is not a compliance certification" in rendered
    assert "DISCLAIMER: This analysis is informational only. It is not legal advice." in rendered
    assert "Consult qualified legal counsel for regulatory compliance decisions." in rendered


def _build_report(tmp_path: Path, *, include_suppressed: bool = False) -> PiranesiReport:
    artifacts = fixture_artifacts(tmp_path)
    detected_findings = list(artifacts["detect"].findings)  # type: ignore[attr-defined]
    if include_suppressed:
        detected_findings.append(
            detected_findings[0].model_copy(
                update={
                    "id": "finding-suppressed",
                    "suppressed": True,
                    "suppression_reason": "accepted risk",
                }
            )
        )
    return build_report(
        scan_result=artifacts["scan"],  # type: ignore[arg-type]
        detected_findings=detected_findings,
        confirmed_findings=artifacts["verify"].findings,  # type: ignore[attr-defined]
        legal_assessments=artifacts["legal"].assessments,  # type: ignore[attr-defined]
        patch_results=artifacts["patch"].patches,  # type: ignore[attr-defined]
        target_dir=tmp_path,
        total_llm_cost_usd=0.42,
        duration_s=1.25,
        stage_timings_s={"scan": 0.1, "detect": 0.1, "triage": 0.1},
    )
