from __future__ import annotations

from pathlib import Path

from piranesi.models import (
    CandidateFinding,
    PathCondition,
    SourceLocation,
    TaintSink,
    TaintSource,
    TaintStep,
)
from piranesi.verify.constraints import (
    StringEq,
    TypeCheck,
    extract_exploit_template,
    select_exploit_template_spec,
)


def test_extract_exploit_template_infers_route_and_normalizes_constraints(tmp_path: Path) -> None:
    app_file = tmp_path / "app.ts"
    app_file.write_text(
        "\n".join(
            [
                'app.get("/search", (req, res) => {',
                "  const q = req.query.q as string;",
                '  if (typeof q !== "string") {',
                "    return res.status(400).send('bad');",
                "  }",
                "  return res.send(q);",
                "});",
            ]
        ),
        encoding="utf-8",
    )
    finding = _candidate_finding(
        app_file,
        source_line=2,
        source_snippet="const q = req.query.q as string;",
        source_type="req.query.q",
        parameter_name="q",
        path_conditions=[
            _raw_condition(app_file, 'typeof q === "string"'),
            _raw_condition(app_file, "q.length >= 0"),
            _raw_condition(app_file, 'q.includes("admin")'),
            _raw_condition(app_file, 'q.includes("admin")'),
            _raw_condition(app_file, 'q === "superadmin"'),
        ],
    )

    template = extract_exploit_template(finding)

    assert template.http_method == "GET"
    assert template.endpoint == "/search"
    assert len(template.payload_slots) == 1
    assert template.payload_slots[0].carrier == "query"
    assert template.payload_slots[0].name == "q"
    assert template.payload_slots[0].encoding == "query"
    assert template.constraint_sets == (
        (
            TypeCheck(var="q", type_name="string"),
            StringEq(var="q", val="superadmin"),
        ),
    )
    assert template.unsat_reason is None


def test_extract_exploit_template_expands_disjunctions(tmp_path: Path) -> None:
    app_file = tmp_path / "app.ts"
    app_file.write_text(
        "\n".join(
            [
                'app.get("/search", (req, res) => {',
                "  const q = req.query.q;",
                "  return res.send(q);",
                "});",
            ]
        ),
        encoding="utf-8",
    )
    finding = _candidate_finding(
        app_file,
        source_line=2,
        source_snippet="const q = req.query.q;",
        source_type="req.query.q",
        parameter_name="q",
        path_conditions=[
            _raw_condition(app_file, 'q === "admin" || q === "root" || q === "guest"'),
        ],
    )

    template = extract_exploit_template(finding)

    assert template.constraint_sets == (
        (StringEq(var="q", val="admin"),),
        (StringEq(var="q", val="root"),),
        (StringEq(var="q", val="guest"),),
    )


def test_extract_exploit_template_marks_contradictions_unsat(tmp_path: Path) -> None:
    app_file = tmp_path / "app.ts"
    app_file.write_text(
        "\n".join(
            [
                'app.post("/login", (req, res) => {',
                "  const username = req.body.username;",
                "  return res.send(username);",
                "});",
            ]
        ),
        encoding="utf-8",
    )
    finding = _candidate_finding(
        app_file,
        source_line=2,
        source_snippet="const username = req.body.username;",
        source_type="req.body.username",
        parameter_name="username",
        path_conditions=[
            _raw_condition(app_file, 'username === "alice"'),
            _raw_condition(app_file, 'username === "bob"'),
        ],
    )

    template = extract_exploit_template(finding)

    assert template.http_method == "POST"
    assert template.endpoint == "/login"
    assert template.payload_slots[0].carrier == "body"
    assert template.payload_slots[0].encoding == "json"
    assert template.constraint_sets == ()
    assert template.unsat_reason == "CONSTRAINTS_UNSATISFIABLE"


def test_extract_exploit_template_selects_ssrf_template_by_cwe(tmp_path: Path) -> None:
    app_file = tmp_path / "proxy.ts"
    app_file.write_text(
        "\n".join(
            [
                'app.get("/proxy", (req, res) => {',
                "  const target = req.query.url;",
                "  return fetch(target).then((r) => r.text()).then((body) => res.send(body));",
                "});",
            ]
        ),
        encoding="utf-8",
    )
    finding = _candidate_finding(
        app_file,
        source_line=2,
        source_snippet="const target = req.query.url;",
        source_type="req.query.url",
        parameter_name="url",
        path_conditions=[],
        vuln_class="CWE-918: Server-Side Request Forgery",
        sink_type="http_request",
        api_name="fetch",
    )

    template = extract_exploit_template(finding)

    assert template.template_id == "ssrf-loopback-probe"
    assert template.template_selection_reason.startswith("matched finding CWE CWE-918")
    assert template.safe_payloads[0] == "http://127.0.0.1:80/"
    assert template.network_callbacks_allowed is False
    assert template.destructive_payloads is False


def test_select_template_spec_uses_metadata_tokens_for_open_redirect(tmp_path: Path) -> None:
    app_file = tmp_path / "redirect.ts"
    app_file.write_text(
        "\n".join(
            [
                'app.get("/jump", (req, res) => {',
                "  const next = req.query.next;",
                "  return res.redirect(next);",
                "});",
            ]
        ),
        encoding="utf-8",
    )
    finding = _candidate_finding(
        app_file,
        source_line=2,
        source_snippet="const next = req.query.next;",
        source_type="req.query.next",
        parameter_name="next",
        path_conditions=[],
        vuln_class="open redirect risk",
        sink_type="redirect",
        api_name="res.redirect",
        metadata={"sink_spec_category": "open_redirect"},
    )

    template = extract_exploit_template(finding)
    _, selection_reason = select_exploit_template_spec(
        finding,
        payload_slots=template.payload_slots,
        http_method=template.http_method,
        endpoint=template.endpoint,
    )

    assert template.template_id == "open-redirect-probe"
    assert "matched sink/category metadata token" in selection_reason
    assert "route=GET /jump" in selection_reason
    assert template.expected_evidence


def test_extract_exploit_template_selects_weak_crypto_template(tmp_path: Path) -> None:
    app_file = tmp_path / "crypto.ts"
    app_file.write_text(
        "\n".join(
            [
                'app.post("/sign", (req, res) => {',
                "  const alg = req.body.alg;",
                "  return sign(req.body.payload, alg);",
                "});",
            ]
        ),
        encoding="utf-8",
    )
    finding = _candidate_finding(
        app_file,
        source_line=2,
        source_snippet="const alg = req.body.alg;",
        source_type="req.body.alg",
        parameter_name="alg",
        path_conditions=[],
        vuln_class="CWE-327: Broken or Risky Crypto Algorithm",
        sink_type="crypto_operation",
        api_name="sign",
    )

    template = extract_exploit_template(finding)

    assert template.template_id == "weak-crypto-algorithm-probe"
    assert template.timeout_ms == 20_000
    assert template.risk_level == "low"
    assert "md5" in template.safe_payloads


def _candidate_finding(
    file_path: Path,
    *,
    source_line: int,
    source_snippet: str,
    source_type: str,
    parameter_name: str,
    path_conditions: list[PathCondition],
    vuln_class: str = "CWE-79",
    sink_type: str = "html_output",
    api_name: str = "res.send",
    metadata: dict[str, object] | None = None,
) -> CandidateFinding:
    source_location = SourceLocation(
        file=str(file_path),
        line=source_line,
        column=11,
        snippet=source_snippet,
    )
    sink_location = SourceLocation(
        file=str(file_path),
        line=source_line + 1,
        column=9,
        snippet="res.send(q);",
    )
    step_location = SourceLocation(
        file=str(file_path),
        line=source_line + 1,
        column=9,
        snippet="return res.send(q);",
    )
    return CandidateFinding(
        id="finding-verify",
        vuln_class=vuln_class,
        source=TaintSource(
            location=source_location,
            source_type=source_type,
            data_categories=["identifier"],
            parameter_name=parameter_name,
        ),
        sink=TaintSink(
            location=sink_location,
            sink_type=sink_type,
            api_name=api_name,
        ),
        taint_path=[
            TaintStep(
                location=step_location,
                operation="call_arg",
                taint_state="tainted",
            )
        ],
        path_conditions=path_conditions,
        confidence=0.9,
        severity="medium",
        metadata={} if metadata is None else dict(metadata),
    )


def _raw_condition(
    file_path: Path,
    expression: str,
    *,
    required_value: bool = True,
) -> PathCondition:
    return PathCondition(
        location=SourceLocation(
            file=str(file_path),
            line=1,
            column=1,
            snippet=expression,
        ),
        condition_type="branch",
        expression=expression,
        required_value=required_value,
        symbolic_constraint=None,
    )
