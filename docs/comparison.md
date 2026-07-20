# Positioning

Onibi is a personal, self-hosted remote cockpit for local managed terminal sessions. It pairs a phone PWA to the user's host, keeps the session local, and routes explicit owner approve, deny, and edit decisions through the approval queue.

On macOS, Ghostty is the supported automatic desktop handover target. Onibi does not emulate, visually match, control, or affiliate with Ghostty.

## When Onibi Fits

- You want a local tmux-backed session with a paired browser cockpit.
- You need explicit owner approval decisions while working with a supported agent adapter.
- You want PWA or Telegram control around the same local session.
- You prefer a local web cockpit rather than a vendor-hosted control plane.

Supported adapters and transport boundaries are documented in [the adapter contract](adapter-contract.md) and [transport guide](transports.md). Capability depends on the selected adapter and transport.

## Alternatives

For a vendor-native remote workflow, use that vendor's first-party documentation: [OpenAI Codex remote connections](https://learn.chatgpt.com/docs/remote-connections) or [Claude Code Remote Control](https://code.claude.com/docs/en/remote-control).

Choose a terminal multiplexer or remote-terminal tool when its session model is the product you need. Onibi does not maintain competitor feature matrices, pricing comparisons, or parity claims.
