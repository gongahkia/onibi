# AI Operator-Control Policy

Date: 2026-05-20

Status: accepted for Phase 6 implementation.

## Policy

Piranesi may use AI only as a local-first drafting and suggestion aid. AI output
must never be treated as confirmed evidence, must never create findings by itself,
and must never interact with targets, tools, scanners, payloads, or infrastructure.

## Allowed Uses

- Draft remediation wording for existing findings.
- Draft executive summary text from existing report facts.
- Suggest deduplication, severity rationale, retest checklist, or reviewer
  checklist text.
- Explain existing workspace evidence to an operator when the prompt is redacted
  and traceable.

## Prohibited Uses

- Autonomous testing, exploitation, scanning, payload generation, or target
  interaction.
- Creating findings, evidence, hosts, clients, assets, or source references that
  do not already exist in the workspace.
- Sending unredacted request/response bodies, secrets, hostnames, client names,
  proprietary snippets, or raw evidence to an external provider.
- Applying AI-authored report text without explicit human approval.

## Provider Boundaries

- Cloud providers require explicit bring-your-own-key configuration.
- External model calls are disabled when privacy mode is enabled.
- Local model providers must use the same prompt redaction, trace logging, and
  approval gates as cloud providers.
- Provider identity, model identity, privacy mode, and external-call status must
  be visible in AI trace records.

## Redaction Boundary

Every prompt payload must pass through the redaction-before-prompt contract before
it reaches any provider. Prompt construction must use redacted workspace facts,
approved report fields, and evidence identifiers. It must not pass raw evidence
directly to provider code.

The implementation contract is documented in
[`ai-redaction.md`](ai-redaction.md).

## Approval Boundary

AI-authored report text starts as a draft. A draft may be accepted, rejected, or
ignored by an operator. Until accepted, it must not alter normalized findings,
report artifacts, evidence records, source references, or chain-of-custody
manifests.

## Traceability

Piranesi must record a redacted prompt payload, provider metadata, response
metadata, approval state, and target field for every AI draft or suggestion. Trace
records must be inspectable and must avoid storing unredacted secrets or client
data.

## Implementation Order

1. Redaction-before-prompt contract and tests.
2. Trace log and approval-state model.
3. BYOK cloud provider configuration and external-call kill switch.
4. Local provider interface.
5. Privacy and hallucination evaluation suite.
6. Human-approved draft modes for remediation, executive summaries, and
   suggestions.
