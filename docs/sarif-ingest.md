# SARIF Ingest

`piranesi ingest sarif` imports an operator-supplied SARIF 2.1.0 JSON export into a local workspace.
Piranesi treats SARIF as an interchange format and does not run the source scanner.

```bash
uv run piranesi ingest sarif \
  --input findings.sarif.json \
  --workspace ./workspace
```

The original export is copied under `raw/sarif/`, and normalized findings are written to
`normalized/findings.json`. Each finding preserves the source digest, raw path, run/result locator,
source tool name, rule ID, primary artifact location, CWE/CVE tags, references, and result message
evidence.

Supported input shape:

- SARIF `version: "2.1.0"` with `runs[].tool.driver.rules[]` and `runs[].results[]`.

Severity is mapped from SARIF `properties.security-severity` when present, then from result or rule
levels. Unsupported runs or malformed results produce parser warnings when other valid results can
still be imported.
