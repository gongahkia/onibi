from __future__ import annotations

import json
from types import SimpleNamespace
from typing import Any

from piranesi.detect.categories import (
    classify_candidate_finding,
    classify_field_name_categories,
    classify_route_context_categories,
)
from piranesi.models import CandidateFinding, SourceLocation, TaintSink, TaintSource


class StubProvider:
    def __init__(self, payload: dict[str, Any]) -> None:
        self.payload = payload
        self.calls: list[dict[str, Any]] = []

    def complete(self, **kwargs: Any) -> SimpleNamespace:
        self.calls.append(kwargs)
        return SimpleNamespace(content=json.dumps(self.payload))


def _build_finding(*, parameter_name: str | None) -> CandidateFinding:
    return CandidateFinding(
        id="finding-1",
        vuln_class="CWE-89",
        source=TaintSource(
            location=SourceLocation(
                file="src/api/users.ts",
                line=10,
                column=8,
                snippet=f"const value = req.body.{parameter_name or 'value'};",
            ),
            source_type="request_body",
            data_categories=["unknown"],
            parameter_name=parameter_name,
        ),
        sink=TaintSink(
            location=SourceLocation(
                file="src/api/users.ts",
                line=20,
                column=4,
                snippet="db.query(sql);",
            ),
            sink_type="sql_query",
            api_name="db.query",
        ),
        taint_path=[],
        path_conditions=[],
        confidence=0.75,
        severity="high",
    )


def test_classify_field_name_categories_matches_common_heuristics() -> None:
    assert classify_field_name_categories("nric") == ["nric"]
    assert classify_field_name_categories("ic_number") == ["nric"]
    assert classify_field_name_categories("email") == ["contact_email"]
    assert classify_field_name_categories("credit_card") == ["financial_credit_card"]
    assert classify_field_name_categories("cc_number") == ["financial_credit_card"]
    assert classify_field_name_categories("employee_salary") == ["financial_income", "employment"]
    assert classify_field_name_categories("password") == ["credentials"]
    assert classify_field_name_categories("password_hash") == []


def test_classify_route_context_categories_uses_route_patterns() -> None:
    assert classify_route_context_categories("/api/users/:id", field_name="id") == ["name"]
    assert classify_route_context_categories("/api/employees/:id", field_name="id") == [
        "employment"
    ]
    assert classify_route_context_categories("/api/payment-methods/cards/:id", field_name="id") == [
        "financial_credit_card"
    ]


def test_classify_candidate_finding_populates_source_categories_from_heuristics() -> None:
    finding = _build_finding(parameter_name="email")

    classified = classify_candidate_finding(finding)

    assert classified.source.data_categories == ["contact_email"]
    assert finding.source.data_categories == ["unknown"]


def test_classify_candidate_finding_uses_llm_fallback() -> None:
    finding = _build_finding(parameter_name="profile_blob")
    provider = StubProvider({"categories": ["contact_email", "not_a_real_category"]})

    classified = classify_candidate_finding(
        finding,
        route_pattern="/api/export",
        provider=provider,  # type: ignore[arg-type]
        model="mock-model",
    )

    assert classified.source.data_categories == ["contact_email"]
    assert provider.calls[0]["stage"] == "detector"
    assert provider.calls[0]["model"] == "mock-model"
    assert provider.calls[0]["response_format"] is not None
    assert "profile_blob" in provider.calls[0]["messages"][1]["content"]
    assert "/api/export" in provider.calls[0]["messages"][1]["content"]
