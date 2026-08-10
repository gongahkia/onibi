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
generation. `config-describe` reports registered ids and bindings. `script-session`
runs an inspectable session fixture with the explicit configuration and prints
the resulting text, script-generation view, and history.

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
  run = function(call)
    local selected = call.context.selections[call.context.primary]
    return {{ kind = "message", text = selected.text }}
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

`zenbu.bind` takes `input`, `command`, and optional `scope`. Inputs are named
keys (`Escape`, `Enter`, `Backspace`, `Tab`, `ArrowUp`, `ArrowDown`,
`ArrowLeft`, `ArrowRight`), logical text, or `Ctrl-X`. A scope is `global`,
`model:<model-id>`, or `model:<model-id>:<status-id>`. More specific matching
scopes win over global; duplicate input/scope pairs in one generation are a
validation error. Bindings are considered before model input. The host retains
`Ctrl-S`, `Ctrl-Q`, and `Ctrl-Alt-R` as non-overridable save, quit, and reload
controls. `config.reload` is a permitted binding target for a script-defined
reload key.

Use `zenbu-headless api` and `zenbu-headless bindings <vim|selection|structural>`
to discover exact model and current status ids before writing a scoped binding;
for example the Vim-style model id is `zenbu.vim-style` and its initial status
id is `normal`.

`zenbu.on` accepts `document-changed` or `after-save`; its `run` function gets
`call.arguments.event`. Hooks run in registration order. A hook error is shown
and stops that callback's effects without undoing an already committed edit.
Zenbu suppresses re-entrance of the same event during delivery, preventing a
document-changed hook from looping on its own edit; this is not a general
asynchronous event system.

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

Selectors return `{ selections = {{anchor, head}, ...}, primary = n }`.
Transformations return `{ edits = {{start, stop, text}, ...} }`. Returned tables
are recursively converted to `nil`, booleans, integers/floats, strings, lists,
and string-keyed records. Functions, userdata, threads, non-string map keys,
and mixed list/map tables are rejected. Offsets, selection primary indexes,
overlapping edits, UTF-8 boundaries, and transactions are validated by Zenbu;
an invalid result creates no partial document mutation.

## Observability, history, and limits

Script descriptors use provider ids such as `script.3` and retain their source
path. `why`/trace reports script load/reload lifecycle, binding resolution,
command/selector/transformation/event callback start/success/failure, and the
ordinary provenance chain including binding, command, selector,
transformation, and event entries. The interactive `Scripts` inspector lists
the active generation, source, provider, counts, and last reload failure.
`Bindings`, `Commands`, `API`, `History`, and `Why` use the same generic views
as builtin behavior. Profiling adds bounded `script-load`, `script-reload`,
`script-command`, `script-selector`, `script-transformation`, and `script-event`
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
Both paths retain trusted-Lua limits: no sandbox, resolver, async callbacks,
timers, external event streams, resource limits, or isolation. Lua error
location is best-effort source/line extraction from PUC Lua messages.
