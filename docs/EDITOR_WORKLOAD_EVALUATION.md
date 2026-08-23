# Editor workload evaluation

Zenbu is intended to evaluate editing models and editor hosts, not to claim
that a keymap with familiar names is a complete editor. This document makes
that distinction testable. A workload is an editor family with a documented
feature suite, an explicit capability matrix, and regressions for every
generic capability added after a failed evaluation.

## What an editor product needs

An editor can vary independently along these layers:

| layer | Zenbu extension point today | current boundary |
| --- | --- | --- |
| editing grammar | OCaml implementation of the public model state machine; logical input, statuses, bindings, and semantic effects | models operate on one current document and only through declarative effects |
| commands and semantic operations | built-in commands, trusted-local Lua, or capability-limited Wasm Components can contribute commands, selectors, transformations, bindings, and events | contributions cannot mutate documents outside a checked transaction |
| language-aware editing | Tree-sitter-backed syntax context and the optional language-service host | only built-in OCaml/JSON syntax registration and current-document LSP results |
| configuration | reloadable Lua configuration and local Wasm plugin discovery | Lua is trusted local code; Components use the declared capability boundary |
| workspace/view host | `zenbu.view.Layout` and host commands for vertical/horizontal splits, focus, close, and only | split leaves currently render independent viewports of the *same active buffer* |
| terminal presentation | renderer frame, style classes, viewport, terminal backend | one fixed terminal renderer; no public theme, mouse, GUI, or widget/layout API |

This is already enough to build and compare distinct **editing grammars**:
the repository has Vim-style, selection-first, and syntax-structural models.
It is not yet enough to recreate an arbitrary complete editor product, because
buffer/workspace ownership and presentation are deliberately narrower than
the grammar API.

## Evaluation rule

For each candidate editor, split its documented behavior into capabilities
instead of copying its keymap. Mark a capability as implemented only when it
has all of the following:

1. an editor-neutral API or host contract;
2. a deterministic behavior test plus an error, cancellation, or boundary
   test;
3. inspection/replay/provenance through the normal Zenbu path where it changes
   a document; and
4. at least two workloads that can use it, or a written reason why it belongs
   to one specific product adapter.

When a workload cannot be reproduced, record the missing layer before adding
code. A model-specific shortcut is not evidence that the framework supports
the capability.

## Baseline editor workloads

The feature sources are the projects' own documentation: [Vim help](https://vimhelp.org/),
[Helix keymap](https://docs.helix-editor.com/master/keymap.html),
[Kakoune documentation](https://github.com/mawww/kakoune/tree/master/doc),
[Micro documentation](https://micro-editor.github.io/), and the
[GNU Emacs manual](https://www.gnu.org/software/emacs/manual/html_node/emacs/index.html).

| workload | supported now | partial foundation | absent before a parity claim |
| --- | --- | --- | --- |
| Vim-style terminal editor | normal/insert/replace/visual grammar, operators, counts, motions, find, basic search requests, registers, undo/redo, and provenance | command palette, save/search host controls, same-buffer split views | Ex command language, macros, broad motion/text-object coverage, marks/jumps, tabs/buffers/windows, mappings as a complete compatibility layer, terminal/GUI appearance parity |
| Helix-style selection editor | selection-first model, multi-edit transactions, occurrence selection, syntax-structural selections, optional LSP completion/hover/current-document definition/rename | multiple viewport views and focus; model switching lets selection-first behavior share the same document history | buffers/pickers, language/config discovery, registers/macros, regex selection algebra, shell pipes, multi-file LSP/workspace edits, full window model, theme parity |
| Kakoune-style multiple-selection editor | explicit ordered selections, selection-first edits, syntax context, bindings and hooks | split view rendering and focused viewport | Kakoune's inclusive anchor/cursor model, selection split/rotate/merge/filter algebra, client/server sessions, shell filters, full command language, face/highlighter ecosystem |
| Micro-style terminal editor | ordinary text editing, syntax spans, trusted Lua configuration, local plugins, save/search/palette | same-buffer splits; Components and Lua can supply editing commands | mouse, interactive shell split, buffer tabs, plugin-manager/install flow, configurable terminal theme, complete keybinding/configuration surface |
| Emacs terminal product | key-addressable commands, buffers represented as kernel documents internally, configuration/plugin concepts, asynchronous language host | no equivalent beyond the generic command and host layers | buffer/window/frame system, minibuffer and completion ecosystem, major/minor mode composition, Elisp/package/process APIs, display engine, terminal appearance parity |

“Supported now” means this repository has a testable behavior, not that its
keystrokes or visual rendering exactly match the named editor. “Partial
foundation” deliberately does not count toward parity.

## Current result and next evaluations

The first host gap exposed by Helix, Kakoune, Micro, and Emacs is the ability
to show more than one view. `zenbu.view.Layout` now composes a binary vertical
or horizontal split tree without gaining access to document mutation. Each
leaf has an independent `Viewport`; input focus selects one leaf. The
terminal regression suite covers layout bounds, divider composition, cursor
translation, focus cycling, closing, and retaining only the focused view.

The next failed capability shared by all four non-Vim workloads is not another
keybinding: it is a **workspace document table**. A generic workspace must
give views stable buffer identifiers, route input and render state to the
focused buffer, coordinate save/dirty state and language clients per buffer,
and validate cross-file transactions atomically. Only then are file switching,
definition targets, buffer pickers, and real pane-to-buffer assignment
meaningful. Shell panes, mouse input, configurable presentation, and
editor-specific command languages are separate later evaluations.

## How to run the evidence

```sh
make check
dune exec test/test_m4_terminal.exe
dune exec test/test_model_runtime.exe
dune exec bin/zenbu_headless.exe -- demo
```

Use `Ctrl-P` in the terminal host and select `workspace.split.vertical`,
`workspace.split.horizontal`, `workspace.pane.next`,
`workspace.pane.close`, or `workspace.pane.only` to exercise the current
same-buffer split-view foundation. These commands are also available through
the typed `Session.handle_host` interface for headless tests and a future host
binding layer.
