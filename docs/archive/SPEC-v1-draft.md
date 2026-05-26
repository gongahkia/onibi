# Onibi Approval Protocol — SPEC v0.1 (draft)

Wire protocol between AI-agent adapters, the Onibi daemon, and phone/web clients. Vendor-neutral: any agent that can shell out and block on an HTTP response can integrate; any client that speaks HTTP+JSON+WebSocket can be a control surface.

This document is normative. `WORKON-PIVOT-ASAP.md` is the project plan; this file is the contract.

## Status

| Version | Status | Notes |
|---|---|---|
| `0.1` | draft | this document; Phase 0 deliverable |
| `1.0` | planned | Phase 1 freeze; backward-compatibility starts here |

Until `1.0`, breaking changes are permitted. After `1.0`, breaking changes require a major bump.

## Scope

In scope:
- adapter → daemon: approval requests, run lifecycle events
- daemon → client: pending approval inbox, realtime push
- client → daemon: approval decisions (allow / deny / allow-with-edits)

Out of scope (v1):
- multi-stakeholder approval (N-of-M)
- approval policy DSL (auto-approve patterns)
- federation between daemons
- agent-to-agent handoff

## Roles

| Role | Process | Speaks |
|---|---|---|
| **Adapter** | hook script invoked by an AI coding agent (Claude Code, Codex, opencode, ...) | HTTP client → daemon |
| **Daemon** | `onibid` (Go binary) running on the host machine | HTTP + WebSocket server |
| **Client** | phone PWA, web PWA, or future native app | HTTP + WebSocket client |

Adapter and daemon usually share a machine. Client is remote (over Tailscale Funnel or LAN).

## Transport

- HTTP/1.1 or HTTP/2 over TLS (TLS terminated by Tailscale Funnel or local LAN HTTP for dev only)
- WebSocket over the same origin
- JSON (UTF-8) for all payloads
- `Content-Type: application/json` required on requests with bodies

## Authentication

Single shared **pairing token** per daemon, generated on first run, stored in OS keychain (macOS) / secret-service (Linux) / encrypted file (fallback).

- HTTP: `Authorization: Bearer <token>` on every request except `/v1/health`
- WebSocket: first frame after `open` must be an `auth` envelope (see below); server replies `auth_ok` or closes with `1008`

v1.1 will add per-device tokens with revocation; v0/v1.0 use one shared token.

### Auth failure

- HTTP: `401 Unauthorized`, body: `{"error":"unauthorized"}`
- WebSocket: close code `1008` ("policy violation"), reason `unauthorized`

## Common types

### IDs

All IDs are opaque strings, ≤ 64 chars, ASCII, URL-safe. Daemon generates them; adapters and clients echo them back.

- `machine_id` — stable per host (random on first daemon start, persisted)
- `run_id` — one per agent invocation
- `approval_id` — one per pending-approval lifecycle
- `event_id` — monotonic per `run_id`

### Timestamps

RFC 3339, UTC, millisecond precision. Example: `2026-05-15T14:30:00.123Z`.

### Envelope (WebSocket only)

```json
{
  "type": "<message_type>",
  "v": "0.1",
  "ts": "2026-05-15T14:30:00.123Z",
  "data": { ... }
}
```

HTTP responses are bare JSON objects (no envelope) for ergonomics with `curl`.

## HTTP API

Base path: `/v1`. All routes require `Authorization: Bearer <token>` unless noted.

### `GET /v1/health`

Unauthenticated liveness probe. For tunnel/load-balancer health checks.

Response `200`:
```json
{ "status": "ok", "version": "0.1.0" }
```

### `GET /v1/bootstrap`

Single-shot initial state for a freshly connected client. Snapshots are also delivered over WebSocket after auth, but this endpoint lets clients render before WS handshake completes.

Response `200`:
```json
{
  "protocol_version": "0.1",
  "host_version": "0.1.0",
  "machine_id": "m_8f3a...",
  "machine_label": "macbook-pro",
  "features": {
    "approval_edit": true,
    "run_feed": true
  },
  "pending_approvals": [ /* Approval objects */ ],
  "recent_runs": [ /* Run summary objects, last 50 */ ]
}
```

### `POST /v1/approval/request`

Called by an adapter when the agent is about to execute a tool that requires approval. **Long-polls — the response is the user's decision.**

Request body:
```json
{
  "machine_id": "m_8f3a...",
  "run_id": "r_4c91...",
  "agent": "claude-code",
  "agent_version": "2.0.10",
  "tool": "Bash",
  "input": {
    "command": "rm -rf node_modules",
    "description": "Clean dependencies"
  },
  "context": {
    "cwd": "/Users/me/project",
    "session_id": "claude-session-abc",
    "tail": [
      "> running tests...",
      "FAIL  src/foo.test.ts"
    ]
  },
  "deadline_ms": 600000
}
```

Field notes:
- `tool` — agent-specific name; daemon does not interpret
- `input` — opaque JSON; passed back to adapter on decision, possibly mutated
- `context.tail` — last N lines of agent output for client UX; max 200 lines, max 64 KiB total
- `deadline_ms` — adapter's max wait; server enforces this with `408` if exceeded

Response `200` (decision received):
```json
{
  "approval_id": "a_77b2...",
  "decision": "allow" | "deny" | "allow_with_edits",
  "updated_input": { ... },
  "reason": "user-supplied free text",
  "decided_at": "2026-05-15T14:31:02.005Z",
  "decided_by_device": "iphone-fingerprint"
}
```

- `updated_input` MUST be present iff `decision == "allow_with_edits"`; same JSON shape as request `input`
- `reason` is optional free-text from the user; surfaced back to the agent

Response `408 Request Timeout` if `deadline_ms` elapsed.
Response `503` if daemon is shutting down; adapter should fail-open (allow without prompt) or fail-closed (deny) per its own policy — see Adapter contract.

### `GET /v1/approval/pending`

List currently-blocked approval requests. Clients use this on cold start; live updates arrive via WebSocket.

Response `200`:
```json
{
  "approvals": [
    {
      "approval_id": "a_77b2...",
      "machine_id": "m_8f3a...",
      "run_id": "r_4c91...",
      "agent": "claude-code",
      "tool": "Bash",
      "input": { ... },
      "context": { ... },
      "requested_at": "2026-05-15T14:30:55.000Z",
      "expires_at": "2026-05-15T14:40:55.000Z"
    }
  ]
}
```

### `POST /v1/approval/:approval_id/decide`

Client submits a decision for a pending approval.

Request body:
```json
{
  "decision": "allow" | "deny" | "allow_with_edits",
  "updated_input": { ... },
  "reason": "user typed this"
}
```

Response `200`:
```json
{ "approval_id": "a_77b2...", "status": "resolved" }
```

Response `404` if `approval_id` unknown or already resolved.
Response `400` if `decision == "allow_with_edits"` and `updated_input` is missing, malformed, or shape-incompatible with the original `input`.
Response `409` if approval already resolved by another client.

After this call, the long-polling adapter's `/v1/approval/request` returns with the decision, and all subscribed WebSocket clients receive an `approval_resolved` push.

### `POST /v1/run/event`

Adapter posts run lifecycle events. Fire-and-forget; not blocking.

Request body:
```json
{
  "machine_id": "m_8f3a...",
  "run_id": "r_4c91...",
  "event_id": 42,
  "type": "run_started" | "tool_use" | "output_chunk" | "run_completed",
  "data": { ... }
}
```

Event-specific `data`:

- `run_started`: `{ "agent": "claude-code", "agent_version": "2.0.10", "cwd": "/...", "command": "claude code 'fix tests'", "started_at": "..." }`
- `tool_use`: `{ "tool": "Bash", "input": { ... }, "approval_id": "a_77b2..." | null }`
- `output_chunk`: `{ "stream": "stdout" | "stderr" | "agent", "text": "..." }` — text may be ANSI-stripped or raw, daemon stores as-is
- `run_completed`: `{ "exit_code": 0, "finished_at": "...", "duration_ms": 12450 }`

Response `204 No Content` on success.
Response `429` if rate-limited (output flooding); adapter SHOULD back off.

## WebSocket protocol

Endpoint: `WS /v1/realtime`.

### Auth handshake

First frame from client:
```json
{ "type": "auth", "v": "0.1", "data": { "token": "<bearer>" } }
```

Server responds:
```json
{
  "type": "auth_ok",
  "v": "0.1",
  "ts": "2026-05-15T14:30:00.000Z",
  "data": {
    "protocol_version": "0.1",
    "host_version": "0.1.0",
    "machine_id": "m_8f3a...",
    "session_id": "ws_2d3f..."
  }
}
```

Or closes connection with `1008 unauthorized`.

After `auth_ok`, server pushes events without explicit subscription. Clients filter locally. (v1.1 may add `subscribe` for bandwidth control on multi-machine.)

### Server push messages

```json
{ "type": "approval_pending", "v": "0.1", "ts": "...", "data": <Approval> }
{ "type": "approval_resolved", "v": "0.1", "ts": "...", "data": <Approval + decision> }
{ "type": "run_started",       "v": "0.1", "ts": "...", "data": <RunSummary> }
{ "type": "run_output_tail",   "v": "0.1", "ts": "...", "data": { "run_id": "...", "lines": ["..."] } }
{ "type": "run_completed",     "v": "0.1", "ts": "...", "data": <RunSummary + exit> }
{ "type": "host_status",       "v": "0.1", "ts": "...", "data": { "machine_id": "...", "online": true } }
```

### Client → server messages

Currently only `auth`. Reserved (rejected with `400` envelope) for future use: `subscribe`, `unsubscribe`, `ping`.

### Token rotation

When the host token is rotated, the daemon closes all WebSockets with `1008 token_rotated`. Clients re-pair via QR / deep link, then reconnect. Long-polling HTTP `/v1/approval/request` calls in flight are NOT terminated — they continue with the old auth context until natural completion or `deadline_ms`. Rationale: terminating in-flight approvals would leave agents hung.

### Heartbeat

Server sends a WebSocket `ping` frame every 30s. Client MUST respond with `pong`. After 2 missed pongs (60s), server closes with `1011 server_error` reason `heartbeat`. Clients SHOULD auto-reconnect with exponential backoff (1s, 2s, 4s, 8s, cap 30s).

## Adapter contract

Each adapter is a small executable invoked by its host AI agent's pre-tool-use hook. Adapter responsibilities:

1. Receive the agent's tool call (via stdin JSON or env vars, agent-specific)
2. POST to `/v1/approval/request` with the tool details
3. Map the daemon's response back into the agent's hook output format
4. Optionally POST `/v1/run/event` for observability

Adapters MUST tolerate:
- daemon offline (`ECONNREFUSED`) — adapter's `--fail-open` flag decides allow vs deny
- daemon 5xx — same fail-open/fail-closed policy
- daemon 408 (deadline) — same

Default policy is **fail-closed** (deny) for safety; users can override per-adapter via the adapter's install config.

### Claude Code (lead adapter, Phase 1)

Hook: `PreToolUse` HTTP hook, configured in `~/.claude/settings.json`.

Daemon exposes this hook at `POST /v1/adapters/claude-code/pretool` which internally calls `/v1/approval/request`. The hook URL + bearer token are written to `~/.claude/settings.json` by `onibi adapter install claude-code`.

Mapping daemon decision → Claude Code hook response:

| Daemon decision | Claude Code hook response |
|---|---|
| `allow` | `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}` |
| `deny` | `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"<reason>"}}` |
| `allow_with_edits` | `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","updatedInput":<updated_input>}}` |

**Minimum Claude Code version: `2.0.10`** (introduced `updatedInput`). Adapter install refuses older versions.

Default `deadline_ms`: `540000` (9 min — leaves margin under Claude Code's 10-min default hook timeout).

### Codex CLI (Phase 3)

Hook: `~/.codex/hooks.json`, script reads JSON from stdin, writes JSON to stdout.

Mapping daemon decision → Codex hook response:

| Daemon decision | Codex hook response |
|---|---|
| `allow` | `{"hookEventName":"PreToolUse","permissionDecision":"allow"}` |
| `deny` | `{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"<reason>"}` |
| `allow_with_edits` | adapter MUST internally degrade to `deny` with `permissionDecisionReason` describing the edit, because Codex does not currently support tool-input mutation [Unverified as of 2026-05-15] |

Limitation: Codex hooks fire reliably for `Bash` tool calls but not `apply_patch` or most MCP tool calls. Phone approvals therefore only intercept `Bash` for Codex runs.

### opencode (Phase 3, hook surface TBD)

Stub. Phase 3 spike determines whether opencode exposes a pre-tool-use intercept; if not, file an upstream issue and ship a passive-observability-only opencode adapter (run events without approval gating).

### Other adapters (community PRs)

Adapter contract for new agents:
1. Implement the `Adapter` Go interface in `internal/adapters/`
2. Document the mapping table above for the new agent
3. Add an `onibi adapter install <name>` command that writes the agent's hook config
4. Provide an integration test that spawns the adapter, posts a tool call, asserts the hook response

## Hook composition

A user may have their own PreToolUse hooks (linters, auditors) configured alongside Onibi's.

Onibi's hook is written **last** in the agent's hook list, so other hooks (deny-style or modify-style) take precedence. If a prior hook returns `deny`, Onibi never sees the tool call — correct behavior. If a prior hook returns `allow_with_edits`, Onibi sees the modified input and gates on that.

If a prior hook returns `defer`, Onibi proceeds normally.

This is documented in the adapter install output and in `onibi adapter status`.

## Error codes

Standard HTTP semantics. Common Onibi-specific codes:

| Code | Meaning |
|---|---|
| `400` | malformed body, schema mismatch, missing required field |
| `401` | bad or missing bearer token |
| `404` | unknown approval_id, run_id, or route |
| `408` | adapter `deadline_ms` elapsed without a user decision |
| `409` | approval already resolved |
| `429` | rate limited (run_event flood) |
| `503` | daemon shutting down |

Error body shape:
```json
{ "error": "kebab-case-code", "message": "human readable" }
```

## Security summary

This SPEC does not redefine the security model — see `docs/security.md` (post-Phase 5 rewrite) for the threat model. Notable invariants this protocol relies on:

- The bearer token is the **only** credential; anyone holding it has full approval authority.
- The host machine MUST NOT trust adapter requests beyond their pairing-token-authenticated bounds. The daemon does not invoke any tool itself; it only relays decisions.
- The `updated_input` field for `allow_with_edits` is **trusted from the client and passed through to the agent without re-validation**. The agent (Claude Code, Codex, etc.) remains responsible for sandboxing its tool execution. Onibi is a control surface, not a sandbox.

## Version negotiation

Both adapter and client send their understood protocol version. Daemon SHOULD accept any `0.x` from clients during v0 (loose) and exact-match for v1.x (strict).

- `auth_ok.data.protocol_version` = daemon's protocol version
- `auth_ok.data.host_version` = daemon's semver (separate from protocol)
- adapters include their `agent_version` per request so the daemon can flag known-bad versions in the UI

## Open questions for v1.0 freeze

1. Whether to support partial `updated_input` (JSON merge patch) vs. full replacement. v0 says full replacement; pressure-test in Phase 2 PWA work.
2. Whether `run_output_tail` should be byte-bounded or line-bounded server-side. v0 says line-bounded (200 lines / 64 KiB).
3. Whether to expose per-adapter health / last-seen as a first-class API or fold into `/v1/bootstrap`.
4. Concurrency: what happens if two clients click "allow" simultaneously. v0 says first-write-wins with `409` to the loser; revisit if PWA UX gets confusing.
5. Whether `deny` should be allowed to also carry an `updated_input` as a "deny but here's what I would have allowed" hint to the agent. Probably no — that's what edit-and-allow is for.

These are tracked here, not in `WORKON-PIVOT-ASAP.md`, because they're protocol-shape questions specific to this contract.
