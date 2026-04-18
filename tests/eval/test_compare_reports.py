from __future__ import annotations

import json
import sys
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))
if str(REPO_ROOT / "eval") not in sys.path:
    sys.path.insert(0, str(REPO_ROOT / "eval"))

from eval import compare_reports  # noqa: E402


def _write_report(
    path: Path,
    *,
    detection_rate: float,
    fp_rate: float,
    ts_detection_rate: float,
    ts_fp_rate: float,
) -> None:
    payload = {
        "results": {
            "overall": {
                "detection_rate": detection_rate,
                "fp_suppression_rate": fp_rate,
            },
            "per_group": {
                "language": {
                    "typescript": {
                        "detection_rate": ts_detection_rate,
                        "fp_suppression_rate": ts_fp_rate,
                    }
                }
            },
        }
    }
    path.write_text(json.dumps(payload, indent=2), encoding="utf-8")


def test_compare_reports_renders_text_summary(tmp_path: Path, capsys) -> None:
    baseline = tmp_path / "baseline.json"
    current = tmp_path / "current.json"
    _write_report(
        baseline,
        detection_rate=0.80,
        fp_rate=0.75,
        ts_detection_rate=0.85,
        ts_fp_rate=0.70,
    )
    _write_report(
        current,
        detection_rate=0.78,
        fp_rate=0.76,
        ts_detection_rate=0.80,
        ts_fp_rate=0.72,
    )

    exit_code = compare_reports.main(
        [
            "--baseline-report",
            str(baseline),
            "--current-report",
            str(current),
            "--top",
            "5",
        ]
    )
    output = capsys.readouterr().out

    assert exit_code == 0
    assert "Validate-All Report Comparison" in output
    assert "detection_rate: 0.800 -> 0.780 (-0.020)" in output
    assert "language=typescript" in output


def test_compare_reports_json_output_and_threshold_failures(tmp_path: Path, capsys) -> None:
    baseline = tmp_path / "baseline.json"
    current = tmp_path / "current.json"
    _write_report(
        baseline,
        detection_rate=0.82,
        fp_rate=0.78,
        ts_detection_rate=0.88,
        ts_fp_rate=0.74,
    )
    _write_report(
        current,
        detection_rate=0.80,
        fp_rate=0.77,
        ts_detection_rate=0.84,
        ts_fp_rate=0.73,
    )

    exit_code = compare_reports.main(
        [
            "--baseline-report",
            str(baseline),
            "--current-report",
            str(current),
            "--json",
            "--min-detection-rate-delta",
            "0.0",
        ]
    )
    output = capsys.readouterr().out
    payload = json.loads(output)

    assert exit_code == 1
    assert payload["comparison"]["overall"]["detection_rate"]["delta"] == pytest.approx(-0.02)


def test_compare_reports_group_delta_threshold_fail(tmp_path: Path, capsys) -> None:
    baseline = tmp_path / "baseline.json"
    current = tmp_path / "current.json"
    _write_report(
        baseline,
        detection_rate=0.82,
        fp_rate=0.78,
        ts_detection_rate=0.88,
        ts_fp_rate=0.74,
    )
    _write_report(
        current,
        detection_rate=0.84,
        fp_rate=0.80,
        ts_detection_rate=0.84,
        ts_fp_rate=0.76,
    )

    exit_code = compare_reports.main(
        [
            "--baseline-report",
            str(baseline),
            "--current-report",
            str(current),
            "--min-group-detection-delta",
            "language=typescript:0.0",
        ]
    )
    output = capsys.readouterr().out
    assert exit_code == 3
    assert "group detection delta threshold failed" in output


def test_compare_reports_writes_markdown_output(tmp_path: Path, capsys) -> None:
    baseline = tmp_path / "baseline.json"
    current = tmp_path / "current.json"
    markdown = tmp_path / "comparison.md"
    _write_report(
        baseline,
        detection_rate=0.84,
        fp_rate=0.79,
        ts_detection_rate=0.90,
        ts_fp_rate=0.75,
    )
    _write_report(
        current,
        detection_rate=0.82,
        fp_rate=0.80,
        ts_detection_rate=0.88,
        ts_fp_rate=0.76,
    )

    exit_code = compare_reports.main(
        [
            "--baseline-report",
            str(baseline),
            "--current-report",
            str(current),
            "--markdown-output",
            str(markdown),
            "--top",
            "3",
        ]
    )
    _ = capsys.readouterr()
    text = markdown.read_text(encoding="utf-8")

    assert exit_code == 0
    assert markdown.exists()
    assert "# Validate-All Comparison" in text
    assert "| Metric | Baseline | Current | Delta |" in text
    assert "Top regressions (detection_rate)" in text
