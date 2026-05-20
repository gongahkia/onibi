from __future__ import annotations

from collections.abc import Sequence
from dataclasses import dataclass
from typing import Any, Literal

from piranesi.rescan.extractors import ReplaySpec

NETWORK_POLICY_SCHEMA_VERSION: Literal["piranesi.rescan-network-policy.v1"] = (
    "piranesi.rescan-network-policy.v1"
)


class NetworkPolicyError(ValueError):
    """Raised when replay scope cannot be bounded to baseline evidence."""


@dataclass(frozen=True, slots=True)
class NetworkPolicy:
    enforcement_mode: Literal[
        "blocked-no-egress-enforcement",
        "explicitly-unenforced-docker-default",
    ]
    allowed_destinations: tuple[str, ...]
    command_destinations: tuple[str, ...]
    override_required: bool
    execution_allowed: bool
    reason: str

    def as_dict(self) -> dict[str, Any]:
        return {
            "schema_version": NETWORK_POLICY_SCHEMA_VERSION,
            "enforcement_mode": self.enforcement_mode,
            "allowed_destinations": list(self.allowed_destinations),
            "command_destinations": list(self.command_destinations),
            "override_required": self.override_required,
            "execution_allowed": self.execution_allowed,
            "reason": self.reason,
        }


def derive_network_policy(
    specs: Sequence[ReplaySpec],
    *,
    allow_unenforced_network: bool,
) -> NetworkPolicy:
    allowed: set[str] = set()
    command_destinations: set[str] = set()
    for spec in specs:
        if not spec.target_scope:
            raise NetworkPolicyError(f"{spec.tool} replay does not include recovered target scope")
        allowed.update(spec.target_scope)
        command_destinations.update(_command_destinations(spec))

    outside_scope = sorted(command_destinations - allowed)
    if outside_scope:
        formatted = ", ".join(outside_scope)
        raise NetworkPolicyError(
            f"recovered replay command expands beyond baseline target scope: {formatted}"
        )

    allowed_destinations = tuple(sorted(allowed))
    command_scope = tuple(sorted(command_destinations))
    if allow_unenforced_network:
        return NetworkPolicy(
            enforcement_mode="explicitly-unenforced-docker-default",
            allowed_destinations=allowed_destinations,
            command_destinations=command_scope,
            override_required=True,
            execution_allowed=True,
            reason=(
                "Docker egress allowlisting is not enforced by this runtime; user explicitly "
                "acknowledged Docker default network behavior."
            ),
        )
    return NetworkPolicy(
        enforcement_mode="blocked-no-egress-enforcement",
        allowed_destinations=allowed_destinations,
        command_destinations=command_scope,
        override_required=True,
        execution_allowed=False,
        reason=(
            "Replay is blocked because Docker egress allowlisting is not enforced by this runtime. "
            "Inspect the recovered scope with --dry-run or pass --allow-unenforced-network to "
            "record an explicit override."
        ),
    )


def _command_destinations(spec: ReplaySpec) -> set[str]:
    if spec.tool == "nmap":
        return _nmap_command_destinations(spec.recovered_command)
    if spec.tool == "nuclei":
        return _nuclei_command_destinations(spec.recovered_command)
    return set()


def _nmap_command_destinations(command: Sequence[str]) -> set[str]:
    destinations: set[str] = set()
    options_with_values = {
        "-iL",
        "-oA",
        "-oG",
        "-oN",
        "-oS",
        "-oX",
        "-p",
        "-p-",
        "--datadir",
        "--dns-servers",
        "--exclude",
        "--excludefile",
        "--max-rate",
        "--min-rate",
        "--script",
        "--script-args",
        "--top-ports",
    }
    index = 1
    while index < len(command):
        token = command[index]
        if token in options_with_values:
            index += 2
            continue
        if token.startswith("-"):
            index += 1
            continue
        destinations.add(token)
        index += 1
    return destinations


def _nuclei_command_destinations(command: Sequence[str]) -> set[str]:
    destinations: set[str] = set()
    target_flags = {"-u", "-target", "-url"}
    index = 1
    while index < len(command):
        arg = command[index]
        if arg in target_flags:
            if index + 1 < len(command):
                destinations.add(command[index + 1])
            index += 2
            continue
        if arg == "-l":
            raise NetworkPolicyError(
                "nuclei replay uses a target list file that cannot be bounded by recovered scope"
            )
        index += 1
    return destinations


__all__ = [
    "NETWORK_POLICY_SCHEMA_VERSION",
    "NetworkPolicy",
    "NetworkPolicyError",
    "derive_network_policy",
]
