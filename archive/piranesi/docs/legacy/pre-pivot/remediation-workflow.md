> Legacy note: This document describes pre-pivot or roadmap behavior. It is retained for historical context and is not current Phase 1 guidance. Current guidance is in README.md, docs/ARCHITECTURE.md, and the pentest workspace/report/retest/sign/serve docs.

# Remediation Workflow

Piranesi remediation workflows are review-only. They turn host posture reports
into closure plans and before/after comparisons, but they do not mutate hosts,
restart services, install packages, or generate configuration-management scripts.

## Generate A Plan

```bash
piranesi remediate plan piranesi-output/host-report.json \
  --output piranesi-output/remediation-plan.md
```

This writes:

```text
piranesi-output/
  remediation-plan.md
  remediation-plan.json
```

Each action includes:

- prioritized action title
- related finding IDs
- owner placeholder
- estimated effort
- risk reduction estimate
- verification command
- rollback considerations
- dependencies between fixes

## Render A Checklist

```bash
piranesi remediate checklist piranesi-output/host-report.json --format markdown
```

The checklist groups actions by finding category and is suitable for tickets or
runbooks.

## Compare Before And After

After remediation work, collect and assess the host again:

```bash
piranesi collect --output piranesi-evidence-after
piranesi assess piranesi-evidence-after --output piranesi-output-after
```

Then compare the two reports:

```bash
piranesi host diff \
  piranesi-output-before/host-report.json \
  piranesi-output-after/host-report.json
```

Diff classifications:

- `new`: present only in the after report
- `fixed`: present only in the before report
- `changed`: matched finding changed severity, title, risk, category, confidence,
  or remediation text
- `unchanged`: matched finding stayed materially the same
- `suppressed`: finding is suppressed in either report

Matching uses stable finding IDs first. If IDs change, Piranesi falls back to
rule ID, instance key, target, and affected component.

## Verify Closure

```bash
piranesi remediate verify \
  piranesi-output-before/host-report.json \
  piranesi-output-after/host-report.json
```

Verification passes when there are no new or changed findings. Fixed findings are
reported as closure progress. Suppressed findings are tracked separately so teams
can distinguish accepted risk from actual remediation.

## Safety

Piranesi does not auto-remediate. Treat generated plans as operator checklists:
review the proposed action, confirm owner and maintenance window, record rollback
state, make the change through your normal process, then re-run collection and
assessment.
