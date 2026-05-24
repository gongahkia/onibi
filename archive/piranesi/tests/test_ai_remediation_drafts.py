from __future__ import annotations

from pathlib import Path

import pytest

from piranesi.ai import (
    AIDraftError,
    StaticLocalAIProvider,
    accept_remediation_draft,
    create_remediation_draft,
    local_provider_config,
    reject_remediation_draft,
)
from piranesi.workspace import (
    EvidenceSnippet,
    NormalizedFinding,
    WorkspaceState,
    create_workspace,
    deterministic_finding_id,
    load_workspace,
    save_workspace,
    utc_now,
)


def test_remediation_draft_does_not_mutate_finding_until_accepted(tmp_path: Path) -> None:
    state = _workspace_with_finding(tmp_path)
    finding_id = state.findings.findings[0].id
    provider = StaticLocalAIProvider(
        config=local_provider_config(provider_name="fake-local", command=["fake"]),
        output_text=f"Remediate existing finding {finding_id} by removing the unsafe header.",
    )

    draft = create_remediation_draft(state, finding_id=finding_id, provider=provider)
    after_draft = load_workspace(state.root)

    assert draft.ai_generated is True
    assert draft.status == "draft"
    assert after_draft.findings.findings[0].remediation is None
    assert len(after_draft.findings.findings) == 1
    assert len(after_draft.findings.findings[0].evidence) == 1

    accepted = accept_remediation_draft(after_draft, draft_id=draft.draft_id)
    after_accept = load_workspace(state.root)
    finding = after_accept.findings.findings[0]

    assert accepted.status == "accepted"
    assert finding.remediation == draft.text
    assert finding.provenance["ai_remediation_trace_id"] == draft.trace_id
    assert len(after_accept.findings.findings) == 1
    assert len(finding.evidence) == 1


def test_remediation_draft_can_be_rejected_without_finding_change(tmp_path: Path) -> None:
    state = _workspace_with_finding(tmp_path)
    finding_id = state.findings.findings[0].id
    provider = StaticLocalAIProvider(
        config=local_provider_config(provider_name="fake-local", command=["fake"]),
        output_text="Remove the unsafe header.",
    )

    draft = create_remediation_draft(state, finding_id=finding_id, provider=provider)
    rejected = reject_remediation_draft(state, draft_id=draft.draft_id)
    after_reject = load_workspace(state.root)

    assert rejected.status == "rejected"
    assert after_reject.findings.findings[0].remediation is None
    with pytest.raises(AIDraftError, match="already rejected"):
        accept_remediation_draft(after_reject, draft_id=draft.draft_id)


def test_remediation_draft_rejects_invented_finding_output(tmp_path: Path) -> None:
    state = _workspace_with_finding(tmp_path)
    provider = StaticLocalAIProvider(
        config=local_provider_config(provider_name="fake-local", command=["fake"]),
        output_text="Create finding:invented and add evidence finding:invented:evidence:1.",
    )

    with pytest.raises(AIDraftError):
        create_remediation_draft(
            state,
            finding_id=state.findings.findings[0].id,
            provider=provider,
        )


def _workspace_with_finding(tmp_path: Path) -> WorkspaceState:
    state = create_workspace(tmp_path / "workspace")
    finding = NormalizedFinding(
        id=deterministic_finding_id("ai-remediation", "finding"),
        title="Unsafe header",
        severity="medium",
        confidence="tool-observed",
        evidence=[EvidenceSnippet(kind="scanner-note", value="Unsafe header observed.")],
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
