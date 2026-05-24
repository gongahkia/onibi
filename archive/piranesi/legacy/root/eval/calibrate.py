"""Ensemble calibration pipeline.

Runs triage against ground truth entries, fits Platt scaling per model,
computes per-CWE correction factors, and searches for optimal thresholds.

Requires an LLM API key (OPENAI_API_KEY or ANTHROPIC_API_KEY).
Output: eval/calibration/{model}.json
"""
from __future__ import annotations

import json
import logging
import math
import sys
from collections import defaultdict
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from eval.ground_truth.schema import GroundTruthEntry
from eval.scoring import load_ground_truth_entries

logger = logging.getLogger(__name__)

_CALIBRATION_DIR = Path(__file__).resolve().parent / "calibration"
_GROUND_TRUTH_DIR = Path(__file__).resolve().parent / "ground_truth"
_MIN_SAMPLES_PER_CWE = 10


# --- data structures ---

@dataclass(frozen=True, slots=True)
class TrialResult:
    gt_id: str
    cwe_id: str
    label: str # "true_positive" | "false_positive"
    model: str
    reported_confidence: float
    verdict: str # "true_positive" | "false_positive"
    correct: bool


@dataclass(frozen=True, slots=True)
class PlattParams:
    a: float
    b: float

    def transform(self, raw_confidence: float) -> float:
        """Apply Platt scaling: P(correct) = sigmoid(a * x + b)."""
        z = self.a * raw_confidence + self.b
        return 1.0 / (1.0 + math.exp(-z))


@dataclass(frozen=True, slots=True)
class CalibrationData:
    model: str
    global_params: PlattParams
    cwe_params: dict[str, PlattParams] # per-CWE override when n >= 10
    n_samples: int
    n_correct: int
    raw_accuracy: float
    optimal_tp_threshold: float
    optimal_fp_threshold: float
    gt_version: str

    def calibrate(self, raw_confidence: float, cwe_id: str | None = None) -> float:
        if cwe_id and cwe_id in self.cwe_params:
            return self.cwe_params[cwe_id].transform(raw_confidence)
        return self.global_params.transform(raw_confidence)

    def to_dict(self) -> dict[str, Any]:
        return {
            "model": self.model,
            "global_params": {"a": self.global_params.a, "b": self.global_params.b},
            "cwe_params": {
                cwe: {"a": p.a, "b": p.b} for cwe, p in self.cwe_params.items()
            },
            "n_samples": self.n_samples,
            "n_correct": self.n_correct,
            "raw_accuracy": self.raw_accuracy,
            "optimal_tp_threshold": self.optimal_tp_threshold,
            "optimal_fp_threshold": self.optimal_fp_threshold,
            "gt_version": self.gt_version,
        }

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> CalibrationData:
        gp = data["global_params"]
        return cls(
            model=data["model"],
            global_params=PlattParams(a=gp["a"], b=gp["b"]),
            cwe_params={
                cwe: PlattParams(a=p["a"], b=p["b"])
                for cwe, p in data.get("cwe_params", {}).items()
            },
            n_samples=data["n_samples"],
            n_correct=data["n_correct"],
            raw_accuracy=data["raw_accuracy"],
            optimal_tp_threshold=data.get("optimal_tp_threshold", 0.7),
            optimal_fp_threshold=data.get("optimal_fp_threshold", 0.3),
            gt_version=data.get("gt_version", "unknown"),
        )


# --- Platt scaling fitter ---

def fit_platt_scaling(
    confidences: list[float],
    labels: list[bool],
    *,
    lr: float = 0.01,
    max_iter: int = 5000,
    tol: float = 1e-7,
) -> PlattParams:
    """Fit logistic regression via gradient descent: P(correct) = sigmoid(a*x + b).

    Uses simple GD — no sklearn dependency needed.
    """
    if not confidences or not labels:
        return PlattParams(a=1.0, b=0.0)
    n = len(confidences)
    a, b = 0.0, 0.0
    prev_loss = float("inf")
    for _ in range(max_iter):
        loss = 0.0
        grad_a, grad_b = 0.0, 0.0
        for x, y in zip(confidences, labels):
            z = a * x + b
            p = 1.0 / (1.0 + math.exp(-z)) if z > -500 else 0.0
            p = max(min(p, 1.0 - 1e-15), 1e-15)
            y_f = 1.0 if y else 0.0
            loss += -(y_f * math.log(p) + (1.0 - y_f) * math.log(1.0 - p))
            err = p - y_f
            grad_a += err * x
            grad_b += err
        loss /= n
        a -= lr * grad_a / n
        b -= lr * grad_b / n
        if abs(prev_loss - loss) < tol:
            break
        prev_loss = loss
    return PlattParams(a=a, b=b)


# --- threshold search ---

def find_optimal_thresholds(
    calibrated_scores: list[float],
    labels: list[bool],
    *,
    steps: int = 100,
) -> tuple[float, float]:
    """Find TP/FP thresholds that maximize F1 on ground truth.

    Returns (tp_threshold, fp_threshold) where:
    - scores >= tp_threshold → predict TP
    - scores <= fp_threshold → predict FP
    - in between → uncertain
    """
    if not calibrated_scores or not labels:
        return 0.7, 0.3
    best_f1 = -1.0
    best_tp_thresh = 0.7
    best_fp_thresh = 0.3
    for tp_i in range(50, steps + 1): # tp threshold from 0.5 to 1.0
        tp_thresh = tp_i / steps
        for fp_i in range(0, 51): # fp threshold from 0.0 to 0.5
            fp_thresh = fp_i / steps
            if fp_thresh >= tp_thresh:
                continue
            tp, fp, fn = 0, 0, 0
            for score, label in zip(calibrated_scores, labels):
                if score >= tp_thresh:
                    if label:
                        tp += 1
                    else:
                        fp += 1
                elif score <= fp_thresh:
                    if label:
                        fn += 1
                # uncertain → neither TP nor FP
            precision = tp / (tp + fp) if (tp + fp) > 0 else 0.0
            recall = tp / (tp + fn) if (tp + fn) > 0 else 0.0
            f1 = 2 * precision * recall / (precision + recall) if (precision + recall) > 0 else 0.0
            if f1 > best_f1:
                best_f1 = f1
                best_tp_thresh = tp_thresh
                best_fp_thresh = fp_thresh
    return best_tp_thresh, best_fp_thresh


# --- calibration pipeline ---

def build_calibration(
    model: str,
    trials: list[TrialResult],
    *,
    gt_version: str = "unknown",
) -> CalibrationData:
    """Build calibration data from trial results for a single model."""
    model_trials = [t for t in trials if t.model == model]
    if not model_trials:
        return CalibrationData(
            model=model,
            global_params=PlattParams(a=1.0, b=0.0),
            cwe_params={},
            n_samples=0, n_correct=0, raw_accuracy=0.0,
            optimal_tp_threshold=0.7, optimal_fp_threshold=0.3,
            gt_version=gt_version,
        )
    confidences = [t.reported_confidence for t in model_trials]
    correct = [t.correct for t in model_trials]
    global_params = fit_platt_scaling(confidences, correct)
    # per-CWE calibration
    by_cwe: dict[str, list[TrialResult]] = defaultdict(list)
    for t in model_trials:
        by_cwe[t.cwe_id].append(t)
    cwe_params: dict[str, PlattParams] = {}
    for cwe_id, cwe_trials in by_cwe.items():
        if len(cwe_trials) >= _MIN_SAMPLES_PER_CWE:
            cwe_confs = [t.reported_confidence for t in cwe_trials]
            cwe_correct = [t.correct for t in cwe_trials]
            cwe_params[cwe_id] = fit_platt_scaling(cwe_confs, cwe_correct)
    # threshold search on calibrated scores
    calibrated = [global_params.transform(c) for c in confidences]
    tp_thresh, fp_thresh = find_optimal_thresholds(calibrated, correct)
    n_correct = sum(1 for c in correct if c)
    return CalibrationData(
        model=model,
        global_params=global_params,
        cwe_params=cwe_params,
        n_samples=len(model_trials),
        n_correct=n_correct,
        raw_accuracy=n_correct / len(model_trials),
        optimal_tp_threshold=tp_thresh,
        optimal_fp_threshold=fp_thresh,
        gt_version=gt_version,
    )


def save_calibration(calibration: CalibrationData, output_dir: Path | None = None) -> Path:
    """Write calibration JSON to eval/calibration/{model}.json."""
    out = output_dir or _CALIBRATION_DIR
    out.mkdir(parents=True, exist_ok=True)
    safe_name = calibration.model.replace("/", "_").replace(":", "_")
    path = out / f"{safe_name}.json"
    path.write_text(json.dumps(calibration.to_dict(), indent=2) + "\n", encoding="utf-8")
    logger.info("saved calibration for %s to %s (%d samples)", calibration.model, path, calibration.n_samples)
    return path


def load_calibration(model: str, calibration_dir: Path | None = None) -> CalibrationData | None:
    """Load calibration JSON for a model. Returns None if not found."""
    cdir = calibration_dir or _CALIBRATION_DIR
    safe_name = model.replace("/", "_").replace(":", "_")
    path = cdir / f"{safe_name}.json"
    if not path.exists():
        return None
    data = json.loads(path.read_text(encoding="utf-8"))
    return CalibrationData.from_dict(data)


def load_all_calibrations(calibration_dir: Path | None = None) -> dict[str, CalibrationData]:
    """Load all calibration JSONs from a directory."""
    cdir = calibration_dir or _CALIBRATION_DIR
    result: dict[str, CalibrationData] = {}
    if not cdir.exists():
        return result
    for path in sorted(cdir.glob("*.json")):
        data = json.loads(path.read_text(encoding="utf-8"))
        cal = CalibrationData.from_dict(data)
        result[cal.model] = cal
    return result


# --- live calibration run (requires LLM API key) ---

def run_calibration(
    models: list[str],
    *,
    ground_truth_dir: Path | None = None,
    output_dir: Path | None = None,
    provider: Any | None = None,
) -> dict[str, CalibrationData]:
    """Run full calibration pipeline against ground truth.

    Requires OPENAI_API_KEY or ANTHROPIC_API_KEY in environment.
    """
    gt_dir = ground_truth_dir or _GROUND_TRUTH_DIR
    entries = load_ground_truth_entries(gt_dir)
    gt_version = f"gt-{len(entries)}"
    logger.info("loaded %d ground truth entries", len(entries))
    if provider is None:
        raise ValueError(
            "provider is required for live calibration runs. "
            "Construct an LLMProvider and pass it explicitly."
        )
    trials = _run_triage_trials(provider, models, entries)
    results: dict[str, CalibrationData] = {}
    for model in models:
        cal = build_calibration(model, trials, gt_version=gt_version)
        save_calibration(cal, output_dir)
        results[model] = cal
        logger.info(
            "model=%s accuracy=%.3f n=%d tp_thresh=%.2f fp_thresh=%.2f",
            model, cal.raw_accuracy, cal.n_samples,
            cal.optimal_tp_threshold, cal.optimal_fp_threshold,
        )
    return results


def _run_triage_trials(
    provider: Any,
    models: list[str],
    entries: list[GroundTruthEntry],
) -> list[TrialResult]:
    """Run each model against each GT entry, collecting trial results."""
    from piranesi.llm.prompts import triage_classify
    from piranesi.llm.sanitize import strip_comments
    trials: list[TrialResult] = []
    for entry in entries:
        messages = _gt_entry_to_triage_messages(entry)
        for model in models:
            try:
                response = provider.complete(
                    stage="triage",
                    model=model,
                    messages=messages,
                    tools=[triage_classify.TOOL_SPEC],
                    tool_choice={"type": "function", "function": {"name": triage_classify.TOOL_NAME}},
                )
                payload = json.loads(response.content)
                verdict = payload.get("verdict", "true_positive")
                confidence = float(payload.get("confidence", 0.5))
            except Exception:
                logger.warning("failed triage for %s with %s, using fallback", entry.id, model, exc_info=True)
                verdict = "true_positive"
                confidence = 0.5
            is_correct = (
                (verdict == "true_positive" and entry.label == "true_positive")
                or (verdict == "false_positive" and entry.label == "false_positive")
            )
            trials.append(TrialResult(
                gt_id=entry.id, cwe_id=entry.cwe_id, label=entry.label,
                model=model, reported_confidence=confidence,
                verdict=verdict, correct=is_correct,
            ))
            logger.debug(
                "%s model=%s verdict=%s conf=%.2f correct=%s",
                entry.id, model, verdict, confidence, is_correct,
            )
    return trials


def _gt_entry_to_triage_messages(entry: GroundTruthEntry) -> list[dict[str, str]]:
    """Convert a ground truth entry to triage prompt messages."""
    finding_summary = (
        f"{entry.cwe_id}: {entry.cwe_name}\n"
        f"Source: {entry.taint_source}\n"
        f"Sink: {entry.taint_sink}\n"
        f"Files: {', '.join(entry.affected_files)}\n"
        f"Lines: {entry.line_numbers}"
    )
    taint_path = " -> ".join(entry.taint_path)
    code_context = entry.notes or ""
    return [
        {
            "role": "system",
            "content": (
                "You are a security triage agent. Classify this finding as "
                "true_positive or false_positive. Respond with JSON containing "
                "verdict, confidence (0-1), and explanation."
            ),
        },
        {
            "role": "user",
            "content": (
                f"Finding:\n{finding_summary}\n\n"
                f"Taint path:\n{taint_path}\n\n"
                f"Code context:\n{code_context}"
            ),
        },
    ]


# --- CLI entry point ---

def main() -> None:
    logging.basicConfig(level=logging.INFO, format="%(levelname)s %(name)s: %(message)s")
    models = sys.argv[1:] if len(sys.argv) > 1 else ["gpt-4o-mini", "gpt-4o"]
    logger.info("calibrating models: %s", models)
    results = run_calibration(models)
    for model, cal in results.items():
        print(f"{model}: accuracy={cal.raw_accuracy:.3f} n={cal.n_samples} "
              f"tp_thresh={cal.optimal_tp_threshold:.2f} fp_thresh={cal.optimal_fp_threshold:.2f}")


if __name__ == "__main__":
    main()
