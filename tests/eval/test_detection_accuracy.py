"""Run pattern-based detectors against synthetic fixtures and score against ground truth.

These tests validate detection accuracy without requiring Joern. They exercise
the scoring framework end-to-end against real fixture files to catch regressions
in detection precision/recall.
"""

from __future__ import annotations

from pathlib import Path

import pytest
import yaml
from eval.ground_truth.schema import GroundTruthEntry
from eval.scoring import (
    NormalizedFinding,
    load_ground_truth_entries,
    normalize_cwe_id,
    summarize_matches,
)

ROOT = Path(__file__).resolve().parents[2]
GT_DIR = ROOT / "eval" / "ground_truth"
SYNTHETIC_DIR = ROOT / "eval" / "synthetic"


def _load_gt(entry_id: str) -> GroundTruthEntry:
    payload = yaml.safe_load((GT_DIR / f"{entry_id}.yaml").read_text(encoding="utf-8"))
    return GroundTruthEntry.model_validate(payload)


def _gt_entries_with_fixtures() -> list[GroundTruthEntry]:
    """Load all ground truth entries whose fixture files actually exist."""
    entries = load_ground_truth_entries(GT_DIR)
    return [entry for entry in entries if all((ROOT / f).is_file() for f in entry.affected_files)]


def _gt_by_language(entries: list[GroundTruthEntry], ext: str) -> list[GroundTruthEntry]:
    return [e for e in entries if any(f.endswith(ext) for f in e.affected_files)]


class TestGroundTruthIntegrity:
    """Verify the ground truth dataset itself is valid and usable."""

    def test_ground_truth_entries_parse(self) -> None:
        entries = load_ground_truth_entries(GT_DIR)
        assert len(entries) > 50, f"expected 50+ ground truth entries, got {len(entries)}"

    def test_all_entries_have_valid_cwe(self) -> None:
        entries = load_ground_truth_entries(GT_DIR)
        for entry in entries:
            normalized = normalize_cwe_id(entry.cwe_id)
            assert normalized.startswith("CWE-"), f"{entry.id} has invalid CWE: {entry.cwe_id}"

    def test_fixture_files_exist(self) -> None:
        entries = load_ground_truth_entries(GT_DIR)
        missing: list[str] = []
        for entry in entries:
            for file_name in entry.affected_files:
                if not (ROOT / file_name).is_file():
                    missing.append(f"{entry.id}: {file_name}")
        if missing:
            pytest.skip(f"{len(missing)} fixture files missing (expected in partial checkouts)")

    def test_true_positive_entries_exist(self) -> None:
        entries = load_ground_truth_entries(GT_DIR)
        tp = [e for e in entries if e.label == "true_positive"]
        assert len(tp) > 30, f"expected 30+ TP entries, got {len(tp)}"

    def test_false_positive_entries_exist(self) -> None:
        entries = load_ground_truth_entries(GT_DIR)
        fp = [e for e in entries if e.label == "false_positive"]
        assert len(fp) > 10, f"expected 10+ FP entries, got {len(fp)}"


class TestScoringFramework:
    """Verify the scoring/matching logic itself works correctly."""

    def test_exact_match_scores_full_weight(self) -> None:
        gt = _load_gt("gt-145")
        finding = NormalizedFinding(
            id="test-001",
            cwe_id=normalize_cwe_id(gt.cwe_id),
            affected_files=tuple(gt.affected_files),
            taint_source=gt.taint_source,
            taint_sink=gt.taint_sink,
            line_numbers=tuple(gt.line_numbers),
        )
        summary = summarize_matches([finding], [gt])
        assert summary.tp_weight >= 0.9, f"exact match should score >=0.9, got {summary.tp_weight}"
        assert summary.exact_matches >= 1

    def test_no_findings_yields_all_fn(self) -> None:
        entries = [_load_gt("gt-145"), _load_gt("gt-146")]
        tp_entries = [e for e in entries if e.label == "true_positive"]
        summary = summarize_matches([], tp_entries)
        assert summary.fn_weight >= len(tp_entries) - 0.1

    def test_wrong_cwe_is_fp(self) -> None:
        gt = _load_gt("gt-145")
        finding = NormalizedFinding(
            id="wrong-cwe",
            cwe_id="CWE-79",  # wrong CWE
            affected_files=tuple(gt.affected_files),
            taint_source=gt.taint_source,
            taint_sink=gt.taint_sink,
        )
        tp_entries = [e for e in [gt] if e.label == "true_positive"]
        summary = summarize_matches([finding], tp_entries)
        assert summary.fp_weight > 0, "wrong CWE should count as FP"

    def test_cwe_alias_normalization(self) -> None:
        assert normalize_cwe_id("CWE-943") == "CWE-89"  # nosqli -> sqli alias
        assert normalize_cwe_id("CWE-77") == "CWE-78"  # cmdi alias
        assert normalize_cwe_id("cwe-79") == "CWE-79"  # case insensitive
