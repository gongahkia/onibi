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
- Added visible pane reads from a maintained terminal-screen model and `recent-unwrapped` reads.
- Added unambiguous agent-target resolution by PTY/session ID, pane ID, agent ID, agent label, or title.
- Routed `agent focus` through orchestration status updates and the existing desktop focus bridge.
- Completed JSON output handling for the remaining CLI command families, including status, doctor, setup, desktop HTTP commands, transports, and adapter install/uninstall.
- Verified with `cargo test`, `cargo fmt --check`, `git diff --check`, and `cargo check --no-default-features`.

### Implemented in the session-persistence pass

- Created GitHub tracking issues #62–#64 for session persistence, CLI session control, and GUI rehydration.
- Added persisted daemon session metadata in `onibi.db`, including optional names, lifecycle state, dimensions, restart command metadata, and exit/stale markers.
- Added orchestration commands:
  - `session.list`
  - `session.attach`
  - `session.stop`
- Added CLI support for:
  - `onibi session launch --name <name>`
  - `onibi session list`
  - `onibi session attach <id-or-name>`
  - `onibi session stop <id-or-name>`
- Added case-insensitive name resolution with duplicate-name rejection among active sessions.
- Added stale-session relaunch from saved command metadata after daemon restart. This restores workflows by spawning a replacement PTY; it is not true process survival across daemon restart.
- Added GUI daemon metadata hydration so live daemon sessions are rebuilt in the desktop store and relayed to xterm, while stale restartable sessions keep restart metadata.
- Verified with `cargo fmt --check`, `cargo test`, `cargo check --no-default-features`, `pnpm --dir app typecheck`, `pnpm --dir app test`, and `git diff --check`.

### Implemented in the workspace-tabs foundation pass

- Added persisted `WorkspaceTab` state with `activeWorkspaceId` and `activeWorkspaceTabId`.
- Migrated legacy global terminal layouts into workspace-scoped terminal tabs during hydration.
- Kept legacy top-level terminal layout fields as mirrors for compatibility with older UI/store callers.
- Added a workspace-level terminal tab strip in the main pane.
- Scoped active terminal pane, maximized pane, split/tab placement, session replacement, session removal, file selection, and web-open flows through the active workspace tab.
- Wired the file tree, sidebar, welcome screen, and workspace picker to explicit active-workspace selection.
- Added focused React tests for empty workspace tabs and workspace-tab switching.
- Verified with `pnpm --dir app typecheck`, `pnpm --dir app test -- --run`, `cargo fmt --check`, `cargo test`, `cargo check --no-default-features`, and `git diff --check`.

### Implemented in the Phase A deepening pass

- Upgraded workspaces into first-class sidebar containers: all saved workspaces are visible, reorderable, persist collapse state, and show a session roll-up badge.
- Added drag-reorder for workspace terminal tabs.
- Persisted split-pane sizes through `react-resizable-panels` so workspace-tab layouts restore user-resized panes.
- Added configurable app keybindings with prefix-chord syntax (`prefix+c`, `ctrl+alt+x`, `cmd+k`, `f1..f12` style), default `Ctrl+B`, per-action multiple bindings, and settings conflict reporting.
- Wired prefix defaults for split right/down, pane focus, pane zoom, workspace navigation, workspace-tab navigation, session close, new session, command palette, and reopen closed editor.
- Added `~/.config/onibi/config.toml` support through GUI import/export/load/save, desktop hydration, CLI/headless defaults, and `onibi --default-config`.
- Added `newPaneCwd` policy support for new split panes (`active`, `workspace`, `home`).
- Verified with `pnpm --dir app typecheck`, `pnpm --dir app test`, `cargo check --manifest-path app/src-tauri/Cargo.toml`, and targeted React tests for FileTree, SettingsPane, MainPane, and CommandPalette.

### Implemented in the agent-detection heuristics pass

- Added a daemon-side heuristic detection registry for Cursor, Cline, Copilot, Gemini, Grok, Kimi, Kiro, Droid, Amp, Antigravity, and Kilo.
- Auto-populated `SessionInfo.agent` from explicit metadata first, then launch command/args/title, then conservative banner-style terminal output.
- Added agent-aware output status heuristics for known agent sessions, while preserving generic shell status inference from OSC 133 shell-integration markers.
- Added arbitration guardrails so explicit agent metadata wins over launch detection, approval-blocked sessions stay `blocked`, and exited/completed sessions stay `done` against weaker output heuristics.
- Expanded the frontend agent catalog and labels so newly detected daemon labels hydrate as real agents instead of falling back to plain shell.
- Added Rust tests for all 11 launch-command detections, output-marker detection, status inference, and arbitration; added a React/store hydration test for daemon-detected heuristic agent labels.
- Verified with `cargo fmt --check`, `cargo test`, `cargo check --no-default-features`, `pnpm --dir app typecheck`, `pnpm --dir app test -- --run`, and `git diff --check`.

### Implemented in the partials completion pass

- Added pane drag-reorder for terminal panes while preserving existing split-tree geometry and persisted split sizes.
- Added terminal layout preset commands for even horizontal, even vertical, main horizontal, main vertical, and tiled pane arrangements.
- Added daemon-side PTY child PID capture plus foreground process-tree polling for the 11 no-hook heuristic agents.
- Added optional `processId` session metadata across the Rust orchestration model and the Tauri/TypeScript bridge.
- Expanded `newPaneCwd` to support `follow` and `fixed:/absolute/path`, including settings UI, TOML normalization, and split/handoff spawn behavior for absolute CWDs.
- Added focused Rust process-inference tests and frontend store tests for pane reorder, layout presets, and CWD resolution.

### Implemented in the config/status foundation pass

- Added `onibi config validate` and `onibi config reload`.
- Added authenticated `/v1/config/status` and `/v1/config/reload` endpoints.
- Added server-runtime live reload for `server.approval_timeout_secs` and `server.pty_ring_limit`.
- Extended `/v1/status` with runtime config, uptime, config path, orchestration socket path, and pane/session counts.
- Added `onibi status server` and `onibi status client` while preserving the combined `onibi status` summary.
- Added focused Rust tests for config validation, status payloads, and config reload responses.

### Implemented in the keybinding finish pass

- Added indexed keybinding actions for workspaces, workspace terminal tabs, and terminal panes.
- Bound default `prefix+1..9` to workspace terminal tabs while keeping workspace and pane index actions configurable.
- Added custom shell command keybindings with `keys`, `command`, and optional `description`; commands send text to the active pane and press Enter.
- Added settings UI for custom command bindings and expanded conflict reporting across app actions and custom commands.
- Added `onibi config reset-keys` to restore default keybindings while preserving unrelated TOML config.
- Added focused Rust and frontend tests for indexed defaults, indexed focus, custom command dispatch, TOML round trips, conflict reporting, and key reset.

### Implemented in the integration lifecycle pass

- Added shared integration status metadata for adapter hooks, including install state, installed/bundled marker versions, outdated detection, install path, and status messages.
- Added Onibi integration version markers to newly installed Claude Code and Codex hook entries.
- Added `onibi integration status`, `onibi integration status --outdated-only`, `onibi integration list`, `onibi integration install <name>`, and `onibi integration uninstall <name>`.
- Kept `onibi adapter list/install/uninstall` as compatibility aliases backed by the richer lifecycle model.
- Extended setup/status/doctor output with the richer integration lifecycle status while preserving existing `adapters` JSON fields for compatibility.
- Added focused Rust tests for current, legacy unmarked, outdated, missing, install, uninstall, and outdated-only lifecycle cases.

### Implemented in the terminal selection/theme pass

- Enabled xterm's native right-click word selection and added double-click selection copy to the system clipboard.
- Kept explicit terminal copy keybindings wired to xterm's current selection, with empty selections ignored.
- Added a built-in `terminal` theme mode that uses imported/host terminal colors when available and falls back to the current dark terminal palette.
- Added focused frontend tests for terminal selection copy behavior, terminal theme option normalization, TOML round trips, and imported terminal color resolution.

### Implemented in the keyboard copy-mode pass

- Added `terminal.copyMode.enter` as an app keybinding action, defaulted to `prefix+[` in frontend settings and Rust config defaults.
- Added xterm-backed keyboard copy mode with `h/j/k/l`, `w/b/e`, `{`/`}`, `v` or `Space`, `y` or `Enter`, and `Escape`/`q`/`Ctrl+C` handling.
- Kept copy-mode state local to the active `TerminalView`, intercepting copy-mode keys before PTY input and reusing xterm's public selection/scrollback APIs.
- Added focused frontend and Rust tests for copy-mode entry, cursor movement, visual selection, copy/exit behavior, and default keybinding coverage.

### Implemented in the UI navigator polish pass

- Added a right-click terminal pane context menu with focus, maximize/restore, new tab, duplicate split, restart, handoff, copy IDs/path, and close actions.
- Added workspace and session navigator modals, exposed through the command palette and default prefix chords.
- Added an in-app keybinding help modal covering app, terminal, and custom command bindings.
- Changed defaults so `prefix+w` opens the workspace navigator, `prefix+g` opens the session navigator, `prefix+?` opens keybinding help, and close-active-session moves to `prefix+x`.
- Added pane agent labels plus a `show_terminal_pane_agent_labels` setting, Layout settings toggle, and TOML round trip.
- Verified with `pnpm --dir app typecheck` and `pnpm --dir app test -- --run src/lib/sessions.test.ts src/components/CommandPalette.test.tsx src/components/MainPane.test.tsx src/components/SettingsPane.test.tsx`.

### Implemented in the shell-mode selector pass

- Added a `terminal_shell_mode` setting with `auto`, `login`, and `non_login` values, plus a General Settings selector.
- Threaded shell mode through shell PTY spawn requests, restart metadata, duplicate sessions, stale shell replacement, and saved/restored arrangements.
- Added backend-aware PTY shell-mode handling with serde-compatible defaults for old requests and persisted sessions.
- Added focused frontend and Rust tests for settings updates, TOML round trips, shell launch propagation, restart metadata, serde defaults, and shell login flag handling.
- Verified with `pnpm --dir app typecheck`, `pnpm --dir app test -- --run src/lib/sessions.test.ts src/components/SettingsPane.test.tsx src/components/NewSessionDialog.test.tsx src/components/MainPane.test.tsx src/components/CommandPalette.test.tsx`, and `cargo test --manifest-path app/src-tauri/Cargo.toml`.

### Implemented in the native provider-event bridge pass

- Added authenticated provider-event ingestion at `/v1/adapters/:agent/event`, normalized across snake_case, camelCase, and nested provider payloads.
- Added provider session metadata to daemon sessions, including provider session ID, conversation ID, native resume command metadata, and last update time.
- Added native event arbitration so provider lifecycle/tool events can set `idle`, `working`, `blocked`, and `done` through the same orchestration status stream used by the GUI and CLI.
- Added conservative provider-to-PTY correlation by exact Onibi session ID, known provider IDs, then one active session with matching agent and cwd.
- Replaced the OpenCode stub with a versioned local plugin installer and native resume metadata using `opencode --session <id>`.
- Added Qoder, GitHub Copilot, and Goose event-bridge integrations, plus resume metadata for Qoder and Goose.
- Registered Gemini and Hermes as resume-only integrations, Aider as history-restore only, Cursor/Pi/OMP as explicit pending native-hook integrations.
- Extended setup/doctor/status integration handling for `event-bridge`, `resume-only`, and `pending` support classes.
- Verified with `cargo test --manifest-path app/src-tauri/Cargo.toml` and `pnpm --dir app typecheck`.

### Implemented in the native provider blocking-approval pass

- Upgraded Qoder, GitHub Copilot CLI, Goose, and OpenCode from observe-only provider events to synchronous pre-tool approval blocking through Onibi.
- Added shared provider hook approval handling for stdin command hooks, including fail-closed deny behavior and provider-specific allow/deny response formats.
- Fixed the Qoder hook matcher to use match-all groups without an invalid `"*"` regex matcher.
- Bumped the Onibi integration marker so old observe-only hook installs are reported as outdated and can be reinstalled.
- Updated adapter docs and tests for provider-specific blocking output, edited-input forwarding, installer config, and OpenCode plugin source.

### Implemented in the terminal reliability hardening pass

- Kept xterm.js as the active terminal path and added WebGL renderer auto-load with safe fallback.
- Added frontend PTY output write batching to reduce render pressure under high-throughput output without dropping bytes.
- Hardened terminal replay/attach ordering so live events are queued until replay resolves, duplicate replay/live overlap is deduped, replay failure can still render live output, and queued exit events flush after replay output.
- Added `terminal_screen_reader_mode` support through settings, TOML round trips, and xterm's `screenReaderMode`.
- Guarded terminal keybinding interception during IME composition so composed input reaches xterm.
- Tightened deterministic session/workspace restoration so cleared workspace state is not repopulated from daemon history, and routed AgentTabBar context closes through the shared `closeSession` lifecycle path.
- Added PTY replay buffer offset/truncation tests plus PTY harness coverage for replay, subscribe-before-snapshot ordering, input echo, resize visibility, and exit status.
- Verified with `pnpm --dir app typecheck`, `pnpm --dir app test`, `cargo test pty --lib`, `cargo test --lib`, and `git diff --check`.

### Implemented in the terminal polish pass

- Added `terminal_copy_format` with `plain`, `ansi`, and `html` modes; plain text remains the default.
- Added ANSI-preserving copy for copy-mode row/range selections through xterm's serialize addon.
- Added rich HTML clipboard writes with plain-text fallback for WebView/browser clipboard compatibility.
- Added opt-in OSC 52 clipboard writes through xterm's OSC parser, with query/malformed/oversized payloads ignored.
- Added `terminal_transparent_background` support so xterm can inherit pane/app backgrounds when explicitly enabled.
- Added terminal render-profile debug counters for bytes, chunks, batches, flush latency, replay duration, and total duration under `localStorage.onibiTerminalDebug = "1"`.
- Added focused frontend and config tests for copy formats, OSC 52 gating, transparent backgrounds, profiling diagnostics, Settings UI, and TOML round trips.

### Implemented in the terminal inline-image and profiling pass

- Added opt-in `terminal_inline_images = "off" | "sixel" | "iterm" | "auto"` support through `@xterm/addon-image`; the default remains `"off"`.
- Added Settings UI for inline image protocol mode and TOML round-trip coverage.
- Added structured terminal render-profile reports under `localStorage.onibiTerminalDebug = "1"` with bounded recent in-memory storage and `onibi:terminal-render-profile` events.
- Added Command Palette support for `Copy Terminal Render Profile`, which requests the active terminal's current profile and copies JSON to the clipboard.
- Added focused frontend tests for image-addon gating, opt-in profile events, profile copy, and config persistence.

### Implemented in the notifications UX pass

- Added quiet-default notification settings for delivery target, foreground-tab suppression, sound enablement, custom completion/request sound paths, and per-agent sound muting.
- Added a shared frontend notification dispatcher for terminal triggers, OSC PTY notifications, in-app toasts, system notifications, terminal notices, and configured sounds.
- Added in-app toast host with compact dismissible toasts and OSC PTY notification routing.
- Routed terminal trigger `notify` actions and terminal-exit completion sound intents through the shared dispatcher.
- Added focused tests for settings/config round trips, dispatcher delivery modes, foreground suppression, per-agent muting, OSC toast routing, and terminal notices.

### Implemented in the workspace-first UX polish pass

- Added per-agent display label overrides in Settings, persisted through `settings.agent_label_overrides` in `config.toml`.
- Routed agent/session labels through the override-aware display helper across terminal panes, tabs, session lists, history, handoff, new-session, and command-palette surfaces.
- Added shared workspace open/activate and workspace removal helpers so worktree entrypoints behave consistently and removed workspaces close owned sessions before pruning UI state.
- Added Explorer empty-state cleanup so clearing all workspaces leaves the file tree empty instead of showing stale workspace contents.
- Surfaced Git worktrees in Explorer with open-as-workspace and launch-default-agent actions, and added matching Command Palette worktree commands.
- Added `onibi worktree open <path> [--agent <agent>] [--prompt <text>]` through the desktop command bridge.
- Added focused frontend/config tests for label overrides, empty workspace state, Explorer worktree open, and Command Palette worktree open.

### Implemented in the Remote Basics V1 pass

- Added SSH-backed local PTY remote sessions through `buildRemoteSshLaunchSpec`, `spawnRemoteSshSession`, the Command Palette, and a dedicated Remote SSH dialog.
- Added first-class remote session metadata (`kind`, `target`, `user`, `host`, `port`, `remoteCwd`, `keybindingPolicy`) across frontend sessions, Tauri spawn requests, daemon restart metadata, persisted daemon sessions, desktop snapshots, and attach/relaunch flows.
- Added `onibi remote ssh <target> --workspace <path> [--cwd <remote-dir>] [--name <title>] [--keybindings local|remote] [--ssh-command <command>]`.
- Added authenticated `/v1/desktop/remote/ssh` support and desktop bridge execution for paired clients.
- Added a global and per-session remote keybinding policy. `local` keeps Onibi terminal shortcuts active; `remote` lets normal terminal shortcuts pass through to the SSH session while preserving local copy-mode.
- Added opt-in pane history persistence with `pane_history_enabled`, including settings UI, config/TOML round trip, persisted transcript retention, and restored-history display when no live PTY replay is available.
- Kept the V1 remote scope intentionally local-PTY based: no remote daemon bootstrap, no fake remote file workspace, and no image-paste bridge yet.
- Verified with `pnpm --dir app typecheck`, `pnpm --dir app test -- --run`, `cargo check --manifest-path app/src-tauri/Cargo.toml`, `cargo test --manifest-path app/src-tauri/Cargo.toml`, and `git diff --check`.

### Implemented in the Remote V2 SSH bootstrap and image bridge pass

- Added current-binary SSH helper bootstrap with default remote helper path `~/.onibi/bin/onibi` and default staging directory `~/.onibi/staged`.
- Added remote bootstrap metadata (`bootstrapStatus`, `helperPath`, `helperVersion`, `stagingDir`, `lastBootstrapAt`) across TypeScript sessions, Rust PTY metadata, restart metadata, hydration, and relaunch persistence.
- Added Tauri commands for `clipboard_read_image_png`, `remote_ssh_bootstrap`, and `remote_ssh_stage_file`.
- Added Command Palette actions for `Bootstrap Active Remote` and `Paste Clipboard Image to Remote`, visible only for active SSH sessions.
- Implemented clipboard image staging by reading the local clipboard image as PNG, streaming it to the remote staging directory over SSH, and pasting only the returned remote staged path into the active remote terminal.
- Added `onibi remote bootstrap ssh <target> --workspace <path> [--cwd <remote-dir>] [--ssh-command <command>] [--helper-path <remote-path>] [--staging-dir <remote-dir>]`.
- Added focused frontend tests for remote metadata persistence, bootstrap command dispatch, image staging, no-image errors, and path paste; added Rust tests for SSH command/script construction and staged filename validation.

### Implemented in the built-in updater pass

- Added Tauri v2 updater integration for signed GUI updater artifacts, backed by GitHub Releases `latest.json`.
- Added a root-level update dialog, automatic once-per-24-hours update checks, and manual `Check for Updates` entrypoints from the Command Palette and Settings.
- Added headless update commands: `onibi update check` and `onibi update install [--yes]`, including manifest parsing, SemVer comparison, platform asset selection, SHA256 verification, optional P-256 signature verification, atomic binary replacement, and user `onibi.service` restart when present.
- Extended the GitHub release workflow to stage updater artifacts, publish `latest.json` and `latest-headless.json`, and require signing material from GitHub Actions secrets/variables.
- Documented desktop/headless update use and release signing setup in `docs/updating.md`.

### Implemented in the approval-cockpit launch pass

- Added Apache-2.0 licensing and repositioned README/launch copy around "local-first approval gate for multi-vendor coding agents."
- Split README adapter support into approval-blocking adapters and mirror/resume/pending-native adapters, removed acknowledgements from launch positioning, and verified README-linked launch assets exist.
- Hardened desktop approvals with queue state, risk badges, optional deny reasons, edit-specific approve CTA, and timeout countdowns.
- Hardened the mobile PWA with transport fallback, targeted approval deep links, risk badges, optional deny reasons, clearer offline/transport state, and push-notification deep links to `/m/?approval=<id>`.
- Added approval history storage filters, authenticated history/export endpoints, and an Activity Center approval audit tab with search, decision/edited filters, JSONL export, and daemon-unavailable state.
- Added backend TOML policy evaluation from `~/.config/onibi/policies.toml`, including `auto-allow`, `always-ask`, `always-deny`, `require_edit`, first-match behavior, audit logging for auto-decisions, and policy validation in config status.
- Added GUI first-run onboarding for default agent selection, adapter install guidance, QR pairing, and safe canary approval.
- Completed small visual/status fixes: transport checking/offline/no-transport states, 4px terminal padding, focus-by-opacity for terminal splits, and light-theme-safe chrome token backgrounds.
- Completed the audit UI polish pass with agent/tool/date filters, decision/edited/search filters, per-tool aggregates, JSONL export of visible rows, and an inline proposed-vs-final diff for edited approvals.
- Completed policy visibility polish with `docs/policies.md`, `onibi config validate --json` policy status, Settings-pane policy validation status, and matched policy names shown in approval requests.
- Added diff-aware file-edit approval previews for `Write`, `Edit`, and `MultiEdit` tool payloads. These are static inline line diffs; a full CodeMirror side-by-side diff remains optional polish.
- Added the mobile PWA kill switch, backed by `/v1/emergency-stop`, which stops daemon-owned PTYs and denies all pending approvals.
- Replaced destructive browser confirmations with native Tauri confirmation dialogs while leaving the synchronous, non-destructive "open URL inside Onibi?" chooser alone.
- Added approval attention escalation: one informational OS attention request per new approval when foreground suppression allows it, an approval activity-tile pulse, and a title-bar approval pulse for non-suppressed active-session approval events.
- Verified with frontend/mobile typechecks and tests, full Rust tests, Rust fmt check, no-default-features check, `git diff --check`, and a Playwright smoke pass.
- Selected the next implementation direction as the full chrome-restraint pass: persistent transport/cwd/font-size/encoding chrome can move out of the StatusBar, the command-palette pill can fade after discovery, and non-approval badges should be visually quiet.

### Implemented in the chrome-restraint pass

- Persisted workspace-sidebar collapse state and kept the empty vertical agent rail to only the `New session` affordance.
- Collapsed the desktop StatusBar to a near-invisible idle edge, with the visible status surface reserved for pending approvals.
- Added a headless transport watcher that emits reachability/offline notifications instead of keeping transport state in persistent chrome.
- Added a transient focus HUD for active cwd, git branch/change count, and last exit code.
- Made the title-bar command-palette affordance fade to a search glyph after first use, tracked in `localStorage`.
- Shrank the Activity bar and moved approval badges/risk markers onto a dedicated attention token instead of the regular blue accent.
- Moved ApprovalModal styling out of inline hard-coded dark styles and into shared CSS classes/tokens with restrained buttons and file-preview rows.
- Added app-level `theme = "system"` refresh on OS color-scheme changes.

### Implemented in the ApprovalModal + chrome cleanup pass

- Replaced static ApprovalModal file previews with read-only CodeMirror previews: Bash uses shell highlighting, `Write` shows highlighted new file content with path, `Edit` shows side-by-side before/after panes, and `MultiEdit` paginates edits 5 per page with edit counts.
- Kept the existing edit-submission contract: Bash edits still submit `{ ...input, command }`; non-Bash edits still parse JSON or fall back to raw text.
- Added persisted sidebar first-launch/default-hide state. The sidebar is visible on first launch, auto-collapses on later launches only without an explicit user preference, and user toggles are preserved as explicit.
- Added persisted `light:X,dark:Y` theme-pair support, treated legacy `theme = "system"` as the default VS Code light/dark pair, and added Settings light/dark pair selectors.
- Kept `Ctrl+B` reserved for the terminal prefix; the Activity bar remains the explicit sidebar toggle path.
- Hid the workspace terminal tab strip when the active workspace has only one terminal tab.
- Finished the scoped chrome token pass for this roadmap slice: JetBrains Mono-first mono defaults, terminal/editor/approval-code ligatures disabled, search highlight tokens, and approval/attention indicators moved off generic accent tokens.
- Verified with `pnpm --dir app typecheck`, direct focused Vitest run for ApprovalModal/MainPane/SettingsPane/CommandPalette/sessions/App, full single-worker Vitest run, and `git diff --check`. The default-pool `pnpm --dir app test` run produced one `EditorBuffer.test.tsx` timeout, but that file passed isolated and the full single-worker suite passed.

### Still out of scope after the orchestration pass

- True live PTY/process survival across daemon restart or binary handoff is still not implemented. Restart persistence is relaunch-based.
- Full native coverage for Pi/OMP/Cursor remains pending until stable public hook/plugin APIs are verified.
- Provider-native blocking approval now covers Claude Code, Bash-only Codex, OpenCode, Qoder, GitHub Copilot CLI, and Goose. Pi/OMP/Cursor remain pending on stable provider APIs.
- xterm.js remains the chosen terminal path for now; libghostty-vt, Kitty graphics, and Kitty keyboard parity remain future terminal-native work.
- Remote V2 stages the current Onibi binary and files over SSH, but it still does not run a persistent remote Onibi daemon or transfer live remote PTYs between binaries.

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
| **Persistent sessions** | Yes — server/client, named sessions, survive restart | Partial — SQLite metadata, named sessions, live daemon attach, and relaunch-based restart resume; no true process survival across daemon restart |
| **Multi-client attach** | Yes — multiple terminals on same session | Partial — multiple clients via WebSocket and orchestration event subscriptions |
| **Detach / reattach** | Yes — full | Partial — GUI/CLI can attach to daemon-owned PTYs while daemon is alive; stale sessions relaunch from metadata after daemon restart |
| **Workspaces** | First-class (project containers, persistent identity) | Yes — persistent workspace identity, all-workspace sidebar containers, active workspace selection, reorder, collapse, roll-up badges, and workspace-scoped terminal tabs |
| **Tabs** | First-class | Yes — editor/agent tabs plus workspace-scoped terminal tabs with independent pane layouts |
| **Panes (real splits)** | Yes — drag-resize, mouse-native, tiling | Yes — vertical/horizontal split with focus, pane zoom, drag-reorder, layout presets, persisted sizes, and per-workspace-tab persistence |
| **Pane runtime** | Real PTYs via portable-pty + ghostty VT | Real PTYs via portable-pty |
| **Workspace/tab/pane reorder (drag/drop)** | Yes | Yes — workspace reorder, workspace terminal-tab reorder, and terminal pane reorder |
| **Agent detection** | 18 agents (auto, process + heuristic) | Partial — explicit metadata, provider-event metadata, command/title/banner-output heuristics, and foreground process polling for 11 no-hook agents; Pi/OMP/Cursor native hooks still pending |
| **Agent state model** | 4 states: idle / working / blocked / done (unviewed) | Yes — canonical statuses, approval/exit transitions, native provider events, shell/agent output heuristics, arbitration guardrails, and focus clear are wired |
| **Direct integrations (hook/plugin)** | 8 versioned: pi, omp, claude, codex, opencode, hermes, qodercli, copilot | Partial — Claude Code HTTP, Codex Bash, OpenCode plugin, Qoder/Copilot/Goose event hooks, Gemini/Hermes resume-only, Pi/OMP/Cursor pending |
| **Heuristic detection (no hook)** | 11 agents (Cursor, Cline, Copilot, Gemini, Grok, Kimi, …) | Yes — launch/title/banner-output detection plus foreground process polling for all 11 |
| **Native agent session resume** | Yes — Claude/Codex/Pi/Hermes/OpenCode convos restore | Partial — saved command relaunch plus provider-ID native resume metadata for Claude/OpenCode/Gemini/Qoder/Hermes/Goose when captured |
| **Approval / gating layer** | No | Yes (primary feature) |
| **Edit tool input before approve** | N/A | Yes (Claude Code `updatedInput`) |
| **Socket API for agents** | Yes — Unix socket, wire protocol v12, bincode | Yes — HTTP/WebSocket plus JSON-lines orchestration over Unix socket + localhost TCP |
| **API: wait-for-output** | Yes (`herdr wait output --match … --regex`) | Yes |
| **API: wait-for-agent-status** | Yes (`herdr wait agent-status`) | Yes |
| **API: pane read (visible/recent/ANSI)** | Yes | Yes — structured visible/recent/recent-unwrapped/ANSI reads |
| **API: pane send-text / send-keys / run** | Yes | Partial — PTY write + common `send-keys`; no explicit `run` helper |
| **CLI surface** | Very broad: workspace/tab/pane/agent/worktree/wait/session/integration/status/config | Expanded: setup/doctor/adapter/transport/token/session/pane/wait/agent/worktree/events with global JSON output |
| **Remote attach** | Yes — SSH bootstrap, auto-install on remote | Partial — SSH-backed local PTY remote sessions with metadata, CLI/API/dialog launch, and remote keybinding policy; no remote daemon bootstrap yet |
| **Mobile / phone UX** | Mobile-narrow TUI layout only | First-class installable PWA |
| **Transport: Tailscale Funnel** | No | Yes |
| **Transport: Cloudflare Quick Tunnel** | No | Yes |
| **Transport: LAN HTTPS + mDNS + QR pair** | No | Yes |
| **Bearer-token auth / pairing** | No (Unix-socket trust) | Yes (ULID token, keyring, rotate) |
| **TLS / HSTS / CSP** | No | Yes |
| **Rate limiting** | No | Yes (tower_governor) |
| **Git worktree management** | Yes (`herdr worktree …`) | Yes — create/remove/list, Explorer/sidebar open and launch actions, and `onibi worktree open` |
| **Git status / staging / commit UI** | Status caching, sidebar indicators | Full UI (staging, commit, diffs, clone, push/pull) |
| **Filesystem browse / read / write** | No (it is a multiplexer) | Yes (file tree, search, CRUD) |
| **In-app editor (CodeMirror)** | No | Yes (syntax HL, Vim mode) |
| **Review/diff baseline tracking** | No | Yes (SHA256 snapshots, per-file accept/reject) |
| **Themes** | 18 built-in (catppuccin, dracula, nord, …) | None advertised |
| **Keybindings: prefix + chord (tmux-style)** | Yes (`ctrl+b` prefix, configurable v2 syntax) | Yes — configurable prefix plus per-action app keybindings, navigator/help defaults, custom command bindings, and conflict reporting |
| **Vim mode** | Yes in copy mode (h/j/k/l, w/b/e, v/y) | Yes (CodeMirror Vim) |
| **Command palette (Cmd+K)** | No | Yes |
| **Mouse-native (click-focus, drag-resize, drag-reorder)** | Yes | Partial |
| **Copy mode (text selection, ANSI-aware)** | Yes — drag, double-click, kb copy mode, OSC 52 | Yes — drag, double-click, keyboard copy mode, plain/ANSI/HTML copy formats, and opt-in OSC 52 clipboard writes are wired |
| **OSC 8 hyperlink click-through** | Yes | xterm.js link addon |
| **Kitty graphics / sixel** | Yes (experimental) | Sixel and iTerm inline images are opt-in through `@xterm/addon-image`; Kitty graphics remain open |
| **Kitty keyboard protocol** | Yes | No |
| **Notifications: in-app toast** | Yes | Yes |
| **Notifications: terminal (OSC)** | Yes | Yes — OSC PTY notifications route through the delivery selector |
| **Notifications: system / push** | Yes (system) | Yes (Web Push VAPIR) |
| **Sound alerts (per-agent muting)** | Yes (mp3, completion/request chimes) | Yes — custom/generated completion/request chimes plus per-agent mute map |
| **Auto-update / release-channel** | Yes (`herdr update`, latest.json, release notes modal) | Yes — signed Tauri GUI updates via GitHub `latest.json`, prompt-based install/relaunch, and headless `onibi update check/install` via `latest-headless.json` |
| **Live binary handoff (running panes)** | Yes (experimental) | No |
| **Onboarding flow** | Yes | `onibi setup` interactive |
| **Doctor / diagnostics** | `herdr status` | `onibi doctor` (deeper: ports, DB, keyring) |
| **Config format** | TOML (`~/.config/herdr/config.toml`) | TOML source at `~/.config/onibi/config.toml` plus SQLite/session store state |
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

Grouped by subsystem. Each item is concrete and scoped for implementation. Items marked `[DONE]`, `[PARTIAL]`, or `[BLOCKED]` were touched by the 2026-06-04 orchestration pass and are left here to preserve the original numbering used by the phase plan and GitHub issues.

### 2.1 Multiplexer model
1. **[DONE] Workspace as first-class container** — persistent identity, all-workspace sidebar containers, active workspace selection, reorder, collapse, and roll-up state exist.
2. **[DONE] Tabs scoped to workspace** — independent terminal pane layouts per workspace tab.
3. **[DONE] Drag-reorder for workspaces / tabs / panes** — workspace reorder, workspace terminal-tab reorder, and terminal pane reorder are done.
4. **[DONE] Workspace-level collapse with roll-up state** (most-urgent agent state across panes).
5. **[DONE] Mouse-native pane border drag-resize** — split sizes persist per workspace terminal tab.
6. **[DONE] Pane tiling layout math** — split tree and split sizes persist per workspace tab, and command-palette layout presets cover even horizontal, even vertical, main horizontal, main vertical, and tiled arrangements.
7. **[DONE] Zoom-pane toggle** (fullscreen a pane within tab).

### 2.2 Agent detection
8. **[DONE] Heuristic agent detection** (process name + terminal-output regex) for 11 agents that have no hook: Cursor, Cline, Copilot, Gemini, Grok, Kimi, Kiro, Droid, Amp, Antigravity, Kilo. Launch command/args/title, conservative banner-output detection, and foreground process polling are done.
9. **[DONE] 4-state agent model**: `idle` / `working` / `blocked` / `done(unviewed)` — canonical values, approval/exit transitions, lightweight output heuristics, and focus clear are wired.
10. **[DONE] Smart state arbitration** combining: foreground process + native provider event + screen/output heuristic, with conflict resolution rules. Explicit/provider metadata now persists on sessions, native events feed the same status stream, and weaker heuristics still cannot override terminal completion guardrails.
11. **[DONE] Done-vs-idle distinction** (unviewed completion → idle once user focuses pane).
12. **[DONE] Per-agent label override** (`herdr agent rename`) — global per-agent display labels are configurable in Settings and persisted through `settings.agent_label_overrides`.

### 2.3 Integrations (versioned hooks/plugins)
13. **[BLOCKED] Pi extension** (`~/.pi/agent/extensions/herdr-agent-state.ts`) — registered as pending until a stable public native extension API is verified.
14. **[BLOCKED] OMP extension** (`~/.omp/agent/extensions/…`) — registered as pending until a stable public native extension API is verified.
15. **[DONE] OpenCode plugin** — versioned local plugin installer at `~/.config/opencode/plugins/onibi-provider-events.js`, provider-event ingestion, synchronous `tool.execute.before` approval blocking, approve-with-edits arg updates, and `opencode --session <id>` resume metadata are wired.
16. **[PARTIAL] Hermes Python plugin** (`~/.hermes-agent/plugins/…`) — Hermes is registered as resume-only with `hermes --resume <id>` metadata; plugin hook installation remains pending.
17. **[DONE] Qoder CLI hook** — versioned command-hook installer records session/tool lifecycle events, blocks `PreToolUse` through Onibi with exit-code-2 denial, supports `updatedInput`, and preserves native `qoder -r <id>` resume metadata.
18. **[DONE] GitHub Copilot hook** — versioned hook config records session/tool/stop/error events through `onibi _hook copilot`, blocks `PreToolUse` through Onibi, and maps approved edits to Copilot `modifiedArgs`.
19. **[DONE] Integration version tracking** — Onibi-native integration markers are tracked for Claude Code and Codex, and `integration status --outdated-only` reports stale/missing marker installs.
20. **[DONE] `integration install/uninstall/status` CLI** as a unified subsystem, with existing `adapter` commands preserved as compatibility aliases.

### 2.4 Socket / orchestration API
21. **[DONE] `wait output --match <text>/--regex --timeout`** — block until pane emits matching text.
22. **[DONE] `wait agent-status --status {idle|working|blocked|done}`** — block until agent state transitions.
23. **[DONE] `pane read --source {visible|recent|recent-unwrapped} --format ansi`** — structured snapshot reads with ANSI preservation.
24. **[DONE] `pane send-keys`** distinct from `send-text` (raw keys, e.g. `Enter`, `Ctrl+C`).
25. **[DONE] `agent send` / `agent read` / `agent focus` / `agent start`** — commands exist with unambiguous ID or agent-label targeting.
26. **[DONE] Event subscriptions** — pub/sub for session/status/PTY lifecycle events exists through `events subscribe`.
27. **[DONE] JSON output flag on every CLI subcommand** for automation.

### 2.5 Session persistence
28. **[DONE] Detach / reattach a running session** — daemon-owned PTYs survive GUI/CLI detach while the daemon is alive; GUI hydration reattaches and relays live sessions.
29. **[DONE] Named sessions** with case-insensitive active-name uniqueness and `onibi session attach <name>`.
30. **[DONE] `herdr session list / stop` equivalent** — `onibi session list`, `onibi session stop <id-or-name>`, and JSON output exist.
31. **[PARTIAL] Native agent session resume** — Onibi PTY restoration is now deterministic for live daemon sessions and stale relaunch fallbacks. Provider-native resume metadata can prefer native resume commands for Claude Code, OpenCode, Gemini, Qoder, Hermes, and Goose when a provider session/conversation ID has been captured. Codex remains unverified and Aider is history-restore only.
32. **Live server handoff** — transfer running PTYs from old binary to new without interruption (experimental but spec'd).
33. **[DONE] Pane history opt-in** (screen scrollback survives restart) — persisted transcript retention is opt-in through `pane_history_enabled` and restores when no live replay is available.

### 2.6 Remote
34. **[DONE] SSH remote attach** (`herdr --remote ssh://…`) — SSH-backed local PTY sessions are implemented through GUI, CLI, desktop API, remote metadata persistence, keybinding policy, and current-binary helper bootstrap. Persistent remote daemon handoff remains a separate future capability.
35. **[DONE] Remote keybindings policy** (`--remote-keybindings local|remote`) — available globally, per GUI launch, and through `onibi remote ssh --keybindings`.
36. **[DONE] Image-paste bridging** (local clipboard image → remote staged file path) — available through the Command Palette for active SSH sessions.

### 2.7 Terminal / rendering
37. **[PARTIAL] Terminal engine parity / vendored libghostty-vt option** — xterm.js remains the chosen implementation path for now, with WebGL fallback, throughput batching, replay hardening, screen-reader mode, and opt-in inline images wired. A libghostty-vt embed remains a future option only if xterm.js cannot cover required Kitty graphics or Kitty keyboard parity.
38. **Kitty graphics protocol** support (experimental).
39. **[DONE] Sixel** rendering — opt-in through `terminal_inline_images = "sixel"` or `"auto"` using `@xterm/addon-image`.
40. **Kitty keyboard protocol** (enhanced key reporting, distinguishes `Ctrl+I` vs `Tab`, etc.).
41. **[PARTIAL] CJK IME cursor-anchor exposure** for input-method candidate placement — IME composition is no longer intercepted by terminal keybindings, but native cursor-anchor placement is still not exposed.
42. **[DONE] Transparent pane backgrounds** inheriting host terminal — available behind `terminal_transparent_background`.

### 2.8 Selection / copy
43. **[DONE] In-pane drag-select with autoscroll** into scrollback — xterm native selection remains active and supports scrollback drag selection.
44. **[DONE] Double-click word-token copy** — xterm word selection is copied to the system clipboard on double-click.
45. **[DONE] Keyboard copy mode** — `prefix+[` enters copy mode; `h/j/k/l`, `w/b/e`, `{`/`}`, `v` or `Space`, `y` or `Enter`, and `Escape`/`q`/`Ctrl+C` are wired through xterm selection/scrollback APIs.
46. **[DONE] ANSI-color-preserving copy** — `terminal_copy_format = "ansi"` copies serialized ANSI row/range data from copy mode.
47. **[DONE] OSC 52 clipboard fallback** for headless / SSH contexts — opt-in `terminal_osc52_clipboard` handles clipboard-set payloads and ignores query/malformed/oversized data.

### 2.9 Keybindings
48. **[DONE] Explicit prefix-chord syntax v2** (`prefix+c`, `ctrl+alt+x`, `cmd+k`, `f1..f12`).
49. **[DONE] Configurable prefix key** (default `Ctrl+B`).
50. **[DONE] Per-action arrays** (multiple bindings per action).
51. **[DONE] Indexed bindings family** — default `prefix+1..9` switches workspace terminal tabs; workspace and pane index actions are configurable.
52. **[DONE] Custom command keybindings** (`[[keybindings.command]]` with trigger + shell action + description).
53. **[DONE] Conflict detection report** in settings.
54. **[DONE] `herdr config reset-keys` equivalent** — `onibi config reset-keys` restores default keybindings and drops custom command bindings.

### 2.10 Theming
55. **[DONE] Built-in themes** — onibi ships broad built-in app/terminal color schemes including VSCode, GitHub, One Dark, Dracula, Catppuccin, Tokyo Night, Night Owl, Material, Monokai, Gruvbox, Nord, Ayu, Palenight, Cobalt, Shades of Purple, Synthwave, Solarized, Onibi Flame, and Terminal.
56. **[DONE] `terminal` theme** that inherits imported/host terminal colors where available and falls back to the current dark terminal palette.
57. **Per-theme custom-color overrides** in TOML.

### 2.11 Notifications & sound
58. **[DONE] Sound alerts** with custom mp3 paths (completion + request chimes) — quiet by default, with custom paths or generated WebAudio chimes when enabled.
59. **[DONE] Per-agent sound muting** (`[ui.sound.agents] droid = false`) — implemented as `settings.sound_agents`.
60. **[DONE] Tab-aware suppression** (foreground tab stays quiet, background tabs notify).
61. **[DONE] Toast delivery selector** — `in_app | terminal | system`.

### 2.12 Update / release
62. **[DONE] Built-in updater** (`herdr update`, periodic check against `latest.json`, in-app release-notes modal) — GUI update checks/install prompts use Tauri's signed updater through GitHub Releases, and headless installs use `onibi update check/install` with checksum/signature verification.
63. **`--handoff` flag** for live in-place upgrade.

### 2.13 Configuration
64. **[DONE] TOML config file** at `~/.config/onibi/config.toml` for keys, terminal shell defaults, scrollback, UI defaults, server port/limits, and workspaces.
65. **[DONE] `--default-config` flag** to print the full default config.
66. **[DONE] Live config reload** — `onibi config reload` applies server-runtime fields (`approval_timeout_secs`, `pty_ring_limit`) without daemon restart; port/UI/keybinding/workspace changes remain restart/client-managed.
67. **[DONE] Shell-mode selector** (`auto | login | non_login`) — settings/TOML, shell PTY request metadata, restart/duplicate/arrangement persistence, and backend login-mode handling are wired.
68. **[DONE] `new_cwd` policy** — implemented `active`, `follow`, `workspace`, `home`, and `fixed:/absolute/path`.
69. **Mobile-width threshold config** for the narrow layout.
70. **Mouse-capture toggle** (tmux-style passthrough on `false`).

### 2.14 Worktree
71. **[DONE] Sidebar grouping by worktree** — Explorer surfaces Git worktrees under each workspace with open and launch-default-agent actions.
72. **[DONE] `herdr worktree open <path>`** to launch a workspace tied to a worktree — implemented as `onibi worktree open <path> [--agent <agent>] [--prompt <text>]` through the desktop bridge.

### 2.15 Build / distribution
73. **Nix flake** (`flake.nix`, `nix/package.nix`) with dev shell.
74. **Just task runner** (`just lint / ci / check / test / test-one / release-prepare / release-publish`).
75. **Staged-docs / staged-CHANGELOG** discipline (`docs/next/` mirror of next release).

### 2.16 Diagnostics / status
76. **[DONE] `herdr status server | client` equivalent** — `onibi status server` and `onibi status client` report protocol/version, config path, runtime config, socket path, uptime, pane/session counts, DB/device/adapters, and daemon reachability.
77. **[DONE] Render-performance profiling** (`render_prof.rs`) — debug render-profile reports are requestable/copyable under `onibiTerminalDebug`, including renderer, dimensions, bytes/chunks/batches, latency, replay, and total-duration metrics.

### 2.17 UI / UX details
78. **[DONE] Right-click pane context menu** — focus, maximize/restore, new tab, duplicate split, restart, handoff, copy IDs/path, and close actions are available from terminal panes.
79. **[DONE] Workspace navigator** (`prefix+w`) and session navigator (`prefix+g`) modal pickers.
80. **[DONE] Keybind-help modal** (in-app reference) — `prefix+?` opens the app/terminal/custom command keybinding reference.
81. **[DONE] First-run onboarding flow** — implemented as item 86's GUI onboarding pass.
82. **Mobile-narrow TUI layout** with single-column workspace/agent switcher (separate from the PWA).
83. **[DONE] Show-agent-labels-on-pane-borders** toggle — pane agent labels default on and can be hidden from Layout settings or `show_terminal_pane_agent_labels` in TOML.

### 2.18 GitHub issue coverage

Open issues created from the orchestration plan:
- #55 — daemon-owned PTY/session runtime. Closed.
- #56 — JSON-lines socket protocol over Unix socket and localhost TCP. Closed.
- #57 — hard GUI PTY bridge cutover to daemon runtime. Closed.
- #58 — consistent `--json` output. Closed.
- #59 — pane read/send-keys/wait output. Closed.
- #60 — canonical agent status and `wait agent-status`. Closed.
- #61 — agent-targeted commands and event subscriptions. Closed.
- #62 — session metadata persistence and restartable session model. Closed.
- #63 — named session CLI list/attach/stop. Closed.
- #64 — GUI rehydrate and attach/relaunch behavior. Closed.

Current open issue count: 0.

---

## 3. Suggested pivot phasing

Order is by leverage (high-value, low-blast-radius first). Numbers reference items above.

**Phase A — agent-multiplexer parity (foundation), remaining after 2026-06-04 orchestration completion:**
Phase A deepening is complete; remaining native-hook limitations are tracked explicitly under Phase B.

Completed or mostly completed from original Phase A:
1, 2, 3, 4, 5, 6, 7, 9, 21, 22, 23, 24, 48, 49, 50, 51, 52, 53, 54, 64, 65, 66, 68, 76.

Additional orchestration items completed or partially completed outside original Phase A:
2, 25, 26, 27, 28, 29, 30, 31 (partial native resume plus relaunch fallback).

**Phase B — agent ecosystem reach:**
Completed from Phase B: 8, 10, 11, 15, 17, 18, 19, 20.
Partial from Phase B: 16, 31.
Remaining native hook/plugin work: 13, 14. Pi/OMP/Cursor remain pending on stable provider APIs.

**Phase C — terminal-native polish:**
Completed from Phase C: 39, 42, 43, 44, 45, 46, 47, 55, 56, 77, plus xterm reliability hardening for 37 and IME interception hardening for 41.
Remaining terminal-native polish: 37 as future libghostty-vt parity only if needed, 38, 40, and 41 native cursor-anchor placement.

**Phase D — remote & distribution:**
Completed from Phase D: 34, 35, 36, 62.
Partial from Phase D: none currently tracked.
Remaining from Phase D: 73, 74.

**Phase E — long tail:**
63, 75, 81, 82.

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

---

## 5. Post-launch evaluation pass — 2026-06-06

Source: competitive-landscape evaluation on 2026-06-06. Lens: ship v1.5.0 as portfolio piece; no more multi-month pivots. External findings that frame this section:

- Anthropic Remote Control (2026-02-25, GA on Pro/Max/Team/Enterprise, native iOS+Android) eats the original "approve from phone" wedge for the Claude-only audience.
- herdr (~3.5k★, Rust+ratatui) leads the terminal-multiplexer dimension Onibi pivoted toward in §1–§4.
- opcode (~21k★, Tauri 2) leads the Claude Code GUI dimension.
- CloudCLI / claudecodeui (~11.7k★) leads the web/mobile multi-CLI UI dimension.
- Multiple ntfy.sh-based scripts (~60 lines bash, on HN) cover the basic phone-approval wedge for free.

Residual moat that survives: `updatedInput` edit-before-approve, multi-vendor blocking (Codex/OpenCode/Qoder/Copilot/Goose, not only Claude), local-first headless Pi, three-transport pairing with bearer auth.

### 5.1 Cuts — stop tracking, document as not-pursuing

Drop from roadmap. Mark `[CUT]` in any tracking issue and close.

| Existing item | Reason |
|---|---|
| **13** Pi extension | BLOCKED on stable API; heuristic detection covers demo. |
| **14** OMP extension | Same as 13. |
| **32** Live server handoff | High engineering cost, near-zero user benefit over relaunch. Herdr-flex. |
| **37** Vendored libghostty-vt option | xterm.js + WebGL + write batching is sufficient for the audience; libghostty integration is months of C/Zig work. |
| **38** Kitty graphics protocol | xterm.js does not support; needs item 37 first. |
| **40** Kitty keyboard protocol | Same as 38. |
| **41** CJK IME cursor-anchor placement | Webview limitation; cannot be fixed without native overlay. |
| **63** `--handoff` live in-place upgrade | Pairs with cut item 32. |
| **73** Nix flake | Audience-niche polish. Skip unless personally used. |
| **74** Just task runner | Make covers it. |
| **82** Mobile-narrow TUI layout | Duplicates the PWA second-screen story; herdr-think. |

Scope-reductions, not full cuts:

- **55** Built-in themes: reduce 19 to 6 well-tested (Catppuccin, Dracula, Nord, Tokyo Night, Solarized, Onibi Flame). The other 13 are checkbox-feature volume with support cost.
- Git UI: cut clone / push / pull (overlap with VS Code/Cursor). Keep stage/commit/diff and review-baseline (differentiated).

### 5.2 Adds — high leverage

Numbered as 84+ to continue the existing numbering.

#### Top tier — required before v1.5.0 looks "compelling" vs. status-quo

84. **[DONE] Audit log UI** — authenticated history/export endpoints and an Activity Center audit tab now exist with search, decision/edited/agent/tool/date filters, per-tool aggregates, JSONL export of visible rows, daemon-unavailable state, and an inline proposed-vs-final diff view for edited approvals.

85. **[DONE] Approval policy / trust profile rules** — backend TOML rules in `~/.config/onibi/policies.toml` now evaluate and log `auto-allow`, `always-ask`, and `always-deny` decisions:
    ```toml
    [[policy]]
    match.tool = "Bash"
    match.command = "^git (status|diff|log|show)"
    decision = "auto-allow"

    [[policy]]
    match.tool = "Bash"
    match.command = "^rm "
    decision = "always-ask"
    require_edit = true
    ```
    Every auto-allowed decision is still logged into item 84's audit log. Policy docs, CLI/status validation output, Settings-pane validation status, and matched policy names in approval requests are done.

86. **[DONE] First-run onboarding flow inside the GUI** (subsumes original item **81**). Four steps: pick agent → install adapter → scan QR with phone → test approval against a safe canary command. Conversion lever for star→user.

#### Secondary — v1.5.x post-launch polish

87. **[DONE] Diff-aware approval modal for file-edit tools** — approvals for `Write`, `Edit`, and `MultiEdit` now render CodeMirror-backed previews instead of generic JSON. `Write` shows the new file content with path and syntax highlighting; `Edit` renders side-by-side before/after panes; `MultiEdit` paginates side-by-side edit panes 5 per page.

88. **[DONE] PWA kill-switch** — one-tap "Stop all agents on this machine" on the phone. Issues termination to every daemon-owned PTY plus broadcasts deny to every pending approval.

89. **[DONE] Read-only spectator pairing** — one-time spectator pairing tokens now return `scope = "read-only"`, can pair once, can read pending/history/status/realtime endpoints, and are server-blocked from decisions, emergency stop, control, hook, and config mutation routes.

90. **[DONE] `onibi safe <agent>` wrapper** — launches a session with `safeMode = true`; safe sessions auto-allow only conservative read-only Bash basics (`pwd`, `ls`, `cat`, `head`, `tail`, `grep`, `rg`, `sed -n`, non-mutating `find`, and `git status/diff/log/show`) and require manual approval for everything else.

91. **WSL2 install path** — document the existing Linux build for WSL2. Probably zero code work; drops the "Windows: not planned" line in README.

92. **[DONE] Tauri auto-updater** — Tauri 2 updater plugin against `latest.json`. Subsumes existing item **62**.

93. **`onibi.sh` static landing page** — mandatory pre-launch. One page with the screencast embedded, three-line pitch, install snippet, link to repo.

### 5.3 Strengthen — README, launch posts, polish

94. **[DONE] README lead** — first element below the title is the demo GIF, not install steps. Install moves below the GIF.

95. **[DONE] Reposition "How Onibi Is Different"** — replace the cmux / hosted-relay framing with positive positioning: "Onibi is the local-first approval gate for multi-vendor agents with edit-before-approve." Do not name competitors in README.

96. **[DONE] Drop the Acknowledgements section** (cmux / Ghostty / Tailscale / Cloudflare). Reads as small-project humility. Cut or move to `docs/credits.md`.

97. **[DONE] Restructure the Support Matrix** — current table renders Aider/Cursor/Goose/etc. as "No / No / No / No" which flunks Onibi unfairly. Either drop the table from README (keep only in `docs/adapters.md`), or split into two short lists: "Approval-blocking adapters" (Claude, Codex Bash, OpenCode, Qoder, Copilot, Goose) and "Mirror adapters" (Gemini, Aider, Cursor).

98. **[DONE] Verify launch assets exist** — `asset/screencast/onibi-launch.gif` and `docs/architecture.png` are README-linked. Fix if placeholders before tagging.

99. **[DONE] Drop the v1.6 / v2.0 milestone bullets** from README Roadmap if the linked issues are empty. Empty milestones make the project look like vapor.

100. **[PARTIAL] Launch posts repositioning** (`docs/launch/hn.md`, `reddit.md`, `devto.md`, `x-thread.md`, `mastodon.md`, `lobsters.md`, `release-notes.md`): HN, Reddit, X, Mastodon, Lobsters, DEV.to, and release notes now lead with edit-before-approve + multi-vendor + no-cloud / local-first approval-cockpit framing. Remaining: decide whether to mention Anthropic Remote Control explicitly in public launch copy.

101. **Archive this comparison matrix post-launch** — move `TO-IMPLEMENT-PIVOT-4-JUN.md` to `docs/archive/` after v1.5.0 ships. Keeping a "we are chasing herdr" comparison live in the repo undermines positioning.

### 5.4 Explicit non-goals

Do not add, even if requested:

- Hosted relay / Onibi cloud (originally pencilled into v2.0 README roadmap). Kills the local-first pitch.
- Native iOS app. APN setup is a tar pit; PWA is sufficient.
- VS Code extension. Crowded category; weak fit for the gate model.
- More agent adapters beyond the current 11. Quality over breadth.
- Multi-machine fan-in / orchestrator-of-orchestrators (v2.0 README roadmap). Different product.

### 5.5 Effort estimate

| Bucket | Effort |
|---|---|
| §5.1 cuts (mostly doc/issue closure) | 1–2 days |
| §5.3 strengthen (README + launch posts + landing) | Mostly done; dev.to launch copy and landing page remain |
| §5.2 top tier 84–86 | Done |
| §5.2 secondary 87–93 | 87, 88, and 92 done; 89–91 and 93 remain. [Inference] ~1 week |

If tagging v1.5.0 immediately: finish the remaining §5.3 dev.to/landing copy, document §5.1 cuts in CHANGELOG, and decide whether the remaining §5.2 secondary items are v1.5.0-blocking or v1.5.1.

### 5.6 Single most-important add if doing only one thing

**Item 84 (audit log UI).** Cheapest relative to existing data, strongest moat-creator (no competitor has it), best portfolio signal (audit trail design is recognizable engineering discipline), cleanest launch narrative ("Onibi remembers everything your agents tried to do").

---

## 6. Ghostty design adoption pass — 2026-06-06

Reference: `DESIGN.md` (Ghostty design language distillation) at repo root. This section captures only the *concrete, implementable* deltas — the philosophical sections of `DESIGN.md` (Ghostty's core ethos, visual restraint principles, interaction principles) are reading material, not roadmap items.

Onibi is an **approval cockpit**, not a terminal emulator. Several Ghostty rules are too aggressive for an app whose product surface is "monitor and gate multiple agents at once." Items below are filtered through that lens.

### 6.1 Adopt — concrete items added to roadmap

Numbered 102+ to continue the existing numbering from §5.2.

102. **[DONE] Auto-hide the workspace sidebar by default after first launch.** Persisted sidebar collapse state now distinguishes first launch from explicit user preference: first launch shows the sidebar, later launches auto-collapse only when no explicit preference exists, and user toggles never get overridden. No `Ctrl+B` sidebar binding was added because it conflicts with the terminal prefix; Activity bar toggle remains the explicit path.

103. **[DONE] Collapse the StatusBar to a single persistent element: pending approvals.** Everything else (transport pill, git branch, cwd, exit code, font-size buttons) moves to:
     - **Transport state** → toasts only (already wired in §5 — leave that in)
     - **Git branch + exit code + cwd** → ephemeral HUD that appears for ~3s on focus change, then fades
     - **Font-size +/-** → command palette only (`Cmd+K → "Terminal: Decrease font size"`)
     The status bar becomes near-invisible at rest and visible only when something needs attention.

104. **[DONE] Reduce window padding** for terminal panes from current values to `4px` x/y. Let xterm.js content reach the pane edges, matching Ghostty's `window-padding-x = 4` community-standard value.

105. **[DONE] Focus-by-opacity for terminal splits.** Remove focus ring/border on the active pane. Apply `opacity: 0.6` to unfocused split panes. The brightest pane is the focused one. Map: `TerminalView.tsx` + `MainPane.tsx` CSS for `.terminal-pane-body[data-active="false"]`.

106. **[DONE] Reserve saturated yellow/gold for pending-approvals only.** Dedicated `--attention*` tokens now cover approval badges, risk badges, countdowns, title/activity pulses, terminal-tab approval status, pane approval attention, session approval badges, and attention history rows. Generic `--accent` / `--accent-2` remains for active selection, focus, drag/drop, and primary actions; search highlights now use separate `--search-match*` tokens.

107. **[DONE] Replace React-rendered confirmation dialogs with native dialogs** via `@tauri-apps/plugin-dialog` (`confirm`). Destructive browser-confirm call sites now go through a native Tauri dialog wrapper with browser/test fallback: discard file changes, remove worktree, delete file/tree item, discard dirty editor buffer, run install command, delete arrangement, close session, and restore arrangement. The only remaining `window.confirm` call is the synchronous, non-destructive "open URL inside Onibi?" chooser.

108. **[DONE] ApprovalModal restraint pass.** ApprovalModal behavior is hardened and the visual layer now uses shared CSS classes/tokens instead of inline hard-coded dark styles. Native padding, no logo/branding, no emoji labels, no decorative gradient, and plain "Allow" / "Deny" / "Edit & Allow" buttons are wired.

109. **[DONE] System light/dark auto-switching at the app level.** `settings.theme` now accepts persisted `light:X,dark:Y` theme pairs, `theme = "system"` remains a backward-compatible alias for `light:vscode-light-plus,dark:vscode-dark-plus`, Settings exposes constrained light/dark selectors, and app resolution follows `matchMedia`.

110. **[DONE] CSS token pass — adopt `DESIGN.md` §15 values for the active chrome scope.** The launch/chrome passes fixed the raw white backgrounds in the new chrome, applied 4px terminal padding, added dedicated attention/search tokens, tokenized ApprovalModal styling, set JetBrains Mono-first defaults for mono surfaces, and disabled ligatures for terminal/editor/approval code.
     ```
     font-family            JetBrains Mono, ui-monospace, monospace
     font-feature-settings  "calt" off
     window-padding         4 4
     unfocused-opacity      0.6 (split), 0.85 (sidebar peripheral)
     divider-color          rgba(255,255,255,0.06) dark / rgba(0,0,0,0.08) light
     search-match-bg        #f6c25d (gold), #f6a878 (peach focused)
     cursor-style           block + blink
     resize-overlay-ms      750
     ```
     Remaining raw colors are either theme definitions, syntax/diff/status colors, QR white backing, or shadows; not all non-token color literals are being treated as chrome debt.

111. **[DONE] Reduce app-rendered chrome elsewhere** — every always-on surface gets one of three verdicts: (a) essential, keep, (b) ephemeral on demand, (c) move to command palette. Concrete passes:
     - Activity bar: keep, but shrink icon set and ensure approvals tile is the only one with a colored badge. **Done.**
     - Agent rail: keep, but show only when ≥1 session exists or workspace is open (already partially true post-#3). **Done for the zero-session/zero-workspace rail: only `New session` remains.**
     - Tab strip on terminal panes: hide when only 1 tab exists in the active workspace tab (mirrors Ghostty `window-show-tab-bar = auto`). **Done.**

### 6.2 Adopt with modification — Ghostty's rule too aggressive as-stated

Three items where Ghostty's design rule is right in spirit but too strict for Onibi's category:

- **§14 #2 "Auto-hide the agent rail when no sessions exist"** — modified to: rail stays visible but collapses to a minimal `+ New session` tile when zero sessions exist. Rationale: the rail IS the multi-agent identity signal; full hiding would make Onibi indistinguishable from a single-session terminal at rest. Tracked as item 111 above.

- **§14 #5 "Drop the Cmd+K pill"** — modified to: pill **fades** to a 20% opacity glyph after the user has invoked the command palette once (tracked in localStorage). Rationale: Ghostty's audience is terminal power-users; Onibi's audience includes phone-paired non-experts. Discoverability for first-time users is worth one frame of persistent chrome.

- **§14 #1 "Auto-hide workspace sidebar by default"** — modified to: visible on first launch (so new users see the file tree exists), persistently collapsed thereafter if the user collapses it once. Rationale: Ghostty has no file tree; Onibi does. The sidebar is functional, not decorative.

### 6.3 Reject — Ghostty rule does not apply to Onibi's category

Two items I am explicitly NOT taking, with reasoning:

- **§14 #4 "Drop the welcome hero"** — the welcome hero ("Onibi — Local-first approval gate for multi-vendor coding agents") is the *only* place Onibi tells a new user what category it belongs to. Ghostty doesn't need this because "terminal emulator" needs no explanation; "approval cockpit" does. Keep the hero, but trim it to one line.

- **§14 #11 "Move Source Control into command palette"** — the persistent Source Control sidebar is a *real productivity surface* for users reviewing agent-generated changes (stage / diff / commit / review baseline). Collapsing to palette adds friction to the most common review workflow. Reject. (The clone/push/pull *sub-features* are still cut per §5.1, but the panel stays.)

### 6.4 Out of scope, but worth noting

- **Quick Terminal (drop-down overlay)** — Ghostty's iconic ephemeral surface. Not applicable to Onibi (the desktop app isn't a terminal-replacement; the relevant equivalent would be a global-hotkey "approve next pending" overlay, which is interesting but bigger than this pass).
- **AppleScript / system scripting bridge** — out of scope for v1.5; revisit if `onibi`-CLI coverage proves insufficient.
- **Proxy icon in title bar** (drag the cwd to other apps) — Tauri does not expose this without custom native code. Defer.

### 6.5 Effort estimate

| Bucket | Effort |
|---|---|
| §6.1 token pass (110) + padding (104) | done for this scoped chrome pass |
| §6.1 status bar collapse (103) + opacity focus (105) | done |
| §6.1 accent audit (106) + auto light/dark (109) | done |
| §6.1 native dialogs (107) + sidebar default-hide (102) | done |
| §6.1 ApprovalModal restraint (108) | done |
| §6.2 modified items | done for CmdK fade, empty-rail restraint, and single-tab terminal strip auto-hide |
| Total | **Requested §6 chrome cleanup items are done; remaining §6 scope is limited to out-of-scope/deferred ideas.** |

If shipping v1.5.0 immediately and only doing one Ghostty-derived change: **item 105 (focus-by-opacity for splits).** It's the most visually striking Ghostty-ism, takes <1 hour, and converts a Cursor/VS-Code-style cockpit into something that visually communicates restraint.

### 6.6 Verification note for §6.1

The chrome-restraint direction was selected for this implementation pass. The `⌘K` pill is retained only as a fading discovery affordance, the StatusBar no longer owns transport/cwd/git/font-size chrome, and non-approval attention badges now use dedicated attention tokens. The welcome hero and Source Control panel remain by explicit category-fit reasoning in §6.3.

---

## 7. Frontend audit punch list — 2026-06-06

Five items surfaced from the desktop frontend audit on 2026-06-06. Each is meaningfully evaluated below — accept, reject, or fold into existing roadmap items. No blind transcription.

### 7.1 Accept — added to roadmap

Numbered 112+ to continue from §6.

112. **[DONE] Mobile PWA audit + polish pass (gesture/read-only slice).** The PWA now has active transport identity, fallback candidates, targeted notification deep links, cached-pending offline copy, risk badges, optional deny reasons, server-provided read-only scope storage, read-only spectator UI state, disabled mutation controls for read-only tokens, and swipe allow/deny gestures for full-access approval cards. Remaining broader audit dimensions:
     - Identity at rest (does the PWA show what app it is, what machine it's paired to?)
     - Approval modal UX on mobile (edit-before-approve on a touch keyboard is hard — needs different affordance than desktop)
     - Pairing affordance (QR re-scan when token rotates, multi-machine pairing)
     - Offline state (graceful degradation when transport drops mid-session)
     - Install-as-PWA prompts (iOS Safari "Add to Home Screen" hint; Android Chrome auto-prompt)
     - Transport fallback when primary URL fails (does the PWA try the next entry from `transports[]`?)
     - Lock-screen + notification deep-link behavior (tapping a push notification should land on that specific approval, not the queue root)
     - Swipe gestures (swipe-to-approve, swipe-to-deny — table-stakes on mobile, may not exist)
     - Light/dark theme (PWA respects `prefers-color-scheme`?)
     **Effort:** 4-6 hours total. **Single highest-leverage remaining frontend work.**

113. **[DONE] ApprovalModal deep audit + behavior polish.** Risk surfacing, edit-specific CTA, optional deny reason, timeout countdown, queue state, matched policy names, Bash shell-highlight previews, `Write` CodeMirror content previews, `Edit` side-by-side CodeMirror previews, and paginated `MultiEdit` previews now exist.
     **Cross-reference:** this is the *behavior/content* audit; item 108 (Ghostty restraint pass) is the *visual* pass. Both are now implemented in the same ApprovalModal surface.

114. **[DONE] Light-theme CSS parity sweep for chrome added in §5 / audit polish.** Specifically: `.welcome-pill`, `.title-cmdk-pill`, `.title-cmdk-kbd`, `.inline-launcher-card`, `.inline-launcher-hint kbd`, `.source-control-worktree-tag` — all use raw `rgba(255,255,255,X)` for backgrounds and will render invisible on light themes. Replace with `var(--bg-1)` / `var(--bg-2)` plus explicit light-theme fallbacks.
     **Cross-reference:** folds into item 110 (CSS token pass per DESIGN.md §15). Same CSS file, same touch — do together. **Effort:** 30 minutes.

115. **[DONE] Transport pill loading-vs-error-vs-empty disambiguation in StatusBar.** Currently `⊘ no transport` is shown for three distinct states: (a) truly unconfigured, (b) fetch failed, (c) initial mount before first poll completes. Differentiate as:
     - `··· checking` — initial poll in-flight (first ≤ 2 s after mount)
     - `⊘ offline` — fetch raised (network or auth error)
     - `⊘ no transport` — fetched cleanly, nothing enabled
     Implementation: add `loadState: "initial" | "ok" | "error"` to StatusBar local state. **Effort:** 15 minutes.

116. **[DONE] Attention escalation when an approval arrives and window IS focused.** Existing cues (toast, title-bar dot, bell badge) are subtle if the user is scrolling. Layered escalation:
     - (a) Title-bar agent dot **pulses** yellow for ~3 s via CSS keyframe, then settles.
     - (b) macOS dock icon requests user attention **exactly once** per approval via Tauri's `Window::request_user_attention(Some(UserAttentionType::Informational))`. **Critical** is rejected — never request critical (which bounces continuously); informational is one bounce. Linux equivalent uses `urgency` hint on the GTK window.
     - (c) The approvals row in the sidebar pulses on first display.
     Respect `suppressForegroundTabNotifications` setting (already exists in store): if the foreground session is the one with the new approval, skip (a)+(b) — the user is already looking at it. **Effort:** ~1 hour.

### 7.2 Reject — duplicate of existing entries

- **Audit log UI / approval policies / onboarding flow** — already roadmapped as items 84 / 85 / 86 in §5.2. Re-listing here would create double-tracking. Rejected as roadmap additions; honored as already-tracked.
- **More themes / animations / micro-interactions** — diminishing returns. Theme count is being *reduced* per §5.1 + item 110, not expanded.
- **Settings UI restructure** — out of scope per §5.4 non-goals. Settings.toml is the source of truth (consistent with §6 Ghostty principle).

### 7.3 Effort estimate

| Item | Effort | Foldable into | Standalone if not folded |
|---|---|---|---|
| 112 PWA audit + polish | Partial complete; remaining swipe/install/multi-pairing polish | — | yes |
| 113 ApprovalModal behavior | Done | item 108 (visual pass) | done |
| 114 light-theme CSS sweep | Done | item 110 (token pass) | done |
| 115 transport loading state | Done | item 103 (StatusBar collapse) | done |
| 116 attention escalation | Done | — | done |
| **Total** | **~0.5–1 day** if folded; ~1–1.5 days standalone | | |

### 7.4 Single most-important if doing only one thing

**Item 112 (mobile PWA audit).** Reason: it is the only chunk of the product I have not opened in any session. Onibi's pitch centers on the phone; if the PWA approval modal is rough, no amount of desktop polish saves the user experience. The audit itself is non-invasive (read-only) and can be done even when shipping v1.5.0 as-is. Any resulting fixes are scoped post-audit.

### 7.5 Sequencing recommendation

If implementing both §6 (Ghostty pass) and §7 (audit punch list):

1. Do **112 (PWA audit)** first — read-only, surfaces unknowns that might change §6 priorities.
2. **114 + 110 + 103 + 115** are done for the scoped CSS/StatusBar pass.
3. **108 + 113** are done for the ApprovalModal visual and behavior pass.
4. **102 + 105 + 104 + 106** are done for the chrome-restraint pass.
5. **109** is done for persisted light/dark theme pairs and OS auto-switching.

Total remaining from §6 + §7 now excludes the completed requested slices: **89 read-only spectator pairing**, **90 `onibi safe <agent>`**, and **112 PWA gesture/read-only polish** are complete. Remaining work is broader/mobile follow-up audit dimensions and unrelated secondary/product items; the requested desktop ApprovalModal/chrome cleanup slice is complete.
