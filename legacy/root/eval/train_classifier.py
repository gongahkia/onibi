#!/usr/bin/env python3
"""Train the local ML false-positive classifier."""

from __future__ import annotations

import argparse
import json
import logging
import pickle
import re
import shutil
from collections import Counter
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

from eval.ground_truth.schema import Complexity, GroundTruthEntry, Label
from eval.scoring import load_ground_truth_entries
from piranesi.models import (
    CandidateFinding,
    PathCondition,
    SourceLocation,
    TaintSink,
    TaintSource,
    TaintStep,
)
from piranesi.triage.ml_classifier import default_model_path, extract_features, feature_names

logger = logging.getLogger(__name__)
_DEFAULT_GT_DIR = Path(__file__).resolve().parent / "ground_truth"
_DEFAULT_MODEL_DIR = default_model_path().parent


def load_training_data(
    gt_dir: Path | None = None,
) -> tuple[Any, Any, list[str]]:
    np = _require_numpy()
    entries = load_ground_truth_entries(gt_dir or _DEFAULT_GT_DIR)
    ordered_features = feature_names()
    feature_rows: list[list[float]] = []
    labels: list[int] = []
    gt_ids: list[str] = []

    for entry in entries:
        finding = gt_entry_to_candidate(entry)
        feature_map = extract_features(finding)
        feature_rows.append([float(feature_map.get(name, 0.0)) for name in ordered_features])
        labels.append(1 if entry.label == Label.TRUE_POSITIVE else 0)
        gt_ids.append(entry.id)

    return np.array(feature_rows, dtype=float), np.array(labels, dtype=int), gt_ids


def train(
    *,
    gt_dir: Path | None = None,
    output_dir: Path | None = None,
    model_version: int | None = None,
    min_recall: float = 0.95,
) -> Path:
    (
        _np,
        CalibratedClassifierCV,
        GradientBoostingClassifier,
        LogisticRegression,
        precision_score,
        recall_score,
        RandomForestClassifier,
        StratifiedKFold,
        cross_val_predict,
    ) = _require_training_dependencies()

    out_dir = (output_dir or _DEFAULT_MODEL_DIR).expanduser()
    out_dir.mkdir(parents=True, exist_ok=True)

    X, y, gt_ids = load_training_data(gt_dir)
    if len(gt_ids) == 0:
        raise RuntimeError("No ground truth entries found for ML training.")

    label_counts = Counter(int(label) for label in y.tolist())
    min_count = min(label_counts.values())
    ordered_features = feature_names()
    logger.info(
        "training ML classifier with %d samples, %d features, TP=%d, FP=%d",
        len(gt_ids),
        X.shape[1] if len(X.shape) == 2 else 0,
        label_counts.get(1, 0),
        label_counts.get(0, 0),
    )

    model_specs = _candidate_models(
        RandomForestClassifier=RandomForestClassifier,
        LogisticRegression=LogisticRegression,
        GradientBoostingClassifier=GradientBoostingClassifier,
    )

    cv_results: list[dict[str, Any]] = []
    best_result: dict[str, Any] | None = None

    if len(label_counts) > 1 and min_count >= 2:
        skf = StratifiedKFold(n_splits=min(5, min_count), shuffle=True, random_state=42)
        for name, model in model_specs:
            probabilities = cross_val_predict(model, X, y, cv=skf, method="predict_proba")
            positive_probabilities = [float(row[1]) for row in probabilities]
            threshold, recall_value, precision_value = _select_threshold(
                y_true=y.tolist(),
                probabilities=positive_probabilities,
                min_recall=min_recall,
                recall_score=recall_score,
                precision_score=precision_score,
            )
            logger.info(
                "%s CV recall=%.3f precision=%.3f threshold=%.3f",
                name,
                recall_value,
                precision_value,
                threshold,
            )
            result = {
                "name": name,
                "model": model,
                "threshold": threshold,
                "recall": recall_value,
                "precision": precision_value,
            }
            cv_results.append(result)
        best_result = _select_best_result(cv_results, min_recall=min_recall)
        assert best_result is not None
        calibrated = CalibratedClassifierCV(
            estimator=best_result["model"],
            cv=skf,
            method="sigmoid",
        )
        calibrated.fit(X, y)
    else:
        logger.warning(
            "GT set is too small for stratified cross-validation; fitting a single fallback model."
        )
        fallback_name, fallback_model = model_specs[0]
        fallback_model.fit(X, y)
        best_result = {
            "name": fallback_name,
            "model": fallback_model,
            "threshold": 0.5,
            "recall": 1.0,
            "precision": 1.0,
        }
        cv_results.append(best_result)
        calibrated = fallback_model

    importance_model = best_result["model"]
    importance_model.fit(X, y)
    version = model_version or _next_version(out_dir)
    model_path = out_dir / f"fp_classifier_v{version}.pkl"
    metadata_path = out_dir / f"fp_classifier_v{version}.json"
    latest_model_path = out_dir / default_model_path().name
    latest_metadata_path = out_dir / "fp_classifier.json"

    metadata = {
        "version": version,
        "training_date": datetime.now(UTC).isoformat(),
        "ground_truth_dir": str((gt_dir or _DEFAULT_GT_DIR).expanduser()),
        "ground_truth_count": len(gt_ids),
        "tp_count": label_counts.get(1, 0),
        "fp_count": label_counts.get(0, 0),
        "feature_names": ordered_features,
        "feature_count": len(ordered_features),
        "model_type": best_result["name"],
        "cv_recall": round(float(best_result["recall"]), 4),
        "cv_precision": round(float(best_result["precision"]), 4),
        "recommended_threshold": round(float(best_result["threshold"]), 4),
        "min_recall_target": float(min_recall),
        "all_models": {
            result["name"]: {
                "recall": round(float(result["recall"]), 4),
                "precision": round(float(result["precision"]), 4),
                "threshold": round(float(result["threshold"]), 4),
            }
            for result in cv_results
        },
        "feature_importances": _feature_importances(importance_model, ordered_features),
    }

    payload = {
        "model": calibrated,
        "feature_names": ordered_features,
        "metadata": metadata,
    }
    with model_path.open("wb") as handle:
        pickle.dump(payload, handle, protocol=pickle.HIGHEST_PROTOCOL)
    metadata_path.write_text(
        json.dumps(metadata, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )

    _write_latest_alias(model_path, latest_model_path)
    shutil.copyfile(metadata_path, latest_metadata_path)
    logger.info("saved ML classifier to %s", model_path)
    logger.info("saved ML classifier metadata to %s", metadata_path)
    return model_path


def gt_entry_to_candidate(entry: GroundTruthEntry) -> CandidateFinding:
    primary_file = entry.affected_files[0] if entry.affected_files else _default_filename(entry)
    primary_line = entry.line_numbers[0] if entry.line_numbers else 1
    sink_line = entry.line_numbers[-1] if entry.line_numbers else primary_line
    source_text = entry.taint_source or "user input"
    sink_text = entry.taint_sink or entry.cwe_name
    framework = entry.framework or _infer_framework(entry)
    severity = _severity_from_entry(entry)
    source_type = _infer_source_type(source_text)
    sink_type = _infer_sink_type(sink_text)
    taint_steps = _build_taint_steps(entry, primary_file, primary_line)
    path_conditions = _build_path_conditions(entry, primary_file, primary_line)
    metadata: dict[str, object] = {
        "framework": framework,
        "dep_reachable": True,
        "field_sensitive": bool(entry.taint_field_path),
        "z3_result": "SKIPPED",
        "commit_age_days": -1,
    }
    if entry.field_sensitive_label is not None:
        metadata["field_sensitive_gt_label"] = entry.field_sensitive_label.value

    return CandidateFinding(
        id=entry.id,
        vuln_class=f"{entry.cwe_id}: {entry.cwe_name}",
        source=TaintSource(
            location=SourceLocation(
                file=primary_file,
                line=primary_line,
                column=1,
                snippet=source_text,
            ),
            source_type=source_type,
            data_categories=["user_input"],
            parameter_name=_infer_parameter_name(source_text),
        ),
        sink=TaintSink(
            location=SourceLocation(
                file=primary_file,
                line=sink_line,
                column=1,
                snippet=sink_text,
            ),
            sink_type=sink_type,
            api_name=sink_text,
        ),
        taint_path=taint_steps,
        path_conditions=path_conditions,
        confidence=0.5,
        severity=severity,
        reachability="reachable",
        metadata=metadata,
    )


def _candidate_models(
    *,
    RandomForestClassifier: Any,
    LogisticRegression: Any,
    GradientBoostingClassifier: Any,
) -> list[tuple[str, Any]]:
    return [
        (
            "random_forest",
            RandomForestClassifier(
                n_estimators=200,
                max_depth=12,
                min_samples_leaf=2,
                class_weight="balanced",
                random_state=42,
                n_jobs=-1,
            ),
        ),
        (
            "logistic_regression",
            LogisticRegression(
                class_weight="balanced",
                max_iter=1000,
                random_state=42,
            ),
        ),
        (
            "gradient_boosted_trees",
            GradientBoostingClassifier(
                n_estimators=200,
                learning_rate=0.08,
                random_state=42,
            ),
        ),
    ]


def _select_threshold(
    *,
    y_true: list[int],
    probabilities: list[float],
    min_recall: float,
    recall_score: Any,
    precision_score: Any,
) -> tuple[float, float, float]:
    best: tuple[float, float, float] | None = None
    threshold_candidates = sorted(
        {0.0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0, *probabilities}
    )
    for threshold in threshold_candidates:
        predictions = [1 if probability >= threshold else 0 for probability in probabilities]
        recall_value = float(recall_score(y_true, predictions, zero_division=0))
        precision_value = float(precision_score(y_true, predictions, zero_division=0))
        if recall_value < min_recall:
            continue
        candidate = (float(threshold), recall_value, precision_value)
        if best is None or precision_value > best[2] or (
            precision_value == best[2] and threshold > best[0]
        ):
            best = candidate

    if best is not None:
        return best

    fallback: tuple[float, float, float] | None = None
    for threshold in threshold_candidates:
        predictions = [1 if probability >= threshold else 0 for probability in probabilities]
        recall_value = float(recall_score(y_true, predictions, zero_division=0))
        precision_value = float(precision_score(y_true, predictions, zero_division=0))
        candidate = (float(threshold), recall_value, precision_value)
        if fallback is None or recall_value > fallback[1] or (
            recall_value == fallback[1] and precision_value > fallback[2]
        ):
            fallback = candidate

    assert fallback is not None
    return fallback


def _select_best_result(results: list[dict[str, Any]], *, min_recall: float) -> dict[str, Any]:
    viable = [result for result in results if float(result["recall"]) >= min_recall]
    if viable:
        return max(
            viable,
            key=lambda item: (
                float(item["precision"]),
                float(item["recall"]),
                float(item["threshold"]),
            ),
        )
    return max(
        results,
        key=lambda item: (
            float(item["recall"]),
            float(item["precision"]),
            float(item["threshold"]),
        ),
    )


def _feature_importances(model: Any, ordered_features: list[str]) -> dict[str, float]:
    raw_importances: list[float]
    if hasattr(model, "feature_importances_"):
        raw_importances = [float(value) for value in model.feature_importances_]
    elif hasattr(model, "coef_"):
        raw_importances = [abs(float(value)) for value in model.coef_[0]]
    else:
        return {}
    pairs = sorted(
        zip(ordered_features, raw_importances, strict=False),
        key=lambda item: item[1],
        reverse=True,
    )
    return {name: round(value, 6) for name, value in pairs[:20]}


def _next_version(model_dir: Path) -> int:
    versions: list[int] = []
    for path in model_dir.glob("fp_classifier_v*.pkl"):
        suffix = path.stem.rsplit("_v", maxsplit=1)[-1]
        if suffix.isdigit():
            versions.append(int(suffix))
    return max(versions, default=0) + 1


def _write_latest_alias(source: Path, latest: Path) -> None:
    if latest.exists() or latest.is_symlink():
        latest.unlink()
    try:
        latest.symlink_to(source.name)
    except OSError:
        shutil.copyfile(source, latest)


def _build_taint_steps(
    entry: GroundTruthEntry,
    file_path: str,
    line_number: int,
) -> list[TaintStep]:
    steps: list[TaintStep] = []
    for index, description in enumerate(entry.taint_path or [entry.taint_sink], start=1):
        steps.append(
            TaintStep(
                location=SourceLocation(
                    file=file_path,
                    line=line_number + index,
                    column=1,
                    snippet=description,
                ),
                operation=description,
                taint_state="tainted",
                through_function=f"step_{index}" if index > 1 else None,
            )
        )
    if steps:
        return steps
    return [
        TaintStep(
            location=SourceLocation(
                file=file_path,
                line=line_number + 1,
                column=1,
                snippet=entry.taint_sink,
            ),
            operation=entry.taint_sink,
            taint_state="tainted",
        )
    ]


def _build_path_conditions(
    entry: GroundTruthEntry,
    file_path: str,
    line_number: int,
) -> list[PathCondition]:
    if entry.complexity not in {Complexity.CONTEXT_SENSITIVE, Complexity.CROSS_MODULE}:
        return []
    return [
        PathCondition(
            location=SourceLocation(
                file=file_path,
                line=line_number,
                column=1,
                snippet=entry.notes or entry.cwe_name,
            ),
            condition_type="branch",
            expression=entry.notes or entry.cwe_name,
            required_value=True,
        )
    ]


def _severity_from_entry(entry: GroundTruthEntry) -> str:
    if entry.cvss_score is not None:
        if entry.cvss_score >= 9.0:
            return "critical"
        if entry.cvss_score >= 7.0:
            return "high"
        if entry.cvss_score >= 4.0:
            return "medium"
        return "low"
    if entry.exploitable:
        return "high"
    return "medium"


def _default_filename(entry: GroundTruthEntry) -> str:
    extension = {
        "typescript": ".ts",
        "javascript": ".js",
        "python": ".py",
        "go": ".go",
        "java": ".java",
    }.get((entry.language or "").lower(), ".ts")
    return f"synthetic{extension}"


def _infer_framework(entry: GroundTruthEntry) -> str:
    tokens = f"{entry.source_project} {entry.notes} {entry.taint_sink}".lower()
    for framework in ("express", "nestjs", "fastify", "nextjs", "django", "flask", "gin", "spring"):
        if framework in tokens:
            return framework
    return "unknown"


def _infer_source_type(source_text: str) -> str:
    lowered = source_text.lower()
    if "req.body" in lowered or "body" in lowered:
        return "req.body"
    if "req.query" in lowered or "query" in lowered:
        return "req.query"
    if "req.params" in lowered or "params" in lowered:
        return "req.params"
    if "req.headers" in lowered or "header" in lowered:
        return "req.headers"
    if "req.cookies" in lowered or "cookie" in lowered:
        return "req.cookies"
    if "env" in lowered:
        return "env"
    if "file" in lowered or "upload" in lowered:
        return "file"
    if "db" in lowered or "database" in lowered:
        return "db"
    return source_text


def _infer_sink_type(sink_text: str) -> str:
    lowered = sink_text.lower()
    if any(token in lowered for token in ("query", "sql", "sequelize", "knex", "where")):
        return "query"
    if any(token in lowered for token in ("exec", "spawn", "system", "command")):
        return "exec"
    if any(token in lowered for token in ("render", "template", "html")):
        return "render"
    if any(token in lowered for token in ("write", "sendfile", "fs.", "path")):
        return "write"
    if "redirect" in lowered:
        return "redirect"
    if any(token in lowered for token in ("eval", "expression", "spel")):
        return "eval"
    if any(
        token in lowered
        for token in ("deserialize", "yaml", "pickle", "json.parse", "objectmapper")
    ):
        return "deserialize"
    return "other"


def _infer_parameter_name(source_text: str) -> str | None:
    segments = re.split(r"[^A-Za-z0-9_]+", source_text)
    for segment in reversed(segments):
        if segment and segment not in {"req", "query", "body", "params"}:
            return segment
    return None


def _require_numpy() -> Any:
    try:
        import numpy as np
    except ModuleNotFoundError as exc:
        raise RuntimeError(
            "Training the ML classifier requires the optional ML dependencies. "
            "Install the dev dependencies or `piranesi[ml]`."
        ) from exc
    return np


def _require_training_dependencies() -> tuple[Any, ...]:
    try:
        import numpy as np
        from sklearn.calibration import CalibratedClassifierCV
        from sklearn.ensemble import GradientBoostingClassifier, RandomForestClassifier
        from sklearn.linear_model import LogisticRegression
        from sklearn.metrics import precision_score, recall_score
        from sklearn.model_selection import StratifiedKFold, cross_val_predict
    except ModuleNotFoundError as exc:
        raise RuntimeError(
            "Training the ML classifier requires the optional ML dependencies. "
            "Install the dev dependencies or `piranesi[ml]`."
        ) from exc
    return (
        np,
        CalibratedClassifierCV,
        GradientBoostingClassifier,
        LogisticRegression,
        precision_score,
        recall_score,
        RandomForestClassifier,
        StratifiedKFold,
        cross_val_predict,
    )


def _build_arg_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--gt-dir", type=Path, default=_DEFAULT_GT_DIR)
    parser.add_argument("--output-dir", type=Path, default=_DEFAULT_MODEL_DIR)
    parser.add_argument("--min-recall", type=float, default=0.95)
    parser.add_argument("--version", type=int, default=None)
    return parser


def main() -> int:
    logging.basicConfig(level=logging.INFO, format="%(levelname)s %(message)s")
    args = _build_arg_parser().parse_args()
    model_path = train(
        gt_dir=args.gt_dir,
        output_dir=args.output_dir,
        model_version=args.version,
        min_recall=args.min_recall,
    )
    print(model_path)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
