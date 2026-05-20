from __future__ import annotations

from pathlib import Path

from piranesi.adapter_sdk import (
    AdapterSdkError,
    PffAdapterBuilder,
    evidence_snippet,
    source_reference,
)
from piranesi.pff import load_and_validate_pff_file, validate_pff_document


def test_adapter_sdk_builds_valid_pff_and_redacts_secret_evidence(tmp_path: Path) -> None:
    builder = PffAdapterBuilder(
        producer_name="example-adapter",
        producer_version="0.1.0",
        engagement={"client": "Example"},
    )
    builder.add_finding(
        finding_id="finding:adapter-sdk",
        title="Adapter SDK finding",
        severity="medium",
        asset="app.example.test",
        evidence=[
            evidence_snippet(
                kind="tool-output",
                value="status=500 token=secret-value",
                locator="results[0]",
                redact=False,
            )
        ],
        source_references=[
            source_reference(
                tool="example-tool",
                input_sha256="0" * 64,
                raw_path="raw/example-tool/results.json",
                locator="results[0]",
            )
        ],
        provenance={"tool": "example-tool"},
    )

    output = builder.write_json(tmp_path / "findings.pff.json")
    document = load_and_validate_pff_file(output)

    validate_pff_document(document)
    finding = document["findings"][0]
    assert finding["source_references"][0]["tool"] == "example-tool"
    assert finding["provenance"]["adapter_sdk"] is True
    assert finding["evidence"][0]["redacted"] is True
    assert "secret-value" not in finding["evidence"][0]["value"]


def test_adapter_sdk_requires_source_references() -> None:
    builder = PffAdapterBuilder(producer_name="example", producer_version="0.1.0")

    try:
        builder.add_finding(
            finding_id="finding:no-source",
            title="Missing source",
            source_references=[],
        )
    except AdapterSdkError as exc:
        assert "source_references are required" in str(exc)
    else:
        raise AssertionError("expected AdapterSdkError")


def test_adapter_sdk_example_writes_valid_document(tmp_path: Path) -> None:
    from examples.pff_adapter_sdk_example import build_example

    output = build_example(tmp_path / "example.pff.json")
    document = load_and_validate_pff_file(output)

    assert document["producer"]["name"] == "example-structural-adapter"
    assert document["findings"][0]["evidence"][0]["redacted"] is True
