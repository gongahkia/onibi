// Package auth owns the owner check and the opt-in TOTP gate. Owner check
// runs on every inbound Telegram update before any handler. TOTP is disabled
// by default; enabled via `onibi setup --enable-totp` or --paranoid.
package auth
