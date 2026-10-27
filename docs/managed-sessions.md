# Managed sessions (currently unwired)

The Telegram transport was removed. The managed-session backend it drove is kept in the tree so a future surface (most likely the web cockpit) can reuse it. This page records what still works, what has no consumer, and what a new surface needs to provide.

The Ghostty web cockpit described in [web.md](web.md) does not use any of this code. It controls existing Ghostty panes directly.

## What still runs

`onibi start` runs the daemon in the foreground without credentials. It serves the same-user intake socket, restores managed sessions, polls multiplexer liveness, expires approvals and purges staged uploads. `onibi system service install` registers the same daemon as a launchd/systemd user service.

These CLI paths reach it over the intake socket:

| Command | Effect |
| --- | --- |
| `onibi session new <shell\|codex\|pi\|claude> [--name] [--cwd] [--mux]` | Start a managed session in tmux, Zellij or GNU Screen, or a Codex App Server session |
| `onibi session adopt <tmux\|zellij\|screen> <target>` | Take control of an existing multiplexer session |
| `onibi session list [--all]` | List sessions from the state database |
| `onibi system status`, `onibi doctor` | Report daemon, service and multiplexer readiness |
| `onibi pi install --experimental` | Install the Pi approval extension that calls `onibi-notify` |

The intake socket also accepts `session_input`, `session_peek` and `session_control` RPCs (`internal/daemon/rpc.go`), but no CLI command sends them.

## What has no consumer

| Capability | Code | Behaviour without a surface |
| --- | --- | --- |
| Pi and Claude tool approvals | `internal/approval`, `Daemon.handleApprovalRequest`, `clients/onibi-notify` | Requests queue and nobody can decide them. They expire after `daemon.approval_timeout` and the agent receives the default denial. |
| Claude `AskUserQuestion` | `internal/daemon/claude_question.go`, `Daemon.AnswerClaudeQuestion` | Questions are published on `ClaudeQuestionEvents()`, which nothing reads. They expire after `daemon.claude_question_timeout`. |
| Codex App Server approvals and questions | `internal/daemon/codex.go`, `internal/codexapp`, `Daemon.RespondCodexRequest` | Requests are published on `CodexEvents()`. Nothing reads it, so once its 128-slot buffer fills, request forwarding blocks for that session. Codex waits for a reply that never arrives. |
| Agent lifecycle (Pi/Claude turn start and end) | `internal/daemon/pi.go`, `AgentEvents()` | Events are dropped when the buffer is full. |
| Session ended events | `SessionEvents()` | Logged by the daemon; the channel drops when full. |
| Multiplexer health alerts | `internal/daemon/health.go` | Logged as `health alert` warnings. |
| Rendered terminal screenshots and fonts | `internal/render`, `Daemon.CaptureSessionScreen`, `screen.*` config | Never requested. |
| Staged uploads | `Daemon.StageUpload`, `daemon.upload_*` config | Nothing calls `StageUpload`; the expiry sweep still runs. |
| Keychain/env-file secrets | `internal/secrets` | Not used by any command. A previously stored bot token remains under keychain service `sh.onibi.daemon`, account `TELEGRAM_BOT_TOKEN`, or in the `.env` fallback file in the state directory. Delete it there if you no longer need it, and revoke the bot with BotFather. |

## Wiring a new surface

A surface replaces what the Telegram bridge did. At minimum it should:

1. Hold one `Daemon` (or talk to it over the intake socket) and read `CodexEvents()`, `AgentEvents()`, `ClaudeQuestionEvents()` and `SessionEvents()` continuously so producers never block.
2. Subscribe to `Queue.Subscribe()` and resolve approvals with `Queue.DecideIdempotently`. Replay pending approvals with `Queue.Pending` after reconnecting.
3. Answer Codex requests with `RespondCodexRequest` and Claude questions with `AnswerClaudeQuestion` or `CancelClaudeQuestion`.
4. Use `SendSessionText`, `SendSessionKey`, `ControlSession`, `ResizeSession`, `CaptureSessionTail` and `CaptureSessionScreen` for input and output.
5. Record a claim before any input side effect and report interrupted actions as uncertain instead of replaying them. The web cockpit's receipts (`internal/web/receipts.go`) are the existing model.
6. Bind each decision or destructive control to an explicit session ID, as the web close confirmation does.

Claude sessions launch with an owned settings file and reject `--bare`, `--settings`, `--dangerously-skip-permissions`, `--allow-dangerously-skip-permissions`, and the `bypassPermissions`/`dontAsk` permission modes, because those bypass the decision path. Keep that guard when wiring a surface.

The removed Telegram implementation is available at commit `b3c6fc90` (`internal/daemon/telegram_bridge.go`, `internal/telegram`, `docs/telegram.md`) and is a useful reference for card state, outbox retries and screen debouncing.
