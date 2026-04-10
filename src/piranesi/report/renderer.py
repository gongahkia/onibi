from __future__ import annotations

import json
from datetime import UTC, datetime
from pathlib import Path

from jinja2 import Environment, FileSystemLoader
from pydantic import BaseModel, ConfigDict, Field

from piranesi import __version__
from piranesi.models import (
    CandidateFinding,
    ConfirmedFinding,
    LegalAssessment,
    PatchResult,
    RegulatoryObligation,
    ScanMetadata,
    ScanResult,
    SourceLocation,
    TaintStep,
)
from piranesi.report.cwe import cwe_title, extract_cwe_id

_SEVERITY_ORDER = ("critical", "high", "medium", "low")


class CombinedFinding(BaseModel):
    model_config = ConfigDict(extra="forbid")

    finding_id: str
    cwe: str
    title: str
    severity: str
    confidence: float
    verified: bool
    verification_method: str
    source_location: SourceLocation
    sink_location: SourceLocation
    taint_path: list[TaintStep] = Field(default_factory=list)
    exploit_payload: str | None = None
    exploit_constraints: list[str] = Field(default_factory=list)
    reproducer_script: str | None = None
    sandbox_response: dict[str, object] = Field(default_factory=dict)
    regulatory_obligations: list[RegulatoryObligation] = Field(default_factory=list)
    legal_risk_tier: str | None = None
    legal_memo_markdown: str | None = None
    patch_diff: str | None = None
    patch_verified: bool | None = None
    patch_explanation: str | None = None
    related_cves: list[str] = Field(default_factory=list)
    pr_body: str | None = None


class ExecutiveSummary(BaseModel):
    model_config = ConfigDict(extra="forbid")

    findings_detected: int
    findings_confirmed: int
    severity_breakdown: dict[str, int] = Field(default_factory=dict)
    top_regulatory_concerns: list[str] = Field(default_factory=list)
    total_llm_cost_usd: float
    duration_s: float


class ReportAppendix(BaseModel):
    model_config = ConfigDict(extra="forbid")

    generated_at: str
    target: str
    piranesi_version: str
    stage_timings_s: dict[str, float] = Field(default_factory=dict)
    total_llm_cost_usd: float
    duration_s: float


class PiranesiReport(BaseModel):
    model_config = ConfigDict(extra="forbid")

    target: str
    generated_at: str
    scan_metadata: ScanMetadata
    executive_summary: ExecutiveSummary
    findings: list[CombinedFinding] = Field(default_factory=list)
    appendix: ReportAppendix


def build_report(
    *,
    scan_result: ScanResult,
    detected_findings: list[CandidateFinding],
    confirmed_findings: list[ConfirmedFinding],
    legal_assessments: list[LegalAssessment],
    patch_results: list[PatchResult],
    target_dir: Path,
    total_llm_cost_usd: float,
    duration_s: float,
    stage_timings_s: dict[str, float],
) -> PiranesiReport:
    generated_at = _utc_now()
    legal_by_id = {
        assessment.finding.finding.finding.id: assessment for assessment in legal_assessments
    }
    patch_by_id = {patch.finding.finding.finding.id: patch for patch in patch_results}

    findings: list[CombinedFinding] = []
    for confirmed in confirmed_findings:
        candidate = confirmed.finding.finding
        finding_id = candidate.id
        legal = legal_by_id.get(finding_id)
        patch = patch_by_id.get(finding_id)
        finding = CombinedFinding(
            finding_id=finding_id,
            cwe=_extract_cwe_id(candidate.vuln_class),
            title=_finding_title(candidate),
            severity=candidate.severity,
            confidence=candidate.confidence,
            verified=True,
            verification_method="smt+sandbox",
            source_location=candidate.source.location,
            sink_location=candidate.sink.location,
            taint_path=list(candidate.taint_path),
            exploit_payload=confirmed.exploit_payload,
            exploit_constraints=list(confirmed.exploit_constraints),
            reproducer_script=confirmed.reproducer_script,
            sandbox_response=dict(confirmed.sandbox_result.response),
            regulatory_obligations=[] if legal is None else list(legal.obligations),
            legal_risk_tier=None if legal is None else legal.risk_tier,
            legal_memo_markdown=None if legal is None else legal.memo_markdown,
            patch_diff=None if patch is None else patch.patch_diff,
            patch_verified=None if patch is None else patch.patch_verified,
            patch_explanation=None if patch is None else patch.patch_explanation,
            related_cves=list(confirmed.related_cves),
        )
        findings.append(finding)

    report = PiranesiReport(
        target=str(target_dir.resolve(strict=False)),
        generated_at=generated_at,
        scan_metadata=scan_result.metadata,
        executive_summary=ExecutiveSummary(
            findings_detected=len(detected_findings),
            findings_confirmed=len(confirmed_findings),
            severity_breakdown=_severity_breakdown(findings),
            top_regulatory_concerns=_top_regulatory_concerns(legal_assessments),
            total_llm_cost_usd=total_llm_cost_usd,
            duration_s=duration_s,
        ),
        findings=findings,
        appendix=ReportAppendix(
            generated_at=generated_at,
            target=str(target_dir.resolve(strict=False)),
            piranesi_version=__version__,
            stage_timings_s=dict(stage_timings_s),
            total_llm_cost_usd=total_llm_cost_usd,
            duration_s=duration_s,
        ),
    )
    findings_with_bodies = [
        finding.model_copy(update={"pr_body": render_pr_body_for_finding(report, finding)})
        for finding in report.findings
    ]
    return report.model_copy(update={"findings": findings_with_bodies})


def update_report_metrics(
    report: PiranesiReport,
    *,
    total_llm_cost_usd: float,
    duration_s: float,
    stage_timings_s: dict[str, float],
) -> PiranesiReport:
    return report.model_copy(
        update={
            "executive_summary": report.executive_summary.model_copy(
                update={
                    "total_llm_cost_usd": total_llm_cost_usd,
                    "duration_s": duration_s,
                }
            ),
            "appendix": report.appendix.model_copy(
                update={
                    "total_llm_cost_usd": total_llm_cost_usd,
                    "duration_s": duration_s,
                    "stage_timings_s": dict(stage_timings_s),
                }
            ),
        }
    )


def write_report_outputs(
    report: PiranesiReport,
    output_dir: Path,
    *,
    report_format: str = "both",
) -> None:
    output_dir.mkdir(parents=True, exist_ok=True)
    (output_dir / "report.json").write_text(report.model_dump_json(indent=2), encoding="utf-8")
    (output_dir / "report.md").write_text(render_markdown(report), encoding="utf-8")
    (output_dir / "pr_body.md").write_text(render_pr_body(report), encoding="utf-8")
    if report_format.lower() == "sarif":
        from piranesi.report.sarif import generate_sarif

        (output_dir / "report.sarif.json").write_text(
            json.dumps(generate_sarif(report), indent=2),
            encoding="utf-8",
        )


def render_markdown(report: PiranesiReport) -> str:
    env = _template_env()
    template = env.get_template("report.md.j2")
    return template.render(report=report)


def render_pr_body(report: PiranesiReport) -> str:
    rendered = [render_pr_body_for_finding(report, finding) for finding in report.findings]
    return "\n\n---\n\n".join(part for part in rendered if part).strip() + "\n"


def render_pr_body_for_finding(report: PiranesiReport, finding: CombinedFinding) -> str:
    env = _template_env()
    template = env.get_template("pr_body.md.j2")
    return template.render(report=report, finding=finding).strip()


def _template_env() -> Environment:
    templates_dir = Path(__file__).resolve().parents[1] / "templates"
    env = Environment(
        loader=FileSystemLoader(str(templates_dir)),
        autoescape=False,  # noqa: S701
        trim_blocks=True,
        lstrip_blocks=True,
    )
    return env


def _extract_cwe_id(vuln_class: str) -> str:
    return extract_cwe_id(vuln_class)


def _finding_title(candidate: CandidateFinding) -> str:
    cwe = _extract_cwe_id(candidate.vuln_class)
    return cwe_title(cwe, fallback=candidate.vuln_class)


def _severity_breakdown(findings: list[CombinedFinding]) -> dict[str, int]:
    counts = dict.fromkeys(_SEVERITY_ORDER, 0)
    for finding in findings:
        severity = finding.severity.lower()
        counts[severity] = counts.get(severity, 0) + 1
    return {severity: count for severity, count in counts.items() if count > 0}


def _top_regulatory_concerns(assessments: list[LegalAssessment]) -> list[str]:
    counts: dict[tuple[str, str], int] = {}
    deadlines: dict[tuple[str, str], str | None] = {}
    for assessment in assessments:
        for obligation in assessment.obligations:
            key = (obligation.framework, obligation.section)
            counts[key] = counts.get(key, 0) + 1
            deadlines[key] = obligation.notification_timeline
    ordered = sorted(counts.items(), key=lambda item: (-item[1], item[0][0], item[0][1]))
    concerns: list[str] = []
    for (framework, section), count in ordered[:3]:
        deadline = deadlines[(framework, section)]
        if deadline:
            concerns.append(f"{framework} {section} ({count} findings, deadline: {deadline})")
        else:
            concerns.append(f"{framework} {section} ({count} findings)")
    return concerns


def _utc_now() -> str:
    return datetime.now(UTC).isoformat(timespec="seconds").replace("+00:00", "Z")


__all__ = [
    "CombinedFinding",
    "ExecutiveSummary",
    "PiranesiReport",
    "ReportAppendix",
    "build_report",
    "render_markdown",
    "render_pr_body",
    "render_pr_body_for_finding",
    "update_report_metrics",
    "write_report_outputs",
]
