# Completed milestone: terminal IDE workflows

Completed and audited on 2026-10-26. This ledger retains the full requested scope
and its evidence. Compatibility and resource claims are limited to the actual
workloads/platforms described in LANGUAGE_SERVERS.md, TERMINAL_COMPAT.md and
PERFORMANCE.md; future breadth is listed separately in IDE_AUDIT.md.

## Requirements and evidence

| Requirement | Acceptance evidence | Status |
| --- | --- | --- |
| Language-server compatibility | Live Go, Rust, Python and TypeScript workflows (completion, diagnostics, navigation, formatting, rename/actions as supported), plus regressions | Verified: live gopls, rust-analyzer, Pyright, pylsp and TypeScript protocol + editor workflows under race detection; fixes for unversioned diagnostics and indexing retries; versions/limits in LANGUAGE_SERVERS.md |
| SSH and terminal compatibility | Actual SSH transport, tmux over SSH, several terminal profiles, resize/input/clipboard limitations documented | Verified: isolated OpenSSH PTYs, eight TERM profiles, tmux-over-SSH, SGR left/right mouse, clipboard wire/fallback, nested shell/Vim resize and cleanup; scope/limits in TERMINAL_COMPAT.md |
| Unicode and worst-case performance | Grapheme-correct editing/wrapping of Unicode-heavy long files; latency, allocations, redraw traffic and RSS measured against 0.2 | Verified: bounded layout/cursor/word traversal; edit/undo/chunk/fuzz/race coverage; warm 1 MiB wrapped redraw ~1.3 ms versus 6.5 s before; cold, long-word, many-line and oversized-cluster measurements; native and constrained Linux Unicode PTYs; comparison with 0.2 in PERFORMANCE.md |
| Embedded terminal | Real PTY shell and interactive full-screen programs; emulation, resize, keyboard/mouse input, scrollback, clean shutdown; bounded memory | Verified: race tests, PTY protocol/backpressure tests, tmux shell/Vim/resize/interrupt/cleanup smoke, plus real SSH → tmux → Shack → shell/Vim checks |
| Debugging through DAP | Launch/attach, breakpoints, continue/pause/step, stack frames, scopes/variables, exit/error cleanup; fake and live adapter coverage | Verified: protocol/race tests, live debugpy launch + attach/detach + abrupt cleanup, editor regression and real tmux control workflow. Other adapters remain compatibility work |
| Complete snippet language | Nested placeholders, linked stops, choices, variables/defaults and transformations against documented grammar, with editing/rebasing tests | Verified: grammar/fuzz/race tests, nested/linked editing and undo, tmux completion workflow. Bounded regex subset and limits documented in USAGE.md |
| LSP file operations | Reviewed create/rename/delete with text edits, file conflicts/options, validation and failure handling; protocol tests | Verified: bounded file/directory planning, rollback failure injection, editor/server-callback tests and real tmux save/undo/redo; explicit filesystem limits in USAGE.md |
| LSP server commands | User-selected commands, resolve, reviewed applyEdit callbacks, cancellation/lifecycle tests | Verified: bidirectional subprocess/race tests and tmux resolve/review/cancel/accept/save/undo workflow |
| Nested splits | Mixed orientations, independent views, resize/close/focus and session round trips | Verified: mixed split, divider drag, collapse, tiny terminal and session regression tests; tmux smoke |
| Git merge resolution | Conflict discovery, base/ours/theirs inspection, explicit per-hunk resolution, undo/save/stage, real conflicting repository test | Verified: real repository and tmux Unicode zdiff3 checks, stale/dirty guards, modify/delete preview and undo; text-focused scope in USAGE.md |
| Left file tree | Expand/collapse, keyboard navigation, click-to-open, refresh/reveal, bounded discovery and no per-frame filesystem work | Verified: indexed hierarchy, keyboard/mouse/narrow-screen tests |
| Minimap | Meaningful text overview, current viewport/position, click/drag navigation; bounded work independent of file size | Verified: sampled background map, Unicode/navigation tests; 500k-line sample benchmark 0.569 ms |
| Left/right mouse controls | Selection/drag, pane/tree/minimap interaction, context menus and menu activation; keyboard alternatives | Verified: selection/context/tree/minimap/preview regression tests; real SSH SGR profile matrix and tmux forwarding verified |
| Low overhead and distribution | Demand-driven terminal/debugger, bounded caches/output, idle behavior, measured artifact/RSS/latency, all five target builds and constrained Linux run | Verified: all five final builds and archive contents/checksums; 14.1 MiB native first-frame RSS; idle core/embedded-shell samples; basic and 1 MiB Unicode editing under 64 MiB / 0.5 CPU in Linux scratch; precise costs and limits in PERFORMANCE.md |
| Markdown and Org rendering | Bundled rendered views, links/code/lists/tables/headings as applicable; bounded cached parsing and rendering benchmarks | Verified: parser/fuzz/race tests, cached viewport benchmark and real tmux source/preview/split/live-refresh workflow; limits in USAGE.md |
| Code folding | Discoverable fold controls, nested folds, cursor/edit behavior, language/markup coverage, large-file tests | Verified: bounded scanner/fuzz tests, independent-pane/nested/edit/reveal tests, 100k-line Unicode projection and real tmux workflow; heuristic coverage/limits in USAGE.md |
| Atomic git commits | Focused commits after relevant checks; preserve user edits | Verified: separate feature, compatibility, Unicode layout, word traversal, cache and release-verification commits; removed README preserved |

## Completed sequence

1. Introduce nested layout and shared input routing, then the tree/minimap/context
   menus. These are also the layout/input foundation for terminal and debug views.
2. Add real PTY emulation and DAP workflows, instantiated only when requested.
3. Complete snippets, LSP resource/command interoperability and merge resolution.
4. Exercise actual servers, SSH/terminal variants and large Unicode workloads;
   fix issues and publish measured costs and practical limits.

UI defaults: show a compact tree on wide terminals when opening a project
explicitly; keep it togglable. Make the minimap optional and sampled/cached, without
another syntax pass. Narrow terminals retain usable text space. No terminal,
debug adapter, build tool or language server is a mandatory editing dependency.


## Final verification commands

- `make check`: formatting, vet and race tests across all packages.
- `python3 scripts/lsp_live.py --race`: actual gopls, rust-analyzer, Pyright,
  pylsp and TypeScript protocol/editor workflows.
- `SHACK_DEBUGPY_PYTHON=/path/to/venv/bin/python go test -race ./internal/dap ./internal/editor -run TestLiveDebug`:
  live launch, attach/detach, stepping, scopes/variables and transport cleanup.
- `scripts/*_smoke.py`: basic PTY, recovery, tmux, IDE, terminal, debug, snippets,
  server commands/resources, Git merges, markup, folding, Unicode and SSH scripts
  all passed against the native executable. Debug requires the optional interpreter;
  SSH requires OpenSSH/tmux/Vim. Tests use isolated temporary files/sockets.
- Unicode layout fuzzing and simulated viewport/index/word benchmarks, plus real
  local/SSH output measurements: results and reproduction commands in PERFORMANCE.md.
- `make release && python3 scripts/verify_release.py`: Linux amd64/arm64/ARMv7 and
  macOS amd64/arm64 executable archives, all documentation and checksums.
- `python3 scripts/linux_smoke.py`: actual Linux amd64 basic and 1 MiB Unicode
  editing in an isolated 64 MiB / 0.5 CPU scratch container without a runtime.

Tests validate the documented scope: bounded snippet transforms, text-focused Git
conflicts, heuristic folds, optional external language/debug servers and
best-effort multi-file filesystem rollback. They do not imply universal support
for every adapter, JavaScript regex feature, terminal font or hardware platform.
