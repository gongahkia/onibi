from __future__ import annotations

import json
from pathlib import Path

import pytest

from piranesi.ai import (
    AISuggestionError,
    StaticLocalAIProvider,
    accept_ai_suggestion_set,
    create_ai_suggestion_set,
    ignore_ai_suggestion_set,
    load_ai_suggestion_set,
    load_ai_traces,
    local_provider_config,
    reject_ai_suggestion_set,
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


def test_ai_suggestions_are_traceable_and_do_not_mutate_workspace(
    tmp_path: Path,
) -> None:
    state = _workspace_with_findings(tmp_path)
    first_id = state.findings.findings[0].id
    second_id = state.findings.findings[1].id
    provider = StaticLocalAIProvider(
        config=local_provider_config(provider_name="fake-local", command=["fake"]),
        output_text=json.dumps(
            {
                "suggestions": [
                    {
                        "kind": "dedupe-candidate",
                        "text": f"Compare {first_id} and {second_id} before final report.",
                        "finding_ids": [first_id, second_id],
                        "evidence_ids": [f"{first_id}:evidence:1"],
                        "report_fields": ["findings"],
                    },
                    {
                        "kind": "severity-rationale",
                        "text": f"Use {first_id}:evidence:1 to explain medium severity.",
                        "finding_ids": [first_id],
                        "evidence_ids": [f"{first_id}:evidence:1"],
                        "report_fields": ["severity"],
                    },
                    {
                        "kind": "retest-checklist",
                        "text": f"Retest the header behavior cited by {second_id}:evidence:1.",
                        "finding_ids": [second_id],
                        "evidence_ids": [f"{second_id}:evidence:1"],
                        "report_fields": ["retest_status"],
                    },
                ]
            }
        ),
    )

    suggestion_set = create_ai_suggestion_set(state, provider=provider)
    after_draft = load_workspace(state.root)
    traces = load_ai_traces(state.root)
    loaded, path = load_ai_suggestion_set(
        state.root,
        suggestion_set_id=suggestion_set.suggestion_set_id,
    )

    assert suggestion_set.status == "draft"
    assert suggestion_set.ai_generated is True
    assert len(suggestion_set.suggestions) == 3
    assert loaded == suggestion_set
    assert path.is_file()
    assert len(traces) == 1
    assert traces[0].approval_state == "draft"
    assert traces[0].target == {
        "field": "ai_suggestions",
        "kinds": ["dedupe-candidate", "severity-rationale", "retest-checklist"],
    }
    assert after_draft.findings == state.findings


@pytest.mark.parametrize(
    ("transition", "expected_status"),
    [
        (accept_ai_suggestion_set, "accepted"),
        (reject_ai_suggestion_set, "rejected"),
        (ignore_ai_suggestion_set, "ignored"),
    ],
)
def test_ai_suggestions_can_be_resolved_without_finding_changes(
    tmp_path: Path,
    transition,
    expected_status: str,
) -> None:
    state = _workspace_with_findings(tmp_path)
    finding_id = state.findings.findings[0].id
    provider = StaticLocalAIProvider(
        config=local_provider_config(provider_name="fake-local", command=["fake"]),
        output_text=json.dumps(
            {
                "suggestions": [
                    {
                        "kind": "retest-checklist",
                        "text": f"Retest existing finding {finding_id}.",
                        "finding_ids": [finding_id],
                        "evidence_ids": [f"{finding_id}:evidence:1"],
                        "report_fields": ["retest_status"],
                    }
                ]
            }
        ),
    )
    suggestion_set = create_ai_suggestion_set(state, provider=provider)

    resolved = transition(state, suggestion_set_id=suggestion_set.suggestion_set_id)
    after_resolution = load_workspace(state.root)
    traces = load_ai_traces(state.root)

    assert resolved.status == expected_status
    assert after_resolution.findings == state.findings
    assert traces[0].approval_state == expected_status
    with pytest.raises(AISuggestionError, match=f"already {expected_status}"):
        accept_ai_suggestion_set(state, suggestion_set_id=suggestion_set.suggestion_set_id)


def test_ai_suggestions_reject_invented_findings_or_evidence(
    tmp_path: Path,
) -> None:
    state = _workspace_with_findings(tmp_path)
    provider = StaticLocalAIProvider(
        config=local_provider_config(provider_name="fake-local", command=["fake"]),
        output_text=json.dumps(
            {
                "suggestions": [
                    {
                        "kind": "dedupe-candidate",
                        "text": "Create finding:invented from finding:invented:evidence:1.",
                        "finding_ids": ["finding:invented"],
                        "evidence_ids": ["finding:invented:evidence:1"],
                        "report_fields": ["findings"],
                    }
                ]
            }
        ),
    )

    with pytest.raises(AISuggestionError):
        create_ai_suggestion_set(state, provider=provider)
    assert load_workspace(state.root).findings == state.findings


def test_ai_suggestions_require_existing_evidence_or_report_field_citation(
    tmp_path: Path,
) -> None:
    state = _workspace_with_findings(tmp_path)
    finding_id = state.findings.findings[0].id
    provider = StaticLocalAIProvider(
        config=local_provider_config(provider_name="fake-local", command=["fake"]),
        output_text=json.dumps(
            {
                "suggestions": [
                    {
                        "kind": "severity-rationale",
                        "text": f"Severity note for {finding_id}.",
                        "finding_ids": [finding_id],
                    }
                ]
            }
        ),
    )

    with pytest.raises(AISuggestionError, match="must cite"):
        create_ai_suggestion_set(state, provider=provider)


def _workspace_with_findings(tmp_path: Path) -> WorkspaceState:
    state = create_workspace(tmp_path / "workspace")
    first = NormalizedFinding(
        id=deterministic_finding_id("ai-suggestions", "first"),
        title="Unsafe header",
        severity="medium",
        confidence="tool-observed",
        evidence=[EvidenceSnippet(kind="scanner-note", value="Unsafe header observed.")],
        first_seen=utc_now(),
        last_seen=utc_now(),
    )
    second = NormalizedFinding(
        id=deterministic_finding_id("ai-suggestions", "second"),
        title="Duplicate unsafe header",
        severity="medium",
        confidence="tool-observed",
        evidence=[EvidenceSnippet(kind="scanner-note", value="Same header on sibling route.")],
        first_seen=utc_now(),
        last_seen=utc_now(),
    )
    updated = WorkspaceState(
        root=state.root,
        workspace=state.workspace,
        findings=state.findings.model_copy(update={"findings": [first, second]}),
    )
    save_workspace(updated)
    return updated
