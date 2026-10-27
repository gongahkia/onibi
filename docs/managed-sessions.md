# Managed sessions

Managed sessions are tmux, Zellij or GNU Screen sessions owned by the Onibi daemon. They keep running when Ghostty quits, have a stable identity, and can be read without focusing anything on the Mac. The web cockpit shows them as **Persistent sessions** alongside Ghostty panes, so this part is not tied to any particular program.

Coding agents get no special treatment. `onibi session new codex`, `claude` or `pi` runs the program exactly as given, with no hooks, settings or flag checks, and the cockpit handles it like any other terminal program: its own prompts appear in the output and raise the generic attention signals. Onibi has no agent-specific approval or question handling; see [Removed integrations](#removed-integrations).

## In the web cockpit

`onibi web` connects to the daemon over the same-user intake socket. If the daemon is not running, the cockpit hides the session group and Ghostty control is unaffected. If Ghostty is unavailable, sessions remain usable.

| Capability | Behaviour |
| --- | --- |
| List | Live sessions with name, multiplexer, working directory and command, refreshed every five seconds. |
| Read | Scrollback plus the visible screen, 200 lines by default, with **More history** raising it to 1,000 and then 2,000 lines. Reading never changes focus and is not counted as session activity. tmux output keeps its SGR colours. Zellij and Screen dumps are plain text. |
| Input and keys | Send, paste, and the same key set as Ghostty panes. Text is typed into tmux, Zellij and Screen sessions as plain keystrokes, never as a bracketed paste, so it behaves the same on all three. |
| Interrupt, rename and end | **Interrupt** sends Ctrl C. **Rename** changes the name Onibi shows, with the same rules as `onibi session rename`. **End session** kills the multiplexer session after a confirmation naming it. |
| Delivery | Every action uses the durable web receipts. The server confirms the session in a fresh daemon listing before dispatch, and the daemon resolves only the full session ID, never a name or prefix. A failure before any byte reaches the daemon, or a daemon rejection before a side effect, is reported as **not sent**. Anything later is **uncertain** and is never replayed. |

**New session** starts one from the phone: a program (the shell, a command on `PATH` or an absolute path, with optional arguments), a folder (from an open pane or session, or a typed absolute path) and an optional name. It uses the configured default multiplexer, like `onibi session new` without `--mux`. Adopting an existing tmux, Zellij or Screen session is not offered from the phone, because it reaches sessions Onibi did not start; use `onibi session adopt` on the Mac.

## Attention signals

The daemon samples the last 40 lines of each multiplexer session at the liveness interval (five seconds by default) and classifies it. The phone shows the result as a badge, and the page title counts sessions that need input or rang a bell.

| State | Meaning |
| --- | --- |
| Needs input | The cursor line ends in a generic prompt: `[y/N]`/`(yes/no)`, "press enter/any key", a password or passphrase prompt, or a question such as "Do you want to …?". A question higher up counts only when every line below it is a menu choice, optionally followed by key hints such as "Esc to cancel". Box-drawing borders around a dialog are ignored, so agent approval dialogs are recognised without any agent-specific code. |
| Bell | The terminal rang its bell since the last change. New output or input sent through Onibi clears it. |
| Active | Output changed within the last 30 seconds. |
| Idle | Output has not changed for 30 seconds. |
| Quiet | No output change has been observed since the daemon started watching. |

These are heuristics over screen text. They have no knowledge of a program's internal state, so a prompt drawn in an unusual way is missed, and text that happens to look like a prompt on the cursor line is reported. The first sample after a daemon start is a baseline, so bells rung while the daemon was stopped are not reported.

Bells come from tmux. For sessions Onibi starts, tmux counts every bell into a session option through an `alert-bell[86]` hook. The fixed index leaves hooks you configure in other slots untouched. Adopted sessions are not modified, so only the window bell flag is available. It stays set until a client views the window, so a second bell before then is not seen. tmux reports bells only when `monitor-bell` is on (the default). Zellij and Screen sessions report prompts and activity but not bells. Desktop notification escapes such as OSC 9 and OSC 777 do not pass through tmux and are not detected.

## What still runs

`onibi web` runs the daemon inside the web server when no daemon is already running, so one command (or one `onibi web service`) is enough. The daemon stops with the web server; its multiplexer sessions keep running and are restored at the next start. `onibi start` runs only the daemon, in the foreground and without credentials, and refuses to start when a daemon already owns the socket. It serves the same-user intake socket, restores managed sessions and polls multiplexer liveness. `onibi system service install` registers that daemon-only setup as a launchd/systemd user service; it is unnecessary when the web login service is installed.

These CLI paths reach it over the intake socket:

| Command | Effect |
| --- | --- |
| `onibi session new [--name] [--cwd] [--mux] <shell\|program> [args...]` | Run the configured shell, or any program on `PATH` or given by absolute path, in tmux, Zellij or GNU Screen. Arguments after the program are passed to it unchanged. `ONIBI_<PROGRAM>_BIN` overrides which binary a bare name resolves to |
| `onibi session adopt <tmux\|zellij\|screen> <target>` | Take control of an existing multiplexer session |
| `onibi session list [--all]` | List sessions from the state database |
| `onibi session attach <name\|id>` | Attach this terminal to the session's multiplexer |
| `onibi session rename <name\|id> <new-name>` | Change the name Onibi shows and resolves. Names use letters, digits, dot, underscore and hyphen, and must be unique among live sessions. The multiplexer's own session name is unchanged, so attach commands keep working |
| `onibi session kill [--yes] <name\|id>` | End the session and the programs in it, after confirmation |
| `onibi system status`, `onibi doctor` | Report daemon, service and multiplexer readiness |

The web cockpit uses the `session_list`, `session_capture`, `session_key`, `session_new`, and exact-ID `session_input` and `session_control` RPCs (`internal/daemon/rpc.go`). `session_peek` and the name-matching forms of `session_input` and `session_control` have no caller.

## What has no consumer

| Capability | Code | Behaviour without a surface |
| --- | --- | --- |
| Multiplexer health alerts | `internal/daemon/health.go` | Logged as `health alert` warnings. |

## Removed integrations

The Pi approval extension, the Claude Code approval and `AskUserQuestion` hooks, the Codex App Server transport, and the `onibi-notify` hook client they relied on have been removed. They are available at commit `e0bbe9e1` (`internal/approval`, `internal/daemon/claude.go`, `internal/daemon/claude_question.go`, `internal/daemon/codex.go`, `internal/codexapp`, `internal/pi`, `clients/onibi-notify`). Upgrading from a release that had them:

- Opening the state database drops the `approvals` and `claude_questions` tables, which may hold raw tool input and answers, and deletes stored Codex thread IDs. Codex App Server sessions recorded by an earlier release are marked ended instead of restored.
- `daemon.approval_timeout`, `daemon.claude_question_timeout`, `daemon.approval_sweep_interval` and `daemon.max_subscribers` are ignored in `config.yaml` and dropped the next time `onibi config set` saves it.
- `make update` and `make uninstall` remove the `onibi-notify` binary an older source install recorded, while it still matches its checksum.
- The intake socket refuses `approval_request`, `claude_question` and `agent_lifecycle` requests.
- `onibi doctor` fails while the Pi extension from an older release is still installed (by default `~/.pi/agent/extensions/onibi.ts`), because it blocks Pi tool calls in every Onibi session. Delete the file and run `/reload` in Pi.

Code that only served Telegram has also been removed: rendered terminal screenshots and their bundled Nerd Fonts, upload staging, the keychain secret store, the per-session output buffer, session resizing, and log secret redaction. It is available at commit `74630036` (`internal/render/png.go`, `internal/daemon/screen_frame.go`, `internal/daemon/upload.go`, `internal/secrets`, `internal/logging/redact.go`). Upgrading from a release that had it:

- `screen.font`, `screen.font_path`, `daemon.upload_ttl`, `daemon.upload_max_bytes` and `daemon.output_buffer_bytes` are ignored in `config.yaml`, whatever their values, and dropped the next time `onibi config set` saves it.
- The daemon deletes the `uploads` folder in the state directory when it starts. Files there were copies of Telegram uploads, previously kept for `daemon.upload_ttl` (seven days by default).
- A bot token stored by an earlier release stays where it was: keychain service `sh.onibi.daemon`, account `TELEGRAM_BOT_TOKEN`, or the `.env` file in the state directory, whose permissions `onibi doctor` still checks. Delete it if you no longer need it, and revoke the bot with BotFather.

The removed Telegram implementation is available at commit `b3c6fc90` (`internal/daemon/telegram_bridge.go`, `internal/telegram`, `docs/telegram.md`) and is a useful reference for card state, outbox retries and screen debouncing.
