# X Thread Draft

Post 1:

```text
I built Onibi: a local-first approval gate for AI coding agents, with approvals on your phone.

An agent pauses. Your phone buzzes. You approve, deny, or edit the command before it runs.

Demo:
```

Attach: `asset/screencast/onibi-launch.gif`

Post 2:

```text
The core interaction is edit-before-approve.

Example: Claude proposes:
rm -rf tests/legacy

From the phone, change it to:
mv tests/legacy tests/legacy.bak

Then approve with edits and let the agent continue.
```

Post 3:

```text
Architecture:

Agents -> Onibi over HTTP hooks.
Onibi -> desktop over Tauri events.
Onibi -> phone over WebSocket + Web Push.
Transports: Tailscale Funnel, Cloudflare Quick Tunnel, LAN HTTPS.
```

Attach: `docs/architecture.png`

Post 4:

```text
Claude Code is the strongest adapter because its PreToolUse hook can return updatedInput.

Codex is Bash-only. Hermes uses ACP. OpenCode, Qoder, Copilot CLI, and Goose have blocking hook/event integrations. Gemini, Aider, Cursor, Pi, and OMP are mirror, resume-only, or pending-native.
```

Post 5:

```text
Onibi is local-first:

- no accounts
- no telemetry
- no hosted relay in v1.5
- SQLite on your machine
- bearer token pairing
- keychain first, 0600 file fallback for headless/Pi
```

Post 6:

```text
It runs as a Tauri desktop app or a headless daemon.

Targets:
- macOS arm64/x86_64
- Linux x86_64
- Raspberry Pi 5 arm64 headless
```

Post 7:

```text
Install:

macOS:
brew tap gongahkia/onibi
brew install onibi

Linux / Pi:
curl -fsSL https://onibi.sh/install.sh | bash

Then:
onibi setup
onibi doctor
```

Post 8:

```text
Repo is Apache-2.0:
https://github.com/gongahkia/onibi

I would love adapter PRs. The approval protocol is intentionally small enough for other local agents and phone clients to implement.
```

Timing:

- Fire within 30 minutes after HN.
- Put the demo GIF on the first post.
- Reply to the thread with the blog link after the repo link is live.
