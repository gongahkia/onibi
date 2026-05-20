from __future__ import annotations

import hashlib
import json
import shlex
import subprocess
import tempfile
from collections.abc import Callable, Mapping, Sequence
from dataclasses import dataclass
from pathlib import Path, PurePosixPath
from typing import Any

from piranesi.rescan.extractors import (
    ReplayExtractionResult,
    ReplaySpec,
    extract_replay_specs,
)
from piranesi.rescan.image_policy import AcceptedImage, ImagePolicyError, validate_replay_image
from piranesi.rescan.runtime import ensure_container_runtime
from piranesi.workspace import (
    AuditEvent,
    ToolInputRecord,
    append_audit_event,
    copy_tool_input,
    create_workspace,
    load_workspace,
    utc_now,
)

SUPPORTED_REPLAY_TOOLS = frozenset({"nmap", "nuclei"})
DEFAULT_RESCAN_TIMEOUT_SECONDS = 900


class RescanExecutionError(RuntimeError):
    """Raised when a replay cannot be executed safely."""


class ReplayImageConfigError(ValueError):
    """Raised when user-supplied replay image configuration is invalid."""


ContainerRunner = Callable[[AcceptedImage, Sequence[str], Path, Path, int], None]


@dataclass(frozen=True, slots=True)
class RescanPlan:
    baseline: Path
    output_workspace: Path
    extraction: ReplayExtractionResult
    images: Mapping[str, AcceptedImage]

    def as_dict(self) -> dict[str, Any]:
        return {
            "schema_version": "piranesi.rescan-plan.v1",
            "baseline_workspace": str(self.baseline),
            "output_workspace": str(self.output_workspace),
            "specs": [spec.model_dump(mode="json") for spec in self.extraction.specs],
            "warnings": self.extraction.warnings,
            "required_images": sorted({spec.tool for spec in self.extraction.specs}),
            "images": {tool: image.provenance() for tool, image in sorted(self.images.items())},
        }


@dataclass(frozen=True, slots=True)
class RescanOutput:
    tool: str
    command: list[str]
    target_scope: list[str]
    input_evidence: list[dict[str, str]]
    image: AcceptedImage
    raw_path: str
    sha256: str
    record_id: str
    spec_sha256: str

    def as_dict(self) -> dict[str, Any]:
        return {
            "tool": self.tool,
            "command": self.command,
            "target_scope": self.target_scope,
            "input_evidence": self.input_evidence,
            "image": self.image.provenance(),
            "raw_path": self.raw_path,
            "sha256": self.sha256,
            "input_record": self.record_id,
            "spec_sha256": self.spec_sha256,
        }


@dataclass(frozen=True, slots=True)
class RescanExecutionResult:
    plan: RescanPlan
    outputs: list[RescanOutput]
    network_policy: str
    dry_run: bool = False

    def as_dict(self) -> dict[str, Any]:
        payload = self.plan.as_dict()
        payload.update(
            {
                "schema_version": "piranesi.rescan-result.v1",
                "dry_run": self.dry_run,
                "network_policy": self.network_policy,
                "outputs": [output.as_dict() for output in self.outputs],
            }
        )
        return payload


@dataclass(frozen=True, slots=True)
class _GeneratedReplayOutput:
    spec: ReplaySpec
    image: AcceptedImage
    command: list[str]
    path: Path
    spec_sha256: str


def default_rescan_output_workspace(baseline: Path | str) -> Path:
    baseline_path = Path(baseline).expanduser().resolve(strict=False)
    return baseline_path.with_name(f"{baseline_path.name}-rescan")


def parse_image_overrides(values: Sequence[str]) -> dict[str, AcceptedImage]:
    images: dict[str, AcceptedImage] = {}
    for value in values:
        tool, separator, reference = value.partition("=")
        normalized_tool = tool.strip()
        if not separator or not normalized_tool or not reference.strip():
            raise ReplayImageConfigError(
                "image overrides must use the form `tool=image@sha256:<digest>`"
            )
        if normalized_tool not in SUPPORTED_REPLAY_TOOLS:
            supported = ", ".join(sorted(SUPPORTED_REPLAY_TOOLS))
            raise ReplayImageConfigError(
                f"unsupported rescan image tool {normalized_tool!r}; expected one of: {supported}"
            )
        if normalized_tool in images:
            raise ReplayImageConfigError(f"duplicate image override for {normalized_tool}")
        try:
            images[normalized_tool] = validate_replay_image(reference)
        except ImagePolicyError as exc:
            raise ReplayImageConfigError(f"{normalized_tool}: {exc}") from exc
    return images


def plan_rescan_from_baseline(
    baseline: Path | str,
    *,
    output_workspace: Path | str | None = None,
    image_overrides: Sequence[str] = (),
) -> RescanPlan:
    baseline_path = Path(baseline).expanduser().resolve(strict=False)
    output_path = (
        Path(output_workspace).expanduser().resolve(strict=False)
        if output_workspace is not None
        else default_rescan_output_workspace(baseline_path)
    )
    if output_path == baseline_path:
        raise RescanExecutionError(
            "rescan output workspace must differ from the baseline workspace"
        )
    extraction = extract_replay_specs(baseline_path)
    images = parse_image_overrides(image_overrides)
    return RescanPlan(
        baseline=baseline_path,
        output_workspace=output_path,
        extraction=extraction,
        images=images,
    )


def execute_rescan_from_baseline(
    baseline: Path | str,
    *,
    output_workspace: Path | str | None = None,
    image_overrides: Sequence[str] = (),
    allow_unenforced_network: bool = False,
    timeout_seconds: int = DEFAULT_RESCAN_TIMEOUT_SECONDS,
    container_runner: ContainerRunner | None = None,
) -> RescanExecutionResult:
    plan = plan_rescan_from_baseline(
        baseline,
        output_workspace=output_workspace,
        image_overrides=image_overrides,
    )
    if not plan.extraction.specs:
        raise RescanExecutionError(_no_specs_message(plan.extraction.warnings))

    if plan.images:
        _require_images_for_specs(plan.extraction.specs, plan.images)
    ensure_container_runtime()
    if not plan.images:
        _require_images_for_specs(plan.extraction.specs, plan.images)
    if not allow_unenforced_network:
        raise RescanExecutionError(
            "rescan execution is blocked until a network egress policy is enforced. "
            "Use --dry-run to inspect recovered commands, or pass "
            "--allow-unenforced-network to explicitly acknowledge Docker default network behavior."
        )

    runner = container_runner or _run_replay_container
    baseline_state = load_workspace(plan.baseline)
    with tempfile.TemporaryDirectory(prefix="piranesi-rescan-") as temp_dir:
        temp_root = Path(temp_dir)
        generated = _generate_replay_outputs(
            plan,
            output_dir=temp_root,
            timeout_seconds=timeout_seconds,
            container_runner=runner,
        )
        output_state = create_workspace(
            plan.output_workspace,
            engagement=baseline_state.workspace.engagement,
            report_settings=baseline_state.workspace.report_settings,
        )
        outputs: list[RescanOutput] = []
        for generated_output in generated:
            metadata = _output_metadata(generated_output)
            output_state, record = copy_tool_input(
                output_state,
                tool=generated_output.spec.tool,
                input_path=generated_output.path,
                metadata=metadata,
            )
            outputs.append(_to_rescan_output(generated_output, record))

    _append_rescan_audit(plan, outputs, network_policy="explicitly-unenforced-docker-default")
    return RescanExecutionResult(
        plan=plan,
        outputs=outputs,
        network_policy="explicitly-unenforced-docker-default",
    )


def build_container_replay_command(spec: ReplaySpec, container_output_path: str) -> list[str]:
    if spec.tool == "nmap":
        return _rewrite_nmap_output(spec.recovered_command, container_output_path)
    if spec.tool == "nuclei":
        return _rewrite_nuclei_output(spec.recovered_command, container_output_path)
    raise RescanExecutionError(f"unsupported replay tool: {spec.tool}")


def spec_sha256(spec: ReplaySpec) -> str:
    payload = spec.model_dump(mode="json")
    encoded = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def _require_images_for_specs(
    specs: Sequence[ReplaySpec],
    images: Mapping[str, AcceptedImage],
) -> None:
    missing = sorted({spec.tool for spec in specs if spec.tool not in images})
    if missing:
        formatted = ", ".join(missing)
        raise ReplayImageConfigError(
            "missing digest-pinned rescan image override for: "
            f"{formatted}. Pass --image tool=repo:tag@sha256:<digest> for each tool."
        )


def _generate_replay_outputs(
    plan: RescanPlan,
    *,
    output_dir: Path,
    timeout_seconds: int,
    container_runner: ContainerRunner,
) -> list[_GeneratedReplayOutput]:
    generated: list[_GeneratedReplayOutput] = []
    for index, spec in enumerate(plan.extraction.specs, start=1):
        output_path = output_dir / _replay_output_filename(index, spec)
        container_output_path = PurePosixPath("/out") / output_path.name
        command = build_container_replay_command(spec, container_output_path.as_posix())
        image = plan.images[spec.tool]
        container_runner(image, command, output_dir, output_path, timeout_seconds)
        if not output_path.is_file():
            raise RescanExecutionError(
                f"{spec.tool} replay did not produce expected output {output_path.name}"
            )
        generated.append(
            _GeneratedReplayOutput(
                spec=spec,
                image=image,
                command=command,
                path=output_path,
                spec_sha256=spec_sha256(spec),
            )
        )
    return generated


def _run_replay_container(
    image: AcceptedImage,
    command: Sequence[str],
    host_output_dir: Path,
    host_output_path: Path,
    timeout_seconds: int,
) -> None:
    _ensure_local_image(image, timeout_seconds=timeout_seconds)
    docker_command = [
        "docker",
        "run",
        "--rm",
        "--pull=never",
        "-v",
        f"{host_output_dir.resolve()}:/out",
        image.reference,
        *command,
    ]
    completed = subprocess.run(
        docker_command,
        check=False,
        capture_output=True,
        text=True,
        timeout=timeout_seconds,
    )
    if completed.returncode != 0:
        raise RescanExecutionError(
            "rescan container failed: "
            f"{shlex.join(docker_command)} exited {completed.returncode}. "
            f"stderr: {_trim(completed.stderr)}"
        )
    if not host_output_path.is_file():
        raise RescanExecutionError(
            "rescan container completed without writing expected output "
            f"{host_output_path.name}. stdout: {_trim(completed.stdout)} "
            f"stderr: {_trim(completed.stderr)}"
        )


def _ensure_local_image(image: AcceptedImage, *, timeout_seconds: int) -> None:
    inspect_command = ["docker", "image", "inspect", image.reference]
    completed = subprocess.run(
        inspect_command,
        check=False,
        capture_output=True,
        text=True,
        timeout=timeout_seconds,
    )
    if completed.returncode != 0:
        raise RescanExecutionError(
            "rescan image is not available locally and will not be pulled implicitly: "
            f"{image.reference}. Pull and verify the digest explicitly before retrying. "
            f"stderr: {_trim(completed.stderr)}"
        )


def _rewrite_nmap_output(command: Sequence[str], output_path: str) -> list[str]:
    rewritten: list[str] = []
    replaced = False
    index = 0
    while index < len(command):
        item = command[index]
        if item == "-oX":
            if index + 1 >= len(command):
                raise RescanExecutionError("recovered nmap command has -oX without a value")
            rewritten.extend(["-oX", output_path])
            replaced = True
            index += 2
            continue
        if item.startswith("-oX") and item != "-oX":
            rewritten.extend(["-oX", output_path])
            replaced = True
            index += 1
            continue
        rewritten.append(item)
        index += 1
    if not replaced:
        rewritten.extend(["-oX", output_path])
    return rewritten


def _rewrite_nuclei_output(command: Sequence[str], output_path: str) -> list[str]:
    rewritten: list[str] = []
    replaced = False
    has_jsonl = False
    index = 0
    while index < len(command):
        item = command[index]
        if item == "-jsonl":
            has_jsonl = True
            rewritten.append(item)
            index += 1
            continue
        if item == "-o":
            if index + 1 >= len(command):
                raise RescanExecutionError("recovered nuclei command has -o without a value")
            rewritten.extend(["-o", output_path])
            replaced = True
            index += 2
            continue
        rewritten.append(item)
        index += 1
    if not has_jsonl:
        rewritten.append("-jsonl")
    if not replaced:
        rewritten.extend(["-o", output_path])
    return rewritten


def _replay_output_filename(index: int, spec: ReplaySpec) -> str:
    extension = "xml" if spec.tool == "nmap" else "jsonl"
    return f"{index:03d}-{spec.tool}.{extension}"


def _output_metadata(output: _GeneratedReplayOutput) -> dict[str, Any]:
    return {
        "rescan": {
            "schema_version": "piranesi.rescan-provenance.v1",
            "spec_sha256": output.spec_sha256,
            "spec": output.spec.model_dump(mode="json"),
            "command": output.command,
            "command_display": shlex.join(output.command),
            "environment": {"allowlist": {}},
            "target_scope": output.spec.target_scope,
            "input_evidence": [item.model_dump(mode="json") for item in output.spec.input_evidence],
            "image": output.image.provenance(),
            "network_policy": "explicitly-unenforced-docker-default",
        }
    }


def _to_rescan_output(
    output: _GeneratedReplayOutput,
    record: ToolInputRecord,
) -> RescanOutput:
    return RescanOutput(
        tool=output.spec.tool,
        command=output.command,
        target_scope=output.spec.target_scope,
        input_evidence=[item.model_dump(mode="json") for item in output.spec.input_evidence],
        image=output.image,
        raw_path=record.raw_path,
        sha256=record.sha256,
        record_id=record.id,
        spec_sha256=output.spec_sha256,
    )


def _append_rescan_audit(
    plan: RescanPlan,
    outputs: Sequence[RescanOutput],
    *,
    network_policy: str,
) -> None:
    state = load_workspace(plan.output_workspace)
    append_audit_event(
        state,
        AuditEvent(
            timestamp=utc_now(),
            command="rescan",
            input_path=str(plan.baseline),
            summary={
                "baseline_workspace": str(plan.baseline),
                "outputs": [output.as_dict() for output in outputs],
                "warnings": plan.extraction.warnings,
                "network_policy": network_policy,
            },
        ),
    )


def _no_specs_message(warnings: Sequence[str]) -> str:
    if warnings:
        return "no replayable scan evidence found: " + "; ".join(warnings)
    return "no replayable scan evidence found"


def _trim(value: str, *, limit: int = 2000) -> str:
    stripped = value.strip()
    if len(stripped) <= limit:
        return stripped
    return stripped[: limit - 3] + "..."


__all__ = [
    "DEFAULT_RESCAN_TIMEOUT_SECONDS",
    "ReplayImageConfigError",
    "RescanExecutionError",
    "RescanExecutionResult",
    "RescanOutput",
    "RescanPlan",
    "build_container_replay_command",
    "default_rescan_output_workspace",
    "execute_rescan_from_baseline",
    "parse_image_overrides",
    "plan_rescan_from_baseline",
    "spec_sha256",
]
