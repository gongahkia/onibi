from __future__ import annotations

import json
import re
import sys
from datetime import date
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
_REGISTRY_RELATIVE_PATH = Path("docs") / "known-limitations.json"
_REQUIRED_FIELDS = (
    "id",
    "title",
    "affected_feature",
    "severity",
    "impact",
    "workaround",
    "status",
    "introduced_version",
    "last_reviewed",
)
_ALLOWED_SEVERITIES = {"low", "medium", "high", "critical"}
_ALLOWED_STATUSES = {"open", "monitoring", "resolved"}
_LIMITATION_ID_PATTERN = re.compile(r"^KL-\d{3}$")
_SEMVER_PATTERN = re.compile(r"^\d+\.\d+\.\d+$")


def collect_known_limitations_errors(root: Path = ROOT) -> list[str]:
    path = root / _REGISTRY_RELATIVE_PATH
    if not path.exists():
        return [f"{_REGISTRY_RELATIVE_PATH} is missing"]

    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        return [f"{_REGISTRY_RELATIVE_PATH} contains invalid JSON: {exc}"]

    if not isinstance(payload, dict):
        return [f"{_REGISTRY_RELATIVE_PATH} root must be a JSON object"]

    schema_version = payload.get("schema_version")
    if not isinstance(schema_version, str) or not schema_version.strip():
        return ["known limitations registry must define a non-empty schema_version"]

    limitations = payload.get("limitations")
    if not isinstance(limitations, list) or not limitations:
        return ["known limitations registry must contain a non-empty limitations array"]

    errors: list[str] = []
    seen_ids: set[str] = set()
    for index, entry in enumerate(limitations):
        errors.extend(_validate_entry(entry, index=index, seen_ids=seen_ids))
    return errors


def main() -> int:
    errors = collect_known_limitations_errors(ROOT)
    if not errors:
        print("known limitations registry checks passed")
        return 0
    print("known limitations registry checks failed:", file=sys.stderr)
    for error in errors:
        print(f"- {error}", file=sys.stderr)
    return 1


def _validate_entry(entry: Any, *, index: int, seen_ids: set[str]) -> list[str]:
    entry_label = f"limitations[{index}]"
    if not isinstance(entry, dict):
        return [f"{entry_label} must be an object"]

    errors: list[str] = []
    for field_name in _REQUIRED_FIELDS:
        value = entry.get(field_name)
        if not isinstance(value, str) or not value.strip():
            errors.append(f"{entry_label}.{field_name} must be a non-empty string")

    limitation_id = entry.get("id")
    if isinstance(limitation_id, str):
        if limitation_id in seen_ids:
            errors.append(f"{entry_label}.id duplicates {limitation_id}")
        else:
            seen_ids.add(limitation_id)
        if not _LIMITATION_ID_PATTERN.match(limitation_id):
            errors.append(f"{entry_label}.id must match KL-###")

    severity = entry.get("severity")
    if isinstance(severity, str) and severity not in _ALLOWED_SEVERITIES:
        errors.append(
            f"{entry_label}.severity must be one of {sorted(_ALLOWED_SEVERITIES)}"
        )

    status = entry.get("status")
    if isinstance(status, str) and status not in _ALLOWED_STATUSES:
        errors.append(f"{entry_label}.status must be one of {sorted(_ALLOWED_STATUSES)}")

    introduced_version = entry.get("introduced_version")
    if isinstance(introduced_version, str) and not _SEMVER_PATTERN.match(introduced_version):
        errors.append(f"{entry_label}.introduced_version must use semver (x.y.z)")

    last_reviewed = entry.get("last_reviewed")
    if isinstance(last_reviewed, str):
        try:
            date.fromisoformat(last_reviewed)
        except ValueError:
            errors.append(f"{entry_label}.last_reviewed must use YYYY-MM-DD")

    docs_refs = entry.get("docs_refs")
    if docs_refs is not None and (
        not isinstance(docs_refs, list)
        or not all(isinstance(item, str) and item.strip() for item in docs_refs)
    ):
        errors.append(f"{entry_label}.docs_refs must be a list of non-empty strings")

    return errors


if __name__ == "__main__":
    raise SystemExit(main())
