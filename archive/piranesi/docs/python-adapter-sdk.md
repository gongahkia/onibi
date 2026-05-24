# Python Adapter SDK v0

The Python adapter SDK helps third-party importers emit valid Piranesi Finding
Format (PFF) v0 documents without depending on internal workspace files.

```python
from piranesi.adapter_sdk import PffAdapterBuilder, evidence_snippet, source_reference

builder = PffAdapterBuilder(
    producer_name="example-adapter",
    producer_version="0.1.0",
)
builder.add_finding(
    finding_id="finding:example",
    title="Example imported finding",
    severity="medium",
    confidence="tool-observed",
    asset="app.example.test",
    evidence=[
        evidence_snippet(
            kind="tool-output",
            value="status=500 token=secret-value",
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
    provenance={"adapter": "example-adapter"},
)
builder.write_json("findings.pff.json")
```

Guardrails:

- `producer_name` and `producer_version` are required.
- Every finding must include at least one source reference.
- Evidence helpers redact token-like secrets even when an adapter asks not to
  redact optional non-secret text.
- Generated documents are validated against `piranesi.pff.v0` before writing.
- The SDK emits PFF documents only; it does not execute plugins, run tools, or
  mutate a workspace directly.
