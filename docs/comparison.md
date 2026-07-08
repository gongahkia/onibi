# Comparison Notes

These notes are positioning docs, not attack pages. Use them to pick the simpler tool for the job.

## Onibi vs OpenCode Mobile

OpenCode Mobile is a focused phone control surface for OpenCode work. Its site describes a mobile client for inspecting running tasks, approving or redirecting changes, and keeping progress moving from a phone. The docs center the product on OpenCode sessions, workspaces, server connection health, provider defaults, changed files, and live progress. The recommended remote setup is an OpenCode server on `127.0.0.1`, protected with `OPENCODE_SERVER_USERNAME` / `OPENCODE_SERVER_PASSWORD`, exposed through a tunnel such as Cloudflare Tunnel.

That is a strong fit when OpenCode is the only agent in scope. OpenCode Mobile can optimize for one server protocol, one workspace model, and a compact task/diff/log workflow. Its public repository is MIT-licensed and forked from OpenCode, so it is the closer OSS-native choice for users who want an OpenCode-first mobile app.

Onibi is broader and heavier. It hosts managed tmux-backed sessions and exposes a live xterm.js cockpit over the browser. The session can be a plain shell, Claude Code, Codex, OpenCode, Goose, or another supported adapter. The same owner session routes terminal I/O, web approvals, snapshots, file browsing, timeline events, web push, chat bridges, and Mac/phone handoff. Public relays are transports; the daemon remains the source of truth for session lifetime and approval enforcement.

Core differences:

| area | OpenCode Mobile | Onibi |
|---|---|---|
| install shape | mobile app/APK pointed at an OpenCode server | paired PWA/browser cockpit, no app-store dependency |
| agent scope | OpenCode workflow | multi-agent plus arbitrary shell/TUI sessions |
| main UI object | workspace/task/session state | live terminal session id |
| approvals | OpenCode-centered review/action flow | hook-backed owner approval queue across supported adapters |
| handoff | mobile client to OpenCode server | phone, Mac/tmux, browser, chat, and notify surfaces |
| transport posture | recommended tunnel to protected OpenCode server | LAN/hotspot/Tailscale/Cloudflare/ngrok/chat transports with Onibi-owned auth |

Honest tradeoff: pick OpenCode Mobile when you use only OpenCode and want the most direct mobile UX for its server/client model. Its task and diff workflow is narrower and therefore can be more polished for that one case. Pick Onibi when you want one local host for multiple agents, plain terminal fallback, browser-based pairing, cross-surface handoff, and approval enforcement that is not tied to one agent's UI.

Sources checked:

- OpenCode Mobile homepage: <https://getopencode.app/>
- OpenCode Mobile introduction: <https://getopencode.app/docs/introduction/>
- OpenCode Mobile features: <https://getopencode.app/docs/features/>
- OpenCode Mobile remote access: <https://getopencode.app/docs/remote-access/>
- OpenCode Mobile GitHub repository: <https://github.com/agustif/opencode-mobile>
