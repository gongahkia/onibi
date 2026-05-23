# KelpClaw Self-Host Deployment

## Local Production Mode

KelpClaw Phase 8 is aimed at a single-host deployment: Fastify API, SQLite workflow/audit/secrets storage, local artifact storage, OpenClaw, live adapters, and Docker-backed custom/codegen execution.

```console
$ corepack enable
$ pnpm install
$ cp .env.example .env
```

Edit `.env` before starting the API:

- `KELPCLAW_ADMIN_TOKEN`: required Bearer token for OpenClaw and API calls.
- `KELPCLAW_SECRET_MASTER_KEY`: required AES-256-GCM master key for encrypted local secrets.
- `KELPCLAW_PUBLIC_BASE_URL`: externally reachable API base URL for OAuth callbacks.
- `KELPCLAW_PLANNER_PROVIDER`: `anthropic`, `openai`, or `openweight` for live planning/codegen during planning.
- `KELPCLAW_CODEGEN_PROVIDER`: optional override for generated-node build roles; defaults to `KELPCLAW_PLANNER_PROVIDER`.
- `ANTHROPIC_API_KEY`: required when the selected live provider is `anthropic`.
- `OPENAI_API_KEY`: required when the selected live provider is `openai`.
- `KELPCLAW_OPENWEIGHT_BASE_URL`: required when the selected live provider is `openweight`; point it at an OpenAI-compatible `/v1` endpoint.
- `KELPCLAW_OPENWEIGHT_API_KEY`: optional bearer token for open-weight gateways that require auth.
- `KELPCLAW_PLANNER_MODEL` and `KELPCLAW_CODEGEN_MODEL`: optional provider model overrides. Provider-specific overrides include `KELPCLAW_OPENAI_PLANNER_MODEL`, `KELPCLAW_OPENAI_CODEGEN_MODEL`, `KELPCLAW_OPENWEIGHT_PLANNER_MODEL`, and `KELPCLAW_OPENWEIGHT_CODEGEN_MODEL`.
- `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`: OAuth web client credentials.
- SMTP, WhatsApp, Telegram, GitHub, Slack, Discord, Notion, Linear, Jira, Airtable, webhook, and database defaults as needed for your providers.

Start the API:

```console
$ pnpm --filter @kelpclaw/api build
$ pnpm --filter @kelpclaw/api start
```

Start OpenClaw in another shell:

```console
$ OPENCLAW_API_TARGET=http://127.0.0.1:8787 \
  VITE_OPENCLAW_ADMIN_TOKEN="$KELPCLAW_ADMIN_TOKEN" \
  pnpm --filter @kelpclaw/openclaw dev
```

OpenClaw stores the admin token in local browser storage and sends `Authorization: Bearer <token>` on API calls.

## Secrets

Production workflows use `secret:<name>` refs. Raw provider tokens must be inserted through the API or OpenClaw integration panel; list responses return metadata only.

```console
$ curl -H "Authorization: Bearer $KELPCLAW_ADMIN_TOKEN" \
  http://127.0.0.1:8787/api/secrets
```

Examples:

```console
$ curl -X PUT http://127.0.0.1:8787/api/secrets \
  -H "Authorization: Bearer $KELPCLAW_ADMIN_TOKEN" \
  -H "content-type: application/json" \
  -d '{"name":"email.smtp.default","value":"{\"host\":\"smtp.example.com\",\"port\":587,\"username\":\"user\",\"password\":\"pass\",\"from\":\"kelp@example.com\"}"}'
```

Google is normally connected through OAuth:

```console
$ curl -H "Authorization: Bearer $KELPCLAW_ADMIN_TOKEN" \
  http://127.0.0.1:8787/api/integrations/google/connect
```

The callback stores the refresh token as encrypted `secret:google.oauth.default`.

## Docker Compose

Compose expects a local `.env` file and mounts the host Docker socket so the API container can launch NanoClaw Docker nodes.

```console
$ cp .env.example .env
$ docker compose up --build
```

OpenClaw: `http://127.0.0.1:5173`

API health: `http://127.0.0.1:8787/health`

The named `kelpclaw-data` volume stores SQLite data and artifacts. The `kelpclaw-workspaces` volume is mounted at `/workspace` for Docker-backed node execution. The API and OpenClaw services are fully wrapped by Compose; the mounted Docker socket is only for NanoClaw's nested Docker-per-node sandbox.

Both containers run a fast preflight before starting the servers. It blocks startup when required admin tokens, provider keys, secret encryption keys, Docker socket access, or writable mounted directories are missing. Set `KELPCLAW_PREFLIGHT=0` only for local debugging when you intentionally want to bypass those startup checks.

To run the whole stack with OpenAI-backed planning/codegen:

```console
$ KELPCLAW_PLANNER_PROVIDER=openai \
  KELPCLAW_CODEGEN_PROVIDER=openai \
  OPENAI_API_KEY=sk-... \
  docker compose up --build
```

To keep Anthropic-backed planning/codegen, leave `KELPCLAW_PLANNER_PROVIDER=anthropic` and set `ANTHROPIC_API_KEY`.

To run against an OpenAI-compatible open-weight endpoint such as Ollama, vLLM, LM Studio, or a hosted gateway:

```console
$ KELPCLAW_PLANNER_PROVIDER=openweight \
  KELPCLAW_CODEGEN_PROVIDER=openweight \
  KELPCLAW_OPENWEIGHT_BASE_URL=http://127.0.0.1:11434/v1 \
  KELPCLAW_OPENWEIGHT_MODEL=qwen2.5-coder \
  docker compose up --build
```

## Runtime Truth, Budgets, And Runs

OpenClaw and the API expose the authoritative lifecycle through `GET /api/workflows/:id/runtime-truth`. The visible stages are `planned`, `accepted`, `generated`, `evaluated`, `approved`, `deployed`, and `runnable`.

Production runs require an active `runner.configuration` deployment for the approved revision. Creating a `workflow.bundle` export is useful for inspection and rollback artifacts, but it is not enough to run production traffic. `POST /api/workflows/:id/runs` now creates a queued `run.workflow` job and returns immediately. The local API worker executes that job, writes run events, and persists per-node checkpoints so a restarted API can mark interrupted runs resumable and reuse completed checkpoints when node input hashes still match. OpenClaw's primary `Deploy` action creates a runner configuration; the deployment panel can also export a bundle, undeploy the active runner, rollback to the recorded target, and export redacted audit JSONL.

Run history and replay are available through:

```console
$ curl -H "Authorization: Bearer $KELPCLAW_ADMIN_TOKEN" \
  http://127.0.0.1:8787/api/workflows/<workflow-id>/runs

$ curl -X POST -H "Authorization: Bearer $KELPCLAW_ADMIN_TOKEN" \
  http://127.0.0.1:8787/api/workflows/<workflow-id>/runs/<run-id>/replay
```

Queued jobs carry attempt metadata, optional `nextRunAt`, and local backoff. Retryable worker failures are requeued until `maxAttempts` is exhausted.

Budget policy and ledgers are available through `GET/PATCH /api/workflows/:id/budget`. The local defaults are `$5.00` per workflow, `$2.00` per generated-node build, `$2.00` per agentic research run, and `$0.25` for expensive retry confirmation. Provider calls are stopped before the next agent step when projected cost would exceed the remaining budget.

Agent role decisions, tokens, and costs are available through `GET /api/workflows/:id/agent-timeline`. Per-node planner and codegen rationale summaries are available through decision trace routes and are included in redacted audit exports. KelpClaw stores summaries and artifacts for eval-building, not raw hidden chain-of-thought.

## Connectors

Built-in adapters remain available for Gmail, Sheets, email, WhatsApp, Telegram, GitHub, Slack, Discord, Notion, Linear, Jira Cloud, Airtable, generic webhook delivery, and database query/execute nodes. SQLite is supported by the built-in database client; Postgres, MySQL, and other engines plug in through the runtime `DatabaseClient` contract. To avoid hand-building hundreds of adapters, KelpClaw also supports stored connector records:

- OpenAPI connectors import operations from `operationId`, falling back to `METHOD /path`.
- Generic HTTP execution enforces the connector's declared allowed hosts.
- Auth supports API keys, bearer tokens, and basic auth through encrypted `secret:<name>` refs. OAuth is v1 external-token setup: provision a token secret, then reference it from the connector.
- MCP support is tool-consumer only. Streamable HTTP MCP servers are supported first; stdio MCP should be kept to explicit local-dev configuration.

OpenAPI import:

```console
$ curl -X POST http://127.0.0.1:8787/api/connectors/openapi/import \
  -H "Authorization: Bearer $KELPCLAW_ADMIN_TOKEN" \
  -H "content-type: application/json" \
  -d '{"name":"Status API","sourceUrl":"https://status.example.com/openapi.json"}'
```

MCP registration:

```console
$ curl -X POST http://127.0.0.1:8787/api/connectors/mcp \
  -H "Authorization: Bearer $KELPCLAW_ADMIN_TOKEN" \
  -H "content-type: application/json" \
  -d '{"name":"Internal Tools","endpointUrl":"https://tools.example.com/mcp"}'
```

OpenClaw's connector panel can import/register connectors, test them, inspect allowed hosts and auth readiness, and add connector operations to the current draft workflow as adapter-backed nodes.

## Schedules And Ops Health

`schedule.activation` deployments materialize durable `workflow_schedules` rows. The local scheduler ticks due schedules, enqueues one `run.workflow` job per fire time, and records schedule-derived idempotency metadata. Cron expressions are 5-field cron strings; UTC is the default timezone unless the schedule node specifies `timezone`.

```console
$ curl -H "Authorization: Bearer $KELPCLAW_ADMIN_TOKEN" \
  http://127.0.0.1:8787/api/workflows/<workflow-id>/schedules

$ curl -X POST -H "Authorization: Bearer $KELPCLAW_ADMIN_TOKEN" \
  http://127.0.0.1:8787/api/workflows/<workflow-id>/schedules/<schedule-id>/pause
```

`GET /api/ops/health` reports SQLite availability, worker and scheduler state, queued/running/failed jobs, resumable runs, connector counts, and failed connector tests.

## Backup, Restore, And Upgrades

Back up SQLite, artifact storage, and the secret master key together. A SQLite-only backup without the artifact directory loses generated-node artifacts and run workspaces; a database backup without `KELPCLAW_SECRET_MASTER_KEY` cannot decrypt stored secrets.

Recommended single-host backup:

```console
$ sqlite3 /data/kelpclaw.db ".backup '/backups/kelpclaw-$(date +%Y%m%d%H%M%S).db'"
$ tar -czf /backups/kelpclaw-artifacts-$(date +%Y%m%d%H%M%S).tgz /data/artifacts /workspace
```

Restore by stopping the API, restoring the database and artifact/workspace directories, setting the same `KELPCLAW_SECRET_MASTER_KEY`, then starting the API. Startup recovery requeues interrupted jobs and marks interrupted runs resumable.

Before upgrades:

- Run `pnpm verify` against the target commit.
- Back up SQLite and artifacts.
- Review `pnpm-lock.yaml` and migration notes for connector/runtime dependency changes.
- Start the API once and check `GET /api/ops/health`.
- Re-test critical connectors with `POST /api/connectors/:id/test`.

## Dev And Test Mode

Use deterministic mode only for tests, demos, and offline work:

```console
$ KELPCLAW_PLANNER_MODE=deterministic \
  NANOCLAW_RUNNER=mock \
  KELPCLAW_SECRET_STORE=memory \
  KELPCLAW_ADMIN_TOKEN=dev-token \
  pnpm --filter @kelpclaw/api start
```

Mock adapters remain available through `createDefaultMockAdapters()` and `.fake` aliases in tests.

## Live Smoke

`pnpm smoke:live` is opt-in and exits without provider calls unless `KELPCLAW_LIVE_SMOKE=1` is set.

Required inputs:

- `KELPCLAW_API_BASE_URL`
- `KELPCLAW_ADMIN_TOKEN`
- `KELPCLAW_SMOKE_SHEET_ID`
- `KELPCLAW_SMOKE_EMAIL_TO`
- `KELPCLAW_SMOKE_WHATSAPP_TO`
- `KELPCLAW_SMOKE_TELEGRAM_CHAT_ID`

Run it only against test inboxes, sheets, recipients, and bot chats:

```console
$ KELPCLAW_LIVE_SMOKE=1 pnpm smoke:live
```

Normal CI must not run this command.
