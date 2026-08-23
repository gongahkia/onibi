# Modal model evaluation

Zenbu treats established editors as compatibility workloads for the public
model API, not as templates for a privileged built-in editor. The current Vim
model is the first such workload; the selection-first and structural models
exercise deliberately different grammars over the same document, history,
clipboard, and extension services.

## Acceptance rule

A model capability counts only when it has all of the following:

1. a deterministic logical-input grammar and inspectable status/input rules;
2. declarative effects that pass through the shared transaction, history,
   replay, clipboard, trace, and provenance paths;
3. a behavior test that covers normal behavior and a boundary or cancellation
   case; and
4. no model-private document mutation, terminal dependency, or host-state
   pointer.

When an established-editor feature cannot meet those constraints, the result
is an API finding. Extend the generic API only when the missing capability is
editor-neutral and can be reused by a second model; otherwise keep the feature
outside the compatibility claim.

## Current workload matrix

| workload | grammar pressure | shared services exercised |
| --- | --- | --- |
| Vim compatibility model | modal normal/insert/replace/visual states, operator-motion composition, counts, find, and search direction | transactions, semantic selectors, explicit selections, clipboard, undo/redo, repeat, and host search requests |
| Selection-first model | select-then-transform, multi-selection, occurrence expansion, and slot prefixes | multi-edit transactions, clipboard, semantic repeat, and model switching |
| Structural model | syntax focus, tree navigation, structural selection, and incremental syntax availability | syntax snapshots, ordinary selections, transaction validation, and model switching |

The next editor workload should be selected by a written feature suite, not by
renaming a keymap. Its value is the reusable capability gaps it exposes—for
example multi-cursor selection composition, AST targeting, or interaction
requests—not an unbounded promise of complete emulation.
