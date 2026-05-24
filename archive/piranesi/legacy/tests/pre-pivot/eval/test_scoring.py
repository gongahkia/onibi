from __future__ import annotations

import json
import sys
from pathlib import Path

import pytest
import yaml  # type: ignore[import-untyped]

REPO_ROOT = Path(__file__).resolve().parents[2]
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))
if str(REPO_ROOT / "eval") not in sys.path:
    sys.path.insert(0, str(REPO_ROOT / "eval"))

from eval import scoring  # noqa: E402
from eval.ground_truth.schema import Complexity, GroundTruthEntry, Label  # noqa: E402


def _ground_truth_entry(
    *,
    entry_id: str,
    label: Label = Label.TRUE_POSITIVE,
    cwe_id: str = "CWE-89",
    cwe_name: str = "SQL Injection",
    affected_files: list[str] | None = None,
    taint_source: str = "req.query.id",
    taint_sink: str = "db.query()",
    taint_step_count: int | None = None,
    taint_field_path: str | None = None,
) -> GroundTruthEntry:
    return GroundTruthEntry(
        id=entry_id,
        source_project="fixture-app",
        commit_hash="deadbeef",
        cwe_id=cwe_id,
        cwe_name=cwe_name,
        label=label,
        affected_files=affected_files or ["src/app.ts"],
        line_numbers=[12],
        taint_source=taint_source,
        taint_sink=taint_sink,
        taint_path=[],
        complexity=Complexity.SIMPLE,
        exploitable=label == Label.TRUE_POSITIVE,
        reference_exploit=None,
        reference_fix_commit=None,
        notes="fixture",
        taint_step_count=taint_step_count,
        taint_field_path=taint_field_path,
    )


def _finding(
    *,
    finding_id: str,
    cwe_id: str = "CWE-89",
    affected_files: tuple[str, ...] = ("src/app.ts",),
    taint_source: str = "req.query.id",
    taint_sink: str = "db.query()",
    taint_step_count: int | None = None,
    taint_field_path: str | None = None,
) -> scoring.NormalizedFinding:
    return scoring.NormalizedFinding(
        id=finding_id,
        cwe_id=cwe_id,
        affected_files=affected_files,
        taint_source=taint_source,
        taint_sink=taint_sink,
        taint_step_count=taint_step_count,
        taint_field_path=taint_field_path,
    )


def test_match_weight_exact_partial_and_none() -> None:
    entry = _ground_truth_entry(entry_id="gt-001")

    exact = _finding(
        finding_id="f-1",
        taint_source="  REQ.QUERY.ID ",
        taint_sink="db.query( )",
    )
    partial = _finding(
        finding_id="f-2",
        taint_source="req.query.id",
        taint_sink="sequelize.query()",
    )
    no_match = _finding(
        finding_id="f-3",
        affected_files=("src/other.ts",),
        taint_source="req.query.id",
        taint_sink="db.query()",
    )

    assert scoring.match_weight(exact, entry) == 1.0
    assert scoring.match_weight(partial, entry) == 0.5
    assert scoring.match_weight(no_match, entry) == 0.0


def test_precision_recall_f1_with_partial_credit() -> None:
    ground_truth = [
        _ground_truth_entry(
            entry_id="gt-001", affected_files=["src/one.ts"], taint_sink="db.query()"
        ),
        _ground_truth_entry(
            entry_id="gt-002",
            affected_files=["src/two.ts"],
            taint_source="req.body.name",
            taint_sink="res.send()",
        ),
    ]
    predictions = [
        _finding(finding_id="f-1", affected_files=("src/one.ts",), taint_sink="db.query()"),
        _finding(
            finding_id="f-2",
            affected_files=("src/two.ts",),
            taint_source="req.body.name",
            taint_sink="document.write()",
        ),
        _finding(
            finding_id="f-3",
            affected_files=("src/three.ts",),
            taint_source="req.params.slug",
            taint_sink="fs.readFile()",
        ),
    ]

    summary = scoring.summarize_matches(predictions, ground_truth)
    precision, recall, f1 = scoring._compute_prf(
        summary.tp_weight, summary.fp_weight, summary.fn_weight
    )

    assert summary.tp_weight == pytest.approx(1.5)
    assert summary.fp_weight == pytest.approx(1.5)
    assert summary.fn_weight == pytest.approx(0.5)
    assert precision == pytest.approx(0.5)
    assert recall == pytest.approx(0.75)
    assert f1 == pytest.approx(0.6)


def test_match_weight_can_upgrade_partial_match_using_field_path() -> None:
    entry = _ground_truth_entry(
        entry_id="gt-003",
        taint_source="req.body",
        taint_sink="db.query()",
        taint_field_path="body.email",
    )
    finding = _finding(
        finding_id="f-4",
        taint_source="request_body",
        taint_sink="db.query()",
        taint_field_path="req.body.email",
    )
    assert scoring.match_weight(finding, entry) == 1.0


def test_match_weight_can_downgrade_exact_match_on_field_path_conflict() -> None:
    entry = _ground_truth_entry(
        entry_id="gt-004",
        taint_source="req.body",
        taint_sink="db.query()",
        taint_field_path="body.id",
    )
    finding = _finding(
        finding_id="f-5",
        taint_source="req.body",
        taint_sink="db.query()",
        taint_field_path="body.name",
    )
    assert scoring.match_weight(finding, entry) == 0.5


def test_match_weight_can_downgrade_exact_match_on_step_count_mismatch() -> None:
    entry = _ground_truth_entry(
        entry_id="gt-005",
        taint_source="req.body",
        taint_sink="db.query()",
        taint_step_count=5,
    )
    finding = _finding(
        finding_id="f-6",
        taint_source="req.body",
        taint_sink="db.query()",
        taint_step_count=2,
    )
    assert scoring.match_weight(finding, entry) == 0.5


def test_normalize_finding_extracts_field_path_and_step_count() -> None:
    finding = scoring.normalize_finding(
        {
            "id": "f-7",
            "cwe_id": "CWE-89",
            "affected_files": ["src/app.ts"],
            "source": {
                "source_type": "request_body",
                "parameter_name": "email",
                "location": {"file": "src/app.ts", "line": 12},
            },
            "sink": {"api_name": "db.query", "location": {"file": "src/app.ts", "line": 14}},
            "taint_path": [{"operation": "assignment"}, {"operation": "call_arg"}],
        }
    )
    assert finding is not None
    assert finding.taint_field_path == "body.email"
    assert finding.taint_step_count == 2


def test_edge_cases_for_metric_calculation() -> None:
    ground_truth = [_ground_truth_entry(entry_id="gt-001")]

    no_predictions = scoring.summarize_matches([], ground_truth)
    precision, recall, f1 = scoring._compute_prf(
        no_predictions.tp_weight,
        no_predictions.fp_weight,
        no_predictions.fn_weight,
    )
    assert precision is None
    assert recall == pytest.approx(0.0)
    assert f1 is None

    no_false_positives = scoring.summarize_matches([_finding(finding_id="f-1")], ground_truth)
    precision, recall, f1 = scoring._compute_prf(
        no_false_positives.tp_weight,
        no_false_positives.fp_weight,
        no_false_positives.fn_weight,
    )
    assert precision == pytest.approx(1.0)
    assert recall == pytest.approx(1.0)
    assert f1 == pytest.approx(1.0)

    empty_ground_truth = scoring.summarize_matches([_finding(finding_id="f-2")], [])
    precision, recall, f1 = scoring._compute_prf(
        empty_ground_truth.tp_weight,
        empty_ground_truth.fp_weight,
        empty_ground_truth.fn_weight,
    )
    assert precision == pytest.approx(0.0)
    assert recall is None
    assert f1 is None


def test_scoring_cli_writes_json_and_prints_table(
    tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    ground_truth_dir = tmp_path / "ground_truth"
    ground_truth_dir.mkdir()
    entries = [
        _ground_truth_entry(entry_id="gt-001", affected_files=["src/app.ts"]),
        _ground_truth_entry(
            entry_id="gt-fp-001",
            label=Label.FALSE_POSITIVE,
            affected_files=["src/safe.ts"],
            taint_source="req.query.userId",
            taint_sink="db.query() with parameterized placeholder",
        ),
    ]
    for entry in entries:
        (ground_truth_dir / f"{entry.id}.yaml").write_text(
            yaml.safe_dump(entry.model_dump(mode="json"), sort_keys=False),
            encoding="utf-8",
        )

    tp_finding = {
        "id": "f-1",
        "cwe_id": "CWE-89",
        "affected_files": ["src/app.ts"],
        "taint_source": "req.query.id",
        "taint_sink": "db.query()",
    }
    fp_finding = {
        "id": "f-2",
        "cwe_id": "CWE-89",
        "affected_files": ["src/safe.ts"],
        "taint_source": "req.query.userId",
        "taint_sink": "db.query() with parameterized placeholder",
    }
    pipeline_output = {
        "commit": "abc1234",
        "total_cost_usd": 1.5,
        "scan": {
            "sources": [
                {"file": "src/app.ts", "value": "req.query.id"},
                {"file": "src/safe.ts", "value": "req.query.userId"},
            ],
            "sinks": [
                {"file": "src/app.ts", "value": "db.query()"},
                {"file": "src/safe.ts", "value": "db.query() with parameterized placeholder"},
            ],
        },
        "detect": [tp_finding, fp_finding],
        "triage": [
            {"finding": tp_finding, "triage_verdict": "confirmed"},
            {"finding": fp_finding, "triage_verdict": "rejected"},
        ],
        "verify": [
            {
                "finding": {"finding": tp_finding, "triage_verdict": "confirmed"},
                "sandbox_result": {"confirmed": True},
            }
        ],
        "findings": [
            {
                "confirmed": {
                    "finding": {"finding": tp_finding, "triage_verdict": "confirmed"},
                    "sandbox_result": {"confirmed": True},
                }
            }
        ],
    }
    pipeline_output_path = tmp_path / "results.json"
    pipeline_output_path.write_text(json.dumps(pipeline_output), encoding="utf-8")
    output_path = tmp_path / "scores.json"

    exit_code = scoring.main(
        [
            "--pipeline-output",
            str(pipeline_output_path),
            "--ground-truth",
            str(ground_truth_dir),
            "--output",
            str(output_path),
        ]
    )

    captured = capsys.readouterr()
    payload = json.loads(output_path.read_text(encoding="utf-8"))

    assert exit_code == 0
    assert "Piranesi Evaluation Report" in captured.out
    assert "Overall Metrics:" in captured.out
    assert "Per-Stage Breakdown:" in captured.out
    assert "Cost:" in captured.out
    assert payload["overall"]["precision"] == pytest.approx(1.0)
    assert payload["overall"]["recall"] == pytest.approx(1.0)
    assert payload["false_positive_handling"]["leaked_through"] == 0
    assert payload["triage"]["fp_filtered"] == 1
    assert payload["verify"]["confirmation_rate"] == pytest.approx(1.0)
