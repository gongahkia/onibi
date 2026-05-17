from __future__ import annotations

from piranesi.llm.prompts import (
    legal_memo_draft,
    patcher_fix,
    scanner_augment,
    skeptic_challenge,
    triage_classify,
)

_PROMPT_MODULES = (
    scanner_augment,
    triage_classify,
    skeptic_challenge,
    patcher_fix,
    legal_memo_draft,
)


def get_canary_fragments() -> tuple[str, ...]:
    fragments: dict[str, None] = {}
    for module in _PROMPT_MODULES:
        for fragment in module.CANARY_FRAGMENTS:
            fragments.setdefault(fragment, None)
    return tuple(fragments)


__all__ = [
    "get_canary_fragments",
    "legal_memo_draft",
    "patcher_fix",
    "scanner_augment",
    "skeptic_challenge",
    "triage_classify",
]
