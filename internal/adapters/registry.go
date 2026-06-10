// Package adapters discovers installed coding agents and routes adapter
// hook installation. Each agent sub-package handles its own settings file
// format (Claude settings.json, Codex hooks.json, OpenCode plugin, etc.)
// and records hook script sha256 in the hooks SQLite table for tamper
// detection (TODO §7.3, threat T9). Phase 2 (claude), 7 (codex/opencode/goose).
package adapters
