from __future__ import annotations

import html
import json
import re
from collections.abc import Callable, Mapping
from dataclasses import dataclass
from typing import Literal
from urllib.parse import quote

from piranesi.verify.sandbox import ExploitResult, SynthesizedPayload, fire_payload

type ConfirmationLevel = Literal["CONFIRMED", "LIKELY", "UNVERIFIABLE"]
type RequestExecutor = Callable[[SynthesizedPayload, int], ExploitResult]

_AUTH_STATUS_CODES = frozenset({401, 403})
_SQL_ERROR_PATTERNS = tuple(
    re.compile(pattern, re.IGNORECASE)
    for pattern in (
        r"sql syntax",
        r"syntax error",
        r"mysql_fetch",
        r"mysql_num_rows",
        r"pg_query",
        r"pdoexception",
        r"sqlstate",
        r"sqlite",
        r"sqlite3",
        r"ora-\d+",
        r"unclosed quotation mark",
        r"odbc sql",
    )
)
_UNION_EXTRACTION_PATTERNS = tuple(
    re.compile(pattern, re.IGNORECASE)
    for pattern in (
        r"\binformation_schema\b",
        r"\bsqlite_master\b",
        r"\bpg_catalog\b",
        r"\bpostgres(?:ql)?\b",
        r"\bmysql\b",
        r"\bmariadb\b",
        r"\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b",
        r"\b\d+\.\d+(?:\.\d+)?\b",
    )
)
_USERNAME_PATTERN = re.compile(r"^[a-z_][a-z0-9_-]{0,31}$", re.IGNORECASE)
_FILE_CONTENT_PATTERNS = (
    re.compile(r"root:x:0:0:"),
    re.compile(r"daemon:x:\d+:\d+:"),
)
_ROW_COLLECTION_KEYS = ("rows", "results", "items", "data", "records")


@dataclass(slots=True)
class ConfirmationResult:
    level: ConfirmationLevel
    evidence: str
    baseline_response: ExploitResult
    exploit_response: ExploitResult


def build_baseline_payload(
    payload: SynthesizedPayload,
    *,
    vuln_class: str | None = None,
) -> SynthesizedPayload:
    replacements = {
        raw_value: _baseline_value(raw_value, vuln_class=vuln_class)
        for raw_value in payload.payload_values.values()
    }
    baseline_values = {
        name: replacements[raw_value] for name, raw_value in payload.payload_values.items()
    }
    return SynthesizedPayload(
        method=payload.method,
        url=_replace_url_values(payload.url, replacements),
        headers=_replace_object(payload.headers, replacements),  # type: ignore[arg-type]
        body=_replace_object(payload.body, replacements),
        payload_values=baseline_values,
        encoding=payload.encoding,
    )


def confirm_exploit(
    vuln_class: str,
    payload: SynthesizedPayload,
    host_port: int,
    *,
    fire_request: RequestExecutor = fire_payload,
    container_logs: str = "",
) -> ConfirmationResult:
    baseline_payload = build_baseline_payload(payload, vuln_class=vuln_class)
    baseline_response = fire_request(baseline_payload, host_port)
    exploit_response = fire_request(payload, host_port)
    return confirm_responses(
        vuln_class,
        payload,
        baseline_response,
        exploit_response,
        container_logs=container_logs,
    )


def confirm_responses(
    vuln_class: str,
    payload: SynthesizedPayload,
    baseline_response: ExploitResult,
    exploit_response: ExploitResult,
    *,
    container_logs: str = "",
) -> ConfirmationResult:
    if baseline_response.error:
        return ConfirmationResult(
            level="UNVERIFIABLE",
            evidence=f"baseline request failed: {baseline_response.error}",
            baseline_response=baseline_response,
            exploit_response=exploit_response,
        )
    if exploit_response.error:
        return ConfirmationResult(
            level="UNVERIFIABLE",
            evidence=f"exploit request failed: {exploit_response.error}",
            baseline_response=baseline_response,
            exploit_response=exploit_response,
        )
    if _requires_auth(baseline_response, exploit_response):
        return ConfirmationResult(
            level="UNVERIFIABLE",
            evidence="endpoint requires authentication or blocks automated verification",
            baseline_response=baseline_response,
            exploit_response=exploit_response,
        )

    normalized = vuln_class.upper()
    if "CWE-89" in normalized or "SQL" in normalized:
        return _confirm_sqli(
            payload,
            baseline_response,
            exploit_response,
            container_logs=container_logs,
        )
    if "CWE-79" in normalized or "XSS" in normalized:
        return _confirm_xss(payload, baseline_response, exploit_response)
    if "CWE-78" in normalized or "CMD" in normalized or "COMMAND" in normalized:
        return _confirm_cmdi(
            payload,
            baseline_response,
            exploit_response,
            container_logs=container_logs,
        )
    if "CWE-22" in normalized or "TRAVERS" in normalized:
        return _confirm_path_traversal(payload, baseline_response, exploit_response)

    if _responses_differ(baseline_response, exploit_response):
        return ConfirmationResult(
            level="LIKELY",
            evidence=_difference_evidence(baseline_response, exploit_response),
            baseline_response=baseline_response,
            exploit_response=exploit_response,
        )
    return ConfirmationResult(
        level="UNVERIFIABLE",
        evidence="exploit response was indistinguishable from baseline",
        baseline_response=baseline_response,
        exploit_response=exploit_response,
    )


def _confirm_sqli(
    payload: SynthesizedPayload,
    baseline_response: ExploitResult,
    exploit_response: ExploitResult,
    *,
    container_logs: str,
) -> ConfirmationResult:
    exploit_body = exploit_response.body
    baseline_body = baseline_response.body
    combined_text = f"{exploit_body}\n{container_logs}"

    if _contains_pattern(combined_text, _SQL_ERROR_PATTERNS):
        return ConfirmationResult(
            level="CONFIRMED",
            evidence="response contained a SQL error indicator",
            baseline_response=baseline_response,
            exploit_response=exploit_response,
        )

    baseline_rows = _extract_row_count(baseline_body)
    exploit_rows = _extract_row_count(exploit_body)
    if baseline_rows is not None and exploit_rows is not None and baseline_rows != exploit_rows:
        return ConfirmationResult(
            level="CONFIRMED",
            evidence=f"response row count changed from {baseline_rows} to {exploit_rows}",
            baseline_response=baseline_response,
            exploit_response=exploit_response,
        )

    if _looks_like_union_extraction(payload, baseline_body, exploit_body):
        return ConfirmationResult(
            level="CONFIRMED",
            evidence="UNION-based payload exposed data absent from baseline",
            baseline_response=baseline_response,
            exploit_response=exploit_response,
        )

    if _looks_like_time_based_sqli(payload, baseline_response, exploit_response):
        return ConfirmationResult(
            level="CONFIRMED",
            evidence="SLEEP payload caused a >5s response delay relative to baseline",
            baseline_response=baseline_response,
            exploit_response=exploit_response,
        )

    if _responses_differ(baseline_response, exploit_response):
        return ConfirmationResult(
            level="LIKELY",
            evidence=_difference_evidence(baseline_response, exploit_response),
            baseline_response=baseline_response,
            exploit_response=exploit_response,
        )

    return ConfirmationResult(
        level="UNVERIFIABLE",
        evidence="SQLi payload did not produce a response distinguishable from baseline",
        baseline_response=baseline_response,
        exploit_response=exploit_response,
    )


def _confirm_xss(
    payload: SynthesizedPayload,
    baseline_response: ExploitResult,
    exploit_response: ExploitResult,
) -> ConfirmationResult:
    exploit_body = exploit_response.body

    for raw_payload in payload.payload_values.values():
        escaped_payload = html.escape(raw_payload, quote=True)
        if escaped_payload != raw_payload and escaped_payload in exploit_body:
            return ConfirmationResult(
                level="UNVERIFIABLE",
                evidence="payload was HTML-encoded in the response and appears sanitized",
                baseline_response=baseline_response,
                exploit_response=exploit_response,
            )
        if raw_payload in exploit_body and _is_active_xss_markup(raw_payload):
            return ConfirmationResult(
                level="CONFIRMED",
                evidence="response reflected the injected markup without escaping",
                baseline_response=baseline_response,
                exploit_response=exploit_response,
            )

    if _contains_unescaped_xss(exploit_body):
        return ConfirmationResult(
            level="CONFIRMED",
            evidence="response contained an unescaped script tag or event handler",
            baseline_response=baseline_response,
            exploit_response=exploit_response,
        )

    if _contains_likely_xss_markup(exploit_body) and _responses_differ(
        baseline_response,
        exploit_response,
    ):
        return ConfirmationResult(
            level="LIKELY",
            evidence="response reflected transformed HTML/JS fragments derived from the payload",
            baseline_response=baseline_response,
            exploit_response=exploit_response,
        )

    return ConfirmationResult(
        level="UNVERIFIABLE",
        evidence="XSS payload was not reflected in an executable form",
        baseline_response=baseline_response,
        exploit_response=exploit_response,
    )


def _confirm_cmdi(
    payload: SynthesizedPayload,
    baseline_response: ExploitResult,
    exploit_response: ExploitResult,
    *,
    container_logs: str,
) -> ConfirmationResult:
    combined_text = f"{exploit_response.body}\n{container_logs}"
    if any(marker in combined_text for marker in ("uid=", "/root:", "/home/")):
        return ConfirmationResult(
            level="CONFIRMED",
            evidence="command output was returned in the response or container logs",
            baseline_response=baseline_response,
            exploit_response=exploit_response,
        )

    if _looks_like_whoami_output(payload, baseline_response, exploit_response):
        return ConfirmationResult(
            level="CONFIRMED",
            evidence="response body matched whoami output and differed from baseline",
            baseline_response=baseline_response,
            exploit_response=exploit_response,
        )

    if _responses_differ(baseline_response, exploit_response):
        return ConfirmationResult(
            level="LIKELY",
            evidence=_difference_evidence(baseline_response, exploit_response),
            baseline_response=baseline_response,
            exploit_response=exploit_response,
        )

    return ConfirmationResult(
        level="UNVERIFIABLE",
        evidence="command-injection payload produced no observable command output",
        baseline_response=baseline_response,
        exploit_response=exploit_response,
    )


def _confirm_path_traversal(
    payload: SynthesizedPayload,
    baseline_response: ExploitResult,
    exploit_response: ExploitResult,
) -> ConfirmationResult:
    if _contains_pattern(exploit_response.body, _FILE_CONTENT_PATTERNS):
        return ConfirmationResult(
            level="CONFIRMED",
            evidence="response contained file content from the traversed path",
            baseline_response=baseline_response,
            exploit_response=exploit_response,
        )

    if baseline_response.status_code == 404 and exploit_response.status_code == 200:
        return ConfirmationResult(
            level="LIKELY",
            evidence="traversal payload returned content where the benign baseline returned 404",
            baseline_response=baseline_response,
            exploit_response=exploit_response,
        )

    if _looks_like_path_content(payload, baseline_response, exploit_response):
        return ConfirmationResult(
            level="LIKELY",
            evidence="response body structure changed in a way consistent with file disclosure",
            baseline_response=baseline_response,
            exploit_response=exploit_response,
        )

    return ConfirmationResult(
        level="UNVERIFIABLE",
        evidence="path-traversal payload did not expose recognizable file content",
        baseline_response=baseline_response,
        exploit_response=exploit_response,
    )


def _replace_object(value: object, replacements: Mapping[str, str]) -> object:
    if isinstance(value, Mapping):
        return {key: _replace_object(item, replacements) for key, item in value.items()}
    if isinstance(value, list):
        return [_replace_object(item, replacements) for item in value]
    if isinstance(value, tuple):
        return tuple(_replace_object(item, replacements) for item in value)
    if isinstance(value, str):
        return replacements.get(value, value)
    return value


def _replace_url_values(url: str, replacements: Mapping[str, str]) -> str:
    updated = url
    for raw_value, replacement in replacements.items():
        updated = updated.replace(quote(raw_value, safe=""), quote(replacement, safe=""))
        updated = updated.replace(raw_value, replacement)
    return updated


def _baseline_value(raw_value: str, *, vuln_class: str | None) -> str:
    lowered = raw_value.lower()
    normalized_vuln = vuln_class.upper() if vuln_class else ""

    if raw_value.isdigit():
        return "1"
    if "CWE-22" in normalized_vuln or "TRAVERS" in normalized_vuln or "../" in raw_value:
        return "piranesi.txt"
    if "CWE-79" in normalized_vuln or "XSS" in normalized_vuln:
        return "hello"
    if "CWE-78" in normalized_vuln or "COMMAND" in normalized_vuln or "CMD" in normalized_vuln:
        return "status"
    if "CWE-89" in normalized_vuln or "SQL" in normalized_vuln:
        return "piranesi"
    if any(marker in lowered for marker in ("sleep(", "pg_sleep", "union select", "or 1=1")):
        return "piranesi"
    return "baseline"


def _requires_auth(
    baseline_response: ExploitResult,
    exploit_response: ExploitResult,
) -> bool:
    return (
        baseline_response.status_code in _AUTH_STATUS_CODES
        and exploit_response.status_code in _AUTH_STATUS_CODES
    )


def _contains_pattern(text: str, patterns: tuple[re.Pattern[str], ...]) -> bool:
    return any(pattern.search(text) for pattern in patterns)


def _extract_row_count(body: str) -> int | None:
    stripped = body.strip()
    if not stripped:
        return 0

    try:
        data = json.loads(stripped)
    except json.JSONDecodeError:
        data = None

    row_count = _json_row_count(data)
    if row_count is not None:
        return row_count

    table_rows = len(re.findall(r"<tr\b", body, flags=re.IGNORECASE))
    if table_rows:
        return table_rows

    return None


def _json_row_count(data: object) -> int | None:
    if isinstance(data, list):
        return len(data)
    if isinstance(data, dict):
        for key in _ROW_COLLECTION_KEYS:
            value = data.get(key)
            if isinstance(value, list):
                return len(value)
    return None


def _looks_like_union_extraction(
    payload: SynthesizedPayload,
    baseline_body: str,
    exploit_body: str,
) -> bool:
    if not any("union select" in value.lower() for value in payload.payload_values.values()):
        return False
    if exploit_body == baseline_body:
        return False
    return _contains_pattern(exploit_body, _UNION_EXTRACTION_PATTERNS) and not _contains_pattern(
        baseline_body,
        _UNION_EXTRACTION_PATTERNS,
    )


def _looks_like_time_based_sqli(
    payload: SynthesizedPayload,
    baseline_response: ExploitResult,
    exploit_response: ExploitResult,
) -> bool:
    if not any("sleep(" in value.lower() for value in payload.payload_values.values()):
        return False
    return (
        exploit_response.elapsed_ms >= 5_000
        and (exploit_response.elapsed_ms - baseline_response.elapsed_ms) >= 4_000
    )


def _is_active_xss_markup(raw_payload: str) -> bool:
    lowered = raw_payload.lower()
    return "<script" in lowered or "onerror=" in lowered or "onload=" in lowered


def _contains_unescaped_xss(body: str) -> bool:
    lowered = body.lower()
    return (
        "<script" in lowered or "onerror=" in lowered or "onload=" in lowered
    ) and "&lt;script" not in lowered


def _contains_likely_xss_markup(body: str) -> bool:
    lowered = body.lower()
    return any(fragment in lowered for fragment in ("<img", "<svg", "alert(1)", "javascript:"))


def _looks_like_whoami_output(
    payload: SynthesizedPayload,
    baseline_response: ExploitResult,
    exploit_response: ExploitResult,
) -> bool:
    if not any("whoami" in value.lower() for value in payload.payload_values.values()):
        return False
    exploit_body = exploit_response.body.strip()
    baseline_body = baseline_response.body.strip()
    return (
        bool(exploit_body)
        and exploit_body != baseline_body
        and bool(_USERNAME_PATTERN.fullmatch(exploit_body))
    )


def _looks_like_path_content(
    payload: SynthesizedPayload,
    baseline_response: ExploitResult,
    exploit_response: ExploitResult,
) -> bool:
    if not any(
        "../" in value or "..%2f" in value.lower() for value in payload.payload_values.values()
    ):
        return False
    if exploit_response.body == baseline_response.body:
        return False
    return bool(exploit_response.body.strip()) and exploit_response.status_code < 400


def _responses_differ(
    baseline_response: ExploitResult,
    exploit_response: ExploitResult,
) -> bool:
    if baseline_response.status_code != exploit_response.status_code:
        return True
    if baseline_response.body != exploit_response.body:
        return True
    return abs(exploit_response.elapsed_ms - baseline_response.elapsed_ms) >= 1_000


def _difference_evidence(
    baseline_response: ExploitResult,
    exploit_response: ExploitResult,
) -> str:
    differences: list[str] = []
    if baseline_response.status_code != exploit_response.status_code:
        differences.append(
            f"status changed from {baseline_response.status_code} to {exploit_response.status_code}"
        )
    if baseline_response.body != exploit_response.body:
        differences.append(
            "response body changed "
            f"({len(baseline_response.body)}B to {len(exploit_response.body)}B)"
        )
    elapsed_delta = exploit_response.elapsed_ms - baseline_response.elapsed_ms
    if abs(elapsed_delta) >= 1_000:
        differences.append(
            "timing shifted "
            f"from {baseline_response.elapsed_ms:.0f}ms to {exploit_response.elapsed_ms:.0f}ms"
        )
    if not differences:
        return "exploit response differed from baseline"
    return "ambiguous exploit-side difference: " + "; ".join(differences)


__all__ = [
    "ConfirmationLevel",
    "ConfirmationResult",
    "build_baseline_payload",
    "confirm_exploit",
    "confirm_responses",
]
