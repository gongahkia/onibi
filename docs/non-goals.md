# Non-Goals

Piranesi is a local-first red-team engagement workspace for preserving evidence,
normalizing findings, producing reports, comparing retests, and signing local
deliverables. It is not positioned as a scanner, scheduler, C2 platform, hosted
portal, or autonomous compliance engine.

## Phase 1 Boundary

The current Phase 1 command surface is intentionally small:

```text
piranesi evidence
piranesi ingest
piranesi report
piranesi rescan
piranesi retest
piranesi sign
piranesi serve
```

`rescan` is an opt-in replay path for scanner evidence that was already manually
executed, ingested, and preserved in a baseline workspace. Normal workflows remain
import-only by default, and the default install does not require Docker or the
optional rescan runtime dependencies.

## Out Of Scope

- Hosted SaaS, authentication, teams, client portals, or multi-tenant service
  operation.
- New scanner engines, autonomous scanning, scheduled scanning, autonomous target
  selection, or unsupervised target interaction.
- Replay that expands beyond the targets, tools, and command shape recovered from
  original ingested evidence.
- New network egress outside the original ingested engagement scope.
- C2 operation, implant management, live session control, payload execution, or
  active exploitation.
- Payload generation, exploit generation, autonomous exploitation, or AI-driven
  target interaction.
- AI-driven decisioning that creates findings, changes evidence, confirms risk, or
  alters report output without explicit human approval.
- Fleet management, live SSH probing, compliance framework automation, or compliance
  certification claims.

Future roadmap items may introduce some adjacent workflows, but they must be tracked
by separate GitHub issues, threat modeled where appropriate, and completed behind
their own acceptance criteria before becoming public product guidance.
