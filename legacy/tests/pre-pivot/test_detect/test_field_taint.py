from __future__ import annotations

from pathlib import Path

import pytest

from piranesi.detect.field_taint import (
    FieldOp,
    FieldSummaryCache,
    annotate_flow_with_fields,
    apply_field_sensitive_pruning,
    classify_step_operation,
    propagate_field_taint,
    prune_untainted_fields,
)
from piranesi.detect.flows import extract_candidate_findings
from piranesi.models import CandidateFinding, SourceLocation, TaintSink, TaintSource, TaintStep
from piranesi.scan.specs import SinkSpec, SinkType, SourceSpec, SourceType

FIELD_FIXTURE_DIR = (
    Path(__file__).resolve().parents[1] / "fixtures" / "typescript" / "field_sensitive"
)


class DummyJoernServer:
    def query(self, _cpgql: str) -> dict[str, object]:
        return {"success": True, "stdout": 'val res0: String = "[]"'}


def _location(snippet: str, line: int) -> SourceLocation:
    return SourceLocation(file="field_sensitive.ts", line=line, column=1, snippet=snippet)


def _step(snippet: str, line: int, *, sanitizer: str | None = None) -> TaintStep:
    return TaintStep(
        location=_location(snippet, line),
        operation="assignment",
        taint_state="tainted",
        through_function=None,
        sanitizer_applied=sanitizer,
    )


def _finding(
    *,
    vuln_class: str,
    source_snippet: str,
    source_parameter: str | None,
    sink_snippet: str,
    steps: list[str | tuple[str, str]],
    sink_type: str,
    sink_api_name: str,
    metadata: dict[str, object] | None = None,
) -> CandidateFinding:
    taint_path = [_step(source_snippet, 1)]
    for index, raw in enumerate(steps, start=2):
        if isinstance(raw, tuple):
            snippet, sanitizer = raw
        else:
            snippet, sanitizer = raw, None
        taint_path.append(_step(snippet, index, sanitizer=sanitizer))

    return CandidateFinding(
        id=f"{vuln_class}-{sink_api_name}-{len(taint_path)}",
        vuln_class=vuln_class,
        source=TaintSource(
            location=_location(source_snippet, 1),
            source_type="request_body",
            data_categories=["unknown"],
            parameter_name=source_parameter,
        ),
        sink=TaintSink(
            location=_location(sink_snippet, len(taint_path) + 1),
            sink_type=sink_type,
            api_name=sink_api_name,
        ),
        taint_path=taint_path,
        path_conditions=[],
        confidence=0.7,
        severity="medium",
        metadata=metadata or {},
    )


def _annotated_state(
    finding: CandidateFinding,
) -> tuple[list, object]:
    server = DummyJoernServer()
    cache = FieldSummaryCache()
    field_steps = annotate_flow_with_fields(finding, server, field_summary_cache=cache)
    state = propagate_field_taint(
        field_steps,
        finding.source,
        vuln_class=finding.vuln_class,
        effective_sanitizers=frozenset(
            item
            for item in finding.metadata.get("effective_sanitizers", [])
            if isinstance(item, str)
        ),
    )
    return field_steps, state


def test_field_sensitive_fixture_inventory_covers_fp_tp_and_edge_cases() -> None:
    fixture_names = {path.name for path in FIELD_FIXTURE_DIR.glob("*.ts")}

    assert len(fixture_names) >= 24
    assert {
        "field-destructure-sanitized.ts",
        "field-spread-override.ts",
        "field-independent-sanitize.ts",
        "field-reassign-safe.ts",
        "field-json-parse-safe-field.ts",
    }.issubset(fixture_names)
    assert {
        "field-destructure-unsanitized.ts",
        "field-spread-tainted.ts",
        "field-computed-access.ts",
        "field-json-parse-sink.ts",
        "field-template-mixed.ts",
        "field-nested-property.ts",
    }.issubset(fixture_names)
    assert {
        "field-rest-spread-destructure.ts",
        "field-default-value.ts",
        "field-dynamic-key-write.ts",
        "field-json-stringify-roundtrip.ts",
    }.issubset(fixture_names)


def test_classify_step_property_read() -> None:
    classified = classify_step_operation("const email = req.body.user.email;")

    assert classified.op is FieldOp.PROPERTY_READ
    assert classified.target == "email"
    assert classified.source_expr == "req.body.user"
    assert classified.field_name == "email"


def test_classify_step_destructuring() -> None:
    classified = classify_step_operation("const { id, name: displayName, ...rest } = req.body;")

    assert classified.op is FieldOp.DESTRUCTURE
    assert classified.source_expr == "req.body"
    assert classified.bindings == (("id", "id"), ("name", "displayName"))


def test_classify_step_spread() -> None:
    classified = classify_step_operation('const merged = { ...req.body, safe: "ok" };')

    assert classified.op is FieldOp.SPREAD
    assert classified.target == "merged"
    assert classified.spread_sources == ("req.body",)
    assert classified.explicit_keys == {"safe"}


def test_classify_step_computed_access() -> None:
    classified = classify_step_operation("const value = req.body[key];")

    assert classified.op is FieldOp.COMPUTED
    assert classified.target == "value"
    assert classified.source_expr == "req.body"
    assert classified.key_expression == "key"


def test_classify_step_sanitizer() -> None:
    classified = classify_step_operation("const safe = escapeHtml(name);")

    assert classified.op is FieldOp.SANITIZER
    assert classified.target == "safe"
    assert classified.source_expr == "escapeHtml(name)"


def test_propagate_simple_destructuring() -> None:
    finding = _finding(
        vuln_class="CWE-89",
        source_snippet="req.body",
        source_parameter="id",
        sink_snippet="db.query(id);",
        steps=[
            "const { id } = req.body;",
            "db.query(id);",
        ],
        sink_type="sql_query",
        sink_api_name="db.query",
    )

    _, state = _annotated_state(finding)

    assert state.taint_labels["id"].field_path == "id"


def test_propagate_nested_property() -> None:
    finding = _finding(
        vuln_class="CWE-89",
        source_snippet="req.body",
        source_parameter=None,
        sink_snippet="db.query(email);",
        steps=[
            "const user = req.body.user;",
            "const email = user.email;",
            "db.query(email);",
        ],
        sink_type="sql_query",
        sink_api_name="db.query",
    )

    _, state = _annotated_state(finding)

    assert state.taint_labels["email"].field_path == "user.email"


def test_propagate_spread_with_override() -> None:
    finding = _finding(
        vuln_class="CWE-79",
        source_snippet="req.body",
        source_parameter=None,
        sink_snippet="res.send(merged.safe);",
        steps=[
            'const merged = { ...req.body, safe: "ok" };',
            "res.send(merged.safe);",
        ],
        sink_type="html_output",
        sink_api_name="res.send",
    )

    field_steps, state = _annotated_state(finding)

    assert "safe" in state.object_safe_fields["merged"]
    assert (
        prune_untainted_fields(finding, field_steps, state, field_summary_cache=FieldSummaryCache())
        is None
    )


def test_propagate_computed_access_conservative() -> None:
    finding = _finding(
        vuln_class="CWE-89",
        source_snippet="req.body",
        source_parameter=None,
        sink_snippet="db.query(value);",
        steps=[
            "const value = req.body[key];",
            "db.query(value);",
        ],
        sink_type="sql_query",
        sink_api_name="db.query",
    )

    _, state = _annotated_state(finding)

    assert state.taint_labels["value"].field_path == ""
    kept = apply_field_sensitive_pruning([finding], DummyJoernServer())
    assert len(kept) == 1
    assert kept[0].id == finding.id


def test_propagate_json_parse() -> None:
    finding = _finding(
        vuln_class="CWE-89",
        source_snippet="req.body",
        source_parameter=None,
        sink_snippet="db.query(parsed.sql);",
        steps=[
            "const parsed = JSON.parse(req.body.data);",
            "db.query(parsed.sql);",
        ],
        sink_type="sql_query",
        sink_api_name="db.query",
    )

    _, state = _annotated_state(finding)

    assert state.taint_labels["parsed"].field_path == ""
    assert state.taint_labels["parsed"].confidence == pytest.approx(0.95)


def test_propagate_template_literal() -> None:
    finding = _finding(
        vuln_class="CWE-89",
        source_snippet="req.body",
        source_parameter=None,
        sink_snippet="db.query(query);",
        steps=[
            "const { id, name } = req.body;",
            ("const safeName = escapeHtml(name);", "escapeHtml"),
            "const query = `SELECT * FROM users WHERE id = '${id}' AND name = '${safeName}'`;",
            "db.query(query);",
        ],
        sink_type="sql_query",
        sink_api_name="db.query",
        metadata={"effective_sanitizers": ["escapeHtml"]},
    )

    _, state = _annotated_state(finding)

    assert state.taint_labels["query"].sanitized_for == frozenset()


def test_prune_sanitized_field() -> None:
    finding = _finding(
        vuln_class="CWE-79",
        source_snippet="req.body",
        source_parameter="name",
        sink_snippet="res.send(safeName);",
        steps=[
            "const { name } = req.body;",
            ("const safeName = escapeHtml(name);", "escapeHtml"),
            "res.send(safeName);",
        ],
        sink_type="html_output",
        sink_api_name="res.send",
        metadata={"effective_sanitizers": ["escapeHtml"]},
    )

    assert apply_field_sensitive_pruning([finding], DummyJoernServer()) == []


def test_preserve_unsanitized_field() -> None:
    finding = _finding(
        vuln_class="CWE-89",
        source_snippet="req.body",
        source_parameter="id",
        sink_snippet="db.query(id);",
        steps=[
            "const { id } = req.body;",
            "db.query(id);",
        ],
        sink_type="sql_query",
        sink_api_name="db.query",
    )

    kept = apply_field_sensitive_pruning([finding], DummyJoernServer())
    assert len(kept) == 1
    assert kept[0].id == finding.id


def test_skip_single_field_source() -> None:
    finding = _finding(
        vuln_class="CWE-89",
        source_snippet="req.params.id",
        source_parameter="id",
        sink_snippet="db.query(req.params.id);",
        steps=["db.query(req.params.id);"],
        sink_type="sql_query",
        sink_api_name="db.query",
    )

    kept = apply_field_sensitive_pruning([finding], DummyJoernServer())
    assert len(kept) == 1
    assert kept[0].id == finding.id


def test_skip_short_path() -> None:
    finding = _finding(
        vuln_class="CWE-79",
        source_snippet="req.body",
        source_parameter=None,
        sink_snippet="req.body",
        steps=[],
        sink_type="html_output",
        sink_api_name="res.send",
    )

    kept = apply_field_sensitive_pruning([finding], DummyJoernServer())
    assert len(kept) == 1
    assert kept[0].id == finding.id


def test_wrong_cwe_sanitizer_not_pruned() -> None:
    finding = _finding(
        vuln_class="CWE-89",
        source_snippet="req.body",
        source_parameter="id",
        sink_snippet="db.query(safeId);",
        steps=[
            "const { id } = req.body;",
            ("const safeId = escapeHtml(id);", "escapeHtml"),
            "db.query(safeId);",
        ],
        sink_type="sql_query",
        sink_api_name="db.query",
        metadata={"effective_sanitizers": ["escapeHtml"]},
    )

    kept = apply_field_sensitive_pruning([finding], DummyJoernServer())
    assert len(kept) == 1
    assert kept[0].id == finding.id


def test_extract_candidate_findings_runs_field_sensitive_pruning(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    body_source = SourceSpec(
        name="express_req_body",
        pattern="",
        source_type=SourceType.REQUEST_BODY,
    )
    html_sink = SinkSpec(
        name="response_output",
        pattern="",
        sink_type=SinkType.HTML_OUTPUT,
        cwe_id="CWE-79",
    )
    finding = _finding(
        vuln_class="CWE-79",
        source_snippet="req.body",
        source_parameter="name",
        sink_snippet="res.send(safeName);",
        steps=[
            "const { name } = req.body;",
            ("const safeName = escapeHtml(name);", "escapeHtml"),
            "res.send(safeName);",
        ],
        sink_type="html_output",
        sink_api_name="res.send",
        metadata={"effective_sanitizers": ["escapeHtml"]},
    )

    def _fake_extract_findings_for_pair(
        *_args: object, **_kwargs: object
    ) -> list[CandidateFinding]:
        return [finding]

    monkeypatch.setattr(
        "piranesi.detect.flows._extract_findings_for_pair", _fake_extract_findings_for_pair
    )
    monkeypatch.setattr(
        "piranesi.detect.flows.extract_interprocedural_findings", lambda *_a, **_k: ()
    )
    monkeypatch.setattr("piranesi.detect.flows.extract_alias_findings", lambda *_a, **_k: ())
    monkeypatch.setattr(
        "piranesi.detect.flows.extract_prototype_pollution_findings",
        lambda *_a, **_k: (),
    )
    monkeypatch.setattr(
        "piranesi.detect.flows.classify_candidate_findings", lambda findings, **_k: findings
    )

    pruned = extract_candidate_findings(
        DummyJoernServer(),  # type: ignore[arg-type]
        joern_project_root=FIELD_FIXTURE_DIR,
        source_specs=(body_source,),
        sink_specs=(html_sink,),
        sanitizer_specs=(),
    )
    preserved = extract_candidate_findings(
        DummyJoernServer(),  # type: ignore[arg-type]
        joern_project_root=FIELD_FIXTURE_DIR,
        source_specs=(body_source,),
        sink_specs=(html_sink,),
        sanitizer_specs=(),
        field_sensitive=False,
    )

    assert pruned == ()
    assert preserved == (finding,)
