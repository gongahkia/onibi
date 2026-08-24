# M11 language services

M11 adds optional asynchronous language intelligence without changing Zenbu's
editing contract. `zenbu.kernel` and `zenbu.model_api` remain free of LSP,
JSON-RPC, subprocess, URI, and protocol-position types. Language output is
Zenbu-owned data, then ordinary semantic effects and transactions decide
whether it can change the active document.

```text
saved buffer path + Language.Registry
        ↓
zenbu.lsp private process adapter ── LSP / JSON-RPC / stdio ── language server
        ↓ bounded event inbox + wakeup fd
zenbu.app.Session (main thread)
        ↓ owned diagnostics / hover / locations / edits
model runtime → validated transaction → history / provenance / syntax refresh
        ↓
zenbu.view ranges and status → terminal colours
```

The default registry starts `ocamllsp` for `.ml` and `.mli` files. It advertises
the `ocaml` language id and discovers a workspace by walking upward to the
nearest `dune-project`, `dune-workspace`, or `.git` marker. No server is
auto-installed, downloaded, or run through a shell. `make bootstrap` installs
the test dependency `ocaml-lsp-server`; an invocation still reports an
actionable unavailable status if `ocamllsp` is absent from `PATH`.
Workspace discovery is a bounded, mutex-protected cache keyed by starting
directory and configured markers; M11 intentionally has no watcher to
invalidate it during a session.

An embedding host may construct `Language.Server_config` values and register
them in `Language.Registry`. A configuration has an executable, argv vector,
explicit environment overrides, filename extensions/language ids, root markers,
working directory, workspace folders, and data-only initialization/options
settings. Registry selection is deterministic by server id.

## Declarative user language-server configuration

The terminal loads an optional TOML file at
`$XDG_CONFIG_HOME/zenbu/language-servers.toml` (or
`$HOME/.config/zenbu/language-servers.toml` when `XDG_CONFIG_HOME` is unset).
`--language-config PATH` selects one explicitly; `--no-language-config` uses
only built-in servers. The default path is optional: a missing file keeps the
built-in registry, while a selected file that fails validation aborts startup.

```toml
version = 1

[[server]]
id = "ocaml.local"
language_ids = ["ocaml"]
extensions = [".ml", ".mli"]
executable = "/usr/bin/ocamllsp"
args = ["--stdio"]
cwd = "/work/project"
environment = { OCAMLPARAM = "_,warn-error=+A" }
root_markers = ["dune-project", ".git"]
workspace_folders = ["/work/project"]
```

This grammar is deliberately small. It accepts only the fields shown above;
there are at most 32 servers, 64 arguments per server, 32 environment values,
and 16 root markers or workspace folders. The executable must resolve to an
absolute regular executable that is not group- or world-writable and whose
ancestor directories are likewise not group- or world-writable. `cwd` and
workspace folders must resolve to existing absolute directories. Root markers
are basenames, not paths. Environment names are conventional upper-case names;
`PATH`, dynamic-loader variables, and `DYLD_*` overrides are rejected. Values
are inherited from the host only where they are not explicitly overridden.

User declarations replace a built-in declaration when they overlap its language
id or extension. They do not give Lua configuration, plugins, model code, or
server responses a way to name a process: the TOML file is a separate host
input and the LSP adapter always uses an argv vector/direct exec path, never a
shell. This is validation of configuration shape and provenance, not a sandbox:
an approved local executable still has the invoking user's authority.

`config.reload` (`Alt-R`) first parses and validates a complete new language
registry, then stages Lua configuration and plugins. A rejected candidate
leaves the previous registry and every healthy language client in place. After
a successful reload, Zenbu recreates clients for the active and inactive local
buffers against the new registry. `Session.Language` records the configuration
source and server identifiers but redacts every argument and environment value;
lifecycle failures remain visible through the ordinary bounded language status
and trace records.

## Lifecycle, coordinates, and synchronization

The adapter spawns the configured executable directly, applies an explicit
working directory when configured, keeps its pipes private, and sends
`initialize`, `initialized`, and `textDocument/didOpen`. `initialize` includes
the discovered root plus configured workspace folders. Its reader and
stderr-drainer threads only decode and enqueue bounded owned events.
`Session.poll_language`, called from the terminal loop after any open buffer's
wakeup fd becomes readable, is the only place that changes session state,
selections, overlays, or documents. It drains the focused client for
interactive replies, then drains inactive clients for diagnostics, server
lifecycle, and accepted workspace edits. Interactive replies from an inactive
buffer are discarded; they cannot open an overlay in the wrong pane. The
terminal can therefore redraw on server output without busy polling.

The client negotiates UTF-8, UTF-16, and UTF-32 position encodings,
text-document synchronization, save text policy, hover, definition,
completion, code actions, rename, diagnostics, semantic tokens, and workspace
settings. It defaults to UTF-16 until the server chooses otherwise.
`Language.Position`
converts only valid
UTF-8 code-point boundaries and rejects offsets inside a multi-byte code point,
a UTF-16 surrogate pair, or a CRLF pair. Lines are zero-based:

```sh
dune exec bin/zenbu_headless.exe -- lsp-position utf-16 12 FILE.ml
```

Each kernel transaction exposes simultaneous source-snapshot edits. For an
incremental server, `Language.Sync` sends those edits in descending source
order, converting each range against its current intermediate text; applying
the emitted changes must reconstruct the committed snapshot or synchronization
fails. A full-sync server receives current complete text. Selection-only
document-version changes are associated with the latest LSP snapshot, even
though they do not send `didChange`. Save sends `didSave` when negotiated.
Saving under a new path rebinds both syntax and the language-service client
while preserving history and resetting only model-private grammar state.

The adapter maps LSP document version to Zenbu document version and contents.
A versioned diagnostic renders only for the current Zenbu version. Unversioned
diagnostics are accepted only at the untouched original snapshot (version zero)
and are dropped after an edit rather than displayed as possibly current. Hover,
completion, definition, and rename retain document version plus source caret
offset. Code actions and range formatting retain document version and the
exact primary-selection range; document formatting retains the whole-document
range. A later edit/caret move sends `$/cancelRequest` where possible, and late
results are discarded before session mutation. Cancellation is a stale safety
mechanism, not a promise that every server stops immediately.

Limits are explicit: 32 KiB headers, 8 MiB protocol bodies, 16 KiB retained
stderr/hover text, 1,024 diagnostics/completion items, and 2 KiB user-visible
server messages. Protocol/decode/start failures set `failed`, retain a bounded
reason, wake the host, and use bounded descriptor close plus TERM/KILL reap.
`language.restart` is explicit. Session shutdown attempts `shutdown`/`exit`
then uses the same cleanup path.

## Commands, rendering, and current scope

`zenbu.language` descriptors appear in the same palette, command inspection,
and host dispatch as other providers:

- `language.status` reports language/server id, executable, root, lifecycle,
  encoding, sync kind, pending request count, diagnostics count, and last error.
- `language.restart`, `.hover`, `.definition`, `.complete`, `.code-action`,
  `.format`, `.format-selection`, `.symbols`, `.workspace-symbols`, and
  `.rename` start the corresponding async operation. Code actions and selection
  formatting use the primary selection; workspace symbols prompt for a query;
  rename opens a text prompt.
- `language.diagnostic.next`, `.previous`, and `.describe-current` navigate or
  describe current diagnostics with normal selection semantics.

`Ctrl-Space` requests completion. Hover and completion are terminal overlays;
type in completion to filter label/filter-text/detail, then use arrows,
Backspace, Enter, or Escape.
Completion accepts only plain text edits: snippets are rejected explicitly, and
main plus additional edits use the one semantic transformation
`language.apply-edits`. The view treats diagnostics as ranges and uses style
precedence selection, search, diagnostic, semantic token, syntax, then plain.
The status row reports error/warning totals.

When a server advertises full semantic-token support, Zenbu automatically
requests a fresh stream after initialization and each document change. A stream
is decoded only against the request snapshot and negotiated position encoding;
it is bounded to 4,096 tokens, requires valid UTF boundaries and non-empty
ordered non-overlapping ranges, and may set only advertised modifier bits.
Late, malformed, or unsupported results leave the existing frame unchanged.
Zenbu maps only namespace, type, function, variable, property, and modifier to
owned renderer roles; other declared server classes are safely omitted. The
`default`, `dark`, and `light` themes each define those roles explicitly.

The M12 workspace host retains language/syntax state with each local buffer.
A definition target for another local file opens or reuses that buffer in the
focused view, then navigates through the ordinary selection effect; it does
not depend on a model-private document pointer.

Rename and `workspace/applyEdit` support edits across **already-open, saved
local buffers**. Before a request is decoded, an adapter captures a URI-to-text
snapshot of every such buffer. Every returned URI must have a snapshot; file
creation, deletion, rename/resource operations, and unopened targets are
rejected. Before Session stages an edit, each target's live contents must still
equal its captured snapshot. Session then stages an ordinary
`language.apply-edits` effect in every affected buffer runtime. It publishes
the candidate runtimes and sends their individual `didChange` notifications
only if every target validates. A failed mapping, stale snapshot, or edit
conflict leaves every buffer unchanged; an inbound `workspace/applyEdit`
receives `applied: false`.

This is Session-level all-or-none publication, not a kernel-wide
multi-document transaction: affected buffers retain independent history and
undo branches.

`language.code-action` requests `textDocument/codeAction` for the current
primary selection. Its inspector records the request id, document version,
range, and bounded action list. A pending request and its displayed actions are
cancelled or discarded when that snapshot is no longer current. Selecting an
enabled action with a workspace edit uses the same checked, all-or-none staging
path as rename. Every attached server command is shown as denied and is never
executed; command-only, disabled, and edit-less actions are rejected. Zenbu
does not auto-open action targets or allow code-action resource operations.

`language.format` requests `textDocument/formatting`; `.format-selection`
requests `textDocument/rangeFormatting` for the current primary selection.
Both use the fixed, explicit `tabSize: 2` and `insertSpaces: true` policy, with
no save-time trigger or product-specific option surface. Returned `TextEdit`s
are decoded against the request snapshot, then staged through
`language.apply-edits` only if the current document version and requested range
are unchanged. Empty results report no change. Dirty buffers are formatted as
their current in-memory snapshot; syntactic validity is left to the language
server. Malformed, stale, or overlapping edits leave the buffer unchanged.

`language.symbols` flattens `textDocument/documentSymbol` hierarchies in
depth-first order, retaining ancestor labels and selection ranges. Results are
bounded to 512 entries, depth 32, and 256 bytes per label/detail. Selecting a
current document symbol uses the normal selection boundary. Workspace-symbol
queries require an explicit project root; a result must resolve to an already
open local buffer inside that root before selection. Duplicate labels remain
separate entries by hierarchy/range; malformed or stale results are discarded.

Zenbu does not coordinate external file changes, or provide project search,
file watching, or general workspace-edit resource operations.

## Inspection, testing, and trust

`Session.Language` is available interactively and headlessly:

```sh
dune exec bin/zenbu_headless.exe -- language-status FILE.ml
dune exec bin/zenbu_headless.exe -- language-fake-session \
  _build/default/test/fake_lsp_server.exe test/fixtures/lsp_ocaml/sample.ml
```

The fake server covers declarative configuration parsing/policy rejection,
redaction, root discovery, configured working-directory/workspace-folder
launch, successful and rejected reload across concurrent local buffers,
initialize/negotiation, full and incremental sync, diagnostics, delayed stale
hover, cancellation, completion additional edits, same- and cross-file
definitions, checked code actions and command denial, document/range formatting and its fixed options,
no-op/stale/malformed/error formatting replies, open-buffer cross-file rename
and server apply-edit, unopened and stale workspace-edit rejection, all-or-none
conflicting workspace edits, semantic-token Unicode/overlap/size/staleness
validation, malformed frames, crash/restart, and shutdown.
`test_m11_ocamllsp` opens a small Dune fixture with real `ocamllsp` and obtains
a hover response.

`Trace_event.Language_service` records server start, negotiation, sync,
request, cancellation, response, decode, exit, and failure stages with server
id, request id/document version where applicable, outcome, and bounded detail.
The generic `why` view formats these events. The profiler adds language sync,
hover, definition, completion, code-action, formatting, semantic-token,
rename, and decode samples.
Neither retains raw
JSON-RPC packets, full source, protocol objects, or server secrets.

Language servers are local processes chosen by the trusted host/embedding
configuration. They are not a Zenbu sandbox, permission system, or extension
capability boundary. Zenbu validates returned coordinates/current-document
edits but cannot make an arbitrary local executable safe; use only trusted
servers.
