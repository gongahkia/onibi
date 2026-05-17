# Capabilities

Piranesi `v0.2.0` is an alpha pentest report engine. This page describes the
current Phase 1 surface only. Historical host-posture and source-code workflows are
legacy context and are not current guidance.

Known limitations are tracked in [`docs/known-limitations.json`](known-limitations.json).

| Capability | Status | Notes |
| --- | --- | --- |
| Workspace contract | Alpha | `workspace.json`, `normalized/findings.json`, `raw/`, `reports/`, `signatures/`, and `audit-log.jsonl`. |
| Real fixture provenance | Alpha | Parser fixtures require provenance, sanitization notes, real tool output, and secret scanning. |
| nmap XML ingest | Alpha | Imports open services and script output from real nmap XML. |
| nuclei JSONL ingest | Alpha | Imports real nuclei JSONL, including template metadata, references, CWE/CVE IDs, matched URLs, extractors, and redacted request/response evidence. |
| Report rendering | Alpha | JSON, Markdown, WeasyPrint PDF, and ReportLab PDF. |
| Chain of custody | Alpha | Local manifest creation and verification for workspace artifacts and audit chain. |
| Retest lifecycle diff | Alpha | `new`, `open`, `closed`, `changed`, `regressed`, and `ambiguous` classifications. |
| Local preview UI | Alpha | `piranesi serve --workspace`; loopback by default, explicit unsafe acknowledgement for non-loopback binds. |

## Roadmap, Not Current Capability

These areas are tracked by GitHub issues but should not be presented as implemented
features until their own acceptance criteria are complete:

- Burp Suite Pro Issues XML ingestion.
- ZAP, Nessus, ffuf, sqlmap, SARIF, Metasploit, and other adapter expansion.
- Engagement-scale validation and design-partner feedback loops.
- Audit evidence packaging and assessor-ready bundles.
- Workflow integrations, PFF SDKs, enterprise deployment, and AI assistance.

## Quality Gates

Phase 1 changes should run:

```bash
uv run python scripts/validate_pentest_fixtures.py
uv run ruff check src/ tests/
uv run ruff format --check src/ tests/
uv run mypy src/piranesi/
uv run pytest -q -m "not integration and not joern and not docker and not e2e and not slow"
```
