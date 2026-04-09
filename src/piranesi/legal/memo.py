from __future__ import annotations

import re
from collections import defaultdict
from collections.abc import Iterable, Sequence

from piranesi.legal.engine import ForwardChainingEngine
from piranesi.legal.rules import (
    RegulatoryRuleSpec,
    add_finding_facts,
    load_mas_trm_rule_specs,
    load_mas_trm_rules,
    load_pdpa_rule_specs,
    load_pdpa_rules,
    pdpa_thresholds,
    query_consequences,
    query_obligations,
)
from piranesi.legal.taxonomy import classify_field, tier_for_category
from piranesi.models.finding import CandidateFinding, ConfirmedFinding
from piranesi.models.legal import LegalAssessment, RegulatoryObligation

DISCLAIMER_TEXT = (
    "DISCLAIMER: This analysis is informational only. It is not legal advice. "
    "Consult qualified legal counsel for regulatory compliance decisions."
)

_CWE_PATTERN = re.compile(r"CWE-\d+", re.IGNORECASE)
_SEVERITY_ORDER = {"LOW": 0, "MEDIUM": 1, "HIGH": 2, "CRITICAL": 3}
_KNOWN_CATEGORY_ALIASES = {"public": "public_info"}
_FRAMEWORK_LABELS = {
    "PDPA": "Personal Data Protection Act 2012 (PDPA)",
    "MAS_TRM": "MAS Technology Risk Management Guidelines (MAS TRM)",
}
_FRAMEWORK_ORDER = {"PDPA": 0, "MAS_TRM": 1}
_VULNERABILITY_LABELS = {
    "CWE-22": "Path Traversal",
    "CWE-78": "Command Injection",
    "CWE-79": "Cross-Site Scripting",
    "CWE-89": "SQL Injection",
}
_CONSEQUENCE_LABELS = {
    "document": "Document the finding, remediation status, and regulatory assessment for compliance records.",
    "notify_individuals": "Prepare communications for affected individuals if the breach assessment confirms individual notification is required.",
    "review": "Review adjacent systems and controls for the same vulnerability pattern and strengthen preventive controls.",
}


def build_default_engine() -> ForwardChainingEngine:
    engine = ForwardChainingEngine()
    register_default_rules(engine)
    return engine


def register_default_rules(engine: ForwardChainingEngine) -> None:
    for rule in [*load_pdpa_rules(), *load_mas_trm_rules()]:
        engine.add_rule(rule)


def assess_finding(
    finding: ConfirmedFinding,
    engine: ForwardChainingEngine,
) -> LegalAssessment:
    candidate = finding.finding.finding
    finding_id = candidate.id
    vuln_class = _normalize_vuln_class(candidate.vuln_class)
    severity = _normalize_severity(candidate.severity)
    data_categories = _extract_data_categories(candidate)
    boolean_facts = _extract_boolean_facts(candidate)

    add_finding_facts(
        engine,
        finding_id=finding_id,
        vuln_class=vuln_class,
        data_categories=data_categories,
        severity=severity,
        affected_individuals=candidate.affected_individuals_estimate,
        boolean_facts=boolean_facts,
        thresholds=pdpa_thresholds(),
    )
    engine.run()

    rule_specs = _load_rule_catalog()
    obligation_facts = query_obligations(engine, finding_id=finding_id)
    consequence_facts = query_consequences(engine, finding_id=finding_id)
    obligations = [
        _obligation_from_fact(
            fact,
            finding_categories=data_categories,
            rule_specs=rule_specs,
        )
        for fact in obligation_facts
    ]
    risk_tier = _determine_risk_tier(
        severity=severity,
        data_categories=data_categories,
        obligations=obligations,
    )

    assessment = LegalAssessment(
        finding=finding,
        obligations=obligations,
        risk_tier=risk_tier,
        memo_markdown="",
    )
    memo_markdown = render_legal_memo(
        finding=finding,
        assessment=assessment,
        consequence_facts=consequence_facts,
    )
    return assessment.model_copy(update={"memo_markdown": memo_markdown})


def render_legal_memo(
    *,
    finding: ConfirmedFinding,
    assessment: LegalAssessment,
    consequence_facts: Sequence[object] | None = None,
) -> str:
    candidate = finding.finding.finding
    vuln_class = _normalize_vuln_class(candidate.vuln_class)
    frameworks = _frameworks_triggered(assessment.obligations)
    grouped = _group_by_framework(assessment.obligations)
    lines = [
        "# Regulatory Impact Assessment",
        "",
        DISCLAIMER_TEXT,
        "",
        "## Finding Reference",
        "",
        "| Field | Value |",
        "|---|---|",
        f"| Finding ID | {candidate.id} |",
        f"| Vulnerability | {_format_vulnerability(vuln_class)} |",
        f"| Location | {_format_location(candidate)} |",
        f"| Data Categories | {_format_data_categories(_extract_data_categories(candidate))} |",
        f"| Severity | {_normalize_severity(candidate.severity)} |",
        f"| Exploit Confirmation | {_format_confirmation(finding)} |",
    ]
    if candidate.affected_individuals_estimate is not None:
        lines.append(
            f"| Estimated Affected Individuals | {candidate.affected_individuals_estimate} |"
        )

    lines.extend(
        [
            "",
            "## Regulatory Frameworks",
            "",
        ]
    )
    if frameworks:
        lines.extend(f"- {_framework_label(framework)}" for framework in frameworks)
    else:
        lines.append("- No encoded regulatory frameworks were triggered by the asserted facts.")

    for framework in frameworks:
        lines.extend(["", f"## {_framework_label(framework)}", ""])
        for obligation in grouped[framework]:
            lines.extend(_render_obligation_section(obligation))

    lines.extend(
        [
            "",
            "## Risk Assessment",
            "",
            f"**Overall risk:** {assessment.risk_tier}",
        ]
    )
    for item in _risk_assessment_points(
        finding=finding,
        frameworks=frameworks,
        obligations=assessment.obligations,
    ):
        lines.append(f"- {item}")

    lines.extend(["", "## Recommended Actions", ""])
    for index, action in enumerate(
        _recommended_actions(
            finding=finding,
            obligations=assessment.obligations,
            consequence_facts=consequence_facts,
        ),
        start=1,
    ):
        lines.append(f"{index}. {action}")

    return "\n".join(lines)


def _render_obligation_section(obligation: RegulatoryObligation) -> list[str]:
    title = obligation.section
    if obligation.severity_modifier:
        title = f"{title} ({obligation.severity_modifier.upper()})"

    lines = [f"### {title}", ""]
    if obligation.rule_id:
        lines.append(f"**Rule ID:** `{obligation.rule_id}`")
        lines.append("")
    lines.extend(
        [
            f"**Obligation text:** {obligation.obligation_text}",
            "",
            f"**Data categories:** {_format_data_categories(obligation.data_categories_affected)}",
            "",
            f"**Penalty range:** {obligation.penalty_range}",
            "",
            "**Notification timeline:** "
            f"{obligation.notification_timeline or 'Not specified by the triggered rule.'}",
            "",
            "**Enforcement precedents:**",
        ]
    )
    if obligation.enforcement_precedents:
        lines.extend(f"- {precedent}" for precedent in obligation.enforcement_precedents)
    else:
        lines.append("- None specified in the current rule set.")
    return [*lines, ""]


def _obligation_from_fact(
    fact: object,
    *,
    finding_categories: Sequence[str],
    rule_specs: dict[str, RegulatoryRuleSpec],
) -> RegulatoryObligation:
    args = getattr(fact, "args")
    rule_id = _string_or_none(args.get("rule_id"))
    rule_spec = rule_specs.get(rule_id or "")
    relevant_categories = _relevant_categories(
        finding_categories,
        rule_spec.data_categories if rule_spec is not None else (),
    )
    return RegulatoryObligation(
        framework=str(args["framework"]),
        section=str(args["section"]),
        obligation_text=str(args["obligation_text"]),
        data_categories_affected=relevant_categories,
        penalty_range=str(args["penalty_range"]),
        notification_timeline=_string_or_none(args.get("notification_timeline")),
        enforcement_precedents=_string_list(args.get("enforcement_precedents")),
        rule_id=rule_id,
        consequences=_string_list(args.get("consequences")),
        severity_modifier=_string_or_none(args.get("severity_modifier")),
    )


def _determine_risk_tier(
    *,
    severity: str,
    data_categories: Sequence[str],
    obligations: Sequence[RegulatoryObligation],
) -> str:
    score = _SEVERITY_ORDER.get(severity, 1)
    if data_categories and _highest_sensitivity_tier(data_categories) == 1:
        score += 1
    if any(obligation.notification_timeline for obligation in obligations):
        score += 1
    if len(_frameworks_triggered(obligations)) > 1:
        score += 1

    bounded = max(0, min(score, 3))
    return ("LOW", "MEDIUM", "HIGH", "CRITICAL")[bounded]


def _risk_assessment_points(
    *,
    finding: ConfirmedFinding,
    frameworks: Sequence[str],
    obligations: Sequence[RegulatoryObligation],
) -> list[str]:
    candidate = finding.finding.finding
    items = [
        f"Confirmed finding severity is {_normalize_severity(candidate.severity)}.",
        f"Impacted data categories: {_format_data_categories(_extract_data_categories(candidate))}.",
    ]
    tier = _highest_sensitivity_tier(_extract_data_categories(candidate))
    items.append(f"Highest detected personal-data sensitivity tier: Tier {tier}.")
    if any(obligation.notification_timeline for obligation in obligations):
        items.append("A mandatory or time-bound notification obligation was triggered.")
    if len(frameworks) > 1:
        items.append(
            "The finding creates exposure under multiple encoded regulatory frameworks."
        )
    if candidate.affected_individuals_estimate is not None:
        items.append(
            f"Estimated affected individuals: {candidate.affected_individuals_estimate}."
        )
    return items


def _recommended_actions(
    *,
    finding: ConfirmedFinding,
    obligations: Sequence[RegulatoryObligation],
    consequence_facts: Sequence[object] | None,
) -> list[str]:
    candidate = finding.finding.finding
    actions: list[str] = [
        f"Immediate: remediate the {_format_vulnerability(_normalize_vuln_class(candidate.vuln_class))} path at {_format_location(candidate)} and verify the exploit is closed.",
    ]
    seen = {actions[0]}

    timelines = sorted(
        {
            obligation.notification_timeline
            for obligation in obligations
            if obligation.notification_timeline
        }
    )
    for timeline in timelines:
        action = (
            f"Within {timeline}: assess breach-notification duties and prepare the required "
            "regulator and individual notifications."
        )
        if action not in seen:
            actions.append(action)
            seen.add(action)

    consequence_actions = sorted(
        {
            str(getattr(fact, "args")["action"])
            for fact in (consequence_facts or [])
            if "action" in getattr(fact, "args")
        },
        key=_consequence_priority,
    )
    if not consequence_actions:
        consequence_actions = sorted(
            {action for obligation in obligations for action in obligation.consequences},
            key=_consequence_priority,
        )

    has_timeline = bool(timelines)
    for consequence in consequence_actions:
        if consequence in {"notify_regulator", "remediate"}:
            continue
        text: str | None
        if consequence == "notify_individuals" and has_timeline:
            text = (
                "Within the same notification window: determine whether affected individuals "
                "must be notified and prepare the communication package."
            )
        else:
            text = _CONSEQUENCE_LABELS.get(consequence)
        if text is not None and text not in seen:
            actions.append(text)
            seen.add(text)

    return actions


def _extract_data_categories(candidate: CandidateFinding) -> list[str]:
    categories: list[str] = []
    for raw_value in candidate.source.data_categories:
        categories.extend(_normalize_category_value(raw_value))

    if not categories:
        fallback_inputs = [
            candidate.source.parameter_name or "",
            candidate.source.source_type,
        ]
        for raw_value in fallback_inputs:
            categories.extend(_normalize_category_value(raw_value))

    deduped: list[str] = []
    for category in categories:
        if category not in deduped:
            deduped.append(category)
    return deduped


def _normalize_category_value(raw_value: str) -> list[str]:
    normalized = raw_value.strip().lower().replace("-", "_").replace(" ", "_")
    if not normalized:
        return []

    canonical = _KNOWN_CATEGORY_ALIASES.get(normalized, normalized)
    try:
        tier_for_category(canonical)
    except ValueError:
        return [
            category
            for category in classify_field(raw_value)
            if _is_known_category(category)
        ]
    return [canonical]


def _extract_boolean_facts(candidate: CandidateFinding) -> dict[str, bool]:
    return {
        "cross_border": candidate.cross_border,
        "is_high_risk_ai": candidate.is_high_risk_ai,
        "no_encryption_at_rest": candidate.no_encryption_at_rest,
        "third_party_processor": candidate.third_party_processor,
    }


def _normalize_vuln_class(value: str) -> str:
    match = _CWE_PATTERN.search(value)
    if match is not None:
        return match.group(0).upper()
    return value.strip().upper()


def _normalize_severity(value: str) -> str:
    normalized = value.strip().upper()
    if normalized in _SEVERITY_ORDER:
        return normalized
    return "MEDIUM"


def _relevant_categories(
    finding_categories: Sequence[str],
    rule_categories: Iterable[str],
) -> list[str]:
    rule_category_set = {category.strip().lower() for category in rule_categories}
    matched = [category for category in finding_categories if category in rule_category_set]
    return matched or list(finding_categories)


def _frameworks_triggered(obligations: Sequence[RegulatoryObligation]) -> list[str]:
    return sorted(
        {obligation.framework for obligation in obligations},
        key=lambda framework: (_FRAMEWORK_ORDER.get(framework, 99), framework),
    )


def _group_by_framework(
    obligations: Sequence[RegulatoryObligation],
) -> dict[str, list[RegulatoryObligation]]:
    grouped: dict[str, list[RegulatoryObligation]] = defaultdict(list)
    for obligation in obligations:
        grouped[obligation.framework].append(obligation)
    for framework, framework_obligations in grouped.items():
        grouped[framework] = sorted(
            framework_obligations,
            key=lambda item: (
                item.section,
                item.rule_id or "",
                item.obligation_text,
            ),
        )
    return dict(grouped)


def _highest_sensitivity_tier(data_categories: Sequence[str]) -> int:
    if not data_categories:
        return 4
    return min(tier_for_category(category) for category in data_categories)


def _format_vulnerability(vuln_class: str) -> str:
    label = _VULNERABILITY_LABELS.get(vuln_class, "Security Finding")
    return f"{label} ({vuln_class})"


def _format_location(candidate: CandidateFinding) -> str:
    location = candidate.sink.location
    return f"{location.file}:{location.line}"


def _format_confirmation(finding: ConfirmedFinding) -> str:
    return "CONFIRMED" if finding.sandbox_result.confirmed else "UNVERIFIED"


def _format_data_categories(categories: Sequence[str]) -> str:
    if not categories:
        return "None detected"
    return ", ".join(f"{category} (Tier {tier_for_category(category)})" for category in categories)


def _framework_label(framework: str) -> str:
    return _FRAMEWORK_LABELS.get(framework, framework.replace("_", " "))


def _consequence_priority(consequence: str) -> tuple[int, str]:
    order = {
        "notify_regulator": 0,
        "notify_individuals": 1,
        "remediate": 2,
        "review": 3,
        "document": 4,
    }
    return (order.get(consequence, 99), consequence)


def _load_rule_catalog() -> dict[str, RegulatoryRuleSpec]:
    return {
        rule_spec.rule_id: rule_spec
        for rule_spec in [*load_pdpa_rule_specs(), *load_mas_trm_rule_specs()]
    }


def _is_known_category(value: str) -> bool:
    try:
        tier_for_category(value)
    except ValueError:
        return False
    return True


def _string_or_none(value: object) -> str | None:
    if value is None:
        return None
    if isinstance(value, str):
        return value
    return str(value)


def _string_list(value: object) -> list[str]:
    if isinstance(value, list):
        return [str(item) for item in value]
    return []


__all__ = [
    "DISCLAIMER_TEXT",
    "assess_finding",
    "build_default_engine",
    "register_default_rules",
    "render_legal_memo",
]
