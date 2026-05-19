# Production Readiness Checklist

## Required Before Production

- Replace single-file SQLite with a managed database if the API has more than one writer.
- Back the artifact store with immutable object storage and lifecycle policies.
- Use a real secret manager-backed `SecretResolver`; do not rely on process env for shared production workers.
- Ship logs, audit records, and run events to a centralized retention system.
- Pin all runtime container images to explicit tags or digests and scan generated dependency manifests.
- Keep NanoClaw workspaces isolated per run and per node, with default-deny network policy.
- Require generated code review before approval and preserve approved revisions as immutable records.
- Run planner, skill-selection, DAG determinism, codegen replay, adapter mock, OpenClaw E2E, and Docker integration suites in CI.

## Operational Smoke Tests

- `GET /health` returns `kelpclaw-api`.
- A draft workflow can be planned, validated, approved, run, and fetched after API restart.
- `GET /api/workflows/:id/audit` shows create/edit/approve/run/delivery records.
- `GET /api/workflows/:id/runs/:runId/events` shows structured events with workflow id, revision id, run id, severity, and correlation id.
- Generated artifact hash drift blocks approval or execution.
- Secret references are visible as references in audit records, while raw token values are redacted from logs, event metadata, and adapter invocation echoes.
