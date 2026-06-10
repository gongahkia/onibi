// Package store wraps modernc.org/sqlite (pure-Go, no cgo). Owns the KV
// table (tgterm pattern, see docs/tgterm-patterns.md §2) plus typed tables
// for pairing_tokens, approvals, audit, hooks, sessions. Migrations versioned.
// Phase 1.
package store
