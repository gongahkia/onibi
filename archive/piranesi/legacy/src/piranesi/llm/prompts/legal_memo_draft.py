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
TOOL_NAME = "submit_legal_memo_draft"
CANARY_FRAGMENTS = (
    "You are Project Piranesi's legal memo drafting stage.",
    "Draft a regulatory impact assessment tied only to the supplied facts.",
)
RESPONSE_SCHEMA: dict[str, object] = {
    "type": "object",
    "additionalProperties": False,
    "properties": {
        "obligations": {
            "type": "array",
            "items": {
                "type": "object",
                "additionalProperties": False,
                "properties": {
                    "regulation": {"type": "string"},
                    "article": {"type": "string"},
                    "obligation": {"type": "string"},
                    "deadline": {"type": "string"},
                },
                "required": ["regulation", "article", "obligation", "deadline"],
            },
        },
        "risk_level": {
            "type": "string",
            "enum": ["high", "medium", "low"],
        },
        "recommended_actions": {
            "type": "array",
            "items": {"type": "string"},
        },
        "notification_required": {"type": "boolean"},
        "notification_deadline_hours": {
            "type": ["integer", "null"],
            "minimum": 0,
        },
    },
    "required": [
        "obligations",
        "risk_level",
        "recommended_actions",
        "notification_required",
        "notification_deadline_hours",
    ],
}
TOOL_SPEC = build_tool_spec(
    name=TOOL_NAME,
    description="Draft a regulatory impact assessment for a confirmed vulnerability.",
    parameters=RESPONSE_SCHEMA,
)
SYSTEM_PROMPT = "\n".join(
    [
        CANARY_FRAGMENTS[0],
        CANARY_FRAGMENTS[1],
        "Do not invent laws, deadlines, or obligations "
        "that are unsupported by the supplied regulations.",
        schema_instruction(TOOL_NAME, RESPONSE_SCHEMA),
    ]
)


def render(
    *,
    vuln_description: str,
    data_categories: Sequence[str],
    jurisdiction: str,
    regulations: Sequence[str],
    severity: str,
    code_context: str | None = None,
    language: str = "typescript",
) -> list[Message]:
    prompt_lines = [
        f"Assess the regulatory impact of this confirmed vulnerability in {jurisdiction}.",
        "",
        f"Vulnerability: {vuln_description}",
        "Data categories affected:",
        list_block(data_categories),
        "",
        "Applicable regulations:",
        list_block(regulations),
        "",
        f"Severity: {severity}",
    ]
    if code_context is not None:
        prompt_lines.extend(["", "Supporting code context:", code_block(language, code_context)])
    return render_messages(SYSTEM_PROMPT, "\n".join(prompt_lines))


__all__ = [
    "CANARY_FRAGMENTS",
    "RESPONSE_SCHEMA",
    "SYSTEM_PROMPT",
    "TOOL_NAME",
    "TOOL_SPEC",
    "VERSION",
    "render",
]
