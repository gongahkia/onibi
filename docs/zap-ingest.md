# OWASP ZAP Ingest

`piranesi ingest zap` imports an operator-supplied OWASP ZAP JSON alert or report export into a
local workspace. Piranesi does not run ZAP, crawl targets, or start active scans.

```bash
uv run piranesi ingest zap \
  --input zap-report.json \
  --workspace ./workspace
```

The original export is copied under `raw/zap/`, and normalized findings are written to
`normalized/findings.json`. Each finding preserves the source digest, raw path, alert locator,
plugin/alert IDs, risk metadata, affected URI instances, references, CWE IDs, and redacted alert
evidence.

Supported input shape:

- ZAP JSON reports with top-level `site[].alerts[]` records.
- ZAP JSON alert arrays with top-level `alerts[]` records.

Unsupported or malformed alert records produce parser warnings when other valid records can still
be imported. Active scanning, authenticated target orchestration, and target interaction are out of
scope for this command.
