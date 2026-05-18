# PHASE 1: KelpClaw Repo Rewrite Strategy

## Goal

Pivot Kelp from a Zig local-first task planner into KelpClaw, a TypeScript monorepo for deterministic AI workflow design and execution.

KelpClaw keeps the repo history but replaces the product surface. OpenClaw becomes the editable workflow planner. NanoClaw becomes the deterministic Docker-per-node runtime.

## Current State

- The repo currently ships a Zig CLI/TUI task planner called `kelp`.
- The storage model is local JSON for projects and tasks.
- There is no web frontend, workflow DAG model, container runtime, skill registry, or integration adapter layer.
- Existing Zig code should be treated as legacy reference material during the rewrite, not as the foundation for the new architecture.

## Target Repo Shape

Use a TypeScript monorepo with these workspaces:

- `apps/openclaw`: React app using React Flow for workflow planning and editing.
- `apps/api`: HTTP API for planning, workflow persistence, validation, approval, and execution control.
- `packages/workflow-spec`: Shared TypeScript types, JSON Schema, fixtures, and spec validation.
- `packages/skill-registry`: Built-in skills, metadata, metaprompts, and skill lookup rules.
- `packages/nanoclaw`: Deterministic DAG compiler and Docker-per-node execution runtime.
- `packages/codegen`: Code-generation node contracts, generated artifact handling, and replay policies.
- `packages/adapters`: Gmail, Sheets, email, WhatsApp, and Telegram adapter interfaces with test doubles.
- `packages/testing`: Shared fixtures, mock providers, deterministic execution harnesses, and E2E helpers.

## Migration Policy

- Preserve the old Zig implementation until the TypeScript monorepo can pass its initial CI checks.
- Move Zig-specific docs and release scripts to a legacy section or archive branch after the first working KelpClaw slice exists.
- Do not mix the old task/project storage model into the KelpClaw workflow IR.
- Rename product copy from Kelp to KelpClaw, while keeping OpenClaw and NanoClaw as subsystem names.
- Keep generated workflow specs diffable and stable across planner regenerations.

## Implementation Checkpoints

1. Add monorepo tooling: package manager workspace config, TypeScript config, linting, formatting, test runner, and CI.
2. Add shared `workflow-spec` package before frontend or runtime code.
3. Add API contracts for planning, validation, approval, and execution.
4. Add OpenClaw shell using React Flow with mocked planner data.
5. Add NanoClaw local Docker execution for a minimal static DAG.
6. Add adapter test doubles before any real external credentials.
7. Retire or archive Zig product paths only after KelpClaw has replacement entrypoints.

## Tests

- Monorepo smoke test: install, typecheck, lint, and run tests from a clean checkout.
- Workflow spec validation test: valid fixtures pass and invalid DAGs fail with stable error codes.
- Legacy safety test: Zig source remains untouched until a dedicated archive/removal change.
- CI test: all workspace packages run in one command without relying on local secrets.

## Acceptance Criteria

- The repo has a clear KelpClaw workspace layout and build path.
- New code is TypeScript-first.
- Existing Zig code is not silently deleted during the first rewrite pass.
- Product docs describe KelpClaw, OpenClaw, and NanoClaw consistently.
- A developer can identify which package owns planner UI, workflow spec, runtime execution, code generation, and adapters.
