from __future__ import annotations

from threading import Lock


class CostTracker:
    def __init__(self) -> None:
        self._lock = Lock()
        self._total_usd = 0.0
        self._by_stage: dict[str, float] = {}

    @property
    def total_usd(self) -> float:
        with self._lock:
            return self._total_usd

    def add(self, cost_usd: float, stage: str) -> float:
        if cost_usd < 0:
            raise ValueError("cost_usd must be non-negative")
        with self._lock:
            self._total_usd += cost_usd
            self._by_stage[stage] = self._by_stage.get(stage, 0.0) + cost_usd
            return self._total_usd

    def total_for_stage(self, stage: str) -> float:
        with self._lock:
            return self._by_stage.get(stage, 0.0)

    def snapshot(self) -> dict[str, float]:
        with self._lock:
            return dict(self._by_stage)
