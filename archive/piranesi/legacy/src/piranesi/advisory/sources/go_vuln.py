from __future__ import annotations

from collections.abc import Mapping, Sequence

import requests

from piranesi.advisory.models import Advisory
from piranesi.advisory.sources.osv import parse_osv_advisory

GO_VULN_INDEX_URL = "https://vuln.go.dev/index/modules.json"
GO_VULN_ENTRY_URL = "https://vuln.go.dev/ID/{id}.json"


def fetch_go_vuln_advisories(
    *,
    since: str | None = None,
    session: requests.Session | None = None,
    ecosystems: Sequence[str] | None = None,
    full: bool = False,
) -> tuple[list[Advisory], str | None]:
    if ecosystems and "go" not in {item.lower() for item in ecosystems}:
        return [], None
    http = session or requests.Session()
    response = http.get(GO_VULN_INDEX_URL, timeout=60)
    response.raise_for_status()
    payload = response.json()
    if not isinstance(payload, list):
        return [], None
    latest_cursor: str | None = None
    target_ids: set[str] = set()
    for module_entry in payload:
        if not isinstance(module_entry, Mapping):
            continue
        vulns = module_entry.get("vulns")
        if not isinstance(vulns, Sequence):
            continue
        for vuln in vulns:
            if not isinstance(vuln, Mapping):
                continue
            vuln_id = vuln.get("id")
            modified = vuln.get("modified")
            if isinstance(modified, str) and (latest_cursor is None or modified > latest_cursor):
                latest_cursor = modified
            if not isinstance(vuln_id, str):
                continue
            if since is not None and not full and isinstance(modified, str) and modified <= since:
                continue
            target_ids.add(vuln_id)
    advisories: list[Advisory] = []
    for vuln_id in sorted(target_ids):
        item_response = http.get(GO_VULN_ENTRY_URL.format(id=vuln_id), timeout=60)
        item_response.raise_for_status()
        advisory = parse_osv_advisory(item_response.json(), source="go_vuln")
        if advisory is not None:
            advisories.append(advisory)
    return advisories, latest_cursor
