from __future__ import annotations

import json
from dataclasses import asdict, dataclass, field
from typing import Literal

from piranesi.models import AttackSurfaceNode, CandidateFinding
from piranesi.report.cwe import cwe_title, extract_cwe_id

NodeType = Literal["goal", "and", "or", "leaf"]
TreeFormat = Literal["markdown", "json", "mermaid"]


@dataclass
class AttackNode:
    label: str
    node_type: NodeType
    children: list[AttackNode] = field(default_factory=list)
    metadata: dict[str, str] = field(default_factory=dict)


def generate_attack_tree(
    finding: CandidateFinding,
    *,
    attack_surface: AttackSurfaceNode | None = None,
) -> AttackNode:
    cwe_id = extract_cwe_id(finding.vuln_class)
    location = _format_location(finding.sink.location.file, finding.sink.location.line)
    source_label = _source_label(finding.source.source_type)
    sanitizer_gaps = _sanitizer_summary(attack_surface)

    if cwe_id == "CWE-89":
        return AttackNode(
            label=f"Exfiltrate data via SQL injection at {location}",
            node_type="goal",
            children=[
                AttackNode(
                    label=f"Inject via {source_label}",
                    node_type="or",
                    children=[
                        AttackNode("UNION-based injection (extract additional tables)", "leaf"),
                        AttackNode(
                            "Boolean-based blind injection (binary search extraction)", "leaf"
                        ),
                        AttackNode("Time-based blind injection (SLEEP/BENCHMARK timing)", "leaf"),
                        AttackNode("Error-based injection (verbose SQL error messages)", "leaf"),
                    ],
                ),
                AttackNode(
                    label="Bypass sanitization",
                    node_type="and",
                    children=[
                        AttackNode(f"Identify sanitizer gaps ({sanitizer_gaps})", "leaf"),
                        AttackNode("Encoding bypass (URL-encode, Unicode, double-encode)", "leaf"),
                    ],
                ),
                AttackNode(
                    label="Escalate access",
                    node_type="and",
                    children=[
                        AttackNode("Read sensitive tables (users, credentials, tokens)", "leaf"),
                        AttackNode(
                            "Write or modify data (UPDATE or INSERT via stacked queries)", "leaf"
                        ),
                        AttackNode(
                            "OS command execution (xp_cmdshell or INTO OUTFILE paths)", "leaf"
                        ),
                    ],
                ),
            ],
        )

    if cwe_id == "CWE-79":
        return AttackNode(
            label=f"Execute arbitrary JavaScript via XSS at {location}",
            node_type="goal",
            children=[
                AttackNode(
                    label=f"Inject payload via {source_label}",
                    node_type="or",
                    children=[
                        AttackNode("Reflected XSS (URL parameter to response body)", "leaf"),
                        AttackNode(
                            "Stored XSS (persist payload and render to other users)", "leaf"
                        ),
                        AttackNode(
                            "DOM-based XSS (client-side sink without server round-trip)", "leaf"
                        ),
                    ],
                ),
                AttackNode(
                    label="Bypass output encoding",
                    node_type="and",
                    children=[
                        AttackNode(
                            "Context escape (HTML attribute, JS string, or CSS context)", "leaf"
                        ),
                        AttackNode("Encoding bypass (HTML entities or JS Unicode escapes)", "leaf"),
                    ],
                ),
                AttackNode(
                    label="Achieve impact",
                    node_type="and",
                    children=[
                        AttackNode("Session hijacking (steal cookies or bearer tokens)", "leaf"),
                        AttackNode("Credential theft (inject a fake login form)", "leaf"),
                        AttackNode("Keylogging (capture input field events)", "leaf"),
                        AttackNode("CSRF via XSS (perform actions as the victim)", "leaf"),
                    ],
                ),
            ],
        )

    if cwe_id == "CWE-78":
        return AttackNode(
            label=f"Execute OS commands via injection at {location}",
            node_type="goal",
            children=[
                AttackNode(
                    label=f"Inject via {source_label}",
                    node_type="or",
                    children=[
                        AttackNode("Shell metacharacter injection (; | && || ` $())", "leaf"),
                        AttackNode("Argument injection (-- prefix or flag smuggling)", "leaf"),
                        AttackNode(
                            "Environment variable injection (PATH or shell option manipulation)",
                            "leaf",
                        ),
                    ],
                ),
                AttackNode(
                    label="Bypass filters",
                    node_type="and",
                    children=[
                        AttackNode("Identify allowed characters and shell parsing gaps", "leaf"),
                        AttackNode("Encoding bypass (hex, octal, or ${IFS} for spaces)", "leaf"),
                    ],
                ),
                AttackNode(
                    label="Escalate impact",
                    node_type="and",
                    children=[
                        AttackNode("Reverse shell (nc, bash, or python one-liner)", "leaf"),
                        AttackNode("File read or write (cat secrets or drop a webshell)", "leaf"),
                        AttackNode(
                            "Privilege escalation (sudo misconfig or SUID binaries)", "leaf"
                        ),
                    ],
                ),
            ],
        )

    if cwe_id == "CWE-22":
        return AttackNode(
            label=f"Read arbitrary files via path traversal at {location}",
            node_type="goal",
            children=[
                AttackNode(
                    label=f"Supply malicious path via {source_label}",
                    node_type="or",
                    children=[
                        AttackNode("Dot-dot traversal (../) to escape the base directory", "leaf"),
                        AttackNode(
                            "Absolute path injection (/etc/passwd or Windows drive paths)", "leaf"
                        ),
                        AttackNode("Archive or symlink abuse to pivot reads and writes", "leaf"),
                    ],
                ),
                AttackNode(
                    label="Bypass normalization",
                    node_type="and",
                    children=[
                        AttackNode(
                            "Mixed separators or double-decoding to defeat path cleanup", "leaf"
                        ),
                        AttackNode("Unicode or percent-encoded traversal sequences", "leaf"),
                    ],
                ),
                AttackNode(
                    label="Achieve impact",
                    node_type="and",
                    children=[
                        AttackNode("Read credentials, keys, and configuration files", "leaf"),
                        AttackNode(
                            "Overwrite application files or authorized_keys when write "
                            "access exists",
                            "leaf",
                        ),
                    ],
                ),
            ],
        )

    if cwe_id == "CWE-918":
        return AttackNode(
            label=f"Reach internal services via SSRF at {location}",
            node_type="goal",
            children=[
                AttackNode(
                    label=f"Control outbound destination via {source_label}",
                    node_type="or",
                    children=[
                        AttackNode(
                            "Direct internal host access (RFC1918, localhost, or link-local)",
                            "leaf",
                        ),
                        AttackNode(
                            "Cloud metadata access (169.254.169.254 or provider equivalents)",
                            "leaf",
                        ),
                        AttackNode(
                            "Protocol smuggling (gopher, file, or redirect chaining)", "leaf"
                        ),
                    ],
                ),
                AttackNode(
                    label="Evade allowlists",
                    node_type="and",
                    children=[
                        AttackNode("DNS rebinding or alternate IP encodings", "leaf"),
                        AttackNode("Open redirect pivot to internal targets", "leaf"),
                    ],
                ),
                AttackNode(
                    label="Achieve impact",
                    node_type="and",
                    children=[
                        AttackNode(
                            "Query internal admin interfaces and service discovery endpoints",
                            "leaf",
                        ),
                        AttackNode("Exfiltrate tokens from metadata services", "leaf"),
                        AttackNode("Scan internal ports and map reachable services", "leaf"),
                    ],
                ),
            ],
        )

    if cwe_id == "CWE-502":
        return AttackNode(
            label=f"Execute a gadget chain via unsafe deserialization at {location}",
            node_type="goal",
            children=[
                AttackNode(
                    label=f"Deliver a crafted payload via {source_label}",
                    node_type="or",
                    children=[
                        AttackNode("Serialized object with attacker-chosen type metadata", "leaf"),
                        AttackNode(
                            "Gadget chain embedded in YAML, JSON, or binary payload", "leaf"
                        ),
                    ],
                ),
                AttackNode(
                    label="Satisfy gadget prerequisites",
                    node_type="and",
                    children=[
                        AttackNode(
                            "Identify available gadget classes and library versions", "leaf"
                        ),
                        AttackNode("Shape the object graph to trigger dangerous callbacks", "leaf"),
                    ],
                ),
                AttackNode(
                    label="Achieve impact",
                    node_type="and",
                    children=[
                        AttackNode("Remote code execution through gadget invocation", "leaf"),
                        AttackNode("Privilege escalation or configuration overwrite", "leaf"),
                    ],
                ),
            ],
        )

    if cwe_id == "CWE-639":
        return AttackNode(
            label=f"Access another user's records via IDOR at {location}",
            node_type="goal",
            children=[
                AttackNode(
                    label=f"Manipulate the identifier via {source_label}",
                    node_type="or",
                    children=[
                        AttackNode("Increment or enumerate numeric identifiers", "leaf"),
                        AttackNode("Swap UUIDs, slugs, or composite keys", "leaf"),
                        AttackNode("Replay links or requests issued for another user", "leaf"),
                    ],
                ),
                AttackNode(
                    label="Bypass ownership checks",
                    node_type="and",
                    children=[
                        AttackNode("Find endpoints that query by user-supplied key only", "leaf"),
                        AttackNode(
                            "Exploit missing tenant or account scoping in repository calls", "leaf"
                        ),
                    ],
                ),
                AttackNode(
                    label="Achieve impact",
                    node_type="and",
                    children=[
                        AttackNode("Read sensitive records belonging to another user", "leaf"),
                        AttackNode("Modify or delete another user's resources", "leaf"),
                    ],
                ),
            ],
        )

    return AttackNode(
        label=f"Exploit {cwe_title(cwe_id, fallback=finding.vuln_class)} at {location}",
        node_type="goal",
        children=[
            AttackNode(
                label=f"Control attacker input via {source_label}",
                node_type="or",
                children=[
                    AttackNode("Send crafted input that reaches the vulnerable code path", "leaf"),
                    AttackNode(
                        "Trigger edge cases through alternate encodings or request shapes", "leaf"
                    ),
                ],
            ),
            AttackNode(
                label="Reach the vulnerable sink",
                node_type="and",
                children=[
                    AttackNode(
                        f"Traverse the observed taint path ({len(finding.taint_path)} steps)",
                        "leaf",
                    ),
                    AttackNode(f"Exploit sanitizer gaps ({sanitizer_gaps})", "leaf"),
                ],
            ),
            AttackNode(
                label="Achieve impact",
                node_type="and",
                children=[
                    AttackNode("Read, modify, or execute attacker-controlled data", "leaf"),
                ],
            ),
        ],
    )


def render_tree(tree: AttackNode, *, format: TreeFormat = "markdown") -> str:
    if format == "json":
        return json.dumps(asdict(tree), indent=2)
    if format == "mermaid":
        return _render_mermaid(tree)
    return _render_markdown(tree)


def _render_markdown(tree: AttackNode) -> str:
    lines: list[str] = []

    def visit(node: AttackNode, depth: int) -> None:
        indent = "  " * depth
        if node.node_type == "leaf":
            lines.append(f"{indent}- {node.label}")
        else:
            lines.append(f"{indent}- **{node.node_type.upper()}**: {node.label}")
        for child in node.children:
            visit(child, depth + 1)

    visit(tree, 0)
    return "\n".join(lines)


def _render_mermaid(tree: AttackNode) -> str:
    lines = ["graph TD"]
    counter = 0

    def next_id() -> str:
        nonlocal counter
        counter += 1
        return f"N{counter}"

    def visit(node: AttackNode) -> str:
        node_id = next_id()
        label = _escape_mermaid_label(
            f"{node.node_type.upper()}: {node.label}" if node.node_type != "leaf" else node.label
        )
        lines.append(f'    {node_id}["{label}"]')
        for child in node.children:
            child_id = visit(child)
            lines.append(f"    {node_id} --> {child_id}")
        return node_id

    visit(tree)
    return "\n".join(lines)


def _format_location(file_name: str, line_number: int) -> str:
    return f"{file_name}:{line_number}"


def _source_label(source_type: str) -> str:
    normalized = source_type.replace("_", " ").replace(".", " ").strip()
    mapping = {
        "request body": "HTTP request body",
        "request param": "HTTP path parameter",
        "url param": "URL parameter",
        "header": "HTTP header",
        "cookie": "cookie value",
        "dependency manifest": "dependency manifest",
        "cli argument": "CLI argument",
    }
    lowered = normalized.lower()
    return mapping.get(lowered, normalized or "attacker-controlled input")


def _sanitizer_summary(attack_surface: AttackSurfaceNode | None) -> str:
    if attack_surface is None or not attack_surface.sanitizers_on_path:
        return "none detected"
    return ", ".join(attack_surface.sanitizers_on_path)


def _escape_mermaid_label(value: str) -> str:
    return value.replace('"', '\\"')


__all__ = [
    "AttackNode",
    "generate_attack_tree",
    "render_tree",
]
