# Direct editing model

`zenbu.direct` is an always-inserting, direct-manipulation editing grammar. It
exists to pressure-test Zenbu's public model API against the common baseline of
non-modal terminal editors; it is not a claim to reproduce Micro or Emacs.

## Tested grammar

| input | semantic behavior |
| --- | --- |
| committed text or `Enter` | insert text immediately |
| arrows, `Home`/`End`, or `Ctrl-A`/`Ctrl-B`/`Ctrl-E`/`Ctrl-F`/`Ctrl-N`/`Ctrl-P` | move the caret through shared selectors |
| `Shift` plus an arrow | extend the primary selection through a shared selector |
| `Backspace`, `Delete`, `Ctrl-H`, or `Ctrl-D` | delete current selection or adjacent text unit |
| `Ctrl-C` / `Ctrl-V` | copy from / paste through the shared unnamed clipboard slot |
| `Ctrl-W` | cut non-empty selections to the unnamed slot and bounded shared kill history |
| `Ctrl-Z` / `Ctrl-Y` | use shared undo / redo history |
| `Ctrl-S` | request host save (Micro-style baseline) |
| `Ctrl-X Ctrl-S` | request host save (Emacs-style baseline) |
| `Ctrl-X Ctrl-F` | open the host file prompt |
| `Ctrl-X k` | close the focused clean buffer |
| `Ctrl-X 2` / `Ctrl-X 3` | request stacked / side-by-side views |
| `Ctrl-X 0` / `Ctrl-X 1` / `Ctrl-X o` | close current / keep current / focus next view |
| `Ctrl-X Escape` or `Ctrl-X Ctrl-G` | cancel the control-X prefix |

Text, selection, clipboard, and history effects are ordinary
`Model_intent`/`Model_effect` values. `Ctrl-S` and `Ctrl-X Ctrl-S` emit only
`Request_save`: the Session retains save-as prompting, file-path ownership,
atomic replacement, and language-service notification. `Ctrl-X` workspace
commands emit only a bounded `Request_workspace`; the Session retains view
geometry, pane/buffer identity, prompts, and file I/O. The M10 host suite
covers the table's text, deletion, history, selection, cancellation, save, and
workspace behavior; inspect the declared grammar with:

```sh
dune exec bin/zenbu_headless.exe -- bindings direct
dune exec bin/zenbu_headless.exe -- describe model zenbu.direct
```

`Ctrl-X k` follows the host's safe-close policy: it refuses an unsaved buffer.
Use the explicit palette command `workspace.buffer.force-close` when discarding
that buffer is intentional. Closing the final clean buffer creates one fresh
unnamed buffer, so the session always has an editable workspace target.

The workspace retains an ordered selection snapshot for every `(pane, buffer)`
pair. A split starts from the source pane's current selection; then each pane
can move independently even while it displays the same buffer. On focus the
host restores that pane's snapshot through the usual checked selection
transaction, and inactive snapshots rebase through forward history edits. M4
regresses distinct direct-model carets and an edit from one pane rebased before
an insertion from the other. This supplies a bounded analogue of Emacs window
point; it is not Emacs's complete window, point, mark, or undo semantics.

`Ctrl-W` is deliberately a cut-region operation rather than a generic delete:
it creates a checked delete transaction, writes the unnamed clipboard slot, and
prepends a UTF-8 entry to a 120-entry session kill history. The history is
shared by local buffers. `editor.kill-ring.yank` pastes its latest entry using
the same transaction path; [`examples/emacs-adapter.lua`](../../examples/emacs-adapter.lua)
maps `Ctrl-Y` to that host command. This is the tested common substrate behind
an Emacs-style `C-w` / `C-y` pairing, not Emacs kill-ring compatibility: Zenbu
does not concatenate adjacent kills, provide `M-y`/yank-pop, retain mark-ring
semantics, or bridge the operating-system clipboard.

The terminal normally reserves several host controls. When Direct is active,
`Ctrl-S`, `Ctrl-F`, `Ctrl-G`, and `Ctrl-P` reach the model instead: save remains
a typed host request, while the latter keys preserve its Emacs-style movement
and prefix-cancellation inputs. The palette and search remain available from
their other documented entry points.

## Deliberate limits

This is not Micro compatibility. It lacks Micro's full command/keybinding
surface, line-fallback cut behavior, tabs and terminal panes, mouse
clipboard/menu behavior, OSC 52/SSH clipboard support, plugin manager,
configuration surface, and rendering parity.

[`examples/micro-adapter.lua`](../../examples/micro-adapter.lua) is a small
trusted Lua adapter for `--model direct`: it maps Micro's `Ctrl-E` command bar
to Zenbu's palette, `Ctrl-W` split cycle to the shared workspace action, and
non-empty-selection `Ctrl-X` to `editor.kill-ring.cut`. It also maps Micro's
`Ctrl-C`/`Ctrl-V` to the fixed host system-clipboard commands; Direct's
built-in `Ctrl-C`/`Ctrl-V` remain internal unnamed-slot operations when the
adapter is not loaded.
Use it as a configuration example, not as a Micro clone:

```sh
dune exec bin/zenbu.exe -- --model direct --config examples/micro-adapter.lua FILE
```

It is also not Emacs compatibility. It lacks composed global/major/minor
keymaps, a buffer-local mark ring, full kill-ring behavior, minibuffer
completion, Elisp and package APIs, processes, frames, display engine, and
terminal appearance parity. Those are separate host and extension-runtime
evaluations, not details that a direct text-entry state machine can establish.
