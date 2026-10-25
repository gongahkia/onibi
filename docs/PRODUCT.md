# Shack product contract

Name: **Shack**. Description: **Atomweight Terminal Text Editor IDE with Sensible Day 1 Defaults**.

## First release

- macOS and Linux, x86-64 and ARM64; SSH and tmux are first-class test cases.
- Familiar, nonmodal controls; mouse selection; file navigation; visible help and
  searchable commands; multiple buffers and multiple selections.
- Literal and regex search, selectable replacement previews, and transformations
  applied as undoable operations, including reviewed multi-file replacements.
- Nested split panes, optional file tree/minimap, context menus, soft wrapping,
  configurable controls, recovery and project sessions.
- On-demand embedded PTY terminals with interactive programs and bounded history.
- Bundled syntax highlighting. Optional completion, diagnostics, definition, and
  formatting through detected language servers; documentation, symbols, references,
  rename and edit-based code actions. No silent dependency downloads.
- Explicit cancellable build/test tasks and optional Git status/diff/staging.
- Optional DAP launch/attach, source breakpoints, stepping, frames and variables.
- Core editing works offline without configuration or a language server.
- Portable executable distribution, with CGO disabled. Linux builds require no
  libc shared library; macOS uses the operating system libraries.
- UTF-8 text, preserved LF/CRLF, grapheme-aware movement, conflict-detecting saves.

## Meaning of atomweight

Measure artifact size, idle memory, startup, ordinary edits, indexed navigation,
large pastes, and bulk replacement. Publish the environment with results. No
unmeasured claim of beating Micro. Avoid polling when idle; bound optional work
and keep language servers away from the interaction thread. Large documents
retain editing when language intelligence or highlighting is disabled.

## Scope boundaries

Native Windows, a plugin marketplace, editing-model DSLs and legacy Zenbu API
compatibility are deferred. Markdown/Org rendering, folding and deeper
language interoperability are active work tracked in [NEXT_MILESTONE.md](NEXT_MILESTONE.md). Current Git
commands do not provide merge resolution. Workspace edits are atomic in memory;
saving several files is sequential. LSP resource/command actions remain unsupported.
Snippets support nested placeholders, choices, variables and bounded transforms;
regex compatibility limits are documented in USAGE.md. See [IDE_AUDIT.md](IDE_AUDIT.md) for the capability matrix.

Clipboard protocols depend on the terminal and multiplexer. Report the path used;
terminal paste remains available when clipboard querying is disabled. Language
servers are optional external tools with their own resource and runtime needs.
