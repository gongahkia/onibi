from __future__ import annotations

from piranesi.report.cwe import cwe_reporting_descriptor, cwe_title, extract_cwe_id


def test_cwe_metadata_normalizes_common_finding_references() -> None:
    assert extract_cwe_id("nuclei:CWE-200") == "CWE-200"
    assert cwe_title("CWE-79") == "Cross-Site Scripting"

    descriptor = cwe_reporting_descriptor("CWE-89")
    assert descriptor["id"] == "CWE-89"
    assert descriptor["shortDescription"] == {"text": "SQL Injection"}
