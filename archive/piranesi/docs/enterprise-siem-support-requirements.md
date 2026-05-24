# Enterprise SIEM And Support Bundle Requirements

Date: 2026-05-20

Status: parked future work behind the enterprise demand gate.

Piranesi does not currently provide SIEM export or automated support bundle
generation. These requirements define what future work must preserve before any
implementation starts.

## Candidate SIEM Event Scope

Future SIEM export should consider summary events only, not raw evidence:

- workspace created, imported, exported, signed, and verified;
- report generated, delivery state changed, and handoff artifact created;
- finding imported, updated, closed, accepted-risk, or retest-annotated;
- evidence file added or redaction metadata changed;
- AI draft/suggestion created, accepted, rejected, or ignored;
- external handoff dry-run and live-send outcomes;
- identity, role, support access, retention, and configuration changes if
  enterprise identity is implemented.

Each event must include schema version, timestamp, actor when available, action,
target identifier, result, workspace/project identifier, and relevant digest or
trace IDs. Raw evidence, request/response bodies, screenshots, transcripts,
payloads, secrets, client names, and hostnames must be omitted or redacted by
default.

## Export Requirements

A future SIEM implementation must define:

- supported format: JSONL, syslog, webhook, vendor API, or file drop;
- delivery mode: dry-run, local file export, push, or pull;
- retry and failure behavior;
- rate limits and backpressure behavior;
- event ordering and idempotency key;
- customer-managed retention and deletion behavior;
- redaction defaults and any operator override gates;
- how exported events map back to audit-log entries and chain-of-custody records.

## Support Bundle Scope

Future support bundles should include diagnostic metadata only:

- Piranesi version and platform information;
- command summaries and non-sensitive error traces;
- workspace schema versions and artifact counts;
- validation failures;
- report/signature verification summaries;
- sanitized configuration and enabled feature flags.

Support bundles must not include raw evidence, report bodies, source artifacts,
AI prompt/response text, secrets, tokens, webhook URLs, client names, hostnames,
or proprietary snippets unless an operator explicitly approves a narrowly scoped
attachment.

## Approval Gates

Before exporting SIEM events or support bundles, future workflows must require:

- dry-run preview;
- visible redaction summary;
- explicit destination or output path;
- operator approval for live send or bundle creation;
- audit-log entry containing destination type, redaction mode, output digest, and
  actor when available.

## Deferral Rule

Do not create implementation issues until
[`enterprise-demand-gate.md`](enterprise-demand-gate.md) is satisfied. SIEM and
support bundle work must start with a threat model that covers redaction failure,
over-retention, support access, accidental evidence disclosure, and export
tampering.
