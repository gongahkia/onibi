from __future__ import annotations

import re
from dataclasses import dataclass
from pathlib import Path
from typing import Final

from piranesi.legal.engine import Rule
from piranesi.legal.rules.common import (
    RegulatoryRuleSpec,
    compile_rule_specs,
    default_rules_path,
    load_rule_specs,
)

PCI_DSS_RULES_PATH = default_rules_path("pci_dss.toml")
_SOURCE_SUFFIXES: Final[set[str]] = {
    ".cjs",
    ".cts",
    ".go",
    ".java",
    ".js",
    ".jsx",
    ".mjs",
    ".mts",
    ".py",
    ".ts",
    ".tsx",
}
_EXCLUDED_PARTS: Final[set[str]] = {
    ".git",
    ".venv",
    "__pycache__",
    "build",
    "dist",
    "node_modules",
    "site-packages",
    "target",
    "vendor",
}
_BLOCK_COMMENT = re.compile(r"/\*.*?\*/", re.DOTALL)
_JS_LINE_COMMENT = re.compile(r"(?m)^\s*//.*$")
_PY_LINE_COMMENT = re.compile(r"(?m)^\s*#.*$")
_IMPORT_KEYWORDS: Final[tuple[str, ...]] = (
    "stripe",
    "braintree",
    "adyen",
    "square",
    "paypal",
    "authorize.net",
    "@stripe/stripe-js",
    "razorpay",
)
_IDENTIFIER_KEYWORDS: Final[tuple[str, ...]] = (
    "payment",
    "card",
    "checkout",
    "billing",
    "cardnumber",
    "card_number",
    "cvv",
    "cvc",
    "pan",
    "expiry_date",
    "cardholder",
    "payment_intent",
    "checkout_session",
)
_API_PATTERNS: Final[tuple[re.Pattern[str], ...]] = (
    re.compile(r"/v\d+/(?:charges|payments)\b", re.IGNORECASE),
    re.compile(r"/checkout\b", re.IGNORECASE),
    re.compile(r"/billing\b", re.IGNORECASE),
)


@dataclass(frozen=True)
class PaymentScopeHit:
    kind: str
    value: str
    file: str


@dataclass(frozen=True)
class PaymentScopeAssessment:
    is_payment_processing: bool
    hits: tuple[PaymentScopeHit, ...]

    @property
    def hit_count(self) -> int:
        return len(self.hits)


def load_pci_dss_rule_specs(path: Path | None = None) -> list[RegulatoryRuleSpec]:
    return load_rule_specs(path or PCI_DSS_RULES_PATH)


def load_pci_dss_rules(path: Path | None = None) -> list[Rule]:
    return compile_rule_specs(load_pci_dss_rule_specs(path))


def detect_payment_processing_scope(
    project_root: Path,
    *,
    files: tuple[str, ...] | list[str] | None = None,
) -> PaymentScopeAssessment:
    resolved_root = project_root.resolve(strict=False)
    raw_hits: list[PaymentScopeHit] = []
    for path in _candidate_source_paths(resolved_root, files=files):
        try:
            content = path.read_text(encoding="utf-8", errors="ignore")
        except OSError:
            continue
        stripped = _strip_comments(content, suffix=path.suffix.lower())
        raw_hits.extend(_scope_hits_for_text(stripped, path))

    deduped: list[PaymentScopeHit] = []
    seen: set[tuple[str, str, str]] = set()
    for hit in raw_hits:
        key = (hit.kind, hit.value.lower(), hit.file)
        if key in seen:
            continue
        seen.add(key)
        deduped.append(hit)

    return PaymentScopeAssessment(
        is_payment_processing=len(deduped) >= 2,
        hits=tuple(deduped),
    )


def _candidate_source_paths(
    project_root: Path,
    *,
    files: tuple[str, ...] | list[str] | None,
) -> list[Path]:
    if files is not None:
        resolved: list[Path] = []
        seen: set[Path] = set()
        for raw_path in files:
            path = Path(raw_path)
            candidate = (
                path.resolve(strict=False)
                if path.is_absolute()
                else (project_root / path).resolve(strict=False)
            )
            if candidate in seen or candidate.suffix.lower() not in _SOURCE_SUFFIXES:
                continue
            seen.add(candidate)
            resolved.append(candidate)
        return resolved

    paths: list[Path] = []
    for path in project_root.rglob("*"):
        if not path.is_file():
            continue
        if path.suffix.lower() not in _SOURCE_SUFFIXES:
            continue
        if any(part in _EXCLUDED_PARTS for part in path.parts):
            continue
        paths.append(path.resolve(strict=False))
    return paths


def _strip_comments(content: str, *, suffix: str) -> str:
    stripped = _BLOCK_COMMENT.sub("", content)
    if suffix in {".py"}:
        return _PY_LINE_COMMENT.sub("", stripped)
    return _JS_LINE_COMMENT.sub("", stripped)


def _scope_hits_for_text(content: str, path: Path) -> list[PaymentScopeHit]:
    lowered = content.lower()
    hits: list[PaymentScopeHit] = []
    for keyword in _IMPORT_KEYWORDS:
        if keyword in lowered:
            hits.append(
                PaymentScopeHit(kind="import", value=keyword, file=str(path.resolve(strict=False)))
            )
    for keyword in _IDENTIFIER_KEYWORDS:
        pattern = re.compile(rf"\b{re.escape(keyword)}\b", re.IGNORECASE)
        if pattern.search(content):
            hits.append(
                PaymentScopeHit(
                    kind="identifier",
                    value=keyword,
                    file=str(path.resolve(strict=False)),
                )
            )
    for pattern in _API_PATTERNS:
        if pattern.search(content):
            hits.append(
                PaymentScopeHit(
                    kind="api_pattern",
                    value=pattern.pattern,
                    file=str(path.resolve(strict=False)),
                )
            )
    return hits


__all__ = [
    "PCI_DSS_RULES_PATH",
    "PaymentScopeAssessment",
    "PaymentScopeHit",
    "detect_payment_processing_scope",
    "load_pci_dss_rule_specs",
    "load_pci_dss_rules",
]
