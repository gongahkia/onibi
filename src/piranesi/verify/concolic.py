from __future__ import annotations

import copy
import re
import time
from collections.abc import Mapping, Sequence
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Literal

import z3  # type: ignore[import-untyped]

from piranesi.models import CandidateFinding, EntryPoint, SourceLocation, TaintStep
from piranesi.verify.constraints import ExploitTemplate, extract_exploit_template
from piranesi.verify.sandbox import SynthesizedPayload
from piranesi.verify.solver import (
    extract_model_values,
    synthesize_payload,
    vulnerability_constraints,
)

_IDENTIFIER_RE = re.compile(r"[A-Za-z_$][\w$]*")
_FUNCTION_RE = re.compile(r"\bfunction\s+(?P<name>[A-Za-z_$][\w$]*)\s*\((?P<params>[^)]*)\)\s*\{")
_COMMENT_LINE_RE = re.compile(r"//.*?$", re.MULTILINE)
_COMMENT_BLOCK_RE = re.compile(r"/\*.*?\*/", re.DOTALL)
_SOURCE_EXPR_RE = re.compile(
    r"\b(?P<root>[A-Za-z_$][\w$]*)\.(?P<section>body|query|params|headers)(?P<rest>(?:\.[A-Za-z_$][\w$]*|\[['\"][^'\"]+['\"]\])*)"
)
_COMPOUND_ASSIGNMENT_RE = re.compile(
    r"^(?P<target>.+?)\s*(?P<op>\+=|-=|\*=|/=|%=)\s*(?P<value>.+)$"
)
_REGEX_META_CHARS = frozenset(".*+?^$()[]{}\\|")
_REGEX_FLAGS_RE = re.compile(r"[A-Za-z]*$")
_REGEX_EXCLUSION_LITERALS = ("'", "<script>", ";", "|", "../")


@dataclass(slots=True)
class TraceStep:
    location: SourceLocation | None
    statement_text: str
    symbolic_state_snapshot: dict[str, str]
    constraint_added: str | None = None


@dataclass(slots=True)
class ConcolicInput:
    finding: CandidateFinding
    taint_path: list[TaintStep]
    function_asts: dict[str, Any]
    call_graph: dict[str, list[str]]
    entry_point: EntryPoint | None = None


@dataclass(slots=True)
class ConcolicResult:
    status: Literal["SAT", "UNSAT", "TIMEOUT", "UNKNOWN"]
    payload: SynthesizedPayload | None = None
    model_values: dict[str, str] | None = None
    execution_trace: list[TraceStep] = field(default_factory=list)
    path_constraints: list[str] = field(default_factory=list)
    infeasible_reason: str | None = None
    paths_explored: int = 0
    z3_solve_time_ms: int = 0


@dataclass(slots=True)
class SymbolicObject:
    name: str
    properties: dict[str, SymValue] = field(default_factory=dict)
    wildcard: SymValue | None = None


type SymValue = z3.ExprRef | SymbolicObject


@dataclass(slots=True)
class SymbolicState:
    store: dict[str, SymValue] = field(default_factory=dict)
    constraints: list[z3.BoolRef] = field(default_factory=list)
    trace: list[TraceStep] = field(default_factory=list)
    call_stack: list[str] = field(default_factory=list)
    return_value: SymValue | None = None
    terminated: bool = False
    path_depth: int = 0

    def clone(self) -> SymbolicState:
        return copy.deepcopy(self)


@dataclass(frozen=True, slots=True)
class FunctionDef:
    name: str
    params: tuple[str, ...]
    body: tuple[Statement, ...]
    source_name: str


@dataclass(frozen=True, slots=True)
class VarDecl:
    line: int
    text: str
    target_text: str
    expr_text: str | None


@dataclass(frozen=True, slots=True)
class AssignStmt:
    line: int
    text: str
    target_text: str
    expr_text: str


@dataclass(frozen=True, slots=True)
class IfStmt:
    line: int
    text: str
    condition_text: str
    then_body: tuple[Statement, ...]
    else_body: tuple[Statement, ...]


@dataclass(frozen=True, slots=True)
class WhileStmt:
    line: int
    text: str
    condition_text: str
    body: tuple[Statement, ...]


@dataclass(frozen=True, slots=True)
class ForStmt:
    line: int
    text: str
    init_text: str | None
    condition_text: str | None
    update_text: str | None
    body: tuple[Statement, ...]


@dataclass(frozen=True, slots=True)
class ReturnStmt:
    line: int
    text: str
    expr_text: str | None


@dataclass(frozen=True, slots=True)
class ExprStmt:
    line: int
    text: str
    expr_text: str


type Statement = VarDecl | AssignStmt | IfStmt | WhileStmt | ForStmt | ReturnStmt | ExprStmt


@dataclass(frozen=True, slots=True)
class LiteralExpr:
    value: str | int | bool | None


@dataclass(frozen=True, slots=True)
class NameExpr:
    name: str


@dataclass(frozen=True, slots=True)
class UnaryExpr:
    op: str
    operand: ExprNode


@dataclass(frozen=True, slots=True)
class BinaryExpr:
    op: str
    left: ExprNode
    right: ExprNode


@dataclass(frozen=True, slots=True)
class ConditionalExpr:
    condition: ExprNode
    consequent: ExprNode
    alternate: ExprNode


@dataclass(frozen=True, slots=True)
class MemberExpr:
    obj: ExprNode
    prop: ExprNode | str
    computed: bool = False


@dataclass(frozen=True, slots=True)
class CallExpr:
    func: ExprNode
    args: tuple[ExprNode, ...]


@dataclass(frozen=True, slots=True)
class ObjectEntry:
    key: str | None
    value: ExprNode
    spread: bool = False


@dataclass(frozen=True, slots=True)
class ObjectExpr:
    entries: tuple[ObjectEntry, ...]


@dataclass(frozen=True, slots=True)
class TemplateExpr:
    parts: tuple[str | ExprNode, ...]


type ExprNode = (
    LiteralExpr
    | NameExpr
    | UnaryExpr
    | BinaryExpr
    | ConditionalExpr
    | MemberExpr
    | CallExpr
    | ObjectExpr
    | TemplateExpr
)


def _compile_js_regex_to_z3(pattern: str) -> z3.ReRef | None:
    # Supports anchors, top-level alternation, char classes, \d/\w/\s, and simple quantifiers.
    if pattern == "":
        return _regex_zero_or_more_any()
    anchored_start = pattern.startswith("^")
    anchored_end = pattern.endswith("$") and not _is_regex_escaped(pattern, len(pattern) - 1)
    body_start = 1 if anchored_start else 0
    body_end = len(pattern) - 1 if anchored_end else len(pattern)
    body = pattern[body_start:body_end]
    if _has_unescaped_anchor(body) or _has_unsupported_regex_construct(body):
        return None
    compiled = _compile_regex_body_to_z3(body)
    if compiled is None:
        return None
    parts: list[z3.ReRef] = []
    if not anchored_start:
        parts.append(_regex_zero_or_more_any())
    parts.append(compiled)
    if not anchored_end:
        parts.append(_regex_zero_or_more_any())
    return _regex_concat(parts)


def _compile_regex_body_to_z3(pattern: str) -> z3.ReRef | None:
    alternatives = _split_regex_alternatives(pattern)
    if alternatives is None:
        return None
    if len(alternatives) > 1:
        compiled_alternatives = [_compile_regex_sequence_to_z3(part) for part in alternatives]
        if any(part is None for part in compiled_alternatives):
            return None
        return _regex_union([part for part in compiled_alternatives if part is not None])
    return _compile_regex_sequence_to_z3(pattern)


def _compile_regex_sequence_to_z3(pattern: str) -> z3.ReRef | None:
    atoms: list[z3.ReRef] = []
    index = 0
    while index < len(pattern):
        atom, index = _parse_regex_atom(pattern, index)
        if atom is None:
            return None
        atom, index = _parse_regex_quantifier(pattern, index, atom)
        if atom is None:
            return None
        atoms.append(atom)
    return _regex_concat(atoms)


def _parse_regex_atom(pattern: str, index: int) -> tuple[z3.ReRef | None, int]:
    char = pattern[index]
    if char == "[":
        return _parse_regex_char_class(pattern, index)
    if char == "\\":
        return _parse_regex_escape(pattern, index)
    if char == ".":
        return _regex_any_char(), index + 1
    if char in _REGEX_META_CHARS:
        return None, index
    return z3.Re(z3.StringVal(char)), index + 1


def _parse_regex_escape(pattern: str, index: int) -> tuple[z3.ReRef | None, int]:
    if index + 1 >= len(pattern):
        return None, index
    escaped = pattern[index + 1]
    if escaped.isdigit():
        return None, index
    shorthand = _regex_shorthand(escaped)
    if shorthand is not None:
        return shorthand, index + 2
    literal = _regex_escape_literal(escaped)
    if literal is None:
        return None, index
    return z3.Re(z3.StringVal(literal)), index + 2


def _parse_regex_char_class(pattern: str, index: int) -> tuple[z3.ReRef | None, int]:
    index += 1
    negated = index < len(pattern) and pattern[index] == "^"
    if negated:
        index += 1
    items: list[z3.ReRef] = []
    while index < len(pattern):
        if pattern[index] == "]":
            if not items:
                return None, index
            inner = _regex_union(items)
            if negated:
                return z3.Intersect(_regex_any_char(), z3.Complement(inner)), index + 1
            return inner, index + 1
        left, index = _parse_regex_class_item(pattern, index)
        if left is None:
            return None, index
        if (
            not left.startswith("\\")
            and len(left) == 1
            and index + 1 < len(pattern)
            and pattern[index] == "-"
            and pattern[index + 1] != "]"
        ):
            right, next_index = _parse_regex_class_item(pattern, index + 1)
            if right is None or len(right) != 1 or ord(left) > ord(right):
                return None, index
            items.append(z3.Range(left, right))
            index = next_index
            continue
        item = _regex_shorthand(left[1:]) if left.startswith("\\") else z3.Re(z3.StringVal(left))
        if item is None:
            return None, index
        items.append(item)
    return None, index


def _parse_regex_class_item(pattern: str, index: int) -> tuple[str | None, int]:
    if pattern[index] == "\\":
        if index + 1 >= len(pattern) or pattern[index + 1].isdigit():
            return None, index
        escaped = pattern[index + 1]
        if escaped in {"d", "w", "s"}:
            return f"\\{escaped}", index + 2
        literal = _regex_escape_literal(escaped)
        return literal, index + 2
    return pattern[index], index + 1


def _parse_regex_quantifier(
    pattern: str, index: int, atom: z3.ReRef
) -> tuple[z3.ReRef | None, int]:
    if index >= len(pattern):
        return atom, index
    quantifier = pattern[index]
    if quantifier == "+":
        return z3.Plus(atom), index + 1
    if quantifier == "*":
        return z3.Star(atom), index + 1
    if quantifier == "?":
        return z3.Option(atom), index + 1
    if quantifier != "{":
        return atom, index
    close = pattern.find("}", index + 1)
    if close == -1:
        return None, index
    spec = pattern[index + 1 : close]
    match = re.fullmatch(r"(\d+)(?:,(\d+))?", spec)
    if match is None:
        return None, index
    lower = int(match.group(1))
    upper = int(match.group(2)) if match.group(2) is not None else lower
    if upper < lower or upper > 64:
        return None, index
    return _regex_repeat(atom, lower, upper), close + 1


def _regex_repeat(atom: z3.ReRef, lower: int, upper: int) -> z3.ReRef:
    repeated = [_regex_concat([atom] * count) for count in range(lower, upper + 1)]
    return _regex_union(repeated)


def _regex_shorthand(char: str) -> z3.ReRef | None:
    if char == "d":
        return z3.Range("0", "9")
    if char == "w":
        return _regex_union(
            [z3.Range("a", "z"), z3.Range("A", "Z"), z3.Range("0", "9"), z3.Re("_")]
        )
    if char == "s":
        return _regex_union([z3.Re(z3.StringVal(ch)) for ch in " \t\n\r\f\v"])
    return None


def _regex_escape_literal(char: str) -> str | None:
    escapes = {"n": "\n", "r": "\r", "t": "\t", "f": "\f", "v": "\v"}
    if char in escapes:
        return escapes[char]
    if char in _REGEX_META_CHARS or char in {"/", "-"}:
        return char
    if char.isalpha():
        return None
    return char


def _regex_concat(parts: Sequence[z3.ReRef]) -> z3.ReRef:
    if not parts:
        return z3.Re(z3.StringVal(""))
    if len(parts) == 1:
        return parts[0]
    result = parts[0]
    for part in parts[1:]:
        result = z3.Concat(result, part)
    return result


def _regex_union(parts: Sequence[z3.ReRef]) -> z3.ReRef:
    if not parts:
        return z3.Re(z3.StringVal(""))
    if len(parts) == 1:
        return parts[0]
    result = parts[0]
    for part in parts[1:]:
        result = z3.Union(result, part)
    return result


def _regex_any_char() -> z3.ReRef:
    return z3.AllChar(z3.ReSort(z3.StringSort()))


def _regex_zero_or_more_any() -> z3.ReRef:
    return z3.Star(_regex_any_char())


def _split_regex_alternatives(pattern: str) -> list[str] | None:
    parts: list[str] = []
    class_depth = 0
    start = 0
    index = 0
    while index < len(pattern):
        char = pattern[index]
        if char == "\\":
            index += 2
            continue
        if char == "[":
            class_depth += 1
        elif char == "]" and class_depth > 0:
            class_depth -= 1
        elif char == "|" and class_depth == 0:
            parts.append(pattern[start:index])
            start = index + 1
        index += 1
    if class_depth != 0:
        return None
    parts.append(pattern[start:])
    return parts


def _has_unescaped_anchor(pattern: str) -> bool:
    index = 0
    while index < len(pattern):
        char = pattern[index]
        if char == "\\":
            index += 2
            continue
        if char in {"^", "$"}:
            return True
        index += 1
    return False


def _has_unsupported_regex_construct(pattern: str) -> bool:
    index = 0
    while index < len(pattern):
        char = pattern[index]
        if char == "\\":
            if index + 1 < len(pattern) and pattern[index + 1].isdigit():
                return True
            index += 2
            continue
        if char in {"(", ")"}:
            return True
        index += 1
    return False


def _is_regex_escaped(pattern: str, index: int) -> bool:
    backslashes = 0
    index -= 1
    while index >= 0 and pattern[index] == "\\":
        backslashes += 1
        index -= 1
    return backslashes % 2 == 1


def _regex_literal_body(raw_pattern: str) -> str | None:
    if not raw_pattern.startswith("/"):
        return None
    index = 1
    in_class = False
    while index < len(raw_pattern):
        char = raw_pattern[index]
        if char == "\\":
            index += 2
            continue
        if char == "[":
            in_class = True
            index += 1
            continue
        if char == "]":
            in_class = False
            index += 1
            continue
        if char == "/" and not in_class:
            flags = raw_pattern[index + 1 :]
            if _REGEX_FLAGS_RE.fullmatch(flags) is None or flags:
                return None
            return raw_pattern[1:index]
        index += 1
    return None


def _regex_exclusion_constraints(pattern: str, target: z3.ExprRef) -> list[z3.BoolRef]:
    constraints: list[z3.BoolRef] = []
    for literal in _REGEX_EXCLUSION_LITERALS:
        if any(_anchored_regex_excludes_char(pattern, char) for char in set(literal)):
            constraints.append(z3.Not(z3.Contains(target, z3.StringVal(literal))))
    return constraints


def _anchored_regex_excludes_char(pattern: str, forbidden: str) -> bool:
    if not (
        pattern.startswith("^")
        and pattern.endswith("$")
        and not _is_regex_escaped(pattern, len(pattern) - 1)
    ):
        return False
    body = pattern[1:-1]
    alternatives = _split_regex_alternatives(body)
    if alternatives is None:
        return False
    return all(_regex_sequence_excludes_char(part, forbidden) for part in alternatives)


def _regex_sequence_excludes_char(pattern: str, forbidden: str) -> bool:
    index = 0
    while index < len(pattern):
        excludes, index = _regex_atom_excludes_char(pattern, index, forbidden)
        if not excludes:
            return False
        index = _skip_regex_quantifier(pattern, index)
        if index is None:
            return False
    return True


def _regex_atom_excludes_char(pattern: str, index: int, forbidden: str) -> tuple[bool | None, int]:
    char = pattern[index]
    if char == "[":
        return _regex_class_excludes_char(pattern, index, forbidden)
    if char == "\\":
        if index + 1 >= len(pattern) or pattern[index + 1].isdigit():
            return None, index
        escaped = pattern[index + 1]
        shorthand = _regex_shorthand_chars(escaped)
        if shorthand is not None:
            return forbidden not in shorthand, index + 2
        literal = _regex_escape_literal(escaped)
        if literal is None:
            return None, index
        return literal != forbidden, index + 2
    if char == ".":
        return False, index + 1
    if char in _REGEX_META_CHARS:
        return None, index
    return char != forbidden, index + 1


def _regex_class_excludes_char(pattern: str, index: int, forbidden: str) -> tuple[bool | None, int]:
    index += 1
    negated = index < len(pattern) and pattern[index] == "^"
    if negated:
        index += 1
    contains = False
    while index < len(pattern):
        if pattern[index] == "]":
            return (contains if negated else not contains), index + 1
        left, index = _regex_class_item_contains_char(pattern, index, forbidden)
        if left is None:
            return None, index
        if (
            index + 1 < len(pattern)
            and pattern[index] == "-"
            and pattern[index + 1] != "]"
            and len(left[0]) == 1
        ):
            right, next_index = _regex_class_item_contains_char(pattern, index + 1, forbidden)
            if right is None or len(right[0]) != 1 or ord(left[0]) > ord(right[0]):
                return None, index
            contains = contains or ord(left[0]) <= ord(forbidden) <= ord(right[0])
            index = next_index
            continue
        contains = contains or left[1]
    return None, index


def _regex_class_item_contains_char(
    pattern: str, index: int, forbidden: str
) -> tuple[tuple[str, bool] | None, int]:
    if pattern[index] == "\\":
        if index + 1 >= len(pattern) or pattern[index + 1].isdigit():
            return None, index
        escaped = pattern[index + 1]
        shorthand = _regex_shorthand_chars(escaped)
        if shorthand is not None:
            return (f"\\{escaped}", forbidden in shorthand), index + 2
        literal = _regex_escape_literal(escaped)
        if literal is None:
            return None, index
        return (literal, literal == forbidden), index + 2
    return (pattern[index], pattern[index] == forbidden), index + 1


def _skip_regex_quantifier(pattern: str, index: int) -> int | None:
    if index >= len(pattern) or pattern[index] not in "+*?{":
        return index
    if pattern[index] in "+*?":
        return index + 1
    close = pattern.find("}", index + 1)
    if close == -1:
        return None
    spec = pattern[index + 1 : close]
    if re.fullmatch(r"\d+(?:,\d+)?", spec) is None:
        return None
    return close + 1


def _regex_shorthand_chars(char: str) -> set[str] | None:
    if char == "d":
        return set("0123456789")
    if char == "w":
        return set("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_")
    if char == "s":
        return set(" \t\n\r\f\v")
    return None


@dataclass(frozen=True, slots=True)
class _Token:
    kind: str
    value: str


class _ExpressionParser:
    def __init__(self, text: str) -> None:
        self._tokens = _tokenize(text)
        self._index = 0

    def parse(self) -> ExprNode:
        expression = self._parse_conditional()
        if self._peek().kind != "EOF":
            raise ValueError(f"unexpected token {self._peek().value!r}")
        return expression

    def _parse_conditional(self) -> ExprNode:
        condition = self._parse_or()
        if self._match("?"):
            consequent = self._parse_conditional()
            self._consume(":")
            alternate = self._parse_conditional()
            return ConditionalExpr(condition=condition, consequent=consequent, alternate=alternate)
        return condition

    def _parse_or(self) -> ExprNode:
        expression = self._parse_and()
        while self._match("||"):
            expression = BinaryExpr(op="||", left=expression, right=self._parse_and())
        return expression

    def _parse_and(self) -> ExprNode:
        expression = self._parse_equality()
        while self._match("&&"):
            expression = BinaryExpr(op="&&", left=expression, right=self._parse_equality())
        return expression

    def _parse_equality(self) -> ExprNode:
        expression = self._parse_comparison()
        while self._peek().value in {"==", "!=", "===", "!=="}:
            operator = self._advance().value
            expression = BinaryExpr(op=operator, left=expression, right=self._parse_comparison())
        return expression

    def _parse_comparison(self) -> ExprNode:
        expression = self._parse_additive()
        while self._peek().value in {"<", "<=", ">", ">="}:
            operator = self._advance().value
            expression = BinaryExpr(op=operator, left=expression, right=self._parse_additive())
        return expression

    def _parse_additive(self) -> ExprNode:
        expression = self._parse_multiplicative()
        while self._peek().value in {"+", "-"}:
            operator = self._advance().value
            expression = BinaryExpr(
                op=operator, left=expression, right=self._parse_multiplicative()
            )
        return expression

    def _parse_multiplicative(self) -> ExprNode:
        expression = self._parse_unary()
        while self._peek().value in {"*", "/", "%"}:
            operator = self._advance().value
            expression = BinaryExpr(op=operator, left=expression, right=self._parse_unary())
        return expression

    def _parse_unary(self) -> ExprNode:
        if self._peek().value in {"!", "-", "+"}:
            operator = self._advance().value
            return UnaryExpr(op=operator, operand=self._parse_unary())
        return self._parse_postfix()

    def _parse_postfix(self) -> ExprNode:
        expression = self._parse_primary()
        while True:
            if self._match("."):
                expression = MemberExpr(
                    obj=expression, prop=self._consume_identifier(), computed=False
                )
                continue
            if self._match("["):
                prop = self._parse_conditional()
                self._consume("]")
                expression = MemberExpr(obj=expression, prop=prop, computed=True)
                continue
            if self._match("("):
                args: list[ExprNode] = []
                if not self._match(")"):
                    while True:
                        args.append(self._parse_conditional())
                        if self._match(")"):
                            break
                        self._consume(",")
                expression = CallExpr(func=expression, args=tuple(args))
                continue
            return expression

    def _parse_primary(self) -> ExprNode:
        token = self._peek()
        if token.value == "(":
            self._advance()
            expression = self._parse_conditional()
            self._consume(")")
            return expression
        if token.value == "{":
            return self._parse_object()
        if token.kind == "NUMBER":
            self._advance()
            return LiteralExpr(value=int(token.value))
        if token.kind == "STRING":
            self._advance()
            return LiteralExpr(value=token.value)
        if token.kind == "REGEX":
            self._advance()
            return LiteralExpr(value=token.value)
        if token.kind == "TEMPLATE":
            self._advance()
            return TemplateExpr(parts=_parse_template_literal(token.value))
        if token.kind == "IDENT":
            self._advance()
            lowered = token.value
            if lowered == "true":
                return LiteralExpr(value=True)
            if lowered == "false":
                return LiteralExpr(value=False)
            if lowered in {"null", "undefined"}:
                return LiteralExpr(value=None)
            return NameExpr(name=token.value)
        raise ValueError(f"unsupported expression token {token.value!r}")

    def _parse_object(self) -> ExprNode:
        self._consume("{")
        entries: list[ObjectEntry] = []
        if self._match("}"):
            return ObjectExpr(entries=tuple(entries))
        while True:
            if self._match("..."):
                entries.append(ObjectEntry(key=None, value=self._parse_conditional(), spread=True))
            else:
                key_token = self._advance()
                if key_token.kind not in {"IDENT", "STRING"}:
                    raise ValueError(f"unsupported object literal key {key_token.value!r}")
                key = key_token.value
                value = self._parse_conditional() if self._match(":") else NameExpr(name=key)
                entries.append(ObjectEntry(key=key, value=value))
            if self._match("}"):
                break
            self._consume(",")
        return ObjectExpr(entries=tuple(entries))

    def _peek(self) -> _Token:
        return self._tokens[self._index]

    def _advance(self) -> _Token:
        token = self._peek()
        self._index += 1
        return token

    def _match(self, value: str) -> bool:
        if self._peek().value != value:
            return False
        self._index += 1
        return True

    def _consume(self, value: str) -> _Token:
        if self._peek().value != value:
            raise ValueError(f"expected {value!r}, found {self._peek().value!r}")
        return self._advance()

    def _consume_identifier(self) -> str:
        token = self._peek()
        if token.kind != "IDENT":
            raise ValueError(f"expected identifier, found {token.value!r}")
        self._advance()
        return token.value


class _ConcolicEngine:
    def __init__(
        self,
        inp: ConcolicInput,
        *,
        template: ExploitTemplate | None,
        max_paths: int,
        timeout_ms: int,
        loop_bound: int,
    ) -> None:
        self.inp = inp
        self.template = template or extract_exploit_template(inp.finding)
        self.max_paths = max_paths
        self.timeout_ms = timeout_ms
        self.loop_bound = loop_bound
        self.deadline = time.monotonic() + max(timeout_ms, 0) / 1000
        self.paths_explored = 0
        self.solve_time_ms = 0
        self._expr_cache: dict[str, ExprNode] = {}
        self._function_cache: dict[str, FunctionDef] = {}
        self._source_cache: dict[str, dict[str, FunctionDef]] = {}
        self._symbol_counter = 0
        self._best_unknown: ConcolicResult | None = None
        self._input_vars: dict[str, z3.ExprRef] = {}
        self._function_aliases = self._build_function_aliases()

    def run(self) -> ConcolicResult:
        if self.timeout_ms <= 0:
            return ConcolicResult(status="TIMEOUT", paths_explored=0)
        try:
            initial_state = self._init_state()
            entry = self._resolve_entry_function()
            if entry is None:
                return ConcolicResult(
                    status="UNKNOWN",
                    infeasible_reason="entry function could not be resolved",
                    paths_explored=0,
                )
            states = self._execute_function(entry, initial_state)
            if self._best_unknown is not None:
                self._best_unknown.paths_explored = self.paths_explored
                self._best_unknown.z3_solve_time_ms = self.solve_time_ms
                return self._best_unknown
            if states:
                self.paths_explored = max(self.paths_explored, len(states))
                return ConcolicResult(
                    status="UNKNOWN",
                    execution_trace=states[0].trace,
                    path_constraints=[str(item) for item in states[0].constraints],
                    infeasible_reason="sink was not reached on explored paths",
                    paths_explored=self.paths_explored,
                    z3_solve_time_ms=self.solve_time_ms,
                )
            return ConcolicResult(
                status="UNSAT",
                infeasible_reason="all explored paths infeasible",
                paths_explored=self.paths_explored,
                z3_solve_time_ms=self.solve_time_ms,
            )
        except TimeoutError:
            return ConcolicResult(
                status="TIMEOUT",
                execution_trace=self._best_unknown.execution_trace if self._best_unknown else [],
                path_constraints=self._best_unknown.path_constraints if self._best_unknown else [],
                paths_explored=self.paths_explored,
                z3_solve_time_ms=self.solve_time_ms,
            )

    def _build_function_aliases(self) -> dict[str, str]:
        aliases: dict[str, str] = {}
        for function_id in self.inp.function_asts:
            aliases[function_id] = function_id
            aliases[_short_function_name(function_id)] = function_id
        entry_point = self.inp.entry_point
        if entry_point is not None:
            aliases[entry_point.function_id] = entry_point.function_id
            aliases[_short_function_name(entry_point.function_id)] = entry_point.function_id
        return aliases

    def _init_state(self) -> SymbolicState:
        state = SymbolicState()
        entry_point = self.inp.entry_point or _default_entry_point(self.inp.finding)
        state.call_stack.append(entry_point.function_id)

        request_objects = {
            "body": SymbolicObject(name="req.body"),
            "query": SymbolicObject(name="req.query"),
            "params": SymbolicObject(name="req.params"),
            "headers": SymbolicObject(name="req.headers"),
        }
        for section, obj in request_objects.items():
            obj.wildcard = self._fresh_string(f"{section}_wildcard")

        if self.template.payload_slots:
            for slot in self.template.payload_slots:
                value = self._fresh_string(f"input_{slot.name}")
                self._input_vars[slot.name] = value
                target = request_objects.get(slot.carrier)
                if target is not None:
                    _set_nested_property(target, slot.field_path, value, f"req.{slot.carrier}")
                else:
                    state.store[slot.name] = value
        else:
            source_match = _source_expression(self.inp.finding.source.source_type)
            name = self.inp.finding.source.parameter_name or "input"
            value = self._fresh_string(f"input_{name}")
            self._input_vars[name] = value
            if source_match is not None and source_match["section"] in request_objects:
                path = _path_from_rest(source_match["rest"]) or (name,)
                _set_nested_property(
                    request_objects[source_match["section"]],
                    path,
                    value,
                    f"req.{source_match['section']}",
                )
            state.store[name] = value

        state.store["req"] = SymbolicObject(
            name="req",
            properties=dict(request_objects),
        )
        for name in entry_point.parameters:
            if name not in state.store:
                if name == "req":
                    continue
                state.store[name] = self._fresh_string(name)
        return state

    def _resolve_entry_function(self) -> FunctionDef | None:
        entry_point = self.inp.entry_point or _default_entry_point(self.inp.finding)
        exact = self._get_function(entry_point.function_id)
        if exact is not None:
            return exact
        short = self._get_function(_short_function_name(entry_point.function_id))
        if short is not None:
            return short
        for function_id in self.inp.function_asts:
            function = self._get_function(function_id)
            if function is not None:
                return function
        return None

    def _get_function(self, name: str) -> FunctionDef | None:
        cached = self._function_cache.get(name)
        if cached is not None:
            return cached
        canonical = self._function_aliases.get(name, name)
        source = self.inp.function_asts.get(canonical)
        if source is None and canonical != name:
            source = self.inp.function_asts.get(name)
        if source is not None:
            functions = self._parse_source_functions(canonical, source)
            if canonical in functions:
                self._function_cache[name] = functions[canonical]
                return functions[canonical]
            short_name = _short_function_name(canonical)
            if short_name in functions:
                self._function_cache[name] = functions[short_name]
                return functions[short_name]
            if functions:
                function = next(iter(functions.values()))
                self._function_cache[name] = function
                return function
        for source_name, raw_source in self.inp.function_asts.items():
            functions = self._parse_source_functions(source_name, raw_source)
            if name in functions:
                self._function_cache[name] = functions[name]
                return functions[name]
            short_name = _short_function_name(name)
            if short_name in functions:
                self._function_cache[name] = functions[short_name]
                return functions[short_name]
        return None

    def _parse_source_functions(self, source_name: str, raw_source: Any) -> dict[str, FunctionDef]:
        cached = self._source_cache.get(source_name)
        if cached is not None:
            return cached
        source = _extract_source_text(raw_source)
        stripped = _strip_comments(source)
        functions: dict[str, FunctionDef] = {}
        for match in _FUNCTION_RE.finditer(stripped):
            name = match.group("name")
            body_start = match.end() - 1
            body_end = _find_matching_delimiter(stripped, body_start, "{", "}")
            body_text = stripped[body_start + 1 : body_end]
            params = tuple(
                item.strip() for item in match.group("params").split(",") if item.strip()
            )
            body_line = stripped[: body_start + 1].count("\n") + 1
            function = FunctionDef(
                name=name,
                params=params,
                body=tuple(_parse_block(body_text, base_line=body_line + 1)),
                source_name=source_name,
            )
            functions[name] = function
            functions[f"{source_name}:{name}"] = function
            functions[_short_function_name(f"{source_name}:{name}")] = function
        if not functions:
            synthetic = FunctionDef(
                name=_short_function_name(source_name),
                params=tuple(
                    (self.inp.entry_point or _default_entry_point(self.inp.finding)).parameters
                ),
                body=tuple(_parse_block(stripped, base_line=1)),
                source_name=source_name,
            )
            functions[source_name] = synthetic
            functions[synthetic.name] = synthetic
        self._source_cache[source_name] = functions
        return functions

    def _execute_function(self, function: FunctionDef, state: SymbolicState) -> list[SymbolicState]:
        self._check_timeout()
        prior_stack = tuple(state.call_stack)
        state.call_stack.append(function.name)
        try:
            if function.params:
                for param in function.params:
                    state.store.setdefault(param, self._fresh_string(param))
            return self._execute_block(function.body, state, function=function)
        finally:
            state.call_stack[:] = list(prior_stack)

    def _execute_block(
        self,
        statements: Sequence[Statement],
        state: SymbolicState,
        *,
        function: FunctionDef,
    ) -> list[SymbolicState]:
        states = [state]
        for statement in statements:
            next_states: list[SymbolicState] = []
            for current in states:
                self._check_timeout()
                if current.terminated:
                    next_states.append(current)
                    continue
                successors = self._execute_statement(statement, current, function=function)
                if isinstance(successors, ConcolicResult):
                    return []
                next_states.extend(successors)
                if self._best_unknown is not None and self._best_unknown.status == "SAT":
                    return []
            states = next_states
            if not states:
                return []
        return states

    def _execute_statement(
        self,
        statement: Statement,
        state: SymbolicState,
        *,
        function: FunctionDef,
    ) -> list[SymbolicState]:
        if isinstance(statement, VarDecl):
            next_state = state.clone()
            if statement.expr_text is None:
                next_state.store[statement.target_text] = self._fresh_string(statement.target_text)
            elif _is_destructuring(statement.target_text):
                self._exec_destructuring(next_state, statement.target_text, statement.expr_text)
            else:
                next_state.store[statement.target_text] = self._eval_text(
                    statement.expr_text, next_state
                )
            self._append_trace(next_state, function, statement)
            return [next_state]
        if isinstance(statement, AssignStmt):
            next_state = state.clone()
            self._apply_assignment(next_state, statement.target_text, statement.expr_text)
            self._append_trace(next_state, function, statement)
            return [next_state]
        if isinstance(statement, ExprStmt):
            next_state = state.clone()
            self._append_trace(next_state, function, statement)
            result = self._eval_text(statement.expr_text, next_state)
            sink_result = self._maybe_solve_at_sink(
                next_state, statement, result, function=function
            )
            if sink_result is not None:
                if sink_result.status == "SAT":
                    self._best_unknown = sink_result
                    return []
                if self._best_unknown is None:
                    self._best_unknown = sink_result
            return [next_state]
        if isinstance(statement, ReturnStmt):
            next_state = state.clone()
            next_state.return_value = (
                self._eval_text(statement.expr_text, next_state)
                if statement.expr_text is not None
                else z3.StringVal("")
            )
            next_state.terminated = True
            self._append_trace(next_state, function, statement)
            return [next_state]
        if isinstance(statement, IfStmt):
            return self._exec_if(statement, state, function=function)
        if isinstance(statement, WhileStmt):
            return self._exec_while(statement, state, function=function)
        return self._exec_for(statement, state, function=function)

    def _exec_if(
        self, statement: IfStmt, state: SymbolicState, *, function: FunctionDef
    ) -> list[SymbolicState]:
        condition = self._to_bool(self._eval_text(statement.condition_text, state), state)
        true_state = state.clone()
        false_state = state.clone()
        true_state.constraints.append(condition)
        false_state.constraints.append(z3.Not(condition))
        true_state.path_depth += 1
        false_state.path_depth += 1

        ordered = self._prioritize_branches(
            [
                (statement.then_body, true_state, condition),
                (statement.else_body, false_state, z3.Not(condition)),
            ],
            sink_hint=self.inp.finding.sink.location.snippet,
        )
        feasible_results: list[tuple[z3.BoolRef, list[SymbolicState]]] = []
        for body, branch_state, branch_condition in ordered:
            self._check_timeout()
            if self.paths_explored >= self.max_paths:
                raise TimeoutError
            self.paths_explored += 1
            if not self._is_feasible(branch_state.constraints):
                continue
            self._append_trace(
                branch_state,
                function,
                statement,
                constraint=str(branch_condition),
            )
            executed = self._execute_block(body, branch_state, function=function)
            if self._best_unknown is not None and self._best_unknown.status == "SAT":
                return []
            feasible_results.append((branch_condition, executed))
        if not feasible_results:
            return []
        if len(feasible_results) == 1:
            return feasible_results[0][1]
        first_condition, first_states = feasible_results[0]
        second_condition, second_states = feasible_results[1]
        if len(first_states) == len(second_states) == 1:
            merged = self._merge_states(
                first_states[0],
                second_states[0],
                first_condition if "Not(" not in str(first_condition) else second_condition,
            )
            return [merged]
        combined: list[SymbolicState] = []
        combined.extend(first_states)
        combined.extend(second_states)
        return combined

    def _exec_while(
        self,
        statement: WhileStmt,
        state: SymbolicState,
        *,
        function: FunctionDef,
    ) -> list[SymbolicState]:
        current = state.clone()
        modified_targets = _collect_modified_targets(statement.body)
        for _ in range(self.loop_bound):
            self._check_timeout()
            condition = self._to_bool(self._eval_text(statement.condition_text, current), current)
            loop_state = current.clone()
            loop_state.constraints.append(condition)
            if not self._is_feasible(loop_state.constraints):
                current.constraints.append(z3.Not(condition))
                self._append_trace(current, function, statement, constraint=str(z3.Not(condition)))
                return [current]
            current.constraints.append(condition)
            self._append_trace(current, function, statement, constraint=str(condition))
            states = self._execute_block(statement.body, current, function=function)
            if not states:
                return []
            current = states[0]
            if current.terminated:
                return [current]
        self._havoc_targets(current, modified_targets)
        exit_condition = z3.Not(
            self._to_bool(self._eval_text(statement.condition_text, current), current)
        )
        current.constraints.append(exit_condition)
        self._append_trace(current, function, statement, constraint=str(exit_condition))
        return [current]

    def _exec_for(
        self, statement: ForStmt, state: SymbolicState, *, function: FunctionDef
    ) -> list[SymbolicState]:
        current = state.clone()
        if statement.init_text:
            self._exec_inline_statement(current, statement.init_text)
        modified_targets = _collect_modified_targets(statement.body)
        if statement.update_text:
            modified_targets.update(_collect_inline_targets(statement.update_text))
        for _ in range(self.loop_bound):
            self._check_timeout()
            if statement.condition_text:
                condition = self._to_bool(
                    self._eval_text(statement.condition_text, current), current
                )
                tentative = current.clone()
                tentative.constraints.append(condition)
                if not self._is_feasible(tentative.constraints):
                    current.constraints.append(z3.Not(condition))
                    self._append_trace(
                        current, function, statement, constraint=str(z3.Not(condition))
                    )
                    return [current]
                current.constraints.append(condition)
            self._append_trace(
                current,
                function,
                statement,
                constraint=str(current.constraints[-1]) if current.constraints else None,
            )
            states = self._execute_block(statement.body, current, function=function)
            if not states:
                return []
            current = states[0]
            if statement.update_text:
                self._exec_inline_statement(current, statement.update_text)
        self._havoc_targets(current, modified_targets)
        if statement.condition_text:
            exit_condition = z3.Not(
                self._to_bool(self._eval_text(statement.condition_text, current), current)
            )
            current.constraints.append(exit_condition)
        return [current]

    def _exec_inline_statement(self, state: SymbolicState, text: str) -> None:
        inline = _parse_simple_statement(text, line=0)
        if isinstance(inline, VarDecl):
            if inline.expr_text is None:
                state.store[inline.target_text] = self._fresh_string(inline.target_text)
            elif _is_destructuring(inline.target_text):
                self._exec_destructuring(state, inline.target_text, inline.expr_text)
            else:
                state.store[inline.target_text] = self._eval_text(inline.expr_text, state)
            return
        if isinstance(inline, AssignStmt):
            self._apply_assignment(state, inline.target_text, inline.expr_text)
            return
        if isinstance(inline, ExprStmt):
            if inline.expr_text.endswith("++"):
                target = inline.expr_text[:-2].strip()
                state.store[target] = (
                    self._to_number(state.store.get(target, z3.IntVal(0)), state) + 1
                )
                return
            if inline.expr_text.endswith("--"):
                target = inline.expr_text[:-2].strip()
                state.store[target] = (
                    self._to_number(state.store.get(target, z3.IntVal(0)), state) - 1
                )
                return
            self._eval_text(inline.expr_text, state)

    def _apply_assignment(self, state: SymbolicState, target_text: str, expr_text: str) -> None:
        if _is_destructuring(target_text):
            self._exec_destructuring(state, target_text, expr_text)
            return
        value = self._eval_text(expr_text, state)
        try:
            target = self._parse_expr(target_text)
        except ValueError:
            state.store[target_text] = value
            return
        if isinstance(target, NameExpr):
            state.store[target.name] = value
            return
        if not isinstance(target, MemberExpr):
            state.store[target_text] = value
            return
        obj_value = self._eval_expr(target.obj, state)
        obj = self._ensure_object(obj_value, name_hint=_member_base_name(target))
        if target.computed:
            prop_value = self._eval_expr(
                target.prop if not isinstance(target.prop, str) else LiteralExpr(target.prop), state
            )
            key = _concrete_string(prop_value)
            if key is None:
                obj.wildcard = value
            else:
                obj.properties[key] = value
        else:
            obj.properties[str(target.prop)] = value
        if isinstance(target.obj, NameExpr):
            state.store[target.obj.name] = obj

    def _exec_destructuring(self, state: SymbolicState, target_text: str, expr_text: str) -> None:
        obj = self._ensure_object(self._eval_text(expr_text, state), name_hint="destructure")
        for key, alias in _parse_destructuring_pattern(target_text):
            state.store[alias] = self._read_property(obj, key)

    def _maybe_solve_at_sink(
        self,
        state: SymbolicState,
        statement: ExprStmt,
        result: SymValue,
        *,
        function: FunctionDef,
    ) -> ConcolicResult | None:
        sink = self.inp.finding.sink
        snippet = sink.location.snippet.strip()
        normalized_statement = " ".join(statement.text.split())
        normalized_snippet = " ".join(snippet.split())
        if sink.api_name not in statement.text and normalized_snippet not in normalized_statement:
            return None
        call_expr = self._parse_expr(statement.expr_text)
        sink_value: SymValue = result
        if isinstance(call_expr, CallExpr) and call_expr.args:
            sink_value = self._eval_expr(call_expr.args[0], state)
        return self._solve_sink(
            state, sink_value, function=function, line=statement.line, text=statement.text
        )

    def _solve_sink(
        self,
        state: SymbolicState,
        sink_value: SymValue,
        *,
        function: FunctionDef,
        line: int,
        text: str,
    ) -> ConcolicResult:
        self._check_timeout()
        solver = z3.Solver()
        remaining_ms = max(1, int((self.deadline - time.monotonic()) * 1000))
        solver.set("timeout", remaining_ms)
        for constraint in state.constraints:
            solver.add(constraint)
        candidate_exprs = self._sink_candidates(sink_value, state)
        if not candidate_exprs:
            candidate_exprs = [self._to_string(sink_value, state)]
        for candidate in candidate_exprs:
            for assertion in vulnerability_constraints(self.inp.finding.vuln_class, candidate):
                solver.add(assertion)
        start = time.monotonic()
        outcome = solver.check()
        self.solve_time_ms += int((time.monotonic() - start) * 1000)
        if outcome == z3.unsat:
            return ConcolicResult(
                status="UNSAT",
                execution_trace=state.trace,
                path_constraints=[str(item) for item in state.constraints],
                infeasible_reason="path constraints unsatisfiable at sink",
                paths_explored=self.paths_explored,
                z3_solve_time_ms=self.solve_time_ms,
            )
        if outcome == z3.unknown:
            reason = solver.reason_unknown() if hasattr(solver, "reason_unknown") else "unknown"
            status: Literal["TIMEOUT", "UNKNOWN"] = (
                "TIMEOUT" if "timeout" in reason.lower() else "UNKNOWN"
            )
            return ConcolicResult(
                status=status,
                execution_trace=state.trace,
                path_constraints=[str(item) for item in state.constraints],
                infeasible_reason=reason,
                paths_explored=self.paths_explored,
                z3_solve_time_ms=self.solve_time_ms,
            )

        model_values = extract_model_values(solver.model(), self._input_vars)
        payload = None
        if self.template.payload_slots:
            payload = synthesize_payload(
                self.template,
                slot=self.template.payload_slots[0],
                model_values=model_values,
            )
        location = SourceLocation(
            file=function.source_name,
            line=line,
            column=1,
            snippet=text,
        )
        trace = list(state.trace)
        trace.append(
            TraceStep(
                location=location,
                statement_text=text,
                symbolic_state_snapshot=self._snapshot_state(state),
                constraint_added="sink",
            )
        )
        return ConcolicResult(
            status="SAT",
            payload=payload,
            model_values=model_values,
            execution_trace=trace,
            path_constraints=[str(item) for item in state.constraints],
            paths_explored=self.paths_explored,
            z3_solve_time_ms=self.solve_time_ms,
        )

    def _sink_candidates(self, value: SymValue, state: SymbolicState) -> list[z3.ExprRef]:
        if isinstance(value, SymbolicObject):
            wildcard = value.wildcard
            if wildcard is not None:
                return self._sink_candidates(wildcard, state)
            return []
        if not self._depends_on_input(value):
            return []
        if value.num_args() == 0:
            return [self._to_string(value, state)]
        if value.decl().kind() == z3.Z3_OP_SEQ_CONCAT:
            candidates: list[z3.ExprRef] = []
            for child in value.children():
                candidates.extend(self._sink_candidates(child, state))
            return candidates or [self._to_string(value, state)]
        return [self._to_string(value, state)]

    def _eval_text(self, text: str, state: SymbolicState) -> SymValue:
        return self._eval_expr(self._parse_expr(text), state)

    def _parse_expr(self, text: str) -> ExprNode:
        cached = self._expr_cache.get(text)
        if cached is not None:
            return cached
        parsed = _ExpressionParser(text).parse()
        self._expr_cache[text] = parsed
        return parsed

    def _eval_expr(self, expression: ExprNode, state: SymbolicState) -> SymValue:
        if isinstance(expression, LiteralExpr):
            if expression.value is None:
                return z3.StringVal("")
            if isinstance(expression.value, bool):
                return z3.BoolVal(expression.value)
            if isinstance(expression.value, int):
                return z3.IntVal(expression.value)
            return z3.StringVal(expression.value)
        if isinstance(expression, NameExpr):
            return state.store.get(expression.name, self._fresh_string(expression.name))
        if isinstance(expression, UnaryExpr):
            operand = self._eval_expr(expression.operand, state)
            if expression.op == "!":
                return z3.Not(self._to_bool(operand, state))
            if expression.op == "-":
                return -self._to_number(operand, state)
            return self._to_number(operand, state)
        if isinstance(expression, BinaryExpr):
            left = self._eval_expr(expression.left, state)
            right = self._eval_expr(expression.right, state)
            operator = expression.op
            if operator == "+":
                if _is_string_like(left) or _is_string_like(right):
                    return z3.Concat(self._to_string(left, state), self._to_string(right, state))
                return self._to_number(left, state) + self._to_number(right, state)
            if operator == "-":
                return self._to_number(left, state) - self._to_number(right, state)
            if operator == "*":
                return self._to_number(left, state) * self._to_number(right, state)
            if operator == "/":
                return z3.ToReal(self._to_number(left, state)) / z3.ToReal(
                    self._to_number(right, state)
                )
            if operator == "%":
                return z3.Mod(self._to_number(left, state), self._to_number(right, state))
            if operator == "&&":
                return z3.And(self._to_bool(left, state), self._to_bool(right, state))
            if operator == "||":
                return z3.Or(self._to_bool(left, state), self._to_bool(right, state))
            if operator == "===":
                return self._strict_eq(left, right, state)
            if operator == "!==":
                return z3.Not(self._strict_eq(left, right, state))
            if operator == "==":
                return self._loose_eq(left, right, state)
            if operator == "!=":
                return z3.Not(self._loose_eq(left, right, state))
            if operator == "<":
                return self._to_number(left, state) < self._to_number(right, state)
            if operator == "<=":
                return self._to_number(left, state) <= self._to_number(right, state)
            if operator == ">":
                return self._to_number(left, state) > self._to_number(right, state)
            return self._to_number(left, state) >= self._to_number(right, state)
        if isinstance(expression, ConditionalExpr):
            condition = self._to_bool(self._eval_expr(expression.condition, state), state)
            consequent = self._eval_expr(expression.consequent, state)
            alternate = self._eval_expr(expression.alternate, state)
            if isinstance(consequent, SymbolicObject) or isinstance(alternate, SymbolicObject):
                return (
                    consequent if self._is_feasible([*state.constraints, condition]) else alternate
                )
            return z3.If(
                condition,
                self._coerce_same_sort(consequent, alternate, state)[0],
                self._coerce_same_sort(consequent, alternate, state)[1],
            )
        if isinstance(expression, MemberExpr):
            obj = self._eval_expr(expression.obj, state)
            if expression.computed:
                prop = self._eval_expr(
                    expression.prop
                    if not isinstance(expression.prop, str)
                    else LiteralExpr(expression.prop),
                    state,
                )
                key = _concrete_string(prop)
                return (
                    self._read_property(
                        self._ensure_object(obj, name_hint=_member_base_name(expression)), key
                    )
                    if key is not None
                    else self._read_property(
                        self._ensure_object(obj, name_hint=_member_base_name(expression)), None
                    )
                )
            if (
                isinstance(obj, z3.ExprRef)
                and expression.prop == "length"
                and obj.sort() == z3.StringSort()
            ):
                return z3.Length(obj)
            return self._read_property(
                self._ensure_object(obj, name_hint=_member_base_name(expression)),
                str(expression.prop),
            )
        if isinstance(expression, CallExpr):
            return self._eval_call(expression, state)
        if isinstance(expression, ObjectExpr):
            obj = SymbolicObject(name=f"object_{self._symbol_counter}")
            for entry in expression.entries:
                if entry.spread:
                    spread_obj = self._ensure_object(
                        self._eval_expr(entry.value, state), name_hint="spread"
                    )
                    obj.properties.update(spread_obj.properties)
                    if spread_obj.wildcard is not None:
                        obj.wildcard = spread_obj.wildcard
                elif entry.key is not None:
                    obj.properties[entry.key] = self._eval_expr(entry.value, state)
            return obj
        parts: list[z3.ExprRef] = []
        for part in expression.parts:
            if isinstance(part, str):
                parts.append(z3.StringVal(part))
            else:
                parts.append(self._to_string(self._eval_expr(part, state), state))
        if not parts:
            return z3.StringVal("")
        result = parts[0]
        for part in parts[1:]:
            result = z3.Concat(result, part)
        return result

    def _eval_call(self, expression: CallExpr, state: SymbolicState) -> SymValue:
        func = expression.func
        args = [self._eval_expr(arg, state) for arg in expression.args]
        if isinstance(func, NameExpr):
            if func.name == "parseInt" or func.name == "Number":
                return self._to_number(args[0] if args else z3.StringVal("0"), state)
            if func.name == "String":
                return self._to_string(args[0] if args else z3.StringVal(""), state)
            if func.name == "Boolean":
                return self._to_bool(args[0] if args else z3.BoolVal(False), state)
            # URI/percent encode+decode modeled as identity so taint still flows
            # through. Enables reasoning about double-encoding bypass: a sink
            # guarded by Contains("'") still sees the payload after decodeURIComponent.
            if func.name in {
                "encodeURIComponent",
                "encodeURI",
                "escape",
                "decodeURIComponent",
                "decodeURI",
                "unescape",
            }:
                return self._to_string(args[0] if args else z3.StringVal(""), state)
            called = self._get_function(func.name)
            if called is not None:
                return self._call_function(called, args, state)
            return self._fresh_string(func.name)
        if isinstance(func, MemberExpr):
            receiver = self._eval_expr(func.obj, state)
            method = func.prop if isinstance(func.prop, str) else None
            if (
                isinstance(func.obj, NameExpr)
                and func.obj.name == "Math"
                and method in {"floor", "ceil", "max", "min"}
            ):
                return self._eval_math_call(method, args, state)
            if method == "slice" or method == "substring":
                string_value = self._to_string(receiver, state)
                start = self._to_number(args[0] if args else z3.IntVal(0), state)
                end = self._to_number(args[1], state) if len(args) > 1 else z3.Length(string_value)
                return z3.SubString(string_value, start, end - start)
            if method == "indexOf":
                return z3.IndexOf(
                    self._to_string(receiver, state),
                    self._to_string(args[0] if args else z3.StringVal(""), state),
                    z3.IntVal(0),
                )
            if method in {"includes", "contains"}:
                return z3.Contains(
                    self._to_string(receiver, state),
                    self._to_string(args[0] if args else z3.StringVal(""), state),
                )
            if method == "startsWith":
                return z3.PrefixOf(
                    self._to_string(args[0] if args else z3.StringVal(""), state),
                    self._to_string(receiver, state),
                )
            if method == "endsWith":
                return z3.SuffixOf(
                    self._to_string(args[0] if args else z3.StringVal(""), state),
                    self._to_string(receiver, state),
                )
            if method in {"replace", "replaceAll"}:
                return z3.Replace(
                    self._to_string(receiver, state),
                    self._to_string(args[0] if args else z3.StringVal(""), state),
                    self._to_string(args[1] if len(args) > 1 else z3.StringVal(""), state),
                )
            # RegExp.test / String.match / String.search of a literal pattern.
            # For literal substrings use Z3 Contains; otherwise fresh bool.
            if method in {"test", "match", "search"}:
                target = self._to_string(receiver, state)
                raw_pattern = None
                regex_body = None
                if (
                    method == "test"
                    and args
                    and isinstance(func.obj, LiteralExpr)
                    and isinstance(func.obj.value, str)
                ):
                    regex_body = _regex_literal_body(func.obj.value)
                    if regex_body is not None:
                        raw_pattern = func.obj.value
                        target = self._to_string(args[0], state)
                if raw_pattern is None and args and expression.args:
                    first_raw = expression.args[0]
                    if isinstance(first_raw, LiteralExpr) and isinstance(first_raw.value, str):
                        raw_pattern = first_raw.value
                        regex_body = _regex_literal_body(raw_pattern)
                if raw_pattern is not None:
                    literal_pattern = regex_body if regex_body is not None else raw_pattern
                    if literal_pattern and all(c not in literal_pattern for c in _REGEX_META_CHARS):
                        return z3.Contains(target, z3.StringVal(literal_pattern))
                    if regex_body is not None:
                        compiled_regex = _compile_js_regex_to_z3(regex_body)
                        if compiled_regex is not None:
                            regex_constraint = z3.InRe(target, compiled_regex)
                            exclusions = _regex_exclusion_constraints(regex_body, target)
                            if exclusions:
                                return z3.And(regex_constraint, *exclusions)
                            return regex_constraint
                return self._fresh_bool(method or "regex")
            if method == "trim":
                result = self._fresh_string("trim")
                state.constraints.append(
                    z3.Length(result) <= z3.Length(self._to_string(receiver, state))
                )
                return result
            if method in {"toLowerCase", "toUpperCase"}:
                result = self._fresh_string(method)
                state.constraints.append(
                    z3.Length(result) == z3.Length(self._to_string(receiver, state))
                )
                return result
            if method == "concat":
                output = self._to_string(receiver, state)
                for item in args:
                    output = z3.Concat(output, self._to_string(item, state))
                return output
            if method == "push":
                return args[-1] if args else z3.StringVal("")
            called = self._get_function(method or "")
            if called is not None:
                return self._call_function(called, [receiver, *args], state)
        return self._fresh_string("call")

    def _call_function(
        self, function: FunctionDef, args: Sequence[SymValue], caller_state: SymbolicState
    ) -> SymValue:
        callee_state = SymbolicState(
            store=copy.deepcopy(caller_state.store),
            constraints=list(caller_state.constraints),
            trace=list(caller_state.trace),
            call_stack=list(caller_state.call_stack),
            path_depth=caller_state.path_depth,
        )
        callee_state.call_stack.append(function.name)
        for index, param in enumerate(function.params):
            if index < len(args):
                callee_state.store[param] = args[index]
        states = self._execute_block(function.body, callee_state, function=function)
        for final_state in states:
            if final_state.return_value is not None:
                caller_state.constraints[:] = list(final_state.constraints)
                caller_state.trace[:] = list(final_state.trace)
                return final_state.return_value
        return self._fresh_string(function.name)

    def _eval_math_call(
        self, method: str, args: Sequence[SymValue], state: SymbolicState
    ) -> SymValue:
        if not args:
            return z3.IntVal(0)
        first = self._to_number(args[0], state)
        if method == "floor":
            return z3.ToInt(first)
        if method == "ceil":
            as_int = z3.ToInt(first)
            return as_int + z3.If(first != as_int, z3.IntVal(1), z3.IntVal(0))
        second = self._to_number(args[1], state) if len(args) > 1 else first
        if method == "max":
            return z3.If(first >= second, first, second)
        return z3.If(first <= second, first, second)

    def _strict_eq(self, left: SymValue, right: SymValue, state: SymbolicState) -> z3.BoolRef:
        if isinstance(left, SymbolicObject) or isinstance(right, SymbolicObject):
            return z3.BoolVal(left is right)
        left_expr, right_expr = self._coerce_same_sort(left, right, state)
        return left_expr == right_expr

    def _loose_eq(self, left: SymValue, right: SymValue, state: SymbolicState) -> z3.BoolRef:
        if isinstance(left, SymbolicObject) or isinstance(right, SymbolicObject):
            return z3.BoolVal(False)
        left_sort = left.sort()
        right_sort = right.sort()
        if left_sort == right_sort:
            return left == right
        if left_sort == z3.StringSort() and right_sort == z3.IntSort():
            return z3.StrToInt(left) == right
        if left_sort == z3.IntSort() and right_sort == z3.StringSort():
            return left == z3.StrToInt(right)
        if left_sort == z3.BoolSort():
            return self._loose_eq(z3.If(left, z3.IntVal(1), z3.IntVal(0)), right, state)
        if right_sort == z3.BoolSort():
            return self._loose_eq(left, z3.If(right, z3.IntVal(1), z3.IntVal(0)), state)
        return z3.BoolVal(False)

    def _to_string(self, value: SymValue, state: SymbolicState) -> z3.ExprRef:
        if isinstance(value, SymbolicObject):
            if value.wildcard is not None and not isinstance(value.wildcard, SymbolicObject):
                return self._to_string(value.wildcard, state)
            return self._fresh_string(value.name)
        if value.sort() == z3.StringSort():
            return value
        if value.sort() == z3.IntSort():
            return z3.IntToStr(value)
        if value.sort() == z3.BoolSort():
            return z3.If(value, z3.StringVal("true"), z3.StringVal("false"))
        return z3.IntToStr(z3.ToInt(value))

    def _to_number(self, value: SymValue, state: SymbolicState) -> z3.ArithRef:
        if isinstance(value, SymbolicObject):
            return z3.StrToInt(self._to_string(value, state))
        if value.sort() == z3.IntSort():
            return value
        if value.sort() == z3.BoolSort():
            return z3.If(value, z3.IntVal(1), z3.IntVal(0))
        if value.sort() == z3.StringSort():
            return z3.StrToInt(value)
        return z3.ToInt(value)

    def _to_bool(self, value: SymValue, state: SymbolicState) -> z3.BoolRef:
        if isinstance(value, SymbolicObject):
            return z3.BoolVal(True)
        if value.sort() == z3.BoolSort():
            return value
        if value.sort() == z3.IntSort():
            return value != 0
        if value.sort() == z3.StringSort():
            return value != z3.StringVal("")
        return value != z3.RealVal(0)

    def _read_property(self, obj: SymbolicObject, key: str | None) -> SymValue:
        if key is not None and key in obj.properties:
            return obj.properties[key]
        if obj.wildcard is not None:
            return obj.wildcard
        if key is None:
            obj.wildcard = self._fresh_string(f"{obj.name}_prop")
            return obj.wildcard
        fresh = self._fresh_string(f"{obj.name}_{key}")
        obj.properties[key] = fresh
        return fresh

    def _ensure_object(self, value: SymValue, *, name_hint: str) -> SymbolicObject:
        if isinstance(value, SymbolicObject):
            return value
        obj = SymbolicObject(
            name=name_hint, wildcard=value if value.sort() == z3.StringSort() else None
        )
        return obj

    def _merge_states(
        self,
        true_state: SymbolicState,
        false_state: SymbolicState,
        condition: z3.BoolRef,
    ) -> SymbolicState:
        merged = SymbolicState(
            store={},
            constraints=_merge_constraints(true_state.constraints, false_state.constraints),
            trace=list(
                true_state.trace
                if len(true_state.trace) >= len(false_state.trace)
                else false_state.trace
            ),
            call_stack=list(true_state.call_stack),
            path_depth=max(true_state.path_depth, false_state.path_depth),
        )
        for name in sorted(set(true_state.store) | set(false_state.store)):
            if name not in true_state.store:
                merged.store[name] = false_state.store[name]
                continue
            if name not in false_state.store:
                merged.store[name] = true_state.store[name]
                continue
            merged.store[name] = self._merge_values(
                true_state.store[name], false_state.store[name], condition
            )
        merged.terminated = true_state.terminated and false_state.terminated
        if true_state.return_value is not None and false_state.return_value is not None:
            merged.return_value = self._merge_values(
                true_state.return_value, false_state.return_value, condition
            )
        return merged

    def _merge_values(self, left: SymValue, right: SymValue, condition: z3.BoolRef) -> SymValue:
        if isinstance(left, SymbolicObject) and isinstance(right, SymbolicObject):
            merged = SymbolicObject(name=f"{left.name}_{right.name}_merged")
            for key in sorted(set(left.properties) | set(right.properties)):
                if key not in left.properties:
                    merged.properties[key] = right.properties[key]
                    continue
                if key not in right.properties:
                    merged.properties[key] = left.properties[key]
                    continue
                merged.properties[key] = self._merge_values(
                    left.properties[key], right.properties[key], condition
                )
            if left.wildcard is not None and right.wildcard is not None:
                merged.wildcard = self._merge_values(left.wildcard, right.wildcard, condition)
            else:
                merged.wildcard = left.wildcard or right.wildcard
            return merged
        if isinstance(left, SymbolicObject):
            return left
        if isinstance(right, SymbolicObject):
            return right
        left_expr, right_expr = self._coerce_same_sort(left, right, SymbolicState())
        return z3.If(condition, left_expr, right_expr)

    def _coerce_same_sort(
        self,
        left: SymValue,
        right: SymValue,
        state: SymbolicState,
    ) -> tuple[z3.ExprRef, z3.ExprRef]:
        if isinstance(left, SymbolicObject):
            return self._to_string(left, state), self._to_string(right, state)
        if isinstance(right, SymbolicObject):
            return self._to_string(left, state), self._to_string(right, state)
        if left.sort() == right.sort():
            return left, right
        if left.sort() == z3.StringSort() or right.sort() == z3.StringSort():
            return self._to_string(left, state), self._to_string(right, state)
        return self._to_number(left, state), self._to_number(right, state)

    def _depends_on_input(self, expr: z3.ExprRef) -> bool:
        if expr.num_args() == 0:
            return expr.decl().name() in {
                variable.decl().name() for variable in self._input_vars.values()
            }
        return any(self._depends_on_input(child) for child in expr.children())

    def _prioritize_branches(
        self,
        branches: Sequence[tuple[Sequence[Statement], SymbolicState, z3.BoolRef]],
        *,
        sink_hint: str,
    ) -> list[tuple[Sequence[Statement], SymbolicState, z3.BoolRef]]:
        tokens = [sink_hint, self.inp.finding.sink.api_name]
        tokens.extend(
            step.location.snippet for step in self.inp.taint_path if step.location.snippet
        )

        def score(branch: tuple[Sequence[Statement], SymbolicState, z3.BoolRef]) -> int:
            text = " ".join(statement.text for statement in branch[0])
            return sum(1 for token in tokens if token and token in text)

        return sorted(branches, key=score, reverse=True)

    def _is_feasible(self, constraints: Sequence[z3.BoolRef]) -> bool:
        solver = z3.Solver()
        solver.set("timeout", 250)
        for constraint in constraints:
            solver.add(constraint)
        return bool(solver.check() != z3.unsat)

    def _havoc_targets(self, state: SymbolicState, targets: set[str]) -> None:
        for target in targets:
            if not target:
                continue
            existing = state.store.get(target)
            if isinstance(existing, SymbolicObject):
                state.store[target] = SymbolicObject(
                    name=f"havoc_{target}",
                    wildcard=self._fresh_string(f"havoc_{target}"),
                )
                continue
            if isinstance(existing, z3.ExprRef) and existing.sort() == z3.BoolSort():
                state.store[target] = z3.Bool(f"havoc_{target}_{self._next_symbol_id()}")
                continue
            if isinstance(existing, z3.ExprRef) and existing.sort() == z3.IntSort():
                state.store[target] = z3.Int(f"havoc_{target}_{self._next_symbol_id()}")
                continue
            state.store[target] = self._fresh_string(f"havoc_{target}")

    def _append_trace(
        self,
        state: SymbolicState,
        function: FunctionDef,
        statement: Statement,
        *,
        constraint: str | None = None,
    ) -> None:
        state.trace.append(
            TraceStep(
                location=SourceLocation(
                    file=function.source_name,
                    line=statement.line,
                    column=1,
                    snippet=statement.text,
                ),
                statement_text=statement.text,
                symbolic_state_snapshot=self._snapshot_state(state),
                constraint_added=constraint,
            )
        )

    def _snapshot_state(self, state: SymbolicState) -> dict[str, str]:
        return {name: _symvalue_to_string(value) for name, value in sorted(state.store.items())}

    def _fresh_string(self, prefix: str) -> z3.ExprRef:
        return z3.String(f"{prefix}_{self._next_symbol_id()}")

    def _fresh_bool(self, prefix: str) -> z3.ExprRef:
        return z3.Bool(f"{prefix}_{self._next_symbol_id()}")

    def _next_symbol_id(self) -> int:
        self._symbol_counter += 1
        return self._symbol_counter

    def _check_timeout(self) -> None:
        if time.monotonic() > self.deadline:
            raise TimeoutError


def concolic_verify(
    inp: ConcolicInput,
    *,
    template: ExploitTemplate | None = None,
    max_paths: int = 100,
    timeout_ms: int = 120_000,
    loop_bound: int = 3,
) -> ConcolicResult:
    engine = _ConcolicEngine(
        inp,
        template=template,
        max_paths=max_paths,
        timeout_ms=timeout_ms,
        loop_bound=loop_bound,
    )
    return engine.run()


def build_concolic_input(
    finding: CandidateFinding,
    *,
    function_asts: Mapping[str, Any] | None = None,
    call_graph: Mapping[str, Sequence[str]] | None = None,
    entry_point: EntryPoint | None = None,
) -> ConcolicInput | None:
    resolved_entry = entry_point or _default_entry_point(finding)
    if function_asts is not None:
        asts = dict(function_asts)
    else:
        asts = {}
        seen: set[tuple[str, str]] = set()
        locations = [finding.source.location, finding.sink.location]
        locations.extend(step.location for step in finding.taint_path)
        for location, function_name in zip(
            locations,
            [
                finding.taint_path[0].through_function
                if finding.taint_path
                else resolved_entry.function_id,
                finding.taint_path[-1].through_function
                if finding.taint_path
                else resolved_entry.function_id,
                *[step.through_function for step in finding.taint_path],
            ],
            strict=False,
        ):
            path = Path(location.file)
            if not path.exists():
                continue
            key = function_name or resolved_entry.function_id or str(path)
            cache_key = (key, str(path))
            if cache_key in seen:
                continue
            asts[key] = path.read_text(encoding="utf-8")
            seen.add(cache_key)
        if not asts:
            source_path = Path(finding.source.location.file)
            if source_path.exists():
                asts[resolved_entry.function_id] = source_path.read_text(encoding="utf-8")
            else:
                return None
    return ConcolicInput(
        finding=finding,
        taint_path=list(finding.taint_path),
        function_asts=asts,
        call_graph={name: list(values) for name, values in (call_graph or {}).items()},
        entry_point=resolved_entry,
    )


def _default_entry_point(finding: CandidateFinding) -> EntryPoint:
    source_function = next(
        (step.through_function for step in finding.taint_path if step.through_function is not None),
        None,
    )
    function_id = source_function or "entry"
    param_root = (
        finding.source.source_type.split(".", 1)[0] if "." in finding.source.source_type else "req"
    )
    parameters = [param_root]
    return EntryPoint(
        function_id=function_id,
        location=finding.source.location,
        kind="route_handler",
        http_method=None,
        route_pattern=None,
        parameters=parameters,
    )


def _extract_source_text(raw_source: Any) -> str:
    if isinstance(raw_source, str):
        return raw_source
    text = getattr(raw_source, "text", None)
    if isinstance(text, bytes):
        return text.decode("utf-8", errors="replace")
    if isinstance(text, str):
        return text
    return str(raw_source)


def _strip_comments(source: str) -> str:
    without_blocks = _COMMENT_BLOCK_RE.sub("", source)
    return _COMMENT_LINE_RE.sub("", without_blocks)


def _parse_block(text: str, *, base_line: int) -> list[Statement]:
    statements: list[Statement] = []
    index = 0
    while index < len(text):
        index = _skip_ws(text, index)
        if index >= len(text):
            break
        line = base_line + text[:index].count("\n")
        if text.startswith("if", index) and _word_boundary(text, index + 2):
            start = index
            condition, index = _extract_parenthesized(text, _skip_ws(text, index + 2))
            then_body, index, _then_raw = _extract_statement_or_block(
                text, index, base_line=base_line
            )
            index = _skip_ws(text, index)
            else_body: list[Statement] = []
            raw = text[start:index]
            if text.startswith("else", index) and _word_boundary(text, index + 4):
                index = _skip_ws(text, index + 4)
                else_body, index, else_raw = _extract_statement_or_block(
                    text, index, base_line=base_line
                )
                raw += " else " + else_raw
            statements.append(
                IfStmt(
                    line=line,
                    text=" ".join(raw.split()),
                    condition_text=condition,
                    then_body=tuple(then_body),
                    else_body=tuple(else_body),
                )
            )
            continue
        if text.startswith("while", index) and _word_boundary(text, index + 5):
            start = index
            condition, index = _extract_parenthesized(text, _skip_ws(text, index + 5))
            body, index, _raw_body = _extract_statement_or_block(text, index, base_line=base_line)
            raw = text[start:index]
            statements.append(
                WhileStmt(
                    line=line,
                    text=" ".join(raw.split()),
                    condition_text=condition,
                    body=tuple(body),
                )
            )
            continue
        if text.startswith("for", index) and _word_boundary(text, index + 3):
            start = index
            header, index = _extract_parenthesized(text, _skip_ws(text, index + 3))
            parts = _split_top_level(header, ";")
            init = parts[0].strip() if parts and parts[0].strip() else None
            for_condition = parts[1].strip() if len(parts) > 1 and parts[1].strip() else None
            update = parts[2].strip() if len(parts) > 2 and parts[2].strip() else None
            body, index, _raw_body = _extract_statement_or_block(text, index, base_line=base_line)
            raw = text[start:index]
            statements.append(
                ForStmt(
                    line=line,
                    text=" ".join(raw.split()),
                    init_text=init,
                    condition_text=for_condition,
                    update_text=update,
                    body=tuple(body),
                )
            )
            continue
        if text.startswith("return", index) and _word_boundary(text, index + 6):
            end = _find_statement_end(text, index)
            raw = text[index:end].strip().rstrip(";")
            expr = raw[6:].strip() or None
            statements.append(ReturnStmt(line=line, text=raw, expr_text=expr))
            index = end + 1
            continue
        end = _find_statement_end(text, index)
        raw = text[index:end].strip().rstrip(";")
        if raw:
            statements.append(_parse_simple_statement(raw, line=line))
        index = end + 1
    return statements


def _parse_simple_statement(text: str, *, line: int) -> Statement:
    if text.startswith(("const ", "let ", "var ")):
        remainder = text.split(None, 1)[1].strip()
        target, expr = _split_assignment(remainder)
        return VarDecl(line=line, text=text, target_text=target, expr_text=expr)
    compound = _COMPOUND_ASSIGNMENT_RE.match(text)
    if compound is not None:
        op = compound.group("op")[0]
        target = compound.group("target").strip()
        value = compound.group("value").strip()
        return AssignStmt(
            line=line, text=text, target_text=target, expr_text=f"{target} {op} ({value})"
        )
    if text.endswith("++"):
        target = text[:-2].strip()
        return AssignStmt(line=line, text=text, target_text=target, expr_text=f"{target} + 1")
    if text.endswith("--"):
        target = text[:-2].strip()
        return AssignStmt(line=line, text=text, target_text=target, expr_text=f"{target} - 1")
    target, expr = _split_assignment(text)
    if expr is not None:
        return AssignStmt(line=line, text=text, target_text=target, expr_text=expr)
    return ExprStmt(line=line, text=text, expr_text=text)


def _split_assignment(text: str) -> tuple[str, str | None]:
    depth = 0
    string_delim: str | None = None
    index = 0
    while index < len(text):
        char = text[index]
        if string_delim is not None:
            if char == "\\":
                index += 2
                continue
            if char == string_delim:
                string_delim = None
            index += 1
            continue
        if char in {"'", '"', "`"}:
            string_delim = char
            index += 1
            continue
        if char in "([{":
            depth += 1
            index += 1
            continue
        if char in ")]}":
            depth -= 1
            index += 1
            continue
        if depth == 0 and char == "=":
            previous = text[index - 1] if index else ""
            following = text[index + 1] if index + 1 < len(text) else ""
            if previous in {"=", "!", "<", ">"} or following == "=":
                index += 1
                continue
            return text[:index].strip(), text[index + 1 :].strip()
        index += 1
    return text.strip(), None


def _extract_statement_or_block(
    text: str,
    index: int,
    *,
    base_line: int,
) -> tuple[list[Statement], int, str]:
    index = _skip_ws(text, index)
    if text[index] == "{":
        end = _find_matching_delimiter(text, index, "{", "}")
        body = text[index + 1 : end]
        return (
            _parse_block(body, base_line=base_line + text[: index + 1].count("\n")),
            end + 1,
            text[index : end + 1],
        )
    end = _find_statement_end(text, index)
    raw = text[index:end].strip().rstrip(";")
    line = base_line + text[:index].count("\n")
    return [_parse_simple_statement(raw, line=line)], end + 1, raw


def _collect_modified_targets(statements: Sequence[Statement]) -> set[str]:
    targets: set[str] = set()
    for statement in statements:
        if isinstance(statement, (VarDecl, AssignStmt)) and not _is_destructuring(
            statement.target_text
        ):
            base = _target_base_name(statement.target_text)
            if base is not None:
                targets.add(base)
        elif isinstance(statement, IfStmt):
            targets.update(_collect_modified_targets(statement.then_body))
            targets.update(_collect_modified_targets(statement.else_body))
        elif isinstance(statement, (ForStmt, WhileStmt)):
            targets.update(_collect_modified_targets(statement.body))
    return targets


def _collect_inline_targets(text: str) -> set[str]:
    statement = _parse_simple_statement(text, line=0)
    if isinstance(statement, (VarDecl, AssignStmt)):
        base = _target_base_name(statement.target_text)
        return {base} if base is not None else set()
    return set()


def _find_statement_end(text: str, start: int) -> int:
    depth = 0
    string_delim: str | None = None
    index = start
    while index < len(text):
        char = text[index]
        if string_delim is not None:
            if char == "\\":
                index += 2
                continue
            if char == string_delim:
                string_delim = None
            index += 1
            continue
        if char in {"'", '"', "`"}:
            string_delim = char
            index += 1
            continue
        if char in "([{":
            depth += 1
            index += 1
            continue
        if char in ")]}":
            if depth == 0 and char == "}":
                return index - 1
            depth -= 1
            index += 1
            continue
        if depth == 0 and char == ";":
            return index
        index += 1
    return len(text)


def _extract_parenthesized(text: str, index: int) -> tuple[str, int]:
    index = _skip_ws(text, index)
    if text[index] != "(":
        raise ValueError("expected '('")
    end = _find_matching_delimiter(text, index, "(", ")")
    return text[index + 1 : end].strip(), end + 1


def _find_matching_delimiter(text: str, start: int, opening: str, closing: str) -> int:
    depth = 0
    string_delim: str | None = None
    index = start
    while index < len(text):
        char = text[index]
        if string_delim is not None:
            if char == "\\":
                index += 2
                continue
            if char == string_delim:
                string_delim = None
            index += 1
            continue
        if char in {"'", '"', "`"}:
            string_delim = char
            index += 1
            continue
        if char == opening:
            depth += 1
        elif char == closing:
            depth -= 1
            if depth == 0:
                return index
        index += 1
    raise ValueError(f"unbalanced {opening}{closing}")


def _skip_ws(text: str, index: int) -> int:
    while index < len(text) and text[index].isspace():
        index += 1
    return index


def _word_boundary(text: str, index: int) -> bool:
    return index >= len(text) or not (text[index].isalnum() or text[index] == "_")


def _parse_destructuring_pattern(pattern: str) -> list[tuple[str, str]]:
    inner = pattern.strip()
    if inner.startswith("{") and inner.endswith("}"):
        inner = inner[1:-1]
    bindings: list[tuple[str, str]] = []
    for part in _split_top_level(inner, ","):
        piece = part.strip()
        if not piece or piece.startswith("..."):
            continue
        if ":" in piece:
            key, alias = piece.split(":", 1)
            bindings.append((key.strip(), alias.strip()))
        else:
            bindings.append((piece, piece))
    return bindings


def _split_top_level(text: str, delimiter: str) -> list[str]:
    parts: list[str] = []
    depth = 0
    string_delim: str | None = None
    start = 0
    index = 0
    while index < len(text):
        char = text[index]
        if string_delim is not None:
            if char == "\\":
                index += 2
                continue
            if char == string_delim:
                string_delim = None
            index += 1
            continue
        if char in {"'", '"', "`"}:
            string_delim = char
            index += 1
            continue
        if char in "([{":
            depth += 1
            index += 1
            continue
        if char in ")]}":
            depth -= 1
            index += 1
            continue
        if depth == 0 and text.startswith(delimiter, index):
            parts.append(text[start:index])
            index += len(delimiter)
            start = index
            continue
        index += 1
    parts.append(text[start:])
    return parts


def _tokenize(text: str) -> list[_Token]:
    tokens: list[_Token] = []
    index = 0
    while index < len(text):
        char = text[index]
        if char.isspace():
            index += 1
            continue
        if text.startswith("...", index):
            tokens.append(_Token(kind="PUNC", value="..."))
            index += 3
            continue
        for operator in (
            "!==",
            "===",
            "&&",
            "||",
            "<=",
            ">=",
            "==",
            "!=",
        ):
            if text.startswith(operator, index):
                tokens.append(_Token(kind="OP", value=operator))
                index += len(operator)
                break
        else:
            if char == "/" and _can_start_regex_literal(tokens):
                value, index = _read_regex_literal(text, index)
                tokens.append(_Token(kind="REGEX", value=value))
                continue
            if char in "(){}[],:?.+-*/%!<>":
                tokens.append(_Token(kind="PUNC" if char in "(){}[],:?." else "OP", value=char))
                index += 1
                continue
            if char in {"'", '"'}:
                value, index = _read_quoted_string(text, index)
                tokens.append(_Token(kind="STRING", value=value))
                continue
            if char == "`":
                value, index = _read_template_string(text, index)
                tokens.append(_Token(kind="TEMPLATE", value=value))
                continue
            if char.isdigit():
                start = index
                while index < len(text) and text[index].isdigit():
                    index += 1
                tokens.append(_Token(kind="NUMBER", value=text[start:index]))
                continue
            identifier = _IDENTIFIER_RE.match(text, index)
            if identifier is not None:
                tokens.append(_Token(kind="IDENT", value=identifier.group(0)))
                index = identifier.end()
                continue
            raise ValueError(f"unsupported token near {text[index : index + 20]!r}")
        continue
    tokens.append(_Token(kind="EOF", value=""))
    return tokens


def _can_start_regex_literal(tokens: Sequence[_Token]) -> bool:
    if not tokens:
        return True
    previous = tokens[-1]
    return previous.kind == "OP" or previous.value in {"(", "[", "{", ",", ":", "?", "!"}


def _read_regex_literal(text: str, index: int) -> tuple[str, int]:
    start = index
    index += 1
    in_class = False
    while index < len(text):
        char = text[index]
        if char == "\\":
            index += 2
            continue
        if char == "[":
            in_class = True
            index += 1
            continue
        if char == "]":
            in_class = False
            index += 1
            continue
        if char == "/" and not in_class:
            index += 1
            while index < len(text) and text[index].isalpha():
                index += 1
            return text[start:index], index
        index += 1
    raise ValueError(f"unterminated regex literal near {text[start : start + 20]!r}")


def _read_quoted_string(text: str, index: int) -> tuple[str, int]:
    quote = text[index]
    index += 1
    parts: list[str] = []
    while index < len(text):
        char = text[index]
        if char == "\\" and index + 1 < len(text):
            parts.append(text[index + 1])
            index += 2
            continue
        if char == quote:
            return "".join(parts), index + 1
        parts.append(char)
        index += 1
    raise ValueError("unterminated string literal")


def _read_template_string(text: str, index: int) -> tuple[str, int]:
    index += 1
    parts: list[str] = []
    depth = 0
    while index < len(text):
        char = text[index]
        if char == "\\" and index + 1 < len(text):
            parts.append(text[index : index + 2])
            index += 2
            continue
        if char == "`" and depth == 0:
            return "".join(parts), index + 1
        if text.startswith("${", index):
            depth += 1
            parts.append("${")
            index += 2
            continue
        if char == "}" and depth > 0:
            depth -= 1
        parts.append(char)
        index += 1
    raise ValueError("unterminated template literal")


def _parse_template_literal(raw: str) -> tuple[str | ExprNode, ...]:
    parts: list[str | ExprNode] = []
    buffer: list[str] = []
    index = 0
    while index < len(raw):
        if raw.startswith("${", index):
            if buffer:
                parts.append("".join(buffer))
                buffer = []
            end = _find_template_expr_end(raw, index + 2)
            parts.append(_ExpressionParser(raw[index + 2 : end]).parse())
            index = end + 1
            continue
        buffer.append(raw[index])
        index += 1
    if buffer:
        parts.append("".join(buffer))
    return tuple(parts)


def _find_template_expr_end(text: str, start: int) -> int:
    depth = 1
    index = start
    string_delim: str | None = None
    while index < len(text):
        char = text[index]
        if string_delim is not None:
            if char == "\\":
                index += 2
                continue
            if char == string_delim:
                string_delim = None
            index += 1
            continue
        if char in {"'", '"', "`"}:
            string_delim = char
            index += 1
            continue
        if text.startswith("${", index):
            depth += 1
            index += 2
            continue
        if char == "}":
            depth -= 1
            if depth == 0:
                return index
        index += 1
    raise ValueError("unterminated template expression")


def _path_from_rest(rest: str) -> tuple[str, ...]:
    if not rest:
        return ()
    segments: list[str] = []
    for match in re.finditer(r"\.([A-Za-z_$][\w$]*)|\[['\"]([^'\"]+)['\"]\]", rest):
        segments.append(match.group(1) or match.group(2))
    return tuple(segments)


def _source_expression(text: str) -> dict[str, str] | None:
    match = _SOURCE_EXPR_RE.search(text)
    return match.groupdict() if match is not None else None


def _set_nested_property(
    obj: SymbolicObject,
    path: Sequence[str],
    value: SymValue,
    prefix: str,
) -> None:
    if not path:
        obj.wildcard = value
        return
    current = obj
    current_prefix = prefix
    for segment in path[:-1]:
        child = current.properties.get(segment)
        if not isinstance(child, SymbolicObject):
            child = SymbolicObject(name=f"{current_prefix}.{segment}")
            current.properties[segment] = child
        current = child
        current_prefix = child.name
    current.properties[path[-1]] = value


def _member_base_name(member: MemberExpr) -> str:
    if isinstance(member.obj, NameExpr):
        return member.obj.name
    return "member"


def _short_function_name(function_id: str) -> str:
    return function_id.rsplit(":", 1)[-1] if ":" in function_id else function_id


def _is_destructuring(text: str) -> bool:
    stripped = text.strip()
    return stripped.startswith("{") and stripped.endswith("}")


def _target_base_name(text: str) -> str | None:
    stripped = text.strip()
    if not stripped:
        return None
    try:
        expression = _ExpressionParser(stripped).parse()
    except ValueError:
        return stripped
    if isinstance(expression, NameExpr):
        return expression.name
    if isinstance(expression, MemberExpr) and isinstance(expression.obj, NameExpr):
        return expression.obj.name
    return None


def _concrete_string(value: SymValue) -> str | None:
    if isinstance(value, SymbolicObject):
        return None
    if z3.is_string_value(value):
        return str(value.as_string())
    return None


def _merge_constraints(left: Sequence[z3.BoolRef], right: Sequence[z3.BoolRef]) -> list[z3.BoolRef]:
    prefix_length = 0
    while (
        prefix_length < len(left)
        and prefix_length < len(right)
        and str(left[prefix_length]) == str(right[prefix_length])
    ):
        prefix_length += 1
    shared = list(left[:prefix_length])
    left_suffix = left[prefix_length:]
    right_suffix = right[prefix_length:]
    if not left_suffix and not right_suffix:
        return shared
    left_expr = z3.And(*left_suffix) if left_suffix else z3.BoolVal(True)
    right_expr = z3.And(*right_suffix) if right_suffix else z3.BoolVal(True)
    shared.append(z3.Or(left_expr, right_expr))
    return shared


def _is_string_like(value: SymValue) -> bool:
    return isinstance(value, SymbolicObject) or (
        isinstance(value, z3.ExprRef) and value.sort() == z3.StringSort()
    )


def _symvalue_to_string(value: SymValue) -> str:
    if isinstance(value, SymbolicObject):
        pieces = {key: _symvalue_to_string(item) for key, item in sorted(value.properties.items())}
        if value.wildcard is not None:
            pieces["*"] = _symvalue_to_string(value.wildcard)
        return f"{value.name}{pieces}"
    return str(value)


__all__ = [
    "ConcolicInput",
    "ConcolicResult",
    "TraceStep",
    "build_concolic_input",
    "concolic_verify",
]
