# PHASE-02 — UI Shell: Tabs, File Tree, Editor

> Dated 27 May 2026. Depends on `PHASE-01-PLAN-27-MAY.md`. Parallel to `PHASE-03-PLAN-27-MAY.md` (different file scope).

## Context

The visual identity. PHASE-01 proved we can render a terminal. PHASE-02 builds the **VSCode-style chrome around it**: a leftmost agent tab bar, a per-project file tree, and a main pane that hosts either the terminal (default) or a minimal monospace editor buffer (when the user clicks a file). This is what differentiates Onibi from a bare terminal app.

Layout reference (from the user-provided VSCode screenshot, adapted):

```
┌──┬───────────────┬─────────────────────────────────────┐
│  │               │                                     │
│ A│  File tree    │  Main pane                          │
│ g│   ▾ project   │   ┌─────────────────────────────┐   │
│ e│     ▸ src     │   │ TerminalView (xterm.js)     │   │
│ n│     ▾ docs    │   │  $ claude code "..."        │   │
│ t│       a.md    │   │  ...                        │   │
│  │       b.md    │   │                             │   │
│ t│   ▸ project-2 │   └─────────────────────────────┘   │
│ a│               │                                     │
│ b│               │  ──or── if file selected:           │
│ s│               │   ┌─────────────────────────────┐   │
│  │               │   │ EditorBuffer (monospace)    │   │
│  │               │   │ # b.md                      │   │
│  │               │   │ ...                         │   │
│  │               │   │ [Save] [Discard] [Close]    │   │
│  │               │   └─────────────────────────────┘   │
└──┴───────────────┴─────────────────────────────────────┘
```

Agent tab bar can flip horizontal/vertical (Settings toggle). Tabs flash on `awaiting-approval` or `run-completed` state.

## Dependencies

- PHASE-01 merged. `PtyManager`, `pty_*` Tauri commands, `TerminalView` component all work.
- `app/` builds and runs.

## Deliverables

### D1 — Layout root

**File**: `app/src/App.tsx` (replaces PHASE-01's single-terminal scaffold)

Use `react-resizable-panels` (add to `package.json`):
```
pnpm --filter onibi-app add react-resizable-panels
```

Structure:
```tsx
<PanelGroup direction="horizontal">
  <Panel defaultSize={6} minSize={4} maxSize={10}><AgentTabBar /></Panel>
  <Panel defaultSize={18} minSize={10}><FileTree /></Panel>
  <Panel defaultSize={76}><MainPane /></Panel>
</PanelGroup>
```

CSS grid (`app/src/styles/layout.css`) defines colours, fonts. Dark theme default at `:root[data-theme="dark"]`, light at `:root[data-theme="light"]`. Tokens: `--bg-0`, `--bg-1`, `--fg-0`, `--accent`, `--flash`.

If the user has toggled tab bar to horizontal (Settings), render:
```tsx
<PanelGroup direction="vertical">
  <Panel size={6}><AgentTabBar orientation="horizontal" /></Panel>
  <Panel><PanelGroup direction="horizontal">...</PanelGroup></Panel>
</PanelGroup>
```

### D2 — Session model

**File**: `app/src/lib/sessions.ts`

```ts
export type AgentKind =
  | "claude-code" | "codex" | "opencode" | "gemini"
  | "aider" | "cursor" | "goose" | "shell";

export type SessionStatus =
  | "idle" | "running" | "awaiting-approval" | "completed" | "error";

export interface Session {
  id: string;                // = ptyId
  agent: AgentKind;
  workspaceId: string;       // foreign key to Workspace
  title: string;             // derived: agent + workspace basename
  status: SessionStatus;
  createdAt: number;
  // approval queue inside this session; resolved when PHASE-03 lands
  pendingApprovals: string[];
}

export interface Workspace {
  id: string;
  path: string;       // absolute
  name: string;       // basename
}
```

Persist sessions + workspaces in `tauri-plugin-store`:
```
pnpm --filter onibi-app add @tauri-apps/plugin-store
```

Configure `app/src-tauri/Cargo.toml`:
```toml
tauri-plugin-store = "2"
```

In Rust `main.rs`:
```rust
.plugin(tauri_plugin_store::Builder::default().build())
```

Use Zustand for in-memory store (add `zustand`).

### D3 — Agent tab bar

**File**: `app/src/components/AgentTabBar.tsx`

Props:
- `orientation: "vertical" | "horizontal"`

Renders one button per active session.
- Icon: `<img src={agentIconUrl(agent)} />` — icon files at `app/src/assets/agents/{agent}.svg`. SVG sources committed under `asset/agent-icons/`.
- Active session has filled background.
- `awaiting-approval` and `completed` statuses → CSS class `flash` applied.

`@keyframes flash` in `layout.css`:
```css
@keyframes flash-ring {
  0%, 100% { box-shadow: 0 0 0 0 var(--flash); }
  50%      { box-shadow: 0 0 0 4px var(--flash); }
}
.tab.flash { animation: flash-ring 1.2s ease-in-out infinite; }
```

Tab tooltip: `${agent} · ${workspace}` on hover.

Bottom of bar (or right end if horizontal):
- "+" button → `<NewSessionDialog />`
- Settings gear → opens Settings modal

### D4 — New session dialog

**File**: `app/src/components/NewSessionDialog.tsx`

Modal with:
1. Dropdown: agent selection (Claude Code, Codex, OpenCode, Gemini, Aider, Cursor, Goose, Plain shell).
2. Dropdown: workspace (lists registered workspaces + "Open Folder…").
3. Optional initial prompt textarea (for agents that accept one — e.g., `claude code "<prompt>"`).
4. Confirm → calls `pty_spawn` with appropriate command line + cwd; creates Session record.

Launch commands per agent (best-effort defaults; user override in Settings later):
- `claude-code` → `claude code` (or `claude code "<prompt>"`)
- `codex` → `codex`
- `opencode` → `opencode`
- `gemini` → `gemini`
- `aider` → `aider`
- `cursor` → `cursor-agent` (if available; document fallback)
- `goose` → `goose session`
- `shell` → default shell from PHASE-01

Look up agent binary on `$PATH` first; if missing, dialog shows install instructions link to `docs/adapters.md` (created later in PHASE-03).

### D5 — File tree

**File**: `app/src/components/FileTree.tsx`

Props: `workspace: Workspace | null`.

Rust commands needed (add to `app/src-tauri/src/fs/mod.rs`):
```rust
#[tauri::command]
async fn fs_list_dir(path: PathBuf) -> Result<Vec<FsEntry>, String> { … }

#[tauri::command]
async fn fs_read_file(path: PathBuf) -> Result<Vec<u8>, String> { … }

#[tauri::command]
async fn fs_write_file(path: PathBuf, data: Vec<u8>) -> Result<(), String> { … }
```

`FsEntry { name: String, path: PathBuf, kind: "file"|"dir", size: u64 }`.

Security: refuse paths outside the registered workspace root (path-canonicalise + `starts_with` check). Throw error to UI if violated.

Frontend tree:
- Lazy load children on expand.
- Sort: dirs first, alpha within.
- Click on file → `setSelectedFile(entry)` → MainPane switches to EditorBuffer.
- Click on workspace header → MainPane returns to TerminalView.
- No drag-and-drop, no rename, no create file (out of scope; user uses terminal for that).
- Filter input at top: substring match across visible nodes.
- Hidden files (`.foo`) hidden by default; toggle via filter bar checkbox.

Workspace add/remove:
- Menu button "Open Folder…" → Tauri `dialog::open` → adds workspace, persists.
- Workspaces listed as expandable roots; "remove workspace" via context menu (right-click).

### D6 — Editor buffer

**File**: `app/src/components/EditorBuffer.tsx`

Props: `path: string`.

UI:
- Header: file path, dirty indicator (•), buttons [Save] [Discard] [Close].
- Body: `<textarea>` styled monospace, full height, wrap=off. Default font: Menlo 13px.

Behavior:
- Mount → `fs_read_file(path)` → decode UTF-8 → set initial content.
- Local edit → mark dirty.
- Save → `fs_write_file(path, bytes)` → mark clean.
- Discard → revert from disk → reload content.
- Close → if dirty, confirm; sets MainPane back to TerminalView.

Binary files: detect non-UTF-8; show "Cannot edit binary file." with hex preview disabled (just message).

Large files: refuse >2 MB; show "File too large; open in $EDITOR." with copy-path button.

### D7 — Main pane

**File**: `app/src/components/MainPane.tsx`

```tsx
function MainPane({ session, selectedFile }: Props) {
  if (selectedFile) return <EditorBuffer path={selectedFile} />;
  if (session) return <TerminalView ptyId={session.id} />;
  return <EmptyState />;  // "Open a folder, start a session"
}
```

### D8 — Settings pane

**File**: `app/src/components/SettingsPane.tsx`

Modal/overlay. Sections (Phase 02 scope; PHASE-03 adds adapter sections):
- **General**: theme (dark/light/system), font family, font size.
- **Layout**: tab bar orientation (vertical/horizontal), tab bar position (left/right or top/bottom).
- **Agents**: per-agent launch command override + binary path lookup result.
- **Workspaces**: list + add/remove.

Persist via `tauri-plugin-store` to `~/.config/onibi/settings.json` (or platform equivalent — let plugin pick).

### D9 — Theme parsing (optional polish)

If `~/.config/ghostty/config` exists, attempt to parse:
- `font-family`
- `font-size`
- `background`
- `foreground`
- `palette = N=#rrggbb` (for ANSI colors in xterm theme)

Apply as default theme. User Settings can override.

If parser fails or file absent, fall back to built-in dark theme. **Do not block startup on theme parse errors.**

### D10 — Agent icons

Create SVGs at `asset/agent-icons/`:
- `claude.svg` — Anthropic/Claude mark (use officially available SVG or community asset; check license)
- `codex.svg` — OpenAI / Codex mark
- `opencode.svg` — sst/opencode logo
- `gemini.svg` — Google Gemini
- `aider.svg` — Aider mark
- `cursor.svg` — Cursor agent
- `goose.svg` — Block/Goose
- `shell.svg` — generic terminal glyph

Copy to `app/src/assets/agents/` for import. Document attribution + licenses in `asset/agent-icons/ATTRIBUTION.md`.

If a brand asset is not freely licensable, use a stylised placeholder (initial letter on coloured square) and note it in the README.

### D11 — Tests

- `AgentTabBar.test.tsx`: renders N tabs from store, applies `flash` class on awaiting-approval status, fires `setActive` on click.
- `FileTree.test.tsx`: refuses paths outside workspace root (mock fs commands), expands/collapses dirs.
- `EditorBuffer.test.tsx`: dirty flag toggles, Save calls `fs_write_file` once with correct bytes.
- `MainPane.test.tsx`: chooses Editor when selectedFile set, Terminal when only session set.
- Rust: `fs::tests` covers path canonicalisation + escape attempts.

## Exit criteria

1. App opens with VSCode-style three-pane layout.
2. User can open multiple workspaces; each appears in file tree.
3. User can create two simultaneous shell sessions; switching tabs swaps the visible terminal correctly (no PTY mix-up).
4. Clicking a file in the tree opens it in the EditorBuffer; pressing Save persists to disk; editing the same file in another tool and clicking Discard reloads.
5. Tab flash animation fires when a session is set to `awaiting-approval` state (simulate via Zustand action in dev tools for Phase 02; PHASE-03 wires the real signal).
6. Settings modal toggles tab bar orientation between vertical and horizontal; layout updates without restart.
7. All vitest + cargo tests pass; CI green.
8. No new permission prompts in `tauri.conf.json` beyond `fs` (scoped to user-selected dirs), `dialog`, `shell:execute` (already needed by PHASE-01), `store`.

## Out of scope (do NOT do in PHASE-02)

- Approval modal or any approval flow (PHASE-03).
- HTTP server / WS (PHASE-03).
- Mobile-related code (PHASE-04).
- Transports (PHASE-05).
- Headless mode (PHASE-06).
- LSP or syntax highlighting.
- Real-time file watching of workspace.
- Git integration / branch indicator on tabs.
- Drag-reordering of tabs across workspaces.

## Verification commands

```sh
cd /Users/gongahkia/Desktop/coding/projects/onibi
git checkout v1.5/phase-02-ui-shell
cd app
pnpm install --frozen-lockfile
pnpm typecheck
pnpm test
cd src-tauri && cargo test --workspace && cd ..
pnpm tauri dev
# manual: open two terminals, edit a file, toggle tab orientation
```

## Reference reading

- `PHASE-00-PLAN-27-MAY.md`, `PHASE-01-PLAN-27-MAY.md` — already done.
- `PHASE-03-PLAN-27-MAY.md` — being built in parallel; coordinate via `SessionStatus` enum which is **owned by this phase** (PHASE-03 imports it).
- [react-resizable-panels docs](https://react-resizable-panels.vercel.app)
- [@tauri-apps/plugin-store](https://v2.tauri.app/plugin/store/)
- [Zustand](https://github.com/pmndrs/zustand)
- [VSCode UI layout reference](https://code.visualstudio.com/api/ux-guidelines/overview)
- cmux UI gallery (`https://cmux.com/`) for vertical-tab agent-icon look.

## PR template

Title: `phase-02: ui shell (tabs, file tree, editor buffer)`

Body checklist:
```
## Deliverables
- [x] D1 PanelGroup layout (App.tsx)
- [x] D2 Session/Workspace model (lib/sessions.ts + zustand + plugin-store)
- [x] D3 AgentTabBar with flash animation
- [x] D4 NewSessionDialog
- [x] D5 FileTree (lazy load, sandboxed paths)
- [x] D6 EditorBuffer (monospace, save/discard/close)
- [x] D7 MainPane switcher
- [x] D8 SettingsPane
- [x] D9 Ghostty config parser (optional)
- [x] D10 agent SVG icons + attribution
- [x] D11 tests

## Verification
- [x] cargo test + pnpm test green
- [x] Manual: 2 simultaneous tabs, file edit/save, orientation toggle (screencast)
- [x] No new permission prompts beyond approved list

Next: PHASE-03 (already in flight in parallel) wires real approval signals into the flash animation.
```
