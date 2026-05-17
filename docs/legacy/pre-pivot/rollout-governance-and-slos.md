# Rollout Governance And SLOs

## Governance Model

## Ownership Matrix
| Control Area | Primary Owner | Secondary Owner | Approval Requirement |
| --- | --- | --- | --- |
| Rule updates and rule-pack promotion | Detection Engineering | Security Engineering | detector owner + security owner |
| Suppression creation and expiry | Service team owner | AppSec partner | service owner + AppSec approval for long-lived suppressions |
| Rollout policy profile changes | Security Engineering | Platform Engineering | change-review ticket + approver |
| Evidence export workflows | Compliance Engineering | Security Engineering | compliance owner |
| Release gate thresholds | Security Governance | Detection Engineering | governance committee sign-off |

## Governance Cadence
1. Weekly: suppression lifecycle review (`expired`, `stale`, and ownerless suppressions).
2. Bi-weekly: detector drift review (benchmark deltas and regression failures).
3. Monthly: rollout profile review (model allowlists, budget ceilings, proof-mode policy).
4. Quarterly: incident and playbook exercise review with MTTR analysis.

## Mandatory Audit Sources
- `piranesi-output/audit-log.jsonl`
- CI lane history (`core-required`, `critical-regressions`, `security-regression`, `extras-contract`)
- drift reports from `piranesi eval compare-reports --json`
- suppression validation outputs from `piranesi suppressions validate`

## SLOs And Error Budgets

| SLO | Target | Breach Condition |
| --- | --- | --- |
| Release gate pass-rate | `>= 98%` of release candidates pass required gates on first attempt | below target in rolling 30-day window |
| Detection drift | overall detection-rate delta `>= -0.01` for staging, `>= 0.00` for prod | threshold failure in release window |
| FP drift | FP-suppression delta `>= -0.01` for staging, `>= 0.00` for prod | threshold failure in release window |
| Redaction quality | `100%` rich-evidence entries include valid redaction metadata and no unredacted prod exports | any prod redaction-quality gate failure |

## SLI Computation
1. Release gate pass-rate:
   - numerator: successful `check_rollout_gates.py` runs.
   - denominator: total release-candidate gate runs.
2. Detection/FP drift:
   - source: `comparison.overall.detection_rate.delta` and `comparison.overall.fp_suppression_rate.delta` from compare-reports payload.
3. Redaction quality:
   - source: `verify.json` redaction metadata checks and audit-log export events.

## Operational Queries
```bash
# Validate suppression lifecycle and capture audit trail
piranesi suppressions validate --project-root . --findings piranesi-output/detect.json --json

# Generate drift comparison
piranesi eval compare-reports \
  --baseline-report eval/history/baseline.json \
  --current-report eval/history/current.json \
  --json > eval/compare-reports.json

# Evaluate rollout gate and SLO indicators for production
python scripts/check_rollout_gates.py \
  --tier prod \
  --artifacts-dir piranesi-output \
  --comparison-json eval/compare-reports.json \
  --json
```

## Escalation Policy
1. Any `SEV-1` incident immediately consumes full quarterly error budget for redaction-quality SLO.
2. Two consecutive drift breaches block profile promotion until corrective action and revalidation complete.
3. Repeated manual policy overrides require governance review and stricter profile defaults.
