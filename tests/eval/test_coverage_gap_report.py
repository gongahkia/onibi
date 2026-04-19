from __future__ import annotations

import json
import sys
from pathlib import Path

import pytest
import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))
if str(REPO_ROOT / "eval") not in sys.path:
    sys.path.insert(0, str(REPO_ROOT / "eval"))

from eval import coverage_gap_report  # noqa: E402
from eval.ground_truth.schema import Complexity, GroundTruthEntry, Label  # noqa: E402


def _entry(
    *,
    entry_id: str,
    cwe_id: str,
    label: Label,
    language: str,
    framework: str,
) -> GroundTruthEntry:
    return GroundTruthEntry(
        id=entry_id,
        source_project="synthetic",
        commit_hash="deadbeef",
        cwe_id=cwe_id,
        cwe_name="Fixture",
        label=label,
        affected_files=["eval/synthetic/sqli-pg-raw.ts"],
        line_numbers=[5],
        taint_source="req.query.id",
        taint_sink="db.query()",
        taint_path=["req.query.id", "db.query(sql)"],
        complexity=Complexity.SIMPLE,
        exploitable=label == Label.TRUE_POSITIVE,
        reference_exploit=None,
        reference_fix_commit=None,
        notes="fixture",
        discovery_method="synthetic",
        language=language,
        framework=framework,
        taint_step_count=2,
        taint_field_path="query.id",
    )


def _write_entries(directory: Path, entries: list[GroundTruthEntry]) -> None:
    directory.mkdir(parents=True, exist_ok=True)
    for entry in entries:
        payload = entry.model_dump(mode="json")
        (directory / f"{entry.id}.yaml").write_text(
            yaml.safe_dump(payload, sort_keys=False),
            encoding="utf-8",
        )


def test_coverage_gap_report_flags_underrepresented_slices(tmp_path: Path, capsys) -> None:
    gt_dir = tmp_path / "ground_truth"
    _write_entries(
        gt_dir,
        [
            _entry(
                entry_id="gt-001",
                cwe_id="CWE-89",
                label=Label.TRUE_POSITIVE,
                language="typescript",
                framework="express",
            ),
            _entry(
                entry_id="gt-002",
                cwe_id="CWE-89",
                label=Label.FALSE_POSITIVE,
                language="typescript",
                framework="express",
            ),
            _entry(
                entry_id="gt-003",
                cwe_id="CWE-22",
                label=Label.TRUE_POSITIVE,
                language="go",
                framework="gin",
            ),
        ],
    )

    exit_code = coverage_gap_report.main(
        [
            "--gt-dir",
            str(gt_dir),
            "--dimension",
            "cwe+language",
            "--min-count",
            "3",
            "--json",
        ]
    )
    payload = json.loads(capsys.readouterr().out)

    assert exit_code == 0
    assert payload["gap_count"] == 2
    assert payload["plan_top_n"] == 10
    assert len(payload["expansion_plan"]) == 2
    first = payload["gaps"][0]
    assert first["dimension"] == "cwe+language"
    assert first["slice_key"] == "CWE-22 | go"
    assert first["needed_for_min_count"] == 2
    assert first["target_true_positive"] == 1
    assert first["target_false_positive"] == 2
    assert first["needed_true_positive"] == 0
    assert first["needed_false_positive"] == 2
    assert "false_positive" in first["recommendation"]


def test_coverage_gap_report_validates_dimension_parts() -> None:
    with pytest.raises(ValueError, match="unsupported dimension part"):
        coverage_gap_report.main([
            "--dimension",
            "cwe+invalid",
        ])


def test_coverage_gap_report_validates_plan_top_n() -> None:
    with pytest.raises(ValueError, match="--plan-top-n must be >= 0"):
        coverage_gap_report.main([
            "--plan-top-n",
            "-1",
        ])
