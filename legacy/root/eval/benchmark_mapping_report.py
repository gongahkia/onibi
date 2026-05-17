from __future__ import annotations

import argparse
import json
from collections import Counter
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import yaml

_ALLOWED_BENCHMARKS = {"owasp_benchmark", "nist_juliet", "internal_seed"}
_ALLOWED_STATUSES = {"planned", "mapped", "deferred"}


@dataclass(frozen=True, slots=True)
class BenchmarkMappingEntry:
    benchmark: str
    benchmark_case_id: str
    cwe_id: str
    language: str
    framework: str
    coverage_status: str
    mapped_ground_truth_ids: tuple[str, ...]
    notes: str

    @classmethod
    def from_payload(cls, payload: dict[str, Any], *, index: int) -> BenchmarkMappingEntry:
        benchmark = str(payload.get("benchmark") or "").strip()
        if benchmark not in _ALLOWED_BENCHMARKS:
            allowed = ", ".join(sorted(_ALLOWED_BENCHMARKS))
            raise ValueError(
                f"entry[{index}] benchmark '{benchmark}' is not supported. allowed: {allowed}"
            )

        status = str(payload.get("coverage_status") or "").strip()
        if status not in _ALLOWED_STATUSES:
            allowed = ", ".join(sorted(_ALLOWED_STATUSES))
            raise ValueError(
                f"entry[{index}] coverage_status '{status}' is not supported. allowed: {allowed}"
            )

        mapped_ids_raw = payload.get("mapped_ground_truth_ids") or []
        if not isinstance(mapped_ids_raw, list):
            raise ValueError(f"entry[{index}] mapped_ground_truth_ids must be a list")
        mapped_ids = tuple(str(item).strip() for item in mapped_ids_raw if str(item).strip())

        return cls(
            benchmark=benchmark,
            benchmark_case_id=str(payload.get("benchmark_case_id") or "").strip(),
            cwe_id=str(payload.get("cwe_id") or "").strip(),
            language=str(payload.get("language") or "").strip(),
            framework=str(payload.get("framework") or "").strip(),
            coverage_status=status,
            mapped_ground_truth_ids=mapped_ids,
            notes=str(payload.get("notes") or "").strip(),
        )


@dataclass(frozen=True, slots=True)
class BenchmarkMappingReport:
    matrix_path: str
    total_entries: int
    by_status: dict[str, int]
    by_benchmark: dict[str, int]
    by_cwe: dict[str, int]
    by_language: dict[str, int]
    mapped_ground_truth_ids: tuple[str, ...]

    def to_dict(self) -> dict[str, Any]:
        return {
            "matrix_path": self.matrix_path,
            "total_entries": self.total_entries,
            "by_status": self.by_status,
            "by_benchmark": self.by_benchmark,
            "by_cwe": self.by_cwe,
            "by_language": self.by_language,
            "mapped_ground_truth_ids": list(self.mapped_ground_truth_ids),
            "mapped_ground_truth_id_count": len(self.mapped_ground_truth_ids),
        }


def load_mapping_entries(matrix_path: Path) -> list[BenchmarkMappingEntry]:
    payload = yaml.safe_load(matrix_path.read_text(encoding="utf-8"))
    if not isinstance(payload, dict):
        raise ValueError(f"invalid mapping matrix in {matrix_path}")
    entries_payload = payload.get("entries")
    if not isinstance(entries_payload, list):
        raise ValueError(f"mapping matrix must include a list field 'entries': {matrix_path}")
    return [
        BenchmarkMappingEntry.from_payload(entry, index=index)
        for index, entry in enumerate(entries_payload)
        if isinstance(entry, dict)
    ]


def build_report(entries: list[BenchmarkMappingEntry], *, matrix_path: Path) -> BenchmarkMappingReport:
    by_status = Counter(entry.coverage_status for entry in entries)
    by_benchmark = Counter(entry.benchmark for entry in entries)
    by_cwe = Counter(entry.cwe_id for entry in entries)
    by_language = Counter(entry.language for entry in entries)
    mapped_ids = sorted(
        {
            item
            for entry in entries
            if entry.coverage_status == "mapped"
            for item in entry.mapped_ground_truth_ids
        }
    )
    return BenchmarkMappingReport(
        matrix_path=str(matrix_path),
        total_entries=len(entries),
        by_status=dict(sorted(by_status.items())),
        by_benchmark=dict(sorted(by_benchmark.items())),
        by_cwe=dict(sorted(by_cwe.items())),
        by_language=dict(sorted(by_language.items())),
        mapped_ground_truth_ids=tuple(mapped_ids),
    )


def render_report(report: BenchmarkMappingReport) -> str:
    lines = [
        "Benchmark Mapping Report",
        f"- Matrix: {report.matrix_path}",
        f"- Total entries: {report.total_entries}",
        f"- Mapped ground-truth IDs: {len(report.mapped_ground_truth_ids)}",
        "",
        "Coverage status",
    ]
    for status, count in report.by_status.items():
        lines.append(f"- {status}: {count}")

    lines.append("")
    lines.append("Benchmarks")
    for benchmark, count in report.by_benchmark.items():
        lines.append(f"- {benchmark}: {count}")

    lines.append("")
    lines.append("Top CWE buckets")
    for cwe_id, count in sorted(report.by_cwe.items(), key=lambda item: item[1], reverse=True):
        lines.append(f"- {cwe_id}: {count}")

    return "\n".join(lines)


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Summarize external benchmark mapping coverage.")
    parser.add_argument(
        "--matrix",
        type=Path,
        default=Path("eval/benchmarks/mapping_matrix.yaml"),
        help="Benchmark mapping matrix YAML path.",
    )
    parser.add_argument(
        "--require-mapped-entries",
        type=int,
        help="Fail if mapped entry count is below this threshold.",
    )
    parser.add_argument(
        "--require-mapped-ground-truth-ids",
        type=int,
        help="Fail if unique mapped ground-truth IDs are below this threshold.",
    )
    parser.add_argument("--json", action="store_true", help="Emit JSON output.")
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    if args.require_mapped_entries is not None and args.require_mapped_entries < 0:
        raise ValueError("--require-mapped-entries must be >= 0")
    if (
        args.require_mapped_ground_truth_ids is not None
        and args.require_mapped_ground_truth_ids < 0
    ):
        raise ValueError("--require-mapped-ground-truth-ids must be >= 0")

    entries = load_mapping_entries(args.matrix)
    report = build_report(entries, matrix_path=args.matrix)

    if args.json:
        print(json.dumps(report.to_dict(), indent=2))
    else:
        print(render_report(report))

    mapped_entries = report.by_status.get("mapped", 0)
    if (
        args.require_mapped_entries is not None
        and mapped_entries < args.require_mapped_entries
    ):
        print(
            "benchmark mapping gate failed: "
            f"mapped entries {mapped_entries} < {args.require_mapped_entries}"
        )
        return 1

    if (
        args.require_mapped_ground_truth_ids is not None
        and len(report.mapped_ground_truth_ids) < args.require_mapped_ground_truth_ids
    ):
        print(
            "benchmark mapping gate failed: "
            f"mapped ground-truth IDs {len(report.mapped_ground_truth_ids)} < "
            f"{args.require_mapped_ground_truth_ids}"
        )
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
