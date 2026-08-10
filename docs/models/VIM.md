# Vim-style model

`zenbu.vim-style` is a substantial Vim-inspired subset, not a Vim-compatible
implementation. It owns its modal grammar, counts, operator-pending state,
text-object prefix, and clipboard-slot prefix. It uses only `zenbu.model_api`.

## States

| state | behavior |
| --- | --- |
| `NORMAL` | accepts counts, movement, operators, paste, history, and repeat |
| `INSERT` | committed UTF-8 text produces one `insert-text` transaction per input event |
| `DELETE…`, `CHANGE…`, `YANK…` | awaits a selector or text-object prefix |
| `SLOT…` | awaits one `a`–`z` clipboard slot name after `"` |
| `G…` | awaits the second `g` of `gg` |

## Supported normal commands

| input | semantic behavior |
| --- | --- |
| `h` / `l` | previous / next UTF-8 scalar caret movement |
| `j` / `k` | adjacent-line scalar-column movement, clamped to line length |
| `w` / `b` / `e` | next-word / previous-word / word-end movement |
| `0` / `^` / `$` | line start / first nonblank / line end |
| `gg` / `G` | document start / document end |
| `d{motion}` | delete the motion target; `dd` is linewise |
| `c{motion}` | delete the target, then enter insert; `cc` is linewise |
| `y{motion}` | copy the target; `yy` stores a linewise entry |
| `diw`, `daw`, `ciw`, `caw` | inner/around current-word text objects |
| `x` / `X` / `s` | delete next scalar / previous scalar / delete next scalar then insert |
| `i` / `a` | insert at current selection / after the next scalar |
| `p` / `P` | paste selected clipboard slot after/before its target boundary |
| `"a` | select slot `a` for the next copy or paste operation |
| `u` / `Ctrl-r` | history undo / redo |
| `.` | repeat the latest repeatable semantic edit |
| `Escape` | cancel a pending state or leave insert |

Counts are parsed in `NORMAL`; a count before an operator multiplies a count
between operator and selector. Thus `3dw` and `3d2w` execute three and six
sequential reusable `next-word + delete` semantic operations. Counts reset
after a completed command or cancellation. Targets beyond a document boundary
reject atomically rather than clipping.

`d`, `c`, and `y` are not kernel operations: they select one of shared delete,
replace/insert, or copy effects. `dw` resolves as `next-word + delete`.

## Deliberate limits

There is no visual mode, ex command language, macros, marks, model-private
search grammar, mappings, register types beyond characterwise/linewise text,
blockwise editing, full desired-column behavior, or Vim compatibility promise.
The terminal's model-neutral `Ctrl-F` search service is available while this
model is active. `a` assumes all active heads can advance when not already at
document end. Cursor positions are UTF-8-scalar-safe, not grapheme or
terminal-cell-aware.

Insert input is intentionally one transaction per committed `Text_input` event.
Consequently `.` repeats the last inserted committed text event, not a whole
insert session. It fully repeats operations such as `dw`; paste is deliberately
not repeatable in M3 because placement is context-sensitive.

## Runtime bindings inspection

`zenbu-headless bindings vim` and `bindings-session` report the current
model-owned `Input_rule` values. NORMAL reports operator prefixes; pending
DELETE/CHANGE/YANK state reports motions, text-object prefixes, and
cancellation. The runtime report is authoritative for registered inputs; this
document explains their model semantics.
