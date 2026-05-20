# Capabilities

Piranesi `v0.2.0` is an alpha local-first red-team engagement workspace. This page
describes the current Phase 1 surface only. Historical host-posture and source-code
workflows are legacy context and are not current guidance.

Known limitations are tracked in [`docs/known-limitations.json`](known-limitations.json).
Current product boundaries are tracked in [`docs/non-goals.md`](non-goals.md).

| Capability | Status | Notes |
| --- | --- | --- |
| Workspace contract | Alpha | `workspace.json`, `normalized/findings.json`, `raw/`, `reports/`, `signatures/`, and `audit-log.jsonl`. |
| Real fixture provenance | Alpha | Parser fixtures require provenance, sanitization notes, real tool output, and secret scanning. |
| nmap XML ingest | Alpha | Imports open services and script output from real nmap XML. |
| nuclei JSONL ingest | Alpha | Imports real nuclei JSONL, including template metadata, references, CWE/CVE IDs, matched URLs, extractors, and redacted request/response evidence. |
| Burp Suite Pro Issues XML ingest | Alpha | Imports real Burp Issues XML, including severity, confidence, host/path/location, CWE/reference metadata, and redacted request/response evidence. |
| OWASP ZAP JSON ingest | Alpha | Imports ZAP JSON alerts, including risk/confidence metadata, affected URI instances, CWE/reference metadata, and redacted alert evidence. |
| Nessus `.nessus` ingest | Alpha | Imports Nessus XML report items, including plugin metadata, host/service context, CVE/CWE/reference metadata, and redacted plugin output. |
| SARIF ingest | Alpha | Imports SARIF 2.1.0 results, including tool/rule metadata, artifact locations, CWE/CVE tags, references, and result evidence. |
| ffuf JSON ingest | Alpha | Imports ffuf discovery results, including URL, status, redirect, and response-size metadata as informational evidence. |
| sqlmap artifact ingest | Alpha | Imports sqlmap JSON summaries and basic text logs, including target, parameter, DBMS, CWE-89, references, and redacted payload evidence. |
| Metasploit JSON ingest | Alpha | Imports exported vulnerability, loot, and session evidence without live session control or payload execution. |
| Neutral C2 JSONL import | Alpha | Preserves local C2-style logs as evidence and appends safe summaries to the engagement timeline without live C2 access. |
| Operator evidence vault | Alpha | CLI and browser UI preserve notes, screenshots, transcripts, C2 logs, payload metadata, detection artifacts, scanner exports, and other local artifacts. |
| Solo engagement management | Alpha | Local client/project/scope metadata, milestones, retest rounds, delivery status, reviewer, and reviewer notes. |
| Pentest report rendering | Alpha | JSON, Markdown, WeasyPrint PDF, and ReportLab PDF. |
| Red-team handoff rendering | Alpha | JSON, Markdown, PDF, and archive ZIP with report artifacts plus workspace indexes; raw evidence is opt-in. |
| Local report template library | Alpha | Operator-owned methodology, remediation, and custom section text can be selected at report render time. |
| GitHub Issues handoff | Alpha | One-way selected finding export with dry-run preview, redacted assets by default, and no bidirectional status sync. |
| Slack notifications | Alpha | Summary-only workflow notifications with dry-run default, redacted engagement labels, and no raw evidence. |
| Email handoff drafts | Alpha | Local `.eml` draft generation for report delivery without sending mail. |
| Chain of custody | Alpha | Local manifest creation and verification for workspace artifacts and audit chain. |
| Piranesi Finding Format v0 | Alpha | Public finding interchange with JSON Schema, validation, workspace export/import, fixture validation, and versioning rules. |
| Python adapter SDK v0 | Alpha | Helpers for third-party adapters to emit valid PFF documents without mutating workspaces directly. |
| Plugin security boundary | Accepted design | Out-of-process, validation-first plugin model; executable plugin runtime and registry mechanics remain deferred. |
| Opt-in rescan replay | Alpha | `piranesi rescan --from-baseline` recovers supported nmap/nuclei commands from baseline evidence, requires optional runtime support and digest-pinned images, and writes raw outputs for existing ingest commands. |
| Retest lifecycle diff | Alpha | `new`, `open`, `closed`, `changed`, `regressed`, and `ambiguous` classifications. |
| Local web app | Alpha | `piranesi serve --workspace`; loopback by default, engagement setup, guided empty states, note capture, evidence file upload, and workspace/report preview. |
| AI drafting and suggestions | Alpha | Redacted, traceable, operator-approved remediation drafts, executive summary drafts, and non-mutating dedupe/severity/retest suggestions. |

## Roadmap, Not Current Capability

These areas are tracked by GitHub issues but should not be presented as implemented
features until their own acceptance criteria are complete:

- Additional long-tail adapter expansion beyond the implemented Phase 1.1 set;
  see [`deferred-adapter-expansion.md`](deferred-adapter-expansion.md) for the
  Phase 1.2 evidence gate.
- Live C2 operation, implant management, payload execution, and active exploitation.
- Autonomous scanning, scheduled scanning, AI-driven target interaction, payload
  generation, and replay beyond original ingested scope.
- Vendor-specific C2 adapters beyond the neutral local JSONL format.
- OS/runtime-enforced network egress allowlisting for replay containers. Current
  rescan derives and validates baseline scope, then requires explicit acknowledgement
  when Docker default networking is used without portable egress enforcement.
- Engagement-scale design-partner feedback loops.
- Audit evidence packaging and assessor-ready bundles.
- Workflow integrations beyond the current one-way GitHub Issues, Slack, and
  email handoff paths.
- Linear and Jira implementations until design-partner evidence justifies them.
- Enterprise deployment.
- Go/TypeScript adapter SDKs, marketplace or registry mechanics, and executable
  plugin runtime.
- Autonomous AI-driven target interaction, payload generation, scanner operation,
  finding creation, evidence creation, or report changes without explicit human
  approval.

## Quality Gates

Phase 1 changes should run:

```bash
uv run python scripts/validate_pentest_fixtures.py
uv run ruff check src/ tests/
uv run ruff format --check src/ tests/
uv run mypy src/piranesi/
uv run pytest -q -m "not integration and not joern and not docker and not e2e and not slow"
```
