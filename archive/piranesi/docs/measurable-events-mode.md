# Measurable Events Mode

Date: 2026-05-20

Status: planned supplementary feature.

## Goal

Support planned or ad hoc red-team actions designed to test whether defenders
detect, triage, escalate, and respond. This converts "we did activity" into "we
measured a defensive process."

## Approach

Add measurable events as a workflow layer above timeline events. A measurable
event describes a test action, expected detection points, expected defender
reaction, actual reaction, evidence, and outcome.

This should be explicit and operator-controlled. Piranesi should not trigger the
event, execute payloads, or interact with targets.

## Proposed Data Shape

Each measurable event should include:

- stable event ID;
- title and description;
- planned or observed timestamp;
- ATT&CK tactic and technique IDs;
- linked timeline event IDs;
- linked evidence IDs;
- expected telemetry;
- expected defender activity;
- actual defender activity;
- response time if known;
- event outcome: planned, executed, detected, missed, partial, blocked,
  cancelled;
- safety notes;
- retest recommendation.

## User Flow

1. Operator plans measurable events before or during an engagement.
2. Operator executes the activity outside Piranesi under the engagement rules.
3. Operator records observed defender behavior and supporting evidence.
4. Piranesi renders a measurable-events table in the red-team report.
5. Retest or purple-team work updates the event outcome.

## Build Slices

1. Add measurable-event workspace model and validation.
2. Add CLI add/list/update commands.
3. Add web UI form and event table.
4. Link measurable events to detection opportunities.
5. Render expected versus actual response in handoff outputs.

## Acceptance Criteria

- Events can be planned before evidence exists.
- Executed events require at least one observation or evidence link.
- Reports distinguish missed, partial, detected, blocked, and cancelled events.
- Event output does not imply malicious activity was performed by Piranesi.
- Tests cover lifecycle transitions and report rendering.

## Non-Goals

- Running scans, payloads, or simulations.
- Triggering alerts automatically.
- Integrating directly with SOC tools.
- Scoring defenders without enough evidence.
