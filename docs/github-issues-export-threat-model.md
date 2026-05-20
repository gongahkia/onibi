# GitHub Issues Export Threat Model

Status: accepted implementation gate for one-way GitHub Issues export.

## Scope

Piranesi may export selected local findings to GitHub Issues as a one-way handoff
workflow. The integration creates or previews GitHub issue payloads from already
normalized workspace findings. It does not sync issue state back into the
workspace, edit local findings from GitHub comments, run scanners, or attach raw
evidence files.

## Assets At Risk

- GitHub tokens, repository identifiers, and destination issue URLs.
- Client names, project names, target hostnames, URLs, IP addresses, and paths.
- Request/response snippets, command output, payloads, screenshots, transcripts,
  and proprietary code fragments referenced by findings.
- Local chain-of-custody metadata, source digests, and raw artifact paths.

## Trust Boundaries

- The local workspace is trusted only as local operator-owned state.
- GitHub is an external system. Everything sent to it must be treated as leaving
  the local-first evidence boundary.
- GitHub issue bodies are not an evidence store. They may contain summarized
  finding context, not raw evidence or secret-bearing snippets.
- Authentication material comes from the operator environment or GitHub CLI. It is
  never written into workspace files, reports, logs, or exported payloads.

## Allowed Payload Data

Default export payloads may include:

- Finding ID, title, severity, confidence, and status.
- Redacted description and remediation text.
- Redacted affected asset labels when the operator enables asset inclusion.
- Source tool names and source digests, without raw local file contents.
- A local-only provenance note telling the operator which Piranesi finding created
  the issue.

Default export payloads must not include:

- Raw request or response evidence.
- Secret values, credentials, tokens, cookies, session identifiers, or private keys.
- Screenshot or transcript bodies.
- Raw local filesystem paths outside stable Piranesi relative artifact references.
- Client identifiers when privacy mode or `--redact-assets` is enabled.

## Redaction Requirements

- Export uses deny-by-default evidence handling: evidence snippets are omitted
  unless a future explicit option is added with its own review.
- Asset values are redacted by default in dry-run examples and can be included only
  through an explicit CLI option.
- The issue body marks exported content as a summary, not verified external
  evidence.
- Dry-run output is available and must use the same redaction path as live export.
- Tests must cover redacted assets, omitted evidence, dry-run output, and generated
  labels.

## Auth And Token Handling

- Piranesi does not store GitHub tokens.
- Live export requires an explicit repository target and an authenticated operator
  environment.
- Token lookup is delegated to `gh`/environment configuration in implementation
  code; tokens are never printed.
- Partial failure records may include the destination repository and finding IDs,
  but not credentials or raw evidence.

## Rate Limits, Retries, And Failure Behavior

- The exporter should create issues one finding at a time and return a structured
  result for each attempted finding.
- 4xx authentication, authorization, validation, and repository errors are
  non-retryable.
- 429 and 5xx responses may be retried with bounded backoff by future live
  clients; the initial implementation may fail closed with a clear message.
- Partial success is reported explicitly. Already-created issue URLs are returned
  so the operator can reconcile manually.
- Dry-run mode must never call GitHub.

## Non-Goals

- Bidirectional synchronization.
- GitHub comment ingestion.
- Issue status to finding status mapping.
- Raw evidence upload or attachment.
- Autonomous triage, exploitation, payload generation, or target interaction.
