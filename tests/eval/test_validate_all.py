from __future__ import annotations

import json
import sys
from datetime import UTC, datetime
from types import SimpleNamespace
from typing import Any

import pytest

from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))
if str(REPO_ROOT / "eval") not in sys.path:
    sys.path.insert(0, str(REPO_ROOT / "eval"))

from eval.validate_all import (  # noqa: E402
    _HISTORY_TIMESTAMP_FORMAT,
    _ValidationRecord,
    _aggregate,
    _build_baseline_comparison,
    _evaluate_group_delta_thresholds,
    _evaluate_group_thresholds,
    _history_snapshot_filename,
    _normalize_group_by,
    _parse_group_delta_thresholds,
    _parse_group_thresholds,
    _sanitize_history_label,
    _update_history_index,
    _write_history_snapshot,
)


def _record(
    *,
    expected_detected: bool,
    matched: bool,
    cwe_id: str = "CWE-89",
    complexity: str = "simple",
    framework: str | None = None,
    language: str | None = None,
    discovery_method: str | None = None,
    source_project: str = "synthetic",
) -> _ValidationRecord:
    result = SimpleNamespace(
        expected_detected=expected_detected,
        matched=matched,
        cwe_id=cwe_id,
        complexity=complexity,
        entry_id="gt-001",
        cve_id=None,
        message="ok",
    )
    entry = SimpleNamespace(
        framework=framework,
        language=language,
        discovery_method=discovery_method,
        source_project=source_project,
    )
    return _ValidationRecord(result=result, entry=entry)


def test_parse_group_thresholds_parses_expected_format() -> None:
    thresholds = _parse_group_thresholds(
        [
            "language=typescript:0.85",
            "framework=express:0.9",
        ]
    )
    assert len(thresholds) == 2
    assert thresholds[0].group == "language"
    assert thresholds[0].value == "typescript"
    assert thresholds[0].threshold == pytest.approx(0.85)


def test_parse_group_thresholds_rejects_invalid_expressions() -> None:
    with pytest.raises(ValueError, match="expected group=value:rate"):
        _parse_group_thresholds(["language:0.85"])
    with pytest.raises(ValueError, match="unsupported group key"):
        _parse_group_thresholds(["team=appsec:0.9"])
    with pytest.raises(ValueError, match="threshold out of range"):
        _parse_group_thresholds(["language=typescript:1.5"])


def test_parse_group_delta_thresholds_parses_expected_format() -> None:
    thresholds = _parse_group_delta_thresholds(
        [
            "language=typescript:-0.02",
            "framework=express:0.01",
        ]
    )
    assert len(thresholds) == 2
    assert thresholds[0].group == "language"
    assert thresholds[0].value == "typescript"
    assert thresholds[0].threshold == pytest.approx(-0.02)


def test_parse_group_delta_thresholds_rejects_invalid_expressions() -> None:
    with pytest.raises(ValueError, match="expected group=value:delta"):
        _parse_group_delta_thresholds(["language:0.1"])
    with pytest.raises(ValueError, match="unsupported group key"):
        _parse_group_delta_thresholds(["team=appsec:0.1"])
    with pytest.raises(ValueError, match="invalid delta"):
        _parse_group_delta_thresholds(["language=typescript:not-a-number"])


def test_normalize_group_by_rejects_unknown_keys() -> None:
    with pytest.raises(ValueError, match="unsupported group key"):
        _normalize_group_by(["language", "unknown_key"])


def test_aggregate_includes_grouped_detection_and_fp_metrics() -> None:
    records = [
        _record(
            expected_detected=True,
            matched=True,
            language="typescript",
            framework="express",
            discovery_method="synthetic",
        ),
        _record(
            expected_detected=True,
            matched=False,
            language="typescript",
            framework="express",
            discovery_method="synthetic",
        ),
        _record(
            expected_detected=False,
            matched=False,
            language="python",
            framework="django",
            discovery_method="cve_mining",
        ),
    ]
    report = _aggregate(records, group_by=("language", "framework"))

    per_group: dict[str, Any] = report["per_group"]
    ts = per_group["language"]["typescript"]
    py = per_group["language"]["python"]
    express = per_group["framework"]["express"]

    assert ts["tp_detected"] == 1
    assert ts["tp_total"] == 2
    assert ts["detection_rate"] == pytest.approx(0.5)
    assert py["fp_caught"] == 1
    assert py["fp_total"] == 1
    assert py["fp_suppression_rate"] == pytest.approx(1.0)
    assert express["total_entries"] == 2


def test_evaluate_group_thresholds_reports_failures() -> None:
    per_group = {
        "language": {
            "typescript": {"detection_rate": 0.5, "fp_suppression_rate": 1.0},
        }
    }
    thresholds = _parse_group_thresholds(
        ["language=typescript:0.8", "language=python:0.7"]
    )
    failures = _evaluate_group_thresholds(
        per_group,
        thresholds,
        metric_key="detection_rate",
        metric_label="detection_rate",
    )
    assert len(failures) == 2
    assert "typescript" in failures[0]
    assert "group value absent" in failures[1]


def test_build_baseline_comparison_calculates_overall_and_group_deltas() -> None:
    current = {
        "overall": {"detection_rate": 0.8, "fp_suppression_rate": 0.7},
        "per_group": {
            "language": {
                "typescript": {"detection_rate": 0.9, "fp_suppression_rate": 0.8},
            }
        },
    }
    baseline = {
        "overall": {"detection_rate": 0.75, "fp_suppression_rate": 0.72},
        "per_group": {
            "language": {
                "typescript": {"detection_rate": 0.85, "fp_suppression_rate": 0.82},
            }
        },
    }
    comparison = _build_baseline_comparison(current, baseline)
    assert comparison["overall"]["detection_rate"]["delta"] == pytest.approx(0.05)
    assert comparison["overall"]["fp_suppression_rate"]["delta"] == pytest.approx(-0.02)
    assert (
        comparison["per_group"]["language"]["typescript"]["detection_rate"]["delta"]
        == pytest.approx(0.05)
    )


def test_evaluate_group_delta_thresholds_reports_failures() -> None:
    comparison = {
        "per_group": {
            "language": {
                "typescript": {
                    "detection_rate": {"delta": -0.05},
                    "fp_suppression_rate": {"delta": 0.03},
                }
            }
        }
    }
    thresholds = _parse_group_delta_thresholds(
        ["language=typescript:-0.01", "language=python:0.0"]
    )
    failures = _evaluate_group_delta_thresholds(
        comparison,
        thresholds,
        metric_key="detection_rate",
        metric_label="detection_rate",
    )
    assert len(failures) == 2
    assert "delta -0.050 < -0.010" in failures[0]
    assert "group value absent" in failures[1]


def test_history_label_is_sanitized() -> None:
    assert _sanitize_history_label("release candidate #12") == "release-candidate-12"
    assert _sanitize_history_label("..") is None
    assert _sanitize_history_label(None) is None


def test_history_snapshot_filename_uses_utc_stamp_and_optional_label() -> None:
    ts = datetime(2026, 4, 18, 12, 30, 45, tzinfo=UTC)
    assert _history_snapshot_filename(timestamp=ts, label=None) == "validate-all-20260418T123045Z.json"
    assert (
        _history_snapshot_filename(timestamp=ts, label="phase41")
        == "validate-all-20260418T123045Z-phase41.json"
    )
    assert ts.strftime(_HISTORY_TIMESTAMP_FORMAT) == "20260418T123045Z"


def test_write_history_snapshot_writes_snapshot_and_latest(tmp_path: Path) -> None:
    report = {"timestamp": "2026-04-18T12:30:45Z", "results": {"overall": {"detection_rate": 0.8}}}
    now = datetime(2026, 4, 18, 12, 30, 45, tzinfo=UTC)
    snapshot = _write_history_snapshot(
        report,
        history_dir=tmp_path / "history",
        now=now,
        label="release#1",
    )
    assert snapshot.snapshot_path.name == "validate-all-20260418T123045Z-release-1.json"
    assert snapshot.latest_path.name == "latest.json"
    assert snapshot.index_path.name == "index.json"
    assert snapshot.snapshot_path.exists()
    assert snapshot.latest_path.exists()
    assert snapshot.index_path.exists()
    assert snapshot.snapshot_path.read_text(encoding="utf-8") == snapshot.latest_path.read_text(
        encoding="utf-8"
    )
    index_payload = json.loads(snapshot.index_path.read_text(encoding="utf-8"))
    assert len(index_payload["entries"]) == 1
    assert index_payload["entries"][0]["snapshot_path"] == str(snapshot.snapshot_path)


def test_update_history_index_replaces_existing_snapshot_entry(tmp_path: Path) -> None:
    history_dir = tmp_path / "history"
    history_dir.mkdir(parents=True, exist_ok=True)
    snapshot_path = history_dir / "validate-all-20260418T123045Z.json"

    report_one = {
        "timestamp": "2026-04-18T12:30:45Z",
        "total_entries": 10,
        "results": {"overall": {"detection_rate": 0.7, "fp_suppression_rate": 0.6}},
    }
    report_two = {
        "timestamp": "2026-04-18T12:31:00Z",
        "total_entries": 10,
        "results": {"overall": {"detection_rate": 0.8, "fp_suppression_rate": 0.65}},
    }

    _update_history_index(report_one, history_dir=history_dir, snapshot_path=snapshot_path)
    _update_history_index(report_two, history_dir=history_dir, snapshot_path=snapshot_path)

    index_payload = json.loads((history_dir / "index.json").read_text(encoding="utf-8"))
    assert len(index_payload["entries"]) == 1
    assert index_payload["entries"][0]["detection_rate"] == pytest.approx(0.8)
