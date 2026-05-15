from __future__ import annotations

import copy
import re
from dataclasses import dataclass, field
from ipaddress import ip_address
from typing import Literal

from pydantic import BaseModel, ConfigDict

from piranesi.host.models import RedactionStatus


class HostRedactionPolicy(BaseModel):
    model_config = ConfigDict(extra="forbid")

    mode: Literal["strict", "balanced", "off"] = "strict"
    preserve_private_ips: bool = False
    preserve_package_names: bool = True
    preserve_usernames: bool = False
    preserve_hostnames: bool = False


@dataclass(slots=True)
class RedactedPayload:
    payload: object
    status: RedactionStatus


@dataclass(slots=True)
class _RedactionContext:
    policy: HostRedactionPolicy
    registry: _PlaceholderRegistry = field(default_factory=lambda: _PlaceholderRegistry())
    hostnames: set[str] = field(default_factory=set)
    usernames: set[str] = field(default_factory=set)


class _PlaceholderRegistry:
    def __init__(self) -> None:
        self._values: dict[str, dict[str, str]] = {}

    def placeholder(self, category: str, value: str) -> str:
        values = self._values.setdefault(category, {})
        if category == "secret":
            values.setdefault(value, "[SECRET]")
            return "[SECRET]"
        existing = values.get(value)
        if existing is not None:
            return existing
        placeholder = f"[{category.upper()}_{len(values) + 1}]"
        values[value] = placeholder
        return placeholder

    def status(self, *, applied: bool, mode: str) -> RedactionStatus:
        categories = {category: len(values) for category, values in sorted(self._values.items())}
        return RedactionStatus(
            applied=applied,
            redacted_value_count=sum(categories.values()),
            categories=categories,
            mode=mode,
        )


_EMAIL_PATTERN = re.compile(r"(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b")
_MAC_PATTERN = re.compile(r"(?i)\b[0-9a-f]{2}(?::[0-9a-f]{2}){5}\b")
_IPV4_PATTERN = re.compile(
    r"(?<![\w.])(?:25[0-5]|2[0-4]\d|1?\d?\d)"
    r"(?:\.(?:25[0-5]|2[0-4]\d|1?\d?\d)){3}(?![\w.])"
)
_IPV6_PATTERN = re.compile(r"(?<![\w.])(?:[A-Fa-f0-9]{0,4}:){2,}[A-Fa-f0-9:.%]+(?![\w.])")
_HOME_PATH_PATTERN = re.compile(
    r"""(?x)
    (?<![\w])
    (?P<path>
      (?:
        /home/[A-Za-z0-9._-]+
        |/Users/[A-Za-z0-9._-]+
      )
      (?:/[^\s'",;)]*)?
    )
    """
)
_PRIVATE_KEY_PATTERN = re.compile(
    r"-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----",
    re.DOTALL,
)
_SECRET_ASSIGNMENT_PATTERN = re.compile(
    r"""(?ix)
    (?P<prefix>
      \b(?:api[_-]?key|access[_-]?token|secret|password|passwd|token|session(?:id)?|cookie)
      \b\s*[:=]\s*
    )
    (?P<value>
      "[^"]+"
      |'[^']+'
      |[^\s,;)}\]]+
    )
    """
)
_AUTHORIZATION_PATTERN = re.compile(
    r"(?i)(?P<prefix>\bauthorization\b\s*[:=]\s*)(?P<value>[^\r\n]+)"
)
_BEARER_PATTERN = re.compile(r"(?i)\bBearer\s+(?P<value>[A-Za-z0-9._-]{8,})")
_PROVIDER_SECRET_PATTERNS: tuple[re.Pattern[str], ...] = (
    re.compile(r"\bsk-[A-Za-z0-9_-]{8,}\b"),
    re.compile(r"\bgh[pousr]_[A-Za-z0-9]{20,}\b"),
    re.compile(r"\bAIza[0-9A-Za-z\-_]{20,}\b"),
    re.compile(r"\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b"),
)


def redact_host_llm_payload(
    payload: object,
    policy: HostRedactionPolicy | None = None,
) -> RedactedPayload:
    selected_policy = policy or HostRedactionPolicy()
    if selected_policy.mode == "off":
        return RedactedPayload(
            payload=copy.deepcopy(payload),
            status=RedactionStatus(applied=False, redacted_value_count=0, mode="off"),
        )
    context = _RedactionContext(policy=selected_policy)
    _collect_known_values(payload, path=(), context=context)
    redacted = _redact_value(payload, path=(), context=context)
    return RedactedPayload(
        payload=redacted,
        status=context.registry.status(applied=True, mode=selected_policy.mode),
    )


def _collect_known_values(
    value: object, *, path: tuple[str, ...], context: _RedactionContext
) -> None:
    if isinstance(value, dict):
        for key, item in value.items():
            _collect_known_values(item, path=(*path, str(key)), context=context)
        return
    if isinstance(value, list):
        for index, item in enumerate(value):
            _collect_known_values(item, path=(*path, str(index)), context=context)
        return
    if not isinstance(value, str):
        return
    key = path[-1].lower() if path else ""
    if key in {"hostname", "host", "nodename"}:
        _add_known_hostname(value, context)
    if key in {"username", "user", "login"}:
        _add_known_username(value, context)
    for match in _HOME_PATH_PATTERN.finditer(value):
        parts = match.group("path").split("/")
        if len(parts) >= 3:
            _add_known_username(parts[2], context)


def _add_known_hostname(value: str, context: _RedactionContext) -> None:
    rendered = value.strip()
    if len(rendered) > 1:
        context.hostnames.add(rendered)


def _add_known_username(value: str, context: _RedactionContext) -> None:
    rendered = value.strip()
    if rendered and rendered != "root":
        context.usernames.add(rendered)


def _redact_value(value: object, *, path: tuple[str, ...], context: _RedactionContext) -> object:
    if isinstance(value, dict):
        return {
            key: _redact_value(item, path=(*path, str(key)), context=context)
            for key, item in value.items()
        }
    if isinstance(value, list):
        return [
            _redact_value(item, path=(*path, str(index)), context=context)
            for index, item in enumerate(value)
        ]
    if isinstance(value, str):
        return _redact_string(value, path=path, context=context)
    return copy.deepcopy(value)


def _redact_string(value: str, *, path: tuple[str, ...], context: _RedactionContext) -> str:
    if _preserve_string(path, context.policy):
        return value
    rendered = value
    rendered = _redact_secrets(rendered, context)
    rendered = _redact_pattern(rendered, _EMAIL_PATTERN, "email", context)
    rendered = _redact_pattern(rendered, _MAC_PATTERN, "mac", context)
    rendered = _redact_home_paths(rendered, context)
    rendered = _redact_ips(rendered, context)
    if not context.policy.preserve_hostnames:
        rendered = _redact_exact_values(rendered, context.hostnames, "hostname", context)
    if not context.policy.preserve_usernames:
        rendered = _redact_exact_values(rendered, context.usernames, "user", context)
    return rendered


def _preserve_string(path: tuple[str, ...], policy: HostRedactionPolicy) -> bool:
    if len(path) >= 3 and path[-1] == "name" and path[-3] == "packages":
        return policy.preserve_package_names
    return len(path) >= 3 and path[-1] == "name" and path[-3] in {"services", "processes"}


def _redact_secrets(value: str, context: _RedactionContext) -> str:
    rendered = _PRIVATE_KEY_PATTERN.sub(
        lambda match: context.registry.placeholder("secret", match.group(0)),
        value,
    )
    rendered = _AUTHORIZATION_PATTERN.sub(
        lambda match: context.registry.placeholder("secret", match.group("value")),
        rendered,
    )
    rendered = _SECRET_ASSIGNMENT_PATTERN.sub(
        lambda match: context.registry.placeholder("secret", match.group("value")),
        rendered,
    )
    rendered = _BEARER_PATTERN.sub(
        lambda match: f"Bearer {context.registry.placeholder('secret', match.group('value'))}",
        rendered,
    )
    for pattern in _PROVIDER_SECRET_PATTERNS:
        rendered = pattern.sub(
            lambda match: context.registry.placeholder("secret", match.group(0)),
            rendered,
        )
    return rendered


def _redact_pattern(
    value: str,
    pattern: re.Pattern[str],
    category: str,
    context: _RedactionContext,
) -> str:
    return pattern.sub(
        lambda match: context.registry.placeholder(category, match.group(0)),
        value,
    )


def _redact_home_paths(value: str, context: _RedactionContext) -> str:
    return _HOME_PATH_PATTERN.sub(
        lambda match: context.registry.placeholder("home_path", match.group("path")),
        value,
    )


def _redact_ips(value: str, context: _RedactionContext) -> str:
    rendered = _IPV4_PATTERN.sub(lambda match: _ip_placeholder(match.group(0), context), value)
    return _IPV6_PATTERN.sub(lambda match: _ip_placeholder(match.group(0), context), rendered)


def _ip_placeholder(value: str, context: _RedactionContext) -> str:
    try:
        parsed = ip_address(value)
    except ValueError:
        return value
    if parsed.is_loopback:
        category = "loopback_ip"
    elif parsed.is_link_local:
        category = "link_local_ip"
    elif parsed.is_private:
        category = "private_ip"
    else:
        category = "public_ip"
    if category == "private_ip" and context.policy.preserve_private_ips:
        return value
    return context.registry.placeholder(category, value)


def _redact_exact_values(
    value: str,
    values: set[str],
    category: str,
    context: _RedactionContext,
) -> str:
    rendered = value
    for raw in sorted(values, key=len, reverse=True):
        if not raw:
            continue
        pattern = re.compile(rf"(?<![A-Za-z0-9_.-]){re.escape(raw)}(?![A-Za-z0-9_.-])")
        rendered = pattern.sub(
            lambda match, raw_value=raw: context.registry.placeholder(category, raw_value),
            rendered,
        )
    return rendered


__all__ = [
    "HostRedactionPolicy",
    "RedactedPayload",
    "redact_host_llm_payload",
]
