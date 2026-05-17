# External Benchmark Mapping Strategy

## Goal
Integrate industry benchmark suites into the Piranesi ground-truth expansion loop while preserving replayability and TP/FP label quality.

## Benchmarks In Scope
1. `OWASP Benchmark` (Java servlet-focused vulnerability test corpus)
2. `NIST Juliet` (language-specific synthetic weakness suites)

## Mapping Contract
Every benchmark import candidate should be represented in `eval/benchmarks/mapping_matrix.yaml` with:

1. `benchmark`
2. `benchmark_case_id`
3. `cwe_id`
4. `language`
5. `framework`
6. `coverage_status` (`planned`, `mapped`, `deferred`)
7. `mapped_ground_truth_ids`
8. `notes`

## Operational Flow
1. Add/refresh benchmark rows in `eval/benchmarks/mapping_matrix.yaml`.
2. Generate a status report:
   - `python3 eval/benchmark_mapping_report.py`
3. Enforce minimum mapped progress in CI:
   - `python3 eval/benchmark_mapping_report.py --require-mapped-entries <N> --require-mapped-ground-truth-ids <M>`
4. Convert benchmark case candidates into replayable fixtures under `eval/cve_fixtures` or `eval/fixtures`.
5. Add paired TP/FP ground-truth entries with `taint_field_path` and `field_sensitive_label`.
6. Re-run:
   - `python3 eval/coverage_gap_report.py`
   - `python3 eval/corpus_quality.py`

## Quality Rules
1. No benchmark case should be marked `mapped` without at least one replayable fixture.
2. `mapped_ground_truth_ids` should include both TP and FP where the benchmark has good/bad variants.
3. CWE/framework mapping should prioritize slices that remain sparse in Track D reports.
4. Defer benchmark rows only with a reason in `notes`.

## Track D Alignment
Track D wave-1 sparse-slice entries (`gt-508` to `gt-517`) are seeded into the matrix as `internal_seed` mappings, so the same reporting/gating path can be used before full OWASP/Juliet ingestion lands.
