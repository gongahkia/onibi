# Track C TODO: CI, Test Architecture, And Release Reliability

## Goal
Establish deterministic, policy-driven CI lanes and reduce environment-caused noise in release confidence.

## Priority
P1.

## Work Items
1. Split CI into lanes:
- `core-required`
- `integration-optional`
- `extras-contract`
2. Add capability preflight checks for Joern, Docker, optional deps, and runtime constraints.
3. Mark environment-bound tests with explicit markers and run only in matching lanes.
4. Add security regression lane for redaction and artifact hygiene.
5. Add schema/contract snapshots for report/plugin/CLI contracts.
6. Introduce merge gates for critical regression categories, not only total pass count.

## Deliverables
1. Updated `.github/workflows/ci.yml`.
2. Marker and preflight utilities in test tooling.
3. Release checklist and CI lane ownership documentation.

## Acceptance Criteria
1. Core lane deterministic and consistently green.
2. Optional/integration failures do not mask core regressions.
3. Contract drift triggers clear, actionable failures.

## Metrics
1. Core lane flake rate below agreed threshold.
2. Mean time to classify failures (product vs env) reduced.

## Status
- [x] Planned
- [x] In progress
- [x] Completed
