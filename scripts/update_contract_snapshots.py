from __future__ import annotations

import json
from pathlib import Path

from piranesi.contracts import build_contract_snapshot

ROOT = Path(__file__).resolve().parents[1]
SNAPSHOT_PATH = ROOT / "tests" / "snapshots" / "contracts" / "cli_plugin_report_contract.json"


def main() -> int:
    SNAPSHOT_PATH.parent.mkdir(parents=True, exist_ok=True)
    snapshot = build_contract_snapshot()
    SNAPSHOT_PATH.write_text(json.dumps(snapshot, indent=2, sort_keys=True), encoding="utf-8")
    print(f"updated contract snapshot: {SNAPSHOT_PATH}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
