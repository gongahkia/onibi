# Onibi

Onibi is a Telegram-native remote command center for one developer's persistent local terminal sessions.

It runs named `tmux` sessions on your machine. From Telegram you can select a session, send literal input, inspect bounded output or a rendered terminal screen, and send `Enter`, `Esc`, `Ctrl-C`, or a guarded kill. Codex uses the local App Server for structured progress, approvals, and questions; ordinary programs remain literal terminal control.

## Scope

Included: Telegram owner pairing, named durable sessions, generic `tmux` control, screenshots, audit/recovery, Codex semantic decisions, and an experimental narrow Pi approval extension.

Not included: browser/PWA UI, QR pairing, Ghostty handover, LAN/relay transports, team collaboration, arbitrary terminal-prompt inference, snapshots, or generic agent-hook catalogues.

## Start

Prerequisites: `tmux`, a Telegram bot token from BotFather, and Go 1.26.4+ when building from source. Codex sessions also require a local authenticated `codex` CLI.

```sh
make build
./bin/onibi telegram setup --token "$ONIBI_TELEGRAM_TOKEN"
./bin/onibi start
```

The first start prints a pairing command. Send it from the one Telegram account that should control Onibi. The bot then accepts:

```text
/new shell --name work --cwd /path/to/repo
/new codex --cwd /path/to/repo
/sessions
/tail 120
/screen
/font
/paste
/interrupt
/kill
```

Plain messages go to the selected session and append Enter. Unknown slash commands go to that session too; prefix a conflicting Onibi command with `//` (for example, `//help`). `/paste` makes the next message literal, with no implicit Enter, and expires after five minutes. Use `/keys` for session-bound `Esc`, `Ctrl-C`, `Enter`, and screen controls.

Codex sessions are semantic, not tmux windows: after `/new codex`, send a normal message to start a turn. A later normal message steers the active turn. `/font` selects the terminal-screen font remotely; JetBrainsMono Nerd Font Mono, Caskaydia Cove Nerd Font Mono, and Go Mono Nerd Font Mono are embedded. To use a locally installed BigBlueTerminal Nerd Font Mono, set `screen.font_path` then `screen.font=custom` locally.

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

## Decisions and safety

Telegram callback payloads are opaque local tokens. Decisions are persisted before agent resumption and are idempotent. High-risk Pi approvals require a second confirmation. Codex App Server approvals and user-input questions become native inline cards; `thread/shellCommand` is intentionally never exposed.

Telegram is not end-to-end encrypted for bots. Treat every message, screenshot, and approval payload as terminal-access-sensitive; do not send secrets through this bot. The local OS account remains trusted.

See [Telegram operation details](docs/telegram.md), [threat model](THREAT-MODEL.md), and the implementation boundary in [REWRITE.md](REWRITE.md).

## Verification

```sh
make vet
make test
make build
```

## License

Apache-2.0.

Bundled screenshot fonts and their licenses are listed in [third-party notices](THIRD_PARTY_NOTICES.md).
