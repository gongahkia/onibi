# M7 trusted-local Lua configuration

M7 is an experimental configuration overlay for a local Zenbu installation.
It pressure-tested the public semantic APIs that M8 now stabilizes for plugin
packages. M7 configuration remains supported, but is not a plugin package,
package manager, sandbox, or stable third-party compatibility promise. For the
stable package contract, use [extensions](EXTENSIONS.md).

## Loading and reload

`zenbu` loads `$XDG_CONFIG_HOME/zenbu/init.lua`, falling back to
`$HOME/.config/zenbu/init.lua`, unless `--config PATH` selects an explicit file
or `--no-config` disables configuration. A missing default file is a no-op; an
explicit missing or invalid file leaves a usable builtin session with a visible
error message. Fedora users need the `lua-libs` package, which supplies the
PUC Lua 5.4 shared library used by the private adapter.

`Ctrl-Alt-R` reloads the selected configuration. `Alt-R` or `Meta-R` is also
accepted because some terminal input protocols cannot report Ctrl-Alt on a
printable key. Zenbu first creates a fresh
Lua state, evaluates the complete file, validates every registration against
the builtin and staged registries, and constructs a replacement command and
semantic-behavior overlay. Only then does it install that overlay and dispose
the previous Lua state. A parse, evaluation, registration, collision, or
callback-validation failure leaves the previous working generation untouched.
Generation identity is monotonic only within one session. The default file is
looked up again at each reload, so a previously absent default may appear and a
deleted default reloads as no active script generation.

Use deterministic headless checks before opening the terminal host:

```sh
dune exec bin/zenbu_headless.exe -- config-check examples/m7-init.lua
dune exec bin/zenbu_headless.exe -- config-describe examples/m7-init.lua
dune exec bin/zenbu_headless.exe -- script-session examples/m7-init.lua test/fixtures/m7-wrap.session
```

`config-check` fully evaluates and validates the file but does not retain its
generation. `config-describe` reports registered commands, semantic ids,
bindings, and any `zenbu.model` id. `script-session` runs an inspectable
session fixture with the explicit configuration and prints the resulting text,
script-generation view, and history.

## Trust and authority

Configuration executes with Lua's standard libraries and a local module search
path beginning at the configuration file's directory (`require "name"` finds
`name.lua` there). Treat the file and adjacent Lua modules as fully trusted
local code. Do not auto-load a repository configuration, download scripts, or
point `--config` at untrusted content. M7 intentionally does not claim
filesystem, process, network, memory, or CPU isolation.

The Zenbu-specific authority is still constrained. Scripts receive copied
data, not mutable editor objects. They cannot call `Document.apply`, mutate
history, retain a `Selection_set`, obtain a terminal handle, access a
Tree-sitter value, or inject a raw transaction. Text changes and selection
changes must be expressed through the same data-only effects and semantic
behavior results used by first-party models. The model runtime resolves,
validates, commits, records provenance, updates syntax, and supports ordinary
undo/redo after the callback returns.

## Lua surface

The configuration sees one global table, `zenbu`. `zenbu.api_version` is `1`.
Registration calls are only meaningful while the configuration file evaluates:

```lua
zenbu.command {
  id = "user.uppercase-message",
  title = "Explain selection",
  description = "Show a normal semantic message.",
  parameters = {
    {
      name = "prefix",
      description = "Text to put before the selected text.",
      required = true,
      kind = "text",
    },
  },
  run = function(call)
    local selected = call.context.selections[call.context.primary]
    return {{ kind = "message", text = call.arguments.prefix .. selected.text }}
  end,
}

zenbu.selector {
  id = "user.document",
  title = "Document",
  description = "Select all document bytes.",
  run = function(call)
    return {
      selections = {{ anchor = 0, head = call.context.document.length }},
      primary = 1,
    }
  end,
}

zenbu.transform {
  id = "user.bracket",
  title = "Bracket",
  description = "Insert brackets around every selected range.",
  run = function(call)
    local edits = {}
    for _, selection in ipairs(call.arguments.selection_set) do
      local start = math.min(selection.anchor, selection.head)
      local stop = math.max(selection.anchor, selection.head)
      table.insert(edits, { start = start, stop = start, text = "[" })
      table.insert(edits, { start = stop, stop = stop, text = "]" })
    end
    return { edits = edits }
  end,
}

zenbu.command {
  id = "user.wrap-document",
  title = "Wrap document",
  description = "Compose registered semantic behavior.",
  run = function(_)
    return {{ kind = "apply", selector = "user.document", transformation = "user.bracket" }}
  end,
}

zenbu.bind { input = "Ctrl-K", command = "user.wrap-document", scope = "global" }
zenbu.on { event = "document-changed", run = function(call)
  return {{ kind = "message", text = "changed by " .. call.arguments.event }}
end }
```

Each `command`, `selector`, and `transform` requires a valid nonempty `id` and
a `run` function; title and description default to the id. Semantic descriptors
also accept `requires_syntax = true`. Ids collide with builtins and other
registrations as errors. Registrations cannot be removed individually: edit the
file and reload to replace the entire generation.

`zenbu.command` additionally accepts an optional `parameters` array. Each entry
has nonempty `name` and `description`, `required` (default `true`), and `kind`
(default `"text"`). The supported kinds are `"text"`, `"selector"`, and
`"transformation"`. Selecting the command in `Ctrl-P`, or resolving a binding
to it, opens one host prompt per parameter. Empty optional values are omitted;
an empty required value is rejected in place and `Escape` cancels the complete
invocation. The callback receives accepted values as `call.arguments[name]`.
Text is a Lua string, selector is its canonical ID string, and transformation
is `{ kind = "..." }` with `text` additionally present for `replace:<text>`.
This is still an ordinary typed command invocation: the script returns the same
declarative effects and retains no prompt or terminal authority.

`zenbu.bind` takes `input`, `command`, and optional `scope`. `input` is one to
sixteen logical input tokens separated by one ASCII space, for example
`"Ctrl-X Ctrl-K"`. A token is named-key input (`Escape`, `Enter`, `Backspace`,
`Tab`, `Delete`, arrows, `Home`, `End`, `PageUp`, or `PageDown`) or logical text with optional
`Ctrl-`, `Shift-`, `Alt-`, and `Meta-` modifiers. Write the text keys `Space`,
`Minus`, `Plus`, `Comma`, `Period`, or `Slash` by name when required inside a
sequence. Unmodified logical-text tokens preserve case, so `Q` and `q` are
distinct bindings; modifier spelling remains canonical (`Ctrl-X` and
`Ctrl-Shift-X`). `<text>` is a distinct wildcard for one committed `Text_input`
event; it does not match a logical key press. `Ctrl-X` remains a one-event
binding and is fully backward compatible.

A scope is `global`, `model:<model-id>`, or
`model:<model-id>:<status-id>`, or `mode:<id>`. A completed custom-mode binding
wins over a model-status binding, which wins over a model binding, which wins
over global. Declare a custom mode before using it:

```lua
zenbu.mode {
  id = "user.leader",
  title = "LEADER",
  description = "A transient project keymap.",
}

zenbu.bind { input = "Ctrl-X", command = "user.enter-leader", mode = "user.leader" }
zenbu.bind { input = "f", command = "user.format", scope = "mode:user.leader", mode = "" }
```

Set `initial = true` on at most one declared mode to make it the default map
for the first buffer and every subsequently opened buffer. This is applied on
session/buffer creation only; a configuration reload preserves the current
buffer's valid stack rather than re-entering an initial mode unexpectedly.

`mode = "id"` replaces the custom-mode stack before its command runs and
`mode = ""` clears it, preserving the original single-transient-map syntax.
For nested maps, use an explicit transition table:

```lua
zenbu.bind {
  input = "g",
  command = "user.enter-goto",
  scope = "mode:user.leader",
  mode = { action = "push", id = "user.goto" },
}
zenbu.bind {
  input = "h",
  command = "user.goto-home",
  scope = "mode:user.goto",
  mode = { action = "pop" },
}
```

The actions are `replace` and `push` (both require a declared `id`), `pop`,
and `clear`. A pushed mode overlays the maps below it: its completed binding
wins, but an otherwise unmatched key may resolve in the next lower custom map,
then the ordinary model map. An unmatched key with any custom mode active is
consumed rather than reaching the base model. Bare `Escape` pops the innermost
custom mode when it has no matching binding, while a mode-local `Escape`
binding takes precedence. A reload retains the full stack only when every
active id remains declared by the replacement generation; otherwise it clears
the stack. Stacks belong to their buffer: opening a buffer starts with no
custom modes unless the configuration declares an initial mode, while switching
views restores the target buffer's stack. This supports nested
leader/transient/minor-map patterns, but not yet arbitrary Emacs-style keymap
composition.

Modes default to `input_mode = "keys"`. A mode may instead set
`input_mode = "text"`, causing the terminal to emit committed Unicode text and
paste as `Text_input` rather than logical key presses. A `<text>` binding must
name a declared `kind = "text"` command parameter through `text_argument`:

```lua
zenbu.mode {
  id = "user.insert",
  title = "INSERT",
  description = "A minimal adapter-defined insert mode.",
  input_mode = "text",
}

zenbu.command {
  id = "user.insert-text",
  parameters = {{ name = "text", description = "Committed text.", kind = "text" }},
  run = function(call)
    return {{ kind = "insert", text = call.arguments.text }}
  end,
}

zenbu.bind {
  input = "<text>",
  command = "user.insert-text",
  text_argument = "text",
  scope = "mode:user.insert",
}
```

The host validates that there is exactly one `<text>` pattern, that its scope
is a declared `input_mode = "text"` custom mode, and that `text_argument`
names a text parameter on the target command. Captured text is passed as one
typed command argument; it never becomes ambient Lua state. This is sufficient
for adapter-defined insert-like modes. Declared custom modes remain a host-owned
binding stack; use `zenbu.model` when the editing grammar itself needs durable
state.

If a more-specific scope has an
incomplete sequence prefix, Zenbu holds that prefix; a less-specific sequence
can still resolve if the later event does not match the narrower candidate.
The first prefix event is never sent to the model. `Escape` cancels a pending
prefix; an unmatched suffix is consumed with an explanatory message rather
than leaking into the model. Identical or prefix-overlapping sequences in one
scope are a staging error, so a command and a prefix cannot be ambiguous.

Bindings are considered before model input. The host retains `Ctrl-S`,
`Ctrl-Shift-S`, `Ctrl-Q`, `Alt-R`, `Ctrl-Alt-R`, `Ctrl-F`, `Ctrl-G`,
`Ctrl-Shift-G`, `Ctrl-P`, `Ctrl-Space`, `Alt-M`, `Meta-M`, `Alt-H`, `Meta-H`,
and `Ctrl-O` as non-overridable controls; none may appear anywhere in a custom
sequence. The explicit host targets permitted to a script binding are
`config.reload`, `editor.macro.record`, `editor.macro.replay`,
`editor.kill-ring.cut`, `editor.kill-ring.yank`, `editor.clipboard.copy`,
`editor.clipboard.paste`, `editor.command-palette`, and the existing
`search.start`, `search.regexp`,
`workspace.split.vertical`, `workspace.split.horizontal`,
`workspace.pane.next`, `workspace.pane.close`, `workspace.pane.only`,
`workspace.pane.grow-width`, `workspace.pane.shrink-width`,
`workspace.pane.grow-height`, `workspace.pane.shrink-height`, and
`workspace.panes.balance`, plus the safe `workspace.buffer.new`,
`workspace.buffer.open`, `workspace.buffer.close`, `workspace.buffer.next`,
and `workspace.buffer.previous` operations, plus `view.scroll.*`,
`view.page.*`, and `view.center`. This is a closed
allow-list rather than arbitrary host-command invocation: it excludes save,
quit, force-quit, model switching, and language requests. A workspace-open
binding can only open the normal host prompt; it receives neither a path nor a
file handle. View commands receive no geometry and operate only on the focused
view. Resize commands carry only a signed one-cell delta; the host selects the
nearest matching divider and keeps at least one cell on either side. The macro
commands are generic session commands, so an editor adapter
can use its native-looking macro keys without making macro logic a model
privilege:

```lua
zenbu.bind { input = "Q", command = "editor.macro.record" }
zenbu.bind { input = "q", command = "editor.macro.replay" }
```

[`helix-adapter.lua`](../examples/helix-adapter.lua) demonstrates the same
boundary for selection-editor view navigation: `PageUp`, `PageDown`, `Ctrl-U`,
`Ctrl-D`, and `z z` request page movement or centering without changing the
selection or exposing renderer state. It is a focused workload fixture, not a
Helix compatibility configuration.

For example, the bundled
[`micro-adapter.lua`](../examples/micro-adapter.lua) runs on the Direct model
and maps Micro's documented `Ctrl-E` command bar and `Ctrl-W` split cycle to
the palette and `workspace.pane.next` respectively, plus selected-text
`Ctrl-X` to the bounded shared kill history:

```lua
zenbu.bind {
  input = "Ctrl-e",
  command = "editor.command-palette",
  scope = "model:zenbu.direct:direct",
}
zenbu.bind {
  input = "Ctrl-w",
  command = "workspace.pane.next",
  scope = "model:zenbu.direct:direct",
}
zenbu.bind {
  input = "Ctrl-x",
  command = "editor.kill-ring.cut",
  scope = "model:zenbu.direct:direct",
}
zenbu.bind {
  input = "Ctrl-c",
  command = "editor.clipboard.copy",
  scope = "model:zenbu.direct:direct",
}
zenbu.bind {
  input = "Ctrl-v",
  command = "editor.clipboard.paste",
  scope = "model:zenbu.direct:direct",
}
```

`editor.macro.record` starts recording when idle and stops/stores a macro when
recording. Its optional `register` text argument selects a named store entry;
an omitted argument uses `@`, so the Lua bindings above retain one predictable
default. The command palette collects that argument through its ordinary typed
prompt. Register names must be valid UTF-8 and one to 64 bytes. A session holds
at most 64 named registers, and each holds at most 1,024 `Key_press` or
`Text_input` events. The recorder never records its own record/replay controls,
does not record palette or command-prompt interaction, and ignores pointer
events.

`editor.macro.replay` accepts the same optional register argument and re-enters
each stored input through the normal Session dispatcher; transactions, hooks,
undo history, provenance, and syntax/language refresh therefore remain
ordinary per-input behavior. Macro storage is session-wide and transient. It
also accepts an optional positive `count` argument. A request is limited to
1,024 iterations and 65,536 replayed inputs, so a malformed adapter cannot
create an unbounded synchronous replay.

The generic descriptor surface
does not prescribe any editor's key grammar. The supplied Vim model uses the
public macro request effect to provide its tested `q{register}`, bare-`q`, and
`@{register}` subset. Helix's selected-register workflow, Kakoune's register
grammar, Emacs's macro ring/name/edit commands and native key grammar,
persistence, and terminal-host-shortcut capture remain absent. The `Macros` inspection
reports the active recording register, last stored register, bounded catalog,
and preview.

Builtin selection-algebra commands are ordinary binding targets too. The regex
forms declare one required text parameter, so binding one opens the same typed
prompt as selecting it in `Ctrl-P`:

```lua
zenbu.bind {
  input = "S",
  command = "editor.selection.split-regex",
  scope = "model:zenbu.selection-first:select",
}
```

The available ids are `editor.selection.select-regex`,
`editor.selection.split-regex`, `editor.selection.keep-regex`,
`editor.selection.remove-regex`, `editor.selection.merge-consecutive`,
`editor.selection.rotate-primary-forward`,
`editor.selection.rotate-primary-backward`,
`editor.selection.rotate-contents-forward`,
`editor.selection.rotate-contents-backward`, `editor.selection.flip`, and
`editor.selection.ensure-forward`. Content rotation needs at least two
non-empty selections and rotates their texts in document order. Its optional
`group-size` text parameter must be a positive integer that divides the number
of selections; it rotates each adjacent group independently. Regexes use OCaml
`Str`, reject zero-width matches, and return an error when a byte-oriented match
would split a UTF-8 code point; they are not a compatibility claim for Helix or
Kakoune regexes.

`zenbu-headless bindings vim` includes the same
reserved-host list beside model and extension bindings.

Use `zenbu-headless api` and `zenbu-headless bindings <vim|selection|direct|structural>`
to discover exact model and current status ids before writing a scoped binding;
for example the Vim-style model id is `zenbu.vim-style` and its initial status
id is `normal`.

`zenbu.on` accepts `document-changed` or `after-save`; its `run` function gets
`call.arguments.event`. Hooks run in registration order. A hook error is shown
and stops that callback's effects without undoing an already committed edit.
Zenbu suppresses re-entrance of the same event during delivery, preventing a
document-changed hook from looping on its own edit; this is not a general
asynchronous event system.

## Script-owned editing grammars

One trusted configuration generation may register one `zenbu.model` and start
it with `zenbu --model script --config PATH`. The declaration owns an explicit
serialisable state value and returns the next state, the complete display
status, and declarative effects for every logical input. The Session owns the
document, history, transaction validation, syntax refresh, host requests,
provenance, and callback lifetime; Lua never receives those mutable objects.

```lua
zenbu.model {
  id = "user.modal",
  title = "Minimal modal fixture",
  initial_state = { mode = "normal" },
  initial_status = { id = "normal", label = "NORMAL", input_mode = "keys" },
  run = function(call)
    local input = call.arguments.input
    local state = call.arguments.state
    if input.kind == "key" and input.key == "i" then
      return {
        state = { mode = "insert" },
        status = { id = "insert", label = "INSERT", input_mode = "text" },
        effects = {},
      }
    elseif input.kind == "text" and state.mode == "insert" then
      return {
        state = state,
        status = { id = "insert", label = "INSERT", input_mode = "text" },
        effects = {{ kind = "insert", text = input.text }},
      }
    end
    return { state = state, status = { id = state.mode, label = string.upper(state.mode) } }
  end,
}
```

`initial_state` and every returned `state` use the ordinary extension-value
format: `nil`, booleans, integers/floats, strings, lists, and string-keyed
records. Mixed tables, functions, userdata, threads, non-string map keys, and
values deeper than 32 tables or containing more than 4,096 values are rejected.
`initial_status` and returned `status` require nonempty `id` and `label`, and
may set `description`, `pending_input`, and `input_mode = "keys" | "text"`.
The input object is `{ kind = "key", key, modifiers }`, `{ kind = "text",
text }`, or `{ kind = "mouse", action, column, row, modifiers }`.

The model response must be `{ state, status, effects? }`; an invalid callback
result aborts that input before any effect commits. `effects` use the ordinary
action vocabulary below, including `{ kind = "view", action = "center" }` or
`{ kind = "view", action = "scroll-lines" | "scroll-pages", amount = integer }`.
Those view requests operate only on the focused host view and do not expose
geometry or renderer state. Reload stages a complete replacement Lua generation
before installation, then recreates each script-model runtime with its declared
initial state so no buffer retains a callback into a disposed Lua state. Script
state migration is intentionally not implicit; make it a future explicit,
versioned contract if a workload needs it.

[`script-modal-editor.lua`](../examples/script-modal-editor.lua) is an
inspectable modal fixture: it implements normal/insert/delete-pending states,
UTF-8 text insertion, semantic word deletion, and a centered-view request. It
demonstrates the framework boundary; it is not a Vim, Helix, Kakoune, Micro, or
Emacs compatibility claim.

Run its deterministic headless workload with:

```sh
dune exec bin/zenbu_headless.exe -- script-session \
  examples/script-modal-editor.lua examples/script-modal-editor.session
```

## Callback input and output

Every callback receives `call.context` and `call.arguments`. Context is copied
data:

- `document`: `{ id, version, length, contents }`, where offsets are UTF-8 bytes.
- `selections`: `{ anchor, head, start, stop, text }` values, and a one-based
  `primary` index.
- `syntax`: `nil` or `{ language, version, has_error }`.

Commands receive `arguments = nil`. Selectors receive the arguments supplied
by a semantic operation. Transformations receive `{ selection_set, arguments }`.
`zenbu.text(start, stop)` returns an in-bounds byte slice for the active
callback. `zenbu.syntax()` evaluates the primary selection, or
`zenbu.syntax(start, stop)` evaluates a supplied in-bounds range. It returns
`nil` without a syntax snapshot, otherwise language/version/error state and a
data-only smallest named node. A node provides `kind`, `start`, `stop`,
`named`, `error`, `missing`, compact `parent`/sibling/first-child values, and
compact named `children`. No parser object crosses the boundary.

Commands and hooks return one action, a list of actions, or `nil`. Actions are:

- `{ kind = "message", text = string }`
- `{ kind = "insert", text = string }`
- `{ kind = "delete" }`
- `{ kind = "replace", text = string }`
- `{ kind = "set-selections", selections = {{anchor, head}, ...}, primary = n }`
- `{ kind = "command", id = string }`
- `{ kind = "apply", selector = string, transformation = string, args = value }`
- `{ kind = "view", action = "center" }`
- `{ kind = "view", action = "scroll-lines" | "scroll-pages", amount = integer }`
- `{ kind = "external-filter", program = absolute-path, arguments = {string, ...} }`
- `{ kind = "background-process", program = absolute-path, arguments = {string, ...} }`

Selectors return `{ selections = {{anchor, head}, ...}, primary = n }`.
Transformations return `{ edits = {{start, stop, text}, ...} }`. Returned tables
are recursively converted to `nil`, booleans, integers/floats, strings, lists,
and string-keyed records. Functions, userdata, threads, non-string map keys,
and mixed list/map tables are rejected. Offsets, selection primary indexes,
overlapping edits, UTF-8 boundaries, and transactions are validated by Zenbu;
an invalid result creates no partial document mutation.

### External selection filters

Trusted Lua actions may request an `external-filter`. Zenbu passes the content
of every current selection independently to the executable's standard input,
then replaces all selections with their corresponding standard outputs in one
checked transaction. The request is intentionally an absolute executable path
and argument vector, never a shell command or a `PATH` lookup:

```lua
{
  kind = "external-filter",
  program = "/usr/bin/tr",
  arguments = { "a-z", "A-Z" },
}
```

`process.filter` is trusted-local configuration authority. Each input and
output, and the combined output for a multi-selection invocation, is limited to
16 MiB; stdout must be UTF-8; and the host terminates a process that runs for
more than five seconds. An unavailable executable, nonzero exit, timeout,
oversized result, or invalid output leaves the document unchanged. External
side effects are not reversible, and a model state transition that requested a
filter is not rolled back on a host failure. This is a bounded common primitive
for Helix/Kakoune-style selection filters and Micro's `textfilter`, not a shell
language, terminal buffer, or shell-completion API.

The bundled modal fixture has `s` to select the current word and `u` to run the
portable POSIX `/usr/bin/tr` example:

```sh
dune exec bin/zenbu_headless.exe -- script-session \
  examples/script-modal-editor.lua examples/external-filter.session
```

### Background programs

Trusted Lua actions may also request a bounded noninteractive
`background-process`:

```lua
{
  kind = "background-process",
  program = "/usr/bin/printf",
  arguments = { "index complete" },
}
```

`process.background` is trusted-local authority. The request is an absolute
executable plus an argument vector; Zenbu does not invoke a shell, perform a
`PATH` lookup, provide stdin, or expose a process handle to the model. At most
64 programs may run per session. Each has a five-second wall-clock limit;
stdout is limited to 16 MiB and must be UTF-8, while stderr is retained only as
a 4 KiB diagnostic. The registry retains at most 64 completed jobs and a
16 KiB UTF-8-safe stdout preview per job. Completion wakes the terminal loop
and updates the host message. Select `process.jobs` from `Ctrl-P`, or inspect
`Jobs` headlessly, to view final status and bounded output. Closing the Session
sends `SIGTERM` to running jobs. `process.job.cancel` is a typed host command
in `Ctrl-P`: enter one retained job id to send `SIGTERM` followed by `SIGKILL`
if it is still running. Cancellation is host-controlled; Lua receives no
process handle or completion callback.

The bundled modal fixture binds `j` to the portable `/usr/bin/printf` example:

```sh
dune exec bin/zenbu_headless.exe -- script-jobs \
  examples/script-modal-editor.lua examples/background-job.session
```

Use the `process.jobs` palette command to inspect retained jobs. Once a job has
finished, `process.job.open-output` accepts its positive `job-id` and opens its
bounded final report as a normal `*job N output*` buffer in the focused pane.
Successful jobs open their retained stdout preview; failed, timed-out, and
cancelled jobs open a diagnostic report. The buffer is not backed by a file and
starts clean, so it can be edited or saved through the ordinary workspace
commands.

There are intentionally no completion callbacks, streamed output, stdin,
terminal/PTY allocation, process groups, or shell-language semantics. A
trusted Lua file can still use Lua's ambient standard libraries outside this
Zenbu action; this action is a checked host facility, not a Lua sandbox.

## Observability, history, and limits

Script descriptors use provider ids such as `script.3` and retain their source
path. `why`/trace reports script load/reload lifecycle, complete binding
resolution with the full input sequence, model/command/selector/transformation/event
callback start/success/failure, and the ordinary provenance chain including binding, command, selector,
transformation, and event entries. The interactive `Scripts` inspector lists
the active generation, source, provider, counts, and last reload failure.
`Bindings`, `Commands`, `API`, `History`, and `Why` use the same generic views
as builtin behavior. Profiling adds bounded `script-load`, `script-reload`,
`script-model`, `script-command`, `script-selector`, `script-transformation`, and `script-event`
samples when `--profile` is enabled.

Dynamic semantic behavior resolves to a concrete transaction before commit, so
normal history, undo/redo, syntax refresh, and concrete transaction replay
remain available. A dynamic operation is intentionally not retained as a
model's repeatable semantic intent: after a reload its callback may no longer
exist or mean the same thing. Scripts should use a command/binding again rather
than expecting `.` to repeat a dynamic transform across generations.

M7 intentionally has no separate capability policy because user configuration
runs with the complete trusted-local Zenbu authority set. M8 plugins declare
their capabilities separately and are checked at the data-only host boundary;
M7 remains a configuration convenience rather than a stable third-party API.
Both paths retain trusted-Lua limits: no sandbox, resolver, completion
callbacks, timers, external event streams, general execution-time limits, or
isolation. The narrowly scoped external filter and background-program actions
have the explicit five-second process limits above. Lua error location is
best-effort source/line extraction from PUC Lua messages.
