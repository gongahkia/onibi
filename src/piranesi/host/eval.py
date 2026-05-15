from __future__ import annotations

import csv
import json
from collections import Counter
from datetime import UTC, datetime
from pathlib import Path
from statistics import mean
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator

from piranesi.host.analyze import analyze_snapshot
from piranesi.host.ingest import HostInputError, load_host_input
from piranesi.host.models import HostFinding, HostPostureReport, HostSnapshot, Severity
from piranesi.llm.provider import LLMProvider

HostBenchmarkName = Literal[
    "piranesi_deterministic",
    "piranesi_deterministic_llm",
    "trivy_only",
    "lynis_only",
    "openscap_only",
]
HostBenchmarkStatus = Literal["ok", "skipped", "error"]
FindingMatrixStatus = Literal[
    "true_positive",
    "false_positive",
    "false_negative",
    "allowed_extra",
    "skipped",
    "expected_absent_pass",
    "expected_absent_violation",
]

BASELINE_ORDER: tuple[HostBenchmarkName, ...] = (
    "piranesi_deterministic",
    "trivy_only",
    "lynis_only",
    "openscap_only",
    "piranesi_deterministic_llm",
)
PRIMARY_BASELINE: HostBenchmarkName = "piranesi_deterministic"


class HostGroundTruthMatcher(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str | None = None
    rule_id: str | None = None
    instance_key: str | None = None
    severity: Severity | None = None
    source_tool: str | None = None
    category: str | None = None
    title_contains: str | None = None
    note: str | None = None

    @model_validator(mode="after")
    def _has_match_constraint(self) -> HostGroundTruthMatcher:
        if not any(
            (
                self.rule_id,
                self.instance_key,
                self.severity,
                self.source_tool,
                self.category,
                self.title_contains,
            )
        ):
            raise ValueError("ground truth matcher must specify at least one constraint")
        return self


class HostGroundTruth(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: Literal[1] = 1
    expected_findings: list[HostGroundTruthMatcher] = Field(default_factory=list)
    expected_absent: list[HostGroundTruthMatcher] = Field(default_factory=list)
    allowed_extra: list[HostGroundTruthMatcher] = Field(default_factory=list)
    clean_fixture: bool = False
    notes: str | None = None


class HostTriageSpeedProxy(BaseModel):
    model_config = ConfigDict(extra="forbid")

    kind: Literal["proxy"] = "proxy"
    label: str = "triage-speed metrics are proxies, not measured analyst time"
    lower_is_better: bool = True
    score: float = 0.0
    mean_findings_after_dedup: float = 0.0
    mean_top_action_count: float = 0.0
    evidence_coverage_score: float = 0.0
    remediation_present_ratio: float = 0.0
    risk_score_present_ratio: float = 0.0
    mean_report_sections_for_top_three_actions: float = 0.0
    proxy_inputs: list[str] = Field(default_factory=list)


class HostBenchmarkMetrics(BaseModel):
    model_config = ConfigDict(extra="forbid")

    fixture_count: int = 0
    expected_issue_count: int = 0
    detected_issue_count: int = 0
    true_positives: int = 0
    false_positives: int = 0
    false_negatives: int = 0
    expected_absent_count: int = 0
    expected_absent_violations: int = 0
    allowed_extra_count: int = 0
    skipped_count: int = 0
    precision: float = 0.0
    recall: float = 0.0
    f1: float = 0.0
    evidence_coverage_score: float = 0.0
    mean_findings_per_host: float = 0.0
    mean_top_action_count: float = 0.0
    time_to_triage_proxy: HostTriageSpeedProxy = Field(default_factory=HostTriageSpeedProxy)


class HostFindingMatrixRow(BaseModel):
    model_config = ConfigDict(extra="forbid")

    fixture: str
    target: str
    baseline: HostBenchmarkName
    status: FindingMatrixStatus
    finding_id: str | None = None
    rule_id: str | None = None
    instance_key: str | None = None
    severity: Severity | None = None
    title: str | None = None
    source_tool: str | None = None
    expected_id: str | None = None
    expected_rule_id: str | None = None
    expected_instance_key: str | None = None
    note: str | None = None


class HostFixtureBenchmarkResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    fixture: str
    target: str
    baseline: HostBenchmarkName
    status: HostBenchmarkStatus
    skip_reason: str | None = None
    metrics: HostBenchmarkMetrics | None = None
    finding_count: int = 0
    top_action_count: int = 0
    evidence_coverage_score: float = 0.0
    matrix: list[HostFindingMatrixRow] = Field(default_factory=list)
    findings_for_metrics: list[HostFinding] = Field(default_factory=list, exclude=True)


class HostBenchmarkBaselineResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: HostBenchmarkName
    status: HostBenchmarkStatus
    skip_reason: str | None = None
    fixture_count: int = 0
    skipped_fixture_count: int = 0
    metrics: HostBenchmarkMetrics | None = None
    fixtures: list[HostFixtureBenchmarkResult] = Field(default_factory=list)


class HostBenchmarkReport(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: Literal[1] = 1
    generated_at: str = Field(default_factory=lambda: datetime.now(UTC).isoformat())
    fixtures_root: str
    fixture_count: int
    primary_baseline: HostBenchmarkName = PRIMARY_BASELINE
    metrics: HostBenchmarkMetrics
    baselines: list[HostBenchmarkBaselineResult] = Field(default_factory=list)
    findings_matrix: list[HostFindingMatrixRow] = Field(default_factory=list)
    notes: list[str] = Field(default_factory=list)


def load_host_ground_truth(path: str | Path) -> HostGroundTruth:
    ground_truth_path = Path(path)
    payload = json.loads(ground_truth_path.read_text(encoding="utf-8"))
    return HostGroundTruth.model_validate(payload)


def discover_host_benchmark_fixtures(fixtures_root: str | Path) -> list[Path]:
    root = Path(fixtures_root)
    if not root.is_dir():
        raise HostInputError(f"host fixture root does not exist: {root}")
    return [
        path
        for path in sorted(root.iterdir(), key=lambda item: item.name)
        if path.is_dir() and (path / "ground_truth.json").is_file()
    ]


def matches_ground_truth(finding: HostFinding, matcher: HostGroundTruthMatcher) -> bool:
    if matcher.rule_id is not None and finding.rule_id != matcher.rule_id:
        return False
    if matcher.instance_key is not None and finding.instance_key != matcher.instance_key:
        return False
    if matcher.severity is not None and finding.severity != matcher.severity:
        return False
    if matcher.source_tool is not None and finding.source_tool != matcher.source_tool:
        return False
    if matcher.category is not None and finding.category != matcher.category:
        return False
    return (
        matcher.title_contains is None
        or matcher.title_contains.casefold() in finding.title.casefold()
    )


def evaluate_host_findings(
    *,
    fixture: str,
    target: str,
    baseline: HostBenchmarkName,
    ground_truth: HostGroundTruth,
    findings: list[HostFinding],
    report: HostPostureReport,
    top_action_count: int | None = None,
) -> HostFixtureBenchmarkResult:
    active_findings = [finding for finding in findings if not finding.suppressed]
    matrix: list[HostFindingMatrixRow] = []
    used_finding_ids: set[str] = set()

    for index, expected in enumerate(ground_truth.expected_findings):
        expected_id = _matcher_id(expected, index)
        matched = _best_matching_finding(active_findings, expected, used_finding_ids)
        if matched is None:
            matrix.append(
                _matrix_row(
                    fixture=fixture,
                    target=target,
                    baseline=baseline,
                    status="false_negative",
                    expected=expected,
                    expected_id=expected_id,
                    note=expected.note,
                )
            )
            continue
        used_finding_ids.add(matched.id)
        matrix.append(
            _matrix_row(
                fixture=fixture,
                target=target,
                baseline=baseline,
                status="true_positive",
                finding=matched,
                expected=expected,
                expected_id=expected_id,
                note=expected.note,
            )
        )

    expected_absent_violations = 0
    for finding in active_findings:
        if finding.id in used_finding_ids:
            continue
        allowed = _first_matching(ground_truth.allowed_extra, finding)
        if allowed is not None:
            matrix.append(
                _matrix_row(
                    fixture=fixture,
                    target=target,
                    baseline=baseline,
                    status="allowed_extra",
                    finding=finding,
                    expected=allowed,
                    expected_id=allowed.id,
                    note=allowed.note,
                )
            )
            continue
        if ground_truth.clean_fixture and finding.category == "coverage":
            matrix.append(
                _matrix_row(
                    fixture=fixture,
                    target=target,
                    baseline=baseline,
                    status="skipped",
                    finding=finding,
                    note="coverage-only finding on clean fixture",
                )
            )
            continue
        absent = _first_matching(ground_truth.expected_absent, finding)
        if absent is not None:
            expected_absent_violations += 1
            matrix.append(
                _matrix_row(
                    fixture=fixture,
                    target=target,
                    baseline=baseline,
                    status="expected_absent_violation",
                    finding=finding,
                    expected=absent,
                    expected_id=absent.id,
                    note=absent.note or "finding matched expected_absent",
                )
            )
            continue
        matrix.append(
            _matrix_row(
                fixture=fixture,
                target=target,
                baseline=baseline,
                status="false_positive",
                finding=finding,
            )
        )

    for index, absent in enumerate(ground_truth.expected_absent):
        if _first_matching(active_findings, absent) is not None:
            continue
        matrix.append(
            _matrix_row(
                fixture=fixture,
                target=target,
                baseline=baseline,
                status="expected_absent_pass",
                expected=absent,
                expected_id=_matcher_id(absent, index),
                note=absent.note,
            )
        )

    resolved_top_actions = (
        top_action_count if top_action_count is not None else len(report.top_actions)
    )
    metrics = _metrics_from_matrix(
        [matrix],
        fixture_reports=[report],
        fixture_findings=[active_findings],
        top_action_counts=[resolved_top_actions],
        evidence_scores=[_evidence_coverage_score(report)],
    )
    metrics = metrics.model_copy(
        update={
            "fixture_count": 1,
            "expected_absent_violations": expected_absent_violations,
        }
    )
    return HostFixtureBenchmarkResult(
        fixture=fixture,
        target=target,
        baseline=baseline,
        status="ok",
        metrics=metrics,
        finding_count=len(active_findings),
        top_action_count=resolved_top_actions,
        evidence_coverage_score=_evidence_coverage_score(report),
        matrix=matrix,
        findings_for_metrics=active_findings,
    )


def build_host_benchmark_report(
    fixtures_root: str | Path,
    *,
    include_llm: bool = False,
    llm_provider: LLMProvider | None = None,
    treat_private_as_public: bool = False,
) -> HostBenchmarkReport:
    root = Path(fixtures_root).resolve(strict=False)
    fixture_paths = discover_host_benchmark_fixtures(root)
    by_baseline: dict[HostBenchmarkName, list[HostFixtureBenchmarkResult]] = {
        baseline: [] for baseline in BASELINE_ORDER
    }
    findings_matrix: list[HostFindingMatrixRow] = []

    for fixture_path in fixture_paths:
        fixture_name = fixture_path.name
        ground_truth = load_host_ground_truth(fixture_path / "ground_truth.json")
        try:
            snapshot = load_host_input(fixture_path)
            deterministic_report = analyze_snapshot(
                snapshot,
                treat_private_as_public=treat_private_as_public,
            )
            llm_report = _llm_report(
                snapshot,
                include_llm=include_llm,
                provider=llm_provider,
                treat_private_as_public=treat_private_as_public,
            )
        except Exception as exc:
            target = fixture_name
            for baseline in BASELINE_ORDER:
                result = _skipped_fixture_result(
                    fixture=fixture_name,
                    target=target,
                    baseline=baseline,
                    reason=f"fixture failed to load or assess: {exc}",
                )
                by_baseline[baseline].append(result)
                findings_matrix.extend(result.matrix)
            continue

        target = deterministic_report.target
        for baseline in BASELINE_ORDER:
            selected = _select_baseline_findings(
                baseline=baseline,
                snapshot=snapshot,
                deterministic_report=deterministic_report,
                llm_report=llm_report,
                include_llm=include_llm,
                llm_provider=llm_provider,
            )
            if selected.skip_reason is not None:
                result = _skipped_fixture_result(
                    fixture=fixture_name,
                    target=target,
                    baseline=baseline,
                    reason=selected.skip_reason,
                )
            else:
                result = evaluate_host_findings(
                    fixture=fixture_name,
                    target=target,
                    baseline=baseline,
                    ground_truth=ground_truth,
                    findings=selected.findings,
                    report=selected.report,
                    top_action_count=selected.top_action_count,
                )
            by_baseline[baseline].append(result)
            findings_matrix.extend(result.matrix)

    baseline_results = [_baseline_result(name, results) for name, results in by_baseline.items()]
    primary = next(result for result in baseline_results if result.name == PRIMARY_BASELINE)
    metrics = primary.metrics or HostBenchmarkMetrics()
    return HostBenchmarkReport(
        fixtures_root=str(root),
        fixture_count=len(fixture_paths),
        metrics=metrics,
        baselines=baseline_results,
        findings_matrix=findings_matrix,
        notes=[
            "Triage-speed metrics are proxies, not measured analyst time.",
            (
                "LLM and baseline-tool comparisons are skipped unless local evidence "
                "or a provider is available."
            ),
        ],
    )


def render_host_benchmark_markdown(report: HostBenchmarkReport) -> str:
    metrics = report.metrics
    lines = [
        "# Piranesi Host Benchmark",
        "",
        f"- Generated: `{report.generated_at}`",
        f"- Fixtures root: `{report.fixtures_root}`",
        f"- Fixtures: **{report.fixture_count}**",
        f"- Primary baseline: `{report.primary_baseline}`",
        "",
        "> Triage-speed metrics in this report are proxies, not measured human analyst time.",
        "",
        "## Primary Metrics",
        "",
        f"- Expected issues: **{metrics.expected_issue_count}**",
        f"- Detected issues: **{metrics.detected_issue_count}**",
        f"- True positives: **{metrics.true_positives}**",
        f"- False positives: **{metrics.false_positives}**",
        f"- False negatives: **{metrics.false_negatives}**",
        f"- Precision: **{metrics.precision:.3f}**",
        f"- Recall: **{metrics.recall:.3f}**",
        f"- F1: **{metrics.f1:.3f}**",
        f"- Evidence coverage score: **{metrics.evidence_coverage_score:.3f}**",
        f"- Mean findings per host: **{metrics.mean_findings_per_host:.3f}**",
        f"- Mean top-action count: **{metrics.mean_top_action_count:.3f}**",
        f"- Time-to-triage proxy score: **{metrics.time_to_triage_proxy.score:.3f}**",
        "",
        "## Triage-Speed Proxy Inputs",
        "",
        f"- Label: {metrics.time_to_triage_proxy.label}",
        f"- Lower is better: `{str(metrics.time_to_triage_proxy.lower_is_better).lower()}`",
        (
            "- Remediation-present ratio: "
            f"**{metrics.time_to_triage_proxy.remediation_present_ratio:.3f}**"
        ),
        (
            "- Risk-score-present ratio: "
            f"**{metrics.time_to_triage_proxy.risk_score_present_ratio:.3f}**"
        ),
        (
            "- Mean report sections for top-three decisions: "
            f"**{metrics.time_to_triage_proxy.mean_report_sections_for_top_three_actions:.3f}**"
        ),
        "",
        "## Baselines",
        "",
        "| Baseline | Status | Fixtures | Skipped | Precision | Recall | F1 | Notes |",
        "| --- | --- | ---: | ---: | ---: | ---: | ---: | --- |",
    ]
    for baseline in report.baselines:
        if baseline.metrics is None:
            precision = recall = f1 = "n/a"
        else:
            precision = f"{baseline.metrics.precision:.3f}"
            recall = f"{baseline.metrics.recall:.3f}"
            f1 = f"{baseline.metrics.f1:.3f}"
        lines.append(
            "| "
            f"`{baseline.name}` | `{baseline.status}` | {baseline.fixture_count} | "
            f"{baseline.skipped_fixture_count} | {precision} | {recall} | {f1} | "
            f"{baseline.skip_reason or ''} |"
        )

    false_positives = [
        row
        for row in report.findings_matrix
        if row.baseline == report.primary_baseline
        and row.status in {"false_positive", "expected_absent_violation"}
    ]
    false_negatives = [
        row
        for row in report.findings_matrix
        if row.baseline == report.primary_baseline and row.status == "false_negative"
    ]
    lines.extend(["", "## Primary False Positives", ""])
    if not false_positives:
        lines.append("No primary-baseline false positives were recorded.")
    for row in false_positives:
        lines.append(
            f"- `{row.fixture}` `{row.rule_id or 'unknown'}` "
            f"{row.instance_key or ''} - {row.title or row.note or ''}".rstrip()
        )

    lines.extend(["", "## Primary False Negatives", ""])
    if not false_negatives:
        lines.append("No primary-baseline false negatives were recorded.")
    for row in false_negatives:
        lines.append(
            f"- `{row.fixture}` expected `{row.expected_rule_id or 'unknown'}` "
            f"{row.expected_instance_key or ''}".rstrip()
        )

    lines.extend(["", "## Notes", ""])
    for note in report.notes:
        lines.append(f"- {note}")
    return "\n".join(lines).rstrip() + "\n"


def write_host_benchmark_outputs(report: HostBenchmarkReport, output_dir: str | Path) -> None:
    path = Path(output_dir)
    path.mkdir(parents=True, exist_ok=True)
    (path / "host_benchmark.json").write_text(
        report.model_dump_json(indent=2),
        encoding="utf-8",
    )
    (path / "host_benchmark.md").write_text(
        render_host_benchmark_markdown(report),
        encoding="utf-8",
    )
    _write_findings_matrix_csv(report.findings_matrix, path / "findings_matrix.csv")


class _SelectedBaseline(BaseModel):
    model_config = ConfigDict(arbitrary_types_allowed=True)

    report: HostPostureReport
    findings: list[HostFinding]
    top_action_count: int
    skip_reason: str | None = None


def _llm_report(
    snapshot: HostSnapshot,
    *,
    include_llm: bool,
    provider: LLMProvider | None,
    treat_private_as_public: bool,
) -> HostPostureReport | None:
    if not include_llm or provider is None:
        return None
    return analyze_snapshot(
        snapshot,
        analysis="both",
        provider=provider,
        treat_private_as_public=treat_private_as_public,
    )


def _select_baseline_findings(
    *,
    baseline: HostBenchmarkName,
    snapshot: HostSnapshot,
    deterministic_report: HostPostureReport,
    llm_report: HostPostureReport | None,
    include_llm: bool,
    llm_provider: LLMProvider | None,
) -> _SelectedBaseline:
    if baseline == "piranesi_deterministic":
        return _SelectedBaseline(
            report=deterministic_report,
            findings=deterministic_report.findings,
            top_action_count=len(deterministic_report.top_actions),
        )
    if baseline == "piranesi_deterministic_llm":
        if not include_llm:
            return _skip_selected(
                deterministic_report,
                "LLM baseline disabled by default; rerun with --include-llm and credentials.",
            )
        if llm_provider is None or llm_report is None:
            return _skip_selected(
                deterministic_report,
                "LLM provider not configured.",
            )
        return _SelectedBaseline(
            report=llm_report,
            findings=llm_report.findings,
            top_action_count=len(llm_report.top_actions),
        )
    if baseline == "trivy_only":
        if "trivy" not in snapshot.raw_evidence:
            return _skip_selected(deterministic_report, "Trivy evidence is not present.")
        findings = [
            finding
            for finding in deterministic_report.findings
            if finding.source_tool == "trivy" or finding.rule_id == "host.cve.trivy"
        ]
        return _SelectedBaseline(
            report=deterministic_report,
            findings=findings,
            top_action_count=_proxy_top_action_count(findings),
        )
    if baseline == "lynis_only":
        if not any(check.source == "lynis" for check in snapshot.baseline_checks):
            return _skip_selected(deterministic_report, "Lynis evidence is not present.")
        findings = [
            finding for finding in deterministic_report.findings if finding.source_tool == "lynis"
        ]
        return _SelectedBaseline(
            report=deterministic_report,
            findings=findings,
            top_action_count=_proxy_top_action_count(findings),
        )
    if baseline == "openscap_only":
        if not any(check.source == "openscap" for check in snapshot.baseline_checks):
            return _skip_selected(deterministic_report, "OpenSCAP evidence is not present.")
        findings = [
            finding
            for finding in deterministic_report.findings
            if finding.source_tool == "openscap"
        ]
        return _SelectedBaseline(
            report=deterministic_report,
            findings=findings,
            top_action_count=_proxy_top_action_count(findings),
        )
    raise AssertionError(f"unhandled baseline: {baseline}")


def _skip_selected(report: HostPostureReport, reason: str) -> _SelectedBaseline:
    return _SelectedBaseline(report=report, findings=[], top_action_count=0, skip_reason=reason)


def _skipped_fixture_result(
    *,
    fixture: str,
    target: str,
    baseline: HostBenchmarkName,
    reason: str,
) -> HostFixtureBenchmarkResult:
    row = HostFindingMatrixRow(
        fixture=fixture,
        target=target,
        baseline=baseline,
        status="skipped",
        note=reason,
    )
    return HostFixtureBenchmarkResult(
        fixture=fixture,
        target=target,
        baseline=baseline,
        status="skipped",
        skip_reason=reason,
        matrix=[row],
    )


def _baseline_result(
    name: HostBenchmarkName,
    results: list[HostFixtureBenchmarkResult],
) -> HostBenchmarkBaselineResult:
    assessed = [result for result in results if result.status == "ok"]
    skipped = [result for result in results if result.status != "ok"]
    if not assessed:
        reason_counts = Counter(result.skip_reason or "skipped" for result in skipped)
        reason = "; ".join(f"{reason} ({count})" for reason, count in sorted(reason_counts.items()))
        return HostBenchmarkBaselineResult(
            name=name,
            status="skipped",
            skip_reason=reason or "no fixtures assessed",
            fixture_count=0,
            skipped_fixture_count=len(skipped),
            fixtures=results,
        )
    metrics = _metrics_from_matrix(
        [result.matrix for result in assessed],
        fixture_reports=[],
        fixture_findings=[],
        top_action_counts=[result.top_action_count for result in assessed],
        evidence_scores=[result.evidence_coverage_score for result in assessed],
        finding_counts=[result.finding_count for result in assessed],
        finding_sets=[result.findings_for_metrics for result in assessed],
    )
    return HostBenchmarkBaselineResult(
        name=name,
        status="ok",
        fixture_count=len(assessed),
        skipped_fixture_count=len(skipped),
        metrics=metrics,
        fixtures=results,
    )


def _metrics_from_matrix(
    matrix_groups: list[list[HostFindingMatrixRow]],
    *,
    fixture_reports: list[HostPostureReport],
    fixture_findings: list[list[HostFinding]],
    top_action_counts: list[int],
    evidence_scores: list[float],
    finding_counts: list[int] | None = None,
    finding_sets: list[list[HostFinding]] | None = None,
) -> HostBenchmarkMetrics:
    rows = [row for group in matrix_groups for row in group]
    true_positives = sum(1 for row in rows if row.status == "true_positive")
    false_positives = sum(
        1 for row in rows if row.status in {"false_positive", "expected_absent_violation"}
    )
    false_negatives = sum(1 for row in rows if row.status == "false_negative")
    allowed_extra = sum(1 for row in rows if row.status == "allowed_extra")
    skipped = sum(1 for row in rows if row.status == "skipped")
    expected_absent = sum(1 for row in rows if row.status == "expected_absent_pass")
    expected_absent_violations = sum(1 for row in rows if row.status == "expected_absent_violation")
    expected_issue_count = true_positives + false_negatives
    detected_issue_count = true_positives + false_positives
    precision = _precision(true_positives, false_positives, expected_issue_count)
    recall = _recall(true_positives, false_negatives)
    f1 = _f1(precision, recall)
    resolved_finding_counts = finding_counts
    if resolved_finding_counts is None:
        resolved_finding_counts = [len(findings) for findings in fixture_findings]
    resolved_finding_sets = finding_sets
    if resolved_finding_sets is None:
        resolved_finding_sets = fixture_findings
    triage_proxy = _triage_speed_proxy(
        finding_counts=resolved_finding_counts,
        top_action_counts=top_action_counts,
        evidence_scores=evidence_scores,
        finding_sets=resolved_finding_sets,
    )
    return HostBenchmarkMetrics(
        fixture_count=len(matrix_groups),
        expected_issue_count=expected_issue_count,
        detected_issue_count=detected_issue_count,
        true_positives=true_positives,
        false_positives=false_positives,
        false_negatives=false_negatives,
        expected_absent_count=expected_absent + expected_absent_violations,
        expected_absent_violations=expected_absent_violations,
        allowed_extra_count=allowed_extra,
        skipped_count=skipped,
        precision=precision,
        recall=recall,
        f1=f1,
        evidence_coverage_score=_mean(evidence_scores),
        mean_findings_per_host=_mean(resolved_finding_counts),
        mean_top_action_count=_mean(top_action_counts),
        time_to_triage_proxy=triage_proxy,
    )


def _triage_speed_proxy(
    *,
    finding_counts: list[int],
    top_action_counts: list[int],
    evidence_scores: list[float],
    finding_sets: list[list[HostFinding]],
) -> HostTriageSpeedProxy:
    all_findings = [finding for group in finding_sets for finding in group]
    remediation_ratio = _ratio(
        sum(1 for finding in all_findings if finding.remediation),
        len(all_findings),
        empty=1.0,
    )
    risk_ratio = _ratio(
        sum(1 for finding in all_findings if finding.risk is not None),
        len(all_findings),
        empty=1.0,
    )
    section_counts = [_report_sections_for_top_three_actions(group) for group in finding_sets]
    evidence = _mean(evidence_scores)
    mean_findings = _mean(finding_counts)
    mean_actions = _mean(top_action_counts)
    mean_sections = _mean(section_counts)
    score = max(
        0.0,
        mean_findings
        + (mean_actions * 2.0)
        + mean_sections
        + ((1.0 - evidence) * 5.0)
        - remediation_ratio
        - risk_ratio,
    )
    return HostTriageSpeedProxy(
        score=round(score, 3),
        mean_findings_after_dedup=round(mean_findings, 3),
        mean_top_action_count=round(mean_actions, 3),
        evidence_coverage_score=round(evidence, 3),
        remediation_present_ratio=round(remediation_ratio, 3),
        risk_score_present_ratio=round(risk_ratio, 3),
        mean_report_sections_for_top_three_actions=round(mean_sections, 3),
        proxy_inputs=[
            "findings after deduplication",
            "top action count",
            "evidence completeness",
            "findings with remediation present",
            "findings with risk score present",
            "report sections needed to decide top three actions",
        ],
    )


def _best_matching_finding(
    findings: list[HostFinding],
    matcher: HostGroundTruthMatcher,
    used_finding_ids: set[str],
) -> HostFinding | None:
    candidates = [
        finding
        for finding in findings
        if finding.id not in used_finding_ids and matches_ground_truth(finding, matcher)
    ]
    if not candidates:
        return None
    return sorted(
        candidates,
        key=lambda finding: (-_match_score(finding, matcher), finding.id),
    )[0]


def _first_matching(
    matchers_or_findings: list[HostGroundTruthMatcher] | list[HostFinding],
    finding_or_matcher: HostFinding | HostGroundTruthMatcher,
) -> HostGroundTruthMatcher | HostFinding | None:
    if isinstance(finding_or_matcher, HostFinding):
        for matcher in matchers_or_findings:
            if isinstance(matcher, HostGroundTruthMatcher) and matches_ground_truth(
                finding_or_matcher,
                matcher,
            ):
                return matcher
        return None
    for finding in matchers_or_findings:
        if isinstance(finding, HostFinding) and matches_ground_truth(
            finding,
            finding_or_matcher,
        ):
            return finding
    return None


def _match_score(finding: HostFinding, matcher: HostGroundTruthMatcher) -> int:
    score = 0
    if matcher.rule_id is not None and finding.rule_id == matcher.rule_id:
        score += 10
    if matcher.instance_key is not None and finding.instance_key == matcher.instance_key:
        score += 10
    if (
        matcher.title_contains is not None
        and matcher.title_contains.casefold() in finding.title.casefold()
    ):
        score += 3
    if matcher.severity is not None and finding.severity == matcher.severity:
        score += 1
    if matcher.source_tool is not None and finding.source_tool == matcher.source_tool:
        score += 1
    if matcher.category is not None and finding.category == matcher.category:
        score += 1
    return score


def _matcher_id(matcher: HostGroundTruthMatcher, index: int) -> str:
    if matcher.id:
        return matcher.id
    parts = [
        matcher.rule_id or "*",
        matcher.instance_key or "*",
        matcher.title_contains or "*",
        str(index),
    ]
    return "::".join(parts)


def _matrix_row(
    *,
    fixture: str,
    target: str,
    baseline: HostBenchmarkName,
    status: FindingMatrixStatus,
    finding: HostFinding | None = None,
    expected: HostGroundTruthMatcher | None = None,
    expected_id: str | None = None,
    note: str | None = None,
) -> HostFindingMatrixRow:
    return HostFindingMatrixRow(
        fixture=fixture,
        target=target,
        baseline=baseline,
        status=status,
        finding_id=finding.id if finding is not None else None,
        rule_id=finding.rule_id if finding is not None else None,
        instance_key=finding.instance_key if finding is not None else None,
        severity=(
            finding.severity if finding is not None else expected.severity if expected else None
        ),
        title=(
            finding.title if finding is not None else expected.title_contains if expected else None
        ),
        source_tool=(
            finding.source_tool
            if finding is not None
            else expected.source_tool
            if expected
            else None
        ),
        expected_id=expected_id,
        expected_rule_id=expected.rule_id if expected is not None else None,
        expected_instance_key=expected.instance_key if expected is not None else None,
        note=note,
    )


def _precision(true_positives: int, false_positives: int, expected_issue_count: int) -> float:
    denominator = true_positives + false_positives
    if denominator == 0:
        return 1.0 if expected_issue_count == 0 else 0.0
    return true_positives / denominator


def _recall(true_positives: int, false_negatives: int) -> float:
    denominator = true_positives + false_negatives
    if denominator == 0:
        return 1.0
    return true_positives / denominator


def _f1(precision: float, recall: float) -> float:
    if precision + recall == 0:
        return 0.0
    return 2 * precision * recall / (precision + recall)


def _ratio(numerator: int, denominator: int, *, empty: float) -> float:
    if denominator == 0:
        return empty
    return numerator / denominator


def _mean(values: list[int] | list[float]) -> float:
    if not values:
        return 0.0
    return float(mean(values))


def _evidence_coverage_score(report: HostPostureReport) -> float:
    completeness = report.host_metadata.get("evidence_completeness")
    if isinstance(completeness, dict) and completeness:
        values = [bool(value) for value in completeness.values()]
        return sum(1 for value in values if value) / len(values)
    inventory = report.evidence_inventory
    if not inventory:
        return 0.0
    expected = {"packages", "listening_ports", "users"}
    present = sum(1 for key in expected if int(inventory.get(key, 0)) > 0)
    return present / len(expected)


def _proxy_top_action_count(findings: list[HostFinding]) -> int:
    if not findings:
        return 0
    return min(3, len({finding.category for finding in findings}))


def _report_sections_for_top_three_actions(findings: list[HostFinding]) -> int:
    sections = {"summary", "findings"}
    if findings:
        sections.add("evidence")
        sections.add("remediation")
    if any(finding.risk is not None for finding in findings):
        sections.add("risk")
    return len(sections)


def _write_findings_matrix_csv(rows: list[HostFindingMatrixRow], path: Path) -> None:
    fieldnames = list(HostFindingMatrixRow.model_fields)
    with path.open("w", encoding="utf-8", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=fieldnames)
        writer.writeheader()
        for row in rows:
            writer.writerow(row.model_dump(mode="json"))
