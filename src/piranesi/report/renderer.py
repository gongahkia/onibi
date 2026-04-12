from __future__ import annotations

import json
import logging
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
    ReachabilityResult,
    RegulatoryObligation,
    ScanMetadata,
    ScannedFunction,
    ScanResult,
    SourceLocation,
    TaintStep,
)
from piranesi.report.cwe import cwe_title, extract_cwe_id

_logger = logging.getLogger(__name__)
_SEVERITY_ORDER = ("critical", "high", "medium", "low", "informational")


class CombinedFinding(BaseModel):
    model_config = ConfigDict(extra="forbid")

    finding_id: str
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


class CandidateReportFinding(BaseModel):
    model_config = ConfigDict(extra="forbid")

    finding_id: str
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
    source_function_id: str | None = None
    package_name: str | None = None
    cross_package: bool = False
    source_package: str | None = None
    sink_package: str | None = None


class SuppressedFinding(BaseModel):
    model_config = ConfigDict(extra="forbid")

    finding_id: str
    cwe: str
    title: str
    severity: str
    confidence: float
    metadata: dict[str, object] = Field(default_factory=dict)
    taint_source: str
    taint_sink: str
    source_location: SourceLocation
    sink_location: SourceLocation
    suppression_reason: str | None = None
    package_name: str | None = None
    cross_package: bool = False
    source_package: str | None = None
    sink_package: str | None = None


class ExecutiveSummary(BaseModel):
    model_config = ConfigDict(extra="forbid")

    findings_detected: int
    suppressed_findings: int = 0
    findings_confirmed: int
    reachable_findings: int = 0
    unreachable_findings: int = 0
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
    scan_metadata: ScanMetadata
    executive_summary: ExecutiveSummary
    active_findings: list[CandidateReportFinding] = Field(default_factory=list)
    unreachable_findings: list[CandidateReportFinding] = Field(default_factory=list)
    findings: list[CombinedFinding] = Field(default_factory=list)
    package_findings: dict[str, list[CombinedFinding]] = Field(default_factory=dict)
    cross_package_findings: list[CombinedFinding] = Field(default_factory=list)
    suppressed_findings: list[SuppressedFinding] = Field(default_factory=list)
    suppressed_findings_by_package: dict[str, list[SuppressedFinding]] = Field(default_factory=dict)
    dead_code_functions: list[ScannedFunction] = Field(default_factory=list)
    dead_code_by_file: dict[str, list[ScannedFunction]] = Field(default_factory=dict)
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
    reachability: ReachabilityResult | None = None,
    include_unreachable: bool = False,
    dead_code_report: bool = False,
) -> PiranesiReport:
    generated_at = _utc_now()
    legal_by_id = {
        assessment.finding.finding.finding.id: assessment for assessment in legal_assessments
    }
    patch_by_id = {patch.finding.finding.finding.id: patch for patch in patch_results}
    reachable_candidates = [
        candidate
        for candidate in detected_findings
        if not candidate.suppressed and candidate.reachability == "reachable"
    ]
    unreachable_candidates = [
        candidate
        for candidate in detected_findings
        if not candidate.suppressed and candidate.reachability != "reachable"
    ]
    active_candidates = (
        [candidate for candidate in detected_findings if not candidate.suppressed]
        if include_unreachable
        else reachable_candidates
    )
    suppressed_findings = [
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
            suppression_reason=candidate.suppression_reason,
            package_name=_package_name(candidate),
            cross_package=bool(candidate.metadata.get("cross_package")),
            source_package=_metadata_string(candidate.metadata.get("source_package")),
            sink_package=_metadata_string(candidate.metadata.get("sink_package")),
        )
        for candidate in detected_findings
        if candidate.suppressed
    ]

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
            reachable_findings=len(reachable_candidates),
            unreachable_findings=len(unreachable_candidates),
            severity_breakdown=_candidate_severity_breakdown(active_candidates),
            top_regulatory_concerns=_top_regulatory_concerns(legal_assessments),
            total_llm_cost_usd=total_llm_cost_usd,
            duration_s=duration_s,
        ),
        active_findings=[_candidate_report_finding(candidate) for candidate in active_candidates],
        unreachable_findings=[
            _candidate_report_finding(candidate) for candidate in unreachable_candidates
        ],
        findings=findings,
        package_findings=_group_report_findings_by_package(findings),
        cross_package_findings=[finding for finding in findings if finding.cross_package],
        suppressed_findings=suppressed_findings,
        suppressed_findings_by_package=_group_suppressed_findings_by_package(suppressed_findings),
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


def _candidate_report_finding(candidate: CandidateFinding) -> CandidateReportFinding:
    original_severity = candidate.metadata.get("reachability_original_severity")
    return CandidateReportFinding(
        finding_id=candidate.id,
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
        source_function_id=_metadata_string(candidate.metadata.get("source_function_id")),
        package_name=_package_name(candidate),
        cross_package=bool(candidate.metadata.get("cross_package")),
        source_package=_metadata_string(candidate.metadata.get("source_package")),
        sink_package=_metadata_string(candidate.metadata.get("sink_package")),
    )


def _candidate_severity_breakdown(findings: list[CandidateFinding]) -> dict[str, int]:
    counts = dict.fromkeys(_SEVERITY_ORDER, 0)
    for finding in findings:
        if finding.suppressed:
            continue
        severity = finding.severity.lower()
        counts[severity] = counts.get(severity, 0) + 1
    return {severity: count for severity, count in counts.items() if count > 0}


def _package_name(candidate: CandidateFinding) -> str | None:
    package_name = _metadata_string(candidate.metadata.get("package"))
    if package_name is not None:
        return package_name
    return _metadata_string(candidate.metadata.get("source_package"))


def _metadata_string(value: object) -> str | None:
    return value if isinstance(value, str) and value else None


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
    "ExecutiveSummary",
    "PiranesiReport",
    "ReportAppendix",
    "SuppressedFinding",
    "build_report",
    "render_markdown",
    "render_pr_body",
    "render_pr_body_for_finding",
    "update_report_metrics",
    "write_report_outputs",
]
