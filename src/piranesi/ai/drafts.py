from __future__ import annotations

import json
from hashlib import sha256
from pathlib import Path
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from piranesi.ai.evals import (
    AIEvaluationError,
    evaluate_ai_output,
    require_trace_approved_for_report_change,
)
from piranesi.ai.providers import AIProvider
from piranesi.ai.redaction import build_redacted_prompt_payload
from piranesi.ai.trace import (
    AITraceError,
    record_ai_trace,
    update_ai_trace_approval,
)
from piranesi.workspace import (
    FINDINGS_FILE,
    AuditEvent,
    NormalizedFinding,
    WorkspaceState,
    append_audit_event,
    file_sha256,
    load_workspace,
    save_workspace,
    utc_now,
    workspace_path,
)

AI_REMEDIATION_DRAFT_SCHEMA_VERSION: Literal["piranesi.ai-remediation-draft.v1"] = (
    "piranesi.ai-remediation-draft.v1"
)
DraftStatus = Literal["draft", "accepted", "rejected"]


class AIDraftError(ValueError):
    """Raised when an AI draft workflow cannot proceed safely."""


class _StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class AIRemediationDraft(_StrictModel):
    schema_version: Literal["piranesi.ai-remediation-draft.v1"] = (
        AI_REMEDIATION_DRAFT_SCHEMA_VERSION
    )
    draft_id: str
    finding_id: str
    text: str
    trace_id: str
    status: DraftStatus = "draft"
    ai_generated: bool = True
    created_at: str
    accepted_at: str | None = None
    rejected_at: str | None = None
    evaluation: dict[str, object] = Field(default_factory=dict)


def create_remediation_draft(
    state: WorkspaceState,
    *,
    finding_id: str,
    provider: AIProvider,
) -> AIRemediationDraft:
    finding = _finding_by_id(state, finding_id)
    prompt = build_redacted_prompt_payload(state, purpose="remediation-draft")
    provider_result = provider.complete(prompt)
    evaluation = evaluate_ai_output(prompt, output_text=provider_result.text)
    try:
        evaluation.require_passed()
    except AIEvaluationError as exc:
        raise AIDraftError(str(exc)) from exc
    trace = record_ai_trace(
        state,
        prompt=prompt,
        provider=provider.trace_metadata,
        output_text=provider_result.text,
        target={"field": "remediation", "finding_id": finding.id},
        response_metadata=provider_result.metadata,
    )
    draft = AIRemediationDraft(
        draft_id=_draft_id("remediation", finding.id, trace.trace_id),
        finding_id=finding.id,
        text=trace.response.text,
        trace_id=trace.trace_id,
        created_at=utc_now(),
        evaluation=evaluation.model_dump(mode="json"),
    )
    path = _remediation_draft_path(state, draft.draft_id)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(draft.model_dump(mode="json"), indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    append_audit_event(
        state,
        AuditEvent(
            timestamp=utc_now(),
            command="ai remediation draft",
            output_path=path.relative_to(state.root).as_posix(),
            output_sha256=file_sha256(path),
            summary={
                "draft_id": draft.draft_id,
                "finding_id": draft.finding_id,
                "trace_id": draft.trace_id,
                "status": draft.status,
            },
        ),
    )
    return draft


def accept_remediation_draft(
    state: WorkspaceState,
    *,
    draft_id: str,
) -> AIRemediationDraft:
    draft, path = load_remediation_draft(state.root, draft_id=draft_id)
    if draft.status != "draft":
        raise AIDraftError(f"remediation draft {draft_id} is already {draft.status}")

    approved_trace = update_ai_trace_approval(
        state,
        trace_id=draft.trace_id,
        approval_state="accepted",
    )
    require_trace_approved_for_report_change(approved_trace, target_field="remediation")
    current = load_workspace(state.root)
    findings = []
    changed = False
    for finding in current.findings.findings:
        if finding.id != draft.finding_id:
            findings.append(finding)
            continue
        changed = True
        provenance = {
            **finding.provenance,
            "ai_remediation_trace_id": draft.trace_id,
            "ai_remediation_draft_id": draft.draft_id,
            "ai_remediation_accepted_at": utc_now(),
        }
        findings.append(
            finding.model_copy(update={"remediation": draft.text, "provenance": provenance})
        )
    if not changed:
        raise AIDraftError(f"finding not found for remediation draft: {draft.finding_id}")
    updated_state = WorkspaceState(
        root=current.root,
        workspace=current.workspace,
        findings=current.findings.model_copy(update={"findings": findings}),
    )
    save_workspace(updated_state)
    accepted = draft.model_copy(update={"status": "accepted", "accepted_at": utc_now()})
    path.write_text(
        json.dumps(accepted.model_dump(mode="json"), indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    append_audit_event(
        updated_state,
        AuditEvent(
            timestamp=utc_now(),
            command="ai remediation accept",
            output_path=FINDINGS_FILE,
            output_sha256=file_sha256(updated_state.root / FINDINGS_FILE),
            summary={
                "draft_id": accepted.draft_id,
                "finding_id": accepted.finding_id,
                "trace_id": accepted.trace_id,
            },
        ),
    )
    return accepted


def reject_remediation_draft(
    state: WorkspaceState,
    *,
    draft_id: str,
) -> AIRemediationDraft:
    draft, path = load_remediation_draft(state.root, draft_id=draft_id)
    if draft.status != "draft":
        raise AIDraftError(f"remediation draft {draft_id} is already {draft.status}")
    try:
        update_ai_trace_approval(state, trace_id=draft.trace_id, approval_state="rejected")
    except AITraceError as exc:
        raise AIDraftError(str(exc)) from exc
    rejected = draft.model_copy(update={"status": "rejected", "rejected_at": utc_now()})
    path.write_text(
        json.dumps(rejected.model_dump(mode="json"), indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    append_audit_event(
        state,
        AuditEvent(
            timestamp=utc_now(),
            command="ai remediation reject",
            output_path=path.relative_to(state.root).as_posix(),
            output_sha256=file_sha256(path),
            summary={"draft_id": rejected.draft_id, "trace_id": rejected.trace_id},
        ),
    )
    return rejected


def load_remediation_draft(
    root: Path | str,
    *,
    draft_id: str,
) -> tuple[AIRemediationDraft, Path]:
    drafts_dir = workspace_path(root, "ai/drafts", allowed_roots=("ai",))
    for path in sorted(drafts_dir.glob("remediation-*.json")):
        try:
            draft = AIRemediationDraft.model_validate(json.loads(path.read_text(encoding="utf-8")))
        except (json.JSONDecodeError, ValidationError) as exc:
            raise AIDraftError(f"invalid remediation draft {path}: {exc}") from exc
        if draft.draft_id == draft_id:
            return draft, path
    raise AIDraftError(f"unknown remediation draft ID: {draft_id}")


def _finding_by_id(state: WorkspaceState, finding_id: str) -> NormalizedFinding:
    for finding in state.findings.findings:
        if finding.id == finding_id:
            return finding
    raise AIDraftError(f"unknown finding ID: {finding_id}")


def _remediation_draft_path(state: WorkspaceState, draft_id: str) -> Path:
    safe_id = draft_id.replace(":", "-")
    return workspace_path(
        state.root,
        f"ai/drafts/remediation-{safe_id}.json",
        allowed_roots=("ai",),
    )


def _draft_id(kind: str, finding_id: str, trace_id: str) -> str:
    digest = sha256(f"{kind}\0{finding_id}\0{trace_id}".encode()).hexdigest()[:24]
    return f"ai-draft:{kind}:{digest}"


__all__ = [
    "AI_REMEDIATION_DRAFT_SCHEMA_VERSION",
    "AIDraftError",
    "AIRemediationDraft",
    "DraftStatus",
    "accept_remediation_draft",
    "create_remediation_draft",
    "load_remediation_draft",
    "reject_remediation_draft",
]
