from __future__ import annotations

import argparse
import json
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any

REPO_ROOT = Path(__file__).resolve().parents[1]
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))
if str(REPO_ROOT / "eval") not in sys.path:
    sys.path.insert(0, str(REPO_ROOT / "eval"))

from eval.coverage_gap_report import (  # noqa: E402
    _DEFAULT_DIMENSIONS,
    _normalize_dimensions,
    build_coverage_gap_report,
    build_filter_predicate,
    load_ground_truth_entries,
)
from eval.ground_truth.schema import GroundTruthEntry, Label  # noqa: E402

_DEFAULT_REQUIRED_FIELDS = (
    "line_numbers",
    "taint_step_count",
    "taint_field_path",
    "field_sensitive_label",
)


@dataclass(frozen=True, slots=True)
class _FieldCoverageThreshold:
    field: str
    threshold: float


@dataclass(frozen=True, slots=True)
class FieldCoverageMetric:
    present: int
    missing: int
    ratio: float

    def to_dict(self) -> dict[str, Any]:
        return {
            "present": self.present,
            "missing": self.missing,
            "ratio": self.ratio,
        }


@dataclass(frozen=True, slots=True)
class CorpusQualityReport:
    gt_dir: str
    total_entries: int
    considered_entries: int
    runnable_entries: int
    runnable_ratio: float
    true_positive_entries: int
    false_positive_entries: int
    false_positive_ratio: float
    min_count: int
    dimensions: tuple[str, ...]
    gap_count: int
    required_field_coverage: dict[str, FieldCoverageMetric]

    def to_dict(self) -> dict[str, Any]:
        return {
            "gt_dir": self.gt_dir,
            "total_entries": self.total_entries,
            "considered_entries": self.considered_entries,
            "runnable_entries": self.runnable_entries,
            "runnable_ratio": self.runnable_ratio,
            "true_positive_entries": self.true_positive_entries,
            "false_positive_entries": self.false_positive_entries,
            "false_positive_ratio": self.false_positive_ratio,
            "min_count": self.min_count,
            "dimensions": list(self.dimensions),
            "gap_count": self.gap_count,
            "required_field_coverage": {
                key: metric.to_dict()
                for key, metric in self.required_field_coverage.items()
            },
        }


def _is_present(value: object) -> bool:
    if value is None:
        return False
    if isinstance(value, str):
        return bool(value.strip())
    if isinstance(value, list | tuple | set | dict):
        return bool(value)
    return True


def _rate(numerator: int, denominator: int) -> float:
    if denominator <= 0:
        return 0.0
    return numerator / denominator


def _resolve_path(path: str) -> Path:
    candidate = Path(path)
    if candidate.is_absolute():
        return candidate
    return REPO_ROOT / candidate


def _is_runnable(entry: GroundTruthEntry) -> bool:
    if not entry.affected_files:
        return False
    return all(_resolve_path(file_path).exists() for file_path in entry.affected_files)


def _parse_field_coverage_thresholds(
    expressions: list[str],
) -> tuple[_FieldCoverageThreshold, ...]:
    thresholds: list[_FieldCoverageThreshold] = []
    for expression in expressions:
        if "=" not in expression:
            raise ValueError(
                f"invalid field coverage threshold '{expression}', expected field=ratio"
            )
        field, threshold_text = expression.split("=", 1)
        normalized_field = field.strip()
        if not normalized_field:
            raise ValueError(
                f"invalid field coverage threshold '{expression}', empty field name"
            )
        try:
            threshold = float(threshold_text)
        except ValueError as exc:
            raise ValueError(
                f"invalid ratio '{threshold_text}' in '{expression}'"
            ) from exc
        if threshold < 0.0 or threshold > 1.0:
            raise ValueError(f"ratio out of range [0,1] in '{expression}'")
        thresholds.append(
            _FieldCoverageThreshold(
                field=normalized_field,
                threshold=threshold,
            )
        )
    return tuple(thresholds)


def _field_coverage(
    entries: list[GroundTruthEntry],
    fields: tuple[str, ...],
) -> dict[str, FieldCoverageMetric]:
    output: dict[str, FieldCoverageMetric] = {}
    payloads = [entry.model_dump(mode="json") for entry in entries]
    for field in fields:
        present = sum(1 for payload in payloads if _is_present(payload.get(field)))
        missing = len(entries) - present
        output[field] = FieldCoverageMetric(
            present=present,
            missing=missing,
            ratio=_rate(present, len(entries)),
        )
    return output


def build_corpus_quality_report(
    entries: list[GroundTruthEntry],
    *,
    gt_dir: Path,
    filters: tuple[tuple[str, str], ...],
    min_count: int,
    dimensions: tuple[tuple[str, ...], ...],
    required_fields: tuple[str, ...],
) -> CorpusQualityReport:
    filtered = [
        entry
        for entry in entries
        if all(str(entry.model_dump(mode="json").get(key)) == expected for key, expected in filters)
    ]

    runnable_entries = sum(1 for entry in filtered if _is_runnable(entry))
    tp_entries = sum(1 for entry in filtered if entry.label == Label.TRUE_POSITIVE)
    fp_entries = sum(1 for entry in filtered if entry.label == Label.FALSE_POSITIVE)

    gap_report = build_coverage_gap_report(
        filtered,
        gt_dir=gt_dir,
        dimensions=dimensions,
        filters=tuple(),
        min_count=min_count,
        max_results_per_dimension=max(len(filtered), 1),
        plan_top_n=0,
    )

    return CorpusQualityReport(
        gt_dir=str(gt_dir),
        total_entries=len(entries),
        considered_entries=len(filtered),
        runnable_entries=runnable_entries,
        runnable_ratio=_rate(runnable_entries, len(filtered)),
        true_positive_entries=tp_entries,
        false_positive_entries=fp_entries,
        false_positive_ratio=_rate(fp_entries, len(filtered)),
        min_count=min_count,
        dimensions=tuple("+".join(parts) for parts in dimensions),
        gap_count=len(gap_report.gaps),
        required_field_coverage=_field_coverage(filtered, required_fields),
    )


def render_corpus_quality(report: CorpusQualityReport) -> str:
    lines = [
        "Corpus Quality Report",
        f"- Directory: {report.gt_dir}",
        f"- Entries considered: {report.considered_entries}/{report.total_entries}",
        (
            f"- Runnable entries: {report.runnable_entries} "
            f"({report.runnable_ratio:.3f})"
        ),
        (
            f"- FP entries: {report.false_positive_entries} "
            f"({report.false_positive_ratio:.3f})"
        ),
        f"- Dimensions: {', '.join(report.dimensions)}",
        f"- Sparse slice gap rows: {report.gap_count}",
        "",
        "Required field coverage",
    ]
    for field, metric in report.required_field_coverage.items():
        lines.append(
            f"- {field}: present={metric.present} missing={metric.missing} ratio={metric.ratio:.3f}"
        )
    return "\n".join(lines)


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Compute corpus quality SLO metrics and optional threshold gates.",
    )
    parser.add_argument(
        "--gt-dir",
        type=Path,
        default=Path("eval/ground_truth"),
        help="Ground-truth directory.",
    )
    parser.add_argument(
        "--filter",
        action="append",
        default=[],
        help="Filter entries by key=value before computing quality metrics.",
    )
    parser.add_argument(
        "--dimension",
        action="append",
        default=[],
        help=(
            "Slice dimension(s), e.g. cwe+language or language+framework. "
            "Repeatable."
        ),
    )
    parser.add_argument(
        "--min-count",
        type=int,
        default=8,
        help="Minimum desired fixtures per slice for sparse-gap accounting.",
    )
    parser.add_argument(
        "--required-field",
        action="append",
        default=[],
        help=(
            "Field that must meet coverage thresholds. "
            "Defaults to line_numbers, taint_step_count, taint_field_path, field_sensitive_label."
        ),
    )
    parser.add_argument(
        "--min-runnable-ratio",
        type=float,
        help="Fail if runnable ratio falls below this value.",
    )
    parser.add_argument(
        "--min-fp-ratio",
        type=float,
        help="Fail if false-positive ratio falls below this value.",
    )
    parser.add_argument(
        "--max-gap-count",
        type=int,
        help="Fail if sparse gap row count exceeds this value.",
    )
    parser.add_argument(
        "--min-field-coverage",
        action="append",
        default=[],
        help="Field coverage threshold in the format field=ratio.",
    )
    parser.add_argument("--json", action="store_true", help="Emit machine-readable JSON output.")
    return parser.parse_args(argv)


def _validate_ratio_threshold(name: str, value: float | None) -> None:
    if value is None:
        return
    if value < 0.0 or value > 1.0:
        raise ValueError(f"{name} must be between 0 and 1")


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    if args.min_count <= 0:
        raise ValueError("--min-count must be > 0")
    if args.max_gap_count is not None and args.max_gap_count < 0:
        raise ValueError("--max-gap-count must be >= 0")

    _validate_ratio_threshold("--min-runnable-ratio", args.min_runnable_ratio)
    _validate_ratio_threshold("--min-fp-ratio", args.min_fp_ratio)

    required_fields = (
        tuple(dict.fromkeys(args.required_field))
        if args.required_field
        else _DEFAULT_REQUIRED_FIELDS
    )
    field_coverage_thresholds = _parse_field_coverage_thresholds(args.min_field_coverage)
    dimensions = _normalize_dimensions(args.dimension or list(_DEFAULT_DIMENSIONS))
    filters = build_filter_predicate(args.filter)
    entries = load_ground_truth_entries(args.gt_dir)
    report = build_corpus_quality_report(
        entries,
        gt_dir=args.gt_dir,
        filters=filters,
        min_count=args.min_count,
        dimensions=dimensions,
        required_fields=required_fields,
    )

    if args.json:
        print(json.dumps(report.to_dict(), indent=2))
    else:
        print(render_corpus_quality(report))

    failures: list[str] = []
    if (
        args.min_runnable_ratio is not None
        and report.runnable_ratio < args.min_runnable_ratio
    ):
        failures.append(
            f"runnable_ratio {report.runnable_ratio:.3f} < {args.min_runnable_ratio:.3f}"
        )
    if args.min_fp_ratio is not None and report.false_positive_ratio < args.min_fp_ratio:
        failures.append(
            f"false_positive_ratio {report.false_positive_ratio:.3f} < {args.min_fp_ratio:.3f}"
        )
    if args.max_gap_count is not None and report.gap_count > args.max_gap_count:
        failures.append(f"gap_count {report.gap_count} > {args.max_gap_count}")

    for threshold in field_coverage_thresholds:
        metric = report.required_field_coverage.get(threshold.field)
        if metric is None:
            failures.append(
                f"{threshold.field} coverage unavailable (field not computed)"
            )
            continue
        if metric.ratio < threshold.threshold:
            failures.append(
                f"{threshold.field} coverage {metric.ratio:.3f} < {threshold.threshold:.3f}"
            )

    if failures:
        for failure in failures:
            print(f"quality gate failed: {failure}")
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
