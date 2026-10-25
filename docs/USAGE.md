# Shack

**Atomweight Terminal Text Editor IDE with Sensible Day 1 Defaults**

Shack is a nonmodal terminal editor for macOS and Linux. Type to insert text;
use arrows, Shift+arrows, or the mouse to select. `Ctrl+E` opens searchable
commands and `F1` opens the same command reference with shortcuts.

This is the second development milestone, `0.2.0-dev`. Features below are implemented;
remaining limitations are listed explicitly. It is not a published stable release.

## Install and launch

A release archive contains the `shack` executable, this guide, and license notices.
Choose the archive matching the operating system and processor:

- Linux: `amd64` (x86-64 baseline), `arm64`, or `arm` (ARMv7).
- macOS: `amd64` (Intel) or `arm64` (Apple Silicon).

Extract it, then run `./shack`. For a user-local installation:

```sh
mkdir -p "$HOME/.local/bin"
install -m 755 shack "$HOME/.local/bin/shack"
# Add $HOME/.local/bin to PATH in your shell configuration if necessary.
```

The executable includes its Go runtime, terminal support, and syntax lexers.
Users do **not** install Go, a C compiler, Lua, Wasmtime, Python, or Node.js to edit
text. Linux artifacts are built with CGO disabled and require no shared libc.
macOS executables use system libraries. Matching CPU/OS and supported operating
system versions are still required; a binary is not universal across machines.
The Go 1.27 runtime requires Linux kernel 3.2+ or macOS 13+, according to the
[Go minimum requirements](https://go.dev/wiki/MinimumRequirements). These are
runtime minima, not platforms on which Shack has all been independently tested.

From this checkout, developers need Go 1.27 or later:

```sh
make build
./build/shack README.txt
./build/shack src/main.go src/util.go
./build/shack .
printf 'scratch text\n' | ./build/shack
```

`make release` builds the five archives and SHA-256 checksums under `dist/`.
No public release or download URL is assumed to exist yet.

## Everyday controls

| Action | Shortcut |
| --- | --- |
| Save / new buffer / close buffer / quit | Ctrl+S / Ctrl+N / Ctrl+W / Ctrl+Q |
| Open path / find project file | Ctrl+O / Ctrl+P |
| Commands / help | Ctrl+E or F2 / F1 |
| Find / preview replacement / next match | Ctrl+F / Ctrl+R / F3 or Alt+F |
| Select all matches / next occurrence | Alt+A / Ctrl+D |
| Select all text | Ctrl+A |
| Copy / cut / paste | Ctrl+C / Ctrl+X / Ctrl+V |
| Undo / redo | Ctrl+Z / Ctrl+Y |
| Next / previous buffer | Alt+N / Alt+P, or click a visible tab |
| Go to line / toggle file tree | Ctrl+G / Ctrl+B |
| Context menu | Right-click or Shift+F10 |
| Indent / outdent selection | Tab / Shift+Tab |
| Completion / go to definition | Ctrl+Space / F12 |
| Move / select by word | Ctrl+Left/Right / Ctrl+Shift+Left/Right |
| Delete word / move lines | Ctrl+Backspace/Delete / Alt+Up/Down |
| Collapse selections / cancel | Escape |
| Redraw after terminal corruption | Ctrl+L |
| Debug start/continue / breakpoint | F5 / F9 |
| Debug step over / into / out | F10 / F11 / Shift+F11 |
| Debug stop or detach | Shift+F5 |

Some terminals intercept shortcuts, particularly Command keys on macOS and
Ctrl+Space. Every editing command with a terminal-sensitive shortcut is also
available through the command palette. These are Ctrl-based shortcuts on macOS;
terminal-native Cmd+V remains available for pasting.

Cut/copy without a selection operates on the current line. A bracketed paste is
one undoable transaction. Contiguous typing undoes in word-sized groups (800 ms idle boundary, up to 128
transactions per group). Saving and navigation end a typing group. History retains
the latest 256 groups in memory; paste and accepted previews remain single edits.

## Preview and edit several matches

1. Press Ctrl+R, enter the search text, then enter its replacement.
2. Review the list. Space includes/excludes a change; `a` toggles all changes.
3. Tab opens the selected change's complete before/after text. Arrow keys scroll
   vertically or horizontally; Tab returns to the list.
4. Enter applies the included changes as one transaction. Escape cancels.
5. Ctrl+Z undoes the entire accepted batch.

Use **Toggle regular expression search** in the palette for Go/RE2-style regexes.
Capture expansion uses `$1` or `${name}`. Searches support multiline patterns with
`(?m)` and `(?s)`; lookaround and backreferences are not supported. Zero-width
patterns are rejected. Results are bounded at 10,000 matches; narrow the query
when that limit is reached.

Alternatively, find with Ctrl+F, then Alt+A to select all matches and type to edit
them together. Ctrl+D first selects the current word, then adds later occurrences.

Palette transformations include uppercase, lowercase, trailing-whitespace cleanup,
sorting, and deduplication. They preview selected text, or the whole buffer when
there is just one empty selection. Use **Preview replace in project** for the project-wide version. Case and line
transformations operate on the active buffer.

## Language intelligence

Highlighting lexers ship inside Shack. Language servers are optional programs:
when a matching server is available on PATH, Shack starts it in the background.
One process is shared by files using the same server in the same workspace, with
at most four server/workspace combinations active at once.

| Files | Detected server |
| --- | --- |
| Go | `gopls` |
| Rust | `rust-analyzer` |
| Python | `pyright-langserver --stdio`, then `pylsp` |
| JavaScript / TypeScript / JSX / TSX | `typescript-language-server --stdio` |
| C / C++ | `clangd` |
| Shell | `bash-language-server start` |
| OCaml | `ocamllsp` |
| JSON | `vscode-json-languageserver --stdio` |

Use the palette for:

- **Hover documentation**, **Signature help**, **Document symbols**, and
  **Workspace symbols**.
- **Go to definition**, **Go to implementation**, **Go to type definition**,
  **Find references**, **Jump back**, and **Jump forward**.
- **Diagnostics**, **Next diagnostic**, and **Previous diagnostic**.
- **Preview rename symbol**, **Code actions**, and **Preview format document**.
- **Language server status** and **Restart language servers**.

Completion appears beside the caret after a 200 ms editing pause, after two word
characters or a dot, when a server is ready. Ctrl+Space requests it explicitly.
Up/Down selects; Tab or Enter accepts; Escape dismisses. Typing continues editing
and refreshes suggestions. Set `auto_complete: false` to request suggestions only.
Snippet completions support nested placeholders, forward and linked stops, choice
menus, variables/defaults, and capture/case/conditional transforms. Tab/Shift+Tab
moves between stops; Up/Down and Enter choose a value. Typing supplies a custom
choice, and **Snippet choices** reopens the menu. Replacing an outer placeholder
removes its nested stops. Transforms refresh when leaving a stop and share its
undo step; editing continues while the bounded transform worker runs.

File, selection, word, workspace, date/time, random and comment variables use the
current document context. `CLIPBOARD` uses Shack's last copied text. Unknown
variables become editable placeholders. The supported syntax follows the
[VS Code snippet grammar](https://code.visualstudio.com/docs/editing/userdefinedsnippets).
Regex evaluation uses a bounded ECMAScript-compatible engine with rune-based
Unicode matching; it does not implement every JavaScript regex detail (including
Unicode-set `v` mode) or TextMate shell interpolation. Oversized, cyclic,
unsupported or timed-out snippets fail with an explanation. Snippet source is
limited to 64 KiB, expansion to 128 KiB and nesting to 32 levels.

Hover and signatures open scrollable read-only buffers; Markdown is shown as text.
Ctrl+W closes these reports. Formatting, rename, and edit-based code actions are
reviewed before application. File resource operations (creation, rename, deletion)
and server-command actions are rejected as a whole. Some servers consequently
expose actions Shack cannot yet perform. Language support varies with each server.

Shack does not download servers or their runtimes. Some servers need Node.js,
Python, or language toolchains; their memory is additional to Shack's. Missing
servers leave ordinary editing and bundled highlighting available. Only C/clangd
has live integration tests for completion, definition, formatting, hover,
references, symbols, signatures and rename, in addition to deterministic protocol tests;
other presets still need testing against their respective server installations.

For a small server, use `shack --no-lsp` or `shack --plain`. Optional highlighting
and LSP are disabled for documents over 1 MiB by default. This keeps large-file
editing available. Highlighting runs on a background worker with a best-effort
budget, but tokenisation is not preemptible within an individual lexer call.

## Project editing and navigation

**Split vertically** and **Split horizontally** divide the focused pane, up to 16
panes with mixed orientations. Drag a divider, or use **Grow pane** / **Shrink pane**,
to resize. **Focus next pane**, directional focus commands, mouse clicks, and
**Close pane** manage them. Small panes cannot be split further. Each pane retains its own cursor and scroll position,
including when several panes show the same file. **Toggle soft wrapping** changes
visual wrapping without inserting newlines; vertical arrows follow visual rows.

**Toggle file tree** (Ctrl+B) shows the project index on the left. It opens by
default when a directory is supplied and the terminal is at least 70 columns wide.
Click folders to expand and files to open. **Focus file tree** enables arrow-key
navigation; Enter opens a file and Tab/Escape returns to the editor. **Reveal file
in tree** locates the active file. The tree uses the same bounded index as the file
picker; refresh after external file changes. Drag its edge to resize.

**Toggle minimap** enables an optional text-density overview on wide terminals.
Click or drag to navigate; the highlighted area marks the viewport. Sampling runs
in the background with bounded work, even for large files. It is not a syntax map
and does not depict every line. Right-click text, tabs, the tree or minimap for
context commands. Double-click selects a word, triple-click a line, and Shift-click
extends a selection. Dialog lists and replacement previews also accept clicks.

**Find in project** uses literal or regex search and opens a list of locations.
Search includes unsaved open buffers. **Preview replace in project** offers the
same per-edit checkboxes and before/after inspection as single-buffer replacement.
All selected targets are validated and staged before any buffer changes. Stale
versions, changed disk files, invalid ranges, or conflicting paths reject the batch.
Accepted changes remain unsaved buffers; **Save all** writes them. Saving several
files is sequential, not an atomic filesystem transaction.

Use **Undo last workspace edit** / **Redo last workspace edit** to revert/reapply
the whole latest batch. This is guarded: if a target was edited or closed since,
use its individual undo history. Normal Ctrl+Z acts on the active buffer.

Discovery uses Git's ignore rules when Git is installed and the project is a Git
repository; tracked files remain included even if ignored by a pattern. Without
Git, a built-in walker skips common dependency/build directories and symlinks,
but does not interpret `.gitignore`. **Refresh project files** rebuilds the index;
**Project index status** reports the mode. The picker indexes up to 10,000 files.
Project search skips binary/unreadable files and files over 1 MiB, and caps retained
results at 2,000 matches / 8 MiB. Escape cancels it. Searches do not follow symlinked
files outside the project. Pass a directory argument to choose the project root.

**Duplicate lines**, **Delete lines**, **Move lines up/down**, **Select line**, and
**Toggle line comments** cover common editing operations. Line moves/duplication
require one contiguous selection. Line comments use a file-extension-based prefix;
this is not a syntax-aware comment engine. **Reload from disk** preserves the old
text in undo. Focus events, when supported by the terminal, warn about external
changes; saves always recheck the disk fingerprint.

## Recovery and returning to a project

Dirty text is snapshotted after up to two seconds, under
`$XDG_STATE_HOME/shack/recovery` (default `~/.local/state/shack/recovery`). Directories
are private and snapshots use mode 0600. Live instances hold locks so they are not
mistaken for crashed sessions. **Recover unsaved buffers** offers abandoned work;
an otherwise empty startup offers it automatically. Recovery does not overwrite
originals. If a file is already open or unreadable, its recovered copy is untitled.
Changed originals remain protected by save conflict checks.

Recovery defaults to 8 MiB of combined unsaved text, at most 64 dirty buffers,
and the first 32 selections per buffer. Startup bounds the combined contents of
offered snapshots by the same text budget. Exceeding the limit produces
a status message and retains the previous snapshot; the newest edits are then
not protected. Abrupt termination can also lose edits since the latest snapshot.
Clean quit/discard removes the current instance's recovery record. Recovery is
not persistent undo or a backup service. `--no-recovery` disables it.

Project sessions remember up to 64 file paths, cursor positions, and 16 nested panes
under the same state directory, with private files. Clean exit saves the session;
**Save project session** also saves explicitly. Use `shack --restore .` or
**Restore project session** to reopen it. Restoration reads current disk contents
and is capped at 16 MiB combined; unsaved text is handled separately by recovery.
Existing open buffers are retained. `--no-session` disables session storage.

## Build, test and Git

**Run build / test task** offers configured tasks plus presets for Go, Cargo, npm,
or Make projects. Nothing runs automatically. The task picker shows the command;
if buffers are dirty, choose whether to save or run against current disk contents.
**Run shell command** explicitly runs a command through `sh -c` in the project.
These require the relevant external toolchain or shell.

Output streams into a read-only buffer, with at most 1 MiB retained.
**Cancel running task** kills its process group; one task runs at a time.
**Task output locations** opens existing `path:line[:column]` locations. Column
interpretation is byte-based and some tools use other conventions. Tasks receive
no interactive input. Use **New terminal** for interactive programs.

When Git is installed, **Git status**, **Git diff**, **Git staged diff**, **Git stage
current file**, and **Git unstage current file** are available. Diffs reflect saved
files; save before staging. These commands do not commit, push, fetch or resolve
merge conflicts. Core editing works without Git.

## Embedded terminal

**New terminal** opens a real PTY in a buffer, in the project directory. It starts
`$SHELL -i`, falling back to `/bin/sh -i`; `terminal_shell` can specify an argument
array instead. Split or switch this buffer like any other buffer. Sessions start
only on request and are not restored on reopening Shack.

While terminal input is active, keys go to the shell or foreground program,
including Ctrl+C, Ctrl+Z and Ctrl+Q. **Ctrl+] returns to editor controls**; use the
palette's **Resume terminal input**, or click the terminal, to resume. **Send Ctrl+]
to terminal** passes the reserved key through. Shift+PageUp/PageDown or the mouse
wheel scroll history; typing returns to the live screen. Drag selects text and
right-click opens terminal commands. Applications that enable mouse reporting
receive mouse events; hold Shift to use Shack's selection/context controls where
the outer terminal forwards that modifier. **Copy terminal selection** requests
OSC 52 and keeps an internal copy; **Export terminal screen** creates a savable
read-only report of the visible screen.

ANSI colors, alternate screens, cursor modes, bracketed paste, resizing and
standard mouse reports are supported. The emulator is not a complete replacement
for a desktop terminal: graphics protocols, every extended key/mouse protocol,
and unmodified hover-motion reporting are not promised. The default scrollback is
500 rows, configurable from 1–2,000, also limited by a 4 MiB cell-storage budget.
Text payloads and emulator parser storage add to that budget. The emulated screen
is capped at 300 columns, 200 rows and 20,000 cells. At most four terminal buffers
may be open; input queues/pastes are capped at 1 MiB. Frames update at most 30 times
per second, with no idle refresh timer.

**Stop terminal**, closing its buffer, or exiting Shack closes the PTY and signals
its shell/foreground process groups. Closing just a pane keeps its buffer alive.
Detached or deliberately daemonized processes are outside that cleanup contract.
Terminal contents are ephemeral and excluded from recovery/session files.

## Debugging

Debugging uses the [Debug Adapter Protocol](https://microsoft.github.io/debug-adapter-protocol/).
Adapters and language runtimes are optional external programs; Shack does not
install or start them on project open. **Debug start or continue** (F5) offers
configured profiles. Python files also offer a `python3 -m debugpy.adapter`
preset, which requires debugpy in that interpreter. Missing adapters report an
error; **Debug output** includes adapter stderr for troubleshooting.

Add profiles under `debuggers` in your user configuration, for example:

```json
{
  "debuggers": {
    "Python current file": {
      "command": ["python3", "-m", "debugpy.adapter"],
      "request": "launch",
      "arguments": {
        "program": "${file}",
        "cwd": "${workspaceFolder}",
        "console": "internalConsole",
        "redirectOutput": true,
        "subProcess": false
      }
    },
    "Attach to Python": {
      "address": "127.0.0.1:5678",
      "request": "attach",
      "arguments": {"justMyCode": true, "subProcess": false}
    }
  }
}
```

Choose either a stdio `command` array or an existing DAP TCP `address`. The attach
example expects a target already started with debugpy's `--listen` and
`--wait-for-client` options. Adapter-specific `arguments` pass through unchanged
apart from `${workspaceFolder}`, `${file}`, `${fileDirname}` and `${fileBasename}`
substitution in strings. These substitutions are also available in command
arguments. Arbitrary VS Code launch-variable resolution is not implemented.
No project-local debug configuration is executed automatically.

Save dirty buffers before launch/attach; the picker offers Save All. F9 or a click
in the marker column of the gutter toggles a line breakpoint. `○` means pending
or unverified and `●` means the adapter verified it; `▶` marks the selected stopped
frame. **Debug breakpoints** lists locations and adapter messages. **Debug edit
breakpoint condition** adds a condition when the adapter supports it. Breakpoints
follow edits/undo and synchronize after saving; editing does not reload running
code. They are retained for the current Shack process, not in session files.

At a stop, Shack opens the source line. F10 steps over, F11 steps into, Shift+F11
steps out, and F5 continues. **Debug pause** interrupts a running target. Use
**Debug threads**, **Debug stack frames**, and **Debug scopes and variables** to
choose a thread/frame and expand values. Expensive scopes load only when selected;
variables and frames are paged. **Debug current location** returns to the selected
frame. **Debug evaluate expression** explicitly evaluates in that frame and may
execute code. **Debug status** describes the current session and controls.

**Debug output** opens a read-only, bounded transcript. Adapter stderr shares its
1 MiB tail buffer; visible output refreshes at most ten times per second.
There is no idle polling. **Debug stop or detach** (Shift+F5) requests termination
for a launched target and detachment for an attached target. Leaving Shack uses
the same distinction. Locally spawned adapters run in separate OS sessions so
their targets cannot take Shack's controlling terminal. After disconnect, an
unresponsive local adapter is killed after a bounded grace period. Cleanup of
separately spawned debuggee processes ultimately depends on the adapter; local
debugpy launch, detach and abrupt-connection cleanup are covered by live tests.

This is source-level debugging, not the whole DAP feature set. One session runs
at a time; data/instruction/function breakpoints, watch persistence, memory and
disassembly views, and multi-process child-session orchestration are not exposed.
Programs needing interactive stdin require a separately launched target and
attach; `runInTerminal`/`startDebugging` reverse requests return explicit errors.
Use an internal-console configuration for launch. Conditions are limited to
4 KiB, breakpoints to 256 per file / 1,024 total, variable expansion to 32 levels,
and protocol messages to 8 MiB. Other adapters can use the same transport, but
live compatibility has been established with debugpy 1.8.22, not every adapter.

## SSH and tmux

Run the binary on the remote host; the local terminal forwards its input/output.
No GUI or remote editor daemon is required. tmux's terminfo definition is bundled.

Copy requests the terminal clipboard via OSC 52 and also retains an internal
clipboard. On a local desktop, Shack tries existing `pbcopy`/`pbpaste`, Wayland,
or X11 clipboard helpers when needed; they are not prerequisites for starting.
Over SSH, Shack avoids accessing the remote desktop clipboard.

OSC 52 support and permissions vary. A copy status says a clipboard write was
requested, not that the terminal confirmed it. If Ctrl+V receives no response,
use your terminal's paste shortcut (usually Ctrl+Shift+V or Cmd+V). **Paste Shack
internal clipboard** is always available from the palette. tmux's `set-clipboard`
policy and the surrounding terminal determine clipboard forwarding. Clipboard
interoperability still needs testing across real SSH hosts and terminal families.

Mouse capture can be toggled from the palette. Shift+mouse selection is handled
by many terminals themselves; it may include displayed gutters.

## Configuration

No file is required. Optional JSON lives at
`$XDG_CONFIG_HOME/shack/config.json`, or `~/.config/shack/config.json`.
Missing fields retain defaults. Example:

```json
{
  "tab_size": 4,
  "insert_spaces": true,
  "auto_indent": true,
  "auto_complete": true,
  "soft_wrap": false,
  "file_tree": "auto",
  "tree_width": 26,
  "minimap": false,
  "terminal_scrollback": 500,
  "recovery": true,
  "recovery_max_bytes": 8388608,
  "session": true,
  "mouse": true,
  "syntax": true,
  "lsp": true,
  "theme": "dark",
  "service_max_bytes": 1048576,
  "max_file_mib": 256,
  "keybindings": {
    "Alt+S": "Split vertically",
    "F6": "Focus next pane",
    "F7": "Find in project",
    "F8": "Run build / test task"
  },
  "tasks": {
    "Check": ["go", "test", "./..."]
  },
  "servers": {
    ".py": {"command": ["pylsp"], "language": "python"}
  }
}
```

`--config PATH` selects a file, `--no-config` uses defaults, `--no-lsp` disables
language servers, `--plain` disables both language servers and highlighting,
and `--readonly` prevents editor mutations. `--version` and `--help` need no TTY.
Keybinding values are palette command names, case-insensitive. An empty value
unmaps that shortcut. Modifiers use `Ctrl+`, `Alt+`, `Shift+`; arrows, function keys,
and named keys are supported. Unknown keys or commands produce startup errors.
Overrides apply to editor commands, while prompts/previews retain their controls.
Terminal protocols cannot distinguish every key combination, so choose keys your
terminal actually sends. Tasks use argument arrays; use `["sh", "-c", "..."]` only
when shell interpretation is wanted. No repository-local configuration is run.
Theme/mouse palette changes last for the session; configuration is read at startup.

## Current boundaries

- UTF-8 text only; files containing NUL bytes or invalid UTF-8 are rejected.
  Existing LF/CRLF is retained. There is no encoding conversion or binary mode.
- Files are loaded into memory; the default open limit is 256 MiB. This is an
  input bound, not a promise that a 256 MiB file fits on a 256 MiB server.
- Undo history is in memory. Sessions/recovery do not preserve undo history.
  A plugin API and complete standalone `.gitignore` parsing are not implemented.
- Saves detect changed/deleted targets, stage beside the file, sync, and atomically
  replace. Existing symlinks resolve at open; hard-linked files require Save as.
  Ownership and permission bits are retained; ACLs/xattrs are not copied.
  External-write detection is not an interprocess lock and has a final race window.
- LSP file operations and server commands, native Windows, general encodings, Git merge
  resolution, and automatic language-server installation remain unimplemented.
- This remains a development editor, not a claim of mature IDE parity. See the
  [capability audit](IDE_AUDIT.md) for the next substantive work.

## Development checks

```sh
make check                         # formatting, vet, tests, race detector
make build
python3 scripts/pty_smoke.py        # real pseudo-terminal editing workflow
python3 scripts/tmux_smoke.py       # isolated tmux server (requires tmux)
python3 scripts/ide_smoke.py        # project edits, panes and tasks in tmux
python3 scripts/terminal_smoke.py   # embedded shell, Vim, resize and cleanup
# Optional live debugging checks (the chosen interpreter needs debugpy):
SHACK_DEBUGPY_PYTHON=/path/to/python go test -race ./internal/dap ./internal/editor
SHACK_DEBUGPY_PYTHON=/path/to/python python3 scripts/debug_smoke.py
python3 scripts/recovery_smoke.py   # SIGKILL and recovery through a real PTY
python3 scripts/redraw_bench.py     # local output timing and bytes per edit
go test ./internal/editor -run '^$' -bench BenchmarkLongLineViewport -benchmem
go test ./internal/text -run '^$' -bench . -benchmem
make release
python3 scripts/linux_smoke.py      # optional Docker scratch-container check
```

The test suite includes a real clangd integration when that executable is available.
Python and tmux above are development-test tools, not editor runtime dependencies.
See [architecture](ARCHITECTURE.md), [product scope](PRODUCT.md), and
[performance observations](PERFORMANCE.md).
