from __future__ import annotations

from piranesi.models import CandidateFinding, SourceLocation, TaintSink, TaintSource
from piranesi.scan.query_quality import build_query_quality_metrics
from piranesi.scan.specs import SinkSpec, SinkType, SourceSpec, SourceType


def test_build_query_quality_metrics_tracks_matched_unmatched_and_noisy_specs() -> None:
    source_specs = (
        SourceSpec(
            name="express_req_body",
            pattern='cpg.call.code("req.body")',
            source_type=SourceType.REQUEST_BODY,
        ),
        SourceSpec(
            name="express_req_query",
            pattern='cpg.call.code("req.query")',
            source_type=SourceType.REQUEST_PARAM,
        ),
        SourceSpec(
            name="custom_source_1",
            pattern='cpg.call.code("ctx.userInput")',
            source_type=SourceType.CUSTOM,
            is_custom=True,
        ),
    )
    sink_specs = (
        SinkSpec(
            name="raw_sql_query",
            pattern='cpg.call.name("query")',
            sink_type=SinkType.SQL_QUERY,
            cwe_id="CWE-89",
        ),
        SinkSpec(
            name="response_output",
            pattern='cpg.call.name("send")',
            sink_type=SinkType.HTML_OUTPUT,
            cwe_id="CWE-79",
        ),
        SinkSpec(
            name="custom_sink_1",
            pattern='cpg.call.name("customSink")',
            sink_type=SinkType.CUSTOM,
            cwe_id="CWE-20",
            is_custom=True,
        ),
    )
    findings = [
        *[
            _candidate_finding(
                finding_id=f"finding-noisy-{index}",
                source_spec_name="express_req_body",
                sink_spec_name="raw_sql_query",
            )
            for index in range(1, 5)
        ],
        _candidate_finding(
            finding_id="finding-single",
            source_spec_name="express_req_query",
            sink_spec_name="response_output",
        ),
        _candidate_finding(
            finding_id="finding-unknown-spec",
            source_spec_name="missing_source",
            sink_spec_name="missing_sink",
        ),
    ]

    metrics = build_query_quality_metrics(
        source_specs=source_specs,
        sink_specs=sink_specs,
        candidate_findings=findings,
        noisy_candidate_threshold=3,
    )

    assert metrics.loaded_source_specs == 3
    assert metrics.loaded_sink_specs == 3
    assert metrics.matched_source_specs == 2
    assert metrics.matched_sink_specs == 2

    assert [spec.name for spec in metrics.unmatched_source_specs] == ["custom_source_1"]
    assert [spec.name for spec in metrics.unmatched_sink_specs] == ["custom_sink_1"]

    source_usage_by_name = {usage.spec.name: usage for usage in metrics.source_specs}
    sink_usage_by_name = {usage.spec.name: usage for usage in metrics.sink_specs}

    assert source_usage_by_name["express_req_body"].candidate_count == 4
    assert source_usage_by_name["express_req_query"].candidate_count == 1
    assert source_usage_by_name["custom_source_1"].candidate_count == 0
    assert source_usage_by_name["custom_source_1"].spec.definition_origin == "config"
    assert source_usage_by_name["custom_source_1"].spec.definition_file == "piranesi.toml"

    assert sink_usage_by_name["raw_sql_query"].candidate_count == 4
    assert sink_usage_by_name["response_output"].candidate_count == 1
    assert sink_usage_by_name["custom_sink_1"].candidate_count == 0

    assert [usage.spec.name for usage in metrics.noisy_source_specs] == ["express_req_body"]
    assert [usage.spec.name for usage in metrics.noisy_sink_specs] == ["raw_sql_query"]


def test_build_query_quality_metrics_marks_unmatched_specs_without_spec_metadata() -> None:
    source_specs = (
        SourceSpec(
            name="express_req_body",
            pattern='cpg.call.code("req.body")',
            source_type=SourceType.REQUEST_BODY,
        ),
    )
    sink_specs = (
        SinkSpec(
            name="raw_sql_query",
            pattern='cpg.call.name("query")',
            sink_type=SinkType.SQL_QUERY,
            cwe_id="CWE-89",
        ),
    )

    metrics = build_query_quality_metrics(
        source_specs=source_specs,
        sink_specs=sink_specs,
        candidate_findings=[_candidate_finding(finding_id="finding-1")],
    )

    assert metrics.matched_source_specs == 0
    assert metrics.matched_sink_specs == 0
    assert [spec.name for spec in metrics.unmatched_source_specs] == ["express_req_body"]
    assert [spec.name for spec in metrics.unmatched_sink_specs] == ["raw_sql_query"]
    assert metrics.noisy_source_specs == []
    assert metrics.noisy_sink_specs == []


def _candidate_finding(
    *,
    finding_id: str,
    source_spec_name: str | None = None,
    sink_spec_name: str | None = None,
) -> CandidateFinding:
    metadata: dict[str, object] = {}
    if source_spec_name is not None:
        metadata["source_spec_name"] = source_spec_name
    if sink_spec_name is not None:
        metadata["sink_spec_name"] = sink_spec_name

    return CandidateFinding(
        id=finding_id,
        vuln_class="CWE-89",
        source=TaintSource(
            location=SourceLocation(file="app.ts", line=10, column=4, snippet="req.body.id"),
            source_type="request_body",
            data_categories=["unknown"],
            parameter_name="id",
        ),
        sink=TaintSink(
            location=SourceLocation(file="app.ts", line=20, column=8, snippet="db.query(sql)"),
            sink_type="sql_query",
            api_name="db.query",
        ),
        taint_path=[],
        path_conditions=[],
        confidence=0.7,
        severity="high",
        metadata=metadata,
    )
