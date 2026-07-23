# Changelog

## v3.0.0 - Command Center

### Breaking

- Replaced the v2 command surface with `start`, `phone`, `session`, `agent`, `telegram`, `workspace`, `transport`, and `system`.
- Removed Internet Relay Chat support and all experimental provider/workspace gates.
- Telegram is now a first-class beta surface; live-device certification remains [Unverified].

### Added

- Added an adaptive terminal command center, bordered status panels, and numbered TTY workflow navigation.
- `onibi start` runs guided onboarding on a fresh interactive install.

## v0.5.0 - Security Hardening

### Breaking

- Removed the Cloudflare plaintext relay bypass; Cloudflare transports now require app-layer E2E.

### Migration

- Remove any local scripts that pass the former unsafe Cloudflare no-E2E flag. There is no replacement; use the default E2E pairing flow.

## v0.3.0 - Web Cockpit Pivot

### Added

- Optional Telegram transport with natural text input/output for tmux-backed shell and agent sessions.
- Telegram approval cards with approve/deny callbacks and `/edit <id> <json>`.
- Transport picker now asks for category first, then supported provider.
- `onibi system doctor --release` bundles after-upgrade, Telegram optional, hook, cert, service, and runtime checks.
- `onibi system uninstall --dry-run --json` emits a machine-readable destructive-action plan.

### Fixed

- `doctor --fix` no longer installs hooks on a fresh machine; it only repairs existing recorded Onibi-managed hooks.
- `onibi system uninstall` now requires confirmation or `--yes` before destructive service, hook, state, or secret removal.

### Breaking

- Removed the old Telegram-first setup path from default onboarding; Telegram is now a v3-native optional bridge.
- `onibi start` now starts the local HTTPS web cockpit and prints a QR pair URL.

### Migration

- Re-pair devices with `onibi start` or `onibi phone pair`.
- Use `onibi phone list` and `onibi phone remove <device-id>` to manage paired browsers.

### Why

- The setup flow now avoids third-party chat setup and keeps the first run local: install hooks, start Onibi, scan QR.

### Acknowledgement

- This is the v1.5-style web cockpit pivot, intentionally revisiting the earlier "no third pivot" rule with user consent.
