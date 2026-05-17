from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True)
class FrameworkSpec:
    key: str
    short_label: str
    long_label: str
    category: str
    version: str
    mapping_last_reviewed: str
    mapping_reviewer: str
    mapping_source: str
    mapping_confidence: float


_DEFAULT_REVIEW_DATE = "2026-04-16"
_DEFAULT_REVIEWER = "Piranesi compliance maintainers"


def _framework(
    key: str,
    short_label: str,
    long_label: str,
    category: str,
    *,
    version: str,
    mapping_source: str,
    mapping_confidence: float = 0.8,
) -> FrameworkSpec:
    return FrameworkSpec(
        key=key,
        short_label=short_label,
        long_label=long_label,
        category=category,
        version=version,
        mapping_last_reviewed=_DEFAULT_REVIEW_DATE,
        mapping_reviewer=_DEFAULT_REVIEWER,
        mapping_source=mapping_source,
        mapping_confidence=mapping_confidence,
    )


FRAMEWORKS: tuple[FrameworkSpec, ...] = (
    _framework(
        "GDPR",
        "GDPR",
        "General Data Protection Regulation (GDPR)",
        "Privacy",
        version="Regulation (EU) 2016/679",
        mapping_source="rules/gdpr.toml",
        mapping_confidence=0.9,
    ),
    _framework(
        "CCPA",
        "CCPA",
        "California Consumer Privacy Act / California Privacy Rights Act (CCPA/CPRA)",
        "Privacy",
        version="CCPA/CPRA (effective 2023-01)",
        mapping_source="rules/ccpa.toml",
        mapping_confidence=0.86,
    ),
    _framework(
        "HIPAA",
        "HIPAA",
        "Health Insurance Portability and Accountability Act (HIPAA)",
        "Privacy",
        version="45 CFR Parts 160 and 164",
        mapping_source="rules/hipaa.toml",
        mapping_confidence=0.88,
    ),
    _framework(
        "PDPA",
        "PDPA",
        "Personal Data Protection Act 2012 (PDPA)",
        "Privacy",
        version="PDPA 2012 (Singapore)",
        mapping_source="rules/pdpa.toml",
        mapping_confidence=0.84,
    ),
    _framework(
        "MAS_TRM",
        "MAS TRM",
        "MAS Technology Risk Management Guidelines (MAS TRM)",
        "Financial",
        version="MAS TRM Guidelines (2021 update)",
        mapping_source="rules/mas_trm.toml",
        mapping_confidence=0.78,
    ),
    _framework(
        "NIS2",
        "NIS2",
        "NIS2 Directive (Directive (EU) 2022/2555)",
        "Cyber",
        version="Directive (EU) 2022/2555",
        mapping_source="rules/nis2.toml",
        mapping_confidence=0.82,
    ),
    _framework(
        "SOC2",
        "SOC 2",
        "SOC 2 Type II Trust Services Criteria (SOC 2)",
        "Cyber",
        version="TSC 2022",
        mapping_source="rules/soc2.toml",
        mapping_confidence=0.74,
    ),
    _framework(
        "PCI_DSS",
        "PCI-DSS",
        "Payment Card Industry Data Security Standard v4.0 (PCI-DSS)",
        "Financial",
        version="PCI DSS v4.0.1",
        mapping_source="rules/pci_dss.toml",
        mapping_confidence=0.8,
    ),
    _framework(
        "EU_AI_ACT",
        "EU AI",
        "EU Artificial Intelligence Act (EU AI Act)",
        "Cyber",
        version="EU AI Act (2024)",
        mapping_source="rules/eu_ai_act.toml",
        mapping_confidence=0.7,
    ),
    _framework(
        "ISO_27001",
        "ISO 27001",
        "ISO/IEC 27001:2022 Annex A",
        "Standards",
        version="ISO/IEC 27001:2022",
        mapping_source="rules/iso27001.toml",
        mapping_confidence=0.76,
    ),
    _framework(
        "NIST_CSF",
        "NIST CSF",
        "NIST Cybersecurity Framework 2.0",
        "Standards",
        version="NIST CSF 2.0",
        mapping_source="rules/nist_csf.toml",
        mapping_confidence=0.73,
    ),
    _framework(
        "CIS_V8",
        "CIS v8",
        "CIS Controls v8 - Control 16 Application Security",
        "Standards",
        version="CIS Controls v8",
        mapping_source="rules/cis.toml",
        mapping_confidence=0.72,
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
