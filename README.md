# KelpClaw

KelpClaw is a TypeScript monorepo for deterministic AI workflow design and execution.

OpenClaw is the editable workflow planner. NanoClaw is the deterministic runtime that compiles approved workflow revisions and executes nodes through a Docker-per-node contract.

The previous Zig CLI/TUI task planner is preserved in this repository as legacy reference material during the rewrite. The TypeScript rewrite does not delete Zig source, installer scripts, or package-release paths.

## Workspace Layout

| Workspace                 | Ownership                                                                                |
| ------------------------- | ---------------------------------------------------------------------------------------- |
| `apps/openclaw`           | React + React Flow workflow planning UI                                                  |
| `apps/api`                | HTTP API for planning, workflow persistence, validation, approval, and execution control |
| `packages/workflow-spec`  | Shared workflow IR types, Zod schemas, JSON Schema, fixtures, and validation errors      |
| `packages/skill-registry` | Built-in deterministic skills, metadata, metaprompts, and lookup rules                   |
| `packages/nanoclaw`       | DAG compiler, topological ordering, Docker command runner, and mock execution runner     |
| `packages/codegen`        | Generated artifact contracts, checksums, and replay policy helpers                       |
| `packages/adapters`       | Gmail, Sheets, email, WhatsApp, and Telegram adapter interfaces with fake adapters       |
| `packages/testing`        | Shared fixtures, fake providers, and deterministic execution harnesses                   |

## Development

KelpClaw uses Node.js, pnpm workspaces, TypeScript, Vitest, ESLint, Prettier, Fastify, Vite, and React Flow.

```console
$ corepack enable
$ pnpm install
$ pnpm verify
```

Useful workspace commands:

```console
$ pnpm --filter @kelpclaw/api test
$ pnpm --filter @kelpclaw/openclaw dev
$ pnpm --filter @kelpclaw/workflow-spec test
```

## Workflow V1 Model

KelpClaw uses the canonical workflow JSON IR with `schemaVersion: "1.0.0"`. The top-level workflow fields are `id`, `schemaVersion`, `name`, `prompt`, `revision`, `nodes`, `edges`, `approval`, `createdAt`, and `updatedAt`.

Workflow nodes use `kind` instead of the earlier planner `type` shape. Supported node kinds are `trigger`, `skill`, `codegen`, `transform`, `approval`, and `delivery`. Each node declares a human description, JSON-Schema-compatible input and output ports, config, runtime settings, and determinism metadata.

Edges are port-aware: each edge connects `source.nodeId/source.port` to `target.nodeId/target.port`. Validation reports stable error codes for duplicate nodes, missing node references, invalid ports, DAG cycles, unapproved execution, unsupported schema versions, and missing codegen provenance or replay metadata.

Canonical serialization keeps object keys and collections stable for snapshots, review diffs, and DAG hashing. The migration harness currently passes through v1 workflows and rejects unsupported schema versions so future IR upgrades can be added without changing callers.

## Approval And Execution

Approving a workflow freezes the current revision into `workflow.approval`, including the approver, approval timestamp, frozen DAG hash, and compiled node order. NanoClaw compiles only approved workflow revisions and emits a v1 `execution_result` envelope for both mock and Docker-backed runners.

Editing an approved workflow creates a new draft revision. Execution remains blocked until that current revision is approved.

## Skill Registry

The built-in skill registry records input and output schemas, required secrets, fake adapter dependencies, runtime templates, metaprompts, validation rules, and example fixtures. Deterministic matching returns scored `SkillMatch` results with explainable reasons. Registry skills are preferred over codegen when the top match reaches the fixed reuse threshold.

## Validation Guarantees

- Workflow specs are diffable and validated with stable error codes.
- OpenClaw renders the shared v1 fixtures with schema version, revision, prompt, node kind, port-aware edges, and approval state.
- NanoClaw execution is covered through an approved-workflow mock runner and Docker command-construction tests.
- Integration adapters are fake-only and do not require secrets.
- CI runs TypeScript format, lint, typecheck, tests, builds, and the legacy Zig test suite.

## Legacy Zig CLI

The legacy `kelp` CLI still builds and tests with Zig:

```console
$ zig build test
$ ./scripts/package-release.sh
```

Legacy storage paths remain unchanged while the KelpClaw replacement entrypoints mature:

- data: `$XDG_DATA_HOME/kelp/data.json` or `$HOME/.local/share/kelp/data.json`
- config: `$XDG_CONFIG_HOME/kelp/config.json` or `$HOME/.config/kelp/config.json`
- `--data-dir` colocates data and config for tests or isolated workspaces
