from __future__ import annotations

from dataclasses import dataclass
from typing import Literal

from piranesi.advisory.db import AdvisoryDBStatus

TrustPolicyMode = Literal["permissive", "verified-only"]
PolicyAction = Literal["ignore", "warn", "fail"]


@dataclass(frozen=True)
class AdvisoryPolicyOutcome:
    mode: TrustPolicyMode
    allowed: bool
    freshness: str
    trust_state: str
    violations: tuple[str, ...]
    warnings: tuple[str, ...]

    def to_dict(self) -> dict[str, object]:
        return {
            "mode": self.mode,
            "allowed": self.allowed,
            "freshness": self.freshness,
            "trust_state": self.trust_state,
            "violations": list(self.violations),
            "warnings": list(self.warnings),
        }


def evaluate_trust_policy(
    status: AdvisoryDBStatus,
    *,
    mode: TrustPolicyMode = "permissive",
    on_missing: PolicyAction = "warn",
    on_stale: PolicyAction = "warn",
    on_unsigned: PolicyAction = "warn",
) -> AdvisoryPolicyOutcome:
    warnings: list[str] = []
    violations: list[str] = []

    if mode == "verified-only" and status.trust_state != "verified":
        violations.append(
            "verified-only policy requires advisory trust_state=verified "
            f"(got {status.trust_state})"
        )

    if status.freshness == "missing":
        _apply_action(
            action=on_missing,
            message="advisory database is missing",
            warnings=warnings,
            violations=violations,
        )
    elif status.freshness == "stale":
        _apply_action(
            action=on_stale,
            message="advisory database is stale",
            warnings=warnings,
            violations=violations,
        )

    if status.trust_state in {"unsigned", "unverified", "unknown"}:
        _apply_action(
            action=on_unsigned,
            message=(
                "advisory snapshot is not cryptographically verified "
                f"(trust_state={status.trust_state})"
            ),
            warnings=warnings,
            violations=violations,
        )

    return AdvisoryPolicyOutcome(
        mode=mode,
        allowed=not violations,
        freshness=status.freshness,
        trust_state=status.trust_state,
        violations=tuple(violations),
        warnings=tuple(warnings),
    )


def _apply_action(
    *,
    action: PolicyAction,
    message: str,
    warnings: list[str],
    violations: list[str],
) -> None:
    if action == "ignore":
        return
    if action == "warn":
        warnings.append(message)
        return
    violations.append(message)
