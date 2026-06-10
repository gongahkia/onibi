// Package render emits PTY-buffer snapshots to Telegram: text (last N lines,
// fenced code block, ANSI stripped) or PNG (vt100 sim → image grid → PNG via
// embedded fixed-width font). Auto-switch heuristic detects TUI usage (alt-
// screen, cursor saves, multiple H sequences) and picks PNG. Phase 2 (text),
// phase 5 (PNG + auto-switch).
package render
