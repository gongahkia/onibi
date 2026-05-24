# Operator Debrief Workflow

Date: 2026-05-20

Status: planned supplementary feature.

## Goal

Capture the operator knowledge that usually disappears after an engagement:
what worked, what failed, what made noise, what controls blocked activity, what
surprised the team, and what should be repeated or avoided next time.

## Approach

Add a structured debrief model linked to timeline events, procedures, findings,
evidence, detection opportunities, and measurable events. The debrief should be
usable as an internal operator note and as a sanitized client-facing lessons
section.

## Proposed Data Shape

Each debrief item should include:

- stable item ID;
- category: worked, failed, noisy, blocked, prevented, surprise, follow-up,
  tooling, process;
- summary;
- linked evidence IDs;
- linked timeline event IDs;
- linked procedure IDs;
- linked finding IDs;
- internal notes;
- client-safe notes;
- sensitivity;
- recommended next action.

## User Flow

1. Operator records debrief items during or after the engagement.
2. Sensitive internal notes stay out of client output by default.
3. Client-safe notes can render into a lessons learned section.
4. Internal archive preserves full operator context for future engagements.

## Build Slices

1. Add debrief model, loader, writer, and tests.
2. Add CLI add/list commands.
3. Add web UI panel.
4. Add client-safe rendering to reports.
5. Add internal-only archive export.

## Acceptance Criteria

- Debrief items support sensitivity and client-safe text.
- Report rendering excludes internal notes by default.
- Items can link to timeline, evidence, findings, and procedures.
- Tests cover redaction, rendering, and invalid references.

## Non-Goals

- Capturing secrets in client-facing output.
- Replacing full operator notes or external team retrospectives.
- Automatically judging operator performance.
- Publishing debrief data to external systems.
