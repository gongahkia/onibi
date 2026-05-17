from __future__ import annotations

import io
import re
from collections.abc import Iterable, Mapping, Sequence
from functools import lru_cache

from pydantic import BaseModel, ConfigDict, Field
from rich import box
from rich.console import Console
from rich.table import Table

from piranesi.legal.frameworks import FRAMEWORK_BY_KEY, FRAMEWORKS, FrameworkSpec
from piranesi.legal.rules import RegulatoryRuleSpec, load_all_rule_specs

_SEVERITY_RANK = {"critical": 3, "high": 2, "medium": 1, "low": 0, "informational": -1}
_MATURITY_LABELS = {
    0: "None",
    1: "Initial",
    2: "Developing",
    3: "Defined",
    4: "Managed",
    5: "Optimizing",
}
_CVE_PATTERN = re.compile(r"CVE-\d{4}-\d+", re.IGNORECASE)
_CWE_PATTERN = re.compile(r"CWE-\d+", re.IGNORECASE)
_META_ONLY_FACTS = {
    "cwe_classified",
    "dependency_scan_executed",
    "owasp_coverage_gap",
    "patch_generated",
    "repeated_cwe_pattern",
    "scan_executed",
    "secure_sdl_gap",
    "severity_assigned",
}


class TrendSignals(BaseModel):
    model_config = ConfigDict(extra="forbid")

    has_baseline: bool = False
    has_trend: bool = False
    scan_count: int = 0
    avg_fix_rate: float = 0.0


class FrameworkMaturityAssessment(BaseModel):
    model_config = ConfigDict(extra="forbid")

    framework: str
    short_label: str
    long_label: str
    score: int
    label: str
    relevant_findings: int
    critical_findings: int
    high_findings: int
    finding_ids: list[str] = Field(default_factory=list)


class ComplianceMaturityAssessment(BaseModel):
    model_config = ConfigDict(extra="forbid")

    assessments: list[FrameworkMaturityAssessment] = Field(default_factory=list)
    overall_score: float = 0.0
    overall_label: str = "None"
    regressions: list[str] = Field(default_factory=list)


class ComplianceMaturityHistory(BaseModel):
    model_config = ConfigDict(extra="forbid")

    scan_dates: list[str] = Field(default_factory=list)
    by_framework: dict[str, list[int]] = Field(default_factory=dict)
    regressions: list[str] = Field(default_factory=list)


class _FindingContext(BaseModel):
    model_config = ConfigDict(extra="forbid")

    finding_id: str
    cwe: str
    severity: str
    metadata: dict[str, object] = Field(default_factory=dict)
    related_cves: list[str] = Field(default_factory=list)
    frameworks_from_obligations: list[str] = Field(default_factory=list)
    data_categories: list[str] = Field(default_factory=list)


@lru_cache(maxsize=1)
def _framework_rule_specs() -> dict[str, tuple[RegulatoryRuleSpec, ...]]:
    grouped: dict[str, list[RegulatoryRuleSpec]] = {framework.key: [] for framework in FRAMEWORKS}
    for spec in load_all_rule_specs():
        if spec.framework in grouped:
            grouped[spec.framework].append(spec)
    return {key: tuple(specs) for key, specs in grouped.items()}


def framework_keys_for_finding(
    finding: object,
    *,
    include_meta: bool = False,
) -> set[str]:
    context = _normalize_finding(finding)
    matched = set(context.frameworks_from_obligations)
    for framework in FRAMEWORKS:
        if framework.key in matched:
            continue
        if any(
            _match_rule_spec_directly(spec, context, include_meta=include_meta)
            for spec in _framework_rule_specs().get(framework.key, ())
        ):
            matched.add(framework.key)
    return matched


def compute_maturity_level(
    *,
    scan_active: bool,
    critical_findings: int,
    high_findings: int,
    trend_signals: TrendSignals | None = None,
    incremental: bool = False,
    suppressed_ratio: float = 0.0,
) -> int:
    if not scan_active:
        return 0

    trend = trend_signals or TrendSignals()
    if critical_findings > 10:
        score = 1
    elif critical_findings == 0 and high_findings == 0 and incremental:
        score = 5
    elif (
        critical_findings == 0
        and high_findings <= 5
        and trend.has_baseline
        and trend.has_trend
        and trend.avg_fix_rate > 0
    ):
        score = 4
    elif critical_findings <= 5 and trend.has_baseline and trend.has_trend:
        score = 3
    elif critical_findings <= 10:
        score = 2
    else:
        score = 1

    if suppressed_ratio >= 0.25 and score > 3:
        return 3
    return score


def assess_report_maturity(
    report: object,
    *,
    trend_signals: TrendSignals | None = None,
    framework_keys: Sequence[str] | None = None,
    incremental: bool = False,
) -> ComplianceMaturityAssessment:
    findings = _report_findings(report)
    summary = getattr(report, "executive_summary", None)
    findings_detected = int(getattr(summary, "findings_detected", 0) or 0)
    suppressed_findings = int(getattr(summary, "suppressed_findings", 0) or 0)
    suppressed_ratio = suppressed_findings / findings_detected if findings_detected > 0 else 0.0

    selected_frameworks = [
        FRAMEWORK_BY_KEY[key]
        for key in (framework_keys or tuple(framework.key for framework in FRAMEWORKS))
        if key in FRAMEWORK_BY_KEY
    ]
    assessments = [
        assess_framework_maturity(
            framework,
            findings,
            trend_signals=trend_signals,
            incremental=incremental,
            suppressed_ratio=suppressed_ratio,
            scan_active=True,
        )
        for framework in selected_frameworks
    ]
    overall_score = (
        round(sum(item.score for item in assessments) / len(assessments), 2) if assessments else 0.0
    )
    overall_label = _MATURITY_LABELS.get(int(overall_score), "None")
    return ComplianceMaturityAssessment(
        assessments=assessments,
        overall_score=overall_score,
        overall_label=overall_label,
    )


def assess_framework_maturity(
    framework: FrameworkSpec | str,
    findings: Sequence[object],
    *,
    trend_signals: TrendSignals | None = None,
    incremental: bool = False,
    suppressed_ratio: float = 0.0,
    scan_active: bool = True,
) -> FrameworkMaturityAssessment:
    framework_spec = FRAMEWORK_BY_KEY[framework] if isinstance(framework, str) else framework
    matched_contexts = [
        context
        for context in (_normalize_finding(finding) for finding in findings)
        if framework_spec.key in framework_keys_for_finding(context)
    ]
    critical = sum(1 for context in matched_contexts if context.severity == "critical")
    high = sum(1 for context in matched_contexts if context.severity == "high")
    score = compute_maturity_level(
        scan_active=scan_active,
        critical_findings=critical,
        high_findings=high,
        trend_signals=trend_signals,
        incremental=incremental,
        suppressed_ratio=suppressed_ratio,
    )
    return FrameworkMaturityAssessment(
        framework=framework_spec.key,
        short_label=framework_spec.short_label,
        long_label=framework_spec.long_label,
        score=score,
        label=_MATURITY_LABELS[score],
        relevant_findings=len(matched_contexts),
        critical_findings=critical,
        high_findings=high,
        finding_ids=sorted({context.finding_id for context in matched_contexts}),
    )


def build_maturity_history(
    *,
    scan_dates: Sequence[str],
    by_framework: Mapping[str, Sequence[int]],
) -> ComplianceMaturityHistory:
    regressions: list[str] = []
    for framework_key, scores in by_framework.items():
        for index in range(1, len(scores)):
            previous = scores[index - 1]
            current = scores[index]
            if current < previous and index < len(scan_dates):
                regressions.append(
                    f"{scan_dates[index]}: {framework_key} maturity regressed "
                    f"{previous} -> {current}"
                )
    return ComplianceMaturityHistory(
        scan_dates=list(scan_dates),
        by_framework={key: list(values) for key, values in by_framework.items()},
        regressions=regressions,
    )


def render_maturity_assessment(assessment: ComplianceMaturityAssessment) -> str:
    buffer = io.StringIO()
    console = Console(file=buffer, force_terminal=False, color_system=None)
    console.print("[bold]Compliance Maturity Assessment[/bold]")
    table = Table(box=box.ROUNDED)
    table.add_column("Framework", style="bold")
    table.add_column("Level", justify="center")
    table.add_column("Label")
    table.add_column("Critical", justify="right")
    table.add_column("High", justify="right")
    table.add_column("Findings", justify="right")
    for item in assessment.assessments:
        table.add_row(
            item.short_label,
            _score_bar(item.score),
            f"{item.label} ({item.score}/5)",
            str(item.critical_findings),
            str(item.high_findings),
            str(item.relevant_findings),
        )
    console.print(table)
    console.print(
        f"[bold]Overall:[/bold] {_score_bar(round(assessment.overall_score))} "
        f"{assessment.overall_score:.2f}/5.0 ({assessment.overall_label})"
    )
    if assessment.regressions:
        console.print("")
        console.print("[bold]Regressions[/bold]")
        for regression in assessment.regressions:
            console.print(f"- {regression}")
    return buffer.getvalue()


def render_maturity_history(history: ComplianceMaturityHistory) -> str:
    buffer = io.StringIO()
    console = Console(file=buffer, force_terminal=False, color_system=None)
    console.print("[bold]Compliance Maturity History[/bold]")
    table = Table(box=box.ROUNDED)
    table.add_column("Framework", style="bold")
    table.add_column("Progression")
    table.add_column("Latest", justify="right")
    for framework in FRAMEWORKS:
        scores = list(history.by_framework.get(framework.key, ()))
        if not scores:
            continue
        table.add_row(
            framework.short_label,
            " -> ".join(str(score) for score in scores),
            f"{scores[-1]}/5",
        )
    console.print(table)
    if history.regressions:
        console.print("")
        console.print("[bold]Regressions[/bold]")
        for regression in history.regressions:
            console.print(f"- {regression}")
    return buffer.getvalue()


def _report_findings(report: object) -> list[object]:
    combined = list(getattr(report, "findings", []) or [])
    active = list(getattr(report, "active_findings", []) or [])
    deduped: dict[str, object] = {}
    for finding in combined:
        finding_id = str(getattr(finding, "finding_id", ""))
        if finding_id:
            deduped[finding_id] = finding
    for finding in active:
        finding_id = str(getattr(finding, "finding_id", ""))
        if finding_id and finding_id not in deduped:
            deduped[finding_id] = finding
    return list(deduped.values())


def _normalize_finding(finding: object) -> _FindingContext:
    if isinstance(finding, _FindingContext):
        return finding

    finding_id = str(getattr(finding, "finding_id", getattr(finding, "id", "unknown")))
    raw_cwe = getattr(finding, "cwe", getattr(finding, "vuln_class", ""))
    cwe = _extract_cwe_id(str(raw_cwe))
    severity = _normalize_severity(str(getattr(finding, "severity", "medium")))
    raw_metadata = getattr(finding, "metadata", {})
    metadata = dict(raw_metadata) if isinstance(raw_metadata, Mapping) else {}
    raw_cves = getattr(finding, "related_cves", [])
    related_cves = [str(item) for item in raw_cves] if isinstance(raw_cves, Iterable) else []
    obligations = getattr(finding, "regulatory_obligations", [])
    frameworks_from_obligations = [
        str(obligation.framework)
        for obligation in obligations
        if getattr(obligation, "framework", None)
    ]
    data_categories: list[str] = []
    source = getattr(finding, "source", None)
    if source is not None:
        raw_categories = getattr(source, "data_categories", [])
        if isinstance(raw_categories, Iterable):
            data_categories = [str(category).strip().lower() for category in raw_categories]

    return _FindingContext(
        finding_id=finding_id,
        cwe=cwe,
        severity=severity,
        metadata=metadata,
        related_cves=related_cves,
        frameworks_from_obligations=frameworks_from_obligations,
        data_categories=data_categories,
    )


def _match_rule_spec_directly(
    rule_spec: RegulatoryRuleSpec,
    finding: object,
    *,
    include_meta: bool,
) -> bool:
    context = _normalize_finding(finding)
    if not include_meta and _is_meta_only_rule(rule_spec):
        return False
    if rule_spec.requires_rule_ids or rule_spec.affected_individuals_gte is not None:
        return False
    if rule_spec.vuln_classes and context.cwe not in set(rule_spec.vuln_classes):
        return False
    if rule_spec.data_categories and not set(context.data_categories) & {
        value.strip().lower() for value in rule_spec.data_categories
    }:
        return False
    boolean_facts = _derived_boolean_facts(context, include_meta=include_meta)
    if any(not boolean_facts.get(name, False) for name in rule_spec.requires_boolean_facts):
        return False
    if rule_spec.requires_any_boolean_facts and not any(
        boolean_facts.get(name, False) for name in rule_spec.requires_any_boolean_facts
    ):
        return False
    return not (
        not rule_spec.vuln_classes
        and not rule_spec.requires_boolean_facts
        and not rule_spec.requires_any_boolean_facts
        and not rule_spec.data_categories
    )


def _derived_boolean_facts(
    context: _FindingContext,
    *,
    include_meta: bool,
) -> dict[str, bool]:
    has_cve = any(_CVE_PATTERN.search(value) for value in context.related_cves)
    if not has_cve:
        for key in ("cve_id", "advisory_id"):
            raw_value = context.metadata.get(key)
            if isinstance(raw_value, str) and _CVE_PATTERN.search(raw_value):
                has_cve = True
                break
    outdated_dependency = any(
        isinstance(context.metadata.get(key), str) and bool(str(context.metadata.get(key)).strip())
        for key in ("patched_version", "fixed_versions")
    )
    dependency_finding = context.cwe == "CWE-1395" or isinstance(
        context.metadata.get("package"), str
    )
    facts = {
        "dependency_outdated": outdated_dependency,
        "has_cve": has_cve,
        "has_known_cve": has_cve,
        "outdated_dependency": outdated_dependency,
        "severity_critical": context.severity == "critical",
        "severity_high_or_above": context.severity in {"high", "critical"},
    }
    if include_meta:
        facts.update(
            {
                "cwe_classified": context.cwe.startswith("CWE-"),
                "dependency_scan_executed": dependency_finding,
                "scan_executed": True,
                "severity_assigned": bool(context.severity),
            }
        )
    return facts


def _is_meta_only_rule(rule_spec: RegulatoryRuleSpec) -> bool:
    required_flags = set(rule_spec.requires_boolean_facts) | set(
        rule_spec.requires_any_boolean_facts
    )
    return (
        not rule_spec.vuln_classes
        and not rule_spec.data_categories
        and not rule_spec.requires_rule_ids
        and rule_spec.affected_individuals_gte is None
        and bool(required_flags)
        and required_flags <= _META_ONLY_FACTS
    )


def _normalize_severity(value: str) -> str:
    normalized = value.strip().lower()
    return normalized if normalized in _SEVERITY_RANK else "medium"


def _extract_cwe_id(value: str) -> str:
    match = _CWE_PATTERN.search(value)
    if match is None:
        return value
    return match.group(0).upper()


def _score_bar(score: int) -> str:
    bounded = max(0, min(score, 5))
    return ("█" * bounded) + ("░" * (5 - bounded))


__all__ = [
    "ComplianceMaturityAssessment",
    "ComplianceMaturityHistory",
    "FrameworkMaturityAssessment",
    "TrendSignals",
    "assess_framework_maturity",
    "assess_report_maturity",
    "build_maturity_history",
    "compute_maturity_level",
    "framework_keys_for_finding",
    "render_maturity_assessment",
    "render_maturity_history",
]
