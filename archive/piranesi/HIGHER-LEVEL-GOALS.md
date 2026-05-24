# Higher-Level Goals

Date: 2026-05-20

Status: supplementary product direction for post-v0.2.0 work.

Piranesi v0.2.0 is complete for its current alpha scope: a local-first
red-team engagement workspace that preserves evidence, normalizes findings,
renders reports, supports handoff artifacts, and signs local deliverables.

The next value step is not "more report generation." The higher-level goal is
to make Piranesi the system that turns red-team activity into defender-usable
evidence, detection gaps, retest plans, and business-facing outcomes.

## Product Thesis

Piranesi should become the local evidence-to-detection handoff layer for
authorized offensive work:

- red teamers preserve what happened without losing provenance;
- blue teamers receive concrete detection opportunities and expected telemetry;
- clients receive outcomes, prevented actions, gaps, and prioritized fixes;
- every claim remains linked to local evidence and chain-of-custody metadata.

This keeps the product aligned with the current non-goals: no live C2, no
autonomous exploitation, no hosted portal, no scanner orchestration, and no
unapproved AI-driven report mutation.

## Strategic Pillars

1. Detection value over finding volume.
   A red-team report should answer what defenders should have seen, what they
   did see, and what detection or response improvement follows.

2. Measurable events over vague activity.
   Intentional test actions should record expected defender activity, observed
   response, and evidence.

3. Handoff quality over artifact dumping.
   Piranesi should package ATT&CK mapping, evidence, IOCs, detection notes,
   timeline, retest guidance, and signatures into reviewable bundles.

4. Attack paths over isolated facts.
   When real gated evidence exists, graph and operator artifacts should explain
   how objectives became reachable, not just list raw tool output.

5. Operator debrief over memory.
   What worked, failed, made noise, got blocked, or exposed a control strength
   should be captured before the engagement context disappears.

6. Delivery QA over manual review.
   Reports and handoffs should fail checks when evidence, redaction, mappings,
   or retest guidance are missing.

7. Client outcomes over tool output.
   The final view should summarize objectives, impact, control gaps, prevented
   actions, and prioritized remediation in a form decision-makers can use.

## Detailed Planning Docs

- [Detection opportunity matrix](docs/detection-opportunity-matrix.md)
- [Measurable events mode](docs/measurable-events-mode.md)
- [Purple-team handoff pack](docs/purple-team-handoff-pack.md)
- [Attack-path evidence import](docs/attack-path-evidence-import.md)
- [Operator debrief workflow](docs/operator-debrief-workflow.md)
- [Report QA before delivery](docs/report-qa-before-delivery.md)
- [Client outcome view](docs/client-outcome-view.md)

## Recommended Build Order

1. Report QA before delivery.
2. Detection opportunity matrix.
3. Measurable events mode.
4. Purple-team handoff pack.
5. Operator debrief workflow.
6. Client outcome view.
7. Attack-path evidence import, only after real gated fixtures exist.

The first item is lowest risk and immediately improves trust in existing
deliverables. The attack-path work should wait for real BloodHound, NetExec, or
similar evidence because unsupported parser claims would weaken the product.

## External Alignment

These goals align with:

- MITRE ATT&CK for tactics, techniques, procedures, detections, and mitigations;
- CISA red-team assessment writeups that emphasize detection, response, and
  measurable defender activity;
- NIST SP 800-115 guidance around planning, conducting tests, analyzing findings,
  and developing mitigation strategies.

## Definition of Success

Piranesi is more value-adding to a red team when it can answer:

- What did we do?
- Why did it matter?
- What evidence proves it?
- What should defenders have seen?
- What did defenders actually see?
- What should change next?
- How do we prove retest or closure later?
