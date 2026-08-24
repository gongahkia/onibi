# View-owned folding

Folding is a `zenbu.app.Session` projection over complete source lines. It is
not a document operation: source bytes, selections, transactions, history,
editing-model state, and scripting APIs retain their ordinary source-byte
semantics.

The command palette exposes three host-only commands:

- `view.fold.selection` creates a manual fold from a primary selection that
  covers two or more source lines. Repeating it for the exact same range
  removes that range. With a one-line primary selection inside a collapsed
  range, it removes the innermost containing fold.
- `view.fold.syntax` uses the smallest named node containing the primary
  selection. The snapshot must have the current document id/version and no
  root or selected-node error; otherwise it rejects without changing the view.
- `view.fold.clear` removes every fold for the focused pane and buffer.

Manual and syntax-derived folds retain separate `Manual` and `Syntax` sources
inside the view layer. Neither source is a model, Lua, Component, or renderer
callback capability. A range is normalized to the source lines containing its
endpoints, leaves the first line as its visible header, and hides every later
line through its final line. Ranges may be strictly nested. Equal ranges and
partial overlaps are rejected, so a projection has one unambiguous visible
header for each hidden run.

## State lifetime

Fold state is keyed by `(pane, buffer)` and the immutable document version.
Splitting a pane copies its current valid folds; different panes may then fold
the same buffer differently. A content edit invalidates every affected
version-bound fold instead of guessing a rebase. Rendering removes stale
states. Layout save/restore deliberately omits folds, as do buffer/model
serialization paths.

## Projection and input

The renderer projects visible source lines before viewport reconciliation. A
fold header keeps its source line number and receives a dim `… N lines folded`
suffix. Viewport scroll, page, and center operate on projected visible rows;
horizontal columns remain the header's source display columns. A primary
selection in hidden content leaves status-line source coordinates unchanged
but draws the physical cursor at the end of the visible fold header. It does
not silently change or unfold the selection.

Pointer rows resolve through the same projection. A row below a fold maps to
its original source line. Clicking its header, including the suffix, maps to
that header's source byte range (the suffix maps to the header end); hidden
rows have no pointer target. Search ranges, diagnostics, and syntax spans
remain source-byte ranges. They style visible header text normally, but ranges
wholly inside hidden lines have no extra marker and become visible only after
unfolding. Search or diagnostic navigation may therefore move the source
selection into a fold while the cursor remains projected to its header.

This is deliberately a compact common denominator. It has no fold-expression
language, persistence, automatic rebase, renderer callbacks, virtual lines,
or product-specific fold UI/key grammar.
