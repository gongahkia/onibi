from __future__ import annotations

import json
import os
from collections.abc import Mapping
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

_AUDIT_LOG_FILE = "audit-log.jsonl"


def append_audit_event(
    *,
    output_dir: Path,
    event_type: str,
    stage: str | None = None,
    approved: bool | None = None,
    details: Mapping[str, Any] | None = None,
) -> Path:
    resolved_dir = output_dir.resolve(strict=False)
    resolved_dir.mkdir(parents=True, exist_ok=True)
    log_path = resolved_dir / _AUDIT_LOG_FILE

    payload: dict[str, Any] = {
        "timestamp": datetime.now(UTC).isoformat(),
        "event_type": event_type,
        "stage": stage,
        "approved": approved,
        "actor": os.getenv("USER") or os.getenv("USERNAME") or "unknown",
        "details": _normalize_value(dict(details or {})),
    }

    with log_path.open("a", encoding="utf-8") as handle:
        handle.write(json.dumps(payload, sort_keys=True))
        handle.write("\n")

    return log_path


def _normalize_value(value: Any) -> Any:
    if value is None:
        return None
    if isinstance(value, str | int | float | bool):
        return value
    if isinstance(value, Path):
        return str(value)
    if isinstance(value, Mapping):
        return {str(key): _normalize_value(item) for key, item in value.items()}
    if isinstance(value, list | tuple | set):
        return [_normalize_value(item) for item in value]
    return str(value)


__all__ = ["append_audit_event"]
