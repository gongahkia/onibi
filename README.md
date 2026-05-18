# KelpClaw

KelpClaw is a TypeScript monorepo for deterministic AI workflow design and execution.

OpenClaw is the editable workflow planner. NanoClaw is the deterministic runtime that compiles workflow DAGs and executes nodes through a Docker-per-node contract.

The previous Zig CLI/TUI task planner is preserved in this repository as legacy reference material during the rewrite. Phase 1 does not delete Zig source, installer scripts, or package-release paths.

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

## Phase 1 Guarantees

- Workflow specs are diffable and validated with stable error codes.
- OpenClaw uses mocked planner data from the shared workflow fixture.
- NanoClaw execution is covered through a mock runner and Docker command-construction tests.
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
