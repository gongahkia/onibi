// Package service generates and installs the OS auto-start unit: launchd
// LaunchAgent on macOS (~/Library/LaunchAgents/sh.onibi.daemon.plist), or
// systemd user unit on Linux (~/.config/systemd/user/onibi.service). User
// agents only run while the user is logged in — matches our local-only,
// laptop-open-only invariant. Phase 9.
package service
