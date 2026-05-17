> Legacy note: This document describes pre-pivot or roadmap behavior. It is retained for historical context and is not current Phase 1 guidance. Current guidance is in README.md, docs/ARCHITECTURE.md, and the pentest workspace/report/retest/sign/serve docs.

# Evaluation Harness

This document covers the two evaluation utilities used to keep fixture quality and detection coverage measurable:

- `eval/ground_truth_audit.py`: metadata quality auditing for `eval/ground_truth`.
- `eval/ground_truth_enrich.py`: deterministic metadata backfill for sparse ground-truth fields.
- `eval/coverage_gap_report.py`: ranking report for under-covered fixture slices.
- `eval/validate_all.py`: fixture execution and coverage scoring against ground truth.
- `eval/compare_reports.py`: diff and regression summary between two `validate_all` reports.

## Ground Truth Metadata Audit

Audit the full ground-truth set:

```bash
python3 eval/ground_truth_audit.py --gt-dir eval/ground_truth
```

Emit JSON and fail when required fields are missing:

```bash
python3 eval/ground_truth_audit.py \
  --gt-dir eval/ground_truth \
  --required-field discovery_method \
  --required-field language \
  --fail-on-missing \
  --json
```

Scope the audit to a subset:

```bash
python3 eval/ground_truth_audit.py \
  --gt-dir eval/ground_truth \
  --filter discovery_method=synthetic \
  --required-field language \
  --required-field framework \
  --fail-on-missing
```

### Notes

- `--filter` accepts repeatable `key=value` pairs.
- `--required-field` is a strict gate for CI.
- `--field` lets you audit additional metadata columns.

## Ground Truth Metadata Enrichment

Preview inferred updates without writing files:

```bash
python3 eval/ground_truth_enrich.py \
  --gt-dir eval/ground_truth \
  --json
```

Persist inferred fields:

```bash
python3 eval/ground_truth_enrich.py \
  --gt-dir eval/ground_truth \
  --field language \
  --field framework \
  --field taint_step_count \
  --field taint_field_path \
  --write
```

Use as a CI guardrail (dry-run + fail if unresolved values remain):

```bash
python3 eval/ground_truth_enrich.py \
  --gt-dir eval/ground_truth \
  --field language \
  --field framework \
  --field taint_step_count \
  --fail-on-unresolved
```

Use `--fail-on-updates` to enforce that metadata is already committed:

```bash
python3 eval/ground_truth_enrich.py \
  --gt-dir eval/ground_truth \
  --field taint_field_path \
  --taint-field-candidates-only \
  --fail-on-unresolved \
  --fail-on-updates
```

### Notes

- `--taint-field-candidates-only` scopes unresolved checks to entries where `taint_field_path` can be inferred from explicit field-access syntax.
- `--fail-on-updates` exits with code `2` when dry-run enrichment would modify any entry.

## Coverage Gap Planning

Find highest-priority slices to expand:

```bash
python3 eval/coverage_gap_report.py \
  --gt-dir eval/ground_truth \
  --dimension cwe+language \
  --dimension cwe+framework \
  --min-count 8
```

JSON output for automation:

```bash
python3 eval/coverage_gap_report.py \
  --gt-dir eval/ground_truth \
  --dimension cwe+language \
  --filter discovery_method=synthetic \
  --json
```

## Batch Fixture Validation

Run detection validation across the full set:

```bash
python3 eval/validate_all.py \
  --gt-dir eval/ground_truth \
  --output /tmp/piranesi-validate-all.json
```

`validate_all` writes history snapshots by default to `eval/history`:

- `validate-all-<UTC timestamp>.json`
- `latest.json` (overwritten on each run)
- `index.json` (rolling snapshot index with key overall metrics)

Control snapshot behavior:

```bash
python3 eval/validate_all.py \
  --gt-dir eval/ground_truth \
  --history-dir /tmp/piranesi-history \
  --history-label release-rc1
```

Disable snapshot writing:

```bash
python3 eval/validate_all.py --gt-dir eval/ground_truth --no-history
```

Global threshold gates:

```bash
python3 eval/validate_all.py \
  --gt-dir eval/ground_truth \
  --min-detection-rate 0.80 \
  --min-fp-rate 0.70
```

Per-group metrics and thresholds:

```bash
python3 eval/validate_all.py \
  --gt-dir eval/ground_truth \
  --group-by language \
  --group-by framework \
  --min-group-detection-rate language=typescript:0.85 \
  --min-group-detection-rate framework=express:0.90 \
  --min-group-fp-rate framework=express:0.75
```

Compare against a previous run and gate on deltas:

```bash
python3 eval/validate_all.py \
  --gt-dir eval/ground_truth \
  --group-by language \
  --baseline-report /tmp/piranesi-validate-all-baseline.json \
  --min-detection-rate-delta -0.01 \
  --min-fp-rate-delta -0.02 \
  --min-group-detection-delta language=typescript:-0.01 \
  --min-group-fp-delta language=typescript:-0.02
```

### Exit Codes

- `0`: all configured thresholds passed.
- `1`: overall detection rate below `--min-detection-rate`.
- `2`: overall FP suppression rate below `--min-fp-rate`.
- `3`: one or more `--min-group-detection-rate` thresholds failed.
- `4`: one or more `--min-group-fp-rate` thresholds failed.
- `5`: overall detection-rate delta below `--min-detection-rate-delta`.
- `6`: overall FP-suppression-rate delta below `--min-fp-rate-delta`.
- `7`: one or more `--min-group-detection-delta` thresholds failed.
- `8`: one or more `--min-group-fp-delta` thresholds failed.

## Report Comparison

Compare two saved `validate_all` reports:

```bash
python3 eval/compare_reports.py \
  --baseline-report /tmp/piranesi-validate-all-baseline.json \
  --current-report /tmp/piranesi-validate-all-current.json
```

Compare latest two history snapshots automatically:

```bash
python3 eval/compare_reports.py --history-dir eval/history
```

Write a PR-friendly markdown summary:

```bash
python3 eval/compare_reports.py \
  --baseline-report /tmp/piranesi-validate-all-baseline.json \
  --current-report /tmp/piranesi-validate-all-current.json \
  --markdown-output /tmp/piranesi-validate-all-comparison.md
```

JSON output with delta thresholds:

```bash
python3 eval/compare_reports.py \
  --baseline-report /tmp/piranesi-validate-all-baseline.json \
  --current-report /tmp/piranesi-validate-all-current.json \
  --json \
  --min-detection-rate-delta -0.01 \
  --min-group-detection-delta language=typescript:-0.02
```
