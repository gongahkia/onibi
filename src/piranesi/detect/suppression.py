from __future__ import annotations

import re
from collections.abc import Sequence
from datetime import UTC, datetime
from datetime import date as date_cls
from fnmatch import fnmatch
from pathlib import Path
from typing import Any

import yaml
from pydantic import BaseModel, ConfigDict, Field, ValidationError, model_validator

from piranesi.models import CandidateFinding, SourceLocation

_INLINE_SUPPRESSION_PATTERN = re.compile(r"piranesi:suppress\s+(?P<cwe>CWE-\d+)\b(?P<rest>.*)")
_REASON_PATTERN = re.compile(r'reason\s*:\s*"(?P<reason>(?:[^"\\]|\\.)*)"')
_TICKET_PATTERN = re.compile(r'ticket\s*:\s*(?:"(?P<quoted>(?:[^"\\]|\\.)*)"|(?P<bare>[^\s]+))')
_CWE_PATTERN = re.compile(r"(CWE-\d+)", re.IGNORECASE)
_INLINE_LINE_PROXIMITY = 2
_IGNORE_FILENAME = ".piranesi-ignore"


class SuppressionRule(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str | None = None
    cwe: str | None = None
    path: str | None = None
    reason: str | None = None
    reason_code: str | None = None
    scope: str | None = None
    owner: str | None = None
    author: str | None = None
    created: date_cls | None = None
    date: date_cls | None = None
    expires: date_cls | None = None
    ticket: str | None = None
    reference: str | None = None
    project_root: str | None = Field(default=None, exclude=True)

    @model_validator(mode="after")
    def _validate_selector(self) -> SuppressionRule:
        self.cwe = _normalize_cwe(self.cwe)
        self.path = None if self.path is None else self.path.strip() or None
        self.reason = None if self.reason is None else self.reason.strip() or None
        self.reason_code = None if self.reason_code is None else self.reason_code.strip() or None
        self.scope = None if self.scope is None else self.scope.strip() or None
        self.owner = None if self.owner is None else self.owner.strip() or None
        self.author = None if self.author is None else self.author.strip() or None
        self.ticket = None if self.ticket is None else self.ticket.strip() or None
        self.reference = None if self.reference is None else self.reference.strip() or None

        if self.owner is None and self.author is not None:
            self.owner = self.author
        if self.author is None and self.owner is not None:
            self.author = self.owner
        if self.created is None and self.date is not None:
            self.created = self.date
        if self.date is None and self.created is not None:
            self.date = self.created
        if self.reference is None and self.ticket is not None:
            self.reference = self.ticket
        if self.ticket is None and self.reference is not None:
            self.ticket = self.reference

        if not any((self.id, self.cwe, self.path)):
            raise ValueError("suppression rule must define at least one selector: id, cwe, or path")
        if self.scope is None:
            self.scope = _default_scope(self)
        return self


class InlineSuppression(BaseModel):
    model_config = ConfigDict(extra="forbid")

    file: str
    line: int
    cwe: str
    reason: str | None = None
    ticket: str | None = None

    @model_validator(mode="after")
    def _normalize(self) -> InlineSuppression:
        normalized = _normalize_cwe(self.cwe)
        if normalized is None:
            raise ValueError("inline suppression requires a CWE selector")
        self.file = str(Path(self.file).resolve(strict=False))
        self.cwe = normalized
        return self


class SuppressionFileValidation(BaseModel):
    model_config = ConfigDict(extra="forbid")

    path: str
    rules: list[SuppressionRule] = Field(default_factory=list)
    invalid_entries: list[str] = Field(default_factory=list)


class SuppressionLifecycleSummary(BaseModel):
    model_config = ConfigDict(extra="forbid")

    total_rules: int = 0
    active_rules: int = 0
    expired_rules: int = 0
    stale_rules: int = 0
    invalid_rules: int = 0
    inline_suppressions: int = 0
    stale_evaluated: bool = True
    expired_selectors: list[str] = Field(default_factory=list)
    stale_selectors: list[str] = Field(default_factory=list)
    invalid_entries: list[str] = Field(default_factory=list)


class SuppressionOutcome(BaseModel):
    model_config = ConfigDict(extra="forbid")

    findings: list[CandidateFinding] = Field(default_factory=list)
    lifecycle: SuppressionLifecycleSummary = Field(default_factory=SuppressionLifecycleSummary)


def load_ignore_file(project_root: str | Path) -> list[SuppressionRule]:
    validation = load_ignore_file_with_diagnostics(project_root)
    if validation.invalid_entries:
        joined = "; ".join(validation.invalid_entries)
        raise ValueError(f"invalid suppression entries in {validation.path}: {joined}")
    return validation.rules


def load_ignore_file_with_diagnostics(project_root: str | Path) -> SuppressionFileValidation:
    root = Path(project_root).resolve(strict=False)
    ignore_path = root / _IGNORE_FILENAME
    if not ignore_path.exists():
        return SuppressionFileValidation(path=str(ignore_path))

    try:
        payload = yaml.safe_load(ignore_path.read_text(encoding="utf-8"))
    except OSError as exc:
        return SuppressionFileValidation(
            path=str(ignore_path),
            invalid_entries=[f"failed to read {ignore_path}: {exc}"],
        )
    except yaml.YAMLError as exc:
        return SuppressionFileValidation(
            path=str(ignore_path),
            invalid_entries=[f"invalid YAML in {ignore_path}: {exc}"],
        )

    if payload is None:
        return SuppressionFileValidation(path=str(ignore_path))
    if not isinstance(payload, dict):
        return SuppressionFileValidation(
            path=str(ignore_path),
            invalid_entries=[f"{ignore_path} must contain a top-level mapping"],
        )

    raw_rules = payload.get("suppressions", [])
    if raw_rules is None:
        return SuppressionFileValidation(path=str(ignore_path))
    if not isinstance(raw_rules, list):
        return SuppressionFileValidation(
            path=str(ignore_path),
            invalid_entries=[f"{ignore_path} must define 'suppressions' as a list"],
        )

    rules: list[SuppressionRule] = []
    invalid_entries: list[str] = []
    for raw_rule in raw_rules:
        if not isinstance(raw_rule, dict):
            invalid_entries.append(f"{ignore_path} contains a non-mapping suppression entry")
            continue
        try:
            rules.append(
                SuppressionRule.model_validate(
                    {
                        **raw_rule,
                        "project_root": str(root),
                    }
                )
            )
        except ValidationError as exc:
            invalid_entries.append(str(exc))
    return SuppressionFileValidation(
        path=str(ignore_path),
        rules=rules,
        invalid_entries=invalid_entries,
    )


def parse_inline_suppressions(source_file: str | Path) -> list[InlineSuppression]:
    path = Path(source_file).resolve(strict=False)
    if not path.exists():
        return []

    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError as exc:
        raise ValueError(f"failed to read {path}") from exc

    suppressions: list[InlineSuppression] = []
    for line_number, line in enumerate(lines, start=1):
        match = _INLINE_SUPPRESSION_PATTERN.search(line)
        if match is None:
            continue

        rest = match.group("rest")
        ticket_match = _TICKET_PATTERN.search(rest)
        suppressions.append(
            InlineSuppression(
                file=str(path),
                line=line_number,
                cwe=match.group("cwe"),
                reason=_unescape(_first_group(_REASON_PATTERN.search(rest), "reason")),
                ticket=_unescape(_first_group(ticket_match, "quoted", "bare")),
            )
        )
    return suppressions


def apply_suppressions(
    findings: Sequence[CandidateFinding],
    rules: Sequence[SuppressionRule],
    inline: Sequence[InlineSuppression],
) -> list[CandidateFinding]:
    return apply_suppressions_with_lifecycle(findings, rules, inline).findings


def apply_suppressions_with_lifecycle(
    findings: Sequence[CandidateFinding],
    rules: Sequence[SuppressionRule],
    inline: Sequence[InlineSuppression],
    *,
    invalid_entries: Sequence[str] | None = None,
    evaluate_stale: bool = True,
    today: date_cls | None = None,
) -> SuppressionOutcome:
    suppressed_findings: list[CandidateFinding] = []
    for finding in findings:
        matched_reason = _suppression_reason_for_finding(finding, rules=rules, inline=inline)
        suppressed_findings.append(
            finding.model_copy(
                update={
                    "suppressed": matched_reason is not None,
                    "suppression_reason": matched_reason,
                }
            )
        )
    lifecycle = summarize_suppression_lifecycle(
        findings=findings,
        rules=rules,
        inline=inline,
        invalid_entries=invalid_entries,
        evaluate_stale=evaluate_stale,
        today=today,
    )
    return SuppressionOutcome(findings=suppressed_findings, lifecycle=lifecycle)


def append_ignore_file_suppression(
    project_root: str | Path,
    *,
    finding_id: str,
    reason: str,
    reason_code: str | None = None,
    owner: str | None = None,
    ticket: str | None = None,
    reference: str | None = None,
    created: date_cls | None = None,
    expires: date_cls | None = None,
    scope: str | None = "id",
) -> Path:
    root = Path(project_root).resolve(strict=False)
    ignore_path = root / _IGNORE_FILENAME
    payload = _load_ignore_payload(ignore_path)
    suppressions = payload.setdefault("suppressions", [])
    if not isinstance(suppressions, list):
        raise ValueError(f"{ignore_path} must define 'suppressions' as a list")

    entry: dict[str, Any] = {
        "id": finding_id,
        "reason": reason,
        "created": (created or datetime.now(UTC).date()).isoformat(),
        "scope": scope or "id",
    }
    if reason_code is not None:
        entry["reason_code"] = reason_code
    if owner is not None:
        entry["owner"] = owner
    if ticket is not None:
        entry["ticket"] = ticket
    if reference is not None:
        entry["reference"] = reference
    if expires is not None:
        entry["expires"] = expires.isoformat()
    suppressions.append(entry)

    ignore_path.write_text(
        yaml.safe_dump(payload, sort_keys=False),
        encoding="utf-8",
    )
    return ignore_path


def _load_ignore_payload(ignore_path: Path) -> dict[str, Any]:
    if not ignore_path.exists():
        return {"suppressions": []}

    try:
        payload = yaml.safe_load(ignore_path.read_text(encoding="utf-8"))
    except OSError as exc:
        raise ValueError(f"failed to read {ignore_path}") from exc
    except yaml.YAMLError as exc:
        raise ValueError(f"invalid YAML in {ignore_path}") from exc

    if payload is None:
        return {"suppressions": []}
    if not isinstance(payload, dict):
        raise ValueError(f"{ignore_path} must contain a top-level mapping")
    return dict(payload)


def _suppression_reason_for_finding(
    finding: CandidateFinding,
    *,
    rules: Sequence[SuppressionRule],
    inline: Sequence[InlineSuppression],
) -> str | None:
    id_rule = _matching_id_rule(finding, rules)
    if id_rule is not None:
        return _format_suppression_reason(id_rule.reason, id_rule.ticket)

    inline_match = _matching_inline_suppression(finding, inline)
    if inline_match is not None:
        return _format_suppression_reason(inline_match.reason, inline_match.ticket)

    path_rule = _matching_generic_rule(finding, rules)
    if path_rule is not None:
        return _format_suppression_reason(path_rule.reason, path_rule.ticket)

    return None


def _matching_id_rule(
    finding: CandidateFinding,
    rules: Sequence[SuppressionRule],
) -> SuppressionRule | None:
    for rule in rules:
        if rule.id is None or _rule_is_expired(rule):
            continue
        if rule.id == finding.id:
            return rule
    return None


def _matching_generic_rule(
    finding: CandidateFinding,
    rules: Sequence[SuppressionRule],
) -> SuppressionRule | None:
    finding_cwe = _normalize_cwe(finding.vuln_class)
    locations = _finding_locations(finding)
    for rule in rules:
        if rule.id is not None or _rule_is_expired(rule):
            continue
        if rule.cwe is not None and rule.cwe != finding_cwe:
            continue
        if rule.path is not None and not any(
            _path_matches(rule, location) for location in locations
        ):
            continue
        return rule
    return None


def _matching_inline_suppression(
    finding: CandidateFinding,
    inline: Sequence[InlineSuppression],
) -> InlineSuppression | None:
    finding_cwe = _normalize_cwe(finding.vuln_class)
    for suppression in inline:
        if suppression.cwe != finding_cwe:
            continue
        for location in _finding_locations(finding):
            normalized_file = str(Path(location.file).resolve(strict=False))
            if suppression.file != normalized_file:
                continue
            if abs(suppression.line - location.line) <= _INLINE_LINE_PROXIMITY:
                return suppression
    return None


def _path_matches(rule: SuppressionRule, location: SourceLocation) -> bool:
    if rule.path is None:
        return True
    relative = _relative_location_path(location.file, project_root=rule.project_root)
    return relative is not None and fnmatch(relative, rule.path)


def _relative_location_path(path_str: str, *, project_root: str | None) -> str | None:
    candidate = Path(path_str)
    if candidate.is_absolute():
        if project_root is None:
            return candidate.as_posix()
        root = Path(project_root).resolve(strict=False)
        try:
            return candidate.resolve(strict=False).relative_to(root).as_posix()
        except ValueError:
            return None
    return candidate.as_posix()


def _finding_locations(finding: CandidateFinding) -> tuple[SourceLocation, ...]:
    return (
        finding.source.location,
        finding.sink.location,
        *(step.location for step in finding.taint_path),
        *(condition.location for condition in finding.path_conditions),
    )


def _format_suppression_reason(reason: str | None, ticket: str | None) -> str | None:
    if reason and ticket:
        return f"{reason} (ticket: {ticket})"
    if reason:
        return reason
    if ticket:
        return f"ticket: {ticket}"
    return "suppressed"


def _rule_is_expired(rule: SuppressionRule) -> bool:
    return rule.expires is not None and rule.expires < datetime.now(UTC).date()


def summarize_suppression_lifecycle(
    *,
    findings: Sequence[CandidateFinding],
    rules: Sequence[SuppressionRule],
    inline: Sequence[InlineSuppression],
    invalid_entries: Sequence[str] | None = None,
    evaluate_stale: bool = True,
    today: date_cls | None = None,
) -> SuppressionLifecycleSummary:
    today_value = datetime.now(UTC).date() if today is None else today
    active_rules = 0
    expired_selectors: list[str] = []
    stale_selectors: list[str] = []
    for rule in rules:
        selector = _rule_selector(rule)
        if _rule_is_expired_on(rule, today=today_value):
            expired_selectors.append(selector)
            continue
        if not evaluate_stale:
            active_rules += 1
            continue
        if any(_rule_matches_finding(rule, finding) for finding in findings):
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
        inline_suppressions=len(inline),
        stale_evaluated=evaluate_stale,
        expired_selectors=expired_selectors,
        stale_selectors=stale_selectors,
        invalid_entries=invalid,
    )


def _default_scope(rule: SuppressionRule) -> str:
    if rule.id:
        return "id"
    if rule.cwe and rule.path:
        return "cwe_path"
    if rule.cwe:
        return "cwe"
    if rule.path:
        return "path"
    return "custom"


def _rule_selector(rule: SuppressionRule) -> str:
    parts: list[str] = []
    if rule.id:
        parts.append(f"id={rule.id}")
    if rule.cwe:
        parts.append(f"cwe={rule.cwe}")
    if rule.path:
        parts.append(f"path={rule.path}")
    return ", ".join(parts) if parts else "<invalid>"


def _rule_matches_finding(rule: SuppressionRule, finding: CandidateFinding) -> bool:
    if rule.id is not None:
        return rule.id == finding.id
    finding_cwe = _normalize_cwe(finding.vuln_class)
    if rule.cwe is not None and rule.cwe != finding_cwe:
        return False
    locations = _finding_locations(finding)
    return rule.path is None or any(_path_matches(rule, location) for location in locations)


def _rule_is_expired_on(rule: SuppressionRule, *, today: date_cls) -> bool:
    return rule.expires is not None and rule.expires < today


def _normalize_cwe(value: str | None) -> str | None:
    if value is None:
        return None
    match = _CWE_PATTERN.search(value.strip())
    if match is None:
        return None
    return match.group(1).upper()


def _unescape(value: str | None) -> str | None:
    if value is None:
        return None
    return value.replace('\\"', '"').replace("\\\\", "\\").strip() or None


def _first_group(match: re.Match[str] | None, *group_names: str) -> str | None:
    if match is None:
        return None
    for group_name in group_names:
        value = match.group(group_name)
        if value:
            return value
    return None


__all__ = [
    "InlineSuppression",
    "SuppressionFileValidation",
    "SuppressionLifecycleSummary",
    "SuppressionOutcome",
    "SuppressionRule",
    "append_ignore_file_suppression",
    "apply_suppressions",
    "apply_suppressions_with_lifecycle",
    "load_ignore_file",
    "load_ignore_file_with_diagnostics",
    "parse_inline_suppressions",
    "summarize_suppression_lifecycle",
]
