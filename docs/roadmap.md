# Roadmap

This roadmap is a contributor-facing snapshot. The linked GitHub milestones are the source of truth for scope changes.

As of 2026-07-16, milestones #9-13 and #21 do not have GitHub due dates set. Expected dates are therefore shown as `TBD` instead of guessed.

| release | theme | expected date | milestone |
|---|---|---|---|
| v0.3.0 | Web cockpit pre-release: tag, fresh Homebrew install, real-iPhone smoke closure, signed and notarized Darwin builds. | TBD | [milestone #9](https://github.com/gongahkia/onibi/milestone/9) |
| v0.4.0 | Depth: Ghostty parity, MCP, Cloudflare E2E, SSH, positioning docs, and core hardening. | TBD | [milestone #10](https://github.com/gongahkia/onibi/milestone/10) |
| v0.5.0 | Historical proposal: provider work moved to deferred, capability-specific milestones; PWA polish and coverage uplift remain separately scoped. | TBD | [legacy milestone #11](https://github.com/gongahkia/onibi/milestone/11) |
| v0.6.0 | Competitive parity: multi-session dashboard, diff viewer, image paste, WireGuard, ZeroTier, and new providers. | TBD | [milestone #12](https://github.com/gongahkia/onibi/milestone/12) |
| v1.0 | Launch-ready: SLSA v3 provenance, reproducible builds, full test/security matrix, and HN launch materials. | TBD | [milestone #13](https://github.com/gongahkia/onibi/milestone/13) |
| Post-v1.1 | Deferred provider capabilities: notification, inbound text, approvals, E2EE, lifecycle, audit, and device verification. | TBD | [milestone #21](https://github.com/gongahkia/onibi/milestone/21) |

## Release Arc

v0.3.0 proves the local web cockpit: managed tmux-backed sessions, QR pairing, owner-gated approvals, and the mobile terminal baseline.

v0.4.0 deepens the core: stronger terminal fidelity, local MCP control, app-layer E2E for Cloudflare relays, SSH-hosted sessions, and sharper positioning.

The historical v0.5.0 provider-parity framing does not authorize a v1 provider surface. Current provider work is deferred to post-v1.1 capability milestones, where each capability requires its own evidence and review.

v0.6.0 moves from "usable cockpit" to competitive parity with richer session UX, media/prompt input, more transports, and more providers.

v1.0 is the launch gate: release provenance, reproducible build evidence, security/test matrices, install paths, and launch materials all have to be boring.

## Not Shipping

- iOS-native app surfaces such as Live Activities, Apple Watch, or native push controls.
- Hosted Onibi relay tier or hosted account system.
- Paid tier, subscription plan, or billing surface.
- Multi-user authorization beyond owner and read-only viewer roles.
- Chat and notify provider promotion outside explicitly enabled experimental profiles.
- Secret redaction for live terminal output.

These may be revisited later, but they are out of scope for the v0.3 to v1.0 arc.
