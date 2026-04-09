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
        r"const pattern = /https?:\/\/example\.com\/api/;" "\n"
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
