"""Evaluation plotting utilities for Piranesi baseline comparisons.

Generates per-CWE bar charts and precision/recall scatter plots from
EvaluationReport data. Requires matplotlib (optional dependency).
"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

try:
    import matplotlib  # type: ignore[import-untyped]

    matplotlib.use("Agg")
    import matplotlib.pyplot as plt  # type: ignore[import-untyped]

    HAS_MATPLOTLIB = True
except ImportError:  # pragma: no cover
    HAS_MATPLOTLIB = False


def _require_matplotlib() -> None:
    if not HAS_MATPLOTLIB:
        msg = "matplotlib is required for plotting: pip install matplotlib"
        raise ImportError(msg)


def plot_per_cwe_bars(
    reports: dict[str, dict[str, Any]],
    output_path: Path,
    metric: str = "f1",
) -> Path:
    """Bar chart comparing per-CWE metric across tools.

    Args:
        reports: {tool_name: EvaluationReport-as-dict}
        output_path: destination PNG path
        metric: one of "precision", "recall", "f1"
    """
    _require_matplotlib()
    tools = sorted(reports.keys())
    cwe_ids: set[str] = set()
    for report in reports.values():
        cwe_ids.update(report.get("per_cwe", {}).keys())
    cwe_ids_sorted = sorted(cwe_ids)
    if not cwe_ids_sorted:
        return output_path
    import numpy as np  # type: ignore[import-untyped]

    x = np.arange(len(cwe_ids_sorted))
    width = 0.8 / max(len(tools), 1)
    fig, ax = plt.subplots(figsize=(max(8, len(cwe_ids_sorted) * 2), 5))
    for i, tool in enumerate(tools):
        per_cwe = reports[tool].get("per_cwe", {})
        values = [per_cwe.get(cwe, {}).get(metric, 0.0) for cwe in cwe_ids_sorted]
        ax.bar(x + i * width, values, width, label=tool)
    ax.set_xlabel("CWE")
    ax.set_ylabel(metric.capitalize())
    ax.set_title(f"Per-CWE {metric.capitalize()} by Tool")
    ax.set_xticks(x + width * (len(tools) - 1) / 2)
    ax.set_xticklabels(cwe_ids_sorted, rotation=45, ha="right")
    ax.set_ylim(0.0, 1.05)
    ax.legend()
    fig.tight_layout()
    fig.savefig(output_path, dpi=150)
    plt.close(fig)
    return output_path


def plot_precision_recall_scatter(
    reports: dict[str, dict[str, Any]],
    output_path: Path,
) -> Path:
    """Scatter plot of overall precision vs recall for each tool."""
    _require_matplotlib()
    fig, ax = plt.subplots(figsize=(6, 6))
    for tool, report in sorted(reports.items()):
        overall = report.get("overall", {})
        p = overall.get("precision", 0.0)
        r = overall.get("recall", 0.0)
        ax.scatter(r, p, s=100, label=tool)
        ax.annotate(tool, (r, p), textcoords="offset points", xytext=(5, 5))
    ax.set_xlabel("Recall")
    ax.set_ylabel("Precision")
    ax.set_title("Precision vs Recall")
    ax.set_xlim(-0.05, 1.05)
    ax.set_ylim(-0.05, 1.05)
    ax.legend()
    ax.grid(alpha=0.3)
    fig.tight_layout()
    fig.savefig(output_path, dpi=150)
    plt.close(fig)
    return output_path


def plot_cost_vs_f1(
    reports: dict[str, dict[str, Any]],
    output_path: Path,
) -> Path:
    """Scatter plot of total cost vs overall F1 score (Pareto frontier)."""
    _require_matplotlib()
    fig, ax = plt.subplots(figsize=(6, 6))
    for tool, report in sorted(reports.items()):
        cost = report.get("total_cost_usd")
        f1 = report.get("overall", {}).get("f1", 0.0)
        if cost is None:
            continue
        ax.scatter(cost, f1, s=100, label=tool)
        ax.annotate(tool, (cost, f1), textcoords="offset points", xytext=(5, 5))
    ax.set_xlabel("Total Cost (USD)")
    ax.set_ylabel("F1 Score")
    ax.set_title("Cost vs F1 (Pareto Frontier)")
    ax.set_ylim(-0.05, 1.05)
    ax.legend()
    ax.grid(alpha=0.3)
    fig.tight_layout()
    fig.savefig(output_path, dpi=150)
    plt.close(fig)
    return output_path


def generate_all_plots(
    reports_dir: Path,
    output_dir: Path,
) -> list[Path]:
    """Load all *_report.json files from reports_dir and generate plots."""
    _require_matplotlib()
    output_dir.mkdir(parents=True, exist_ok=True)
    reports: dict[str, dict[str, Any]] = {}
    for path in sorted(reports_dir.glob("*_report.json")):
        tool = path.stem.removesuffix("_report")
        with path.open() as f:
            reports[tool] = json.load(f)
    if not reports:
        return []
    paths = [
        plot_per_cwe_bars(reports, output_dir / "per_cwe_f1.png", metric="f1"),
        plot_per_cwe_bars(reports, output_dir / "per_cwe_precision.png", metric="precision"),
        plot_per_cwe_bars(reports, output_dir / "per_cwe_recall.png", metric="recall"),
        plot_precision_recall_scatter(reports, output_dir / "precision_recall.png"),
        plot_cost_vs_f1(reports, output_dir / "cost_vs_f1.png"),
    ]
    return paths
