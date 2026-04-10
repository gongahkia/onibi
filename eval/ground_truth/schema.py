from enum import StrEnum

from pydantic import BaseModel


class Label(StrEnum):
    TRUE_POSITIVE = "true_positive"
    FALSE_POSITIVE = "false_positive"  # known safe, should NOT be flagged


class Complexity(StrEnum):
    SIMPLE = "simple"  # direct taint flow, single function
    INTERPROCEDURAL = "inter"  # crosses function boundaries
    CONTEXT_SENSITIVE = "ctx"  # requires context-sensitive analysis


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
