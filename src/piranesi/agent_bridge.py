from __future__ import annotations

import json
import os
import shlex
import shutil
import subprocess
from datetime import UTC
from datetime import datetime as datetime_cls
from pathlib import Path
from typing import Any, Literal
from uuid import uuid4

from pydantic import BaseModel, ConfigDict, Field, ValidationError, field_validator

from piranesi import __version__
from piranesi.evidence import (
    EvidenceKind,
    EvidenceSensitivity,
    add_evidence_file,
    load_evidence_index,
)
from piranesi.pff import (
    LATEST_PFF_SCHEMA_VERSION,
    findings_from_pff_document,
    load_and_validate_pff_file,
)
from piranesi.workspace import (
    AUDIT_LOG_FILE,
    EVIDENCE_FILE,
    FINDINGS_FILE,
    AuditEvent,
    WorkspaceState,
    copy_tool_input,
    create_workspace,
    file_sha256,
    upsert_findings,
    utc_now,
    workspace_path,
)
from piranesi.workspace import append_audit_event as append_workspace_audit_event

AGENT_CONTEXT_SCHEMA_VERSION: Literal["piranesi.agent-context.v0"] = "piranesi.agent-context.v0"
AGENT_CONFIG_SCHEMA_VERSION: Literal["piranesi.agent-config.v0"] = "piranesi.agent-config.v0"
AGENT_RUN_SCHEMA_VERSION: Literal["piranesi.agent-run.v0"] = "piranesi.agent-run.v0"
AGENT_CONFIG_FILE = "agent/config.json"
DEFAULT_AGENT_TIMEOUT_SECONDS = 60 * 60
DEFAULT_AGENT_CHECK_TIMEOUT_SECONDS = 30

AgentRunMode = Literal["passive-recon", "active-scan", "triage", "retest", "other"]


class AgentBridgeError(ValueError):
    """Raised when an external pentest agent handoff is invalid or unsafe."""


class _StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class AgentIdentity(_StrictModel):
    name: str
    version: str
    kind: Literal["external", "internal", "service", "script"] = "external"

    @field_validator("name", "version")
    @classmethod
    def _not_empty(cls, value: str) -> str:
        if not value.strip():
            raise ValueError("must not be empty")
        return value


class AgentAuthorization(_StrictModel):
    approved_by: str
    approved_at: str
    approval_reference: str | None = None
    scope_acknowledged: bool = True

    @field_validator("approved_by", "approved_at")
    @classmethod
    def _not_empty(cls, value: str) -> str:
        if not value.strip():
            raise ValueError("must not be empty")
        return value


class AgentCommandRecord(_StrictModel):
    argv: list[str]
    cwd: str | None = None
    started_at: str | None = None
    completed_at: str | None = None
    exit_code: int | None = None
    output_path: str | None = None

    @field_validator("argv")
    @classmethod
    def _argv_not_empty(cls, value: list[str]) -> list[str]:
        if not value:
            raise ValueError("argv must include at least one token")
        if any(not item.strip() for item in value):
            raise ValueError("argv tokens must not be empty")
        return value


class AgentArtifact(_StrictModel):
    path: str
    kind: EvidenceKind = "other"
    title: str | None = None
    sensitivity: EvidenceSensitivity = "sensitive"
    tags: list[str] = Field(default_factory=list)
    notes: str | None = None

    @field_validator("path")
    @classmethod
    def _path_not_empty(cls, value: str) -> str:
        if not value.strip():
            raise ValueError("path must not be empty")
        return value


class AgentRunManifest(_StrictModel):
    schema_version: Literal["piranesi.agent-run.v0"] = AGENT_RUN_SCHEMA_VERSION
    run_id: str
    agent: AgentIdentity
    authorization: AgentAuthorization
    mode: AgentRunMode = "triage"
    scope: list[str]
    started_at: str | None = None
    completed_at: str | None = None
    pff_path: str | None = None
    artifacts: list[AgentArtifact] = Field(default_factory=list)
    commands: list[AgentCommandRecord] = Field(default_factory=list)
    notes: str | None = None

    @field_validator("run_id")
    @classmethod
    def _run_id_not_empty(cls, value: str) -> str:
        if not value.strip():
            raise ValueError("run_id must not be empty")
        return value

    @field_validator("scope")
    @classmethod
    def _scope_not_empty(cls, value: list[str]) -> list[str]:
        if not value:
            raise ValueError("scope must include at least one target or asset")
        if any(not item.strip() for item in value):
            raise ValueError("scope entries must not be empty")
        return value


class AgentContextDocument(_StrictModel):
    schema_version: Literal["piranesi.agent-context.v0"] = AGENT_CONTEXT_SCHEMA_VERSION
    piranesi_version: str = __version__
    workspace: dict[str, Any]
    engagement: dict[str, Any]
    policy: dict[str, Any]


class AgentImportResult(_StrictModel):
    run_id: str
    agent: dict[str, Any]
    mode: AgentRunMode
    manifest_raw_path: str
    manifest_sha256: str
    pff_raw_path: str | None = None
    pff_sha256: str | None = None
    findings: int = 0
    created: int = 0
    updated: int = 0
    evidence: int = 0
    evidence_ids: list[str] = Field(default_factory=list)

    def as_payload(self) -> dict[str, Any]:
        return self.model_dump(mode="json")


class AgentExecutionResult(_StrictModel):
    run_id: str
    live: bool
    command: list[str]
    context_path: str
    manifest_path: str
    run_dir: str
    stdout_path: str
    stderr_path: str
    exit_code: int | None = None
    imported: AgentImportResult | None = None

    def as_payload(self) -> dict[str, Any]:
        return self.model_dump(mode="json")


class AgentProfile(_StrictModel):
    name: str
    command: str
    check_command: str | None = None
    login_command: str | None = None
    default_mode: AgentRunMode = "triage"
    timeout_seconds: int = DEFAULT_AGENT_TIMEOUT_SECONDS
    required_env: list[str] = Field(default_factory=list)
    notes: str | None = None

    @field_validator("name", "command")
    @classmethod
    def _not_empty(cls, value: str) -> str:
        if not value.strip():
            raise ValueError("must not be empty")
        return value

    @field_validator("timeout_seconds")
    @classmethod
    def _positive_timeout(cls, value: int) -> int:
        if value <= 0:
            raise ValueError("timeout_seconds must be greater than zero")
        return value

    @field_validator("required_env")
    @classmethod
    def _env_names_not_empty(cls, value: list[str]) -> list[str]:
        if any(not item.strip() for item in value):
            raise ValueError("required_env entries must not be empty")
        return sorted(set(value))


class AgentConfigDocument(_StrictModel):
    schema_version: Literal["piranesi.agent-config.v0"] = AGENT_CONFIG_SCHEMA_VERSION
    profiles: list[AgentProfile] = Field(default_factory=list)


class AgentCheckResult(_StrictModel):
    name: str
    configured: bool
    command_available: bool
    required_env_present: bool
    check_exit_code: int | None = None
    login_configured: bool = False
    issues: list[str] = Field(default_factory=list)

    def as_payload(self) -> dict[str, Any]:
        return self.model_dump(mode="json")


class AgentLoginResult(_StrictModel):
    name: str
    command: list[str]
    stdout_path: str
    stderr_path: str
    exit_code: int

    def as_payload(self) -> dict[str, Any]:
        return self.model_dump(mode="json")


def load_agent_config(root: Path | str) -> AgentConfigDocument:
    path = workspace_path(root, AGENT_CONFIG_FILE, allowed_roots=("agent",))
    if not path.exists():
        return AgentConfigDocument()
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise AgentBridgeError(f"invalid agent config JSON: {exc.msg}") from exc
    except OSError as exc:
        raise AgentBridgeError(f"cannot read agent config: {exc}") from exc
    if not isinstance(payload, dict):
        raise AgentBridgeError("agent config must be a JSON object")
    version = payload.get("schema_version")
    if version != AGENT_CONFIG_SCHEMA_VERSION:
        raise AgentBridgeError(
            f"unsupported agent config schema version {version!r}; "
            f"expected {AGENT_CONFIG_SCHEMA_VERSION!r}"
        )
    try:
        return AgentConfigDocument.model_validate(payload)
    except ValidationError as exc:
        raise AgentBridgeError(f"invalid agent config: {exc}") from exc


def save_agent_config(root: Path | str, config: AgentConfigDocument) -> Path:
    path = workspace_path(root, AGENT_CONFIG_FILE, allowed_roots=("agent",))
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(config.model_dump(mode="json"), indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    return path


def upsert_agent_profile(state: WorkspaceState, profile: AgentProfile) -> AgentConfigDocument:
    config = load_agent_config(state.root)
    profiles = [existing for existing in config.profiles if existing.name != profile.name]
    profiles.append(profile)
    updated = AgentConfigDocument(profiles=sorted(profiles, key=lambda item: item.name))
    path = save_agent_config(state.root, updated)
    append_workspace_audit_event(
        state,
        AuditEvent(
            timestamp=utc_now(),
            command="agent add",
            output_path=AGENT_CONFIG_FILE,
            output_sha256=file_sha256(path),
            summary={
                "name": profile.name,
                "check_configured": profile.check_command is not None,
                "login_configured": profile.login_command is not None,
                "required_env": profile.required_env,
            },
        ),
    )
    return updated


def get_agent_profile(root: Path | str, name: str) -> AgentProfile:
    config = load_agent_config(root)
    for profile in config.profiles:
        if profile.name == name:
            return profile
    raise AgentBridgeError(f"unknown agent profile: {name}")


def check_agent_profile(
    root: Path | str,
    name: str,
    *,
    timeout_seconds: int = DEFAULT_AGENT_CHECK_TIMEOUT_SECONDS,
) -> AgentCheckResult:
    if timeout_seconds <= 0:
        raise AgentBridgeError("agent check timeout must be greater than zero seconds")
    profile = get_agent_profile(root, name)
    issues: list[str] = []
    command_argv = _split_command(profile.command)
    command_available = _command_available(command_argv[0])
    if not command_available:
        issues.append(f"command executable is not available: {command_argv[0]}")
    missing_env = [item for item in profile.required_env if not os.environ.get(item)]
    if missing_env:
        issues.append("missing required environment variables: " + ", ".join(missing_env))

    check_exit_code: int | None = None
    if profile.check_command is not None:
        check_argv = _split_command(profile.check_command)
        if not _command_available(check_argv[0]):
            issues.append(f"check executable is not available: {check_argv[0]}")
            check_exit_code = None
        else:
            try:
                completed = subprocess.run(
                    check_argv,
                    cwd=Path(root).expanduser().resolve(strict=False),
                    stdout=subprocess.DEVNULL,
                    stderr=subprocess.DEVNULL,
                    check=False,
                    timeout=timeout_seconds,
                )
            except subprocess.TimeoutExpired:
                issues.append(f"check command timed out after {timeout_seconds} seconds")
            else:
                check_exit_code = completed.returncode
                if completed.returncode != 0:
                    issues.append(f"check command exited with {completed.returncode}")

    return AgentCheckResult(
        name=profile.name,
        configured=True,
        command_available=command_available,
        required_env_present=not missing_env,
        check_exit_code=check_exit_code,
        login_configured=profile.login_command is not None,
        issues=issues,
    )


def login_agent_profile(
    root: Path | str,
    name: str,
    *,
    timeout_seconds: int = DEFAULT_AGENT_TIMEOUT_SECONDS,
) -> AgentLoginResult:
    if timeout_seconds <= 0:
        raise AgentBridgeError("agent login timeout must be greater than zero seconds")
    state = create_workspace(root)
    profile = get_agent_profile(state.root, name)
    if profile.login_command is None:
        raise AgentBridgeError(f"agent profile {name!r} does not define a login command")
    argv = _split_command(profile.login_command)
    profile_dir = workspace_path(
        state.root,
        Path("agent") / "profiles" / _safe_component(profile.name),
        allowed_roots=("agent",),
    )
    profile_dir.mkdir(parents=True, exist_ok=True)
    stdout_path = profile_dir / "login-stdout.txt"
    stderr_path = profile_dir / "login-stderr.txt"
    with (
        stdout_path.open("w", encoding="utf-8") as stdout_handle,
        stderr_path.open("w", encoding="utf-8") as stderr_handle,
    ):
        try:
            completed = subprocess.run(
                argv,
                cwd=profile_dir,
                env=dict(os.environ),
                stdout=stdout_handle,
                stderr=stderr_handle,
                check=False,
                timeout=timeout_seconds,
            )
        except subprocess.TimeoutExpired as exc:
            raise AgentBridgeError(
                f"agent login command timed out after {timeout_seconds} seconds; "
                f"stdout={stdout_path} stderr={stderr_path}"
            ) from exc
    result = AgentLoginResult(
        name=profile.name,
        command=_redacted_argv(argv),
        stdout_path=str(stdout_path),
        stderr_path=str(stderr_path),
        exit_code=completed.returncode,
    )
    append_workspace_audit_event(
        state,
        AuditEvent(
            timestamp=utc_now(),
            command="agent login",
            output_path=str(stdout_path),
            output_sha256=file_sha256(stdout_path),
            summary=result.as_payload(),
        ),
    )
    if completed.returncode != 0:
        raise AgentBridgeError(
            f"agent login command exited with {completed.returncode}; "
            f"stdout={stdout_path} stderr={stderr_path}"
        )
    return result


def build_agent_context(state: WorkspaceState) -> AgentContextDocument:
    evidence_index = load_evidence_index(state.root)
    return AgentContextDocument(
        workspace={
            "root": str(state.root),
            "findings": len(state.findings.findings),
            "evidence": len(evidence_index.evidence),
            "audit_log": AUDIT_LOG_FILE,
        },
        engagement=state.workspace.engagement.model_dump(mode="json"),
        policy={
            "purpose": "external-pentest-agent-bridge",
            "source_of_truth": "Piranesi workspace remains authoritative after import.",
            "required_run_manifest_schema": AGENT_RUN_SCHEMA_VERSION,
            "findings_contract": LATEST_PFF_SCHEMA_VERSION,
            "authorization_required": True,
            "scope_required": True,
            "human_review_required": True,
            "forbidden_without_separate_operator_approval": [
                "targets outside workspace scope",
                "credential use",
                "payload execution",
                "exploitation",
                "C2 or live session control",
                "raw evidence upload to third-party services",
            ],
        },
    )


def write_agent_context(state: WorkspaceState, output_path: Path | None = None) -> Path:
    path = output_path or workspace_path(state.root, "agent/context.json", allowed_roots=("agent",))
    path = path.expanduser().resolve(strict=False)
    path.parent.mkdir(parents=True, exist_ok=True)
    document = build_agent_context(state)
    path.write_text(
        json.dumps(document.model_dump(mode="json"), indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    return path


def load_agent_run_manifest(path: Path | str) -> AgentRunManifest:
    manifest_path = Path(path)
    try:
        payload = json.loads(manifest_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise AgentBridgeError(f"invalid agent run manifest JSON: {exc.msg}") from exc
    except OSError as exc:
        raise AgentBridgeError(f"cannot read agent run manifest: {exc}") from exc
    if not isinstance(payload, dict):
        raise AgentBridgeError("agent run manifest must be a JSON object")
    try:
        manifest = AgentRunManifest.model_validate(payload)
    except ValidationError as exc:
        raise AgentBridgeError(f"invalid agent run manifest: {exc}") from exc
    if manifest.pff_path is None and not manifest.artifacts:
        raise AgentBridgeError("agent run manifest must include pff_path, artifacts, or both")
    _validate_manifest_relative_paths(manifest)
    return manifest


def import_agent_run_manifest(
    manifest_path: Path | str,
    *,
    workspace: Path | str,
) -> AgentImportResult:
    manifest_source = Path(manifest_path).expanduser().resolve(strict=True)
    manifest = load_agent_run_manifest(manifest_source)
    state = create_workspace(workspace)
    _validate_agent_scope(manifest, state)
    state, manifest_record = copy_tool_input(
        state,
        tool="agent-manifest",
        input_path=manifest_source,
        metadata={
            "run_id": manifest.run_id,
            "agent": manifest.agent.model_dump(mode="json"),
            "mode": manifest.mode,
            "scope": manifest.scope,
        },
    )

    pff_raw_path: str | None = None
    pff_sha256: str | None = None
    created = 0
    updated = 0
    findings_count = 0
    if manifest.pff_path is not None:
        pff_source = _resolve_manifest_path(manifest_source.parent, manifest.pff_path)
        state, pff_record = copy_tool_input(
            state,
            tool="agent-pff",
            input_path=pff_source,
            metadata={"run_id": manifest.run_id, "agent": manifest.agent.name},
        )
        raw_pff = workspace_path(state.root, pff_record.raw_path, allowed_roots=("raw",))
        document = load_and_validate_pff_file(raw_pff)
        findings = [
            _attach_agent_provenance(finding, manifest=manifest)
            for finding in findings_from_pff_document(
                document,
                input_sha256=pff_record.sha256,
                raw_path=pff_record.raw_path,
            )
        ]
        before_ids = {finding.id for finding in state.findings.findings}
        incoming_ids = {finding.id for finding in findings}
        state = upsert_findings(state, findings)
        created = len(incoming_ids - before_ids)
        updated = len(incoming_ids & before_ids)
        findings_count = len(incoming_ids)
        pff_raw_path = pff_record.raw_path
        pff_sha256 = pff_record.sha256

    evidence_ids: list[str] = []
    for artifact in manifest.artifacts:
        artifact_source = _resolve_manifest_path(manifest_source.parent, artifact.path)
        _index, record = add_evidence_file(
            state.root,
            file_path=artifact_source,
            kind=artifact.kind,
            title=artifact.title,
            source=f"agent:{manifest.agent.name}",
            sensitivity=artifact.sensitivity,
            tags=sorted(
                set(artifact.tags)
                | {"agent-run", f"agent:{manifest.agent.name}", f"run:{manifest.run_id}"}
            ),
            notes=artifact.notes,
        )
        evidence_ids.append(record.id)

    result = AgentImportResult(
        run_id=manifest.run_id,
        agent=manifest.agent.model_dump(mode="json"),
        mode=manifest.mode,
        manifest_raw_path=manifest_record.raw_path,
        manifest_sha256=manifest_record.sha256,
        pff_raw_path=pff_raw_path,
        pff_sha256=pff_sha256,
        findings=findings_count,
        created=created,
        updated=updated,
        evidence=len(evidence_ids),
        evidence_ids=evidence_ids,
    )
    append_workspace_audit_event(
        state,
        AuditEvent(
            timestamp=utc_now(),
            command="agent import-run",
            input_path=manifest_record.raw_path,
            input_sha256=manifest_record.sha256,
            output_path=FINDINGS_FILE if findings_count else EVIDENCE_FILE,
            output_sha256=_agent_import_output_sha256(state, findings_count=findings_count),
            summary=result.as_payload(),
        ),
    )
    return result


def run_agent_command(
    *,
    workspace: Path | str,
    command: str,
    run_id: str | None = None,
    mode: AgentRunMode = "triage",
    approved_by: str | None = None,
    approval_reference: str | None = None,
    live: bool = False,
    import_manifest: bool = True,
    timeout_seconds: int = DEFAULT_AGENT_TIMEOUT_SECONDS,
) -> AgentExecutionResult:
    state = create_workspace(workspace)
    if not state.workspace.engagement.scope:
        raise AgentBridgeError("workspace engagement scope is required before running an agent")
    if timeout_seconds <= 0:
        raise AgentBridgeError("agent timeout must be greater than zero seconds")
    if live and not approved_by:
        raise AgentBridgeError("--approved-by is required when running an agent with --live")

    resolved_run_id = run_id or _default_run_id()
    if not resolved_run_id.strip():
        raise AgentBridgeError("run ID must not be empty")
    run_dir = workspace_path(
        state.root,
        Path("agent") / "runs" / _safe_component(resolved_run_id),
        allowed_roots=("agent",),
    )
    run_dir.mkdir(parents=True, exist_ok=True)
    context_path = write_agent_context(state, run_dir / "context.json")
    manifest_path = run_dir / "agent-run.json"
    stdout_path = run_dir / "stdout.txt"
    stderr_path = run_dir / "stderr.txt"

    raw_argv = _split_command(command)
    argv = _format_command_argv(
        raw_argv,
        context_path=context_path,
        manifest_path=manifest_path,
        run_dir=run_dir,
        workspace_root=state.root,
        scope=state.workspace.engagement.scope,
        run_id=resolved_run_id,
    )
    result = AgentExecutionResult(
        run_id=resolved_run_id,
        live=live,
        command=_redacted_argv(argv),
        context_path=str(context_path),
        manifest_path=str(manifest_path),
        run_dir=str(run_dir),
        stdout_path=str(stdout_path),
        stderr_path=str(stderr_path),
    )

    if not live:
        _append_agent_run_audit(
            state,
            result=result,
            timeout_seconds=timeout_seconds,
            approved_by=approved_by,
            approval_reference=approval_reference,
        )
        return result

    env = dict(os.environ)
    env.update(
        {
            "PIRANESI_AGENT_CONTEXT": str(context_path),
            "PIRANESI_AGENT_MANIFEST": str(manifest_path),
            "PIRANESI_AGENT_RUN_DIR": str(run_dir),
            "PIRANESI_AGENT_WORKSPACE": str(state.root),
            "PIRANESI_AGENT_SCOPE": ",".join(state.workspace.engagement.scope),
            "PIRANESI_AGENT_RUN_ID": resolved_run_id,
            "PIRANESI_AGENT_MODE": mode,
            "PIRANESI_AGENT_APPROVED_BY": approved_by or "",
            "PIRANESI_AGENT_APPROVAL_REFERENCE": approval_reference or "",
        }
    )

    with (
        stdout_path.open("w", encoding="utf-8") as stdout_handle,
        stderr_path.open("w", encoding="utf-8") as stderr_handle,
    ):
        try:
            completed = subprocess.run(
                argv,
                cwd=run_dir,
                env=env,
                stdout=stdout_handle,
                stderr=stderr_handle,
                check=False,
                timeout=timeout_seconds,
            )
        except subprocess.TimeoutExpired as exc:
            raise AgentBridgeError(
                f"agent command timed out after {timeout_seconds} seconds; "
                f"stdout={stdout_path} stderr={stderr_path}"
            ) from exc

    result = result.model_copy(update={"exit_code": completed.returncode})
    _append_agent_run_audit(
        state,
        result=result,
        timeout_seconds=timeout_seconds,
        approved_by=approved_by,
        approval_reference=approval_reference,
    )
    if completed.returncode != 0:
        raise AgentBridgeError(
            f"agent command exited with {completed.returncode}; "
            f"stdout={stdout_path} stderr={stderr_path}"
        )
    if not import_manifest:
        return result
    if not manifest_path.is_file():
        raise AgentBridgeError(
            f"agent command completed but did not write manifest: {manifest_path}"
        )

    imported = import_agent_run_manifest(manifest_path, workspace=state.root)
    return result.model_copy(update={"imported": imported})


def _attach_agent_provenance(finding: Any, *, manifest: AgentRunManifest) -> Any:
    provenance = dict(finding.provenance)
    provenance["agent_run"] = {
        "run_id": manifest.run_id,
        "agent": manifest.agent.model_dump(mode="json"),
        "mode": manifest.mode,
        "scope": manifest.scope,
        "authorization": manifest.authorization.model_dump(mode="json"),
    }
    tags = sorted(set(finding.tags) | {"agent-run", f"agent:{manifest.agent.name}"})
    return finding.model_copy(update={"provenance": provenance, "tags": tags})


def _agent_import_output_sha256(state: WorkspaceState, *, findings_count: int) -> str:
    output_path = state.root / (FINDINGS_FILE if findings_count else EVIDENCE_FILE)
    return file_sha256(output_path)


def _validate_manifest_relative_paths(manifest: AgentRunManifest) -> None:
    if manifest.pff_path is not None:
        _validate_relative_path(manifest.pff_path, field="pff_path")
    for artifact in manifest.artifacts:
        _validate_relative_path(artifact.path, field="artifacts.path")
    for command in manifest.commands:
        if command.output_path is not None:
            _validate_relative_path(command.output_path, field="commands.output_path")


def _validate_agent_scope(manifest: AgentRunManifest, state: WorkspaceState) -> None:
    workspace_scope = set(state.workspace.engagement.scope)
    if not workspace_scope:
        raise AgentBridgeError(
            "workspace engagement scope is required before importing an agent run"
        )
    out_of_scope = sorted(set(manifest.scope) - workspace_scope)
    if out_of_scope:
        joined = ", ".join(out_of_scope)
        raise AgentBridgeError(
            "agent run scope includes targets outside workspace scope: " + joined
        )


def _append_agent_run_audit(
    state: WorkspaceState,
    *,
    result: AgentExecutionResult,
    timeout_seconds: int,
    approved_by: str | None,
    approval_reference: str | None,
) -> None:
    context_path = Path(result.context_path)
    output_sha256 = file_sha256(context_path) if context_path.is_file() else None
    append_workspace_audit_event(
        state,
        AuditEvent(
            timestamp=utc_now(),
            command="agent run",
            output_path=result.context_path,
            output_sha256=output_sha256,
            summary={
                "run_id": result.run_id,
                "live": result.live,
                "command": result.command,
                "exit_code": result.exit_code,
                "manifest_path": result.manifest_path,
                "stdout_path": result.stdout_path,
                "stderr_path": result.stderr_path,
                "timeout_seconds": timeout_seconds,
                "approved_by": approved_by,
                "approval_reference": approval_reference,
            },
        ),
    )


def _split_command(command: str) -> list[str]:
    try:
        argv = shlex.split(command)
    except ValueError as exc:
        raise AgentBridgeError(f"invalid agent command: {exc}") from exc
    if not argv:
        raise AgentBridgeError("agent command must not be empty")
    return argv


def _command_available(executable: str) -> bool:
    path = Path(executable)
    if path.is_absolute() or "/" in executable:
        return path.exists() and os.access(path, os.X_OK)
    return shutil.which(executable) is not None


def _format_command_argv(
    argv: list[str],
    *,
    context_path: Path,
    manifest_path: Path,
    run_dir: Path,
    workspace_root: Path,
    scope: list[str],
    run_id: str,
) -> list[str]:
    values = {
        "context": str(context_path),
        "manifest": str(manifest_path),
        "run_dir": str(run_dir),
        "workspace": str(workspace_root),
        "scope": ",".join(scope),
        "run_id": run_id,
    }
    try:
        return [item.format(**values) for item in argv]
    except KeyError as exc:
        raise AgentBridgeError(f"unknown agent command placeholder: {exc}") from exc


def _redacted_argv(argv: list[str]) -> list[str]:
    redacted: list[str] = []
    redact_next = False
    for item in argv:
        lowered = item.lower()
        if redact_next:
            redacted.append("[redacted]")
            redact_next = False
            continue
        if any(marker in lowered for marker in ("token", "secret", "password", "api-key")):
            if "=" in item:
                key, _separator, _value = item.partition("=")
                redacted.append(f"{key}=[redacted]")
            else:
                redacted.append(item)
                redact_next = True
            continue
        redacted.append(item)
    return redacted


def _default_run_id() -> str:
    timestamp = datetime_cls.now(UTC).strftime("%Y%m%dT%H%M%SZ")
    return f"agent-{timestamp}-{uuid4().hex[:8]}"


def _safe_component(value: str) -> str:
    cleaned = "".join(char if char.isalnum() or char in {".", "-", "_"} else "-" for char in value)
    cleaned = cleaned.strip(".-")
    return cleaned or "agent-run"


def _validate_relative_path(value: str, *, field: str) -> None:
    rel_path = Path(value)
    if rel_path.is_absolute():
        raise AgentBridgeError(f"{field} must be relative to the manifest directory")
    if not rel_path.parts or any(part in {"", ".", ".."} for part in rel_path.parts):
        raise AgentBridgeError(f"{field} cannot be empty or contain traversal segments")


def _resolve_manifest_path(base_dir: Path, value: str) -> Path:
    _validate_relative_path(value, field="manifest path")
    base = base_dir.resolve(strict=True)
    target = (base / value).resolve(strict=False)
    try:
        target.relative_to(base)
    except ValueError as exc:
        raise AgentBridgeError(f"manifest path escapes manifest directory: {value}") from exc
    if not target.is_file():
        raise AgentBridgeError(f"manifest referenced file does not exist: {value}")
    return target


__all__ = [
    "AGENT_CONFIG_FILE",
    "AGENT_CONFIG_SCHEMA_VERSION",
    "AGENT_CONTEXT_SCHEMA_VERSION",
    "AGENT_RUN_SCHEMA_VERSION",
    "DEFAULT_AGENT_CHECK_TIMEOUT_SECONDS",
    "DEFAULT_AGENT_TIMEOUT_SECONDS",
    "AgentArtifact",
    "AgentAuthorization",
    "AgentBridgeError",
    "AgentCheckResult",
    "AgentCommandRecord",
    "AgentConfigDocument",
    "AgentContextDocument",
    "AgentExecutionResult",
    "AgentIdentity",
    "AgentImportResult",
    "AgentLoginResult",
    "AgentProfile",
    "AgentRunManifest",
    "AgentRunMode",
    "build_agent_context",
    "check_agent_profile",
    "get_agent_profile",
    "import_agent_run_manifest",
    "load_agent_config",
    "load_agent_run_manifest",
    "login_agent_profile",
    "run_agent_command",
    "save_agent_config",
    "upsert_agent_profile",
    "write_agent_context",
]
