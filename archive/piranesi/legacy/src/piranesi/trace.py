from __future__ import annotations

from pathlib import Path
from typing import TYPE_CHECKING, TextIO

from pydantic import BaseModel, ConfigDict

if TYPE_CHECKING:
    from piranesi.config import BudgetConfig, TraceConfig


class TraceBudgetExceededError(RuntimeError):
    """Raised when cumulative trace cost exceeds the configured budget."""


class TraceEntry(BaseModel):
    model_config = ConfigDict(extra="forbid")

    timestamp: str
    stage: str
    model: str
    prompt_hash: str
    response_hash: str
    prompt_tokens: int
    response_tokens: int
    cost_usd: float
    duration_ms: int
    cache_hit: bool
    finding_id: str | None = None
    verdict: str | None = None
    prompt: str | None = None
    response: str | None = None


class TraceSummary(BaseModel):
    model_config = ConfigDict(extra="forbid")

    entry_count: int = 0
    prompt_tokens: int = 0
    response_tokens: int = 0
    total_cost_usd: float = 0.0


class TraceWriter:
    def __init__(self, config: TraceConfig, budget: BudgetConfig | None = None) -> None:
        self._config = config
        self._budget = budget
        self._path = Path(config.file_path)
        self._handle: TextIO | None = None
        self._summary = TraceSummary()

    @property
    def path(self) -> Path:
        return self._path

    def open(self) -> None:
        if not self._config.enabled:
            return
        self._path.parent.mkdir(parents=True, exist_ok=True)
        self._handle = self._path.open("a", encoding="utf-8")

    def write(self, entry: TraceEntry) -> None:
        if not self._config.enabled:
            return
        if self._handle is None:
            self.open()
        assert self._handle is not None
        serialized_entry = entry
        if not self._config.log_prompts:
            serialized_entry = entry.model_copy(update={"prompt": None, "response": None})
        self._handle.write(serialized_entry.model_dump_json())
        self._handle.write("\n")
        self._handle.flush()
        self._summary.entry_count += 1
        self._summary.prompt_tokens += serialized_entry.prompt_tokens
        self._summary.response_tokens += serialized_entry.response_tokens
        self._summary.total_cost_usd += serialized_entry.cost_usd
        if self._budget is not None and self._summary.total_cost_usd > self._budget.max_cost_usd:
            raise TraceBudgetExceededError(
                "trace budget exceeded: "
                f"{self._summary.total_cost_usd:.2f} > {self._budget.max_cost_usd:.2f}"
            )

    def close(self) -> None:
        if self._handle is not None:
            self._handle.close()
            self._handle = None

    def summary(self) -> TraceSummary:
        return self._summary.model_copy(deep=True)
