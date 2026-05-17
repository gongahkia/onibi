from __future__ import annotations

import json
import subprocess
import sys
import types
from pathlib import Path
from typing import Any

import pytest
import yaml  # type: ignore[import-untyped]

REPO_ROOT = Path(__file__).resolve().parents[2]
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))
if str(REPO_ROOT / "eval") not in sys.path:
    sys.path.insert(0, str(REPO_ROOT / "eval"))

from eval import scoring  # noqa: E402
from eval.baselines.llm_only_runner import run_llm_only_baseline  # noqa: E402
from eval.baselines.opengrep_normalizer import normalize_opengrep_output  # noqa: E402
from eval.baselines.opengrep_runner import run_opengrep_scan  # noqa: E402
from eval.ground_truth.schema import Complexity, GroundTruthEntry, Label  # noqa: E402

from piranesi.config import ModelsConfig, PiranesiConfig, TraceConfig  # noqa: E402


def _ground_truth_entry(
    *,
    entry_id: str,
    cwe_id: str,
    cwe_name: str,
    affected_file: str,
    line_numbers: list[int],
    taint_source: str = "req.query.id",
    taint_sink: str = "db.query()",
    label: Label = Label.TRUE_POSITIVE,
) -> GroundTruthEntry:
    return GroundTruthEntry(
        id=entry_id,
        source_project="fixture-app",
        commit_hash="deadbeef",
        cwe_id=cwe_id,
        cwe_name=cwe_name,
        label=label,
        affected_files=[affected_file],
        line_numbers=line_numbers,
        taint_source=taint_source,
        taint_sink=taint_sink,
        taint_path=[],
        complexity=Complexity.SIMPLE,
        exploitable=label == Label.TRUE_POSITIVE,
        reference_exploit=None,
        reference_fix_commit=None,
        notes="fixture",
    )


def _write_ground_truth(ground_truth_dir: Path, entries: list[GroundTruthEntry]) -> None:
    ground_truth_dir.mkdir(parents=True, exist_ok=True)
    for entry in entries:
        path = ground_truth_dir / f"{entry.id}.yaml"
        path.write_text(
            yaml.safe_dump(entry.model_dump(mode="json"), sort_keys=False),
            encoding="utf-8",
        )


def test_opengrep_normalizer_extracts_cwe_lines_and_trace(fixtures_dir: Path) -> None:
    raw_payload = json.loads(
        (fixtures_dir / "eval" / "opengrep_raw_output.json").read_text(encoding="utf-8")
    )

    normalized = normalize_opengrep_output(
        raw_payload,
        project_root=Path("/tmp/project"),  # noqa: S108
        tool_name="opengrep",
    )

    assert normalized["tool"] == "opengrep"
    assert normalized["pipeline_findings"] == 2

    xss_finding = normalized["findings"][0]
    traversal_finding = normalized["findings"][1]

    assert xss_finding["cwe_id"] == "CWE-79"
    assert xss_finding["affected_files"] == ["src/views/render.ts"]
    assert xss_finding["line_numbers"] == [18]
    assert xss_finding["taint_source"] == "req.query.q"
    assert xss_finding["taint_sink"] == "res.send(req.query.q)"
    assert len(xss_finding["taint_path"]) == 3

    assert traversal_finding["cwe_id"] == "CWE-22"
    assert traversal_finding["affected_files"] == ["src/fs/read.ts"]
    assert traversal_finding["line_numbers"] == [33]


def test_scoring_handles_line_based_baseline_findings(tmp_path: Path, fixtures_dir: Path) -> None:
    raw_payload = json.loads(
        (fixtures_dir / "eval" / "opengrep_raw_output.json").read_text(encoding="utf-8")
    )
    normalized = normalize_opengrep_output(
        raw_payload, project_root=tmp_path / "project", tool_name="opengrep"
    )

    normalized_output_path = tmp_path / "opengrep-normalized.json"
    normalized_output_path.write_text(json.dumps(normalized), encoding="utf-8")

    ground_truth_dir = tmp_path / "ground_truth"
    _write_ground_truth(
        ground_truth_dir,
        [
            _ground_truth_entry(
                entry_id="gt-001",
                cwe_id="CWE-79",
                cwe_name="Cross-Site Scripting",
                affected_file="src/views/render.ts",
                line_numbers=[18],
                taint_source="req.query.q",
                taint_sink="res.send(req.query.q)",
            ),
            _ground_truth_entry(
                entry_id="gt-002",
                cwe_id="CWE-22",
                cwe_name="Path Traversal",
                affected_file="src/fs/read.ts",
                line_numbers=[33],
                taint_source="req.query.path",
                taint_sink="fs.readFile()",
            ),
        ],
    )

    report = scoring.score_pipeline_output(normalized_output_path, ground_truth_dir)

    assert report.overall.precision == pytest.approx(1.0)
    assert report.overall.recall == pytest.approx(1.0)
    assert report.overall.f1 == pytest.approx(1.0)


def test_comparison_report_renders_side_by_side_table(tmp_path: Path, fixtures_dir: Path) -> None:
    llm_output_path = fixtures_dir / "eval" / "llm_only_normalized_output.json"
    opengrep_output_path = tmp_path / "opengrep-normalized.json"
    piranesi_output_path = tmp_path / "piranesi.json"

    opengrep_output = {
        "tool": "opengrep",
        "total_cost_usd": 0.0,
        "findings": [
            {
                "id": "og-001",
                "tool": "opengrep",
                "cwe_id": "CWE-89",
                "description": "User input reaches a SQL query.",
                "affected_files": ["src/app.ts"],
                "line_numbers": [12],
                "taint_source": "",
                "taint_sink": "",
            },
            {
                "id": "og-002",
                "tool": "opengrep",
                "cwe_id": "CWE-79",
                "description": "User input is written into HTML.",
                "affected_files": ["src/ui.ts"],
                "line_numbers": [27],
                "taint_source": "",
                "taint_sink": "",
            },
        ],
    }
    piranesi_output = {
        "total_cost_usd": 1.5,
        "findings": [
            {
                "id": "p-001",
                "cwe_id": "CWE-89",
                "affected_files": ["src/app.ts"],
                "taint_source": "req.query.id",
                "taint_sink": "db.query()",
            },
            {
                "id": "p-002",
                "cwe_id": "CWE-79",
                "affected_files": ["src/ui.ts"],
                "taint_source": "req.query.q",
                "taint_sink": "res.send()",
            },
        ],
    }

    opengrep_output_path.write_text(json.dumps(opengrep_output), encoding="utf-8")
    piranesi_output_path.write_text(json.dumps(piranesi_output), encoding="utf-8")

    ground_truth_dir = tmp_path / "ground_truth"
    _write_ground_truth(
        ground_truth_dir,
        [
            _ground_truth_entry(
                entry_id="gt-001",
                cwe_id="CWE-89",
                cwe_name="SQL Injection",
                affected_file="src/app.ts",
                line_numbers=[12],
                taint_source="req.query.id",
                taint_sink="db.query()",
            ),
            _ground_truth_entry(
                entry_id="gt-002",
                cwe_id="CWE-79",
                cwe_name="Cross-Site Scripting",
                affected_file="src/ui.ts",
                line_numbers=[27],
                taint_source="req.query.q",
                taint_sink="res.send()",
            ),
        ],
    )

    reports = scoring.score_multiple_outputs(
        {
            "piranesi": piranesi_output_path,
            "opengrep": opengrep_output_path,
            "llm_only": llm_output_path,
        },
        ground_truth_dir,
    )
    comparison = scoring.build_comparison_report(reports)
    rendered = scoring.render_comparison_report(comparison)

    assert "Baseline Comparison" in rendered
    assert "Piranesi" in rendered
    assert "OpenGrep" in rendered
    assert "LLM-Only" in rendered
    assert "Regulatory map" in rendered
    assert "Exploit gen" in rendered


def test_run_opengrep_scan_falls_back_to_semgrep_when_needed(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    calls: list[list[str]] = []

    def _which(binary: str) -> str | None:
        return {
            "opengrep": None,
            "semgrep": "/usr/local/bin/semgrep",
        }.get(binary)

    def _run(
        command: list[str],
        *,
        capture_output: bool,
        text: bool,
        timeout: int,
        check: bool,
    ) -> subprocess.CompletedProcess[str]:
        assert capture_output is True
        assert text is True
        assert check is False
        assert timeout > 0
        calls.append(command)
        return subprocess.CompletedProcess(
            args=command,
            returncode=0,
            stdout=json.dumps({"results": [], "errors": [], "version": "1.0.0"}),
            stderr="",
        )

    monkeypatch.setattr("eval.baselines.opengrep_runner.shutil.which", _which)
    monkeypatch.setattr("eval.baselines.opengrep_runner.subprocess.run", _run)

    result = run_opengrep_scan(tmp_path)

    assert result.tool == "semgrep"
    assert calls[0][0] == "/usr/local/bin/semgrep"
    assert "--json" in calls[0]


def test_llm_only_runner_uses_detector_model_and_normalizes_findings(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    project_dir = tmp_path / "project"
    source_dir = project_dir / "src"
    source_dir.mkdir(parents=True)
    (source_dir / "app.ts").write_text(
        "export function handler(req, res) { return db.query(req.query.id); }\n",
        encoding="utf-8",
    )

    calls: list[dict[str, Any]] = []

    def _completion(*, model: str, messages: list[dict[str, str]], **kwargs: Any) -> dict[str, Any]:
        calls.append({"model": model, "messages": messages, "kwargs": kwargs})
        return {
            "choices": [
                {
                    "message": {
                        "content": json.dumps(
                            {
                                "findings": [
                                    {
                                        "file": "src/app.ts",
                                        "line_numbers": [1],
                                        "cwe_id": "CWE-89",
                                        "description": (
                                            "Unsanitized request input reaches a SQL query sink."
                                        ),
                                        "severity": "high",
                                    }
                                ]
                            }
                        )
                    }
                }
            ]
        }

    fake_litellm = types.ModuleType("litellm")
    fake_litellm.completion = _completion  # type: ignore[attr-defined]
    fake_litellm.completion_cost = lambda response: 0.0  # type: ignore[attr-defined]
    monkeypatch.setitem(sys.modules, "litellm", fake_litellm)
    monkeypatch.setattr("piranesi.llm.provider.litellm", fake_litellm)

    config = PiranesiConfig(
        models=ModelsConfig(detector="detector-model"),
        trace=TraceConfig(enabled=False),
    )
    payload = run_llm_only_baseline(project_dir, config=config)

    assert calls[0]["model"] == "detector-model"
    assert payload["metadata"]["model"] == "detector-model"
    assert payload["pipeline_findings"] == 1
    assert payload["findings"][0]["affected_files"] == ["src/app.ts"]
    assert payload["findings"][0]["cwe_id"] == "CWE-89"
