# Nessus Ingest

`piranesi ingest nessus` imports an operator-supplied Nessus `.nessus` XML export into a local
workspace. Piranesi does not run Nessus, authenticate to scanners, or start active scans.

```bash
uv run piranesi ingest nessus \
  --input scan-results.nessus \
  --workspace ./workspace
```

The original export is copied under `raw/nessus/`, and normalized findings are written to
`normalized/findings.json`. Each finding preserves the source digest, raw path, host/plugin locator,
plugin ID, plugin family, risk metadata, affected host/service, CVE/CWE identifiers, references,
and redacted plugin output.

Supported input shape:

- Nessus `NessusClientData_v2` XML with `ReportHost` and `ReportItem` records.

Malformed `ReportItem` records produce parser warnings when other valid records can still be
imported. Active scanning, scan scheduling, and authenticated scanner orchestration are out of
scope for this command.
