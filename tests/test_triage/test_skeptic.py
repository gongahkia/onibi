from __future__ import annotations

import json

import pytest

from piranesi.llm.router import TokenBudgetExceededError
from piranesi.triage import SkepticAgent

from ._helpers import RecordingProvider, build_candidate_finding


def test_skeptic_builds_prompt_with_sanitized_code_and_required_fields() -> None:
    finding = build_candidate_finding()
    provider = RecordingProvider(
        {
            ("skeptic", "skeptic-model"): [
                {
                    "verdict": "false_positive",
                    "confidence": 0.84,
                    "reasoning": "The helper wraps the dangerous call in a parameterized query.",
                    "mitigations_found": ["parameterized query helper"],
                    "remaining_risk": "",
                }
            ]
        }
    )
    skeptic = SkepticAgent(
        provider=provider,  # type: ignore[arg-type]
        model="skeptic-model",
        detector_model="detector-model",
    )

    messages = skeptic.build_messages(finding)
    user_prompt = messages[1]["content"]

    assert "CWE-89" in user_prompt
    assert "SQL Injection" in user_prompt
    assert "src/api/users.ts:18" in user_prompt
    assert "req.query.id -> ... -> db.query" in user_prompt
    assert "upstream validation note" not in user_prompt
    assert "query builder" not in user_prompt
    assert "sink call" not in user_prompt

    result = skeptic.analyze(finding)

    assert result.model == "skeptic-model"
    assert result.verdict == "false_positive"
    assert "parameterized query" in result.reasoning
    assert provider.calls[0]["stage"] == "skeptic"
    assert provider.calls[0]["tool_choice"] == {
        "type": "function",
        "function": {"name": "submit_skeptic_challenge"},
    }
    audit_record = json.loads(result.as_audit_record())
    assert audit_record["reasoning"] == result.reasoning


@pytest.mark.parametrize("verdict", ["genuine", "false_positive", "uncertain"])
def test_skeptic_parses_each_supported_verdict(verdict: str) -> None:
    finding = build_candidate_finding()
    provider = RecordingProvider(
        {
            ("skeptic", "skeptic-model"): [
                {
                    "verdict": verdict,
                    "confidence": 0.51,
                    "reasoning": f"verdict={verdict}",
                    "mitigations_found": ["guard"] if verdict == "false_positive" else [],
                    "remaining_risk": "manual review",
                }
            ]
        }
    )
    skeptic = SkepticAgent(
        provider=provider,  # type: ignore[arg-type]
        model="skeptic-model",
        detector_model="detector-model",
    )

    result = skeptic.analyze(finding)

    assert result.verdict == verdict
    assert result.confidence == pytest.approx(0.51)
    assert result.reasoning == f"verdict={verdict}"


def test_skeptic_gracefully_degrades_on_malformed_json_response() -> None:
    finding = build_candidate_finding()
    provider = RecordingProvider({("skeptic", "skeptic-model"): ["not-json-at-all"]})
    skeptic = SkepticAgent(
        provider=provider,  # type: ignore[arg-type]
        model="skeptic-model",
        detector_model="detector-model",
    )

    result = skeptic.analyze(finding)

    assert result.verdict == "uncertain"
    assert result.confidence == pytest.approx(0.0)
    assert "Malformed structured skeptic response" in result.reasoning
    assert result.remaining_risk == "Unable to parse skeptic output."


def test_skeptic_enforces_different_model_from_detector() -> None:
    finding = build_candidate_finding()
    skeptic = SkepticAgent(
        provider=RecordingProvider({}),  # type: ignore[arg-type]
        model="shared-model",
        detector_model="shared-model",
    )

    with pytest.raises(ValueError, match="skeptic model must differ from detector model"):
        skeptic.analyze(finding)


def test_skeptic_returns_uncertain_when_token_budget_is_exhausted() -> None:
    class _BudgetExhaustedProvider:
        def complete(self, **kwargs: object) -> object:
            raise TokenBudgetExceededError("token budget exhausted")

    skeptic = SkepticAgent(
        provider=_BudgetExhaustedProvider(),  # type: ignore[arg-type]
        model="skeptic-model",
        detector_model="detector-model",
    )

    result = skeptic.analyze(build_candidate_finding())

    assert result.verdict == "uncertain"
    assert "token budget" in result.reasoning.lower()
    assert "manual review" in result.remaining_risk.lower()
