# PHASE-03 — Adapters + Approval Gating

> Dated 27 May 2026. Depends on `PHASE-01-PLAN-27-MAY.md`. Parallel to `PHASE-02-PLAN-27-MAY.md`. Authoritative for `SPEC.md`.

## Context

The functional core. PHASE-03 builds the **approval protocol**: agent hooks block on Onibi, Onibi presents the pending decision to the user, user decides, hook unblocks. This is what makes Onibi useful — a terminal alone doesn't justify existing alongside cmux. Mobile (PHASE-04) and transports (PHASE-05) both consume this protocol.

The protocol must be vendor-neutral (third parties can write adapters or alternative phone clients) and versioned. It is documented in `SPEC.md` at repo root, frozen at v1.0 for the launch.

The hard dependency: Claude Code's `PreToolUse` hook returning `{permissionDecision: "allow", updatedInput: {...}}` (shipped in v2.0.10, January 2026). Without this, "edit-then-approve" collapses to a clumsy deny-and-suggest loop costing an extra LLM turn.

## Dependencies

- PHASE-01 merged (PtyManager exists).
- PHASE-02 either merged or in flight; the `Session.pendingApprovals: string[]` field on the session model is **owned by PHASE-02** but consumed here. Coordinate via shared `app/src/lib/sessions.ts` types.
- Claude Code v2.0.10+ installed on the dev machine for end-to-end testing.

## Deliverables

### D1 — SPEC.md (v1.0 protocol freeze)

**File**: `SPEC.md` (rewrite from scratch; previous v1 daemon spec is archived at `docs/archive/SPEC-v1-draft.md`).

Sections:

#### 1. Overview
- Single Rust binary, dual mode: GUI (Tauri) or `--headless`. Same HTTP+WS surface.
- HTTP server binds on a chosen port (default 17893, configurable). Bound to localhost in GUI mode by default; transports (PHASE-05) publish externally.
- All payloads JSON. Server-Sent Events deferred to v1.1 (WS-hostile networks).
- Auth: single shared bearer token, `Authorization: Bearer <token>`. Generated on first boot.

#### 2. Identity
- `machine_id`: ULID generated on first boot. First-class field on every approval, run event, and WS message.
- `session_id`: ULID per PTY session.
- `approval_id`: ULID per approval request.

#### 3. Endpoints

`POST /v1/approval/request`
- Adapter posts; daemon long-polls (default 600s timeout, 10 min) for a decision.
- Body:
  ```json
  {
    "machine_id": "01J...",
    "session_id": "01J...",
    "agent": "claude-code|codex|opencode|...",
    "tool": "Bash|Edit|Write|...",
    "input": {...},          // arbitrary JSON; agent-specific
    "cwd": "/path/to/cwd",
    "metadata": {"prompt": "..."}
  }
  ```
- Response (long-polled):
  ```json
  {
    "approval_id": "01J...",
    "decision": "allow|deny",
    "updatedInput": {...} | null,  // present iff edited
    "reason": "..." | null
  }
  ```

`GET /v1/approval/pending` → list of in-flight approvals (clients use to populate inbox).

`POST /v1/approval/:id/decide`
- Body: `{decision: "allow"|"deny", updatedInput?: {...}, reason?: "..."}`
- 200 on success; 409 if already decided (race: another client beat us).

`POST /v1/run/event`
- Lifecycle events from adapters:
  ```json
  {
    "machine_id": "...",
    "session_id": "...",
    "kind": "started|tool-use|output-chunk|completed",
    "timestamp": "RFC3339",
    "payload": {...}
  }
  ```

`POST /v1/pty/output`
- High-bandwidth tail. Adapters or PTY manager push chunks (base64-encoded).
- Server fans out via WS only; not persisted past in-memory rolling buffer (last 5000 bytes per session).

`GET /v1/qr` → PNG; pairing QR encoding `{host, port, token, cert_fingerprint?, transports: [...]}`. Used by mobile in PHASE-04.

`POST /v1/pair`
- Body: `{deviceLabel: "iPhone-15", pushSubscription?: WebPushSubscription}`
- Response: `{deviceId: "01J...", machineId: "..."}` (deviceId stored for v1.1 revocation; in v1.0 it's the same single bearer token, deviceId is informational only).

`WS /v1/realtime`
- Handshake: `Authorization: Bearer <token>` header.
- Server pushes JSON messages, one per line:
  ```json
  {"type": "approval-pending", "approval_id": "...", "agent": "...", "tool": "...", ...}
  {"type": "approval-resolved", "approval_id": "...", "decision": "allow", "by": "deviceId"}
  {"type": "run-event", "kind": "completed", "session_id": "...", "exit_code": 0}
  {"type": "pty-output", "session_id": "...", "data": "<base64>"}
  {"type": "ping"}
  ```
- Client → server: `{"type": "pong"}` heartbeat every 30s; server disconnects after 90s silence.

#### 4. Compatibility
- All envelope objects carry `"protocol_version": "1.0"`. Clients reject mismatches with 426 Upgrade Required.

#### 5. Failure modes
- Adapter long-poll timeout (no decision in 10 min) → respond `{decision: "deny", reason: "timeout"}`.
- Adapter posted while no client connected → store approval as pending in DB; first client to attach gets it.
- Restart: reload pending approvals from DB; emit `approval-pending` events on WS reconnect.

### D2 — Rust HTTP + WS server

**Crates** (add to `app/src-tauri/Cargo.toml`):
```toml
axum = { version = "0.7", features = ["ws"] }
tower = "0.5"
tower-http = { version = "0.5", features = ["cors", "trace"] }
tokio = { version = "1", features = ["full"] }
tokio-tungstenite = "0.21"
rusqlite = { version = "0.31", features = ["bundled"] }
r2d2 = "0.8"
r2d2_sqlite = "0.24"
ulid = { version = "1", features = ["serde"] }
base64 = "0.22"
```

**Module layout**:
```
app/src-tauri/src/
├── server/
│   ├── mod.rs            # start_server() spawned from main.rs and headless.rs
│   ├── routes.rs
│   ├── ws_hub.rs
│   ├── auth.rs           # bearer token middleware
│   └── pairing.rs        # QR generation (use `qrcodegen` crate)
├── approval/
│   ├── mod.rs
│   ├── store.rs          # sqlite
│   └── pending.rs        # in-memory map: approval_id -> oneshot::Sender<Decision>
└── protocol/
    ├── mod.rs            # serde types matching SPEC.md
    └── version.rs        # 1.0 const
```

`approval::store` schema:
```sql
CREATE TABLE IF NOT EXISTS approvals (
  approval_id TEXT PRIMARY KEY,
  machine_id  TEXT NOT NULL,
  session_id  TEXT NOT NULL,
  agent       TEXT NOT NULL,
  tool        TEXT NOT NULL,
  input       TEXT NOT NULL,         -- JSON
  cwd         TEXT NOT NULL,
  metadata    TEXT,                  -- JSON
  decision    TEXT,                  -- NULL until decided
  updated_input TEXT,
  reason      TEXT,
  decided_by  TEXT,
  created_at  INTEGER NOT NULL,      -- unix millis
  decided_at  INTEGER
);
CREATE TABLE IF NOT EXISTS run_events (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  machine_id  TEXT NOT NULL,
  session_id  TEXT NOT NULL,
  kind        TEXT NOT NULL,
  payload     TEXT NOT NULL,
  ts          INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS devices (
  device_id   TEXT PRIMARY KEY,
  label       TEXT,
  push_subscription TEXT,
  created_at  INTEGER NOT NULL,
  last_seen   INTEGER
);
CREATE INDEX IF NOT EXISTS idx_approvals_undecided ON approvals(decided_at) WHERE decided_at IS NULL;
```

Connection pool: `r2d2::Pool<r2d2_sqlite::SqliteConnectionManager>`. DB path: `~/.config/onibi/onibi.db` (use `directories` crate for cross-platform paths).

`ws_hub`:
- `broadcast::Sender<ServerMessage>` shared across all subscribers.
- Each WS client subscribes; messages serialised once, fanned out to all sockets.

**Server startup** (in `app/src-tauri/src/server/mod.rs`):
```rust
pub async fn start_server(state: AppState, port: u16) -> Result<()> {
    let app = Router::new()
        .route("/v1/approval/request", post(routes::approval_request))
        .route("/v1/approval/pending", get(routes::approval_pending))
        .route("/v1/approval/:id/decide", post(routes::approval_decide))
        .route("/v1/run/event", post(routes::run_event))
        .route("/v1/pty/output", post(routes::pty_output))
        .route("/v1/pair", post(routes::pair))
        .route("/v1/qr", get(routes::qr))
        .route("/v1/realtime", get(ws::realtime))
        .layer(middleware::from_fn(auth::require_bearer))
        .with_state(state);
    axum::serve(TcpListener::bind(("0.0.0.0", port)).await?, app).await?;
    Ok(())
}
```

Called from `main.rs` after Tauri ready, and from `headless.rs` standalone.

### D3 — Auth + secrets

**Module**: `app/src-tauri/src/secret/`

Use `keyring` crate (cross-platform: macOS Keychain, Linux Secret Service, Windows Credential Manager). On Linux without Secret Service (Pi headless), fall back to file at `~/.config/onibi/token.txt` mode 0600 (warn in logs).

Generate token on first boot: 32 random bytes → base64url. Persist. Re-load on every subsequent boot.

`POST /v1/token/rotate` (CLI-only via local socket; not exposed externally): generates new token, invalidates old in-memory cache, prints new for pairing.

CLI command `onibi token rotate` walks the same path.

### D4 — Adapters

Each adapter is a Rust module + a hook-installation template stored at `adapters/<name>/`.

#### `app/src-tauri/src/adapters/claude_code.rs`
Provides:
- `pub fn install() -> Result<()>` writes the hook into `~/.claude/settings.json` (preserving other hooks). Use `serde_json` to merge.
  ```json
  {
    "hooks": {
      "PreToolUse": [
        {
          "url": "http://127.0.0.1:17893/v1/adapters/claude-code/hook",
          "timeout": 600000,
          "headers": {"Authorization": "Bearer ${ONIBI_TOKEN}"}
        }
      ]
    }
  }
  ```
- `pub fn uninstall() -> Result<()>` removes the matching hook entry.
- HTTP handler at `/v1/adapters/claude-code/hook` (registered in server routes) — receives Claude's PreToolUse payload, transforms to approval request, blocks until decision, returns `{permissionDecision, updatedInput}` per Claude Code's schema.

Minimum supported Claude Code version detection: invoke `claude --version`, parse semver. If `< 2.0.10`, refuse install with clear message.

#### `app/src-tauri/src/adapters/codex.rs`
- Writes `~/.codex/hooks.json`:
  ```json
  {
    "hooks": [
      {
        "event": "tool",
        "tool": "Bash",
        "command": ["onibi", "_hook", "codex"]
      }
    ]
  }
  ```
- The `_hook codex` CLI subcommand reads stdin JSON, POSTs `/v1/approval/request`, blocks, writes JSON to stdout per Codex's schema (`{"permissionDecision": "allow"|"deny"}`).
- `apply_patch` not interceptable — documented as limitation in `docs/adapters.md`.

#### `app/src-tauri/src/adapters/opencode.rs`
- Phase 03 spike (1 day): determine hook surface. If absent, file upstream issue, stub adapter to do run-event mirroring only.

#### `app/src-tauri/src/adapters/{gemini,aider,cursor,goose}.rs`
- Stub: register agent kind, install no-op (or run-event-only via shell wrapper if hook surface verified).
- Document in `docs/adapters.md` what level of support each agent gets:
  - **Full**: approval intercept + edit + run events
  - **Partial**: run events only (mirror)
  - **Stub**: nothing wired; agent appears in tab bar with shell session

For Phase 03 v1.0 launch:
- **Full**: Claude Code, Codex (Bash-only)
- **Partial / Stub**: OpenCode, Gemini, Aider, Cursor, Goose (depending on spike results)

### D5 — CLI

**File**: `app/src-tauri/src/cli/mod.rs`

The same Rust binary, when invoked with arguments, dispatches to CLI subcommands instead of launching Tauri.

```
onibi setup                          # interactive wizard
onibi status                         # daemon status / health
onibi token rotate                   # regen bearer token
onibi token show                     # print current (for headless setup)
onibi adapter list                   # available + installed agents
onibi adapter install <name>         # install hooks
onibi adapter uninstall <name>
onibi _hook <name>                   # internal: stdin->approval->stdout shim
onibi --headless [--port N]          # daemon-only mode (PHASE-06)
```

Use `clap` (derive macros):
```toml
clap = { version = "4", features = ["derive"] }
```

Detect CLI mode in `main.rs`: if `argc > 1` and arg[1] isn't a known Tauri arg, route to clap.

### D6 — Desktop UI: ApprovalModal

**File**: `app/src/components/ApprovalModal.tsx`

Triggered by WS `approval-pending` events from a local in-process Tauri-side bridge (route the WS messages through Tauri events so the React app doesn't have to authenticate against its own server).

Mounts:
- Agent name + icon
- Tool name (badge)
- Command / input (monospace, syntax-highlighted only with `<pre>` tags — no real syntax highlight)
- cwd
- Last 50 lines of run output (from in-memory PTY ring buffer)
- Three primary buttons: Allow (green), Edit (yellow), Deny (red)
- Edit → expands to a `<textarea>` pre-filled with the input (for Bash, the command string; for Edit tool, the diff)
- Submit Edit → calls `/v1/approval/:id/decide` with `{decision: "allow", updatedInput: {...}}`

Auto-dismiss if another client decides first (WS `approval-resolved`); show toast "Resolved on <deviceLabel>".

Tab flash status (from PHASE-02's session store) is set to `"awaiting-approval"` on receipt of pending; cleared on resolved.

### D7 — Tests

Rust:
- `approval::store::tests`: roundtrip insert + decide + load-pending-on-boot.
- `server::routes::tests` (using `axum-test` or `tower::ServiceExt`): post approval → simulate decide in another task → original POST returns expected JSON.
- `adapters::claude_code::tests`: install/uninstall preserves existing hooks; minimum-version check rejects v2.0.9.

Frontend:
- `ApprovalModal.test.tsx`: renders pending event, Allow click POSTs decide.
- E2E: spawn a mock "agent" process that POSTs to `/v1/approval/request`, simulate UI click, assert mock receives expected decision JSON.

### D8 — Integration test

End-to-end test as a shell script (`scripts/e2e-claude.sh`):

```sh
#!/usr/bin/env bash
set -euo pipefail
# Assumes onibi running in headless on port 17893, Claude Code installed
onibi adapter install claude-code
echo 'echo "hi from claude"' | claude code &
CLAUDE_PID=$!
# Wait for hook to fire
APPROVAL_ID=$(curl -s -H "Authorization: Bearer $(onibi token show)" http://127.0.0.1:17893/v1/approval/pending | jq -r '.[0].approval_id')
[[ -n "$APPROVAL_ID" ]]
# Approve
curl -s -X POST -H "Authorization: Bearer $(onibi token show)" \
  -d '{"decision":"allow"}' \
  http://127.0.0.1:17893/v1/approval/$APPROVAL_ID/decide
wait $CLAUDE_PID
```

CI runs this on macOS only (Claude Code might not install on Ubuntu CI; gated by env var).

## Exit criteria

1. `SPEC.md` v1.0 frozen at repo root.
2. `onibid` (i.e. `onibi --headless`) starts an HTTP+WS server on port 17893 bound to 127.0.0.1.
3. Bearer token generated and stored in keychain on first run.
4. `cargo test --workspace` passes.
5. Manual: from a fresh Claude Code session, run `claude code "delete node_modules"`. Onibi UI shows ApprovalModal. Click "Edit", change `rm -rf node_modules` to `echo skipped`, click Approve. Claude Code executes `echo skipped`. Screencast-able.
6. Codex adapter works for Bash tool calls (manual + documented limitations).
7. `onibi adapter install claude-code` is idempotent and refuses Claude Code <2.0.10.
8. `e2e-claude.sh` passes on macOS dev machine.
9. SQLite DB at `~/.config/onibi/onibi.db` survives a restart with no data loss; pending approvals re-broadcast on WS reconnect.

## Out of scope (do NOT do in PHASE-03)

- Mobile PWA (PHASE-04).
- Web push (PHASE-04, but VAPID keypair generation is here as part of pairing setup).
- Transports (PHASE-05). Server binds 127.0.0.1 in Phase 03.
- Per-device tokens / revocation (v1.1).
- Approval policies (auto-approve `git status`, etc.) — v1.2.
- Audit log UI (v1.1).
- Aider, Gemini, Cursor, Goose full approval support — stubs only.

## Verification commands

```sh
cd /Users/gongahkia/Desktop/coding/projects/onibi
git checkout v1.5/phase-03-adapters
cd app/src-tauri && cargo test --workspace && cd ..
cd app && pnpm test && cd ..
# Manual: launch app, install adapter, run Claude Code
cd app && pnpm tauri dev &
sleep 5
./scripts/e2e-claude.sh
```

## Reference reading

- `docs/archive/SPEC-v1-draft.md` — the v1 daemon spec; mine for protocol structure (machine_id design, event-sourced approach).
- `docs/archive/WORKON-PIVOT-ASAP.md` — Claude Code hook research findings (lines around "Hook-surface findings verified by WebSearch").
- [Claude Code hooks v2.0.10+ docs](https://docs.claude.com/en/docs/claude-code/hooks)
- [Codex CLI hook docs](https://github.com/openai/codex/blob/main/docs/hooks.md) — verify before implementation; the v1 plan noted this is <6 months old and moving.
- [axum routing](https://docs.rs/axum)
- [rusqlite](https://docs.rs/rusqlite)
- [keyring crate](https://docs.rs/keyring)

## PR template

Title: `phase-03: adapters + approval protocol (SPEC v1.0)`

Body checklist:
```
## Deliverables
- [x] D1 SPEC.md v1.0 frozen
- [x] D2 axum HTTP + WS server (port 17893, 127.0.0.1)
- [x] D3 keyring-backed bearer token
- [x] D4 adapters: claude-code (full), codex (bash-only), stubs for opencode/gemini/aider/cursor/goose
- [x] D5 CLI (onibi setup/status/token/adapter/_hook)
- [x] D6 ApprovalModal wired to WS events
- [x] D7 rust + frontend tests
- [x] D8 scripts/e2e-claude.sh

## Verification
- [x] cargo test + pnpm test green
- [x] Manual: Claude Code "delete node_modules" → edit-then-approve flow works (screencast)
- [x] e2e-claude.sh green on macOS
- [x] SQLite DB persists across restart

Next: PHASE-04 (mobile PWA + web push — the killer demo).
```
