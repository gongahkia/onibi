# Selection-first model

`zenbu.selection-first` is a Kakoune/Helix-inspired model, not a remapped Vim
subset. Selectors visibly update the active `Selection_set`; transformations
then operate on that set. It uses only `zenbu.model_api`.

## States and commands

| input | semantic behavior |
| --- | --- |
| `h` / `j` / `k` / `l` | visibly select previous/adjacent/next scalar target |
| `w` / `b` / `e` | visibly select next-word / previous-word / word-end target |
| `W` | select the current word class |
| `0` / `^` / `$` / `L` | select line start / first nonblank / line end / current line |
| `gg` / `G` | select document start / document end targets |
| `*` | select all non-overlapping literal occurrences of the primary selection |
| `d` / `x` | delete non-empty current selections together |
| `c` | delete current selections and enter insert |
| `y` | copy current selections to the selected clipboard slot |
| `p` / `P` | replace current selections / paste before their starts |
| `i` | enter insert; committed text replaces current selections |
| `,` | retain only the primary selection |
| `)` / `(` | make the next / previous selection in document order primary |
| `Alt-_` | merge selections that touch at a document boundary |
| `<count> Alt-)` / `<count> Alt-(` | rotate non-empty selection contents forward / backward; a count partitions adjacent groups |
| `Alt-;` / `Alt-:` | flip every selection's anchor/head / normalize every selection forward |
| `"a` | choose clipboard slot `a` for the next copy or paste |
| `u` / `Ctrl-r` / `.` | undo / redo / semantic repeat |
| `Escape` | collapse current selections to their ends |

Digits repeat a selector operation. The model intentionally treats a direct
delete of only empty carets as a no-op instead of creating a no-content history
change. Its essential contrast with Vim-style grammar is visible in the shared
equivalence case:

```text
vim-style:       d w
selection-first: w d
semantic target: next-word + delete
```

`W * d` demonstrates model-neutral multi-selection editing. Starting at the
first `foo` in `foo bar foo baz foo`, `W` selects it, `*` creates three literal
selections, and `d` resolves one shared delete transaction for all three.

The command palette additionally exposes reusable selection commands:
`editor.selection.select-regex`, `editor.selection.split-regex`,
`editor.selection.keep-regex`, `editor.selection.remove-regex`,
`editor.selection.merge-consecutive`, `editor.selection.rotate-primary-forward`,
`editor.selection.rotate-primary-backward`,
`editor.selection.rotate-contents-forward`,
`editor.selection.rotate-contents-backward`, `editor.selection.flip`, and
`editor.selection.ensure-forward`. The four regex commands prompt for a
pattern; a Lua adapter can bind one in the selection scope, for example:

```lua
zenbu.bind {
  input = "S",
  command = "editor.selection.split-regex",
  scope = "model:zenbu.selection-first:select",
}
```

Regexes use OCaml's `Str` dialect, operate on each current selection, reject
zero-width matches, and reject a result that ends inside a UTF-8 code point.
They produce ordinary `set-selections` intents, so they are undoable,
inspectable, and replay-safe through the same path as every other selection
change.

## Deliberate limits

There is no model-private syntax-aware selection, multiple-cursor add-next UI,
block selection, grapheme/display-cell navigation, or Kakoune/Helix
compatibility promise. Rotation requires at least two non-empty selections and
uses Zenbu's document order. A count partitions the set into independent,
equal-size groups and is rejected unless it divides the selection count; its
exact post-edit selection state still differs from native editors. `Str` is not
the regex dialect of either editor, and Zenbu's half-open ranges are not
Kakoune's inclusive anchor/cursor selections. The model-neutral terminal search
UI is available through `Ctrl-F` without changing this model's grammar.
Clipboard slots and history/repeat use the same runtime services as the
Vim-style model.

## Runtime bindings inspection

`zenbu-headless bindings selection` and `bindings-session` render the current
selection-first `Input_rule` values. Its visible-selection grammar remains
different from Vim's pending-operator grammar even though both reports use the
same generic format.
