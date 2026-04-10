from __future__ import annotations

import pytest

from piranesi.legal.taxonomy import classify_field, supported_categories, tier_for_category


def test_classify_field_matches_expected_categories() -> None:
    assert classify_field("nric_number") == ["nric"]
    assert classify_field("creditCardNumber") == ["financial_credit_card"]
    assert classify_field("employee_salary") == ["financial_income", "employment"]
    assert classify_field("contactEmail") == ["contact_email"]
    assert classify_field("user_name") == ["username"]
    assert classify_field("public_profile_url") == ["public_info"]


def test_classify_field_matches_gdpr_special_categories() -> None:
    assert classify_field("political_opinion") == ["political"]
    assert classify_field("trade_union_membership") == ["trade_union"]
    assert classify_field("sexual_orientation") == ["sexual_orientation"]


def test_classify_field_ignores_non_personal_secret_fields() -> None:
    assert classify_field("session_token") == []
    assert classify_field("password_hash") == []


def test_tier_for_category_supports_detailed_and_alias_categories() -> None:
    assert tier_for_category("health") == 1
    assert tier_for_category("political") == 1
    assert tier_for_category("trade_union") == 1
    assert tier_for_category("sexual_orientation") == 1
    assert tier_for_category("credentials") == 2
    assert tier_for_category("financial_credit_card") == 2
    assert tier_for_category("financial") == 2
    assert tier_for_category("contact_email") == 3
    assert tier_for_category("public") == 4


def test_supported_categories_include_gdpr_special_categories() -> None:
    categories = supported_categories()

    assert "political" in categories
    assert "trade_union" in categories
    assert "sexual_orientation" in categories


def test_tier_for_category_rejects_unknown_categories() -> None:
    with pytest.raises(ValueError, match="unknown personal data category"):
        tier_for_category("credential")
