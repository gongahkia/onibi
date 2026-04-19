from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app

runner = CliRunner()


def test_intel_help_lists_commands() -> None:
    result = runner.invoke(app, ["intel", "--help"])

    assert result.exit_code == 0
    assert "normalize" in result.stdout
    assert "graph" in result.stdout
    assert "summary" in result.stdout


def test_intel_normalize_graph_and_summary_workflow(fixtures_dir: Path, tmp_path: Path) -> None:
    snapshot = fixtures_dir / "intel" / "sample-sarif.json"
    normalized = tmp_path / "normalized.json"
    graph = tmp_path / "graph.json"
    summary = tmp_path / "summary.json"

    normalize = runner.invoke(
        app,
        [
            "intel",
            "normalize",
            str(snapshot),
            "--tool",
            "sarif",
            "--source-name",
            "codeql-ci",
            "--trust-level",
            "verified",
            "--output",
            str(normalized),
        ],
    )
    assert normalize.exit_code == 0, normalize.stdout
    assert normalized.exists()

    graph_result = runner.invoke(
        app,
        [
            "intel",
            "graph",
            "--normalized",
            str(normalized),
            "--output",
            str(graph),
        ],
    )
    assert graph_result.exit_code == 0, graph_result.stdout
    graph_payload = json.loads(graph.read_text(encoding="utf-8"))
    assert len(graph_payload["nodes"]) >= 2
    assert len(graph_payload["edges"]) >= 1

    summary_result = runner.invoke(
        app,
        [
            "intel",
            "summary",
            "--normalized",
            str(normalized),
            "--output",
            str(summary),
        ],
    )
    assert summary_result.exit_code == 0, summary_result.stdout
    summary_payload = json.loads(summary.read_text(encoding="utf-8"))
    assert summary_payload["source_name"] == "codeql-ci"
    assert summary_payload["findings_total"] == 1
