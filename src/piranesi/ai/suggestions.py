from __future__ import annotations

import json
from hashlib import sha256
from pathlib import Path
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from piranesi.ai.evals import AIEvaluationError, evaluate_ai_output
from piranesi.ai.providers import AIProvider
from piranesi.ai.redaction import RedactedPromptPayload, build_redacted_prompt_payload
from piranesi.ai.trace import AITraceError, record_ai_trace, update_ai_trace_approval
from piranesi.workspace import (
    AuditEvent,
    WorkspaceState,
    append_audit_event,
    file_sha256,
    utc_now,
    workspace_path,
)

AI_SUGGESTION_SET_SCHEMA_VERSION: Literal["piranesi.ai-suggestion-set.v1"] = (
    "piranesi.ai-suggestion-set.v1"
)
AISuggestionKind = Literal["dedupe-candidate", "severity-rationale", "retest-checklist"]
AISuggestionStatus = Literal["draft", "accepted", "rejected", "ignored"]

ALLOWED_SUGGESTION_REPORT_FIELDS = (
    "affected_assets",
    "confidence",
    "evidence",
    "executive_summary",
    "findings",
    "limitations",
    "methodology",
    "retest_status",
    "severity",
    "status",
)
DEFAULT_SUGGESTION_KINDS: tuple[AISuggestionKind, ...] = (
    "dedupe-candidate",
    "severity-rationale",
    "retest-checklist",
)


class AISuggestionError(ValueError):
    """Raised when an AI suggestion workflow cannot proceed safely."""


class _StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class AISuggestion(_StrictModel):
    kind: AISuggestionKind
    text: str
    finding_ids: list[str] = Field(default_factory=list)
    evidence_ids: list[str] = Field(default_factory=list)
    report_fields: list[str] = Field(default_factory=list)


class AISuggestionSet(_StrictModel):
    schema_version: Literal["piranesi.ai-suggestion-set.v1"] = AI_SUGGESTION_SET_SCHEMA_VERSION
    suggestion_set_id: str
    suggestions: list[AISuggestion]
    trace_id: str
    status: AISuggestionStatus = "draft"
    ai_generated: bool = True
    created_at: str
    accepted_at: str | None = None
    rejected_at: str | None = None
    ignored_at: str | None = None
    evaluation: dict[str, object] = Field(default_factory=dict)


def create_ai_suggestion_set(
    state: WorkspaceState,
    *,
    provider: AIProvider,
    kinds: list[AISuggestionKind] | None = None,
) -> AISuggestionSet:
    requested_kinds = tuple(kinds or DEFAULT_SUGGESTION_KINDS)
    prompt = build_redacted_prompt_payload(state, purpose="suggestion-mode")
    prompt = _suggestion_prompt(prompt, requested_kinds=requested_kinds)
    provider_result = provider.complete(prompt)
    evaluation = evaluate_ai_output(prompt, output_text=provider_result.text)
    try:
        evaluation.require_passed()
    except AIEvaluationError as exc:
        raise AISuggestionError(str(exc)) from exc

    suggestions = _parse_suggestions(provider_result.text)
    _validate_suggestions(prompt, suggestions, requested_kinds=requested_kinds)
    trace = record_ai_trace(
        state,
        prompt=prompt,
        provider=provider.trace_metadata,
        output_text=provider_result.text,
        target={"field": "ai_suggestions", "kinds": list(requested_kinds)},
        response_metadata=provider_result.metadata,
    )
    suggestion_set = AISuggestionSet(
        suggestion_set_id=_suggestion_set_id(trace.trace_id),
        suggestions=suggestions,
        trace_id=trace.trace_id,
        created_at=utc_now(),
        evaluation=evaluation.model_dump(mode="json"),
    )
    path = _suggestion_set_path(state, suggestion_set.suggestion_set_id)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(suggestion_set.model_dump(mode="json"), indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    append_audit_event(
        state,
        AuditEvent(
            timestamp=utc_now(),
            command="ai suggestions draft",
            output_path=path.relative_to(state.root).as_posix(),
            output_sha256=file_sha256(path),
            summary={
                "suggestion_set_id": suggestion_set.suggestion_set_id,
                "trace_id": suggestion_set.trace_id,
                "status": suggestion_set.status,
                "suggestions": len(suggestion_set.suggestions),
            },
        ),
    )
    return suggestion_set


def accept_ai_suggestion_set(
    state: WorkspaceState,
    *,
    suggestion_set_id: str,
) -> AISuggestionSet:
    return _transition_ai_suggestion_set(
        state,
        suggestion_set_id=suggestion_set_id,
        status="accepted",
    )


def reject_ai_suggestion_set(
    state: WorkspaceState,
    *,
    suggestion_set_id: str,
) -> AISuggestionSet:
    return _transition_ai_suggestion_set(
        state,
        suggestion_set_id=suggestion_set_id,
        status="rejected",
    )


def ignore_ai_suggestion_set(
    state: WorkspaceState,
    *,
    suggestion_set_id: str,
) -> AISuggestionSet:
    return _transition_ai_suggestion_set(
        state,
        suggestion_set_id=suggestion_set_id,
        status="ignored",
    )


def load_ai_suggestion_set(
    root: Path | str,
    *,
    suggestion_set_id: str,
) -> tuple[AISuggestionSet, Path]:
    suggestions_dir = workspace_path(root, "ai/suggestions", allowed_roots=("ai",))
    for path in sorted(suggestions_dir.glob("suggestions-*.json")):
        try:
            suggestion_set = AISuggestionSet.model_validate(
                json.loads(path.read_text(encoding="utf-8"))
            )
        except (json.JSONDecodeError, ValidationError) as exc:
            raise AISuggestionError(f"invalid AI suggestion set {path}: {exc}") from exc
        if suggestion_set.suggestion_set_id == suggestion_set_id:
            return suggestion_set, path
    raise AISuggestionError(f"unknown AI suggestion set ID: {suggestion_set_id}")


def _transition_ai_suggestion_set(
    state: WorkspaceState,
    *,
    suggestion_set_id: str,
    status: AISuggestionStatus,
) -> AISuggestionSet:
    suggestion_set, path = load_ai_suggestion_set(
        state.root,
        suggestion_set_id=suggestion_set_id,
    )
    if suggestion_set.status != "draft":
        raise AISuggestionError(
            f"AI suggestion set {suggestion_set_id} is already {suggestion_set.status}"
        )
    try:
        update_ai_trace_approval(state, trace_id=suggestion_set.trace_id, approval_state=status)
    except AITraceError as exc:
        raise AISuggestionError(str(exc)) from exc

    timestamp_field = f"{status}_at"
    updated = suggestion_set.model_copy(
        update={"status": status, timestamp_field: utc_now()},
    )
    path.write_text(
        json.dumps(updated.model_dump(mode="json"), indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    append_audit_event(
        state,
        AuditEvent(
            timestamp=utc_now(),
            command=f"ai suggestions {status}",
            output_path=path.relative_to(state.root).as_posix(),
            output_sha256=file_sha256(path),
            summary={
                "suggestion_set_id": updated.suggestion_set_id,
                "trace_id": updated.trace_id,
                "status": updated.status,
            },
        ),
    )
    return updated


def _suggestion_prompt(
    prompt: RedactedPromptPayload,
    *,
    requested_kinds: tuple[AISuggestionKind, ...],
) -> RedactedPromptPayload:
    policy = {
        **prompt.policy,
        "suggestion_mode": "dedupe-severity-retest",
        "allowed_suggestion_kinds": list(requested_kinds),
        "allowed_report_fields": list(ALLOWED_SUGGESTION_REPORT_FIELDS),
        "suggestions_may_create_findings": False,
        "suggestions_may_add_evidence": False,
        "suggestions_may_mutate_workspace": False,
        "required_response_format": {
            "suggestions": [
                {
                    "kind": "dedupe-candidate|severity-rationale|retest-checklist",
                    "text": "operator-facing suggestion text",
                    "finding_ids": ["existing finding IDs only"],
                    "evidence_ids": ["existing evidence IDs only"],
                    "report_fields": ["allowed report field names only"],
                }
            ]
        },
    }
    return prompt.model_copy(update={"policy": policy})


def _parse_suggestions(output_text: str) -> list[AISuggestion]:
    try:
        payload = json.loads(output_text)
    except json.JSONDecodeError as exc:
        raise AISuggestionError("AI suggestion output must be a JSON object") from exc
    if not isinstance(payload, dict) or not isinstance(payload.get("suggestions"), list):
        raise AISuggestionError("AI suggestion output must contain a suggestions list")
    try:
        return [AISuggestion.model_validate(item) for item in payload["suggestions"]]
    except ValidationError as exc:
        raise AISuggestionError(f"invalid AI suggestion payload: {exc}") from exc


def _validate_suggestions(
    prompt: RedactedPromptPayload,
    suggestions: list[AISuggestion],
    *,
    requested_kinds: tuple[AISuggestionKind, ...],
) -> None:
    if not suggestions:
        raise AISuggestionError("AI suggestion output must include at least one suggestion")

    requested = set(requested_kinds)
    allowed_finding_ids = {finding.id for finding in prompt.findings}
    allowed_evidence_ids = {
        evidence.evidence_id for finding in prompt.findings for evidence in finding.evidence
    }
    allowed_report_fields = set(ALLOWED_SUGGESTION_REPORT_FIELDS)

    for index, suggestion in enumerate(suggestions, start=1):
        if suggestion.kind not in requested:
            raise AISuggestionError(f"suggestion {index} has unrequested kind: {suggestion.kind}")
        if not suggestion.text.strip():
            raise AISuggestionError(f"suggestion {index} text must not be empty")

        unknown_finding_ids = sorted(set(suggestion.finding_ids) - allowed_finding_ids)
        if unknown_finding_ids:
            raise AISuggestionError(
                f"suggestion {index} references unknown finding IDs: "
                f"{', '.join(unknown_finding_ids)}"
            )

        unknown_evidence_ids = sorted(set(suggestion.evidence_ids) - allowed_evidence_ids)
        if unknown_evidence_ids:
            raise AISuggestionError(
                f"suggestion {index} references unknown evidence IDs: "
                f"{', '.join(unknown_evidence_ids)}"
            )

        unknown_report_fields = sorted(set(suggestion.report_fields) - allowed_report_fields)
        if unknown_report_fields:
            raise AISuggestionError(
                f"suggestion {index} references unsupported report fields: "
                f"{', '.join(unknown_report_fields)}"
            )

        if not suggestion.evidence_ids and not suggestion.report_fields:
            raise AISuggestionError(
                f"suggestion {index} must cite existing evidence IDs or report fields"
            )


def _suggestion_set_path(state: WorkspaceState, suggestion_set_id: str) -> Path:
    safe_id = suggestion_set_id.replace(":", "-")
    return workspace_path(
        state.root,
        f"ai/suggestions/suggestions-{safe_id}.json",
        allowed_roots=("ai",),
    )


def _suggestion_set_id(trace_id: str) -> str:
    digest = sha256(f"suggestions\0{trace_id}".encode()).hexdigest()[:24]
    return f"ai-suggestions:{digest}"


__all__ = [
    "AI_SUGGESTION_SET_SCHEMA_VERSION",
    "ALLOWED_SUGGESTION_REPORT_FIELDS",
    "DEFAULT_SUGGESTION_KINDS",
    "AISuggestion",
    "AISuggestionError",
    "AISuggestionKind",
    "AISuggestionSet",
    "AISuggestionStatus",
    "accept_ai_suggestion_set",
    "create_ai_suggestion_set",
    "ignore_ai_suggestion_set",
    "load_ai_suggestion_set",
    "reject_ai_suggestion_set",
]
