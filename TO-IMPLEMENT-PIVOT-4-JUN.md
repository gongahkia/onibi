# TO-IMPLEMENT-PIVOT-4-JUN

Pivot scope: align onibi closer to herdr's terminal-native, agent-multiplexer surface.

Source projects:
- herdr: `/Users/gongahkia/Desktop/coding/projects/herdr` (Rust TUI, ratatui + ghostty)
- onibi: `/Users/gongahkia/Desktop/coding/projects/onibi` (Tauri desktop + mobile PWA)

## 0. Status update — 2026-06-04

Do **not** remove this file yet. The original SPEC.md work is done and SPEC.md has been removed, but this pivot document still contains the remaining herdr-parity roadmap.

### Implemented in the orchestration pass

- Created GitHub tracking issues #55–#61 for the orchestration subsystem.
- Added daemon-owned PTY/session runtime in `app/src-tauri/src/orchestration.rs`.
- Added JSON-lines orchestration protocol over localhost TCP (`127.0.0.1:17894`) and Unix socket (`~/.config/onibi/onibi-orchestration.sock`).
- Hard-cut the GUI PTY bridge to use the daemon orchestration runtime instead of owning its own `PtyManager`.
- Added event streaming for session start, status, PTY output, PTY exit, and PTY notifications.
- Added CLI/control commands for:
  - `wait output`
  - `wait agent-status`
  - `pane read`
  - `pane send-keys`
  - `agent list/read/send/start/focus`
  - `events subscribe`
- Added global CLI `--json` support and JSON output for the new orchestration commands plus several existing commands.
- Added canonical status values: `idle`, `working`, `blocked`, `done`.
- Wired approval blocking to set agent status to `blocked`, approval decisions back to `working`, and PTY exit to `done`.
- Verified with `cargo test`, `cargo fmt --check`, `git diff --check`, and `cargo check --no-default-features`.

### Still partial after the orchestration pass

- `pane read --source visible` is currently a tail-of-buffer approximation, not a full terminal-screen/VT snapshot.
- `recent-unwrapped` is not implemented yet.
- Agent targeting still resolves to PTY/session IDs; there is no stable semantic agent selector or per-agent aliasing.
- `agent focus` exists at the protocol level but is not yet a rich GUI focus operation. CLI `session focus` still uses the existing desktop focus route.
- The status model exists, but does not yet implement done-unviewed → idle-on-focus, process heuristics, screen heuristics, or state arbitration.
- `--json` is not yet uniform for every legacy CLI command path, especially older desktop HTTP and adapter install/uninstall outputs.
- Daemon-owned PTYs survive GUI bridge changes while the daemon is alive, but there is still no named session attach, stop, restart persistence, or live binary handoff.

---

## 1. Comparison Matrix

| Dimension | herdr | onibi |
|---|---|---|
| **Primary form factor** | Terminal-native TUI | Tauri desktop GUI + mobile PWA |
| **Core problem** | Multiplexing & orchestrating concurrent agents | Approval/gating + remote phone supervision |
| **Language** | Rust (Edition 2021) | Rust (Tauri backend) + TS/React (UI) |
| **Render engine** | ratatui + libghostty-vt (vendored) | xterm.js + CodeMirror 6 + React 19 |
| **Distribution** | Single binary (herdr installer, brew, nix) | DMG / AppImage / deb / brew / pi-install.sh |
| **Cross-platform** | macOS x86_64/arm64, Linux x86_64/arm64 (glibc) | macOS, Linux, Pi (headless), iOS/Android (PWA) |
| **Persistent sessions** | Yes — server/client, named sessions, survive restart | Partial — SQLite store, approvals rehydrate; daemon-owned PTYs while daemon is alive; no restart pane resume |
| **Multi-client attach** | Yes — multiple terminals on same session | Partial — multiple clients via WebSocket and orchestration event subscriptions |
| **Detach / reattach** | Yes — full | Partial — GUI can attach to daemon-owned PTYs while daemon is alive; no named attach/restart resume |
| **Workspaces** | First-class (project containers, persistent identity) | Workspace path only; no container abstraction |
| **Tabs** | First-class | Yes (agent/editor/terminal tabs) |
| **Panes (real splits)** | Yes — drag-resize, mouse-native, tiling | Yes — vertical/horizontal split with focus |
| **Pane runtime** | Real PTYs via portable-pty + ghostty VT | Real PTYs via portable-pty |
| **Workspace/tab/pane reorder (drag/drop)** | Yes | No |
| **Agent detection** | 18 agents (auto, process + heuristic) | 7 adapters (2 real, 5 stubs) |
| **Agent state model** | 4 states: idle / working / blocked / done (unviewed) | Partial — canonical statuses exist; approval/exit transitions wired; no done-unviewed/focus heuristic |
| **Direct integrations (hook/plugin)** | 8 versioned: pi, omp, claude, codex, opencode, hermes, qodercli, copilot | 2 working: claude-code (HTTP), codex (shell); 5 stubs |
| **Heuristic detection (no hook)** | 11 agents (Cursor, Cline, Copilot, Gemini, Grok, Kimi, …) | None |
| **Native agent session resume** | Yes — Claude/Codex/Pi/Hermes/OpenCode convos restore | No |
| **Approval / gating layer** | No | Yes (primary feature) |
| **Edit tool input before approve** | N/A | Yes (Claude Code `updatedInput`) |
| **Socket API for agents** | Yes — Unix socket, wire protocol v12, bincode | Yes — HTTP/WebSocket plus JSON-lines orchestration over Unix socket + localhost TCP |
| **API: wait-for-output** | Yes (`herdr wait output --match … --regex`) | Yes |
| **API: wait-for-agent-status** | Yes (`herdr wait agent-status`) | API yes; status source still partial |
| **API: pane read (visible/recent/ANSI)** | Yes | Partial — structured recent/ANSI reads; visible is tail-of-buffer approximation |
| **API: pane send-text / send-keys / run** | Yes | Partial — PTY write + common `send-keys`; no explicit `run` helper |
| **CLI surface** | Very broad: workspace/tab/pane/agent/worktree/wait/session/integration/status/config | Expanded: setup/doctor/adapter/transport/token/session/pane/wait/agent/events; legacy JSON still partial |
| **Remote attach** | Yes — SSH bootstrap, auto-install on remote | N/A (uses transports for phone, not SSH attach) |
| **Mobile / phone UX** | Mobile-narrow TUI layout only | First-class installable PWA |
| **Transport: Tailscale Funnel** | No | Yes |
| **Transport: Cloudflare Quick Tunnel** | No | Yes |
| **Transport: LAN HTTPS + mDNS + QR pair** | No | Yes |
| **Bearer-token auth / pairing** | No (Unix-socket trust) | Yes (ULID token, keyring, rotate) |
| **TLS / HSTS / CSP** | No | Yes |
| **Rate limiting** | No | Yes (tower_governor) |
| **Git worktree management** | Yes (`herdr worktree …`) | Yes (create/remove/list) |
| **Git status / staging / commit UI** | Status caching, sidebar indicators | Full UI (staging, commit, diffs, clone, push/pull) |
| **Filesystem browse / read / write** | No (it is a multiplexer) | Yes (file tree, search, CRUD) |
| **In-app editor (CodeMirror)** | No | Yes (syntax HL, Vim mode) |
| **Review/diff baseline tracking** | No | Yes (SHA256 snapshots, per-file accept/reject) |
| **Themes** | 18 built-in (catppuccin, dracula, nord, …) | None advertised |
| **Keybindings: prefix + chord (tmux-style)** | Yes (`ctrl+b` prefix, configurable v2 syntax) | Cmd+K palette only |
| **Vim mode** | Yes in copy mode (h/j/k/l, w/b/e, v/y) | Yes (CodeMirror Vim) |
| **Command palette (Cmd+K)** | No | Yes |
| **Mouse-native (click-focus, drag-resize, drag-reorder)** | Yes | Partial |
| **Copy mode (text selection, ANSI-aware)** | Yes — drag, double-click, kb copy mode, OSC 52 | Browser-native |
| **OSC 8 hyperlink click-through** | Yes | xterm.js link addon |
| **Kitty graphics / sixel** | Yes (experimental) | No |
| **Kitty keyboard protocol** | Yes | No |
| **Notifications: in-app toast** | Yes | Yes |
| **Notifications: terminal (OSC)** | Yes | Partial |
| **Notifications: system / push** | Yes (system) | Yes (Web Push VAPIR) |
| **Sound alerts (per-agent muting)** | Yes (mp3, completion/request chimes) | No |
| **Auto-update / release-channel** | Yes (`herdr update`, latest.json, release notes modal) | No (manual brew/install.sh) |
| **Live binary handoff (running panes)** | Yes (experimental) | No |
| **Onboarding flow** | Yes | `onibi setup` interactive |
| **Doctor / diagnostics** | `herdr status` | `onibi doctor` (deeper: ports, DB, keyring) |
| **Config format** | TOML (`~/.config/herdr/config.toml`) | SQLite + JSON settings in app |
| **Live config reload** | Yes (`herdr server reload-config`) | No |
| **Session persistence format** | JSON snapshots | SQLite |
| **Test infra** | cargo-nextest, integration harness w/ socket fixtures | cargo test + vitest + RTL + shell e2e |
| **Build deps** | Zig 0.15 (vendored ghostty), Just | pnpm + Tauri CLI |
| **Nix flake** | Yes | No |
| **CHANGELOG discipline** | Staged `docs/next/CHANGELOG.md`, release scripts | Versioned launch notes |
| **Scrollback** | Configurable byte limit (default 10MB) | xterm.js scrollback |
| **CJK IME / cursor anchor** | Yes (experimental) | Browser-default |
| **Headless daemon mode** | Server is always headless-capable | Yes (`--headless --auto-transports`) |

---

## 2. Remaining herdr-parity gaps

Grouped by subsystem. Each item is concrete and scoped for implementation. Items marked `[DONE]` or `[PARTIAL]` were touched by the 2026-06-04 orchestration pass and are left here to preserve the original numbering used by the phase plan and GitHub issues.

### 2.1 Multiplexer model
1. **Workspace as first-class container** — persistent identity derived from git repo / folder; sidebar grouping; reorderable.
2. **Tabs scoped to workspace** — independent pane layouts per tab; not just agent tabs.
3. **Drag-reorder for workspaces / tabs / panes** in sidebar.
4. **Workspace-level collapse with roll-up state** (most-urgent agent state across panes).
5. **Mouse-native pane border drag-resize**.
6. **Pane tiling layout math** (true tmux-style splits beyond a fixed two-pane split).
7. **Zoom-pane toggle** (fullscreen a pane within tab).

### 2.2 Agent detection
8. **Heuristic agent detection** (process name + terminal-output regex) for 11 agents that have no hook: Cursor, Cline, Copilot, Gemini, Grok, Kimi, Kiro, Droid, Amp, Antigravity, Kilo.
9. **[PARTIAL] 4-state agent model**: `idle` / `working` / `blocked` / `done(unviewed)` — canonical values exist and some transitions are wired; done-unviewed/focus semantics and heuristic state sources remain.
10. **Smart state arbitration** combining: foreground process + integration hook + screen heuristic, with conflict resolution rules.
11. **Done-vs-idle distinction** (unviewed completion → idle once user focuses pane).
12. **Per-agent label override** (`herdr agent rename`).

### 2.3 Integrations (versioned hooks/plugins)
13. **Pi extension** (`~/.pi/agent/extensions/herdr-agent-state.ts`).
14. **OMP extension** (`~/.omp/agent/extensions/…`).
15. **OpenCode plugin** (`~/.opencode/plugins/…js`) — onibi has stub only.
16. **Hermes Python plugin** (`~/.hermes-agent/plugins/…`).
17. **Qoder CLI hook**.
18. **GitHub Copilot hook**.
19. **Integration version tracking** (`HERDR_INTEGRATION_VERSION` markers, `integration status --outdated-only`).
20. **`integration install/uninstall/status` CLI** as a unified subsystem (onibi has per-adapter only).

### 2.4 Socket / orchestration API
21. **[DONE] `wait output --match <text>/--regex --timeout`** — block until pane emits matching text.
22. **[PARTIAL] `wait agent-status --status {idle|working|blocked|done}`** — API exists; richer status sources and focus semantics remain under item 9.
23. **[PARTIAL] `pane read --source {visible|recent|recent-unwrapped} --format ansi`** — structured recent/ANSI reads exist; `visible` is tail-of-buffer and `recent-unwrapped` is missing.
24. **[DONE] `pane send-keys`** distinct from `send-text` (raw keys, e.g. `Enter`, `Ctrl+C`).
25. **[PARTIAL] `agent send` / `agent read` / `agent focus` / `agent start`** — commands exist; stable semantic agent addressing and richer focus behavior remain.
26. **[DONE] Event subscriptions** — pub/sub for session/status/PTY lifecycle events exists through `events subscribe`.
27. **[PARTIAL] JSON output flag on every CLI subcommand** for automation — global flag exists; legacy command paths still need uniform JSON envelopes.

### 2.5 Session persistence
28. **Detach / reattach a running session** (client exits, processes survive, reattach later).
29. **Named sessions** with isolated namespaces (`herdr session attach <name>`).
30. **`herdr session list / stop`**.
31. **Native agent session resume** — restore Claude/Codex/Pi/Hermes/OpenCode conversations on server restart.
32. **Live server handoff** — transfer running PTYs from old binary to new without interruption (experimental but spec'd).
33. **Pane history opt-in** (screen scrollback survives restart).

### 2.6 Remote
34. **SSH remote attach** (`herdr --remote ssh://…`) with auto-bootstrap install on the remote host.
35. **Remote keybindings policy** (`--remote-keybindings local|remote`).
36. **Image-paste bridging** (local clipboard image → remote staged file path).

### 2.7 Terminal / rendering
37. **Vendored libghostty-vt** terminal engine (sixels, Kitty graphics, OSC 8, Kitty keyboard protocol). xterm.js does not cover sixels / Kitty graphics.
38. **Kitty graphics protocol** support (experimental).
39. **Sixel** rendering.
40. **Kitty keyboard protocol** (enhanced key reporting, distinguishes `Ctrl+I` vs `Tab`, etc.).
41. **CJK IME cursor-anchor exposure** for input-method candidate placement.
42. **Transparent pane backgrounds** inheriting host terminal.

### 2.8 Selection / copy
43. **In-pane drag-select with autoscroll** into scrollback.
44. **Double-click word-token copy** (Unicode-aware boundaries) with visual feedback.
45. **Keyboard copy mode** (prefix+[, h/j/k/l, w/b/e, {/}, v/Space, y/Enter).
46. **ANSI-color-preserving copy** (codes optional via flag).
47. **OSC 52 clipboard fallback** for headless / SSH contexts.

### 2.9 Keybindings
48. **Explicit prefix-chord syntax v2** (`prefix+c`, `ctrl+alt+x`, `cmd+k`, `f1..f12`).
49. **Configurable prefix key** (default `Ctrl+B`).
50. **Per-action arrays** (multiple bindings per action).
51. **Indexed bindings family** (`prefix+1..9` → switch workspace/tab/agent by index, configurable).
52. **Custom command keybindings** (`[[keys.command]]` with trigger + shell action + description).
53. **Conflict detection report** at config load.
54. **`herdr config reset-keys`** to drop custom keymap.

### 2.10 Theming
55. **18 built-in themes** (catppuccin, tokyo-night, dracula, nord, gruvbox, one-dark, solarized, kanagawa, rose-pine, vesper + light variants + terminal).
56. **`terminal` theme** that inherits host ANSI palette.
57. **Per-theme custom-color overrides** in TOML.

### 2.11 Notifications & sound
58. **Sound alerts** with custom mp3 paths (completion + request chimes).
59. **Per-agent sound muting** (`[ui.sound.agents] droid = false`).
60. **Tab-aware suppression** (foreground tab stays quiet, background tabs notify).
61. **Toast delivery selector** — `in_app | terminal | system`.

### 2.12 Update / release
62. **Built-in updater** (`herdr update`, periodic check against `latest.json`, in-app release-notes modal).
63. **`--handoff` flag** for live in-place upgrade.

### 2.13 Configuration
64. **TOML config file** at `~/.config/herdr/config.toml` with full surface (keys, terminal shell, scrollback, UI, experimental).
65. **`--default-config` flag** to print the full default config.
66. **Live config reload** (`herdr server reload-config`).
67. **Shell-mode selector** (`auto | login | non_login`).
68. **`new_cwd` policy** (`follow | home | current | /fixed/path`) for new panes.
69. **Mobile-width threshold config** for the narrow layout.
70. **Mouse-capture toggle** (tmux-style passthrough on `false`).

### 2.14 Worktree
71. **Sidebar grouping by worktree** (already partial in onibi via Git UI, but no sidebar surface).
72. **`herdr worktree open <path>`** to launch a workspace tied to a worktree.

### 2.15 Build / distribution
73. **Nix flake** (`flake.nix`, `nix/package.nix`) with dev shell.
74. **Just task runner** (`just lint / ci / check / test / test-one / release-prepare / release-publish`).
75. **Staged-docs / staged-CHANGELOG** discipline (`docs/next/` mirror of next release).

### 2.16 Diagnostics / status
76. **`herdr status server | client`** with protocol version, socket path, uptime, pane count.
77. **Render-performance profiling** (`render_prof.rs`).

### 2.17 UI / UX details
78. **Right-click pane context menu**.
79. **Workspace navigator** (`prefix+w`) and session navigator (`prefix+g`) modal pickers.
80. **Keybind-help modal** (in-app reference).
81. **First-run onboarding flow**.
82. **Mobile-narrow TUI layout** with single-column workspace/agent switcher (separate from the PWA).
83. **Show-agent-labels-on-pane-borders** toggle.

### 2.18 GitHub issue coverage

Open issues created from the orchestration plan:
- #55 — daemon-owned PTY/session runtime. Implemented in the 2026-06-04 pass.
- #56 — JSON-lines socket protocol over Unix socket and localhost TCP. Implemented in the 2026-06-04 pass.
- #57 — hard GUI PTY bridge cutover to daemon runtime. Implemented in the 2026-06-04 pass.
- #58 — consistent `--json` output. Partially implemented; legacy CLI paths still need uniform JSON envelopes.
- #59 — pane read/send-keys/wait output. Mostly implemented; full VT-visible snapshot and `recent-unwrapped` remain.
- #60 — canonical agent status and `wait agent-status`. API implemented; state semantics and heuristics remain.
- #61 — agent-targeted commands and event subscriptions. Commands/events implemented; semantic agent addressing remains.

Issue cleanup recommendation after the implementation commit lands:
- Close #55, #56, and #57.
- Keep #58, #59, #60, and #61 open or split them into narrower follow-ups matching the remaining gaps above.

---

## 3. Suggested pivot phasing

Order is by leverage (high-value, low-blast-radius first). Numbers reference items above.

**Phase A — agent-multiplexer parity (foundation), remaining after 2026-06-04:**
1, 2, 5, 6, 9, 23, 28, 29, 30, 48, 64.

Completed or mostly completed from original Phase A:
21, 22, 24.

Additional orchestration items completed or partially completed outside original Phase A:
25, 26, 27.

**Phase B — agent ecosystem reach:**
8, 10, 11, 13, 14, 15 (replace stub), 16, 17, 18, 19, 20, 31.

**Phase C — terminal-native polish:**
3, 4, 7, 37 (decision: keep xterm.js or embed ghostty-vt), 38, 40, 43, 44, 45, 55, 56.

**Phase D — remote & distribution:**
34, 35, 62, 73, 74.

**Phase E — long tail:**
46, 49, 51, 52, 58, 59, 60, 63, 66, 67, 68, 75, 76, 78–83.

---

## 4. Items where onibi already differentiates (do NOT regress)

Keep as moat — herdr has no equivalent:
- Approval / gating layer with `updatedInput` edit pipeline.
- Mobile PWA with Web Push.
- Tailscale Funnel / Cloudflare Quick Tunnel / LAN HTTPS + mDNS + QR pairing.
- Bearer-token auth, keyring storage, token rotation, rate limiting, HSTS, CSP.
- Integrated CodeMirror editor + file tree + full Git staging/commit/diff UI.
- Review/diff baseline tracking with per-file accept/reject + audit trail.
- Headless Pi distribution (systemd unit, install script).
- `onibi doctor` depth (ports, DB, keyring, adapter binaries).
