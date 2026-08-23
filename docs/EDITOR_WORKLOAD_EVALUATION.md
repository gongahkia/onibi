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
| commands and semantic operations | built-in commands, trusted-local Lua, or capability-limited Wasm Components can contribute commands, selectors, transformations, scoped one-to-sixteen-event bindings, and events | contributions cannot mutate documents outside a checked transaction; host-reserved controls remain unavailable |
| language-aware editing | Tree-sitter-backed syntax context and the optional language-service host | only built-in OCaml/JSON syntax registration; cross-file edits require every target to be open and saved |
| configuration | reloadable Lua configuration and local Wasm plugin discovery | Lua is trusted local code; Components use the declared capability boundary |
| workspace/view host | `zenbu.view.Layout` plus host commands to create/open/cycle buffers and split, focus, close, or retain views | local buffers have independent model/history, save, syntax, diagnostics, search, and viewport state; no project/workspace discovery, target auto-open, or global history |
| terminal presentation | renderer frame, semantic style classes, viewport, terminal backend, built-in/custom TOML themes, and basic typed pointer gestures | no runtime theme switching, GUI, or widget/layout API; pointer support is canvas-only |

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
| Vim-style terminal editor | normal/insert/replace/visual grammar, operators, counts, motions, find, basic search requests, registers, undo/redo, local buffers/views, provenance, scoped sequence bindings, typed command prompts, and generic keyboard macro recording/replay | command palette and generic host controls | Ex command language, Vim macro/register compatibility, broad motion/text-object coverage, marks/jumps, compatibility mappings, and terminal/GUI appearance parity |
| Helix-style selection editor | selection-first model, multi-edit transactions, occurrence selection, syntax-structural selections, local buffers/views, scoped sequence bindings, nested declarative modes including one initial adapter map, generic keyboard macro recording/replay, and optional LSP completion/hover/definition/rename across already-open saved buffers | buffers can be assigned to split views | picker/config discovery, named registers/macros, regex selection algebra, shell pipes, general workspace edits, full window model, and theme parity |
| Kakoune-style multiple-selection editor | explicit ordered selections, selection-first edits, syntax context, scoped bindings/hooks, nested declarative modes including one initial adapter map, generic keyboard macro recording/replay, and local buffers/views | split views render independently and focus routes input to the assigned buffer | Kakoune's inclusive anchor/cursor model, selection split/rotate/merge/filter algebra, named/register-backed macros, client/server sessions, shell filters, full command language, and face/highlighter ecosystem |
| Micro-style terminal editor | ordinary text editing, syntax spans, local buffers/views, trusted Lua configuration, local plugins, save/search/palette, terminal themes, basic click/drag selection plus wheel scrolling, and scoped sequence bindings | Components and Lua can supply editing commands | mouse clipboard/menu/multi-click parity, interactive shell split, buffer tabs, plugin-manager/install flow, runtime theme/configuration surface, and complete keybinding/configuration surface |
| Emacs terminal product | key-addressable commands, buffer-local stackable declared transient modes, local buffers in split views, a typed argument minibuffer, generic keyboard macro recording/replay, configuration/plugin concepts, and asynchronous language host | transient stack composition, not general Emacs keymap composition | buffer/window/frame system, completion ecosystem, major/minor mode composition, macro ring/naming/editing, Elisp/package/process APIs, display engine, and terminal appearance parity |

“Supported now” means this repository has a testable behavior, not that its
keystrokes or visual rendering exactly match the named editor. “Partial
foundation” deliberately does not count toward parity.

## Current result and next evaluations

The first host gaps exposed by Helix, Kakoune, Micro, and Emacs are the ability
to show more than one view and to assign those views to independent documents.
`zenbu.view.Layout` composes a binary vertical or horizontal split tree without
gaining document-mutation authority. The Session workspace now gives each
buffer a stable id and retains its model runtime/history, save state, syntax
context, LSP client handle, diagnostics, search state, and viewports. Input
focus activates the selected pane's buffer. The terminal regression suite
covers layout bounds, divider composition, cursor translation, focus cycling,
buffer creation, independent edits, pane-to-buffer rendering, file opening,
and duplicate-open rejection.

The cross-file coordination evaluation now has a bounded result. Every open
saved buffer contributes a request-time text snapshot to each language client;
the terminal waits on every client wakeup descriptor. Cross-file definitions
open/reuse a local buffer. Rename and `workspace/applyEdit` stage edits against
all target snapshots, then publish all candidate buffer runtimes only when
every target validates. Regression tests cover successful rename/apply-edit,
unopened targets, stale target snapshots, and conflicts without partial source
or target edits. This is not a general project workspace: targets are not
auto-opened, resource operations are rejected, and undo/history remains per
buffer. Project search, shell panes, full mouse interaction, configurable
presentation, and editor-specific command languages are separate evaluations.

The keymap evaluation exposed a shared missing contract. Helix documents
normal, select, picker, prompt, and nested minor modes; its `g`, `z`,
`Ctrl-w`, and `Space` modes make ordered input a product requirement rather
than a collection of flat keys. [Helix keymap](https://docs.helix-editor.com/keymap.html)
also distinguishes view movement from selection movement. Kakoune documents
scoped mappings and user modes, while Emacs documents event sequences and
global, major-mode, and minor-mode keymap precedence. See [Kakoune
commands](https://github.com/mawww/kakoune/blob/master/doc/pages/commands.asciidoc)
and [Emacs keymaps](https://www.gnu.org/software/emacs/manual/html_node/emacs/Keymaps.html).

Zenbu now evaluates this common subset through a typed, bounded input-sequence
contract. Lua and Component plugins accept one to sixteen events with ordinary
scope selection; Session holds prefixes outside editing models, rejects
same-scope prefix ambiguity at staging, records the full sequence in trace and
provenance, and consumes an unbound suffix. M4/M7/M8 regressions cover parsing,
modifier/named-key aliases, prefix persistence/cancellation, scope fallback,
atomic staging, reserved-host rejection, and cross-plugin collisions.

The next keymap result is a declared custom-mode stack for trusted Lua.
`replace`, `push`, `pop`, and `clear` transitions are staged with bindings;
the innermost map takes precedence while lower stacked maps remain available as
fallbacks. An unmatched bare `Escape` pops the innermost map, and a reload
retains the stack only when the replacement generation still declares every
id. M7 covers nested push/pop, lower-map fallback, unmatched-input containment,
Escape fallback, and invalidation on reload. This is enough to prototype
Helix-style nested prefixes and transient leader maps. It is not general Emacs
keymap composition: there are no dynamically enabled independent minor maps,
per-buffer independently composed local maps, or script-owned arbitrary state
machines; Component ABI v1 cannot currently declare modes or transitions.

The insert-mode evaluation exposed a related boundary. A custom keymap could
previously bind only fixed logical keys, so it could not express an adapter's
committed-text state. Modes can now declare `input_mode = "text"`; a typed
`<text>` binding captures one committed Unicode text or paste event and forwards
it only to a declared text command parameter. M4 proves the wildcard remains
distinct from logical keys, while M7 covers Unicode delivery, text-entry status,
Escape exit, and staging rejection for a missing or undeclared parameter. This
is an adapter-defined insert-mode primitive, not a general input-method API or
a Lua-owned mutable event loop.

Keyboard macros were the next common failure. Helix exposes experimental
record/replay commands, Kakoune records and replays keypresses through its `@`
register, and Emacs defines a keyboard macro by executing its recorded command
sequence once and replaying it later. See [Helix keymap](https://docs.helix-editor.com/master/keymap.html),
[Kakoune keys](https://github.com/mawww/kakoune/blob/master/doc/pages/keys.asciidoc),
and [Emacs keyboard macros](https://www.gnu.org/s/emacs/manual/html_node/emacs/Keyboard-Macros.html).
Zenbu now has `editor.macro.record` and `editor.macro.replay` as generic host
descriptors. A Lua adapter can bind them to `Q` and `q`; plain binding tokens
are case-sensitive so that mapping is representable. Recording executes its
input once, stores at most 1,024 keyboard/text events, omits its own controls
and pointer input, and retains one session-wide latest macro. Replay feeds the
stored events through the standard Session input dispatcher, preserving normal
transactions, hooks, history, syntax/language refresh, and error handling.
M10 regression coverage checks adapter bindings, Unicode text input, repeat
replay, empty replay rejection, state inspection, and the recording bound. It
does not claim named macro registers, persistence, editing, counts, or exact
Vim/Helix/Kakoune/Emacs macro semantics.

The next evaluation gap was argument-taking commands. The command palette and
custom bindings now collect descriptor-declared text, built-in selector, and
built-in transformation parameters before invoking the normal typed command
effect. M10 regression coverage executes `editor.apply`, rejects an invalid
selector without mutating the document, and proves a trusted Lua command
receives a prompted text value through `call.arguments`. This is a reusable
minibuffer foundation, not Vim Ex, Kakoune command language, or Emacs minibuffer
and completion parity. Component ABI v1 commands cannot yet declare parameter
metadata, so their palette entries remain parameterless.

The shared presentation result now has two small contracts. The renderer
continues to produce semantic styles, while the terminal maps those styles
through `default`, `dark`, `light`, or a validated `--theme` TOML file. It also
converts primary press/drag/release and wheel events into canvas-only host
gestures; selections retain grapheme and transaction boundaries, and an
explicitly scrolled viewport persists until keyboard input resumes following.
The regression suite checks stable built-in names, default-palette
compatibility, true-colour parsing, decoration overrides, rejection of unknown
roles, pointer decoding, grapheme-safe selection, status-row exclusion,
scrolling persistence, and pane focus. It does not claim window/widget,
mouse-clipboard, font, or GUI parity.

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
workspace. Use `workspace.buffer.new`, `workspace.buffer.open`,
`workspace.buffer.next`, and `workspace.buffer.previous` to exercise the
buffer table. These commands are also available through the typed
`Session.handle_host` interface for headless tests and a future host binding
layer.
