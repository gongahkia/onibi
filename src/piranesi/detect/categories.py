from __future__ import annotations

import json
import re
from collections.abc import Mapping, Sequence
from typing import TYPE_CHECKING

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from piranesi.legal.taxonomy import classify_field, supported_categories
from piranesi.models import CandidateFinding, TaintSource

if TYPE_CHECKING:
    from piranesi.llm.provider import LLMProvider

_UNKNOWN_CATEGORY = "unknown"
_CAMEL_CASE_BOUNDARY = re.compile(r"(?<=[a-z0-9])(?=[A-Z])")
_NON_ALNUM = re.compile(r"[^a-z0-9]+")
_SUPPORTED_CATEGORY_SET = frozenset(supported_categories())
_GENERIC_PERSON_ROUTE_TOKENS = frozenset(
    {
        "account",
        "accounts",
        "customer",
        "customers",
        "member",
        "members",
        "person",
        "people",
        "profile",
        "profiles",
        "staff",
        "team",
        "teams",
        "user",
        "users",
    }
)
_GENERIC_IDENTIFIER_FIELDS = frozenset(
    {
        "account_id",
        "customer_id",
        "employee_id",
        "id",
        "member_id",
        "patient_id",
        "person_id",
        "profile_id",
        "record_id",
        "staff_id",
        "user_id",
        "uuid",
    }
)
_FIELD_ALIAS_CATEGORIES: tuple[tuple[tuple[str, ...], str], ...] = (
    (("cc", "cc_no", "cc_num", "cc_number"), "financial_credit_card"),
    (("ic", "ic_no", "ic_num", "ic_number"), "nric"),
)
_ROUTE_ALIAS_CATEGORIES: tuple[tuple[frozenset[str], str], ...] = (
    (
        frozenset({"address", "addresses", "postal", "postcode", "zip", "zipcode"}),
        "contact_address",
    ),
    (frozenset({"bank", "banks", "banking", "iban", "routing", "swift"}), "financial_bank"),
    (
        frozenset({"billing", "card", "cards", "checkout", "payment", "payments", "wallet"}),
        "financial_credit_card",
    ),
    (
        frozenset({"criminal", "crime", "offence", "offences", "offense", "offenses", "police"}),
        "criminal",
    ),
    (frozenset({"dob", "birthday", "birthdays"}), "dob"),
    (frozenset({"email", "emails", "mail"}), "contact_email"),
    (frozenset({"employee", "employees", "employer", "employment", "hr"}), "employment"),
    (frozenset({"genetic", "genetics", "genome", "genomics"}), "genetic"),
    (frozenset({"health", "healthcare", "medical", "medicine", "patient", "patients"}), "health"),
    (frozenset({"nationality", "nationalities", "citizenship"}), "nationality"),
    (frozenset({"nric", "identity_card"}), "nric"),
    (
        frozenset({"payroll", "salary", "salaries", "wage", "wages", "income", "bonus", "bonuses"}),
        "financial_income",
    ),
    (frozenset({"phone", "phones", "mobile", "mobiles", "tel", "telephone"}), "contact_phone"),
    (frozenset({"race", "races", "ethnicity", "ethnicities"}), "race"),
    (frozenset({"religion", "religions", "faith"}), "religion"),
)
_CREDENTIAL_EXCLUSION_TOKENS = frozenset(
    {"digest", "hash", "hashed", "jwt", "salt", "signature", "token"}
)
_DIRECT_CREDENTIAL_FIELDS = frozenset({"otp", "passcode", "password", "passwd", "pin"})
_CREDENTIAL_PHRASES = (
    "account_password",
    "login_password",
    "new_password",
    "one_time_password",
    "otp_code",
    "pass_code",
    "reset_code",
    "verification_code",
)


class _LLMCategoryPayload(BaseModel):
    model_config = ConfigDict(extra="forbid")

    categories: list[str] = Field(default_factory=list)


def classify_candidate_findings(
    findings: Sequence[CandidateFinding],
    *,
    route_patterns_by_finding_id: Mapping[str, str | None] | None = None,
    provider: LLMProvider | None = None,
    model: str | None = None,
) -> tuple[CandidateFinding, ...]:
    route_lookup = route_patterns_by_finding_id or {}
    return tuple(
        classify_candidate_finding(
            finding,
            route_pattern=route_lookup.get(finding.id),
            provider=provider,
            model=model,
        )
        for finding in findings
    )


def classify_candidate_finding(
    finding: CandidateFinding,
    *,
    route_pattern: str | None = None,
    provider: LLMProvider | None = None,
    model: str | None = None,
) -> CandidateFinding:
    categories = classify_source_data_categories(
        finding.source,
        route_pattern=route_pattern,
        provider=provider,
        model=model,
    )
    return finding.model_copy(
        update={
            "source": finding.source.model_copy(
                update={
                    "data_categories": categories,
                }
            )
        }
    )


def classify_source_data_categories(
    source: TaintSource,
    *,
    route_pattern: str | None = None,
    provider: LLMProvider | None = None,
    model: str | None = None,
) -> list[str]:
    field_name = _field_name_for_source(source)
    field_categories = classify_field_name_categories(field_name)
    route_categories = classify_route_context_categories(route_pattern, field_name=field_name)

    categories = list(field_categories)
    if route_categories:
        if categories and route_categories == ["name"]:
            pass
        else:
            categories = _merge_categories(categories, route_categories)

    if not categories and provider is not None:
        categories = _classify_with_llm(
            field_name=field_name,
            route_pattern=route_pattern,
            provider=provider,
            model=model,
        )

    return categories or [_UNKNOWN_CATEGORY]


def classify_field_name_categories(field_name: str | None) -> list[str]:
    normalized = _normalize_identifier(field_name)
    if not normalized:
        return []

    categories = list(classify_field(normalized))
    for aliases, category in _FIELD_ALIAS_CATEGORIES:
        if normalized in aliases:
            categories.append(category)

    if _looks_like_credentials_field(normalized):
        categories.append("credentials")

    return _dedupe_categories(categories)


def classify_route_context_categories(
    route_pattern: str | None,
    *,
    field_name: str | None = None,
) -> list[str]:
    normalized_route = _normalize_identifier(route_pattern)
    if not normalized_route:
        return []

    route_parts = [token for token in normalized_route.split("_") if token]
    route_tokens = set(route_parts)
    field_hint = _normalize_identifier(field_name)
    route_hint = "_".join(_singularize_route_token(token) for token in route_parts)
    categories = list(classify_field(route_hint))

    for aliases, category in _ROUTE_ALIAS_CATEGORIES:
        if route_tokens & aliases:
            categories.append(category)

    if (
        not categories
        and route_tokens & _GENERIC_PERSON_ROUTE_TOKENS
        and (not field_hint or field_hint in _GENERIC_IDENTIFIER_FIELDS)
    ):
        categories.append("name")

    return _dedupe_categories(categories)


def _classify_with_llm(
    *,
    field_name: str | None,
    route_pattern: str | None,
    provider: LLMProvider,
    model: str | None,
) -> list[str]:
    supported = ", ".join(supported_categories())
    response = provider.complete(
        stage="detector",
        model=model,
        messages=[
            {
                "role": "system",
                "content": (
                    "You classify likely personal-data categories for Project Piranesi. "
                    "Return strict JSON matching the response schema. "
                    "Only use supported categories from this taxonomy: "
                    f"{supported}. "
                    "Return an empty categories list when the field is unlikely "
                    "to be personal data or the context is too ambiguous."
                ),
            },
            {
                "role": "user",
                "content": (
                    "What type of personal data is likely stored in "
                    f"field '{field_name or '<unknown>'}' in context of "
                    f"'{route_pattern or '<unknown route>'}'? "
                    "Prefer the smallest plausible set of categories."
                ),
            },
        ],
        response_format=_LLMCategoryPayload,
        temperature=0.0,
        max_tokens=256,
    )

    payload = _parse_llm_payload(response.content)
    return _dedupe_categories(payload.categories)


def _parse_llm_payload(content: str) -> _LLMCategoryPayload:
    try:
        raw_payload = json.loads(content)
    except json.JSONDecodeError as exc:
        raise ValueError(f"invalid category-classification payload: {content!r}") from exc

    try:
        return _LLMCategoryPayload.model_validate(raw_payload)
    except ValidationError as exc:
        raise ValueError(f"invalid category-classification structure: {content!r}") from exc


def _field_name_for_source(source: TaintSource) -> str | None:
    if source.parameter_name:
        return source.parameter_name
    snippet_field = _extract_field_name(source.location.snippet)
    if snippet_field is not None:
        return snippet_field
    return _extract_field_name(source.source_type)


def _extract_field_name(value: str | None) -> str | None:
    normalized = _normalize_identifier(value)
    if not normalized:
        return None
    parts = [part for part in normalized.split("_") if part]
    if not parts:
        return None
    return parts[-1]


def _merge_categories(left: Sequence[str], right: Sequence[str]) -> list[str]:
    return _dedupe_categories([*left, *right])


def _dedupe_categories(categories: Sequence[str]) -> list[str]:
    deduped: list[str] = []
    seen: set[str] = set()
    for category in categories:
        normalized = category.strip().lower()
        if not normalized or normalized in seen:
            continue
        if normalized not in _SUPPORTED_CATEGORY_SET:
            continue
        deduped.append(normalized)
        seen.add(normalized)
    return deduped


def _looks_like_credentials_field(normalized: str) -> bool:
    tokens = {token for token in normalized.split("_") if token}
    if tokens & _CREDENTIAL_EXCLUSION_TOKENS:
        return False
    if normalized in _DIRECT_CREDENTIAL_FIELDS:
        return True
    if tokens & _DIRECT_CREDENTIAL_FIELDS:
        return True
    return any(phrase in normalized for phrase in _CREDENTIAL_PHRASES)


def _normalize_identifier(value: str | None) -> str:
    if value is None:
        return ""
    stripped = value.strip()
    if not stripped:
        return ""
    snake_case = _CAMEL_CASE_BOUNDARY.sub("_", stripped)
    lowered = snake_case.lower()
    collapsed = _NON_ALNUM.sub("_", lowered)
    return collapsed.strip("_")


def _singularize_route_token(token: str) -> str:
    if token.endswith("ies") and len(token) > 3:
        return f"{token[:-3]}y"
    if token.endswith("sses") or token.endswith("ics"):
        return token
    if token.endswith("s") and len(token) > 3:
        return token[:-1]
    return token


__all__ = [
    "classify_candidate_finding",
    "classify_candidate_findings",
    "classify_field_name_categories",
    "classify_route_context_categories",
    "classify_source_data_categories",
]
