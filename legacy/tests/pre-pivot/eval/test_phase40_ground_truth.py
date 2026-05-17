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


def _load_phase_entry_ids(source_project: str) -> list[str]:
    entry_ids: list[str] = []
    for path in sorted(GT_DIR.glob("gt-*.yaml")):
        payload = yaml.safe_load(path.read_text(encoding="utf-8"))
        if payload.get("source_project") == source_project:
            entry_ids.append(str(payload["id"]))
    return entry_ids


def test_phase40_ground_truth_entries_exist_and_parse() -> None:
    entry_ids = _load_phase_entry_ids("synthetic-phase40")
    entries = [_load(entry_id) for entry_id in entry_ids]

    assert len(entries) == 1
    assert entry_ids == ["gt-481"]
    assert {entry.id for entry in entries} == set(entry_ids)
    assert all(entry.discovery_method == "synthetic" for entry in entries)
    assert all(entry.language == "ruby" for entry in entries)

    label_counts = Counter(entry.label for entry in entries)
    framework_counts = Counter(entry.framework for entry in entries)

    assert label_counts == {"true_positive": 1}
    assert framework_counts == {"rails": 1}


def test_phase40_ground_truth_fixtures_exist() -> None:
    entry_ids = _load_phase_entry_ids("synthetic-phase40")
    for entry_id in entry_ids:
        entry = _load(entry_id)
        for file_name in entry.affected_files:
            assert (ROOT / file_name).is_file(), f"missing fixture for {entry_id}: {file_name}"
