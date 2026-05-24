from __future__ import annotations

import json
import re
import tomllib
from collections import defaultdict, deque
from collections.abc import Mapping, Sequence
from pathlib import Path

from piranesi.models import CandidateFinding, ReachabilityResult, ScannedFunction, ScanResult

_TEST_FILE_PATTERN = re.compile(
    r"(^|/)(tests?/|__tests__/|test_.*|.*_test\.(?:go|py)$|.*\.(?:test|spec)\.[^.]+$)",
    re.IGNORECASE,
)
_TEST_FUNCTION_PATTERN = re.compile(r"^(test[_A-Z].*|Test[A-Z].*)$")
_EXPORT_ENTRY_FILENAMES = frozenset(
    {"index.ts", "index.tsx", "index.js", "index.jsx", "__init__.py"}
)
_JS_EXPORT_PATTERNS = (
    r"\bexport\s+(?:async\s+)?function\s+{name}\b",
    r"\bexport\s+(?:const|let|var)\s+{name}\b",
    r"\bexport\s*\{{[^}}]*\b{name}\b[^}}]*\}}",
    r"\bexports\.{name}\s*=",
    r"\bmodule\.exports\.{name}\s*=",
    r"\bmodule\.exports\s*=\s*\{{[^}}]*\b{name}\b[^}}]*\}}",
)
_PYTHON_ROUTE_DECORATOR_PATTERN = re.compile(
    r"@\s*(?:app|bp|blueprint|router|api)\.(?:route|get|post|put|delete|patch|options|head|websocket)\s*\(",
    re.IGNORECASE,
)
_JAVA_ROUTE_DECORATOR_PATTERN = re.compile(
    r"@\s*(?:GetMapping|PostMapping|PutMapping|DeleteMapping|PatchMapping|RequestMapping)\b"
)
_NODE_ROUTE_TEMPLATE = (
    r"\b(?:app|router|server)\.(?:get|post|put|delete|patch|use|all)\s*\([^,\n]+,\s*{name}\b"
)
_GO_ROUTE_TEMPLATE = (
    r"\b(?:r|router|e)\.(?:GET|POST|PUT|DELETE|PATCH|Use|Get|Post|Put|Delete|Patch)\s*"
    r"\([^,\n]+,\s*{name}\b"
)
_DJANGO_ROUTE_TEMPLATE = r"\b(?:path|re_path)\s*\([^,\n]+,\s*{name}\b"
_EXCLUDED_ENTRY_PARTS = frozenset(
    {
        "node_modules",
        "dist",
        "build",
        ".next",
        ".venv",
        "venv",
        "__pycache__",
        "vendor",
        # piranesi output dirs
        "piranesi-output",
        ".piranesi-cache",
        ".piranesi-out",
    }
)
_PIRANESI_TRACE_PREFIX = ".piranesi-trace"
_NON_USER_CODE_NAMES = frozenset({":program", "<global>", "<init>", "<clinit>"})


class _SourceFileIndex:
    def __init__(self) -> None:
        self._text_cache: dict[Path, str | None] = {}
        self._lines_cache: dict[Path, tuple[str, ...]] = {}

    def text_for(self, path_str: str) -> str | None:
        path = Path(path_str).resolve(strict=False)
        if path not in self._text_cache:
            try:
                self._text_cache[path] = path.read_text(encoding="utf-8")
            except OSError:
                self._text_cache[path] = None
        return self._text_cache[path]

    def lines_for(self, path_str: str) -> tuple[str, ...]:
        path = Path(path_str).resolve(strict=False)
        if path not in self._lines_cache:
            text = self.text_for(path_str)
            self._lines_cache[path] = () if text is None else tuple(text.splitlines())
        return self._lines_cache[path]


def analyze_reachability(
    scan_result: ScanResult,
    findings: Sequence[CandidateFinding],
    *,
    project_root: Path,
    include_tests: bool = False,
) -> tuple[list[CandidateFinding], ReachabilityResult]:
    call_graph = build_call_graph(scan_result.call_graph)
    indexed_functions = _index_functions(scan_result.functions)
    entry_points = identify_entry_points(
        scan_result,
        project_root=project_root,
        include_tests=include_tests,
    )
    all_function_ids = set(indexed_functions.function_ids)
    for caller, callees in call_graph.items():
        all_function_ids.add(caller)
        all_function_ids.update(callees)

    if not entry_points:
        reachable_functions = set(all_function_ids)
        unreachable_functions: set[str] = set()
    else:
        reachable_functions = compute_reachable(entry_points, call_graph)
        unreachable_functions = {
            function_id
            for function_id in indexed_functions.function_ids
            if function_id not in reachable_functions
        }

    annotated_findings: list[CandidateFinding] = []
    for finding in findings:
        annotated_findings.append(
            _annotate_finding(
                finding,
                reachable_functions=reachable_functions,
                entry_points=entry_points,
                indexed_functions=indexed_functions,
            )
        )

    dead_code_functions = (
        _dead_code_functions(
            indexed_functions.functions,
            unreachable_functions=unreachable_functions,
            include_tests=include_tests,
        )
        if entry_points
        else []
    )
    return annotated_findings, ReachabilityResult(
        reachable_functions=reachable_functions,
        unreachable_functions=unreachable_functions,
        entry_points=entry_points,
        call_graph_edges=sum(len(callees) for callees in call_graph.values()),
        dead_code_functions=dead_code_functions,
    )


def build_call_graph(call_graph: Mapping[str, Sequence[str]]) -> dict[str, set[str]]:
    adjacency: dict[str, set[str]] = {}
    for caller, callees in call_graph.items():
        if not caller:
            continue
        adjacency.setdefault(caller, set()).update(
            callee for callee in callees if isinstance(callee, str) and callee
        )
    return adjacency


def identify_entry_points(
    scan_result: ScanResult,
    *,
    project_root: Path,
    include_tests: bool = False,
) -> set[str]:
    entry_points = {
        entry_point.function_id
        for entry_point in scan_result.entry_points
        if entry_point.function_id
    }
    indexed_functions = _index_functions(scan_result.functions)
    file_index = _SourceFileIndex()

    for function in indexed_functions.functions:
        if _is_explicit_main(function):
            entry_points.add(function.function_id)
        if include_tests and _is_test_entry_point(function):
            entry_points.add(function.function_id)
        if _is_exported_entry_point(function, file_index=file_index):
            entry_points.add(function.function_id)
        if _has_framework_route_reference(function, file_index=file_index):
            entry_points.add(function.function_id)

    entry_points.update(
        _cli_entry_points(
            indexed_functions=indexed_functions,
            project_root=project_root,
        )
    )
    return entry_points


def compute_reachable(
    entry_points: set[str],
    call_graph: Mapping[str, set[str]],
) -> set[str]:
    visited: set[str] = set()
    queue = deque(entry_points)
    while queue:
        function_id = queue.popleft()
        if function_id in visited:
            continue
        visited.add(function_id)
        for callee in call_graph.get(function_id, ()):
            if callee not in visited:
                queue.append(callee)
    return visited


class _FunctionIndex:
    def __init__(self, functions: Sequence[ScannedFunction]) -> None:
        deduped: dict[str, ScannedFunction] = {}
        for function in sorted(functions, key=_function_sort_key):
            deduped.setdefault(function.function_id, function)
        self.functions = list(deduped.values())
        self.function_ids = frozenset(deduped)
        self.by_id = deduped
        self.by_file: dict[Path, list[ScannedFunction]] = defaultdict(list)
        for function in self.functions:
            self.by_file[_normalize_path(function.location.file)].append(function)
        for file_functions in self.by_file.values():
            file_functions.sort(
                key=lambda function: (
                    function.location.line,
                    function.location.column,
                    function.name,
                )
            )


def _index_functions(functions: Sequence[ScannedFunction]) -> _FunctionIndex:
    return _FunctionIndex(functions)


def _annotate_finding(
    finding: CandidateFinding,
    *,
    reachable_functions: set[str],
    entry_points: set[str],
    indexed_functions: _FunctionIndex,
) -> CandidateFinding:
    metadata = dict(finding.metadata)
    source_function_id = _finding_source_function_id(finding, indexed_functions)
    if source_function_id is not None:
        metadata["source_function_id"] = source_function_id

    if finding.reachability != "reachable":
        return finding.model_copy(update={"metadata": metadata})

    if not entry_points or source_function_id is None:
        return finding.model_copy(update={"metadata": metadata})

    if source_function_id in reachable_functions:
        return finding.model_copy(update={"metadata": metadata})

    metadata.setdefault("reachability_original_severity", finding.severity)
    return finding.model_copy(
        update={
            "severity": "informational",
            "reachability": "unreachable",
            "metadata": metadata,
        }
    )


def _finding_source_function_id(
    finding: CandidateFinding,
    indexed_functions: _FunctionIndex,
) -> str | None:
    for key in ("source_function_id", "source_function", "function_id"):
        value = finding.metadata.get(key)
        if isinstance(value, str) and value in indexed_functions.function_ids:
            return value

    source_path = _normalize_path(finding.source.location.file)
    for step in finding.taint_path:
        if (
            isinstance(step.through_function, str)
            and step.through_function in indexed_functions.function_ids
            and _normalize_path(step.location.file) == source_path
        ):
            return step.through_function

    file_functions = indexed_functions.by_file.get(source_path, ())
    enclosing_function: ScannedFunction | None = None
    for function in file_functions:
        if function.location.line <= finding.source.location.line:
            enclosing_function = function
            continue
        break
    if enclosing_function is not None:
        return enclosing_function.function_id

    for step in finding.taint_path:
        if (
            isinstance(step.through_function, str)
            and step.through_function in indexed_functions.function_ids
        ):
            return step.through_function
    return None


def _dead_code_functions(
    functions: Sequence[ScannedFunction],
    *,
    unreachable_functions: set[str],
    include_tests: bool,
) -> list[ScannedFunction]:
    dead_code: list[ScannedFunction] = []
    for function in functions:
        if function.function_id not in unreachable_functions:
            continue
        if not _is_dead_code_candidate(function, include_tests=include_tests):
            continue
        dead_code.append(function)
    return sorted(dead_code, key=_function_sort_key)


def _is_dead_code_candidate(function: ScannedFunction, *, include_tests: bool) -> bool:
    if function.name in _NON_USER_CODE_NAMES or function.name.startswith("<operator>"):
        return False
    return include_tests or not _is_test_file(function.location.file)


def _is_explicit_main(function: ScannedFunction) -> bool:
    return function.name == "main"


def _is_test_entry_point(function: ScannedFunction) -> bool:
    return _is_test_file(function.location.file) or bool(
        _TEST_FUNCTION_PATTERN.match(function.name)
    )


def _is_exported_entry_point(
    function: ScannedFunction,
    *,
    file_index: _SourceFileIndex,
) -> bool:
    file_name = Path(function.location.file).name
    if file_name not in _EXPORT_ENTRY_FILENAMES:
        return False
    if file_name == "__init__.py":
        return not function.name.startswith("_")

    source_text = file_index.text_for(function.location.file)
    if source_text is None:
        return False
    escaped_name = re.escape(function.name)
    return any(
        re.search(pattern.format(name=escaped_name), source_text) for pattern in _JS_EXPORT_PATTERNS
    )


def _has_framework_route_reference(
    function: ScannedFunction,
    *,
    file_index: _SourceFileIndex,
) -> bool:
    lines = file_index.lines_for(function.location.file)
    source_text = file_index.text_for(function.location.file)
    if source_text is None:
        return False

    line_number = max(1, function.location.line)
    decorator_window = "\n".join(lines[max(0, line_number - 4) : line_number])
    if _PYTHON_ROUTE_DECORATOR_PATTERN.search(decorator_window):
        return True
    if _JAVA_ROUTE_DECORATOR_PATTERN.search(decorator_window):
        return True

    escaped_name = re.escape(function.name)
    return any(
        re.search(pattern.format(name=escaped_name), source_text)
        for pattern in (_NODE_ROUTE_TEMPLATE, _GO_ROUTE_TEMPLATE, _DJANGO_ROUTE_TEMPLATE)
    )


def _cli_entry_points(
    *,
    indexed_functions: _FunctionIndex,
    project_root: Path,
) -> set[str]:
    entry_points: set[str] = set()

    for bin_target in _package_bin_targets(project_root):
        for function in _functions_for_path(indexed_functions, project_root / bin_target):
            if function.name == ":program" or function.name == "main":
                entry_points.add(function.function_id)
        if not any(
            function.function_id in entry_points
            for function in _functions_for_path(indexed_functions, project_root / bin_target)
        ):
            stem = Path(bin_target).stem
            for function in indexed_functions.functions:
                if function.name == stem:
                    entry_points.add(function.function_id)

    for module_name, function_name in _pyproject_script_targets(project_root):
        candidate_path = project_root / Path(*module_name.split(".")).with_suffix(".py")
        for function in _functions_for_path(indexed_functions, candidate_path):
            if function.name == function_name:
                entry_points.add(function.function_id)
        if function_name:
            for function in indexed_functions.functions:
                if function.name == function_name:
                    entry_points.add(function.function_id)
    return entry_points


def _functions_for_path(
    indexed_functions: _FunctionIndex,
    path: Path,
) -> tuple[ScannedFunction, ...]:
    normalized = path.resolve(strict=False)
    return tuple(indexed_functions.by_file.get(normalized, ()))


def _package_bin_targets(project_root: Path) -> tuple[Path, ...]:
    targets: list[Path] = []
    for package_json in _iter_project_files(project_root, "package.json"):
        try:
            payload = json.loads(package_json.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            continue
        raw_bin = payload.get("bin")
        if isinstance(raw_bin, str) and raw_bin:
            targets.append(
                (package_json.parent / raw_bin)
                .resolve(strict=False)
                .relative_to(project_root.resolve(strict=False))
            )
            continue
        if isinstance(raw_bin, dict):
            for value in raw_bin.values():
                if isinstance(value, str) and value:
                    targets.append(
                        (package_json.parent / value)
                        .resolve(strict=False)
                        .relative_to(project_root.resolve(strict=False))
                    )
    return tuple(dict.fromkeys(targets))


def _pyproject_script_targets(project_root: Path) -> tuple[tuple[str, str], ...]:
    targets: list[tuple[str, str]] = []
    for pyproject in _iter_project_files(project_root, "pyproject.toml"):
        try:
            payload = tomllib.loads(pyproject.read_text(encoding="utf-8"))
        except (OSError, tomllib.TOMLDecodeError):
            continue
        project = payload.get("project")
        if not isinstance(project, dict):
            continue
        scripts = project.get("scripts")
        if not isinstance(scripts, dict):
            continue
        for value in scripts.values():
            if not isinstance(value, str) or ":" not in value:
                continue
            module_name, function_name = value.split(":", 1)
            targets.append((module_name.strip(), function_name.strip()))
    return tuple(dict.fromkeys(targets))


def _iter_project_files(project_root: Path, file_name: str) -> tuple[Path, ...]:
    root = project_root.resolve(strict=False)
    matches: list[Path] = []
    for candidate in root.rglob(file_name):
        if _is_excluded_entry_path(candidate):
            continue
        matches.append(candidate.resolve(strict=False))
    return tuple(matches)


def _is_excluded_entry_path(path: Path) -> bool:
    return any(
        part in _EXCLUDED_ENTRY_PARTS or part.startswith(_PIRANESI_TRACE_PREFIX)
        for part in path.parts
    )


def _is_test_file(path_str: str) -> bool:
    path = Path(path_str)
    if any(part.lower() == "fixtures" for part in path.parts):
        return False
    return bool(_TEST_FILE_PATTERN.search(path.as_posix()))


def _normalize_path(path_str: str) -> Path:
    return Path(path_str).resolve(strict=False)


def _function_sort_key(function: ScannedFunction) -> tuple[str, int, int, str]:
    return (
        function.location.file,
        function.location.line,
        function.location.column,
        function.name,
    )


__all__ = [
    "analyze_reachability",
    "build_call_graph",
    "compute_reachable",
    "identify_entry_points",
]
