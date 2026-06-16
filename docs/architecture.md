# Architecture

Onibi is a local user daemon plus small helper CLIs. It hosts agent processes under PTYs, accepts hook events on a same-UID Unix socket, persists durable state in SQLite, and routes user decisions through Telegram.

## Component Map

```text
+---------------------------+      +-----------------------------+
| Telegram (api.telegram.org)|<---->|  internal/telegram          |
+---------------------------+ HTTPS|  client.go long-poll        |
            ^                      |  router.go owner-check      |
            |                      +--------------+--------------+
            | tg.sendData                          | OnText/OnCB/OnReply
            v                                      v
+---------------------------+      +-----------------------------+
| Mini App (docs/miniapp/)  |      |  internal/daemon            |
| static HTML/JS,           |      |  daemon.go    lifecycle     |
| WebApp.SecureStorage      |      |  approvals.go approval RPC  |
+---------------------------+      |  prompts.go   prompt queue  |
                                   |  commands.go  /commands     |
                                   |  threading.go session route |
                                   |  encrypted.go AES-GCM Mini  |
                                   |  output.go    PNG / tail    |
                                   +--------------+--------------+
                                                  | spawn / signal
                                                  v
                                   +-----------------------------+
                                   |  internal/pty               |
                                   |  host.go creack/pty         |
                                   |  ring buffer (buffer.go)    |
                                   +--------------+--------------+
                                                  | child PTY
                                                  v
                                   +-----------------------------+
                                   | agent binary (claude, codex,|
                                   | goose, gemini, copilot, ...)|
                                   +--------------+--------------+
                                                  | hook fires
                                                  v
                                   +-----------------------------+
                                   | clients/onibi-notify        |
                                   | --type ... (--wait for RPC) |
                                   +--------------+--------------+
                                                  | JSON over Unix socket
                                                  v  (peer-UID checked)
                                   +-----------------------------+
                                   |  internal/intake/server.go  |
                                   |  paths.Socket               |
                                   +--------------+--------------+
                                                  |
                                                  v
                                   +-----------------------------+
                                   |  internal/store (SQLite)    |
                                   |  approvals, prompts,        |
                                   |  sessions, audit, kv,       |
                                   |  pairing_tokens, hooks      |
                                   +-----------------------------+
```

`onibi mcp` is a separate stdio JSON-RPC server process. It exposes MCP tools to a local MCP client and reaches the daemon through the same Unix socket as hooks.

## Invariants

- No inbound network listener. Telegram uses outbound HTTPS long polling.
- Local Unix socket peers must have the same OS UID as the daemon.
- The Telegram router checks owner identity before dispatching inbound updates.
- Hook intake is fail-open for agent UX: if the daemon is unavailable, hooks return an error to their caller but the agent-side wrapper can proceed.
- Approval requests are fail-closed at the agent tool boundary: a blocked tool waits for an explicit daemon response or timeout.
- SQLite is the durable source for owner, approvals, prompts, sessions, audits, pairing tokens, and hook hashes.

## Intake Protocol

Hooks and MCP tools write JSON events to `paths.Socket`. The daemon fills `ts` when it is omitted.

```json
{
  "type": "agent_message",
  "session": "s1",
  "agent": "codex",
  "text": "turn started",
  "ts": 1760000000
}
```

Event union fields:

| Field | JSON | Use |
|---|---|---|
| `Type` | `type` | One of the intake event types. |
| `Session` | `session` | Onibi session id. |
| `Agent` | `agent` | Adapter/provider name. |
| `PID` | `pid` | Fallback process identity. |
| `CWD` | `cwd` | Provider working directory. |
| `EventName` | `event_name` | Provider-native lifecycle event. |
| `ProviderSessionID` | `provider_session_id` | Provider-native session id. |
| `Status` | `status` | Exit status for command/session events. |
| `Cmd` | `cmd` | Shell command line. |
| `Elapsed` | `elapsed_ms` | Shell command duration. |
| `Text` | `text` | Human-readable detail or text to inject. |
| `Tail` | `tail` | Output tail supplied by a hook. |
| `Enter` | `enter` | `session_input`: append newline. |
| `Limit` | `limit` | `session_peek`: maximum bytes. |
| `ApprovalID` | `approval_id` | Approval id when already known. |
| `Tool` | `tool` | Tool name for approval. |
| `InputJSON` | `input_json` | Raw tool input JSON. |
| `RawJSON` | `raw_json` | Raw provider hook payload. |
| `TS` | `ts` | Unix timestamp seconds. |

Event types:

| Type | Mode | Meaning |
|---|---|---|
| `agent_done` | fire-and-forget | Agent finished a turn. |
| `agent_awaiting` | fire-and-forget | Agent is waiting for user input. |
| `agent_message` | fire-and-forget | Status or provider lifecycle message. |
| `cmd_done` | fire-and-forget | Shell command finished. |
| `session_exited` | fire-and-forget | Hosted process exited. |
| `approval_request` | RPC | Agent tool call needs owner decision. |
| `session_input` | RPC | Write text into a live session. |
| `session_peek` | RPC | Return recent session output. |
| `session_new` | RPC | Create a tmux-backed session. |
| `session_show` | RPC | Open a visible terminal for a tmux-backed session. |
| `session_hide` | RPC | Detach visible clients or end a tmux-backed session. |

Example approval request:

```json
{
  "type": "approval_request",
  "session": "s1",
  "agent": "claude",
  "tool": "Bash",
  "input_json": "{\"command\":\"ls\"}"
}
```

Example session input:

```json
{
  "type": "session_input",
  "session": "s1",
  "text": "continue",
  "enter": true
}
```

## Approval State Machine

SQLite stores approvals in `approvals`. New tool calls start in `pending` and end in one terminal state.

```text
pending
  | approve
  v
approved

pending
  | deny
  v
denied

pending
  | edit(valid JSON)
  v
edited

pending
  | sweeper after expires_at
  v
expired

pending
  | daemon shutdown / send failure / missing handler
  v
cancelled
```

Defaults:

- Approval TTL: 5 minutes.
- Paranoid TTL: 60 seconds.
- Sweep interval: 15 seconds unless configured otherwise.

The queue keeps in-memory waiters keyed by approval id. SQLite holds the durable row. A decision updates SQLite first, then delivers exactly one decision to the waiter if the waiter still exists.

Telegram message ids are stored on the approval row as `chat_id` and `msg_id`. They allow the daemon to edit the original approval message after a decision or rerender pending approvals on restart.

## Encrypted Envelope

Encrypted mode wraps Telegram-visible payloads in an envelope from `internal/envelope/envelope.go`.

Wire JSON before base64url encoding:

```json
{
  "v": 1,
  "kind": "approval",
  "exp": 1760000300,
  "nonce": "base64url-12-byte-nonce",
  "ct": "base64url-ciphertext"
}
```

Plaintext JSON inside AES-GCM:

```json
{
  "v": 1,
  "kind": "approval",
  "id": "approval-id",
  "title": "Bash",
  "risk": "low",
  "body": "rendered body",
  "mime": "text/plain",
  "data_b64": "",
  "file": ""
}
```

Key derivation:

- Seed: 32 random bytes, base64url encoded.
- KDF: HKDF-SHA256 with salt `onibi-envelope-v1` and info `telegram-mini-app`.
- Cipher: AES-256-GCM.
- Nonce: 12 random bytes per message.
- AAD: `v=<version>;kind=<kind>;exp=<unix-expiry>`.

The seed is stored locally by the daemon and in Telegram Mini App SecureStorage on the device.

## Daemon Goroutines

`Daemon.Run` starts these long-running loops:

| Goroutine | Source | Purpose |
|---|---|---|
| Signal handler | `daemon.go` | Cancels context on SIGINT/SIGTERM. |
| Intake server | `internal/intake/server.go` | Accepts hook and MCP events over the Unix socket. |
| Idle detector | `turn_complete.go` | Polls sessions and emits turn-complete notifications after inactivity. |
| Approval sweeper | `approval/expiry.go` | Expires overdue pending approvals. |
| Telegram long poll | `telegram` client | Receives updates and dispatches through owner-checked router; exits polling on `getUpdates` conflicts so doctor reports a hard recovery state. |
| Exit waiter | `daemon.go` | Optional; exits interactive `onibi run` when hosted sessions finish. |

Session spawn also starts:

- `readLoop`: copies PTY output into the ring buffer and mirrors it to stdout.
- `waitHost`: waits for the child process and marks the session ended.

## Sessions And PTY

`onibi run`, `onibi shell`, and `onibi wrap` create legacy PTY-backed
sessions. Telegram `/new` and `onibi new` create tmux-backed sessions so the
same logical session can switch between headless and visible modes.

| Field | Purpose |
|---|---|
| `ID` | Opaque id passed to hooks as `ONIBI_SESSION_ID`. |
| `Name` | Human label from CLI or agent name. |
| `Agent` | Adapter name. |
| `Cmd` | Spawned command line. |
| `Host` | PTY host from `internal/pty`. |
| `Buf` | Fixed-capacity ring buffer. |
| `Transport` | `pty` or `tmux`. |
| `TmuxTarget` | Target when wrapping an existing tmux pane. |

PTY sessions use `creack/pty` through `internal/pty/host.go`. The child receives:

```bash
ONIBI_SOCK=/path/to/onibi.sock
ONIBI_SESSION_ID=<session-id>
```

The ring buffer keeps the newest bytes only. `Snapshot` returns a chronological copy, oldest first. The default per-session buffer is 64 KiB.

Tmux-backed sessions store `Transport=tmux` and `TmuxTarget`. Headless means the
tmux target is live with no attached terminal clients. Visible means Ghostty or
Terminal.app is attached to that tmux target. `/show` opens or attaches a
terminal. `/hide` can detach visible clients and continue headless, or end the
session.

Daemon startup reconciles persisted session rows. Live tmux targets are restored
into memory and recaptured into the ring buffer. Stale PTY rows and missing tmux
targets are marked ended and audited as stale sessions.

Telegram-created sessions must specify an explicit project directory with
`--project <alias>` or `--cwd <path>`. Project aliases are stored in SQLite KV
and prevent remote sessions from starting accidentally in the daemon state dir.

Text preview uses the ring snapshot directly. PNG render uses `/render` and
replays the snapshot through a vt100 model, draws a fixed-cell terminal image
with `golang.org/x/image/font/basicfont`, then encodes PNG. This is a
terminal-buffer render, not a Ghostty/window screenshot.

## Storage

SQLite path:

```text
<state-dir>/onibi.sqlite
```

The DB uses WAL, foreign keys, and a single open connection. File permissions are forced to `0600`.

Tables:

| Table | Purpose |
|---|---|
| `schema_version` | Tracks schema baseline. |
| `kv` | Owner id, bot id, config flags, transient values with optional expiry. |
| `pairing_tokens` | Single-use Telegram deeplink tokens with expiry. |
| `approvals` | Tool approval state machine and Telegram message ids. |
| `audit` | Append-only decision, prompt, session, and risk/anomaly events. |
| `hooks` | Installed adapter/shell hook path, SHA-256, version, install time. |
| `sessions` | Session registry for active and ended PTY/tmux sessions. |
| `prompt_queue` | Telegram-originated prompts queued per session. |

The store package owns typed helpers for sessions, prompts, audit rows, and KV. Some daemon paths still use raw SQL for approval rows because they need state-machine-specific predicates.
