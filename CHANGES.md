# Changes

## Local-Only Pi Pivot

- Added a Zig `kelp-pi` scaffold with policy checks, scope files, approval tokens, dry-run scanner gating, index ingest, ask, bundle assemble/verify, model manifest checks, chat shell, and Ed25519 key generation.
- Added `policies/appsec-agent-baseline.toml`, ported from the TypeScript `appsec-agent-baseline` rules.
- Added `models/manifest.toml` with the selected Qwen3 0.6B Q4_K_M GGUF URL and an explicit SHA-256 fill-in gate.
- Rewrote pivot-facing docs around a Pi-resident runtime: no cloud APIs, no laptop control plane, no provider table, and no Ollama daemon.
- Kept TypeScript and Rust packages as legacy parity references until Zig reaches feature parity.

## Kelp Pi Target Introduction

- Added Kelp Pi design docs and task tracking for a Raspberry Pi 5 AppSec field unit.
- Added the Rust `kelp-pi-agent` foundation with data-dir checks, audit logging, key generation, signed envelopes, retrieval chunking, `/ask`, and policy primitives.
- Added README coverage for the Pi target with a control-plane/Pi-data-plane diagram.

## AppSec Harness Pivot

- Repositioned KelpClaw as a reproducible AppSec agent harness.
- Added `kelp-claw appsec audit` for Dockerfile build metadata, passive scanner evidence import, scoped triage assistant execution, SARIF output, and signed audit bundles.
- Added `appsec-agent-baseline` policy pack.
- Removed the Find Evil/SIFT vertical package, commands, examples, fixtures, and docs.
- Updated top-level docs and CI naming toward AppSec evidence handoff.
