# Incident Response Playbooks

## Scope
These playbooks cover scanner-side security and integrity incidents for Piranesi operation inside an organization.

## Severity Model
- `SEV-1`: active data exposure risk, integrity compromise of released evidence, or trusted advisory/rule corruption in production.
- `SEV-2`: high-confidence detection-quality regression with material security blind spot.
- `SEV-3`: contained reliability issue with no confirmed data/integrity impact.

## Playbook 1: Evidence Redaction Failure (`SEV-1`)
### Trigger
Any production audit event indicating unredacted evidence export, or manual confirmation that sensitive data was exported in `verify.json` or compliance bundles.

### Containment (0-30 min)
1. Stop all evidence exports for affected environment.
2. Rotate exposed credentials/secrets when exposure is plausible.
3. Preserve current artifacts and `audit-log.jsonl` in a read-only incident bucket.

### Eradication (30-180 min)
1. Identify failing redaction path by replaying target artifact generation in isolated staging.
2. Patch redaction logic and add regression tests.
3. Re-run `security-regression` lane plus targeted redaction tests.

### Recovery
1. Rebuild evidence bundles with `--redact` enabled.
2. Re-run rollout gates for affected tier.
3. Resume exports only after incident commander sign-off.

### Post-Incident
1. File root-cause report and MTTR metrics.
2. Add synthetic regression case to permanent suite.
3. Update runbook controls if detection lag exceeded SLO.

## Playbook 2: Trusted Advisory or Rule Integrity Compromise (`SEV-1`/`SEV-2`)
### Trigger
Signature validation failures, provenance mismatch, or unauthorized rule-pack changes in approved channels.

### Containment
1. Force ingestion to verified-only trust mode.
2. Freeze advisory/rule updates and block downstream promotion.
3. Snapshot current DB and signing metadata for forensics.

### Eradication
1. Rebuild advisory/rule source from trusted snapshots.
2. Rotate signing keys if compromise is suspected.
3. Verify manifests and signatures before re-enabling ingestion.

### Recovery
1. Execute trusted import workflow.
2. Run detector regression lane and benchmark comparison.
3. Re-enable pipeline promotion with governance approval.

## Playbook 3: Detection Blind Spot Regression (`SEV-2`)
### Trigger
Detection-rate drift breach, benchmark misses on previously detected classes, or high-confidence customer-reported false negatives.

### Containment
1. Freeze production policy/profile promotions.
2. Pin to last known-good rule/model profile in rollout config.
3. Open temporary coverage exception only with explicit risk acceptance.

### Eradication
1. Reproduce on ground-truth fixture.
2. Patch detector/rule logic.
3. Add regression fixture and threshold gate in eval pipeline.

### Recovery
1. Re-run `critical-regressions` and drift checks.
2. Validate production gate with comparison payload.
3. Lift freeze after owner + AppSec approval.

## On-Call Model
- Primary owner: Security Engineering (scanner platform).
- Secondary owner: Detection correctness owner.
- Tertiary owner: Governance/compliance owner.

Escalate immediately to legal/privacy if customer or regulated data may be exposed.

## Drill Cadence
1. Monthly table-top for one selected playbook.
2. Quarterly technical game day that runs full containment and recovery commands.
3. Annual cross-functional simulation with legal/compliance observers.

Each drill must generate:
- start/end timestamp,
- detected-vs-expected MTTR,
- command log,
- identified control gaps,
- follow-up owner and due date.
