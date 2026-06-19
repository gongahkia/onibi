# Changes

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
