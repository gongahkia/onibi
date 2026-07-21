# Provider Capability Milestones

Status: deferred. This document is a planning and evidence policy, not a supported-provider list or promotion claim.

Onibi's supported product remains the single-owner local web cockpit. Telegram and IRC remain explicit `experimental.providers=true` opt-ins; neither is certified or enabled by default. No third-party provider can add a browser owner, bypass pairing, or expand the personal-owner trust boundary.

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

Every future provider issue must identify its capability rows, local tests, secret-gated live path, privacy disclosure, and owner-binding model. It remains experimental until an explicit product/security review accepts those results. No generic profile, shared interface, or test result can promote another provider or capability.

Provider work must fail closed when identity, encryption, or required decision semantics are unavailable. If a provider lacks a documented native blocking boundary or safe edit mapping, Onibi must report that limitation instead of claiming approval parity.

## Current boundary

The only deferred provider transports are `telegram` and `irc`; both require `experimental.providers=true`. Their exact capabilities and remaining live evidence are documented in [Transports](./transports.md), with Telegram's detail in [Telegram Chat Cockpit](./telegram.md). Other removed provider transports are not latent compatibility promises.
