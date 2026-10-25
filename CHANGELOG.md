# Changelog

## Unreleased

- Retired the repository's public GitHub Pages documentation site. The Ghostty cockpit continues to run on the Mac over private Tailscale HTTPS.

- Added passive `onibi web doctor` diagnostics, explicit native probes, typed availability errors, and Ghostty version capabilities.
- Added a Mac readiness sheet and distinct phone states for connection, native inventory, focused capture, permissions, and session expiry. Recovery targets the failed capability and preserves pane drafts.
- Coalesced concurrent inventory polls, added bounded failure backoff, retained fresh mutation preconditions, and reduced redundant post-input observation. Rejected actions immediately reconcile readiness; lost connections stop Follow.
- Added Chromium/WebKit integration tests against the real Go HTTPS handler, embedded UI, and SQLite receipts, including crash recovery, restart revocation, and session expiry. Release archives now use the current documentation files.

- Expanded the Ghostty cockpit with native window creation, four-direction splits, named pane/tab/window closure, pane/tab titles, and allowlisted layout, scrollback, font, and fullscreen controls.
- Redesigned the phone interface with a searchable window/tab/pane navigator, amber/mint surfaces, control and key drawers, Ctrl/Alt letters and function keys, per-pane memory-only drafts, output freshness, and opt-in bounded Follow.
- Made native request queuing cancellable and prevented canceled output reads from replacing another pane's view. Added native-script fixtures, Chromium/WebKit browser regressions, and automated accessibility checks.

- Added a Mac-served, phone-friendly Ghostty interface with private Tailscale HTTPS, browser pairing, exact native targets, bounded focused-pane text, input/keys, and tab creation and confirmed closure.
- Added durable web action receipts, interrupted-delivery recovery, session revocation, and explicit output capability states.
- Retained optional Telegram control of named tmux, Zellij and Screen sessions, rendered screenshots, Codex App Server decisions, and Pi/Claude hooks.
- Fixed Telegram pairing and literal paste safeguards, target-bound kill confirmation, outbox replacement races, hook cancellation, Codex shutdown, configured terminal lifecycle handling, secret storage, upload integrity and CLI diagnostics.
- Updated patched Go dependencies and CI coverage for native releases and the mobile interface. See [the audit](docs/product-audit.md) for findings and remaining product gaps.
