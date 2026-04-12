from __future__ import annotations

import shutil
from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.rules.testing import build_rule_coverage_report, run_all_rule_tests

runner = CliRunner()


def test_run_all_rule_tests_passes_positive_and_negative_inline_cases(fixtures_dir: Path) -> None:
    rules_dir = fixtures_dir / "custom_rules" / "rules" / "nosql-injection.toml"

    summary = run_all_rule_tests(rules_dir)

    assert summary.rule_count == 1
    assert summary.total == 2
    assert summary.passed == 2
    assert summary.failed == 0
    assert any(result.expected_finding and result.passed for result in summary.results)
    assert any((not result.expected_finding) and result.passed for result in summary.results)


def test_run_all_rule_tests_reports_failed_expectations(
    tmp_path: Path,
    fixtures_dir: Path,
) -> None:
    custom_root = fixtures_dir / "custom_rules"
    rules_dir = tmp_path / "rules"
    rules_dir.mkdir(parents=True, exist_ok=True)
    shutil.copytree(custom_root / "tests", tmp_path / "tests")

    bad_rule = (
        (custom_root / "rules" / "nosql-injection.toml")
        .read_text(encoding="utf-8")
        .replace(
            "expect_sink_line = 16",
            "expect_sink_line = 99",
        )
    )
    (rules_dir / "nosql-injection.toml").write_text(bad_rule, encoding="utf-8")

    summary = run_all_rule_tests(rules_dir)

    assert summary.total == 2
    assert summary.passed == 1
    assert summary.failed == 1
    assert any(
        "did not match observed" in result.message
        for result in summary.results
        if not result.passed
    )


def test_build_rule_coverage_report_maps_cwe_aliases(
    tmp_path: Path,
    fixtures_dir: Path,
) -> None:
    rules_dir = fixtures_dir / "custom_rules" / "rules" / "nosql-injection.toml"
    ground_truth_dir = tmp_path / "ground_truth"
    ground_truth_dir.mkdir(parents=True, exist_ok=True)
    (ground_truth_dir / "gt-001.yaml").write_text("id: gt-001\ncwe_id: CWE-89\n", encoding="utf-8")
    (ground_truth_dir / "gt-002.yaml").write_text("id: gt-002\ncwe_id: CWE-79\n", encoding="utf-8")

    report = build_rule_coverage_report(rules_dir, ground_truth_dir=ground_truth_dir)

    assert report.rule_count == 1
    assert report.custom_cwe_ids == ("CWE-943",)
    assert report.covered_entry_ids == ("gt-001",)
    assert report.uncovered_entry_ids == ("gt-002",)
    assert report.rows[0].normalized_cwe_id == "CWE-89"


def test_rules_test_all_cli_runs_inline_tests(
    fixtures_dir: Path,
    monkeypatch,
) -> None:
    custom_root = fixtures_dir / "custom_rules"
    monkeypatch.chdir(custom_root)

    result = runner.invoke(app, ["rules", "test-all"])

    assert result.exit_code == 0
    assert "PASS custom-nosql-001" in result.stdout
    assert "Summary: 2/2 passed, 0 failed across 3 rules" in result.stdout


def test_rules_coverage_cli_reports_ground_truth_entries(
    tmp_path: Path,
    fixtures_dir: Path,
) -> None:
    rules_dir = fixtures_dir / "custom_rules" / "rules" / "nosql-injection.toml"
    ground_truth_dir = tmp_path / "ground_truth"
    ground_truth_dir.mkdir(parents=True, exist_ok=True)
    (ground_truth_dir / "gt-001.yaml").write_text("id: gt-001\ncwe_id: CWE-89\n", encoding="utf-8")
    (ground_truth_dir / "gt-002.yaml").write_text("id: gt-002\ncwe_id: CWE-79\n", encoding="utf-8")

    result = runner.invoke(
        app,
        [
            "rules",
            "coverage",
            "--rules-dir",
            str(rules_dir),
            "--ground-truth",
            str(ground_truth_dir),
        ],
    )

    assert result.exit_code == 0
    assert "Custom rule CWEs: CWE-943" in result.stdout
    assert "gt-001" in result.stdout
    assert "gt-002" in result.stdout
