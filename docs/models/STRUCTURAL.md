# Structural model

The M5 structural model is a third editing philosophy: it navigates syntax
relationships and makes the selected node visible through the existing
selection set before transforming it. It is not a Vim AST-motion layer, a
language-specific OCaml refactoring tool, or an alternate mutation path.

It links only to `zenbu.model_api` and `zenbu.syntax`. Every structural target
becomes an ordinary `set-selections` intent; delete, change, copy, paste,
undo, redo, and repeat use existing runtime services.

## Key grammar

| input | action |
| --- | --- |
| `f` | focus the smallest named node at the primary caret/selection |
| `↑` / `↓` | select parent / first named child |
| `→` / `←` | select next / previous named sibling |
| `e` / `r` | expand to parent / shrink through model-local expand history |
| `m` | select all named siblings with the current node kind |
| `x` | delete visible structural selection(s) |
| `c` | delete visible selection(s), then enter text input |
| `y` / `p` | copy visible selection(s) / replace with unnamed clipboard entry |
| `i` | enter ordinary text input at the current selection(s) |
| `u` / `Ctrl-R` | shared undo / redo |
| `.` | repeat the latest shared textual semantic intent |
| `Escape` | leave structural text input |

`STRUCT | <kind>` appears in generic status after a focused navigation target;
same-kind multi-selection adds the count. `NO SYNTAX` appears when the current
session has no syntax snapshot. There is no fallback that pretends byte-based
movement is structural.

## Selection and transformations

For a focused OCaml top-level declaration, `m` selects sibling declarations of
the same grammar kind. The selections are ordinary, non-overlapping Zenbu
selections, so `x` creates one atomic `delete-selected-ranges` transaction and
one undo step. This is the same deletion machinery used by the Vim-style and
selection-first models.

`c` first issues the same shared delete intent, then switches generic status to
`Text_entry`. Committed text creates ordinary insert intents and may leave the
program invalid. Escape returns to navigation; the next structural command
receives the synchronously refreshed syntax snapshot. The model intentionally
does not require valid source between keystrokes.

Expand records only prior anchor/head offsets in model-local state, never a
syntax node or backend pointer. Its stack is valid across structural
selection-only transitions and invalidates on a mismatched document version,
which safely discards it after edits, undo, or redo.

`repeat` is deliberately modest: once a structural delete/change has resolved
to a shared textual intent, `.` repeats that concrete intent over the current
ordinary selections. It does not re-evaluate a prior syntax selector against
different source. Concrete transactions therefore remain deterministic for
M1 replay without a live parser.

## Limits

M5 supports structural navigation for registered OCaml and JSON sessions, but
does not special-case their grammar kinds. It offers only first child and
named-sibling traversal, not full pre-order traversal, named fields, arbitrary
descendant queries, language-specific commands, highlighting, or refactoring.
Unknown-language buffers should use Vim-style or selection-first editing; a
structural session reports `NO SYNTAX` without mutating text.

## Runtime bindings inspection

`zenbu-headless bindings structural` and `bindings-session` render current
structural rules. Syntax-dependent rules explicitly say that syntax is needed,
but the generic inspector sees only stable syntax selector ids and never a
parser node or backend type.
