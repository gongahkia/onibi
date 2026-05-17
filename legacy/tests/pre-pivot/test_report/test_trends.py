from __future__ import annotations

import json
from pathlib import Path

import pytest
from typer.testing import CliRunner

from piranesi import __version__
from piranesi.cli import app
from piranesi.diff import BaselineArtifact, Finding
from piranesi.models import SourceLocation
from piranesi.report.trends import build_trend_report

runner = CliRunner()


def test_build_trend_report_computes_expected_metrics(tmp_path: Path) -> None:
    _write_baseline_history(tmp_path)

    report = build_trend_report(tmp_path)

    assert report.scans == 5
    assert report.period.start == "2026-01-01"
    assert report.period.end == "2026-01-05"
    assert report.series.scan_dates == [
        "2026-01-01",
        "2026-01-02",
        "2026-01-03",
        "2026-01-04",
        "2026-01-05",
    ]
    assert report.series.total_findings == [2, 3, 3, 2, 2]
    assert report.series.by_severity["critical"] == [1, 1, 0, 0, 0]
    assert report.series.by_severity["high"] == [1, 1, 1, 1, 0]
    assert report.series.by_severity["medium"] == [0, 1, 1, 0, 1]
    assert report.series.by_severity["low"] == [0, 0, 1, 1, 1]
    assert report.series.by_severity["informational"] == [0, 0, 0, 0, 0]
    assert report.series.by_cwe["CWE-89"] == [1, 1, 1, 1, 1]
    assert report.series.by_cwe["CWE-79"] == [1, 1, 0, 0, 0]
    assert report.series.by_cwe["CWE-22"] == [0, 1, 1, 0, 0]
    assert report.series.by_cwe["CWE-918"] == [0, 0, 1, 1, 1]
    assert report.series.fix_rate == [0, 0, 1, 1, 1]
    assert report.series.new_finding_velocity == [0, 1, 1, 0, 1]
    assert report.series.mean_time_to_fix_days == [None, None, 2.0, 2.0, 4.0]
    assert report.summary.total_reduction == 0.0
    assert report.summary.avg_fix_rate == 0.75
    assert report.summary.avg_new_finding_velocity == 0.75
    assert report.summary.mean_time_to_fix_days == pytest.approx(2.67)
    assert report.summary.alerts == [
        "2026-01-02: finding count increased 50.0% (2 -> 3)",
    ]


def test_trends_cli_writes_json_and_supports_filters(tmp_path: Path) -> None:
    _write_baseline_history(tmp_path)

    result = runner.invoke(
        app,
        [
            "trends",
            str(tmp_path),
            "--since",
            "2026-01-02",
            "--until",
            "2026-01-04",
        ],
    )

    assert result.exit_code == 0
    assert "Piranesi Trend Report" in result.stdout
    assert "warning:" not in result.stdout

    payload = json.loads((tmp_path / "trends.json").read_text(encoding="utf-8"))
    assert payload["period"] == {"start": "2026-01-02", "end": "2026-01-04"}
    assert payload["scans"] == 3
    assert payload["series"]["scan_dates"] == ["2026-01-02", "2026-01-03", "2026-01-04"]
    assert payload["series"]["total_findings"] == [3, 3, 2]
    assert payload["series"]["fix_rate"] == [0, 1, 1]
    assert payload["series"]["new_finding_velocity"] == [0, 1, 0]
    assert payload["summary"]["alerts"] == []


def test_trends_cli_can_emit_json_to_stdout(tmp_path: Path) -> None:
    _write_baseline_history(tmp_path)

    result = runner.invoke(app, ["trends", str(tmp_path), "--format", "json"])

    assert result.exit_code == 0
    payload = json.loads(result.stdout)
    assert payload["scans"] == 5
    assert payload["summary"]["alerts"] == [
        "2026-01-02: finding count increased 50.0% (2 -> 3)",
    ]
    assert (tmp_path / "trends.json").exists()


def _write_baseline_history(output_dir: Path) -> None:
    scans = [
        (
            "scan-01.json",
            "2026-01-01T00:00:00Z",
            [
                _finding("fingerprint-a", "CWE-89: SQL Injection", "high", "a"),
                _finding("fingerprint-b", "CWE-79: Cross-Site Scripting", "critical", "b"),
            ],
        ),
        (
            "scan-02.json",
            "2026-01-02T00:00:00Z",
            [
                _finding("fingerprint-a", "CWE-89: SQL Injection", "high", "a"),
                _finding("fingerprint-b", "CWE-79: Cross-Site Scripting", "critical", "b"),
                _finding("fingerprint-c", "CWE-22: Path Traversal", "medium", "c"),
            ],
        ),
        (
            "scan-03.json",
            "2026-01-03T00:00:00Z",
            [
                _finding("fingerprint-a", "CWE-89: SQL Injection", "high", "a"),
                _finding("fingerprint-c", "CWE-22: Path Traversal", "medium", "c"),
                _finding("fingerprint-d", "CWE-918: Server-Side Request Forgery", "low", "d"),
            ],
        ),
        (
            "scan-04.json",
            "2026-01-04T00:00:00Z",
            [
                _finding("fingerprint-a", "CWE-89: SQL Injection", "high", "a"),
                _finding("fingerprint-d", "CWE-918: Server-Side Request Forgery", "low", "d"),
            ],
        ),
        (
            "scan-05.json",
            "2026-01-05T00:00:00Z",
            [
                _finding("fingerprint-d", "CWE-918: Server-Side Request Forgery", "low", "d"),
                _finding("fingerprint-e", "CWE-89: SQL Injection", "medium", "e"),
            ],
        ),
    ]

    for filename, created_at, findings in scans:
        artifact = BaselineArtifact(
            created_at=created_at,
            piranesi_version=__version__,
            source_path=filename,
            findings=findings,
        )
        (output_dir / filename).write_text(artifact.model_dump_json(indent=2), encoding="utf-8")


def _finding(
    stable_fingerprint: str,
    vuln_class: str,
    severity: str,
    suffix: str,
) -> Finding:
    source_location = SourceLocation(
        file=f"src/{suffix}.ts",
        line=10,
        column=5,
        snippet=f"const userInput{suffix} = req.query.value;",
    )
    sink_location = SourceLocation(
        file=f"src/{suffix}.ts",
        line=20,
        column=9,
        snippet=f"dangerousCall{suffix}(userInput{suffix});",
    )
    return Finding(
        id=f"finding-{suffix}",
        stable_fingerprint=stable_fingerprint,
        vuln_class=vuln_class,
        severity=severity,
        confidence=0.95,
        source_location=source_location,
        sink_location=sink_location,
        source_type="req.query",
        source_parameter="value",
        sink_type="dangerousCall",
        sink_api="dangerousCall()",
        taint_path_length=1,
        taint_operations=["assignment"],
    )
