# Onibi

Telegram-controlled coding-agent host. A single static Go binary runs as a
user-agent on your laptop; coding agents (Claude Code, Codex, OpenCode,
Goose) live under PTYs that the daemon owns, and approval prompts /
turn-completion signals get routed to a Telegram bot. You approve, deny,
edit, and inject text from your phone — no PWA install, no tunnels, no
inbound network, no accounts.

[![CI](https://github.com/gongahkia/onibi/actions/workflows/ci.yml/badge.svg)](https://github.com/gongahkia/onibi/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

> **Status:** v2 development in progress. Setup, daemon, Claude approvals,
> multi-agent hook installers, service install, and doctor are implemented;
> see `TODO-10-JUN.md` for remaining phase work.
> See `TODO-10-JUN.md` for the single source of truth on plan, phases,
> security model, and decisions. Prior v1 (Swift, archived under
> `docs/archive/v0-README.md`) and v1.5 (Rust+Tauri+PWA, archived under
> `docs/archive/v1.5-rust/`) never shipped. This is the right shape per
> antirez/tgterm validation.

## Why Telegram

- The app is already on your phone, authenticated, and supports rich UI
  (inline keyboards, photos, replies, threading).
- Zero install on mobile. Zero network setup. Zero certs.
- Outbound HTTPS only — daemon never opens a port.

## Install (not yet shipping)

```sh
brew install gongahkia/onibi/onibi
onibi setup
onibi install-service
```

`onibi setup` walks you through three user-visible steps:

1. Create a bot in @BotFather and paste the token (stored in macOS Keychain).
2. Tap the deeplink the wizard prints (or scan the terminal QR) to permanently
   pair your Telegram account as the daemon's owner.
3. Acknowledge the Telegram 2-step verification reminder (mandatory — see
   security model).

After that you never see an auth prompt during normal use. Pair once, then
linked.

## Security in one paragraph

The daemon's only network surface is outbound HTTPS to `api.telegram.org`.
The bot token is stored in your macOS Keychain (or `.env` 0600 fallback) and
never appears in argv or logs. Every inbound Telegram update is checked
against the owner chat_id at a chokepoint before any handler runs.
Approvals expire after 5 minutes. The deeplink-pair token is single-use,
constant-time-compared, and 5-minute-TTL'd, eliminating the
first-message-becomes-owner race. The single threat we do not defend against
is compromise of the user's Telegram account itself — which is why the setup
wizard requires acknowledgment of Telegram's built-in 2-step verification.
Full threat model in `TODO-10-JUN.md` §7 and (later) `docs/security.md`.

## Architecture

Single Go binary. Subcommands:

- `onibi setup` — one-time pair flow
- `onibi run` — start the daemon (called by LaunchAgent / systemd)
- `onibi install-hooks --agent <name>` — write hook block/plugin to agent settings
- `onibi install-service` — install LaunchAgent (macOS) or systemd user unit
- `onibi doctor` — health + integrity check
- `onibi rotate-token` — replace bot token via @BotFather /revoke
- `onibi sessions` / `onibi log` — introspection
- `onibi version`

The same binary spawned without subcommand prints help.

A second tiny binary `onibi-notify` is invoked by agent and shell hooks; it
writes a JSON event to the daemon's local Unix-domain socket and fails open
when the daemon is down.

Current first-class adapters: Claude, Codex, OpenCode, Goose, Gemini,
GitHub Copilot CLI, Pi, Amp, plus opt-in zsh/bash/fish command-done hooks.
Blocking approvals are used where the provider supports them; Goose and shell
hooks are event/notification bridges.

## License

Apache-2.0. See [LICENSE](LICENSE).
