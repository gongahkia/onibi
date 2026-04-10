# Phase 19: Ensemble Calibration + LLM Cost Optimization

**Estimated effort: 20-30 ideal hours**
**Blocked by: Phase 9 (sufficient ground truth), Phase 4 (ensemble voter)**
**Blocks: Nothing**
**Target milestone: v1.0**

---

## 1. Phase Overview

Piranesi's ensemble voter uses uncalibrated self-reported confidence from LLMs. The RISKS_AND_OPEN_QUESTIONS.md doc acknowledges this is "a hack." With 149 ground truth entries now available, we can compute empirical calibration correction factors per model, per CWE — making triage verdicts meaningful instead of arbitrary.

This phase also adds cost-aware model routing: automatically select cheaper models for easy findings and expensive models only for ambiguous cases.

---

## 2. Calibration Pipeline

**Estimated effort: 10-12h**

### 2.1 Ground Truth Evaluation Run

Implement `eval/calibrate.py`:

1. Run Piranesi's triage stage against all 149 ground truth entries with each configured model.
2. For each (model, finding) pair, record: reported confidence, actual correctness (TP/FP from ground truth label).
3. Output: `eval/calibration/{model_name}.json` with raw data.

### 2.2 Platt Scaling (Logistic Calibration)

For each model, fit a logistic regression:
```
P(correct | reported_confidence) = 1 / (1 + exp(-(a * reported_confidence + b)))
```

Parameters `a` and `b` are learned from the ground truth evaluation. This maps raw LLM confidence to calibrated probability.

### 2.3 Per-CWE Calibration

Models may be better calibrated for some CWE classes than others. Compute per-CWE correction factors when sample size permits (n >= 10):

```python
calibration = {
    "gpt-4o": {
        "global": {"a": 1.2, "b": -0.3},
        "CWE-89": {"a": 1.5, "b": -0.1},  # better on SQLi
        "CWE-79": {"a": 0.9, "b": -0.5},  # overconfident on XSS
    }
}
```

### 2.4 Integration

Update `src/piranesi/triage/ensemble.py`:
- Load calibration data from `eval/calibration/` if available
- Apply Platt scaling to model-reported confidence before aggregation
- Fall back to uncalibrated confidence if no calibration data exists

---

## 3. Cost-Aware Model Routing

**Estimated effort: 8-10h**

### 3.1 Difficulty Estimation

Before sending a finding to the triage ensemble, estimate its "difficulty":

| Signal | Easy (cheap model) | Hard (expensive model) |
|--------|-------------------|----------------------|
| CWE class | CWE-89 (well-understood) | CWE-918 (context-dependent) |
| Path length | Short (2-3 steps) | Long (5+ steps) |
| Sanitizers on path | None | Multiple partial sanitizers |
| Historical accuracy | Model consistently correct | Model historically wrong |

### 3.2 Routing Logic

```python
def select_triage_model(finding: CandidateFinding, budget_remaining: float) -> str:
    difficulty = estimate_difficulty(finding)
    if difficulty < 0.3 and budget_remaining > 0.5:
        return config.models.triage_cheap  # e.g., "gpt-4o-mini"
    elif difficulty > 0.7 or budget_remaining > 2.0:
        return config.models.triage_expensive  # e.g., "gpt-4o"
    else:
        return config.models.triage  # default
```

### 3.3 Budget Optimization

Track cost per finding and optimize:
- If a cheap model agrees with the expensive model on 90% of easy findings, route easy findings to cheap model by default.
- Use the expensive model only for the 10% ambiguous cases.
- Target: 50% cost reduction with < 2% accuracy loss.

---

## 4. Confidence Threshold Tuning

**Estimated effort: 4-5h**

### 4.1 Optimal Threshold Search

Current thresholds are hardcoded: >= 0.7 → TP, <= 0.3 → FP. Use ground truth to find optimal thresholds:

```python
def find_optimal_thresholds(
    calibrated_scores: list[float],
    labels: list[bool],
) -> tuple[float, float]:
    """Find TP/FP thresholds that maximize F1 score."""
```

### 4.2 Per-CWE Thresholds

Different CWE classes may need different thresholds:
- SQLi: high threshold (0.8) because false negatives are dangerous
- XSS: lower threshold (0.6) because context matters more

---

## 5. Acceptance Criteria

- [ ] `eval/calibrate.py` runs triage against all ground truth with multiple models
- [ ] Platt scaling parameters computed and stored per model
- [ ] Per-CWE calibration when n >= 10
- [ ] Calibrated confidence used in ensemble aggregation
- [ ] Cost-aware routing: cheap model for easy findings, expensive for hard
- [ ] 50% cost reduction target with < 2% accuracy loss
- [ ] Optimal TP/FP thresholds computed from ground truth
- [ ] Calibration data versioned with ground truth version
