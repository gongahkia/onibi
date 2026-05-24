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
    "CWE-942": CweMetadata(
        name="PermissiveCrossDomainPolicy",
        short_description="Permissive Cross-Domain Policy",
        full_description=(
            "The application reflects or overexposes cross-origin access rules in a way "
            "that allows untrusted origins to access sensitive responses."
        ),
        tags=("security", "cors", "owasp-a05"),
    ),
    "CWE-1021": CweMetadata(
        name="ImproperRestrictionOfRenderedUILayersOrFrames",
        short_description="Missing X-Frame-Options",
        full_description=(
            "The application renders responses without a frame-embedding restriction, "
            "increasing exposure to clickjacking."
        ),
        tags=("security", "headers", "clickjacking", "owasp-a05"),
    ),
    "CWE-693": CweMetadata(
        name="ProtectionMechanismFailure",
        short_description="Missing Security Protection",
        full_description=(
            "The application is missing a security protection such as CSP or hardened "
            "middleware, reducing the effectiveness of defense-in-depth controls."
        ),
        tags=("security", "headers", "middleware", "owasp-a05"),
    ),
    "CWE-319": CweMetadata(
        name="CleartextTransmissionOfSensitiveInformation",
        short_description="Missing HSTS",
        full_description=(
            "The application does not enforce HTTPS transport protections such as HSTS, "
            "which weakens resistance to downgrade and interception attacks."
        ),
        tags=("security", "headers", "transport", "owasp-a05"),
    ),
    "CWE-614": CweMetadata(
        name="SensitiveCookieWithoutSecureFlag",
        short_description="Cookie Missing Secure Flag",
        full_description=(
            "A session or sensitive cookie is configured without the Secure attribute, "
            "allowing it to be sent over non-HTTPS requests."
        ),
        tags=("security", "cookies", "owasp-a05"),
    ),
    "CWE-1004": CweMetadata(
        name="SensitiveCookieWithoutHttpOnlyFlag",
        short_description="Cookie Missing HttpOnly Flag",
        full_description=(
            "A session or sensitive cookie is configured without the HttpOnly attribute, "
            "making it accessible to client-side scripts."
        ),
        tags=("security", "cookies", "owasp-a05"),
    ),
    "CWE-1395": CweMetadata(
        name="DependencyOnVulnerableThirdPartyComponent",
        short_description="Vulnerable Dependency",
        full_description=(
            "The application depends on a third-party component with a known published "
            "security advisory and an available remediated version."
        ),
        tags=("security", "dependencies", "owasp-a06", "owasp-a08"),
    ),
    "CWE-1321": CweMetadata(
        name="PrototypePollution",
        short_description="Prototype Pollution",
        full_description=(
            "User-controlled object keys reach unsafe merge or assignment logic that can "
            "modify JavaScript object prototypes."
        ),
        tags=("security", "prototype-pollution", "owasp-a03"),
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
                else f"Piranesi imported a {title.lower()} issue from source evidence."
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
