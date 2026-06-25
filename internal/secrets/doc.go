// Package secrets stores Onibi credentials in the OS keystore via
// 99designs/keyring (macOS Keychain, Linux Secret Service, Windows Credential
// Manager). Falls back to .env with 0600 in 0700 state dir when no keystore is
// available.
package secrets
