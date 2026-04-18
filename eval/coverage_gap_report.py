from __future__ import annotations

import argparse
import json
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import yaml

try:
    from eval.ground_truth.schema import GroundTruthEntry, Label
except ImportError:  # pragma: no cover - supports `python eval/coverage_gap_report.py`
    from ground_truth.schema import GroundTruthEntry, Label  # type: ignore[import-not-found,no-redef]

_ALLOWED_DIMENSION_PARTS = {
    "cwe",
    "language",
    "framework",
    "discovery_method",
    "complexity",
    "label",
    "source_project",
}
_DEFAULT_DIMENSIONS = ("cwe+language", "cwe+framework", "language+framework")


@dataclass(frozen=True, slots=True)
class GapRow:
    dimension: str
    slice_key: str
    count: int
    true_positive: int
    false_positive: int
    needed_for_min_count: int
    recommendation: str
    priority: int

    def to_dict(self) -> dict[str, Any]:
        return {
            "dimension": self.dimension,
            "slice_key": self.slice_key,
            "count": self.count,
            "true_positive": self.true_positive,
            "false_positive": self.false_positive,
            "needed_for_min_count": self.needed_for_min_count,
            "recommendation": self.recommendation,
            "priority": self.priority,
        }


@dataclass(frozen=True, slots=True)
class CoverageGapReport:
    gt_dir: str
    total_entries: int
    considered_entries: int
    min_count: int
    dimensions: tuple[str, ...]
    gaps: tuple[GapRow, ...]

    def to_dict(self) -> dict[str, Any]:
        by_dimension: dict[str, list[dict[str, Any]]] = defaultdict(list)
        for gap in self.gaps:
            by_dimension[gap.dimension].append(gap.to_dict())
        return {
            "gt_dir": self.gt_dir,
            "total_entries": self.total_entries,
            "considered_entries": self.considered_entries,
            "min_count": self.min_count,
            "dimensions": list(self.dimensions),
            "gap_count": len(self.gaps),
            "gaps": [gap.to_dict() for gap in self.gaps],
            "gaps_by_dimension": dict(by_dimension),
        }


def _is_present(value: object) -> bool:
    if value is None:
        return False
    if isinstance(value, str):
        return bool(value.strip())
    if isinstance(value, list | tuple | set | dict):
        return bool(value)
    return True


def build_filter_predicate(expressions: list[str]) -> tuple[tuple[str, str], ...]:
    pairs: list[tuple[str, str]] = []
    for expression in expressions:
        if "=" not in expression:
            raise ValueError(f"invalid filter expression: {expression}")
        key, value = expression.split("=", 1)
        normalized_key = key.strip()
        normalized_value = value.strip()
        if not normalized_key:
            raise ValueError(f"invalid filter expression: {expression}")
        pairs.append((normalized_key, normalized_value))
    return tuple(pairs)


def _matches_filters(payload: dict[str, Any], filters: tuple[tuple[str, str], ...]) -> bool:
    if not filters:
        return True
    for key, expected in filters:
        current = payload.get(key)
        if current is None or str(current) != expected:
            return False
    return True


def load_ground_truth_entries(gt_dir: Path) -> list[GroundTruthEntry]:
    entries: list[GroundTruthEntry] = []
    seen_ids: set[str] = set()
    for path in sorted(gt_dir.glob("*.yaml")):
        payload = yaml.safe_load(path.read_text(encoding="utf-8"))
        entry = GroundTruthEntry.model_validate(payload)
        if entry.id in seen_ids:
            raise ValueError(f"duplicate ground truth id: {entry.id}")
        seen_ids.add(entry.id)
        entries.append(entry)
    return entries


def _normalize_dimensions(dimensions: list[str] | None) -> tuple[tuple[str, ...], ...]:
    raw_dimensions = dimensions if dimensions else list(_DEFAULT_DIMENSIONS)
    normalized: list[tuple[str, ...]] = []
    seen: set[str] = set()

    for raw in raw_dimensions:
        parts = [part.strip() for part in raw.split("+") if part.strip()]
        if not parts:
            raise ValueError(f"invalid dimension: {raw}")
        for part in parts:
            if part not in _ALLOWED_DIMENSION_PARTS:
                allowed = ", ".join(sorted(_ALLOWED_DIMENSION_PARTS))
                raise ValueError(f"unsupported dimension part '{part}'. allowed: {allowed}")
        canonical = tuple(parts)
        key = "+".join(canonical)
        if key in seen:
            continue
        seen.add(key)
        normalized.append(canonical)

    return tuple(normalized)


def _entry_dimension_value(entry: GroundTruthEntry, part: str) -> str:
    if part == "cwe":
        return str(entry.cwe_id)
    if part == "language":
        return str(entry.language or "unknown")
    if part == "framework":
        return str(entry.framework or "unknown")
    if part == "discovery_method":
        return str(entry.discovery_method)
    if part == "complexity":
        return str(entry.complexity)
    if part == "label":
        return str(entry.label)
    if part == "source_project":
        return str(entry.source_project)
    return "unknown"


def _recommendation(*, tp_count: int, fp_count: int, needed_for_min_count: int) -> str:
    actions: list[str] = []
    if needed_for_min_count > 0:
        actions.append(f"add {needed_for_min_count} fixtures to reach minimum count")
    if tp_count == 0:
        actions.append("add true_positive fixture(s)")
    if fp_count == 0:
        actions.append("add false_positive fixture(s)")
    if not actions:
        actions.append("maintain current coverage")
    return "; ".join(actions)


def _priority(*, needed_for_min_count: int, tp_count: int, fp_count: int, count: int) -> int:
    score = needed_for_min_count * 100
    if tp_count == 0:
        score += 40
    if fp_count == 0:
        score += 40
    if count <= 1:
        score += 10
    return score


def build_coverage_gap_report(
    entries: list[GroundTruthEntry],
    *,
    gt_dir: Path,
    dimensions: tuple[tuple[str, ...], ...],
    filters: tuple[tuple[str, str], ...],
    min_count: int,
    max_results_per_dimension: int,
) -> CoverageGapReport:
    filtered_entries = [
        entry
        for entry in entries
        if _matches_filters(entry.model_dump(mode="json"), filters)
    ]

    gaps: list[GapRow] = []
    for dimension_parts in dimensions:
        bucket_counts: dict[tuple[str, ...], dict[str, int]] = defaultdict(
            lambda: {"count": 0, "tp": 0, "fp": 0}
        )
        for entry in filtered_entries:
            key = tuple(_entry_dimension_value(entry, part) for part in dimension_parts)
            bucket = bucket_counts[key]
            bucket["count"] += 1
            if entry.label == Label.TRUE_POSITIVE:
                bucket["tp"] += 1
            elif entry.label == Label.FALSE_POSITIVE:
                bucket["fp"] += 1

        dimension_name = "+".join(dimension_parts)
        ranked_rows: list[GapRow] = []
        for key_parts, counts in bucket_counts.items():
            needed = max(0, min_count - counts["count"])
            tp_count = counts["tp"]
            fp_count = counts["fp"]
            if needed == 0 and tp_count > 0 and fp_count > 0:
                continue

            ranked_rows.append(
                GapRow(
                    dimension=dimension_name,
                    slice_key=" | ".join(key_parts),
                    count=counts["count"],
                    true_positive=tp_count,
                    false_positive=fp_count,
                    needed_for_min_count=needed,
                    recommendation=_recommendation(
                        tp_count=tp_count,
                        fp_count=fp_count,
                        needed_for_min_count=needed,
                    ),
                    priority=_priority(
                        needed_for_min_count=needed,
                        tp_count=tp_count,
                        fp_count=fp_count,
                        count=counts["count"],
                    ),
                )
            )

        ranked_rows.sort(
            key=lambda row: (
                row.priority,
                row.needed_for_min_count,
                -row.count,
                row.slice_key,
            ),
            reverse=True,
        )
        gaps.extend(ranked_rows[:max_results_per_dimension])

    return CoverageGapReport(
        gt_dir=str(gt_dir),
        total_entries=len(entries),
        considered_entries=len(filtered_entries),
        min_count=min_count,
        dimensions=tuple("+".join(parts) for parts in dimensions),
        gaps=tuple(gaps),
    )


def render_coverage_gap_report(report: CoverageGapReport) -> str:
    lines = [
        "Coverage Gap Planner",
        f"- Directory: {report.gt_dir}",
        f"- Entries considered: {report.considered_entries}/{report.total_entries}",
        f"- Minimum target per slice: {report.min_count}",
        f"- Dimensions: {', '.join(report.dimensions)}",
        f"- Gap rows: {len(report.gaps)}",
        "",
    ]

    current_dimension: str | None = None
    for gap in report.gaps:
        if gap.dimension != current_dimension:
            if current_dimension is not None:
                lines.append("")
            lines.append(f"{gap.dimension}")
            current_dimension = gap.dimension
        lines.append(
            "- "
            f"{gap.slice_key}: count={gap.count} "
            f"tp={gap.true_positive} fp={gap.false_positive} "
            f"needed={gap.needed_for_min_count} priority={gap.priority}"
        )
        lines.append(f"  recommendation={gap.recommendation}")

    if not report.gaps:
        lines.append("No coverage gaps found for selected dimensions.")

    return "\n".join(lines)


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Identify under-covered ground-truth slices and suggest expansion targets.",
    )
    parser.add_argument(
        "--gt-dir",
        type=Path,
        default=Path("eval/ground_truth"),
        help="Ground-truth directory.",
    )
    parser.add_argument(
        "--dimension",
        action="append",
        default=[],
        help=(
            "Slice dimension(s), e.g. cwe+language or framework+language. "
            "Repeatable."
        ),
    )
    parser.add_argument(
        "--filter",
        action="append",
        default=[],
        help="Filter entries by key=value before computing gaps.",
    )
    parser.add_argument(
        "--min-count",
        type=int,
        default=8,
        help="Minimum desired fixtures per slice.",
    )
    parser.add_argument(
        "--max-results-per-dimension",
        type=int,
        default=20,
        help="Maximum reported gap rows per dimension.",
    )
    parser.add_argument("--json", action="store_true", help="Emit machine-readable JSON output.")
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    if args.min_count <= 0:
        raise ValueError("--min-count must be > 0")
    if args.max_results_per_dimension <= 0:
        raise ValueError("--max-results-per-dimension must be > 0")

    entries = load_ground_truth_entries(args.gt_dir)
    dimensions = _normalize_dimensions(args.dimension)
    filters = build_filter_predicate(args.filter)
    report = build_coverage_gap_report(
        entries,
        gt_dir=args.gt_dir,
        dimensions=dimensions,
        filters=filters,
        min_count=args.min_count,
        max_results_per_dimension=args.max_results_per_dimension,
    )

    if args.json:
        print(json.dumps(report.to_dict(), indent=2))
    else:
        print(render_coverage_gap_report(report))
    return 0


if __name__ == "__main__":  # pragma: no cover
    raise SystemExit(main())
