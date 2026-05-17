from __future__ import annotations

import json
import sys
from pathlib import Path

import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))
if str(REPO_ROOT / "eval") not in sys.path:
    sys.path.insert(0, str(REPO_ROOT / "eval"))

from eval import ground_truth_audit  # noqa: E402
from eval.ground_truth.schema import Complexity, GroundTruthEntry, Label  # noqa: E402


def _entry(
    *,
    entry_id: str,
    source_project: str,
    framework: str | None,
) -> GroundTruthEntry:
    return GroundTruthEntry(
        id=entry_id,
        source_project=source_project,
        commit_hash="deadbeef",
        cwe_id="CWE-89",
        cwe_name="SQL Injection",
        label=Label.TRUE_POSITIVE,
        affected_files=["eval/synthetic/sqli-pg-raw.ts"],
        line_numbers=[5],
        taint_source="req.query.id",
        taint_sink="db.query()",
        taint_path=["req.query.id", "db.query(sql)"],
        complexity=Complexity.SIMPLE,
        exploitable=True,
        reference_exploit=None,
        reference_fix_commit=None,
        notes="fixture",
        discovery_method="synthetic",
        language="typescript",
        framework=framework,
    )


def _write_entries(directory: Path, entries: list[GroundTruthEntry]) -> None:
    directory.mkdir(parents=True, exist_ok=True)
    for entry in entries:
        (directory / f"{entry.id}.yaml").write_text(
            yaml.safe_dump(entry.model_dump(mode="json"), sort_keys=False),
            encoding="utf-8",
        )


def test_ground_truth_audit_fails_when_required_field_is_missing(
    tmp_path: Path,
    capsys,
) -> None:
    gt_dir = tmp_path / "ground_truth"
    _write_entries(
        gt_dir,
        [
            _entry(entry_id="gt-001", source_project="legacy", framework=None),
            _entry(entry_id="gt-002", source_project="legacy", framework="express"),
        ],
    )

    exit_code = ground_truth_audit.main(
        [
            "--gt-dir",
            str(gt_dir),
            "--required-field",
            "framework",
            "--fail-on-missing",
            "--json",
        ]
    )
    output = capsys.readouterr().out
    payload = json.loads(output)

    assert exit_code == 1
    assert payload["has_required_missing"] is True
    assert payload["field_coverage"]["framework"]["missing"] == 1
    assert "gt-001" in payload["field_coverage"]["framework"]["missing_ids"]


def test_ground_truth_audit_filter_can_scope_required_field_enforcement(
    tmp_path: Path,
    capsys,
) -> None:
    gt_dir = tmp_path / "ground_truth"
    _write_entries(
        gt_dir,
        [
            _entry(entry_id="gt-001", source_project="legacy", framework=None),
            _entry(entry_id="gt-002", source_project="phase41", framework="express"),
        ],
    )

    exit_code = ground_truth_audit.main(
        [
            "--gt-dir",
            str(gt_dir),
            "--required-field",
            "framework",
            "--filter",
            "source_project=phase41",
            "--fail-on-missing",
            "--json",
        ]
    )
    output = capsys.readouterr().out
    payload = json.loads(output)

    assert exit_code == 0
    assert payload["audited_entries"] == 1
    assert payload["missing_required_count"] == 0
    assert payload["has_required_missing"] is False
