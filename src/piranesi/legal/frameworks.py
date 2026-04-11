from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True)
class FrameworkSpec:
    key: str
    short_label: str
    long_label: str
    category: str


FRAMEWORKS: tuple[FrameworkSpec, ...] = (
    FrameworkSpec("GDPR", "GDPR", "General Data Protection Regulation (GDPR)", "Privacy"),
    FrameworkSpec(
        "CCPA",
        "CCPA",
        "California Consumer Privacy Act / California Privacy Rights Act (CCPA/CPRA)",
        "Privacy",
    ),
    FrameworkSpec(
        "HIPAA",
        "HIPAA",
        "Health Insurance Portability and Accountability Act (HIPAA)",
        "Privacy",
    ),
    FrameworkSpec(
        "PDPA",
        "PDPA",
        "Personal Data Protection Act 2012 (PDPA)",
        "Privacy",
    ),
    FrameworkSpec(
        "MAS_TRM",
        "MAS TRM",
        "MAS Technology Risk Management Guidelines (MAS TRM)",
        "Financial",
    ),
    FrameworkSpec("NIS2", "NIS2", "NIS2 Directive (Directive (EU) 2022/2555)", "Cyber"),
    FrameworkSpec(
        "SOC2",
        "SOC 2",
        "SOC 2 Type II Trust Services Criteria (SOC 2)",
        "Cyber",
    ),
    FrameworkSpec(
        "PCI_DSS",
        "PCI-DSS",
        "Payment Card Industry Data Security Standard v4.0 (PCI-DSS)",
        "Financial",
    ),
    FrameworkSpec(
        "EU_AI_ACT",
        "EU AI",
        "EU Artificial Intelligence Act (EU AI Act)",
        "Cyber",
    ),
    FrameworkSpec(
        "ISO_27001",
        "ISO 27001",
        "ISO/IEC 27001:2022 Annex A",
        "Standards",
    ),
    FrameworkSpec(
        "NIST_CSF",
        "NIST CSF",
        "NIST Cybersecurity Framework 2.0",
        "Standards",
    ),
    FrameworkSpec(
        "CIS_V8",
        "CIS v8",
        "CIS Controls v8 - Control 16 Application Security",
        "Standards",
    ),
)

FRAMEWORK_BY_KEY: dict[str, FrameworkSpec] = {framework.key: framework for framework in FRAMEWORKS}

FRAMEWORK_CATEGORY_GROUPS: tuple[tuple[str, tuple[str, ...]], ...] = (
    ("Privacy", ("PDPA", "GDPR", "CCPA", "HIPAA")),
    ("Financial", ("MAS_TRM", "PCI_DSS")),
    ("Cyber", ("NIS2", "SOC2", "EU_AI_ACT")),
    ("Standards", ("ISO_27001", "NIST_CSF", "CIS_V8")),
)

FRAMEWORK_ALIASES: dict[str, str] = {
    "ccpa": "CCPA",
    "cis": "CIS_V8",
    "cis_v8": "CIS_V8",
    "cis-v8": "CIS_V8",
    "eu_ai_act": "EU_AI_ACT",
    "eu-ai-act": "EU_AI_ACT",
    "euaiact": "EU_AI_ACT",
    "gdpr": "GDPR",
    "hipaa": "HIPAA",
    "iso27001": "ISO_27001",
    "iso_27001": "ISO_27001",
    "iso-27001": "ISO_27001",
    "mas_trm": "MAS_TRM",
    "mas-trm": "MAS_TRM",
    "nist": "NIST_CSF",
    "nist_csf": "NIST_CSF",
    "nist-csf": "NIST_CSF",
    "nis2": "NIS2",
    "pci": "PCI_DSS",
    "pci_dss": "PCI_DSS",
    "pci-dss": "PCI_DSS",
    "pdpa": "PDPA",
    "soc2": "SOC2",
    "soc_2": "SOC2",
    "soc-2": "SOC2",
}


def resolve_framework_key(value: str) -> str | None:
    normalized = value.strip().lower().replace(" ", "_")
    if not normalized:
        return None
    if normalized in FRAMEWORK_ALIASES:
        return FRAMEWORK_ALIASES[normalized]
    upper = normalized.upper()
    if upper in FRAMEWORK_BY_KEY:
        return upper
    return None


__all__ = [
    "FRAMEWORKS",
    "FRAMEWORK_ALIASES",
    "FRAMEWORK_BY_KEY",
    "FRAMEWORK_CATEGORY_GROUPS",
    "FrameworkSpec",
    "resolve_framework_key",
]
