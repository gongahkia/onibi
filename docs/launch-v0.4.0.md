# v0.4.0 Launch Draft

Status: draft for optional HN-style post after `v0.4.0` is tagged and published.

## Title

Show HN: Onibi v0.4 - a phone cockpit for local coding agents

## Link

https://github.com/gongahkia/onibi

## Text

The v0.4 flagship is MCP recursion with hook-based approval enforcement. Onibi now lets local MCP clients inspect sessions, tail scrubbed context, write to live PTYs, and decide low/medium-risk approvals through [`onibi mcp`](mcp.md), while the same approval queue still enforces owner-gated hooks before risky tools run.

That is the safety story I wanted before adding more remote surfaces. MCP clients talk to a local stdio process, daemon-backed actions go through same-UID Unix socket checks, and high-risk approval paths stay out of automatic approve/edit flows. The phone cockpit remains the owner control plane instead of becoming a bearer-token web API.

Also landed in v0.4:

- Terminal fidelity: `xterm-ghostty` terminfo, JetBrains Mono, Sixel/IIP rendering, and Kitty graphics transcoding.
- Cloudflare relay hardening: Quick Tunnel moved from public tunnel to app-layer E2E for terminal bytes, event payloads, control requests, approval decisions, and input; see [`docs/SPEC-e2e.md`](SPEC-e2e.md).
- Remote hosts: [`onibi up --ssh`](ssh-transport.md) can bootstrap a user service on an SSH host, start loopback-only Onibi remotely, and keep phone pairing through a local SSH tunnel.
- Positioning and hardening: comparison docs, threat model coverage, adapter registry work, and release checks now make remaining risk easier to audit.

Branding note: Onibi is not affiliated with the [Ghostty](https://ghostty.org) terminal emulator project; see [Branding](branding.md).

I am posting this as a release draft, not a claim that every external smoke has passed. The remaining release checklist is explicit about signed updates, Homebrew/curl installs, fresh-machine validation, and physical-device tests such as the open real-iPhone Cloudflare, diff-card, and Web Push smokes tracked in [#80](https://github.com/gongahkia/onibi/issues/80), [#81](https://github.com/gongahkia/onibi/issues/81), and [#82](https://github.com/gongahkia/onibi/issues/82).

## First Comment

Technical notes:

- The default path remains the web cockpit. Chat transports such as Telegram are opt-in via setup plus `onibi up --transport=telegram`.
- Cloudflare Quick is treated as an untrusted relay; the app payload is encrypted above the tunnel.
- MCP approval decisions refuse high-risk approve/edit paths.
- The SSH transport uses user services and known-host verification; it does not install root services on the remote host.
- The remote SSH binary-size/RSS gates are intentionally not hidden behind release prose. If they fail, the release checklist should say so.
