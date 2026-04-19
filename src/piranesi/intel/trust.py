from __future__ import annotations

from datetime import UTC, datetime
from typing import Literal

TrustLevel = Literal["verified", "trusted", "untrusted"]


def trust_level_score(level: TrustLevel) -> float:
    return {
        "verified": 1.0,
        "trusted": 0.75,
        "untrusted": 0.4,
    }[level]


def staleness_score(
    *,
    collected_at: str | None,
    stale_after_hours: int,
    now: datetime | None = None,
) -> float:
    if collected_at is None:
        return 0.5

    anchor = now or datetime.now(UTC)
    try:
        collected = datetime.fromisoformat(collected_at)
    except ValueError:
        return 0.5

    if collected.tzinfo is None:
        collected = collected.replace(tzinfo=UTC)
    age_hours = max(0.0, (anchor - collected).total_seconds() / 3600.0)
    if stale_after_hours <= 0:
        return 0.0
    ratio = age_hours / float(stale_after_hours)
    if ratio <= 1.0:
        return max(0.0, 1.0 - (ratio * 0.4))
    decay = min(1.0, (ratio - 1.0) / 3.0)
    return max(0.0, 0.6 - (decay * 0.6))


def source_quality_score(
    *,
    trust_level: TrustLevel,
    collected_at: str | None,
    stale_after_hours: int,
    now: datetime | None = None,
) -> tuple[float, float, float]:
    trust = trust_level_score(trust_level)
    stale = staleness_score(collected_at=collected_at, stale_after_hours=stale_after_hours, now=now)
    composite = max(0.0, min(1.0, (trust * 0.65) + (stale * 0.35)))
    return composite, trust, stale
