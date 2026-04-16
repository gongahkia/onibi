from __future__ import annotations

import json
import logging
from datetime import UTC, datetime
from hashlib import sha256
from pathlib import Path
from typing import Literal

from jinja2 import Environment, FileSystemLoader
from pydantic import BaseModel, ConfigDict, Field

from piranesi import __version__
from piranesi.models import (
    CandidateFinding,
    ConfirmedFinding,
    LegalAssessment,
    PatchResult,
    QueryQualityMetrics,
    ReachabilityResult,
    RegulatoryObligation,
    ScanMetadata,
    ScannedFunction,
    ScanResult,
    SourceLocation,
    TaintStep,
    TriagedFinding,
)
from piranesi.report.cwe import cwe_title, extract_cwe_id

_logger = logging.getLogger(__name__)
_SEVERITY_ORDER = ("critical", "high", "medium", "low", "informational")
EvidenceStatus = Literal[
    "confirmed",
    "triaged_active_candidate",
    "static_candidate",
    "unreachable_candidate",
    "suppressed",
]
_EVIDENCE_STATUS_ORDER: tuple[EvidenceStatus, ...] = (
    "confirmed",
    "triaged_active_candidate",
    "static_candidate",
    "unreachable_candidate",
    "suppressed",
)
_EVIDENCE_STATUS_LABELS: dict[EvidenceStatus, str] = {
    "confirmed": "Dynamically verified issue",
    "triaged_active_candidate": "LLM-triaged active candidate",
    "static_candidate": "Static candidate",
    "unreachable_candidate": "Unreachable candidate",
    "suppressed": "Suppressed finding",
}
_CONFIDENCE_COMPONENT_WEIGHTS = {
    "static_reachability": 0.20,
    "source_quality": 0.14,
    "sink_quality": 0.14,
    "sanitizer_signal": 0.20,
    "triage_signal": 0.12,
    "verification_signal": 0.15,
    "suppression_signal": 0.05,
}


class MatchedSpec(BaseModel):
    model_config = ConfigDict(extra="forbid")

    spec_id: str | None = None
    name: str | None = None
    category: str | None = None
    cwe: str | None = None
    severity: str | None = None
    is_custom: bool | None = None


class SanitizerExplanation(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    effectiveness: str | None = None
    observed_on_path: bool = False


class PropagationPathSummary(BaseModel):
    model_config = ConfigDict(extra="forbid")

    path_node_count: int = 0
    path_edge_count: int = 0
    operation_sequence: list[str] = Field(default_factory=list)
    source_to_sink: str
    includes_sanitizer_steps: bool = False


class VerificationState(BaseModel):
    model_config = ConfigDict(extra="forbid")

    state: str
    verified: bool = False
    verification_method: str | None = None
    triage_verdict: str | None = None
    triage_mode: str | None = None
    suppression_reason: str | None = None


class ConfidenceComponent(BaseModel):
    model_config = ConfigDict(extra="forbid")

    score: float
    weight: float
    weighted_score: float
    rationale: str


class ConfidenceBreakdown(BaseModel):
    model_config = ConfigDict(extra="forbid")

    model_version: str = "v1"
    reported_confidence: float
    static_reachability: ConfidenceComponent
    source_quality: ConfidenceComponent
    sink_quality: ConfidenceComponent
    sanitizer_signal: ConfidenceComponent
    triage_signal: ConfidenceComponent
    verification_signal: ConfidenceComponent
    suppression_signal: ConfidenceComponent
    contextual_confidence: float
    final_confidence: float
    formula: str = (
        "weighted sum of component scores; final_confidence preserves pipeline confidence"
    )


class FindingExplanation(BaseModel):
    model_config = ConfigDict(extra="forbid")

    matched_source_spec: MatchedSpec
    matched_sink_spec: MatchedSpec
    sanitizers_considered: list[SanitizerExplanation] = Field(default_factory=list)
    sanitizers_observed: list[str] = Field(default_factory=list)
    propagation_path: PropagationPathSummary
    verification_state: VerificationState
    confidence: ConfidenceBreakdown
    severity_basis: str


class CombinedFinding(BaseModel):
    model_config = ConfigDict(extra="forbid")

    finding_id: str
    evidence_status: EvidenceStatus = "confirmed"
    explanation: FindingExplanation | None = None
    cwe: str
    title: str
    severity: str
    confidence: float
    metadata: dict[str, object] = Field(default_factory=dict)
    verified: bool
    verification_method: str
    taint_source: str
    taint_sink: str
    source_location: SourceLocation
    sink_location: SourceLocation
    taint_path: list[TaintStep] = Field(default_factory=list)
    exploit_payload: str | None = None
    exploit_constraints: list[str] = Field(default_factory=list)
    verification_template_id: str | None = None
    verification_template_reason: str | None = None
    verification_template_risk_level: str | None = None
    verification_expected_evidence: list[str] = Field(default_factory=list)
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
    package_name: str | None = None
    cross_package: bool = False
    source_package: str | None = None
    sink_package: str | None = None
    cluster_id: str | None = None
    cluster_size: int = 1
    cluster_representative: bool = True


class CandidateReportFinding(BaseModel):
    model_config = ConfigDict(extra="forbid")

    finding_id: str
    evidence_status: EvidenceStatus = "static_candidate"
    explanation: FindingExplanation | None = None
    cwe: str
    title: str
    severity: str
    original_severity: str | None = None
    confidence: float
    metadata: dict[str, object] = Field(default_factory=dict)
    taint_source: str
    taint_sink: str
    source_location: SourceLocation
    sink_location: SourceLocation
    reachability: str = "reachable"
    triage_verdict: str | None = None
    triage_mode: str | None = None
    source_function_id: str | None = None
    package_name: str | None = None
    cross_package: bool = False
    source_package: str | None = None
    sink_package: str | None = None
    cluster_id: str | None = None
    cluster_size: int = 1
    cluster_representative: bool = True


class SuppressedFinding(BaseModel):
    model_config = ConfigDict(extra="forbid")

    finding_id: str
    evidence_status: EvidenceStatus = "suppressed"
    explanation: FindingExplanation | None = None
    cwe: str
    title: str
    severity: str
    confidence: float
    metadata: dict[str, object] = Field(default_factory=dict)
    taint_source: str
    taint_sink: str
    source_location: SourceLocation
    sink_location: SourceLocation
    reachability: str = "reachable"
    suppression_reason: str | None = None
    package_name: str | None = None
    cross_package: bool = False
    source_package: str | None = None
    sink_package: str | None = None


class FindingCluster(BaseModel):
    model_config = ConfigDict(extra="forbid")

    cluster_id: str
    title: str
    cwe: str
    severity: str
    representative_finding_id: str
    finding_ids: list[str] = Field(default_factory=list)
    count: int
    taint_sink: str
    sink_location: SourceLocation
    source_locations: list[SourceLocation] = Field(default_factory=list)
    package_name: str | None = None
    cross_package: bool = False
    source_package: str | None = None
    sink_package: str | None = None


class ExecutiveSummary(BaseModel):
    model_config = ConfigDict(extra="forbid")

    findings_detected: int
    suppressed_findings: int = 0
    findings_confirmed: int
    status_breakdown: dict[str, int] = Field(default_factory=dict)
    reachable_findings: int = 0
    unreachable_findings: int = 0
    finding_clusters: int = 0
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
    files_scanned: list[str] = Field(default_factory=list)
    status_legend: dict[str, str] = Field(default_factory=lambda: dict(_EVIDENCE_STATUS_LABELS))
    scan_metadata: ScanMetadata
    executive_summary: ExecutiveSummary
    active_findings: list[CandidateReportFinding] = Field(default_factory=list)
    unreachable_findings: list[CandidateReportFinding] = Field(default_factory=list)
    finding_clusters: list[FindingCluster] = Field(default_factory=list)
    findings: list[CombinedFinding] = Field(default_factory=list)
    package_findings: dict[str, list[CombinedFinding]] = Field(default_factory=dict)
    cross_package_findings: list[CombinedFinding] = Field(default_factory=list)
    suppressed_findings: list[SuppressedFinding] = Field(default_factory=list)
    suppressed_findings_by_package: dict[str, list[SuppressedFinding]] = Field(default_factory=dict)
    query_quality: QueryQualityMetrics | None = None
    dead_code_functions: list[ScannedFunction] = Field(default_factory=list)
    dead_code_by_file: dict[str, list[ScannedFunction]] = Field(default_factory=dict)
    appendix: ReportAppendix


def build_report(
    *,
    scan_result: ScanResult,
    detected_findings: list[CandidateFinding],
    triaged_findings: list[TriagedFinding] | None = None,
    confirmed_findings: list[ConfirmedFinding],
    legal_assessments: list[LegalAssessment],
    patch_results: list[PatchResult],
    target_dir: Path,
    total_llm_cost_usd: float,
    duration_s: float,
    stage_timings_s: dict[str, float],
    reachability: ReachabilityResult | None = None,
    include_unreachable: bool = False,
    dead_code_report: bool = False,
) -> PiranesiReport:
    generated_at = _utc_now()
    triage_by_id = _triage_lookup(triaged_findings or [])
    legal_by_id = {
        assessment.finding.finding.finding.id: assessment for assessment in legal_assessments
    }
    patch_by_id = {patch.finding.finding.finding.id: patch for patch in patch_results}
    unsuppressed_candidates = [
        candidate for candidate in detected_findings if not candidate.suppressed
    ]
    reachable_candidates = [
        candidate
        for candidate in unsuppressed_candidates
        if candidate.reachability == "reachable"
    ]
    unreachable_candidates = [
        candidate
        for candidate in unsuppressed_candidates
        if candidate.reachability != "reachable"
    ]
    active_candidate_pool = (
        list(unsuppressed_candidates) if include_unreachable else reachable_candidates
    )
    active_candidates = [
        candidate
        for candidate in active_candidate_pool
        if (
            (triaged := triage_by_id.get(candidate.id)) is None
            or triaged.triage_verdict != "false_positive"
        )
    ]
    suppressed_findings: list[SuppressedFinding] = []
    for candidate in detected_findings:
        if not candidate.suppressed:
            continue
        triaged = triage_by_id.get(candidate.id)
        suppressed_findings.append(
            SuppressedFinding(
                finding_id=candidate.id,
                cwe=_extract_cwe_id(candidate.vuln_class),
                title=_finding_title(candidate),
                severity=candidate.severity,
                confidence=candidate.confidence,
                metadata=dict(candidate.metadata),
                taint_source=candidate.source.source_type,
                taint_sink=candidate.sink.api_name,
                source_location=candidate.source.location,
                sink_location=candidate.sink.location,
                reachability=candidate.reachability,
                suppression_reason=candidate.suppression_reason,
                package_name=_package_name(candidate),
                cross_package=bool(candidate.metadata.get("cross_package")),
                source_package=_metadata_string(candidate.metadata.get("source_package")),
                sink_package=_metadata_string(candidate.metadata.get("sink_package")),
                explanation=_build_finding_explanation(
                    candidate,
                    evidence_status="suppressed",
                    triaged=triaged,
                    confirmed=None,
                    verification_method=None,
                    verified=False,
                    suppression_reason=candidate.suppression_reason,
                ),
            )
        )
    active_report_findings = [
        _candidate_report_finding(
            candidate,
            triaged=triage_by_id.get(candidate.id),
            cluster_by_id={},
        )
        for candidate in active_candidates
    ]
    unreachable_report_findings = [
        _candidate_report_finding(
            candidate,
            triaged=triage_by_id.get(candidate.id),
        )
        for candidate in unreachable_candidates
    ]
    active_clusters = _cluster_candidate_findings(active_candidates)
    active_cluster_by_id = _cluster_lookup(active_clusters)
    active_report_findings = [
        finding.model_copy(update=_cluster_fields(finding.finding_id, active_cluster_by_id))
        for finding in active_report_findings
    ]
    unreachable_report_findings = [
        finding.model_copy(update=_cluster_fields(finding.finding_id, active_cluster_by_id))
        for finding in unreachable_report_findings
    ]
    status_breakdown = _status_breakdown(
        active_findings=active_report_findings,
        unreachable_findings=unreachable_report_findings,
        confirmed_count=len(confirmed_findings),
        suppressed_count=len(suppressed_findings),
    )

    findings: list[CombinedFinding] = []
    for confirmed in confirmed_findings:
        candidate = confirmed.finding.finding
        finding_id = candidate.id
        legal = legal_by_id.get(finding_id)
        patch = patch_by_id.get(finding_id)
        finding = CombinedFinding(
            finding_id=finding_id,
            evidence_status="confirmed",
            explanation=_build_finding_explanation(
                candidate,
                evidence_status="confirmed",
                triaged=confirmed.finding,
                confirmed=confirmed,
                verification_method="smt+sandbox",
                verified=True,
            ),
            cwe=_extract_cwe_id(candidate.vuln_class),
            title=_finding_title(candidate),
            severity=candidate.severity,
            confidence=candidate.confidence,
            metadata=dict(candidate.metadata),
            verified=True,
            verification_method="smt+sandbox",
            taint_source=candidate.source.source_type,
            taint_sink=candidate.sink.api_name,
            source_location=candidate.source.location,
            sink_location=candidate.sink.location,
            taint_path=list(candidate.taint_path),
            exploit_payload=confirmed.exploit_payload,
            exploit_constraints=list(confirmed.exploit_constraints),
            verification_template_id=confirmed.verification_template_id,
            verification_template_reason=confirmed.verification_template_reason,
            verification_template_risk_level=confirmed.verification_template_risk_level,
            verification_expected_evidence=list(confirmed.verification_expected_evidence),
            reproducer_script=confirmed.reproducer_script,
            sandbox_response=dict(confirmed.sandbox_result.response),
            regulatory_obligations=[] if legal is None else list(legal.obligations),
            legal_risk_tier=None if legal is None else legal.risk_tier,
            legal_memo_markdown=None if legal is None else legal.memo_markdown,
            patch_diff=None if patch is None else patch.patch_diff,
            patch_verified=None if patch is None else patch.patch_verified,
            patch_explanation=None if patch is None else patch.patch_explanation,
            related_cves=list(confirmed.related_cves),
            package_name=_package_name(candidate),
            cross_package=bool(candidate.metadata.get("cross_package")),
            source_package=_metadata_string(candidate.metadata.get("source_package")),
            sink_package=_metadata_string(candidate.metadata.get("sink_package")),
            **_cluster_fields(candidate.id, active_cluster_by_id),
        )
        findings.append(finding)

    report = PiranesiReport(
        target=str(target_dir.resolve(strict=False)),
        generated_at=generated_at,
        files_scanned=list(scan_result.files_scanned),
        scan_metadata=scan_result.metadata,
        executive_summary=ExecutiveSummary(
            findings_detected=len(detected_findings),
            suppressed_findings=len(suppressed_findings),
            findings_confirmed=len(confirmed_findings),
            status_breakdown=status_breakdown,
            reachable_findings=len(reachable_candidates),
            unreachable_findings=len(unreachable_candidates),
            finding_clusters=len(active_clusters),
            severity_breakdown=_candidate_severity_breakdown(active_candidates),
            top_regulatory_concerns=_top_regulatory_concerns(legal_assessments),
            total_llm_cost_usd=total_llm_cost_usd,
            duration_s=duration_s,
        ),
        active_findings=active_report_findings,
        unreachable_findings=unreachable_report_findings,
        finding_clusters=active_clusters,
        findings=findings,
        package_findings=_group_report_findings_by_package(findings),
        cross_package_findings=[finding for finding in findings if finding.cross_package],
        suppressed_findings=suppressed_findings,
        suppressed_findings_by_package=_group_suppressed_findings_by_package(suppressed_findings),
        query_quality=scan_result.query_quality,
        dead_code_functions=(
            []
            if not dead_code_report or reachability is None
            else list(reachability.dead_code_functions)
        ),
        dead_code_by_file=(
            {}
            if not dead_code_report or reachability is None
            else _group_dead_code_by_file(reachability.dead_code_functions)
        ),
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
    format_name = report_format.lower()
    if format_name == "sarif":
        from piranesi.report.sarif import generate_sarif

        (output_dir / "report.sarif.json").write_text(
            json.dumps(generate_sarif(report), indent=2),
            encoding="utf-8",
        )
    if format_name == "junit":
        from piranesi.report.junit import generate_junit_xml

        (output_dir / "report.junit.xml").write_text(
            generate_junit_xml(report),
            encoding="utf-8",
        )
    if format_name == "csv":
        from piranesi.report.csv import generate_csv

        (output_dir / "findings.csv").write_text(generate_csv(report), encoding="utf-8")
    from piranesi.plugin import discover_reporter_plugins

    for reporter in discover_reporter_plugins():
        try:
            reporter.render(report, output_dir)
            _logger.info("reporter plugin '%s' wrote to %s", reporter.name(), output_dir)
        except Exception:
            _logger.warning("reporter plugin '%s' failed", reporter.name(), exc_info=True)


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


def _candidate_report_finding(
    candidate: CandidateFinding,
    *,
    triaged: TriagedFinding | None = None,
    cluster_by_id: dict[str, FindingCluster] | None = None,
) -> CandidateReportFinding:
    original_severity = candidate.metadata.get("reachability_original_severity")
    evidence_status = _candidate_evidence_status(candidate, triaged=triaged)
    return CandidateReportFinding(
        finding_id=candidate.id,
        evidence_status=evidence_status,
        explanation=_build_finding_explanation(
            candidate,
            evidence_status=evidence_status,
            triaged=triaged,
            confirmed=None,
            verification_method=None,
            verified=False,
        ),
        cwe=_extract_cwe_id(candidate.vuln_class),
        title=_finding_title(candidate),
        severity=candidate.severity,
        original_severity=original_severity if isinstance(original_severity, str) else None,
        confidence=candidate.confidence,
        metadata=dict(candidate.metadata),
        taint_source=candidate.source.source_type,
        taint_sink=candidate.sink.api_name,
        source_location=candidate.source.location,
        sink_location=candidate.sink.location,
        reachability=candidate.reachability,
        triage_verdict=None if triaged is None else triaged.triage_verdict,
        triage_mode=None if triaged is None else triaged.triage_mode,
        source_function_id=_metadata_string(candidate.metadata.get("source_function_id")),
        package_name=_package_name(candidate),
        cross_package=bool(candidate.metadata.get("cross_package")),
        source_package=_metadata_string(candidate.metadata.get("source_package")),
        sink_package=_metadata_string(candidate.metadata.get("sink_package")),
        **_cluster_fields(candidate.id, cluster_by_id or {}),
    )


def _candidate_severity_breakdown(findings: list[CandidateFinding]) -> dict[str, int]:
    counts = dict.fromkeys(_SEVERITY_ORDER, 0)
    for finding in findings:
        if finding.suppressed:
            continue
        severity = finding.severity.lower()
        counts[severity] = counts.get(severity, 0) + 1
    return {severity: count for severity, count in counts.items() if count > 0}


def _candidate_evidence_status(
    candidate: CandidateFinding,
    *,
    triaged: TriagedFinding | None,
) -> EvidenceStatus:
    if candidate.reachability != "reachable":
        return "unreachable_candidate"
    if triaged is None:
        return "static_candidate"
    if triaged.triage_mode == "deterministic":
        return "static_candidate"
    if triaged.triage_verdict == "false_positive":
        return "static_candidate"
    return "triaged_active_candidate"


def _triage_lookup(findings: list[TriagedFinding]) -> dict[str, TriagedFinding]:
    return {finding.finding.id: finding for finding in findings}


def _status_breakdown(
    *,
    active_findings: list[CandidateReportFinding],
    unreachable_findings: list[CandidateReportFinding],
    confirmed_count: int,
    suppressed_count: int,
) -> dict[str, int]:
    counts: dict[str, int] = dict.fromkeys(_EVIDENCE_STATUS_ORDER, 0)
    counts["confirmed"] = confirmed_count
    counts["suppressed"] = suppressed_count
    statuses_by_finding: dict[str, str] = {}
    for finding in active_findings:
        statuses_by_finding[finding.finding_id] = finding.evidence_status
    for finding in unreachable_findings:
        statuses_by_finding[finding.finding_id] = finding.evidence_status
    for status in statuses_by_finding.values():
        counts[status] += 1
    return {status: count for status, count in counts.items() if count > 0}


def _build_finding_explanation(
    candidate: CandidateFinding,
    *,
    evidence_status: EvidenceStatus,
    triaged: TriagedFinding | None,
    confirmed: ConfirmedFinding | None,
    verification_method: str | None,
    verified: bool,
    suppression_reason: str | None = None,
) -> FindingExplanation:
    sanitizers_considered, sanitizers_observed = _build_sanitizer_explanations(candidate)
    return FindingExplanation(
        matched_source_spec=_matched_source_spec(candidate),
        matched_sink_spec=_matched_sink_spec(candidate),
        sanitizers_considered=sanitizers_considered,
        sanitizers_observed=sanitizers_observed,
        propagation_path=_propagation_path_summary(candidate),
        verification_state=_verification_state(
            evidence_status=evidence_status,
            triaged=triaged,
            verification_method=verification_method,
            verified=verified,
            suppression_reason=suppression_reason,
        ),
        confidence=_confidence_breakdown(
            candidate,
            evidence_status=evidence_status,
            triaged=triaged,
            confirmed=confirmed,
            verification_method=verification_method,
            verified=verified,
            suppression_reason=suppression_reason,
            sanitizers_considered=sanitizers_considered,
        ),
        severity_basis=_severity_basis(candidate),
    )


def _matched_source_spec(candidate: CandidateFinding) -> MatchedSpec:
    name = _metadata_string(candidate.metadata.get("source_spec_name"))
    return MatchedSpec(
        spec_id=None if name is None else f"source:{name}",
        name=name,
        category=_metadata_string(candidate.metadata.get("source_spec_category")),
        is_custom=_metadata_bool(candidate.metadata.get("source_spec_custom")),
    )


def _matched_sink_spec(candidate: CandidateFinding) -> MatchedSpec:
    name = _metadata_string(candidate.metadata.get("sink_spec_name"))
    return MatchedSpec(
        spec_id=None if name is None else f"sink:{name}",
        name=name,
        category=_metadata_string(candidate.metadata.get("sink_spec_category")),
        cwe=_metadata_string(candidate.metadata.get("sink_spec_cwe")),
        severity=candidate.severity,
        is_custom=_metadata_bool(candidate.metadata.get("sink_spec_custom")),
    )


def _build_sanitizer_explanations(
    candidate: CandidateFinding,
) -> tuple[list[SanitizerExplanation], list[str]]:
    observed = set(_observed_sanitizers(candidate))
    observed.update(_metadata_strings(candidate.metadata.get("effective_sanitizers")))
    observed.update(_metadata_strings(candidate.metadata.get("partial_sanitizers")))
    observed.update(_metadata_strings(candidate.metadata.get("ineffective_sanitizers")))

    considered: dict[str, str | None] = {}
    effectiveness = candidate.metadata.get("sanitizer_effectiveness")
    if isinstance(effectiveness, dict):
        for raw_name, raw_effectiveness in effectiveness.items():
            name = _metadata_string(raw_name)
            if name is None:
                continue
            considered[name] = _metadata_string(raw_effectiveness)

    for name in observed:
        considered.setdefault(name, None)

    ordered = sorted(considered.items(), key=lambda item: item[0])
    return (
        [
            SanitizerExplanation(
                name=name,
                effectiveness=effectiveness,
                observed_on_path=name in observed,
            )
            for name, effectiveness in ordered
        ],
        sorted(observed),
    )


def _observed_sanitizers(candidate: CandidateFinding) -> tuple[str, ...]:
    observed = [
        step.sanitizer_applied
        for step in candidate.taint_path
        if step.sanitizer_applied is not None and step.sanitizer_applied.strip()
    ]
    deduped = tuple(dict.fromkeys(item.strip() for item in observed))
    return deduped


def _propagation_path_summary(candidate: CandidateFinding) -> PropagationPathSummary:
    operations = [
        step.operation.strip()
        for step in candidate.taint_path
        if step.operation and step.operation.strip()
    ]
    source = candidate.source.location
    sink = candidate.sink.location
    return PropagationPathSummary(
        path_node_count=max(2, len(candidate.taint_path) + 2),
        path_edge_count=max(1, len(candidate.taint_path) + 1),
        operation_sequence=operations,
        source_to_sink=f"{source.file}:{source.line} -> {sink.file}:{sink.line}",
        includes_sanitizer_steps=any(step.sanitizer_applied for step in candidate.taint_path),
    )


def _verification_state(
    *,
    evidence_status: EvidenceStatus,
    triaged: TriagedFinding | None,
    verification_method: str | None,
    verified: bool,
    suppression_reason: str | None,
) -> VerificationState:
    if evidence_status == "confirmed":
        state = "verified_confirmed"
    elif evidence_status == "suppressed":
        state = "suppressed"
    elif evidence_status == "unreachable_candidate":
        state = "unreachable_candidate"
    else:
        state = "candidate"
    return VerificationState(
        state=state,
        verified=verified,
        verification_method=verification_method,
        triage_verdict=None if triaged is None else triaged.triage_verdict,
        triage_mode=None if triaged is None else triaged.triage_mode,
        suppression_reason=suppression_reason,
    )


def _confidence_breakdown(
    candidate: CandidateFinding,
    *,
    evidence_status: EvidenceStatus,
    triaged: TriagedFinding | None,
    confirmed: ConfirmedFinding | None,
    verification_method: str | None,
    verified: bool,
    suppression_reason: str | None,
    sanitizers_considered: list[SanitizerExplanation],
) -> ConfidenceBreakdown:
    source_match = _matched_source_spec(candidate)
    sink_match = _matched_sink_spec(candidate)

    reachability_score, reachability_reason = _reachability_component(candidate)
    source_score, source_reason = _source_quality_component(source_match)
    sink_score, sink_reason = _sink_quality_component(sink_match)
    sanitizer_score, sanitizer_reason = _sanitizer_component(
        candidate,
        sanitizers_considered=sanitizers_considered,
    )
    triage_score, triage_reason = _triage_component(triaged, evidence_status=evidence_status)
    verification_score, verification_reason = _verification_component(
        evidence_status=evidence_status,
        verification_method=verification_method,
        verified=verified,
        confirmed=confirmed,
    )
    suppression_score, suppression_reason_text = _suppression_component(
        evidence_status=evidence_status,
        suppression_reason=suppression_reason,
    )

    static_reachability = _confidence_component(
        score=reachability_score,
        weight=_CONFIDENCE_COMPONENT_WEIGHTS["static_reachability"],
        rationale=reachability_reason,
    )
    source_quality = _confidence_component(
        score=source_score,
        weight=_CONFIDENCE_COMPONENT_WEIGHTS["source_quality"],
        rationale=source_reason,
    )
    sink_quality = _confidence_component(
        score=sink_score,
        weight=_CONFIDENCE_COMPONENT_WEIGHTS["sink_quality"],
        rationale=sink_reason,
    )
    sanitizer_signal = _confidence_component(
        score=sanitizer_score,
        weight=_CONFIDENCE_COMPONENT_WEIGHTS["sanitizer_signal"],
        rationale=sanitizer_reason,
    )
    triage_signal = _confidence_component(
        score=triage_score,
        weight=_CONFIDENCE_COMPONENT_WEIGHTS["triage_signal"],
        rationale=triage_reason,
    )
    verification_signal = _confidence_component(
        score=verification_score,
        weight=_CONFIDENCE_COMPONENT_WEIGHTS["verification_signal"],
        rationale=verification_reason,
    )
    suppression_signal = _confidence_component(
        score=suppression_score,
        weight=_CONFIDENCE_COMPONENT_WEIGHTS["suppression_signal"],
        rationale=suppression_reason_text,
    )
    contextual_confidence = round(
        static_reachability.weighted_score
        + source_quality.weighted_score
        + sink_quality.weighted_score
        + sanitizer_signal.weighted_score
        + triage_signal.weighted_score
        + verification_signal.weighted_score
        + suppression_signal.weighted_score,
        3,
    )
    return ConfidenceBreakdown(
        reported_confidence=round(candidate.confidence, 3),
        static_reachability=static_reachability,
        source_quality=source_quality,
        sink_quality=sink_quality,
        sanitizer_signal=sanitizer_signal,
        triage_signal=triage_signal,
        verification_signal=verification_signal,
        suppression_signal=suppression_signal,
        contextual_confidence=contextual_confidence,
        final_confidence=round(candidate.confidence, 3),
    )


def _confidence_component(*, score: float, weight: float, rationale: str) -> ConfidenceComponent:
    normalized_score = max(0.0, min(1.0, score))
    weighted = round(normalized_score * weight, 3)
    return ConfidenceComponent(
        score=round(normalized_score, 3),
        weight=round(weight, 3),
        weighted_score=weighted,
        rationale=rationale,
    )


def _reachability_component(candidate: CandidateFinding) -> tuple[float, str]:
    if candidate.reachability == "reachable":
        return 1.0, "flow is reachable from known entry points"
    return 0.35, "flow is currently unreachable from known entry points"


def _source_quality_component(source_match: MatchedSpec) -> tuple[float, str]:
    if source_match.name is None:
        return 0.6, "source matched but no concrete source spec metadata was recorded"
    if source_match.is_custom is True:
        return 0.75, "source comes from a custom source spec"
    return 1.0, f"source matched built-in spec '{source_match.name}'"


def _sink_quality_component(sink_match: MatchedSpec) -> tuple[float, str]:
    if sink_match.name is None:
        return 0.65, "sink matched but no concrete sink spec metadata was recorded"
    if sink_match.is_custom is True:
        return 0.75, "sink comes from a custom sink spec"
    if sink_match.cwe is None:
        return 0.85, f"sink spec '{sink_match.name}' has no explicit CWE tag"
    return 1.0, f"sink matched built-in spec '{sink_match.name}'"


def _sanitizer_component(
    candidate: CandidateFinding,
    *,
    sanitizers_considered: list[SanitizerExplanation],
) -> tuple[float, str]:
    if _metadata_bool(candidate.metadata.get("sanitizer_bypassed")) is True:
        patterns = _metadata_strings(candidate.metadata.get("sanitizer_bypass_patterns"))
        if patterns:
            return 1.0, f"sanitizer bypass patterns detected: {', '.join(patterns)}"
        return 1.0, "sanitizer bypass was detected on the path"

    effective = _metadata_strings(candidate.metadata.get("effective_sanitizers"))
    if effective:
        return 0.05, f"effective sanitizers observed: {', '.join(effective)}"

    partial = _metadata_strings(candidate.metadata.get("partial_sanitizers"))
    if partial:
        return 0.6, f"partial sanitizers observed: {', '.join(partial)}"

    ineffective = _metadata_strings(candidate.metadata.get("ineffective_sanitizers"))
    if ineffective:
        return 0.8, f"ineffective sanitizers observed: {', '.join(ineffective)}"

    if sanitizers_considered:
        names = ", ".join(sanitizer.name for sanitizer in sanitizers_considered)
        return 0.85, f"sanitizers considered on path: {names}"
    return 1.0, "no sanitizer signals reduced confidence"


def _triage_component(
    triaged: TriagedFinding | None,
    *,
    evidence_status: EvidenceStatus,
) -> tuple[float, str]:
    if triaged is None:
        if evidence_status == "static_candidate":
            return 0.6, "finding has no triage verdict and remains static-only"
        return 0.7, "triage metadata unavailable for this finding"

    verdict = triaged.triage_verdict
    mode = triaged.triage_mode
    if verdict == "false_positive":
        return 0.1, "triage marked the finding as false positive"
    if mode == "deterministic":
        return 0.75, "deterministic triage preserved the static finding"
    if mode == "ml_prefilter":
        return 0.65, "ML prefilter contributed to triage routing"
    return 1.0, f"triage retained finding as {verdict}"


def _verification_component(
    *,
    evidence_status: EvidenceStatus,
    verification_method: str | None,
    verified: bool,
    confirmed: ConfirmedFinding | None,
) -> tuple[float, str]:
    if evidence_status == "confirmed" and verified:
        if confirmed is not None and confirmed.sandbox_result.confirmed:
            return (
                1.0,
                f"dynamic verification confirmed via {verification_method or 'verify stage'}",
            )
        return (
            0.95,
            f"verification evidence attached via {verification_method or 'verify stage'}",
        )
    if evidence_status == "unreachable_candidate":
        return 0.2, "finding is unreachable and has no dynamic verification"
    if evidence_status == "suppressed":
        return 0.3, "finding is suppressed and not dynamically verified"
    return 0.5, "finding is a candidate without dynamic verification evidence"


def _suppression_component(
    *,
    evidence_status: EvidenceStatus,
    suppression_reason: str | None,
) -> tuple[float, str]:
    if evidence_status != "suppressed":
        return 1.0, "finding is not suppressed"
    if suppression_reason:
        return 0.0, f"finding suppressed: {suppression_reason}"
    return 0.0, "finding suppressed without explicit reason"


def _severity_basis(candidate: CandidateFinding) -> str:
    sink_spec_name = _metadata_string(candidate.metadata.get("sink_spec_name"))
    sink_spec_cwe = _metadata_string(candidate.metadata.get("sink_spec_cwe"))
    if sink_spec_name is not None:
        if sink_spec_cwe is not None:
            return (
                f"severity '{candidate.severity}' from sink spec "
                f"'{sink_spec_name}' mapped to {sink_spec_cwe}"
            )
        return f"severity '{candidate.severity}' from sink spec '{sink_spec_name}'"
    cwe = _extract_cwe_id(candidate.vuln_class)
    return f"severity '{candidate.severity}' inferred from vulnerability class {cwe}"


def _cluster_candidate_findings(candidates: list[CandidateFinding]) -> list[FindingCluster]:
    grouped: dict[tuple[object, ...], list[CandidateFinding]] = {}
    for candidate in candidates:
        grouped.setdefault(_cluster_key(candidate), []).append(candidate)

    clusters: list[FindingCluster] = []
    for key, cluster_findings in grouped.items():
        ordered = sorted(
            cluster_findings,
            key=lambda finding: (
                _severity_sort_key(finding.severity),
                -finding.confidence,
                finding.sink.location.file,
                finding.sink.location.line,
                finding.id,
            ),
        )
        representative = ordered[0]
        cluster_id = _cluster_id(key)
        clusters.append(
            FindingCluster(
                cluster_id=cluster_id,
                title=_finding_title(representative),
                cwe=_extract_cwe_id(representative.vuln_class),
                severity=_max_severity(ordered),
                representative_finding_id=representative.id,
                finding_ids=[finding.id for finding in ordered],
                count=len(ordered),
                taint_sink=representative.sink.api_name,
                sink_location=representative.sink.location,
                source_locations=_dedupe_locations(
                    [finding.source.location for finding in ordered]
                ),
                package_name=_package_name(representative),
                cross_package=bool(representative.metadata.get("cross_package")),
                source_package=_metadata_string(representative.metadata.get("source_package")),
                sink_package=_metadata_string(representative.metadata.get("sink_package")),
            )
        )
    return sorted(
        clusters,
        key=lambda cluster: (
            _severity_sort_key(cluster.severity),
            cluster.sink_location.file,
            cluster.sink_location.line,
            cluster.cluster_id,
        ),
    )


def _cluster_lookup(clusters: list[FindingCluster]) -> dict[str, FindingCluster]:
    return {finding_id: cluster for cluster in clusters for finding_id in cluster.finding_ids}


def _cluster_fields(
    finding_id: str,
    cluster_by_id: dict[str, FindingCluster],
) -> dict[str, object]:
    cluster = cluster_by_id.get(finding_id)
    if cluster is None:
        return {}
    return {
        "cluster_id": cluster.cluster_id,
        "cluster_size": cluster.count,
        "cluster_representative": finding_id == cluster.representative_finding_id,
    }


def _cluster_key(candidate: CandidateFinding) -> tuple[object, ...]:
    return (
        _extract_cwe_id(candidate.vuln_class),
        _normalize_report_path(candidate.sink.location.file),
        candidate.sink.location.line,
        candidate.sink.api_name,
        _package_name(candidate),
        bool(candidate.metadata.get("cross_package")),
        _metadata_string(candidate.metadata.get("source_package")),
        _metadata_string(candidate.metadata.get("sink_package")),
    )


def _cluster_id(key: tuple[object, ...]) -> str:
    payload = json.dumps(key, sort_keys=True, separators=(",", ":"), default=str)
    return "cluster-" + sha256(payload.encode("utf-8")).hexdigest()[:16]


def _normalize_report_path(path: str) -> str:
    return Path(path).as_posix()


def _max_severity(findings: list[CandidateFinding]) -> str:
    return min((finding.severity for finding in findings), key=_severity_sort_key)


def _severity_sort_key(severity: str) -> int:
    normalized = severity.lower()
    try:
        return _SEVERITY_ORDER.index(normalized)
    except ValueError:
        return len(_SEVERITY_ORDER)


def _dedupe_locations(locations: list[SourceLocation]) -> list[SourceLocation]:
    deduped: dict[tuple[str, int, int, str], SourceLocation] = {}
    for location in locations:
        key = (
            _normalize_report_path(location.file),
            location.line,
            location.column,
            location.snippet,
        )
        deduped.setdefault(key, location)
    return list(deduped.values())


def _package_name(candidate: CandidateFinding) -> str | None:
    package_name = _metadata_string(candidate.metadata.get("package"))
    if package_name is not None:
        return package_name
    return _metadata_string(candidate.metadata.get("source_package"))


def _metadata_string(value: object) -> str | None:
    return value if isinstance(value, str) and value else None


def _metadata_strings(value: object) -> list[str]:
    if isinstance(value, list):
        values = [item.strip() for item in value if isinstance(item, str) and item.strip()]
        return list(dict.fromkeys(values))
    return []


def _metadata_bool(value: object) -> bool | None:
    if isinstance(value, bool):
        return value
    return None


def _group_report_findings_by_package(
    findings: list[CombinedFinding],
) -> dict[str, list[CombinedFinding]]:
    grouped: dict[str, list[CombinedFinding]] = {}
    for finding in findings:
        if finding.cross_package or finding.package_name is None:
            continue
        grouped.setdefault(finding.package_name, []).append(finding)
    return grouped


def _group_suppressed_findings_by_package(
    findings: list[SuppressedFinding],
) -> dict[str, list[SuppressedFinding]]:
    grouped: dict[str, list[SuppressedFinding]] = {}
    for finding in findings:
        if finding.cross_package or finding.package_name is None:
            continue
        grouped.setdefault(finding.package_name, []).append(finding)
    return grouped


def _group_dead_code_by_file(
    functions: list[ScannedFunction],
) -> dict[str, list[ScannedFunction]]:
    grouped: dict[str, list[ScannedFunction]] = {}
    for function in functions:
        grouped.setdefault(function.location.file, []).append(function)
    for file_name, file_functions in grouped.items():
        grouped[file_name] = sorted(
            file_functions,
            key=lambda function: (
                function.location.line,
                function.location.column,
                function.name,
            ),
        )
    return grouped


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
    "CandidateReportFinding",
    "CombinedFinding",
    "ConfidenceBreakdown",
    "ConfidenceComponent",
    "ExecutiveSummary",
    "FindingCluster",
    "FindingExplanation",
    "MatchedSpec",
    "PiranesiReport",
    "PropagationPathSummary",
    "ReportAppendix",
    "SanitizerExplanation",
    "SuppressedFinding",
    "VerificationState",
    "build_report",
    "render_markdown",
    "render_pr_body",
    "render_pr_body_for_finding",
    "update_report_metrics",
    "write_report_outputs",
]
