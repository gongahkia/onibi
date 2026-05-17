# Track D Wave 1 Quality Artifacts (2026-04-19)

This directory captures the automated quality outputs generated after the Track D wave-1 fixture and metadata expansion.

## Files
1. `coverage_gap.json`: output of `eval/coverage_gap_report.py` (`--min-count 8 --plan-top-n 15`)
2. `corpus_quality.json`: output of `eval/corpus_quality.py` (`--min-count 8`)
3. `metadata_audit.json`: output of `eval/ground_truth_audit.py` for key metadata fields
4. `benchmark_mapping.json`: output of `eval/benchmark_mapping_report.py`

## Snapshot Highlights
1. `runnable_ratio`: `0.820`
2. `coverage gap rows`: `60`
3. `field_sensitive_label`: `436 present / 176 missing`
4. `taint_field_path`: `442 present / 170 missing`
5. benchmark mapping status: `mapped=5`, `planned=8`

## Regeneration
```bash
mkdir -p eval/reports/track_d_wave1_2026-04-19
python3 eval/coverage_gap_report.py --json --min-count 8 --plan-top-n 15 > eval/reports/track_d_wave1_2026-04-19/coverage_gap.json
python3 eval/corpus_quality.py --json --min-count 8 > eval/reports/track_d_wave1_2026-04-19/corpus_quality.json
python3 eval/ground_truth_audit.py --json --field line_numbers --field taint_step_count --field taint_field_path --field field_sensitive_label > eval/reports/track_d_wave1_2026-04-19/metadata_audit.json
python3 eval/benchmark_mapping_report.py --json > eval/reports/track_d_wave1_2026-04-19/benchmark_mapping.json
```
