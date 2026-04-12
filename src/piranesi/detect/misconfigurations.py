from __future__ import annotations

import hashlib
import json
import re
from bisect import bisect_right
from collections.abc import Sequence
from dataclasses import dataclass
from pathlib import Path

from piranesi.detect.flows import severity_for_cwe
from piranesi.models import CandidateFinding, SourceLocation, TaintSink, TaintSource

_SOURCE_FILE_EXTENSIONS = frozenset({".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs"})
_DEFAULT_DATA_CATEGORIES = ["unknown"]
_STATIC_SOURCE_TYPE = "security_configuration"
_STATIC_SINK_TYPE = "security_misconfiguration"

_EXPRESS_IMPORT_PATTERN = re.compile(
    r'(?m)^\s*(?:import\s+.+\s+from\s+[\'"]express[\'"]|'
    r'(?:const|let|var)\s+\w+\s*=\s*require\([\'"]express[\'"]\)|'
    r'require\([\'"]express[\'"]\))'
)
_EXPRESS_APP_PATTERN = re.compile(r"\bexpress\s*\(")
_HELMET_PATTERN = re.compile(r"\bhelmet\s*\(")
_HELMET_IMPORT_PATTERN = re.compile(
    r'(?m)^\s*(?:import\s+.+\s+from\s+[\'"]helmet[\'"]|'
    r'(?:const|let|var)\s+\w+\s*=\s*require\([\'"]helmet[\'"]\))'
)
_RESPONSE_PATTERN = re.compile(r"\b(?P<receiver>[A-Za-z_$][\w$]*)\.(?P<method>send|render)\s*\(")
_CORS_WILDCARD_HEADER_PATTERN = re.compile(
    r"\b(?P<receiver>[A-Za-z_$][\w$]*)\.(?P<api>setHeader|header|set)\s*"
    r"\(\s*['\"]Access-Control-Allow-Origin['\"]\s*,\s*['\"]\*['\"]\s*\)"
)
_CORS_MIDDLEWARE_PATTERN = re.compile(r"\bcors\s*\(\s*{(?P<body>.*?)}\s*\)", re.DOTALL)
_COOKIE_OBJECT_PATTERN = re.compile(r"cookie\s*:\s*{(?P<body>.*?)}", re.DOTALL)
_COOKIE_SECURE_FALSE_PATTERN = re.compile(r"\bsecure\s*:\s*false\b")
_COOKIE_HTTP_ONLY_FALSE_PATTERN = re.compile(r"\bhttpOnly\s*:\s*false\b")
_ORIGIN_WILDCARD_PATTERN = re.compile(r"\borigin\s*:\s*['\"]\*['\"]")
_CREDENTIALS_TRUE_PATTERN = re.compile(r"\bcredentials\s*:\s*true\b")

_MISSING_SECURITY_HEADERS: tuple[tuple[str, str], ...] = (
    ("X-Frame-Options", "CWE-1021"),
    ("Content-Security-Policy", "CWE-693"),
    ("Strict-Transport-Security", "CWE-319"),
)


@dataclass(frozen=True, slots=True)
class _ScannedFile:
    path: Path
    text: str
    line_starts: tuple[int, ...]
    brace_pairs: dict[int, int]

    @classmethod
    def load(cls, path: Path) -> _ScannedFile | None:
        try:
            text = path.read_text(encoding="utf-8")
        except OSError:
            return None
        return cls(
            path=path.resolve(strict=False),
            text=text,
            line_starts=_line_starts(text),
            brace_pairs=_brace_pairs(text),
        )

    def location_for_index(self, index: int, *, snippet: str | None = None) -> SourceLocation:
        line_number, column_number = _line_and_column(self.line_starts, index)
        return SourceLocation(
            file=str(self.path),
            line=line_number,
            column=column_number,
            snippet=snippet or _line_text(self.text, line_number),
        )

    def containing_block(self, index: int) -> tuple[int, int]:
        block_start = 0
        block_end = len(self.text)
        for start, end in self.brace_pairs.items():
            if start < index < end and start >= block_start:
                block_start = start
                block_end = end
        return block_start, block_end


def extract_misconfiguration_findings(
    project_root: str | Path,
    *,
    frameworks: Sequence[str] | None = None,
    files: Sequence[Path] | None = None,
) -> tuple[CandidateFinding, ...]:
    root = Path(project_root).resolve(strict=False)
    scanned_files = tuple(_load_scanned_files(root, files=files))
    if not scanned_files:
        return ()

    normalized_frameworks = {framework.lower() for framework in frameworks or ()}
    if "express" not in normalized_frameworks and not _looks_like_express_project(
        root,
        scanned_files,
    ):
        return ()

    has_helmet = any(_HELMET_PATTERN.search(file.text) for file in scanned_files)
    findings: list[CandidateFinding] = []
    if not has_helmet:
        findings.extend(_detect_missing_helmet(scanned_files))

    for scanned_file in scanned_files:
        findings.extend(_detect_wildcard_cors(scanned_file))
        findings.extend(_detect_insecure_cookie_settings(scanned_file))
        if not has_helmet:
            findings.extend(_detect_missing_security_headers(scanned_file))

    return tuple(_dedupe_findings(findings))


def _load_scanned_files(project_root: Path, *, files: Sequence[Path] | None) -> list[_ScannedFile]:
    candidate_paths = (
        [Path(path) for path in files]
        if files is not None
        else sorted(path for path in project_root.rglob("*") if path.is_file())
    )
    scanned_files: list[_ScannedFile] = []
    for path in candidate_paths:
        if path.suffix not in _SOURCE_FILE_EXTENSIONS:
            continue
        scanned_file = _ScannedFile.load(path)
        if scanned_file is not None:
            scanned_files.append(scanned_file)
    return scanned_files


def _looks_like_express_project(project_root: Path, scanned_files: Sequence[_ScannedFile]) -> bool:
    package_json = project_root / "package.json"
    if package_json.is_file():
        try:
            payload = json.loads(package_json.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            payload = None
        if isinstance(payload, dict):
            for key in (
                "dependencies",
                "devDependencies",
                "peerDependencies",
                "optionalDependencies",
            ):
                section = payload.get(key)
                if isinstance(section, dict) and "express" in section:
                    return True

    return any(
        _EXPRESS_IMPORT_PATTERN.search(scanned_file.text)
        or _EXPRESS_APP_PATTERN.search(scanned_file.text)
        for scanned_file in scanned_files
    )


def _detect_wildcard_cors(scanned_file: _ScannedFile) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for match in _CORS_WILDCARD_HEADER_PATTERN.finditer(scanned_file.text):
        receiver = match.group("receiver")
        block_start, block_end = scanned_file.containing_block(match.start())
        block_text = scanned_file.text[block_start:block_end]
        credentials_pattern = re.compile(
            rf"\b{re.escape(receiver)}\.(?:setHeader|header|set)\s*"
            r"\(\s*['\"]Access-Control-Allow-Credentials['\"]\s*,\s*(?:['\"]true['\"]|true)\s*\)"
        )
        if not credentials_pattern.search(block_text):
            continue
        findings.append(
            _build_static_finding(
                cwe_id="CWE-942",
                location=scanned_file.location_for_index(match.start()),
                api_name=f"{receiver}.{match.group('api')}",
                parameter_name="Access-Control-Allow-Origin",
            )
        )

    for match in _CORS_MIDDLEWARE_PATTERN.finditer(scanned_file.text):
        body = match.group("body")
        if not _ORIGIN_WILDCARD_PATTERN.search(body):
            continue
        if not _CREDENTIALS_TRUE_PATTERN.search(body):
            continue
        findings.append(
            _build_static_finding(
                cwe_id="CWE-942",
                location=scanned_file.location_for_index(match.start()),
                api_name="cors",
                parameter_name="Access-Control-Allow-Origin",
            )
        )
    return findings


def _detect_missing_security_headers(scanned_file: _ScannedFile) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for match in _RESPONSE_PATTERN.finditer(scanned_file.text):
        receiver = match.group("receiver")
        block_start, _ = scanned_file.containing_block(match.start())
        prior_text = scanned_file.text[block_start : match.start()]
        location = scanned_file.location_for_index(match.start())
        api_name = f"{receiver}.{match.group('method')}"
        for header_name, cwe_id in _MISSING_SECURITY_HEADERS:
            if _has_upstream_header_setter(prior_text, receiver=receiver, header_name=header_name):
                continue
            findings.append(
                _build_static_finding(
                    cwe_id=cwe_id,
                    location=location,
                    api_name=api_name,
                    parameter_name=header_name,
                )
            )
    return findings


def _detect_insecure_cookie_settings(scanned_file: _ScannedFile) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for match in _COOKIE_OBJECT_PATTERN.finditer(scanned_file.text):
        body = match.group("body")
        body_start = match.start("body")
        secure_match = _COOKIE_SECURE_FALSE_PATTERN.search(body)
        if secure_match is not None:
            findings.append(
                _build_static_finding(
                    cwe_id="CWE-614",
                    location=scanned_file.location_for_index(body_start + secure_match.start()),
                    api_name="cookie.secure",
                    parameter_name="secure",
                )
            )
        http_only_match = _COOKIE_HTTP_ONLY_FALSE_PATTERN.search(body)
        if http_only_match is not None:
            findings.append(
                _build_static_finding(
                    cwe_id="CWE-1004",
                    location=scanned_file.location_for_index(body_start + http_only_match.start()),
                    api_name="cookie.httpOnly",
                    parameter_name="httpOnly",
                )
            )
    return findings


def _detect_missing_helmet(scanned_files: Sequence[_ScannedFile]) -> list[CandidateFinding]:
    anchor_file, anchor_index = _helmet_anchor(scanned_files)
    location = anchor_file.location_for_index(anchor_index)
    return [
        _build_static_finding(
            cwe_id="CWE-693",
            location=location,
            api_name="helmet()",
            parameter_name="helmet",
        )
    ]


def _helmet_anchor(scanned_files: Sequence[_ScannedFile]) -> tuple[_ScannedFile, int]:
    for scanned_file in scanned_files:
        for pattern in (_EXPRESS_IMPORT_PATTERN, _EXPRESS_APP_PATTERN):
            match = pattern.search(scanned_file.text)
            if match is not None:
                return scanned_file, match.start()
    return scanned_files[0], 0


def _has_upstream_header_setter(prior_text: str, *, receiver: str, header_name: str) -> bool:
    pattern = re.compile(
        rf"\b{re.escape(receiver)}\.(?:setHeader|header|set)\s*"
        rf"\(\s*['\"]{re.escape(header_name)}['\"]\s*,"
    )
    return pattern.search(prior_text) is not None


def _build_static_finding(
    *,
    cwe_id: str,
    location: SourceLocation,
    api_name: str,
    parameter_name: str | None,
) -> CandidateFinding:
    return CandidateFinding(
        id=_static_finding_id(
            cwe_id=cwe_id,
            file=location.file,
            line=location.line,
            column=location.column,
            api_name=api_name,
        ),
        vuln_class=cwe_id,
        source=TaintSource(
            location=location,
            source_type=_STATIC_SOURCE_TYPE,
            data_categories=list(_DEFAULT_DATA_CATEGORIES),
            parameter_name=parameter_name,
        ),
        sink=TaintSink(
            location=location,
            sink_type=_STATIC_SINK_TYPE,
            api_name=api_name,
        ),
        taint_path=[],
        path_conditions=[],
        confidence=0.9,
        severity=severity_for_cwe(cwe_id),
    )


def _static_finding_id(
    *,
    cwe_id: str,
    file: str,
    line: int,
    column: int,
    api_name: str,
) -> str:
    material = "|".join((cwe_id, file, str(line), str(column), api_name))
    return hashlib.sha256(material.encode("utf-8")).hexdigest()


def _dedupe_findings(findings: Sequence[CandidateFinding]) -> list[CandidateFinding]:
    deduped: list[CandidateFinding] = []
    seen_ids: set[str] = set()
    for finding in findings:
        if finding.id in seen_ids:
            continue
        deduped.append(finding)
        seen_ids.add(finding.id)
    return deduped


def _line_starts(text: str) -> tuple[int, ...]:
    starts = [0]
    for index, char in enumerate(text):
        if char == "\n":
            starts.append(index + 1)
    return tuple(starts)


def _line_and_column(line_starts: Sequence[int], index: int) -> tuple[int, int]:
    line_index = bisect_right(line_starts, index) - 1
    line_start = line_starts[max(line_index, 0)]
    return max(line_index + 1, 1), index - line_start + 1


def _line_text(text: str, line_number: int) -> str:
    lines = text.splitlines()
    if 1 <= line_number <= len(lines):
        return lines[line_number - 1]
    return ""


def _brace_pairs(text: str) -> dict[int, int]:
    stack: list[int] = []
    pairs: dict[int, int] = {}
    for index, char in enumerate(text):
        if char == "{":
            stack.append(index)
        elif char == "}" and stack:
            pairs[stack.pop()] = index
    return pairs


__all__ = ["extract_misconfiguration_findings"]
