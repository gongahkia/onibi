# Purple-Team Handoff Pack

Date: 2026-05-20

Status: planned supplementary feature.

## Goal

Create a defender-ready bundle that packages what the blue team needs after a
red-team engagement: timeline, procedures, ATT&CK mapping, detection
opportunities, measurable events, IOCs, evidence references, retest notes, and
chain-of-custody metadata.

## Approach

Extend the existing red-team archive export with a purple-team handoff profile.
The profile should favor detection engineering and response improvement over
executive narrative.

The pack should be local, deterministic, and reviewable before delivery.

## Proposed Contents

- `purple-team-handoff.md`;
- `detection-opportunity-matrix.json`;
- `measurable-events.json`;
- `timeline.jsonl`;
- `procedures.json`;
- `iocs.json`;
- `evidence-index.json`;
- `retest-checklist.json`;
- `handoff-manifest.json`;
- optional raw evidence only when explicitly requested.

## User Flow

1. Operator completes report and detection opportunity records.
2. Operator runs a purple-team handoff export.
3. Piranesi validates references and redaction.
4. Piranesi writes a ZIP archive plus manifest.
5. Blue team receives a bundle optimized for detection and response work.

## Build Slices

1. Add archive profile selection for red-team versus purple-team handoff.
2. Add manifest fields for detection and retest artifacts.
3. Add renderers for Markdown and JSON handoff views.
4. Add report QA checks before export.
5. Document the workflow.

## Acceptance Criteria

- Purple-team archive can be generated from a populated workspace.
- Archive includes detection opportunities and measurable events when present.
- Raw evidence inclusion is opt-in.
- Manifest lists every artifact path and digest.
- Tests cover archive contents, redaction, and missing-reference failures.

## Non-Goals

- Sending artifacts to external systems.
- Creating SIEM rules automatically.
- Replacing final client report deliverables.
- Including raw secrets by default.
