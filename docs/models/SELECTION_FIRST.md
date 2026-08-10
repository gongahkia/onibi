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

## Deliberate limits

There is no syntax-aware selection, search UI, multiple-cursor add-next UI,
selection split command, block selection, grapheme/display-cell navigation, or
Kakoune/Helix compatibility promise. Clipboard slots and history/repeat use the
same runtime services as the Vim-style model.

## Runtime bindings inspection

`zenbu-headless bindings selection` and `bindings-session` render the current
selection-first `Input_rule` values. Its visible-selection grammar remains
different from Vim's pending-operator grammar even though both reports use the
same generic format.
