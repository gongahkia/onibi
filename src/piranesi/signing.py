from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Literal, cast

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from piranesi import __version__
from piranesi.workspace import (
    AUDIT_LOG_FILE,
    FINDINGS_FILE,
    WORKSPACE_FILE,
    WorkspaceState,
    file_sha256,
    load_workspace,
    workspace_path,
)

MANIFEST_SCHEMA_VERSION: Literal["piranesi.chain-of-custody.v1"] = "piranesi.chain-of-custody.v1"
ArtifactRole = Literal[
    "workspace",
    "findings",
    "audit-log",
    "raw-input",
    "report",
    "evidence",
    "timeline",
    "objective",
    "procedure",
    "detection",
    "signature",
]


class SigningError(ValueError):
    """Raised when a workspace cannot be signed or verified."""


class _StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class ManifestArtifact(_StrictModel):
    path: str
    sha256: str
    role: ArtifactRole


class AuditChainEntry(_StrictModel):
    line: int
    previous_hash: str
    event_hash: str
    command: str | None = None
    timestamp: str | None = None


class ReplayProvenanceEvidence(_StrictModel):
    path: str
    sha256: str


class ReplayProvenanceEnvelope(_StrictModel):
    schema_version: Literal["piranesi.replay-provenance.v1"] = "piranesi.replay-provenance.v1"
    tool: str
    input_record: str
    replay_spec_sha256: str
    command: list[str]
    command_display: str | None = None
    environment: dict[str, Any] = Field(default_factory=dict)
    target_scope: list[str]
    input_evidence: list[ReplayProvenanceEvidence]
    image: dict[str, str | None]
    network_policy: str
    output_evidence: list[ReplayProvenanceEvidence]


class ChainOfCustodyManifest(_StrictModel):
    schema_version: Literal["piranesi.chain-of-custody.v1"] = MANIFEST_SCHEMA_VERSION
    manifest_id: str
    piranesi_version: str
    workspace_schema_version: str
    findings_schema_version: str
    command: dict[str, Any]
    artifacts: list[ManifestArtifact]
    tool_inputs: list[dict[str, Any]]
    replay_provenance: list[ReplayProvenanceEnvelope] = Field(default_factory=list)
    audit_chain: list[AuditChainEntry] = Field(default_factory=list)
    audit_chain_head: str
    limitations: list[str]


@dataclass(frozen=True)
class VerificationFailure:
    path: str
    message: str
    expected_sha256: str | None = None
    actual_sha256: str | None = None


@dataclass(frozen=True)
class VerificationResult:
    ok: bool
    manifest_path: Path
    manifest_id: str
    failures: list[VerificationFailure]


def sign_workspace(root: Path | str) -> tuple[ChainOfCustodyManifest, Path]:
    state = load_workspace(root)
    manifest = build_manifest(state)
    manifest_path = workspace_path(
        state.root,
        f"signatures/manifest-{manifest.manifest_id}.json",
        allowed_roots=("signatures",),
    )
    manifest_path.write_text(_canonical_json(manifest.model_dump(mode="json")), encoding="utf-8")
    return manifest, manifest_path


def verify_workspace(
    root: Path | str,
    *,
    manifest_path: Path | None = None,
) -> VerificationResult:
    state = load_workspace(root)
    resolved_manifest = manifest_path or latest_manifest_path(state.root)
    if resolved_manifest is None:
        raise SigningError("no chain-of-custody manifest found under signatures/")
    if not resolved_manifest.is_file():
        raise SigningError(f"manifest file does not exist: {resolved_manifest}")

    try:
        payload = json.loads(resolved_manifest.read_text(encoding="utf-8"))
        manifest = ChainOfCustodyManifest.model_validate(payload)
    except (json.JSONDecodeError, ValidationError) as exc:
        raise SigningError(f"invalid manifest: {exc}") from exc

    failures: list[VerificationFailure] = []
    expected_manifest_id = _manifest_id(payload)
    if expected_manifest_id != manifest.manifest_id:
        failures.append(
            VerificationFailure(
                path=resolved_manifest.name,
                message="manifest_id does not match canonical manifest content",
                expected_sha256=manifest.manifest_id,
                actual_sha256=expected_manifest_id,
            )
        )

    for artifact in manifest.artifacts:
        path = workspace_path(state.root, artifact.path)
        if not path.is_file():
            failures.append(VerificationFailure(path=artifact.path, message="covered file missing"))
            continue
        actual_sha = file_sha256(path)
        if actual_sha != artifact.sha256:
            failures.append(
                VerificationFailure(
                    path=artifact.path,
                    message="covered file digest mismatch",
                    expected_sha256=artifact.sha256,
                    actual_sha256=actual_sha,
                )
            )

    audit_path = state.root / AUDIT_LOG_FILE
    actual_chain = audit_chain(audit_path)
    if actual_chain.head != manifest.audit_chain_head:
        failures.append(
            VerificationFailure(
                path=AUDIT_LOG_FILE,
                message="audit chain head mismatch",
                expected_sha256=manifest.audit_chain_head,
                actual_sha256=actual_chain.head,
            )
        )

    return VerificationResult(
        ok=not failures,
        manifest_path=resolved_manifest,
        manifest_id=manifest.manifest_id,
        failures=failures,
    )


def build_manifest(state: WorkspaceState) -> ChainOfCustodyManifest:
    audit = audit_chain(state.root / AUDIT_LOG_FILE)
    artifacts = collect_manifest_artifacts(state.root)
    payload = {
        "schema_version": MANIFEST_SCHEMA_VERSION,
        "manifest_id": "",
        "piranesi_version": __version__,
        "workspace_schema_version": state.workspace.schema_version,
        "findings_schema_version": state.findings.schema_version,
        "command": {
            "name": "piranesi sign",
            "arguments": {"workspace": "<workspace>"},
        },
        "artifacts": [artifact.model_dump(mode="json") for artifact in artifacts],
        "tool_inputs": [
            _tool_input_manifest(item)
            for item in sorted(state.workspace.tool_inputs, key=lambda record: record.id)
        ],
        "replay_provenance": [
            envelope.model_dump(mode="json") for envelope in replay_provenance_envelopes(state)
        ],
        "audit_chain": [entry.model_dump(mode="json") for entry in audit.entries],
        "audit_chain_head": audit.head,
        "limitations": [
            "This manifest uses local SHA-256 digests only.",
            "It does not provide cryptographic identity, RFC3161 timestamping, or Sigstore trust.",
        ],
    }
    payload["manifest_id"] = _manifest_id(payload)
    return ChainOfCustodyManifest.model_validate(payload)


def collect_manifest_artifacts(workspace_root: Path) -> list[ManifestArtifact]:
    artifacts: list[ManifestArtifact] = []
    for relative, role in (
        (WORKSPACE_FILE, "workspace"),
        (FINDINGS_FILE, "findings"),
        (AUDIT_LOG_FILE, "audit-log"),
    ):
        path = workspace_path(workspace_root, relative)
        if path.is_file():
            artifacts.append(
                ManifestArtifact(
                    path=relative,
                    sha256=file_sha256(path),
                    role=cast(ArtifactRole, role),
                )
            )

    for root_name, role in (
        ("raw", "raw-input"),
        ("reports", "report"),
        ("evidence", "evidence"),
        ("timeline", "timeline"),
        ("objectives", "objective"),
        ("procedures", "procedure"),
        ("detections", "detection"),
    ):
        root = workspace_root / root_name
        if not root.is_dir():
            continue
        for path in sorted(item for item in root.rglob("*") if item.is_file()):
            artifacts.append(
                ManifestArtifact(
                    path=path.relative_to(workspace_root).as_posix(),
                    sha256=file_sha256(path),
                    role=cast(ArtifactRole, role),
                )
            )
    return sorted(artifacts, key=lambda artifact: (artifact.role, artifact.path))


def replay_provenance_envelopes(state: WorkspaceState) -> list[ReplayProvenanceEnvelope]:
    envelopes: list[ReplayProvenanceEnvelope] = []
    for record in sorted(state.workspace.tool_inputs, key=lambda item: item.id):
        raw_rescan = record.metadata.get("rescan")
        if not isinstance(raw_rescan, dict):
            continue
        envelopes.append(_replay_provenance_envelope(record, raw_rescan))
    return envelopes


@dataclass(frozen=True)
class _AuditChain:
    entries: list[AuditChainEntry]
    head: str


def audit_chain(path: Path) -> _AuditChain:
    previous = "0" * 64
    entries: list[AuditChainEntry] = []
    if not path.is_file():
        return _AuditChain(entries=[], head=previous)
    for line_number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), start=1):
        event: dict[str, Any] | None = None
        stripped = line.strip()
        if stripped:
            try:
                raw_event = json.loads(stripped)
                if isinstance(raw_event, dict):
                    event = raw_event
            except json.JSONDecodeError:
                event = {"_invalid_json": stripped}
        event_payload: dict[str, Any] = event or {"_blank": True}
        event_hash = _sha256_text(previous + _canonical_json(event_payload))
        entries.append(
            AuditChainEntry(
                line=line_number,
                previous_hash=previous,
                event_hash=event_hash,
                command=_string_or_none(event_payload.get("command")),
                timestamp=_string_or_none(event_payload.get("timestamp")),
            )
        )
        previous = event_hash
    return _AuditChain(entries=entries, head=previous)


def latest_manifest_path(workspace_root: Path) -> Path | None:
    signatures = workspace_root / "signatures"
    if not signatures.is_dir():
        return None
    manifests = sorted(signatures.glob("manifest-*.json"))
    return manifests[-1] if manifests else None


def verification_result_payload(result: VerificationResult) -> dict[str, Any]:
    return {
        "ok": result.ok,
        "manifest_id": result.manifest_id,
        "manifest_path": str(result.manifest_path),
        "failures": [
            {
                "path": failure.path,
                "message": failure.message,
                "expected_sha256": failure.expected_sha256,
                "actual_sha256": failure.actual_sha256,
            }
            for failure in result.failures
        ],
    }


def _tool_input_manifest(record: Any) -> dict[str, Any]:
    metadata = dict(record.metadata)
    return {
        "id": record.id,
        "tool": record.tool,
        "raw_path": record.raw_path,
        "sha256": record.sha256,
        "tool_version": metadata.get(f"{record.tool}_version") or metadata.get("tool_version"),
        "command_args": metadata.get("args") or metadata.get("command"),
    }


def _replay_provenance_envelope(
    record: Any,
    metadata: dict[str, Any],
) -> ReplayProvenanceEnvelope:
    input_evidence = [
        ReplayProvenanceEvidence(path=item["path"], sha256=item["sha256"])
        for item in metadata.get("input_evidence", [])
        if isinstance(item, dict)
        and isinstance(item.get("path"), str)
        and isinstance(item.get("sha256"), str)
    ]
    return ReplayProvenanceEnvelope(
        tool=record.tool,
        input_record=record.id,
        replay_spec_sha256=str(metadata.get("spec_sha256") or ""),
        command=[str(item) for item in metadata.get("command", [])],
        command_display=_string_or_none(metadata.get("command_display")),
        environment=_dict_or_empty(metadata.get("environment")),
        target_scope=[str(item) for item in metadata.get("target_scope", [])],
        input_evidence=input_evidence,
        image=_image_provenance(metadata.get("image")),
        network_policy=str(metadata.get("network_policy") or "unknown"),
        output_evidence=[ReplayProvenanceEvidence(path=record.raw_path, sha256=record.sha256)],
    )


def _image_provenance(value: Any) -> dict[str, str | None]:
    if not isinstance(value, dict):
        return {}
    image: dict[str, str | None] = {}
    for key in ("image_reference", "image_repository", "image_tag", "image_digest"):
        item = value.get(key)
        image[key] = item if isinstance(item, str) or item is None else str(item)
    return image


def _dict_or_empty(value: Any) -> dict[str, Any]:
    return dict(value) if isinstance(value, dict) else {}


def _manifest_id(payload: dict[str, Any]) -> str:
    canonical_payload = dict(payload)
    canonical_payload["manifest_id"] = ""
    return _sha256_text(_canonical_json(canonical_payload))


def _canonical_json(payload: Any) -> str:
    return json.dumps(payload, sort_keys=True, separators=(",", ":")) + "\n"


def _sha256_text(text: str) -> str:
    import hashlib

    return hashlib.sha256(text.encode("utf-8")).hexdigest()


def _string_or_none(value: object) -> str | None:
    return value if isinstance(value, str) else None


__all__ = [
    "ChainOfCustodyManifest",
    "ManifestArtifact",
    "ReplayProvenanceEnvelope",
    "SigningError",
    "VerificationFailure",
    "VerificationResult",
    "audit_chain",
    "build_manifest",
    "collect_manifest_artifacts",
    "latest_manifest_path",
    "replay_provenance_envelopes",
    "sign_workspace",
    "verification_result_payload",
    "verify_workspace",
]
