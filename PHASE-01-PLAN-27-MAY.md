# PHASE-01 — PTY Core + Terminal Rendering

> Dated 27 May 2026. Sequel to `PHASE-00-PLAN-27-MAY.md`. Sequential prerequisite for PHASE-02 and PHASE-03.

## Context

Without a working pseudo-terminal that can spawn and host an agent process (Claude Code, Codex, …) with full bidirectional I/O, **nothing else in this project works**. PHASE-01 is "proof of life": prove that we can spawn a shell, render xterm.js in the webview, and pipe bytes both ways at acceptable latency. Subsequent phases assume the PTY layer is solid.

The "Independent Asian" rule applies here: a dev arriving cold should be able to read this file, run `git checkout v1.5/phase-01-pty`, and start coding without asking questions.

## Dependencies

- PHASE-00 complete and merged to `v1.5`. CI green.
- `app/` Tauri scaffold launches.
- macOS 14+ or Ubuntu 22.04+ host with Rust stable + Node 20+ + pnpm 8+.

## Deliverables

### D1 — Rust PTY module

**File**: `app/src-tauri/src/pty/mod.rs`

Use the `portable-pty` crate (https://docs.rs/portable-pty). Add to `app/src-tauri/Cargo.toml`:

```toml
[dependencies]
portable-pty = "0.8"
tokio = { version = "1", features = ["full"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
anyhow = "1"
thiserror = "1"
tracing = "0.1"
tracing-subscriber = "0.3"
uuid = { version = "1", features = ["v4", "serde"] }
parking_lot = "0.12"
```

Module surface (idiomatic Rust):

```rust
// app/src-tauri/src/pty/mod.rs
mod manager;
mod session;

pub use manager::PtyManager;
pub use session::{PtyId, PtySession, PtySpawnRequest, PtyExitStatus};
```

`PtySpawnRequest`:
- `command: String` (default: `$SHELL` or `/bin/bash`)
- `args: Vec<String>`
- `cwd: Option<PathBuf>`
- `env: Vec<(String, String)>` (merged with current env)
- `rows: u16`, `cols: u16`

`PtyManager`:
- `pub fn new() -> Arc<Self>`
- `pub async fn spawn(&self, req: PtySpawnRequest) -> Result<PtyId>`
- `pub async fn write(&self, id: PtyId, data: &[u8]) -> Result<()>`
- `pub async fn resize(&self, id: PtyId, rows: u16, cols: u16) -> Result<()>`
- `pub async fn kill(&self, id: PtyId) -> Result<()>`
- `pub fn subscribe(&self, id: PtyId) -> Result<broadcast::Receiver<Bytes>>` (per-session output channel)
- `pub async fn list(&self) -> Vec<PtyId>`

Internal storage: `Arc<RwLock<HashMap<PtyId, PtySession>>>` using `parking_lot::RwLock`.

`PtySession` owns:
- The `portable-pty::Child` handle.
- Writer side of the master PTY (wrapped in async-aware mutex).
- A `tokio::sync::broadcast::Sender<Bytes>` for output fan-out (capacity 1024).
- A background reader task that loops `master.try_clone_reader()?.read()` → broadcast.

PTY exit semantics:
- When the child exits, emit a final broadcast message of variant `PtyEvent::Exit(status)` (use a custom enum, not raw `Bytes`).
- Update internal state to `Terminated`. Subsequent writes return `Err(PtyError::Terminated)`.

### D2 — Tauri commands wiring

**File**: `app/src-tauri/src/main.rs` (additions)

```rust
#[tauri::command]
async fn pty_spawn(
    state: tauri::State<'_, Arc<PtyManager>>,
    req: PtySpawnRequest,
) -> Result<PtyId, String> { … }

#[tauri::command]
async fn pty_write(
    state: tauri::State<'_, Arc<PtyManager>>,
    id: PtyId,
    data: Vec<u8>,
) -> Result<(), String> { … }

#[tauri::command]
async fn pty_resize(
    state: tauri::State<'_, Arc<PtyManager>>,
    id: PtyId,
    rows: u16,
    cols: u16,
) -> Result<(), String> { … }

#[tauri::command]
async fn pty_kill(
    state: tauri::State<'_, Arc<PtyManager>>,
    id: PtyId,
) -> Result<(), String> { … }
```

Register on `tauri::Builder`:
```rust
.manage(Arc::new(PtyManager::new()))
.invoke_handler(tauri::generate_handler![pty_spawn, pty_write, pty_resize, pty_kill])
```

**Event emission** (Rust → JS): each PTY spawns a tokio task that subscribes to the broadcast channel and emits events to the webview:
```rust
window.emit(&format!("pty:{}", id), PtyEvent::Data(bytes_base64))?;
```

Encode binary as base64 to avoid JSON corruption of non-UTF-8 bytes.

Event variants emitted to webview:
- `pty:<id>` with `{type: "data", data: "<base64>"}`
- `pty:<id>` with `{type: "exit", code: number, signal: string | null}`

### D3 — Frontend terminal component

**File**: `app/src/components/TerminalView.tsx`

```tsx
import { useEffect, useRef } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebglAddon } from "@xterm/addon-webgl";
import { WebLinksAddon } from "@xterm/addon-web-links";
import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import "@xterm/xterm/css/xterm.css";

export interface TerminalViewProps {
  ptyId: string;       // already-spawned PTY id
  fontFamily?: string; // default: "Menlo, Monaco, monospace"
  fontSize?: number;   // default: 13
}

export function TerminalView({ ptyId, fontFamily, fontSize }: TerminalViewProps) { … }
```

Add to `app/package.json`:
```json
"dependencies": {
  "@xterm/xterm": "^5.5.0",
  "@xterm/addon-fit": "^0.10.0",
  "@xterm/addon-webgl": "^0.18.0",
  "@xterm/addon-web-links": "^0.11.0",
  "@tauri-apps/api": "^2.0.0"
}
```

Implementation requirements:
- Mount xterm on a ref'd `div`.
- Load WebGL addon; fall back to canvas if WebGL fails (catch the error from `term.loadAddon(webgl)`).
- Listen to `pty:<id>` events; base64-decode and `term.write(decoded)`.
- On `term.onData(d => invoke("pty_write", { id: ptyId, data: bytes(d) }))`.
- On `ResizeObserver` of the container → `fitAddon.fit()` → `invoke("pty_resize", { id, rows, cols })`.
- Cleanup: dispose addons + term, `unlisten()`, on unmount.
- Theme: dark default. Cursor blink: on. Scrollback: 10000 lines.

### D4 — Hook into App shell

**File**: `app/src/App.tsx`

Minimum viable: on mount, call `invoke("pty_spawn", { req: { command: shellPath(), args: [], cwd: null, env: [], rows: 30, cols: 100 } })`, then render `<TerminalView ptyId={id} />`.

For PHASE-01 this is a single full-screen terminal. PHASE-02 will replace `App.tsx` with the full layout.

### D5 — Helpers / utilities

**File**: `app/src-tauri/src/util/shell.rs`

```rust
pub fn default_shell() -> String {
    std::env::var("SHELL").unwrap_or_else(|_| "/bin/bash".to_string())
}
```

**File**: `app/src/lib/tauri-bridge.ts` — typed wrappers around all `invoke()` calls. PHASE-02+ uses these instead of raw `invoke`.

### D6 — Tests

**Rust** (`app/src-tauri/src/pty/manager.rs` `#[cfg(test)] mod tests`):
- Spawn `echo hello\n`. Read broadcast channel until exit. Assert "hello" in output.
- Spawn `cat`, write `ping\n`, assert output contains `ping`.
- Spawn `sleep 10`, kill, assert exit status received within 1s.
- Resize active PTY; assert no panic.

CI: `cargo test --workspace`.

**Frontend** (`app/src/components/TerminalView.test.tsx` with vitest + jsdom):
- Mounts without throwing.
- Subscribes to event listener on mount; unsubscribes on unmount.
- Calls `invoke` with base64-decoded bytes on `term.onData`.

Mock `@tauri-apps/api` in `app/vitest.setup.ts`.

### D7 — Performance smoke check

Before exit, run this manual check:
```
yes | head -n 100000
```
in the terminal. Should render without freezing the webview. If frame drops are visible, add a server-side coalescing buffer (flush at 16ms boundaries).

### D8 — Logging

`tracing` initialised in `main.rs`:
```rust
tracing_subscriber::fmt().with_env_filter("info,onibi=debug").init();
```
PTY module logs at `info` (spawn/exit) and `debug` (writes/resizes).

## Exit criteria

1. `cargo test --workspace` passes on macOS + Linux.
2. `pnpm tauri dev` opens, terminal renders, user can run: `claude code --help`, `vim`, `htop`, `top`, `ls -la` without visual artifacts.
3. ANSI 256-color output (e.g., `ls --color=always`) renders correctly.
4. Mouse selection + Cmd-C / Ctrl-Shift-C copy works.
5. Resizing the window resizes the terminal correctly (no half-rendered rows).
6. `yes | head -n 100000` completes in under 5 seconds with smooth scroll.
7. Closing the terminal window kills the child process (no orphaned shells in `ps`).
8. Integration test: from a Rust test, spawn `sh -c 'echo hi; sleep 0.1; exit 7'`, assert output `hi\n` received, exit code 7.

## Out of scope (do NOT do in PHASE-01)

- Multiple tabs / multiple PTYs (PHASE-02).
- File tree (PHASE-02).
- Editor pane (PHASE-02).
- Agent detection / icon (PHASE-02).
- Approval flow (PHASE-03).
- HTTP server / WS endpoints (PHASE-03).
- Mobile rendering (PHASE-04).
- Ghostty config parsing for theme (PHASE-02, optional polish).
- LSP / syntax highlighting in editor (out of scope entirely — explicitly excluded by user decision).

## Verification commands

```sh
cd /Users/gongahkia/Desktop/coding/projects/onibi
git checkout v1.5/phase-01-pty
cd app/src-tauri
cargo test --workspace
cd ..
pnpm install --frozen-lockfile
pnpm typecheck
pnpm test
pnpm tauri dev   # manual smoke check per exit criterion 2-7
```

## Reference reading

- `PHASE-00-PLAN-27-MAY.md` — what was just done.
- `PHASE-02-PLAN-27-MAY.md` — what comes next (the UI layout that will consume your PTY layer).
- `PHASE-03-PLAN-27-MAY.md` — adapter layer; PTY spawn requests will eventually carry agent metadata.
- [portable-pty docs](https://docs.rs/portable-pty/latest/portable_pty/)
- [xterm.js docs](https://xtermjs.org/docs/) and [@xterm packages](https://github.com/xtermjs/xterm.js/tree/master/addons)
- [Tauri commands](https://v2.tauri.app/develop/calling-rust/) and [events](https://v2.tauri.app/develop/calling-frontend/)
- Reference: how cmux structures PTY ownership in Swift — [manaflow-ai/cmux Sources/](https://github.com/manaflow-ai/cmux) (not for code reuse; for design ergonomics)

## PR template

Title: `phase-01: pty core + terminal rendering`

Body checklist:
```
## Deliverables
- [x] D1 Rust PTY module (app/src-tauri/src/pty/)
- [x] D2 Tauri commands wired
- [x] D3 TerminalView.tsx with xterm.js
- [x] D4 App.tsx single-terminal proof of life
- [x] D5 helpers (shell.rs, tauri-bridge.ts)
- [x] D6 Rust + frontend tests
- [x] D7 perf smoke (yes | head -n 100000)
- [x] D8 tracing logging

## Verification
- [x] cargo test green
- [x] pnpm test green
- [x] manual smoke: claude/vim/htop render correctly (attach screencast)
- [x] resize works
- [x] no orphan processes after window close

Next: PHASE-02 (UI shell — tabs, file tree, editor).
```
