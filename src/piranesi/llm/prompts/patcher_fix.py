from __future__ import annotations

from piranesi.llm.prompts._base import (
    Message,
    build_tool_spec,
    code_block,
    render_messages,
    schema_instruction,
)

VERSION = "1.0.0"
TOOL_NAME = "submit_patch_fix"
CANARY_FRAGMENTS = (
    "You are Project Piranesi's patch generation stage.",
    "Generate the smallest safe fix that removes the confirmed vulnerability.",
)
RESPONSE_SCHEMA: dict[str, object] = {
    "type": "object",
    "additionalProperties": False,
    "properties": {
        "patched_code": {"type": "string"},
        "explanation": {"type": "string"},
        "mitigation_type": {
            "type": "string",
            "enum": [
                "authorization",
                "canonicalization",
                "encoding",
                "escaping",
                "parameterization",
                "validation",
                "other",
            ],
        },
    },
    "required": ["patched_code", "explanation", "mitigation_type"],
}
TOOL_SPEC = build_tool_spec(
    name=TOOL_NAME,
    description="Generate a minimal code fix for a confirmed vulnerability.",
    parameters=RESPONSE_SCHEMA,
)
SYSTEM_PROMPT = "\n".join(
    [
        CANARY_FRAGMENTS[0],
        CANARY_FRAGMENTS[1],
        "Preserve behavior, match the repository's style, "
        "and avoid introducing new security issues.",
        schema_instruction(TOOL_NAME, RESPONSE_SCHEMA),
    ]
)


def render(
    *,
    vuln_description: str,
    cwe_id: str,
    vulnerable_code: str,
    language: str = "typescript",
) -> list[Message]:
    user_prompt = "\n".join(
        [
            "Produce a minimal fix for this confirmed vulnerability.",
            "",
            f"Vulnerability: {vuln_description}",
            f"CWE: {cwe_id}",
            "",
            "Affected code:",
            code_block(language, vulnerable_code),
            "",
            "Requirements:",
            "- Eliminate the vulnerability.",
            "- Preserve existing functionality.",
            "- Follow the existing code style.",
            "- Prefer the most idiomatic mitigation for the framework in use.",
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
