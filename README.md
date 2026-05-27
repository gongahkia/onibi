# Onibi

Onibi - cockpit for local AI coding agents, with a phone in your pocket.

[![CI](https://github.com/gongahkia/onibi/actions/workflows/ci.yml/badge.svg?branch=v1.5)](https://github.com/gongahkia/onibi/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Onibi runs Claude Code, Codex, and other local agents in a desktop cockpit, then mirrors approval requests to a mobile PWA. When an agent pauses before a dangerous action, your phone buzzes; you can approve, deny, or edit the command without being at the keyboard.

![Onibi launch screencast](asset/screencast/onibi-launch.gif)

## Install

macOS:

```sh
brew tap gongahkia/onibi
brew install onibi
```

Linux / Raspberry Pi:

```sh
curl -fsSL https://onibi.sh/install.sh | bash
```

Source checkout:

```sh
git clone https://github.com/gongahkia/onibi
cd onibi
pnpm install
cd app/src-tauri
cargo build
```

## Quick Start

```sh
onibi setup
onibi doctor
onibi adapter install claude-code
onibi --headless --auto-transports
```

Then scan the pairing QR with your phone, install the PWA, and run:

```sh
claude code "remove the old generated test fixtures"
```

When Claude pauses at a tool call, Onibi shows the approval on desktop and phone. Editing `rm -rf tests/legacy` into `mv tests/legacy tests/legacy.bak` returns an `updatedInput` decision to the agent.

## Architecture

![Onibi architecture](docs/architecture.png)

Onibi is a single Rust/Tauri codebase with a headless mode. The same approval server backs the desktop UI, the mobile PWA, and the CLI.

Core protocol endpoints:

- `POST /v1/approval/request` - adapter long-polls until a decision exists.
- `POST /v1/approval/:id/decide` - desktop or phone submits allow/deny/edit.
- `WS /v1/realtime` - approvals, run events, terminal output, and heartbeat.
- `GET /v1/qr` - pairing payload with token, machine ID, and transports.

## Support Matrix

Agents:

| Agent | Approval | Edit | Mirror | Notes |
| --- | ---: | ---: | ---: | --- |
| Claude Code | Yes | Yes | Yes | Full path; requires `updatedInput` support. |
| Codex CLI | Bash only | No | Yes | `apply_patch` and MCP tools are not interceptable. |
| OpenCode | No | No | Yes | Shell/mirror integration for v1.5. |
| Gemini CLI | No | No | Yes | Shell/mirror integration for v1.5. |
| Aider | No | No | Yes | Shell/mirror integration for v1.5. |
| Cursor agent | No | No | Yes | Shell/mirror integration for v1.5. |
| Goose | No | No | Yes | Shell/mirror integration for v1.5. |

Platforms:

| Platform | Desktop app | Headless daemon | Notes |
| --- | ---: | ---: | --- |
| macOS arm64/x86_64 | Yes | Yes | Homebrew install path. |
| Linux x86_64 | Yes | Yes | AppImage/deb plus curl-pipe daemon. |
| Raspberry Pi 5 arm64 | No | Yes | Headless-first self-hosted path. |
| Windows | No | No | Not planned for v1.5. |

Transports:

| Transport | Public LTE access | Account | Notes |
| --- | ---: | ---: | --- |
| Tailscale Funnel | Yes | Tailscale | Best persistent personal setup. |
| Cloudflare Quick Tunnel | Yes | No | Best demo path; URL is ephemeral. |
| LAN HTTPS | No | No | Requires trusting Onibi's self-signed cert. |

## How Onibi Is Different

cmux is a strong macOS-native cockpit for multiple agents. Onibi focuses on the cross-platform and phone-first approval problem: Linux/Pi headless support, a PWA second screen, and three first-class transports.

Vanilla terminals and tmux are still the right substrate for many workflows. Onibi adds a structured approval protocol around local agents: the tool call blocks, the phone gets context, and the final decision can include an edited payload.

Hosted relays optimize for convenience. Onibi is local-first: no accounts, no telemetry, no hosted relay in v1.5.

## Docs

- [Security model](docs/security.md)
- [Adapter capabilities](docs/adapters.md)
- [Transports](docs/transports.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Protocol spec](SPEC.md)

## Roadmap

- [v1.6 milestone](https://github.com/gongahkia/onibi/issues?q=is%3Aissue+milestone%3Av1.6) - per-device tokens, audit log UI, deeper adapter hooks, SSE fallback.
- [v2.0 milestone](https://github.com/gongahkia/onibi/issues?q=is%3Aissue+milestone%3Av2.0) - hosted relay option, native mobile app, multi-machine fan-in.

## License

Apache-2.0. See [LICENSE](LICENSE).

## Acknowledgements

Onibi is inspired by the local-agent cockpit category that cmux pushed forward, by Ghostty's terminal-engine discipline, and by the practical transport work from Tailscale and Cloudflare.
