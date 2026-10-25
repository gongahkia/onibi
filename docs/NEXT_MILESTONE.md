# Active milestone: complete terminal IDE workflows

This is the implementation and verification ledger for the user's continuing goal.
The whole scope remains active until the evidence below exists. Finished pieces do
not narrow the goal or justify declaring the entire milestone complete.

## Requirements and evidence

| Requirement | Acceptance evidence | Status |
| --- | --- | --- |
| Language-server compatibility | Live Go, Rust, Python and TypeScript workflows (completion, diagnostics, navigation, formatting, rename/actions as supported), plus regressions | Pending |
| SSH and terminal compatibility | Actual SSH transport, tmux over SSH, several terminal profiles, resize/input/clipboard limitations documented | Pending |
| Unicode and worst-case performance | Grapheme-correct editing/wrapping of Unicode-heavy long files; latency, allocations, redraw traffic and RSS measured against 0.2 | Pending |
| Embedded terminal | Real PTY shell and interactive full-screen programs; emulation, resize, keyboard/mouse input, scrollback, clean shutdown; bounded memory | Implemented: race tests, PTY protocol/backpressure tests, tmux shell/Vim/resize/interrupt/cleanup smoke; SSH matrix remains below |
| Debugging through DAP | Launch/attach, breakpoints, continue/pause/step, stack frames, scopes/variables, exit/error cleanup; fake and live adapter coverage | Implemented: protocol/race tests, live debugpy launch + attach/detach + abrupt cleanup, editor regression and real tmux control workflow. Other adapters remain compatibility work |
| Complete snippet language | Nested placeholders, linked stops, choices, variables/defaults and transformations against documented grammar, with editing/rebasing tests | Implemented: grammar/fuzz/race tests, nested/linked editing and undo, tmux completion workflow. Bounded regex subset and limits documented in USAGE.md |
| LSP file operations | Reviewed create/rename/delete with text edits, file conflicts/options, validation and failure handling; protocol tests | Implemented: bounded file/directory planning, rollback failure injection, editor/server-callback tests and real tmux save/undo/redo; explicit filesystem limits in USAGE.md |
| LSP server commands | User-selected commands, resolve, reviewed applyEdit callbacks, cancellation/lifecycle tests | Implemented: bidirectional subprocess/race tests and tmux resolve/review/cancel/accept/save/undo workflow |
| Nested splits | Mixed orientations, independent views, resize/close/focus and session round trips | Implemented: mixed split, divider drag, collapse, tiny terminal and session regression tests; tmux smoke |
| Git merge resolution | Conflict discovery, base/ours/theirs inspection, explicit per-hunk resolution, undo/save/stage, real conflicting repository test | Implemented: real repository and tmux Unicode zdiff3 checks, stale/dirty guards, modify/delete preview and undo; text-focused scope in USAGE.md |
| Left file tree | Expand/collapse, keyboard navigation, click-to-open, refresh/reveal, bounded discovery and no per-frame filesystem work | Implemented: indexed hierarchy, keyboard/mouse/narrow-screen tests |
| Minimap | Meaningful text overview, current viewport/position, click/drag navigation; bounded work independent of file size | Implemented: sampled background map, Unicode/navigation tests; 500k-line sample benchmark 0.569 ms |
| Left/right mouse controls | Selection/drag, pane/tree/minimap interaction, context menus and menu activation; keyboard alternatives | Implemented: selection/context/tree/minimap/preview regression tests; outer-terminal compatibility still needs matrix |
| Low overhead and distribution | Demand-driven terminal/debugger, bounded caches/output, idle behavior, measured artifact/RSS/latency, all five target builds and constrained Linux run | Current increment verified: all five builds; 11.4 MiB idle RSS; 64 MiB / 0.5 CPU Linux scratch editing. Repeat after remaining features |
| Markdown and Org rendering | Bundled rendered views, links/code/lists/tables/headings as applicable; bounded cached parsing and rendering benchmarks | Implemented: parser/fuzz/race tests, cached viewport benchmark and real tmux source/preview/split/live-refresh workflow; limits in USAGE.md |
| Code folding | Discoverable fold controls, nested folds, cursor/edit behavior, language/markup coverage, large-file tests | Pending; after current IDE tasks |
| Atomic git commits | Focused commits after relevant checks; preserve user edits | Ongoing |

## Sequence

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
