# Certified adapter contract

This is the v1 contract for the certified coding-agent adapters: Claude, Codex, and Pi. Other installed adapters are deferred or experimental and are not covered by this contract.

`onibi agent status --json` reports `certified: true` and a versioned `contract` object for these three adapters. The CLI is the authoritative machine-readable capability report.

## Installation

Each certified adapter uses managed, idempotent hook installation; records an integrity hash; and backs up the original config before modification. `onibi agent inspect --agent <name>` reports expected and observed hook state. Do not trust a hook command without inspecting it.

## Approval decisions

All certified hooks deliver requests over the same-UID Unix socket. While that socket and daemon are reachable, they block the tool call and map decisions as follows:

| Onibi decision | Adapter effect |
| --- | --- |
| `approve` | allow |
| `deny` | deny |
| `edited` | allow with replacement input |
| `expired` | deny |

The hooks intentionally allow if the daemon is unavailable or the local request times out. This is reported as `daemon_unavailable: "allow"` and `request_timeout: "allow"`; it is not an enforcement guarantee for those paths. `cancelled` also allows, so daemon shutdown does not strand a local agent.

Codex additionally requires its user to review and trust the current non-managed hook definitions before they run. Its capability report sets `review_required: true`; Onibi can verify its hook file and recorded hash, but cannot verify Codex's persisted trust decision. Until Codex trusts the current definitions, Codex approval enforcement is unavailable.

## Approval payload v1

Certified adapters submit `onibi.approval.v1` over the local intake socket. The model contains `session_id`, `agent`, `tool`, and a JSON-object `input`. Onibi rejects unsupported schema versions and non-object inputs, canonicalizes the input, then derives `details` and `risk` itself. Adapter-supplied risk, target, command, and path values are not trusted by the phone cockpit.

Web approval responses expose a nested `approval` object with the version, scrubbed input, derived details, and derived risk. They do not expose the raw input. Legacy top-level fields remain for existing cockpit clients.

## Lifecycle and recovery

Every certified adapter reports session start, activity, approval requests, and turn completion. Claude and Pi also report session exit; Codex currently reports no session-exit event and exposes `session_exit: false`.

Pending approval rows survive daemon restart for inspection and audit, but a parked hook connection cannot be reattached. The contract reports that limitation explicitly. Reload or review changed hooks as follows:

- Claude: run `claude` and inspect `/hooks`.
- Codex: run `codex`, review hooks, and trust matching commands.
- Pi: run `/reload`.

Pi reload and session replacement emit `session_shutdown` before a fresh extension instance receives `session_start`; Onibi maps those events to session exit and activity. Pi executes edited tool input without re-validating it, so Onibi accepts edited approval input only when it is a JSON object. An invalid Pi approval response or edited input blocks the tool.

## Audit

Approval decisions are audit rows containing a payload SHA-256 and compact decision metadata. Raw tool payloads, including replacement input, must not be written to audit detail. The queue is idempotent: only its first pending-to-terminal decision wins.
