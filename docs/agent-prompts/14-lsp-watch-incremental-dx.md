# Prompt: Tighten LSP and Watch Mode Incremental Developer Experience

You are working in Piranesi, an AppSec CLI with LSP/watch capabilities. Developer feedback should be fast, incremental, and clear without rerunning expensive whole-repo analysis unnecessarily.

Goal: improve LSP/watch mode incremental behavior and developer ergonomics.

Implementation requirements:

- Inspect `src/piranesi/lsp/`, `src/piranesi/watch/` if present, and CLI commands related to watch/LSP.
- Determine how file changes currently trigger rescans.
- Add or improve incremental invalidation so editing one file only recomputes relevant findings where feasible.
- Debounce rapid changes and avoid duplicate diagnostics.
- Ensure diagnostics include stable IDs, severity, evidence level, and actionable message.
- Add tests for changed-file invalidation or lower-level units if end-to-end LSP tests are too heavy.
- Update docs with editor/watch workflow examples.

Acceptance criteria:

- Watch/LSP output is faster and less noisy for repeated edits.
- Diagnostics are stable enough for editors to update in place.
- The feature degrades gracefully if full incremental analysis is not possible.
