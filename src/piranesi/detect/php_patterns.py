from __future__ import annotations

import hashlib
import re
from bisect import bisect_right
from collections.abc import Sequence
from dataclasses import dataclass
from pathlib import Path

from piranesi.detect.flows import severity_for_cwe
from piranesi.models import CandidateFinding, SourceLocation, TaintSink, TaintSource
from piranesi.scan.specs import SinkType, SourceType

_SOURCE_FILE_EXTENSIONS = frozenset({".php", ".yaml", ".yml"})
_DEFAULT_DATA_CATEGORIES = ["unknown"]
_PHP_FRAMEWORKS = frozenset({"php", "laravel", "symfony", "wordpress"})
_STATE_CHANGING_METHODS = frozenset({"post", "put", "patch", "delete"})
_LARAVEL_SENSITIVE_FIELDS = frozenset(
    {
        "is_admin",
        "isadmin",
        "admin",
        "role",
        "roles",
        "permission",
        "permissions",
        "password",
        "is_staff",
        "is_superuser",
    }
)

_PHP_SUPERGLOBAL = re.compile(r"\$_(?:GET|POST|REQUEST|COOKIE)\b", re.IGNORECASE)
_PHP_EXTRACT_CALL = re.compile(r"\bextract\s*\(\s*(?P<arg>[^)]+)\)", re.IGNORECASE)
_PHP_VARIABLE_VARIABLE = re.compile(r"\$\$(?:[A-Za-z_]\w*|\{[^}]+\})")
_PHP_LOOSE_ZERO_COMPARE = re.compile(
    r"(?:\$_(?:GET|POST|REQUEST|COOKIE)\s*\[[^\]]+\]\s*==\s*0|"
    r"0\s*==\s*\$_(?:GET|POST|REQUEST|COOKIE)\s*\[[^\]]+\])",
    re.IGNORECASE,
)
_PHP_MAGIC_HASH_COMPARE = re.compile(
    r"\b(?:md5|sha1)\s*\([^)]*\)\s*==\s*['\"]0e\d+['\"]",
    re.IGNORECASE,
)
_PHP_AUTH_FUNCTION = re.compile(
    r"(?is)function\s+(?P<name>[A-Za-z_]\w*(?:auth|login|verify|check|validate)\w*)\s*"
    r"\([^)]*\)\s*\{(?P<body>.*?)\}",
)
_PHP_UNSERIALIZE_CALL = re.compile(r"\bunserialize\s*\(\s*(?P<arg>[^)]+)\)", re.IGNORECASE)
_PHP_MAGIC_METHOD = re.compile(
    r"function\s+(?P<name>__wakeup|__destruct)\s*\([^)]*\)\s*\{", re.IGNORECASE
)
_PHP_DANGEROUS_CALL = re.compile(
    r"\b(?:exec|system|passthru|shell_exec|include|require|unlink|file_put_contents|"
    r"mysqli_query|curl_exec|eval)\s*\(",
    re.IGNORECASE,
)
_PHP_EVAL_CALL = re.compile(r"\beval\s*\(\s*(?P<arg>[^)]+)\)", re.IGNORECASE)

_LARAVEL_ROUTE_CALL = re.compile(
    r"Route::(?P<method>get|post|put|patch|delete)\s*\(\s*['\"](?P<path>[^'\"]+)['\"](?P<tail>.*?)\)\s*;",
    re.IGNORECASE | re.DOTALL,
)
_LARAVEL_ROUTE_AUTH = re.compile(
    r"(?:middleware\s*\(|['\"]auth['\"]|['\"]can:[^'\"]+['\"]|->middleware\s*\()",
    re.IGNORECASE,
)
_LARAVEL_SENSITIVE_ROUTE = re.compile(
    r"/(?:admin|internal|manage|settings|users|roles)\b", re.IGNORECASE
)
_LARAVEL_GUARDED_EMPTY = re.compile(r"\$guarded\s*=\s*\[\s*\]\s*;", re.IGNORECASE)
_LARAVEL_FILLABLE = re.compile(r"\$fillable\s*=\s*\[(?P<body>.*?)\]\s*;", re.IGNORECASE | re.DOTALL)
_BLADE_FORM = re.compile(r"(?is)<form\b(?P<attrs>[^>]*)>(?P<body>.*?)</form>")
_BLADE_STATE_METHOD = re.compile(r"method\s*=\s*['\"](?P<method>post|get)['\"]", re.IGNORECASE)
_BLADE_METHOD_OVERRIDE = re.compile(
    r"@method\s*\(\s*['\"](?P<method>put|patch|delete)['\"]\s*\)", re.IGNORECASE
)
_BLADE_CSRF = re.compile(r"@csrf|csrf_field\s*\(", re.IGNORECASE)
_BLADE_UNESCAPED = re.compile(r"\{!!\s*.+?\s*!!\}", re.DOTALL)

_SYMFONY_ANONYMOUS_ADMIN = re.compile(
    r"path\s*:\s*\^/admin.*roles\s*:\s*IS_AUTHENTICATED_ANONYMOUSLY",
    re.IGNORECASE,
)
_SYMFONY_AUTHENTICATOR_HINT = re.compile(
    r"\b(?:form_login|json_login|http_basic|custom_authenticator|guard|access_token)\b",
    re.IGNORECASE,
)
_SYMFONY_VOTER_CLASS = re.compile(r"class\s+\w+\s+extends\s+Voter\b", re.IGNORECASE)
_SYMFONY_VOTER_GRANT = re.compile(
    r"(?:return\s+true\s*;|return\s+VoterInterface::ACCESS_GRANTED\s*;)",
    re.IGNORECASE,
)

_WORDPRESS_OPEN_REST_ROUTE = re.compile(
    r"register_rest_route\s*\((?P<body>.*?)['\"]permission_callback['\"]\s*=>\s*['\"]__return_true['\"]",
    re.IGNORECASE | re.DOTALL,
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


def extract_php_pattern_findings(
    project_root: str | Path,
    *,
    frameworks: Sequence[str] | None = None,
    files: Sequence[Path] | None = None,
) -> tuple[CandidateFinding, ...]:
    root = Path(project_root).resolve(strict=False)
    normalized_frameworks = {framework.lower() for framework in frameworks or ()}
    scanned_files = tuple(_load_scanned_files(root, files=files, frameworks=normalized_frameworks))
    if not scanned_files:
        return ()

    if normalized_frameworks and not (normalized_frameworks & _PHP_FRAMEWORKS):
        return ()
    if not normalized_frameworks and not any(
        file.path.suffix == ".php" or file.path.name.startswith("security.")
        for file in scanned_files
    ):
        return ()

    findings: list[CandidateFinding] = []
    findings.extend(_detect_extract_calls(scanned_files))
    findings.extend(_detect_variable_variables(scanned_files))
    findings.extend(_detect_type_juggling(scanned_files))
    findings.extend(_detect_deserialization(scanned_files))
    findings.extend(_detect_eval_calls(scanned_files))
    findings.extend(_detect_blade_unescaped_output(scanned_files))
    findings.extend(_detect_laravel_patterns(scanned_files))
    findings.extend(_detect_symfony_patterns(scanned_files))
    findings.extend(_detect_wordpress_patterns(scanned_files))
    return tuple(_dedupe_findings(findings))


def _load_scanned_files(
    project_root: Path,
    *,
    files: Sequence[Path] | None,
    frameworks: set[str],
) -> list[_ScannedFile]:
    if files is None:
        candidate_paths = sorted(path for path in project_root.rglob("*") if path.is_file())
    else:
        candidate_paths = [Path(path).resolve(strict=False) for path in files]
        if "symfony" in frameworks:
            for extra in (
                project_root / "config" / "packages" / "security.yaml",
                project_root / "config" / "packages" / "security.yml",
            ):
                if extra.is_file():
                    candidate_paths.append(extra.resolve(strict=False))

    scanned_files: list[_ScannedFile] = []
    seen: set[Path] = set()
    for path in candidate_paths:
        if path in seen:
            continue
        seen.add(path)
        if not path.is_file():
            continue
        if path.suffix not in _SOURCE_FILE_EXTENSIONS:
            continue
        if "vendor" in path.parts:
            continue
        scanned_file = _ScannedFile.load(path)
        if scanned_file is not None:
            scanned_files.append(scanned_file)
    return scanned_files


def _detect_extract_calls(scanned_files: Sequence[_ScannedFile]) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for scanned_file in scanned_files:
        if scanned_file.path.suffix != ".php":
            continue
        for match in _PHP_EXTRACT_CALL.finditer(scanned_file.text):
            argument = match.group("arg")
            if not _PHP_SUPERGLOBAL.search(argument):
                continue
            findings.append(
                _build_static_finding(
                    cwe_id="CWE-915",
                    location=scanned_file.location_for_index(match.start()),
                    source_type=SourceType.REQUEST_BODY.value,
                    sink_type=SinkType.ORM_WRITE.value,
                    api_name="extract",
                    parameter_name=argument.strip(),
                    confidence=0.8,
                    metadata={"framework": "php"},
                )
            )
    return findings


def _detect_variable_variables(scanned_files: Sequence[_ScannedFile]) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for scanned_file in scanned_files:
        if scanned_file.path.suffix != ".php":
            continue
        for match in _PHP_VARIABLE_VARIABLE.finditer(scanned_file.text):
            block_start, block_end = scanned_file.containing_block(match.start())
            block_text = scanned_file.text[block_start:block_end]
            if not _PHP_SUPERGLOBAL.search(block_text):
                continue
            findings.append(
                _build_static_finding(
                    cwe_id="CWE-473",
                    location=scanned_file.location_for_index(match.start()),
                    source_type=SourceType.REQUEST_PARAM.value,
                    sink_type=SinkType.AUTH_SENSITIVE.value,
                    api_name="variable variable",
                    parameter_name="$$var",
                    confidence=0.65,
                    metadata={"framework": "php"},
                )
            )
    return findings


def _detect_type_juggling(scanned_files: Sequence[_ScannedFile]) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for scanned_file in scanned_files:
        if scanned_file.path.suffix != ".php":
            continue
        for match in _PHP_LOOSE_ZERO_COMPARE.finditer(scanned_file.text):
            findings.append(
                _build_static_finding(
                    cwe_id="CWE-1289",
                    location=scanned_file.location_for_index(match.start()),
                    source_type=SourceType.REQUEST_PARAM.value,
                    sink_type=SinkType.AUTH_SENSITIVE.value,
                    api_name="loose comparison",
                    parameter_name="== 0",
                    confidence=0.7,
                    metadata={"framework": "php"},
                )
            )
        for match in _PHP_MAGIC_HASH_COMPARE.finditer(scanned_file.text):
            findings.append(
                _build_static_finding(
                    cwe_id="CWE-1289",
                    location=scanned_file.location_for_index(match.start()),
                    source_type="security_configuration",
                    sink_type=SinkType.AUTH_SENSITIVE.value,
                    api_name="magic hash comparison",
                    parameter_name="0e*",
                    confidence=0.8,
                    metadata={"framework": "php"},
                )
            )
        for function_match in _PHP_AUTH_FUNCTION.finditer(scanned_file.text):
            body = function_match.group("body")
            comparator_match = re.search(r"==|!=", body)
            if comparator_match is None or not _PHP_SUPERGLOBAL.search(body):
                continue
            findings.append(
                _build_static_finding(
                    cwe_id="CWE-1289",
                    location=scanned_file.location_for_index(
                        function_match.start() + comparator_match.start()
                    ),
                    source_type=SourceType.REQUEST_PARAM.value,
                    sink_type=SinkType.AUTH_SENSITIVE.value,
                    api_name=function_match.group("name"),
                    parameter_name="loose auth comparison",
                    confidence=0.6,
                    metadata={"framework": "php"},
                )
            )
    return findings


def _detect_deserialization(scanned_files: Sequence[_ScannedFile]) -> list[CandidateFinding]:
    gadget_methods = _dangerous_magic_methods(scanned_files)
    findings: list[CandidateFinding] = []
    for scanned_file in scanned_files:
        if scanned_file.path.suffix != ".php":
            continue
        for match in _PHP_UNSERIALIZE_CALL.finditer(scanned_file.text):
            argument = match.group("arg")
            if not _PHP_SUPERGLOBAL.search(argument) and "input(" not in argument:
                continue
            findings.append(
                _build_static_finding(
                    cwe_id="CWE-502",
                    location=scanned_file.location_for_index(match.start()),
                    source_type=SourceType.COOKIE.value
                    if "COOKIE" in argument.upper()
                    else SourceType.REQUEST_BODY.value,
                    sink_type=SinkType.DESERIALIZATION.value,
                    api_name="unserialize",
                    parameter_name=argument.strip(),
                    confidence=0.85 if gadget_methods else 0.6,
                    metadata={
                        "framework": "php",
                        "gadget_chain": bool(gadget_methods),
                        "magic_methods": sorted(gadget_methods),
                    },
                )
            )
    return findings


def _dangerous_magic_methods(scanned_files: Sequence[_ScannedFile]) -> set[str]:
    methods: set[str] = set()
    for scanned_file in scanned_files:
        if scanned_file.path.suffix != ".php":
            continue
        for match in _PHP_MAGIC_METHOD.finditer(scanned_file.text):
            block_start, block_end = scanned_file.containing_block(match.start())
            block_text = scanned_file.text[block_start:block_end]
            if _PHP_DANGEROUS_CALL.search(block_text):
                methods.add(match.group("name"))
    return methods


def _detect_eval_calls(scanned_files: Sequence[_ScannedFile]) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for scanned_file in scanned_files:
        if scanned_file.path.suffix != ".php":
            continue
        for match in _PHP_EVAL_CALL.finditer(scanned_file.text):
            argument = match.group("arg")
            if not _PHP_SUPERGLOBAL.search(argument):
                continue
            findings.append(
                _build_static_finding(
                    cwe_id="CWE-94",
                    location=scanned_file.location_for_index(match.start()),
                    source_type=SourceType.REQUEST_BODY.value,
                    sink_type=SinkType.EVAL.value,
                    api_name="eval",
                    parameter_name=argument.strip(),
                    confidence=0.85,
                    metadata={"framework": "php"},
                )
            )
    return findings


def _detect_blade_unescaped_output(scanned_files: Sequence[_ScannedFile]) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for scanned_file in scanned_files:
        if ".blade.php" not in scanned_file.path.name:
            continue
        for match in _BLADE_UNESCAPED.finditer(scanned_file.text):
            findings.append(
                _build_static_finding(
                    cwe_id="CWE-79",
                    location=scanned_file.location_for_index(match.start()),
                    source_type="template_variable",
                    sink_type=SinkType.HTML_OUTPUT.value,
                    api_name="Blade raw echo",
                    parameter_name="{!! !!}",
                    confidence=0.75,
                    metadata={"framework": "laravel"},
                )
            )
    return findings


def _detect_laravel_patterns(scanned_files: Sequence[_ScannedFile]) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for scanned_file in scanned_files:
        if scanned_file.path.suffix != ".php":
            continue
        for match in _LARAVEL_GUARDED_EMPTY.finditer(scanned_file.text):
            findings.append(
                _build_static_finding(
                    cwe_id="CWE-915",
                    location=scanned_file.location_for_index(match.start()),
                    source_type=SourceType.REQUEST_BODY.value,
                    sink_type=SinkType.ORM_WRITE.value,
                    api_name="$guarded = []",
                    parameter_name="$guarded",
                    confidence=0.75,
                    metadata={"framework": "laravel"},
                )
            )
        for match in _LARAVEL_FILLABLE.finditer(scanned_file.text):
            body = match.group("body")
            sensitive_fields = {
                token.strip().strip("'\"").lower()
                for token in re.findall(r"['\"]([^'\"]+)['\"]", body)
                if token.strip().strip("'\"").lower() in _LARAVEL_SENSITIVE_FIELDS
            }
            if not sensitive_fields:
                continue
            findings.append(
                _build_static_finding(
                    cwe_id="CWE-915",
                    location=scanned_file.location_for_index(match.start()),
                    source_type=SourceType.REQUEST_BODY.value,
                    sink_type=SinkType.ORM_WRITE.value,
                    api_name="$fillable",
                    parameter_name=", ".join(sorted(sensitive_fields)),
                    confidence=0.65,
                    metadata={"framework": "laravel"},
                )
            )
        for match in _LARAVEL_ROUTE_CALL.finditer(scanned_file.text):
            method = match.group("method").lower()
            path = match.group("path")
            tail = match.group("tail")
            if not _LARAVEL_SENSITIVE_ROUTE.search(path):
                continue
            if _LARAVEL_ROUTE_AUTH.search(tail):
                continue
            findings.append(
                _build_static_finding(
                    cwe_id="CWE-306",
                    location=scanned_file.location_for_index(match.start()),
                    source_type=SourceType.REQUEST_PARAM.value,
                    sink_type=SinkType.AUTH_SENSITIVE.value,
                    api_name=f"Route::{method}",
                    parameter_name=path,
                    confidence=0.55,
                    metadata={"framework": "laravel"},
                )
            )
        for match in _BLADE_FORM.finditer(scanned_file.text):
            method = "get"
            method_match = _BLADE_STATE_METHOD.search(match.group("attrs"))
            if method_match is not None:
                method = method_match.group("method").lower()
            if method not in _STATE_CHANGING_METHODS and not _BLADE_METHOD_OVERRIDE.search(
                match.group("body")
            ):
                continue
            if _BLADE_CSRF.search(match.group("body")):
                continue
            findings.append(
                _build_static_finding(
                    cwe_id="CWE-352",
                    location=scanned_file.location_for_index(match.start()),
                    source_type=SourceType.REQUEST_BODY.value,
                    sink_type=SinkType.STATE_CHANGE_HANDLER.value,
                    api_name="Blade form",
                    parameter_name="missing @csrf",
                    confidence=0.65,
                    metadata={"framework": "laravel"},
                )
            )
    return findings


def _detect_symfony_patterns(scanned_files: Sequence[_ScannedFile]) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for scanned_file in scanned_files:
        if scanned_file.path.suffix in {".yaml", ".yml"} and scanned_file.path.name.startswith(
            "security."
        ):
            for match in _SYMFONY_ANONYMOUS_ADMIN.finditer(scanned_file.text):
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-306",
                        location=scanned_file.location_for_index(match.start()),
                        source_type="security_configuration",
                        sink_type=SinkType.AUTH_SENSITIVE.value,
                        api_name="security.yaml access_control",
                        parameter_name="IS_AUTHENTICATED_ANONYMOUSLY",
                        confidence=0.7,
                        metadata={"framework": "symfony"},
                    )
                )
            if "firewalls:" in scanned_file.text and not _SYMFONY_AUTHENTICATOR_HINT.search(
                scanned_file.text
            ):
                index = scanned_file.text.index("firewalls:")
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-306",
                        location=scanned_file.location_for_index(index),
                        source_type="security_configuration",
                        sink_type=SinkType.AUTH_SENSITIVE.value,
                        api_name="security.yaml firewall",
                        parameter_name="missing authenticator",
                        confidence=0.45,
                        metadata={"framework": "symfony"},
                    )
                )
            continue
        if scanned_file.path.suffix != ".php":
            continue
        if not _SYMFONY_VOTER_CLASS.search(scanned_file.text):
            continue
        for match in _SYMFONY_VOTER_GRANT.finditer(scanned_file.text):
            findings.append(
                _build_static_finding(
                    cwe_id="CWE-269",
                    location=scanned_file.location_for_index(match.start()),
                    source_type="security_configuration",
                    sink_type=SinkType.AUTH_SENSITIVE.value,
                    api_name="Symfony voter",
                    parameter_name="ACCESS_GRANTED",
                    confidence=0.65,
                    metadata={"framework": "symfony"},
                )
            )
    return findings


def _detect_wordpress_patterns(scanned_files: Sequence[_ScannedFile]) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for scanned_file in scanned_files:
        if scanned_file.path.suffix != ".php":
            continue
        for match in _WORDPRESS_OPEN_REST_ROUTE.finditer(scanned_file.text):
            findings.append(
                _build_static_finding(
                    cwe_id="CWE-306",
                    location=scanned_file.location_for_index(match.start()),
                    source_type=SourceType.REQUEST_BODY.value,
                    sink_type=SinkType.AUTH_SENSITIVE.value,
                    api_name="register_rest_route",
                    parameter_name="permission_callback=__return_true",
                    confidence=0.7,
                    metadata={"framework": "wordpress"},
                )
            )
    return findings


def _build_static_finding(
    *,
    cwe_id: str,
    location: SourceLocation,
    source_type: str,
    sink_type: str,
    api_name: str,
    parameter_name: str | None,
    confidence: float,
    severity: str | None = None,
    metadata: dict[str, object] | None = None,
) -> CandidateFinding:
    resolved_metadata = metadata or {}
    return CandidateFinding(
        id=_static_finding_id(
            cwe_id=cwe_id,
            file=location.file,
            line=location.line,
            column=location.column,
            api_name=api_name,
            parameter_name=parameter_name,
        ),
        vuln_class=cwe_id,
        source=TaintSource(
            location=location,
            source_type=source_type,
            data_categories=list(_DEFAULT_DATA_CATEGORIES),
            parameter_name=parameter_name,
        ),
        sink=TaintSink(
            location=location,
            sink_type=sink_type,
            api_name=api_name,
        ),
        taint_path=[],
        path_conditions=[],
        confidence=confidence,
        severity=severity or severity_for_cwe(cwe_id),
        metadata=resolved_metadata,
    )


def _static_finding_id(
    *,
    cwe_id: str,
    file: str,
    line: int,
    column: int,
    api_name: str,
    parameter_name: str | None,
) -> str:
    material = "|".join((cwe_id, file, str(line), str(column), api_name, parameter_name or ""))
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


__all__ = ["extract_php_pattern_findings"]
