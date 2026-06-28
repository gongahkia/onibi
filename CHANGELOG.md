# Changelog

## v0.3.0 - Web Cockpit Pivot

### Added

- Optional Telegram transport with natural text input/output for tmux-backed shell and agent sessions.
- Telegram approval cards with approve/deny callbacks and `/edit <id> <json>`.
- Transport picker now asks for category first, then supported provider.

### Breaking

- Removed the old chat-bot transport from the active runtime; Telegram is now a v3-native optional bridge.
- `onibi up` now starts the local HTTPS web cockpit and prints a QR pair URL.

### Migration

- Re-pair devices with `onibi up` or `onibi pair`.
- Use `onibi devices` and `onibi unpair <device-id>` to manage paired browsers.

### Why

- The setup flow now avoids third-party chat setup and keeps the first run local: install hooks, start Onibi, scan QR.

### Acknowledgement

- This is the v1.5-style web cockpit pivot, intentionally revisiting the earlier "no third pivot" rule with user consent.
