# IDE completeness audit

This audit guides the next development milestone. A complete editor/IDE claim
needs working workflows, data-loss protection, and measured interaction costs.
A list of language-protocol method names alone is insufficient.

Baseline comparisons: Micro's documented splits, terminal, soft wrapping and
customisation (https://micro-editor.github.io/); Helix's workspace search, symbol
navigation, references, rename and code actions
(https://docs.helix-editor.com/master/lsp.html).

## Implementation priorities

1. Correctness and trust: recovery of unsaved buffers, meaningful undo groups,
   external-change handling, and validated all-or-none workspace edits.
2. Responsive interaction: incremental terminal redraws, indexed long ASCII lines,
   bounded background work and reproducible complete-frame benchmarks.
3. Navigation: split panes, soft wrapping, project search, ignore-aware indexing,
   symbols, references and jump history.
4. Coding: hover/signatures, inline completion, snippet handling, rename, code
   actions, diagnostics navigation, line commands and configurable shortcuts.
5. Development loop: cancellable build/test tasks with navigable output, Git status
   and diffs, and returning to a project session.

## Larger remaining product investments

An embedded PTY terminal and debugger UI/DAP integration need their own terminal
emulation, input ownership and process-lifecycle work. A plugin API, persistent
on-disk undo, general encodings, syntax-aware structural editing, automatic server
installation, native Windows and mature accessibility support are separate gaps.
These must remain explicit until implemented and verified. External terminal or
command execution is not advertised as an embedded terminal/debugger.
