# Provider Capability Milestones

Status: Telegram beta evidence policy. This document does not certify an unverified live result.

Onibi's product remains single-owner. Telegram is a first-class beta chat cockpit; its remaining live-device evidence is unverified. It cannot add a browser owner, bypass pairing for the web cockpit, or expand the personal-owner trust boundary.

## Independent capability rows

Future provider work is assessed per capability. Evidence for one row does not imply another row, provider parity, or production support.

| capability | required local evidence | required promotion evidence |
| --- | --- | --- |
| notification delivery | bounded/redacted payload, provider error, retry, and rate-limit fixture tests | live delivery result |
| inbound text | owner binding, non-owner rejection, replay/cursor handling, and PTY-routing tests | live owner-text result |
| approvals | request binding, idempotent approve/deny, supported-edit validation, and forgery tests | live decision result |
| E2EE | documented trust/key-storage design and encrypted fixture tests | device-verification and live encrypted-message result |
| lifecycle | cancellation, reconnect, backoff, restart, and recovery tests | live reconnect/restart result |
| audit | payload-hash, redaction, and failure-path tests | reviewed live artifact with no raw sensitive payloads |
| device verification | enrollment, identity-change, revocation, and impersonation tests | live owner-device verification result |

## Promotion gate

Every future provider issue must identify its capability rows, local tests, secret-gated live path, privacy disclosure, and owner-binding model. No generic profile, shared interface, or test result can promote another provider or capability.

Provider work must fail closed when identity, encryption, or required decision semantics are unavailable. If a provider lacks a documented native blocking boundary or safe edit mapping, Onibi must report that limitation instead of claiming approval parity.

## Current boundary

Telegram is the only supported chat transport. Its exact capabilities and remaining live evidence are documented in [Transports](./transports.md) and [Telegram Chat Cockpit](./telegram.md). Other provider transports are not latent compatibility promises.
