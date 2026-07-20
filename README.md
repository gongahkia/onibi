# `Onibi`

<p align="center">
  <img src="./asset/logo/marketing.png" width="50%" alt="Onibi">
</p>

<p align="center">
  <a href="https://github.com/gongahkia/onibi/actions/workflows/ci.yml"><img alt="ci" src="https://img.shields.io/github/actions/workflow/status/gongahkia/onibi/ci.yml?branch=main&style=flat-square"></a>
  <img alt="go" src="https://img.shields.io/badge/go-1.26.4%2B-blue?style=flat-square">
  <img alt="platform" src="https://img.shields.io/badge/platform-macOS%20focused%20%7C%20Linux%20beta-lightgrey?style=flat-square">
  <img alt="transport" src="https://img.shields.io/badge/transport-local%20web%20cockpit-1f766f?style=flat-square">
  <img alt="license" src="https://img.shields.io/badge/license-Apache--2.0-blue?style=flat-square">
</p>

Coding agents often run locally, but approvals and quick fixes arrive when you are away from the keyboard.

Onibi runs local shells and coding agents in managed tmux-backed sessions, then gives your phone a live terminal and approval cockpit over HTTPS/WebSocket.

Status: focused local web cockpit. Onibi hosts a managed terminal locally, pairs one owner phone, and routes Claude Code approval requests to that cockpit.

## Quick Start

```bash
brew install gongahkia/onibi/onibi
onibi install-hooks --agent claude
onibi up
```

Use `onibi up --transport=lan` for local pairing or `--transport=tailscale` for an authenticated tailnet. Run `onibi status` and `onibi doctor --fix` when validating an install. Update Onibi through its package manager. Source-build setup lives in [`CONTRIBUTING.md`](./CONTRIBUTING.md).

On iPhone:

1. Install and fully trust the printed `onibi-local-ca.mobileconfig` once.
2. Scan the QR printed by `onibi up`.
3. Use the terminal cockpit from Safari. Use `MAC` to open the same session in a visible macOS terminal and `PHONE` to return it to Safari.

If a managed Wi-Fi blocks device-to-device traffic, connect the Mac to the iPhone hotspot, rerun `./bin/onibi up`, and scan the new QR.

## What Makes This Different

- Host-local sessions: Onibi creates and owns tmux-backed PTYs instead of only monitoring another app's session.
- Same session on phone and Mac: drive one live session from mobile Safari or Ghostty on macOS.
- Approval enforcement: supported hooks can block risky tool calls before execution, including Claude Code `PreToolUse` deny flows.
- Terminal fidelity: live xterm.js over WebSocket, `xterm-ghostty` terminfo, JetBrains Mono, Sixel/IIP rendering, Kitty graphics transcoding to IIP, and a [terminal fidelity smoke matrix](./docs/terminal-fidelity.md).

Branding note: Onibi is not affiliated with the [Ghostty](https://ghostty.org) terminal emulator project; see [Branding](./docs/branding.md).

## What Works Now

- Managed tmux-backed session created by `onibi up`.
- Live xterm.js terminal over `/ws/pty`.
- Multi-session phone dashboard at `/#/sessions` with attach and guarded kill actions.
- Pair-by-QR over local HTTPS.
- iPhone trusted local CA profile.
- Top controls: `MAC`, `PHONE`, `INT`, `KILL`.
- Bottom soft-key bar: `Esc`, `Tab`, `Ctrl`, `Alt`, arrows, `^C`, `^D`, `^Z`, `Paste`, theme toggle.
- Claude Code hook approvals rendered as web overlay cards.
- Deny flow blocks Claude Write calls before file creation.
- Local shell fallback for arbitrary commands and `vim`.
- `onibi show` / `onibi hide` for tmux-backed session visibility.
- [`docs/comparison.md`](./docs/comparison.md) states Onibi's focused scope and alternatives.

## Product Scope

See [`docs/roadmap.md`](./docs/roadmap.md) for the focused product boundary.

## Main Commands

```bash
./bin/onibi
./bin/onibi quickstart
./bin/onibi status
./bin/onibi up
./bin/onibi show
./bin/onibi hide --headless
./bin/onibi pair
./bin/onibi cloudflare setup
./bin/onibi devices
./bin/onibi unpair <device-id>
./bin/onibi install-hooks --interactive
./bin/onibi hooks --show --all
./bin/onibi adapters
./bin/onibi doctor
./bin/onibi uninstall --dry-run
./bin/onibi logo
```

CLI aliases include `start` for `up`, `qr` for `pair`, `phones` for `devices`, `integrations` for `adapters`, `integrate` for `install-hooks`, `check` for `doctor`, and `ps` for `sessions`.

Useful CLI flags:

- Global: `--quiet`, `--debug`, `--no-logo`, `--logo-width <cols>`, `--color auto|always|never`.
- `up`: `--first-run`, `--shell <bin>`, `--cwd <dir>`, `--no-login-shell`, `--visible`, `--no-qr`, `--log-file <path>`.
- `pair`: `--host <host>`, `--port <port>`, `--copy`, `--no-qr`, `--fallbacks=false`, `--json`.
- `install-hooks`: installs supported agent hooks; `--all` installs detected agent hooks without prompting; `--dry-run` prints the plan.
- `status`: `--compact`, `--watch`, `--interval <duration>`, `--timeout <duration>`, `--no-doctor`, `--no-hooks`, `--json`, `--strict`.
- `doctor`: `--fix`, `--release`, `--explain`, `--offline`, `--json`.
- `uninstall`: `--service`, `--hooks`, `--agent <name>`, `--state`, `--yes`, `--dry-run`, `--json`.

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

- [`docs/ios-cert-install.md`](./docs/ios-cert-install.md): iPhone certificate trust flow.
- [`docs/pwa-install.md`](./docs/pwa-install.md): Home Screen install flow for iOS and Android.
- [`docs/fresh-machine-smoke.md`](./docs/fresh-machine-smoke.md): fresh macOS/Ubuntu install dry-run.
- [`docs/linux-beta.md`](./docs/linux-beta.md): Linux beta matrix and release-host policy.
- [`BENCHMARKS.md`](./BENCHMARKS.md): local cold-start, RSS, and PTY-throughput baselines.
- [`docs/transports.md`](./docs/transports.md): local web transport paths.
- [`docs/wireguard-setup.md`](./docs/wireguard-setup.md): self-hosted WireGuard transport setup.
- [`docs/zerotier-setup.md`](./docs/zerotier-setup.md): ZeroTier mesh transport setup.
- [`docs/transport-smoke.md`](./docs/transport-smoke.md): real-device transport smoke checklist.
- [`docs/ws-events-protocol.md`](./docs/ws-events-protocol.md): WebSocket event protocol.
- [`docs/comparison.md`](./docs/comparison.md): focused product positioning and alternatives.
- [`docs/terminal-fidelity.md`](./docs/terminal-fidelity.md): iPhone Safari terminal application smoke matrix.
- [`docs/branding.md`](./docs/branding.md): Onibi and Ghostty naming boundaries.
- [`docs/tap-integrity.md`](./docs/tap-integrity.md): Homebrew tap checksum and GPG verification.
- [`docs/web-push.md`](./docs/web-push.md): iOS web push setup and troubleshooting.
- [`docs/adapter-contract.md`](./docs/adapter-contract.md): v1-certified Claude, Codex, and Pi adapter capabilities and limits.
- [`docs/roadmap.md`](./docs/roadmap.md): focused product boundary.

## Security

- HTTPS is required for the phone cockpit.
- Pair tokens are single-use and short-lived.
- Owner browser identity is stored in an HttpOnly Secure cookie.
- WebSocket upgrades require the owner cookie plus matching token.
- Approval hooks fail open if the local daemon is unavailable.
- Linux without Secret Service stores the SQLite master key in an unencrypted 0600 fallback file; see [`docs/security.md#at-rest-state`](./docs/security.md#at-rest-state).
- The local OS user account remains trusted.

## Branding

Onibi is not affiliated with the [Ghostty](https://ghostty.org) terminal emulator project. Onibi uses `xterm-ghostty` as a technical compatibility reference and may launch Ghostty for macOS handover; it does not claim visual matching or control. Usage boundaries live in [`docs/branding.md`](./docs/branding.md).

## License

Apache-2.0.
