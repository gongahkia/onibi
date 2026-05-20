from __future__ import annotations

from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from piranesi.adapters.nmap import parse_nmap_xml_file
from piranesi.adapters.nuclei import parse_nuclei_jsonl_file
from piranesi.rescan.executor import (
    ContainerRunner,
    RescanExecutionResult,
    RescanOutput,
    execute_rescan_from_baseline,
)
from piranesi.workspace import (
    NormalizedFinding,
    WorkspaceState,
    load_workspace,
    upsert_findings,
    workspace_path,
)


class ReplayHarnessError(AssertionError):
    """Raised when deterministic replay output does not match expectations."""


@dataclass(frozen=True, slots=True)
class ReplayHarnessResult:
    rescan: RescanExecutionResult
    observed: list[dict[str, Any]]
    expected: list[dict[str, Any]]

    @property
    def matches(self) -> bool:
        return self.observed == self.expected


def run_deterministic_replay_harness(
    baseline: Path | str,
    *,
    expected_workspace: Path | str,
    output_workspace: Path | str,
    image_overrides: Sequence[str],
    container_runner: ContainerRunner,
    allow_unenforced_network: bool = True,
) -> ReplayHarnessResult:
    rescan = execute_rescan_from_baseline(
        baseline,
        output_workspace=output_workspace,
        image_overrides=image_overrides,
        allow_unenforced_network=allow_unenforced_network,
        container_runner=container_runner,
    )
    observed_state = ingest_replay_outputs(rescan.plan.output_workspace, rescan.outputs)
    expected_state = load_workspace(expected_workspace)
    result = ReplayHarnessResult(
        rescan=rescan,
        observed=normalize_findings_for_replay(observed_state.findings.findings),
        expected=normalize_findings_for_replay(expected_state.findings.findings),
    )
    if not result.matches:
        raise ReplayHarnessError("replay normalized findings differ from expected workspace")
    return result


def ingest_replay_outputs(
    workspace: Path | str,
    outputs: Sequence[RescanOutput],
) -> WorkspaceState:
    state = load_workspace(workspace)
    for output in outputs:
        raw_path = workspace_path(state.root, output.raw_path, allowed_roots=("raw",))
        if output.tool == "nmap":
            parse_result = parse_nmap_xml_file(
                raw_path,
                input_sha256=output.sha256,
                raw_path=output.raw_path,
            )
        elif output.tool == "nuclei":
            parse_result = parse_nuclei_jsonl_file(
                raw_path,
                input_sha256=output.sha256,
                raw_path=output.raw_path,
            )
        else:
            raise ReplayHarnessError(f"unsupported replay harness tool: {output.tool}")
        state = upsert_findings(state, parse_result.findings)
    return state


def normalize_findings_for_replay(
    findings: Sequence[NormalizedFinding],
) -> list[dict[str, Any]]:
    return sorted(
        (_normalize_finding(finding.model_dump(mode="json")) for finding in findings),
        key=lambda item: str(item["id"]),
    )


def _normalize_finding(payload: dict[str, Any]) -> dict[str, Any]:
    normalized = dict(payload)
    normalized.pop("first_seen", None)
    normalized.pop("last_seen", None)
    normalized["source_references"] = [
        _normalize_source_reference(item)
        for item in normalized.get("source_references", [])
        if isinstance(item, dict)
    ]
    return normalized


def _normalize_source_reference(payload: Mapping[str, Any]) -> dict[str, Any]:
    normalized = dict(payload)
    normalized["raw_path"] = "<raw-path>"
    return normalized


__all__ = [
    "ReplayHarnessError",
    "ReplayHarnessResult",
    "ingest_replay_outputs",
    "normalize_findings_for_replay",
    "run_deterministic_replay_harness",
]
