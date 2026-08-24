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
| editing grammar | OCaml models or one trusted-local Lua `zenbu.model` declaration per configuration generation; logical input, explicit serialisable state, statuses, semantic effects, and optional versioned reload migration | models operate on one current document and only through declarative effects; migration passes only bounded data values and cannot receive mutable editor/runtime objects |
| commands and semantic operations | built-in commands, trusted-local Lua, or capability-limited Wasm Components can contribute commands, selectors, transformations, scoped one-to-sixteen-event bindings, and events; trusted Lua may additionally declare buffer-local data-only binding layers and request a bounded argument-vector selection filter or an inspectable background program | contributions cannot mutate documents outside a checked transaction; process actions cannot become a shell, terminal, or general job API |
| language-aware editing | Tree-sitter-backed syntax context and the optional language-service host | host-staged OCaml/JSON built-in grammar bundles only; no grammar download/native loading; cross-file edits require every target to be open and saved |
| configuration | reloadable Lua configuration and local Wasm plugin discovery | Lua is trusted local code; Components use the declared capability boundary |
| workspace/view host | `zenbu.view.Layout` plus host commands to create/open/cycle buffers; split, focus, close, retain, grow/shrink the nearest matching divider, drag an exact visible divider, balance views, save/restore validated local JSON layouts, open a file through an explicit-root host picker, search literal text through that same bounded root, and report external local-file changes; scroll/project by visible line/page; center the focused view; and add version-bound manual or syntax-derived folds; ordered selections are retained per `(pane, buffer)` and rebase through forward local history | local buffers have independent model/history, save, syntax, diagnostics, search, viewport, and view-only fold state; layouts contain only clean file-backed host state, picker/search candidates remain host-owned relative paths, and watcher events retain both clean and dirty buffers unchanged; no target auto-open, global history, arbitrary view widgets, cross-machine sync, automatic reload/merge, fold expressions/persistence, or per-product layout policy |
| terminal presentation | renderer frame, semantic style classes, viewport, terminal backend, host-switchable built-in/custom TOML themes, pure line-number/status-row profiles, an optional bounded host buffer line, bounded snapshot-bound virtual rows/trailing annotations, and typed canvas-selection plus exact-divider pointer gestures | no GUI or widget/layout API; annotations have no provider callbacks or input authority, and the buffer line is intentionally noninteractive |

This is already enough to build and compare distinct **editing grammars**:
the repository has Vim-style, selection-first, direct, syntax-structural, and
one runtime-selected trusted Lua model. The Lua model contract is deliberately
data-only and transaction-backed, not a claim that a familiar keymap recreates
an editor product.
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
| Vim-style terminal editor | normal/insert/replace/visual grammar, operators, counts, motions, find, literal search requests, an opt-in host `Str` regexp search, registers, undo/redo, local buffers/views, provenance, scoped sequence bindings, typed command prompts, a bounded declarative command-line adapter protocol, bounded named keyboard-macro storage/replay, generic rebased named session locations and jump history, and the tested `q{register}` / bare-`q` / counted-`@{register}` / `m{mark}` / backtick-mark / `Ctrl-o` / `Ctrl-i` subsets | command palette and generic host controls | Ex command language, Vim regexp/search semantics, uppercase/global-register semantics, macro editing/persistence, broad motion/text-object coverage, linewise/global marks, full jump-source/per-window semantics, compatibility mappings, and terminal/GUI appearance parity |
| Helix-style selection editor | selection-first model, multi-edit transactions, occurrence selection, syntax-structural selections, regex selection/splitting/filtering through `Str`, host literal/regexp search and reviewed query-replace, touching-range merge, primary and content rotation, orientation operations, local buffers/views, scoped sequence bindings, nested declarative modes including one initial adapter map, adapter-bindable page/center view requests, bounded named keyboard-macro storage/replay, trusted Lua's checked external selection filter, and optional LSP completion, hover, definition, checked code actions, and rename across already-open saved buffers | buffers can be assigned to split views; fixed system clipboard host commands are available to a future adapter | picker/config discovery, native `+`/`*` register grammar and multi-selection clipboard behavior, Helix selected-register macro workflow, Helix regex and exact post-rotation selection semantics, shell command-line/pipes, general workspace edits, full window model, and theme parity |
| Kakoune-style multiple-selection editor | explicit ordered selections, selection-first edits, syntax context, regex selection/splitting/filtering through `Str`, host literal/regexp search, touching-range merge, primary and validated count-grouped content rotation, orientation operations, scoped bindings/hooks, nested declarative modes including one initial adapter map, adapter-bindable page/center view requests, bounded named keyboard-macro storage/replay, generic rebased named session locations, named local buffers/views, and trusted Lua's checked external selection filter | split views render independently and focus routes input to the assigned buffer | Kakoune's inclusive anchor/cursor model, exact regex/count grouping and post-rotation selection semantics, Kakoune register-selection macro grammar, native mark/jump-list grammar, client/server sessions, shell expansions and asynchronous socket integration, full command language, and face/highlighter ecosystem |
| Micro-style terminal editor | direct text/caret/selection editing, `Ctrl-S` host save, the supplied `Ctrl-E` palette / `Ctrl-W` split-cycle / selected-text `Ctrl-X` cut / `Ctrl-C`/`Ctrl-V` system-clipboard Lua adapter, syntax spans, named local buffers/views, an optional host-owned visual buffer line, trusted Lua configuration, local plugins, search/palette, terminal themes, basic click/drag selection plus adapter-bindable keyboard viewport movement, scoped sequence bindings, and trusted Lua's checked external selection filter | Components and Lua can supply editing commands; trusted Lua may start bounded no-stdin streaming programs, inspect live/final snapshots, open a static report buffer, and the host can cancel a retained job | Micro's complete keybinding behavior, line-fallback cut behavior, mouse clipboard/menu/multi-click parity, OSC 52/SSH clipboard behavior, interactive shell split, interactive buffer tabs, plugin-manager/install flow, runtime theme/configuration surface, process input, and appearance parity |
| Emacs terminal product | direct text/caret/selection editing, `Ctrl-W` cut, supplied `Ctrl-Y` latest-kill adapter, a bounded 120-entry cross-buffer kill history, fixed system-clipboard host commands available to an adapter, `Ctrl-X Ctrl-S` save, `Ctrl-X Ctrl-F` host open prompt, `Ctrl-X 2/3/0/1/o/k` split/close/only/focus/clean-buffer-close requests, tested `Ctrl-X ^`, `Ctrl-X }`, `Ctrl-X {`, and `Ctrl-X +` grow/balance requests over a generic split tree, exact-divider dragging, same-buffer panes with independently retained/rebased ordered selections, key-addressable commands, host literal/regexp search, buffer-local stackable declared transient modes and bounded dynamic binding layers, named local buffers in split views, typed page/center view requests, a typed argument minibuffer, bounded named/countable keyboard-macro storage/replay, generic rebased named session locations, configuration/plugin concepts, bounded streaming background-job snapshots opened as normal buffers, and asynchronous language host | transient/layer composition and a bounded window-point analogue, not general Emacs keymap/window composition; trusted Lua's bounded program registry is a process-API probe | buffer/window/frame system, completion ecosystem, arbitrary keymap composition and Component-defined layers, Emacs regexp/search-ring semantics, automatic kill/yank clipboard integration, adjacent-kill concatenation, yank-pop, macro ring and Emacs macro name/edit commands, buffer-local mark and mark-ring semantics, Elisp/package/process APIs, display engine, product minimum-window behavior, numeric arguments, layout persistence, and terminal appearance parity |

“Supported now” means this repository has a testable behavior, not that its
keystrokes or visual rendering exactly match the named editor. “Partial
foundation” deliberately does not count toward parity.

Every table entry that lists host literal/regexp search also includes a
palette-only, one-buffer replacement probe. `search.replace.literal` and
`search.replace.regexp` make one history transaction over leftmost
non-overlapping accepted matches. `search.query-replace.literal` and
`search.query-replace.regexp` add a version-bound host review loop with skip,
replace, replace-remaining, and quit decisions; each accepted decision is a
checked transaction. None supplies a native editor's capture expansion,
search/replace history, cross-buffer operation, key grammar, or replacement
dialect, so this shared host capability does not advance a product-parity claim.

The regexp-search probe makes the compatibility boundary concrete. Emacs
documents separate incremental regexp searches, including forward and backward
entry points; Kakoune documents regex-driven search and selection operations;
Micro documents an incremental `Ctrl-F` find prompt and repeat commands. Zenbu
therefore supplies a separate incremental `search.regexp` host command, while
keeping model `Request_search` literal. Its `Str` dialect is byte-oriented and
rejects zero-width and UTF-8-boundary-splitting results, so it does **not**
stand in for the native regex/search semantics of any of these editors. See
[GNU Emacs regexp search](https://www.gnu.org/software/emacs/manual/html_node/emacs/Regexp-Search.html),
[Kakoune search keys](https://github.com/mawww/kakoune/blob/master/doc/pages/keys.asciidoc),
and [Micro default bindings](https://github.com/micro-editor/micro/blob/master/runtime/help/defaultkeys.md).

Replacement is a separate pressure case. Micro documents `replace` with
replace-all and regexp/capture flags, while Emacs documents interactive
query-replace and regexp query-replace. Zenbu offers palette commands that
either apply all safe matches at once or enter a host-owned review loop with
skip, one replacement, replace-remaining, and quit. It has no capture
expansion, cross-buffer replacement, replacement history, or product
replacement grammar. See [Micro commands](https://github.com/micro-editor/micro/blob/master/runtime/help/commands.md)
and [GNU Emacs query replace](https://www.gnu.org/software/emacs/manual/html_node/emacs/Query-Replace.html).

Micro's [default bindings](https://github.com/micro-editor/micro/blob/master/runtime/help/defaultkeys.md)
assign `Ctrl-S` to save, `Ctrl-E` to its command bar, `Ctrl-W` to split focus,
and `Ctrl-X` to cut. The [Emacs manual](https://www.gnu.org/software/emacs/manual/html_node/emacs/Other-Window.html)
defines `C-x o` as other-window; its [window commands](https://www.gnu.org/software/emacs/manual/html_node/emacs/Change-Window.html)
and [reference](https://www.gnu.org/software/emacs/refcards/pdf/refcard.pdf)
cover `C-x 0/1/2/3`, while its [file-visiting contract](https://www.gnu.org/software/emacs/manual/html_node/emacs/Visiting.html)
uses `C-x C-f`. The previous three supplied models left their shared
direct-manipulation and model-to-workspace baseline unrepresented.
`zenbu.direct` now routes committed text, caret/selection motion, clipboard,
and undo/redo through the existing public semantic API, requests save through
path-free `Request_save`, and requests a bounded workspace operation through
`Request_workspace`. It reserves `Ctrl-X Ctrl-S` for the Emacs sequence while
supporting Micro's standalone `Ctrl-S`; the bundled trusted Micro adapter
intercepts selected-text `Ctrl-X` before that prefix and requests a checked
host cut. It also exercises `Ctrl-E` and `Ctrl-W` without seeing paths, panes,
or mutable document state. M10 covers text, deletion, history, selection
extension, prefix cancellation, save, workspace effects, and adapter bindings.
The real terminal delegates `Ctrl-S`, `Ctrl-F`, `Ctrl-G`, and `Ctrl-P` to Direct
so its controls are reachable rather than intercepted by global host bindings.

Pane resizing is the next shared workspace pressure point. The [Emacs window
commands](https://www.gnu.org/software/emacs/manual/html_node/emacs/Change-Window.html)
define `C-x ^`, `C-x }`, `C-x {`, and `C-x +` for vertical growth, horizontal
growth/shrink, and balancing. [Vim's window help](https://vimhelp.org/windows.txt.html)
also defines incremental resizing and equalization, including mouse divider
dragging. Zenbu therefore adds both a model-neutral layout request that carries
only a direction and signed cell delta, and a host-owned exact-divider pointer
gesture. The host finds the focused pane's nearest matching ancestor divider
for keyboard requests, retains one cell for each child, and stores the
resulting proportion in the layout tree. `zenbu.direct` maps the four Emacs
keys above; scripts can opt into the generic host command IDs. This is useful
evidence for a reusable layout contract, not product parity: there are no
numeric prefixes, configurable product minimum sizes, `C-x -` semantics, or
tab/frame layouts. The host can additionally save and restore a versioned local
layout only after validating every clean file-backed buffer and view position;
this does not serialize an Emacs session, model state, or product window state.

This remains a generic test adapter, not Micro or Emacs parity: their
product-specific workspace, process, extension, and presentation layers remain
the evaluation backlog.

The next direct-editing gap was the distinction between a copied register and
an Emacs-style kill. [Emacs documents killing as insertion into a kill ring
shared by buffers](https://www.gnu.org/software/emacs/manual/html_node/emacs/Deletion-and-Killing.html);
Micro documents cut/paste bindings but does not turn every copy into a kill.
Zenbu therefore adds `Cut_to_clipboard` and `Paste_from_kill_ring` instead of
changing existing copy/register effects. A successful cut deletes through the
normal transaction path, writes its requested ordinary slot, and prepends one
entry to a bounded 120-entry session history; the Session synchronizes only
that history across buffers. `editor.kill-ring.cut` and
`editor.kill-ring.yank` are palette commands and an intentionally narrow Lua
binding allow-list. Direct maps `Ctrl-W` to cut a non-empty selection, the
Micro adapter maps `Ctrl-X` to the same command, and the Emacs adapter maps
`Ctrl-Y` to the newest ring entry. M2 regresses the capacity and invalid-index
boundary; M10 regresses Direct cut/yank and cross-buffer adapter behavior.
There is no kill coalescing, `M-y`/yank-pop, automatic synchronization between
the kill history and system clipboard, or claim that these key bindings
establish product parity.

Clipboard behavior is the next common host boundary. Helix reserves `+` for
the system clipboard and gives it explicit paste/yank commands; Micro's
documentation specifies `pbcopy`/`pbpaste` on macOS and a Linux external
provider; Emacs integrates kill/yank with the desktop clipboard. See [Helix
registers](https://docs.helix-editor.com/master/registers.html), [Helix
clipboard commands](https://docs.helix-editor.com/master/commands.html),
[Micro copy/paste](https://github.com/micro-editor/micro/blob/master/runtime/help/copypaste.md),
and the [Emacs clipboard manual](https://www.gnu.org/software/emacs/manual/html_node/emacs/Clipboard.html).
Zenbu now provides only the reusable core: `editor.clipboard.copy` and
`editor.clipboard.paste` are fixed host descriptors with an injectable UTF-8,
16 MiB provider. Production picks known executable pairs—`pbcopy`/`pbpaste`,
`wl-copy`/`wl-paste`, `xclip`, or `xsel`—rather than exposing a shell command
to Lua. Copy requires a non-empty selection, writes externally before it
updates the local unnamed slot, and paste converts one non-empty external value
into the ordinary checked replacement transaction. M10 regresses adapter
dispatch, write/read behavior, provider unavailability, invalid UTF-8, and the
size boundary. The Micro adapter maps `Ctrl-C` and `Ctrl-V` to it; Direct’s
default keys stay internal, so a user can choose the product behavior with
configuration. This does not establish Helix `+`/`*` registers or their
multi-selection payload semantics, Emacs automatic interprogram integration,
Windows or SSH clipboard support, or terminal OSC 52 behavior.

Terminal-editor appearance exposes a distinct renderer boundary. Helix
documents absolute and relative `editor.line-number` policies; a hard-coded
status row and no gutter meant Zenbu could not reproduce even that common
chrome choice without changing renderer code. See [Helix
configuration](https://docs.helix-editor.com/master/configuration.html). Zenbu
now provides a pure host-switchable presentation profile with absolute,
relative, or hidden line numbers; detailed, minimal, or hidden status rows;
and an optional bounded buffer line. The profile only changes frame space
allocation: its gutter uses display rows, reduces the text canvas, shifts
physical cursor projection, and has a safe cursorless fallback when the gutter
consumes the canvas. A visible buffer line reserves the top row, names local
buffers deterministically, marks the current/dirty buffer, and shifts both
cursor and canvas-only pointer coordinates; it is noninteractive. M4 regresses
profile validation, absolute/relative gutter layout, status-row allocation,
buffer-line coordinate translation, the narrow-canvas boundary, live
switching, semantic-state preservation, and invalid-profile retention. The paired host-owned
`view.theme.switch` command validates a built-in or TOML theme and has the
terminal backend apply the stored value immediately before drawing. M4 proves
theme selection/invalid retention and semantic-state preservation. This is a
generic terminal chrome primitive, not an appearance-parity claim:
product-defined widgets/status-line functions, minimaps, fonts, mouse menus,
and complete Vim/Helix/Kakoune/Micro/Emacs display engines remain absent.

Emacs exposes a stricter same-buffer window requirement: every window has its
own point, selecting a window installs that point in the buffer, and a newly
split window inherits the existing position. [Its window-point
documentation](https://www.gnu.org/s/emacs/manual/html_node/elisp/Window-Point.html)
is therefore a useful generic workspace test rather than merely an Emacs key
binding. Zenbu now stores a versioned ordered selection snapshot for every
`(pane, buffer)` pair. Splitting copies the source snapshot, inactive snapshots
rebase with `Document.transform_offset` through forward history edits, and
focusing a pane restores its snapshot through a checked selection-only
transaction. The renderer projects each pane from its saved selection; the
terminal still exposes a physical cursor only for the focused pane. M4 proves two Direct-model panes can insert at distinct
carets in one buffer after an intervening edit rebases the inactive position.
This is deliberately narrower than Emacs window point: positions on an
unreachable history branch are reset to the current buffer selection, and
Zenbu has no window parameters, narrowing, mark semantics, frames, or Emacs
display engine.

## Current result and next evaluations

The first host gaps exposed by Helix, Kakoune, Micro, and Emacs are the ability
to show more than one view and to assign those views to independent documents.
`zenbu.view.Layout` composes a binary vertical or horizontal split tree without
gaining document-mutation authority. The Session workspace now gives each
buffer a stable id and retains its model runtime/history, save state, syntax
context, LSP client handle, diagnostics, search state, and viewports. Input
focus activates the selected pane's buffer. The terminal regression suite
covers layout bounds, divider composition, cursor translation, focus cycling,
buffer creation, independent edits, pane-to-buffer rendering, same-buffer
per-pane caret restoration and rebasing, file opening, and duplicate-open
rejection.

The next common workspace failure was buffer identity. [Micro exposes
tabs](https://github.com/micro-editor/micro/blob/master/runtime/help/defaultkeys.md),
[Helix a buffer picker](https://docs.helix-editor.com/commands.html),
[Kakoune named scratch buffers](https://github.com/mawww/kakoune/blob/master/doc/pages/commands.asciidoc),
and [Emacs named buffers](https://www.gnu.org/software/emacs/manual/html_node/emacs/Buffers.html).
Zenbu now retains an optional host-owned UTF-8 display name with every buffer.
`workspace.buffers` exposes stable numeric IDs, labels, current state, and
dirty markers; the typed `workspace.buffer.switch` and
`workspace.buffer.rename` commands select or label a focused buffer without
granting a model buffer-table access. `workspace.buffer.close` safely rejects a
dirty buffer while its separate force-close descriptor deliberately discards it;
closing the final clean buffer creates one fresh unnamed buffer. Generated job
reports use this same contract as `*job N output*`. M10 regresses typed
rename/switch dispatch, listing, invalid-name rejection, non-mutation, safe and
forced closing, final-buffer replacement, and Direct's `Ctrl-X k` request. This
remains a compact workspace substrate, not tabs, a fuzzy picker, buffer-local
options, indirect buffers, Kakoune client/server buffers, or a complete buffer
lifecycle.

The cross-file coordination evaluation now has a bounded result. Every open
saved buffer contributes a request-time text snapshot to each language client;
the terminal waits on every client wakeup descriptor. Cross-file definitions
open/reuse a local buffer. Rename and `workspace/applyEdit` stage edits against
all target snapshots, then publish all candidate buffer runtimes only when
every target validates. Regression tests cover successful rename/apply-edit,
unopened targets, stale target snapshots, and conflicts without partial source
or target edits. This is not a general project workspace: targets are not
auto-opened, resource operations are rejected, and undo/history remains per
buffer. Shell panes, full mouse interaction, configurable presentation, and
editor-specific command languages are separate evaluations.

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

The next keymap result was a declared custom-mode stack for trusted Lua.
`replace`, `push`, `pop`, and `clear` transitions are staged with bindings;
the innermost map takes precedence while lower stacked maps remain available as
fallbacks. An unmatched bare `Escape` pops the innermost map, and a reload
retains the stack only when the replacement generation still declares every
id. M7 covers nested push/pop, lower-map fallback, unmatched-input containment,
Escape fallback, and invalidation on reload. This is enough to prototype
Helix-style nested prefixes and transient leader maps. Lua also has bounded,
buffer-local dynamic binding layers: declared maps start disabled, enable and
disable atomically through typed host commands, resolve by ordinary scope then
priority, and reject equal-priority same-scope prefix overlap. The lifecycle is
visible in `Bindings` and `Why`; reload drops stale or newly conflicting maps.
This is still not general Emacs keymap composition: priority is bounded,
lifecycle commands are not bindable, and Component ABI v1 cannot declare modes,
transitions, or layers.

The insert-mode evaluation exposed a related boundary. A custom keymap could
previously bind only fixed logical keys, so it could not express an adapter's
committed-text state. Modes can now declare `input_mode = "text"`; a typed
`<text>` binding captures one committed Unicode text or paste event and forwards
it only to a declared text command parameter. M4 proves the wildcard remains
distinct from logical keys, while M7 covers Unicode delivery, text-entry status,
Escape exit, and staging rejection for a missing or undeclared parameter. This
is an adapter-defined insert-mode primitive, not a general input-method API. A
later Lua-model evaluation supplies an explicit serialisable state-machine
contract rather than a mutable event loop.

Keyboard macros were the next common failure. Helix exposes experimental
record/replay commands, Kakoune records and replays keypresses through its `@`
register, and Emacs defines a keyboard macro by executing its recorded command
sequence once and replaying it later. See [Helix keymap](https://docs.helix-editor.com/master/keymap.html),
[Kakoune keys](https://github.com/mawww/kakoune/blob/master/doc/pages/keys.asciidoc),
and [Emacs keyboard macros](https://www.gnu.org/s/emacs/manual/html_node/emacs/Keyboard-Macros.html).
Zenbu now has `editor.macro.record` and `editor.macro.replay` as generic host
descriptors. A Lua adapter can bind them to `Q` and `q`; plain binding tokens
are case-sensitive so that mapping is representable. Their optional
`register` argument stores or retrieves a bounded named session entry (`@` is
the default): at most 64 valid UTF-8 names of at most 64 bytes, each holding at
most 1,024 keyboard/text events. Recording executes its input once, omits its
own controls, palette/argument-prompt interaction, and pointer input. Replay
feeds the stored events through the standard Session input dispatcher,
preserving normal transactions, hooks, history, syntax/language refresh, and
error handling. M10 regression coverage checks adapter bindings, Unicode text
input, default and named register replay, catalog inspection, empty replay
rejection, and the recording bound. The public model effect exposes only the
active recording name plus reserve/toggle/replay requests, letting the supplied
Vim model implement the tested `q{register}`, bare-`q`, and counted
`@{register}` subset without access to macro contents or Session internals.
The count is limited to 1,024 iterations and 65,536 input events. This remains
a shared storage primitive, not Helix selected-register, Kakoune register
grammar, or Emacs macro-ring/name/edit compatibility. It excludes Vim
uppercase/global registers, macro editing, and persistence.

Persistent locations were the next shared navigation gap. Vim's marks and
jump list, Kakoune's client jump list and selection registers, and Emacs's
per-buffer mark and mark ring all require navigation state that survives
ordinary edits. See [Vim motions](https://vimhelp.org/motion.txt.html),
[Kakoune keys](https://github.com/mawww/kakoune/blob/master/doc/pages/keys.asciidoc),
and [the Emacs mark ring](https://www.gnu.org/software/emacs/manual/html_node/emacs/Mark-Ring.html).
Zenbu now exposes `editor.location.set` and `editor.location.jump` as typed
host descriptors. A location captures a valid UTF-8 name (at most 64 bytes),
buffer id, ordered selection set, primary index, and source document version;
there are at most 64 locations per Session. On every input it rebases saved
offsets through the active history lineage with `Document.transform_offset`.
Jumping activates the captured buffer and restores the selection set through a
normal selection-only transaction, retaining normal history, trace, and
provenance. If the buffer is unavailable or the recorded version is not
reachable from the current history branch, the location is marked stale and
the jump is rejected. M10 covers capture, insertion rebasing, cross-buffer
activation, inspection, stale-branch rejection, and use from both Vim-style
and selection-first model runtimes. `Request_location` lets an editing model
request capture or restoration without observing Session state; the Vim
workload maps `m{mark}` and a one-scalar backtick-mark jump to that effect.

This is an editor-neutral substrate, not broad native compatibility. Vim still
lacks linewise/global marks; Kakoune lacks register/client navigation grammar;
Emacs lacks buffer-local mark and mark-ring commands. Those product adapters
remain separate evaluation work.

The same research exposed a follow-on gap: Vim documents older/newer jump-list
traversal on `Ctrl-O`/`Ctrl-I`, including a fixed 100-entry per-window list;
Kakoune likewise uses `Ctrl-O`/`Ctrl-I` for its client jump list and explicitly
pushes the prior selection for goto, buffer-switch, and search commands. See
[Vim jump motions](https://vimhelp.org/motion.txt.html) and [Kakoune jump
list](https://github.com/mawww/kakoune/blob/master/doc/pages/keys.asciidoc).
Zenbu now provides `editor.jump.push`, `editor.jump.backward`, and
`editor.jump.forward`, plus the model-neutral `Request_jump` effect. The host
stores a rebased ordered selection set with its buffer id/version in a bounded
100-entry backward/forward stack; crossing a new jump clears forward entries.
`editor.location.jump` records the source selection before it restores its
target, and the Vim workload maps counted `Ctrl-O` and `Ctrl-I` to traversal.
The selection-first workload maps `Ctrl-S`, `Ctrl-O`, and `Ctrl-I` to the same
host effects, matching the documented normal-mode controls of both Helix and
Kakoune. M10 covers backward/forward restoration, stack inspection, a
mark-generated return point, the Helix/Kakoune-style save/backward/forward
sequence, generic push/backward traversal, and the existing stale-branch
safety rule.

This is not full Vim/Kakoune/Emacs jump compatibility. The stack is
session-wide rather than Vim-per-window or Kakoune-per-client, locations from
unavailable history are skipped, and only explicit push/location-jump actions
record entries today. Goto motions, search, buffer switches, definitions,
Emacs mark-ring rules, persistence, and product-specific commands remain
separate evaluations.

The next evaluation gap was argument-taking commands. The command palette and
custom bindings now collect descriptor-declared text, built-in selector, and
built-in transformation parameters before invoking the normal typed command
effect. M10 regression coverage executes `editor.apply`, rejects an invalid
selector without mutating the document, and proves a trusted Lua command
receives a prompted text value through `call.arguments`. This is a reusable
minibuffer foundation, not Vim Ex, Kakoune command language, or Emacs minibuffer
and completion parity. Component ABI v1 commands cannot yet declare parameter
metadata, so their palette entries remain parameterless.

The command-line comparison is intentionally narrower still. Vim's command
line, Micro's command bar, and Kakoune's command language each carry
product-level parsing and history behavior. The M10 command-line fixture only
establishes that `:exact.command-id token ...` reaches a registered descriptor
through its existing typed parameters, and that unknown, missing, or extra
arguments are rejected before an effect runs. Quoting, completion, history,
and all command-name compatibility remain deferred.

The next shared Helix/Kakoune failure was selection-set algebra. Helix exposes
regex select/split/filter, merging, orientation, and primary-selection
operations in its [keymap](https://docs.helix-editor.com/master/keymap.html).
Kakoune documents corresponding selection manipulation and anchor/cursor
behavior in its [keys reference](https://github.com/mawww/kakoune/blob/master/doc/pages/keys.asciidoc).
Zenbu now exposes eleven model-neutral `editor.selection.*` descriptors: regex
select, split, keep, remove; merge touching ranges; rotate primary selection in
either direction; rotate non-empty selection contents in either direction;
flip orientation; and normalize forward orientation. Selection operations
derive ordinary `set-selections` intents from copied context. Content rotation
uses the explicit `replace-selection-contents` intent, which requires exactly
one replacement per current selection and resolves into one atomic multi-edit
transaction. Its optional positive `group-size` parameter partitions adjacent
groups and is rejected unless it divides the selection count; the
selection-first model forwards a digit count to this parameter for `Alt-(`/
`Alt-)`. The M3 suite proves ordered output, filtering, primary rotation,
touching-only merge, orientation, unequal-length and grouped content rotation,
malformed/zero-width patterns, UTF-8 boundary rejection, invalid count
rejection, and replay; M10 proves scoped Lua bindings open both typed regex and
optional grouped-rotation prompts, perform the commands, and retain command
provenance. This does **not** establish Helix or Kakoune parity: `Str` is
byte-oriented and a boundary-violating match is rejected, Zenbu ranges are
half-open rather than Kakoune-inclusive, post-edit selections use Zenbu
rebasing, and the exact native count-grouping behavior remains unverified.

Adding this command family also exceeded the original palette's fixed first
16 displayed results. The palette now keeps the keyboard-selected result in a
moving 16-item render window while all providers remain searchable. M10 covers
both movement beyond the initial result window and filtering a builtin
selection descriptor or host descriptor by id/provider.

The shared presentation result now has three small contracts. The renderer
continues to produce semantic styles, while the terminal maps those styles
through `default`, `dark`, `light`, or a validated `--theme` TOML file. It also
converts primary press/drag/release and wheel events into canvas-only host
gestures; selections retain grapheme and transaction boundaries, and an
explicitly scrolled viewport persists until keyboard input resumes following.
Finally, the host may compose a bounded noninteractive buffer line above the
pane layout. The regression suite checks stable built-in names,
default-palette compatibility, true-colour parsing, decoration overrides,
rejection of unknown roles, pointer decoding, grapheme-safe selection,
status-row and gutter exclusion, buffer-line coordinate translation, scrolling
persistence, and pane focus. It does not claim window/widget,
mouse-clipboard, font, or GUI parity.

Keyboard view movement was the next cross-product failure. [Helix's documented
view mode](https://docs.helix-editor.com/master/keymap.html) distinguishes
scrolling and centering from selection movement; [Kakoune's key
reference](https://github.com/mawww/kakoune/blob/master/doc/pages/keys.asciidoc)
exposes scrolling/centering through normal-mode mappings; [Micro's default
bindings](https://github.com/micro-editor/micro/blob/master/runtime/help/defaultkeys.md)
and [Emacs point movement](https://www.gnu.org/software/emacs/manual/html_node/emacs/Moving-Point.html)
expose page navigation independently of text mutation. Zenbu therefore adds
`Request_viewport` with line scroll, page scroll, and center operations. M10
also keeps a bounded pane-local display policy for vertical scroll margins and
built-in line-number/status presentation overrides; it is not a general window
option registry.
Models supply only the request; `Session` derives the focused pane, source-row
height, and active presentation policy, then clamps the viewport. The request
has no document, selection, history, renderer-frame, or terminal-handle
authority. Terminal input now preserves `PageUp` and `PageDown` as named keys,
and the closed Lua host-binding allow-list exposes `view.scroll.*`,
`view.page.*`, and `view.center`. A script-owned model can issue the same
checked `view` action after choosing it from its own state transition; it still
receives neither dimensions nor renderer state. The bundled
[`helix-adapter.lua`](../examples/helix-adapter.lua) maps `PageUp`, `PageDown`,
`Ctrl-U`, `Ctrl-D`, and `z z` in the selection model. M2 verifies the effect is
nonsemantic, M4 verifies decoding, viewport geometry, status-row policy,
clamping, and centering, and M10 verifies a scoped Lua adapter and sequence
prefix. This is still not Helix/Kakoune/Micro/Emacs view parity: there is no
native view-mode grammar, horizontal scroll margins, arbitrary window-local
state, or product-specific scrolling behavior.

The next grammar evaluation was persistent script ownership. A static adapter
can rename commands and layer transient bindings, but it cannot encode
operator-pending, count, prefix, or product-specific mode state. `zenbu.model`
therefore registers one trusted Lua grammar with an initial serialisable state
and status, then accepts `{ input, state }` and returns `{ state, status,
effects? }` for every input. The model's identity is retained in normal trace
and transaction provenance; the Host validates every returned effect exactly as
for built-in models. Callback failures leave both state and document unchanged.
M7 and reload-migration regressions cover text-mode transitions, UTF-8
insertion, successful schema upgrade/downgrade, rejected migration retention,
disabled persistence, bounded exported values, provenance,
replacement-generation reload, disposed-callback avoidance, and an invalid
response boundary. The bundled
[`script-modal-editor.lua`](../examples/script-modal-editor.lua) is the first
workload fixture. It makes a stateful modal adapter demonstrable, not complete
product parity: reload migration is local to one live session, with no
cross-session state persistence; Component-defined dynamic maps, arbitrary
workspace or display APIs, product command languages, general process
integration, and full terminal appearance reproduction remain absent.

The next shared product failure was external filtering. Helix documents `|`,
`!`, and related shell commands over selections; Kakoune documents selection
filters; and Micro documents `textfilter`. See the [Helix keymap](https://docs.helix-editor.com/master/keymap.html), [Kakoune key reference](https://github.com/mawww/kakoune/blob/master/doc/pages/keys.asciidoc), and [Micro command bar](https://github.com/micro-editor/micro/blob/master/runtime/help/commands.md).
Zenbu now provides a smaller common contract: a trusted model or Lua command
can request `external-filter` with an absolute executable and argument vector.
The host invokes it once per current selection, caps input/output and total
replacement at 16 MiB, requires UTF-8 output, applies successful replacements
in a single normal transaction, and terminates a process after five seconds.
M7 covers multi-selection output, transaction provenance, relative-program
rejection, and nonzero process failure with no document mutation. This avoids
turning a configuration string into a shell command.

The next process-integration probe now streams: trusted Lua can request a
`background-process` with the same absolute-path/argument-vector boundary. A
Session admits at most 64 running programs, supplies no stdin, starts each in a
host-owned `/` cwd and fixed environment, continuously drains stdout/stderr,
and wakes the terminal for retained output as well as completion. It retains
64 KiB per stream, kills the isolated process group after five seconds or 16
MiB combined output, requires valid UTF-8 stdout, and exposes snapshots in
`process.jobs`. `process.job.open-output` turns the current or final bounded
report into a named normal workspace buffer. The host can cancel a retained
running job through the typed `process.job.cancel` command, and Session
shutdown joins its readers after process-group cleanup. M7 regresses live
output, fixed cwd/environment, backpressure, timeout, malformed UTF-8,
document non-mutation, cancellation tree cleanup, and host shutdown.

This remains intentionally narrower than a terminal product. [Vim's jobs and
channels](https://vimhelp.org/channel.txt.html) support callbacks, selectable
protocols, input, and buffers, while its [terminal feature](https://vimhelp.org/terminal.txt.html)
owns a terminal emulator, focused input, a PTY, sizing, and terminal modes.
Zenbu deliberately exposes none of those authorities: there is no callback,
stdin, shell expansion, PTY, terminal emulation, or interactive pane. The
process group is cleanup machinery rather than an adapter-facing API.

## Executable workload scenarios

[`test/test_editor_workloads.ml`](../test/test_editor_workloads.ml) reads the
TOML fixtures in [`test/fixtures/workloads`](../test/fixtures/workloads). Each
fixture identifies an upstream primary source, the precise Zenbu boundary it
exercises, its supported/partial/rejected outcome, and the expected document,
ordered selections, focused view, status, typed command trace, and (where a
workflow reaches model execution) normal `Why` provenance.
The test also requires every declared limitation to remain in this document.
It is therefore evidence for a narrow capability rather than evidence that a
familiar key establishes product parity.

| fixture | outcome | Zenbu boundary and declared limit |
| --- | --- | --- |
| `vim-delete-next-word` | supported | The Vim model’s tested `d` + next-word selector; Ex command language, Vim regexp semantics, broad text objects, and appearance parity remain absent. |
| `helix-page-down-adapter` | partial | The supplied Helix-style adapter requests model-neutral page movement; picker/config discovery, native register behavior, shell command-line/pipes, full windows, and theme parity remain absent. |
| `kakoune-ordered-multiple-selection` | partial | The selection-first model applies one checked transaction to ordered current selections; Kakoune’s inclusive anchor/cursor model, client/server sessions, socket integration, command language, and face/highlighter ecosystem remain absent. |
| `micro-selected-cut-adapter` | partial | The supplied Micro-style adapter maps selected-text `Ctrl-X` to a checked cut; line-fallback cut behavior, mouse/OSC 52 behavior, interactive panes/tabs, plugin installation, and appearance parity remain absent. |
| `emacs-kill-yank-adapter` | partial | The Direct model plus supplied `Ctrl-Y` adapter exercises bounded kill history; kill coalescing, yank-pop, automatic clipboard synchronization, Elisp, general keymaps/windows, and display parity remain absent. |
| `vim-ex-substitute-rejected` | rejected | A leading `:` is not a Vim-model command prompt. [#27](https://github.com/gongahkia/zenbu/issues/27) owns evaluation of one bounded `:substitute` workflow; it explicitly excludes an Ex parser, Vim regexp semantics, ranges, mappings, history, and compatibility claims. |

The fixture strings below are intentionally exact: the conformance test checks
them against the baseline matrix above so a limitation cannot disappear while a
scenario remains labeled supported or partial.

Terminal-presentation evidence is kept separately in the
[deterministic frame snapshots](PRESENTATION.md#deterministic-frame-snapshots).
Those fixtures distinguish Zenbu-owned profiles from the supplied editor-style
adapters and inspect only semantic frame data; they do not establish product
appearance parity.

- `vim-delete-next-word` and `vim-ex-substitute-rejected`: Ex command language, Vim regexp/search semantics, uppercase/global-register semantics, macro editing/persistence, broad motion/text-object coverage, linewise/global marks, full jump-source/per-window semantics, compatibility mappings, and terminal/GUI appearance parity.
- `helix-page-down-adapter`: picker/config discovery, native `+`/`*` register grammar and multi-selection clipboard behavior, Helix selected-register macro workflow, Helix regex and exact post-rotation selection semantics, shell command-line/pipes, general workspace edits, full window model, and theme parity.
- `kakoune-ordered-multiple-selection`: Kakoune's inclusive anchor/cursor model, exact regex/count grouping and post-rotation selection semantics, Kakoune register-selection macro grammar, native mark/jump-list grammar, client/server sessions, shell expansions and asynchronous socket integration, full command language, and face/highlighter ecosystem.
- `micro-selected-cut-adapter`: Micro's complete keybinding behavior, line-fallback cut behavior, mouse clipboard/menu/multi-click parity, OSC 52/SSH clipboard behavior, interactive shell split, interactive buffer tabs, plugin-manager/install flow, runtime theme/configuration surface, process input, and appearance parity.
- `emacs-kill-yank-adapter`: buffer/window/frame system, completion ecosystem, arbitrary keymap composition and Component-defined layers, Emacs regexp/search-ring semantics, automatic kill/yank clipboard integration, adjacent-kill concatenation, yank-pop, macro ring and Emacs macro name/edit commands, buffer-local mark and mark-ring semantics, Elisp/package/process APIs, display engine, product minimum-window behavior, numeric arguments, layout persistence, and terminal appearance parity.

## How to run the evidence

```sh
make check
dune exec test/test_m4_terminal.exe
dune exec test/test_model_runtime.exe
dune exec test/test_editor_workloads.exe
dune exec bin/zenbu_headless.exe -- demo
```

Use `Ctrl-P` in the terminal host and select `workspace.split.vertical`,
`workspace.split.horizontal`, `workspace.pane.next`,
`workspace.pane.close`, `workspace.pane.only`,
`workspace.pane.grow-width`, `workspace.pane.shrink-width`,
`workspace.pane.grow-height`, `workspace.pane.shrink-height`, or
`workspace.panes.balance` to exercise the current workspace. Use
`workspace.buffer.new`, `workspace.buffer.open`,
`workspace.buffers`, `workspace.buffer.switch`, `workspace.buffer.rename`,
`workspace.buffer.next`, `workspace.buffer.previous`,
`workspace.layout.save`, and `workspace.layout.restore` to exercise the
buffer table and local layout format. Use `workspace.project.root.set`,
`workspace.file-picker`, and `workspace.project.search` to inspect the
explicit-root navigation and bounded search boundaries.
These commands are also available through the typed `Session.handle_host`
interface for headless tests and a future host binding layer.
