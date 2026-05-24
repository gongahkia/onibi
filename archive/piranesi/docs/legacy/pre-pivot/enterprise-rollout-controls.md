> Legacy note: This document describes pre-pivot or roadmap behavior. It is retained for historical context and is not current Phase 1 guidance. Current guidance is in README.md, docs/ARCHITECTURE.md, and the pentest workspace/report/retest/sign/serve docs.

# Enterprise Rollout Controls

## Purpose
This document defines the mandatory operational controls for promoting Piranesi usage from development to production.

## Rollout Tiers

| Tier | Primary Use | Mandatory Controls | Blocking Gate |
| --- | --- | --- | --- |
| `dev` | local development and feature hardening | authorized usage acknowledgement, deterministic CI lanes green, baseline artifact generation (`scan.json`, `detect.json`, `report.json`) | `python scripts/check_rollout_gates.py --tier dev --artifacts-dir piranesi-output` |
| `staging` | pre-production validation against representative systems | all `dev` controls plus verification artifact generation (`verify.json`, `legal.json`), suppression lifecycle hygiene (no invalid/expired suppressions), compare-reports drift checks | `python scripts/check_rollout_gates.py --tier staging --artifacts-dir piranesi-output --comparison-json eval/compare-reports.json` |
| `prod` | organizational production reporting and compliance evidence workflows | all `staging` controls plus zero active critical findings, stale suppression ban, redaction-quality integrity, auditable policy overrides, auditable evidence exports | `python scripts/check_rollout_gates.py --tier prod --artifacts-dir piranesi-output --comparison-json eval/compare-reports.json` |

Production rollout is prohibited when any required gate fails.

## Environment Policy Profiles
Use `[rollout.policy_profiles]` in `piranesi.toml` to lock verification and LLM controls per environment.

```toml
[rollout]
environment = "prod"
policy_profile = "prod_strict"

[rollout.policy_profiles.dev_flexible]
verify_proof_mode = "unsafe"
max_cost_usd = 10.0
max_tokens = 500000
allowed_models = ["gpt-4o-mini", "gpt-4o"]
suppression_fail_on_invalid = true

[rollout.policy_profiles.staging_guardrails]
verify_proof_mode = "safe"
max_cost_usd = 4.0
max_tokens = 250000
allowed_models = ["gpt-4o-mini", "gpt-4o"]
suppression_fail_on_invalid = true
suppression_fail_on_expired = true

[rollout.policy_profiles.prod_strict]
verify_proof_mode = "safe"
max_cost_usd = 2.5
max_tokens = 100000
trace_log_prompts = false
allowed_models = ["gpt-4o-mini"]
suppression_fail_on_invalid = true
suppression_fail_on_expired = true
suppression_fail_on_stale = true
```

If configured models violate `allowed_models`, configuration load fails.

## Artifact Retention And Access Controls

| Artifact Class | Paths | Minimum Retention | Access Scope | Control Notes |
| --- | --- | --- | --- | --- |
| Core scan artifacts | `scan.json`, `detect.json`, `report.json` | 90 days | engineering + appsec | baseline release evidence and regression root-cause data |
| Verification and legal artifacts | `verify.json`, `legal.json`, evidence bundles | 180 days | appsec + legal/compliance | sensitive by default; export only from approved runners |
| Audit trail | `audit-log.jsonl` | 365 days | security governance + auditors | contains policy overrides, suppression actions, and evidence export events |
| Drift comparison outputs | `eval/compare-reports.json` | 180 days | detector owners + appsec | used to prove detection/FP drift control |

Retention may be extended by legal hold or active incident response.

## Release-Readiness Checklist
1. Required CI lanes are green (`core-required`, `critical-regressions`, `security-regression`, `extras-contract`).
2. Staging/prod runs include `verify.json`, `legal.json`, and `report.json`.
3. Suppression lifecycle is clean for target tier (`invalid=0`, `expired=0`, and for prod `stale=0`).
4. Drift comparison is generated and meets tier thresholds.
5. Audit log is present and includes policy/suppression/evidence events for the release run.
6. Production rollout gate script returns exit code `0`.

## Operational Commands
```bash
# 1) Run full pipeline for release candidate
piranesi run . --authorized --yes --output piranesi-output

# 2) Generate compliance bundle with redaction enabled
piranesi compliance bundle --framework all --artifacts-dir piranesi-output --output piranesi-output/compliance --redact

# 3) Compare benchmark drift against baseline snapshot
piranesi eval compare-reports \
  --baseline-report eval/history/baseline.json \
  --current-report eval/history/current.json \
  --json > eval/compare-reports.json

# 4) Enforce production rollout gate
python scripts/check_rollout_gates.py \
  --tier prod \
  --artifacts-dir piranesi-output \
  --comparison-json eval/compare-reports.json
```
