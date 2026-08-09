# ADR 0010: runtime clipboard slots and semantic repeat

## Context

Yank/paste, history navigation, and dot repeat cannot be credible if copied
text, history, or repeat records are private mutable fields inside the Vim-style
grammar. A selection-first model can legitimately use the same copied text and
semantic edit history.

## Decision

Add immutable clipboard slots to the model runtime. Slots store validated UTF-8
text and generic characterwise/linewise shape. Add declarative effects to copy
a selector, paste a slot, undo, redo, and repeat the latest textual semantic
intent batch. Models can observe slots through immutable context, but only the
runtime interprets effects and changes services.

M3 records one transaction per committed text input. Repeat therefore supports
completed semantic actions such as delete and the last committed insert input;
it intentionally does not attempt full insert-session or paste repeat.

## Alternatives considered

- Store Vim registers and undo state in Vim model state: prevents fair reuse by
  the selection-first model and obscures editor semantics.
- Add a global mutable clipboard: harms deterministic tests and future model
  isolation.
- Replay raw input for dot: depends on the model grammar instead of semantics.

## Consequences

Vim-style `"a`, `yy`, and `p` are grammar over a shared service, not privileged
core operations. The service remains deliberately narrow: no system clipboard,
register expressions, macros, or full repeat grouping are implied.
