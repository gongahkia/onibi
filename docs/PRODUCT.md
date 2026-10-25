# Shack product contract

Name: **Shack**. Description: **Atomweight Terminal Text Editor IDE with Sensible Day 1 Defaults**.

## First release

- macOS and Linux, x86-64 and ARM64; SSH and tmux are first-class test cases.
- Familiar, nonmodal controls; mouse selection; file navigation; visible help and
  searchable commands; multiple buffers and multiple selections.
- Literal and regex search, selectable replacement previews, and transformations
  applied as one undoable operation.
- Bundled syntax highlighting. Optional completion, diagnostics, definition, and
  formatting through detected language servers; no silent dependency downloads.
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

Native Windows, debugger integration, embedded terminals, a Git UI, a plugin
marketplace, editing-model DSLs, and legacy Zenbu API compatibility are deferred.
The initial replacement preview is scoped to the active buffer. Project-wide
refactoring needs separate file-conflict and undo design.

Clipboard protocols depend on the terminal and multiplexer. Report the path used;
terminal paste remains available when clipboard querying is disabled. Language
servers are optional external tools with their own resource and runtime needs.
