from __future__ import annotations

import json
from pathlib import Path

from piranesi.ai import build_redacted_prompt_payload, load_ai_traces, record_ai_trace
from piranesi.signing import sign_workspace, verify_workspace
from piranesi.workspace import (
    EngagementMetadata,
    EvidenceSnippet,
    NormalizedFinding,
    WorkspaceState,
    create_workspace,
    deterministic_finding_id,
    save_workspace,
    utc_now,
)


def test_ai_trace_records_redacted_prompt_output_and_audit_event(tmp_path: Path) -> None:
    state = _workspace_with_finding(tmp_path)
    prompt = build_redacted_prompt_payload(state, purpose="remediation-draft")

    record = record_ai_trace(
        state,
        prompt=prompt,
        provider={"name": "fake", "model": "fake-model", "external_call": False},
        output_text=(
            "Tell Acme Sensitive Client to rotate token=abc123 on "
            "https://portal.acme-sensitive.example"
        ),
        target={"finding_id": state.findings.findings[0].id, "field": "remediation"},
        response_metadata={"finish_reason": "stop"},
    )

    traces = load_ai_traces(state.root)
    encoded = (state.root / "ai" / "traces.jsonl").read_text(encoding="utf-8")
    audit_lines = (state.root / "audit-log.jsonl").read_text(encoding="utf-8").splitlines()

    assert traces == [record]
    assert record.approval_state == "draft"
    assert record.prompt_sha256
    assert record.response_sha256
    assert record.chain_of_custody["trace_path"] == "ai/traces.jsonl"
    assert "Acme Sensitive Client" not in encoded
    assert "abc123" not in encoded
    assert "portal.acme-sensitive.example" not in encoded
    assert json.loads(audit_lines[-1])["command"] == "ai trace"
    assert json.loads(audit_lines[-1])["summary"]["trace_id"] == record.trace_id


def test_ai_trace_log_is_covered_by_chain_of_custody(tmp_path: Path) -> None:
    state = _workspace_with_finding(tmp_path)
    prompt = build_redacted_prompt_payload(state, purpose="summary-draft")
    record_ai_trace(
        state,
        prompt=prompt,
        provider={"name": "fake", "model": "fake-model", "external_call": False},
        output_text="Draft summary from existing evidence only.",
        target={"field": "executive_summary"},
    )

    _manifest, manifest_path = sign_workspace(state.root)
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))

    assert any(
        artifact["path"] == "ai/traces.jsonl" and artifact["role"] == "ai-trace"
        for artifact in manifest["artifacts"]
    )

    trace_path = state.root / "ai" / "traces.jsonl"
    trace_path.write_text(trace_path.read_text(encoding="utf-8") + "\n", encoding="utf-8")
    result = verify_workspace(state.root, manifest_path=manifest_path)

    assert result.ok is False
    assert any(failure.path == "ai/traces.jsonl" for failure in result.failures)


def _workspace_with_finding(tmp_path: Path) -> WorkspaceState:
    state = create_workspace(
        tmp_path / "workspace",
        engagement=EngagementMetadata(
            client="Acme Sensitive Client",
            project="Portal Review",
            scope=["portal.acme-sensitive.example"],
        ),
    )
    finding = NormalizedFinding(
        id=deterministic_finding_id("ai-trace", "finding"),
        title="Portal issue",
        severity="medium",
        confidence="tool-observed",
        evidence=[
            EvidenceSnippet(
                kind="scanner-note",
                value="Observed diagnostic header on portal.acme-sensitive.example",
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
