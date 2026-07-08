# Why Onibi Hosts Sessions Instead Of Monitoring Them

The 2026 remote-agent landscape is no longer one product category. The useful
split is architectural: some tools monitor an existing agent surface and add
mobile approval/control on top, while others host the terminal session and own
the whole path from process lifetime to phone UI.

Onibi is in the second group. It starts and tracks managed tmux-backed sessions,
serves the web cockpit, routes PTY bytes, handles approvals, and provides
handoff between phone and Mac. That is heavier than a notification layer. It is
also the reason one Onibi session can be a shell today, Claude Code tomorrow,
OpenCode after that, and a visible Ghostty/tmux attach when the laptop is open.

Branding note: Onibi is not affiliated with the [Ghostty](https://ghostty.org)
terminal emulator project; see [Branding](../branding.md).

## The Two Patterns

The monitor-and-approve pattern is attractive because it leaves the existing
terminal stack alone. If the agent already runs somewhere, the companion watches
events, forwards decisions, and maybe mirrors logs. The product can be small,
fast to adopt, and excellent at one agent's approval loop.

The host-session pattern asks for more. It has to create sessions, remember
them, proxy terminal I/O, expose a browser UI, and clean up after itself. In
return, it does not depend on a separate multiplexer or an agent-specific web
surface being the source of truth. The host owns the lifecycle.

Onibi picked host-session. It still uses tmux as the local process substrate,
but the user does not need to pre-create the mux layout. `onibi up` creates a
managed session, prints a QR, serves the phone cockpit, and lets approvals,
terminal input, snapshots, file browsing, sharing, and handoff attach to the
same session id.

## The Landscape

herdr-remote sits closest to the monitor pattern. The Herdr docs describe an
agent dashboard for coding agents, and herdr-remote adds phone access to Herdr
sessions without SSH setup.[^herdr-docs][^herdr-remote] If you already run
Herdr as your coordination layer, that is a good fit: mobile access extends the
system you are already using.

CCRC, or Claude Code Remote Control, is agent-specific. Anthropic's docs present
Remote Control as a way to connect Claude's web or mobile app to Claude Code
running on your machine, using outbound network connections and the Claude API
instead of inbound SSH.[^ccrc] Its strength is obvious: for Claude Code users,
it is close to the vendor path and can be lighter than running a separate
terminal cockpit.

Codex Remote is also vendor-native. OpenAI's Codex docs describe remote
connections as a way to connect Codex to local projects and continue work from
the web or mobile surfaces.[^codex-remote] If your work is primarily Codex and
you want the official Codex remote experience, that path should have better
product integration than any generic host can claim.

OpenCode Mobile is a different shape again. OpenCode documents a headless server
with an OpenAPI endpoint, and the OpenCode Mobile App listing describes an
unofficial mobile app that connects to an OpenCode server.[^opencode-server][^opencode-mobile]
The natural center is OpenCode itself: run the OpenCode server path and use a
mobile client optimized for that workflow.

Moshi is worth naming because it overlaps with the "phone to real terminal"
goal. Moshi's docs position it as terminal access for coding agents from a phone
or browser.[^moshi] If your main requirement is remote terminal access with less
interest in Onibi's approval queue and multi-provider routing, it may be the
simpler fit.

## Why Onibi Hosts

Onibi's premise is that a coding-agent cockpit should not be only an approval
inbox. The terminal is still the unit of work. Agents print context, ask for
commands, run TUIs, crash, recover, and sometimes need a human to type a shell
line that is not an approval decision.

Owning the session lets Onibi keep one session id across those states. The same
id drives `/ws/pty`, `/ws/events`, `/control`, `/approval`, snapshots, files,
viewer sharing, web push, chat bridges, and Mac/phone handoff. The phone sees
the live terminal, not just a summarized event stream. The Mac can attach to the
same tmux target instead of starting a parallel workspace.

The second reason is agent plurality. Onibi has adapters and hooks for multiple
agent CLIs, but the PTY path is not agent-specific. If a tool speaks terminal,
Onibi can host it. If an agent emits approvals through supported hooks, Onibi
can put those approvals in the same owner-gated queue. The result is not as
deeply integrated with any single vendor as CCRC or Codex Remote, but it avoids
choosing one vendor cockpit as the universal UI.

The third reason is transport control. Onibi can expose the same hosted session
over LAN, hotspot, Tailscale, Cloudflare, ngrok, chat, or notify-only paths.
For Cloudflare relay modes, Onibi adds app-layer E2E frames so the relay moves
ciphertext instead of terminal bytes. That is easier to reason about when the
daemon owns the web server, terminal bridge, and approval endpoints together.

## Where The Others Win

Pick herdr-remote over Onibi when Herdr is already your source of truth. If your
team's workflows already sit in Herdr's dashboard and you mainly need phone
access to those sessions, a Herdr-native remote layer is less ceremony than a
second host.

Pick CCRC over Onibi when the work is Claude Code-only and vendor-native remote
control matters more than a generic terminal cockpit. It is the better fit when
you want Claude's own remote-control flow and do not need Onibi's multi-agent
session list, chat transports, or arbitrary shell fallback.

Pick Codex Remote over Onibi when the project lives in the official Codex remote
workflow. If the goal is to start, continue, and review Codex work from OpenAI's
own surfaces, Onibi should not be treated as a replacement for that product
integration.

Pick OpenCode Mobile over Onibi when OpenCode is the only agent in scope and you
want the mobile UX built around OpenCode's server/client model. Onibi's generic
PTY hosting is useful when you switch tools; it is extra machinery if you do
not.

Pick Moshi over Onibi when you want a purpose-built phone terminal and do not
need Onibi's approval model, local CA pairing flow, chat/notify transports,
viewer sharing, file panel, or Mac/phone session handoff.

## When To Pick Onibi

Pick Onibi when the session is the product. That means you care about one local
process lifetime, one mobile terminal, one approval queue, and one handoff path
across multiple agents and plain shell work. It is also the better fit when you
want local-first hosting under your user account, with optional public relays
treated as transports rather than as the source of truth.

The tradeoff is setup. Onibi has to own certificates, QR pairing, tmux targets,
WebSocket routing, hooks, static frontend assets, and transport diagnostics. If
you only need mobile approval for one vendor agent, that can be more than the
job requires.

That tradeoff is deliberate. Onibi hosts sessions because approvals are only one
moment in the work loop. The durable object is the running terminal, and the
phone should be able to see and control that terminal directly.

[^herdr-docs]: Herdr docs, "Agents", <https://herdr.dev/docs/agents/>.
[^herdr-remote]: herdr-remote README, <https://github.com/dcolinmorgan/herdr-remote>.
[^ccrc]: Anthropic Claude Code docs, "Remote Control", <https://code.claude.com/docs/en/remote-control>.
[^codex-remote]: OpenAI Codex docs, "Remote Connections", <https://developers.openai.com/codex/remote-connections>.
[^opencode-server]: OpenCode docs, "Server", <https://opencode.ai/docs/server/>.
[^opencode-mobile]: OpenCode Mobile App listing, <https://apps.apple.com/us/app/opencode-mobile-app/id6757406313>.
[^moshi]: Moshi docs, <https://getmoshi.app/docs>.
