# Shack

**Atomweight Terminal Text Editor IDE with Sensible Day 1 Defaults**

Shack is a nonmodal terminal editor for macOS and Linux. Type to insert text;
use arrows, Shift+arrows, or the mouse to select. `Ctrl+E` opens searchable
commands and `F1` opens the same command reference with shortcuts.

This is the first development version, `0.1.0-dev`. Features below are implemented;
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
| Go to line | Ctrl+G |
| Indent / outdent selection | Tab / Shift+Tab |
| Completion / go to definition | Ctrl+Space / F12 |
| Collapse selections / cancel | Escape |
| Redraw after terminal corruption | Ctrl+L |

Some terminals intercept shortcuts, particularly Command keys on macOS and
Ctrl+Space. Every editing command with a terminal-sensitive shortcut is also
available through the command palette. These are Ctrl-based shortcuts on macOS;
terminal-native Cmd+V remains available for pasting.

Cut/copy without a selection operates on the current line. A bracketed paste is
one undoable transaction. Ordinary typing currently records one transaction per
keystroke; history retains the latest 256 transactions in memory.

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
there is just one empty selection. Replacements and transformations currently
operate on one buffer, rather than a whole project.

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

Use **Language server status**, **Complete symbol**, **Go to definition**,
**Diagnostics**, **Preview format document**, and **Restart language servers** in
the palette. Completion is explicitly requested rather than opening an automatic
popup on every keystroke. Formatting uses the normal replacement preview.

Shack does not download servers or their runtimes. Some servers need Node.js,
Python, or language toolchains; their memory is additional to Shack's. Missing
servers leave ordinary editing and bundled highlighting available. Only C/clangd
has a live integration test in addition to the deterministic protocol tests;
other presets still need testing against their respective server installations.

For a small server, use `shack --no-lsp` or `shack --plain`. Optional highlighting
and LSP are disabled for documents over 1 MiB by default. This keeps large-file
editing available. Highlighting runs on a background worker with a best-effort
budget, but tokenisation is not preemptible within an individual lexer call.

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
  "mouse": true,
  "syntax": true,
  "lsp": true,
  "theme": "dark",
  "service_max_bytes": 1048576,
  "max_file_mib": 256,
  "servers": {
    ".py": {"command": ["pylsp"], "language": "python"}
  }
}
```

`--config PATH` selects a file, `--no-config` uses defaults, `--no-lsp` disables
language servers, `--plain` disables both language servers and highlighting,
and `--readonly` prevents editor mutations. `--version` and `--help` need no TTY.
Theme/mouse palette changes last for the session; configuration is read at startup.

## Current boundaries

- UTF-8 text only; files containing NUL bytes or invalid UTF-8 are rejected.
  Existing LF/CRLF is retained. There is no encoding conversion or binary mode.
- Files are loaded into memory; the default open limit is 256 MiB. This is an
  input bound, not a promise that a 256 MiB file fits on a 256 MiB server.
- Lines scroll horizontally. Soft wrapping, splits, persistent sessions, disk
  undo/crash recovery, custom keybindings, and a plugin API are not implemented.
- The file picker indexes up to 10,000 files, skips common dependency/build
  directories and symlinks, and does not yet implement full `.gitignore` rules.
- Saves detect changed/deleted targets, stage beside the file, sync, and atomically
  replace. Existing symlinks resolve at open; hard-linked files require Save as.
  Ownership and permission bits are retained; ACLs/xattrs are not copied.
  External-write detection is not an interprocess lock and has a final race window.
- Snippet completions, hover, semantic rename, debugging, a Git UI, an embedded
  terminal, project-wide edits, and native Windows support are deferred.

## Development checks

```sh
make check                         # formatting, vet, tests, race detector
make build
python3 scripts/pty_smoke.py        # real pseudo-terminal editing workflow
python3 scripts/tmux_smoke.py       # isolated tmux server (requires tmux)
go test ./internal/text -run '^$' -bench . -benchmem
make release
python3 scripts/linux_smoke.py      # optional Docker scratch-container check
```

The test suite includes a real clangd integration when that executable is available.
Python and tmux above are development-test tools, not editor runtime dependencies.
See [architecture](ARCHITECTURE.md), [product scope](PRODUCT.md), and
[performance observations](PERFORMANCE.md).
