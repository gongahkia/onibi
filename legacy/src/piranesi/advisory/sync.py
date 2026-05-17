from __future__ import annotations

from dataclasses import dataclass, field

import requests

from piranesi.advisory.db import AdvisoryDB, utc_now
from piranesi.advisory.epss import enrich_epss
from piranesi.advisory.exploit import check_exploit_availability
from piranesi.advisory.models import Advisory
from piranesi.advisory.sources import (
    fetch_ghsa_advisories,
    fetch_go_vuln_advisories,
    fetch_nvd_advisories,
    fetch_osv_advisories,
)


@dataclass(frozen=True)
class SyncResult:
    source_counts: dict[str, int] = field(default_factory=dict)
    total_upserted: int = 0
    epss_updated: int = 0
    exploit_updated: int = 0


def sync_advisories(
    db: AdvisoryDB,
    *,
    sources: tuple[str, ...] = ("osv", "ghsa", "nvd", "go_vuln"),
    full: bool = False,
    ecosystems: tuple[str, ...] | None = None,
    session: requests.Session | None = None,
    github_token: str | None = None,
    nvd_api_key: str | None = None,
) -> SyncResult:
    http = session or requests.Session()
    source_counts: dict[str, int] = {}
    total_upserted = 0
    normalized_ecosystems = (
        None if ecosystems is None else tuple(item.lower() for item in ecosystems)
    )

    for source_name in sources:
        last_sync = None if full else _last_sync_for(db, source_name)
        advisories, cursor = _fetch_source(
            source_name,
            since=last_sync,
            ecosystems=normalized_ecosystems,
            full=full,
            session=http,
            github_token=github_token,
            nvd_api_key=nvd_api_key,
        )
        count = db.upsert_advisories(advisories)
        db.upsert_sync_metadata(
            source=source_name,
            last_sync=utc_now(),
            last_cursor=cursor,
            record_count=count,
        )
        source_counts[source_name] = count
        total_upserted += count

    epss_updated = enrich_epss(db, session=http)
    exploit_updated = check_exploit_availability(db, session=http, github_token=github_token)
    return SyncResult(
        source_counts=source_counts,
        total_upserted=total_upserted,
        epss_updated=epss_updated,
        exploit_updated=exploit_updated,
    )


def _last_sync_for(db: AdvisoryDB, source_name: str) -> str | None:
    metadata = db.get_sync_metadata(source_name)
    if metadata is None:
        return None
    return metadata.last_cursor or metadata.last_sync


def _fetch_source(
    source_name: str,
    *,
    since: str | None,
    ecosystems: tuple[str, ...] | None,
    full: bool,
    session: requests.Session,
    github_token: str | None,
    nvd_api_key: str | None,
) -> tuple[list[Advisory], str | None]:
    if source_name == "osv":
        return fetch_osv_advisories(since=since, session=session, ecosystems=ecosystems, full=full)
    if source_name == "ghsa":
        return fetch_ghsa_advisories(since=since, session=session, token=github_token, full=full)
    if source_name == "nvd":
        return fetch_nvd_advisories(since=since, session=session, api_key=nvd_api_key, full=full)
    if source_name == "go_vuln":
        return fetch_go_vuln_advisories(
            since=since,
            session=session,
            ecosystems=ecosystems,
            full=full,
        )
    raise ValueError(f"unsupported advisory source: {source_name}")
