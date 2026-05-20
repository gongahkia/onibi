from __future__ import annotations

import json
from hashlib import sha256
from pathlib import Path
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from piranesi.ai.redaction import (
    PromptRedactionContext,
    RedactedPromptPayload,
    redact_text_for_prompt,
)
from piranesi.workspace import (
    AuditEvent,
    WorkspaceState,
    append_audit_event,
    file_sha256,
    utc_now,
    workspace_path,
)

AI_TRACE_SCHEMA_VERSION: Literal["piranesi.ai-trace.v1"] = "piranesi.ai-trace.v1"
AI_TRACE_FILE = "ai/traces.jsonl"
AIApprovalState = Literal["draft", "accepted", "rejected", "ignored"]


class AITraceError(ValueError):
    """Raised when AI trace records cannot be written or loaded."""


class _StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class AITraceResponse(_StrictModel):
    text: str
    metadata: dict[str, Any] = Field(default_factory=dict)
    redacted: bool = True


class AITraceRecord(_StrictModel):
    schema_version: Literal["piranesi.ai-trace.v1"] = AI_TRACE_SCHEMA_VERSION
    trace_id: str
    created_at: str
    purpose: str
    target: dict[str, Any]
    provider: dict[str, Any]
    prompt: dict[str, Any]
    prompt_sha256: str
    response: AITraceResponse
    response_sha256: str
    approval_state: AIApprovalState = "draft"
    chain_of_custody: dict[str, Any] = Field(default_factory=dict)


def record_ai_trace(
    state: WorkspaceState,
    *,
    prompt: RedactedPromptPayload,
    provider: dict[str, Any],
    output_text: str,
    target: dict[str, Any],
    response_metadata: dict[str, Any] | None = None,
    approval_state: AIApprovalState = "draft",
) -> AITraceRecord:
    context = PromptRedactionContext.from_workspace(state)
    redacted_output = redact_text_for_prompt(output_text, context=context, field="ai.response")
    prompt_payload = prompt.provider_payload()
    prompt_digest = _payload_sha256(prompt_payload)
    response_payload = {
        "text": redacted_output,
        "metadata": dict(response_metadata or {}),
        "redacted": True,
    }
    response_digest = _payload_sha256(response_payload)
    created_at = utc_now()
    trace_id = sha256(
        _canonical_json(
            {
                "created_at": created_at,
                "prompt_sha256": prompt_digest,
                "response_sha256": response_digest,
                "target": target,
            }
        ).encode("utf-8")
    ).hexdigest()[:24]
    record = AITraceRecord(
        trace_id=f"ai-trace:{trace_id}",
        created_at=created_at,
        purpose=prompt.purpose,
        target=dict(target),
        provider=dict(provider),
        prompt=prompt_payload,
        prompt_sha256=prompt_digest,
        response=AITraceResponse.model_validate(response_payload),
        response_sha256=response_digest,
        approval_state=approval_state,
        chain_of_custody={
            "trace_path": AI_TRACE_FILE,
            "prompt_redaction_schema": prompt.schema_version,
        },
    )

    trace_path = workspace_path(state.root, AI_TRACE_FILE, allowed_roots=("ai",))
    trace_path.parent.mkdir(parents=True, exist_ok=True)
    with trace_path.open("a", encoding="utf-8") as handle:
        handle.write(_canonical_json(record.model_dump(mode="json")))
    append_audit_event(
        state,
        AuditEvent(
            timestamp=created_at,
            command="ai trace",
            output_path=AI_TRACE_FILE,
            output_sha256=file_sha256(trace_path),
            summary={
                "trace_id": record.trace_id,
                "purpose": record.purpose,
                "provider": provider.get("name"),
                "model": provider.get("model"),
                "approval_state": approval_state,
                "target": target,
                "prompt_sha256": prompt_digest,
                "response_sha256": response_digest,
            },
        ),
    )
    return record


def load_ai_traces(root: Path | str) -> list[AITraceRecord]:
    trace_path = workspace_path(root, AI_TRACE_FILE, allowed_roots=("ai",))
    if not trace_path.is_file():
        return []
    records: list[AITraceRecord] = []
    lines = trace_path.read_text(encoding="utf-8").splitlines()
    for line_number, line in enumerate(lines, start=1):
        if not line.strip():
            continue
        try:
            payload = json.loads(line)
            records.append(AITraceRecord.model_validate(payload))
        except (json.JSONDecodeError, ValidationError) as exc:
            raise AITraceError(f"invalid AI trace record on line {line_number}: {exc}") from exc
    return records


def update_ai_trace_approval(
    state: WorkspaceState,
    *,
    trace_id: str,
    approval_state: AIApprovalState,
) -> AITraceRecord:
    records = load_ai_traces(state.root)
    updated: list[AITraceRecord] = []
    selected: AITraceRecord | None = None
    for record in records:
        if record.trace_id == trace_id:
            selected = record.model_copy(update={"approval_state": approval_state})
            updated.append(selected)
        else:
            updated.append(record)
    if selected is None:
        raise AITraceError(f"unknown AI trace ID: {trace_id}")

    trace_path = workspace_path(state.root, AI_TRACE_FILE, allowed_roots=("ai",))
    trace_path.write_text(
        "".join(_canonical_json(record.model_dump(mode="json")) for record in updated),
        encoding="utf-8",
    )
    append_audit_event(
        state,
        AuditEvent(
            timestamp=utc_now(),
            command="ai trace approval",
            output_path=AI_TRACE_FILE,
            output_sha256=file_sha256(trace_path),
            summary={"trace_id": trace_id, "approval_state": approval_state},
        ),
    )
    return selected


def _payload_sha256(payload: dict[str, Any]) -> str:
    return sha256(_canonical_json(payload).encode("utf-8")).hexdigest()


def _canonical_json(payload: object) -> str:
    return json.dumps(payload, sort_keys=True, separators=(",", ":")) + "\n"


__all__ = [
    "AI_TRACE_FILE",
    "AI_TRACE_SCHEMA_VERSION",
    "AIApprovalState",
    "AITraceError",
    "AITraceRecord",
    "AITraceResponse",
    "load_ai_traces",
    "record_ai_trace",
    "update_ai_trace_approval",
]
