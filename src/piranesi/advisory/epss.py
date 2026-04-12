from __future__ import annotations

import time
from collections.abc import Iterable

import requests

from piranesi.advisory.db import AdvisoryDB

EPSS_API_URL = "https://api.first.org/data/v1/epss"


def epss_label(score: float | None, percentile: float | None = None) -> str | None:
    if score is None:
        return None
    if score >= 0.5:
        return "actively_exploited_risk"
    if score >= 0.1:
        return "high_exploit_probability"
    if score >= 0.01:
        return "moderate_exploit_probability"
    return "low_exploit_probability"


def enrich_epss(
    db: AdvisoryDB,
    *,
    batch_size: int = 100,
    session: requests.Session | None = None,
    max_age_days: int = 7,
    sleep_s: float = 0.1,
) -> int:
    cve_ids = db.iter_cve_ids_needing_epss(max_age_days=max_age_days)
    if not cve_ids:
        return 0

    http = session or requests.Session()
    updated = 0
    for batch in _batched(cve_ids, batch_size):
        response = http.get(
            EPSS_API_URL,
            params={"cve": ",".join(batch)},
            timeout=30,
        )
        response.raise_for_status()
        payload = response.json()
        updated += db.update_epss_scores(parse_epss_response(payload))
        if sleep_s > 0:
            time.sleep(sleep_s)
    return updated


def parse_epss_response(payload: object) -> dict[str, tuple[float, float]]:
    if not isinstance(payload, dict):
        return {}
    data = payload.get("data")
    if not isinstance(data, list):
        return {}
    scores: dict[str, tuple[float, float]] = {}
    for item in data:
        if not isinstance(item, dict):
            continue
        cve = item.get("cve")
        epss = item.get("epss")
        percentile = item.get("percentile")
        if not isinstance(cve, str) or epss is None or percentile is None:
            continue
        try:
            scores[cve] = (float(epss), float(percentile))
        except (TypeError, ValueError):
            continue
    return scores


def _batched(items: Iterable[str], size: int) -> list[list[str]]:
    current: list[str] = []
    batches: list[list[str]] = []
    for item in items:
        current.append(item)
        if len(current) >= size:
            batches.append(current)
            current = []
    if current:
        batches.append(current)
    return batches
