from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any

try:
    from eval.validate_all import (
        _build_baseline_comparison,
        _evaluate_group_delta_thresholds,
        _load_report,
        _parse_group_delta_thresholds,
    )
except ImportError:  # pragma: no cover - supports `python eval/compare_reports.py`
    from validate_all import (  # type: ignore[import-not-found,no-redef]
        _build_baseline_comparison,
        _evaluate_group_delta_thresholds,
        _load_report,
        _parse_group_delta_thresholds,
    )


def _format_metric(value: float | None) -> str:
    if value is None:
        return "n/a"
    return f"{value:.3f}"


def _format_delta(value: float | None) -> str:
    if value is None:
        return "n/a"
    return f"{value:+.3f}"


def _collect_group_metric_deltas(
    comparison: dict[str, Any],
    *,
    metric_key: str,
) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    per_group = comparison.get("per_group", {})
    if not isinstance(per_group, dict):
        return rows
    for group, values in per_group.items():
        if not isinstance(values, dict):
            continue
        for value, metrics in values.items():
            if not isinstance(metrics, dict):
                continue
            metric_payload = metrics.get(metric_key)
            if not isinstance(metric_payload, dict):
                continue
            rows.append(
                {
                    "group": str(group),
                    "value": str(value),
                    "current": metric_payload.get("current"),
                    "baseline": metric_payload.get("baseline"),
                    "delta": metric_payload.get("delta"),
                }
            )
    rows.sort(
        key=lambda row: (
            row["delta"] if isinstance(row.get("delta"), float) else float("-inf"),
            row["group"],
            row["value"],
        )
    )
    return rows


def render_comparison_summary(
    comparison: dict[str, Any],
    *,
    baseline_report: Path,
    current_report: Path,
    top: int,
) -> str:
    overall = comparison.get("overall", {})
    detection = overall.get("detection_rate", {})
    fp = overall.get("fp_suppression_rate", {})

    lines = [
        "Validate-All Report Comparison",
        f"- Baseline: {baseline_report}",
        f"- Current:  {current_report}",
        "",
        "Overall deltas",
        (
            "- detection_rate: "
            f"{_format_metric(detection.get('baseline'))} -> {_format_metric(detection.get('current'))} "
            f"({_format_delta(detection.get('delta'))})"
        ),
        (
            "- fp_suppression_rate: "
            f"{_format_metric(fp.get('baseline'))} -> {_format_metric(fp.get('current'))} "
            f"({_format_delta(fp.get('delta'))})"
        ),
    ]

    for metric_key in ("detection_rate", "fp_suppression_rate"):
        rows = _collect_group_metric_deltas(comparison, metric_key=metric_key)
        lines.append("")
        lines.append(f"Top regressions by {metric_key}")
        regressions = [
            row for row in rows if isinstance(row.get("delta"), float) and row["delta"] < 0
        ]
        if not regressions:
            lines.append("- none")
        else:
            for row in regressions[:top]:
                lines.append(
                    "- "
                    f"{row['group']}={row['value']}: "
                    f"{_format_metric(row['baseline'])} -> {_format_metric(row['current'])} "
                    f"({_format_delta(row['delta'])})"
                )
    return "\n".join(lines)


def render_comparison_markdown(
    comparison: dict[str, Any],
    *,
    baseline_report: Path,
    current_report: Path,
    top: int,
) -> str:
    overall = comparison.get("overall", {})
    detection = overall.get("detection_rate", {})
    fp = overall.get("fp_suppression_rate", {})
    lines = [
        "# Validate-All Comparison",
        "",
        f"- Baseline: `{baseline_report}`",
        f"- Current: `{current_report}`",
        "",
        "## Overall",
        "",
        "| Metric | Baseline | Current | Delta |",
        "| --- | ---: | ---: | ---: |",
        (
            "| detection_rate | "
            f"{_format_metric(detection.get('baseline'))} | "
            f"{_format_metric(detection.get('current'))} | "
            f"{_format_delta(detection.get('delta'))} |"
        ),
        (
            "| fp_suppression_rate | "
            f"{_format_metric(fp.get('baseline'))} | "
            f"{_format_metric(fp.get('current'))} | "
            f"{_format_delta(fp.get('delta'))} |"
        ),
    ]

    for metric_key in ("detection_rate", "fp_suppression_rate"):
        rows = _collect_group_metric_deltas(comparison, metric_key=metric_key)
        regressions = [row for row in rows if isinstance(row.get("delta"), float) and row["delta"] < 0]
        lines.extend(
            [
                "",
                f"## Top regressions ({metric_key})",
                "",
                "| Group | Value | Baseline | Current | Delta |",
                "| --- | --- | ---: | ---: | ---: |",
            ]
        )
        if not regressions:
            lines.append("| - | - | - | - | - |")
            continue
        for row in regressions[:top]:
            lines.append(
                "| "
                f"{row['group']} | {row['value']} | "
                f"{_format_metric(row['baseline'])} | {_format_metric(row['current'])} | {_format_delta(row['delta'])} |"
            )

    return "\n".join(lines) + "\n"


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Compare two validate_all report JSON files and summarize deltas."
    )
    parser.add_argument(
        "--baseline-report",
        type=Path,
        required=True,
        help="Baseline validate_all report JSON path.",
    )
    parser.add_argument(
        "--current-report",
        type=Path,
        required=True,
        help="Current validate_all report JSON path.",
    )
    parser.add_argument("--json", action="store_true", help="Emit JSON output.")
    parser.add_argument(
        "--markdown-output",
        type=Path,
        help="Optional markdown file path for PR/changelog-ready comparison output.",
    )
    parser.add_argument(
        "--top",
        type=int,
        default=10,
        help="Number of top regressions per metric to print in text mode.",
    )
    parser.add_argument(
        "--min-detection-rate-delta",
        type=float,
        help="Exit with code 1 if overall detection-rate delta is below this value.",
    )
    parser.add_argument(
        "--min-fp-rate-delta",
        type=float,
        help="Exit with code 2 if overall fp-suppression-rate delta is below this value.",
    )
    parser.add_argument(
        "--min-group-detection-delta",
        action="append",
        default=[],
        help="Per-group detection-rate delta threshold in the form group=value:delta.",
    )
    parser.add_argument(
        "--min-group-fp-delta",
        action="append",
        default=[],
        help="Per-group fp-suppression-rate delta threshold in the form group=value:delta.",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    baseline_payload = _load_report(args.baseline_report)
    current_payload = _load_report(args.current_report)

    baseline_results = baseline_payload.get("results", {})
    current_results = current_payload.get("results", {})
    if not isinstance(baseline_results, dict) or not isinstance(current_results, dict):
        raise ValueError("both reports must include an object field: results")

    comparison = _build_baseline_comparison(current_results, baseline_results)
    output = {
        "baseline_report": str(args.baseline_report),
        "current_report": str(args.current_report),
        "comparison": comparison,
    }

    if args.json:
        print(json.dumps(output, indent=2))
    else:
        print(
            render_comparison_summary(
                comparison,
                baseline_report=args.baseline_report,
                current_report=args.current_report,
                top=max(args.top, 1),
            )
        )

    if args.markdown_output is not None:
        args.markdown_output.parent.mkdir(parents=True, exist_ok=True)
        args.markdown_output.write_text(
            render_comparison_markdown(
                comparison,
                baseline_report=args.baseline_report,
                current_report=args.current_report,
                top=max(args.top, 1),
            ),
            encoding="utf-8",
        )

    detection_delta = comparison["overall"]["detection_rate"]["delta"]
    if args.min_detection_rate_delta is not None:
        if not isinstance(detection_delta, float) or detection_delta < args.min_detection_rate_delta:
            return 1

    fp_delta = comparison["overall"]["fp_suppression_rate"]["delta"]
    if args.min_fp_rate_delta is not None:
        if not isinstance(fp_delta, float) or fp_delta < args.min_fp_rate_delta:
            return 2

    detection_group_thresholds = _parse_group_delta_thresholds(args.min_group_detection_delta)
    detection_group_failures = _evaluate_group_delta_thresholds(
        comparison,
        detection_group_thresholds,
        metric_key="detection_rate",
        metric_label="detection_rate",
    )
    if detection_group_failures:
        for failure in detection_group_failures:
            print(f"group detection delta threshold failed: {failure}")
        return 3

    fp_group_thresholds = _parse_group_delta_thresholds(args.min_group_fp_delta)
    fp_group_failures = _evaluate_group_delta_thresholds(
        comparison,
        fp_group_thresholds,
        metric_key="fp_suppression_rate",
        metric_label="fp_suppression_rate",
    )
    if fp_group_failures:
        for failure in fp_group_failures:
            print(f"group fp delta threshold failed: {failure}")
        return 4

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
