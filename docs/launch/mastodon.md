# Mastodon Thread Draft

Post 1:

```text
I built Onibi: a local-first approval gate for AI coding agents, with approvals on your phone.

Agents run locally. When a tool call needs permission, Onibi sends it to the desktop and mobile PWA so you can approve, deny, or edit before it runs.

https://github.com/gongahkia/onibi
```

Attach: `asset/screencast/onibi-launch.gif`

Post 2:

```text
The important bit is edit-before-approve.

If Claude proposes `rm -rf tests/legacy`, the phone can change it to `mv tests/legacy tests/legacy.bak` and approve the edited payload. No extra model turn required for agents that support edited hook input.
```

Post 3:

```text
Stack:

- Rust/Tauri desktop app
- headless daemon for Linux/Pi
- React mobile PWA
- WebSocket + Web Push
- Tailscale Funnel, Cloudflare Quick Tunnel, LAN HTTPS
- Apache-2.0
```

Post 4:

```text
Claude Code is the strongest adapter because its hook supports edited input. Codex has Bash-only interception. OpenCode, Qoder, Copilot CLI, and Goose have blocking hook/event integrations. Gemini, Aider, and Cursor remain mirror/resume-oriented.

Adapter PRs welcome.
```

Timing:

- Fire after X thread or in parallel later the same day.
- Use alt text on the GIF and architecture image.
