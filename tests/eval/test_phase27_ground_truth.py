from __future__ import annotations

from pathlib import Path

import yaml

from eval.ground_truth.schema import GroundTruthEntry

ROOT = Path(__file__).resolve().parents[2]
GT_DIR = ROOT / "eval" / "ground_truth"


def _load(entry_id: str) -> GroundTruthEntry:
    payload = yaml.safe_load((GT_DIR / f"{entry_id}.yaml").read_text(encoding="utf-8"))
    return GroundTruthEntry.model_validate(payload)


def test_phase27_ground_truth_entries_exist_and_parse() -> None:
    tp_ids = [f"gt-{index:03d}" for index in range(145, 177)]
    fp_ids = [f"gt-fp-{index:03d}" for index in range(40, 51)]

    entries = [_load(entry_id) for entry_id in [*tp_ids, *fp_ids]]

    assert len(entries) == 43
    assert {entry.id for entry in entries} == set(tp_ids) | set(fp_ids)


def test_phase27_ground_truth_fixtures_exist() -> None:
    tp_ids = [f"gt-{index:03d}" for index in range(145, 177)]
    fp_ids = [f"gt-fp-{index:03d}" for index in range(40, 51)]

    for entry_id in [*tp_ids, *fp_ids]:
        entry = _load(entry_id)
        for file_name in entry.affected_files:
            assert (ROOT / file_name).is_file(), f"missing fixture for {entry_id}: {file_name}"
