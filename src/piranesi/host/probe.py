"""Adaptive probing: deterministic probe plan generation and safe execution.

Safety invariants:
- Only allowlisted commands can be executed.
- No shell invocation — all commands use argument arrays via subprocess.run.
- Probe plans are JSON-reviewable before execution.
- Unknown probe IDs are rejected at execution time.
"""

from __future__ import annotations

import json
import shutil
import subprocess
from collections.abc import Callable, Sequence
from pathlib import Path
from typing import Protocol

from pydantic import BaseModel, ConfigDict

from piranesi.host.collect import (
    CollectionCommandResult,
    CollectionStatus,
    HostCollectionManifest,
)
from piranesi.host.ingest import HostInputError, load_host_input, redact_auth_value
from piranesi.host.models import (
    FollowupProbe,
    HostFinding,
    HostSnapshot,
    ProbePlan,
)

# ---------------------------------------------------------------------------
# Allowlisted probe templates — the ONLY commands that can be executed
# ---------------------------------------------------------------------------

ALLOWED_PROBES: dict[str, FollowupProbe] = {}


def _register(probe: FollowupProbe) -> FollowupProbe:
    ALLOWED_PROBES[probe.id] = probe
    return probe


# --- SSH follow-ups ---
_register(
    FollowupProbe(
        id="followup.ssh.last_logins",
        reason="Public SSH with password auth — collect recent login history.",
        capability="auth",
        command=["last", "-n", "25"],
        output_name="last_logins",
        risk="read_only",
        redaction_hints=["ip_address", "username"],
    )
)
_register(
    FollowupProbe(
        id="followup.ssh.lastb_failures",
        reason="Public SSH with password auth — collect recent failed login attempts.",
        capability="auth",
        command=["lastb", "-n", "25"],
        output_name="lastb_failures",
        risk="potentially_sensitive",
        redaction_hints=["ip_address", "username"],
    )
)
_register(
    FollowupProbe(
        id="followup.ssh.sshd_effective_config",
        reason="SSH config findings present but sshd -T not collected.",
        capability="sshd_config",
        command=["sshd", "-T"],
        output_name="sshd_effective_config",
        risk="read_only",
    )
)

# --- Redis follow-ups ---
_register(
    FollowupProbe(
        id="followup.redis.process_detail",
        reason="Public Redis — collect process command line and arguments.",
        capability="service",
        osquery=(
            "select p.pid, p.name, p.path, p.cmdline, u.username as user from processes p "
            "left join users u on p.uid = u.uid where p.name = 'redis-server';"
        ),
        output_name="redis_process_detail",
        risk="read_only",
    )
)
_register(
    FollowupProbe(
        id="followup.redis.service_unit",
        reason="Public Redis — collect systemd service unit state.",
        capability="service",
        osquery=(
            "select name, active_state, sub_state, unit_file_state, fragment_path from "
            "systemd_units where name like 'redis%';"
        ),
        output_name="redis_service_unit",
        risk="read_only",
    )
)

# --- Firewall follow-ups ---
_register(
    FollowupProbe(
        id="followup.firewall.ufw_status",
        reason="Firewall evidence missing — try ufw status.",
        capability="firewall",
        command=["ufw", "status", "verbose"],
        output_name="ufw_status",
        risk="read_only",
    )
)
_register(
    FollowupProbe(
        id="followup.firewall.iptables_rules",
        reason="Firewall evidence missing — try iptables -S.",
        capability="firewall",
        command=["iptables", "-S"],
        output_name="iptables_rules",
        risk="read_only",
    )
)
_register(
    FollowupProbe(
        id="followup.firewall.nft_ruleset",
        reason="Firewall evidence missing — try nft list ruleset.",
        capability="firewall",
        command=["nft", "list", "ruleset"],
        output_name="nft_ruleset",
        risk="read_only",
    )
)

# --- Privileged user follow-ups ---
_register(
    FollowupProbe(
        id="followup.identity.sudoers",
        reason="Privileged user found — collect sudoers entries.",
        capability="admin_groups",
        osquery=(
            "select path, label as key, value from augeas where path like '/etc/sudoers%' "
            "and value != '';"
        ),
        output_name="sudoers_entries",
        risk="read_only",
    )
)
_register(
    FollowupProbe(
        id="followup.identity.group_sudo",
        reason="Privileged user found — collect sudo group membership.",
        capability="admin_groups",
        command=["getent", "group", "sudo"],
        output_name="group_sudo",
        risk="read_only",
    )
)
_register(
    FollowupProbe(
        id="followup.identity.group_wheel",
        reason="Privileged user found — collect wheel group membership.",
        capability="admin_groups",
        command=["getent", "group", "wheel"],
        output_name="group_wheel",
        risk="read_only",
    )
)

# --- Database follow-ups ---
_register(
    FollowupProbe(
        id="followup.db.service_unit",
        reason="Public database port — collect systemd service unit state.",
        capability="service",
        osquery=(
            "select name, active_state, sub_state, unit_file_state, fragment_path from "
            "systemd_units where name like '%sql%' or name like '%mongo%' or name like "
            "'%elastic%';"
        ),
        output_name="db_service_unit",
        risk="read_only",
    )
)


# ---------------------------------------------------------------------------
# Deterministic probe plan generation
# ---------------------------------------------------------------------------


class ProbeGenerationError(RuntimeError):
    """Raised when probe plan generation fails."""


def generate_probe_plan(
    snapshot: HostSnapshot,
    findings: list[HostFinding],
    *,
    base_input: str | Path | None = None,
) -> ProbePlan:
    probes: list[FollowupProbe] = []
    seen_ids: set[str] = set()
    has_sshd_config = bool(snapshot.raw_evidence.get("commands", {}).get("sshd_effective_config"))  # type: ignore[union-attr]
    has_firewall = _has_firewall_evidence(snapshot)
    has_sudoers = bool(snapshot.raw_evidence.get("osquery", {}).get("sudoers"))  # type: ignore[union-attr]
    has_admin_groups = any(
        k in (snapshot.raw_evidence.get("commands", {}) or {})  # type: ignore[union-attr]
        for k in ("group_sudo", "group_admin", "group_wheel")
    )

    for finding in findings:
        if finding.suppressed:
            continue

        # SSH exposure + password auth -> auth probes
        if finding.rule_id in {"host.listener.ssh_public", "host.ssh.password_authentication"}:
            _add_probe(probes, seen_ids, "followup.ssh.last_logins", [finding.id])
            _add_probe(probes, seen_ids, "followup.ssh.lastb_failures", [finding.id])
            if not has_sshd_config:
                _add_probe(probes, seen_ids, "followup.ssh.sshd_effective_config", [finding.id])

        # High-risk service: Redis
        if finding.rule_id == "host.listener.high_risk_service" and "Redis" in finding.title:
            _add_probe(probes, seen_ids, "followup.redis.process_detail", [finding.id])
            _add_probe(probes, seen_ids, "followup.redis.service_unit", [finding.id])

        # High-risk service: database ports
        if finding.rule_id == "host.listener.high_risk_service" and any(
            db in finding.title for db in ("MySQL", "PostgreSQL", "MongoDB", "Elasticsearch")
        ):
            _add_probe(probes, seen_ids, "followup.db.service_unit", [finding.id])

        # Missing firewall evidence
        if (
            finding.rule_id == "host.coverage.missing_evidence"
            and "firewall" in (finding.affected_component or "").lower()
            and not has_firewall
        ):
            _add_probe(probes, seen_ids, "followup.firewall.ufw_status", [finding.id])
            _add_probe(probes, seen_ids, "followup.firewall.iptables_rules", [finding.id])
            _add_probe(probes, seen_ids, "followup.firewall.nft_ruleset", [finding.id])

        # Firewall inactive with public listeners
        if finding.rule_id == "host.firewall.inactive" and not has_firewall:
            _add_probe(probes, seen_ids, "followup.firewall.ufw_status", [finding.id])
            _add_probe(probes, seen_ids, "followup.firewall.iptables_rules", [finding.id])
            _add_probe(probes, seen_ids, "followup.firewall.nft_ruleset", [finding.id])

        # Privileged user
        if finding.rule_id == "host.identity.privileged_user":
            if not has_sudoers:
                _add_probe(probes, seen_ids, "followup.identity.sudoers", [finding.id])
            if not has_admin_groups:
                _add_probe(probes, seen_ids, "followup.identity.group_sudo", [finding.id])
                _add_probe(probes, seen_ids, "followup.identity.group_wheel", [finding.id])

    return ProbePlan(
        target=snapshot.identity.hostname,
        base_input=_resolved_base_input(base_input),
        probes=probes,
    )


def _add_probe(
    probes: list[FollowupProbe],
    seen: set[str],
    probe_id: str,
    finding_ids: list[str],
) -> None:
    if probe_id in seen:
        # merge finding_ids into existing probe
        for p in probes:
            if p.id == probe_id:
                for fid in finding_ids:
                    if fid not in p.finding_ids:
                        p.finding_ids.append(fid)
                return
        return
    template = ALLOWED_PROBES.get(probe_id)
    if template is None:
        return
    probe = template.model_copy(deep=True)
    probe.finding_ids = list(finding_ids)
    probes.append(probe)
    seen.add(probe_id)


def _has_firewall_evidence(snapshot: HostSnapshot) -> bool:
    commands = snapshot.raw_evidence.get("commands")
    if not isinstance(commands, dict):
        return False
    return any(k in commands for k in ("ufw_status", "iptables_rules", "nft_ruleset"))


# ---------------------------------------------------------------------------
# Safe probe executor
# ---------------------------------------------------------------------------


class ProbeExecutionError(RuntimeError):
    """Raised when probe execution encounters a safety violation."""


class ProbeExecutorRunner(Protocol):
    def __call__(
        self,
        args: Sequence[str],
        *,
        capture_output: bool,
        text: bool,
        timeout: int,
    ) -> subprocess.CompletedProcess[str]:
        """Run a command and return a completed process."""


class ProbeExecutionResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    plan: ProbePlan
    output_dir: str
    manifest_path: str
    executed: int = 0
    skipped: int = 0
    failed: int = 0
    rejected: int = 0
    manifest: HostCollectionManifest


def execute_probe_plan(
    plan: ProbePlan,
    output_dir: str | Path,
    *,
    timeout_seconds: int = 30,
    executable_lookup: Callable[[str], str | None] = shutil.which,
    command_runner: ProbeExecutorRunner = subprocess.run,  # type: ignore[assignment]
) -> ProbeExecutionResult:
    out = Path(output_dir).expanduser().resolve(strict=False)
    out.mkdir(parents=True, exist_ok=True)
    followup_dir = out / "raw" / "followup"
    followup_dir.mkdir(parents=True, exist_ok=True)
    commands_dir = out / "raw" / "commands"
    osquery_dir = out / "raw" / "osquery"

    _merge_base_input(plan.base_input, out)

    # write probe plan for auditability
    (followup_dir / "probe-plan.json").write_text(plan.model_dump_json(indent=2), encoding="utf-8")

    manifest = HostCollectionManifest(
        output_dir=str(out),
        raw_dir=str(out / "raw"),
    )

    executed = 0
    skipped = 0
    failed = 0
    rejected = 0

    for probe in plan.probes:
        # SAFETY: reject any probe not in the allowlist
        if probe.id not in ALLOWED_PROBES:
            rejected += 1
            manifest.commands.append(
                CollectionCommandResult(
                    tool="followup",
                    name=probe.output_name,
                    command=probe.command or [],
                    status="failed",
                    stderr=f"REJECTED: probe {probe.id} is not in the allowlist",
                )
            )
            continue

        allowed = ALLOWED_PROBES[probe.id]

        # SAFETY: verify the command matches the allowlisted template exactly
        if probe.command is not None and probe.command != allowed.command:
            rejected += 1
            manifest.commands.append(
                CollectionCommandResult(
                    tool="followup",
                    name=probe.output_name,
                    command=probe.command,
                    status="failed",
                    stderr=f"REJECTED: probe {probe.id} command does not match allowlist",
                )
            )
            continue

        if probe.osquery is not None and probe.osquery != allowed.osquery:
            rejected += 1
            manifest.commands.append(
                CollectionCommandResult(
                    tool="followup",
                    name=probe.output_name,
                    status="failed",
                    stderr=f"REJECTED: probe {probe.id} osquery does not match allowlist",
                )
            )
            continue

        # execute the probe
        if probe.command is not None:
            result = _execute_command_probe(
                probe,
                followup_dir,
                commands_dir,
                executable_lookup,
                command_runner,
                timeout_seconds,
                manifest,
            )
        elif probe.osquery is not None:
            result = _execute_osquery_probe(
                probe,
                followup_dir,
                osquery_dir,
                executable_lookup,
                command_runner,
                timeout_seconds,
                manifest,
            )
        else:
            skipped += 1
            manifest.commands.append(
                CollectionCommandResult(
                    tool="followup",
                    name=probe.output_name,
                    status="skipped",
                    stderr="probe has no command or osquery query",
                )
            )
            continue

        if result == "ok":
            executed += 1
        elif result == "missing":
            skipped += 1
        else:
            failed += 1

    # write probe results summary
    (followup_dir / "probe-results.json").write_text(
        json.dumps(
            {
                "executed": executed,
                "skipped": skipped,
                "failed": failed,
                "rejected": rejected,
                "total": len(plan.probes),
            },
            indent=2,
        ),
        encoding="utf-8",
    )

    _write_reassessable_snapshot(out, manifest)

    manifest_path = out / "collection-manifest.json"
    manifest_path.write_text(manifest.model_dump_json(indent=2), encoding="utf-8")

    return ProbeExecutionResult(
        plan=plan,
        output_dir=str(out),
        manifest_path=str(manifest_path),
        executed=executed,
        skipped=skipped,
        failed=failed,
        rejected=rejected,
        manifest=manifest,
    )


def _execute_command_probe(
    probe: FollowupProbe,
    followup_dir: Path,
    commands_dir: Path,
    executable_lookup: Callable[[str], str | None],
    command_runner: ProbeExecutorRunner,
    timeout_seconds: int,
    manifest: HostCollectionManifest,
) -> CollectionStatus:
    assert probe.command is not None
    executable = executable_lookup(probe.command[0])
    if executable is None:
        manifest.commands.append(
            CollectionCommandResult(
                tool="followup",
                name=probe.output_name,
                command=probe.command,
                status="missing",
                stderr=f"{probe.command[0]} not found on PATH",
            )
        )
        return "missing"
    resolved = [executable, *probe.command[1:]]
    try:
        completed = command_runner(
            resolved,
            capture_output=True,
            text=True,
            timeout=timeout_seconds,
        )
    except subprocess.TimeoutExpired:
        manifest.commands.append(
            CollectionCommandResult(
                tool="followup",
                name=probe.output_name,
                command=resolved,
                status="timeout",
                stderr=f"timed out after {timeout_seconds}s",
            )
        )
        return "timeout"
    output_file = commands_dir / f"{probe.output_name}.json"
    output_file.parent.mkdir(parents=True, exist_ok=True)
    stdout = _safe_probe_stdout(probe, completed.stdout)
    payload = {
        "command": resolved,
        "stdout": stdout,
        "stderr": _compact_probe_stderr(completed.stderr),
        "exit_code": completed.returncode,
    }
    output_file.write_text(json.dumps(payload, indent=2), encoding="utf-8")
    (followup_dir / f"{probe.output_name}.json").write_text(
        json.dumps(payload, indent=2),
        encoding="utf-8",
    )
    status: CollectionStatus = "ok" if completed.returncode == 0 else "failed"
    manifest.commands.append(
        CollectionCommandResult(
            tool="followup",
            name=probe.output_name,
            command=resolved,
            status=status,
            exit_code=completed.returncode,
            output_file=str(output_file),
            stderr=completed.stderr[:200] if completed.stderr else None,
        )
    )
    return status


def _execute_osquery_probe(
    probe: FollowupProbe,
    followup_dir: Path,
    osquery_dir: Path,
    executable_lookup: Callable[[str], str | None],
    command_runner: ProbeExecutorRunner,
    timeout_seconds: int,
    manifest: HostCollectionManifest,
) -> CollectionStatus:
    assert probe.osquery is not None
    osqueryi = executable_lookup("osqueryi")
    if osqueryi is None:
        manifest.commands.append(
            CollectionCommandResult(
                tool="followup",
                name=probe.output_name,
                status="missing",
                stderr="osqueryi not found on PATH",
            )
        )
        return "missing"
    command = [osqueryi, "--json", probe.osquery]
    try:
        completed = command_runner(
            command,
            capture_output=True,
            text=True,
            timeout=timeout_seconds,
        )
    except subprocess.TimeoutExpired:
        manifest.commands.append(
            CollectionCommandResult(
                tool="followup",
                name=probe.output_name,
                command=command,
                status="timeout",
                stderr=f"timed out after {timeout_seconds}s",
            )
        )
        return "timeout"
    output_file = osquery_dir / f"{probe.output_name}.json"
    output_file.parent.mkdir(parents=True, exist_ok=True)
    output_file.write_text(completed.stdout or "[]", encoding="utf-8")
    (followup_dir / f"{probe.output_name}.json").write_text(
        completed.stdout or "[]",
        encoding="utf-8",
    )
    status: CollectionStatus = "ok" if completed.returncode == 0 else "failed"
    manifest.commands.append(
        CollectionCommandResult(
            tool="followup",
            name=probe.output_name,
            command=command,
            status=status,
            exit_code=completed.returncode,
            output_file=str(output_file),
            stderr=completed.stderr[:200] if completed.stderr else None,
        )
    )
    return status


def _resolved_base_input(base_input: str | Path | None) -> str | None:
    if base_input is None:
        return None
    return str(Path(base_input).expanduser().resolve(strict=False))


def _merge_base_input(base_input: str | None, output_dir: Path) -> None:
    if not base_input:
        return
    base = Path(base_input).expanduser().resolve(strict=False)
    if not base.exists():
        return
    if base.is_file():
        if base.name == "host_snapshot.json":
            shutil.copy2(base, output_dir / "host_snapshot.json")
        return
    for evidence_name in ("osquery", "trivy", "commands", "lynis", "openscap"):
        source = _base_evidence_dir(base, evidence_name)
        if source is None:
            continue
        destination = output_dir / "raw" / evidence_name
        try:
            if source.resolve(strict=False) == destination.resolve(strict=False):
                continue
        except OSError:
            pass
        shutil.copytree(source, destination, dirs_exist_ok=True)


def _base_evidence_dir(base: Path, name: str) -> Path | None:
    for candidate in (base / "raw" / name, base / name):
        if candidate.is_dir():
            return candidate
    return None


def _write_reassessable_snapshot(output_dir: Path, manifest: HostCollectionManifest) -> None:
    try:
        snapshot = load_host_input(output_dir)
    except HostInputError:
        return
    snapshot_path = output_dir / "host_snapshot.json"
    snapshot_path.write_text(snapshot.model_dump_json(indent=2), encoding="utf-8")
    manifest.snapshot_file = str(snapshot_path)


def _safe_probe_stdout(probe: FollowupProbe, stdout: str | None) -> str:
    rendered = stdout or ""
    if probe.capability == "auth" or "auth" in probe.redaction_hints:
        rendered = redact_auth_value(rendered)
    max_lines = 120 if probe.capability == "auth" else 500
    max_chars = 16_000 if probe.capability == "auth" else 64_000
    lines = rendered.splitlines()
    if len(lines) > max_lines:
        rendered = "\n".join(lines[:max_lines])
        rendered += f"\n[TRUNCATED after {max_lines} lines]"
    if len(rendered) > max_chars:
        rendered = rendered[:max_chars] + f"\n[TRUNCATED after {max_chars} characters]"
    return rendered


def _compact_probe_stderr(stderr: str | None) -> str | None:
    if not stderr:
        return None
    rendered = redact_auth_value(" ".join(stderr.split()))
    return rendered[:500] if rendered else None
