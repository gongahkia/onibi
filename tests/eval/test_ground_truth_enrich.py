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

from eval import ground_truth_enrich  # noqa: E402
from eval.ground_truth.schema import Complexity, GroundTruthEntry, Label  # noqa: E402


def _entry(
    *,
    entry_id: str,
    source_project: str,
    affected_files: list[str],
    taint_path: list[str],
    language: str | None = None,
    framework: str | None = None,
    taint_step_count: int | None = None,
) -> GroundTruthEntry:
    return GroundTruthEntry(
        id=entry_id,
        source_project=source_project,
        commit_hash="deadbeef",
        cwe_id="CWE-89",
        cwe_name="SQL Injection",
        label=Label.TRUE_POSITIVE,
        affected_files=affected_files,
        line_numbers=[5],
        taint_source="req.query.id",
        taint_sink="db.query()",
        taint_path=taint_path,
        complexity=Complexity.SIMPLE,
        exploitable=True,
        reference_exploit=None,
        reference_fix_commit=None,
        notes="fixture",
        discovery_method="synthetic",
        language=language,
        framework=framework,
        taint_step_count=taint_step_count,
    )


def _write_entries(directory: Path, entries: list[GroundTruthEntry]) -> None:
    directory.mkdir(parents=True, exist_ok=True)
    for entry in entries:
        payload = entry.model_dump(mode="json")
        (directory / f"{entry.id}.yaml").write_text(
            yaml.safe_dump(payload, sort_keys=False),
            encoding="utf-8",
        )


def test_enrich_ground_truth_reports_unresolved_entries(tmp_path: Path, capsys) -> None:
    gt_dir = tmp_path / "ground_truth"
    _write_entries(
        gt_dir,
        [
            _entry(
                entry_id="gt-001",
                source_project="owasp-nodegoat",
                affected_files=["app/routes/allocations.js"],
                taint_path=["req.query.id", "db.query(sql)"],
            ),
            _entry(
                entry_id="gt-002",
                source_project="unknown-project",
                affected_files=["frontend/app.tsx", "backend/app.py"],
                taint_path=["input", "sink"],
            ),
        ],
    )

    exit_code = ground_truth_enrich.main(
        [
            "--gt-dir",
            str(gt_dir),
            "--json",
        ]
    )
    payload = json.loads(capsys.readouterr().out)

    assert exit_code == 0
    assert payload["updated_entries"] == 2
    assert payload["updated_fields"] == 8
    assert payload["unresolved"]["language"]["count"] == 1
    assert payload["unresolved"]["framework"]["count"] == 1
    assert "gt-002" in payload["unresolved"]["language"]["entry_ids"]


def test_enrich_ground_truth_write_updates_yaml(tmp_path: Path, capsys) -> None:
    gt_dir = tmp_path / "ground_truth"
    _write_entries(
        gt_dir,
        [
            _entry(
                entry_id="gt-003",
                source_project="synthetic",
                affected_files=["eval/synthetic/sqli-pg-raw.ts"],
                taint_path=["req.query.id", "db.query(sql)", "sink"],
            )
        ],
    )

    exit_code = ground_truth_enrich.main(
        [
            "--gt-dir",
            str(gt_dir),
            "--write",
            "--json",
        ]
    )
    _ = capsys.readouterr()

    assert exit_code == 0
    payload = yaml.safe_load((gt_dir / "gt-003.yaml").read_text(encoding="utf-8"))
    assert payload["language"] == "typescript"
    assert payload["framework"] == "general"
    assert payload["taint_step_count"] == 3
    assert payload["taint_field_path"] == "query.id"
    assert payload["field_sensitive_label"] == "true_positive"


def test_enrich_ground_truth_fail_on_unresolved(tmp_path: Path, capsys) -> None:
    gt_dir = tmp_path / "ground_truth"
    _write_entries(
        gt_dir,
        [
            _entry(
                entry_id="gt-004",
                source_project="unknown-project",
                affected_files=["frontend/app.tsx", "backend/app.py"],
                taint_path=["input", "sink"],
            )
        ],
    )

    exit_code = ground_truth_enrich.main(
        [
            "--gt-dir",
            str(gt_dir),
            "--fail-on-unresolved",
            "--json",
        ]
    )
    payload = json.loads(capsys.readouterr().out)

    assert exit_code == 1
    assert payload["unresolved"]["language"]["count"] == 1


def test_enrich_ground_truth_fail_on_updates(tmp_path: Path, capsys) -> None:
    gt_dir = tmp_path / "ground_truth"
    _write_entries(
        gt_dir,
        [
            _entry(
                entry_id="gt-005",
                source_project="synthetic",
                affected_files=["eval/synthetic/sqli-pg-raw.ts"],
                taint_path=["req.query.id", "db.query(sql)", "sink"],
            )
        ],
    )

    exit_code = ground_truth_enrich.main(
        [
            "--gt-dir",
            str(gt_dir),
            "--fail-on-updates",
            "--json",
        ]
    )
    payload = json.loads(capsys.readouterr().out)

    assert exit_code == 2
    assert payload["updated_fields"] > 0


def test_enrich_ground_truth_taint_candidates_only_reduces_unresolved(
    tmp_path: Path,
    capsys,
) -> None:
    gt_dir = tmp_path / "ground_truth"
    _write_entries(
        gt_dir,
        [
            _entry(
                entry_id="gt-006",
                source_project="synthetic",
                affected_files=["eval/synthetic/sqli-pg-raw.ts"],
                taint_path=["req.query.id", "db.query(sql)"],
            ),
            _entry(
                entry_id="gt-007",
                source_project="synthetic",
                affected_files=["eval/synthetic/sqli-pg-raw.ts"],
                taint_path=["input", "db.query(sql)"],
            ),
        ],
    )
    payload = yaml.safe_load((gt_dir / "gt-007.yaml").read_text(encoding="utf-8"))
    payload["taint_source"] = "attacker-controlled SQL fragment"
    (gt_dir / "gt-007.yaml").write_text(
        yaml.safe_dump(payload, sort_keys=False),
        encoding="utf-8",
    )

    exit_code = ground_truth_enrich.main(
        [
            "--gt-dir",
            str(gt_dir),
            "--field",
            "taint_field_path",
            "--taint-field-candidates-only",
            "--json",
        ]
    )
    payload = json.loads(capsys.readouterr().out)

    assert exit_code == 0
    assert payload["updated_entries"] == 1
    assert payload["unresolved"] == {}


def test_enrich_ground_truth_field_sensitive_label_requires_concrete_field_path(
    tmp_path: Path,
    capsys,
) -> None:
    gt_dir = tmp_path / "ground_truth"
    _write_entries(
        gt_dir,
        [
            _entry(
                entry_id="gt-008",
                source_project="synthetic",
                affected_files=["eval/synthetic/sqli-pg-raw.ts"],
                taint_path=["req.body", "db.query(sql)"],
            )
        ],
    )
    payload = yaml.safe_load((gt_dir / "gt-008.yaml").read_text(encoding="utf-8"))
    payload["taint_source"] = "req.body"
    (gt_dir / "gt-008.yaml").write_text(
        yaml.safe_dump(payload, sort_keys=False),
        encoding="utf-8",
    )

    exit_code = ground_truth_enrich.main(
        [
            "--gt-dir",
            str(gt_dir),
            "--field",
            "field_sensitive_label",
            "--json",
        ]
    )
    payload = json.loads(capsys.readouterr().out)

    assert exit_code == 0
    assert payload["updated_entries"] == 0
    assert payload["unresolved"]["field_sensitive_label"]["count"] == 1
