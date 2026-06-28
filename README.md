# Onibi

<p align="center">
  <img src="./asset/logo/onibi.png" width="50%" alt="Onibi">
</p>

<p align="center">
  <a href="https://github.com/gongahkia/onibi/actions/workflows/ci.yml"><img alt="ci" src="https://img.shields.io/github/actions/workflow/status/gongahkia/onibi/ci.yml?branch=main&style=flat-square"></a>
  <img alt="go" src="https://img.shields.io/badge/go-1.26.4%2B-blue?style=flat-square">
  <img alt="platform" src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey?style=flat-square">
  <img alt="transport" src="https://img.shields.io/badge/transport-LAN%20%7C%20Tailscale-1f766f?style=flat-square">
  <img alt="license" src="https://img.shields.io/badge/license-Apache--2.0-blue?style=flat-square">
</p>

Web-controlled coding-agent host with a live xterm.js terminal and approval cockpit.

Onibi runs local shells and coding agents in managed tmux-backed sessions, exposes a phone cockpit over HTTPS/WebSocket, and routes tool approvals through an owner-only web UI. Run `onibi up`, choose LAN/hotspot or Tailscale Funnel, scan the QR from your phone, then drive or hand over the same live session between mobile Safari and a visible Mac terminal.

Status: v3 web-cockpit pivot in progress. The local shell cockpit, managed tmux session path, iPhone pairing, live terminal, resize/reconnect smoke, handover controls, Claude Code approval overlay, device management command surface, and Tailscale transport have local or real-phone coverage. Release prep is still tracked in [`TODO.md`](./TODO.md).

## Quick Start

```bash
make build
./bin/onibi install-hooks --agent claude
./bin/onibi up
```

`onibi up` prompts for a pairing transport when run from a terminal. Use `./bin/onibi up --transport=lan`, `--transport=tailscale`, or `--transport=auto` to skip the picker.

On iPhone:

1. Install and fully trust the printed `onibi-local-ca.mobileconfig` once.
2. Scan the QR printed by `onibi up`.
3. Use the terminal cockpit from Safari. Use `MAC` to open the same session in a visible macOS terminal and `PHONE` to return it to Safari.

If a managed Wi-Fi blocks device-to-device traffic, connect the Mac to the iPhone hotspot, rerun `./bin/onibi up`, and scan the new QR.

## What Works Now

- Managed tmux-backed session created by `onibi up`.
- Live xterm.js terminal over `/ws/pty`.
- Pair-by-QR over local HTTPS.
- iPhone trusted local CA profile.
- Top controls: `MAC`, `PHONE`, `INT`, `KILL`.
- Bottom soft-key bar: `Esc`, `Tab`, `Ctrl`, `Alt`, arrows, `^C`, `^D`, `^Z`, `Paste`, theme toggle.
- Claude Code hook approvals rendered as web overlay cards.
- Deny flow blocks Claude Write calls before file creation.
- Local shell fallback for arbitrary commands and `vim`.
- `onibi show` / `onibi hide` for tmux-backed session visibility.

## Main Commands

```bash
./bin/onibi
./bin/onibi quickstart
./bin/onibi status
./bin/onibi up
./bin/onibi show
./bin/onibi hide --headless
./bin/onibi pair
./bin/onibi devices
./bin/onibi unpair <device-id>
./bin/onibi install-hooks --interactive
./bin/onibi hooks --show --all
./bin/onibi adapters
./bin/onibi doctor
./bin/onibi logo
```

CLI aliases include `start` for `up`, `qr` for `pair`, `phones` for `devices`, `integrations` for `adapters`, `integrate` for `install-hooks`, `check` for `doctor`, and `ps` for `sessions`.

Useful CLI flags:

- Global: `--quiet`, `--debug`, `--no-logo`, `--logo-width <cols>`, `--color auto|always|never`.
- `up`: `--shell <bin>`, `--cwd <dir>`, `--no-login-shell`, `--visible`, `--no-qr`, `--log-file <path>`.
- `pair`: `--host <host>`, `--port <port>`, `--copy`, `--no-qr`, `--fallbacks=false`, `--json`.
- `status`: `--compact`, `--watch`, `--interval <duration>`, `--timeout <duration>`, `--no-doctor`, `--no-hooks`, `--json`, `--strict`.

## Current Test Flow

After `./bin/onibi up` and phone pairing:

1. Run `vim /tmp/onibi-smoke.txt` from the phone.
2. Edit, press `ESC`, then `:wq`.
3. Rotate the phone and confirm the terminal remains usable.
4. Confirm no visible `ONIBI-RESIZE:*` marker appears.
5. Tap `MAC`; confirm the same cwd/history/running process opens in a macOS terminal.
6. Tap `PHONE`; confirm the same session returns to Safari.
7. For hotspot mode, background Safari for 10 seconds and return; use airplane mode only on shared Wi-Fi.
8. Run `claude`.
9. Ask Claude to create `/tmp/onibi-approval-deny.txt`.
10. Tap `Deny` on the Onibi approval card.
11. Verify the file does not exist.
12. Stop `onibi up` and confirm no `onibi-*` tmux sessions remain.

## Architecture

- `internal/tmux` creates managed tmux-backed sessions for handover.
- `internal/pty` bridges web terminal I/O to local PTYs and tmux attach clients.
- `internal/web` serves HTTPS, static frontend assets, `/ws/pty`, `/ws/events`, `/control`, `/approval`, and `/pair`.
- `internal/intake` receives hook events from `onibi-notify` over a same-UID Unix socket.
- `internal/approval` owns the approval queue and decision state machine.
- `frontend/` contains the xterm.js cockpit.

## Docs

- [`TODO.md`](./TODO.md): authoritative v3 task list.
- [`docs/ios-cert-install.md`](./docs/ios-cert-install.md): iPhone certificate trust flow.
- [`docs/transports.md`](./docs/transports.md): LAN/Tailscale transport notes.
- [`docs/ws-events-protocol.md`](./docs/ws-events-protocol.md): WebSocket event protocol.

## Security

- HTTPS is required for the phone cockpit.
- Pair tokens are single-use and short-lived.
- Owner browser identity is stored in an HttpOnly Secure cookie.
- WebSocket upgrades require the owner cookie plus matching token.
- Approval hooks fail open if the local daemon is unavailable.
- The local OS user account remains trusted.

## License

Apache-2.0.
