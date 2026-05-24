from __future__ import annotations

import json
import re
from dataclasses import dataclass, field
from hashlib import sha256
from pathlib import Path
from typing import Any
from urllib.parse import parse_qsl, urlencode, urljoin, urlsplit, urlunsplit

from piranesi.models import CandidateFinding
from piranesi.models.finding import (
    VerificationBodyExcerpt,
    VerificationEvidence,
    VerificationRedactionStatus,
    VerificationResponseDiffSummary,
    VerificationTimingSummary,
)
from piranesi.verify.sandbox import ExploitResult, SandboxCapture, SynthesizedPayload

_SENSITIVE_KEY_TOKENS = (
    "auth",
    "authorization",
    "cookie",
    "credential",
    "key",
    "passwd",
    "password",
    "secret",
    "session",
    "token",
)
_SENSITIVE_HEADER_NAMES = frozenset(
    {
        "authorization",
        "cookie",
        "proxy-authorization",
        "set-cookie",
        "x-api-key",
        "x-auth-token",
    }
)
_ALLOWED_HEADER_NAMES = frozenset(
    {
        "accept",
        "cache-control",
        "content-length",
        "content-type",
        "date",
        "etag",
        "location",
        "server",
        "x-correlation-id",
        "x-request-id",
        "x-trace-id",
        "x-powered-by",
    }
)
_INLINE_SECRET_PATTERNS: tuple[tuple[re.Pattern[str], str], ...] = (
    (
        re.compile(
            r"""(?ix)
            (?P<prefix>
                (?:"|')?
                [\w.-]*
                (?:authorization|cookie|api[_-]?key|access[_-]?token|password|passwd|secret|session(?:id)?|token)
                [\w.-]*
                (?:"|')?
                \s*:\s*
                (?:"|')
            )
            (?P<value>[^"'\r\n]+)
            (?P<suffix>(?:"|'))
            """
        ),
        r"\g<prefix>[REDACTED]\g<suffix>",
    ),
    (
        re.compile(
            r"""(?ix)
            (?P<prefix>
                (?:"|')?
                [\w.-]*
                (?:api[_-]?key|access[_-]?token|password|passwd|secret|session(?:id)?|token)
                [\w.-]*
                (?:"|')?
                \s*=\s*
                (?:"|')
            )
            (?P<value>[^"'\r\n]+)
            (?P<suffix>(?:"|'))
            """
        ),
        r"\g<prefix>[REDACTED]\g<suffix>",
    ),
    (
        re.compile(r"(?i)(?P<prefix>\bauthorization\s*[:=]\s*)(?P<value>[^\s,;]+(?:\s+[^\s,;]+)?)"),
        r"\g<prefix>[REDACTED]",
    ),
    (
        re.compile(r"(?i)(?P<prefix>\bcookie\s*[:=]\s*)(?P<value>[^\n\r]+)"),
        r"\g<prefix>[REDACTED]",
    ),
    (
        re.compile(
            r"(?i)(?P<prefix>\b(?:api[_-]?key|password|passwd|secret|session|token)\b\s*[:=]\s*)(?P<value>[^\s,;]+)"
        ),
        r"\g<prefix>[REDACTED]",
    ),
)
_ERROR_SIGNATURE_PATTERN = re.compile(r"\b[A-Z][A-Z0-9_:-]{2,}(?:\(\d+\))?\b")
_SCREENSHOT_SUFFIXES = {".png", ".jpg", ".jpeg", ".webp"}
_UNSAFE_ARTIFACT_STEM_PATTERN = re.compile(r"[^A-Za-z0-9._-]+")
_PREVIEW_LENGTH = 240
_FULL_BODY_LENGTH = 8192


@dataclass(slots=True)
class _RedactionTracker:
    redacted_fields: set[str] = field(default_factory=set)
    redacted_value_count: int = 0

    def mark(self, field_name: str, count: int = 1) -> None:
        if count <= 0:
            return
        self.redacted_fields.add(field_name)
        self.redacted_value_count += count


def build_verification_evidence(
    *,
    finding: CandidateFinding,
    template_id: str | None,
    payload: SynthesizedPayload | None,
    base_url: str | None,
    baseline_response: ExploitResult | None,
    exploit_response: ExploitResult | None,
    baseline_capture: SandboxCapture | None,
    exploit_capture: SandboxCapture | None,
    reason: str,
    evidence: list[str],
    error_text: str | None,
) -> tuple[VerificationEvidence, str, list[str], str | None, dict[str, object]]:
    tracker = _RedactionTracker()
    sensitive_values = _collect_sensitive_values(finding)

    sanitized_reason = _sanitize_text(
        reason,
        sensitive_values=sensitive_values,
        tracker=tracker,
        field_name="reason",
    )
    sanitized_evidence = [
        _sanitize_text(
            entry,
            sensitive_values=sensitive_values,
            tracker=tracker,
            field_name="evidence",
        )
        for entry in evidence
    ]

    attempted_url = _resolve_attempted_url(
        finding=finding,
        payload=payload,
        base_url=base_url,
    )
    attempted_url = _sanitize_url(
        attempted_url,
        sensitive_values=sensitive_values,
        tracker=tracker,
        field_name="attempted_url",
    )
    attempted_route = _attempted_route(finding=finding, attempted_url=attempted_url)
    method = _attempted_method(finding=finding, payload=payload)

    request_headers = _sanitize_headers(
        {} if payload is None else payload.headers,
        sensitive_values=sensitive_values,
        tracker=tracker,
        field_name="request_headers",
        limit=12,
        allowlist=False,
    )
    request_body = _stringify_payload_body(payload)
    sanitized_request_body = _sanitize_text(
        request_body,
        sensitive_values=sensitive_values,
        tracker=tracker,
        field_name="request_body",
        max_length=_FULL_BODY_LENGTH,
    )

    response_headers_subset = _sanitize_headers(
        {} if exploit_response is None else exploit_response.headers,
        sensitive_values=sensitive_values,
        tracker=tracker,
        field_name="response_headers",
        limit=8,
        allowlist=True,
    )
    body_excerpt = _build_body_excerpt(
        "" if exploit_response is None else exploit_response.body,
        sensitive_values=sensitive_values,
        tracker=tracker,
        field_name="response_body",
    )

    response_diff = _build_response_diff(
        baseline_response=baseline_response,
        exploit_response=exploit_response,
    )
    timing_summary = _build_timing_summary(
        baseline_response=baseline_response,
        exploit_response=exploit_response,
        baseline_capture=baseline_capture,
        exploit_capture=exploit_capture,
    )

    sanitized_error_text = None
    if error_text is not None:
        sanitized_error_text = _sanitize_text(
            error_text,
            sensitive_values=sensitive_values,
            tracker=tracker,
            field_name="error_text",
        )
    screenshots = _collect_screenshots(baseline_capture, exploit_capture)

    rich_evidence = VerificationEvidence(
        attempted_url=attempted_url,
        attempted_route=attempted_route,
        method=method,
        payload_class=finding.vuln_class,
        template_id=template_id,
        status_code=None if exploit_response is None else exploit_response.status_code,
        response_diff_summary=response_diff,
        timing_summary=timing_summary,
        error_signature=_error_signature(sanitized_error_text or sanitized_reason),
        headers_subset=response_headers_subset,
        body_excerpt=body_excerpt,
        screenshot_paths=screenshots,
        redaction_status=VerificationRedactionStatus(
            applied=tracker.redacted_value_count > 0,
            redacted_value_count=tracker.redacted_value_count,
            redacted_fields=sorted(tracker.redacted_fields),
        ),
    )

    artifact_payload: dict[str, object] = {
        "finding_id": finding.id,
        "status": "attempted",
        "reason": sanitized_reason,
        "evidence": sanitized_evidence,
        "request": {
            "method": method,
            "attempted_url": attempted_url,
            "attempted_route": attempted_route,
            "headers": request_headers,
            "body_excerpt": _build_body_excerpt(
                sanitized_request_body,
                sensitive_values=set(),
                tracker=_RedactionTracker(),
                field_name="request_body_excerpt",
            ).model_dump(mode="json"),
        },
        "baseline_response": _response_artifact_payload(
            baseline_response,
            sensitive_values=sensitive_values,
            tracker=tracker,
            field_name="baseline_response",
        ),
        "exploit_response": _response_artifact_payload(
            exploit_response,
            sensitive_values=sensitive_values,
            tracker=tracker,
            field_name="exploit_response",
        ),
        "response_diff_summary": (
            None if response_diff is None else response_diff.model_dump(mode="json")
        ),
        "timing_summary": (
            None if timing_summary is None else timing_summary.model_dump(mode="json")
        ),
        "error_signature": rich_evidence.error_signature,
        "screenshots": screenshots,
        "redaction_status": rich_evidence.redaction_status.model_dump(mode="json"),
    }
    return (
        rich_evidence,
        sanitized_reason,
        sanitized_evidence,
        sanitized_error_text,
        artifact_payload,
    )


def write_verification_evidence_artifact(
    *,
    output_dir: Path,
    finding_id: str,
    payload: dict[str, object],
) -> str:
    evidence_dir = output_dir / "verification-evidence"
    evidence_dir.mkdir(parents=True, exist_ok=True)
    safe_stem = _safe_artifact_stem(finding_id)
    evidence_root = evidence_dir.resolve(strict=False)
    path = (evidence_root / f"{safe_stem}.json").resolve(strict=False)
    if not path.is_relative_to(evidence_root):
        raise ValueError("verification evidence artifact path escaped output directory")
    path.write_text(json.dumps(payload, indent=2, sort_keys=True), encoding="utf-8")
    return str(path)


def _safe_artifact_stem(value: str) -> str:
    trimmed = value.strip()
    collapsed = _UNSAFE_ARTIFACT_STEM_PATTERN.sub("_", trimmed)
    stem = collapsed.strip("._")
    if not stem:
        stem = "finding"
    if stem != trimmed:
        digest = sha256(trimmed.encode("utf-8")).hexdigest()[:8]
        stem = f"{stem}-{digest}"
    if len(stem) > 120:
        digest = sha256(trimmed.encode("utf-8")).hexdigest()[:8]
        stem = f"{stem[:111]}-{digest}"
    return stem


def _collect_sensitive_values(finding: CandidateFinding) -> set[str]:
    values: set[str] = set()
    for raw_key, raw_value in finding.metadata.items():
        key = str(raw_key).strip().lower()
        if not _is_sensitive_key(key):
            continue
        for candidate in _iter_string_values(raw_value):
            normalized = candidate.strip()
            if len(normalized) >= 4:
                values.add(normalized)
    return values


def _iter_string_values(value: Any) -> list[str]:
    if isinstance(value, str):
        return [value]
    if isinstance(value, dict):
        collected: list[str] = []
        for nested in value.values():
            collected.extend(_iter_string_values(nested))
        return collected
    if isinstance(value, list | tuple | set):
        collected = []
        for nested in value:
            collected.extend(_iter_string_values(nested))
        return collected
    return []


def _is_sensitive_key(key: str) -> bool:
    return any(token in key for token in _SENSITIVE_KEY_TOKENS)


def _resolve_attempted_url(
    *,
    finding: CandidateFinding,
    payload: SynthesizedPayload | None,
    base_url: str | None,
) -> str | None:
    if payload is not None and payload.url:
        split = urlsplit(payload.url)
        if split.scheme and split.netloc:
            return payload.url
        if base_url:
            return urljoin(base_url.rstrip("/") + "/", payload.url.lstrip("/"))
        target_url = finding.metadata.get("verification_target_url")
        if isinstance(target_url, str) and target_url.strip():
            return urljoin(target_url.rstrip("/") + "/", payload.url.lstrip("/"))
        return payload.url

    route = finding.metadata.get("verification_route")
    target_url = finding.metadata.get("verification_target_url")
    if isinstance(route, str) and route.strip():
        route_value = route.strip()
        if isinstance(target_url, str) and target_url.strip():
            return urljoin(target_url.rstrip("/") + "/", route_value.lstrip("/"))
        return route_value
    if isinstance(target_url, str) and target_url.strip():
        return target_url.strip()
    return None


def _attempted_route(*, finding: CandidateFinding, attempted_url: str | None) -> str | None:
    if attempted_url:
        split = urlsplit(attempted_url)
        if split.path:
            return split.path
    route = finding.metadata.get("verification_route")
    if isinstance(route, str) and route.strip():
        return route.strip()
    return None


def _attempted_method(
    *,
    finding: CandidateFinding,
    payload: SynthesizedPayload | None,
) -> str | None:
    if payload is not None and payload.method:
        return payload.method.upper()
    method = finding.metadata.get("verification_http_method")
    if isinstance(method, str) and method.strip():
        return method.strip().upper()
    return None


def _sanitize_url(
    value: str | None,
    *,
    sensitive_values: set[str],
    tracker: _RedactionTracker,
    field_name: str,
) -> str | None:
    if value is None:
        return None
    split = urlsplit(value)
    if not split.query:
        return _sanitize_text(
            value,
            sensitive_values=sensitive_values,
            tracker=tracker,
            field_name=field_name,
        )
    items: list[tuple[str, str]] = []
    for key, raw_value in parse_qsl(split.query, keep_blank_values=True):
        if _is_sensitive_key(key.lower()) or _contains_sensitive_value(raw_value, sensitive_values):
            tracker.mark(field_name)
            items.append((key, "[REDACTED]"))
        else:
            items.append((key, raw_value))
    query = urlencode(items, doseq=True)
    rebuilt = urlunsplit((split.scheme, split.netloc, split.path, query, split.fragment))
    return _sanitize_text(
        rebuilt,
        sensitive_values=sensitive_values,
        tracker=tracker,
        field_name=field_name,
    )


def _sanitize_headers(
    headers: dict[str, str] | Any,
    *,
    sensitive_values: set[str],
    tracker: _RedactionTracker,
    field_name: str,
    limit: int,
    allowlist: bool,
) -> dict[str, str]:
    if not isinstance(headers, dict):
        return {}

    filtered_items = []
    for key, value in headers.items():
        if not isinstance(key, str) or not isinstance(value, str):
            continue
        normalized = key.strip().lower()
        if (
            allowlist
            and normalized not in _ALLOWED_HEADER_NAMES
            and normalized not in _SENSITIVE_HEADER_NAMES
        ):
            continue
        filtered_items.append((key.strip(), value.strip()))

    subset: dict[str, str] = {}
    for key, value in sorted(filtered_items, key=lambda item: item[0].lower())[:limit]:
        normalized = key.lower()
        if normalized in _SENSITIVE_HEADER_NAMES or _is_sensitive_key(normalized):
            tracker.mark(field_name)
            subset[key] = "[REDACTED]"
            continue
        subset[key] = _sanitize_text(
            value,
            sensitive_values=sensitive_values,
            tracker=tracker,
            field_name=field_name,
        )
    return subset


def _build_body_excerpt(
    value: str,
    *,
    sensitive_values: set[str],
    tracker: _RedactionTracker,
    field_name: str,
) -> VerificationBodyExcerpt:
    sanitized = _sanitize_text(
        value,
        sensitive_values=sensitive_values,
        tracker=tracker,
        field_name=field_name,
        max_length=_FULL_BODY_LENGTH,
    )
    if not sanitized:
        return VerificationBodyExcerpt()
    preview = sanitized[:_PREVIEW_LENGTH]
    return VerificationBodyExcerpt(
        sha256=sha256(sanitized.encode("utf-8")).hexdigest(),
        preview=preview,
        truncated=len(sanitized) > _PREVIEW_LENGTH,
        length=len(sanitized),
    )


def _build_response_diff(
    *,
    baseline_response: ExploitResult | None,
    exploit_response: ExploitResult | None,
) -> VerificationResponseDiffSummary | None:
    if baseline_response is None or exploit_response is None:
        return None

    baseline_headers = {
        key.lower(): value
        for key, value in baseline_response.headers.items()
        if isinstance(key, str)
    }
    exploit_headers = {
        key.lower(): value
        for key, value in exploit_response.headers.items()
        if isinstance(key, str)
    }
    changed_headers = sorted(
        {
            key
            for key in set(baseline_headers) | set(exploit_headers)
            if baseline_headers.get(key) != exploit_headers.get(key)
        }
    )
    status_changed = baseline_response.status_code != exploit_response.status_code
    body_changed = baseline_response.body != exploit_response.body
    summary = (
        f"status:{baseline_response.status_code}->{exploit_response.status_code}; "
        f"body_changed:{'yes' if body_changed else 'no'}; "
        f"header_changes:{len(changed_headers)}"
    )
    return VerificationResponseDiffSummary(
        summary=summary,
        baseline_status_code=baseline_response.status_code,
        exploit_status_code=exploit_response.status_code,
        status_code_changed=status_changed,
        body_changed=body_changed,
        body_delta_chars=len(exploit_response.body) - len(baseline_response.body),
        changed_headers=changed_headers[:12],
    )


def _build_timing_summary(
    *,
    baseline_response: ExploitResult | None,
    exploit_response: ExploitResult | None,
    baseline_capture: SandboxCapture | None,
    exploit_capture: SandboxCapture | None,
) -> VerificationTimingSummary | None:
    if (
        baseline_response is None
        and exploit_response is None
        and baseline_capture is None
        and exploit_capture is None
    ):
        return None

    baseline_elapsed = None if baseline_response is None else float(baseline_response.elapsed_ms)
    exploit_elapsed = None if exploit_response is None else float(exploit_response.elapsed_ms)
    delta_elapsed = None
    if baseline_elapsed is not None and exploit_elapsed is not None:
        delta_elapsed = exploit_elapsed - baseline_elapsed
    return VerificationTimingSummary(
        baseline_elapsed_ms=baseline_elapsed,
        exploit_elapsed_ms=exploit_elapsed,
        baseline_capture_ms=None if baseline_capture is None else float(baseline_capture.timing_ms),
        exploit_capture_ms=None if exploit_capture is None else float(exploit_capture.timing_ms),
        delta_elapsed_ms=delta_elapsed,
    )


def _response_artifact_payload(
    response: ExploitResult | None,
    *,
    sensitive_values: set[str],
    tracker: _RedactionTracker,
    field_name: str,
) -> dict[str, object] | None:
    if response is None:
        return None
    excerpt = _build_body_excerpt(
        response.body,
        sensitive_values=sensitive_values,
        tracker=tracker,
        field_name=f"{field_name}.body",
    )
    headers = _sanitize_headers(
        response.headers,
        sensitive_values=sensitive_values,
        tracker=tracker,
        field_name=f"{field_name}.headers",
        limit=16,
        allowlist=False,
    )
    return {
        "status_code": response.status_code,
        "elapsed_ms": response.elapsed_ms,
        "headers": headers,
        "body_excerpt": excerpt.model_dump(mode="json"),
        "error": (
            None
            if response.error is None
            else _sanitize_text(
                response.error,
                sensitive_values=sensitive_values,
                tracker=tracker,
                field_name=f"{field_name}.error",
            )
        ),
    }


def _collect_screenshots(
    baseline_capture: SandboxCapture | None,
    exploit_capture: SandboxCapture | None,
) -> list[str]:
    paths: list[str] = []
    for capture in (baseline_capture, exploit_capture):
        if capture is None:
            continue
        for entry in capture.side_effects:
            if not isinstance(entry, str):
                continue
            lowered = entry.lower().strip()
            suffix = Path(lowered).suffix
            if "screenshot" in lowered or suffix in _SCREENSHOT_SUFFIXES:
                paths.append(entry)
    return sorted(dict.fromkeys(paths))


def _error_signature(value: str | None) -> str | None:
    if value is None or not value.strip():
        return None
    match = _ERROR_SIGNATURE_PATTERN.search(value)
    if match is not None:
        return match.group(0)
    return f"ERR-{sha256(value.encode('utf-8')).hexdigest()[:12]}"


def _sanitize_text(
    value: str,
    *,
    sensitive_values: set[str],
    tracker: _RedactionTracker,
    field_name: str,
    max_length: int | None = None,
) -> str:
    sanitized = value
    for token in sorted(sensitive_values, key=len, reverse=True):
        if not token:
            continue
        occurrences = sanitized.count(token)
        if occurrences <= 0:
            continue
        sanitized = sanitized.replace(token, "[REDACTED]")
        tracker.mark(field_name, occurrences)

    for pattern, replacement in _INLINE_SECRET_PATTERNS:
        sanitized, count = pattern.subn(replacement, sanitized)
        tracker.mark(field_name, count)

    if max_length is not None and len(sanitized) > max_length:
        sanitized = sanitized[:max_length]
    return sanitized


def _contains_sensitive_value(value: str, sensitive_values: set[str]) -> bool:
    return any(token and token in value for token in sensitive_values)


def _stringify_payload_body(payload: SynthesizedPayload | None) -> str:
    if payload is None or payload.body is None:
        return ""
    if isinstance(payload.body, str):
        return payload.body
    try:
        return json.dumps(payload.body, separators=(",", ":"), sort_keys=True)
    except TypeError:
        return str(payload.body)


__all__ = [
    "build_verification_evidence",
    "write_verification_evidence_artifact",
]
