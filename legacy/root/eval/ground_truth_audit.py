from __future__ import annotations

import argparse
import json
from collections import Counter
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import yaml

try:
    from eval.ground_truth.schema import GroundTruthEntry
except ImportError:  # pragma: no cover - supports `python eval/ground_truth_audit.py`
    from ground_truth.schema import GroundTruthEntry  # type: ignore[import-not-found,no-redef]

DEFAULT_AUDIT_FIELDS = (
    "discovery_method",
    "language",
    "framework",
    "taint_step_count",
    "taint_field_path",
    "field_sensitive_label",
    "cve_id",
    "ghsa_id",
    "fix_commit",
    "vulnerable_commit",
)
_DISTRIBUTION_FIELDS = (
    "label",
    "cwe_id",
    "language",
    "framework",
    "complexity",
    "discovery_method",
)


@dataclass(frozen=True, slots=True)
class FieldCoverage:
    present: int
    missing: int
    missing_ids: tuple[str, ...]


@dataclass(frozen=True, slots=True)
class GroundTruthAudit:
    gt_dir: str
    total_entries: int
    audited_entries: int
    audit_fields: tuple[str, ...]
    required_fields: tuple[str, ...]
    field_coverage: dict[str, FieldCoverage]
    distributions: dict[str, dict[str, int]]
    missing_required_count: int
    has_required_missing: bool

    def to_dict(self) -> dict[str, Any]:
        return {
            "gt_dir": self.gt_dir,
            "total_entries": self.total_entries,
            "audited_entries": self.audited_entries,
            "audit_fields": list(self.audit_fields),
            "required_fields": list(self.required_fields),
            "field_coverage": {
                field: {
                    "present": coverage.present,
                    "missing": coverage.missing,
                    "missing_ids": list(coverage.missing_ids),
                }
                for field, coverage in self.field_coverage.items()
            },
            "distributions": self.distributions,
            "missing_required_count": self.missing_required_count,
            "has_required_missing": self.has_required_missing,
        }


def _is_present(value: object) -> bool:
    if value is None:
        return False
    if isinstance(value, str):
        return bool(value.strip())
    if isinstance(value, list | tuple | set | dict):
        return bool(value)
    return True


def load_ground_truth_entries(gt_dir: Path) -> list[GroundTruthEntry]:
    entries: list[GroundTruthEntry] = []
    seen_ids: set[str] = set()
    for path in sorted(gt_dir.glob("*.yaml")):
        payload = yaml.safe_load(path.read_text(encoding="utf-8"))
        entry = GroundTruthEntry.model_validate(payload)
        if entry.id in seen_ids:
            raise ValueError(f"duplicate ground truth id: {entry.id}")
        seen_ids.add(entry.id)
        entries.append(entry)
    return entries


def build_filter_predicate(expressions: list[str]) -> tuple[tuple[str, str], ...]:
    pairs: list[tuple[str, str]] = []
    for expression in expressions:
        if "=" not in expression:
            raise ValueError(f"invalid filter expression: {expression}")
        key, value = expression.split("=", 1)
        pairs.append((key.strip(), value.strip()))
    return tuple(pairs)


def _entry_matches_filters(entry: GroundTruthEntry, filters: tuple[tuple[str, str], ...]) -> bool:
    if not filters:
        return True
    payload = entry.model_dump(mode="json")
    for key, expected in filters:
        current = payload.get(key)
        if current is None or str(current) != expected:
            return False
    return True


def audit_ground_truth(
    entries: list[GroundTruthEntry],
    *,
    gt_dir: Path,
    audit_fields: tuple[str, ...],
    required_fields: tuple[str, ...],
    filters: tuple[tuple[str, str], ...],
    show_missing_limit: int,
) -> GroundTruthAudit:
    filtered_entries = [entry for entry in entries if _entry_matches_filters(entry, filters)]
    payloads = [entry.model_dump(mode="json") for entry in filtered_entries]

    field_coverage: dict[str, FieldCoverage] = {}
    missing_required_count = 0
    for field in audit_fields:
        missing_ids = [
            entry.id
            for entry, payload in zip(filtered_entries, payloads)
            if not _is_present(payload.get(field))
        ]
        present = len(filtered_entries) - len(missing_ids)
        missing = len(missing_ids)
        field_coverage[field] = FieldCoverage(
            present=present,
            missing=missing,
            missing_ids=tuple(missing_ids[:show_missing_limit]),
        )
        if field in required_fields:
            missing_required_count += missing

    distributions: dict[str, dict[str, int]] = {}
    for field in _DISTRIBUTION_FIELDS:
        counter = Counter(
            str(payload.get(field) if _is_present(payload.get(field)) else "unknown")
            for payload in payloads
        )
        distributions[field] = dict(counter)

    return GroundTruthAudit(
        gt_dir=str(gt_dir),
        total_entries=len(entries),
        audited_entries=len(filtered_entries),
        audit_fields=audit_fields,
        required_fields=required_fields,
        field_coverage=field_coverage,
        distributions=distributions,
        missing_required_count=missing_required_count,
        has_required_missing=missing_required_count > 0,
    )


def render_audit(audit: GroundTruthAudit) -> str:
    lines = [
        "Ground Truth Audit",
        f"- Directory: {audit.gt_dir}",
        f"- Entries considered: {audit.audited_entries}/{audit.total_entries}",
    ]
    if audit.required_fields:
        lines.append("- Required fields: " + ", ".join(audit.required_fields))
        lines.append(f"- Missing required values: {audit.missing_required_count}")
    else:
        lines.append("- Required fields: none")

    lines.append("")
    lines.append("Field coverage")
    for field in audit.audit_fields:
        coverage = audit.field_coverage[field]
        lines.append(f"- {field}: present={coverage.present} missing={coverage.missing}")
        if coverage.missing_ids:
            lines.append(f"  sample_missing_ids={', '.join(coverage.missing_ids)}")

    lines.append("")
    lines.append("Distributions")
    for field in _DISTRIBUTION_FIELDS:
        lines.append(f"- {field}:")
        for value, count in sorted(
            audit.distributions.get(field, {}).items(),
            key=lambda item: item[1],
            reverse=True,
        ):
            lines.append(f"  {value}: {count}")
    return "\n".join(lines)


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Audit eval/ground_truth metadata coverage.")
    parser.add_argument(
        "--gt-dir",
        type=Path,
        default=Path("eval/ground_truth"),
        help="Ground-truth directory.",
    )
    parser.add_argument(
        "--field",
        action="append",
        default=[],
        help="Field to audit. Repeatable. Defaults to a standard metadata set.",
    )
    parser.add_argument(
        "--required-field",
        action="append",
        default=[],
        help="Field that must be present. Repeatable.",
    )
    parser.add_argument(
        "--filter",
        action="append",
        default=[],
        help="Filter entries by key=value before auditing.",
    )
    parser.add_argument(
        "--show-missing-limit",
        type=int,
        default=10,
        help="Maximum missing entry IDs to include per field in output.",
    )
    parser.add_argument("--json", action="store_true", help="Emit JSON output.")
    parser.add_argument(
        "--fail-on-missing",
        action="store_true",
        help="Return exit code 1 when any required field is missing.",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    fields = tuple(args.field) if args.field else DEFAULT_AUDIT_FIELDS
    required = tuple(dict.fromkeys(args.required_field))
    merged_fields = tuple(dict.fromkeys((*fields, *required)))
    filters = build_filter_predicate(args.filter)
    entries = load_ground_truth_entries(args.gt_dir)
    audit = audit_ground_truth(
        entries,
        gt_dir=args.gt_dir,
        audit_fields=merged_fields,
        required_fields=required,
        filters=filters,
        show_missing_limit=max(args.show_missing_limit, 0),
    )

    if args.json:
        print(json.dumps(audit.to_dict(), indent=2))
    else:
        print(render_audit(audit))

    if args.fail_on_missing and audit.has_required_missing:
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
