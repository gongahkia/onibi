# Phase 33: ML-Based False Positive Reduction

**Estimated effort: 30-40 ideal hours**
**Blocked by: Phase 8 (FP reduction baseline), Phase 19 (calibration infrastructure)**
**Blocks: None (additive pre-filter layer)**
**Target milestone: v0.4.0**

---

## 1. Overview

### 1.1 Problem

LLM-based triage via `CalibratedEnsembleVoter` (in `src/piranesi/triage/ensemble.py`) is effective at classifying findings as TP/FP. Each finding requires 1-3 LLM calls (ensemble voting + optional skeptic + optional escalation). Cost per finding: $0.01-0.10 depending on model tier and token count.

For a large codebase producing 500+ candidate findings, LLM triage alone costs $5-50 per scan. This is the dominant cost in the pipeline budget (`budget.max_cost_usd`).

A significant fraction of these findings are obvious false positives — parameterized queries flagged as SQLi, HTML-escaped output flagged as XSS, unreachable code paths, etc. These do not require LLM reasoning to dismiss.

### 1.2 Goal

Train a lightweight scikit-learn classifier as a **pre-filter** before LLM triage. The classifier runs locally, costs zero per prediction, and filters out obvious FPs before expensive LLM calls.

### 1.3 Expected Outcome

- 60-80% of obvious FPs caught by ML classifier (marked as `likely_fp`, excluded from LLM triage)
- Remaining 20-40% of ambiguous findings escalated to LLM ensemble as before
- **Zero real vulnerabilities missed** — optimize for recall >= 95% at the filtering threshold
- Net LLM cost reduction: 40-60% per scan
- Zero external ML service dependency — model trains and runs locally via scikit-learn

### 1.4 Architecture

```
CandidateFinding[] ──► ML Classifier (local, ~1ms/finding)
                           │
                    ┌──────┴──────┐
                    │             │
              P(TP) < 0.5   P(TP) >= 0.5
                    │             │
             mark likely_fp   escalate to
             (skip LLM)      LLM ensemble
```

---

## 2. Feature Engineering

### 2.1 Feature Vector Schema

Each `CandidateFinding` is transformed into a fixed-width numeric feature vector. Features are extracted from the finding's `CandidateFinding` model (defined in `src/piranesi/models/finding.py`) and its constituent `TaintSource`, `TaintSink`, `TaintStep`, and `PathCondition` objects (from `src/piranesi/models/taint.py`).

| # | Feature Name | Type | Source | Extraction |
|---|---|---|---|---|
| 1 | `cwe_id` | categorical (one-hot) | `finding.vuln_class` | Parse CWE-XX from vuln_class string, one-hot encode top 15 CWEs + "other" bucket |
| 2 | `confidence` | float [0,1] | `finding.confidence` | Direct |
| 3 | `taint_path_length` | int | `finding.taint_path` | `len(finding.taint_path)` |
| 4 | `has_sanitizer_on_path` | bool→int | `finding.taint_path` | `any(step.sanitizer_applied is not None for step in finding.taint_path)` |
| 5 | `sanitizer_cwe_match` | bool→int | sanitizer_validation.py | Check if sanitizer on path is effective for the finding's CWE via `SANITIZER_EFFECTIVENESS` lookup |
| 6 | `source_type` | categorical (one-hot) | `finding.source.source_type` | One-hot encode: `req.body`, `req.query`, `req.params`, `req.headers`, `req.cookies`, `env`, `file`, `db`, `other` |
| 7 | `sink_type` | categorical (one-hot) | `finding.sink.sink_type` | One-hot encode: `query`, `exec`, `render`, `write`, `redirect`, `eval`, `deserialize`, `other` |
| 8 | `framework` | categorical (one-hot) | `finding.metadata.get("framework")` | One-hot encode: `express`, `nestjs`, `fastify`, `nextjs`, `django`, `flask`, `gin`, `spring`, `unknown` |
| 9 | `file_extension` | categorical (one-hot) | `finding.source.location.file` | Extract extension, one-hot encode: `.ts`, `.tsx`, `.js`, `.jsx`, `.py`, `.go`, `.java`, `other` |
| 10 | `function_depth` | int | `finding.taint_path` | Count distinct `through_function` values in taint steps |
| 11 | `is_reachable` | bool→int | `finding.reachability` | `1 if finding.reachability == "reachable" else 0` |
| 12 | `is_dep_reachable` | bool→int | `finding.metadata` | `finding.metadata.get("dep_reachable", True)` |
| 13 | `field_sensitive_taint` | bool→int | `finding.metadata` | `finding.metadata.get("field_sensitive", False)` — from Phase 31 if available |
| 14 | `path_condition_count` | int | `finding.path_conditions` | `len(finding.path_conditions)` |
| 15 | `z3_result` | categorical (one-hot) | `finding.metadata` | `finding.metadata.get("z3_result", "SKIPPED")` — one-hot: `SAT`, `UNSAT`, `UNKNOWN`, `SKIPPED` |
| 16 | `code_complexity` | int | `finding.sink.location.snippet` | Approximate cyclomatic complexity: count `if`/`else`/`for`/`while`/`switch`/`case`/`catch`/`&&`/`\|\|`/`?` in sink snippet |
| 17 | `has_test_coverage` | bool→int | heuristic | Check if a test file exists for the module containing the sink (e.g., `foo.ts` → `foo.test.ts` or `__tests__/foo.ts`) |
| 18 | `commit_age_days` | int | `finding.metadata` | `finding.metadata.get("commit_age_days", -1)` — `-1` if unavailable, imputed to median during training |
| 19 | `severity_ordinal` | int | `finding.severity` | Map: `informational`→0, `low`→1, `medium`→2, `high`→3, `critical`→4 |
| 20 | `has_path_condition_unsat` | bool→int | `finding.path_conditions` + `finding.metadata` | `1 if finding.metadata.get("z3_result") == "UNSAT" else 0` |

Total raw feature count (before one-hot expansion): 20
Estimated feature vector width after one-hot encoding: ~55-65 dimensions.

### 2.2 Feature Extraction Implementation

```python
# src/piranesi/triage/ml_features.py
from __future__ import annotations
import re
from dataclasses import dataclass, field
from pathlib import Path
from piranesi.models import CandidateFinding
from piranesi.detect.sanitizer_validation import SANITIZER_EFFECTIVENESS

_TOP_CWES = [
    "CWE-79", "CWE-89", "CWE-78", "CWE-22", "CWE-918",
    "CWE-502", "CWE-94", "CWE-77", "CWE-611", "CWE-943",
    "CWE-200", "CWE-352", "CWE-287", "CWE-384", "CWE-601",
]
_SOURCE_TYPES = [
    "req.body", "req.query", "req.params", "req.headers",
    "req.cookies", "env", "file", "db",
]
_SINK_TYPES = ["query", "exec", "render", "write", "redirect", "eval", "deserialize"]
_FRAMEWORKS = ["express", "nestjs", "fastify", "nextjs", "django", "flask", "gin", "spring"]
_EXTENSIONS = [".ts", ".tsx", ".js", ".jsx", ".py", ".go", ".java"]
_Z3_RESULTS = ["SAT", "UNSAT", "UNKNOWN", "SKIPPED"]
_SEVERITY_MAP = {"informational": 0, "low": 1, "medium": 2, "high": 3, "critical": 4}
_COMPLEXITY_PATTERN = re.compile(r"\b(if|else|for|while|switch|case|catch)\b|&&|\|\||\?")

def extract_features(finding: CandidateFinding) -> dict[str, float]:
    """Extract ML feature dict from a CandidateFinding. Keys are feature names, values are floats."""
    feats: dict[str, float] = {}
    # cwe one-hot
    cwe = _parse_cwe(finding.vuln_class)
    for c in _TOP_CWES:
        feats[f"cwe_{c}"] = 1.0 if cwe == c else 0.0
    feats["cwe_other"] = 0.0 if cwe in _TOP_CWES else 1.0
    # numeric
    feats["confidence"] = finding.confidence
    feats["taint_path_length"] = float(len(finding.taint_path))
    feats["has_sanitizer_on_path"] = float(
        any(step.sanitizer_applied is not None for step in finding.taint_path)
    )
    feats["sanitizer_cwe_match"] = float(_check_sanitizer_cwe_match(finding))
    # source_type one-hot
    src = finding.source.source_type
    for st in _SOURCE_TYPES:
        feats[f"src_{st}"] = 1.0 if src == st else 0.0
    feats["src_other"] = 0.0 if src in _SOURCE_TYPES else 1.0
    # sink_type one-hot
    sk = finding.sink.sink_type
    for st in _SINK_TYPES:
        feats[f"sink_{st}"] = 1.0 if sk == st else 0.0
    feats["sink_other"] = 0.0 if sk in _SINK_TYPES else 1.0
    # framework one-hot
    fw = finding.metadata.get("framework", "unknown")
    for f in _FRAMEWORKS:
        feats[f"fw_{f}"] = 1.0 if fw == f else 0.0
    feats["fw_unknown"] = 0.0 if fw in _FRAMEWORKS else 1.0
    # file extension one-hot
    ext = _file_ext(finding.source.location.file)
    for e in _EXTENSIONS:
        feats[f"ext_{e}"] = 1.0 if ext == e else 0.0
    feats["ext_other"] = 0.0 if ext in _EXTENSIONS else 1.0
    # function depth
    through_fns = {s.through_function for s in finding.taint_path if s.through_function}
    feats["function_depth"] = float(len(through_fns))
    # reachability
    feats["is_reachable"] = 1.0 if finding.reachability == "reachable" else 0.0
    feats["is_dep_reachable"] = float(finding.metadata.get("dep_reachable", True))
    feats["field_sensitive_taint"] = float(finding.metadata.get("field_sensitive", False))
    # path conditions
    feats["path_condition_count"] = float(len(finding.path_conditions))
    # z3 result one-hot
    z3 = str(finding.metadata.get("z3_result", "SKIPPED"))
    for r in _Z3_RESULTS:
        feats[f"z3_{r}"] = 1.0 if z3 == r else 0.0
    # code complexity (heuristic from sink snippet)
    feats["code_complexity"] = float(
        len(_COMPLEXITY_PATTERN.findall(finding.sink.location.snippet))
    )
    # test coverage heuristic
    feats["has_test_coverage"] = float(_has_test_file(finding.sink.location.file))
    # commit age
    feats["commit_age_days"] = float(finding.metadata.get("commit_age_days", -1))
    # severity ordinal
    feats["severity_ordinal"] = float(_SEVERITY_MAP.get(finding.severity, 2))
    # z3 UNSAT shortcut
    feats["has_path_condition_unsat"] = 1.0 if z3 == "UNSAT" else 0.0
    return feats

def _parse_cwe(vuln_class: str) -> str:
    match = re.search(r"CWE-\d+", vuln_class)
    return match.group(0) if match else "CWE-0"

def _file_ext(path: str) -> str:
    return Path(path).suffix.lower()

def _check_sanitizer_cwe_match(finding: CandidateFinding) -> bool:
    cwe = _parse_cwe(finding.vuln_class)
    for step in finding.taint_path:
        if step.sanitizer_applied and step.sanitizer_applied in SANITIZER_EFFECTIVENESS:
            eff = SANITIZER_EFFECTIVENESS[step.sanitizer_applied]
            if cwe in eff and eff[cwe].value == "effective":
                return True
    return False

def _has_test_file(file_path: str) -> bool:
    p = Path(file_path)
    stem = p.stem
    parent = p.parent
    candidates = [
        parent / f"{stem}.test{p.suffix}",
        parent / f"{stem}.spec{p.suffix}",
        parent / "__tests__" / p.name,
        parent / "tests" / p.name,
    ]
    return any(c.exists() for c in candidates)

def feature_names() -> list[str]:
    """Return ordered list of feature names matching extract_features() key order."""
    names: list[str] = []
    for c in _TOP_CWES:
        names.append(f"cwe_{c}")
    names.append("cwe_other")
    names.extend(["confidence", "taint_path_length", "has_sanitizer_on_path", "sanitizer_cwe_match"])
    for st in _SOURCE_TYPES:
        names.append(f"src_{st}")
    names.append("src_other")
    for st in _SINK_TYPES:
        names.append(f"sink_{st}")
    names.append("sink_other")
    for f in _FRAMEWORKS:
        names.append(f"fw_{f}")
    names.append("fw_unknown")
    for e in _EXTENSIONS:
        names.append(f"ext_{e}")
    names.append("ext_other")
    names.extend([
        "function_depth", "is_reachable", "is_dep_reachable", "field_sensitive_taint",
        "path_condition_count",
    ])
    for r in _Z3_RESULTS:
        names.append(f"z3_{r}")
    names.extend([
        "code_complexity", "has_test_coverage", "commit_age_days",
        "severity_ordinal", "has_path_condition_unsat",
    ])
    return names
```

### 2.3 Feature Extraction from Ground Truth

Ground truth entries (`eval/ground_truth/gt-*.yaml`, schema in `eval/ground_truth/schema.py`) contain `taint_source`, `taint_sink`, `cwe_id`, `taint_path`, `complexity`, etc. To train the classifier, each GT entry must be matched to a corresponding `CandidateFinding` produced by running the scanner on its `source_project`.

If no scan output exists, a synthetic `CandidateFinding` is constructed from the GT entry fields with conservative defaults for missing features (e.g., `confidence=0.5`, `path_condition_count=0`, `z3_result="SKIPPED"`).

---

## 3. Training Pipeline

### 3.1 Script: `eval/train_classifier.py`

```python
#!/usr/bin/env python3
"""Train ML FP classifier from ground truth entries."""
from __future__ import annotations
import json
import logging
import pickle
import sys
from pathlib import Path
from datetime import datetime, UTC

import numpy as np
from sklearn.ensemble import RandomForestClassifier, GradientBoostingClassifier
from sklearn.linear_model import LogisticRegression
from sklearn.model_selection import StratifiedKFold, cross_val_predict
from sklearn.metrics import classification_report, recall_score, precision_score
from sklearn.calibration import CalibratedClassifierCV

from eval.ground_truth.schema import GroundTruthEntry
from eval.scoring import load_ground_truth_entries
from piranesi.triage.ml_features import extract_features, feature_names

logger = logging.getLogger(__name__)
_GT_DIR = Path(__file__).resolve().parent / "ground_truth"
_MODEL_DIR = Path(__file__).resolve().parent.parent / "models"

def load_training_data(
    gt_dir: Path | None = None,
) -> tuple[np.ndarray, np.ndarray, list[str]]:
    """Load GT entries, extract feature vectors, return (X, y, gt_ids).

    y: 1 = true_positive, 0 = false_positive
    """
    entries = load_ground_truth_entries(gt_dir or _GT_DIR)
    names = feature_names()
    X_rows: list[list[float]] = []
    y_labels: list[int] = []
    gt_ids: list[str] = []
    for entry in entries:
        finding = _gt_entry_to_candidate(entry)
        feats = extract_features(finding)
        row = [feats.get(name, 0.0) for name in names]
        X_rows.append(row)
        y_labels.append(1 if entry.label == "true_positive" else 0)
        gt_ids.append(entry.id)
    return np.array(X_rows), np.array(y_labels), gt_ids

def train(
    *,
    gt_dir: Path | None = None,
    output_dir: Path | None = None,
    model_version: int | None = None,
) -> Path:
    out = output_dir or _MODEL_DIR
    out.mkdir(parents=True, exist_ok=True)
    X, y, gt_ids = load_training_data(gt_dir)
    n_samples, n_features = X.shape
    logger.info("training data: %d samples, %d features, %d TP, %d FP",
                n_samples, n_features, int(y.sum()), int((1 - y).sum()))
    # stratified k-fold by CWE
    cwe_labels = _extract_cwe_strata(gt_ids, gt_dir or _GT_DIR)
    skf = StratifiedKFold(n_splits=min(5, min_class_count(y)), shuffle=True, random_state=42)
    # --- Random Forest (primary) ---
    rf = RandomForestClassifier(
        n_estimators=200, max_depth=12, min_samples_leaf=3,
        class_weight="balanced", random_state=42, n_jobs=-1,
    )
    rf_pred = cross_val_predict(rf, X, y, cv=skf, method="predict_proba")[:, 1]
    rf_recall = recall_score(y, (rf_pred >= 0.5).astype(int))
    rf_precision = precision_score(y, (rf_pred >= 0.5).astype(int))
    logger.info("RF CV: recall=%.3f precision=%.3f", rf_recall, rf_precision)
    # --- Logistic Regression (baseline) ---
    lr = LogisticRegression(
        class_weight="balanced", max_iter=1000, random_state=42,
    )
    lr_pred = cross_val_predict(lr, X, y, cv=skf, method="predict_proba")[:, 1]
    lr_recall = recall_score(y, (lr_pred >= 0.5).astype(int))
    lr_precision = precision_score(y, (lr_pred >= 0.5).astype(int))
    logger.info("LR CV: recall=%.3f precision=%.3f", lr_recall, lr_precision)
    # --- Gradient Boosted Trees (comparison) ---
    gbt = GradientBoostingClassifier(
        n_estimators=200, max_depth=5, min_samples_leaf=5,
        learning_rate=0.1, random_state=42,
    )
    gbt_pred = cross_val_predict(gbt, X, y, cv=skf, method="predict_proba")[:, 1]
    gbt_recall = recall_score(y, (gbt_pred >= 0.5).astype(int))
    gbt_precision = precision_score(y, (gbt_pred >= 0.5).astype(int))
    logger.info("GBT CV: recall=%.3f precision=%.3f", gbt_recall, gbt_precision)
    # --- Select best model by recall >= 0.95, then max precision ---
    candidates = [
        ("random_forest", rf, rf_recall, rf_precision),
        ("logistic_regression", lr, lr_recall, lr_precision),
        ("gradient_boosted_trees", gbt, gbt_recall, gbt_precision),
    ]
    viable = [(n, m, r, p) for n, m, r, p in candidates if r >= 0.95]
    if not viable:
        viable = sorted(candidates, key=lambda x: x[2], reverse=True)[:1] # best recall
        logger.warning("no model achieved >= 95%% recall; selecting best: %s (%.3f)", viable[0][0], viable[0][2])
    best_name, best_model, best_recall, best_precision = max(viable, key=lambda x: x[3])
    logger.info("selected: %s (recall=%.3f, precision=%.3f)", best_name, best_recall, best_precision)
    # --- Retrain on full dataset with Platt calibration ---
    calibrated = CalibratedClassifierCV(best_model, cv=skf, method="sigmoid")
    calibrated.fit(X, y)
    # --- Feature importance ---
    best_model.fit(X, y) # fit uncalibrated for feature importance
    importances = _get_feature_importances(best_model, feature_names())
    # --- Serialize ---
    version = model_version or _next_version(out)
    model_path = out / f"fp_classifier_v{version}.pkl"
    with open(model_path, "wb") as f:
        pickle.dump(calibrated, f, protocol=pickle.HIGHEST_PROTOCOL)
    # also write latest symlink
    latest = out / "fp_classifier.pkl"
    if latest.exists() or latest.is_symlink():
        latest.unlink()
    latest.symlink_to(model_path.name)
    # --- Metadata ---
    metadata = {
        "version": version,
        "training_date": datetime.now(UTC).isoformat(),
        "gt_count": n_samples,
        "tp_count": int(y.sum()),
        "fp_count": int((1 - y).sum()),
        "feature_names": feature_names(),
        "feature_count": n_features,
        "model_type": best_name,
        "cv_recall": round(best_recall, 4),
        "cv_precision": round(best_precision, 4),
        "feature_importances": importances,
        "all_models": {
            name: {"recall": round(r, 4), "precision": round(p, 4)}
            for name, _, r, p in candidates
        },
    }
    meta_path = out / f"fp_classifier_v{version}.json"
    meta_path.write_text(json.dumps(metadata, indent=2) + "\n", encoding="utf-8")
    logger.info("saved model to %s", model_path)
    logger.info("saved metadata to %s", meta_path)
    return model_path

def _get_feature_importances(model, names: list[str]) -> dict[str, float]:
    if hasattr(model, "feature_importances_"):
        imp = model.feature_importances_
    elif hasattr(model, "coef_"):
        imp = abs(model.coef_[0])
    else:
        return {}
    pairs = sorted(zip(names, imp), key=lambda x: x[1], reverse=True)
    return {name: round(float(val), 6) for name, val in pairs}

def _next_version(model_dir: Path) -> int:
    existing = list(model_dir.glob("fp_classifier_v*.pkl"))
    if not existing:
        return 1
    versions = []
    for p in existing:
        try:
            versions.append(int(p.stem.split("_v")[1]))
        except (IndexError, ValueError):
            pass
    return max(versions, default=0) + 1

def _extract_cwe_strata(gt_ids: list[str], gt_dir: Path) -> np.ndarray:
    """Return per-sample CWE label for stratification."""
    entries = {e.id: e for e in load_ground_truth_entries(gt_dir)}
    return np.array([entries[gid].cwe_id if gid in entries else "unknown" for gid in gt_ids])

def min_class_count(y: np.ndarray) -> int:
    _, counts = np.unique(y, return_counts=True)
    return int(counts.min())

def _gt_entry_to_candidate(entry: GroundTruthEntry) -> CandidateFinding:
    """Build a synthetic CandidateFinding from a GT entry for feature extraction."""
    from piranesi.models import (
        CandidateFinding, PathCondition, SourceLocation,
        TaintSink, TaintSource, TaintStep,
    )
    loc = SourceLocation(
        file=entry.affected_files[0] if entry.affected_files else "unknown.ts",
        line=entry.line_numbers[0] if entry.line_numbers else 1,
        column=0, snippet="",
    )
    source = TaintSource(
        location=loc, source_type=entry.taint_source,
        data_categories=[], parameter_name=None,
    )
    sink = TaintSink(location=loc, sink_type=entry.taint_sink, api_name=entry.taint_sink)
    steps = [
        TaintStep(location=loc, operation=step_desc, taint_state="tainted")
        for step_desc in entry.taint_path
    ]
    return CandidateFinding(
        id=entry.id, vuln_class=entry.cwe_id,
        source=source, sink=sink, taint_path=steps,
        path_conditions=[], confidence=0.5,
        severity="medium",
        reachability="reachable",
    )

if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO, format="%(levelname)s %(name)s: %(message)s")
    train()
```

### 3.2 Cross-Validation Strategy

- **Stratification**: `StratifiedKFold` with `k=min(5, min_class_count)`. Stratify by label (TP/FP) to maintain class balance in each fold.
- **Per-CWE stratification** (secondary): When GT grows large enough (>500 entries), switch to per-CWE stratified splits to ensure each CWE is represented in every fold.
- **Metric priority**: Optimize for **recall >= 95%** first (never miss real vulnerabilities), then maximize precision. This means the classifier is conservative — it only filters findings it is highly confident are FP.

### 3.3 Model Selection Criteria

| Model | Strengths | When to use |
|---|---|---|
| Random Forest | Robust with small datasets, handles mixed feature types, built-in feature importance | Primary choice for < 1000 GT entries |
| Logistic Regression | Interpretable, fast, calibrated probabilities | Baseline comparison; use if RF overfits |
| Gradient Boosted Trees | Best accuracy on tabular data | Use when GT exceeds 1000 entries |

### 3.4 Hyperparameter Tuning

For the initial release with ~186 GT entries, hardcoded hyperparameters are used (shown above). When GT grows past 500 entries, add `RandomizedSearchCV`:

```python
from sklearn.model_selection import RandomizedSearchCV
param_dist = {
    "n_estimators": [100, 200, 500],
    "max_depth": [5, 8, 12, None],
    "min_samples_leaf": [1, 3, 5, 10],
    "min_samples_split": [2, 5, 10],
}
search = RandomizedSearchCV(
    RandomForestClassifier(class_weight="balanced", random_state=42),
    param_dist, n_iter=20, cv=skf,
    scoring="recall", random_state=42, n_jobs=-1,
)
```

---

## 4. Active Learning Loop

### 4.1 Problem

186 GT entries may be insufficient for robust generalization, especially for rare CWEs. The classifier needs more labeled data, but manual labeling of scan output is expensive.

### 4.2 Solution: Uncertainty Sampling

The classifier identifies findings where it is most uncertain (prediction probability near 0.5) and presents them to the user for labeling. These are the most informative samples for improving the model.

### 4.3 Script: `eval/active_learn.py`

```python
#!/usr/bin/env python3
"""Active learning loop for FP classifier GT expansion."""
from __future__ import annotations
import json
import logging
import sys
from pathlib import Path

from piranesi.triage.ml_classifier import load_model, predict
from piranesi.triage.ml_features import extract_features

logger = logging.getLogger(__name__)
_GT_DIR = Path(__file__).resolve().parent / "ground_truth"

def active_learn(
    findings_json: Path,
    *,
    model_path: Path | None = None,
    uncertainty_low: float = 0.4,
    uncertainty_high: float = 0.6,
    max_samples: int = 20,
    convergence_threshold: float = 0.05,
) -> None:
    """Run one active learning iteration.

    1. Load unlabeled findings from a scan output JSON
    2. Run classifier, select uncertain findings (0.4 <= P(TP) <= 0.6)
    3. Present to user for labeling via CLI prompt
    4. Write new GT entries
    5. Print retrain instructions
    """
    from piranesi.models import CandidateFinding
    raw = json.loads(findings_json.read_text(encoding="utf-8"))
    findings = [CandidateFinding.model_validate(f) for f in raw]
    classifier = load_model(model_path)
    scored = predict(findings, classifier=classifier)
    # select uncertain findings
    uncertain = [
        (f, score) for f, score in scored
        if uncertainty_low <= score <= uncertainty_high
    ]
    uncertain.sort(key=lambda x: abs(x[1] - 0.5)) # most uncertain first
    uncertain = uncertain[:max_samples]
    if not uncertain:
        print("No uncertain findings to label. Model may be well-calibrated.")
        return
    print(f"\n{len(uncertain)} uncertain findings to label:\n")
    labeled = 0
    for i, (finding, score) in enumerate(uncertain, 1):
        print(f"[{i}/{len(uncertain)}] {finding.vuln_class}")
        print(f"  Source: {finding.source.source_type} @ {finding.source.location.file}:{finding.source.location.line}")
        print(f"  Sink:   {finding.sink.api_name} @ {finding.sink.location.file}:{finding.sink.location.line}")
        print(f"  ML score: {score:.3f} (uncertain)")
        print(f"  Snippet: {finding.sink.location.snippet[:120]}")
        while True:
            answer = input("  Is this a real vulnerability? [y/n/s(kip)/q(uit)] ").strip().lower()
            if answer in ("y", "n", "s", "q"):
                break
        if answer == "q":
            break
        if answer == "s":
            continue
        label = "true_positive" if answer == "y" else "false_positive"
        _write_gt_entry(finding, label)
        labeled += 1
    print(f"\nLabeled {labeled} findings. Run `python eval/train_classifier.py` to retrain.")

def _write_gt_entry(finding: CandidateFinding, label: str) -> None:
    """Append a new GT entry YAML file."""
    import yaml
    gt_dir = _GT_DIR
    existing = list(gt_dir.glob("gt-*.yaml")) + list(gt_dir.glob("gt-fp-*.yaml"))
    next_id = len(existing) + 1
    prefix = "gt" if label == "true_positive" else "gt-fp"
    gt_id = f"{prefix}-{next_id:03d}"
    entry = {
        "id": gt_id,
        "source_project": "active-learning",
        "commit_hash": "active-learning-no-commit",
        "cwe_id": finding.vuln_class,
        "cwe_name": finding.vuln_class,
        "label": label,
        "affected_files": [finding.source.location.file],
        "line_numbers": [finding.source.location.line, finding.sink.location.line],
        "taint_source": finding.source.source_type,
        "taint_sink": finding.sink.api_name,
        "taint_path": [step.operation for step in finding.taint_path],
        "complexity": "inter" if len(finding.taint_path) > 2 else "simple",
        "exploitable": label == "true_positive",
        "reference_exploit": None,
        "reference_fix_commit": None,
        "notes": f"Added via active learning (ML confidence: {finding.confidence:.3f})",
    }
    path = gt_dir / f"{gt_id}.yaml"
    import yaml
    path.write_text(yaml.dump(entry, default_flow_style=False, sort_keys=False), encoding="utf-8")
    logger.info("wrote GT entry: %s", path)

if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO)
    if len(sys.argv) < 2:
        print("Usage: python eval/active_learn.py <findings.json> [--model models/fp_classifier.pkl]")
        sys.exit(1)
    active_learn(Path(sys.argv[1]))
```

### 4.4 Convergence Criterion

Retrain after each labeling batch. Track accuracy on a held-out validation set across iterations. Stop when accuracy change between iterations is < 5%:

```
Iteration 1: accuracy 0.78  (Δ = N/A)
Iteration 2: accuracy 0.83  (Δ = 0.05) → continue
Iteration 3: accuracy 0.86  (Δ = 0.03) → converged, stop
```

---

## 5. Integration with Pipeline

### 5.1 Module: `src/piranesi/triage/ml_classifier.py`

```python
from __future__ import annotations
import logging
import pickle
from pathlib import Path
from typing import Any

from piranesi.models import CandidateFinding
from piranesi.triage.ml_features import extract_features, feature_names

_logger = logging.getLogger(__name__)
_DEFAULT_MODEL_PATH = Path(__file__).resolve().parent.parent.parent.parent / "models" / "fp_classifier.pkl"

class MLClassifier:
    """Lightweight ML pre-filter for FP reduction."""
    __slots__ = ("_model", "_feature_names", "_path")

    def __init__(self, model: Any, path: Path | None = None) -> None:
        self._model = model
        self._feature_names = feature_names()
        self._path = path

    def predict_proba(self, findings: list[CandidateFinding]) -> list[float]:
        """Return P(true_positive) for each finding."""
        import numpy as np
        if not findings:
            return []
        rows = []
        for f in findings:
            feats = extract_features(f)
            rows.append([feats.get(name, 0.0) for name in self._feature_names])
        X = np.array(rows)
        probas = self._model.predict_proba(X)[:, 1] # P(TP)
        return probas.tolist()

def load_model(path: Path | None = None) -> MLClassifier | None:
    """Load trained model from pickle. Returns None if file missing."""
    p = path or _DEFAULT_MODEL_PATH
    if not p.exists():
        _logger.debug("no ML model at %s, skipping pre-filter", p)
        return None
    with open(p, "rb") as f:
        model = pickle.load(f) # noqa: S301 — trusted local file
    _logger.info("loaded ML FP classifier from %s", p)
    return MLClassifier(model, p)

def predict(
    findings: list[CandidateFinding],
    *,
    classifier: MLClassifier | None = None,
    threshold: float = 0.5,
) -> list[tuple[CandidateFinding, float]]:
    """Score findings and return (finding, P(TP)) pairs."""
    if classifier is None:
        classifier = load_model()
    if classifier is None:
        return [(f, 1.0) for f in findings] # no model → pass all through
    scores = classifier.predict_proba(findings)
    return list(zip(findings, scores))

def filter_findings(
    findings: list[CandidateFinding],
    *,
    classifier: MLClassifier | None = None,
    threshold: float = 0.5,
) -> tuple[list[CandidateFinding], list[CandidateFinding]]:
    """Split findings into (escalate_to_llm, likely_fp).

    Returns:
        escalate: findings with P(TP) >= threshold → send to LLM triage
        filtered: findings with P(TP) < threshold → mark as likely FP
    """
    scored = predict(findings, classifier=classifier, threshold=threshold)
    escalate = [f for f, score in scored if score >= threshold]
    filtered = [f for f, score in scored if score < threshold]
    _logger.info(
        "ML pre-filter: %d/%d escalated, %d/%d filtered as likely FP (threshold=%.2f)",
        len(escalate), len(findings), len(filtered), len(findings), threshold,
    )
    return escalate, filtered
```

### 5.2 Pipeline Integration Point

In `src/piranesi/pipeline.py`, the ML pre-filter runs **after** detection and **before** LLM triage. The existing `TriageArtifact` stage calls `CalibratedEnsembleVoter.triage_finding()` per candidate. The ML classifier intercepts this:

```python
# in the triage stage of run_pipeline()
from piranesi.triage.ml_classifier import filter_findings, load_model

ml_classifier = load_model() if config.triage.ml_prefilter else None
escalate, likely_fp = filter_findings(candidates, classifier=ml_classifier, threshold=config.triage.ml_threshold)
# triage only escalated findings with LLM
triaged = [voter.triage_finding(f) for f in escalate]
# mark filtered findings as FP without LLM cost
for f in likely_fp:
    triaged.append(TriagedFinding(
        finding=f, triage_verdict="false_positive",
        skeptic_analysis="ML pre-filter: classified as likely FP",
        ensemble_score=0.0, escalated=False,
    ))
```

### 5.3 Config Integration

Add to `PiranesiConfig` in `src/piranesi/config.py`:

```python
class TriageConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    ml_prefilter: bool = True # enable ML pre-filter
    ml_threshold: float = 0.5 # P(TP) threshold for LLM escalation
    ml_model_path: str | None = None # custom model path; None = default
    ml_conservative: bool = False # if True, use threshold 0.3

# add to PiranesiConfig:
triage: TriageConfig = Field(default_factory=TriageConfig)
```

TOML usage:

```toml
[triage]
ml_prefilter = true
ml_threshold = 0.5
# ml_conservative = true  # use 0.3 threshold, fewer FPs filtered, safer
```

### 5.4 Threshold Modes

| Mode | Threshold | FP filtered | Risk |
|---|---|---|---|
| Standard | 0.5 | ~60-70% | Low — good balance |
| Conservative | 0.3 | ~30-40% | Very low — only filters highly confident FPs |
| Aggressive | 0.7 | ~80-90% | Medium — may miss some edge-case TPs |

When `ml_conservative = true`, the threshold is overridden to 0.3 regardless of `ml_threshold`.

### 5.5 Fallback Behavior

If no trained model exists at the expected path (`models/fp_classifier.pkl`):
- `load_model()` returns `None`
- `filter_findings()` returns all findings as "escalate" (no filtering)
- LLM triage proceeds as before
- No error, no warning (debug log only)

---

## 6. Calibration

### 6.1 Platt Scaling on Classifier Output

The scikit-learn `CalibratedClassifierCV` with `method="sigmoid"` applies Platt scaling during training. This produces calibrated probabilities where `predict_proba` output matches empirical frequency:

- If the model says P(TP)=0.8, then ~80% of findings at that score are actually TP
- This is critical for threshold-based filtering — uncalibrated Random Forest probabilities are often poorly calibrated at the extremes

### 6.2 Integration with `eval/calibrate.py`

The existing `eval/calibrate.py` calibrates LLM triage confidence via Platt scaling. The ML classifier calibration is independent but complementary:

1. ML classifier: calibrated via `CalibratedClassifierCV` at training time
2. LLM triage: calibrated via `eval/calibrate.py` at calibration time
3. Combined: ML pre-filter probability is logged but does **not** feed into the LLM ensemble score. The two systems are independent pre-filter and triage stages.

### 6.3 Per-CWE Calibration

Different CWEs have different base FP rates. CWE-79 (XSS) has a higher FP rate than CWE-89 (SQLi) in most SAST tools.

The trained model implicitly captures per-CWE behavior via the one-hot CWE features. For explicit per-CWE threshold adjustment, add to `eval/train_classifier.py`:

```python
def compute_per_cwe_thresholds(
    X: np.ndarray, y: np.ndarray, gt_ids: list[str], model,
) -> dict[str, float]:
    """Find per-CWE threshold that achieves recall >= 0.95."""
    # group predictions by CWE
    probas = model.predict_proba(X)[:, 1]
    by_cwe: dict[str, list[tuple[float, int]]] = defaultdict(list)
    entries = {e.id: e for e in load_ground_truth_entries()}
    for gid, prob, label in zip(gt_ids, probas, y):
        cwe = entries[gid].cwe_id if gid in entries else "unknown"
        by_cwe[cwe].append((prob, int(label)))
    thresholds: dict[str, float] = {}
    for cwe, pairs in by_cwe.items():
        if len(pairs) < 5:
            continue
        # find lowest threshold where recall >= 0.95
        tp_probs = sorted([p for p, l in pairs if l == 1])
        if not tp_probs:
            thresholds[cwe] = 0.5
            continue
        idx = max(0, int(len(tp_probs) * 0.05) - 1) # 5th percentile of TP scores
        thresholds[cwe] = tp_probs[idx]
    return thresholds
```

---

## 7. Model Versioning

### 7.1 File Layout

```
models/
  fp_classifier.pkl          → symlink to latest version
  fp_classifier_v1.pkl       → trained model (pickle)
  fp_classifier_v1.json      → metadata
  fp_classifier_v2.pkl
  fp_classifier_v2.json
```

### 7.2 Metadata Schema

```json
{
  "version": 1,
  "training_date": "2026-04-11T14:30:00+00:00",
  "gt_count": 186,
  "tp_count": 120,
  "fp_count": 66,
  "feature_names": ["cwe_CWE-79", "cwe_CWE-89", "..."],
  "feature_count": 62,
  "model_type": "random_forest",
  "cv_recall": 0.9583,
  "cv_precision": 0.8214,
  "feature_importances": {
    "has_sanitizer_on_path": 0.142,
    "sanitizer_cwe_match": 0.118,
    "confidence": 0.095,
    "taint_path_length": 0.083,
    "is_reachable": 0.071,
    "...": "..."
  },
  "all_models": {
    "random_forest": {"recall": 0.9583, "precision": 0.8214},
    "logistic_regression": {"recall": 0.9417, "precision": 0.7856},
    "gradient_boosted_trees": {"recall": 0.9500, "precision": 0.8071}
  }
}
```

### 7.3 CLI Commands

Add to `src/piranesi/cli.py`:

```python
model_app = typer.Typer(add_completion=False, help="Manage ML FP classifier.", no_args_is_help=True)
app.add_typer(model_app, name="model")

@model_app.command("info")
def model_info(
    model_path: Annotated[Path | None, typer.Option("--model")] = None,
) -> None:
    """Show current ML classifier version and training stats."""
    from piranesi.triage.ml_classifier import _DEFAULT_MODEL_PATH
    p = model_path or _DEFAULT_MODEL_PATH
    meta_candidates = [p.with_suffix(".json"), p.parent / (p.stem + ".json")]
    for meta_path in meta_candidates:
        if meta_path.exists():
            meta = json.loads(meta_path.read_text(encoding="utf-8"))
            console.print(f"Model:     {meta.get('model_type', 'unknown')}")
            console.print(f"Version:   v{meta.get('version', '?')}")
            console.print(f"Trained:   {meta.get('training_date', 'unknown')}")
            console.print(f"GT count:  {meta.get('gt_count', '?')} ({meta.get('tp_count', '?')} TP, {meta.get('fp_count', '?')} FP)")
            console.print(f"CV Recall: {meta.get('cv_recall', '?')}")
            console.print(f"CV Prec:   {meta.get('cv_precision', '?')}")
            top5 = list(meta.get("feature_importances", {}).items())[:5]
            if top5:
                console.print("Top features:")
                for name, imp in top5:
                    console.print(f"  {name}: {imp:.4f}")
            return
    console.print(f"No model metadata found at {p}", style="yellow")

@model_app.command("train")
def model_train(
    gt_dir: Annotated[Path | None, typer.Option("--gt-dir")] = None,
    output_dir: Annotated[Path | None, typer.Option("--output-dir")] = None,
) -> None:
    """Retrain ML classifier from current ground truth."""
    from eval.train_classifier import train
    path = train(gt_dir=gt_dir, output_dir=output_dir)
    console.print(f"Model saved to {path}")
```

---

## 8. Testing Strategy

### 8.1 Unit Tests: Feature Extraction

`tests/test_triage/test_ml_features.py`:

```python
from piranesi.models import (
    CandidateFinding, SourceLocation, TaintSource, TaintSink, TaintStep,
)
from piranesi.triage.ml_features import extract_features, feature_names

def _make_finding(**overrides) -> CandidateFinding:
    loc = SourceLocation(file="app.ts", line=10, column=0, snippet="db.query(sql)")
    defaults = dict(
        id="test-1", vuln_class="CWE-89",
        source=TaintSource(location=loc, source_type="req.body", data_categories=[]),
        sink=TaintSink(location=loc, sink_type="query", api_name="db.query"),
        taint_path=[], path_conditions=[], confidence=0.8,
        severity="high", reachability="reachable", metadata={},
    )
    defaults.update(overrides)
    return CandidateFinding(**defaults)

def test_feature_names_match_extract():
    f = _make_finding()
    feats = extract_features(f)
    names = feature_names()
    assert set(feats.keys()) == set(names)

def test_cwe_one_hot():
    f = _make_finding(vuln_class="CWE-89")
    feats = extract_features(f)
    assert feats["cwe_CWE-89"] == 1.0
    assert feats["cwe_CWE-79"] == 0.0
    assert feats["cwe_other"] == 0.0

def test_sanitizer_on_path():
    loc = SourceLocation(file="app.ts", line=1, column=0, snippet="x")
    step = TaintStep(
        location=loc, operation="escape", taint_state="sanitized",
        sanitizer_applied="html_escape",
    )
    f = _make_finding(taint_path=[step])
    feats = extract_features(f)
    assert feats["has_sanitizer_on_path"] == 1.0

def test_confidence_passthrough():
    f = _make_finding(confidence=0.42)
    feats = extract_features(f)
    assert feats["confidence"] == 0.42

def test_unknown_cwe_goes_to_other():
    f = _make_finding(vuln_class="CWE-9999")
    feats = extract_features(f)
    assert feats["cwe_other"] == 1.0
    for c in ["CWE-79", "CWE-89", "CWE-78"]:
        assert feats[f"cwe_{c}"] == 0.0
```

### 8.2 Model Tests: Train/Evaluate

`tests/test_triage/test_ml_classifier.py`:

```python
import numpy as np
import pytest
from unittest.mock import patch

def test_train_on_subset_achieves_recall():
    """Train on ground truth, verify recall >= 0.90 (relaxed for small dataset)."""
    from eval.train_classifier import load_training_data
    X, y, ids = load_training_data()
    if len(y) < 20:
        pytest.skip("not enough GT entries for model test")
    from sklearn.ensemble import RandomForestClassifier
    from sklearn.model_selection import cross_val_predict
    from sklearn.metrics import recall_score
    rf = RandomForestClassifier(
        n_estimators=100, class_weight="balanced", random_state=42,
    )
    pred = cross_val_predict(rf, X, y, cv=3, method="predict_proba")[:, 1]
    recall = recall_score(y, (pred >= 0.5).astype(int))
    assert recall >= 0.90, f"recall {recall:.3f} < 0.90"

def test_filter_findings_fallback_when_no_model():
    """When no model exists, all findings should pass through."""
    from piranesi.triage.ml_classifier import filter_findings
    from tests.test_triage.test_ml_features import _make_finding
    findings = [_make_finding() for _ in range(5)]
    escalate, filtered = filter_findings(findings, classifier=None)
    assert len(escalate) == 5
    assert len(filtered) == 0
```

### 8.3 Integration Test: Pipeline with ML Pre-Filter

`tests/test_triage/test_ml_pipeline_integration.py`:

```python
def test_ml_prefilter_reduces_llm_calls(monkeypatch):
    """Mock ML classifier, verify LLM triage only called for escalated findings."""
    from piranesi.triage.ml_classifier import MLClassifier, filter_findings
    from tests.test_triage.test_ml_features import _make_finding
    # mock classifier that marks 3/5 as FP
    class FakeModel:
        def predict_proba(self, X):
            n = X.shape[0]
            # first 3: FP (low score), last 2: TP (high score)
            probs = np.array([[0.8, 0.2]] * 3 + [[0.2, 0.8]] * 2)
            return probs[:n]
    classifier = MLClassifier(FakeModel())
    findings = [_make_finding(id=f"f-{i}") for i in range(5)]
    escalate, filtered = filter_findings(findings, classifier=classifier, threshold=0.5)
    assert len(escalate) == 2
    assert len(filtered) == 3
```

### 8.4 Regression Test

After GT grows, retrain and compare metrics against the previous model version metadata JSON:

```python
def test_no_recall_regression():
    """After retraining, recall should not decrease by more than 2%."""
    import json
    from pathlib import Path
    model_dir = Path("models")
    versions = sorted(model_dir.glob("fp_classifier_v*.json"))
    if len(versions) < 2:
        pytest.skip("need 2+ model versions for regression test")
    prev = json.loads(versions[-2].read_text())
    curr = json.loads(versions[-1].read_text())
    assert curr["cv_recall"] >= prev["cv_recall"] - 0.02, (
        f"recall regressed: {prev['cv_recall']:.4f} → {curr['cv_recall']:.4f}"
    )
```

---

## 9. Privacy and Offline

### 9.1 Data Locality

- Training data: local `eval/ground_truth/` YAML files only
- Trained model: local pickle file in `models/`
- Inference: runs entirely in-process via scikit-learn, no network calls
- No telemetry, no model uploads, no external API calls

### 9.2 Dependency Management

scikit-learn is added as an **optional dev dependency**, not a core requirement. Users who never train a model do not need it installed. The pre-trained model can be loaded with just pickle + numpy (transitive dep of scikit-learn).

Add to `pyproject.toml`:

```toml
[project.optional-dependencies]
ml = ["scikit-learn>=1.4.0", "numpy>=1.26.0"]
# ... existing groups ...
dev = [
    "pytest>=8.0.0",
    "scikit-learn>=1.4.0",  # for ML classifier training
    "numpy>=1.26.0",
    # ... existing dev deps ...
]
```

At runtime, `ml_classifier.py` imports numpy only when `load_model()` successfully loads a pickle. If scikit-learn is not installed and no pre-trained model exists, the feature degrades gracefully (all findings pass to LLM triage).

### 9.3 Pickle Safety

The model pickle is loaded from a trusted local path only. The `load_model()` function:
- Only reads from `models/fp_classifier.pkl` or an explicit `--model` path
- Never loads pickles from network, user input, or scan targets
- The `# noqa: S301` annotation documents the deliberate use of `pickle.load`

---

## 10. Acceptance Criteria

- [ ] `src/piranesi/triage/ml_features.py` extracts 20+ features from `CandidateFinding`
- [ ] `src/piranesi/triage/ml_classifier.py` loads model, predicts, filters findings
- [ ] `eval/train_classifier.py` trains RF/LR/GBT, selects best by recall, serializes
- [ ] `eval/active_learn.py` presents uncertain findings for human labeling
- [ ] Cross-validation recall >= 95% on current GT (or best achievable with < 200 samples)
- [ ] Pipeline integration: ML pre-filter runs before LLM triage when enabled
- [ ] Config: `[triage] ml_prefilter`, `ml_threshold`, `ml_conservative` supported
- [ ] CLI: `piranesi model info` and `piranesi model train` implemented
- [ ] Fallback: graceful degradation when no model file exists
- [ ] Unit tests for feature extraction, model loading, filtering
- [ ] Model metadata JSON written alongside pickle
- [ ] scikit-learn in optional deps only, not core requirements
- [ ] No external network calls during training or inference

---

## 11. Estimated Effort Breakdown

| Task | Hours |
|---|---|
| Feature extraction module (`ml_features.py`) | 4-5h |
| Classifier module (`ml_classifier.py`) | 3-4h |
| Training pipeline (`train_classifier.py`) | 5-6h |
| Active learning script (`active_learn.py`) | 3-4h |
| Pipeline integration + config | 4-5h |
| CLI commands (`model info`, `model train`) | 2-3h |
| Calibration integration | 2-3h |
| Tests (unit + model + integration + regression) | 5-6h |
| Documentation + review | 2-3h |
| **Total** | **30-39h** |
