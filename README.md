# KelpClaw

KelpClaw is a TypeScript monorepo for deterministic AI workflow design and execution.

OpenClaw is the editable workflow planner. NanoClaw is the deterministic runtime that compiles approved workflow revisions and executes nodes through a Docker-per-node contract.

## Workspace Layout

| Workspace                 | Ownership                                                                                |
| ------------------------- | ---------------------------------------------------------------------------------------- |
| `apps/openclaw`           | React + React Flow workflow planning UI                                                  |
| `apps/api`                | HTTP API for planning, workflow persistence, validation, approval, and execution control |
| `packages/workflow-spec`  | Shared workflow IR types, Zod schemas, JSON Schema, fixtures, and validation errors      |
| `packages/skill-registry` | Built-in deterministic skills, metadata, metaprompts, and lookup rules                   |
| `packages/nanoclaw`       | DAG compiler, production runner, Docker command runner, and deterministic test runner    |
| `packages/codegen`        | Generated artifact contracts, checksums, and replay policy helpers                       |
| `packages/adapters`       | Live provider adapters, generic connectors, and deterministic test mocks                 |
| `packages/testing`        | Shared fixtures, mock providers, and deterministic execution harnesses                   |

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
$ pnpm validate:fixtures
$ pnpm check:codegen-policy
```

Quickstart, deployment notes for durable SQLite mode, Docker Compose, and production readiness live in
[`docs/quickstart.md`](docs/quickstart.md),
[`docs/deployment.md`](docs/deployment.md),
[`docs/agent-runtime-demo.md`](docs/agent-runtime-demo.md), and
[`docs/production-readiness.md`](docs/production-readiness.md).

## Workflow V1 Model

KelpClaw uses the canonical workflow JSON IR with `schemaVersion: "1.0.0"`. The top-level workflow fields are `id`, `schemaVersion`, `name`, `prompt`, `revision`, `nodes`, `edges`, `approval`, `createdAt`, and `updatedAt`.

Workflow nodes use `kind` instead of the earlier planner `type` shape. Supported node kinds are `trigger`, `skill`, `codegen`, `transform`, `approval`, and `delivery`. Each node declares a human description, JSON-Schema-compatible input and output ports, config, runtime settings, and determinism metadata.

Edges are port-aware: each edge connects `source.nodeId/source.port` to `target.nodeId/target.port`. Validation reports stable error codes for duplicate nodes, missing node references, invalid ports, DAG cycles, unapproved execution, unsupported schema versions, and missing codegen provenance or replay metadata.

Canonical serialization keeps object keys and collections stable for snapshots, review diffs, and DAG hashing. The migration harness currently passes through v1 workflows and rejects unsupported schema versions so future IR upgrades can be added without changing callers.

## Approval And Execution

Approving a workflow freezes the current revision into `workflow.approval`, including the approver, approval timestamp, frozen DAG hash, and compiled node order. NanoClaw compiles only approved workflow revisions and emits a v1 `execution_result` envelope for both mock and Docker-backed runners.

Editing an approved workflow creates a new draft revision. Execution remains blocked until that current revision is approved.

## NanoClaw Runtime Controls

API runs use the production runner by default. Adapter nodes invoke canonical live adapters (`adapter.gmail`, `adapter.sheets`, `adapter.email`, `adapter.whatsapp`, `adapter.telegram`, `adapter.github`, `adapter.slack`, `adapter.discord`, `adapter.notion`, `adapter.linear`, `adapter.jira`, `adapter.airtable`, `adapter.webhook`), deterministic built-in nodes run in-process, and custom/codegen nodes fall back to Docker. Set `NANOCLAW_RUNNER=mock` only for tests and offline demos. Optional controls are `NANOCLAW_DOCKER_BIN` for a non-default Docker binary and `NANOCLAW_HOST_WORKSPACE` for command-construction compatibility.

NanoClaw writes each run under a preserved workspace in the OS temp directory unless callers pass `workspaceRoot`. The workspace contains `workflow.json`, per-node `input.json` and `output.json`, `stdout.log`, `stderr.log`, an `artifacts/` directory, and `run-manifest.json` for replay.

Docker nodes receive only declared runtime environment variables plus NanoClaw paths:

- `NANOCLAW_WORKFLOW_SPEC`
- `NANOCLAW_NODE_INPUT`
- `NANOCLAW_NODE_OUTPUT`
- `NANOCLAW_ARTIFACTS_DIR`
- `NANOCLAW_NODE_ID`
- `NANOCLAW_ATTEMPT`

Containers mount the frozen workflow spec read-only and the node attempt workspace read-write. Network mode is `none` unless the node declares adapter or external API access. CPU, memory, timeout, retry count, backoff, logs, artifacts, attempts, skipped downstream nodes, and replay metadata are captured in the execution result.

## Skill Registry

The built-in skill registry records input and output schemas, required secrets, live adapter dependencies, runtime templates, metaprompts, validation rules, and example fixtures. Deterministic matching returns scored `SkillMatch` results with explainable reasons. Registry skills are preferred over codegen when the top match reaches the fixed reuse threshold.

## Auth, Secrets, And Integrations

The API server requires `KELPCLAW_ADMIN_TOKEN` outside test construction. OpenClaw sends it as a Bearer token from its integration panel or `VITE_OPENCLAW_ADMIN_TOKEN`.

Production secrets use encrypted local SQLite storage with `KELPCLAW_SECRET_MASTER_KEY`. Workflow specs store only `secret:<name>` refs; raw values are written through `/api/secrets` or the OpenClaw setup panel and are never returned by list APIs.

Google uses OAuth web flow endpoints under `/api/integrations/google/*`. SMTP email, WhatsApp Cloud API, Telegram Bot API, GitHub, Slack, Discord, Notion, Linear, Jira Cloud, Airtable, generic webhook delivery, and database adapters use encrypted provider secrets. The built-in database runtime supports SQLite directly and exposes a `DatabaseClient` contract for Postgres, MySQL, and other engines. Mock adapters and `.fake` ids remain test helpers only.

## Phase 5 Codegen

`POST /api/workflows/plan` now uses the registry-backed draft planner instead of the legacy fixture mock. The planner checks built-in and promoted skill metadata first. When no deterministic skill reaches the reuse threshold, it creates an explicit codegen node with planner rationale, generated source and dependency artifact references, sandbox policy, review state, and replay metadata.

Live code generation can use either the Anthropic Agent SDK or OpenAI Responses API. Set `KELPCLAW_PLANNER_PROVIDER=anthropic` with `ANTHROPIC_API_KEY`, or `KELPCLAW_PLANNER_PROVIDER=openai` with `OPENAI_API_KEY`. `KELPCLAW_CODEGEN_PROVIDER` can override the planner provider for generated-node build roles. Tests and local deterministic harnesses inject fake generators, so CI does not need live provider credentials.

Generated artifacts are written to a content-addressed local store. Set `KELPCLAW_ARTIFACT_STORE` to override the default `.kelpclaw/artifacts` path. Runtime-generated artifacts are ignored by git.

Codegen nodes must be reviewed before approval:

```console
$ curl -X POST /api/workflows/:id/codegen/:nodeId/review
$ curl -X POST /api/workflows/:id/codegen/:nodeId/promote
```

NanoClaw is the deterministic workflow runtime, not a model provider. It verifies stored artifact hashes before execution, materializes reviewed generated source into the isolated node workspace, and never regenerates code during approved runs. Promotion writes a reusable skill record to the artifact store and registers it so future matching can reuse the promoted skill instead of creating another codegen node.

## Validation Guarantees

- Workflow specs are diffable and validated with stable error codes.
- OpenClaw renders the shared v1 fixtures with schema version, revision, prompt, node kind, port-aware edges, and approval state.
- NanoClaw execution is covered through an approved-workflow mock runner and Docker command-construction tests.
- Integration adapters are production-capable; missing live secrets fail as structured run output.
- CI runs TypeScript format, lint, typecheck, tests, and builds.
