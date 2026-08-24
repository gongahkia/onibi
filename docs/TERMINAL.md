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
`search.query-replace.literal` and `search.query-replace.regexp` collect the
same arguments, then retain a version-bound non-overlapping match plan in a
host-owned review state. `s` skips, `r` replaces the current match through one
checked transaction, `a` replaces every remaining planned match through one
checked transaction, and `q`/`Escape` stops without changing unreviewed
matches. A version change outside the review cancels the remaining plan before
an edit. Replacements are literal for both modes, so capture templates are not
supported. These are not adapter-bindable because their arguments and review
keys remain host input. This is not product-regexp or project-search
compatibility. The
palette filters descriptor id/title/summary/provider. A selected descriptor
with parameters enters a host-owned prompt for each parameter; `Escape`
abandons the invocation. Text parameters accept committed UTF-8.
Built-in selector parameters use canonical selector IDs such as `document` and
transformations use `select`, `delete`, `collapse-to-start`, `collapse-to-end`,
or `replace:<text>`. The completed values become a normal
`Command_invocation`; they are not parsed as an Ex command or applied through a
special mutation path.

`editor.command-line` is a separate, deliberately small protocol available in
the palette and as a permitted trusted-adapter binding target. It opens a
colon-prefixed text prompt that accepts `:exact.command-id argument ...`.
Command IDs must match exactly one currently active palette descriptor; prefix
abbreviations, aliases, product commands, and recursive `editor.command-line`
dispatch are rejected. Each token is converted by the same declared parameter
kind used by the palette before the normal command/host invocation path runs.
Unknown IDs, missing required arguments, extra arguments, and invalid typed
values leave the prompt open without an effect.

The grammar is bounded to 4,096 bytes and splits only on ASCII spaces. Quoting,
escaping, shell expansion, pipes, redirection, command history, and completion
are intentionally deferred. Consequently a text argument that contains spaces
must use the palette's typed prompt. This is a generic declarative command-line
adapter protocol, not a Vim Ex, Micro command-bar, or Kakoune command-language
compatibility claim. `Escape` cancels the line.

Background-job output is a host Jobs inspector and an explicitly opened static
report buffer, not a terminal pane. The streaming job contract has no stdin,
PTY, terminal emulation, terminal resize, or focused process input; its
isolated process group exists only so the host can cancel and clean it up. An
interactive PTY would require a separate terminal-buffer ownership and input
contract, so it remains deferred rather than being implied by streaming output.

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
terminal resizing and balance resets every split to equal proportions. A primary
press on an exact visible divider begins a host-owned drag of that original
split; buffer lines, status rows, and document canvas cells are not targets.
The gesture changes neither document nor selection state. This is deliberately
a small generic layout contract, not per-editor minimum-window policy, numeric
prefixes, or product-specific window policy.

`workspace.project.root.set` accepts one palette path and canonicalizes a
readable directory as the host-owned project root. `workspace.file-picker` then
provides a text-filtered, deterministic list of files below that root; it has no
model effect or adapter binding, so models, Lua, and Components receive neither
paths nor filesystem handles. Discovery skips dot-prefixed names, unreadable
files/directories, files containing NUL in its first 8 KiB, and every symlink;
the selected root itself may be a symlink because it is canonicalized first.
The picker revalidates a selected non-empty relative path without `.` or `..`
components, rejects a target outside the canonical root, then delegates to the
normal buffer opener. Therefore an already-open picked file is reused and its
read/snapshot policy remains the existing buffer-load policy. This is a bounded
local navigation surface.

`workspace.project.search` accepts a literal query through the palette and
searches the same canonical root. Its host-owned result view is bounded to 512
files, 256 results, the first 1 MiB of each file, and 32 MiB total; the latest
query, scan counts, truncation state, and limits are available through the
project-search inspector. Search uses the picker's discovery policy: dot-prefixed names are
ignored (it does not read `.gitignore`), and unreadable, NUL-containing,
symlink, and invalid-UTF-8 files are skipped. Results contain a relative path,
one-based line, and UTF-8-safe byte offset. `Enter` revalidates the path and
literal at that offset before the normal buffer opener focuses a selection;
stale results are rejected. `Escape` only closes the result view, so cancelling
does not mutate a document. There is no shell/ripgrep integration, replacement,
file watching, or filesystem capability exposed to models, Lua, or Components.

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

The focused pane can additionally hold version-bound manual or syntax-derived
fold ranges. The view keeps a header source row and replaces later covered rows
with a dim folded-line suffix; the renderer, viewport, and pointer path all
use that same projection. This never alters source bytes, selections, history,
or model state. [Folding](FOLDING.md) specifies validation, invalidation,
cursor, pointer, search, and diagnostic behavior.

Embedding hosts can supply another view-only projection: bounded
snapshot-bound trailing annotations and virtual rows. A virtual row participates
in scrolling but has no pointer target, and every visible annotation has a
textual fallback in addition to its theme role. [Display decorations](DECORATIONS.md)
defines the provider, stale-data, ordering, and limit contract.

System clipboard commands are deliberately deferred: platform-specific
`wl-copy`, `xclip`, and `pbcopy` discovery does not belong in the semantic
runtime. Zenbu's existing internal clipboard slots remain available to models.

## Persistence and extensions

Save/save-as write and fsync an exclusive adjacent temporary file, preserve an
existing target mode, close it, then rename it over the target. A normal save
compares the current target's file identity and contents with the baseline
captured when it was opened or last saved; it refuses a changed, replaced, or
missing target. Save-as intentionally replaces its destination after the same
atomic write and establishes a new baseline; there is no interactive overwrite
confirmation in M11. Success updates active path and saved version/contents.
Zenbu does not fsync the parent directory.

Every saved local buffer also registers with a host-owned polling watcher. Its
worker compares the saved identity/content baseline in the background and
writes only to a private wakeup pipe; Session drains events on the terminal
thread and never forwards them to a model. A changed inode is `replaced`, a
missing target is `deleted`, same-inode content divergence is `modified`, and
backend `overflow`/`failure` events are retained verbatim in `Session.File_watches`.
At most 256 undrained events are retained; exceeding that limit records one
synthetic `overflow` event until the terminal drains the queue.
The portable polling backend identifies a same-directory rename by the saved
inode; a move outside that directory is reported as deletion. The event
contract also carries `renamed` for native backends and the controllable test
source.
Both clean and dirty buffers are retained unchanged: a notice states the
classification and whether the retained buffer is clean or dirty. There is no
automatic reload, overwrite, merge, document transaction, or model callback.

`workspace.layout.save` and `workspace.layout.restore` are palette commands
with a required JSON-file `path`. Layout save writes schema version 2 and only
accepts clean, file-backed buffers whose saved file baseline still matches the
filesystem. It records host-owned buffer identifiers/paths/display names,
first-party model identifiers, split ratios, focused pane, viewports, and
ordered selections per `(pane, buffer)` pair. It never serializes document
contents, unsaved buffers, model internals, Lua/Component state, terminal
handles, LSP objects, plugins, jobs, or history. Restore also accepts schema
version 1, defaulting its absent viewports to origin/follow-cursor behavior.
It validates the whole JSON shape, schema, split tree, ratios, file reads,
language identifiers, UTF-8 selection boundaries, and pane/buffer references
before replacing the current session. A missing file, stale schema, malformed
layout, or invalid offset leaves the existing session unchanged and reports a
structured error. Layout files are local-machine session convenience, not
cross-machine synchronization, a project format, or a process/product-state
format.

M11 starts an optional language service for a saved path selected by the
language registry (the default is `ocamllsp` for OCaml). The backend waits on
stdin and every open buffer's client wakeup descriptor, so an idle terminal
redraws when diagnostics or feature replies arrive. Hover, completion, and code
actions are host overlays; diagnostic navigation and accepted language edits
use normal selection effects/transactions. Definitions can open a local target
buffer, and rename, checked code actions, or `workspace/applyEdit` can update
every already-open saved target all-or-none. Document and range formatting use
fixed two-space options and normal transactions. Code-action server commands
are explicitly denied. `language.status` is available through the palette and
inspector. See [Language services](LANGUAGE_SERVICES.md).

Plugin inspection shows manifest, runtime, capabilities, contributions, limits,
deadline, last error, and health. A Component fuel/memory/trap/deadline failure
makes its runtime `unavailable`; repeated callbacks return
`extension-runtime-unavailable` without entering Wasmtime. Component command
and event completions wake the terminal through the normal session descriptor
set and are committed only after their source snapshot revalidation. Reload
creates a fresh healthy generation on success and discards old pending results.
