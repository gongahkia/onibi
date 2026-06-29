# Changelog

## v0.3.0 - Web Cockpit Pivot

### Added

- Optional Telegram transport with natural text input/output for tmux-backed shell and agent sessions.
- Telegram approval cards with approve/deny callbacks and `/edit <id> <json>`.
- Transport picker now asks for category first, then supported provider.
- `onibi status` now surfaces update-check state and next actions.
- `onibi status` caches update checks for 6 hours; use `--refresh-update` to bypass the cache.
- `onibi doctor --release` bundles update, after-upgrade, Telegram optional, hook, cert, service, and runtime checks.
- `onibi uninstall --dry-run --json` emits a machine-readable destructive-action plan.

### Fixed

- `doctor --fix` no longer installs hooks on a fresh machine; it only repairs existing recorded Onibi-managed hooks.
- `onibi uninstall` now requires confirmation or `--yes` before destructive service, hook, state, or secret removal.
- Update-check now prints source-specific commands for source, Homebrew cask, and release-archive installs.

### Breaking

- Removed the old Telegram-first setup path from default onboarding; Telegram is now a v3-native optional bridge.
- `onibi up` now starts the local HTTPS web cockpit and prints a QR pair URL.

### Migration

- Re-pair devices with `onibi up` or `onibi pair`.
- Use `onibi devices` and `onibi unpair <device-id>` to manage paired browsers.

### Why

- The setup flow now avoids third-party chat setup and keeps the first run local: install hooks, start Onibi, scan QR.

### Acknowledgement

- This is the v1.5-style web cockpit pivot, intentionally revisiting the earlier "no third pivot" rule with user consent.
