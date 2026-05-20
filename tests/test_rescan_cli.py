from __future__ import annotations

import json
import shutil
from collections.abc import Sequence
from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.rescan.executor import (
    build_container_replay_command,
    execute_rescan_from_baseline,
)
from piranesi.rescan.image_policy import AcceptedImage
from piranesi.rescan.network_policy import NetworkPolicyError, derive_network_policy
from piranesi.rescan.runtime import ContainerRuntimeStatus, RescanRuntimeError
from piranesi.workspace import AUDIT_LOG_FILE, load_workspace

FIXTURE_ROOT = Path(__file__).parent / "fixtures" / "pentest"
NMAP_FIXTURE = FIXTURE_ROOT / "nmap" / "localhost-http.xml"
NUCLEI_FIXTURE = FIXTURE_ROOT / "nuclei" / "localhost-http.jsonl"
DIGEST = "sha256:" + "a" * 64
runner = CliRunner()


def test_rescan_dry_run_outputs_recovered_specs_without_runtime(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    for tool, fixture in [("nmap", NMAP_FIXTURE), ("nuclei", NUCLEI_FIXTURE)]:
        result = runner.invoke(
            app,
            ["ingest", tool, "--input", str(fixture), "--workspace", str(workspace)],
        )
        assert result.exit_code == 0, result.output

    result = runner.invoke(
        app,
        ["rescan", "--from-baseline", str(workspace), "--dry-run", "--json"],
    )

    assert result.exit_code == 0, result.output
    payload = json.loads(result.stdout)
    assert payload["dry_run"] is True
    assert payload["baseline_workspace"] == str(workspace.resolve())
    assert payload["output_workspace"] == str(workspace.resolve().with_name("workspace-rescan"))
    assert {spec["tool"] for spec in payload["specs"]} == {"nmap", "nuclei"}
    assert payload["warnings"] == []
    assert payload["required_images"] == ["nmap", "nuclei"]
    assert payload["network_policy"]["enforcement_mode"] == "blocked-no-egress-enforcement"
    assert payload["network_policy"]["allowed_destinations"] == [
        "127.0.0.1",
        "http://127.0.0.1:48766",
        "localhost",
    ]


def test_rescan_reports_missing_runtime_cleanly(monkeypatch, tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    ingest = runner.invoke(
        app,
        ["ingest", "nmap", "--input", str(NMAP_FIXTURE), "--workspace", str(workspace)],
    )
    assert ingest.exit_code == 0, ingest.output

    def missing_runtime() -> ContainerRuntimeStatus:
        raise RescanRuntimeError(
            "rescan container runtime is unavailable: missing Docker CLI. "
            "Install optional rescan support with `piranesi[rescan]`."
        )

    monkeypatch.setattr("piranesi.rescan.executor.ensure_container_runtime", missing_runtime)

    result = runner.invoke(
        app,
        ["rescan", "--from-baseline", str(workspace), "--json-errors"],
    )

    assert result.exit_code == 1
    payload = json.loads(result.output)
    assert payload["exit_code"] == 1
    assert "rescan container runtime is unavailable" in payload["error"]
    assert "piranesi[rescan]" in payload["error"]


def test_rescan_rejects_unpinned_images_before_runtime(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    ingest = runner.invoke(
        app,
        ["ingest", "nmap", "--input", str(NMAP_FIXTURE), "--workspace", str(workspace)],
    )
    assert ingest.exit_code == 0, ingest.output

    result = runner.invoke(
        app,
        [
            "rescan",
            "--from-baseline",
            str(workspace),
            "--image",
            "nmap=nmap:latest",
            "--json-errors",
        ],
    )

    assert result.exit_code == 2
    payload = json.loads(result.output)
    assert "pinned" in payload["error"]


def test_rescan_requires_documented_network_override(monkeypatch, tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    ingest = runner.invoke(
        app,
        ["ingest", "nmap", "--input", str(NMAP_FIXTURE), "--workspace", str(workspace)],
    )
    assert ingest.exit_code == 0, ingest.output
    monkeypatch.setattr(
        "piranesi.rescan.executor.ensure_container_runtime",
        lambda: ContainerRuntimeStatus(docker_python_available=True, docker_cli_path="/bin/docker"),
    )

    result = runner.invoke(
        app,
        [
            "rescan",
            "--from-baseline",
            str(workspace),
            "--image",
            f"nmap=ghcr.io/acme/nmap:v1@{DIGEST}",
            "--json-errors",
        ],
    )

    assert result.exit_code == 1
    payload = json.loads(result.output)
    assert payload["exit_code"] == 1
    assert "Docker egress allowlisting is not enforced" in payload["error"]
    assert "--allow-unenforced-network" in payload["error"]


def test_network_policy_rejects_command_scope_expansion() -> None:
    spec = _spec("nmap", ["nmap", "-sV", "-oX", "old.xml", "10.0.0.2"])
    spec = spec.model_copy(update={"target_scope": ["10.0.0.1"]})

    try:
        derive_network_policy([spec], allow_unenforced_network=True)
    except NetworkPolicyError as exc:
        assert "expands beyond baseline target scope" in str(exc)
        assert "10.0.0.2" in str(exc)
    else:
        raise AssertionError("expected command scope expansion to be rejected")


def test_execute_rescan_writes_ingest_compatible_raw_outputs(
    monkeypatch,
    tmp_path: Path,
) -> None:
    baseline = tmp_path / "baseline"
    output = tmp_path / "current"
    ingest = runner.invoke(
        app,
        ["ingest", "nmap", "--input", str(NMAP_FIXTURE), "--workspace", str(baseline)],
    )
    assert ingest.exit_code == 0, ingest.output

    monkeypatch.setattr(
        "piranesi.rescan.executor.ensure_container_runtime",
        lambda: ContainerRuntimeStatus(docker_python_available=True, docker_cli_path="/bin/docker"),
    )

    def fake_runner(
        _image: AcceptedImage,
        command: Sequence[str],
        _host_output_dir: Path,
        host_output_path: Path,
        _timeout_seconds: int,
    ) -> None:
        output_index = list(command).index("-oX")
        assert command[output_index + 1] == "/out/001-nmap.xml"
        shutil.copyfile(NMAP_FIXTURE, host_output_path)

    result = execute_rescan_from_baseline(
        baseline,
        output_workspace=output,
        image_overrides=[f"nmap=ghcr.io/acme/nmap:v1@{DIGEST}"],
        allow_unenforced_network=True,
        container_runner=fake_runner,
    )

    assert len(result.outputs) == 1
    raw_output = output / result.outputs[0].raw_path
    assert raw_output.is_file()

    reingest = runner.invoke(
        app,
        ["ingest", "nmap", "--input", str(raw_output), "--workspace", str(output), "--json"],
    )
    assert reingest.exit_code == 0, reingest.output
    state = load_workspace(output)
    assert {record.tool for record in state.workspace.tool_inputs} == {"nmap"}
    assert len(state.findings.findings) == 2

    audit_events = [
        json.loads(line)
        for line in (output / AUDIT_LOG_FILE).read_text(encoding="utf-8").splitlines()
    ]
    assert any(event["command"] == "rescan" for event in audit_events)
    rescan_event = next(event for event in audit_events if event["command"] == "rescan")
    assert (
        rescan_event["summary"]["network_policy"]["enforcement_mode"]
        == "explicitly-unenforced-docker-default"
    )


def test_build_container_replay_command_forces_ingest_output_paths() -> None:
    nmap_command = build_container_replay_command(
        _spec("nmap", ["nmap", "-sV", "-oX", "old.xml", "127.0.0.1"]),
        "/out/current.xml",
    )
    nuclei_command = build_container_replay_command(
        _spec("nuclei", ["nuclei", "-jsonl", "-u", "http://127.0.0.1:48766"]),
        "/out/current.jsonl",
    )

    assert nmap_command == ["nmap", "-sV", "-oX", "/out/current.xml", "127.0.0.1"]
    assert nuclei_command[-2:] == ["-o", "/out/current.jsonl"]


def _spec(tool: str, command: list[str]):
    from piranesi.rescan.extractors import ReplayEvidence, ReplaySpec

    return ReplaySpec(
        tool=tool,
        recovered_command=command,
        target_scope=["127.0.0.1"],
        input_evidence=[ReplayEvidence(path=f"raw/{tool}/input", sha256="b" * 64)],
        confidence="high",
    )
