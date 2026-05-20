from __future__ import annotations

import shutil
from collections.abc import Sequence
from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.rescan.harness import normalize_findings_for_replay, run_deterministic_replay_harness
from piranesi.rescan.image_policy import AcceptedImage
from piranesi.rescan.runtime import ContainerRuntimeStatus
from piranesi.workspace import load_workspace

FIXTURE_ROOT = Path(__file__).parent / "fixtures" / "pentest"
NMAP_FIXTURE = FIXTURE_ROOT / "nmap" / "localhost-http.xml"
NUCLEI_FIXTURE = FIXTURE_ROOT / "nuclei" / "localhost-http.jsonl"
DIGEST = "sha256:" + "a" * 64
runner = CliRunner()


def test_deterministic_replay_harness_compares_normalized_findings(
    monkeypatch,
    tmp_path: Path,
) -> None:
    baseline = tmp_path / "baseline"
    expected = tmp_path / "expected"
    output = tmp_path / "current"
    _ingest_fixture_workspace(baseline)
    _ingest_fixture_workspace(expected)
    monkeypatch.setattr(
        "piranesi.rescan.executor.ensure_container_runtime",
        lambda: ContainerRuntimeStatus(docker_python_available=True, docker_cli_path="/bin/docker"),
    )

    def fixture_container(
        _image: AcceptedImage,
        command: Sequence[str],
        _host_output_dir: Path,
        host_output_path: Path,
        _timeout_seconds: int,
    ) -> None:
        source = NMAP_FIXTURE if command[0] == "nmap" else NUCLEI_FIXTURE
        shutil.copyfile(source, host_output_path)

    result = run_deterministic_replay_harness(
        baseline,
        expected_workspace=expected,
        output_workspace=output,
        image_overrides=[
            f"nmap=ghcr.io/acme/nmap:v1@{DIGEST}",
            f"nuclei=ghcr.io/acme/nuclei:v1@{DIGEST}",
        ],
        container_runner=fixture_container,
    )

    assert result.matches is True
    assert result.rescan.network_policy == "explicitly-unenforced-docker-default"
    assert {item["provenance"]["tool"] for item in result.observed} == {"nmap", "nuclei"}


def test_replay_normalization_ignores_timestamps_and_raw_paths(tmp_path: Path) -> None:
    first = tmp_path / "first"
    second = tmp_path / "second"
    _ingest_fixture_workspace(first)
    _ingest_fixture_workspace(second)
    first_state = load_workspace(first)
    second_state = load_workspace(second)

    normalized_first = normalize_findings_for_replay(first_state.findings.findings)
    normalized_second = normalize_findings_for_replay(second_state.findings.findings)

    assert normalized_first == normalized_second
    assert "first_seen" not in normalized_first[0]
    assert normalized_first[0]["source_references"][0]["raw_path"] == "<raw-path>"


def _ingest_fixture_workspace(workspace: Path) -> None:
    for tool, fixture in (("nmap", NMAP_FIXTURE), ("nuclei", NUCLEI_FIXTURE)):
        result = runner.invoke(
            app,
            ["ingest", tool, "--input", str(fixture), "--workspace", str(workspace)],
        )
        assert result.exit_code == 0, result.output
