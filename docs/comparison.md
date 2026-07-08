# Comparison Notes

These notes are positioning docs, not attack pages. Use them to pick the simpler tool for the job.

## Onibi vs OpenAI Codex Remote

Codex Remote is the first-party path for using Codex away from the host machine. OpenAI's remote-connections docs describe using Codex in the ChatGPT mobile app to work with Codex on a connected Mac or Windows host, continue work from another supported Codex App device, or connect the Codex App to projects on an SSH host. The connected host supplies the projects, threads, files, credentials, permissions, plugins, Computer Use setup, browser setup, and local tools. The phone sends prompts, approvals, and follow-up messages through OpenAI's secure relay layer.

That is the best fit for existing Codex users who want the official ChatGPT/Codex flow. Setup starts in the Codex App, pairs a phone by QR code, and then exposes host projects and Codex threads inside ChatGPT mobile. It also inherits OpenAI account, workspace, admin, MFA/SSO/passkey, and plan behavior. Current Codex docs list Codex in ChatGPT plans and note that API-key sign-in leaves some cloud features unavailable.

Onibi chooses a different source of truth. It hosts the terminal session locally under the user's OS account, then exposes a browser cockpit, WebSocket PTY stream, owner approval queue, snapshots, file panel, timeline, and handoff controls. Codex can be one hosted agent, but the same session model also covers Claude Code, OpenCode, Goose, Copilot CLI, Gemini, Amp, Pi, and plain shell/TUI work. Public relay modes are optional transports, and Onibi's Cloudflare relay path documents its app-layer E2E frame protocol in `docs/SPEC-e2e.md`.

Core differences:

| area | Codex Remote | Onibi |
|---|---|---|
| account model | Codex access in a ChatGPT account/workspace | local daemon; no ChatGPT subscription required for Onibi itself |
| agent scope | Codex | multi-agent plus arbitrary shell/TUI sessions |
| host model | Codex App host or SSH host reached through Codex | Onibi-managed tmux-backed local session |
| mobile surface | ChatGPT mobile / Codex App remote control | PWA/browser cockpit plus chat/notify bridges |
| relay model | OpenAI secure relay for authorized devices | LAN/hotspot/Tailscale/Cloudflare/ngrok/chat transports; Cloudflare path has documented app-layer E2E |
| controls | prompts, approvals, outputs, diffs, terminal output, screenshots | terminal I/O, approvals, files, snapshots, timeline, handoff, sharing, push |

Honest tradeoff: pick Codex Remote when the work is already Codex-first and you want the lowest-friction, first-party ChatGPT mobile integration. It should win on account integration, Codex-specific continuity, and official support. Pick Onibi when the durable object is a local terminal session and you need one cockpit across multiple agents, non-OpenAI tools, local-first hosting, open transport docs, and chat-control fallbacks.

Sources checked:

- OpenAI Codex remote connections: <https://developers.openai.com/codex/remote-connections>
- OpenAI Codex app: <https://developers.openai.com/codex/app>
- OpenAI Codex pricing: <https://developers.openai.com/codex/pricing>

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
