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
- `KELPCLAW_PLANNER_PROVIDER`: `anthropic` or `openai` for live planning/codegen during planning.
- `KELPCLAW_CODEGEN_PROVIDER`: optional override for generated-node build roles; defaults to `KELPCLAW_PLANNER_PROVIDER`.
- `ANTHROPIC_API_KEY`: required when the selected live provider is `anthropic`.
- `OPENAI_API_KEY`: required when the selected live provider is `openai`.
- `KELPCLAW_PLANNER_MODEL` and `KELPCLAW_CODEGEN_MODEL`: optional provider model overrides. OpenAI-specific overrides are `KELPCLAW_OPENAI_PLANNER_MODEL` and `KELPCLAW_OPENAI_CODEGEN_MODEL`.
- `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`: OAuth web client credentials.
- SMTP, WhatsApp, and Telegram defaults as needed for your providers.

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
