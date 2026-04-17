# Phase 01: Host Control Plane And Session Instrumentation

## Objective

Upgrade Onibi from a read-only polling gateway into a bidirectional, realtime host service for controllable Ghostty sessions on macOS.

At the end of this phase, the macOS host must be able to:

- instrument newly opened interactive shell sessions inside Ghostty
- expose a stable session registry that differentiates monitor-only sessions from controllable sessions
- stream live output for controllable sessions from the point of instrumentation onward
- accept remote input for a specific session
- support the limited v1 remote key set: text, `Enter`, `Ctrl-C`, arrow keys, and space
- broadcast session state and output to remote clients over a shared realtime API

This phase does **not** need to ship polished client UX. It is the host and protocol foundation that later web clients will consume.

## Non-Negotiable Constraints

- Do not edit `README.md`.
- Keep existing read-only mobile monitoring behavior functional while introducing the new control plane.
- `v1` host scope is `macOS + Ghostty` only.
- Do not assume any specific CLI inside Ghostty. Codex, Claude, Gemini CLI, Copilot CLI, or a plain shell command must all be treated identically.
- Sessions only need to be controllable from the point the new shell instrumentation is active onward.
- The product remains a personal paired-device system using Tailscale and a pairing token, not a multi-user cloud system.

## Current Repo Context

This repo currently has three relevant areas:

- `Onibi/`: macOS menubar host app
- `OnibiCore/`: shared models and networking code used by host and companion clients
- `OnibiWeb/`: companion web client

Current mobile host behavior:

- `Onibi/Services/MobileGatewayService.swift` starts a token-protected TCP listener on `127.0.0.1` and exposes it through Tailscale Serve.
- `OnibiCore/Services/MobileGatewayRouter.swift` only supports token-protected `GET` endpoints.
- `OnibiCore/Services/MobileAPIClient.swift` is polling-only.
- `OnibiCore/ViewModels/MobileMonitorViewModel.swift` polls every 5 seconds.

Current shell/session behavior:

- `Onibi/Services/ShellHookInstaller.swift` only writes `CMD_START` and `CMD_END` lines into `~/.config/onibi/terminal.log`.
- `Onibi/Services/GhosttyIPCClient.swift` only detects Ghostty app/window state. It does **not** write into a live shell session.
- `Onibi/Services/BackgroundTaskScheduler.swift` and `Onibi/Services/LogFileParser.swift` treat the host as an ingest pipeline for logs and notifications, not as a live session broker.

## Architecture Decision

Implement a host-side **session proxy** per interactive shell session.

### Why this design

The existing shell hooks only capture command lifecycle events. They do not provide:

- a stable write path into the session
- a live output stream
- a per-session transport for remote control

Ghostty itself is not exposing a repo-local API here for stdin injection. The pragmatic v1 design is:

1. bootstrap newly opened interactive shells through an Onibi session proxy
2. let the proxy own the PTY boundary for that session
3. have the proxy register with the host service over a local transport
4. route remote input from clients to that proxy
5. route live session output from the proxy back to the host and out to clients

### Required host-side components

1. **Session proxy executable**
   - Runs locally on macOS.
   - Spawns the user's real shell as a child process.
   - Owns the PTY for that shell.
   - Streams child output to the host.
   - Accepts input frames from the host and writes the correct byte sequence into the PTY.

2. **Session registry on the host**
   - Tracks each controllable session.
   - Stores metadata, lifecycle state, last activity timestamps, and a bounded output ring buffer.

3. **Realtime gateway**
   - Extends the current host gateway to support websocket sessions.
   - Broadcasts session snapshots and output chunks.
   - Accepts remote input requests and forwards them to the correct session proxy.

4. **Shared protocol models**
   - Live in `OnibiCore/` so companion clients can conform to the same contract.

## Recommended File And Module Layout

Use this as the implementation target unless a better repo-local naming pattern already emerges during coding.

### New shared files in `OnibiCore/`

- `OnibiCore/Models/RealtimeGatewayModels.swift`
- `OnibiCore/Models/ControllableSessionModels.swift`
- `OnibiCore/Services/RealtimeGatewayCodec.swift`

### New macOS host files in `Onibi/`

- `Onibi/Services/ControllableSessionRegistry.swift`
- `Onibi/Services/SessionOutputBuffer.swift`
- `Onibi/Services/SessionProxyCoordinator.swift`
- `Onibi/Services/RealtimeGatewayService.swift`
- `Onibi/Services/LocalSessionProxyListener.swift`

### New executable target

Create a new Swift executable target for the local session proxy.

Recommended target name:

- `OnibiSessionProxy`

Recommended source folder:

- `OnibiSessionProxy/`

This target can be added to `Package.swift` and, if needed for local development ergonomics, referenced from project generation later.

## Session Proxy Design

## Proxy startup contract

The proxy must be startable from an interactive shell bootstrap and should receive enough information to locate the host.

Required launch inputs:

- `ONIBI_PROXY_SOCKET_PATH`: unix socket path for host registration, for example `~/.config/onibi/control.sock`
- `ONIBI_HOST_SESSION_ID`: generated UUID for the session
- `ONIBI_PARENT_SHELL`: full shell path to launch, for example `/bin/zsh`
- `ONIBI_PARENT_SHELL_ARGS`: optional login/interactive args
- `ONIBI_PROXY_VERSION`: host/proxy compatibility string

The proxy should:

1. create a PTY pair
2. spawn the real shell on the slave side
3. connect to the host listener on the unix socket path
4. send a `register` message
5. forward output chunks and lifecycle events to the host
6. listen for host `input` messages and write them to the PTY master

### Important loop-prevention rule

The shell bootstrap must guard against recursive proxy launching.

Use an environment sentinel such as:

- `ONIBI_SESSION_PROXY_ACTIVE=1`

If this env var is already present, the shell integration must **not** re-exec through the proxy again.

## Shell Bootstrap Changes

Extend `Onibi/Services/ShellHookInstaller.swift`.

### Existing behavior to preserve

Keep the existing command lifecycle logging for backward compatibility.

### New behavior to add

For interactive shells only:

1. if the proxy sentinel env var is absent
2. if the shell is running inside Ghostty
3. if remote control is enabled in Onibi settings
4. exec into the proxy launcher rather than staying in the raw shell process

Recommended high-level zsh flow:

```zsh
if [[ -o interactive ]] && [[ -n "$GHOSTTY_RESOURCES_DIR" ]] && [[ -z "$ONIBI_SESSION_PROXY_ACTIVE" ]]; then
  export ONIBI_SESSION_PROXY_ACTIVE=1
  export ONIBI_HOST_SESSION_ID="${TERM_SESSION_ID:-$(uuidgen)}"
  exec "$ONIBI_PROXY_BINARY" --shell "$SHELL" --login
fi
```

Do not hardcode `GHOSTTY_RESOURCES_DIR` if a more reliable Ghostty-specific env marker is available at implementation time, but keep the check Ghostty-scoped.

### Settings changes

Add new macOS settings fields to the existing app settings model:

- `remoteControlEnabled: Bool`
- `sessionProxySocketPath: String`
- `sessionOutputBufferLineLimit: Int`
- `sessionOutputBufferByteLimit: Int`

Default values should be conservative and local-only.

## Local Host <-> Proxy Protocol

Use newline-delimited JSON frames over a local unix domain socket.

### Why NDJSON

- easy to debug with logs
- easy to stream incrementally
- simple to parse in Swift without introducing a binary protocol
- good enough for v1 local transport

### Proxy -> host messages

#### Register

```json
{
  "type": "register",
  "sessionId": "UUID",
  "shell": "/bin/zsh",
  "pid": 12345,
  "startedAt": "2026-04-02T10:00:00Z",
  "workingDirectory": "/Users/example/project",
  "hostname": "MacBook-Pro.local"
}
```

#### Output chunk

```json
{
  "type": "output",
  "sessionId": "UUID",
  "stream": "stdout",
  "timestamp": "2026-04-02T10:00:05Z",
  "data": "base64-encoded-utf8-or-raw-bytes"
}
```

Use base64 so control bytes and partial UTF-8 sequences are preserved. Decode on the host before buffering and rebroadcasting.

#### Session state

```json
{
  "type": "state",
  "sessionId": "UUID",
  "status": "running"
}
```

Statuses for v1:

- `starting`
- `running`
- `exited`
- `failed`

#### Exit

```json
{
  "type": "exit",
  "sessionId": "UUID",
  "exitCode": 0,
  "timestamp": "2026-04-02T10:10:00Z"
}
```

#### Heartbeat

```json
{
  "type": "heartbeat",
  "sessionId": "UUID",
  "timestamp": "2026-04-02T10:00:30Z"
}
```

### Host -> proxy messages

#### Input text

```json
{
  "type": "input",
  "sessionId": "UUID",
  "kind": "text",
  "text": "npm test"
}
```

#### Input key

```json
{
  "type": "input",
  "sessionId": "UUID",
  "kind": "key",
  "key": "enter"
}
```

Allowed v1 `key` values:

- `enter`
- `ctrl_c`
- `arrow_up`
- `arrow_down`
- `arrow_left`
- `arrow_right`
- `space`

### Required key-to-byte mapping in the proxy

- `enter` -> `\r`
- `ctrl_c` -> byte `0x03`
- `arrow_up` -> `\u001B[A`
- `arrow_down` -> `\u001B[B`
- `arrow_right` -> `\u001B[C`
- `arrow_left` -> `\u001B[D`
- `space` -> ` `

## Remote Client <-> Host API Design

Introduce a new `v2` realtime API while keeping existing `v1` polling endpoints working.

### Authentication

- Keep the existing pairing token model.
- For HTTP, continue using `Authorization: Bearer <token>`.
- For WebSocket, require an explicit first frame auth handshake because browser WebSocket clients cannot set arbitrary headers reliably.

### New HTTP endpoints

Add these under `OnibiCore/Services/MobileGatewayRouter.swift` or a new router layer if the current one becomes too limited.

#### `GET /api/v2/bootstrap`

Returns:

- host health
- feature flags
- current controllable sessions
- current diagnostics summary

#### `GET /api/v2/sessions`

Returns a snapshot of controllable sessions.

#### `GET /api/v2/sessions/:id/buffer`

Returns the current bounded output buffer for a session.

Suggested response payload fields:

- `session`
- `bufferCursor`
- `chunks`
- `truncated: Bool`

#### `POST /api/v2/sessions/:id/input`

Fallback input endpoint for non-websocket paths.

Request body options:

```json
{ "kind": "text", "text": "npm test" }
```

or

```json
{ "kind": "key", "key": "enter" }
```

### WebSocket endpoint

Suggested route:

- `/api/v2/realtime`

#### Client auth frame

```json
{
  "type": "auth",
  "token": "pairing-token"
}
```

The host must reject all non-auth frames until auth succeeds.

#### Client frames

```json
{ "type": "subscribe", "sessionId": "UUID" }
{ "type": "unsubscribe", "sessionId": "UUID" }
{ "type": "request_buffer", "sessionId": "UUID" }
{ "type": "send_input", "sessionId": "UUID", "kind": "text", "text": "ls" }
{ "type": "send_input", "sessionId": "UUID", "kind": "key", "key": "enter" }
```

#### Server frames

```json
{ "type": "auth_ok", "hostVersion": "x.y.z" }
{ "type": "sessions_snapshot", "sessions": [] }
{ "type": "session_added", "session": {} }
{ "type": "session_updated", "session": {} }
{ "type": "session_removed", "sessionId": "UUID" }
{ "type": "buffer_snapshot", "sessionId": "UUID", "chunks": [], "bufferCursor": "cursor" }
{ "type": "output", "sessionId": "UUID", "chunk": {} }
{ "type": "input_accepted", "sessionId": "UUID", "clientRequestId": "UUID" }
{ "type": "error", "code": "session_not_found", "message": "..." }
```

## Shared Model Additions In `OnibiCore/`

Create models that are intentionally generic and client-safe.

Recommended types:

- `ControllableSessionSnapshot`
- `SessionTransportStatus`
- `SessionOutputChunk`
- `SessionOutputBufferSnapshot`
- `RemoteInputPayload`
- `RealtimeClientMessage`
- `RealtimeServerMessage`
- `FeatureFlagsResponse`

### `ControllableSessionSnapshot` should include

- `id: String`
- `displayName: String`
- `startedAt: Date`
- `lastActivityAt: Date`
- `status: SessionTransportStatus`
- `isControllable: Bool`
- `workingDirectory: String?`
- `lastCommandPreview: String?`
- `bufferCursor: String?`

## Session Registry Requirements

Implement `ControllableSessionRegistry` as the host source of truth.

It must support:

- register session
- update session metadata and heartbeat
- append output chunk
- fetch output buffer snapshot
- route input to active proxy connection
- expire disconnected sessions after a grace period
- broadcast state changes to active websocket clients

### Buffering rules

Use a bounded ring buffer per session.

Recommended defaults:

- line limit: 1000 lines
- byte limit: 256 KB

When the buffer truncates older output, expose that fact through the buffer snapshot.

## Changes To Existing Host Services

### `MobileGatewayService`

Refactor it so it no longer assumes only simple request/response GET handling.

It should own:

- the public local/Tailscale listener
- HTTP route handling
- websocket upgrade handling
- reference to the new `RealtimeGatewayService`
- reference to the new `ControllableSessionRegistry`

### `HostMobileGatewayDataProvider`

Continue to support the legacy monitoring endpoints, but enrich diagnostics with remote-control state:

- controllable session count
- connected websocket client count
- proxy registration failures
- stale proxy count
- local socket path health

## Test Plan

### Unit tests

Add or extend tests for:

- protocol encoding/decoding in `OnibiCoreTests`
- session buffer truncation
- session registry lifecycle and expiry
- key mapping for `enter`, `ctrl_c`, arrows, and `space`
- auth handshake enforcement for websocket sessions

### Integration tests

Add integration coverage for:

- proxy registration -> session visible in registry
- output chunk -> buffer snapshot -> websocket broadcast
- websocket `send_input` -> routed to correct proxy
- invalid token -> auth rejected
- disconnected proxy -> session marked stale or removed

### Manual validation checklist

On a real macOS machine with Ghostty and Tailscale:

1. Enable remote control in Onibi settings.
2. Reinstall shell hooks if needed.
3. Open a new Ghostty session.
4. Verify a controllable session appears in the host session registry.
5. Send text input remotely and confirm it appears in the Ghostty session.
6. Send `Enter` and confirm command execution.
7. Send `Ctrl-C` and confirm the running command interrupts.
8. Send arrow keys and confirm shell history navigation or cursor movement works.
9. Confirm live output arrives remotely after instrumentation begins.

## Acceptance Criteria

This phase is complete when all of the following are true:

- newly opened Ghostty sessions are registered as controllable sessions
- the host can stream live output for those sessions
- the host can route remote text and key input into those sessions
- the host exposes stable `v2` HTTP and websocket contracts in `OnibiCore`
- existing `v1` mobile monitoring routes still work
- diagnostics expose enough host state to debug session-control failures

## Risks And Tradeoffs

### PTY proxy complexity

This is the hardest part of the project. It is still the correct v1 tradeoff because it gives a real session transport instead of a brittle keystroke hack.

### Existing sessions remain uncontrollable

That is acceptable for v1. Only sessions started after the proxy bootstrap is installed need to be controllable.

### Output is only live from instrumentation onward

That is also acceptable for v1 and should be documented in diagnostics/UI copy later.

### Full-screen TUIs are out of scope

The limited v1 key set is intentional. This phase should not chase terminal emulation, resize negotiation, mouse events, or alternate-screen correctness.
