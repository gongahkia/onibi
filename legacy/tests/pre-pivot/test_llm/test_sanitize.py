from __future__ import annotations

from piranesi.llm import sanitize
from piranesi.llm.prompts import triage_classify


def test_strip_comments_preserves_line_numbers() -> None:
    source = (
        "const a = 1; // trailing comment\n"
        "/**\n"
        " * docs that should be removed\n"
        " */\n"
        "const b = 2; /* inline block\n"
        "still comment */\n"
        "return a + b;\n"
    )

    sanitized = sanitize.strip_comments(source)
    source_lines = source.splitlines()
    sanitized_lines = sanitized.splitlines()

    assert len(sanitized_lines) == len(source_lines)
    assert sanitized_lines[0].startswith("const a = 1; ")
    assert sanitized_lines[1].strip() == ""
    assert sanitized_lines[2].strip() == ""
    assert sanitized_lines[3].strip() == ""
    assert sanitized_lines[4].startswith("const b = 2; ")
    assert sanitized_lines[5].strip() == ""
    assert sanitized_lines[6] == "return a + b;"
    assert "trailing comment" not in sanitized
    assert "docs that should be removed" not in sanitized
    assert "inline block" not in sanitized


def test_strip_comments_keeps_strings_regexes_and_template_literals() -> None:
    source = (
        'const url = "https://example.com/api"; // real comment\n'
        r"const pattern = /https?:\/\/example\.com\/api/;"
        "\n"
        "const template = `literal /* not a comment */ and // also not a comment`;\n"
        "const nested = `${value /* real block comment */}`;\n"
    )

    sanitized = sanitize.strip_comments(source)

    assert '"https://example.com/api"' in sanitized
    assert r"/https?:\/\/example\.com\/api/" in sanitized
    assert "literal /* not a comment */ and // also not a comment" in sanitized
    assert "real comment" not in sanitized
    assert "real block comment" not in sanitized
    assert len(sanitized.splitlines()) == len(source.splitlines())


def test_detect_prompt_canary_matches_known_prompt_fragments() -> None:
    fragment = triage_classify.CANARY_FRAGMENTS[0]
    response = f"Leaked system prompt: {fragment.upper()}"

    matches = sanitize.detect_prompt_canary(response)

    assert fragment in matches
    assert sanitize.contains_prompt_canary(response) is True
    assert sanitize.contains_prompt_canary("ordinary model output") is False


def test_redact_sensitive_text_masks_likely_credentials() -> None:
    raw = (
        "authorization: Bearer sk-abcdefghijklmnopqrstuvwx\n"
        "cookie: sid=abc123\n"
        "api_key=super-secret-key\n"
        "PRIVATE_KEY_REDACTED\n"
    )

    redacted = sanitize.redact_sensitive_text(raw)

    assert "sk-abcdefghijklmnopqrstuvwx" not in redacted
    assert "super-secret-key" not in redacted
    assert "BEGIN PRIVATE KEY" not in redacted
    assert "[REDACTED]" in redacted
    assert "[REDACTED_PRIVATE_KEY]" in redacted


def test_redact_sensitive_text_masks_provider_token_variants() -> None:
    raw = (
        "gh token: GITHUB_TOKEN_REDACTED\n"
        "jwt: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9."
        "eyJzdWIiOiIxMjM0NTY3ODkwIn0."
        "SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c\n"
        "bearer-only: Bearer tokenvalue123456\n"
    )

    redacted = sanitize.redact_sensitive_text(raw)

    assert "GITHUB_TOKEN_REDACTED" not in redacted
    assert "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9" not in redacted
    assert "tokenvalue123456" not in redacted
    assert "[REDACTED_GITHUB_TOKEN]" in redacted or "gh token: [REDACTED]" in redacted
    assert "[REDACTED_JWT]" in redacted
    assert "[REDACTED_TOKEN]" in redacted


def test_redact_prompt_messages_redacts_nested_text_content() -> None:
    messages = [
        {"role": "system", "content": "Keep output strict JSON."},
        {
            "role": "user",
            "content": [
                {"type": "text", "text": "password: hunter2"},
                {"type": "text", "text": "Authorization: Bearer abcdef"},
            ],
        },
    ]

    redacted = sanitize.redact_prompt_messages(messages)

    assert redacted[0]["content"] == "Keep output strict JSON."
    assert "hunter2" not in redacted[1]["content"][0]["text"]
    assert "[REDACTED]" in redacted[1]["content"][0]["text"]
    assert "Bearer abcdef" not in redacted[1]["content"][1]["text"]


def test_redact_prompt_messages_redacts_nested_sensitive_mapping_keys() -> None:
    messages = [
        {
            "role": "user",
            "content": {
                "headers": {
                    "Authorization": "Bearer top-secret-token-value",
                    "X-Trace-Id": "trace-123",
                },
                "body": {
                    "profile": {"sessionToken": "nested-session-token"},
                    "events": [{"token": "event-token-value"}, {"note": "safe"}],
                },
            },
        }
    ]

    redacted = sanitize.redact_prompt_messages(messages)
    content = redacted[0]["content"]

    assert content["headers"]["Authorization"] == "[REDACTED]"
    assert content["headers"]["X-Trace-Id"] == "trace-123"
    assert content["body"]["profile"]["sessionToken"] == "[REDACTED]"
    assert content["body"]["events"][0]["token"] == "[REDACTED]"  # noqa: S105
    assert content["body"]["events"][1]["note"] == "safe"
