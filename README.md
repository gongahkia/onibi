# `Onibi`

<p align="center">
  <img src="./asset/logo/marketing.png" width="50%" alt="Onibi">
</p>

<p align="center">
  <a href="https://github.com/gongahkia/onibi/actions/workflows/ci.yml"><img alt="ci" src="https://img.shields.io/github/actions/workflow/status/gongahkia/onibi/ci.yml?branch=main&style=flat-square"></a>
  <img alt="go" src="https://img.shields.io/badge/go-1.26.4%2B-blue?style=flat-square">
  <img alt="platform" src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey?style=flat-square">
  <img alt="transport" src="https://img.shields.io/badge/transport-LAN%20%7C%20Tailscale%20%7C%20WireGuard%20%7C%20ZeroTier%20%7C%20Cloudflare%20%7C%20ngrok%20%7C%20Chat%20%7C%20Notify-1f766f?style=flat-square">
  <img alt="license" src="https://img.shields.io/badge/license-Apache--2.0-blue?style=flat-square">
</p>

Coding agents often run locally, but approvals and quick fixes arrive when you are away from the keyboard.

Onibi runs local shells and coding agents in managed tmux-backed sessions, then gives your phone a live terminal and approval cockpit over HTTPS/WebSocket.

Status: v3 web-cockpit pivot in progress. The local shell cockpit, managed tmux session path, iPhone pairing, live terminal, resize/reconnect smoke, handover controls, Claude Code approval overlay, device management command surface, and Tailscale transport have local or real-phone coverage. Remaining work is tracked in GitHub issues.

Launch demos and cockpit visuals live in [`SHOWCASE.md`](./SHOWCASE.md).

## Quick Start

```bash
brew install gongahkia/onibi/onibi
onibi install-hooks --agent claude
onibi up
```

`onibi up` prompts first for a category (`Web URL`, `Chat`, or `Notify-only`) and then for a supported provider. Use `onibi up --transport=<mode>` to skip the picker. Supported modes are `lan`, `tailscale`, `tailscale-private`, `wireguard`, `zerotier`, `cloudflare-quick`, `cloudflare-named`, `ngrok`, `telegram`, `matrix`, `slack`, `discord`, `zulip`, `irc`, `signal`, `pushover`, `ntfy`, `gotify`, `apns`, `sms`, `email`, and `auto`.
Run `onibi status`, `onibi update-check`, and `onibi doctor --fix` when validating an install or upgrade. Source-build setup lives in [`CONTRIBUTING.md`](./CONTRIBUTING.md).

On iPhone:

1. Install and fully trust the printed `onibi-local-ca.mobileconfig` once.
2. Scan the QR printed by `onibi up`.
3. Use the terminal cockpit from Safari. Use `MAC` to open the same session in a visible macOS terminal and `PHONE` to return it to Safari.

If a managed Wi-Fi blocks device-to-device traffic, connect the Mac to the iPhone hotspot, rerun `./bin/onibi up`, and scan the new QR.

## What Makes This Different

- Host-local sessions: Onibi creates and owns tmux-backed PTYs instead of only monitoring another app's session.
- Same session everywhere: drive one live session from mobile Safari, a visible Mac terminal, chat transports, or local MCP clients.
- Approval enforcement: supported hooks can block risky tool calls before execution, including Claude Code `PreToolUse` deny flows.
- Terminal fidelity: live xterm.js over WebSocket, `xterm-ghostty` terminfo, a Ghostty-inspired theme, JetBrains Mono, Sixel/IIP rendering, Kitty graphics transcoding to IIP, and a [terminal fidelity smoke matrix](./docs/terminal-fidelity.md).

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
- Chat text control: Telegram, Matrix, Slack, Discord, Zulip, IRC, and Signal can route terminal text; approvals support provider-native actions where available.
- Notify-only approvals: Pushover, ntfy, Gotify, APNs, SMS, and email publish approval alerts without terminal text input.
- [`docs/comparison.md`](./docs/comparison.md) covers tradeoffs against remote-agent and terminal-sharing tools.

## Roadmap

The v0.3 to v1.0 release arc is tracked in [`docs/roadmap.md`](./docs/roadmap.md), with each release tied to a GitHub milestone.

## Main Commands

```bash
./bin/onibi
./bin/onibi quickstart
./bin/onibi status
./bin/onibi up
./bin/onibi show
./bin/onibi hide --headless
./bin/onibi pair
./bin/onibi share <session-id>
./bin/onibi telegram setup
./bin/onibi cloudflare setup
./bin/onibi devices
./bin/onibi unpair <device-id>
./bin/onibi install-hooks --interactive
./bin/onibi hooks --show --all
./bin/onibi adapters
./bin/onibi doctor
./bin/onibi update-check
./bin/onibi uninstall --dry-run
./bin/onibi logo
```

CLI aliases include `start` for `up`, `qr` for `pair`, `phones` for `devices`, `integrations` for `adapters`, `integrate` for `install-hooks`, `check` for `doctor`, and `ps` for `sessions`.

Useful CLI flags:

- Global: `--quiet`, `--debug`, `--no-logo`, `--logo-width <cols>`, `--color auto|always|never`.
- `up`: `--first-run`, `--shell <bin>`, `--cwd <dir>`, `--no-login-shell`, `--visible`, `--no-qr`, `--log-file <path>`.
- `telegram`: `setup [--no-check]`, `status [--check]`, `disable`; `up --transport=telegram` starts chat-native text control.
- `pair`: `--host <host>`, `--port <port>`, `--copy`, `--no-qr`, `--fallbacks=false`, `--json`.
- `share`: `--ttl <duration>`, `--max-viewers <n>`, `--copy`, `--no-qr`, `--fallbacks=false`, `--json`.
- `install-hooks`: no args auto-detects agent config dirs and shell RC files; `--all` installs detected hooks without prompting; `--dry-run` prints the plan.
- `status`: `--compact`, `--watch`, `--interval <duration>`, `--timeout <duration>`, `--no-doctor`, `--no-hooks`, `--no-update`, `--refresh-update`, `--json`, `--strict`.
- `doctor`: `--fix`, `--release`, `--after-upgrade`, `--explain`, `--offline`, `--json`.
- `uninstall`: `--service`, `--hooks`, `--agent <name>`, `--shell <name>`, `--state`, `--yes`, `--dry-run`, `--json`.

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
- `internal/telegram`, `internal/matrix`, `internal/slack`, `internal/discord`, `internal/zulip`, `internal/irc`, and `internal/signal` provide chat-native text input/output and approval callbacks.
- `internal/pushover`, `internal/ntfy`, `internal/gotify`, `internal/apns`, `internal/sms`, and `internal/email` provide notify-only approval delivery.
- `internal/intake` receives hook events from `onibi-notify` over a same-UID Unix socket.
- `internal/approval` owns the approval queue and decision state machine.
- `frontend/` contains the xterm.js cockpit.
- [`docs/comparison.md`](./docs/comparison.md) compares Onibi's session-hosting architecture with Herdr, Codex Remote, CCRC, and OpenCode Mobile.

## Docs

- [`docs/ios-cert-install.md`](./docs/ios-cert-install.md): iPhone certificate trust flow.
- [`docs/pwa-install.md`](./docs/pwa-install.md): Home Screen install flow for iOS and Android.
- [`docs/fresh-machine-smoke.md`](./docs/fresh-machine-smoke.md): fresh macOS/Ubuntu install dry-run.
- [`docs/linux-beta.md`](./docs/linux-beta.md): Linux beta matrix and release-host policy.
- [`docs/paired-host-registry.md`](./docs/paired-host-registry.md): encrypted local PWA host registry.
- [`SHOWCASE.md`](./SHOWCASE.md): sanitized asciinema launch demos.
- [`BENCHMARKS.md`](./BENCHMARKS.md): local cold-start, RSS, and PTY-throughput baselines.
- [`docs/transports.md`](./docs/transports.md): transport categories and provider notes.
- [`docs/wireguard-setup.md`](./docs/wireguard-setup.md): self-hosted WireGuard transport setup.
- [`docs/zerotier-setup.md`](./docs/zerotier-setup.md): ZeroTier mesh transport setup.
- [`docs/ssh-transport.md`](./docs/ssh-transport.md): SSH remote bootstrap, tunnel, service, and teardown model.
- [`docs/transport-smoke.md`](./docs/transport-smoke.md): real-device transport smoke checklist.
- [`docs/ws-events-protocol.md`](./docs/ws-events-protocol.md): WebSocket event protocol.
- [`docs/comparison.md`](./docs/comparison.md): positioning against remote-agent and terminal-sharing tools.
- [`docs/ghostty-parity.md`](./docs/ghostty-parity.md): Ghostty/web cockpit visual parity smoke.
- [`docs/terminal-fidelity.md`](./docs/terminal-fidelity.md): iPhone Safari terminal application smoke matrix.
- [`docs/branding.md`](./docs/branding.md): Onibi and Ghostty naming boundaries.
- [`docs/files-panel.md`](./docs/files-panel.md): file tree/view/edit security model.
- [`docs/sharing.md`](./docs/sharing.md): read-only viewer sharing threat model and TTL guidance.
- [`docs/team-setup.md`](./docs/team-setup.md): one-owner, one-viewer workspace sharing walkthrough.
- [`docs/tap-integrity.md`](./docs/tap-integrity.md): Homebrew tap checksum and GPG verification.
- [`docs/web-push.md`](./docs/web-push.md): iOS web push setup and troubleshooting.
- [`docs/workspaces.md`](./docs/workspaces.md): team-shareable workspace defaults and private binding workflow.
- [`docs/trust-policies.md`](./docs/trust-policies.md): auto-approval trust policy examples and threat model.
- [`docs/adapter-contract.md`](./docs/adapter-contract.md): v1-certified Claude, Codex, and Pi adapter capabilities and limits.
- [`docs/anomaly-detection.md`](./docs/anomaly-detection.md): anomaly rules, examples, and current custom-rule limits.
- [`docs/pricing.md`](./docs/pricing.md): budget pricing table and update notes.
- [`docs/roadmap.md`](./docs/roadmap.md): v0.3 to v1.0 milestone framing.
- [`docs/update-check-schema.md`](./docs/update-check-schema.md): machine-readable `update-check --json` contract.

## Security

- HTTPS is required for the phone cockpit.
- Pair tokens are single-use and short-lived.
- Owner browser identity is stored in an HttpOnly Secure cookie.
- WebSocket upgrades require the owner cookie plus matching token.
- Approval hooks fail open if the local daemon is unavailable.
- Linux without Secret Service stores the SQLite master key in an unencrypted 0600 fallback file; see [`docs/security.md#at-rest-state`](./docs/security.md#at-rest-state).
- The local OS user account remains trusted.

## Branding

Onibi is not affiliated with the [Ghostty](https://ghostty.org) terminal emulator project. Onibi uses the `xterm-ghostty` terminfo and a Ghostty-inspired color theme; the name and any perceived resemblance are coincidental / homage. Usage boundaries live in [`docs/branding.md`](./docs/branding.md).

## License

Apache-2.0.
