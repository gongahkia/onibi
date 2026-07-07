# Zig Parity Notes

Issue #30 ports Rust-side parity coverage into Zig tests while keeping the legacy Rust tests in `packages/pi-agent/tests`.

## Ported Coverage

- `policy_sync.rs`: Zig now tests policy pack rotation persistence, pull payload state, and audit events in `app/policy.zig`.
- `scan_scope.rs`: Zig tests scope matching, approval expiry, scanner sandbox argv, and scanner raw/normalized persistence across `app/scope.zig` and `app/scanner.zig`.
- `acceptance_manifest.rs`: Zig signs and verifies acceptance artifact manifests and detects tampering in `app/acceptance.zig`.
- `storage_quota.rs`: Zig tests quota floor evaluation and scan refusal audit behavior in `app/quota.zig` and `app/scanner.zig`.
- `outbox_replay.rs`: Zig queues replay envelopes in stable order, emits them once, and moves them to `outbox/sent` in `app/outbox.zig`.

## Intentionally Dropped Rust-Only Assertions

- `policy_sync.rs` daemon polling assertions are not ported because the Zig pivot has no long-running control-plane daemon; Zig covers the persisted policy state and pull payload used by that daemon path.
- `storage_quota.rs` `normalize` and `upload accept` command assertions are not ported because those Rust commands do not exist in the Zig pivot; Zig covers the shared quota decision and scan gate.
- Rust wire-envelope signature assertions are retained in the legacy Rust suite until full wire protocol cutover; Zig parity covers local persisted artifacts and signed acceptance/bundle manifests.
