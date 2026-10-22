# Changelog

## Unreleased

- Added a Mac-served, phone-friendly Ghostty interface with private Tailscale HTTPS, browser pairing, exact native targets, bounded focused-pane text, input/keys, and tab creation and confirmed closure.
- Added durable web action receipts, interrupted-delivery recovery, session revocation, and explicit output capability states.
- Retained optional Telegram control of named tmux, Zellij and Screen sessions, rendered screenshots, Codex App Server decisions, and Pi/Claude hooks.
- Fixed Telegram pairing and literal paste safeguards, target-bound kill confirmation, outbox replacement races, hook cancellation, Codex shutdown, configured terminal lifecycle handling, secret storage, upload integrity and CLI diagnostics.
- Updated patched Go dependencies and CI coverage for native releases and the mobile interface. See [the audit](docs/product-audit.md) for findings and remaining product gaps.
