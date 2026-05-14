# TODO 16: Add Remediation Planning And Change Tracking

## Goal

Move Piranesi from "reports problems" to "helps teams close problems" while
remaining safe and review-first.

Wide adoption depends on clear remediation workflows, owner assignment, progress
tracking, and before/after comparison.

## Current State

Host findings include remediation text and stable IDs. Suppressions exist, but
there is no remediation plan artifact, owner workflow, or host-posture baseline
diff equivalent focused on closure.

## Desired CLI

Add:

```bash
piranesi remediate plan piranesi-output/host-report.json --output remediation-plan.md
piranesi remediate checklist piranesi-output/host-report.json --format markdown
piranesi host diff before/host-report.json after/host-report.json
piranesi remediate verify before/host-report.json after/host-report.json
```

## Remediation Plan

Generate:

```text
remediation-plan.json
remediation-plan.md
```

Include:

- Prioritized actions.
- Related finding IDs.
- Owner placeholder.
- Estimated effort.
- Risk reduction estimate.
- Verification command.
- Rollback considerations.
- Dependencies between fixes.

## Change Tracking

Host diff should classify findings:

- `new`
- `fixed`
- `changed`
- `unchanged`
- `suppressed`

Use stable finding IDs and fallback matching by rule ID, instance key, target, and
affected component.

## Safety Requirements

- Do not auto-remediate by default.
- If future auto-remediation is added, require explicit `--apply` and a generated
  patch/script preview.
- Keep remediation text evidence-bound and conservative.

## Tests

Add tests for:

- Remediation plan generation from vulnerable fixture.
- Diff detects fixed and unchanged findings.
- Finding ID fallback matching works across title changes.
- Markdown checklist renders stable action groups.
- Suppressed findings are tracked separately.

## Documentation

Add:

```text
docs/remediation-workflow.md
```

Update host docs with before/after examples.

## Acceptance Criteria

- Users can generate a clear remediation plan from a report.
- Users can compare before/after reports to show progress.
- Plans are review-friendly and do not mutate hosts.
- Diff artifacts are stable enough for CI and tickets.

## Out Of Scope

- Automatic remediation execution.
- Configuration management tool generation.
- Service restarts or package upgrades.

## Validation Commands

```bash
uv run pytest tests/test_host_remediation.py tests/test_host_posture.py
uv run piranesi remediate plan /tmp/piranesi-out/host-report.json --output /tmp/remediation-plan.md
```

