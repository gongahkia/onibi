from __future__ import annotations

import re
from collections.abc import Iterable
from dataclasses import dataclass
from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:
    from collections.abc import Sequence

_REGEX_PREFIX_KEYWORDS = frozenset(
    {
        "await",
        "case",
        "delete",
        "do",
        "else",
        "in",
        "instanceof",
        "new",
        "of",
        "return",
        "throw",
        "typeof",
        "void",
        "yield",
    }
)
_SENSITIVE_FIELD_VALUE_PATTERN = re.compile(
    r"""(?ix)
    (?P<prefix>
        \b(?:api[_-]?key|access[_-]?token|secret|password|passwd|token|session(?:id)?|cookie)\b
        \s*[:=]\s*
    )
    (?P<value>[^\s,;]+)
    """
)
_AUTHORIZATION_HEADER_PATTERN = re.compile(
    r"(?i)(?P<prefix>\\bauthorization\\b\\s*[:=]\\s*)(?P<value>[^\\r\\n]+)"
)
_COOKIE_HEADER_PATTERN = re.compile(
    r"(?i)(?P<prefix>\\b(?:set-cookie|cookie)\\b\\s*[:=]\\s*)(?P<value>[^\\r\\n]+)"
)
_JSON_SENSITIVE_VALUE_PATTERN = re.compile(
    r"""(?ix)
    (?P<prefix>
        "(?:api[_-]?key|access[_-]?token|secret|password|passwd|token|session(?:id)?|cookie|authorization)"
        \s*:\s*"
    )
    (?P<value>[^"]+)
    (?P<suffix>")
    """
)
_PRIVATE_KEY_BLOCK_PATTERN = re.compile(
    r"-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----",
    re.DOTALL,
)
_PROVIDER_SECRET_PATTERNS: tuple[tuple[re.Pattern[str], str], ...] = (
    (re.compile(r"\\bsk-[A-Za-z0-9]{20,}\\b"), "[REDACTED_API_KEY]"),
    (re.compile(r"\\bgh[pousr]_[A-Za-z0-9]{20,}\\b"), "[REDACTED_GITHUB_TOKEN]"),
    (re.compile(r"\\bAIza[0-9A-Za-z\\-_]{20,}\\b"), "[REDACTED_API_KEY]"),
    (
        re.compile(r"\\beyJ[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+\\b"),
        "[REDACTED_JWT]",
    ),
)


@dataclass(slots=True)
class _CodeState:
    regex_allowed: bool
    template_expr_depth: int | None = None


def strip_comments(source: str) -> str:
    """Strip JS/TS comments while preserving original line numbering."""

    output: list[str] = []
    code_stack: list[_CodeState] = [_CodeState(regex_allowed=True)]
    mode = "code"
    regex_in_char_class = False
    index = 0
    length = len(source)

    while index < length:
        char = source[index]
        next_char = source[index + 1] if index + 1 < length else ""
        state = code_stack[-1]

        if mode == "code":
            if char == "'" and next_char:
                output.append(char)
                state.regex_allowed = False
                mode = "single_quote"
                index += 1
                continue
            if char == '"':
                output.append(char)
                state.regex_allowed = False
                mode = "double_quote"
                index += 1
                continue
            if char == "`":
                output.append(char)
                state.regex_allowed = False
                mode = "template"
                index += 1
                continue
            if char == "/" and next_char == "/":
                output.extend((" ", " "))
                index += 2
                while index < length and source[index] not in "\r\n":
                    output.append(" ")
                    index += 1
                continue
            if char == "/" and next_char == "*":
                output.extend((" ", " "))
                index += 2
                while index < length:
                    if source[index] == "*" and index + 1 < length and source[index + 1] == "/":
                        output.extend((" ", " "))
                        index += 2
                        break
                    if source[index] in "\r\n":
                        output.append(source[index])
                    else:
                        output.append(" ")
                    index += 1
                continue
            if char == "/" and state.regex_allowed:
                output.append(char)
                state.regex_allowed = False
                mode = "regex"
                regex_in_char_class = False
                index += 1
                continue
            if _is_identifier_start(char):
                token, index = _consume_identifier(source, index)
                output.append(token)
                state.regex_allowed = token in _REGEX_PREFIX_KEYWORDS
                continue
            if char.isdigit():
                token, index = _consume_number(source, index)
                output.append(token)
                state.regex_allowed = False
                continue

            output.append(char)
            index += 1

            if char == "{":
                if state.template_expr_depth is not None:
                    state.template_expr_depth += 1
                state.regex_allowed = True
                continue
            if char == "}":
                state.regex_allowed = False
                if state.template_expr_depth is not None:
                    state.template_expr_depth -= 1
                    if state.template_expr_depth == 0:
                        code_stack.pop()
                        mode = "template"
                continue
            if char in ")]":
                state.regex_allowed = False
                continue
            if char == "/":
                state.regex_allowed = True
                continue
            if char in ",:;?!=+-*%&|^~<>":
                state.regex_allowed = True
                continue

        elif mode == "single_quote":
            output.append(char)
            index += 1
            if char == "\\" and index < length:
                output.append(source[index])
                index += 1
                continue
            if char == "'":
                mode = "code"

        elif mode == "double_quote":
            output.append(char)
            index += 1
            if char == "\\" and index < length:
                output.append(source[index])
                index += 1
                continue
            if char == '"':
                mode = "code"

        elif mode == "template":
            if char == "\\":
                output.append(char)
                index += 1
                if index < length:
                    output.append(source[index])
                    index += 1
                continue
            if char == "`":
                output.append(char)
                index += 1
                mode = "code"
                continue
            if char == "$" and next_char == "{":
                output.extend((char, next_char))
                code_stack.append(_CodeState(regex_allowed=True, template_expr_depth=1))
                mode = "code"
                index += 2
                continue
            output.append(char)
            index += 1

        elif mode == "regex":
            output.append(char)
            index += 1
            if char == "\\" and index < length:
                output.append(source[index])
                index += 1
                continue
            if char == "[":
                regex_in_char_class = True
                continue
            if char == "]":
                regex_in_char_class = False
                continue
            if char == "/" and not regex_in_char_class:
                while index < length and source[index].isalpha():
                    output.append(source[index])
                    index += 1
                mode = "code"
                code_stack[-1].regex_allowed = False
                continue
            if char in "\r\n":
                mode = "code"

    return "".join(output)


def detect_prompt_canary(
    response: str,
    known_fragments: Iterable[str] | None = None,
) -> list[str]:
    """Return prompt fragments that appear in an LLM response."""

    fragments = (
        tuple(known_fragments) if known_fragments is not None else _default_canary_fragments()
    )
    normalized_response = response.casefold()
    matches: list[str] = []
    seen: set[str] = set()

    for fragment in fragments:
        if fragment and fragment not in seen and fragment.casefold() in normalized_response:
            matches.append(fragment)
            seen.add(fragment)
    return matches


def contains_prompt_canary(
    response: str,
    known_fragments: Iterable[str] | None = None,
) -> bool:
    """Return True when an LLM response leaks a known prompt fragment."""

    return bool(detect_prompt_canary(response, known_fragments=known_fragments))


def redact_sensitive_text(text: str) -> str:
    """Best-effort redaction for likely credential material in LLM prompts."""

    redacted = _PRIVATE_KEY_BLOCK_PATTERN.sub("[REDACTED_PRIVATE_KEY]", text)
    for pattern, replacement in _PROVIDER_SECRET_PATTERNS:
        redacted = pattern.sub(replacement, redacted)
    redacted = _AUTHORIZATION_HEADER_PATTERN.sub(r"\g<prefix>[REDACTED]", redacted)
    redacted = _COOKIE_HEADER_PATTERN.sub(r"\g<prefix>[REDACTED]", redacted)
    redacted = _SENSITIVE_FIELD_VALUE_PATTERN.sub(r"\g<prefix>[REDACTED]", redacted)
    redacted = _JSON_SENSITIVE_VALUE_PATTERN.sub(r"\g<prefix>[REDACTED]\g<suffix>", redacted)
    return redacted


def redact_prompt_messages(messages: Sequence[dict[str, Any]]) -> list[dict[str, Any]]:
    """Return deep-copied messages with string content redacted."""

    redacted: list[dict[str, Any]] = []
    for message in messages:
        updated = dict(message)
        updated["content"] = _redact_value(updated.get("content"))
        redacted.append(updated)
    return redacted


def _default_canary_fragments() -> Sequence[str]:
    from piranesi.llm import prompts

    return prompts.get_canary_fragments()


def _redact_value(value: Any) -> Any:
    if isinstance(value, str):
        return redact_sensitive_text(value)
    if isinstance(value, list):
        return [_redact_value(item) for item in value]
    if isinstance(value, tuple):
        return tuple(_redact_value(item) for item in value)
    if isinstance(value, dict):
        return {key: _redact_value(item) for key, item in value.items()}
    return value


def _consume_identifier(source: str, start: int) -> tuple[str, int]:
    end = start + 1
    while end < len(source) and _is_identifier_part(source[end]):
        end += 1
    return source[start:end], end


def _consume_number(source: str, start: int) -> tuple[str, int]:
    end = start + 1
    while end < len(source) and (source[end].isalnum() or source[end] in "._"):
        end += 1
    return source[start:end], end


def _is_identifier_start(char: str) -> bool:
    return char.isalpha() or char in "_$"


def _is_identifier_part(char: str) -> bool:
    return char.isalnum() or char in "_$"


__all__ = [
    "contains_prompt_canary",
    "detect_prompt_canary",
    "redact_prompt_messages",
    "redact_sensitive_text",
    "strip_comments",
]
