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
    SourceReference,
    deterministic_finding_id,
    utc_now,
)


class FfufParseError(ValueError):
    """Raised when ffuf JSON cannot be parsed into findings."""


@dataclass(frozen=True)
class FfufParseResult:
    findings: list[NormalizedFinding]
    warnings: list[str]
    metadata: dict[str, Any]


def parse_ffuf_json_file(
    input_path: Path,
    *,
    input_sha256: str,
    raw_path: str,
) -> FfufParseResult:
    try:
        payload = json.loads(input_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise FfufParseError(f"invalid ffuf JSON: {exc.msg}") from exc
    except OSError as exc:
        raise FfufParseError(f"cannot read ffuf JSON: {exc}") from exc

    if not isinstance(payload, dict):
        raise FfufParseError("unsupported ffuf JSON: expected JSON object")
    raw_results = payload.get("results")
    if not isinstance(raw_results, list) or not raw_results:
        raise FfufParseError("empty ffuf JSON: document contains no result records")

    warnings: list[str] = []
    findings: list[NormalizedFinding] = []
    for index, result in enumerate(raw_results, start=1):
        if not isinstance(result, dict):
            warnings.append(f"result {index}: expected object")
            continue
        finding = _finding_from_result(
            result,
            commandline=_as_str(payload.get("commandline")),
            input_sha256=input_sha256,
            raw_path=raw_path,
            result_number=index,
            warnings=warnings,
        )
        if finding is not None:
            findings.append(finding)

    if not findings:
        raise FfufParseError("ffuf JSON contained no valid result records")

    metadata = {
        "format": "ffuf-json",
        "commandline": _as_str(payload.get("commandline")),
        "records": len(raw_results),
        "valid_records": len(findings),
        "malformed_records": len(raw_results) - len(findings),
        "summary": {
            "findings": len({finding.id for finding in findings}),
            "warnings": len(warnings),
        },
    }
    return FfufParseResult(findings=findings, warnings=warnings, metadata=metadata)


def _finding_from_result(
    result: dict[str, Any],
    *,
    commandline: str | None,
    input_sha256: str,
    raw_path: str,
    result_number: int,
    warnings: list[str],
) -> NormalizedFinding | None:
    url = _as_str(result.get("url"))
    if url is None:
        warnings.append(f"result {result_number}: missing url")
        return None
    status = _as_int(result.get("status"))
    parsed = urlparse(url)
    asset = parsed.hostname or _as_str(result.get("host"))
    service = _service_context(url)
    path = parsed.path or "/"
    title = f"ffuf discovered HTTP {status or 'response'} at {path}"
    locator = f"result[{result_number}]"
    now = utc_now()
    source = SourceReference(
        tool="ffuf",
        input_sha256=input_sha256,
        raw_path=raw_path,
        locator=locator,
        metadata={
            "url": url,
            "status": status,
            "length": _as_int(result.get("length")),
            "words": _as_int(result.get("words")),
            "lines": _as_int(result.get("lines")),
            "redirectlocation": _as_str(result.get("redirectlocation")),
            "commandline": commandline,
        },
    )
    return NormalizedFinding(
        id=deterministic_finding_id("ffuf", url, status, _as_str(result.get("input"))),
        title=title,
        severity="info",
        confidence="tool-observed",
        description="ffuf discovered a matching HTTP response from the supplied wordlist.",
        asset=asset,
        service=service,
        tags=["ffuf", "content-discovery"],
        evidence=[_evidence(result, url=url, locator=locator)],
        source_references=[source],
        affected_instances=[
            AffectedInstance(
                asset=asset or url,
                service=service,
                location=url,
                metadata={
                    "status": status,
                    "redirectlocation": _as_str(result.get("redirectlocation")),
                },
            )
        ],
        first_seen=now,
        last_seen=now,
        provenance={"tool": "ffuf", "type": "result"},
    )


def _service_context(url: str) -> ServiceContext | None:
    parsed = urlparse(url)
    scheme = parsed.scheme or None
    port = parsed.port
    if port is None and scheme == "https":
        port = 443
    elif port is None and scheme == "http":
        port = 80
    if port is None and scheme is None:
        return None
    return ServiceContext(port=port, protocol=scheme, name=scheme)


def _evidence(result: dict[str, Any], *, url: str, locator: str) -> EvidenceSnippet:
    status = _as_int(result.get("status"))
    length = _as_int(result.get("length"))
    words = _as_int(result.get("words"))
    lines = _as_int(result.get("lines"))
    return EvidenceSnippet(
        kind="ffuf-result",
        value=f"ffuf matched {url} status={status} length={length} words={words} lines={lines}",
        locator=locator,
    )


def _as_str(value: object) -> str | None:
    if isinstance(value, str) and value:
        return value
    if isinstance(value, dict):
        return json.dumps(value, sort_keys=True)
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


__all__ = ["FfufParseError", "FfufParseResult", "parse_ffuf_json_file"]
