from __future__ import annotations

from collections.abc import Callable, Mapping, Sequence
from dataclasses import dataclass, field
from typing import TYPE_CHECKING, Literal
from urllib.parse import quote

import z3  # type: ignore[import-untyped]

from piranesi.models import CandidateFinding
from piranesi.verify.constraints import (
    ExploitTemplate,
    IntBound,
    LogicalAnd,
    LogicalOr,
    PayloadSlot,
    StringContains,
    StringEq,
    StringLength,
    TypeCheck,
    VerifierConstraint,
)
from piranesi.verify.sandbox import SynthesizedPayload

if TYPE_CHECKING:
    from piranesi.verify.concolic import ConcolicInput, ConcolicResult

DEFAULT_TIMEOUT_MS = 30_000
SolveStatus = Literal["SAT", "UNVERIFIABLE"]
SolverFactory = Callable[[], z3.Solver]
FORBIDDEN_PAYLOAD_PATTERNS = (
    "drop ",
    "delete ",
    "update ",
    "insert ",
    "truncate ",
    "rm ",
    "rm -",
    "dd ",
    "mkfs",
    "kill ",
    "shutdown",
    "chmod ",
    "chown ",
)
_SQLI_SAFE_PAYLOADS = (
    "' OR 1=1--",
    "' UNION SELECT NULL--",
    "' UNION SELECT NULL,NULL--",
    "'; SELECT pg_sleep(5)--",
    "'; SELECT SLEEP(5)--",
)
_XSS_SAFE_PAYLOADS = (
    "<script>alert(1)</script>",
    '"><img src=x onerror=alert(1)>',
    "'><svg/onload=alert(1)>",
)
_CMDI_SAFE_PAYLOADS = (
    "; id",
    "| cat /etc/passwd",
    "$(whoami)",
    "`id`",
)
_PATH_TRAVERSAL_SAFE_PAYLOADS = (
    "../../../etc/passwd",
    "....//....//....//etc/passwd",
    "..%2f..%2f..%2fetc/passwd",
)
_SSRF_SAFE_PAYLOADS = (
    "http://127.0.0.1:80/",
    "http://localhost/",
    "http://[::1]/",
)
_OPEN_REDIRECT_SAFE_PAYLOADS = (
    "https://example.com/piranesi-probe",
    "//example.com/piranesi-probe",
    "///example.com/piranesi-probe",
)
_INSECURE_DESERIALIZATION_SAFE_PAYLOADS = (
    '{"piranesi_probe":"deserialize"}',
    "rO0ABXQADnBpcmFuZXNpLXByb2Jl",
    "!!python/object/apply:builtins.str ['piranesi-probe']",
)
_WEAK_CRYPTO_SAFE_PAYLOADS = (
    "md5",
    "sha1",
    "des",
    "rc4",
    "tls1.0",
)


@dataclass(slots=True)
class PayloadSolution:
    slot: PayloadSlot
    constraint_set_index: int
    payload: SynthesizedPayload
    model_values: dict[str, str]


@dataclass(slots=True)
class SolveAttempt:
    status: SolveStatus
    reason: str | None = None
    solution: PayloadSolution | None = None


@dataclass(slots=True)
class SolverResult:
    status: SolveStatus
    reason: str | None = None
    solutions: tuple[PayloadSolution, ...] = field(default_factory=tuple)
    concolic_result: ConcolicResult | None = None


class ConstraintTypeConflict(ValueError):
    """Raised when path conditions require incompatible Z3 sorts."""


class _VariableRegistry:
    def __init__(self) -> None:
        self.variables: dict[str, z3.ExprRef] = {}
        self.sorts: dict[str, str] = {}

    def bind(self, name: str, expected_sort: str | None = None) -> z3.ExprRef:
        existing = self.variables.get(name)
        existing_sort = self.sorts.get(name)
        resolved_sort = expected_sort or existing_sort or "string"
        if existing is not None:
            if (
                expected_sort is not None
                and existing_sort is not None
                and existing_sort != expected_sort
            ):
                raise ConstraintTypeConflict(
                    f"variable {name!r} cannot be both {existing_sort!r} and {expected_sort!r}"
                )
            return existing

        expression = _declare_variable(name, resolved_sort)
        self.variables[name] = expression
        self.sorts[name] = resolved_sort
        return expression


def solve_exploit_template(
    template: ExploitTemplate,
    *,
    timeout_ms: int = DEFAULT_TIMEOUT_MS,
    allow_unsafe_payloads: bool = False,
    solver_factory: SolverFactory | None = None,
    concolic_input: ConcolicInput | None = None,
    finding: CandidateFinding | None = None,
) -> SolverResult:
    resolved_timeout_ms = template.timeout_ms if timeout_ms == DEFAULT_TIMEOUT_MS else timeout_ms
    if template.unsat_reason is not None:
        return SolverResult(status="UNVERIFIABLE", reason=template.unsat_reason)

    solutions: list[PayloadSolution] = []
    last_reason = "CONSTRAINTS_UNSATISFIABLE"
    constraint_sets = template.constraint_sets or ((),)
    for slot in template.payload_slots:
        for index, constraint_set in enumerate(constraint_sets):
            attempt = solve_constraint_set(
                template,
                constraint_set,
                slot=slot,
                constraint_set_index=index,
                timeout_ms=resolved_timeout_ms,
                allow_unsafe_payloads=allow_unsafe_payloads,
                solver_factory=solver_factory,
            )
            if attempt.status == "SAT" and attempt.solution is not None:
                solutions.append(attempt.solution)
                break
            if attempt.reason is not None:
                last_reason = attempt.reason

    if solutions:
        return SolverResult(status="SAT", solutions=tuple(solutions))

    resolved_concolic_input = concolic_input
    if resolved_concolic_input is None and finding is not None:
        from piranesi.verify.concolic import build_concolic_input

        resolved_concolic_input = build_concolic_input(finding)

    if resolved_concolic_input is not None:
        from piranesi.verify.concolic import concolic_verify

        concolic_result = concolic_verify(
            resolved_concolic_input,
            template=template,
            timeout_ms=resolved_timeout_ms,
        )
        if concolic_result.status == "SAT" and concolic_result.payload is not None:
            if not allow_unsafe_payloads and any(
                not is_safe_payload(payload_value)
                for payload_value in concolic_result.payload.payload_values.values()
            ):
                return SolverResult(
                    status="UNVERIFIABLE",
                    reason="UNSAFE_PAYLOAD_REJECTED",
                    concolic_result=concolic_result,
                )
            if not template.payload_slots:
                return SolverResult(
                    status="UNVERIFIABLE",
                    reason="MISSING_PAYLOAD_SLOT",
                    concolic_result=concolic_result,
                )
            slot = template.payload_slots[0]
            model_values = (
                dict(concolic_result.model_values)
                if concolic_result.model_values is not None
                else dict(concolic_result.payload.payload_values)
            )
            return SolverResult(
                status="SAT",
                solutions=(
                    PayloadSolution(
                        slot=slot,
                        constraint_set_index=0,
                        payload=concolic_result.payload,
                        model_values=model_values,
                    ),
                ),
                concolic_result=concolic_result,
            )
        return SolverResult(
            status="UNVERIFIABLE",
            reason=_reason_from_concolic(concolic_result),
            concolic_result=concolic_result,
        )

    return SolverResult(status="UNVERIFIABLE", reason=last_reason)


def solve_constraint_set(
    template: ExploitTemplate,
    constraint_set: Sequence[VerifierConstraint],
    *,
    slot: PayloadSlot,
    constraint_set_index: int = 0,
    timeout_ms: int = DEFAULT_TIMEOUT_MS,
    allow_unsafe_payloads: bool = False,
    solver_factory: SolverFactory | None = None,
) -> SolveAttempt:
    last_reason = "CONSTRAINTS_UNSATISFIABLE"
    for forced_payload in _template_payload_candidates(template):
        try:
            solver, variables = build_z3_query(
                template,
                constraint_set,
                slot=slot,
                timeout_ms=timeout_ms,
                solver_factory=solver_factory,
                forced_payload=forced_payload,
            )
        except ConstraintTypeConflict:
            return SolveAttempt(status="UNVERIFIABLE", reason="CONSTRAINTS_UNSATISFIABLE")

        outcome = solver.check()
        if outcome == z3.sat:
            model_values = extract_model_values(solver.model(), variables)
            if forced_payload is not None:
                model_values[slot.name] = forced_payload
            if not allow_unsafe_payloads and not is_safe_payload(model_values[slot.name]):
                last_reason = "UNSAFE_PAYLOAD_REJECTED"
                continue
            payload = synthesize_payload(template, slot=slot, model_values=model_values)
            return SolveAttempt(
                status="SAT",
                solution=PayloadSolution(
                    slot=slot,
                    constraint_set_index=constraint_set_index,
                    payload=payload,
                    model_values=model_values,
                ),
            )
        if outcome == z3.unsat:
            continue

        unknown_reason = ""
        if hasattr(solver, "reason_unknown"):
            unknown_reason = solver.reason_unknown() or ""
        normalized_reason = unknown_reason.lower()
        if "timeout" in normalized_reason or "canceled" in normalized_reason:
            last_reason = "SOLVER_TIMEOUT"
        else:
            last_reason = "SOLVER_UNKNOWN"

    return SolveAttempt(status="UNVERIFIABLE", reason=last_reason)


def _reason_from_concolic(result: ConcolicResult) -> str:
    if result.status == "UNSAT":
        return "path constraints unsatisfiable at sink"
    if result.status == "TIMEOUT":
        return "CONCOLIC_TIMEOUT"
    return result.infeasible_reason or "CONCOLIC_UNVERIFIABLE"


def build_z3_query(
    template: ExploitTemplate,
    constraint_set: Sequence[VerifierConstraint],
    *,
    slot: PayloadSlot,
    timeout_ms: int = DEFAULT_TIMEOUT_MS,
    solver_factory: SolverFactory | None = None,
    forced_payload: str | None = None,
) -> tuple[z3.Solver, dict[str, z3.ExprRef]]:
    solver = (solver_factory or z3.Solver)()
    solver.set("timeout", timeout_ms)

    registry = _VariableRegistry()
    for condition in constraint_set:
        solver.add(translate_condition(condition, registry))

    payload_var = registry.bind(slot.name, "string")
    if forced_payload is not None:
        solver.add(payload_var == z3.StringVal(forced_payload))
    else:
        for assertion in vulnerability_constraints(template.vuln_class, payload_var):
            solver.add(assertion)
    return solver, dict(registry.variables)


def translate_condition(
    condition: VerifierConstraint,
    registry: _VariableRegistry,
) -> z3.BoolRef:
    if isinstance(condition, StringEq):
        variable = registry.bind(condition.var, "string")
        return variable == z3.StringVal(condition.val)
    if isinstance(condition, StringContains):
        variable = registry.bind(condition.var, "string")
        return z3.Contains(variable, z3.StringVal(condition.substr))
    if isinstance(condition, StringLength):
        variable = registry.bind(condition.var, "string")
        return _compare_expr(z3.Length(variable), condition.op, condition.n)
    if isinstance(condition, IntBound):
        variable = registry.bind(condition.var, "int")
        return _compare_expr(variable, condition.op, condition.n)
    if isinstance(condition, TypeCheck):
        registry.bind(condition.var, condition.type_name)
        return z3.BoolVal(True)
    if isinstance(condition, LogicalAnd):
        return z3.And(*(translate_condition(child, registry) for child in condition.children))
    if isinstance(condition, LogicalOr):
        return z3.Or(*(translate_condition(child, registry) for child in condition.children))
    return z3.Not(translate_condition(condition.child, registry))


def vulnerability_constraints(vuln_class: str, payload_var: z3.ExprRef) -> tuple[z3.BoolRef, ...]:
    normalized = vuln_class.upper()
    if "CWE-89" in normalized or "SQL" in normalized:
        return (z3.Contains(payload_var, z3.StringVal("'")),)
    if "CWE-79" in normalized or "XSS" in normalized:
        return (z3.Contains(payload_var, z3.StringVal("<script>")),)
    if "CWE-78" in normalized or "CMD" in normalized or "COMMAND" in normalized:
        return (
            z3.Or(
                z3.Contains(payload_var, z3.StringVal(";")),
                z3.Contains(payload_var, z3.StringVal("|")),
            ),
        )
    if "CWE-22" in normalized or "TRAVERS" in normalized:
        return (z3.Contains(payload_var, z3.StringVal("../")),)
    if "CWE-918" in normalized or "SSRF" in normalized:
        return (z3.Contains(payload_var, z3.StringVal("http://")),)
    if "CWE-601" in normalized or "REDIRECT" in normalized:
        return (
            z3.Or(
                z3.Contains(payload_var, z3.StringVal("://")),
                z3.Contains(payload_var, z3.StringVal("//")),
            ),
        )
    if "CWE-502" in normalized or "DESERIAL" in normalized:
        return (
            z3.Or(
                z3.Contains(payload_var, z3.StringVal("{")),
                z3.Contains(payload_var, z3.StringVal("!!")),
                z3.Contains(payload_var, z3.StringVal("rO0A")),
            ),
        )
    if (
        "CWE-327" in normalized
        or "CWE-326" in normalized
        or "WEAK_CRYPTO" in normalized
        or "CIPHER" in normalized
    ):
        return (
            z3.Or(
                z3.Contains(payload_var, z3.StringVal("md5")),
                z3.Contains(payload_var, z3.StringVal("sha1")),
                z3.Contains(payload_var, z3.StringVal("des")),
                z3.Contains(payload_var, z3.StringVal("rc4")),
            ),
        )
    return ()


def safe_payload_candidates(vuln_class: str) -> tuple[str | None, ...]:
    normalized = vuln_class.upper()
    if "CWE-89" in normalized or "SQL" in normalized:
        return _SQLI_SAFE_PAYLOADS
    if "CWE-79" in normalized or "XSS" in normalized:
        return _XSS_SAFE_PAYLOADS
    if "CWE-78" in normalized or "CMD" in normalized or "COMMAND" in normalized:
        return _CMDI_SAFE_PAYLOADS
    if "CWE-22" in normalized or "TRAVERS" in normalized:
        return _PATH_TRAVERSAL_SAFE_PAYLOADS
    if "CWE-918" in normalized or "SSRF" in normalized:
        return _SSRF_SAFE_PAYLOADS
    if "CWE-601" in normalized or "REDIRECT" in normalized:
        return _OPEN_REDIRECT_SAFE_PAYLOADS
    if "CWE-502" in normalized or "DESERIAL" in normalized:
        return _INSECURE_DESERIALIZATION_SAFE_PAYLOADS
    if (
        "CWE-327" in normalized
        or "CWE-326" in normalized
        or "WEAK_CRYPTO" in normalized
        or "CIPHER" in normalized
    ):
        return _WEAK_CRYPTO_SAFE_PAYLOADS
    return (None,)


def _template_payload_candidates(template: ExploitTemplate) -> tuple[str | None, ...]:
    if template.safe_payloads:
        return tuple(template.safe_payloads)
    return safe_payload_candidates(template.vuln_class)


def is_safe_payload(payload: str) -> bool:
    lowered = payload.casefold()
    return not any(pattern in lowered for pattern in FORBIDDEN_PAYLOAD_PATTERNS)


def extract_model_values(
    model: z3.ModelRef,
    variables: Mapping[str, z3.ExprRef],
) -> dict[str, str]:
    values: dict[str, str] = {}
    for name, expression in variables.items():
        value = model.eval(expression, model_completion=True)
        values[name] = _model_value_to_string(value, expression)
    return values


def synthesize_payload(
    template: ExploitTemplate,
    *,
    slot: PayloadSlot,
    model_values: Mapping[str, str],
) -> SynthesizedPayload:
    raw_value = model_values[slot.name]
    method = template.http_method.upper()
    url = template.endpoint or "/"
    headers: dict[str, str] = {}
    body: object | None = None
    encoding = slot.encoding

    if slot.carrier == "body":
        if slot.encoding == "urlencoded":
            headers["Content-Type"] = "application/x-www-form-urlencoded"
            body = {slot.request_key: raw_value}
        else:
            headers["Content-Type"] = "application/json"
            body = _nested_body(slot.field_path, raw_value)
    elif slot.carrier == "query":
        body = {slot.request_key: raw_value}
        encoding = "query"
    elif slot.carrier == "path":
        url = _inject_path_value(url, slot, raw_value)
        encoding = "path"
    else:
        headers[slot.request_key] = raw_value

    return SynthesizedPayload(
        method=method,
        url=url,
        headers=headers,
        body=body,
        payload_values={slot.name: raw_value},
        encoding=encoding,
    )


def _declare_variable(name: str, sort_name: str) -> z3.ExprRef:
    if sort_name == "string":
        return z3.String(name)
    if sort_name == "int":
        return z3.Int(name)
    if sort_name == "float":
        return z3.Real(name)
    if sort_name == "bool":
        return z3.Bool(name)
    raise ConstraintTypeConflict(f"unsupported Z3 sort {sort_name!r} for variable {name!r}")


def _compare_expr(expression: z3.ExprRef, operator: str, number: int) -> z3.BoolRef:
    if operator == "eq":
        return expression == number
    if operator == "lt":
        return expression < number
    if operator == "le":
        return expression <= number
    if operator == "gt":
        return expression > number
    return expression >= number


def _model_value_to_string(value: z3.ExprRef, expression: z3.ExprRef) -> str:
    sort = expression.sort()
    if sort == z3.StringSort():
        return str(value.as_string())
    if sort == z3.IntSort():
        return str(value.as_long())
    if sort == z3.BoolSort():
        return "true" if z3.is_true(value) else "false"
    return str(value)


def _nested_body(path: Sequence[str], raw_value: str) -> dict[str, object]:
    if not path:
        return {"payload": raw_value}
    node: object = raw_value
    for segment in reversed(path):
        node = {segment: node}
    return node if isinstance(node, dict) else {"payload": raw_value}


def _inject_path_value(url: str, slot: PayloadSlot, raw_value: str) -> str:
    encoded = quote(raw_value, safe="")
    placeholder_candidates = (
        f":{slot.request_key}",
        f":{slot.field_path[-1]}",
        "{" + slot.request_key + "}",
        "{" + slot.field_path[-1] + "}",
    )
    for placeholder in dict.fromkeys(placeholder_candidates):
        if placeholder in url:
            return url.replace(placeholder, encoded, 1)
    if not url or url == "/":
        return f"/{encoded}"
    return f"{url.rstrip('/')}/{encoded}"


__all__ = [
    "DEFAULT_TIMEOUT_MS",
    "FORBIDDEN_PAYLOAD_PATTERNS",
    "PayloadSolution",
    "SolveAttempt",
    "SolverResult",
    "build_z3_query",
    "extract_model_values",
    "is_safe_payload",
    "safe_payload_candidates",
    "solve_constraint_set",
    "solve_exploit_template",
    "synthesize_payload",
    "translate_condition",
    "vulnerability_constraints",
]
