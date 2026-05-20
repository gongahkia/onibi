from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

from piranesi.workspace import (
    AffectedInstance,
    EvidenceSnippet,
    NormalizedFinding,
    ServiceContext,
    Severity,
    SourceReference,
    deterministic_finding_id,
    utc_now,
)


class ZapParseError(ValueError):
    """Raised when OWASP ZAP JSON cannot be parsed into findings."""


@dataclass(frozen=True)
class ZapParseResult:
    findings: list[NormalizedFinding]
    warnings: list[str]
    metadata: dict[str, Any]


def parse_zap_json_file(
    input_path: Path,
    *,
    input_sha256: str,
    raw_path: str,
) -> ZapParseResult:
    try:
        payload = json.loads(input_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise ZapParseError(f"invalid ZAP JSON: {exc.msg}") from exc
    except OSError as exc:
        raise ZapParseError(f"cannot read ZAP JSON: {exc}") from exc

    if not isinstance(payload, dict):
        raise ZapParseError("unsupported ZAP JSON: expected JSON object")

    alerts = _alert_records(payload)
    if not alerts:
        raise ZapParseError("empty ZAP JSON: document contains no alert records")

    warnings: list[str] = []
    findings: list[NormalizedFinding] = []
    for index, record in enumerate(alerts, start=1):
        finding = _finding_from_alert(
            record,
            input_sha256=input_sha256,
            raw_path=raw_path,
            alert_number=index,
            warnings=warnings,
        )
        if finding is not None:
            findings.append(finding)

    if not findings:
        raise ZapParseError("ZAP JSON contained no valid alert records")

    metadata = {
        "format": "zap-json",
        "zap_version": _as_str(payload.get("@version")) or _as_str(payload.get("version")),
        "generated_at": _as_str(payload.get("@generated")) or _as_str(payload.get("generated")),
        "records": len(alerts),
        "valid_records": len(findings),
        "malformed_records": len(alerts) - len(findings),
        "alert_refs": sorted(
            {
                str(finding.provenance["zap_alert_ref"])
                for finding in findings
                if finding.provenance.get("zap_alert_ref")
            }
        ),
        "summary": {
            "findings": len({finding.id for finding in findings}),
            "warnings": len(warnings),
        },
    }
    return ZapParseResult(findings=findings, warnings=warnings, metadata=metadata)


def _alert_records(payload: dict[str, Any]) -> list[dict[str, Any]]:
    direct = payload.get("alerts")
    if isinstance(direct, list):
        return [item for item in direct if isinstance(item, dict)]

    records: list[dict[str, Any]] = []
    sites = payload.get("site")
    if isinstance(sites, dict):
        sites = [sites]
    if isinstance(sites, list):
        for site in sites:
            if not isinstance(site, dict):
                continue
            site_context = {
                "site_name": _as_str(site.get("@name")) or _as_str(site.get("name")),
                "site_host": _as_str(site.get("@host")) or _as_str(site.get("host")),
                "site_port": _as_int(site.get("@port")) or _as_int(site.get("port")),
                "site_ssl": (
                    _as_bool(site.get("@ssl"))
                    if "@ssl" in site
                    else _as_bool(site.get("ssl"))
                ),
            }
            alerts = site.get("alerts")
            if not isinstance(alerts, list):
                continue
            for alert in alerts:
                if isinstance(alert, dict):
                    records.append({**site_context, **alert})
    return records


def _finding_from_alert(
    alert: dict[str, Any],
    *,
    input_sha256: str,
    raw_path: str,
    alert_number: int,
    warnings: list[str],
) -> NormalizedFinding | None:
    title = _as_str(alert.get("alert")) or _as_str(alert.get("name"))
    if title is None:
        warnings.append(f"alert {alert_number}: missing alert name")
        return None

    instances = _instances(alert)
    location = _first_location(instances) or _as_str(alert.get("url")) or _as_str(alert.get("uri"))
    site_name = _as_str(alert.get("site_name"))
    asset = (
        _asset_from_location(location)
        or _as_str(alert.get("site_host"))
        or _asset_from_location(site_name)
    )
    if asset is None and location is None:
        warnings.append(f"alert {alert_number}: missing site, host, url, or instance uri")
        return None

    now = utc_now()
    alert_ref = _as_str(alert.get("alertRef")) or _as_str(alert.get("pluginid"))
    service = _service_context(location=location or site_name, alert=alert)
    locator = f"alert[{alert_ref or alert_number}]"
    source = SourceReference(
        tool="zap",
        input_sha256=input_sha256,
        raw_path=raw_path,
        locator=locator,
        metadata={
            "alert_ref": alert_ref,
            "plugin_id": _as_str(alert.get("pluginid")),
            "riskcode": _as_str(alert.get("riskcode")),
            "riskdesc": _as_str(alert.get("riskdesc")),
            "confidence": _as_str(alert.get("confidence")),
            "source_id": _as_str(alert.get("sourceid")),
        },
    )
    affected_instances = [
        _affected_instance_from_zap_instance(instance, asset=asset, location=location, alert=alert)
        for instance in instances
    ]
    if not affected_instances:
        affected_instances = [
            AffectedInstance(
                asset=asset or location or "unknown",
                service=service,
                location=location,
            )
        ]

    return NormalizedFinding(
        id=deterministic_finding_id("zap", alert_ref or title, title, location),
        title=title,
        severity=_map_severity(_as_str(alert.get("riskcode")) or _as_str(alert.get("riskdesc"))),
        confidence="tool-observed",
        description=_clean_html(_as_str(alert.get("desc"))),
        remediation=_clean_html(_as_str(alert.get("solution"))),
        asset=asset,
        service=service,
        weakness_ids=_weakness_ids(alert),
        references=_references(alert),
        tags=_tags(alert_ref=alert_ref, weakness_ids=_weakness_ids(alert)),
        evidence=_evidence(alert, instances=instances, title=title, locator=locator),
        source_references=[source],
        affected_instances=affected_instances,
        first_seen=now,
        last_seen=now,
        provenance={
            "tool": "zap",
            "type": "alert",
            "zap_alert_ref": alert_ref,
            "zap_confidence": _as_str(alert.get("confidence")),
        },
    )


def _affected_instance_from_zap_instance(
    instance: dict[str, Any],
    *,
    asset: str | None,
    location: str | None,
    alert: dict[str, Any],
) -> AffectedInstance:
    uri = _as_str(instance.get("uri"))
    return AffectedInstance(
        asset=_asset_from_location(uri) or asset or "unknown",
        service=_service_context(location=uri or location, alert=alert),
        location=uri or location,
        metadata={
            "method": _as_str(instance.get("method")),
            "parameter": _as_str(instance.get("param")),
            "attack": _as_str(instance.get("attack")),
        },
    )


def _instances(alert: dict[str, Any]) -> list[dict[str, Any]]:
    raw_instances = alert.get("instances")
    if not isinstance(raw_instances, list):
        return []
    return [item for item in raw_instances if isinstance(item, dict)]


def _first_location(instances: list[dict[str, Any]]) -> str | None:
    for instance in instances:
        location = _as_str(instance.get("uri"))
        if location:
            return location
    return None


def _service_context(*, location: str | None, alert: dict[str, Any]) -> ServiceContext | None:
    parsed = urlparse(location or "")
    scheme = parsed.scheme or None
    port = parsed.port or _as_int(alert.get("site_port"))
    site_ssl = alert.get("site_ssl")
    if port is None and scheme == "https":
        port = 443
    elif port is None and scheme == "http":
        port = 80
    elif port is not None and scheme is None:
        scheme = "https" if site_ssl is True else "http" if site_ssl is False else None
    if port is None and scheme is None:
        return None
    return ServiceContext(port=port, protocol=scheme, name=scheme)


def _asset_from_location(location: str | None) -> str | None:
    if not location:
        return None
    parsed = urlparse(location)
    if parsed.hostname:
        return parsed.hostname
    return location.split("/", 1)[0] or None


def _weakness_ids(alert: dict[str, Any]) -> list[str]:
    ids: set[str] = set()
    cweid = _as_str(alert.get("cweid"))
    if cweid and cweid not in {"0", "-1"}:
        ids.add(f"CWE-{cweid.removeprefix('CWE-')}")
    return sorted(ids)


def _references(alert: dict[str, Any]) -> list[str]:
    value = _as_str(alert.get("reference"))
    if not value:
        return []
    return sorted({item.strip() for item in value.splitlines() if item.strip()})


def _tags(*, alert_ref: str | None, weakness_ids: list[str]) -> list[str]:
    tags = {"zap"}
    if alert_ref:
        tags.add(f"zap-alert-{alert_ref}")
    tags.update(item.lower() for item in weakness_ids)
    return sorted(tags)


def _evidence(
    alert: dict[str, Any],
    *,
    instances: list[dict[str, Any]],
    title: str,
    locator: str,
) -> list[EvidenceSnippet]:
    snippets = [
        EvidenceSnippet(
            kind="zap-alert",
            value=f"ZAP reported {title}",
            locator=locator,
        )
    ]
    for index, instance in enumerate(instances, start=1):
        uri = _as_str(instance.get("uri"))
        evidence = _as_str(instance.get("evidence"))
        method = _as_str(instance.get("method"))
        if uri:
            snippets.append(
                EvidenceSnippet(
                    kind="zap-instance",
                    value=f"{method or 'HTTP'} {uri}",
                    locator=f"{locator}/instance[{index}]",
                )
            )
        if evidence:
            snippets.append(
                EvidenceSnippet(
                    kind="zap-evidence",
                    value=evidence,
                    redacted=True,
                    locator=f"{locator}/instance[{index}]",
                )
            )
    return snippets


def _map_severity(value: str | None) -> Severity:
    normalized = (value or "info").strip().lower()
    riskcode_map: dict[str, Severity] = {
        "0": "info",
        "1": "low",
        "2": "medium",
        "3": "high",
        "4": "critical",
    }
    if normalized in riskcode_map:
        return riskcode_map[normalized]
    for severity in ("critical", "high", "medium", "low"):
        if severity in normalized:
            return severity  # type: ignore[return-value]
    return "info"


def _clean_html(value: str | None) -> str | None:
    if value is None:
        return None
    cleaned = value.replace("<p>", " ").replace("</p>", " ")
    return " ".join(cleaned.split()) or None


def _as_str(value: object) -> str | None:
    if isinstance(value, str) and value:
        return value
    return None


def _as_int(value: object) -> int | None:
    if isinstance(value, int):
        return value
    if isinstance(value, str):
        try:
            return int(value)
        except ValueError:
            return None
    return None


def _as_bool(value: object) -> bool | None:
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        normalized = value.strip().lower()
        if normalized in {"true", "1", "yes"}:
            return True
        if normalized in {"false", "0", "no"}:
            return False
    return None


__all__ = ["ZapParseError", "ZapParseResult", "parse_zap_json_file"]
