# PHASE 6: Integrations And Delivery Adapters

## Goal

Build integrations through adapter interfaces first, with mocked credentials and test doubles before real provider auth.

Primary result delivery is email. WhatsApp and Telegram are secondary push channels for time-sensitive alerts.

## Adapter Model

Adapters expose external systems to KelpClaw without leaking provider-specific code into OpenClaw, NanoClaw, or workflow-spec.

Initial adapter families:

- Gmail trigger and receipt search.
- Sheets append, update, and lookup.
- Email approval and result delivery.
- WhatsApp push alert.
- Telegram push alert.

Each adapter must define:

- Adapter id and version.
- Supported operations.
- Input and output schemas.
- Required secrets.
- Network policy.
- Rate-limit and retry behavior.
- Test double implementation.
- Fixture payloads.

## Delivery Rules

- Email is the default channel for approvals, summaries, and final results.
- WhatsApp and Telegram are opt-in secondary channels.
- Time-sensitive alerts can use WhatsApp or Telegram only when the workflow declares the channel.
- Delivery nodes must record provider response ids when real providers are used.
- Mock delivery must produce deterministic response ids for tests.

## Credentials And Secrets

Phase 6 does not require real production OAuth or provider setup.

The first implementation uses:

- Mock credentials for local tests.
- Secret references in workflow specs, not raw secret values.
- Adapter-specific validation that fails clearly when a real provider is requested without credentials.
- A future-compatible shape for OAuth tokens and provider keys.

## API Contracts

Adapter execution receives:

- Operation name.
- Versioned input payload.
- Secret reference map.
- Runtime context with workflow id, node id, run id, and retry attempt.

Adapter execution returns:

- Operation status.
- Structured output payload.
- Provider metadata.
- Retryable or non-retryable error details.
- Audit events.

## Implementation Checkpoints

1. Define adapter interface in TypeScript.
2. Implement deterministic test doubles for Gmail, Sheets, email, WhatsApp, and Telegram.
3. Add fixture payloads for receipt extraction into Sheets.
4. Wire adapter declarations into skill registry records.
5. Add NanoClaw network policy enforcement based on adapter requirements.
6. Add OpenClaw UI fields for selecting delivery channels and adapter-backed skills.
7. Add provider credential validation stubs.
8. Defer real OAuth and production provider credentials to a later hardening pass.

## Tests

- Unit tests for every adapter interface implementation.
- Contract tests using fixture payloads.
- NanoClaw integration tests using mocked adapters only.
- Delivery tests proving email is default and WhatsApp/Telegram are opt-in.
- Missing-secret tests with stable, user-facing validation errors.
- E2E mocked workflow: Gmail receipts to Sheets with email result delivery.

## Acceptance Criteria

- KelpClaw can run integration-heavy workflows locally without real credentials.
- Adapter behavior is testable and deterministic with mocks.
- Workflow specs reference secrets safely.
- Email delivery is available as the primary result path.
- WhatsApp and Telegram are modeled as secondary push channels without blocking the core workflow path.
