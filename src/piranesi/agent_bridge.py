from __future__ import annotations

import json
import os
import shlex
import shutil
import subprocess
import urllib.error
import urllib.parse
import urllib.request
from datetime import UTC
from datetime import datetime as datetime_cls
from pathlib import Path
from typing import Any, Literal, cast
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
AgentExecutionType = Literal["local", "cloud-http"]
AgentAuthType = Literal["none", "api-key-env", "oauth-cli", "custom-login"]
AgentPresetName = Literal["openclaw", "claude", "codex", "cloud-http"]


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
    command: str | None = None
    execution_type: AgentExecutionType = "local"
    check_command: str | None = None
    login_command: str | None = None
    auth_type: AgentAuthType = "none"
    api_key_env: str | None = None
    remote_url: str | None = None
    remote_auth_env: str | None = None
    default_mode: AgentRunMode = "triage"
    timeout_seconds: int = DEFAULT_AGENT_TIMEOUT_SECONDS
    required_env: list[str] = Field(default_factory=list)
    notes: str | None = None

    @field_validator("name")
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

    @field_validator(
        "command",
        "login_command",
        "check_command",
        "api_key_env",
        "remote_url",
        "remote_auth_env",
    )
    @classmethod
    def _optional_not_empty(cls, value: str | None) -> str | None:
        if value is not None and not value.strip():
            raise ValueError("must not be empty")
        return value


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


def build_agent_preset(
    preset: AgentPresetName | str,
    *,
    name: str | None = None,
    openclaw_agent: str = "piranesi",
    openclaw_provider: str = "openai-codex",
    remote_url: str | None = None,
    remote_auth_env: str | None = None,
) -> AgentProfile:
    profile_name = name or preset
    if preset == "openclaw":
        prompt = _agent_prompt("OpenClaw")
        return AgentProfile(
            name=profile_name,
            command=(
                "openclaw agent "
                f"--agent {shlex.quote(openclaw_agent)} "
                f"--message {shlex.quote(prompt)}"
            ),
            check_command="openclaw --version",
            login_command=f"openclaw models auth login --provider {shlex.quote(openclaw_provider)}",
            auth_type="oauth-cli",
            default_mode="triage",
            notes=(
                "OpenClaw preset based on the documented `openclaw agent --message` "
                "and `openclaw models auth login --provider` CLI surface."
            ),
        )
    if preset == "claude":
        prompt = _agent_prompt("Claude Code")
        return AgentProfile(
            name=profile_name,
            command=f"claude -p --add-dir {{run_dir}} {shlex.quote(prompt)}",
            check_command="claude --version",
            login_command="claude auth",
            auth_type="oauth-cli",
            default_mode="triage",
            notes=(
                "Claude Code CLI preset. The CLI must write PFF and the manifest into the run dir."
            ),
        )
    if preset == "codex":
        prompt = _agent_prompt("Codex CLI")
        return AgentProfile(
            name=profile_name,
            command=(
                "codex exec --cd {run_dir} --sandbox workspace-write "
                f"--ask-for-approval never {shlex.quote(prompt)}"
            ),
            check_command="codex doctor",
            login_command="codex login",
            auth_type="oauth-cli",
            default_mode="triage",
            notes="Codex CLI preset. The CLI must write PFF and the manifest into the run dir.",
        )
    if preset == "cloud-http":
        if remote_url is None:
            raise AgentBridgeError("--remote-url is required for the cloud-http preset")
        return AgentProfile(
            name=profile_name,
            execution_type="cloud-http",
            remote_url=remote_url,
            remote_auth_env=remote_auth_env,
            auth_type="api-key-env" if remote_auth_env else "none",
            api_key_env=remote_auth_env,
            default_mode="triage",
            notes=(
                "Cloud HTTP preset. Piranesi POSTs scoped context and imports the "
                "returned manifest/PFF/artifacts locally."
            ),
        )
    raise AgentBridgeError(f"unknown agent preset: {preset}")


def available_agent_presets() -> list[dict[str, str]]:
    return [
        {
            "name": "openclaw",
            "type": "local",
            "summary": "OpenClaw CLI via `openclaw agent --message`.",
        },
        {
            "name": "claude",
            "type": "local",
            "summary": "Claude Code CLI via non-interactive print mode.",
        },
        {
            "name": "codex",
            "type": "local",
            "summary": "Codex CLI via non-interactive `codex exec`.",
        },
        {
            "name": "cloud-http",
            "type": "cloud-http",
            "summary": "Generic HTTP endpoint returning Piranesi manifest/PFF/artifacts.",
        },
    ]


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
    _validate_profile(profile)
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
                "execution_type": profile.execution_type,
                "auth_type": profile.auth_type,
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
    command_available = True
    if profile.execution_type == "local":
        if profile.command is None:
            command_available = False
            issues.append("local profile does not define a command")
        else:
            command_argv = _split_command(profile.command)
            command_available = _command_available(command_argv[0])
            if not command_available:
                issues.append(f"command executable is not available: {command_argv[0]}")
    elif profile.remote_url is None:
        issues.append("cloud-http profile does not define remote_url")

    required_env = _profile_required_env(profile)
    missing_env = [item for item in required_env if not os.environ.get(item)]
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
        login_configured=profile.login_command is not None or profile.auth_type == "api-key-env",
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
    if profile.auth_type == "api-key-env":
        if profile.api_key_env is None:
            raise AgentBridgeError(f"agent profile {name!r} does not define api_key_env")
        if not os.environ.get(profile.api_key_env):
            raise AgentBridgeError(
                f"environment variable {profile.api_key_env!r} is required for agent login"
            )
        profile_dir = workspace_path(
            state.root,
            Path("agent") / "profiles" / _safe_component(profile.name),
            allowed_roots=("agent",),
        )
        profile_dir.mkdir(parents=True, exist_ok=True)
        stdout_path = profile_dir / "login-stdout.txt"
        stderr_path = profile_dir / "login-stderr.txt"
        stdout_path.write_text(
            f"API key environment variable present: {profile.api_key_env}\n",
            encoding="utf-8",
        )
        stderr_path.write_text("", encoding="utf-8")
        result = AgentLoginResult(
            name=profile.name,
            command=["env", profile.api_key_env],
            stdout_path=str(stdout_path),
            stderr_path=str(stderr_path),
            exit_code=0,
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
        return result
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
    prose_fallback: bool = False,
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
    if not manifest_path.is_file() and prose_fallback:
        _write_prose_fallback_manifest(
            manifest_path,
            run_id=resolved_run_id,
            mode=mode,
            scope=state.workspace.engagement.scope,
            approved_by=approved_by or "unknown",
            approval_reference=approval_reference,
            stdout_path=stdout_path,
            stderr_path=stderr_path,
            agent_name=Path(argv[0]).name,
        )
    if not manifest_path.is_file():
        raise AgentBridgeError(
            f"agent command completed but did not write manifest: {manifest_path}"
        )

    imported = import_agent_run_manifest(manifest_path, workspace=state.root)
    return result.model_copy(update={"imported": imported})


def run_agent_profile(
    *,
    workspace: Path | str,
    profile: AgentProfile,
    run_id: str | None = None,
    mode: AgentRunMode | None = None,
    approved_by: str | None = None,
    approval_reference: str | None = None,
    live: bool = False,
    import_manifest: bool = True,
    timeout_seconds: int | None = None,
    prose_fallback: bool = False,
) -> AgentExecutionResult:
    _validate_profile(profile)
    resolved_timeout = timeout_seconds or profile.timeout_seconds
    resolved_mode = mode or profile.default_mode
    if profile.execution_type == "local":
        if profile.command is None:
            raise AgentBridgeError(f"agent profile {profile.name!r} does not define a command")
        return run_agent_command(
            workspace=workspace,
            command=profile.command,
            run_id=run_id,
            mode=resolved_mode,
            approved_by=approved_by,
            approval_reference=approval_reference,
            live=live,
            import_manifest=import_manifest,
            timeout_seconds=resolved_timeout,
            prose_fallback=prose_fallback,
        )
    return run_cloud_agent_profile(
        workspace=workspace,
        profile=profile,
        run_id=run_id,
        mode=resolved_mode,
        approved_by=approved_by,
        approval_reference=approval_reference,
        live=live,
        import_manifest=import_manifest,
        timeout_seconds=resolved_timeout,
        prose_fallback=prose_fallback,
    )


def run_cloud_agent_profile(
    *,
    workspace: Path | str,
    profile: AgentProfile,
    run_id: str | None = None,
    mode: AgentRunMode = "triage",
    approved_by: str | None = None,
    approval_reference: str | None = None,
    live: bool = False,
    import_manifest: bool = True,
    timeout_seconds: int = DEFAULT_AGENT_TIMEOUT_SECONDS,
    prose_fallback: bool = False,
) -> AgentExecutionResult:
    _validate_profile(profile)
    state = create_workspace(workspace)
    if profile.remote_url is None:
        raise AgentBridgeError(f"cloud agent profile {profile.name!r} does not define remote_url")
    if not state.workspace.engagement.scope:
        raise AgentBridgeError("workspace engagement scope is required before running an agent")
    if live and not approved_by:
        raise AgentBridgeError("--approved-by is required when running an agent with --live")
    if timeout_seconds <= 0:
        raise AgentBridgeError("agent timeout must be greater than zero seconds")

    resolved_run_id = run_id or _default_run_id()
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
    result = AgentExecutionResult(
        run_id=resolved_run_id,
        live=live,
        command=["cloud-http", profile.remote_url],
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

    context = json.loads(context_path.read_text(encoding="utf-8"))
    request_payload = {
        "schema_version": "piranesi.agent-cloud-request.v0",
        "run_id": resolved_run_id,
        "mode": mode,
        "scope": state.workspace.engagement.scope,
        "context": context,
        "authorization": {
            "approved_by": approved_by,
            "approval_reference": approval_reference,
        },
    }
    headers = {"Content-Type": "application/json", "Accept": "application/json, text/plain"}
    if profile.remote_auth_env:
        token = os.environ.get(profile.remote_auth_env)
        if not token:
            raise AgentBridgeError(f"environment variable {profile.remote_auth_env!r} is required")
        headers["Authorization"] = f"Bearer {token}"
    request = urllib.request.Request(  # noqa: S310 - _validate_profile requires HTTPS.
        profile.remote_url,
        data=json.dumps(request_payload).encode("utf-8"),
        headers=headers,
        method="POST",
    )
    try:
        with urllib.request.urlopen(  # noqa: S310 - _validate_profile requires HTTPS.
            request,
            timeout=timeout_seconds,
        ) as response:
            body = response.read()
            content_type = response.headers.get("Content-Type", "")
    except urllib.error.URLError as exc:
        raise AgentBridgeError(f"cloud agent request failed: {exc}") from exc

    stdout_path.write_bytes(body)
    stderr_path.write_text("", encoding="utf-8")
    result = result.model_copy(update={"exit_code": 0})
    _append_agent_run_audit(
        state,
        result=result,
        timeout_seconds=timeout_seconds,
        approved_by=approved_by,
        approval_reference=approval_reference,
    )

    if "json" in content_type.lower():
        try:
            response_payload = json.loads(body.decode("utf-8"))
        except json.JSONDecodeError as exc:
            raise AgentBridgeError(f"cloud agent returned invalid JSON: {exc.msg}") from exc
        _materialize_cloud_agent_response(
            response_payload,
            run_dir=run_dir,
            manifest_path=manifest_path,
            run_id=resolved_run_id,
            mode=mode,
            scope=state.workspace.engagement.scope,
            approved_by=approved_by or "unknown",
            approval_reference=approval_reference,
            agent_name=profile.name,
        )
    elif prose_fallback:
        _write_prose_fallback_manifest(
            manifest_path,
            run_id=resolved_run_id,
            mode=mode,
            scope=state.workspace.engagement.scope,
            approved_by=approved_by or "unknown",
            approval_reference=approval_reference,
            stdout_path=stdout_path,
            stderr_path=stderr_path,
            agent_name=profile.name,
        )
    else:
        raise AgentBridgeError(
            "cloud agent returned non-JSON output; rerun with --prose-fallback to "
            "preserve it as evidence"
        )

    if not import_manifest:
        return result
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


def _profile_required_env(profile: AgentProfile) -> list[str]:
    required = set(profile.required_env)
    if profile.api_key_env:
        required.add(profile.api_key_env)
    if profile.remote_auth_env:
        required.add(profile.remote_auth_env)
    return sorted(required)


def _validate_profile(profile: AgentProfile) -> None:
    if profile.execution_type == "local" and profile.command is None:
        raise AgentBridgeError(f"local agent profile {profile.name!r} requires a command")
    if profile.execution_type == "cloud-http" and profile.remote_url is None:
        raise AgentBridgeError(f"cloud-http agent profile {profile.name!r} requires remote_url")
    if profile.execution_type == "cloud-http":
        try:
            url = cast(str, profile.remote_url)
            parsed = urllib.parse.urlparse(url)
        except Exception as exc:
            raise AgentBridgeError(
                f"cloud-http agent profile {profile.name!r} has invalid remote_url"
            ) from exc
        if parsed.scheme != "https":
            raise AgentBridgeError(
                f"cloud-http agent profile {profile.name!r} remote_url must use https"
            )
    if profile.auth_type == "api-key-env" and profile.api_key_env is None:
        raise AgentBridgeError(f"api-key-env agent profile {profile.name!r} requires api_key_env")
    if profile.auth_type in {"oauth-cli", "custom-login"} and profile.login_command is None:
        raise AgentBridgeError(
            f"{profile.auth_type} agent profile {profile.name!r} requires login_command"
        )


def _write_prose_fallback_manifest(
    manifest_path: Path,
    *,
    run_id: str,
    mode: AgentRunMode,
    scope: list[str],
    approved_by: str,
    approval_reference: str | None,
    stdout_path: Path,
    stderr_path: Path,
    agent_name: str,
) -> None:
    artifacts = [
        {
            "path": stdout_path.name,
            "kind": "transcript",
            "title": "Agent stdout transcript",
            "sensitivity": "sensitive",
            "tags": ["agent-prose-fallback", "stdout"],
        }
    ]
    if stderr_path.is_file() and stderr_path.stat().st_size > 0:
        artifacts.append(
            {
                "path": stderr_path.name,
                "kind": "transcript",
                "title": "Agent stderr transcript",
                "sensitivity": "sensitive",
                "tags": ["agent-prose-fallback", "stderr"],
            }
        )
    manifest = {
        "schema_version": AGENT_RUN_SCHEMA_VERSION,
        "run_id": run_id,
        "agent": {
            "name": agent_name,
            "version": "unknown",
            "kind": "external",
        },
        "authorization": {
            "approved_by": approved_by,
            "approved_at": utc_now(),
            "approval_reference": approval_reference,
            "scope_acknowledged": True,
        },
        "mode": mode,
        "scope": scope,
        "artifacts": artifacts,
        "notes": (
            "Agent did not emit structured PFF/manifest output; Piranesi preserved "
            "the prose transcript as evidence only."
        ),
    }
    manifest_path.write_text(
        json.dumps(manifest, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )


def _materialize_cloud_agent_response(
    payload: Any,
    *,
    run_dir: Path,
    manifest_path: Path,
    run_id: str,
    mode: AgentRunMode,
    scope: list[str],
    approved_by: str,
    approval_reference: str | None,
    agent_name: str,
) -> None:
    if not isinstance(payload, dict):
        raise AgentBridgeError("cloud agent JSON response must be an object")
    pff_payload = payload.get("pff")
    if pff_payload is not None:
        if not isinstance(pff_payload, dict):
            raise AgentBridgeError("cloud agent pff field must be a JSON object")
        (run_dir / "findings.pff.json").write_text(
            json.dumps(pff_payload, indent=2, sort_keys=True) + "\n",
            encoding="utf-8",
        )

    artifacts_payload = payload.get("artifacts") or []
    if not isinstance(artifacts_payload, list):
        raise AgentBridgeError("cloud agent artifacts field must be a list")
    artifacts: list[dict[str, Any]] = []
    for index, artifact in enumerate(artifacts_payload):
        if not isinstance(artifact, dict):
            raise AgentBridgeError(f"cloud agent artifact {index} must be an object")
        path_value = artifact.get("path")
        content = artifact.get("content")
        if not isinstance(path_value, str) or not path_value.strip():
            raise AgentBridgeError(f"cloud agent artifact {index} is missing path")
        _validate_relative_path(path_value, field="artifacts.path")
        artifact_path = run_dir / path_value
        artifact_path.parent.mkdir(parents=True, exist_ok=True)
        if isinstance(content, str):
            artifact_path.write_text(content, encoding="utf-8")
        elif content is None:
            artifact_path.touch(exist_ok=True)
        else:
            raise AgentBridgeError(f"cloud agent artifact {index} content must be a string")
        artifacts.append(
            {
                "path": path_value,
                "kind": artifact.get("kind", "other"),
                "title": artifact.get("title"),
                "sensitivity": artifact.get("sensitivity", "sensitive"),
                "tags": artifact.get("tags", []),
                "notes": artifact.get("notes"),
            }
        )

    manifest_payload = payload.get("manifest")
    if manifest_payload is None:
        manifest_payload = {
            "schema_version": AGENT_RUN_SCHEMA_VERSION,
            "run_id": run_id,
            "agent": {
                "name": agent_name,
                "version": str(payload.get("agent_version") or "unknown"),
                "kind": "service",
            },
            "authorization": {
                "approved_by": approved_by,
                "approved_at": utc_now(),
                "approval_reference": approval_reference,
                "scope_acknowledged": True,
            },
            "mode": mode,
            "scope": scope,
            "pff_path": "findings.pff.json" if pff_payload is not None else None,
            "artifacts": artifacts,
            "notes": payload.get("notes"),
        }
        if manifest_payload["pff_path"] is None:
            del manifest_payload["pff_path"]
    if not isinstance(manifest_payload, dict):
        raise AgentBridgeError("cloud agent manifest field must be a JSON object")
    manifest_path.write_text(
        json.dumps(manifest_payload, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
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


def _agent_prompt(agent_label: str) -> str:
    return (
        f"You are running as {agent_label} under Piranesi's external pentest agent "
        "bridge. Read the scoped context JSON at {context}. Keep all generated files "
        "under {run_dir}. Write normalized findings to Piranesi Finding Format at "
        "{run_dir}/findings.pff.json. Write the agent run manifest to {manifest}. "
        "The manifest scope must match the context scope exactly. If you cannot make "
        "structured findings, write useful notes to stdout and do not invent findings."
    )


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
    "AgentAuthType",
    "AgentAuthorization",
    "AgentBridgeError",
    "AgentCheckResult",
    "AgentCommandRecord",
    "AgentConfigDocument",
    "AgentContextDocument",
    "AgentExecutionResult",
    "AgentExecutionType",
    "AgentIdentity",
    "AgentImportResult",
    "AgentLoginResult",
    "AgentPresetName",
    "AgentProfile",
    "AgentRunManifest",
    "AgentRunMode",
    "available_agent_presets",
    "build_agent_context",
    "build_agent_preset",
    "check_agent_profile",
    "get_agent_profile",
    "import_agent_run_manifest",
    "load_agent_config",
    "load_agent_run_manifest",
    "login_agent_profile",
    "run_agent_command",
    "run_agent_profile",
    "save_agent_config",
    "upsert_agent_profile",
    "write_agent_context",
]
