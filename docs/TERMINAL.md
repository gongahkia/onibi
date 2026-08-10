# M4-M7 terminal host

`zenbu [--model vim|selection|structural] [--language ID] [--config PATH|--no-config] [FILE]` is the
interactive M4-M7 executable.
It loads an existing UTF-8 file, or creates an unnamed empty buffer when no
file is supplied. File open and UTF-8 validation happen before terminal mode
is entered; errors are reported on stderr. The deterministic
`zenbu_headless` executable is retained for replay and scripted model tests.

Run an installed development switch with:

```sh
dune exec bin/zenbu.exe -- --model vim FILE
dune exec bin/zenbu.exe -- --model selection FILE
dune exec bin/zenbu.exe -- --model structural FILE
```

`Ctrl-S` saves an existing file. `Ctrl-Q` exits when clean; when dirty it shows
a warning and requires a second `Ctrl-Q`. Unnamed save-as is intentionally not
implemented. EOF with unsaved changes returns an error after terminal cleanup
and does not write the file.

M7 configuration defaults to `$XDG_CONFIG_HOME/zenbu/init.lua` (or
`$HOME/.config/zenbu/init.lua`). `--config PATH` chooses an explicit file and
`--no-config` disables it. `Ctrl-Alt-R` is a host-reserved staged reload key;
`Alt-R`/`Meta-R` is its terminal-portable fallback when a terminal cannot encode
the combined printable-key modifier.
`Ctrl-S` and `Ctrl-Q` remain host-reserved too. Script bindings are decoded to
ordinary logical input only after those controls have been handled, so scripts
cannot replace save/quit/reload policy. See [scripting](SCRIPTING.md).

## Backend and lifecycle

`terminal/Backend` uses `notty-community` only in its implementation. It
requires stdin and stdout TTYs, enters raw non-canonical input plus an alternate
screen, hides the physical cursor, maps terminal events to `Terminal.Event`,
and restores input, cursor, and normal screen through `Fun.protect`. The
backend also snapshots input attributes before creation and uses a conservative
cleanup fallback if creation or release raises; its process-exit disposal is an
additional last resort. Mouse and bracketed paste are disabled.

The rest of the program sees no Notty values. `Input_decoder` maps printable
UTF-8 keys, Escape, Enter, Backspace, Tab, Delete, arrows, Home/End, and
reported modifiers into the public logical `Input_event` protocol. Resize stays
a host event. For an unmodified printable key, the model's generic
`Model_status.input_mode` decides between `Key_press` and `Text_input`; the
adapter never branches on a model id, Vim mode, or selection-model state.
M5's structural model uses the existing named arrow events and generic status
line; the terminal does not inspect syntax nodes or grammar kinds.

## Coordinates and rendering

| layer | coordinate | purpose |
| --- | --- | --- |
| document/model | UTF-8 byte offset at a code-point boundary | anchors, selections, transactions |
| view | source line plus display column | viewport and visible selection geometry |
| frame | zero-based row and column | pure styled cells and logical cursor |
| backend | terminal cursor position | physical presentation only |

The view preserves document bytes. Uuseg finds extended grapheme clusters,
Uucp supplies a terminal-width hint, tabs stop every four columns, and C0/DEL
controls render as caret notation. A renderer indexes source line boundaries,
then lays out only viewport lines. It separately styles primary selection,
secondary selections, status/message cells, and leaves physical cursor control
to the backend. The viewport adjusts vertically and horizontally so the primary
selection head is visible. Tiny terminals render one clipped warning row.

Terminal column width is inherently heuristic. Complex scripts and emoji can
occupy a different width in a particular terminal than Uucp's estimate. M4
does not support soft wrapping, proportional display, mouse selection, or
pixel-perfect emoji alignment.

## Persistence

The session retains its saved document version and source contents. A matching
version is immediately clean; later versions compare their source contents, so
selection-only history changes are not dirty and undoing to saved contents is
clean. Saving writes and fsyncs an exclusive temporary file beside the target,
preserves an existing target mode, then renames the temporary file over the
target. This avoids in-place truncation, but M4 does not fsync the parent
directory, detect external file changes, or implement save-as.

## M4 integration pressure report

M4 exposed one addition to the public model protocol: `Model_status.input_mode`.
Without it, a terminal host would have to inspect model-specific identifiers
such as `insert` to distinguish a command key from committed text. The new
two-case disposition is generic; Vim-style, selection-first, and the retained
operator-first proof model declare `Text_entry` only while accepting committed
text. No terminal state or terminal package type entered the kernel or model
API.

The existing immutable `Editor_context`, semantic effects, transactions,
history, and selection offsets otherwise supported the host unchanged. The M4
session owns dirty/save/quit and viewport concerns above models. Rendering
projects the existing immutable context; it does not add display columns,
terminal cells, or filenames to documents. M4 tests exercise terminal-event
decoding, Unicode/tab display mapping, primary and secondary frame styling,
viewport/tiny-terminal behavior, both model choices, safe file save, dirty
state after undo, and quit policy.

## M5 terminal integration

The session detects OCaml and JSON from file extensions, or accepts the narrow
`--language ID` override. It constructs an optional syntax service above the
model runtime; unsupported extensions remain ordinary buffers. The session's
only structural-specific work is selecting the registered `Structural` model,
exactly as it selects the other models. Rendering still consumes document text,
ordinary selections, and generic model status. No terminal module imports or
names Tree-sitter or AST types.

## M6 inspector

`zenbu --trace` enables a bounded local trace (capacity 1024) and `--profile`
enables bounded local CPU-time samples (also capacity 1024). Neither option
sends information anywhere. With tracing enabled, `Ctrl-O` toggles a read-only
generic `Why` overlay for the most recent input; `Escape` dismisses it. The
overlay uses ordinary Zenbu frame rows, follows resize through the normal render
loop, and never mutates the document.

This is a host-level Zenbu inspector, not Vim's Ex language or a model-specific
command mode. Headless inspection provides command, binding, history, syntax,
and profile access without a command palette.

## M7 configuration integration

`Session` selects script bindings before delivering normal model input, using
model+status, model, then global scope. A script command still enters the
ordinary runtime as `Invoke_command`; selectors and transformations are
resolved through the normal semantic behavior registry. A document-changed hook
runs only after a committed content change, and an after-save hook only after a
successful host save. The session suppresses delivery of the same hook event
while it is already active. Terminal and renderer modules still know no Lua,
script callback, or Tree-sitter type; they only render the session message and
generic inspector lines.
