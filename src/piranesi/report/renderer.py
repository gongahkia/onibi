from __future__ import annotations

import json
import logging
from datetime import UTC, datetime
from fnmatch import fnmatch
from hashlib import sha256
from pathlib import Path
from typing import Literal

from jinja2 import Environment, FileSystemLoader
from pydantic import BaseModel, ConfigDict, Field, ValidationError

from piranesi import __version__
from piranesi.advisory import advisory_db_path, get_advisory_db_status
from piranesi.config import (
    OwnershipConfig,
    OwnershipPackageMappingConfig,
    OwnershipPathMappingConfig,
)
from piranesi.detect.suppression import SuppressionLifecycleSummary
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
from piranesi.models.finding import (
    VerificationAttempt,
    VerificationEvidence,
    VerificationPrecondition,
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
RiskBand = Literal["low", "medium", "high", "critical"]
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
    outcome: str | None = None
    proof_mode: str | None = None
    target_profile: str | None = None
    launch_log_path: str | None = None
    startup_error: str | None = None
    reason: str | None = None
    verification_method: str | None = None
    triage_verdict: str | None = None
    triage_mode: str | None = None
    suppression_reason: str | None = None
    preconditions: list[VerificationPrecondition] = Field(default_factory=list)
    missing_preconditions: list[str] = Field(default_factory=list)
    evidence: list[str] = Field(default_factory=list)
    rich_evidence: VerificationEvidence | None = None
    evidence_artifact_path: str | None = None
    actionable_next_steps: list[str] = Field(default_factory=list)


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


class CompositeRiskComponent(BaseModel):
    model_config = ConfigDict(extra="forbid")

    points: float
    rationale: str


class CompositeRiskBreakdown(BaseModel):
    model_config = ConfigDict(extra="forbid")

    model_version: str = "v1"
    severity: CompositeRiskComponent
    confidence: CompositeRiskComponent
    source_exposure: CompositeRiskComponent
    sink_criticality: CompositeRiskComponent
    ownership_signal: CompositeRiskComponent
    verification_signal: CompositeRiskComponent
    exploitability_signal: CompositeRiskComponent
    advisory_signal: CompositeRiskComponent
    reachable_path_signal: CompositeRiskComponent
    suppression_signal: CompositeRiskComponent
    total_score: float
    risk_band: RiskBand
    formula: str = "sum(component points), clamped to [0, 100]"


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
    composite_risk_score: float = 0.0
    composite_risk_band: RiskBand = "low"
    composite_risk: CompositeRiskBreakdown | None = None
    metadata: dict[str, object] = Field(default_factory=dict)
    ownership: OwnershipMetadata = Field(default_factory=lambda: OwnershipMetadata())
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
    composite_risk_score: float = 0.0
    composite_risk_band: RiskBand = "low"
    composite_risk: CompositeRiskBreakdown | None = None
    metadata: dict[str, object] = Field(default_factory=dict)
    ownership: OwnershipMetadata = Field(default_factory=lambda: OwnershipMetadata())
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
    composite_risk_score: float = 0.0
    composite_risk_band: RiskBand = "low"
    composite_risk: CompositeRiskBreakdown | None = None
    metadata: dict[str, object] = Field(default_factory=dict)
    ownership: OwnershipMetadata = Field(default_factory=lambda: OwnershipMetadata())
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
    suppression_rules_total: int = 0
    suppression_rules_active: int = 0
    suppression_rules_expired: int = 0
    suppression_rules_stale: int = 0
    suppression_rules_invalid: int = 0
    reachable_findings: int = 0
    unreachable_findings: int = 0
    finding_clusters: int = 0
    severity_breakdown: dict[str, int] = Field(default_factory=dict)
    composite_risk_breakdown: dict[str, int] = Field(default_factory=dict)
    highest_composite_risk_score: float = 0.0
    highest_composite_risk_band: RiskBand | None = None
    highest_composite_risk_finding_id: str | None = None
    top_composite_risk_findings: list[str] = Field(default_factory=list)
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


class ControlOwnerMapping(BaseModel):
    model_config = ConfigDict(extra="forbid")

    framework: str
    control: str
    owner: str


class OwnershipMetadata(BaseModel):
    model_config = ConfigDict(extra="forbid")

    service: str | None = None
    system: str | None = None
    team: str | None = None
    owner: str | None = None
    repository: str | None = None
    environment: str | None = None
    control_owner: str | None = None
    package: str | None = None
    source_path: str | None = None
    sink_path: str | None = None
    matched_package_mapping: str | None = None
    matched_path_mapping: str | None = None


class ReportOwnershipContext(BaseModel):
    model_config = ConfigDict(extra="forbid")

    service: str | None = None
    system: str | None = None
    team: str | None = None
    owner: str | None = None
    repository: str | None = None
    environment: str | None = None
    control_owner: str | None = None
    control_mappings: list[ControlOwnerMapping] = Field(default_factory=list)


class AdvisoryDbFreshness(BaseModel):
    model_config = ConfigDict(extra="forbid")

    path: str
    exists: bool
    schema_version: int | None = None
    advisory_count: int = 0
    affected_package_count: int = 0
    sources: list[str] = Field(default_factory=list)
    last_updated: str | None = None
    checksum_sha256: str | None = None
    freshness: str = "missing"
    stale_after_days: int = 14
    age_days: float | None = None
    warnings: list[str] = Field(default_factory=list)


class KnownLimitation(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    title: str
    affected_feature: str
    severity: str
    impact: str
    workaround: str
    status: str
    introduced_version: str
    last_reviewed: str


class PiranesiReport(BaseModel):
    model_config = ConfigDict(extra="forbid")

    target: str
    generated_at: str
    files_scanned: list[str] = Field(default_factory=list)
    status_legend: dict[str, str] = Field(default_factory=lambda: dict(_EVIDENCE_STATUS_LABELS))
    scan_metadata: ScanMetadata
    advisory_db: AdvisoryDbFreshness | None = None
    known_limitations: list[KnownLimitation] = Field(default_factory=list)
    ownership_context: ReportOwnershipContext = Field(default_factory=ReportOwnershipContext)
    executive_summary: ExecutiveSummary
    suppression_lifecycle: SuppressionLifecycleSummary | None = None
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
    verification_attempts: list[VerificationAttempt] | None = None,
    legal_assessments: list[LegalAssessment],
    patch_results: list[PatchResult],
    target_dir: Path,
    total_llm_cost_usd: float,
    duration_s: float,
    stage_timings_s: dict[str, float],
    reachability: ReachabilityResult | None = None,
    include_unreachable: bool = False,
    dead_code_report: bool = False,
    suppression_lifecycle: SuppressionLifecycleSummary | None = None,
    ownership_config: OwnershipConfig | None = None,
) -> PiranesiReport:
    generated_at = _utc_now()
    ownership_context = _report_ownership_context(
        ownership_config=ownership_config,
        scan_result=scan_result,
        target_dir=target_dir,
    )
    triage_by_id = _triage_lookup(triaged_findings or [])
    attempts_by_id = _verification_attempt_lookup(verification_attempts or [])
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
        ownership = _resolve_finding_ownership(
            candidate,
            target_dir=target_dir,
            ownership_config=ownership_config,
            ownership_context=ownership_context,
        )
        composite_risk = _composite_risk_breakdown(
            candidate,
            evidence_status="suppressed",
            verified=False,
            confirmed=None,
            verification_attempt=attempts_by_id.get(candidate.id),
            ownership=ownership,
        )
        suppressed_findings.append(
            SuppressedFinding(
                finding_id=candidate.id,
                cwe=_extract_cwe_id(candidate.vuln_class),
                title=_finding_title(candidate),
                severity=candidate.severity,
                confidence=candidate.confidence,
                composite_risk_score=composite_risk.total_score,
                composite_risk_band=composite_risk.risk_band,
                composite_risk=composite_risk,
                metadata=dict(candidate.metadata),
                ownership=ownership,
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
                    verification_attempt=attempts_by_id.get(candidate.id),
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
            verification_attempt=attempts_by_id.get(candidate.id),
            cluster_by_id={},
            target_dir=target_dir,
            ownership_config=ownership_config,
            ownership_context=ownership_context,
        )
        for candidate in active_candidates
    ]
    unreachable_report_findings = [
        _candidate_report_finding(
            candidate,
            triaged=triage_by_id.get(candidate.id),
            verification_attempt=attempts_by_id.get(candidate.id),
            target_dir=target_dir,
            ownership_config=ownership_config,
            ownership_context=ownership_context,
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
    active_report_findings = sorted(active_report_findings, key=_composite_risk_sort_key)
    unreachable_report_findings = sorted(unreachable_report_findings, key=_composite_risk_sort_key)
    suppressed_findings = sorted(suppressed_findings, key=_composite_risk_sort_key)
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
        ownership = _resolve_finding_ownership(
            candidate,
            target_dir=target_dir,
            ownership_config=ownership_config,
            ownership_context=ownership_context,
        )
        composite_risk = _composite_risk_breakdown(
            candidate,
            evidence_status="confirmed",
            verified=True,
            confirmed=confirmed,
            verification_attempt=attempts_by_id.get(candidate.id),
            ownership=ownership,
        )
        finding = CombinedFinding(
            finding_id=finding_id,
            evidence_status="confirmed",
            explanation=_build_finding_explanation(
                candidate,
                evidence_status="confirmed",
                triaged=confirmed.finding,
                confirmed=confirmed,
                verification_attempt=attempts_by_id.get(candidate.id),
                verification_method="smt+sandbox",
                verified=True,
            ),
            cwe=_extract_cwe_id(candidate.vuln_class),
            title=_finding_title(candidate),
            severity=candidate.severity,
            confidence=candidate.confidence,
            composite_risk_score=composite_risk.total_score,
            composite_risk_band=composite_risk.risk_band,
            composite_risk=composite_risk,
            metadata=dict(candidate.metadata),
            ownership=ownership,
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
    findings = sorted(findings, key=_composite_risk_sort_key)
    risk_ranked_findings = _ranked_findings_by_risk(
        findings=findings,
        active_findings=active_report_findings,
        unreachable_findings=unreachable_report_findings,
        suppressed_findings=suppressed_findings,
    )
    top_risk = risk_ranked_findings[0] if risk_ranked_findings else None
    advisory_db = _advisory_db_freshness(target_dir=target_dir)
    known_limitations = _known_limitations_for_report()

    report = PiranesiReport(
        target=str(target_dir.resolve(strict=False)),
        generated_at=generated_at,
        files_scanned=list(scan_result.files_scanned),
        scan_metadata=scan_result.metadata,
        advisory_db=advisory_db,
        known_limitations=known_limitations,
        ownership_context=ownership_context,
        executive_summary=ExecutiveSummary(
            findings_detected=len(detected_findings),
            suppressed_findings=len(suppressed_findings),
            findings_confirmed=len(confirmed_findings),
            status_breakdown=status_breakdown,
            suppression_rules_total=(
                0 if suppression_lifecycle is None else suppression_lifecycle.total_rules
            ),
            suppression_rules_active=(
                0 if suppression_lifecycle is None else suppression_lifecycle.active_rules
            ),
            suppression_rules_expired=(
                0 if suppression_lifecycle is None else suppression_lifecycle.expired_rules
            ),
            suppression_rules_stale=(
                0 if suppression_lifecycle is None else suppression_lifecycle.stale_rules
            ),
            suppression_rules_invalid=(
                0 if suppression_lifecycle is None else suppression_lifecycle.invalid_rules
            ),
            reachable_findings=len(reachable_candidates),
            unreachable_findings=len(unreachable_candidates),
            finding_clusters=len(active_clusters),
            severity_breakdown=_candidate_severity_breakdown(active_candidates),
            composite_risk_breakdown=_composite_risk_band_breakdown(risk_ranked_findings),
            highest_composite_risk_score=(
                0.0 if top_risk is None else top_risk.composite_risk_score
            ),
            highest_composite_risk_band=(
                None if top_risk is None else top_risk.composite_risk_band
            ),
            highest_composite_risk_finding_id=(
                None if top_risk is None else top_risk.finding_id
            ),
            top_composite_risk_findings=[
                finding.finding_id for finding in risk_ranked_findings[:5]
            ],
            top_regulatory_concerns=_top_regulatory_concerns(legal_assessments),
            total_llm_cost_usd=total_llm_cost_usd,
            duration_s=duration_s,
        ),
        suppression_lifecycle=suppression_lifecycle,
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
    verification_attempt: VerificationAttempt | None = None,
    cluster_by_id: dict[str, FindingCluster] | None = None,
    target_dir: Path,
    ownership_config: OwnershipConfig | None,
    ownership_context: ReportOwnershipContext,
) -> CandidateReportFinding:
    original_severity = candidate.metadata.get("reachability_original_severity")
    evidence_status = _candidate_evidence_status(candidate, triaged=triaged)
    ownership = _resolve_finding_ownership(
        candidate,
        target_dir=target_dir,
        ownership_config=ownership_config,
        ownership_context=ownership_context,
    )
    composite_risk = _composite_risk_breakdown(
        candidate,
        evidence_status=evidence_status,
        verified=False,
        confirmed=None,
        verification_attempt=verification_attempt,
        ownership=ownership,
    )
    return CandidateReportFinding(
        finding_id=candidate.id,
        evidence_status=evidence_status,
        explanation=_build_finding_explanation(
            candidate,
            evidence_status=evidence_status,
            triaged=triaged,
            confirmed=None,
            verification_attempt=verification_attempt,
            verification_method=None,
            verified=False,
        ),
        cwe=_extract_cwe_id(candidate.vuln_class),
        title=_finding_title(candidate),
        severity=candidate.severity,
        original_severity=original_severity if isinstance(original_severity, str) else None,
        confidence=candidate.confidence,
        composite_risk_score=composite_risk.total_score,
        composite_risk_band=composite_risk.risk_band,
        composite_risk=composite_risk,
        metadata=dict(candidate.metadata),
        ownership=ownership,
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


def _verification_attempt_lookup(
    attempts: list[VerificationAttempt],
) -> dict[str, VerificationAttempt]:
    return {attempt.finding_id: attempt for attempt in attempts}


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
    verification_attempt: VerificationAttempt | None,
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
            verification_attempt=verification_attempt,
            verification_method=verification_method,
            verified=verified,
            suppression_reason=suppression_reason,
        ),
        confidence=_confidence_breakdown(
            candidate,
            evidence_status=evidence_status,
            triaged=triaged,
            confirmed=confirmed,
            verification_attempt=verification_attempt,
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
    verification_attempt: VerificationAttempt | None,
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
    outcome = None if verification_attempt is None else verification_attempt.status
    proof_mode = None if verification_attempt is None else verification_attempt.proof_mode
    target_profile = None if verification_attempt is None else verification_attempt.target_profile
    launch_log_path = None if verification_attempt is None else verification_attempt.launch_log_path
    startup_error = None if verification_attempt is None else verification_attempt.startup_error
    reason = None if verification_attempt is None else verification_attempt.reason
    evidence = [] if verification_attempt is None else list(verification_attempt.evidence)
    rich_evidence = None if verification_attempt is None else verification_attempt.rich_evidence
    evidence_artifact_path = (
        None if verification_attempt is None else verification_attempt.evidence_artifact_path
    )
    preconditions = [] if verification_attempt is None else list(verification_attempt.preconditions)
    missing = [
        precondition.key
        for precondition in preconditions
        if precondition.required and precondition.status == "missing"
    ]
    next_steps = sorted(
        {
            precondition.next_step.strip()
            for precondition in preconditions
            if isinstance(precondition.next_step, str) and precondition.next_step.strip()
        }
    )
    if reason is None and evidence_status != "confirmed":
        outcome = "not_attempted"
        reason = "verification attempt metadata unavailable for this finding"
    return VerificationState(
        state=state,
        verified=verified,
        outcome=outcome,
        proof_mode=proof_mode,
        target_profile=target_profile,
        launch_log_path=launch_log_path,
        startup_error=startup_error,
        reason=reason,
        verification_method=verification_method,
        triage_verdict=None if triaged is None else triaged.triage_verdict,
        triage_mode=None if triaged is None else triaged.triage_mode,
        suppression_reason=suppression_reason,
        preconditions=preconditions,
        missing_preconditions=missing,
        evidence=evidence,
        rich_evidence=rich_evidence,
        evidence_artifact_path=evidence_artifact_path,
        actionable_next_steps=next_steps,
    )


def _confidence_breakdown(
    candidate: CandidateFinding,
    *,
    evidence_status: EvidenceStatus,
    triaged: TriagedFinding | None,
    confirmed: ConfirmedFinding | None,
    verification_attempt: VerificationAttempt | None,
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
        verification_attempt=verification_attempt,
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


def _composite_risk_breakdown(
    candidate: CandidateFinding,
    *,
    evidence_status: EvidenceStatus,
    verified: bool,
    confirmed: ConfirmedFinding | None,
    verification_attempt: VerificationAttempt | None,
    ownership: OwnershipMetadata,
) -> CompositeRiskBreakdown:
    severity_points, severity_reason = _risk_severity_component(candidate.severity)
    confidence_points = round(max(0.0, min(1.0, candidate.confidence)) * 15.0, 2)
    confidence_reason = f"scaled from finding confidence {candidate.confidence:.2f}"

    source_points, source_reason = _risk_source_exposure_component(candidate)
    sink_points, sink_reason = _risk_sink_criticality_component(candidate)
    ownership_points, ownership_reason = _risk_ownership_component(ownership)
    verification_points, verification_reason = _risk_verification_component(
        evidence_status=evidence_status,
        verified=verified,
        verification_attempt=verification_attempt,
    )
    exploitability_points, exploitability_reason = _risk_exploitability_component(
        candidate,
        confirmed=confirmed,
        verification_attempt=verification_attempt,
    )
    advisory_points, advisory_reason = _risk_advisory_component(candidate, confirmed=confirmed)
    reachable_points, reachable_reason = _risk_reachability_component(candidate)
    suppression_points, suppression_reason = _risk_suppression_component(evidence_status)

    raw_total = (
        severity_points
        + confidence_points
        + source_points
        + sink_points
        + ownership_points
        + verification_points
        + exploitability_points
        + advisory_points
        + reachable_points
        + suppression_points
    )
    total_score = round(max(0.0, min(100.0, raw_total)), 1)
    risk_band = _composite_risk_band(total_score)

    return CompositeRiskBreakdown(
        severity=_composite_risk_component(severity_points, severity_reason),
        confidence=_composite_risk_component(confidence_points, confidence_reason),
        source_exposure=_composite_risk_component(source_points, source_reason),
        sink_criticality=_composite_risk_component(sink_points, sink_reason),
        ownership_signal=_composite_risk_component(ownership_points, ownership_reason),
        verification_signal=_composite_risk_component(verification_points, verification_reason),
        exploitability_signal=_composite_risk_component(
            exploitability_points,
            exploitability_reason,
        ),
        advisory_signal=_composite_risk_component(advisory_points, advisory_reason),
        reachable_path_signal=_composite_risk_component(reachable_points, reachable_reason),
        suppression_signal=_composite_risk_component(suppression_points, suppression_reason),
        total_score=total_score,
        risk_band=risk_band,
    )


def _composite_risk_component(points: float, rationale: str) -> CompositeRiskComponent:
    return CompositeRiskComponent(points=round(points, 2), rationale=rationale)


def _risk_severity_component(severity: str) -> tuple[float, str]:
    normalized = severity.lower()
    weights = {
        "critical": 35.0,
        "high": 28.0,
        "medium": 20.0,
        "low": 12.0,
        "informational": 6.0,
    }
    points = weights.get(normalized, 10.0)
    return points, f"severity '{normalized}' contributes {points:.1f} points"


def _risk_source_exposure_component(candidate: CandidateFinding) -> tuple[float, str]:
    source_type = candidate.source.source_type.lower()
    category = _metadata_string(candidate.metadata.get("source_spec_category")) or ""
    external_markers = (
        "req.",
        "request",
        "query",
        "param",
        "header",
        "cookie",
        "body",
        "url",
    )
    if any(marker in source_type for marker in external_markers):
        return 10.0, "source appears externally reachable (request-driven input)"
    if category in {"request_body", "request_param", "header", "cookie", "url_param"}:
        return 9.0, f"source spec category '{category}' is externally influenced"
    if "env" in source_type or category == "env_var":
        return 4.0, "source is environment-driven and less directly exposed"
    return 6.0, "source exposure is moderate based on available metadata"


def _risk_sink_criticality_component(candidate: CandidateFinding) -> tuple[float, str]:
    sink_type = candidate.sink.sink_type.lower()
    api_name = candidate.sink.api_name.lower()
    cwe = _extract_cwe_id(candidate.vuln_class)
    high_cwe = {"CWE-78", "CWE-89", "CWE-94", "CWE-95"}
    medium_cwe = {"CWE-79", "CWE-22", "CWE-434"}
    if sink_type in {"sql_query", "command_execution", "shell_exec"}:
        return 10.0, f"sink type '{sink_type}' is high impact"
    if cwe in high_cwe:
        return 10.0, f"{cwe} is mapped as high sink criticality"
    if sink_type in {"file_write", "file_read", "http_request", "eval", "html_output"}:
        return 7.0, f"sink type '{sink_type}' is medium-high impact"
    if cwe in medium_cwe:
        return 7.0, f"{cwe} is mapped as medium sink criticality"
    if "query" in api_name or "exec" in api_name:
        return 8.0, f"sink api '{candidate.sink.api_name}' suggests high-risk execution"
    return 4.0, "sink criticality is moderate with current metadata"


def _risk_ownership_component(ownership: OwnershipMetadata) -> tuple[float, str]:
    points = 0.0
    missing_fields: list[str] = []
    if ownership.team is None:
        points += 2.5
        missing_fields.append("team")
    if ownership.owner is None:
        points += 2.5
        missing_fields.append("owner")
    if ownership.service is None:
        points += 1.0
        missing_fields.append("service")
    if not missing_fields:
        return 0.0, "ownership metadata includes service/team/owner attribution"
    points = min(points, 5.0)
    return points, f"missing ownership metadata: {', '.join(missing_fields)}"


def _risk_verification_component(
    *,
    evidence_status: EvidenceStatus,
    verified: bool,
    verification_attempt: VerificationAttempt | None,
) -> tuple[float, str]:
    points_by_status = {
        "confirmed": 20.0,
        "triaged_active_candidate": 10.0,
        "static_candidate": 6.0,
        "unreachable_candidate": -12.0,
        "suppressed": -10.0,
    }
    points = points_by_status[evidence_status]
    rationale = f"evidence status '{evidence_status}' contributes {points:.1f} points"
    if verified and points < 20.0:
        points = 20.0
        rationale = "verified finding elevates verification signal"
    if verification_attempt is not None and verification_attempt.status == "error":
        points += 3.0
        rationale += "; verification attempt errored"
    return points, rationale


def _risk_exploitability_component(
    candidate: CandidateFinding,
    *,
    confirmed: ConfirmedFinding | None,
    verification_attempt: VerificationAttempt | None,
) -> tuple[float, str]:
    points = 0.0
    reasons: list[str] = []
    if confirmed is not None and confirmed.exploit_payload:
        points += 8.0
        reasons.append("exploit payload reproduced")
    rich_evidence = None if verification_attempt is None else verification_attempt.rich_evidence
    if (
        rich_evidence is not None
        and rich_evidence.response_diff_summary is not None
        and (
            rich_evidence.response_diff_summary.status_code_changed
            or rich_evidence.response_diff_summary.body_changed
        )
    ):
        points += 3.0
        reasons.append("verification response materially changed")
    exploit_status = _metadata_string(candidate.metadata.get("exploit_status"))
    if exploit_status in {"known_exploited", "poc_available", "actively_exploited_risk"}:
        points += 2.0
        reasons.append(f"exploit status={exploit_status}")
    points = min(points, 10.0)
    if not reasons:
        return 0.0, "no direct exploitability evidence captured"
    return points, "; ".join(reasons)


def _risk_advisory_component(
    candidate: CandidateFinding,
    *,
    confirmed: ConfirmedFinding | None,
) -> tuple[float, str]:
    cve_ids: set[str] = set()
    if confirmed is not None:
        cve_ids.update(cve for cve in confirmed.related_cves if cve)
    for key in ("cve", "cve_id"):
        value = _metadata_string(candidate.metadata.get(key))
        if value:
            cve_ids.add(value)
    cve_ids.update(_metadata_strings(candidate.metadata.get("cves")))
    cve_ids.update(_metadata_strings(candidate.metadata.get("aliases")))

    points = min(6.0, 2.0 * len(cve_ids))
    reasons: list[str] = []
    if cve_ids:
        reasons.append(f"{len(cve_ids)} advisory identifier(s)")
    epss_score = candidate.metadata.get("epss_score")
    if isinstance(epss_score, (float, int)):
        if epss_score >= 0.7:
            points += 2.0
            reasons.append("high EPSS score")
        elif epss_score >= 0.3:
            points += 1.0
            reasons.append("moderate EPSS score")
    points = min(points, 10.0)
    if not reasons:
        return 0.0, "no advisory/exploit feed signal attached"
    return points, "; ".join(reasons)


def _risk_reachability_component(candidate: CandidateFinding) -> tuple[float, str]:
    if candidate.reachability == "reachable":
        return 10.0, "flow is reachable from known entry points"
    return -15.0, "flow currently marked unreachable"


def _risk_suppression_component(evidence_status: EvidenceStatus) -> tuple[float, str]:
    if evidence_status == "suppressed":
        return -30.0, "suppression lowers remediation priority score"
    return 0.0, "finding is not suppressed"


def _composite_risk_band(score: float) -> RiskBand:
    if score >= 75.0:
        return "critical"
    if score >= 55.0:
        return "high"
    if score >= 30.0:
        return "medium"
    return "low"


def _composite_risk_sort_key(
    finding: CombinedFinding | CandidateReportFinding | SuppressedFinding,
) -> tuple[float, int, str]:
    return (-finding.composite_risk_score, _severity_sort_key(finding.severity), finding.finding_id)


def _ranked_findings_by_risk(
    *,
    findings: list[CombinedFinding],
    active_findings: list[CandidateReportFinding],
    unreachable_findings: list[CandidateReportFinding],
    suppressed_findings: list[SuppressedFinding],
) -> list[CombinedFinding | CandidateReportFinding | SuppressedFinding]:
    all_findings: list[CombinedFinding | CandidateReportFinding | SuppressedFinding] = [
        *findings,
        *active_findings,
        *unreachable_findings,
        *suppressed_findings,
    ]
    return sorted(all_findings, key=_composite_risk_sort_key)


def _composite_risk_band_breakdown(
    findings: list[CombinedFinding | CandidateReportFinding | SuppressedFinding],
) -> dict[str, int]:
    order: tuple[RiskBand, ...] = ("critical", "high", "medium", "low")
    counts: dict[str, int] = dict.fromkeys(order, 0)
    for finding in findings:
        counts[finding.composite_risk_band] = counts.get(finding.composite_risk_band, 0) + 1
    return {band: count for band, count in counts.items() if count > 0}


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
    verification_attempt: VerificationAttempt | None,
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
    if verification_attempt is not None:
        if verification_attempt.status == "skipped":
            return 0.35, verification_attempt.reason
        if verification_attempt.status == "inconclusive":
            return 0.45, verification_attempt.reason
        if verification_attempt.status == "error":
            return 0.3, verification_attempt.reason
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


def _advisory_db_freshness(
    *,
    target_dir: Path,
) -> AdvisoryDbFreshness:
    status = get_advisory_db_status(advisory_db_path(target_dir))
    return AdvisoryDbFreshness(
        path=str(status.path),
        exists=status.exists,
        schema_version=status.schema_version,
        advisory_count=status.advisory_count,
        affected_package_count=status.affected_package_count,
        sources=list(status.sources),
        last_updated=status.last_updated,
        checksum_sha256=status.checksum_sha256,
        freshness=status.freshness,
        stale_after_days=status.stale_after_days,
        age_days=status.age_days,
        warnings=list(status.warnings),
    )


def _known_limitations_for_report() -> list[KnownLimitation]:
    registry_path = Path(__file__).resolve().parents[3] / "docs" / "known-limitations.json"
    if not registry_path.exists():
        return []
    try:
        payload = json.loads(registry_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        _logger.warning("unable to load known limitations registry: %s", exc)
        return []
    raw_limitations = payload.get("limitations")
    if not isinstance(raw_limitations, list):
        return []
    limitations: list[KnownLimitation] = []
    for entry in raw_limitations:
        if not isinstance(entry, dict):
            continue
        try:
            limitation = KnownLimitation.model_validate(entry)
        except ValidationError:
            continue
        if limitation.status == "resolved":
            continue
        limitations.append(limitation)
    return sorted(limitations, key=lambda limitation: limitation.id)


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


def _report_ownership_context(
    *,
    ownership_config: OwnershipConfig | None,
    scan_result: ScanResult,
    target_dir: Path,
) -> ReportOwnershipContext:
    config = ownership_config or OwnershipConfig()
    repository = config.repository
    if repository is None and config.autodetect_repository:
        repository = _repository_name(scan_result=scan_result, target_dir=target_dir)
    service = config.service
    if service is None and config.autodetect_service:
        service = repository or _repository_name(scan_result=scan_result, target_dir=target_dir)
    control_mappings = [
        ControlOwnerMapping(
            framework=mapping.framework,
            control=mapping.control,
            owner=mapping.owner,
        )
        for mapping in config.control_mappings
    ]
    return ReportOwnershipContext(
        service=service,
        system=config.system,
        team=config.team,
        owner=config.owner,
        repository=repository,
        environment=config.environment,
        control_owner=config.control_owner,
        control_mappings=control_mappings,
    )


def _resolve_finding_ownership(
    candidate: CandidateFinding,
    *,
    target_dir: Path,
    ownership_config: OwnershipConfig | None,
    ownership_context: ReportOwnershipContext,
) -> OwnershipMetadata:
    config = ownership_config or OwnershipConfig()
    source_path = _relative_report_path(candidate.source.location.file, target_dir=target_dir)
    sink_path = _relative_report_path(candidate.sink.location.file, target_dir=target_dir)
    package_name = _package_name(candidate)

    resolved = OwnershipMetadata(
        service=ownership_context.service,
        system=ownership_context.system,
        team=ownership_context.team,
        owner=ownership_context.owner,
        repository=ownership_context.repository,
        environment=ownership_context.environment,
        control_owner=ownership_context.control_owner,
        package=package_name,
        source_path=source_path,
        sink_path=sink_path,
    )

    package_mapping = _match_package_mapping(candidate, mappings=config.package_mappings)
    if package_mapping is not None:
        resolved = _apply_ownership_override(resolved, mapping=package_mapping)
        resolved = resolved.model_copy(update={"matched_package_mapping": package_mapping.package})

    path_mapping = _match_path_mapping(
        source_path=source_path,
        sink_path=sink_path,
        mappings=config.path_mappings,
    )
    if path_mapping is not None:
        resolved = _apply_ownership_override(resolved, mapping=path_mapping)
        resolved = resolved.model_copy(update={"matched_path_mapping": path_mapping.path})

    return resolved


def _match_package_mapping(
    candidate: CandidateFinding,
    *,
    mappings: list[OwnershipPackageMappingConfig],
) -> OwnershipPackageMappingConfig | None:
    package_candidates = {
        value.lower()
        for value in (
            _package_name(candidate),
            _metadata_string(candidate.metadata.get("source_package")),
            _metadata_string(candidate.metadata.get("sink_package")),
        )
        if value is not None
    }
    if not package_candidates:
        return None
    match: OwnershipPackageMappingConfig | None = None
    for mapping in mappings:
        if mapping.package.lower() in package_candidates:
            match = mapping
    return match


def _match_path_mapping(
    *,
    source_path: str | None,
    sink_path: str | None,
    mappings: list[OwnershipPathMappingConfig],
) -> OwnershipPathMappingConfig | None:
    best: tuple[int, int, OwnershipPathMappingConfig] | None = None
    for index, mapping in enumerate(mappings):
        if not _mapping_matches_path(mapping.path, source_path=source_path, sink_path=sink_path):
            continue
        rank = _mapping_specificity(mapping.path)
        if best is None or (rank, index) > (best[0], best[1]):
            best = (rank, index, mapping)
    return None if best is None else best[2]


def _mapping_matches_path(
    pattern: str,
    *,
    source_path: str | None,
    sink_path: str | None,
) -> bool:
    return (source_path is not None and fnmatch(source_path, pattern)) or (
        sink_path is not None and fnmatch(sink_path, pattern)
    )


def _mapping_specificity(pattern: str) -> int:
    wildcardless = pattern.replace("*", "").replace("?", "")
    return len(wildcardless)


def _apply_ownership_override(
    ownership: OwnershipMetadata,
    *,
    mapping: OwnershipPathMappingConfig | OwnershipPackageMappingConfig,
) -> OwnershipMetadata:
    update: dict[str, object] = {}
    for field_name in (
        "service",
        "system",
        "team",
        "owner",
        "repository",
        "environment",
        "control_owner",
    ):
        value = getattr(mapping, field_name)
        if value is not None:
            update[field_name] = value
    if not update:
        return ownership
    return ownership.model_copy(update=update)


def _relative_report_path(path: str, *, target_dir: Path) -> str:
    candidate = Path(path)
    if not candidate.is_absolute():
        return candidate.as_posix()
    try:
        return (
            candidate.resolve(strict=False)
            .relative_to(target_dir.resolve(strict=False))
            .as_posix()
        )
    except ValueError:
        return candidate.as_posix()


def _repository_name(*, scan_result: ScanResult, target_dir: Path) -> str | None:
    project_root_name = Path(scan_result.project_root).name.strip()
    if project_root_name:
        return project_root_name
    target_name = target_dir.resolve(strict=False).name.strip()
    return target_name or None


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
    "CompositeRiskBreakdown",
    "CompositeRiskComponent",
    "ConfidenceBreakdown",
    "ConfidenceComponent",
    "ExecutiveSummary",
    "FindingCluster",
    "FindingExplanation",
    "KnownLimitation",
    "MatchedSpec",
    "OwnershipMetadata",
    "PiranesiReport",
    "PropagationPathSummary",
    "ReportAppendix",
    "ReportOwnershipContext",
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
