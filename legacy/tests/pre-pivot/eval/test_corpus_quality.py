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

from eval import corpus_quality  # noqa: E402
from eval.ground_truth.schema import Complexity, GroundTruthEntry, Label  # noqa: E402


def _entry(
    *,
    entry_id: str,
    label: Label,
    affected_file: str,
    taint_field_path: str | None,
    field_sensitive_label: Label | None = None,
) -> GroundTruthEntry:
    return GroundTruthEntry(
        id=entry_id,
        source_project="synthetic",
        commit_hash="deadbeef",
        cwe_id="CWE-89",
        cwe_name="SQL Injection",
        label=label,
        affected_files=[affected_file],
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
        language="typescript",
        framework="express",
        taint_step_count=2,
        taint_field_path=taint_field_path,
        field_sensitive_label=field_sensitive_label,
    )


def _write_entries(directory: Path, entries: list[GroundTruthEntry]) -> None:
    directory.mkdir(parents=True, exist_ok=True)
    for entry in entries:
        payload = entry.model_dump(mode="json")
        (directory / f"{entry.id}.yaml").write_text(
            yaml.safe_dump(payload, sort_keys=False),
            encoding="utf-8",
        )


def test_corpus_quality_reports_runnable_and_field_coverage(tmp_path: Path, capsys) -> None:
    gt_dir = tmp_path / "ground_truth"
    existing_fixture = tmp_path / "fixtures" / "present.ts"
    existing_fixture.parent.mkdir(parents=True, exist_ok=True)
    existing_fixture.write_text("console.log('ok')\n", encoding="utf-8")

    _write_entries(
        gt_dir,
        [
            _entry(
                entry_id="gt-001",
                label=Label.TRUE_POSITIVE,
                affected_file=str(existing_fixture),
                taint_field_path="query.id",
                field_sensitive_label=Label.TRUE_POSITIVE,
            ),
            _entry(
                entry_id="gt-002",
                label=Label.FALSE_POSITIVE,
                affected_file=str(tmp_path / "fixtures" / "missing.ts"),
                taint_field_path=None,
                field_sensitive_label=None,
            ),
        ],
    )

    exit_code = corpus_quality.main(
        [
            "--gt-dir",
            str(gt_dir),
            "--dimension",
            "cwe+language",
            "--min-count",
            "2",
            "--json",
        ]
    )
    payload = json.loads(capsys.readouterr().out)

    assert exit_code == 0
    assert payload["considered_entries"] == 2
    assert payload["runnable_entries"] == 1
    assert payload["runnable_ratio"] == pytest.approx(0.5)
    assert payload["false_positive_ratio"] == pytest.approx(0.5)
    assert payload["required_field_coverage"]["taint_field_path"]["present"] == 1
    assert payload["required_field_coverage"]["field_sensitive_label"]["present"] == 1


def test_corpus_quality_enforces_thresholds(tmp_path: Path, capsys) -> None:
    gt_dir = tmp_path / "ground_truth"
    _write_entries(
        gt_dir,
        [
            _entry(
                entry_id="gt-003",
                label=Label.TRUE_POSITIVE,
                affected_file=str(tmp_path / "fixtures" / "missing.ts"),
                taint_field_path=None,
                field_sensitive_label=None,
            )
        ],
    )

    exit_code = corpus_quality.main(
        [
            "--gt-dir",
            str(gt_dir),
            "--dimension",
            "cwe+language",
            "--min-runnable-ratio",
            "0.8",
            "--min-fp-ratio",
            "0.2",
            "--max-gap-count",
            "0",
            "--min-field-coverage",
            "taint_field_path=1.0",
        ]
    )
    output = capsys.readouterr().out

    assert exit_code == 1
    assert "quality gate failed: runnable_ratio" in output
    assert "quality gate failed: false_positive_ratio" in output
    assert "quality gate failed: gap_count" in output
    assert "quality gate failed: taint_field_path coverage" in output


def test_parse_field_coverage_thresholds_rejects_invalid_inputs() -> None:
    with pytest.raises(ValueError, match="expected field=ratio"):
        corpus_quality._parse_field_coverage_thresholds(["taint_field_path:0.9"])
    with pytest.raises(ValueError, match="invalid ratio"):
        corpus_quality._parse_field_coverage_thresholds(["taint_field_path=abc"])
    with pytest.raises(ValueError, match="ratio out of range"):
        corpus_quality._parse_field_coverage_thresholds(["taint_field_path=1.2"])
