from __future__ import annotations

import re
from collections.abc import Iterable, Sequence
from dataclasses import dataclass, field
from pathlib import Path

from piranesi.models import CandidateFinding, DepReachabilityResult

_JS_SOURCE_EXTENSIONS = frozenset({".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs"})
_PYTHON_SOURCE_EXTENSIONS = frozenset({".py"})
_SOURCE_EXTENSIONS = _JS_SOURCE_EXTENSIONS | _PYTHON_SOURCE_EXTENSIONS
_IGNORED_PATH_SEGMENTS = frozenset(
    {
        ".git",
        ".next",
        ".venv",
        "__pycache__",
        "build",
        "coverage",
        "dist",
        "node_modules",
        "site-packages",
        "vendor",
        "venv",
    }
)

_JS_IMPORT_FROM_RE = re.compile(
    r"(?m)^\s*import\s+(?P<clause>[^;\n]+?)\s+from\s+['\"](?P<module>[^'\"]+)['\"]\s*;?"
)
_JS_SIDE_EFFECT_IMPORT_RE = re.compile(r"(?m)^\s*import\s+['\"](?P<module>[^'\"]+)['\"]\s*;?")
_JS_REQUIRE_DESTRUCTURED_RE = re.compile(
    r"(?m)^\s*(?:const|let|var)\s+\{(?P<names>[^}]+)\}\s*=\s*require\(\s*['\"](?P<module>[^'\"]+)['\"]\s*\)\s*;?"
)
_JS_REQUIRE_MEMBER_ASSIGN_RE = re.compile(
    r"(?m)^\s*(?:const|let|var)\s+(?P<local>[A-Za-z_$][\w$]*)\s*=\s*require\(\s*['\"](?P<module>[^'\"]+)['\"]\s*\)\.(?P<member>[A-Za-z_$][\w$]*)\s*;?"
)
_JS_REQUIRE_ASSIGN_RE = re.compile(
    r"(?m)^\s*(?:const|let|var)\s+(?P<local>[A-Za-z_$][\w$]*)\s*=\s*require\(\s*['\"](?P<module>[^'\"]+)['\"]\s*\)\s*;?"
)
_JS_REQUIRE_BARE_RE = re.compile(r"(?m)^\s*require\(\s*['\"](?P<module>[^'\"]+)['\"]\s*\)\s*;?")
_JS_REQUIRE_MEMBER_CALL_RE = re.compile(
    r"require\(\s*['\"](?P<module>[^'\"]+)['\"]\s*\)\.(?P<member>[A-Za-z_$][\w$]*)\s*\("
)

_PY_FROM_IMPORT_RE = re.compile(
    r"(?m)^\s*from\s+(?P<module>[A-Za-z_][\w.]*|\.[\w.]*)\s+import\s+(?P<names>[^\n]+)"
)
_PY_IMPORT_RE = re.compile(r"(?m)^\s*import\s+(?P<modules>[^\n]+)")

_JS_BLOCK_COMMENT_RE = re.compile(r"/\*.*?\*/", re.DOTALL)
_JS_LINE_COMMENT_RE = re.compile(r"(?m)//.*$")
_PY_LINE_COMMENT_RE = re.compile(r"(?m)#.*$")

_BACKTICK_TOKEN_RE = re.compile(r"`(?P<token>[A-Za-z_$][\w$./-]*(?:\(\))?)`")
_FUNCTION_TOKEN_RE = re.compile(r"\b(?P<token>[A-Za-z_$][\w$./-]*)\s*\(\)")
_DOTTED_TOKEN_RE = re.compile(r"\b(?P<token>[A-Za-z_$][\w$]*(?:[./][A-Za-z_$][\w$-]*)+)\b")
_MODULE_CONTEXT_RE = re.compile(
    r"\b(?:module|function|method|api|operator|attribute)\s+['\"`]?([A-Za-z_$][\w$./-]*)['\"`]?",
    re.IGNORECASE,
)


@dataclass(frozen=True, slots=True)
class _ImportedBinding:
    file_path: Path
    package_name: str
    module_specifier: str
    local_name: str | None = None
    imported_name: str | None = None
    whole_package: bool = False


@dataclass(slots=True)
class _DependencyIndex:
    bindings_by_package: dict[str, list[_ImportedBinding]] = field(default_factory=dict)
    usage_text_by_file: dict[Path, str] = field(default_factory=dict)
    direct_member_calls_by_package: dict[str, set[str]] = field(default_factory=dict)
    import_graph: dict[str, set[str]] = field(default_factory=dict)

    def add_binding(self, binding: _ImportedBinding) -> None:
        self.bindings_by_package.setdefault(binding.package_name, []).append(binding)
        self.import_graph.setdefault(str(binding.file_path), set()).add(
            f"dep:{binding.package_name}"
        )

    def add_direct_member_call(self, package_name: str, member_name: str) -> None:
        self.direct_member_calls_by_package.setdefault(package_name, set()).add(member_name)

    def add_import_edge(self, source: Path, target: str) -> None:
        self.import_graph.setdefault(str(source), set()).add(target)

    @property
    def import_graph_edges(self) -> int:
        return sum(len(targets) for targets in self.import_graph.values())


def apply_dependency_reachability(
    project_root: Path,
    findings: Sequence[CandidateFinding],
) -> tuple[CandidateFinding, ...]:
    annotated, _ = _annotate_dependency_findings(project_root, findings)
    return annotated


def analyze_dependency_reachability(
    project_root: Path,
    findings: Sequence[CandidateFinding],
) -> DepReachabilityResult:
    _, result = _annotate_dependency_findings(project_root, findings)
    return result


def _annotate_dependency_findings(
    project_root: Path,
    findings: Sequence[CandidateFinding],
) -> tuple[tuple[CandidateFinding, ...], DepReachabilityResult]:
    if not findings:
        return (), DepReachabilityResult()

    dependency_findings = [
        finding
        for finding in findings
        if finding.sink.sink_type == "dependency_vulnerability"
        and isinstance(finding.metadata.get("package"), str)
    ]
    if not dependency_findings:
        return tuple(findings), DepReachabilityResult()

    index = _build_dependency_index(project_root)
    annotated: list[CandidateFinding] = []
    reachable_deps: set[str] = set()
    unreachable_deps: set[str] = set()

    for finding in findings:
        reachability = _finding_reachability(finding, index)
        if reachability is None:
            annotated.append(finding)
            continue

        finding_keys = _finding_dependency_keys(finding)
        if reachability == "dep_unreachable":
            annotated.append(finding.model_copy(update={"reachability": "dep_unreachable"}))
            unreachable_deps.update(finding_keys)
            continue

        annotated.append(finding)
        reachable_deps.update(finding_keys)

    return (
        tuple(annotated),
        DepReachabilityResult(
            reachable_deps=reachable_deps,
            unreachable_deps=unreachable_deps,
            import_graph_edges=index.import_graph_edges,
        ),
    )


def _finding_reachability(
    finding: CandidateFinding,
    index: _DependencyIndex,
) -> str | None:
    if finding.sink.sink_type != "dependency_vulnerability":
        return None

    raw_package = finding.metadata.get("package")
    if not isinstance(raw_package, str):
        return None

    package_name = _normalize_package_name(raw_package)
    targets = _extract_vulnerable_targets(finding, package_name=package_name)
    if not targets:
        return "reachable"

    bindings = index.bindings_by_package.get(package_name, [])
    if any(binding.whole_package for binding in bindings):
        return "reachable"

    direct_member_calls = index.direct_member_calls_by_package.get(package_name, set())
    for target in targets:
        if target in direct_member_calls:
            return "reachable"

        matching_bindings = [
            binding for binding in bindings if _binding_matches_target(binding, target)
        ]
        if not matching_bindings:
            continue
        if any(_binding_is_used(binding, index) for binding in matching_bindings):
            return "reachable"

    return "dep_unreachable"


def _binding_matches_target(binding: _ImportedBinding, target: str) -> bool:
    if _normalize_symbol_name(binding.imported_name) == target:
        return True
    return _module_suffix(binding.module_specifier) == target


def _binding_is_used(binding: _ImportedBinding, index: _DependencyIndex) -> bool:
    if not binding.local_name:
        return False
    usage_text = index.usage_text_by_file.get(binding.file_path)
    if not usage_text:
        return False

    escaped_name = re.escape(binding.local_name)
    if re.search(rf"(?<![\w$.]){escaped_name}\s*\(", usage_text):
        return True
    if re.search(rf"(?<![\w$.]){escaped_name}\s*(?:\.\s*[A-Za-z_$][\w$]*)+\s*\(", usage_text):
        return True
    return re.search(rf"(?<![\w$.]){escaped_name}\b", usage_text) is not None


def _finding_dependency_keys(finding: CandidateFinding) -> set[str]:
    raw_package = finding.metadata.get("package")
    if not isinstance(raw_package, str):
        return set()
    package_name = _normalize_package_name(raw_package)
    targets = _extract_vulnerable_targets(finding, package_name=package_name)
    if not targets:
        return {f"{package_name}:*"}
    return {f"{package_name}:{target}" for target in targets}


def _extract_vulnerable_targets(
    finding: CandidateFinding,
    *,
    package_name: str,
) -> tuple[str, ...]:
    raw_title = finding.metadata.get("title")
    if not isinstance(raw_title, str) or not raw_title.strip():
        return ()

    candidates: list[str] = []
    candidates.extend(match.group("token") for match in _BACKTICK_TOKEN_RE.finditer(raw_title))
    candidates.extend(match.group("token") for match in _FUNCTION_TOKEN_RE.finditer(raw_title))
    candidates.extend(match.group("token") for match in _DOTTED_TOKEN_RE.finditer(raw_title))
    candidates.extend(match.group(1) for match in _MODULE_CONTEXT_RE.finditer(raw_title))

    normalized_targets: list[str] = []
    seen: set[str] = set()
    for candidate in candidates:
        normalized = _normalize_target_candidate(candidate, package_name=package_name)
        if normalized is None or normalized in seen:
            continue
        normalized_targets.append(normalized)
        seen.add(normalized)
    return tuple(normalized_targets)


def _normalize_target_candidate(candidate: str, *, package_name: str) -> str | None:
    normalized = candidate.strip().strip("`'\"()[]{}")
    normalized = normalized.replace("::", ".").strip(".")
    normalized = re.sub(r"\(\)$", "", normalized)
    if not normalized:
        return None

    lowered = normalized.lower()
    if lowered.startswith("cve-") or lowered.startswith("ghsa-"):
        return None
    if lowered in {"prototype", "pollution", "redos", "vulnerability"}:
        return None

    normalized_package = _normalize_package_name(package_name)
    package_dot_prefix = f"{normalized_package}."
    package_path_prefix = f"{normalized_package}/"
    if lowered.startswith(package_dot_prefix):
        normalized = normalized[len(package_dot_prefix) :]
    elif lowered.startswith(package_path_prefix):
        normalized = normalized[len(package_path_prefix) :]
    elif lowered.startswith("_."):
        normalized = normalized[2:]

    meaningful_parts = [
        part
        for part in re.split(r"[./]", normalized)
        if part and _normalize_symbol_name(part) != normalized_package
    ]
    if not meaningful_parts:
        return None

    target = meaningful_parts[-1]
    if not re.fullmatch(r"[A-Za-z_$][\w$-]*", target):
        return None
    return _normalize_symbol_name(target)


def _build_dependency_index(project_root: Path) -> _DependencyIndex:
    index = _DependencyIndex()
    root = project_root.resolve(strict=False)

    for path in sorted(root.rglob("*")):
        if not path.is_file() or path.suffix.lower() not in _SOURCE_EXTENSIONS:
            continue
        if any(part in _IGNORED_PATH_SEGMENTS for part in path.parts):
            continue
        try:
            text = path.read_text(encoding="utf-8")
        except OSError:
            continue
        resolved_path = path.resolve(strict=False)
        if path.suffix.lower() in _JS_SOURCE_EXTENSIONS:
            _analyze_javascript_file(resolved_path, text, root, index)
        else:
            _analyze_python_file(resolved_path, text, root, index)

    return index


def _analyze_javascript_file(
    file_path: Path,
    text: str,
    project_root: Path,
    index: _DependencyIndex,
) -> None:
    spans: list[tuple[int, int]] = []

    for match in _JS_IMPORT_FROM_RE.finditer(text):
        module_specifier = match.group("module")
        spans.append(match.span())
        if _is_local_module(module_specifier):
            target = _resolve_js_local_module(file_path, module_specifier, project_root)
            if target is not None:
                index.add_import_edge(file_path, str(target))
            continue
        for binding in _parse_js_import_clause(file_path, module_specifier, match.group("clause")):
            index.add_binding(binding)

    for match in _JS_SIDE_EFFECT_IMPORT_RE.finditer(text):
        module_specifier = match.group("module")
        spans.append(match.span())
        if _is_local_module(module_specifier):
            target = _resolve_js_local_module(file_path, module_specifier, project_root)
            if target is not None:
                index.add_import_edge(file_path, str(target))
            continue
        package_name, subpath = _split_js_package(module_specifier)
        index.add_binding(
            _ImportedBinding(
                file_path=file_path,
                package_name=package_name,
                module_specifier=module_specifier,
                imported_name=_module_suffix(subpath),
                whole_package=not bool(subpath),
            )
        )

    for match in _JS_REQUIRE_DESTRUCTURED_RE.finditer(text):
        module_specifier = match.group("module")
        spans.append(match.span())
        if _is_local_module(module_specifier):
            target = _resolve_js_local_module(file_path, module_specifier, project_root)
            if target is not None:
                index.add_import_edge(file_path, str(target))
            continue
        package_name, _ = _split_js_package(module_specifier)
        for imported_name, local_name in _parse_js_named_specifiers(
            match.group("names"),
            alias_separator=":",
        ):
            index.add_binding(
                _ImportedBinding(
                    file_path=file_path,
                    package_name=package_name,
                    module_specifier=module_specifier,
                    local_name=local_name,
                    imported_name=imported_name,
                )
            )

    for match in _JS_REQUIRE_MEMBER_ASSIGN_RE.finditer(text):
        module_specifier = match.group("module")
        spans.append(match.span())
        if _is_local_module(module_specifier):
            target = _resolve_js_local_module(file_path, module_specifier, project_root)
            if target is not None:
                index.add_import_edge(file_path, str(target))
            continue
        package_name, _ = _split_js_package(module_specifier)
        index.add_binding(
            _ImportedBinding(
                file_path=file_path,
                package_name=package_name,
                module_specifier=module_specifier,
                local_name=match.group("local"),
                imported_name=match.group("member"),
            )
        )

    for match in _JS_REQUIRE_ASSIGN_RE.finditer(text):
        module_specifier = match.group("module")
        spans.append(match.span())
        if _is_local_module(module_specifier):
            target = _resolve_js_local_module(file_path, module_specifier, project_root)
            if target is not None:
                index.add_import_edge(file_path, str(target))
            continue
        package_name, subpath = _split_js_package(module_specifier)
        index.add_binding(
            _ImportedBinding(
                file_path=file_path,
                package_name=package_name,
                module_specifier=module_specifier,
                local_name=match.group("local"),
                imported_name=_module_suffix(subpath),
                whole_package=not bool(subpath),
            )
        )

    for match in _JS_REQUIRE_BARE_RE.finditer(text):
        module_specifier = match.group("module")
        spans.append(match.span())
        if _is_local_module(module_specifier):
            target = _resolve_js_local_module(file_path, module_specifier, project_root)
            if target is not None:
                index.add_import_edge(file_path, str(target))
            continue
        package_name, subpath = _split_js_package(module_specifier)
        index.add_binding(
            _ImportedBinding(
                file_path=file_path,
                package_name=package_name,
                module_specifier=module_specifier,
                imported_name=_module_suffix(subpath),
                whole_package=not bool(subpath),
            )
        )

    for match in _JS_REQUIRE_MEMBER_CALL_RE.finditer(text):
        module_specifier = match.group("module")
        if _is_local_module(module_specifier):
            continue
        package_name, _ = _split_js_package(module_specifier)
        index.add_direct_member_call(package_name, _normalize_symbol_name(match.group("member")))

    index.usage_text_by_file[file_path] = _strip_js_comments(_blank_spans(text, spans))


def _analyze_python_file(
    file_path: Path,
    text: str,
    project_root: Path,
    index: _DependencyIndex,
) -> None:
    spans: list[tuple[int, int]] = []

    for match in _PY_FROM_IMPORT_RE.finditer(text):
        module_specifier = match.group("module")
        spans.append(match.span())
        if _is_local_python_module(module_specifier, file_path, project_root):
            target = _resolve_python_local_module(file_path, module_specifier, project_root)
            if target is not None:
                index.add_import_edge(file_path, str(target))
            continue

        package_name, _ = _split_python_module(module_specifier)
        names = match.group("names").strip()
        if names == "*":
            index.add_binding(
                _ImportedBinding(
                    file_path=file_path,
                    package_name=package_name,
                    module_specifier=module_specifier,
                    whole_package=True,
                )
            )
            continue

        for imported_name, local_name in _parse_python_import_names(names):
            index.add_binding(
                _ImportedBinding(
                    file_path=file_path,
                    package_name=package_name,
                    module_specifier=module_specifier,
                    local_name=local_name,
                    imported_name=imported_name,
                )
            )

    for match in _PY_IMPORT_RE.finditer(text):
        spans.append(match.span())
        for module_specifier, alias in _parse_python_module_list(match.group("modules")):
            if _is_local_python_module(module_specifier, file_path, project_root):
                target = _resolve_python_local_module(file_path, module_specifier, project_root)
                if target is not None:
                    index.add_import_edge(file_path, str(target))
                continue
            package_name, _ = _split_python_module(module_specifier)
            local_name = alias or module_specifier.split(".", 1)[0]
            index.add_binding(
                _ImportedBinding(
                    file_path=file_path,
                    package_name=package_name,
                    module_specifier=module_specifier,
                    local_name=local_name,
                    whole_package=True,
                )
            )

    index.usage_text_by_file[file_path] = _strip_python_comments(_blank_spans(text, spans))


def _parse_js_import_clause(
    file_path: Path,
    module_specifier: str,
    clause: str,
) -> list[_ImportedBinding]:
    package_name, subpath = _split_js_package(module_specifier)
    normalized_clause = clause.strip()
    bindings: list[_ImportedBinding] = []

    if normalized_clause.startswith("{") and normalized_clause.endswith("}"):
        for imported_name, local_name in _parse_js_named_specifiers(
            normalized_clause[1:-1],
            alias_separator="as",
        ):
            bindings.append(
                _ImportedBinding(
                    file_path=file_path,
                    package_name=package_name,
                    module_specifier=module_specifier,
                    local_name=local_name,
                    imported_name=imported_name,
                )
            )
        return bindings

    if normalized_clause.startswith("*"):
        alias_match = re.match(r"\*\s+as\s+([A-Za-z_$][\w$]*)", normalized_clause)
        if alias_match is None:
            return bindings
        bindings.append(
            _ImportedBinding(
                file_path=file_path,
                package_name=package_name,
                module_specifier=module_specifier,
                local_name=alias_match.group(1),
                imported_name=_module_suffix(subpath),
                whole_package=not bool(subpath),
            )
        )
        return bindings

    if "," in normalized_clause:
        default_part, remainder = normalized_clause.split(",", 1)
        bindings.extend(
            _parse_js_default_binding(
                file_path, package_name, module_specifier, subpath, default_part
            )
        )
        remainder = remainder.strip()
        if remainder.startswith("{") and remainder.endswith("}"):
            for imported_name, local_name in _parse_js_named_specifiers(
                remainder[1:-1],
                alias_separator="as",
            ):
                bindings.append(
                    _ImportedBinding(
                        file_path=file_path,
                        package_name=package_name,
                        module_specifier=module_specifier,
                        local_name=local_name,
                        imported_name=imported_name,
                    )
                )
        return bindings

    bindings.extend(
        _parse_js_default_binding(
            file_path,
            package_name,
            module_specifier,
            subpath,
            normalized_clause,
        )
    )
    return bindings


def _parse_js_default_binding(
    file_path: Path,
    package_name: str,
    module_specifier: str,
    subpath: str,
    raw_local_name: str,
) -> list[_ImportedBinding]:
    local_name = raw_local_name.strip()
    if not re.fullmatch(r"[A-Za-z_$][\w$]*", local_name):
        return []
    return [
        _ImportedBinding(
            file_path=file_path,
            package_name=package_name,
            module_specifier=module_specifier,
            local_name=local_name,
            imported_name=_module_suffix(subpath),
            whole_package=not bool(subpath),
        )
    ]


def _parse_js_named_specifiers(
    raw_names: str,
    *,
    alias_separator: str,
) -> Iterable[tuple[str, str]]:
    for raw_part in raw_names.split(","):
        part = raw_part.strip()
        if not part:
            continue
        if alias_separator == "as" and " as " in part:
            imported_name, local_name = [item.strip() for item in part.split(" as ", 1)]
        elif alias_separator == ":" and ":" in part:
            imported_name, local_name = [item.strip() for item in part.split(":", 1)]
        else:
            imported_name = part
            local_name = part
        if imported_name and local_name:
            yield imported_name, local_name


def _parse_python_import_names(raw_names: str) -> Iterable[tuple[str, str]]:
    for raw_part in raw_names.split(","):
        part = raw_part.strip()
        if not part:
            continue
        if " as " in part:
            imported_name, local_name = [item.strip() for item in part.split(" as ", 1)]
        else:
            imported_name = part
            local_name = part
        if imported_name and local_name:
            yield imported_name, local_name


def _parse_python_module_list(raw_modules: str) -> Iterable[tuple[str, str | None]]:
    for raw_part in raw_modules.split(","):
        part = raw_part.strip()
        if not part:
            continue
        if " as " in part:
            module_name, alias = [item.strip() for item in part.split(" as ", 1)]
        else:
            module_name = part
            alias = None
        if module_name:
            yield module_name, alias


def _split_js_package(module_specifier: str) -> tuple[str, str]:
    normalized = module_specifier.strip()
    if normalized.startswith("@"):
        parts = normalized.split("/")
        if len(parts) < 2:
            return _normalize_package_name(normalized), ""
        package_name = "/".join(parts[:2])
        subpath = "/".join(parts[2:])
        return _normalize_package_name(package_name), subpath

    parts = normalized.split("/", 1)
    package_name = _normalize_package_name(parts[0])
    subpath = parts[1] if len(parts) == 2 else ""
    return package_name, subpath


def _split_python_module(module_specifier: str) -> tuple[str, str]:
    normalized = module_specifier.lstrip(".")
    if not normalized:
        return "", ""
    parts = normalized.split(".", 1)
    package_name = _normalize_package_name(parts[0])
    subpath = parts[1] if len(parts) == 2 else ""
    return package_name, subpath


def _module_suffix(module_specifier: str | None) -> str:
    if not module_specifier:
        return ""
    normalized = module_specifier.strip().strip("./")
    if not normalized:
        return ""
    parts = re.split(r"[./]", normalized)
    return _normalize_symbol_name(parts[-1]) if parts else ""


def _normalize_package_name(package_name: str) -> str:
    return package_name.strip().lower().replace("-", "_")


def _normalize_symbol_name(symbol_name: str | None) -> str:
    if not symbol_name:
        return ""
    return symbol_name.strip().lower().replace("-", "_")


def _is_local_module(module_specifier: str) -> bool:
    return module_specifier.startswith(".") or module_specifier.startswith("/")


def _resolve_js_local_module(
    file_path: Path,
    module_specifier: str,
    project_root: Path,
) -> Path | None:
    base = (
        project_root / module_specifier.lstrip("/")
        if module_specifier.startswith("/")
        else file_path.parent / module_specifier
    )
    candidates = [base]
    candidates.extend(base.with_suffix(suffix) for suffix in _JS_SOURCE_EXTENSIONS)
    candidates.extend(base / f"index{suffix}" for suffix in _JS_SOURCE_EXTENSIONS)
    for candidate in candidates:
        resolved = candidate.resolve(strict=False)
        if resolved.exists() and resolved.is_file():
            return resolved
    return None


def _is_local_python_module(
    module_specifier: str,
    file_path: Path,
    project_root: Path,
) -> bool:
    if module_specifier.startswith("."):
        return True
    return _resolve_python_absolute_module(module_specifier, project_root) is not None


def _resolve_python_local_module(
    file_path: Path,
    module_specifier: str,
    project_root: Path,
) -> Path | None:
    if module_specifier.startswith("."):
        return _resolve_python_relative_module(file_path, module_specifier)
    return _resolve_python_absolute_module(module_specifier, project_root)


def _resolve_python_relative_module(file_path: Path, module_specifier: str) -> Path | None:
    level = len(module_specifier) - len(module_specifier.lstrip("."))
    remainder = module_specifier[level:]
    base_dir = file_path.parent
    for _ in range(max(level - 1, 0)):
        base_dir = base_dir.parent
    if remainder:
        base_dir = base_dir.joinpath(*remainder.split("."))
    return _resolve_python_module_path(base_dir)


def _resolve_python_absolute_module(module_specifier: str, project_root: Path) -> Path | None:
    base = project_root.joinpath(*module_specifier.split("."))
    return _resolve_python_module_path(base)


def _resolve_python_module_path(base: Path) -> Path | None:
    for candidate in (base.with_suffix(".py"), base / "__init__.py"):
        resolved = candidate.resolve(strict=False)
        if resolved.exists() and resolved.is_file():
            return resolved
    return None


def _blank_spans(text: str, spans: Sequence[tuple[int, int]]) -> str:
    if not spans:
        return text

    buffer = list(text)
    for start, end in spans:
        for index in range(start, min(end, len(buffer))):
            if buffer[index] != "\n":
                buffer[index] = " "
    return "".join(buffer)


def _strip_js_comments(text: str) -> str:
    without_blocks = _JS_BLOCK_COMMENT_RE.sub(
        lambda match: _preserve_newlines(match.group(0)), text
    )
    return _JS_LINE_COMMENT_RE.sub("", without_blocks)


def _strip_python_comments(text: str) -> str:
    return _PY_LINE_COMMENT_RE.sub("", text)


def _preserve_newlines(fragment: str) -> str:
    return "".join("\n" if char == "\n" else " " for char in fragment)


__all__ = [
    "DepReachabilityResult",
    "analyze_dependency_reachability",
    "apply_dependency_reachability",
]
