# PHASE 3: OpenClaw Planner UI

## Goal

Build OpenClaw as the KelpClaw workflow planning surface.

Users type a workflow prompt, inspect the generated flowchart, edit nodes and edges in React Flow, reprompt specific nodes, and approve a frozen DAG for NanoClaw execution.

## Product Flow

1. User enters a prompt such as "extract transaction details from Gmail receipts into Sheets".
2. OpenClaw asks the API to plan a draft workflow from available skills, metaprompts, and adapter capabilities.
3. The draft appears as a React Flow canvas.
4. User edits labels, configs, edges, and node prompts.
5. User reprompts individual nodes when a step is wrong or underspecified.
6. OpenClaw validates the graph and shows blocking issues inline.
7. User approves the workflow.
8. The approved workflow is compiled and run by NanoClaw.

## UI Requirements

- First screen is the planner workspace, not a marketing landing page.
- Main layout contains prompt input, React Flow canvas, selected-node inspector, validation panel, and run/approval controls.
- Node visuals must distinguish trigger, skill, code-gen, transform, approval, and delivery nodes.
- Code-gen nodes must show generated artifact status and sandbox policy.
- Validation errors must point to the affected node or edge.
- Approval must show a diff between the latest draft and the frozen execution spec.

## API Contracts

OpenClaw calls these API routes:

- `POST /api/workflows/plan`: create a draft from a prompt.
- `POST /api/workflows/:id/reprompt-node`: regenerate one node in context.
- `POST /api/workflows/:id/validate`: validate the current graph.
- `POST /api/workflows/:id/approve`: freeze a static DAG revision.
- `POST /api/workflows/:id/runs`: start NanoClaw execution.
- `GET /api/workflows/:id/runs/:runId`: fetch run state, logs, and results.

All routes use shared workflow-spec types. The frontend must not define duplicate wire shapes.

## State Model

- Draft graph state lives in the UI while editing.
- Server persistence stores each saved draft revision.
- Approved revisions are immutable.
- Running a workflow always references an approved revision id.
- UI regeneration must preserve manually edited nodes unless the user explicitly reprompts them.

## Implementation Checkpoints

1. Create the OpenClaw React app shell and import React Flow.
2. Render fixture workflows from `packages/workflow-spec`.
3. Implement prompt-to-draft planning with mocked API responses.
4. Implement node inspector editing for labels, configs, inputs, outputs, retry, timeout, and delivery channel.
5. Implement node reprompt flow with before/after diff.
6. Implement graph validation display and blocking approval errors.
7. Implement approval view that freezes the canonical workflow JSON.
8. Implement run status view backed by NanoClaw run events.

## Tests

- Component tests for node rendering, inspector edits, and validation messages.
- Canvas tests for adding, dragging, connecting, and deleting nodes.
- API integration tests using mocked planner and workflow-spec fixtures.
- Snapshot tests for draft-to-approved spec diffs.
- E2E test for prompt, edit, validate, approve, run, and inspect result.

## Acceptance Criteria

- A user can create and edit a workflow visually.
- React Flow state round-trips through the shared workflow spec.
- Approval is blocked for invalid graphs.
- Approved workflow JSON is stable and diffable.
- Running a workflow starts from an approved revision, not from mutable UI state.
