from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path

from piranesi.models import CandidateFinding
from piranesi.models.finding import VerificationPrecondition
from piranesi.verify.constraints import ExploitTemplate


@dataclass(frozen=True, slots=True)
class VerificationPreconditionEvaluation:
    preconditions: tuple[VerificationPrecondition, ...]
    missing_required: tuple[VerificationPrecondition, ...]
    skip_reason: str | None


def evaluate_verification_preconditions(
    *,
    finding: CandidateFinding,
    template: ExploitTemplate,
    target_dir: Path,
    no_execute: bool,
) -> VerificationPreconditionEvaluation:
    metadata = finding.metadata
    target_has_package = (target_dir / "package.json").is_file()
    payload_keys = [slot.request_key for slot in template.payload_slots]

    preconditions: list[VerificationPrecondition] = []

    user_target_url = _metadata_string(metadata.get("verification_target_url"))
    if user_target_url is not None:
        preconditions.append(
            VerificationPrecondition(
                key="target_url",
                description="Base URL used for dynamic verification requests",
                status="user_provided",
                value=user_target_url,
                source="finding.metadata.verification_target_url",
            )
        )
    elif target_has_package:
        preconditions.append(
            VerificationPrecondition(
                key="target_url",
                description="Base URL used for dynamic verification requests",
                status="inferred",
                value="http://127.0.0.1:{sandbox_port}",
                source="sandbox runtime",
            )
        )
    else:
        preconditions.append(
            VerificationPrecondition(
                key="target_url",
                description="Base URL used for dynamic verification requests",
                status="missing",
                source="target metadata",
                next_step=(
                    "Provide finding.metadata['verification_target_url'] or run verify against a "
                    "target directory with package.json so sandbox URL can be inferred."
                ),
            )
        )

    user_route = _metadata_string(metadata.get("verification_route"))
    inferred_route = template.endpoint.strip()
    if user_route is not None:
        preconditions.append(
            VerificationPrecondition(
                key="route_mapping",
                description="HTTP route for exercising the vulnerable code path",
                status="user_provided",
                value=user_route,
                source="finding.metadata.verification_route",
            )
        )
    elif inferred_route and inferred_route != "/":
        preconditions.append(
            VerificationPrecondition(
                key="route_mapping",
                description="HTTP route for exercising the vulnerable code path",
                status="inferred",
                value=inferred_route,
                source="source snippet route inference",
            )
        )
    else:
        preconditions.append(
            VerificationPrecondition(
                key="route_mapping",
                description="HTTP route for exercising the vulnerable code path",
                status="missing",
                source="source snippet route inference",
                next_step=(
                    "Add finding.metadata['verification_route'] with a concrete endpoint "
                    "or improve route inference context."
                ),
            )
        )

    user_method = _metadata_string(metadata.get("verification_http_method"))
    if user_method is not None:
        preconditions.append(
            VerificationPrecondition(
                key="http_method",
                description="HTTP method required to reach the vulnerable route",
                status="user_provided",
                value=user_method.upper(),
                source="finding.metadata.verification_http_method",
            )
        )
    elif template.http_method.strip():
        preconditions.append(
            VerificationPrecondition(
                key="http_method",
                description="HTTP method required to reach the vulnerable route",
                status="inferred",
                value=template.http_method.upper(),
                source="source snippet route inference",
            )
        )
    else:
        preconditions.append(
            VerificationPrecondition(
                key="http_method",
                description="HTTP method required to reach the vulnerable route",
                status="missing",
                source="source snippet route inference",
                next_step=(
                    "Add finding.metadata['verification_http_method'] with GET/POST/etc."
                ),
            )
        )

    requires_auth = bool(
        _metadata_bool(metadata.get("requires_auth"))
        or _metadata_bool(metadata.get("auth_required"))
    )
    auth_header = _metadata_string(metadata.get("verification_auth_header"))
    cookie = _metadata_string(metadata.get("verification_cookie"))
    if requires_auth and not auth_header and not cookie:
        preconditions.append(
            VerificationPrecondition(
                key="auth_cookies",
                description="Authentication headers/cookies needed by the target route",
                status="missing",
                source="finding metadata",
                next_step=(
                    "Provide finding.metadata['verification_auth_header'] and/or "
                    "finding.metadata['verification_cookie']."
                ),
            )
        )
    elif auth_header or cookie:
        if auth_header and cookie:
            provided = "header+cookie"
        elif auth_header:
            provided = "header"
        else:
            provided = "cookie"
        preconditions.append(
            VerificationPrecondition(
                key="auth_cookies",
                description="Authentication headers/cookies needed by the target route",
                status="user_provided",
                required=requires_auth,
                value=provided,
                source="finding metadata",
            )
        )
    else:
        preconditions.append(
            VerificationPrecondition(
                key="auth_cookies",
                description="Authentication headers/cookies needed by the target route",
                status="satisfied",
                required=False,
                value="not required",
                source="finding metadata",
            )
        )

    if any(slot.carrier == "body" for slot in template.payload_slots):
        preconditions.append(
            VerificationPrecondition(
                key="request_body",
                description="Request payload shape required by the route",
                status="inferred",
                value=", ".join(payload_keys) or "payload",
                source="taint source carrier inference",
            )
        )
    else:
        preconditions.append(
            VerificationPrecondition(
                key="request_body",
                description="Request payload shape required by the route",
                status="satisfied",
                required=False,
                value="not required",
                source="taint source carrier inference",
            )
        )

    if target_has_package:
        preconditions.append(
            VerificationPrecondition(
                key="runtime_service",
                description="Runnable target service for sandbox execution",
                status="inferred",
                value=str(target_dir),
                source="target filesystem",
            )
        )
    else:
        preconditions.append(
            VerificationPrecondition(
                key="runtime_service",
                description="Runnable target service for sandbox execution",
                status="missing",
                source="target filesystem",
                next_step=(
                    "Ensure target directory contains package.json and a start script, "
                    "or supply an explicit verification target URL."
                ),
            )
        )

    if template.network_callbacks_allowed:
        callback_url = _metadata_string(metadata.get("verification_callback_url"))
        if callback_url is None:
            preconditions.append(
                VerificationPrecondition(
                    key="callback_server",
                    description="Out-of-band callback listener used by this template",
                    status="missing",
                    source="finding metadata",
                    next_step=(
                        "Provide finding.metadata['verification_callback_url'] "
                        "for callback tests."
                    ),
                )
            )
        else:
            preconditions.append(
                VerificationPrecondition(
                    key="callback_server",
                    description="Out-of-band callback listener used by this template",
                    status="user_provided",
                    value=callback_url,
                    source="finding.metadata.verification_callback_url",
                )
            )
    else:
        preconditions.append(
            VerificationPrecondition(
                key="callback_server",
                description="Out-of-band callback listener used by this template",
                status="satisfied",
                required=False,
                value="not required",
                source="template defaults",
            )
        )

    if no_execute:
        preconditions.append(
            VerificationPrecondition(
                key="proof_mode",
                description="Execution mode permits sandbox request replay",
                status="user_provided",
                value="no_execute",
                source="CLI flag",
                next_step="Rerun without --no-execute to perform sandbox verification.",
            )
        )
    else:
        preconditions.append(
            VerificationPrecondition(
                key="proof_mode",
                description="Execution mode permits sandbox request replay",
                status="satisfied",
                value="execute",
                source="CLI flag",
            )
        )

    frozen_preconditions = tuple(preconditions)
    missing_required = tuple(
        precondition
        for precondition in frozen_preconditions
        if precondition.required and precondition.status == "missing"
    )

    if no_execute:
        skip_reason = "verification skipped: --no-execute is enabled"
    elif missing_required:
        missing_keys = ", ".join(precondition.key for precondition in missing_required)
        skip_reason = f"verification skipped: missing required preconditions ({missing_keys})"
    else:
        skip_reason = None

    return VerificationPreconditionEvaluation(
        preconditions=frozen_preconditions,
        missing_required=missing_required,
        skip_reason=skip_reason,
    )


def _metadata_string(value: object) -> str | None:
    if not isinstance(value, str):
        return None
    stripped = value.strip()
    return stripped or None


def _metadata_bool(value: object) -> bool:
    return bool(value) if isinstance(value, bool) else False


__all__ = [
    "VerificationPreconditionEvaluation",
    "evaluate_verification_preconditions",
]
