from __future__ import annotations

from collections.abc import Sequence

from piranesi.llm.prompts._base import (
    Message,
    build_tool_spec,
    code_block,
    list_block,
    render_messages,
    schema_instruction,
)

VERSION = "1.0.0"
TOOL_NAME = "submit_scanner_augmentation"
CANARY_FRAGMENTS = (
    "You are Project Piranesi's scanner augmentation stage.",
    "Identify only security-relevant sources or sinks missing from the standard list.",
)
RESPONSE_SCHEMA: dict[str, object] = {
    "type": "object",
    "additionalProperties": False,
    "properties": {
        "additional_sources": {
            "type": "array",
            "items": {"type": "string"},
        },
        "additional_sinks": {
            "type": "array",
            "items": {"type": "string"},
        },
        "reasoning": {"type": "string"},
    },
    "required": ["additional_sources", "additional_sinks", "reasoning"],
}
TOOL_SPEC = build_tool_spec(
    name=TOOL_NAME,
    description="Return additional security-relevant sources and sinks for a code snippet.",
    parameters=RESPONSE_SCHEMA,
)
SYSTEM_PROMPT = "\n".join(
    [
        CANARY_FRAGMENTS[0],
        CANARY_FRAGMENTS[1],
        "Do not repeat the standard sources or sinks unless the code proves a new variant.",
        schema_instruction(TOOL_NAME, RESPONSE_SCHEMA),
    ]
)


def render(
    *,
    standard_sources: Sequence[str],
    standard_sinks: Sequence[str],
    function_code: str,
    language: str = "typescript",
) -> list[Message]:
    user_prompt = "\n".join(
        [
            "Review the following function for unmodeled security-relevant inputs and outputs.",
            "",
            "Standard sources:",
            list_block(standard_sources),
            "",
            "Standard sinks:",
            list_block(standard_sinks),
            "",
            "Function:",
            code_block(language, function_code),
        ]
    )
    return render_messages(SYSTEM_PROMPT, user_prompt)


__all__ = [
    "CANARY_FRAGMENTS",
    "RESPONSE_SCHEMA",
    "SYSTEM_PROMPT",
    "TOOL_NAME",
    "TOOL_SPEC",
    "VERSION",
    "render",
]
