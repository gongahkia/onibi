from __future__ import annotations

import ast
import hashlib
import io
import re
import tokenize
from bisect import bisect_right
from dataclasses import dataclass, field
from pathlib import Path
from typing import TYPE_CHECKING

from piranesi.scan.incremental import IncrementalResult

if TYPE_CHECKING:
    from piranesi.scan.cpg_graph import PiranesiCPG


_JS_FUNCTION_PATTERN = re.compile(
    r"""
    (?:
        (?P<named>(?:^|\n)\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s+
        (?P<named_name>[A-Za-z_$][\w$]*)\s*\((?P<named_params>[^)]*)\)\s*\{)
      |
        (?P<arrow>(?:^|\n)\s*(?:export\s+)?(?:const|let|var)\s+
        (?P<arrow_name>[A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?\((?P<arrow_params>[^)]*)\)\s*=>\s*\{)
      |
        (?P<expr>(?:^|\n)\s*(?:export\s+)?(?:const|let|var)\s+
        (?P<expr_name>[A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?function(?:\s+[A-Za-z_$][\w$]*)?\s*
        \((?P<expr_params>[^)]*)\)\s*\{)
      |
        (?P<method>(?:^|\n)\s*(?:public\s+|private\s+|protected\s+|static\s+|async\s+)*
        (?P<method_name>[A-Za-z_$][\w$]*)\s*\((?P<method_params>[^)]*)\)\s*\{)
    )
    """,
    re.MULTILINE | re.VERBOSE,
)
_JS_METHOD_KEYWORDS = frozenset({"if", "for", "while", "switch", "catch", "function"})
_JS_SUFFIXES = {".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs"}
_PYTHON_SUFFIXES = {".py"}


@dataclass(frozen=True, slots=True)
class ParsedFunction:
    function_id: str
    name: str
    relative_path: str
    line_start: int
    line_end: int
    parameters: tuple[str, ...]
    parameter_signature: str
    body_hash: str
    source: str
    anonymous: bool = False
    enclosing_scope: str | None = None


@dataclass(frozen=True, slots=True)
class FunctionDiff:
    added_functions: dict[str, ParsedFunction] = field(default_factory=dict)
    removed_functions: dict[str, ParsedFunction] = field(default_factory=dict)
    modified_functions: dict[str, ParsedFunction] = field(default_factory=dict)
    unchanged_functions: dict[str, ParsedFunction] = field(default_factory=dict)
    reparsed_files: dict[str, tuple[ParsedFunction, ...]] = field(default_factory=dict)
    whole_file_reanalysis: set[str] = field(default_factory=set)

    @property
    def changed_function_ids(self) -> set[str]:
        return {
            *self.added_functions.keys(),
            *self.removed_functions.keys(),
            *self.modified_functions.keys(),
        }

    @property
    def changed_file_paths(self) -> set[str]:
        changed: set[str] = set()
        for function in (
            *self.added_functions.values(),
            *self.removed_functions.values(),
            *self.modified_functions.values(),
        ):
            changed.add(function.relative_path)
        changed.update(self.whole_file_reanalysis)
        return changed


def compute_function_diff(
    old_cpg: PiranesiCPG,
    incremental: IncrementalResult,
    project_root: Path,
) -> FunctionDiff:
    added_functions: dict[str, ParsedFunction] = {}
    removed_functions: dict[str, ParsedFunction] = {}
    modified_functions: dict[str, ParsedFunction] = {}
    unchanged_functions: dict[str, ParsedFunction] = {}
    reparsed_files: dict[str, tuple[ParsedFunction, ...]] = {}
    whole_file_reanalysis: set[str] = set()

    for relative_path in sorted(incremental.deleted, key=lambda item: item.as_posix()):
        relative_key = relative_path.as_posix()
        old_ids = old_cpg.functions_by_file(relative_key)
        for function_id in old_ids:
            removed_functions[function_id] = old_cpg.functions[function_id].to_parsed_function()

    changed_files = sorted(
        incremental.changed_files,
        key=lambda item: item.as_posix(),
    )
    for relative_path in changed_files:
        absolute_path = (project_root / relative_path).resolve(strict=False)
        relative_key = relative_path.as_posix()
        parsed_functions, requires_whole_file_reanalysis = parse_functions_from_file(
            absolute_path,
            project_root,
        )
        reparsed_files[relative_key] = parsed_functions
        if requires_whole_file_reanalysis:
            whole_file_reanalysis.add(relative_key)

        old_ids = old_cpg.functions_by_file(relative_key)
        old_functions = {function_id: old_cpg.functions[function_id] for function_id in old_ids}
        new_functions = {function.function_id: function for function in parsed_functions}

        for function_id, new_function in new_functions.items():
            old_function = old_functions.get(function_id)
            if old_function is None:
                added_functions[function_id] = new_function
                continue
            if old_function.body_hash != new_function.body_hash:
                modified_functions[function_id] = new_function
            else:
                unchanged_functions[function_id] = new_function

        for function_id, old_function in old_functions.items():
            if function_id not in new_functions:
                removed_functions[function_id] = old_function.to_parsed_function()

    return FunctionDiff(
        added_functions=added_functions,
        removed_functions=removed_functions,
        modified_functions=modified_functions,
        unchanged_functions=unchanged_functions,
        reparsed_files=reparsed_files,
        whole_file_reanalysis=whole_file_reanalysis,
    )


def parse_functions_from_file(
    file_path: Path,
    project_root: Path,
) -> tuple[tuple[ParsedFunction, ...], bool]:
    if not file_path.exists():
        return (), False

    source = file_path.read_text(encoding="utf-8")
    relative_path = file_path.resolve(strict=False).relative_to(project_root).as_posix()
    suffix = file_path.suffix.lower()

    if suffix in _PYTHON_SUFFIXES:
        return _parse_python_functions(source, relative_path), False
    if suffix in _JS_SUFFIXES:
        parsed = _parse_js_like_functions(source, relative_path)
        return parsed, False
    return (), True


def function_body_hash(source: str, *, language: str | None = None) -> str:
    normalized = _strip_comments(source, language=language)
    normalized = re.sub(r"\s+", " ", normalized).strip()
    return hashlib.sha256(normalized.encode("utf-8")).hexdigest()


def _parse_python_functions(source: str, relative_path: str) -> tuple[ParsedFunction, ...]:
    try:
        tree = ast.parse(source)
    except SyntaxError:
        return ()

    lines = source.splitlines()
    parsed: list[ParsedFunction] = []
    parent_stack: list[tuple[int, int, str]] = []

    class _Visitor(ast.NodeVisitor):
        def visit_FunctionDef(self, node: ast.FunctionDef) -> None:
            _handle_function(node)
            self.generic_visit(node)

        def visit_AsyncFunctionDef(self, node: ast.AsyncFunctionDef) -> None:
            _handle_function(node)
            self.generic_visit(node)

    def _handle_function(node: ast.FunctionDef | ast.AsyncFunctionDef) -> None:
        while parent_stack and node.lineno > parent_stack[-1][1]:
            parent_stack.pop()

        line_start = node.lineno
        line_end = node.end_lineno or node.lineno
        source_segment = "\n".join(lines[line_start - 1 : line_end])
        parameters = tuple(argument.arg for argument in node.args.args)
        parameter_signature = ",".join(parameters)
        enclosing_scope = parent_stack[-1][2] if parent_stack else None
        function_id = build_function_identity(
            relative_path=relative_path,
            name=node.name,
            parameter_signature=parameter_signature,
            line_start=line_start,
            enclosing_scope=enclosing_scope,
            anonymous=False,
        )
        parsed.append(
            ParsedFunction(
                function_id=function_id,
                name=node.name,
                relative_path=relative_path,
                line_start=line_start,
                line_end=line_end,
                parameters=parameters,
                parameter_signature=parameter_signature,
                body_hash=function_body_hash(source_segment, language="python"),
                source=source_segment,
                enclosing_scope=enclosing_scope,
            )
        )
        parent_stack.append((line_start, line_end, node.name))

    _Visitor().visit(tree)
    return tuple(parsed)


def _parse_js_like_functions(source: str, relative_path: str) -> tuple[ParsedFunction, ...]:
    masked = _mask_js_like_source(source)
    line_starts = _line_starts(source)
    parsed: list[ParsedFunction] = []

    for match in _JS_FUNCTION_PATTERN.finditer(masked):
        name = (
            match.group("named_name")
            or match.group("arrow_name")
            or match.group("expr_name")
            or match.group("method_name")
        )
        params = (
            match.group("named_params")
            or match.group("arrow_params")
            or match.group("expr_params")
            or match.group("method_params")
            or ""
        )
        if not name or name in _JS_METHOD_KEYWORDS:
            continue

        brace_index = match.end() - 1
        end_index = _find_matching_brace(masked, brace_index)
        if end_index is None:
            continue

        line_start = _line_number_for_offset(line_starts, match.start())
        line_end = _line_number_for_offset(line_starts, end_index)
        source_segment = source[match.start() : end_index]
        parameters = tuple(
            _normalize_parameter_name(item) for item in params.split(",") if item.strip()
        )
        parameter_signature = ",".join(parameter for parameter in parameters if parameter)

        parsed.append(
            ParsedFunction(
                function_id=build_function_identity(
                    relative_path=relative_path,
                    name=name,
                    parameter_signature=parameter_signature,
                    line_start=line_start,
                    enclosing_scope=_enclosing_scope(parsed, line_start),
                    anonymous=False,
                ),
                name=name,
                relative_path=relative_path,
                line_start=line_start,
                line_end=line_end,
                parameters=tuple(parameter for parameter in parameters if parameter),
                parameter_signature=parameter_signature,
                body_hash=function_body_hash(source_segment, language="javascript"),
                source=source_segment,
                enclosing_scope=_enclosing_scope(parsed, line_start),
            )
        )

    return tuple(_dedupe_parsed_functions(parsed))


def build_function_identity(
    *,
    relative_path: str,
    name: str,
    parameter_signature: str,
    line_start: int,
    enclosing_scope: str | None,
    anonymous: bool,
) -> str:
    if anonymous:
        scope = enclosing_scope or "global"
        return f"{relative_path}::anonymous@{line_start}[{scope}]"
    return f"{relative_path}::{name}({parameter_signature})"


def _enclosing_scope(parsed_functions: list[ParsedFunction], line_start: int) -> str | None:
    for function in reversed(parsed_functions):
        if function.line_start <= line_start <= function.line_end:
            return function.name
    return None


def _normalize_parameter_name(raw_parameter: str) -> str:
    parameter = raw_parameter.strip()
    if not parameter:
        return ""
    parameter = re.sub(r"^[.]{3}", "", parameter)
    parameter = re.sub(r"^(public|private|protected|readonly|static)\s+", "", parameter)
    parameter = re.sub(r":.*$", "", parameter)
    parameter = re.sub(r"=.*$", "", parameter)
    return parameter.strip()


def _strip_comments(source: str, *, language: str | None = None) -> str:
    if language == "python":
        result: list[str] = []
        try:
            tokens = tokenize.generate_tokens(io.StringIO(source).readline)
            for token_type, token_string, *_ in tokens:
                if token_type == tokenize.COMMENT:
                    continue
                result.append(token_string)
        except tokenize.TokenError:
            return source
        return "".join(result)

    without_block_comments = re.sub(r"/\*.*?\*/", " ", source, flags=re.DOTALL)
    return re.sub(r"//.*?$", " ", without_block_comments, flags=re.MULTILINE)


def _mask_js_like_source(source: str) -> str:
    chars = list(source)
    index = 0
    length = len(chars)
    while index < length:
        char = chars[index]
        next_char = chars[index + 1] if index + 1 < length else ""
        if char == "/" and next_char == "/":
            while index < length and chars[index] != "\n":
                if chars[index] != "\n":
                    chars[index] = " "
                index += 1
            continue
        if char == "/" and next_char == "*":
            chars[index] = " "
            index += 1
            chars[index] = " "
            index += 1
            while index < length - 1:
                if chars[index] == "*" and chars[index + 1] == "/":
                    chars[index] = " "
                    chars[index + 1] = " "
                    index += 2
                    break
                if chars[index] != "\n":
                    chars[index] = " "
                index += 1
            continue
        if char in {'"', "'", "`"}:
            quote = char
            chars[index] = " "
            index += 1
            while index < length:
                current = chars[index]
                if current == "\\":
                    chars[index] = " "
                    if index + 1 < length and chars[index + 1] != "\n":
                        chars[index + 1] = " "
                    index += 2
                    continue
                if current == quote:
                    chars[index] = " "
                    index += 1
                    break
                if current != "\n":
                    chars[index] = " "
                index += 1
            continue
        index += 1
    return "".join(chars)


def _find_matching_brace(masked: str, start_index: int) -> int | None:
    depth = 0
    for index in range(start_index, len(masked)):
        char = masked[index]
        if char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
            if depth == 0:
                return index + 1
    return None


def _line_starts(source: str) -> tuple[int, ...]:
    starts = [0]
    for match in re.finditer(r"\n", source):
        starts.append(match.end())
    return tuple(starts)


def _line_number_for_offset(line_starts: tuple[int, ...], offset: int) -> int:
    return bisect_right(line_starts, offset)


def _dedupe_parsed_functions(functions: list[ParsedFunction]) -> list[ParsedFunction]:
    deduped: dict[str, ParsedFunction] = {}
    for function in functions:
        deduped.setdefault(function.function_id, function)
    return list(deduped.values())


__all__ = [
    "FunctionDiff",
    "ParsedFunction",
    "build_function_identity",
    "compute_function_diff",
    "function_body_hash",
    "parse_functions_from_file",
]
