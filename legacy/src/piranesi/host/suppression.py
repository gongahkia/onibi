from __future__ import annotations

from collections.abc import Sequence
from datetime import UTC, datetime
from datetime import date as date_cls

from pydantic import BaseModel, ConfigDict, Field

from piranesi.detect.suppression import (
    SuppressionLifecycleSummary,
    SuppressionRule,
)
from piranesi.host.controls import control_summary_for_findings
from piranesi.host.models import HostFinding, HostPostureReport


def apply_host_suppressions(
    report: HostPostureReport,
    rules: Sequence[SuppressionRule],
) -> HostPostureReport:
    outcome = apply_host_suppressions_with_lifecycle(
        report,
        rules,
        evaluate_stale=False,
    )
    return outcome.report


def apply_host_suppressions_with_lifecycle(
    report: HostPostureReport,
    rules: Sequence[SuppressionRule],
    *,
    invalid_entries: Sequence[str] | None = None,
    evaluate_stale: bool = True,
    today: date_cls | None = None,
) -> HostSuppressionOutcome:
    findings = [
        finding.model_copy(
            update={
                "suppressed": (reason := _host_suppression_reason_for_finding(finding, rules))
                is not None,
                "suppression_reason": reason,
            }
        )
        for finding in report.findings
    ]
    lifecycle = summarize_host_suppression_lifecycle(
        findings=report.findings,
        rules=rules,
        invalid_entries=invalid_entries,
        evaluate_stale=evaluate_stale,
        today=today,
    )
    return HostSuppressionOutcome(
        report=report.model_copy(
            update={
                "findings": findings,
                "control_summary": control_summary_for_findings(findings),
            }
        ),
        lifecycle=lifecycle,
    )


def summarize_host_suppression_lifecycle(
    *,
    findings: Sequence[HostFinding],
    rules: Sequence[SuppressionRule],
    invalid_entries: Sequence[str] | None = None,
    evaluate_stale: bool = True,
    today: date_cls | None = None,
) -> SuppressionLifecycleSummary:
    today_value = datetime.now(UTC).date() if today is None else today
    finding_ids = {finding.id for finding in findings}
    active_rules = 0
    expired_selectors: list[str] = []
    stale_selectors: list[str] = []

    for rule in rules:
        selector = _rule_selector(rule)
        if _rule_is_expired_on(rule, today=today_value):
            expired_selectors.append(selector)
            continue
        if rule.id is None:
            continue
        if not evaluate_stale:
            active_rules += 1
            continue
        if rule.id in finding_ids:
            active_rules += 1
        else:
            stale_selectors.append(selector)

    invalid = list(invalid_entries or [])
    return SuppressionLifecycleSummary(
        total_rules=len(rules),
        active_rules=active_rules,
        expired_rules=len(expired_selectors),
        stale_rules=len(stale_selectors),
        invalid_rules=len(invalid),
        inline_suppressions=0,
        stale_evaluated=evaluate_stale,
        expired_selectors=expired_selectors,
        stale_selectors=stale_selectors,
        invalid_entries=invalid,
    )


class HostSuppressionOutcome(BaseModel):
    model_config = ConfigDict(extra="forbid")

    report: HostPostureReport
    lifecycle: SuppressionLifecycleSummary = Field(default_factory=SuppressionLifecycleSummary)


def _host_suppression_reason_for_finding(
    finding: HostFinding,
    rules: Sequence[SuppressionRule],
) -> str | None:
    for rule in rules:
        if rule.id is None or _rule_is_expired_on(rule, today=datetime.now(UTC).date()):
            continue
        if rule.id == finding.id:
            return _format_suppression_reason(rule.reason, rule.ticket)
    return None


def _format_suppression_reason(reason: str | None, ticket: str | None) -> str:
    if reason and ticket:
        return f"{reason} (ticket: {ticket})"
    if reason:
        return reason
    if ticket:
        return f"ticket: {ticket}"
    return "suppressed"


def _rule_selector(rule: SuppressionRule) -> str:
    parts: list[str] = []
    if rule.id:
        parts.append(f"id={rule.id}")
    if rule.cwe:
        parts.append(f"cwe={rule.cwe}")
    if rule.path:
        parts.append(f"path={rule.path}")
    return ", ".join(parts) if parts else "<invalid>"


def _rule_is_expired_on(rule: SuppressionRule, *, today: date_cls) -> bool:
    return rule.expires is not None and rule.expires < today


__all__ = [
    "HostSuppressionOutcome",
    "apply_host_suppressions",
    "apply_host_suppressions_with_lifecycle",
    "summarize_host_suppression_lifecycle",
]
