# GitHub Release Notes Draft

Tag: `v1.5.0`

Title:

```text
Onibi v1.5.0 - Tauri cockpit + mobile second-screen
```

Body:

```markdown
Onibi v1.5.0 is the first launch release of the local-first approval cockpit for AI coding agents.

## Highlights

- Desktop cockpit built with Tauri, React, and xterm.js.
- Headless daemon mode for Linux and Raspberry Pi 5.
- Mobile PWA for pending approvals and terminal mirror.
- Approval protocol with long-polling hooks, WebSocket fanout, and structured allow/deny/edit decisions.
- Claude Code adapter with edit-before-approve support through `updatedInput`.
- Codex CLI Bash-only approval interception.
- Shell/mirror sessions for OpenCode, Gemini, Aider, Cursor agent, and Goose.
- Tailscale Funnel, Cloudflare Quick Tunnel, and LAN HTTPS transports.
- `onibi setup`, `onibi status`, and `onibi doctor` support commands.
- Security hardening: bearer auth, approval rate limit, body limits, WebSocket message cap, HSTS for tunnel responses, CSP for the PWA.

## Install

macOS:

```sh
brew tap gongahkia/onibi
brew install onibi
```

Linux / Pi:

```sh
curl -fsSL https://onibi.sh/install.sh | bash
```

## First Run

```sh
onibi setup
onibi doctor
onibi adapter install claude-code
onibi --headless --auto-transports
```

Scan the pairing QR with your phone and install the PWA.

## Known Limitations

- Codex interception is Bash-only; `apply_patch` and MCP tools are not blocked.
- Non-Claude/Non-Codex adapters are launch-visible as shell/mirror integrations.
- LAN HTTPS requires trusting Onibi's self-signed certificate on the phone.
- Cloudflare Quick Tunnel URLs are ephemeral.
- Windows is not supported in v1.5.

## Verification

- `cargo fmt --check`
- `cargo clippy --all-targets --all-features -- -D warnings`
- `cargo test --workspace`
- `pnpm --filter onibi-app typecheck`
- `pnpm --filter onibi-app test`
- `pnpm --filter onibi-mobile typecheck`
- `pnpm --filter onibi-mobile test`
- `cargo audit`
- `pnpm audit --prod`

See `docs/dry-run-log.md` for final QA notes.
```
