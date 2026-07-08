# v0.4.0 Launch Draft

Status: draft for optional HN-style post after `v0.4.0` is tagged and published.

## Title

Show HN: Onibi v0.4 - a phone cockpit for local coding agents

## Link

https://github.com/gongahkia/onibi

## Text

Onibi is a local daemon that lets a phone drive a real tmux-backed shell or coding-agent session. It serves a live web cockpit over HTTPS/WebSocket, keeps approvals owner-gated, and lets the same session move between mobile Safari, chat transports, MCP clients, and a visible Mac terminal.

The v0.4 theme is depth over novelty. The terminal path now tries to be recognizably Ghostty-shaped: `xterm-ghostty` terminfo, a Ghostty-default theme, JetBrains Mono, Sixel/IIP rendering, Kitty graphics transcoding, and parity smokes against native Ghostty captures. This is not pixel-perfect emulation, but it makes the browser cockpit feel like the same terminal instead of a generic web console.

Branding note: Onibi is not affiliated with the [Ghostty](https://ghostty.org) terminal emulator project; see [Branding](branding.md).

The second thread is MCP recursion. `onibi mcp` exposes local stdio tools for listing sessions, peeking/tailing scrubbed output, writing to live PTYs, requesting approvals, and deciding low/medium-risk approvals. It deliberately stays local: stdio plus same-UID Unix socket checks, no remote MCP listener, and no bearer-token surface.

Cloudflare support moved from "public tunnel" to "public tunnel with app-layer E2E." Quick Tunnel URLs carry only the relay key fragment to the browser, while terminal bytes, event payloads, control requests, approval decisions, and user input are encrypted with per-pair HKDF + AES-GCM before crossing the relay. Named Tunnels now have explicit setup/status/disable commands and can fetch tunnel tokens through the Cloudflare API without putting tokens in process args.

The SSH transport work is about making remote terminals boring. `onibi up --ssh user@host` detects the remote platform, uploads matching `onibi` and `onibi-notify` binaries, installs a user service, starts loopback-only Onibi on the remote host, then opens a local SSH tunnel so the phone still pairs through the laptop. The small-server resource gate is intentionally strict and still tracked separately from the physical smoke.

I am posting this as a release draft, not a claim that every external smoke has passed. The remaining release checklist is explicit about signed updates, Homebrew/curl installs, fresh-machine validation, and physical-device tests.

## First Comment

Technical notes:

- The default path remains the web cockpit. Chat transports such as Telegram are opt-in via setup plus `onibi up --transport=telegram`.
- Cloudflare Quick is treated as an untrusted relay; the app payload is encrypted above the tunnel.
- MCP approval decisions refuse high-risk approve/edit paths.
- The SSH transport uses user services and known-host verification; it does not install root services on the remote host.
- The remote SSH binary-size/RSS gates are intentionally not hidden behind release prose. If they fail, the release checklist should say so.
