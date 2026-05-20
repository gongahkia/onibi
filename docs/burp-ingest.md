# Burp Suite Pro Ingestion

`piranesi ingest burp` imports Burp Suite Pro Issues XML into a local workspace:

```bash
piranesi ingest burp --input issues.xml --workspace ./workspace
```

The adapter is import-only. It does not call Burp APIs, proxy traffic, crawl targets,
or run active testing.

## What Is Preserved

- The original XML export is copied under `raw/burp/`.
- Each normalized finding keeps a source reference with the raw path, input SHA-256,
  Burp serial number, type, host, path, location, severity, and confidence.
- Request and response evidence from `<requestresponse>` entries is decoded when
  Burp marks it as base64 and stored as redacted evidence snippets.
- CWE IDs and references are extracted from Burp classification/background fields
  where present.

Imported Burp findings use Piranesi confidence `tool-observed`. Burp's original
confidence, such as `Certain` or `Firm`, is preserved in provenance metadata rather
than treated as manual verification by Piranesi.

## Fixture Policy

Tests use sanitized Burp Suite Pro Issues XML from an authorized local lab target.
New Burp fixtures must include provenance, sanitization notes, and secret review in
`tests/fixtures/pentest/provenance.json`.
