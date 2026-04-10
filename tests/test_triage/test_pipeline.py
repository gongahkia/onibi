from __future__ import annotations

import json

from piranesi.triage import CalibratedEnsembleVoter, SkepticAgent

from ._helpers import RecordingProvider, build_candidate_finding


def test_full_triage_pipeline_errs_on_side_of_caution_when_skeptic_disagrees() -> None:
    finding = build_candidate_finding()
    provider = RecordingProvider(
        {
            ("triage", "m1"): [
                {
                    "verdict": "false_positive",
                    "confidence": 0.91,
                    "explanation": "framework helper may parameterize",
                }
            ],
            ("triage", "m2"): [
                {
                    "verdict": "false_positive",
                    "confidence": 0.83,
                    "explanation": "query appears wrapped by a library",
                }
            ],
            ("triage", "m3"): [
                {
                    "verdict": "false_positive",
                    "confidence": 0.88,
                    "explanation": "taint may stop before the sink",
                }
            ],
            ("skeptic", "skeptic-model"): [
                {
                    "verdict": "genuine",
                    "confidence": 0.74,
                    "reasoning": "The string interpolation still reaches db.query directly.",
                    "mitigations_found": [],
                    "remaining_risk": "Injection remains exploitable.",
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

    triaged = voter.triage_finding(finding, skeptic=skeptic)

    assert triaged.triage_verdict == "true_positive"
    assert triaged.ensemble_score == 0.0
    assert triaged.escalated is False
    assert {call["stage"] for call in provider.calls} == {"triage", "skeptic"}
    assert len(provider.calls) == 4
    audit_record = json.loads(triaged.skeptic_analysis)
    assert audit_record["verdict"] == "genuine"
    assert "exploitable" in audit_record["remaining_risk"].lower()


def test_full_triage_pipeline_filters_when_ensemble_and_skeptic_both_call_false_positive() -> None:
    finding = build_candidate_finding()
    provider = RecordingProvider(
        {
            ("triage", "m1"): [
                {
                    "verdict": "false_positive",
                    "confidence": 0.91,
                    "explanation": "helper likely parameterizes",
                }
            ],
            ("triage", "m2"): [
                {
                    "verdict": "false_positive",
                    "confidence": 0.90,
                    "explanation": "sink receives a prepared statement",
                }
            ],
            ("triage", "m3"): [
                {
                    "verdict": "false_positive",
                    "confidence": 0.87,
                    "explanation": "sanitizer seems to terminate taint",
                }
            ],
            ("skeptic", "skeptic-model"): [
                {
                    "verdict": "false_positive",
                    "confidence": 0.81,
                    "reasoning": "A parameterized helper wraps the sink call.",
                    "mitigations_found": ["parameterized helper"],
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

    triaged = voter.triage_finding(finding, skeptic=skeptic)

    assert triaged.triage_verdict == "false_positive"
    assert triaged.ensemble_score == 0.0
    assert triaged.escalated is False
