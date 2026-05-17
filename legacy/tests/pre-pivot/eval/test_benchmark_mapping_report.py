from __future__ import annotations

import json
import sys
from pathlib import Path

import pytest
import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))
if str(REPO_ROOT / "eval") not in sys.path:
    sys.path.insert(0, str(REPO_ROOT / "eval"))

from eval import benchmark_mapping_report  # noqa: E402


def _write_matrix(path: Path, entries: list[dict[str, object]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        yaml.safe_dump({"version": 1, "entries": entries}, sort_keys=False),
        encoding="utf-8",
    )


def test_benchmark_mapping_report_summarizes_matrix(tmp_path: Path, capsys) -> None:
    matrix = tmp_path / "mapping.yaml"
    _write_matrix(
        matrix,
        [
            {
                "benchmark": "owasp_benchmark",
                "benchmark_case_id": "OWASP-BENCHMARK:CWE-89:*",
                "cwe_id": "CWE-89",
                "language": "java",
                "framework": "servlet",
                "coverage_status": "planned",
                "mapped_ground_truth_ids": [],
                "notes": "planned",
            },
            {
                "benchmark": "internal_seed",
                "benchmark_case_id": "WAVE1:CWE352:gorilla",
                "cwe_id": "CWE-352",
                "language": "go",
                "framework": "gorilla",
                "coverage_status": "mapped",
                "mapped_ground_truth_ids": ["gt-508", "gt-509"],
                "notes": "mapped",
            },
        ],
    )

    exit_code = benchmark_mapping_report.main(
        [
            "--matrix",
            str(matrix),
            "--json",
        ]
    )
    payload = json.loads(capsys.readouterr().out)

    assert exit_code == 0
    assert payload["total_entries"] == 2
    assert payload["by_status"]["mapped"] == 1
    assert payload["by_status"]["planned"] == 1
    assert payload["mapped_ground_truth_id_count"] == 2


def test_benchmark_mapping_report_enforces_thresholds(tmp_path: Path, capsys) -> None:
    matrix = tmp_path / "mapping.yaml"
    _write_matrix(
        matrix,
        [
            {
                "benchmark": "internal_seed",
                "benchmark_case_id": "WAVE1:CWE352:gorilla",
                "cwe_id": "CWE-352",
                "language": "go",
                "framework": "gorilla",
                "coverage_status": "mapped",
                "mapped_ground_truth_ids": ["gt-508"],
                "notes": "mapped",
            }
        ],
    )

    exit_code = benchmark_mapping_report.main(
        [
            "--matrix",
            str(matrix),
            "--require-mapped-entries",
            "2",
            "--require-mapped-ground-truth-ids",
            "2",
        ]
    )
    output = capsys.readouterr().out

    assert exit_code == 1
    assert "benchmark mapping gate failed" in output


def test_benchmark_mapping_report_rejects_invalid_entry(tmp_path: Path) -> None:
    matrix = tmp_path / "mapping.yaml"
    _write_matrix(
        matrix,
        [
            {
                "benchmark": "unknown",
                "benchmark_case_id": "X",
                "cwe_id": "CWE-89",
                "language": "java",
                "framework": "servlet",
                "coverage_status": "planned",
                "mapped_ground_truth_ids": [],
                "notes": "invalid",
            }
        ],
    )

    with pytest.raises(ValueError, match="benchmark 'unknown' is not supported"):
        benchmark_mapping_report.main(["--matrix", str(matrix)])
