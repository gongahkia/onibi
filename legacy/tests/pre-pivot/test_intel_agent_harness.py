from __future__ import annotations

from piranesi.intel.agent_harness import AgentActionRequest, AgentPolicy, enforce_agent_policy


def test_agent_policy_blocks_mutating_by_default() -> None:
    policy = AgentPolicy()
    request = AgentActionRequest(operation="run", mutating=True, payload={"command": "rm -rf /"})

    decision = enforce_agent_policy(request, policy)

    assert decision.allowed is False
    assert "mutating operations" in decision.reason


def test_agent_policy_blocks_sensitive_evidence_without_explicit_permission() -> None:
    policy = AgentPolicy(allow_mutating_operations=True)
    request = AgentActionRequest(
        operation="explain",
        includes_sensitive_evidence=True,
        payload={"raw_evidence": "token=secret"},
    )

    decision = enforce_agent_policy(request, policy)

    assert decision.allowed is False
    assert "sensitive evidence" in decision.reason


def test_agent_policy_allows_whitelisted_read_only_operation() -> None:
    policy = AgentPolicy(allowed_operations=["parse", "normalize"])
    request = AgentActionRequest(operation="parse", mutating=False)

    decision = enforce_agent_policy(request, policy)

    assert decision.allowed is True
