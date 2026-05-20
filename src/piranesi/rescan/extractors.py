from __future__ import annotations

import json
import shlex
from pathlib import Path
from typing import Any, Literal
from urllib.parse import urlparse
from xml.etree.ElementTree import Element, ParseError

from defusedxml import ElementTree as DefusedET
from pydantic import BaseModel, ConfigDict, Field

from piranesi.workspace import (
    ToolInputRecord,
    WorkspaceError,
    file_sha256,
    load_workspace,
    workspace_path,
)

ReplayConfidence = Literal["low", "medium", "high"]


class ReplayExtractionError(ValueError):
    """Raised when baseline evidence cannot produce a replay spec safely."""


class _StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class ReplayEvidence(_StrictModel):
    path: str
    sha256: str


class ReplaySpec(_StrictModel):
    schema_version: Literal["piranesi.replay-spec.v1"] = "piranesi.replay-spec.v1"
    tool: Literal["nmap", "nuclei"]
    recovered_command: list[str]
    target_scope: list[str]
    input_evidence: list[ReplayEvidence]
    confidence: ReplayConfidence
    metadata: dict[str, Any] = Field(default_factory=dict)


class ReplayExtractionResult(_StrictModel):
    schema_version: Literal["piranesi.replay-extraction.v1"] = "piranesi.replay-extraction.v1"
    specs: list[ReplaySpec] = Field(default_factory=list)
    warnings: list[str] = Field(default_factory=list)


def extract_replay_specs(root: Path | str) -> ReplayExtractionResult:
    try:
        state = load_workspace(root)
    except WorkspaceError as exc:
        raise ReplayExtractionError(str(exc)) from exc
    specs: list[ReplaySpec] = []
    warnings: list[str] = []
    for record in sorted(state.workspace.tool_inputs, key=lambda item: item.id):
        if record.tool not in {"nmap", "nuclei"}:
            continue
        try:
            specs.append(extract_replay_spec_for_input(state.root, record))
        except ReplayExtractionError as exc:
            warnings.append(f"{record.raw_path}: {exc}")
    if not specs and not warnings:
        warnings.append("workspace has no supported nmap or nuclei baseline evidence")
    return ReplayExtractionResult(specs=specs, warnings=warnings)


def extract_replay_spec_for_input(root: Path | str, record: ToolInputRecord) -> ReplaySpec:
    if record.tool == "nmap":
        return _extract_nmap_spec(root, record)
    if record.tool == "nuclei":
        return _extract_nuclei_spec(root, record)
    raise ReplayExtractionError(f"unsupported replay tool: {record.tool}")


def _extract_nmap_spec(root: Path | str, record: ToolInputRecord) -> ReplaySpec:
    path = workspace_path(root, record.raw_path, allowed_roots=("raw",))
    try:
        document = DefusedET.parse(path).getroot()
    except ParseError as exc:
        raise ReplayExtractionError(f"invalid nmap XML: {exc}") from exc
    args = document.attrib.get("args")
    if not args:
        raise ReplayExtractionError("nmap XML does not include original args")
    command = shlex.split(args)
    if not command or Path(command[0]).name != "nmap":
        raise ReplayExtractionError("nmap args do not start with nmap")
    target_scope = _nmap_targets(document)
    if not target_scope:
        raise ReplayExtractionError("nmap XML does not include recoverable target scope")
    return ReplaySpec(
        tool="nmap",
        recovered_command=command,
        target_scope=target_scope,
        input_evidence=[ReplayEvidence(path=record.raw_path, sha256=record.sha256)],
        confidence="high",
        metadata={
            "nmap_version": document.attrib.get("version"),
            "xmloutputversion": document.attrib.get("xmloutputversion"),
            "evidence_sha256": file_sha256(path),
        },
    )


def _extract_nuclei_spec(root: Path | str, record: ToolInputRecord) -> ReplaySpec:
    path = workspace_path(root, record.raw_path, allowed_roots=("raw",))
    targets: set[str] = set()
    templates: set[str] = set()
    records = 0
    for line_number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), start=1):
        stripped = line.strip()
        if not stripped:
            continue
        records += 1
        try:
            payload = json.loads(stripped)
        except json.JSONDecodeError as exc:
            raise ReplayExtractionError(
                f"line {line_number}: invalid nuclei JSON ({exc.msg})"
            ) from exc
        if not isinstance(payload, dict):
            raise ReplayExtractionError(f"line {line_number}: expected nuclei JSON object")
        target = _nuclei_target(payload)
        template = _string(payload.get("template-path")) or _string(payload.get("template-id"))
        if target:
            targets.add(target)
        if template:
            templates.add(template)
    if records == 0:
        raise ReplayExtractionError("nuclei JSONL contains no records")
    if not targets:
        raise ReplayExtractionError("nuclei JSONL does not include recoverable target scope")
    if not templates:
        raise ReplayExtractionError("nuclei JSONL does not include recoverable templates")
    if len(targets) > 1:
        raise ReplayExtractionError(
            "nuclei JSONL contains multiple targets without an original list file"
        )
    command = ["nuclei", "-jsonl", "-u", next(iter(sorted(targets)))]
    for template in sorted(templates):
        command.extend(["-t", template])
    return ReplaySpec(
        tool="nuclei",
        recovered_command=command,
        target_scope=sorted(targets),
        input_evidence=[ReplayEvidence(path=record.raw_path, sha256=record.sha256)],
        confidence="medium",
        metadata={
            "templates": sorted(templates),
            "records": records,
            "evidence_sha256": file_sha256(path),
        },
    )


def _nmap_targets(document: Element) -> list[str]:
    targets: set[str] = set()
    for host in document.findall("host"):
        for address in host.findall("address"):
            value = address.attrib.get("addr")
            if value:
                targets.add(value)
        hostname = host.find("./hostnames/hostname")
        if hostname is not None and hostname.attrib.get("name"):
            targets.add(hostname.attrib["name"])
    return sorted(targets)


def _nuclei_target(payload: dict[str, Any]) -> str | None:
    for key in ("url", "matched-at", "host"):
        value = _string(payload.get(key))
        if value:
            parsed = urlparse(value)
            if parsed.scheme and parsed.netloc:
                return f"{parsed.scheme}://{parsed.netloc}"
            return value
    return None


def _string(value: object) -> str | None:
    if isinstance(value, str) and value.strip():
        return value.strip()
    return None


__all__ = [
    "ReplayConfidence",
    "ReplayEvidence",
    "ReplayExtractionError",
    "ReplayExtractionResult",
    "ReplaySpec",
    "extract_replay_spec_for_input",
    "extract_replay_specs",
]
