# MCP

Onibi exposes a local MCP stdio server so MCP-capable clients can inspect sessions, send notifications, request approvals, and write to live sessions through the Onibi daemon.

Run it with:

```bash
onibi mcp
```

The MCP process is local. It opens the same SQLite DB as the CLI and connects to the same same-UID Unix socket as hooks for daemon-backed tools.

## Client Config

Claude Desktop-style config:

```json
{
  "mcpServers": {
    "onibi": {
      "command": "/Users/you/.local/bin/onibi",
      "args": ["mcp"]
    }
  }
}
```

If `onibi` is already on `PATH`, this is enough:

```json
{
  "mcpServers": {
    "onibi": {
      "command": "onibi",
      "args": ["mcp"]
    }
  }
}
```

The daemon should already be running for every tool except `onibi_session_list`:

```bash
onibi up
onibi doctor --mode installed
```

## Auth Model

MCP clients do not talk to Telegram directly. They talk to `onibi mcp` over stdio. For daemon actions, `onibi mcp` connects to the local Unix socket.

The socket enforces same-UID peer credentials:

- macOS: `LOCAL_PEERCRED`.
- Linux: `SO_PEERCRED`.

There is no network listener, bearer token, or remote MCP auth layer. A different OS user cannot use the socket unless the operating system reports the same effective UID.

## Tool Summary

| Tool | Needs live daemon | Reads DB | Writes socket | Purpose |
|---|---:|---:|---:|---|
| `onibi_notify` | Yes | No | Yes | Send a status message to Telegram through the daemon. |
| `onibi_approval_request` | Yes | No | Yes | Block for a Telegram approval decision. |
| `onibi_session_list` | No | Yes | No | List recorded sessions from SQLite. |
| `onibi_session_input` | Yes | No | Yes | Write text into a live session. |
| `onibi_session_peek` | Yes | No | Yes | Read recent output from a live session. |

## `onibi_notify`

Input schema:

```json
{
  "session": "string, optional",
  "agent": "string, optional",
  "text": "string, required"
}
```

Output schema:

```json
{
  "ok": true,
  "message": "queued"
}
```

Example:

```json
{
  "session": "s1",
  "agent": "codex",
  "text": "started long-running refactor"
}
```

Daemon down response:

```json
{
  "ok": false,
  "message": "daemon unavailable: dial unix ..."
}
```

Validation errors are MCP tool errors. Example: missing `text` returns `text required`.

## `onibi_approval_request`

Input schema:

```json
{
  "session": "string, optional",
  "agent": "string, optional",
  "tool": "string, required",
  "input_json": "string, required; must itself be valid JSON",
  "timeout_seconds": "integer, optional"
}
```

Output schema:

```json
{
  "decision": "approve | deny | edited | expired | cancelled",
  "updated_input": "string, optional",
  "reason": "string, optional",
  "decided_by": 123456789
}
```

Example:

```json
{
  "session": "s1",
  "agent": "claude",
  "tool": "Bash",
  "input_json": "{\"command\":\"git status --short\"}",
  "timeout_seconds": 120
}
```

Approve response:

```json
{
  "decision": "approve",
  "decided_by": 123456789
}
```

Edited response:

```json
{
  "decision": "edited",
  "updated_input": "{\"command\":\"git status --short --branch\"}",
  "decided_by": 123456789
}
```

Validation errors are MCP tool errors:

- Missing `tool`: `tool required`.
- Invalid `input_json`: `input_json must be valid JSON`.
- Daemon unavailable or socket timeout: MCP tool error from the Unix socket request.

Timeout behavior:

- `timeout_seconds <= 0`: client waits up to 6 minutes.
- `timeout_seconds > 10 minutes`: capped to 10 minutes.
- Approval TTL is owned by the daemon approval queue, normally 5 minutes or 60 seconds in paranoid mode.

## `onibi_session_list`

Input schema:

```json
{
  "all": "boolean, optional; include ended sessions",
  "n": "integer, optional; max sessions"
}
```

Output schema:

```json
{
  "sessions": [
    {
      "id": "s1",
      "name": "codex",
      "agent": "codex",
      "cwd": "/repo",
      "command": "codex",
      "transport": "pty",
      "tmux_target": "",
      "started_at": "2026-06-14T12:00:00Z",
      "ended_at": "",
      "ended": false
    }
  ]
}
```

Example:

```json
{
  "all": true,
  "n": 20
}
```

Notes:

- Does not require the daemon socket.
- Requires the local SQLite DB.
- `n <= 0` defaults to 50.
- `n > 200` is capped to 200.

Error shape:

- DB missing or unavailable: MCP tool error.

## `onibi_session_input`

Input schema:

```json
{
  "session": "string, optional when exactly one live session exists",
  "text": "string, required",
  "enter": "boolean, optional"
}
```

Output schema:

```json
{
  "ok": true,
  "message": "sent to codex (s1)"
}
```

Example:

```json
{
  "session": "s1",
  "text": "continue",
  "enter": true
}
```

Daemon behavior:

- If `enter` is true and `text` does not end in `\n`, the daemon appends a newline.
- Empty text is rejected.
- If `session` is empty and exactly one live session exists, that session is selected.
- If no live session or multiple live sessions exist, the daemon returns an error.

Error shape:

- Missing `text`: MCP tool error `text required`.
- Daemon unavailable: MCP tool error from Unix socket dial/read.
- Daemon rejection: MCP tool error with the daemon reason.

## `onibi_session_peek`

Input schema:

```json
{
  "session": "string, optional when exactly one live session exists",
  "tail_bytes": "integer, optional"
}
```

Output schema:

```json
{
  "text": "recent session output"
}
```

Example:

```json
{
  "session": "s1",
  "tail_bytes": 8000
}
```

Daemon behavior:

- Reads the session ring buffer snapshot.
- `tail_bytes <= 0` defaults to 8000.
- `tail_bytes > 65536` is capped to 8000 by the daemon RPC path.
- Session selection follows the same rules as `onibi_session_input`.

Error shape:

- Daemon unavailable: MCP tool error from Unix socket dial/read.
- Daemon rejection: MCP tool error with the daemon reason.

## Troubleshooting

No sessions:

```bash
onibi sessions --all
onibi doctor
```

Daemon unavailable:

```bash
onibi up
onibi doctor --mode installed
```

MCP client cannot find `onibi`:

```bash
which onibi
onibi version
```

Use an absolute `command` path in the MCP client config when the client does not inherit your shell `PATH`.

Session input fails with ambiguous target:

```bash
onibi sessions
```

Then call the MCP tool again with a concrete `session` id.
