# v0.3.0 Launch Draft

Status: draft for optional HN-style post after `v0.3.0` is tagged and published.

## Title

Show HN: Onibi v0.3 - a web cockpit for local coding-agent sessions

## Link

https://github.com/gongahkia/onibi

## Text

Onibi runs local shells and coding agents in managed tmux-backed sessions, then gives a phone a live terminal and approval cockpit over HTTPS/WebSocket. The practical use case is keeping a real Mac/Linux terminal session alive while approving tool calls, interrupting runs, or typing into `vim` from mobile Safari.

v0.3.0 is the web-cockpit pivot. First run no longer starts by asking for a Telegram bot. It starts local, installs hooks, prints a QR pair URL, and lets the user choose a transport category only when they need one. LAN/hotspot remains the default path; Tailscale, Cloudflare, ngrok, chat, and notify-only providers are explicit choices.

The phone cockpit has the pieces that made this worth a release: live xterm.js PTY streaming, `xterm-ghostty` terminfo, soft keys for mobile terminal work, `MAC`/`PHONE` handover, owner-gated approval cards, device management, and restart-aware status checks.

Branding note: Onibi is not affiliated with the [Ghostty](https://ghostty.org) terminal emulator project; see [Branding](branding.md).

Telegram did not disappear. It became an optional bridge. `onibi telegram setup` stores the BotFather token, `onibi up --transport=telegram` starts chat-native text control, and approval cards support approve/deny/edit callbacks. That keeps chat useful without making third-party setup the default onboarding path.

The release also tightens operational surfaces: `onibi doctor --release` bundles upgrade/hook/cert/service/runtime checks, `doctor --fix` only repairs existing Onibi-managed hooks, and `uninstall --dry-run --json` prints a machine-readable destructive-action plan before anything is removed.

This is a draft announcement, not a publication note. Release/signing/Homebrew/device validation should stay in the release checklist until those gates pass.

## First Comment

Notes:

- Re-pair existing devices with `onibi up` or `onibi pair`.
- Manage paired browsers with `onibi devices` and `onibi unpair <device-id>`.
- Telegram users should run `onibi telegram setup`, then `onibi up --transport=telegram`.
- The default install story is intentionally local-first: install hooks, start Onibi, scan QR.
- This release intentionally revisits the earlier "no third pivot" rule with user consent.
