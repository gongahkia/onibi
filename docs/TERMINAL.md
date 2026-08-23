# Terminal host

`zenbu` is the interactive host; it validates a UTF-8 file before terminal
mode, or opens an unnamed buffer. An explicitly requested configuration or
plugin package is also staged before terminal initialization: a bad request
exits with its concise structured error rather than hiding it behind a
non-terminal error. `zenbu_headless` remains the non-TTY, deterministic
tooling surface.

## Host interactions

The session owns host UI and file policy above all models. These reserved keys
take priority over model/configuration bindings.

| Key | action |
| --- | --- |
| `Ctrl-S` / `Ctrl-Shift-S` | save / prompt for atomic save-as |
| `Ctrl-Q` | quit; a dirty buffer requires a second press to force quit |
| `Alt-R` / `Ctrl-Alt-R` | staged Lua/plugin reload |
| `Ctrl-F` | literal Unicode search; `Ctrl-G` / `Ctrl-Shift-G` move matches |
| `Ctrl-P` | command palette; prompts for declared command arguments |
| `Alt-M` | live editing-model picker |
| `Alt-H` / `Ctrl-O` | metadata-derived help / latest-`why` inspector |
| `Ctrl-Space` | explicit language completion |

Search retains its query kind, UTF-8 query, pre-search selection, highlight
ranges, and current result. `Ctrl-F` and model `Request_search` effects start
the literal variant. The `search.regexp` palette command and permitted trusted
adapter binding target start the incremental OCaml `Str` variant. It accepts
only non-empty, non-overlapping matches whose byte endpoints are UTF-8 code
point boundaries; an invalid, zero-width, or boundary-splitting match leaves
the selection unchanged and the prompt open for correction. Both variants move
through an ordinary semantic `set-selections` intent, refresh after document
changes, restore the pre-search selection on prompt cancellation, and are
inspectable through `Session.Search` or `zenbu-headless search-session`.
The palette-only `search.replace.literal` and `search.replace.regexp`
descriptors each collect `query` and `replacement` text. They recompute the
current buffer's matches and commit all accepted non-overlapping matches through
one `replace-ranges` transaction, with `search.matches`/`replace-all`
provenance. The regexp variant has the same non-empty and UTF-8-endpoint checks
as regexp search; `$1` and every other replacement byte sequence are literal.
Their descriptors join `search.start`, `search.regexp`, `search.next`, and
`search.previous` with provider `zenbu.app`, so they appear in `commands`,
`describe command`, and the same palette as model, Lua, and plugin commands.
They are not adapter-bindable because both required arguments remain host prompt
input. This is not product-regexp, query-replace, or project-search
compatibility. The
palette filters descriptor id/title/summary/provider. A selected descriptor
with parameters enters a host-owned prompt for each parameter; `Escape`
abandons the invocation. Text parameters accept committed UTF-8.
Built-in selector parameters use canonical selector IDs such as `document` and
transformations use `select`, `delete`, `collapse-to-start`, `collapse-to-end`,
or `replace:<text>`. The completed values become a normal
`Command_invocation`; they are not parsed as an Ex command or applied through a
special mutation path.

The Vim compatibility model may request this same host interaction with `/`
and `?`, then request next/previous results with `n` and `N`. The model selects
only direction; the terminal retains prompt, query, rendering, and selection
provenance ownership.

The model picker preserves history, document/selections, clipboard, command and
semantic registries, syntax service, trace/profiler handles, execution identity,
and repeatable semantic intents. It initializes the new model's private grammar
state, so a pending operator/shrink stack never leaks across models.

The palette's `workspace.pane.grow-width`, `workspace.pane.shrink-width`,
`workspace.pane.grow-height`, `workspace.pane.shrink-height`, and
`workspace.panes.balance` commands adjust the layout tree without altering a
document, selection, history, or viewport. A grow/shrink request moves the
focused pane's nearest vertical or horizontal divider by one cell; if none can
move while preserving one cell for each child, it is rejected. Ratios survive
terminal resizing and balance resets every split to equal proportions. This is
deliberately a small generic layout contract, not per-editor minimum-window
policy, numeric prefixes, divider dragging, or layout persistence.

## Backend and rendering

`terminal/Backend` is the only Notty user. It owns raw alternate-screen mode,
cursor restoration, mouse reporting, and a partial-creation cleanup fallback;
no Notty value reaches session, models, view, or kernel.

Mouse reporting is enabled for the editor canvas. A primary press focuses the
pane and places its caret; `Shift`-primary extends the active selection, and a
primary drag creates a single grapheme-safe selection. The wheel scrolls the
pane three source lines at a time without moving its selection. A subsequent
keyboard edit or movement resumes cursor-following. Middle and secondary
buttons, mouse clipboard integration, clicks on the status row, and all mouse
interaction while a prompt, palette, or inspector is visible are intentionally
ignored. Terminal mouse events become typed `Input_event` values, but Session
owns their host semantics and applies selections through ordinary checked
semantic effects rather than exposing terminal state to editing models.

Bracketed paste is enabled. Notty provides start/end markers; the adapter
collects intervening printable UTF-8, Enter, and Tab events into one
`Terminal.Event.Paste`. `Input_decoder` creates one public `Text_input` only
when generic `Model_status.input_mode` is `Text_entry`; command grammars ignore
it. This remains best-effort because terminal paste markers are best-effort
protocol data.

The view projects immutable context into cells. Uuseg supplies grapheme
segmentation, Uucp a width hint, tabs expand every four columns, and controls
render as caret notation. `Syntax.Highlight` reaches the view as ranges/classes
only. M11 diagnostics likewise reach it as owned ranges/classes. Style
precedence is **selection > search > diagnostic > syntax > plain**. Unknown
languages have no spans and render plain. The viewport follows the primary
selection unless deliberately scrolled with the wheel; soft wrapping,
multi-click/word selection, terminal capability probing, and exact emoji width
remain out of scope.

System clipboard commands are deliberately deferred: platform-specific
`wl-copy`, `xclip`, and `pbcopy` discovery does not belong in the semantic
runtime. Zenbu's existing internal clipboard slots remain available to models.

## Persistence and extensions

Save/save-as write and fsync an exclusive adjacent temporary file, preserve an
existing target mode, close it, then rename it over the target. Save-as
intentionally replaces an existing destination after the same atomic write;
there is no interactive overwrite confirmation in M11. Success updates active
path and saved version/contents. Zenbu does not fsync the parent directory or
detect external modifications.

M11 starts an optional language service for a saved path selected by the
language registry (the default is `ocamllsp` for OCaml). The backend waits on
stdin and every open buffer's client wakeup descriptor, so an idle terminal
redraws when diagnostics or feature replies arrive. Hover and completion are
host overlays; diagnostic navigation and accepted language edits use normal
selection effects/transactions. Definitions can open a local target buffer,
and rename or `workspace/applyEdit` can update every already-open saved target
all-or-none. `language.status` is available through the palette and inspector.
See [Language services](LANGUAGE_SERVICES.md).

Plugin inspection shows manifest, runtime, capabilities, contributions, limits,
last error, and health. A Component fuel/memory/trap failure makes its runtime
`unavailable`; repeated callbacks return `extension-runtime-unavailable`
without entering Wasmtime. Reload creates a fresh healthy generation on success.
