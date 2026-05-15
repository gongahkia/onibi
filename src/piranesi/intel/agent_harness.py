from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

AgentOperation = Literal["run", "parse", "normalize", "score", "explain"]


def _default_agent_operations() -> list[AgentOperation]:
    return ["run", "parse", "normalize", "score", "explain"]


class AgentActionRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    operation: AgentOperation
    mutating: bool = False
    includes_sensitive_evidence: bool = False
    payload: dict[str, object] = Field(default_factory=dict)


class AgentPolicy(BaseModel):
    model_config = ConfigDict(extra="forbid")

    allow_mutating_operations: bool = False
    allow_sensitive_evidence: bool = False
    allowed_operations: list[AgentOperation] = Field(default_factory=_default_agent_operations)


class AgentPolicyDecision(BaseModel):
    model_config = ConfigDict(extra="forbid")

    allowed: bool
    reason: str


def enforce_agent_policy(request: AgentActionRequest, policy: AgentPolicy) -> AgentPolicyDecision:
    if request.operation not in policy.allowed_operations:
        return AgentPolicyDecision(
            allowed=False,
            reason=f"operation '{request.operation}' is not in policy allowlist",
        )

    if request.mutating and not policy.allow_mutating_operations:
        return AgentPolicyDecision(
            allowed=False,
            reason="mutating operations are denied by policy",
        )

    if request.includes_sensitive_evidence and not policy.allow_sensitive_evidence:
        return AgentPolicyDecision(
            allowed=False,
            reason="sensitive evidence handling is denied by policy",
        )

    return AgentPolicyDecision(allowed=True, reason="policy check passed")
