# Vim compatibility model

`zenbu.vim-style` is a compatibility stress test for the public editing-model
API. It is not Zenbu's privileged editor implementation: its modal grammar
turns logical input into `Model_effect` values and has no direct document,
history, terminal, or search-state access.

## States

| state | behavior |
| --- | --- |
| `NORMAL` | counts, motions, operators, find, paste, search requests, history, and repeat |
| `INSERT` | committed UTF-8 text inserts; `Ctrl-w`, `Ctrl-u`, and `Ctrl-r{slot}` apply insert-mode edits |
| `REPLACE` | committed text replaces the next UTF-8 scalar, or inserts at document end |
| `DELETE…`, `CHANGE…`, `YANK…` | waits for a motion or word text object |
| `VISUAL` / `VISUAL LINE` | motions extend characterwise or linewise selections; `d`, `c`, and `y` act on them |
| pending find/replace/register states | wait for one scalar or clipboard-slot key without terminal coupling |

## Supported commands

| input | semantic behavior |
| --- | --- |
| `h` / `j` / `k` / `l` | UTF-8-safe scalar and adjacent-line movement |
| `w` / `b` / `e` | next-word, previous-word, and word-end movement |
| `0` / `^` / `$` / `gg` / `G` | line and document boundaries |
| `f{char}` / `F{char}` / `t{char}` / `T{char}` | scalar find motions; `;` repeats and `,` reverses the latest find |
| `d{motion}` / `c{motion}` / `y{motion}` | operator motions, including `dd`, `cc`, `yy`, `df{char}`, `cf{char}`, and `yf{char}` |
| `diw` / `daw` / `ciw` / `caw` | inner/around current-word text objects |
| `D` / `C` / `S` | delete to line end, change to line end, or change the current line |
| `x` / `X` / `s` | delete next scalar, previous scalar, or delete next scalar then insert |
| `i` / `a` / `I` / `A` / `o` / `O` | Vim-style insert, append, and open-line entry points |
| `r{char}` / `R` | replace one scalar or enter replace mode |
| `v` / `V` | characterwise or linewise visual selection |
| `p` / `P` / `"a` | paste before/after and select a named internal clipboard slot |
| `Ctrl-r{slot}` in insert | insert a named internal clipboard slot at the caret |
| `/` / `?` / `n` / `N` | request forward/backward host literal search and repeat it |
| `u` / `Ctrl-r` / `.` | shared undo, redo, and semantic repeat |

Counts combine across an operator and its motion. Commands reject unavailable
targets atomically rather than silently clipping at a document boundary.

## API pressure boundaries

Find operators need an arbitrary selection calculated from a model-owned
character search, but still must commit one ordinary transaction. The generic
`Apply_to_selections` effect exists for this boundary: it validates supplied
UTF-8-safe offsets, then applies a shared transformation or clipboard copy in
the model runtime. `df{char}` therefore remains one undoable transaction rather
than a private Vim mutation or an intermediate selection-history entry.

Likewise, `/` and `?` emit a generic search request. The terminal host owns the
literal query prompt, highlighting, result persistence, and selection
provenance; the Vim model only chooses forward or backward direction. Other
models can use the same effect without depending on terminal code.

## Deliberate limits

This is not a full Vim clone. It currently excludes Ex/command-line commands,
full Vim-compatible macro/register behavior, marks, mappings, registers beyond
Zenbu's internal characterwise and linewise slots, blockwise visual mode, text
objects beyond words, regex search, full desired-column behavior, multi-buffer
workflows, and Vimscript/plugin compatibility. Zenbu's generic session keyboard
macro store is transient and bounded (`@` by default). The supplied model maps
`q{register}` to start recording, bare `q` to stop, and `@{register}` to replay
one Unicode-scalar register name; an ordinary positive Vim count repeats
`@{register}` through the host's 1,024-iteration/65,536-event replay budget.
It does not implement uppercase/global register behavior, persistence, or
macro editing. The shared `.` repeat facility records ordinary semantic intents;
model-calculated find operators such as `df{char}` do not yet establish a
repeat source. The explicit limits make additions useful API tests instead of an
accidental second editor kernel.

`zenbu-headless bindings vim` and `bindings-session` report the runtime input
rules. The model tests are the executable compatibility baseline; each added
command must retain normal transaction/history/provenance behavior.
