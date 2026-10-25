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
| Layout | Up to 16 nested panes, draggable ratios, independent same-buffer cursors, Unicode soft wrapping | Minimum pane sizes constrain further splitting |
| Project navigation | Ignore-aware Git index, standalone fallback, project search, jump history | Fallback exclusions are not full `.gitignore`; explicit result/file bounds |
| Bulk changes | Multi-file replacement/rename/action preview, selective edits, all-or-none buffer staging, guarded group undo/redo | Text edits remain unsaved until requested; accepted resource operations change disk with best-effort rollback |
| Language intelligence | Debounced inline completion, nested/linked snippets, choices, variables/transforms, hover, signatures, references, definitions, implementations, types, symbols, diagnostics navigation, formatting, rename, resolved edit/command actions | Requires a capable external server; resource operations have bounded snapshots and best-effort rollback; snippet bounds and regex differences are documented |
| Familiar editing | Word movement/deletion, line duplication/deletion/movement, line comments, configurable shortcuts | Line operations need a contiguous selection; comment prefixes are extension based |
| Development loop | Cancellable build/test commands, bounded output, navigable output locations, Git status/diffs/stage/unstage | Tasks are noninteractive; embedded PTY buffers run interactive programs. DAP source debugging is available; merge UI still pending |
| Sessions | Private project session storage and explicit restoration of files, panes and positions | Up to 64 files / 16 panes; 16 MiB restore budget; recovery handles dirty text separately |

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
- Embedded PTY: bracketed paste, application cursor keys, SGR mouse reports,
  blocked-input/output cleanup, plus a real shell and Vim Unicode edit/save,
  resize and interruption inside tmux.
- File tree/minimap/context menus and nested split/session behavior have regression
  tests. Full remaining scope is tracked in [NEXT_MILESTONE.md](NEXT_MILESTONE.md).
- DAP: live debugpy 1.8.22 launch, attach/detach, breakpoint/variable/step
  workflows, abrupt connection cleanup; tmux function-key, pause and target
  termination workflow. Protocol tests cover timeouts, blocked writes, response
  ordering and output bounds.
- LSP commands: subprocess protocol tests and tmux resolve/review/cancel/accept/
  save/undo workflow; reader concurrency, callback cancellation, stale sources and
  transport shutdown have race-enabled coverage.
- LSP resources: filesystem failure injection, options/path/size bounds, conflicts,
  annotated and sequential edits, dirty/deleted buffers, saved-new-child rename,
  shutdown, and real tmux create/rename/delete/save/undo/redo coverage.
- Snippets: grammar/fuzz and stale-result/undo tests, plus a real tmux completion
  through a protocol fixture, nested linked edits, choices, variables and transforms.
- Distribution and measured resource observations are in [PERFORMANCE.md](PERFORMANCE.md).

## What should come next

1. **Compatibility and daily-use hardening.** Exercise gopls, rust-analyzer,
   pyright/pylsp and TypeScript servers; real SSH hosts; multiple terminal families;
   Unicode-heavy long lines; repositories with large numbers of files; external
   file watchers; and repeated crash/restore/save workflows. Establish reproducible
   key-latency, tail-latency and peak-memory budgets on low-resource hardware.
2. **Embedded terminal compatibility.** Real PTY buffers now support shells and Vim,
   alternate screens, colors, resize, scrollback, input ownership and cleanup.
   Continue with SSH and terminal-family coverage; see the bounded emulator
   contract in [USAGE.md](USAGE.md).
3. **Debugger compatibility.** DAP launch/attach, breakpoints, stepping, frames,
   scopes, variables, evaluation and output now have live debugpy and tmux checks.
   Broaden adapter coverage and interactive-debuggee support; advanced DAP
   features remain outside the current source-debugging workflow.
4. **Deeper language and project editing.** Git merge resolution, richer
   diagnostics and gitignore behavior without Git. Broaden live-server coverage
   of reviewed resource operations and server-command callbacks.
5. **Distribution maturity.** Signed/notarized macOS distribution, package-manager
   installation, release automation, broader CPU/OS tests, and native Windows later.

Plugin APIs, automatic server installation, persistent on-disk undo, general
encodings, syntax-aware structural editing and accessibility work remain separate
investments. These must stay explicit until implemented and verified.
