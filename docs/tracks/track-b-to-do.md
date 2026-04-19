# Track B TODO: Detection Correctness And Signal Quality

## Goal
Restore and improve precision/recall stability for critical vulnerability classes and framework flows.

## Priority
P1. Start after Track A begins; run in parallel with Track C.

## Work Items
1. Fix interprocedural wrapper sink promotion regressions.
2. Fix expression-arrow wrapper detection misses.
3. Correct wrapper location metadata consistency.
4. Constrain alias/prototype/source scanning scope to target-relevant files.
5. Add regression fixtures for SQL helper wrappers and framework routing variants.
6. Add explicit precision guardrails for known noisy sink patterns.
7. Ensure known-limitations registry and report output stay synchronized.

## Deliverables
1. Code fixes in detector modules (`flows`, `interprocedural`, source scan).
2. Updated known-limitations handling where schema drift exists.
3. Expanded targeted tests in `tests/test_detect/*`.

## Acceptance Criteria
1. Existing detection regression tests for wrappers/flows pass.
2. False-positive inflation regression is corrected.
3. NodeGoat and vuln-express documented expectations remain truthful and stable.

## Metrics
1. Detection-rate and FP-suppression-rate trend dashboards show no regression.
2. Per-framework precision floor established and enforced by CI gates.

## Status
- [x] Planned
- [x] In progress
- [x] Completed
