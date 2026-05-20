from __future__ import annotations

import re
from dataclasses import dataclass, field
from typing import Any, Literal
from urllib.parse import urlsplit, urlunsplit

from pydantic import BaseModel, ConfigDict, Field

from piranesi.workspace import NormalizedFinding, WorkspaceState, utc_now

PROMPT_SCHEMA_VERSION: Literal["piranesi.ai.prompt.v1"] = "piranesi.ai.prompt.v1"

SECRET_PATTERN = re.compile(
    r"(?i)\b(token|secret|password|passwd|api[_-]?key|session|cookie)\s*[:=]\s*([^\s;&]+)"
)
AUTH_HEADER_PATTERN = re.compile(r"(?i)\b(authorization\s*:\s*)(bearer|basic)\s+[^\s]+")
PRIVATE_KEY_PATTERN = re.compile(
    r"-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----",
    re.DOTALL,
)
IPV4_PATTERN = re.compile(r"\b(?:\d{1,3}\.){3}\d{1,3}\b")
HOSTNAME_PATTERN = re.compile(
    r"\b(?=.{1,253}\b)(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+"
    r"(?:[A-Za-z]{2,63}|test|local|internal|corp|lan)\b"
)
RAW_EVIDENCE_KIND_TERMS = (
    "request",
    "response",
    "curl",
    "payload",
    "transcript",
    "loot",
    "session",
    "secret",
)


class _StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class RedactionEvent(_StrictModel):
    category: Literal["secret", "host", "client", "raw-evidence", "private-key"]
    placeholder: str
    field: str


class PromptEvidence(_StrictModel):
    evidence_id: str
    kind: str
    value: str
    redacted: bool
    locator: str | None = None


class PromptFinding(_StrictModel):
    id: str
    title: str
    severity: str
    confidence: str
    status: str
    asset: str | None = None
    description: str | None = None
    remediation: str | None = None
    weakness_ids: list[str] = Field(default_factory=list)
    references: list[str] = Field(default_factory=list)
    evidence: list[PromptEvidence] = Field(default_factory=list)


class RedactedPromptPayload(_StrictModel):
    schema_version: Literal["piranesi.ai.prompt.v1"] = PROMPT_SCHEMA_VERSION
    purpose: str
    generated_at: str
    policy: dict[str, Any]
    engagement: dict[str, Any]
    findings: list[PromptFinding]
    redactions: list[RedactionEvent]

    def provider_payload(self) -> dict[str, Any]:
        return self.model_dump(mode="json")


@dataclass
class PromptRedactionContext:
    client_identifiers: set[str] = field(default_factory=set)
    _host_placeholders: dict[str, str] = field(default_factory=dict, init=False)
    _client_placeholders: dict[str, str] = field(default_factory=dict, init=False)
    _events: list[RedactionEvent] = field(default_factory=list, init=False)

    @classmethod
    def from_workspace(cls, state: WorkspaceState) -> PromptRedactionContext:
        identifiers = {
            value.strip()
            for value in (
                state.workspace.engagement.client,
                state.workspace.engagement.project,
                state.workspace.engagement.owner,
            )
            if value and value.strip()
        }
        return cls(client_identifiers=identifiers)

    @property
    def events(self) -> list[RedactionEvent]:
        return list(self._events)

    def redact(self, value: str | None, *, field: str) -> str | None:
        if value is None:
            return None
        redacted = value
        redacted = PRIVATE_KEY_PATTERN.sub(
            lambda match: self._record_static("private-key", "[redacted private key]", field),
            redacted,
        )
        redacted = AUTH_HEADER_PATTERN.sub(
            lambda match: f"{match.group(1)}[redacted credential]", redacted
        )
        if redacted != value:
            self._events.append(
                RedactionEvent(
                    category="secret",
                    placeholder="[redacted credential]",
                    field=field,
                )
            )
        redacted = SECRET_PATTERN.sub(
            lambda match: f"{match.group(1)}=[redacted secret]", redacted
        )
        if redacted != value and not any(
            event.category == "secret" and event.field == field for event in self._events
        ):
            self._events.append(
                RedactionEvent(
                    category="secret",
                    placeholder="[redacted secret]",
                    field=field,
                )
            )
        for identifier in sorted(self.client_identifiers, key=len, reverse=True):
            if identifier and identifier in redacted:
                placeholder = self._client_placeholder(identifier)
                redacted = redacted.replace(identifier, placeholder)
                self._events.append(
                    RedactionEvent(category="client", placeholder=placeholder, field=field)
                )
        redacted = _redact_urls(redacted, self, field=field)
        redacted = IPV4_PATTERN.sub(
            lambda match: self._host_placeholder(match.group(0), field=field), redacted
        )
        redacted = HOSTNAME_PATTERN.sub(
            lambda match: self._host_placeholder(match.group(0), field=field), redacted
        )
        return redacted

    def raw_evidence_placeholder(self, *, kind: str, value: str, field: str) -> str:
        placeholder = f"[redacted {kind} evidence; length={len(value)}]"
        self._events.append(
            RedactionEvent(category="raw-evidence", placeholder=placeholder, field=field)
        )
        return placeholder

    def _host_placeholder(self, value: str, *, field: str) -> str:
        placeholder = self._host_placeholders.get(value)
        if placeholder is None:
            placeholder = f"[host:{len(self._host_placeholders) + 1}]"
            self._host_placeholders[value] = placeholder
        self._events.append(RedactionEvent(category="host", placeholder=placeholder, field=field))
        return placeholder

    def _client_placeholder(self, value: str) -> str:
        placeholder = self._client_placeholders.get(value)
        if placeholder is None:
            placeholder = f"[client:{len(self._client_placeholders) + 1}]"
            self._client_placeholders[value] = placeholder
        return placeholder

    def _record_static(self, category: str, placeholder: str, field: str) -> str:
        self._events.append(
            RedactionEvent(
                category=category,  # type: ignore[arg-type]
                placeholder=placeholder,
                field=field,
            )
        )
        return placeholder


def redact_text_for_prompt(
    value: str,
    *,
    context: PromptRedactionContext | None = None,
    field: str = "text",
) -> str:
    active_context = context or PromptRedactionContext()
    return active_context.redact(value, field=field) or ""


def build_redacted_prompt_payload(
    state: WorkspaceState,
    *,
    purpose: str,
) -> RedactedPromptPayload:
    context = PromptRedactionContext.from_workspace(state)
    findings = [
        _prompt_finding(finding, context=context)
        for finding in sorted(state.findings.findings, key=lambda item: item.id)
    ]
    engagement = {
        "client": context.redact(state.workspace.engagement.client, field="engagement.client"),
        "project": context.redact(state.workspace.engagement.project, field="engagement.project"),
        "scope": [
            context.redact(scope, field="engagement.scope") or "[redacted scope]"
            for scope in state.workspace.engagement.scope
        ],
    }
    return RedactedPromptPayload(
        purpose=purpose,
        generated_at=utc_now(),
        policy={
            "redaction": "required-before-provider",
            "raw_evidence": "omitted-or-summarized",
            "ai_may_create_findings": False,
            "human_approval_required": True,
        },
        engagement=engagement,
        findings=findings,
        redactions=_dedupe_events(context.events),
    )


def _prompt_finding(
    finding: NormalizedFinding,
    *,
    context: PromptRedactionContext,
) -> PromptFinding:
    evidence = [
        _prompt_evidence(finding, index=index, context=context)
        for index, _item in enumerate(finding.evidence, start=1)
    ]
    return PromptFinding(
        id=finding.id,
        title=context.redact(finding.title, field=f"finding.{finding.id}.title") or "",
        severity=finding.severity,
        confidence=finding.confidence,
        status=finding.status,
        asset=context.redact(finding.asset, field=f"finding.{finding.id}.asset"),
        description=context.redact(
            finding.description,
            field=f"finding.{finding.id}.description",
        ),
        remediation=context.redact(
            finding.remediation,
            field=f"finding.{finding.id}.remediation",
        ),
        weakness_ids=list(finding.weakness_ids),
        references=[
            context.redact(reference, field=f"finding.{finding.id}.reference") or ""
            for reference in finding.references
        ],
        evidence=evidence,
    )


def _prompt_evidence(
    finding: NormalizedFinding,
    *,
    index: int,
    context: PromptRedactionContext,
) -> PromptEvidence:
    item = finding.evidence[index - 1]
    field = f"finding.{finding.id}.evidence.{index}"
    raw_kind = _is_raw_evidence_kind(item.kind)
    if item.redacted or raw_kind:
        value = context.raw_evidence_placeholder(kind=item.kind, value=item.value, field=field)
        redacted = True
    else:
        redacted_value = context.redact(item.value, field=field) or ""
        value = redacted_value
        redacted = redacted_value != item.value
    return PromptEvidence(
        evidence_id=f"{finding.id}:evidence:{index}",
        kind=item.kind,
        value=value,
        redacted=redacted,
        locator=context.redact(item.locator, field=f"{field}.locator"),
    )


def _is_raw_evidence_kind(kind: str) -> bool:
    normalized = kind.lower()
    return any(term in normalized for term in RAW_EVIDENCE_KIND_TERMS)


def _redact_urls(value: str, context: PromptRedactionContext, *, field: str) -> str:
    def replace(match: re.Match[str]) -> str:
        parsed = urlsplit(match.group(0))
        if not parsed.netloc:
            return match.group(0)
        placeholder = context._host_placeholder(parsed.netloc, field=field)
        return urlunsplit((parsed.scheme, placeholder, parsed.path, "", ""))

    return re.sub(r"https?://[^\s\"'<>]+", replace, value)


def _dedupe_events(events: list[RedactionEvent]) -> list[RedactionEvent]:
    seen: set[tuple[str, str, str]] = set()
    deduped: list[RedactionEvent] = []
    for event in events:
        key = (event.category, event.placeholder, event.field)
        if key in seen:
            continue
        seen.add(key)
        deduped.append(event)
    return deduped


__all__ = [
    "PROMPT_SCHEMA_VERSION",
    "PromptEvidence",
    "PromptFinding",
    "PromptRedactionContext",
    "RedactedPromptPayload",
    "RedactionEvent",
    "build_redacted_prompt_payload",
    "redact_text_for_prompt",
]
