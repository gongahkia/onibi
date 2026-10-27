# Managed sessions

Managed sessions are tmux, Zellij or GNU Screen sessions owned by the Onibi daemon. They keep running when Ghostty quits, have a stable identity, and can be read without focusing anything on the Mac. The web cockpit shows them as **Persistent sessions** alongside Ghostty panes, so this part is not tied to any particular program.

The agent-specific paths (Pi, Claude and Codex approvals and questions) still have no decision surface. They are described under [What has no consumer](#what-has-no-consumer).

## In the web cockpit

`onibi web` connects to the daemon over the same-user intake socket. If the daemon is not running, the cockpit hides the session group and Ghostty control is unaffected. If Ghostty is unavailable, sessions remain usable.

| Capability | Behaviour |
| --- | --- |
| List | Live sessions with name, multiplexer, working directory and command, refreshed every five seconds. |
| Read | Scrollback plus the visible screen, 200 lines by default, with **More history** raising it to 1,000 and then 2,000 lines. Reading never changes focus and is not counted as session activity. tmux output keeps its SGR colours. Zellij and Screen dumps are plain text. |
| Input and keys | Send, paste, and the same key set as Ghostty panes. |
| Interrupt and end | **Interrupt** sends Ctrl C. **End session** kills the multiplexer session after a confirmation naming it. |
| Delivery | Every action uses the durable web receipts. The server confirms the session in a fresh daemon listing before dispatch, and the daemon resolves only the full session ID, never a name or prefix. A failure before any byte reaches the daemon, or a daemon rejection before a side effect, is reported as **not sent**. Anything later is **uncertain** and is never replayed. |

Starting or adopting sessions from the phone is not offered. Use `onibi session new` or `onibi session adopt` on the Mac.

## Attention signals

The daemon samples the last 40 lines of each multiplexer session at the liveness interval (five seconds by default) and classifies it. The phone shows the result as a badge, and the page title counts sessions that need input or rang a bell.

| State | Meaning |
| --- | --- |
| Needs input | The cursor line ends in a generic prompt: `[y/N]`/`(yes/no)`, "press enter/any key", a password or passphrase prompt, or a question such as "Do you want to …?". A question higher up counts only when every line below it is a menu choice. |
| Bell | The terminal rang its bell since the last change. New output or input sent through Onibi clears it. |
| Active | Output changed within the last 30 seconds. |
| Idle | Output has not changed for 30 seconds. |
| Quiet | No output change has been observed since the daemon started watching. |

These are heuristics over screen text. They have no knowledge of a program's internal state, so a prompt drawn in an unusual way is missed, and text that happens to look like a prompt on the cursor line is reported. The first sample after a daemon start is a baseline, so bells rung while the daemon was stopped are not reported.

Bells come from tmux. For sessions Onibi starts, tmux counts every bell into a session option through an `alert-bell[86]` hook. The fixed index leaves hooks you configure in other slots untouched. Adopted sessions are not modified, so only the window bell flag is available. It stays set until a client views the window, so a second bell before then is not seen. tmux reports bells only when `monitor-bell` is on (the default). Zellij and Screen sessions report prompts and activity but not bells. Desktop notification escapes such as OSC 9 and OSC 777 do not pass through tmux and are not detected.

## What still runs

`onibi start` runs the daemon in the foreground without credentials. It serves the same-user intake socket, restores managed sessions, polls multiplexer liveness, expires approvals and purges staged uploads. `onibi system service install` registers the same daemon as a launchd/systemd user service.

These CLI paths reach it over the intake socket:

| Command | Effect |
| --- | --- |
| `onibi session new <shell\|codex\|pi\|claude> [--name] [--cwd] [--mux]` | Start a managed session in tmux, Zellij or GNU Screen. `codex` runs the interactive Codex CLI like any other program |
| `onibi session adopt <tmux\|zellij\|screen> <target>` | Take control of an existing multiplexer session |
| `onibi session list [--all]` | List sessions from the state database |
| `onibi system status`, `onibi doctor` | Report daemon, service and multiplexer readiness |
| `onibi pi install --experimental` | Install the Pi approval extension that calls `onibi-notify` |

The web cockpit uses the `session_list`, `session_capture`, `session_key`, and exact-ID `session_input` and `session_control` RPCs (`internal/daemon/rpc.go`). `session_peek` and the name-matching forms of `session_input` and `session_control` have no caller.

## What has no consumer

| Capability | Code | Behaviour without a surface |
| --- | --- | --- |
| Pi and Claude tool approvals | `internal/approval`, `Daemon.handleApprovalRequest`, `clients/onibi-notify` | Requests queue and nobody can decide them. They expire after `daemon.approval_timeout` and the agent receives the default denial. |
| Claude `AskUserQuestion` | `internal/daemon/claude_question.go`, `Daemon.AnswerClaudeQuestion` | Questions are published on `ClaudeQuestionEvents()`, which nothing reads. They expire after `daemon.claude_question_timeout`. |
| Codex App Server sessions | `internal/daemon/codex.go`, `internal/codexapp`, `Daemon.StartCodexSession`, `Daemon.RespondCodexRequest` | Not started by any command, and leftover App Server sessions are marked ended on restart instead of resumed. Approval requests would be published on `CodexEvents()`, which nothing reads, so Codex would wait for replies that never arrive. |
| Agent lifecycle (Pi/Claude turn start and end) | `internal/daemon/pi.go`, `AgentEvents()` | Events are dropped when the buffer is full. |
| Session ended events | `SessionEvents()` | Logged by the daemon; the channel drops when full. |
| Multiplexer health alerts | `internal/daemon/health.go` | Logged as `health alert` warnings. |
| Rendered terminal screenshots and fonts | `internal/render`, `Daemon.CaptureSessionScreen`, `screen.*` config | Never requested. |
| Staged uploads | `Daemon.StageUpload`, `daemon.upload_*` config | Nothing calls `StageUpload`; the expiry sweep still runs. |
| Keychain/env-file secrets | `internal/secrets` | Not used by any command. A previously stored bot token remains under keychain service `sh.onibi.daemon`, account `TELEGRAM_BOT_TOKEN`, or in the `.env` fallback file in the state directory. Delete it there if you no longer need it, and revoke the bot with BotFather. |

## Wiring agent decisions

The web cockpit covers terminal control. A surface for agent decisions would replace the rest of what the Telegram bridge did. At minimum it should:

1. Hold one `Daemon` (or talk to it over the intake socket) and read `CodexEvents()`, `AgentEvents()`, `ClaudeQuestionEvents()` and `SessionEvents()` continuously so producers never block.
2. Subscribe to `Queue.Subscribe()` and resolve approvals with `Queue.DecideIdempotently`. Replay pending approvals with `Queue.Pending` after reconnecting.
3. Answer Codex requests with `RespondCodexRequest` and Claude questions with `AnswerClaudeQuestion` or `CancelClaudeQuestion`.
4. Use `SendSessionText`, `SendSessionKey`, `ControlSession`, `ResizeSession`, `CaptureSessionTail` and `CaptureSessionScreen` for input and output.
5. Record a claim before any input side effect and report interrupted actions as uncertain instead of replaying them. The web cockpit's receipts (`internal/web/receipts.go`) are the existing model.
6. Bind each decision or destructive control to an explicit session ID, as the web close confirmation does.

Claude sessions launch with an owned settings file and reject `--bare`, `--settings`, `--dangerously-skip-permissions`, `--allow-dangerously-skip-permissions`, and the `bypassPermissions`/`dontAsk` permission modes, because those bypass the decision path. Keep that guard when wiring a surface.

The removed Telegram implementation is available at commit `b3c6fc90` (`internal/daemon/telegram_bridge.go`, `internal/telegram`, `docs/telegram.md`) and is a useful reference for card state, outbox retries and screen debouncing.
