from __future__ import annotations

from collections.abc import Mapping, Sequence
from dataclasses import dataclass

from piranesi.models import AttackSurfaceNode, CandidateFinding, ConfirmedFinding, EntryPoint


@dataclass(frozen=True)
class DreadScore:
    damage: int
    reproducibility: int
    exploitability: int
    affected_users: int
    discoverability: int

    @property
    def total(self) -> int:
        return (
            self.damage
            + self.reproducibility
            + self.exploitability
            + self.affected_users
            + self.discoverability
        )

    @property
    def normalized(self) -> float:
        return round(self.total / 5.0, 1)

    @property
    def risk_level(self) -> str:
        normalized = self.normalized
        if normalized >= 8.0:
            return "critical"
        if normalized >= 6.0:
            return "high"
        if normalized >= 4.0:
            return "medium"
        return "low"


@dataclass(frozen=True)
class ExposureContext:
    entry_point: EntryPoint | None
    attack_surface: AttackSurfaceNode | None
    is_public_route: bool
    requires_auth: bool
    is_admin_route: bool
    is_documented_route: bool
    is_websocket_or_event: bool
    crosses_trust_boundary: bool


def score_dread(
    finding: CandidateFinding,
    *,
    entry_points: Sequence[EntryPoint] | None = None,
    attack_surface: Sequence[AttackSurfaceNode] | None = None,
    verification_result: object | None = None,
) -> DreadScore:
    context = _exposure_context(
        finding,
        entry_points=entry_points or (),
        attack_surface=attack_surface or (),
    )
    damage = _damage_score(finding)
    reproducibility = _reproducibility_score(finding, verification_result)
    exploitability = _exploitability_score(finding, context)
    affected_users = _affected_users_score(finding, context)
    discoverability = _discoverability_score(finding, context)
    return DreadScore(
        damage=_clamp_dimension(damage),
        reproducibility=_clamp_dimension(reproducibility),
        exploitability=_clamp_dimension(exploitability),
        affected_users=_clamp_dimension(affected_users),
        discoverability=_clamp_dimension(discoverability),
    )


def score_all(
    findings: Sequence[CandidateFinding],
    *,
    entry_points: Sequence[EntryPoint] | None = None,
    attack_surface: Sequence[AttackSurfaceNode] | None = None,
    verification_results: dict[str, object] | None = None,
) -> dict[str, DreadScore]:
    results = verification_results or {}
    return {
        finding.id: score_dread(
            finding,
            entry_points=entry_points,
            attack_surface=attack_surface,
            verification_result=results.get(finding.id),
        )
        for finding in findings
    }


def _damage_score(finding: CandidateFinding) -> int:
    severity = _normalize_severity(finding.severity)
    sensitive = _has_sensitive_data(finding)
    if severity == "critical":
        return 10 if sensitive else 9
    if severity == "high":
        return 8 if sensitive else 7
    if severity == "medium":
        return 6 if sensitive else 5
    if severity == "low":
        return 3
    return 1


def _reproducibility_score(finding: CandidateFinding, verification_result: object | None) -> int:
    if _is_confirmed(verification_result):
        return 10

    confidence = _verification_confidence(verification_result)
    if confidence is None:
        confidence = finding.confidence
    if confidence >= 0.9:
        return 8
    if confidence >= 0.7:
        return 7
    if confidence >= 0.5:
        return 5
    return 3


def _exploitability_score(finding: CandidateFinding, context: ExposureContext) -> int:
    if finding.source.source_type == "dependency_manifest":
        score = 4
    elif finding.path_conditions:
        score = 3
    elif _is_direct_input_to_sink(finding):
        score = (7 if context.requires_auth else 10) if context.is_public_route else 6
    elif len(finding.taint_path) > 3:
        score = 5
    else:
        score = 6

    epss_score = _metadata_float(finding.metadata, "epss_score")
    if epss_score is not None and epss_score >= 0.5:
        score += 2

    exploit_status = str(finding.metadata.get("exploit_status", "")).lower()
    if exploit_status in {"in_the_wild", "weaponized"}:
        score += 3
    return min(score, 10)


def _affected_users_score(finding: CandidateFinding, context: ExposureContext) -> int:
    if finding.source.source_type == "dependency_manifest":
        return 2 if _is_dev_dependency(finding) else 8
    if context.is_admin_route:
        return 3
    if context.is_public_route and context.requires_auth:
        return 7
    if context.is_public_route:
        return 10
    return 2


def _discoverability_score(finding: CandidateFinding, context: ExposureContext) -> int:
    source_file = finding.source.location.file.lower()
    if source_file.endswith(".min.js") or ".min." in source_file:
        score = 2
    elif context.is_public_route and context.is_documented_route:
        score = 10
    elif context.is_public_route:
        score = 8
    elif context.is_websocket_or_event:
        score = 6
    elif context.entry_point is not None:
        score = 5
    elif len(finding.taint_path) > 3:
        score = 3
    else:
        score = 3

    if context.crosses_trust_boundary:
        score += 1
    return min(score, 10)


def _exposure_context(
    finding: CandidateFinding,
    *,
    entry_points: Sequence[EntryPoint],
    attack_surface: Sequence[AttackSurfaceNode],
) -> ExposureContext:
    matched_surface = _match_attack_surface(finding, attack_surface)
    matched_entry = _match_entry_point(finding, entry_points, matched_surface)
    is_public_route = matched_entry is not None and matched_entry.route_pattern is not None
    requires_auth = _requires_auth(matched_entry)
    is_admin_route = _is_admin_route(matched_entry)
    is_documented_route = bool(finding.metadata.get("openapi_documented"))
    is_websocket_or_event = (
        "websocket" in finding.source.source_type.lower()
        or "event" in finding.source.source_type.lower()
        or (
            matched_entry is not None
            and matched_entry.kind in {"websocket_handler", "event_handler"}
        )
    )
    crosses_trust_boundary = bool(finding.metadata.get("crosses_trust_boundary")) or requires_auth
    return ExposureContext(
        entry_point=matched_entry,
        attack_surface=matched_surface,
        is_public_route=is_public_route,
        requires_auth=requires_auth,
        is_admin_route=is_admin_route,
        is_documented_route=is_documented_route,
        is_websocket_or_event=is_websocket_or_event,
        crosses_trust_boundary=crosses_trust_boundary,
    )


def _match_entry_point(
    finding: CandidateFinding,
    entry_points: Sequence[EntryPoint],
    matched_surface: AttackSurfaceNode | None,
) -> EntryPoint | None:
    source_function_id = finding.metadata.get("source_function_id")
    if isinstance(source_function_id, str):
        for entry_point in entry_points:
            if entry_point.function_id == source_function_id:
                return entry_point

    if matched_surface is not None:
        for entry_point in entry_points:
            if entry_point.function_id == matched_surface.function_id:
                return entry_point

    same_file = [
        entry_point
        for entry_point in entry_points
        if entry_point.location.file == finding.source.location.file
    ]
    if len(same_file) == 1:
        return same_file[0]
    for entry_point in same_file:
        if entry_point.location.line == finding.source.location.line:
            return entry_point
    return None


def _match_attack_surface(
    finding: CandidateFinding,
    attack_surface: Sequence[AttackSurfaceNode],
) -> AttackSurfaceNode | None:
    for node in attack_surface:
        if (
            node.location.file == finding.source.location.file
            and node.location.line == finding.source.location.line
            and node.source_type == finding.source.source_type
        ):
            return node
    for node in attack_surface:
        if (
            node.location.file == finding.source.location.file
            and node.source_type == finding.source.source_type
        ):
            return node
    return None


def _normalize_severity(value: str) -> str:
    normalized = value.strip().lower()
    if normalized in {"critical", "high", "medium", "low", "informational", "info"}:
        return "informational" if normalized in {"informational", "info"} else normalized
    return "medium"


def _has_sensitive_data(finding: CandidateFinding) -> bool:
    markers = {
        *(category.lower() for category in finding.source.data_categories),
        (finding.source.parameter_name or "").lower(),
        finding.source.source_type.lower(),
        str(finding.metadata.get("title", "")).lower(),
    }
    sensitive_tokens = (
        "pii",
        "financial",
        "payment",
        "card",
        "credit",
        "health",
        "medical",
        "ssn",
        "nric",
        "passport",
        "token",
        "secret",
        "credential",
        "email",
        "phone",
        "address",
        "salary",
    )
    return any(token in marker for marker in markers for token in sensitive_tokens if marker)


def _is_confirmed(verification_result: object | None) -> bool:
    if verification_result is None:
        return False
    if isinstance(verification_result, ConfirmedFinding):
        return verification_result.sandbox_result.confirmed
    sandbox_result = getattr(verification_result, "sandbox_result", None)
    if sandbox_result is not None and getattr(sandbox_result, "confirmed", False):
        return True
    if isinstance(verification_result, Mapping):
        sandbox_payload = verification_result.get("sandbox_result")
        if isinstance(sandbox_payload, Mapping) and sandbox_payload.get("confirmed") is True:
            return True
        return bool(verification_result.get("confirmed"))
    return False


def _verification_confidence(verification_result: object | None) -> float | None:
    if verification_result is None:
        return None
    if isinstance(verification_result, Mapping):
        return _coerce_float(verification_result.get("confidence"))
    for attribute in ("ensemble_score", "confidence"):
        value = getattr(verification_result, attribute, None)
        coerced = _coerce_float(value)
        if coerced is not None:
            return coerced
    return None


def _is_direct_input_to_sink(finding: CandidateFinding) -> bool:
    return len(finding.taint_path) <= 1 and not finding.path_conditions


def _requires_auth(entry_point: EntryPoint | None) -> bool:
    if entry_point is None:
        return False
    auth_markers = (
        "auth",
        "authenticate",
        "authorization",
        "passport",
        "jwt",
        "session",
        "guard",
        "requirelogin",
        "requireuser",
        "require_admin",
    )
    lowered = " ".join(
        [*entry_point.middleware, entry_point.function_id, entry_point.route_pattern or ""]
    ).lower()
    return any(marker in lowered for marker in auth_markers)


def _is_admin_route(entry_point: EntryPoint | None) -> bool:
    if entry_point is None:
        return False
    lowered = " ".join(
        [*entry_point.middleware, entry_point.function_id, entry_point.route_pattern or ""]
    ).lower()
    return any(token in lowered for token in ("admin", "root", "superuser", "privileged"))


def _is_dev_dependency(finding: CandidateFinding) -> bool:
    for key in ("dev_dependency", "development_only"):
        value = finding.metadata.get(key)
        if isinstance(value, bool):
            return value
    scope = str(finding.metadata.get("dependency_scope", "")).lower()
    return scope in {"dev", "development", "test"}


def _metadata_float(metadata: Mapping[str, object], key: str) -> float | None:
    return _coerce_float(metadata.get(key))


def _coerce_float(value: object) -> float | None:
    if isinstance(value, bool):
        return None
    if isinstance(value, (float, int)):
        return float(value)
    if isinstance(value, str):
        try:
            return float(value)
        except ValueError:
            return None
    return None


def _clamp_dimension(value: int) -> int:
    return max(1, min(value, 10))


__all__ = [
    "DreadScore",
    "score_all",
    "score_dread",
]
