from __future__ import annotations

from importlib import import_module

__all__ = [
    "BudgetExceededError",
    "LLMProvider",
    "LLMResponse",
    "ModelRouter",
]


def __getattr__(name: str) -> object:
    if name in {"LLMProvider", "LLMResponse"}:
        module = import_module("piranesi.llm.provider")
        return getattr(module, name)
    if name in {"BudgetExceededError", "ModelRouter"}:
        module = import_module("piranesi.llm.router")
        return getattr(module, name)
    raise AttributeError(f"module {__name__!r} has no attribute {name!r}")
