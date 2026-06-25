# Onibi

<p align="center">
  <img src="./asset/logo/onibi.png" width="50%" alt="Onibi">
</p>

<p align="center">
  <a href="https://github.com/gongahkia/onibi/actions/workflows/ci.yml"><img alt="ci" src="https://img.shields.io/github/actions/workflow/status/gongahkia/onibi/ci.yml?branch=main&style=flat-square"></a>
  <img alt="go" src="https://img.shields.io/badge/go-1.26.4%2B-blue?style=flat-square">
  <img alt="platform" src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey?style=flat-square">
  <img alt="transport" src="https://img.shields.io/badge/transport-LAN%20web-1f766f?style=flat-square">
  <img alt="license" src="https://img.shields.io/badge/license-Apache--2.0-blue?style=flat-square">
</p>

Web-controlled coding-agent host with a live xterm.js terminal and approval cockpit.

Onibi runs local shells and coding agents under PTYs, exposes a phone cockpit over local HTTPS/WebSocket, and routes tool approvals through an owner-only web UI. The current v3 path is LAN/hotspot-first: run `onibi up`, scan the QR from your phone, then drive the live terminal and approval overlay from mobile Safari.

Status: v3 web-cockpit pivot in progress. The local shell cockpit, iPhone pairing, live terminal, resize/reconnect smoke, toolbar controls, Claude Code approval overlay, and device management command surface have been validated locally. Mobile polish, Tailscale transport, and release prep are still tracked in [`TODO.md`](./TODO.md).

## Quick Start

```bash
make build
./bin/onibi install-hooks --agent claude
./bin/onibi up
```

On iPhone:

1. Install and fully trust the printed `onibi-local-ca.mobileconfig` once.
2. Scan the QR printed by `onibi up`.
3. Use the terminal cockpit from Safari.

If a managed Wi-Fi blocks device-to-device traffic, connect the Mac to the iPhone hotspot, rerun `./bin/onibi up`, and scan the new QR.

## What Works Now

- Live xterm.js terminal over `/ws/pty`.
- Pair-by-QR over local HTTPS.
- iPhone trusted local CA profile.
- Top controls: `INT`, `KILL`.
- Bottom soft-key bar: `Esc`, `Tab`, `Ctrl`, `Alt`, arrows, `^C`, `^D`, `^Z`, `Paste`, theme toggle.
- Claude Code hook approvals rendered as web overlay cards.
- Deny flow blocks Claude Write calls before file creation.
- Local shell fallback for arbitrary commands and `vim`.

## Main Commands

```bash
./bin/onibi up
./bin/onibi pair
./bin/onibi devices
./bin/onibi unpair <device-id>
./bin/onibi install-hooks --agent claude
./bin/onibi hooks --show --agent claude
./bin/onibi adapters
./bin/onibi doctor
```

## Current Test Flow

After `./bin/onibi up` and phone pairing:

1. Run `vim /tmp/onibi-smoke.txt` from the phone.
2. Edit, press `ESC`, then `:wq`.
3. Rotate the phone and confirm the terminal remains usable.
4. For hotspot mode, background Safari for 10 seconds and return; use airplane mode only on shared Wi-Fi.
5. Run `claude`.
6. Ask Claude to create `/tmp/onibi-approval-deny.txt`.
7. Tap `Deny` on the Onibi approval card.
8. Verify the file does not exist.

## Architecture

- `internal/pty` hosts shells and agents under PTYs.
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
