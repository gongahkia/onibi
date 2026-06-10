// Package auth owns the owner check (chokepoint middleware — see TODO §11
// rule 13) and the opt-in TOTP gate. Owner check runs on every inbound
// Telegram update before any handler. TOTP is disabled by default; enabled
// via `onibi setup --enable-totp` or --paranoid. Phase 1.
package auth
