# IDE capability audit — 0.2 development milestone

A fully featured CLI IDE needs working workflows, protection against lost text,
and measured interaction costs. A list of protocol method names is insufficient.
The development milestone closes the largest everyday gaps; Shack is not yet a
mature replacement for every Micro, Vim or IDE workflow.

The baseline was informed by [Micro's feature documentation](https://micro-editor.github.io/),
[Helix's language-server documentation](https://docs.helix-editor.com/master/lsp.html),
and the [LSP specification](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/).
Git discovery follows [`git ls-files`](https://git-scm.com/docs/git-ls-files).

## Implemented in this milestone

| Area | Working behavior | Important boundary |
| --- | --- | --- |
| Trust | Grouped typing undo, private crash recovery, disk-change warnings, undoable reload, complete Save All/close flow | Recovery is bounded, delayed and not persistent undo |
| Interaction | Incremental terminal updates; indexed ASCII positioning and visible-line slicing | Extremely long mixed-Unicode lines can still scan/allocate heavily |
| Layout | Up to four panes, independent same-buffer cursors, Unicode soft wrapping | Equal panes in one shared orientation, no nested split tree |
| Project navigation | Ignore-aware Git index, standalone fallback, project search, jump history | Fallback exclusions are not full `.gitignore`; explicit result/file bounds |
| Bulk changes | Multi-file replacement/rename/action preview, selective edits, all-or-none buffer staging, guarded group undo/redo | Saving is sequential; edits remain unsaved until requested |
| Language intelligence | Debounced inline completion, basic linked snippets, hover, signatures, references, definitions, implementations, types, symbols, diagnostics navigation, formatting, rename, edit-based actions | Requires a capable external server; complex snippets, resource operations and server-command actions remain unsupported |
| Familiar editing | Word movement/deletion, line duplication/deletion/movement, line comments, configurable shortcuts | Line operations need a contiguous selection; comment prefixes are extension based |
| Development loop | Cancellable build/test commands, bounded output, navigable output locations, Git status/diffs/stage/unstage | No interactive task input, debugger, terminal emulator or merge-resolution UI |
| Sessions | Private project session storage and explicit restoration of files, panes and positions | Up to 64 files / four panes; 16 MiB restore budget; recovery handles dirty text separately |

## Validation

- `make check`: formatting, vet and race-enabled tests.
- Live clangd: completion, definition, formatting, hover, references, document
  symbols, signature help, and rename.
- Real PTY: normal editing, selective replace, undo/redo/save, plus SIGKILL recovery
  and preservation of the original until explicit save.
- Isolated tmux: Unicode paste, splits, wrapping, project replacement, group undo/
  redo, Save All, task execution and output navigation.
- Regression tests cover symlinked project roots, disk-conflicting previews,
  same-file pane rebasing, stale language sessions and late closes, snippet ranges,
  session restoration, and subprocess cancellation/output limits.
- Distribution and measured resource observations are in [PERFORMANCE.md](PERFORMANCE.md).

## What should come next

1. **Compatibility and daily-use hardening.** Exercise gopls, rust-analyzer,
   pyright/pylsp and TypeScript servers; real SSH hosts; multiple terminal families;
   Unicode-heavy long lines; repositories with large numbers of files; external
   file watchers; and repeated crash/restore/save workflows. Establish reproducible
   key-latency, tail-latency and peak-memory budgets on low-resource hardware.
2. **A real embedded terminal.** PTY lifecycle, terminal emulation, resize handling,
   input ownership, scrollback and SSH/tmux interoperability need a cohesive design.
   The current output buffer is not an interactive terminal.
3. **Debugger integration.** A DAP client and UI for launch/attach, breakpoints,
   stepping, stack frames and variables. Build/test commands are not debugging.
4. **Deeper language and project editing.** Complete snippet grammar, server-command
   actions with reviewed `workspace/applyEdit`, LSP resource operations, richer
   diagnostics, gitignore behavior without Git, nested splits and merge resolution.
5. **Distribution maturity.** Signed/notarized macOS distribution, package-manager
   installation, release automation, broader CPU/OS tests, and native Windows later.

Plugin APIs, automatic server installation, persistent on-disk undo, general
encodings, syntax-aware structural editing and accessibility work remain separate
investments. These must stay explicit until implemented and verified.
