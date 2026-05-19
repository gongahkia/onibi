# Production Readiness Checklist

## Required For Single-Host Production

- Set `KELPCLAW_ADMIN_TOKEN` and require it for OpenClaw and all API inspection/mutation routes.
- Set `KELPCLAW_SECRET_MASTER_KEY` before API startup; back it up separately from SQLite.
- Use SQLite on durable storage and back up the database plus artifact store together.
- Keep `NANOCLAW_RUNNER=production` so adapter nodes use live adapters and custom/codegen nodes use Docker fallback.
- Mount the Docker socket only on the API host that is allowed to execute NanoClaw containers.
- Keep generated/codegen runtime images pinned to explicit tags or digests.
- Keep Docker network default-deny; adapter and codegen nodes must declare provider hosts.
- Store provider credentials through encrypted `secret:<name>` refs. Do not place raw tokens in workflow specs.
- Configure Google OAuth web credentials with the exact `KELPCLAW_PUBLIC_BASE_URL` callback.
- Use SMTP, WhatsApp Cloud API, and Telegram Bot credentials owned by testable service accounts or bots.
- Run `pnpm verify` from a clean checkout before deployment.

## Operational Smoke Tests

- `GET /health` returns `kelpclaw-api`.
- Unauthorized calls to `/api/secrets`, workflow audit, run events, and revision routes return 401.
- `/api/secrets` lists only metadata and integration readiness, never raw secret values.
- Google OAuth connect, callback, status, and revoke complete against a configured OAuth client.
- A workflow can be planned, validated, approved, run, and inspected after API restart.
- `GET /api/workflows/:id/audit` shows create/edit/approve/adapter/delivery/run records.
- `GET /api/workflows/:id/runs/:runId/events` shows structured events with workflow, revision, run, severity, and correlation ids.
- Missing live secrets produce failed structured run output instead of falling back to mock adapters.
- Generated artifact hash drift blocks approval or execution.
- `KELPCLAW_LIVE_SMOKE=1 pnpm smoke:live` succeeds against explicit test recipients and sheet ids.

## Known Boundaries

- This is single-host admin-token auth, not multi-user RBAC.
- SQLite is the durable store for this phase; move to managed SQL before multi-host writes.
- Local encrypted secrets are suitable for self-hosting; use a managed secret manager before multi-tenant SaaS.
- Provider OAuth beyond Google is still configured by service tokens/secrets, not per-user OAuth.
- Central log shipping, metrics dashboards, alerting, and incident retention are deployment responsibilities.
