from __future__ import annotations

from pathlib import Path

from piranesi.adapter_sdk import PffAdapterBuilder, evidence_snippet, source_reference


def build_example(output_path: Path) -> Path:
    builder = PffAdapterBuilder(
        producer_name="example-structural-adapter",
        producer_version="0.1.0",
        engagement={"client": "Example", "project": "Adapter SDK"},
    )
    builder.add_finding(
        finding_id="finding:example-structural-adapter",
        title="Example structural adapter finding",
        severity="low",
        confidence="tool-observed",
        asset="app.example.test",
        description="Minimal structural example for SDK users.",
        evidence=[
            evidence_snippet(
                kind="tool-output",
                value="observed status=500 token=example-secret",
                locator="results[0]",
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
        provenance={"example": True},
    )
    return builder.write_json(output_path)


if __name__ == "__main__":
    build_example(Path("example-findings.pff.json"))
