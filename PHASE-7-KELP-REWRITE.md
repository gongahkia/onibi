# PHASE 7: Enterprise Hardening And Deployment

## Goal

Prepare KelpClaw for enterprise and industry use by hardening observability, auditability, security, determinism, CI, and deployment.

This phase turns the working planner and runtime into an operable system.

## Observability

Capture structured events for:

- Prompt planning.
- Skill matching.
- Draft edits and node reprompts.
- Workflow approval.
- DAG compilation.
- Node container lifecycle.
- Adapter calls.
- Code-gen artifact creation and verification.
- Delivery events.
- Run completion or failure.

Each event must include workflow id, revision id, run id when available, node id when available, timestamp, severity, and correlation id.

## Audit And Governance

Enterprise audit records must show:

- Who created or edited a workflow.
- What changed between revisions.
- Which generated code was approved.
- Which secrets were referenced.
- Which containers ran.
- Which external adapters were called.
- What result was delivered and through which channel.

Approved workflow revisions and generated artifacts are immutable.

## Durable Persistence And Planner Backend

Phase 3 uses in-memory workflow state and a deterministic mocked planner. Those are acceptable for the planner UI milestone only.

Enterprise hardening must replace those temporary pieces with durable services:

- Replace `InMemoryWorkflowStore` with a database-backed workflow store.
- Persist draft revisions, immutable approved revisions, run records, run events, audit records, and approval diffs.
- Persist generated artifacts and manifests in content-addressed object storage.
- Add schema migrations for workflow, run, artifact, and audit tables.
- Make the planner backend deployable independently from OpenClaw, with configurable model/provider settings.
- Keep a deterministic mock planner mode for tests, demos, and offline development.

## Security

Security requirements:

- Secret values are never stored in workflow specs.
- Containers receive only declared secret references resolved at runtime.
- Network access is denied by default.
- Docker images and generated code artifacts are content-addressed or version-pinned.
- Code-gen dependencies are pinned and scanned.
- Runtime workspaces are isolated per node and per run.
- Logs redact secrets and provider tokens.

## Determinism And Evals

KelpClaw must continuously test that approved workflows do not drift.

Required checks:

- Planner evals for common workflow prompts.
- Skill selection evals proving existing skills are preferred over code-gen nodes.
- DAG determinism tests for stable execution ordering.
- Code-gen replay tests using persisted artifacts.
- Adapter mock tests with deterministic payloads.
- Regression fixtures for known enterprise workflows.

## CI And Deployment

CI must run:

- TypeScript typecheck.
- Lint and formatting checks.
- Unit tests.
- Integration tests with Docker.
- Workflow spec fixture validation.
- Frontend component and E2E tests.
- Security checks for generated code packages where feasible.

Deployment targets:

- Local developer mode with Docker.
- Single-host demo deployment.
- Future production deployment with separate OpenClaw UI, API, NanoClaw workers, object storage, database, and secret manager.

## Implementation Checkpoints

1. Add structured logging and run event schema.
2. Add audit log persistence for workflow revisions and approvals.
3. Add secret reference resolution and redaction policy.
4. Add content hash verification for images and generated artifacts.
5. Add planner, skill selection, runtime, and adapter eval suites.
6. Add Docker-backed integration tests to CI.
7. Add deployment docs for local and single-host environments.
8. Add production readiness checklist.
9. Replace in-memory workflow/run persistence with a durable database-backed store.
10. Add deployable live planner backend configuration while preserving deterministic mock mode for tests.

## Tests

- Audit log tests for create, edit, approve, run, and deliver flows.
- Secret redaction tests for logs, errors, and event streams.
- Determinism tests for repeated approved runs.
- Docker image and generated artifact verification tests.
- Durable-store tests for draft revisions, approved revisions, runs, audit events, and migration compatibility.
- Planner backend tests for live-provider configuration, deterministic mock fallback, and valid workflow-spec output.
- CI smoke tests from a clean checkout.
- Deployment smoke test for local Docker mode.

## Acceptance Criteria

- Every approved run has inspectable audit and observability records.
- Secrets are referenced safely and redacted consistently.
- Deterministic behavior is covered by automated regression tests.
- Docker-backed integration tests run in CI.
- The repo documents local, demo, and future production deployment paths.
- Workflow state survives API process restarts through durable persistence.
- Production planning can use a live planner backend without changing approved-run determinism.
