# Detection Opportunity Matrix

Date: 2026-05-20

Status: planned supplementary feature.

## Goal

Create a first-class matrix that links red-team activity to ATT&CK techniques,
expected telemetry, actual detection, defender response, evidence, and follow-up
work. This makes the engagement useful to defenders instead of only producing a
finding list.

## Approach

Add a local detection-opportunity model that can reference existing timeline
events, procedures, findings, evidence records, and IOCs. Each row should capture
one opportunity for the blue team to detect, investigate, or respond to a
specific red-team action.

The model should be local JSON in the workspace and should render into Markdown,
PDF, archive, and handoff outputs.

## Proposed Data Shape

Each opportunity should include:

- stable opportunity ID;
- ATT&CK tactic and technique ID/name;
- linked timeline event IDs;
- linked procedure IDs;
- linked evidence IDs;
- linked finding IDs;
- expected telemetry sources;
- expected detection logic or analytic description;
- actual defender signal observed;
- actual defender response observed;
- gap status: not-tested, detected, missed, partial, blocked, not-applicable;
- recommended detection or response improvement;
- owner or target team;
- retest notes.

## User Flow

1. Operator records timeline events and procedures during the engagement.
2. Operator creates detection opportunities from the UI or CLI.
3. Operator links evidence, findings, procedures, and IOCs.
4. Blue-team handoff includes a matrix of expected versus actual coverage.
5. Retest records update the gap status.

## Build Slices

1. Add workspace model, loader, writer, and schema tests.
2. Add CLI commands to add/list opportunities.
3. Add local web UI create/list panels.
4. Render the matrix in red-team handoff Markdown/PDF/archive.
5. Add retest support for gap-status updates.

## Acceptance Criteria

- Opportunities can be created without mutating findings.
- Every opportunity can link to evidence and timeline records.
- Handoff output includes expected telemetry, actual response, and gap status.
- Missing evidence links are visible in report QA.
- Tests cover empty matrix, populated matrix, invalid references, and rendering.

## Non-Goals

- Generating SIEM rules automatically.
- Claiming detection coverage without operator input.
- Calling external EDR, SIEM, or SOAR APIs.
- Autonomous ATT&CK mapping without review.
