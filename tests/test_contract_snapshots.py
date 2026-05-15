from __future__ import annotations

import json
from pathlib import Path

from piranesi.contracts import build_contract_snapshot

_SNAPSHOT_PATH = (
    Path(__file__).resolve().parent / "snapshots" / "contracts" / "cli_plugin_report_contract.json"
)


def test_contract_snapshot_is_current() -> None:
    assert _SNAPSHOT_PATH.exists(), (
        "missing contract snapshot; run "
        "`PYTHONPATH=src python3 scripts/update_contract_snapshots.py`"
    )
    expected = json.loads(_SNAPSHOT_PATH.read_text(encoding="utf-8"))
    actual = build_contract_snapshot()
    assert actual == expected, (
        "contract snapshot drift detected; run "
        "`PYTHONPATH=src python3 scripts/update_contract_snapshots.py` and review changes"
    )
