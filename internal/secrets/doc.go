// Package secrets stores bot tokens and optional TOTP secrets in the OS
// keystore via 99designs/keyring (macOS Keychain, Linux Secret Service,
// Windows Credential Manager). Falls back to .env with 0600 in 0700 state
// dir when no keystore is available. Token never in argv, never in logs.
// Phase 1. See TODO §7.3 enforcement bullets.
package secrets
