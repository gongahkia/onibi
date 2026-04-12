from __future__ import annotations

from collections import Counter
from pathlib import Path

import yaml
from eval.ground_truth.schema import GroundTruthEntry

ROOT = Path(__file__).resolve().parents[2]
GT_DIR = ROOT / "eval" / "ground_truth"


def _load(entry_id: str) -> GroundTruthEntry:
    payload = yaml.safe_load((GT_DIR / f"{entry_id}.yaml").read_text(encoding="utf-8"))
    return GroundTruthEntry.model_validate(payload)


def test_phase39_ground_truth_entries_exist_and_parse() -> None:
    entry_ids = [f"gt-{index:03d}" for index in range(482, 508)]
    entries = [_load(entry_id) for entry_id in entry_ids]

    assert len(entries) == 26
    assert {entry.id for entry in entries} == set(entry_ids)
    assert all(entry.discovery_method == "synthetic" for entry in entries)
    assert all(entry.language == "php" for entry in entries)

    frameworks = Counter(entry.framework for entry in entries)
    labels = Counter(entry.label for entry in entries)

    assert frameworks == {"php": 14, "laravel": 6, "wordpress": 4, "symfony": 2}
    assert labels == {"true_positive": 18, "false_positive": 8}


def test_phase39_ground_truth_fixtures_exist() -> None:
    entry_ids = [f"gt-{index:03d}" for index in range(482, 508)]
    for entry_id in entry_ids:
        entry = _load(entry_id)
        for file_name in entry.affected_files:
            assert (ROOT / file_name).is_file(), f"missing fixture for {entry_id}: {file_name}"
