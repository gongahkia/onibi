from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any
from xml.etree.ElementTree import Element, ParseError

from defusedxml import ElementTree as DefusedET

from piranesi.workspace import (
    AffectedInstance,
    EvidenceSnippet,
    NormalizedFinding,
    ServiceContext,
    SourceReference,
    deterministic_finding_id,
    utc_now,
)


class NmapParseError(ValueError):
    """Raised when nmap XML cannot be parsed into workspace findings."""


@dataclass(frozen=True)
class NmapParseResult:
    findings: list[NormalizedFinding]
    warnings: list[str]
    metadata: dict[str, Any]


def parse_nmap_xml_file(
    input_path: Path,
    *,
    input_sha256: str,
    raw_path: str,
) -> NmapParseResult:
    try:
        root = DefusedET.parse(input_path).getroot()
    except ParseError as exc:
        raise NmapParseError(f"invalid nmap XML: {exc}") from exc
    except OSError as exc:
        raise NmapParseError(f"cannot read nmap XML: {exc}") from exc

    if root is None:
        raise NmapParseError("invalid nmap XML: missing document root")
    if root.tag != "nmaprun" or root.attrib.get("scanner") != "nmap":
        raise NmapParseError("unsupported nmap XML: expected nmaprun scanner='nmap'")

    xml_version = root.attrib.get("xmloutputversion")
    if not xml_version or not xml_version.startswith("1."):
        raise NmapParseError(f"unsupported nmap XML output version {xml_version!r}; expected 1.x")

    hosts = root.findall("host")
    if not hosts:
        raise NmapParseError("empty nmap scan: XML contains no host records")

    warnings: list[str] = []
    findings: list[NormalizedFinding] = []
    metadata = _scan_metadata(root)

    for host in hosts:
        findings.extend(
            _host_findings(
                host,
                input_sha256=input_sha256,
                raw_path=raw_path,
                warnings=warnings,
            )
        )

    if not findings:
        warnings.append("nmap scan contains no open services or script findings")

    metadata["summary"] = {
        "hosts": len(hosts),
        "findings": len(findings),
        "warnings": len(warnings),
    }
    return NmapParseResult(findings=findings, warnings=warnings, metadata=metadata)


def _scan_metadata(root: Element) -> dict[str, Any]:
    scaninfo = []
    for item in root.findall("scaninfo"):
        scaninfo.append(
            {
                "type": item.attrib.get("type"),
                "protocol": item.attrib.get("protocol"),
                "services": item.attrib.get("services"),
                "numservices": item.attrib.get("numservices"),
            }
        )
    return {
        "scanner": root.attrib.get("scanner"),
        "nmap_version": root.attrib.get("version"),
        "args": root.attrib.get("args"),
        "start": root.attrib.get("start"),
        "startstr": root.attrib.get("startstr"),
        "xmloutputversion": root.attrib.get("xmloutputversion"),
        "scaninfo": scaninfo,
    }


def _host_findings(
    host: Element,
    *,
    input_sha256: str,
    raw_path: str,
    warnings: list[str],
) -> list[NormalizedFinding]:
    asset = _host_asset(host)
    if asset is None:
        warnings.append("skipped host with no address or hostname")
        return []

    findings: list[NormalizedFinding] = []
    for port in host.findall("./ports/port"):
        state = port.find("state")
        if state is None or state.attrib.get("state") != "open":
            continue
        port_id = _port_id(port, asset=asset, warnings=warnings)
        if port_id is None:
            continue
        protocol = port.attrib.get("protocol") or "tcp"
        service = _service_context(port, port_id=port_id, protocol=protocol)
        locator = f"host[{asset}]/port[{protocol}/{port_id}]"
        source = SourceReference(
            tool="nmap",
            input_sha256=input_sha256,
            raw_path=raw_path,
            locator=locator,
            metadata={"state_reason": state.attrib.get("reason")},
        )
        findings.append(_service_finding(asset=asset, service=service, source=source))

        for script in port.findall("script"):
            script_finding = _script_finding(
                asset=asset,
                service=service,
                script=script,
                source=source,
            )
            if script_finding is not None:
                findings.append(script_finding)

    return findings


def _service_finding(
    *,
    asset: str,
    service: ServiceContext,
    source: SourceReference,
) -> NormalizedFinding:
    now = utc_now()
    protocol = service.protocol or "tcp"
    port = service.port or 0
    name = service.name or "unknown"
    title = f"Open {protocol}/{port} {name} service"
    product_parts = [service.product, service.version]
    product = " ".join(part for part in product_parts if part)
    evidence = f"nmap observed {name} open on {asset} {protocol}/{port}"
    if product:
        evidence = f"{evidence} ({product})"
    return NormalizedFinding(
        id=deterministic_finding_id("nmap", "service", asset, protocol, port, name),
        title=title,
        severity="info",
        confidence="tool-observed",
        asset=asset,
        service=service,
        evidence=[EvidenceSnippet(kind="service", value=evidence, locator=source.locator)],
        source_references=[source],
        affected_instances=[AffectedInstance(asset=asset, service=service)],
        tags=["nmap", "open-service"],
        first_seen=now,
        last_seen=now,
        provenance={"tool": "nmap", "type": "open_service"},
    )


def _script_finding(
    *,
    asset: str,
    service: ServiceContext,
    script: Element,
    source: SourceReference,
) -> NormalizedFinding | None:
    script_id = script.attrib.get("id")
    output = script.attrib.get("output")
    if not script_id or not output:
        return None

    now = utc_now()
    protocol = service.protocol or "tcp"
    port = service.port or 0
    title = f"nmap {script_id} output for {asset} {protocol}/{port}"
    script_source = source.model_copy(
        update={
            "locator": f"{source.locator}/script[{script_id}]",
            "metadata": {**source.metadata, "script_id": script_id},
        }
    )
    return NormalizedFinding(
        id=deterministic_finding_id("nmap", "script", asset, protocol, port, script_id),
        title=title,
        severity="info",
        confidence="tool-observed",
        asset=asset,
        service=service,
        evidence=[EvidenceSnippet(kind=f"nmap-script:{script_id}", value=output)],
        source_references=[script_source],
        affected_instances=[AffectedInstance(asset=asset, service=service)],
        tags=["nmap", "nmap-script", script_id],
        first_seen=now,
        last_seen=now,
        provenance={"tool": "nmap", "type": "script", "script_id": script_id},
    )


def _host_asset(host: Element) -> str | None:
    for address_type in ("ipv4", "ipv6", "mac"):
        for address in host.findall("address"):
            if address.attrib.get("addrtype") == address_type and address.attrib.get("addr"):
                return address.attrib["addr"]
    hostname = host.find("./hostnames/hostname")
    if hostname is not None and hostname.attrib.get("name"):
        return hostname.attrib["name"]
    return None


def _port_id(port: Element, *, asset: str, warnings: list[str]) -> int | None:
    raw_port = port.attrib.get("portid")
    if raw_port is None:
        warnings.append(f"skipped open port for {asset} with no portid")
        return None
    try:
        return int(raw_port)
    except ValueError:
        warnings.append(f"skipped open port for {asset} with invalid portid {raw_port!r}")
        return None


def _service_context(port: Element, *, port_id: int, protocol: str) -> ServiceContext:
    service = port.find("service")
    if service is None:
        return ServiceContext(port=port_id, protocol=protocol)
    return ServiceContext(
        port=port_id,
        protocol=protocol,
        name=service.attrib.get("name"),
        product=service.attrib.get("product"),
        version=service.attrib.get("version") or service.attrib.get("extrainfo"),
    )


__all__ = ["NmapParseError", "NmapParseResult", "parse_nmap_xml_file"]
