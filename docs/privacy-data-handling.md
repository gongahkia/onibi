# Privacy And Data Handling

Piranesi Phase 1 is local-first and import-only. It does not run active scans, upload
workspace data, or call external services as part of `ingest`, `report`, `retest`,
`sign`, or `serve`.

## Local Artifacts

| Data | Location | Notes |
| --- | --- | --- |
| Raw tool exports | `workspace/raw/<tool>/` | Copied input files; parsers do not mutate raw evidence. |
| Normalized findings | `workspace/normalized/findings.json` | Deterministic report-ready findings. |
| Audit log | `workspace/audit-log.jsonl` | Append-only command events with input/output digests. |
| Reports | `workspace/reports/` or requested output directory | JSON, Markdown, or PDF artifacts. |
| Signatures | `workspace/signatures/` | Chain-of-custody manifests. |

## Evidence Redaction

Adapters can mark evidence snippets as sensitive. nuclei request, response, and curl
command evidence is retained but marked redacted by default. Report rendering hides
redacted evidence unless the operator explicitly requests sensitive evidence:

```bash
piranesi report --workspace ./workspace --include-sensitive-evidence
```

## Local Preview

`piranesi serve` binds to `127.0.0.1` by default. Non-loopback binds require
`--unsafe-bind` and print a warning. The server exposes fixed report-preview routes
only and does not serve arbitrary workspace paths.

## External Network Calls

The current Phase 1 commands do not perform external model calls, ticket creation,
host collection, or hosted synchronization. Future integrations must document auth,
rate limits, redaction behavior, and data egress before becoming current guidance.
