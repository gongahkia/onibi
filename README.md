# Onibi

Telegram-controlled coding-agent host.

Onibi runs as a local user daemon on macOS/Linux, hosts coding agents under
PTYs, and routes approvals, turn-complete signals, screenshots, and prompt
injection through a Telegram bot. It uses outbound HTTPS to Telegram only: no
tunnels, no inbound ports, no SaaS account.

[![CI](https://github.com/gongahkia/onibi/actions/workflows/ci.yml/badge.svg)](https://github.com/gongahkia/onibi/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

> Status: v2 development. Core daemon, setup, approvals, prompt queue,
> Telegram controls, service install, doctor, and adapters are implemented.
> Live provider e2e and signed public release validation still gate a stable
> tag.

## Install

Development:

```sh
git clone https://github.com/gongahkia/onibi
cd onibi
make install
onibi setup --complete
```

Release path after the tap is published:

```sh
brew install --cask gongahkia/onibi/onibi
onibi setup --complete
```

`setup --complete` pairs your Telegram bot, stores the token in Keychain when
available, offers service install, offers detected agent hooks, and ends with
`onibi doctor`.

## Use

```sh
onibi version
onibi doctor
onibi config list
onibi adapters
onibi run claude
onibi wrap lazygit
onibi mcp
```

Telegram commands:

- `/new <agent>`
- `/sessions`
- `/status`
- `/prompt <text>`
- `/peek`
- `/interrupt`
- `/kill`
- `/snooze`
- `/log`
- `/help`

Supported adapters: Claude Code, Codex, OpenCode, Goose, Gemini, GitHub
Copilot CLI, Pi, Amp, plus opt-in zsh/bash/fish command-done hooks.
`onibi wrap <command>` can host any local TUI/CLI under the same PTY preview
and Telegram text-in controls.

MCP-capable agents can run `onibi mcp` as a local stdio MCP server. It exposes
notify, approval_request, session_list, session_input, and session_peek tools
over the local Onibi daemon socket.

## Configure

```sh
onibi config path
onibi config show
onibi config list
onibi config set daemon.approval_timeout 2m
onibi config set shell.min_duration 10s
onibi setup --enable-encrypted-mode
onibi config set telegram.encrypted_mode on
onibi config validate
```

`onibi config init` writes `config.yaml` under the Onibi state dir.
`telegram.encrypted_mode` is `off`, `ask`, or `on`. `on` encrypts approvals,
session previews/output, prompt acks, logs/status/session lists, and Mini App
actions; use `/secure` for prompt entry so Telegram stores ciphertext.

## Uninstall

```sh
onibi uninstall --dry-run
onibi uninstall
onibi uninstall --state --yes
```

Default uninstall removes the service and all Onibi-managed hooks while keeping
state. `--state --yes` also removes local state and stored Onibi secrets.

## Security

Bot token storage prefers macOS Keychain or Linux Secret Service; `.env` is a
0600 fallback. Every inbound Telegram update is checked against the paired
owner chat id before handlers run. Approvals expire. Pairing uses a single-use
deeplink token. Onibi does not defend against full compromise of the local user
account or owner Telegram account.

Enable Telegram 2-step verification. Full model: [docs/security.md](docs/security.md).

## Recovery

Start with:

```sh
onibi doctor --fix
```

Then see [docs/troubleshooting.md](docs/troubleshooting.md) for lost token,
owner rotation, stale webhook, no updates, sleep/wake, service, and hook drift.

## Release

Release checklist and signing/notarization rules live in
[docs/release.md](docs/release.md).

## License

Apache-2.0. See [LICENSE](LICENSE).
