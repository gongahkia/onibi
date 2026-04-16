from __future__ import annotations

import pytest

from piranesi.llm.router import TokenBudgetExceededError
from piranesi.triage import CalibratedEnsembleVoter, SkepticAgent
from piranesi.triage.ensemble import _temperature_scale

from ._helpers import RecordingProvider, build_candidate_finding, build_sandbox_result


def test_ensemble_uses_majority_vote_without_calibration_data() -> None:
    finding = build_candidate_finding()
    provider = RecordingProvider(
        {
            ("triage", "m1"): [
                {
                    "verdict": "true_positive",
                    "confidence": 0.91,
                    "explanation": "Unsanitized input reaches SQL.",
                }
            ],
            ("triage", "m2"): [
                {
                    "verdict": "false_positive",
                    "confidence": 0.88,
                    "explanation": "ORM likely parameterizes here.",
                }
            ],
            ("triage", "m3"): [
                {
                    "verdict": "true_positive",
                    "confidence": 0.61,
                    "explanation": "Query string is interpolated.",
                }
            ],
        }
    )

    voter = CalibratedEnsembleVoter(
        provider=provider,  # type: ignore[arg-type]
        models=("m1", "m2", "m3"),
    )

    decision = voter.classify(finding)

    assert decision.verdict == "true_positive"
    assert decision.ensemble_score == pytest.approx(2 / 3)
    assert decision.escalated is False


def test_ensemble_applies_temperature_scaling_and_precision_weights() -> None:
    finding = build_candidate_finding()
    provider = RecordingProvider(
        {
            ("triage", "m1"): [
                {
                    "verdict": "true_positive",
                    "confidence": 0.90,
                    "explanation": "Direct interpolation.",
                }
            ],
            ("triage", "m2"): [
                {
                    "verdict": "false_positive",
                    "confidence": 0.80,
                    "explanation": "Potential ORM guard.",
                }
            ],
            ("triage", "m3"): [
                {
                    "verdict": "true_positive",
                    "confidence": 0.60,
                    "explanation": "Sink is still reachable.",
                }
            ],
        }
    )

    voter = CalibratedEnsembleVoter(
        provider=provider,  # type: ignore[arg-type]
        models=("m1", "m2", "m3"),
        calibration_temperatures={"m1": 2.0, "m2": 1.0, "m3": 1.0},
        historical_precision={"CWE-89: SQL Injection": {"m1": 0.8, "m2": 0.2, "m3": 1.0}},
    )

    decision = voter.classify(finding)
    expected = (
        0.8 * _temperature_scale(0.90, 2.0)
        + 0.2 * _temperature_scale(0.20, 1.0)
        + 1.0 * _temperature_scale(0.60, 1.0)
    ) / 2.0

    assert decision.ensemble_score == pytest.approx(expected)
    assert decision.verdict == "uncertain"
    assert [vote.weight for vote in decision.votes] == pytest.approx([0.8, 0.2, 1.0])


def test_ensemble_escalates_when_score_is_uncertain() -> None:
    finding = build_candidate_finding()
    provider = RecordingProvider(
        {
            ("triage", "m1"): [
                {
                    "verdict": "true_positive",
                    "confidence": 0.90,
                    "explanation": "Direct interpolation.",
                }
            ],
            ("triage", "m2"): [
                {
                    "verdict": "false_positive",
                    "confidence": 0.80,
                    "explanation": "Potential ORM guard.",
                }
            ],
            ("triage", "m3"): [
                {
                    "verdict": "true_positive",
                    "confidence": 0.60,
                    "explanation": "Sink is reachable.",
                }
            ],
            ("triage", "expensive-model"): [
                {
                    "verdict": "false_positive",
                    "confidence": 0.95,
                    "explanation": "A framework helper parameterizes the query.",
                }
            ],
        }
    )

    voter = CalibratedEnsembleVoter(
        provider=provider,  # type: ignore[arg-type]
        models=("m1", "m2", "m3"),
        calibration_temperatures={"m1": 2.0, "m2": 1.0, "m3": 1.0},
        escalation_model="expensive-model",
    )

    decision = voter.classify(finding)

    assert decision.escalated is True
    assert decision.escalation_model == "expensive-model"
    assert decision.verdict == "false_positive"
    assert decision.ensemble_score == pytest.approx(0.05)
    assert {call["model"] for call in provider.calls[:3]} == {"m1", "m2", "m3"}
    assert provider.calls[-1]["model"] == "expensive-model"


@pytest.mark.parametrize(
    ("verdict", "confidence", "expected_score"),
    [
        ("true_positive", 0.92, 1.0),
        ("false_positive", 0.87, 0.0),
    ],
)
def test_ensemble_handles_unanimous_votes(
    verdict: str,
    confidence: float,
    expected_score: float,
) -> None:
    finding = build_candidate_finding()
    provider = RecordingProvider(
        {
            ("triage", "m1"): [
                {
                    "verdict": verdict,
                    "confidence": confidence,
                    "explanation": "vote-1",
                }
            ],
            ("triage", "m2"): [
                {
                    "verdict": verdict,
                    "confidence": confidence,
                    "explanation": "vote-2",
                }
            ],
            ("triage", "m3"): [
                {
                    "verdict": verdict,
                    "confidence": confidence,
                    "explanation": "vote-3",
                }
            ],
        }
    )
    voter = CalibratedEnsembleVoter(
        provider=provider,  # type: ignore[arg-type]
        models=("m1", "m2", "m3"),
    )

    decision = voter.classify(finding)

    assert decision.verdict == verdict
    assert decision.ensemble_score == pytest.approx(expected_score)
    assert decision.escalated is False


def test_ensemble_returns_uncertain_for_perfect_split_without_escalation_model() -> None:
    finding = build_candidate_finding()
    provider = RecordingProvider(
        {
            ("triage", "m1"): [
                {
                    "verdict": "true_positive",
                    "confidence": 0.91,
                    "explanation": "unsafe interpolation",
                }
            ],
            ("triage", "m2"): [
                {
                    "verdict": "false_positive",
                    "confidence": 0.88,
                    "explanation": "framework helper may parameterize",
                }
            ],
        }
    )
    voter = CalibratedEnsembleVoter(
        provider=provider,  # type: ignore[arg-type]
        models=("m1", "m2"),
        num_models=2,
    )

    decision = voter.classify(finding)

    assert decision.verdict == "uncertain"
    assert decision.ensemble_score == pytest.approx(0.5)
    assert decision.escalated is False


def test_z3_verified_findings_cannot_be_suppressed() -> None:
    finding = build_candidate_finding()
    provider = RecordingProvider(
        {
            ("triage", "m1"): [
                {
                    "verdict": "false_positive",
                    "confidence": 0.99,
                    "explanation": "Should be ignored by invariant.",
                }
            ],
            ("triage", "m2"): [
                {
                    "verdict": "false_positive",
                    "confidence": 0.99,
                    "explanation": "Should be ignored by invariant.",
                }
            ],
            ("triage", "m3"): [
                {
                    "verdict": "false_positive",
                    "confidence": 0.99,
                    "explanation": "Should be ignored by invariant.",
                }
            ],
            ("skeptic", "skeptic-model"): [
                {
                    "verdict": "false_positive",
                    "confidence": 0.95,
                    "reasoning": "The skeptical analysis should not matter after verification.",
                    "mitigations_found": ["placeholder"],
                    "remaining_risk": "",
                }
            ],
        }
    )
    voter = CalibratedEnsembleVoter(
        provider=provider,  # type: ignore[arg-type]
        models=("m1", "m2", "m3"),
    )
    skeptic = SkepticAgent(
        provider=provider,  # type: ignore[arg-type]
        model="skeptic-model",
        detector_model="detector-model",
    )

    triaged = voter.triage_finding(
        finding,
        skeptic=skeptic,
        sandbox_result=build_sandbox_result(confirmed=True),
    )

    assert triaged.triage_verdict == "confirmed"
    assert triaged.triage_override_logged is True
    assert triaged.ensemble_score == pytest.approx(1.0)
    assert provider.calls == []


def test_ensemble_uses_conservative_fallback_when_token_budget_is_exhausted() -> None:
    class _BudgetExhaustedProvider:
        def complete(self, **kwargs: object) -> object:
            raise TokenBudgetExceededError("token budget exhausted")

    voter = CalibratedEnsembleVoter(
        provider=_BudgetExhaustedProvider(),  # type: ignore[arg-type]
        models=("triage-model",),
        num_models=1,
    )

    decision = voter.classify(build_candidate_finding())

    assert decision.verdict == "true_positive"
    assert decision.ensemble_score == pytest.approx(0.5)
    assert "token budget" in decision.votes[0].explanation.lower()
