from enum import StrEnum

from pydantic import BaseModel


class Label(StrEnum):
    TRUE_POSITIVE = "true_positive"
    FALSE_POSITIVE = "false_positive"  # known safe, should NOT be flagged


class Complexity(StrEnum):
    SIMPLE = "simple"  # direct taint flow, single function
    INTERPROCEDURAL = "inter"  # crosses function boundaries
    CONTEXT_SENSITIVE = "ctx"  # requires context-sensitive analysis
    CROSS_MODULE = "cross_module"  # spans import boundaries
    MULTI_STEP = "multi_step"  # 3+ taint steps


class DiscoveryMethod(StrEnum):
    MANUAL = "manual"
    SYNTHETIC = "synthetic"
    CVE_MINING = "cve_mining"


class GroundTruthEntry(BaseModel):
    id: str  # e.g., "gt-001"
    source_project: str  # e.g., "owasp-nodegoat"
    commit_hash: str  # pinned commit
    cwe_id: str  # e.g., "CWE-89"
    cwe_name: str  # e.g., "SQL Injection"
    label: Label  # true_positive or false_positive
    affected_files: list[str]  # relative paths
    line_numbers: list[int]  # primary vulnerable lines
    taint_source: str  # e.g., "req.query.id"
    taint_sink: str  # e.g., "db.query()"
    taint_path: list[str]  # intermediate steps
    complexity: Complexity
    exploitable: bool  # is a working exploit possible?
    reference_exploit: str | None  # exploit description or script path
    reference_fix_commit: str | None  # commit that fixed the vuln
    notes: str  # additional context
    cve_id: str | None = None
    ghsa_id: str | None = None
    fix_commit: str | None = None
    vulnerable_commit: str | None = None
    patch_diff: str | None = None
    discovery_method: DiscoveryMethod = DiscoveryMethod.MANUAL
    language: str | None = None
    framework: str | None = None
    cvss_score: float | None = None
    taint_step_count: int | None = None
    taint_field_path: str | None = None
    field_sensitive_label: Label | None = None
