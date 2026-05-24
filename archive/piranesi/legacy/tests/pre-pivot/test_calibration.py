"""Tests for ensemble calibration and cost-aware routing."""

from __future__ import annotations

import math
from pathlib import Path

import pytest
from eval.calibrate import (
    CalibrationData,
    PlattParams,
    TrialResult,
    build_calibration,
    find_optimal_thresholds,
    fit_platt_scaling,
    load_calibration,
    save_calibration,
)

from piranesi.config import BudgetConfig, ModelsConfig, PiranesiConfig
from piranesi.llm.cost import CostTracker
from piranesi.llm.router import ModelRouter, estimate_difficulty
from piranesi.triage.ensemble import CalibratedEnsembleVoter
from tests.test_triage._helpers import RecordingProvider, build_candidate_finding

# --- Platt scaling ---


def test_platt_params_identity_when_a1_b0() -> None:
    p = PlattParams(a=1.0, b=0.0)
    assert p.transform(0.0) == pytest.approx(0.5)
    assert p.transform(5.0) == pytest.approx(1.0 / (1.0 + math.exp(-5.0)))


def test_platt_params_with_bias() -> None:
    p = PlattParams(a=2.0, b=-1.0)
    result = p.transform(0.8)
    expected = 1.0 / (1.0 + math.exp(-(2.0 * 0.8 - 1.0)))
    assert result == pytest.approx(expected)


def test_fit_platt_scaling_separable_data() -> None:
    confidences = [0.9, 0.85, 0.8, 0.1, 0.15, 0.2]
    labels = [True, True, True, False, False, False]
    params = fit_platt_scaling(confidences, labels)
    assert params.transform(0.9) > 0.5  # high conf → high prob
    assert params.transform(0.1) < 0.5  # low conf → low prob


def test_fit_platt_scaling_empty_data() -> None:
    params = fit_platt_scaling([], [])
    assert params.a == 1.0
    assert params.b == 0.0


def test_fit_platt_scaling_all_correct() -> None:
    confidences = [0.8, 0.9, 0.7, 0.85]
    labels = [True, True, True, True]
    params = fit_platt_scaling(confidences, labels)
    assert params.transform(0.8) > 0.7  # should map high confidence → high calibrated


# --- threshold search ---


def test_find_optimal_thresholds_returns_valid_bounds() -> None:
    scores = [0.9, 0.85, 0.1, 0.15, 0.5]
    labels = [True, True, False, False, True]
    tp_thresh, fp_thresh = find_optimal_thresholds(scores, labels)
    assert 0.5 <= tp_thresh <= 1.0
    assert 0.0 <= fp_thresh <= 0.5
    assert fp_thresh < tp_thresh


def test_find_optimal_thresholds_empty() -> None:
    tp, fp = find_optimal_thresholds([], [])
    assert tp == 0.7
    assert fp == 0.3


def test_find_optimal_thresholds_perfect_separation() -> None:
    scores = [0.95, 0.90, 0.85, 0.05, 0.10, 0.15]
    labels = [True, True, True, False, False, False]
    tp_thresh, _fp_thresh = find_optimal_thresholds(scores, labels)
    assert tp_thresh <= 0.90  # should find threshold that captures all TPs


# --- build_calibration ---


def test_build_calibration_from_trials() -> None:
    trials = [
        TrialResult("gt-1", "CWE-89", "true_positive", "m1", 0.9, "true_positive", True),
        TrialResult("gt-2", "CWE-89", "false_positive", "m1", 0.8, "false_positive", True),
        TrialResult("gt-3", "CWE-89", "true_positive", "m1", 0.7, "true_positive", True),
        TrialResult("gt-4", "CWE-79", "true_positive", "m1", 0.6, "false_positive", False),
        TrialResult("gt-5", "CWE-79", "false_positive", "m1", 0.3, "true_positive", False),
    ]
    cal = build_calibration("m1", trials, gt_version="gt-5")
    assert cal.model == "m1"
    assert cal.n_samples == 5
    assert cal.n_correct == 3
    assert cal.raw_accuracy == pytest.approx(0.6)
    assert cal.gt_version == "gt-5"
    assert cal.cwe_params == {}  # < 10 samples per CWE


def test_build_calibration_per_cwe_when_enough_samples() -> None:
    trials = [
        TrialResult(
            f"gt-{i}", "CWE-89", "true_positive", "m1", 0.8 + i * 0.01, "true_positive", True
        )
        for i in range(12)
    ]
    cal = build_calibration("m1", trials)
    assert "CWE-89" in cal.cwe_params


def test_build_calibration_empty_model() -> None:
    cal = build_calibration("m1", [])
    assert cal.n_samples == 0
    assert cal.raw_accuracy == 0.0


# --- save/load roundtrip ---


def test_calibration_save_load_roundtrip(tmp_path: Path) -> None:
    cal = CalibrationData(
        model="gpt-4o",
        global_params=PlattParams(a=1.5, b=-0.3),
        cwe_params={"CWE-89": PlattParams(a=2.0, b=-0.1)},
        n_samples=100,
        n_correct=85,
        raw_accuracy=0.85,
        optimal_tp_threshold=0.75,
        optimal_fp_threshold=0.25,
        gt_version="gt-149",
    )
    save_calibration(cal, tmp_path)
    loaded = load_calibration("gpt-4o", tmp_path)
    assert loaded is not None
    assert loaded.model == "gpt-4o"
    assert loaded.global_params.a == pytest.approx(1.5)
    assert loaded.global_params.b == pytest.approx(-0.3)
    assert "CWE-89" in loaded.cwe_params
    assert loaded.n_samples == 100
    assert loaded.optimal_tp_threshold == pytest.approx(0.75)


def test_load_calibration_returns_none_when_missing(tmp_path: Path) -> None:
    assert load_calibration("nonexistent-model", tmp_path) is None


def test_calibration_data_calibrate_uses_cwe_override() -> None:
    cal = CalibrationData(
        model="m1",
        global_params=PlattParams(a=1.0, b=0.0),
        cwe_params={"CWE-89": PlattParams(a=3.0, b=-1.0)},
        n_samples=50,
        n_correct=40,
        raw_accuracy=0.8,
        optimal_tp_threshold=0.7,
        optimal_fp_threshold=0.3,
        gt_version="gt-50",
    )
    global_result = cal.calibrate(0.8)
    cwe_result = cal.calibrate(0.8, "CWE-89")
    assert global_result != cwe_result  # CWE-specific params differ


# --- difficulty estimation ---


def test_estimate_difficulty_simple_sqli() -> None:
    finding = build_candidate_finding("CWE-89: SQL Injection")
    d = estimate_difficulty(finding)
    assert 0.0 <= d <= 1.0
    assert d < 0.4  # sqli with short path = easy


def test_estimate_difficulty_ssrf_higher() -> None:
    finding = build_candidate_finding("CWE-918: SSRF")
    d = estimate_difficulty(finding)
    assert d > estimate_difficulty(build_candidate_finding("CWE-89: SQL Injection"))


def test_estimate_difficulty_no_cwe_prefix() -> None:
    finding = build_candidate_finding("sql_injection")
    d = estimate_difficulty(finding)
    assert 0.0 <= d <= 1.0  # should not crash


# --- cost-aware routing ---


def test_select_triage_model_cheap_for_easy() -> None:
    config = PiranesiConfig(
        models=ModelsConfig(triage="expensive-model"),
        budget=BudgetConfig(max_cost_usd=10.0),
    )
    from piranesi.config import ModelFallbackConfig

    config = PiranesiConfig(
        models=ModelsConfig(triage="expensive-model"),
        models_fallback=ModelFallbackConfig(default="cheap-model"),
        budget=BudgetConfig(max_cost_usd=10.0),
    )
    tracker = CostTracker()
    router = ModelRouter(config=config, cost_tracker=tracker)
    finding = build_candidate_finding("CWE-89: SQL Injection")  # easy
    selected = router.select_triage_model(finding)
    assert selected == "cheap-model"


def test_select_triage_model_expensive_for_hard() -> None:
    config = PiranesiConfig(
        models=ModelsConfig(triage="expensive-model"),
        budget=BudgetConfig(max_cost_usd=10.0),
    )
    tracker = CostTracker()
    router = ModelRouter(config=config, cost_tracker=tracker)
    finding = build_candidate_finding("CWE-918: SSRF")  # hard
    selected = router.select_triage_model(finding)
    assert selected == "expensive-model"


def test_select_triage_model_respects_budget() -> None:
    from piranesi.llm.router import BudgetExceededError

    config = PiranesiConfig(
        models=ModelsConfig(triage="model"),
        budget=BudgetConfig(max_cost_usd=1.0),
    )
    tracker = CostTracker()
    tracker.add(1.0, "triage")
    router = ModelRouter(config=config, cost_tracker=tracker)
    finding = build_candidate_finding()
    with pytest.raises(BudgetExceededError):
        router.select_triage_model(finding)


# --- ensemble with calibration ---


def test_ensemble_loads_platt_calibration(tmp_path: Path) -> None:
    cal = CalibrationData(
        model="m1",
        global_params=PlattParams(a=2.0, b=-0.5),
        cwe_params={},
        n_samples=50,
        n_correct=40,
        raw_accuracy=0.8,
        optimal_tp_threshold=0.65,
        optimal_fp_threshold=0.35,
        gt_version="gt-50",
    )
    save_calibration(cal, tmp_path)
    provider = RecordingProvider(
        {
            ("triage", "m1"): [
                {
                    "verdict": "true_positive",
                    "confidence": 0.85,
                    "explanation": "Direct interpolation.",
                }
            ],
        }
    )
    voter = CalibratedEnsembleVoter(
        provider=provider,  # type: ignore[arg-type]
        models=("m1",),
        calibration_dir=tmp_path,
    )
    decision = voter.classify(build_candidate_finding())
    # should have applied Platt scaling
    assert decision.votes[0].calibrated_true_positive_score is not None


def test_ensemble_falls_back_without_calibration() -> None:
    provider = RecordingProvider(
        {
            ("triage", "m1"): [
                {
                    "verdict": "true_positive",
                    "confidence": 0.85,
                    "explanation": "Direct interpolation.",
                }
            ],
        }
    )
    voter = CalibratedEnsembleVoter(
        provider=provider,  # type: ignore[arg-type]
        models=("m1",),
        calibration_dir=Path("/nonexistent"),
    )
    decision = voter.classify(build_candidate_finding())
    # should work fine without calibration data
    assert decision.verdict == "true_positive"
    assert decision.votes[0].calibrated_true_positive_score is None


# --- serialization ---


def test_calibration_data_dict_roundtrip() -> None:
    cal = CalibrationData(
        model="test",
        global_params=PlattParams(a=1.2, b=-0.3),
        cwe_params={"CWE-89": PlattParams(a=1.5, b=-0.1)},
        n_samples=100,
        n_correct=85,
        raw_accuracy=0.85,
        optimal_tp_threshold=0.72,
        optimal_fp_threshold=0.28,
        gt_version="gt-100",
    )
    d = cal.to_dict()
    restored = CalibrationData.from_dict(d)
    assert restored.model == cal.model
    assert restored.global_params.a == pytest.approx(cal.global_params.a)
    assert restored.cwe_params["CWE-89"].b == pytest.approx(cal.cwe_params["CWE-89"].b)
    assert restored.optimal_tp_threshold == pytest.approx(cal.optimal_tp_threshold)
