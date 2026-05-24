from __future__ import annotations

from pathlib import Path

import pytest

from piranesi.ai import (
    AIDraftError,
    StaticLocalAIProvider,
    accept_executive_summary_draft,
    create_executive_summary_draft,
    local_provider_config,
    reject_executive_summary_draft,
)
from piranesi.report.pentest import build_pentest_report, render_markdown
from piranesi.workspace import (
    EvidenceSnippet,
    NormalizedFinding,
    WorkspaceState,
    create_workspace,
    deterministic_finding_id,
    save_workspace,
    utc_now,
)


def test_executive_summary_draft_changes_report_only_after_acceptance(
    tmp_path: Path,
) -> None:
    state = _workspace_with_finding(tmp_path)
    provider = StaticLocalAIProvider(
        config=local_provider_config(provider_name="fake-local", command=["fake"]),
        output_text="One medium finding remains open and needs owner review.",
    )

    draft = create_executive_summary_draft(state, provider=provider)
    before_accept = build_pentest_report(state, redact_sensitive_evidence=True)

    assert draft.ai_generated is True
    assert draft.status == "draft"
    assert "accepted_ai_draft" not in before_accept.executive_summary

    accepted = accept_executive_summary_draft(state, draft_id=draft.draft_id)
    after_accept = build_pentest_report(state, redact_sensitive_evidence=True)
    markdown = render_markdown(after_accept)

    assert accepted.status == "accepted"
    accepted_payload = after_accept.executive_summary["accepted_ai_draft"]
    assert accepted_payload["text"] == draft.text
    assert accepted_payload["trace_id"] == draft.trace_id
    assert "Accepted AI draft: One medium finding remains open" in markdown
    assert len(after_accept.findings) == 1


def test_executive_summary_draft_can_be_rejected_without_report_change(
    tmp_path: Path,
) -> None:
    state = _workspace_with_finding(tmp_path)
    provider = StaticLocalAIProvider(
        config=local_provider_config(provider_name="fake-local", command=["fake"]),
        output_text="Summary draft.",
    )

    draft = create_executive_summary_draft(state, provider=provider)
    rejected = reject_executive_summary_draft(state, draft_id=draft.draft_id)
    report = build_pentest_report(state, redact_sensitive_evidence=True)

    assert rejected.status == "rejected"
    assert "accepted_ai_draft" not in report.executive_summary
    with pytest.raises(AIDraftError, match="already rejected"):
        accept_executive_summary_draft(state, draft_id=draft.draft_id)


def test_executive_summary_draft_rejects_invented_findings(tmp_path: Path) -> None:
    state = _workspace_with_finding(tmp_path)
    provider = StaticLocalAIProvider(
        config=local_provider_config(provider_name="fake-local", command=["fake"]),
        output_text="Executive summary says finding:invented proves a new host.",
    )

    with pytest.raises(AIDraftError, match="invented-finding-id"):
        create_executive_summary_draft(state, provider=provider)


def _workspace_with_finding(tmp_path: Path) -> WorkspaceState:
    state = create_workspace(tmp_path / "workspace")
    finding = NormalizedFinding(
        id=deterministic_finding_id("ai-summary", "finding"),
        title="Open medium finding",
        severity="medium",
        confidence="tool-observed",
        evidence=[EvidenceSnippet(kind="scanner-note", value="Existing evidence.")],
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
