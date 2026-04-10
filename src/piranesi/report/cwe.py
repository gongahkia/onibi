from __future__ import annotations

import re
from dataclasses import dataclass

_CWE_PATTERN = re.compile(r"(CWE-\d+)", re.IGNORECASE)


@dataclass(frozen=True)
class CweMetadata:
    name: str
    short_description: str
    full_description: str
    tags: tuple[str, ...]


_CWE_METADATA: dict[str, CweMetadata] = {
    "CWE-22": CweMetadata(
        name="PathTraversal",
        short_description="Path Traversal",
        full_description=(
            "User-controlled path input reaches a file system operation without confinement "
            "to an expected base directory."
        ),
        tags=("security", "path-traversal", "owasp-a01"),
    ),
    "CWE-78": CweMetadata(
        name="CommandInjection",
        short_description="Command Injection",
        full_description=(
            "User-controlled input reaches shell or process execution without safe "
            "argument handling."
        ),
        tags=("security", "command-injection", "owasp-a03"),
    ),
    "CWE-79": CweMetadata(
        name="CrossSiteScripting",
        short_description="Cross-Site Scripting",
        full_description=(
            "User-controlled input reaches an HTML rendering sink without appropriate "
            "encoding or sanitization."
        ),
        tags=("security", "xss", "owasp-a03"),
    ),
    "CWE-89": CweMetadata(
        name="SQLInjection",
        short_description="SQL Injection",
        full_description=("User-controlled input reaches a SQL query without parameterization."),
        tags=("security", "sql-injection", "owasp-a03"),
    ),
    "CWE-94": CweMetadata(
        name="CodeInjection",
        short_description="Code Injection",
        full_description=(
            "User-controlled input reaches dynamic code execution without validation or sandboxing."
        ),
        tags=("security", "code-injection", "owasp-a03"),
    ),
    "CWE-918": CweMetadata(
        name="ServerSideRequestForgery",
        short_description="Server-Side Request Forgery",
        full_description=(
            "User-controlled input reaches an outbound network request sink without "
            "allowlisting or destination validation."
        ),
        tags=("security", "ssrf", "owasp-a10"),
    ),
}


def extract_cwe_id(vuln_class: str) -> str:
    match = _CWE_PATTERN.search(vuln_class)
    if match is None:
        return vuln_class
    return match.group(1).upper()


def cwe_title(cwe: str, *, fallback: str | None = None) -> str:
    metadata = _CWE_METADATA.get(cwe.upper())
    if metadata is not None:
        return metadata.short_description
    if fallback is not None:
        _, _, title = fallback.partition(":")
        return title.strip() or fallback.strip()
    return cwe.upper()


def cwe_reporting_descriptor(cwe: str, *, fallback: str | None = None) -> dict[str, object]:
    normalized = cwe.upper()
    metadata = _CWE_METADATA.get(normalized)
    title = cwe_title(normalized, fallback=fallback)
    return {
        "id": normalized,
        "name": metadata.name if metadata is not None else _descriptor_name(title),
        "shortDescription": {"text": title},
        "fullDescription": {
            "text": (
                metadata.full_description
                if metadata is not None
                else f"Piranesi confirmed a {title.lower()} issue."
            )
        },
        "helpUri": _cwe_help_uri(normalized),
        "properties": {
            "tags": list(metadata.tags) if metadata is not None else ["security"],
        },
    }


def _cwe_help_uri(cwe: str) -> str:
    digits = "".join(char for char in cwe if char.isdigit())
    if not digits:
        return "https://cwe.mitre.org/"
    return f"https://cwe.mitre.org/data/definitions/{digits}.html"


def _descriptor_name(title: str) -> str:
    words = re.findall(r"[A-Za-z0-9]+", title)
    if not words:
        return "UnknownFinding"
    return "".join(word[:1].upper() + word[1:] for word in words)
