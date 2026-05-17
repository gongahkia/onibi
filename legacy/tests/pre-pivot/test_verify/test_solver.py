from __future__ import annotations

from collections.abc import Sequence

import z3  # type: ignore[import-untyped]

from piranesi.verify.constraints import (
    ExploitTemplate,
    IntBound,
    LogicalAnd,
    LogicalNot,
    LogicalOr,
    PayloadSlot,
    StringContains,
    StringEq,
    StringLength,
    TypeCheck,
)
from piranesi.verify.solver import (
    build_z3_query,
    is_safe_payload,
    safe_payload_candidates,
    solve_constraint_set,
    solve_exploit_template,
    synthesize_payload,
)


def test_z3_translation_string_eq() -> None:
    slot = _slot(name="input", carrier="query", encoding="query")
    attempt = solve_constraint_set(
        _template(vuln_class="CWE-200", slot=slot),
        [StringEq(var="input", val="hello")],
        slot=slot,
    )

    assert attempt.status == "SAT"
    assert attempt.solution is not None
    assert attempt.solution.model_values["input"] == "hello"


def test_z3_translation_string_contains() -> None:
    slot = _slot(name="input", carrier="query", encoding="query")
    attempt = solve_constraint_set(
        _template(vuln_class="CWE-200", slot=slot),
        [StringContains(var="input", substr="admin")],
        slot=slot,
    )

    assert attempt.status == "SAT"
    assert attempt.solution is not None
    assert "admin" in attempt.solution.model_values["input"]


def test_z3_translation_string_length() -> None:
    slot = _slot(name="input", carrier="query", encoding="query")
    attempt = solve_constraint_set(
        _template(vuln_class="CWE-200", slot=slot),
        [StringLength(var="input", op="ge", n=6)],
        slot=slot,
    )

    assert attempt.status == "SAT"
    assert attempt.solution is not None
    assert len(attempt.solution.model_values["input"]) >= 6


def test_z3_translation_int_bound_and_type_check() -> None:
    slot = _slot(name="input", carrier="query", encoding="query")
    attempt = solve_constraint_set(
        _template(vuln_class="CWE-200", slot=slot),
        [
            TypeCheck(var="count", type_name="int"),
            IntBound(var="count", op="eq", n=7),
        ],
        slot=slot,
    )

    assert attempt.status == "SAT"
    assert attempt.solution is not None
    assert attempt.solution.model_values["count"] == "7"

    _, variables = build_z3_query(
        _template(vuln_class="CWE-200", slot=slot),
        [
            TypeCheck(var="count", type_name="int"),
            IntBound(var="count", op="eq", n=7),
        ],
        slot=slot,
    )
    assert str(variables["count"].sort()) == "Int"


def test_z3_translation_logical_and_or_not() -> None:
    slot = _slot(name="input", carrier="query", encoding="query")
    constraint = LogicalAnd(
        (
            LogicalOr(
                (
                    StringEq(var="input", val="hello"),
                    StringEq(var="input", val="world"),
                )
            ),
            LogicalNot(StringContains(var="input", substr="zzz")),
        )
    )

    attempt = solve_constraint_set(
        _template(vuln_class="CWE-200", slot=slot),
        [constraint],
        slot=slot,
    )

    assert attempt.status == "SAT"
    assert attempt.solution is not None
    assert attempt.solution.model_values["input"] in {"hello", "world"}


def test_sqli_payload_synthesis_json_body() -> None:
    slot = _slot(name="username", carrier="body", encoding="json")
    result = solve_exploit_template(
        _template(vuln_class="CWE-89", slot=slot, method="POST", endpoint="/login")
    )

    assert result.status == "SAT"
    payload = result.solutions[0].payload
    assert payload.method == "POST"
    assert payload.url == "/login"
    assert payload.encoding == "json"
    assert payload.headers["Content-Type"] == "application/json"
    assert payload.body == {"username": payload.payload_values["username"]}
    assert "'" in payload.payload_values["username"]


def test_xss_payload_synthesis_query_params() -> None:
    slot = _slot(name="q", carrier="query", encoding="query")
    result = solve_exploit_template(
        _template(vuln_class="CWE-79", slot=slot, method="GET", endpoint="/search")
    )

    assert result.status == "SAT"
    payload = result.solutions[0].payload
    assert payload.url == "/search"
    assert payload.encoding == "query"
    assert payload.body == {"q": payload.payload_values["q"]}
    assert "<script>" in payload.payload_values["q"]


def test_cmdi_payload_synthesis_headers() -> None:
    slot = _slot(
        name="x-command",
        carrier="header",
        field_path=("X-Command",),
        encoding="json",
    )
    result = solve_exploit_template(
        _template(vuln_class="CWE-78", slot=slot, method="GET", endpoint="/run")
    )

    assert result.status == "SAT"
    payload = result.solutions[0].payload
    assert payload.url == "/run"
    assert payload.body is None
    assert payload.headers["X-Command"] == payload.payload_values["x-command"]
    assert payload.payload_values["x-command"] in safe_payload_candidates("CWE-78")
    assert is_safe_payload(payload.payload_values["x-command"])
    assert any(
        token in payload.payload_values["x-command"]
        for token in ("id", "whoami", "cat /etc/passwd")
    )


def test_path_traversal_payload_synthesis_path_params() -> None:
    slot = _slot(name="file", carrier="path", encoding="path")
    result = solve_exploit_template(
        _template(vuln_class="CWE-22", slot=slot, method="GET", endpoint="/files/:file")
    )

    assert result.status == "SAT"
    payload = result.solutions[0].payload
    assert payload.encoding == "path"
    assert payload.url.startswith("/files/")
    assert "%2F" in payload.url
    assert ".." in payload.url


def test_ssrf_payload_synthesis_uses_loopback_payload() -> None:
    slot = _slot(name="url", carrier="query", encoding="query")
    result = solve_exploit_template(
        _template(vuln_class="CWE-918", slot=slot, method="GET", endpoint="/proxy")
    )

    assert result.status == "SAT"
    payload = result.solutions[0].payload
    assert payload.url == "/proxy"
    assert payload.body == {"url": payload.payload_values["url"]}
    assert payload.payload_values["url"] in safe_payload_candidates("CWE-918")
    assert payload.payload_values["url"].startswith("http://")


def test_open_redirect_payload_synthesis_uses_external_destination() -> None:
    slot = _slot(name="next", carrier="query", encoding="query")
    result = solve_exploit_template(
        _template(vuln_class="CWE-601", slot=slot, method="GET", endpoint="/jump")
    )

    assert result.status == "SAT"
    payload = result.solutions[0].payload
    assert payload.body == {"next": payload.payload_values["next"]}
    assert payload.payload_values["next"] in safe_payload_candidates("CWE-601")
    assert "example.com" in payload.payload_values["next"]


def test_insecure_deserialization_payload_synthesis_uses_marker_payload() -> None:
    slot = _slot(name="blob", carrier="body", encoding="json")
    result = solve_exploit_template(
        _template(vuln_class="CWE-502", slot=slot, method="POST", endpoint="/import")
    )

    assert result.status == "SAT"
    payload = result.solutions[0].payload
    assert payload.payload_values["blob"] in safe_payload_candidates("CWE-502")
    assert payload.headers["Content-Type"] == "application/json"
    assert payload.body == {"blob": payload.payload_values["blob"]}


def test_weak_crypto_payload_synthesis_uses_known_weak_algorithm() -> None:
    slot = _slot(name="alg", carrier="query", encoding="query")
    result = solve_exploit_template(
        _template(vuln_class="CWE-327", slot=slot, method="GET", endpoint="/sign")
    )

    assert result.status == "SAT"
    payload = result.solutions[0].payload
    assert payload.payload_values["alg"] in safe_payload_candidates("CWE-327")
    assert payload.payload_values["alg"] in {"md5", "sha1", "des", "rc4", "tls1.0"}


def test_synthesize_urlencoded_body_post_processes_after_z3() -> None:
    slot = _slot(name="username", carrier="body", encoding="urlencoded")
    payload = synthesize_payload(
        _template(vuln_class="CWE-89", slot=slot, method="POST", endpoint="/login"),
        slot=slot,
        model_values={"username": "' OR 1=1--"},
    )

    assert payload.encoding == "urlencoded"
    assert payload.headers["Content-Type"] == "application/x-www-form-urlencoded"
    assert payload.body == {"username": "' OR 1=1--"}


def test_sqli_payload_generation_uses_safe_read_only_candidate() -> None:
    slot = _slot(name="username", carrier="body", encoding="json")
    result = solve_exploit_template(
        _template(vuln_class="CWE-89", slot=slot, method="POST", endpoint="/login")
    )

    assert result.status == "SAT"
    payload_value = result.solutions[0].payload.payload_values["username"]
    assert payload_value in safe_payload_candidates("CWE-89")
    assert is_safe_payload(payload_value)
    assert any(token in payload_value for token in ("OR 1=1", "UNION SELECT", "SLEEP"))


def test_solver_rejects_destructive_payloads_without_unsafe_opt_in() -> None:
    slot = _slot(name="username", carrier="body", encoding="json")
    template = _template(
        vuln_class="CWE-89",
        slot=slot,
        method="POST",
        endpoint="/users",
        safe_payloads=("'; DELETE FROM users;--",),
        destructive_payloads=True,
    )

    safe_result = solve_exploit_template(template)
    unsafe_result = solve_exploit_template(template, allow_unsafe_payloads=True)

    assert safe_result.status == "UNVERIFIABLE"
    assert safe_result.reason == "UNSAFE_PAYLOAD_REJECTED"
    assert unsafe_result.status == "SAT"
    assert unsafe_result.solutions
    assert "DELETE" in unsafe_result.solutions[0].payload.payload_values["username"]


def test_timeout_handling_returns_unverifiable() -> None:
    slot = _slot(name="input", carrier="query", encoding="query")

    class FakeTimeoutSolver:
        def set(self, *_args: object, **_kwargs: object) -> None:
            return None

        def add(self, *_args: z3.ExprRef) -> None:
            return None

        def check(self) -> z3.CheckSatResult:
            return z3.unknown

        def reason_unknown(self) -> str:
            return "timeout"

    attempt = solve_constraint_set(
        _template(vuln_class="CWE-200", slot=slot),
        [StringContains(var="input", substr="x")],
        slot=slot,
        solver_factory=FakeTimeoutSolver,
    )

    assert attempt.status == "UNVERIFIABLE"
    assert attempt.reason == "SOLVER_TIMEOUT"


def test_unsat_handling_returns_unverifiable() -> None:
    slot = _slot(name="input", carrier="query", encoding="query")
    attempt = solve_constraint_set(
        _template(vuln_class="CWE-200", slot=slot),
        [
            StringEq(var="input", val="alice"),
            StringEq(var="input", val="bob"),
        ],
        slot=slot,
    )

    assert attempt.status == "UNVERIFIABLE"
    assert attempt.reason == "CONSTRAINTS_UNSATISFIABLE"


def _template(
    *,
    vuln_class: str,
    slot: PayloadSlot,
    method: str = "GET",
    endpoint: str = "/",
    constraint_sets: Sequence[Sequence[object]] | None = None,
    safe_payloads: Sequence[str] = (),
    destructive_payloads: bool = False,
) -> ExploitTemplate:
    resolved_sets = tuple(tuple(constraint_set) for constraint_set in (constraint_sets or [()]))
    return ExploitTemplate(
        vuln_class=vuln_class,
        http_method=method,
        endpoint=endpoint,
        payload_slots=(slot,),
        path_conditions=(),
        constraint_sets=resolved_sets,  # type: ignore[arg-type]
        safe_payloads=tuple(safe_payloads),
        destructive_payloads=destructive_payloads,
        unsat_reason=None,
    )


def _slot(
    *,
    name: str,
    carrier: str,
    encoding: str,
    field_path: tuple[str, ...] | None = None,
) -> PayloadSlot:
    return PayloadSlot(
        name=name,
        carrier=carrier,  # type: ignore[arg-type]
        field_path=field_path or (name,),
        source=f"req.{carrier}.{name}",
        encoding=encoding,  # type: ignore[arg-type]
    )
