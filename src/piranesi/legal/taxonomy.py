from __future__ import annotations

import re

_CAMEL_CASE_BOUNDARY = re.compile(r"(?<=[a-z0-9])(?=[A-Z])")
_NON_ALNUM = re.compile(r"[^a-z0-9]+")

_CATEGORY_TIERS: dict[str, int] = {
    "nric": 1,
    "fin": 1,
    "biometric": 1,
    "genetic": 1,
    "health": 1,
    "credentials": 2,
    "financial": 2,
    "financial_bank": 2,
    "financial_credit_card": 2,
    "financial_income": 2,
    "employment": 2,
    "criminal": 2,
    "contact": 3,
    "contact_phone": 3,
    "contact_email": 3,
    "contact_address": 3,
    "dob": 3,
    "nationality": 3,
    "race": 3,
    "religion": 3,
    "name": 4,
    "username": 4,
    "public": 4,
    "public_info": 4,
}


def classify_field(field_name: str) -> list[str]:
    normalized = _normalize_field_name(field_name)
    if not normalized:
        return []

    tokens = set(normalized.split("_"))
    categories: list[str] = []

    def add(category: str) -> None:
        if category not in categories:
            categories.append(category)

    if _has_token(tokens, "nric") or _contains_phrase(
        normalized,
        "ic_no",
        "ic_number",
        "identity_card",
    ):
        add("nric")
    if _has_token(tokens, "fin") or _contains_phrase(normalized, "fin_no", "fin_number"):
        add("fin")
    if _has_token(tokens, "biometric", "fingerprint", "iris", "retina") or _contains_phrase(
        normalized,
        "face_data",
        "face_scan",
        "facial_scan",
        "voiceprint",
    ):
        add("biometric")
    if _has_token(tokens, "genetic", "dna", "genome", "genomic"):
        add("genetic")
    if _has_token(
        tokens,
        "health",
        "medical",
        "diagnosis",
        "patient",
        "allergy",
        "prescription",
    ) or _contains_phrase(
        normalized,
        "medical_record",
        "lab_result",
        "health_record",
    ):
        add("health")

    if _has_token(tokens, "bank", "iban", "swift", "routing") or _contains_phrase(
        normalized,
        "bank_account",
        "account_number",
        "account_no",
    ):
        add("financial_bank")
    if _has_token(tokens, "cvv", "cvc", "pan") or _contains_phrase(
        normalized,
        "credit_card",
        "debit_card",
        "card_number",
        "card_no",
    ):
        add("financial_credit_card")
    if _has_token(
        tokens,
        "salary",
        "income",
        "payroll",
        "wage",
        "bonus",
        "compensation",
    ) or _contains_phrase(
        normalized,
        "annual_income",
    ):
        add("financial_income")
    if _has_token(tokens, "employee", "employment", "employer", "resume", "cv") or _contains_phrase(
        normalized,
        "job_title",
        "performance_review",
        "hr_record",
    ):
        add("employment")
    if _has_token(
        tokens,
        "criminal",
        "conviction",
        "arrest",
        "offence",
        "offense",
        "charge",
        "police",
    ):
        add("criminal")

    if _has_token(tokens, "phone", "mobile", "telephone", "tel", "cell") or _contains_phrase(
        normalized,
        "phone_number",
        "mobile_number",
        "contact_number",
    ):
        add("contact_phone")
    if _has_token(tokens, "email") or _contains_phrase(
        normalized,
        "email_address",
        "e_mail",
    ):
        add("contact_email")
    if _has_token(tokens, "address", "postal", "postcode", "zipcode", "zip") or _contains_phrase(
        normalized,
        "postal_code",
        "zip_code",
        "mailing_address",
    ):
        add("contact_address")
    if _has_token(tokens, "dob", "birthday") or _contains_phrase(
        normalized,
        "date_of_birth",
        "birth_date",
    ):
        add("dob")
    if _has_token(tokens, "nationality", "citizenship") or _contains_phrase(
        normalized,
        "country_of_citizenship",
    ):
        add("nationality")
    if _has_token(tokens, "race", "ethnicity", "ethnic"):
        add("race")
    if _has_token(tokens, "religion", "faith", "belief"):
        add("religion")

    username_detected = _has_token(tokens, "username", "handle") or _contains_phrase(
        normalized,
        "user_name",
        "screen_name",
        "login_name",
    )
    if username_detected:
        add("username")
    if not username_detected and (
        _has_token(tokens, "name")
        or _contains_phrase(
            normalized,
            "full_name",
            "first_name",
            "last_name",
            "given_name",
            "family_name",
            "display_name",
        )
    ):
        add("name")
    if _has_token(tokens, "public") or _contains_phrase(
        normalized,
        "public_profile",
        "profile_url",
        "social_profile",
        "public_bio",
    ):
        add("public_info")

    return categories


def tier_for_category(category: str) -> int:
    normalized = category.strip().lower()
    try:
        return _CATEGORY_TIERS[normalized]
    except KeyError as exc:
        raise ValueError(f"unknown personal data category: {category}") from exc


def supported_categories() -> tuple[str, ...]:
    return tuple(_CATEGORY_TIERS)


def _normalize_field_name(field_name: str) -> str:
    stripped = field_name.strip()
    if not stripped:
        return ""
    snake = _CAMEL_CASE_BOUNDARY.sub("_", stripped)
    lowered = snake.lower()
    collapsed = _NON_ALNUM.sub("_", lowered)
    return collapsed.strip("_")


def _has_token(tokens: set[str], *needles: str) -> bool:
    return any(needle in tokens for needle in needles)


def _contains_phrase(normalized: str, *phrases: str) -> bool:
    return any(phrase in normalized for phrase in phrases)


__all__ = ["classify_field", "supported_categories", "tier_for_category"]
