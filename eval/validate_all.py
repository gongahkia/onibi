from __future__ import annotations

import argparse
import json
import re
import sys
from collections import defaultdict
from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

REPO_ROOT = Path(__file__).resolve().parents[1]
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))
if str(REPO_ROOT / "src") not in sys.path:
    sys.path.insert(0, str(REPO_ROOT / "src"))

from eval.fixture_validation import (
    DetectionExecutionError,
    PIRANESI_VERSION,
    build_filter_predicate,
    cleanup_output_dir,
    load_ground_truth_entry,
    resolve_fixture_root,
    run_detection,
    validation_error_result,
    validation_result_from_findings,
)
from eval.scoring import NormalizedFinding


@dataclass(frozen=True, slots=True)
class _ValidationRecord:
    result: Any
    entry: Any


@dataclass(frozen=True, slots=True)
class _GroupThreshold:
    group: str
    value: str
    threshold: float


@dataclass(frozen=True, slots=True)
class _GroupDeltaThreshold:
    group: str
    value: str
    threshold: float


@dataclass(frozen=True, slots=True)
class _HistorySnapshot:
    snapshot_path: Path
    latest_path: Path
    index_path: Path


_ALLOWED_GROUP_KEYS = {
    "cwe",
    "framework",
    "language",
    "complexity",
    "discovery_method",
    "source_project",
}
_HISTORY_TIMESTAMP_FORMAT = "%Y%m%dT%H%M%SZ"
_HISTORY_LABEL_PATTERN = re.compile(r"[^A-Za-z0-9._-]+")


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Batch-validate ground-truth fixtures and report per-CWE detection rates."
    )
    parser.add_argument("--gt-dir", type=Path, required=True, help="Ground-truth directory.")
    parser.add_argument(
        "--fixtures-dir",
        type=Path,
        help="Optional base directory for relative fixture paths.",
    )
    parser.add_argument("--output", type=Path, help="Write the validation report to JSON.")
    parser.add_argument(
        "--history-dir",
        type=Path,
        default=Path("eval/history"),
        help="Directory for auto-written validate_all history snapshots.",
    )
    parser.add_argument(
        "--history-label",
        type=str,
        help="Optional label suffix for history snapshots (sanitized).",
    )
    parser.add_argument(
        "--baseline-report",
        type=Path,
        help="Optional previous validate_all report JSON used for delta comparisons.",
    )
    parser.add_argument(
        "--filter",
        action="append",
        default=[],
        help="Filter entries by key=value, for example discovery_method=cve_mining.",
    )
    parser.add_argument(
        "--min-detection-rate",
        type=float,
        help="Exit with code 1 if the overall TP detection rate is below this threshold.",
    )
    parser.add_argument(
        "--min-fp-rate",
        type=float,
        help="Exit with code 2 if the overall FP suppression rate is below this threshold.",
    )
    parser.add_argument(
        "--min-detection-rate-delta",
        type=float,
        help="Exit with code 5 if (current_detection_rate - baseline_detection_rate) is below this value.",
    )
    parser.add_argument(
        "--min-fp-rate-delta",
        type=float,
        help="Exit with code 6 if (current_fp_suppression_rate - baseline_fp_suppression_rate) is below this value.",
    )
    parser.add_argument(
        "--group-by",
        action="append",
        default=[],
        help=(
            "Include grouped metrics by one of: "
            "cwe, framework, language, complexity, discovery_method, source_project."
        ),
    )
    parser.add_argument(
        "--min-group-detection-rate",
        action="append",
        default=[],
        help=(
            "Per-group TP detection threshold in the form "
            "group=value:rate, for example language=typescript:0.85."
        ),
    )
    parser.add_argument(
        "--min-group-fp-rate",
        action="append",
        default=[],
        help=(
            "Per-group FP suppression threshold in the form "
            "group=value:rate, for example framework=express:0.90."
        ),
    )
    parser.add_argument(
        "--min-group-detection-delta",
        action="append",
        default=[],
        help=(
            "Per-group detection-rate delta threshold in the form "
            "group=value:delta where delta = current - baseline."
        ),
    )
    parser.add_argument(
        "--min-group-fp-delta",
        action="append",
        default=[],
        help=(
            "Per-group FP suppression delta threshold in the form "
            "group=value:delta where delta = current - baseline."
        ),
    )
    parser.add_argument("--keep-output", action="store_true", help="Keep per-fixture stage artifacts.")
    parser.add_argument(
        "--no-history",
        action="store_true",
        help="Disable auto-writing timestamped history snapshots.",
    )
    parser.add_argument("--verbose", action="store_true", help="Stream Piranesi output while scanning.")
    return parser.parse_args(argv)


def _load_entries(gt_dir: Path, filters: list[str]) -> list[Any]:
    predicate = build_filter_predicate(filters)
    entries = []
    for path in sorted(gt_dir.glob("*.yaml")):
        entry = load_ground_truth_entry(path)
        if predicate(entry):
            entries.append(entry)
    return entries


def _rate(numerator: int, denominator: int) -> float | None:
    if denominator == 0:
        return None
    return numerator / denominator


def _normalize_group_by(group_by: list[str]) -> tuple[str, ...]:
    normalized = []
    seen: set[str] = set()
    for item in group_by:
        key = item.strip()
        if key not in _ALLOWED_GROUP_KEYS:
            allowed = ", ".join(sorted(_ALLOWED_GROUP_KEYS))
            raise ValueError(f"unsupported group key '{key}'. allowed: {allowed}")
        if key not in seen:
            seen.add(key)
            normalized.append(key)
    return tuple(normalized)


def _parse_group_thresholds(expressions: list[str]) -> tuple[_GroupThreshold, ...]:
    thresholds: list[_GroupThreshold] = []
    for expression in expressions:
        if "=" not in expression or ":" not in expression:
            raise ValueError(
                f"invalid group threshold '{expression}', expected group=value:rate"
            )
        group, remainder = expression.split("=", 1)
        value, threshold_text = remainder.rsplit(":", 1)
        normalized_group = group.strip()
        if normalized_group not in _ALLOWED_GROUP_KEYS:
            allowed = ", ".join(sorted(_ALLOWED_GROUP_KEYS))
            raise ValueError(
                f"unsupported group key '{normalized_group}' in '{expression}'. allowed: {allowed}"
            )
        normalized_value = value.strip()
        if not normalized_value:
            raise ValueError(f"empty group value in '{expression}'")
        try:
            threshold = float(threshold_text)
        except ValueError as exc:
            raise ValueError(f"invalid threshold '{threshold_text}' in '{expression}'") from exc
        if threshold < 0.0 or threshold > 1.0:
            raise ValueError(f"threshold out of range [0,1] in '{expression}'")
        thresholds.append(
            _GroupThreshold(
                group=normalized_group,
                value=normalized_value,
                threshold=threshold,
            )
        )
    return tuple(thresholds)


def _parse_group_delta_thresholds(expressions: list[str]) -> tuple[_GroupDeltaThreshold, ...]:
    thresholds: list[_GroupDeltaThreshold] = []
    for expression in expressions:
        if "=" not in expression or ":" not in expression:
            raise ValueError(
                f"invalid group delta threshold '{expression}', expected group=value:delta"
            )
        group, remainder = expression.split("=", 1)
        value, threshold_text = remainder.rsplit(":", 1)
        normalized_group = group.strip()
        if normalized_group not in _ALLOWED_GROUP_KEYS:
            allowed = ", ".join(sorted(_ALLOWED_GROUP_KEYS))
            raise ValueError(
                f"unsupported group key '{normalized_group}' in '{expression}'. allowed: {allowed}"
            )
        normalized_value = value.strip()
        if not normalized_value:
            raise ValueError(f"empty group value in '{expression}'")
        try:
            threshold = float(threshold_text)
        except ValueError as exc:
            raise ValueError(f"invalid delta '{threshold_text}' in '{expression}'") from exc
        thresholds.append(
            _GroupDeltaThreshold(
                group=normalized_group,
                value=normalized_value,
                threshold=threshold,
            )
        )
    return tuple(thresholds)


def _group_value(record: _ValidationRecord, group: str) -> str:
    result = record.result
    entry = record.entry
    if group == "cwe":
        return str(result.cwe_id)
    if group == "framework":
        return str(entry.framework or "unknown")
    if group == "language":
        return str(entry.language or "unknown")
    if group == "complexity":
        return str(result.complexity)
    if group == "discovery_method":
        return str(entry.discovery_method or "unknown")
    if group == "source_project":
        return str(entry.source_project or "unknown")
    return "unknown"


def _aggregate_grouped(
    records: list[_ValidationRecord],
    *,
    group_by: tuple[str, ...],
) -> dict[str, dict[str, dict[str, Any]]]:
    grouped: dict[str, dict[str, dict[str, int | float | None]]] = {}
    for group in group_by:
        buckets: dict[str, dict[str, int]] = defaultdict(
            lambda: {
                "tp_detected": 0,
                "tp_total": 0,
                "fp_caught": 0,
                "fp_total": 0,
                "total_entries": 0,
            }
        )
        for record in records:
            value = _group_value(record, group)
            bucket = buckets[value]
            result = record.result
            bucket["total_entries"] += 1
            if result.expected_detected:
                bucket["tp_total"] += 1
                if result.matched:
                    bucket["tp_detected"] += 1
            else:
                bucket["fp_total"] += 1
                if not result.matched:
                    bucket["fp_caught"] += 1
        enriched: dict[str, dict[str, Any]] = {}
        for value, bucket in sorted(buckets.items()):
            enriched[value] = {
                "tp_detected": bucket["tp_detected"],
                "tp_total": bucket["tp_total"],
                "detection_rate": _rate(bucket["tp_detected"], bucket["tp_total"]),
                "fp_caught": bucket["fp_caught"],
                "fp_total": bucket["fp_total"],
                "fp_suppression_rate": _rate(bucket["fp_caught"], bucket["fp_total"]),
                "total_entries": bucket["total_entries"],
            }
        grouped[group] = enriched
    return grouped


def _aggregate(
    records: list[_ValidationRecord],
    *,
    group_by: tuple[str, ...],
) -> dict[str, Any]:
    results = [record.result for record in records]
    tp_detected = sum(1 for result in results if result.expected_detected and result.matched)
    tp_total = sum(1 for result in results if result.expected_detected)
    fp_caught = sum(1 for result in results if not result.expected_detected and not result.matched)
    fp_total = sum(1 for result in results if not result.expected_detected)

    per_cwe: dict[str, dict[str, Any]] = defaultdict(
        lambda: {
            "tp_detected": 0,
            "tp_total": 0,
            "rate": None,
            "fp_caught": 0,
            "fp_total": 0,
            "fp_suppression_rate": None,
        }
    )
    per_complexity: dict[str, dict[str, Any]] = defaultdict(
        lambda: {"detected": 0, "total": 0, "rate": None}
    )
    missed: list[dict[str, Any]] = []
    unexpected: list[dict[str, Any]] = []

    for result in results:
        cwe_bucket = per_cwe[result.cwe_id]
        if result.expected_detected:
            cwe_bucket["tp_total"] += 1
            if result.matched:
                cwe_bucket["tp_detected"] += 1
            else:
                missed.append(
                    {
                        "id": result.entry_id,
                        "cwe": result.cwe_id,
                        "cve_id": result.cve_id,
                        "reason": result.message,
                    }
                )
        else:
            cwe_bucket["fp_total"] += 1
            if not result.matched:
                cwe_bucket["fp_caught"] += 1
            else:
                unexpected.append(
                    {
                        "id": result.entry_id,
                        "cwe": result.cwe_id,
                        "cve_id": result.cve_id,
                        "reason": result.message,
                    }
                )

        complexity_bucket = per_complexity[result.complexity]
        complexity_bucket["total"] += 1
        if result.expected_detected:
            if result.matched:
                complexity_bucket["detected"] += 1
        elif not result.matched:
            complexity_bucket["detected"] += 1

    for bucket in per_cwe.values():
        bucket["rate"] = _rate(bucket["tp_detected"], bucket["tp_total"])
        bucket["fp_suppression_rate"] = _rate(bucket["fp_caught"], bucket["fp_total"])

    for bucket in per_complexity.values():
        bucket["rate"] = _rate(bucket["detected"], bucket["total"])

    output = {
        "overall": {
            "true_positives_detected": tp_detected,
            "true_positives_total": tp_total,
            "detection_rate": _rate(tp_detected, tp_total),
            "false_positives_caught": fp_caught,
            "false_positives_total": fp_total,
            "fp_suppression_rate": _rate(fp_caught, fp_total),
        },
        "per_cwe": dict(sorted(per_cwe.items())),
        "per_complexity": dict(sorted(per_complexity.items())),
        "missed": missed,
        "unexpected": unexpected,
    }
    if group_by:
        output["per_group"] = _aggregate_grouped(records, group_by=group_by)
    return output


def _evaluate_group_thresholds(
    per_group: dict[str, dict[str, dict[str, Any]]],
    thresholds: tuple[_GroupThreshold, ...],
    *,
    metric_key: str,
    metric_label: str,
) -> list[str]:
    failures: list[str] = []
    for threshold in thresholds:
        group_bucket = per_group.get(threshold.group)
        if group_bucket is None:
            failures.append(
                f"{threshold.group}={threshold.value} {metric_label} missing (group not computed)"
            )
            continue
        value_bucket = group_bucket.get(threshold.value)
        if value_bucket is None:
            failures.append(
                f"{threshold.group}={threshold.value} {metric_label} missing (group value absent)"
            )
            continue
        value = value_bucket.get(metric_key)
        if not isinstance(value, float):
            failures.append(
                f"{threshold.group}={threshold.value} {metric_label} is n/a < {threshold.threshold:.3f}"
            )
            continue
        if value < threshold.threshold:
            failures.append(
                f"{threshold.group}={threshold.value} {metric_label} {value:.3f} < {threshold.threshold:.3f}"
            )
    return failures


def _rate_delta(
    current: float | None,
    baseline: float | None,
) -> float | None:
    if current is None or baseline is None:
        return None
    return current - baseline


def _load_report(path: Path) -> dict[str, Any]:
    payload = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(payload, dict):
        raise ValueError(f"baseline report must be a JSON object: {path}")
    return payload


def _build_baseline_comparison(
    current_results: dict[str, Any],
    baseline_results: dict[str, Any],
) -> dict[str, Any]:
    current_overall = current_results.get("overall", {})
    baseline_overall = baseline_results.get("overall", {})
    current_per_group = current_results.get("per_group", {})
    baseline_per_group = baseline_results.get("per_group", {})

    overall = {
        "detection_rate": {
            "current": current_overall.get("detection_rate"),
            "baseline": baseline_overall.get("detection_rate"),
            "delta": _rate_delta(
                current_overall.get("detection_rate"),
                baseline_overall.get("detection_rate"),
            ),
        },
        "fp_suppression_rate": {
            "current": current_overall.get("fp_suppression_rate"),
            "baseline": baseline_overall.get("fp_suppression_rate"),
            "delta": _rate_delta(
                current_overall.get("fp_suppression_rate"),
                baseline_overall.get("fp_suppression_rate"),
            ),
        },
    }

    per_group: dict[str, dict[str, dict[str, dict[str, float | None]]]] = {}
    for group in sorted(set(current_per_group) | set(baseline_per_group)):
        current_group = current_per_group.get(group, {})
        baseline_group = baseline_per_group.get(group, {})
        values: dict[str, dict[str, dict[str, float | None]]] = {}
        for value in sorted(set(current_group) | set(baseline_group)):
            current_value = current_group.get(value, {})
            baseline_value = baseline_group.get(value, {})
            values[value] = {
                "detection_rate": {
                    "current": current_value.get("detection_rate"),
                    "baseline": baseline_value.get("detection_rate"),
                    "delta": _rate_delta(
                        current_value.get("detection_rate"),
                        baseline_value.get("detection_rate"),
                    ),
                },
                "fp_suppression_rate": {
                    "current": current_value.get("fp_suppression_rate"),
                    "baseline": baseline_value.get("fp_suppression_rate"),
                    "delta": _rate_delta(
                        current_value.get("fp_suppression_rate"),
                        baseline_value.get("fp_suppression_rate"),
                    ),
                },
            }
        per_group[group] = values
    return {"overall": overall, "per_group": per_group}


def _evaluate_group_delta_thresholds(
    comparison: dict[str, Any],
    thresholds: tuple[_GroupDeltaThreshold, ...],
    *,
    metric_key: str,
    metric_label: str,
) -> list[str]:
    failures: list[str] = []
    per_group = comparison.get("per_group", {})
    for threshold in thresholds:
        group_bucket = per_group.get(threshold.group)
        if group_bucket is None:
            failures.append(
                f"{threshold.group}={threshold.value} {metric_label} delta missing (group not available)"
            )
            continue
        value_bucket = group_bucket.get(threshold.value)
        if value_bucket is None:
            failures.append(
                f"{threshold.group}={threshold.value} {metric_label} delta missing (group value absent)"
            )
            continue
        metric_bucket = value_bucket.get(metric_key, {})
        delta = metric_bucket.get("delta") if isinstance(metric_bucket, dict) else None
        if not isinstance(delta, float):
            failures.append(
                f"{threshold.group}={threshold.value} {metric_label} delta is n/a < {threshold.threshold:.3f}"
            )
            continue
        if delta < threshold.threshold:
            failures.append(
                f"{threshold.group}={threshold.value} {metric_label} delta {delta:.3f} < {threshold.threshold:.3f}"
            )
    return failures


def _sanitize_history_label(value: str | None) -> str | None:
    if value is None:
        return None
    sanitized = _HISTORY_LABEL_PATTERN.sub("-", value.strip())
    sanitized = sanitized.strip("-_.")
    return sanitized or None


def _history_snapshot_filename(
    *,
    timestamp: datetime,
    label: str | None,
) -> str:
    stamp = timestamp.astimezone(UTC).strftime(_HISTORY_TIMESTAMP_FORMAT)
    if label:
        return f"validate-all-{stamp}-{label}.json"
    return f"validate-all-{stamp}.json"


def _write_history_snapshot(
    report: dict[str, Any],
    *,
    history_dir: Path,
    now: datetime,
    label: str | None,
) -> _HistorySnapshot:
    history_dir.mkdir(parents=True, exist_ok=True)
    filename = _history_snapshot_filename(
        timestamp=now,
        label=_sanitize_history_label(label),
    )
    snapshot_path = history_dir / filename
    latest_path = history_dir / "latest.json"
    serialized = json.dumps(report, indent=2) + "\n"
    snapshot_path.write_text(serialized, encoding="utf-8")
    latest_path.write_text(serialized, encoding="utf-8")
    index_path = _update_history_index(
        report,
        history_dir=history_dir,
        snapshot_path=snapshot_path,
    )
    return _HistorySnapshot(
        snapshot_path=snapshot_path,
        latest_path=latest_path,
        index_path=index_path,
    )


def _update_history_index(
    report: dict[str, Any],
    *,
    history_dir: Path,
    snapshot_path: Path,
    max_entries: int = 200,
) -> Path:
    index_path = history_dir / "index.json"
    existing: list[dict[str, Any]] = []
    if index_path.exists():
        try:
            payload = json.loads(index_path.read_text(encoding="utf-8"))
            if isinstance(payload, dict) and isinstance(payload.get("entries"), list):
                existing = [item for item in payload["entries"] if isinstance(item, dict)]
        except json.JSONDecodeError:
            existing = []

    overall = report.get("results", {}).get("overall", {})
    entry = {
        "timestamp": report.get("timestamp"),
        "snapshot_path": str(snapshot_path),
        "total_entries": report.get("total_entries"),
        "detection_rate": overall.get("detection_rate"),
        "fp_suppression_rate": overall.get("fp_suppression_rate"),
    }

    existing = [item for item in existing if item.get("snapshot_path") != str(snapshot_path)]
    existing.append(entry)
    existing.sort(key=lambda item: str(item.get("timestamp", "")))
    if max_entries > 0 and len(existing) > max_entries:
        existing = existing[-max_entries:]

    payload = {
        "updated_at": datetime.now(UTC).isoformat(),
        "entries": existing,
    }
    index_path.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
    return index_path


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    detection_group_thresholds = _parse_group_thresholds(args.min_group_detection_rate)
    fp_group_thresholds = _parse_group_thresholds(args.min_group_fp_rate)
    detection_group_delta_thresholds = _parse_group_delta_thresholds(
        args.min_group_detection_delta
    )
    fp_group_delta_thresholds = _parse_group_delta_thresholds(args.min_group_fp_delta)
    grouped_keys = _normalize_group_by(
        [
            *args.group_by,
            *(threshold.group for threshold in detection_group_thresholds),
            *(threshold.group for threshold in fp_group_thresholds),
            *(threshold.group for threshold in detection_group_delta_thresholds),
            *(threshold.group for threshold in fp_group_delta_thresholds),
        ]
    )
    entries = _load_entries(args.gt_dir, args.filter)
    cache: dict[
        tuple[str, str | None, str | None],
        tuple[list[NormalizedFinding] | None, Path, str | None],
    ] = {}
    records: list[_ValidationRecord] = []

    try:
        for entry in entries:
            fixture_root = resolve_fixture_root(entry, fixtures_dir=args.fixtures_dir)
            cache_key = (str(fixture_root), entry.framework, entry.language)
            if cache_key not in cache:
                try:
                    findings, output_dir = run_detection(
                        entry,
                        fixture_root=fixture_root,
                        fixtures_dir=args.fixtures_dir,
                        verbose=args.verbose,
                        keep_output=args.keep_output,
                    )
                    cache[cache_key] = (findings, output_dir, None)
                except DetectionExecutionError as exc:
                    cache[cache_key] = (None, exc.output_dir, str(exc))
            cached_findings, output_dir, error = cache[cache_key]
            if error is not None or cached_findings is None:
                result = validation_error_result(
                    entry,
                    error=error or "unknown validation error",
                    fixture_root=fixture_root,
                    output_dir=output_dir,
                )
            else:
                result = validation_result_from_findings(
                    entry,
                    findings=cached_findings,
                    fixture_root=fixture_root,
                    output_dir=output_dir,
                )
            records.append(_ValidationRecord(result=result, entry=entry))
            print(result.message)

        report: dict[str, Any] = {
            "timestamp": datetime.now(UTC).isoformat(),
            "piranesi_version": PIRANESI_VERSION,
            "total_entries": len(entries),
            "results": _aggregate(records, group_by=grouped_keys),
        }

        baseline_report: dict[str, Any] | None = None
        comparison: dict[str, Any] | None = None
        if args.baseline_report is not None:
            baseline_report = _load_report(args.baseline_report)
            baseline_results = baseline_report.get("results", {})
            if not isinstance(baseline_results, dict):
                raise ValueError("baseline report missing object field: results")
            comparison = _build_baseline_comparison(
                report["results"],
                baseline_results,
            )
            report["comparison"] = {
                "baseline_report": str(args.baseline_report),
                "metrics": comparison,
            }

        history_snapshot: _HistorySnapshot | None = None
        if not args.no_history:
            history_snapshot = _write_history_snapshot(
                report,
                history_dir=args.history_dir,
                now=datetime.now(UTC),
                label=args.history_label,
            )
            report["history"] = {
                "snapshot_path": str(history_snapshot.snapshot_path),
                "latest_path": str(history_snapshot.latest_path),
                "index_path": str(history_snapshot.index_path),
            }

        if args.output is not None:
            args.output.write_text(json.dumps(report, indent=2), encoding="utf-8")

        overall = report["results"]["overall"]
        detection_rate = overall["detection_rate"]
        fp_rate = overall["fp_suppression_rate"]
        print(
            "overall: "
            f"tp {overall['true_positives_detected']}/{overall['true_positives_total']} "
            f"({detection_rate if detection_rate is not None else 'n/a'}) | "
            f"fp {overall['false_positives_caught']}/{overall['false_positives_total']} "
            f"({fp_rate if fp_rate is not None else 'n/a'})"
        )

        if args.min_detection_rate is not None and detection_rate is not None:
            if detection_rate < args.min_detection_rate:
                return 1
        if args.min_fp_rate is not None and fp_rate is not None:
            if fp_rate < args.min_fp_rate:
                return 2

        per_group = report["results"].get("per_group", {})
        detection_failures = _evaluate_group_thresholds(
            per_group,
            detection_group_thresholds,
            metric_key="detection_rate",
            metric_label="detection_rate",
        )
        if detection_failures:
            for failure in detection_failures:
                print(f"group detection threshold failed: {failure}")
            return 3

        fp_failures = _evaluate_group_thresholds(
            per_group,
            fp_group_thresholds,
            metric_key="fp_suppression_rate",
            metric_label="fp_suppression_rate",
        )
        if fp_failures:
            for failure in fp_failures:
                print(f"group fp threshold failed: {failure}")
            return 4

        if args.min_detection_rate_delta is not None:
            if comparison is None:
                print("overall detection delta threshold failed: baseline report not provided")
                return 5
            delta = comparison["overall"]["detection_rate"]["delta"]
            if not isinstance(delta, float) or delta < args.min_detection_rate_delta:
                print(
                    "overall detection delta threshold failed: "
                    f"{delta if isinstance(delta, float) else 'n/a'} < {args.min_detection_rate_delta}"
                )
                return 5

        if args.min_fp_rate_delta is not None:
            if comparison is None:
                print("overall fp delta threshold failed: baseline report not provided")
                return 6
            delta = comparison["overall"]["fp_suppression_rate"]["delta"]
            if not isinstance(delta, float) or delta < args.min_fp_rate_delta:
                print(
                    "overall fp delta threshold failed: "
                    f"{delta if isinstance(delta, float) else 'n/a'} < {args.min_fp_rate_delta}"
                )
                return 6

        if detection_group_delta_thresholds:
            if comparison is None:
                print("group detection delta threshold failed: baseline report not provided")
                return 7
            failures = _evaluate_group_delta_thresholds(
                comparison,
                detection_group_delta_thresholds,
                metric_key="detection_rate",
                metric_label="detection_rate",
            )
            if failures:
                for failure in failures:
                    print(f"group detection delta threshold failed: {failure}")
                return 7

        if fp_group_delta_thresholds:
            if comparison is None:
                print("group fp delta threshold failed: baseline report not provided")
                return 8
            failures = _evaluate_group_delta_thresholds(
                comparison,
                fp_group_delta_thresholds,
                metric_key="fp_suppression_rate",
                metric_label="fp_suppression_rate",
            )
            if failures:
                for failure in failures:
                    print(f"group fp delta threshold failed: {failure}")
                return 8
        return 0
    finally:
        if not args.keep_output:
            for _findings, output_dir, _error in cache.values():
                cleanup_output_dir(output_dir)


if __name__ == "__main__":
    raise SystemExit(main())
