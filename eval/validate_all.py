from __future__ import annotations

import argparse
import json
import sys
from collections import defaultdict
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
    parser.add_argument("--keep-output", action="store_true", help="Keep per-fixture stage artifacts.")
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


def _aggregate(results: list[Any]) -> dict[str, Any]:
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

    return {
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


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    entries = _load_entries(args.gt_dir, args.filter)
    cache: dict[
        tuple[str, str | None, str | None],
        tuple[list[Any] | None, Path, str | None],
    ] = {}
    results = []

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
            findings, output_dir, error = cache[cache_key]
            if error is not None or findings is None:
                result = validation_error_result(
                    entry,
                    error=error or "unknown validation error",
                    fixture_root=fixture_root,
                    output_dir=output_dir,
                )
            else:
                result = validation_result_from_findings(
                    entry,
                    findings=findings,
                    fixture_root=fixture_root,
                    output_dir=output_dir,
                )
            results.append(result)
            print(result.message)

        report = {
            "timestamp": datetime.now(UTC).isoformat(),
            "piranesi_version": PIRANESI_VERSION,
            "total_entries": len(entries),
            "results": _aggregate(results),
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
        return 0
    finally:
        if not args.keep_output:
            for _findings, output_dir, _error in cache.values():
                cleanup_output_dir(output_dir)


if __name__ == "__main__":
    raise SystemExit(main())
