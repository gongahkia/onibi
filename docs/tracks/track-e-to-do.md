# Track E TODO: Advisory And Supply-Chain Trust Strengthening

## Goal
Improve integrity and trust of advisory data and downstream prioritization decisions.

## Priority
P2.

## Work Items
1. Add signed snapshot verification for advisory imports.
2. Persist provenance metadata for source digests/signatures.
3. Introduce policy mode for `verified-only` advisory ingestion.
4. Add explicit stale/missing/unsigned policy enforcement options.
5. Improve risk scoring integration for CVSS v4, EPSS v4, and KEV inputs with clear precedence rules.
6. Add docs for offline and air-gapped trusted workflows.

## Deliverables
1. Advisory DB schema/extensions for provenance metadata.
2. CLI/API switches for trust policy.
3. Tests for signed/unsigned/stale DB behavior.

## Acceptance Criteria
1. Unsigned or tampered snapshots are rejected in strict mode.
2. Policy outcomes are deterministic and clearly reported.
3. Backward-compatible path retained for non-strict users.

## Metrics
1. Advisory trust-policy compliance rate in CI.
2. Number of scans using verified advisory context.

## Status
- [ ] Planned
- [ ] In progress
- [ ] Completed
