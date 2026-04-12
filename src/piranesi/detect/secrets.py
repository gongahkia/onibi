from __future__ import annotations

import hashlib
import math
import os
import re
from collections import Counter, defaultdict
from collections.abc import Iterable
from dataclasses import dataclass
from pathlib import Path

from piranesi.models import CandidateFinding, SourceLocation, TaintSink, TaintSource

_DEFAULT_DATA_CATEGORIES = ["unknown"]
_DEFAULT_MAX_FILE_SIZE = 1_048_576
_DIRECTORY_EXCLUSIONS = frozenset({"node_modules", "vendor", ".git"})
_ENTROPY_THRESHOLD = 4.5
_MIN_ENTROPY_LENGTH = 21
_ENTROPY_TOKEN_PATTERN = re.compile(
    r"(?<![A-Za-z0-9+/=_-])[A-Za-z0-9+/=_-]{21,}(?![A-Za-z0-9+/=_-])"
)
_TEST_FILENAME_PATTERNS = (
    re.compile(r".*\.test\.[^.]+$", re.IGNORECASE),
    re.compile(r".*\.spec\.[^.]+$", re.IGNORECASE),
    re.compile(r"^test_[^.]+", re.IGNORECASE),
)
_TEST_DIRECTORY_NAMES = frozenset({"tests", "__tests__", "test"})
_REDACTION_MARKER = "[REDACTED_SECRET]"


@dataclass(frozen=True, slots=True)
class _SecretRule:
    kind: str
    pattern: re.Pattern[str]
    severity: str
    confidence: float


_SECRET_RULES = (
    _SecretRule(
        kind="aws_access_key",
        pattern=re.compile(r"AKIA[0-9A-Z]{16}"),
        severity="critical",
        confidence=0.99,
    ),
    _SecretRule(
        kind="stripe_secret_key",
        pattern=re.compile(r"sk_live_[A-Za-z0-9]{24,}"),
        severity="high",
        confidence=0.99,
    ),
    _SecretRule(
        kind="github_token",
        pattern=re.compile(r"ghp_[A-Za-z0-9]{36}"),
        severity="high",
        confidence=0.99,
    ),
    _SecretRule(
        kind="slack_token",
        pattern=re.compile(r"xox[bpors]-[A-Za-z0-9-]{10,}"),
        severity="high",
        confidence=0.99,
    ),
    _SecretRule(
        kind="sendgrid_api_key",
        pattern=re.compile(r"SG\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}"),
        severity="high",
        confidence=0.99,
    ),
    _SecretRule(
        kind="pem_private_key",
        pattern=re.compile(r"-----BEGIN[^\n]*PRIVATE KEY-----"),
        severity="critical",
        confidence=1.0,
    ),
)
_PRIVATE_KEY_KIND = "pem_private_key"


def extract_secret_findings(
    project_root: str | Path,
    *,
    include_tests: bool = False,
    max_file_size: int = _DEFAULT_MAX_FILE_SIZE,
    changed_files: set[Path] | None = None,
) -> tuple[CandidateFinding, ...]:
    root = Path(project_root).resolve(strict=False)
    findings: list[CandidateFinding] = []
    for path in _iter_candidate_files(
        root,
        include_tests=include_tests,
        max_file_size=max_file_size,
        changed_files=changed_files,
    ):
        findings.extend(_scan_file(path, project_root=root))
    return tuple(findings)


def shannon_entropy(value: str) -> float:
    if not value:
        return 0.0
    length = len(value)
    counts = Counter(value)
    entropy = 0.0
    for count in counts.values():
        probability = count / length
        entropy -= probability * math.log2(probability)
    return entropy


def _iter_candidate_files(
    project_root: Path,
    *,
    include_tests: bool,
    max_file_size: int,
    changed_files: set[Path] | None,
) -> Iterable[Path]:
    if changed_files is not None:
        normalized_paths = []
        for relative_path in changed_files:
            candidate = (project_root / relative_path).resolve(strict=False)
            normalized_paths.append(candidate)
        for path in sorted(normalized_paths):
            if not _should_scan_file(
                path,
                project_root=project_root,
                include_tests=include_tests,
                max_file_size=max_file_size,
            ):
                continue
            yield path
        return

    for current_root, directories, filenames in os.walk(project_root, topdown=True):
        directories[:] = sorted(
            directory for directory in directories if directory not in _DIRECTORY_EXCLUSIONS
        )
        base_dir = Path(current_root)
        for filename in sorted(filenames):
            path = base_dir / filename
            if not _should_scan_file(
                path,
                project_root=project_root,
                include_tests=include_tests,
                max_file_size=max_file_size,
            ):
                continue
            yield path


def _should_scan_file(
    path: Path,
    *,
    project_root: Path,
    include_tests: bool,
    max_file_size: int,
) -> bool:
    if not path.exists() or not path.is_file():
        return False

    try:
        relative_path = path.resolve(strict=False).relative_to(project_root)
    except ValueError:
        return False

    if path.name == ".env.example":
        return False
    if any(part in _DIRECTORY_EXCLUSIONS for part in relative_path.parts[:-1]):
        return False
    if not include_tests and _is_test_file(relative_path):
        return False
    try:
        return path.stat().st_size <= max_file_size
    except OSError:
        return False


def _is_test_file(relative_path: Path) -> bool:
    if any(part.lower() in _TEST_DIRECTORY_NAMES for part in relative_path.parts[:-1]):
        return True
    filename = relative_path.name
    return any(pattern.match(filename) for pattern in _TEST_FILENAME_PATTERNS)


def _scan_file(path: Path, *, project_root: Path) -> list[CandidateFinding]:
    try:
        raw = path.read_bytes()
    except OSError:
        return []

    if b"\x00" in raw:
        return []

    text = raw.decode("utf-8", errors="ignore")
    if not text:
        return []

    findings: list[CandidateFinding] = []
    matched_spans_by_line: defaultdict[int, list[tuple[int, int]]] = defaultdict(list)
    private_key_detected = False
    lines = text.splitlines()
    absolute_path = path.resolve(strict=False)
    relative_path = absolute_path.relative_to(project_root).as_posix()

    for line_number, line in enumerate(lines, start=1):
        for rule in _SECRET_RULES:
            for match in rule.pattern.finditer(line):
                matched_spans_by_line[line_number].append(match.span())
                if rule.kind == _PRIVATE_KEY_KIND:
                    private_key_detected = True
                findings.append(
                    _build_finding(
                        kind=rule.kind,
                        severity=rule.severity,
                        confidence=rule.confidence,
                        match_text=match.group(0),
                        absolute_path=absolute_path,
                        relative_path=relative_path,
                        line_number=line_number,
                        column_number=match.start() + 1,
                        line=line,
                        start=match.start(),
                        end=match.end(),
                    )
                )

    if private_key_detected:
        return findings

    for line_number, line in enumerate(lines, start=1):
        for match in _ENTROPY_TOKEN_PATTERN.finditer(line):
            if _spans_overlap(match.span(), matched_spans_by_line[line_number]):
                continue
            token = match.group(0)
            if len(token) < _MIN_ENTROPY_LENGTH or shannon_entropy(token) <= _ENTROPY_THRESHOLD:
                continue
            findings.append(
                _build_finding(
                    kind="high_entropy_string",
                    severity="high",
                    confidence=0.65,
                    match_text=token,
                    absolute_path=absolute_path,
                    relative_path=relative_path,
                    line_number=line_number,
                    column_number=match.start() + 1,
                    line=line,
                    start=match.start(),
                    end=match.end(),
                )
            )

    return findings


def _build_finding(
    *,
    kind: str,
    severity: str,
    confidence: float,
    match_text: str,
    absolute_path: Path,
    relative_path: str,
    line_number: int,
    column_number: int,
    line: str,
    start: int,
    end: int,
) -> CandidateFinding:
    location = SourceLocation(
        file=str(absolute_path),
        line=line_number,
        column=column_number,
        snippet=_redact_snippet(line, start=start, end=end),
    )
    return CandidateFinding(
        id=_secret_finding_id(
            kind=kind,
            relative_path=relative_path,
            line_number=line_number,
            column_number=column_number,
            match_text=match_text,
        ),
        vuln_class="CWE-798",
        source=TaintSource(
            location=location,
            source_type=f"hardcoded_{kind}",
            data_categories=list(_DEFAULT_DATA_CATEGORIES),
        ),
        sink=TaintSink(
            location=location,
            sink_type="hardcoded_credential",
            api_name=kind,
        ),
        taint_path=[],
        path_conditions=[],
        confidence=confidence,
        severity=severity,
    )


def _secret_finding_id(
    *,
    kind: str,
    relative_path: str,
    line_number: int,
    column_number: int,
    match_text: str,
) -> str:
    material = "|".join(
        (
            "CWE-798",
            kind,
            relative_path,
            str(line_number),
            str(column_number),
            hashlib.sha256(match_text.encode("utf-8")).hexdigest(),
        )
    )
    return hashlib.sha256(material.encode("utf-8")).hexdigest()


def _redact_snippet(line: str, *, start: int, end: int) -> str:
    return f"{line[:start]}{_REDACTION_MARKER}{line[end:]}"


def _spans_overlap(span: tuple[int, int], existing_spans: Iterable[tuple[int, int]]) -> bool:
    start, end = span
    for existing_start, existing_end in existing_spans:
        if start < existing_end and existing_start < end:
            return True
    return False


__all__ = ["extract_secret_findings", "shannon_entropy"]
