# Terminal presentation profiles

Zenbu keeps terminal chrome in a pure `zenbu.view.Presentation` profile. A
profile may allocate a line-number gutter and choose the status-row density;
it does not receive input, create widgets, inspect mutable terminal state, or
mutate a document. The renderer still produces terminal-independent frames,
so profiles do not affect transactions, model behavior, replay, or extension
authority.

Select one at launch:

```sh
zenbu --presentation default FILE
zenbu --presentation numbered FILE
zenbu --presentation relative FILE
zenbu --presentation minimal FILE
zenbu --presentation bare FILE
zenbu --presentation buffered FILE
```

`default` preserves the original no-gutter, detailed status row. `numbered`
uses one-based absolute line numbers; `relative` uses zero for the primary
line and the absolute distance for other visible lines. `minimal` keeps a
short status row, `bare` allocates every terminal row to source rendering, and
`buffered` adds one host-owned buffer line above the ordinary detailed
presentation.

Use a TOML file to combine the two policies:

```toml
name = "selection-editor"
line_numbers = "relative" # none, absolute, or relative
status_line = "minimal"   # detailed, minimal, or hidden
buffer_line = "visible"   # visible or hidden
```

Unknown fields and values are rejected before the terminal starts. A gutter
reduces only the text canvas width and shifts the physical cursor projection;
horizontal viewports remain in document display columns, never gutter columns.
If a terminal is narrower than the gutter, Zenbu renders a safe cursorless
frame rather than using invalid coordinates.

Helix documents `editor.line-number = "relative"` as a supported terminal
configuration, which is the cross-product pressure case for this policy:
[Helix configuration](https://docs.helix-editor.com/master/configuration.html).
The profile is useful for other terminal-editor workloads, but it is not a
claim of their complete display systems.

Switch to a built-in profile or validated TOML profile while Zenbu is running
through `Ctrl-P` → `view.presentation.switch`. The profile argument uses the
same `default|numbered|relative|minimal|bare|buffered|PATH` form as
`--presentation`.
The change is host-owned: it redraws chrome without changing a document,
selection, history, model state, or replay result.

Each pane may instead inherit that session presentation or choose a bounded
built-in override without a buffer line. The workspace-wide buffer line and
custom presentation TOML files remain session-level policy. See [pane-local
display options](VIEW_OPTIONS.md).

When `buffer_line = "visible"`, Zenbu reserves the top terminal row for a
bounded workspace summary. It lists open local buffers by stable numeric id,
brackets the current buffer, and adds `*` to dirty buffers. Labels use an
explicit buffer name when present, otherwise the file basename or `[No Name]`;
the row truncates at the terminal width. It is deliberately not a clickable or
scriptable tab-widget API: clicking the row is ignored, all pointer source
coordinates begin below it, and normal buffer switching remains
`workspace.buffer.switch`, `workspace.buffer.next`,
`workspace.buffer.previous`, or the command palette. The line only shifts
canvas geometry and cursor projection; it does not change document contents,
selections, history, model state, or authority.

This supports a small visual workload shared by terminal editors with visible
buffer bars without claiming their tab implementations. See [Micro commands](https://github.com/micro-editor/micro/blob/master/runtime/help/commands.md),
[Helix commands](https://docs.helix-editor.com/commands.html), and the
[GNU Emacs Tab Bars manual](https://www.gnu.org/software/emacs/manual/html_node/emacs/Tab-Bars.html).

## Deterministic frame snapshots

[`test/test_presentation_snapshots.ml`](../test/test_presentation_snapshots.ml)
compares checked-in frames under [`test/fixtures/presentation`](../test/fixtures/presentation).
They serialize frame dimensions, cursor coordinates, every cell's text/display
width/semantic style, and the active semantic palette. They deliberately do
not contain terminal escape sequences, platform fonts, or screenshots.

| fixture | classification | evidence and retained boundary |
| --- | --- | --- |
| `zenbu-owned-relative-unicode-diagnostics` | Zenbu-owned profile | relative gutter, Unicode cell width, selection/diagnostic precedence, status message, and dark semantic palette |
| `helix-style-relative-page-adapter` | Helix-style adapter | checked page navigation with relative chrome; it is not Helix view-mode or theme parity |
| `micro-style-buffered-split-adapter` | Micro-style adapter | buffer line, focused vertical split, selected Unicode text, and invalid-theme fallback retaining the dark palette; it is not Micro tab or terminal parity |
| `emacs-style-numbered-split-adapter` | Emacs-style adapter | numbered horizontal split and bounded kill/yank adapter path; it is not an Emacs window/display implementation |
| `zenbu-owned-numbered-tiny` | Zenbu-owned profile | safe cursorless gutter-only terminal boundary |

An adapter view request, such as the Helix-style page command, keeps its
explicit viewport through rendering. A later caret movement returns that pane
to cursor-following behavior. This keeps page navigation visual rather than a
document mutation.

Each snapshot embeds a nonempty review reason. There is intentionally no
automatic snapshot-update command: a visual-frame change must update the
checked-in semantic data and its fixture definition's review reason for normal
review. Run it directly with:

```sh
dune exec test/test_presentation_snapshots.exe
```

Editor-owned status-line functions, interactive tabs, arbitrary widgets,
minimaps, GUI rendering, mouse menus, terminal font control, and exact
Vim/Helix/Kakoune/Micro/Emacs appearance are intentionally outside this
contract. Runtime theme selection has the adjacent, equally host-owned
`view.theme.switch` command; see [Themes](THEMES.md).
