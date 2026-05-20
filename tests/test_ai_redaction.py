from __future__ import annotations

import json
from pathlib import Path

from piranesi.ai import PromptRedactionContext, build_redacted_prompt_payload
from piranesi.workspace import (
    EngagementMetadata,
    EvidenceSnippet,
    NormalizedFinding,
    ServiceContext,
    WorkspaceState,
    create_workspace,
    deterministic_finding_id,
    save_workspace,
    utc_now,
)


def test_redacted_prompt_payload_omits_sensitive_workspace_values(tmp_path: Path) -> None:
    state = create_workspace(
        tmp_path / "workspace",
        engagement=EngagementMetadata(
            client="Acme Sensitive Client",
            project="Portal Review",
            scope=["https://portal.acme-sensitive.example/login"],
        ),
    )
    finding_id = deterministic_finding_id("ai-redaction", "portal")
    finding = NormalizedFinding(
        id=finding_id,
        title="Session cookie disclosure on portal.acme-sensitive.example",
        severity="high",
        confidence="tool-observed",
        description="Portal Review exposed cookie=session-value on portal.acme-sensitive.example.",
        remediation="Rotate password=hunter2 and remove diagnostic headers.",
        asset="portal.acme-sensitive.example",
        service=ServiceContext(port=443, protocol="https", name="https"),
        evidence=[
            EvidenceSnippet(
                kind="http-request",
                value=(
                    "GET / HTTP/1.1\n"
                    "Host: portal.acme-sensitive.example\n"
                    "Authorization: Bearer live-secret-token\n"
                ),
                redacted=True,
                locator="https://portal.acme-sensitive.example/",
            ),
            EvidenceSnippet(
                kind="scanner-note",
                value="token=fixture-token on 10.10.10.8",
                redacted=False,
            ),
        ],
        first_seen=utc_now(),
        last_seen=utc_now(),
    )
    state = WorkspaceState(
        root=state.root,
        workspace=state.workspace,
        findings=state.findings.model_copy(update={"findings": [finding]}),
    )
    save_workspace(state)

    payload = build_redacted_prompt_payload(state, purpose="test-redaction")
    encoded = json.dumps(payload.provider_payload(), sort_keys=True)

    assert payload.schema_version == "piranesi.ai.prompt.v1"
    assert finding_id in encoded
    assert f"{finding_id}:evidence:1" in encoded
    assert "http-request evidence" in encoded
    for forbidden in [
        "Acme Sensitive Client",
        "Portal Review",
        "portal.acme-sensitive.example",
        "live-secret-token",
        "session-value",
        "hunter2",
        "10.10.10.8",
    ]:
        assert forbidden not in encoded
    assert "[host:1]" in encoded
    assert any(event.category == "raw-evidence" for event in payload.redactions)
    assert any(event.category == "secret" for event in payload.redactions)


def test_redact_text_for_prompt_can_be_inspected_without_workspace() -> None:
    context = PromptRedactionContext(client_identifiers={"ClientName"})

    redacted = context.redact(
        "ClientName sent api_key=abc123 to https://api.clientname.example/v1",
        field="unit",
    )

    assert redacted is not None
    assert "ClientName" not in redacted
    assert "abc123" not in redacted
    assert "api.clientname.example" not in redacted
    assert "[client:1]" in redacted
    assert "[redacted secret]" in redacted
