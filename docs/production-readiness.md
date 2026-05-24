# Production Readiness Checklist

## Required For Single-Host Production

- Set `KELPCLAW_ADMIN_TOKEN` and require it for OpenClaw and all API inspection/mutation routes.
- Set `KELPCLAW_SECRET_MASTER_KEY` before API startup; back it up separately from SQLite.
- Use SQLite on durable storage and back up the database plus artifact store together.
- Keep `NANOCLAW_RUNNER=production` so adapter nodes use live adapters and custom/codegen nodes use Docker fallback.
- Mount the Docker socket only on the API host that is allowed to execute NanoClaw containers.
- Keep generated/codegen runtime images pinned to explicit tags or digests.
- Keep Docker network default-deny; adapter and codegen nodes must declare provider hosts.
- NanoClaw itself does not require Anthropic, OpenAI, or open-weight endpoint credentials. Model credentials are only used by the selected planner/codegen provider before approval.
- Store provider credentials through encrypted `secret:<name>` refs. Do not place raw tokens in workflow specs.
- Configure Google OAuth web credentials with the exact `KELPCLAW_PUBLIC_BASE_URL` callback.
- Use SMTP, WhatsApp Cloud API, Telegram Bot, GitHub, Slack, Discord, Notion, Linear, Jira, Airtable, webhook, and database credentials owned by testable service accounts or bots.
- Run `pnpm verify` from a clean checkout before deployment.
- Confirm `GET /api/runtime/providers` shows the intended planner, agentic, codegen, fixer, and evaluator providers as configured.
- Confirm workflow budgets are set through `GET/PATCH /api/workflows/:id/budget` before live agent runs.
- Confirm production runs use an active `runner.configuration` deployment, not merely the latest approved revision.
- Confirm `GET /api/ops/health` is `ok` and shows an active worker and scheduler.
- Confirm `GET /api/ops/health` reports router eval status and scoped memory counts.
- Run `pnpm eval:router` or `POST /api/router/evals/run` after classifier changes.
- Test every OpenAPI/MCP connector with `POST /api/connectors/:id/test` before routing live workflows through it.
- Confirm connector secrets are stored only through `/api/secrets`; connector records should contain secret refs, not token values.
- Back up SQLite, artifacts/workspaces, and `KELPCLAW_SECRET_MASTER_KEY` as one recovery unit.

## Operational Smoke Tests

- `GET /health` returns `kelpclaw-api`.
- Unauthorized calls to `/api/secrets`, workflow audit, run events, and revision routes return 401.
- `/api/secrets` lists only metadata and integration readiness, never raw secret values.
- Google OAuth connect, callback, status, and revoke complete against a configured OAuth client.
- A workflow can be planned, validated, evaluated, approved, deployed as a runner configuration, run, and inspected after API restart.
- `GET /api/workflows/:id/audit` shows create/edit/approve/adapter/delivery/run records.
- `GET /api/workflows/:id/audit/export` returns redacted JSONL including deployment, budget, provider, and decision trace records.
- `GET /api/workflows/:id/runtime-truth` shows `runnable` only after an active runner deployment exists.
- `GET /api/ops/health` reports database, worker, scheduler, run, job, and connector state.
- `POST /api/router/evaluate` returns route scores, confidence, alternatives, matched signals, and model requirements.
- `GET /api/router/evals` lists the checked-in router eval corpus; `POST /api/router/evals/run` must pass before release.
- `GET /api/workflows/:id/memory` lists only redacted scoped agent memory records for the workflow.
- OpenAPI import creates operations, allowed hosts, auth requirements, and test status.
- MCP registration lists tool operations from a Streamable HTTP MCP server.
- `GET /api/workflows/:id/runs/:runId/events` shows structured events with workflow, revision, run, severity, and correlation ids.
- `GET /api/workflows/:id/runs/:runId` includes checkpoint records after worker execution.
- Failed-run replay creates a new queued run from the original approved revision and deployment.
- `kelp-claw cross-agent-replay-smoke` returns `ok: true` with the same replay shape for `claude-code`, `codex-cli`, and `goose`.
- Schedule pause/resume survives API restart and `workflow_schedules` rows show next fire, last fire, and missed count.
- Missing live secrets produce failed structured run output instead of falling back to mock adapters.
- Database Query is read-only; Database Execute requires the connection secret to opt in with `allowWrites=true`.
- Generated artifact hash drift blocks approval or execution.
- `kelp-claw otlp-smoke` succeeds against the deployment OTLP endpoint and reports one trace with one span per smoke tool call.
- `KELPCLAW_LIVE_SMOKE=1 pnpm smoke:live` succeeds against explicit test recipients and sheet ids.

## Retention And Recovery

- Set per-workflow retention policy through `GET/PATCH /api/workflows/:id/retention`.
- Keep failed-run artifacts longer than successful-run artifacts while debugging new workflows.
- Preserve failed runs until replay or incident review is complete.
- Verify backup restore by starting the API against a restored SQLite database and checking `/api/ops/health`.
- After an unclean API stop, expect running jobs to requeue when retry attempts remain and running runs to become `resuming`.

## Alert Policies

- Configure per-workflow policies through `GET/PATCH /api/workflows/:id/alerts`.
- V1 policies record alert lifecycle events for matched failures and use existing email, Telegram, or webhook adapter delivery when those secrets are present.
- Treat alert delivery as best-effort single-host notification, not a replacement for external monitoring.

## Agent Runtime Controls

- Router classification is deterministic. Each route includes per-route scores, confidence, matched signals, alternatives, and classifier version.
- Agentic nodes may use legacy `agentic.tools` for builtin tools and structured `toolGrants` for builtin, MCP, or adapter tools.
- Policy denies undeclared hosts, undeclared secret refs, missing operation metadata, and write side-effect tool grants. Denials are recorded in runtime trace metadata.
- Agent memory is structured SQLite memory, not vector search. Scopes are `none`, `node`, `workflow`, and `workspace`; workspace reads require shareable records in the same namespace.
- Memory writes are redacted before persistence and must come from structured `memoryWrites` in agent output. Raw secrets, authorization headers, and provider transcripts are not stored as memory.
- OpenClaw's Agent Runtime panel shows route scores, eval status, memory records, and runtime policy/memory trace events.

## Known Boundaries

- This is single-host HMAC-signed or server-mapped RBAC, not a multi-tenant identity provider.
- SQLite is the durable store for this phase; move to managed SQL before multi-host writes.
- Local encrypted secrets are suitable for self-hosting; use a managed secret manager before multi-tenant SaaS.
- Provider OAuth beyond Google is still configured by service tokens/secrets, not per-user OAuth.
- Retry and checkpoint recovery are sequential single-host mechanisms, not Temporal-style distributed orchestration.
- Compensation emits `compensation.required` events for completed side-effect nodes; it does not auto-run compensation unless a workflow explicitly models that step.
- Agent memory retrieval is deterministic scoped lookup; embeddings and external vector stores are not part of this tranche.
- Central log shipping and external incident management remain deployment responsibilities.
- Agent-run hash chains are tamper-evident, not tamper-proof.
- `kelp-claw audit-anchor <runId>` writes a local JSONL anchor of the current chain head and can also POST the same anchor to `KELPCLAW_AUDIT_ANCHOR_ENDPOINT`. External TSA, WORM, or object-lock storage is still a deployment responsibility.
- OTLP/Datadog export depends on a configured OTLP endpoint. Without `KELPCLAW_OTLP_TRACES_ENDPOINT`, `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`, or an OTLP base endpoint, KelpClaw cannot verify external trace intake.
- KelpClaw produces evidence-ready records for governance review; it does not certify regulatory compliance.
