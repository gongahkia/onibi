# MCP

Onibi exposes a local MCP stdio server. MCP clients can inspect sessions, fetch transcripts, list/deny approvals, kill sessions, and write to live PTYs through the Onibi daemon.

Run it directly:

```bash
onibi mcp
```

The process is local. It opens the same SQLite DB as the CLI and uses the same same-UID Unix socket as hooks for daemon-backed actions.

## Install

Claude config path:

```bash
mkdir -p ~/.claude
$EDITOR ~/.claude/mcp.json
```

Minimal config:

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

If the client does not inherit your shell `PATH`, use an absolute command:

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

Start the daemon before daemon-backed tools:

```bash
onibi up
onibi doctor --mode installed
```

## Auth

MCP clients do not talk to the phone browser. They talk to `onibi mcp` over stdio. Daemon actions go through the local Unix socket.

The socket enforces same-UID peer credentials:

| OS | check |
|---|---|
| macOS | `LOCAL_PEERCRED` |
| Linux | `SO_PEERCRED` |

There is no network listener, bearer token, or remote MCP auth layer.

## Tools

| Tool | Daemon | DB | Purpose |
|---|---:|---:|---|
| `onibi_list_sessions` | No | Yes | Current session inventory for MCP clients. |
| `onibi_kill_session` | Yes | No | Kill a live Onibi session. |
| `onibi_fetch_transcript` | No | Yes + Claude JSONL | Fetch scrubbed transcript turns. |
| `onibi_list_pending_approvals` | No | Yes | List pending approvals across sessions. |
| `onibi_decide_approval` | No | Yes | Approve, deny, or edit low/medium-risk approvals. |
| `onibi_tail_logs` | Yes | Yes | Return scrubbed audit rows and PTY tail. |
| `onibi_notify` | Yes | No | Send a status event. |
| `onibi_approval_request` | Yes | No | Create an approval and wait for owner decision. |
| `onibi_session_list` | No | Yes | Legacy session list wrapper. |
| `onibi_session_input` | Yes | No | Write text into a live PTY. |
| `onibi_session_peek` | Yes | No | Read recent PTY output. |

## Resources

`onibi://sessions/{id}/transcript`

Returns `application/json` text containing scrubbed transcript turns:

```json
[
  {
    "turn_index": 1,
    "role": "assistant",
    "content": "done",
    "tool_calls": [
      {
        "id": "toolu_1",
        "name": "Bash",
        "input": {
          "command": "git status --short"
        }
      }
    ]
  }
]
```

Current transcript source is Claude JSONL under `~/.claude/projects/...`. Non-Claude agents return a resource error until their transcript formats are implemented.

## Schemas

### `onibi_list_sessions`

Input:

```json
{
  "include_remote": false
}
```

Output:

```json
[
  {
    "id": "s1",
    "agent": "claude",
    "cwd": "/repo",
    "started_at": "2026-06-29T12:00:00Z",
    "last_activity": "2026-06-29T12:01:00Z",
    "pending_approvals_count": 1,
    "tokens_used": 0,
    "cost_usd": 0,
    "role_required": "owner",
    "workspace": ""
  }
]
```

### `onibi_kill_session`

Input:

```json
{
  "session_id": "s1",
  "force": false
}
```

Output:

```json
{
  "killed": true,
  "signal": "SIGKILL"
}
```

### `onibi_fetch_transcript`

Input:

```json
{
  "session_id": "s1",
  "since_turn": 0,
  "max_turns": 50
}
```

Output is an array of scrubbed turns:

```json
[
  {
    "turn_index": 2,
    "role": "assistant",
    "content": "done",
    "tool_calls": []
  }
]
```

`max_turns <= 0` defaults to 50. Tool calls and content are scrubbed with `internal/approval/scrub.go`.

### `onibi_list_pending_approvals`

Input:

```json
{}
```

Output:

```json
[
  {
    "id": "appr_1",
    "session_id": "s1",
    "tool": "Bash",
    "args_scrubbed": "{\"command\":\"git status --short\"}",
    "risk_level": "low",
    "agent": "claude",
    "scrubbed_input": "{\"command\":\"git status --short\"}",
    "risk_reasons": [],
    "expires_at": "2026-06-29T12:05:00Z"
  }
]
```

This uses the same risk classifier and scrubber as the web approval inbox.

### `onibi_decide_approval`

Input:

```json
{
  "approval_id": "appr_1",
  "verdict": "approve",
  "edited_args": ""
}
```

`verdict` accepts `approve`, `deny`, or `edit`. `edited_args` is required for `edit` and must pass the tool input schema validator.

Output:

```json
{
  "ok": true,
  "state": "approved",
  "verdict": "approve",
  "delivered": false
}
```

High-risk policy:

- MCP refuses `approve` and `edit` when `risk_level` is `high`.
- High-risk approval must be decided by a human in the web cockpit.
- `deny` remains allowed.

### `onibi_tail_logs`

Input:

```json
{
  "session_id": "s1",
  "lines": 200
}
```

Output:

```json
{
  "audit": [
    {
      "id": 42,
      "ts": "2026-06-29T12:00:00Z",
      "action": "approval.request",
      "session_id": "s1",
      "payload_hash": "sha256...",
      "detail": "tool=Bash id=appr_1"
    }
  ],
  "pty_tail": "recent terminal output"
}
```

Audit detail and PTY tail are scrubbed.

### Legacy / Utility Tools

`onibi_notify`:

```json
{
  "session": "s1",
  "agent": "codex",
  "text": "started long-running refactor"
}
```

`onibi_approval_request`:

```json
{
  "session": "s1",
  "agent": "claude",
  "tool": "Bash",
  "input_json": "{\"command\":\"git status --short\"}",
  "timeout_seconds": 120
}
```

`onibi_session_list`:

```json
{
  "all": true,
  "n": 20
}
```

`onibi_session_input`:

```json
{
  "session": "s1",
  "text": "continue",
  "enter": true
}
```

`onibi_session_peek`:

```json
{
  "session": "s1",
  "tail_bytes": 8000
}
```

## Example Calls

Typical agent-control flow:

1. Call `onibi_list_sessions`.
2. Pick a session id.
3. Call `onibi_fetch_transcript`.
4. Call `onibi_list_pending_approvals`.
5. Deny unsafe approvals with `onibi_decide_approval`.
6. Send input with `onibi_session_input`.

Example arguments:

```json
{
  "session_id": "s1",
  "since_turn": 10,
  "max_turns": 20
}
```

```json
{
  "approval_id": "appr_1",
  "verdict": "deny"
}
```

```json
{
  "session": "s1",
  "text": "run the unit tests now",
  "enter": true
}
```

## Sample Agent-Control Script

Run from this repo or any Go module with `github.com/mark3labs/mcp-go` available:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := mcpclient.NewClient(transport.NewCommand("onibi", "mcp"))
	if err := c.Start(ctx); err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	init := mcp.InitializeRequest{}
	init.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	init.Params.ClientInfo = mcp.Implementation{Name: "onibi-agent-control", Version: "dev"}
	if _, err := c.Initialize(ctx, init); err != nil {
		log.Fatal(err)
	}

	sessions := call(ctx, c, "onibi_list_sessions", map[string]any{"include_remote": false})
	fmt.Println(string(sessions))

	pending := call(ctx, c, "onibi_list_pending_approvals", map[string]any{})
	fmt.Println(string(pending))
}

func call(ctx context.Context, c *mcpclient.Client, name string, args map[string]any) []byte {
	res, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: name, Arguments: args}})
	if err != nil {
		log.Fatal(err)
	}
	if res.IsError {
		log.Fatalf("%s: %+v", name, res.Content)
	}
	b, err := json.MarshalIndent(res.StructuredContent, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	return b
}
```

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

Client cannot find `onibi`:

```bash
which onibi
onibi version
```

Use an absolute `command` path in `~/.claude/mcp.json` when needed.
