# KelpClaw Deployment

## Local Developer Mode

Install dependencies and run the full local verification suite:

```console
$ corepack enable
$ pnpm install
$ pnpm verify
```

Run the API with durable SQLite persistence:

```console
$ cp .env.example .env
$ KELPCLAW_WORKFLOW_STORE=sqlite \
  KELPCLAW_WORKFLOW_DB=.kelpclaw/workflow.sqlite \
  KELPCLAW_PLANNER_MODE=deterministic \
  pnpm --filter @kelpclaw/api start
```

Run OpenClaw separately:

```console
$ OPENCLAW_API_TARGET=http://127.0.0.1:8787 pnpm --filter @kelpclaw/openclaw dev
```

Deterministic planner mode is intended for tests, demos, and offline development. Live planning uses the Anthropic-compatible path:

```console
$ KELPCLAW_PLANNER_MODE=live \
  KELPCLAW_PLANNER_PROVIDER=anthropic \
  KELPCLAW_PLANNER_MODEL=<model> \
  ANTHROPIC_API_KEY=<key> \
  pnpm --filter @kelpclaw/api start
```

## Single-Host Demo

The compose stack runs OpenClaw, the API, a SQLite database file, and the content-addressed artifact store on a shared named volume:

```console
$ docker compose up --build
```

OpenClaw is exposed at `http://127.0.0.1:5173`; the API health endpoint is `http://127.0.0.1:8787/health`.

The demo defaults to deterministic planning. To use live planning, set `KELPCLAW_PLANNER_MODE=live`, `KELPCLAW_PLANNER_PROVIDER=anthropic`, and `ANTHROPIC_API_KEY` in a local `.env` file before starting Compose.

## Future Production Topology

Production should split the current single-host pieces into independently deployable services:

- OpenClaw UI behind a static web tier.
- KelpClaw API with SQLite replaced by a managed SQL database when multi-host writes are required.
- NanoClaw workers with isolated per-run workspaces and Docker or container-orchestrator sandboxing.
- Content-addressed object storage for generated artifacts and manifests.
- Secret manager-backed `SecretResolver` for `env:` or provider-native secret references.
- Centralized log/event ingestion for audit and observability records.
