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
| `Ctrl-P` | command palette over active builtin, Lua, and plugin commands |
| `Alt-M` | live editing-model picker |
| `Alt-H` / `Ctrl-O` | metadata-derived help / latest-`why` inspector |

Search retains a literal UTF-8 query, pre-search selection, highlight ranges,
and current result. It moves through an ordinary semantic `set-selections`
intent, refreshes after document changes, restores the pre-search selection on
prompt cancellation, and is inspectable through `Session.Search` or
`zenbu-headless search-session`. Its descriptors are `search.start`,
`search.next`, and `search.previous` with provider `zenbu.app`, so they appear
in `commands`, `describe command`, bindings, and the same palette as model,
Lua, and plugin commands. It is not regex/project search or a model-specific
grammar. The palette filters descriptor id/title/summary/provider; commands
needing arguments remain discoverable but M10 has no argument-form prompt.

The model picker preserves history, document/selections, clipboard, command and
semantic registries, syntax service, trace/profiler handles, execution identity,
and repeatable semantic intents. It initializes the new model's private grammar
state, so a pending operator/shrink stack never leaks across models.

## Backend and rendering

`terminal/Backend` is the only Notty user. It owns raw alternate-screen mode,
cursor restoration, and a partial-creation cleanup fallback; no Notty value
reaches session, models, view, or kernel. Mouse is disabled.

Bracketed paste is enabled. Notty provides start/end markers; the adapter
collects intervening printable UTF-8, Enter, and Tab events into one
`Terminal.Event.Paste`. `Input_decoder` creates one public `Text_input` only
when generic `Model_status.input_mode` is `Text_entry`; command grammars ignore
it. This remains best-effort because terminal paste markers are best-effort
protocol data.

The view projects immutable context into cells. Uuseg supplies grapheme
segmentation, Uucp a width hint, tabs expand every four columns, and controls
render as caret notation. `Syntax.Highlight` reaches the view as ranges/classes
only. Style precedence is **selection > search > syntax > plain**. Unknown
languages have no spans and render plain. The viewport follows the primary
selection; soft wrapping, mouse selection, capability probing, and exact emoji
width remain out of scope.

System clipboard commands are deliberately deferred: platform-specific
`wl-copy`, `xclip`, and `pbcopy` discovery does not belong in the semantic
runtime. Zenbu's existing internal clipboard slots remain available to models.

## Persistence and extensions

Save/save-as write and fsync an exclusive adjacent temporary file, preserve an
existing target mode, close it, then rename it over the target. Save-as
intentionally replaces an existing destination after the same atomic write;
there is no interactive overwrite confirmation in M10. Success updates active
path and saved version/contents. Zenbu does not fsync the parent directory or
detect external modifications.

Plugin inspection shows manifest, runtime, capabilities, contributions, limits,
last error, and health. A Component fuel/memory/trap failure makes its runtime
`unavailable`; repeated callbacks return `extension-runtime-unavailable`
without entering Wasmtime. Reload creates a fresh healthy generation on success.
