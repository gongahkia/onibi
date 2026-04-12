from __future__ import annotations

import ast
import hashlib
import re
from dataclasses import dataclass
from pathlib import Path
from string import ascii_letters, digits

from piranesi.models import CandidateFinding, SourceLocation, TaintSink, TaintSource
from piranesi.scan.specs import SinkType, SourceType

_SUPPORTED_SUFFIXES = frozenset({".js", ".jsx", ".ts", ".tsx", ".py", ".java"})
_JS_REGEX_LITERAL_PATTERN = re.compile(
    r"(?P<prefix>(?:^|[=(,:;\[]|\breturn\b)\s*)/(?P<pattern>(?:\\.|[^/\\\n])+)/(?P<flags>[a-z]*)"
)
_JS_REGEXP_CONSTRUCTOR_PATTERN = re.compile(
    r"new\s+RegExp\(\s*(?P<arg>`(?:\\.|[^`])*`|\"(?:\\.|[^\"\\])*\"|'(?:\\.|[^'\\])*')"
)
_PY_RE_COMPILE_PATTERN = re.compile(
    r"re[.]compile\(\s*(?P<arg>r?\"(?:\\.|[^\"\\])*\"|r?'(?:\\.|[^'\\])*')"
)
_JAVA_PATTERN_COMPILE_PATTERN = re.compile(
    r"Pattern[.]compile\(\s*(?P<arg>\"(?:\\.|[^\"\\])*\"|'(?:\\.|[^'\\])*')"
)
_QUANTIFIER_TOKEN = re.compile(r"(?:[+*?]|\{\d+(?:,\d*)?\})")
_BACKREFERENCE_WITH_QUANTIFIER = re.compile(r"\\[1-9](?:[+*?]|\{\d+(?:,\d*)?\})")


@dataclass(frozen=True, slots=True)
class RegexFinding:
    pattern: str
    file_path: str
    line_number: int
    vulnerability_type: str
    confidence: float
    api_name: str
    code: str


def extract_redos_findings(
    project_root: Path,
    *,
    files: list[Path] | tuple[Path, ...] | None = None,
) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    candidates = files if files is not None else list(project_root.rglob("*"))
    for path in candidates:
        if not path.is_file() or path.suffix not in _SUPPORTED_SUFFIXES:
            continue
        with path.open(encoding="utf-8", errors="ignore") as handle:
            content = handle.read()
        for finding in scan_text_for_redos(content, path=path):
            findings.append(_candidate_from_regex_finding(finding))
    return findings


def scan_text_for_redos(content: str, *, path: Path) -> tuple[RegexFinding, ...]:
    findings: list[RegexFinding] = []
    findings.extend(_scan_js_regex_literals(content, path=path))
    findings.extend(_scan_js_regexp_constructors(content, path=path))
    findings.extend(_scan_python_re_compile(content, path=path))
    findings.extend(_scan_java_pattern_compile(content, path=path))
    deduped: dict[tuple[str, int, str], RegexFinding] = {}
    for finding in findings:
        deduped.setdefault(
            (finding.file_path, finding.line_number, finding.vulnerability_type),
            finding,
        )
    return tuple(deduped.values())


def analyze_regex_pattern(pattern: str) -> tuple[str, float] | None:
    if _has_nested_quantifiers(pattern):
        return ("nested_quantifier", 0.95)
    if _has_backreference_quantifier(pattern):
        return ("backreference_quantifier", 0.9)
    if _has_overlapping_alternation(pattern):
        return ("overlapping_alternation", 0.85)
    return None


def _scan_js_regex_literals(content: str, *, path: Path) -> list[RegexFinding]:
    findings: list[RegexFinding] = []
    for match in _JS_REGEX_LITERAL_PATTERN.finditer(content):
        pattern = match.group("pattern")
        analysis = analyze_regex_pattern(pattern)
        if analysis is None:
            continue
        variant, confidence = analysis
        findings.append(
            RegexFinding(
                pattern=pattern,
                file_path=str(path),
                line_number=_line_number(content, match.start()),
                vulnerability_type=variant,
                confidence=confidence,
                api_name="regex_literal",
                code=match.group(0).strip(),
            )
        )
    return findings


def _scan_js_regexp_constructors(content: str, *, path: Path) -> list[RegexFinding]:
    findings: list[RegexFinding] = []
    for match in _JS_REGEXP_CONSTRUCTOR_PATTERN.finditer(content):
        pattern = _decode_literal(match.group("arg"))
        if pattern is None:
            continue
        analysis = analyze_regex_pattern(pattern)
        if analysis is None:
            continue
        variant, confidence = analysis
        findings.append(
            RegexFinding(
                pattern=pattern,
                file_path=str(path),
                line_number=_line_number(content, match.start()),
                vulnerability_type=variant,
                confidence=confidence,
                api_name="RegExp",
                code=match.group(0).strip(),
            )
        )
    return findings


def _scan_python_re_compile(content: str, *, path: Path) -> list[RegexFinding]:
    findings: list[RegexFinding] = []
    for match in _PY_RE_COMPILE_PATTERN.finditer(content):
        pattern = _decode_literal(match.group("arg"))
        if pattern is None:
            continue
        analysis = analyze_regex_pattern(pattern)
        if analysis is None:
            continue
        variant, confidence = analysis
        findings.append(
            RegexFinding(
                pattern=pattern,
                file_path=str(path),
                line_number=_line_number(content, match.start()),
                vulnerability_type=variant,
                confidence=confidence,
                api_name="re.compile",
                code=match.group(0).strip(),
            )
        )
    return findings


def _scan_java_pattern_compile(content: str, *, path: Path) -> list[RegexFinding]:
    findings: list[RegexFinding] = []
    for match in _JAVA_PATTERN_COMPILE_PATTERN.finditer(content):
        pattern = _decode_literal(match.group("arg"))
        if pattern is None:
            continue
        analysis = analyze_regex_pattern(pattern)
        if analysis is None:
            continue
        variant, confidence = analysis
        findings.append(
            RegexFinding(
                pattern=pattern,
                file_path=str(path),
                line_number=_line_number(content, match.start()),
                vulnerability_type=variant,
                confidence=confidence,
                api_name="Pattern.compile",
                code=match.group(0).strip(),
            )
        )
    return findings


def _candidate_from_regex_finding(finding: RegexFinding) -> CandidateFinding:
    location = SourceLocation(
        file=finding.file_path,
        line=finding.line_number,
        column=1,
        snippet=finding.code,
    )
    fingerprint = hashlib.sha256(
        f"{finding.file_path}|{finding.line_number}|{finding.pattern}|{finding.vulnerability_type}".encode()
    ).hexdigest()
    return CandidateFinding(
        id=fingerprint,
        vuln_class="CWE-1333",
        source=TaintSource(
            location=location,
            source_type=SourceType.CUSTOM.value,
            data_categories=["unknown"],
            parameter_name=None,
        ),
        sink=TaintSink(
            location=location,
            sink_type=SinkType.REGEX_INJECTION.value,
            api_name=finding.api_name,
        ),
        taint_path=[],
        path_conditions=[],
        confidence=finding.confidence,
        severity="medium",
        metadata={
            "regex_pattern": finding.pattern,
            "redos_variant": finding.vulnerability_type,
            "static_regex": True,
        },
    )


def _has_nested_quantifiers(pattern: str) -> bool:
    for body in _quantified_group_bodies(pattern):
        quantifier_count = len(_QUANTIFIER_TOKEN.findall(body))
        if quantifier_count >= 1:
            return True
    return False


def _has_backreference_quantifier(pattern: str) -> bool:
    return _BACKREFERENCE_WITH_QUANTIFIER.search(pattern) is not None


def _has_overlapping_alternation(pattern: str) -> bool:
    for body in _quantified_group_bodies(pattern):
        branches = _split_top_level_alternation(body)
        if len(branches) < 2:
            continue
        signatures = [_branch_signature(branch) for branch in branches]
        for index, left in enumerate(signatures):
            for right in signatures[index + 1 :]:
                if left & right:
                    return True
    return False


def _quantified_group_bodies(pattern: str) -> tuple[str, ...]:
    groups: list[str] = []
    stack: list[int] = []
    escaped = False
    in_class = False
    for index, char in enumerate(pattern):
        if escaped:
            escaped = False
            continue
        if char == "\\":
            escaped = True
            continue
        if char == "[" and not in_class:
            in_class = True
            continue
        if char == "]" and in_class:
            in_class = False
            continue
        if in_class:
            continue
        if char == "(":
            stack.append(index)
            continue
        if char != ")" or not stack:
            continue
        start = stack.pop()
        if index + 1 >= len(pattern):
            continue
        quantifier_match = _QUANTIFIER_TOKEN.match(pattern[index + 1 :])
        if quantifier_match is None:
            continue
        groups.append(pattern[start + 1 : index])
    return tuple(groups)


def _split_top_level_alternation(body: str) -> tuple[str, ...]:
    parts: list[str] = []
    depth = 0
    escaped = False
    in_class = False
    start = 0
    for index, char in enumerate(body):
        if escaped:
            escaped = False
            continue
        if char == "\\":
            escaped = True
            continue
        if char == "[" and not in_class:
            in_class = True
            continue
        if char == "]" and in_class:
            in_class = False
            continue
        if in_class:
            continue
        if char == "(":
            depth += 1
            continue
        if char == ")":
            depth = max(depth - 1, 0)
            continue
        if char == "|" and depth == 0:
            parts.append(body[start:index])
            start = index + 1
    parts.append(body[start:])
    return tuple(part.strip() for part in parts if part.strip())


def _branch_signature(branch: str) -> frozenset[str]:
    normalized = branch
    while normalized.startswith("?:"):
        normalized = normalized[2:]
    normalized = normalized.lstrip("^")
    if not normalized:
        return frozenset()
    if normalized.startswith("\\w"):
        return frozenset(ascii_letters + digits + "_")
    if normalized.startswith("\\d"):
        return frozenset(digits)
    if normalized.startswith("\\s"):
        return frozenset(" \t\r\n")
    if normalized.startswith("["):
        closing = normalized.find("]")
        if closing != -1:
            return frozenset(_expand_character_class(normalized[1:closing]))
    if normalized.startswith("\\") and len(normalized) > 1:
        return frozenset({normalized[1]})
    return frozenset({normalized[0]})


def _expand_character_class(body: str) -> set[str]:
    expanded: set[str] = set()
    index = 0
    while index < len(body):
        if index + 2 < len(body) and body[index + 1] == "-":
            start = body[index]
            end = body[index + 2]
            expanded.update(chr(code) for code in range(ord(start), ord(end) + 1))
            index += 3
            continue
        expanded.add(body[index])
        index += 1
    return expanded


def _decode_literal(raw_value: str) -> str | None:
    literal = raw_value.strip()
    if literal.startswith("r'") or literal.startswith('r"'):
        literal = literal[1:]
    if literal.startswith("`") and literal.endswith("`"):
        return literal[1:-1]
    try:
        decoded = ast.literal_eval(literal)
    except (SyntaxError, ValueError):
        return None
    return decoded if isinstance(decoded, str) else None


def _line_number(content: str, offset: int) -> int:
    return content.count("\n", 0, offset) + 1


__all__ = [
    "RegexFinding",
    "analyze_regex_pattern",
    "extract_redos_findings",
    "scan_text_for_redos",
]
