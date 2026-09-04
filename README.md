# Onibi

Onibi is a Telegram-native remote command center for one developer's persistent local terminal sessions.

It runs named `tmux` sessions on your machine. From Telegram you can select a session, send literal input, inspect bounded output or a rendered terminal screen, send navigation/modifier keys, resize a tmux window, or use a guarded kill. Codex uses the local App Server for structured progress, approvals, and questions; Pi and Claude Code use local hooks for remote approvals and completion updates.

## Scope

Included: Telegram owner pairing, named durable sessions, generic `tmux` control, screenshots, audit/recovery, Codex semantic decisions, Pi approval extension, and Claude Code approval/completion hooks.

Not included: browser/PWA UI, QR pairing, Ghostty handover, LAN/relay transports, team collaboration, arbitrary terminal-prompt inference, snapshots, or generic agent-hook catalogues.

## Start

Prerequisites: `tmux`, a Telegram bot token from BotFather, and Go 1.26.4+ when building from source. Codex sessions require a local authenticated `codex` CLI; Claude Code sessions require a local authenticated `claude` CLI.

```sh
make build
./bin/onibi telegram setup --token "$ONIBI_TELEGRAM_TOKEN"
./bin/onibi start
```

The first start prints a pairing command. Send it from the one Telegram account that should control Onibi. The bot then accepts:

```text
/new shell --name work --cwd /path/to/repo
/new codex --cwd /path/to/repo
/new claude --cwd /path/to/repo
/sessions
/tail 120
/screen
/font
/keys
/key ctrl-d
/size large
/paste
/interrupt
/kill
```

Plain messages go to the selected session and append Enter. Unknown slash commands go to that session too; prefix a conflicting Onibi command with `//` (for example, `//help`). `/paste` makes the next message literal, with no implicit Enter, and expires after five minutes. `/keys` exposes arrows, Tab, Shift-Tab, Backspace, Delete, Home, End, PgUp, PgDn, Esc, Ctrl-C, Ctrl-D, Ctrl-Z, Ctrl-L, Ctrl-R, Enter, and tmux size presets; `/key <name>` accepts the same keys plus `ctrl-a` through `ctrl-z`, `meta-a` through `meta-z`, and `f1` through `f12`.

Codex sessions are semantic, not tmux windows: after `/new codex`, send a normal message to start a turn. A later normal message steers the active turn. Claude Code runs in tmux; Onibi attaches an owned settings file with permission, structured `AskUserQuestion`, and completion hooks. Claude question cards support sequential single-select, multi-select, and free-text answers; their default expiry is three minutes and is configurable with `daemon.claude_question_timeout` (30s–10m). `/font` selects the terminal-screen font remotely; JetBrainsMono Nerd Font Mono, Caskaydia Cove Nerd Font Mono, and Go Mono Nerd Font Mono are embedded. To use a locally installed BigBlueTerminal Nerd Font Mono, set `screen.font_path` then `screen.font=custom` locally.

## Local CLI

```sh
./bin/onibi session new shell --name work
./bin/onibi session list
./bin/onibi telegram status --check
./bin/onibi system status
./bin/onibi system logs --tail 100
./bin/onibi system config set screen.font caskaydia-cove-nerd
./bin/onibi system service install
```

`onibi session new` needs a running daemon. `onibi system service install` starts `onibi start` in the per-user service manager.

`onibi system status` reports actual daemon socket and service liveness. If `daemon_running=false`, start the daemon or install/restart the service before using Telegram.

## Delivery and uploads

Onibi persists Telegram update claims before executing an input. If it restarts mid-update, it marks that input uncertain and asks you to inspect/resend rather than executing it again. Screens, final tails, and session-ended notices are queued as durable intents and retried; terminal text and PNGs are held only in memory around delivery and never stored in that queue. Automatic generic-input screens are debounced for 500ms, deduplicated by rendered terminal state, and share one capture between the status tail and PNG. `/status` reports poll freshness, delivery failures, queue depth, and tmux health for each live session; it alerts after three consecutive poll failures, on permanent delivery failures, and once on recovery.

The daemon checks managed tmux sessions every five seconds. A locally ended tmux session becomes unavailable immediately on the next check and sends one ended notice. Change the cadence with `daemon.liveness_interval` (1s–5m).

Send a Telegram document to stage it privately for the selected live tmux session. Onibi downloads it to `state/uploads/<session-id>/`, reports its local path, and does not insert or execute it. Defaults: 20 MiB, seven-day retention; configure `daemon.upload_max_bytes` (1–100 MiB) and `daemon.upload_ttl` (1h–30d).

## Decisions and safety

Telegram callback payloads are opaque local tokens. Decisions are persisted before agent resumption and are idempotent. High-risk Pi approvals require a second confirmation. Codex App Server approvals and user-input questions become native inline cards; Claude `AskUserQuestion` calls receive the same structured interaction; `thread/shellCommand` is intentionally never exposed. Claude `--bare`, `--settings`, permission-bypass, and `dontAsk` modes are rejected at session creation.

Telegram is not end-to-end encrypted for bots. Treat every message, screenshot, and approval payload as terminal-access-sensitive; do not send secrets through this bot. The local OS account remains trusted.

See [Telegram operation details](docs/telegram.md), [threat model](THREAT-MODEL.md), and the implementation boundary in [REWRITE.md](REWRITE.md).

## Verification

```sh
make vet
make test
make build
```

`go test ./...` is hermetic. `make test` also runs the mandatory live production-Telegram E2E against a dedicated test account and bot; it creates shell, Codex, and Claude sessions, sends agent prompts, verifies a daemon restart, and attempts cleanup of its temporary state and managed sessions. It fails before execution unless all required credentials are set:

```sh
export ONIBI_E2E_API_ID=...
export ONIBI_E2E_API_HASH=...
export ONIBI_E2E_SESSION_FILE=/secure/path/to/authorized-gotd.session
export ONIBI_E2E_BOT_USERNAME=your_dedicated_test_bot
export ONIBI_E2E_BOT_TOKEN=...
make test
```

The MTProto account must already be authorized in the supplied `gotd` session file and must be the dedicated bot's private-chat owner. Keep this bot separate from normal Onibi use: the test launches a real daemon and real Codex and Claude turns.

## License

Apache-2.0.

Bundled screenshot fonts and their licenses are listed in [third-party notices](THIRD_PARTY_NOTICES.md).
