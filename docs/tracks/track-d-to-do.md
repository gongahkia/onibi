# Track D TODO: Ground Truth And Coverage Expansion Program

## Goal
Improve generalization confidence by expanding sparse CWE/language/framework slices with balanced TP/FP coverage.

## Priority
P1/P2 continuous program.

## Work Items
1. Prioritize sparse slices using `eval/coverage_gap_report.py`.
2. Add first-wave fixtures for top 10 sparse slices with explicit TP/FP targets.
3. Increase replayable entries by converting manual-only entries into runnable fixtures when possible.
4. Improve metadata consistency (`line_numbers`, `taint_step_count`, `taint_field_path`).
5. Add field-sensitive labels beyond current narrow phase coverage.
6. Add external benchmark mapping strategy (OWASP Benchmark, NIST Juliet where relevant).
7. Define corpus quality SLOs and CI thresholds.

## Deliverables
1. New fixtures in `eval/fixtures/*`.
2. New/updated GT entries in `eval/ground_truth/*`.
3. Automated quality reports committed per cycle.

## Acceptance Criteria
1. High-priority sparse slices hit minimum counts and TP/FP balance targets.
2. Runnable fixture coverage ratio increases from current baseline.
3. Metadata audit shows zero critical consistency failures.

## Metrics
1. Gap count by dimension over time.
2. TP/FP ratio quality by slice.
3. Percentage of runnable GT entries.

## Status
- [x] Planned
- [x] In progress
- [ ] Completed
