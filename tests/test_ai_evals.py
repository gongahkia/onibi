from __future__ import annotations

from pathlib import Path

import pytest

from piranesi.ai import (
    AIEvaluationError,
    build_redacted_prompt_payload,
    evaluate_ai_output,
    record_ai_trace,
    require_trace_approved_for_report_change,
)
from piranesi.workspace import (
    EvidenceSnippet,
    NormalizedFinding,
    WorkspaceState,
    create_workspace,
    deterministic_finding_id,
    save_workspace,
    utc_now,
)


def test_ai_eval_passes_when_output_cites_existing_finding_and_evidence(
    tmp_path: Path,
) -> None:
    state = _workspace_with_finding(tmp_path)
    prompt = build_redacted_prompt_payload(state, purpose="eval")
    finding_id = state.findings.findings[0].id
    evidence_id = f"{finding_id}:evidence:1"

    result = evaluate_ai_output(
        prompt,
        output_text=f"Draft remediation for {finding_id} using {evidence_id}.",
    )

    assert result.passed is True


def test_ai_eval_rejects_sensitive_or_invented_output(tmp_path: Path) -> None:
    state = _workspace_with_finding(tmp_path)
    prompt = build_redacted_prompt_payload(state, purpose="eval")

    result = evaluate_ai_output(
        prompt,
        output_text=(
            "Create finding:invented for admin.internal.example with "
            "finding:invented:evidence:1 token=abc123."
        ),
    )

    assert result.passed is False
    assert {failure.code for failure in result.failures} == {
        "invented-evidence-id",
        "invented-finding-id",
        "unredacted-sensitive-output",
    }
    with pytest.raises(AIEvaluationError):
        result.require_passed()


def test_ai_eval_blocks_unapproved_report_changes(tmp_path: Path) -> None:
    state = _workspace_with_finding(tmp_path)
    prompt = build_redacted_prompt_payload(state, purpose="eval")
    draft_trace = record_ai_trace(
        state,
        prompt=prompt,
        provider={"name": "fake", "model": "fake", "external_call": False},
        output_text="Draft text",
        target={"field": "remediation", "finding_id": state.findings.findings[0].id},
        approval_state="draft",
    )
    accepted_trace = draft_trace.model_copy(update={"approval_state": "accepted"})

    with pytest.raises(AIEvaluationError, match="requires accepted approval state"):
        require_trace_approved_for_report_change(draft_trace, target_field="remediation")
    with pytest.raises(AIEvaluationError, match="cannot apply"):
        require_trace_approved_for_report_change(accepted_trace, target_field="executive_summary")

    require_trace_approved_for_report_change(accepted_trace, target_field="remediation")


def _workspace_with_finding(tmp_path: Path) -> WorkspaceState:
    state = create_workspace(tmp_path / "workspace")
    finding = NormalizedFinding(
        id=deterministic_finding_id("ai-eval", "finding"),
        title="Existing finding",
        severity="low",
        confidence="tool-observed",
        evidence=[
            EvidenceSnippet(
                kind="scanner-note",
                value="Existing evidence from imported scanner output.",
            )
        ],
        first_seen=utc_now(),
        last_seen=utc_now(),
    )
    updated = WorkspaceState(
        root=state.root,
        workspace=state.workspace,
        findings=state.findings.model_copy(update={"findings": [finding]}),
    )
    save_workspace(updated)
    return updated
