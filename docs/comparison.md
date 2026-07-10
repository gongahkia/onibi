# Comparison Notes

These notes are positioning docs, not attack pages. Use them to pick the simpler tool for the job.

## Quick Matrix

| tool | license | install path | host-local | phone UX | multi-agent support | tool approval enforcement | chat control surface | self-hostable | subscription |
|---|---|---|---|---|---|---|---|---|---|
| [Claude Code Remote Control](https://code.claude.com/docs/en/remote-control) | proprietary / commercial | Claude Code plus claude.ai or Claude mobile app | yes, Claude Code runs on the user's machine | Claude app and web | Claude Code | Claude Code permission flow | Claude app/web only | no | Claude plan required; API keys unsupported |
| [OpenAI Codex Remote](https://developers.openai.com/codex/remote-connections) | proprietary / commercial | Codex App plus ChatGPT/Codex mobile or web | yes, through a connected host or SSH host | ChatGPT/Codex surfaces | Codex | Codex approval flow | ChatGPT/Codex only | no | ChatGPT/Codex access required |
| [OpenCode Mobile](https://getopencode.app/) | Apache-2.0 app repo | mobile app or dev build plus OpenCode server | yes, OpenCode runs on the user's host/server | native/mobile app | OpenCode | OpenCode workflow only | no separate chat bridge | yes for OpenCode server and app repo | no paid tier found in checked docs |
| [Herdr + herdr-remote](https://herdr.dev/) | Herdr AGPL-3.0-or-later / commercial; herdr-remote has no license file found | curl/brew/mise Herdr plus herdr-remote relay | yes, Herdr panes run locally/remotely under Herdr | PWA/web, menu bar, Telegram | many agent panes | blocked-agent quick actions | Telegram approvals/notifications | yes for Herdr and relay components | Herdr commercial license for non-AGPL org use |
| [Moshi](https://getmoshi.app/) | proprietary app | App Store / Google Play | partial: connects to SSH/Mosh hosts | native iPhone/iPad/Android | agent-aware terminal over SSH/Mosh | terminal/app approval surfaces, not Onibi-style hooks | no separate chat bridge | no | free tier plus Pro |
| [sshx](https://github.com/ekzhang/sshx) | MIT | curl installer or Homebrew | partial: local process shares terminal through sshx mesh | browser terminal | arbitrary terminal, not agent-aware | no | no | no; README says self-hosting is not supported | none documented |
| [ttyd](https://github.com/tsl0922/ttyd) | MIT | brew, apt, snap, releases, WinGet, Scoop | yes | browser terminal | arbitrary command, not agent-aware | no | no | yes | none |
| [tmate](https://github.com/tmate-io/tmate) | BSD | source or package managers | partial: tmux fork with pairing service | SSH/tmux terminal | arbitrary terminal, not agent-aware | no | no | not covered in checked README | none |
| [Upterm](https://upterm.dev/) | Apache-2.0 | Homebrew cask, Scoop, binaries, source | partial: host shares an SSH session through Upterm server | SSH terminal | arbitrary terminal, not agent-aware | no | no | yes, supports a self-hosted server | none |

## Quick Pick Rules

- Pick Claude Code Remote Control when the workflow is Claude-only and official Anthropic mobile/web continuity matters more than agent plurality.
- Pick Codex Remote when the workflow is Codex-first and ChatGPT/Codex account integration is the desired control plane.
- Pick OpenCode Mobile when OpenCode is the only agent and a focused native/mobile OpenCode client is enough.
- Pick Herdr when the primary need is a terminal-native multi-pane agent multiplexer with phone/Telegram monitoring added on top.
- Pick Moshi when the desired product is a polished native SSH/Mosh terminal for phone-first agent watching.
- Pick sshx when the goal is instant collaborative browser terminal sharing through the hosted sshx mesh.
- Pick ttyd when you want a simple self-hosted web terminal for one command.
- Pick tmate when you want tmux-style SSH pair programming, especially with an existing tmux workflow.
- Pick Upterm when you want SSH-based terminal sharing, CI/debug access, or a self-hostable relay.
- Pick Onibi when you want one local, multi-agent, approval-enforcing phone/browser/chat cockpit around a hosted tmux-backed session.

## Onibi vs Herdr

Herdr is a strong neighboring design, not a straw man. Herdr's own positioning is terminal-native agent multiplexing: persistent real panes, detach/reattach, remote SSH attach, agent state, and a CLI/socket API agents can drive. Its docs describe agents staying in real terminal panes with shells, logs, prompts, and running processes intact, plus blocked/working/done/idle rollups across panes, tabs, and workspaces. `herdr-remote` adds a separate phone/menu-bar/Telegram surface with a web app, one-tap approvals, status views, a phone terminal view, special mobile keys, and Cloudflare quick-tunnel setup.

That means the honest comparison is not "Onibi has terminals and Herdr does not." Both care about live terminal state. The difference is the center of gravity: Herdr starts as an agent-aware terminal multiplexer and can be extended outward; Onibi starts as a local web/chat cockpit that owns pairing, HTTPS/WebSocket, mobile UI, provider bridges, and approval routing around tmux-backed sessions.

Side-by-side:

| capability | Herdr + herdr-remote | Onibi |
|---|---|---|
| session runtime | Herdr background server with persistent panes | Onibi-managed tmux-backed PTYs |
| primary UI | terminal multiplexer, SSH attach, direct terminal attach | phone-first browser cockpit with live xterm.js |
| phone access | `herdr-remote` web app/PWA through a relay URL | built-in paired HTTPS/PWA cockpit |
| agent state | first-class blocked/working/done/idle rollups | session list, timeline, approvals, anomaly/push events |
| direct terminal | terminal/SSH/direct attach; herdr-remote phone terminal view | `/ws/pty` live terminal with typing, soft keys, resize, replay |
| approvals | blocked-agent quick actions and Telegram inline approvals | owner approval queue, edit/deny/approve, runtime trust, hook enforcement |
| chat control | Telegram approval/notification surface in herdr-remote | Telegram/Slack/Matrix/Discord/Zulip/IRC/Signal text I/O plus approvals where supported |
| extension model | CLI/socket API, integrations, plugins | adapters, intake socket, provider bridges, web event stream |

Four differentiators:

1. **Session hosting.** Herdr also hosts persistent PTY panes, but users adopt Herdr as the multiplexer and add `herdr-remote` when they want phone/browser access. Onibi bundles the session host and phone cockpit in one daemon flow: `onibi up` creates or attaches the managed session, serves the cockpit, and owns pairing/auth.
2. **Real live terminal.** Herdr has real terminal panes and direct attach; current `herdr-remote` docs also show a phone terminal view. Onibi's distinction is that the live browser terminal is the default product surface, with xterm.js, mobile soft keys, resize/replay behavior, file/timeline panels, and handoff controls built around one session id.
3. **Hook-based enforcement.** Herdr integrations primarily report session identity, lifecycle state, or both; its remote surface exposes approval actions for blocked agents. Onibi's approval path is the enforcement boundary: provider hooks send blocking requests to the Onibi queue, and deny/edit decisions feed back through the hook contract before tool execution continues.
4. **Chat control surface.** `herdr-remote` documents Telegram inline approvals and notifications. Onibi's chat providers are broader control surfaces: Telegram, Matrix, Slack, Discord, Zulip, IRC, and Signal can send text into the hosted session, while notify-only providers stay approval/alert-only.

When to pick Herdr over Onibi: choose Herdr if you already want a terminal-native multiplexer for many agent panes and mainly need state rollups, direct terminal attach, SSH workflows, or a phone notification/approval add-on. Choose Onibi if you want a self-contained mobile/browser cockpit, chat text input, transport choice, and an owner approval queue around a hosted session rather than a terminal multiplexer as the primary UI.

Sources checked:

- Herdr homepage: <https://herdr.dev/>
- Herdr agents docs: <https://herdr.dev/docs/agents/>
- Herdr persistence and remote access: <https://herdr.dev/docs/persistence-remote/>
- Herdr integrations docs: <https://herdr.dev/docs/integrations/>
- Herdr comparison page: <https://herdr.dev/compare/>
- herdr-remote README: <https://github.com/dcolinmorgan/herdr-remote>

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

## Onibi vs Claude Code Remote Control

Claude Code Remote Control is Anthropic's first-party way to continue a local Claude Code session from another device. Anthropic's docs describe connecting `claude.ai/code` or the Claude mobile app to a Claude Code process running on the user's machine. The local process keeps the filesystem, MCP servers, tools, and project configuration available, while the browser or phone stays in sync with the terminal session. The connection uses outbound HTTPS through Anthropic, with no inbound ports opened on the host.

That makes CCRC the obvious fit for Claude-only users who already live in Claude Code and want the official phone/browser path. It supports CLI and VS Code entry points, QR/session URL handoff, mobile push notifications, server mode, and Claude app/web access. Current Anthropic docs describe it as a research preview, list Pro, Max, Team, and Enterprise in the requirements, and say API keys are not supported. Team and Enterprise owners may also need to enable Remote Control before users can connect.

Onibi overlaps on the local-session claim but keeps the host independent of one vendor's remote surface. It runs the session under Onibi's tmux-backed host, exposes a live xterm.js cockpit, and uses hook-level approval enforcement for supported agents. Claude Code is one supported adapter, not the whole product. The same Onibi daemon can host Codex, OpenCode, Goose, Gemini, Copilot CLI, Amp, Pi, and plain shell/TUI sessions, then route control through the browser cockpit or Telegram, Slack, Matrix, Discord, Zulip, IRC, Signal, and notify-only providers.

Core differences:

| area | Claude Code Remote Control | Onibi |
|---|---|---|
| account model | claude.ai subscription; API keys unsupported for Remote Control | local OSS daemon; no paid Onibi tier |
| agent scope | Claude Code | multi-agent plus arbitrary shell/TUI sessions |
| remote surface | claude.ai/code and Claude mobile app | paired PWA/browser cockpit plus chat/notify bridges |
| session host | existing Claude Code process | Onibi-managed tmux-backed process |
| transport | outbound HTTPS through Anthropic | LAN/hotspot/Tailscale/Cloudflare/ngrok/chat transports |
| enforcement | Claude Code permission flow | adapter hooks plus owner approval queue and runtime trust controls |

When to pick CCRC: choose Claude Code Remote Control when the workflow is Claude-only, the user already has eligible Claude access, and the priority is zero extra daemon setup with Anthropic-supported mobile/web continuity. Choose Onibi when you need one cockpit across multiple agents, open local transport choices, chat fallback, or hook enforcement that can apply outside Claude Code.

Sources checked:

- Anthropic Claude Code Remote Control: <https://code.claude.com/docs/en/remote-control>

## Onibi vs OpenCode Mobile

OpenCode Mobile is a focused phone control surface for OpenCode work. Its site describes a mobile client for inspecting running tasks, approving or redirecting changes, and keeping progress moving from a phone. The docs center the product on OpenCode sessions, workspaces, server connection health, provider defaults, changed files, and live progress. The recommended remote setup is an OpenCode server on `127.0.0.1`, protected with `OPENCODE_SERVER_USERNAME` / `OPENCODE_SERVER_PASSWORD`, exposed through a tunnel such as Cloudflare Tunnel.

That is a strong fit when OpenCode is the only agent in scope. OpenCode Mobile can optimize for one server protocol, one workspace model, and a compact task/diff/log workflow. The current site links to `alvarolorentedev/opencode-mobile`, an Apache-2.0 Expo/React Native app for connecting to an OpenCode server, so it is the closer OSS-native choice for users who want an OpenCode-first mobile app.

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
- OpenCode Mobile GitHub repository: <https://github.com/alvarolorentedev/opencode-mobile>

## Onibi vs Moshi

Moshi is a native mobile terminal for SSH/Mosh and AI-agent workflows. Its site emphasizes phone-first terminal ergonomics, tmux integration, image paste, diff viewing, voice-to-terminal, Mosh/ET resilience, lock-screen and Watch surfaces, file browsing, and SSH keys in Keychain. The pricing page says the app is free to start and that Pro unlocks features such as Mosh, multiplexer pairing, and image paste.

That makes Moshi the stronger fit when the user wants a polished native terminal app and is comfortable reaching hosts over SSH/Mosh. Onibi is a daemon plus web/chat cockpit: it creates the session, owns pairing/auth, exposes approvals, routes chat text, and can serve read-only viewers without requiring a native app install.

Honest tradeoff: pick Moshi for native mobile terminal quality, Mosh resilience, and mobile OS integrations. Pick Onibi when the core need is multi-agent approval enforcement, owner-gated web pairing, chat/notify providers, and a local daemon that hosts the terminal session itself.

Sources checked:

- Moshi homepage: <https://getmoshi.app/>
- Moshi pricing: <https://getmoshi.app/pricing>
- Moshi subscription docs: <https://getmoshi.app/docs/subscription>

## Onibi vs sshx

sshx is a fast collaborative terminal-sharing tool. Its README describes a secure web-based shared terminal, a one-command curl installer, browser collaboration, E2E encryption, nearest-server routing, automatic reconnection, and predictive echo. The same README says self-hosted deployments are not supported at the moment.

That makes sshx the better fit for quick collaborative terminal sharing or CI debugging where a browser URL is the product. Onibi is heavier: it hosts named sessions, keeps owner/viewer roles, provides approval and file/timeline/snapshot surfaces, and supports several transports instead of only terminal sharing through the sshx mesh.

Honest tradeoff: pick sshx for instant web terminal collaboration. Pick Onibi when the remote surface must understand coding-agent approvals, phone handoff, chat input, and long-lived session state.

Sources checked:

- sshx README: <https://github.com/ekzhang/sshx>

## Onibi vs ttyd

ttyd is a self-hosted web terminal for running a command over a browser. Its README lists a broad install surface, read-only-by-default browser terminals, optional writable clients, SSL, basic auth, file transfer support, Sixel output, and custom command execution.

That makes ttyd the simpler fit when the job is "put this command in a browser" and the operator owns the surrounding auth/reverse proxy story. Onibi adds the product layer ttyd intentionally does not have: pairing tokens, owner/viewer roles, managed tmux sessions, approvals, handoff, chat/notify providers, and session metadata.

Honest tradeoff: pick ttyd for a small self-hosted browser terminal. Pick Onibi when the user experience is a coding-agent cockpit rather than a generic terminal iframe.

Sources checked:

- ttyd README: <https://github.com/tsl0922/ttyd>

## Onibi vs tmate

tmate is a tmux fork for instant terminal pairing. Its README describes it as an instant pairing solution and says tmate is BSD-licensed. The natural workflow is SSH/tmux-style collaboration rather than a phone-specific approval cockpit.

That makes tmate a good fit for pair programming and direct terminal sharing with users who already understand tmux/SSH. Onibi keeps tmux under the hood but adds mobile browser controls, web pairing, owner/viewer roles, approval cards, chat providers, snapshots, and local daemon state.

Honest tradeoff: pick tmate for direct tmux-style pair access. Pick Onibi when viewers should be read-only, approvals need enforcement, or the phone cockpit should be the primary surface.

Sources checked:

- tmate README: <https://github.com/tmate-io/tmate>

## Onibi vs Upterm

Upterm is an SSH-based terminal sharing tool for pair programming, NAT/firewall traversal, remote debugging, and CI. Its README documents Homebrew/Scoop/binary/source installs, `upterm host`, shared SSH connection strings, authorized keys, GitHub/GitLab/SourceHut/Codeberg user authorization, SFTP/SCP controls, local TCP forwarding, WebSocket mode, CI debugging, and self-hosted server support.

That makes Upterm the better fit when the user wants an SSH-native sharing primitive with strong access-control knobs and optional self-hosting. Onibi is not trying to replace SSH sharing; it wraps local sessions in a phone/browser/chat product surface with approval semantics and agent-aware workflows.

Honest tradeoff: pick Upterm for SSH-native session sharing, CI access, or self-hosted relay control. Pick Onibi when the desired object is a managed coding-agent cockpit with approvals, handoff, viewers, and provider bridges.

Sources checked:

- Upterm homepage: <https://upterm.dev/>
- Upterm README: <https://github.com/owenthereal/upterm>
