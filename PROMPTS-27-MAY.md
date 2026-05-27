# PROMPTS — Onibi v1.5 Re-Pivot

> Dated 27 May 2026. Copy-paste prompts for coding agents (Claude Code, Codex, Cursor agent, etc.) to execute each phase of the Onibi v1.5 build. Each prompt is **self-contained** — the agent receiving it can pick it up cold with only `git clone` access to `gongahkia/onibi` and the referenced PHASE doc.

## Usage

Each prompt below is a fenced code block. Copy the entire block (including the framing lines) and paste it as the **first message** to a fresh coding-agent session. Do not summarise it. Do not preface it.

Agents work on isolated branches off `v1.5`:
- Branch naming: `v1.5/phase-XX-<short-slug>`.
- PRs merge into `v1.5` (not `main`).
- `main` stays clean until PHASE-07 final merge.

## Execution order

```
A: Prompt-00          (sequential, must precede everything)
B: Prompt-01          (sequential after A)
C: Prompt-02, Prompt-03   (parallel after B)
D: Prompt-04, Prompt-05   (parallel after C)
E: Prompt-06          (sequential after D)
F: Prompt-07          (sequential after E — final)
```

When dispatching parallel prompts (Group C, Group D), open them in separate agent sessions. Each is told what NOT to touch to minimise merge conflicts.

---

## Prompt-00 — Foundation & demolition

```text
You are an independent contributor on the Onibi project. Read these files in full before doing anything:

1. /Users/gongahkia/Desktop/coding/projects/onibi/PHASE-00-PLAN-27-MAY.md  — your authoritative spec
2. /Users/gongahkia/Desktop/coding/projects/onibi/WORKON-PIVOT-ASAP.md     — context on the prior v1 plan you are superseding
3. /Users/gongahkia/Desktop/coding/projects/onibi/CLAUDE.md                — project communication / coding style rules

Your job: execute every deliverable D1 through D11 in PHASE-00. Specifically:

- Tag current main as v0-archive and push. Branch v1.5; sub-branch v1.5/phase-00-foundation.
- Delete v0 Swift surface: Onibi/, OnibiCore/, OnibiSessionProxy/, OnibiTests/, OnibiCoreTests/, Package.swift, Package.resolved, Onibi.xcodeproj/, and Swift portions of the Makefile.
- Archive WORKON-PIVOT-ASAP.md, current README.md, SPEC.md, OnibiWeb/, docs/phases/, docs/security.md (if present) under docs/archive/.
- Scaffold Tauri 2.x React+TS app at app/ via `pnpm create tauri-app@latest app -- --template react-ts --manager pnpm --identifier sh.onibi.desktop`. Configure tauri.conf.json and Cargo.toml per D4.
- Scaffold Vite PWA at mobile/ per D5.
- Create root files: new placeholder README.md (~30 lines, terse v1.5 pitch + links to PHASE docs), LICENSE (Apache-2.0), Makefile (with make targets dev-app, dev-mobile, test, build, clean), .gitignore additions, .editorconfig, rust-toolchain.toml (stable), pnpm-workspace.yaml.
- Confirm all 8 PHASE docs + PROMPTS-27-MAY.md exist at repo root (they are already committed from the planning round — do NOT rewrite them).
- CI rewrite: replace .github/workflows/* with ci.yml (rust + node tests, tauri build smoke on macOS + Ubuntu) and release.yml (cross-compile matrix on tag push) per D8.
- Close GH issues #6–#29 with the exact comment "Superseded by PHASE-XX-PLAN-27-MAY.md re-pivot (v1.5). See repo root for new phase docs."
- Write docs/archive/README.md as a one-line index.

Verify all 9 exit criteria in PHASE-00 are met. The CI on the merge commit must be green.

What is OUT of scope and must NOT be done in this prompt:
- Writing any Rust code beyond what `cargo tauri init` generated.
- Implementing PTY (PHASE-01).
- Rewriting SPEC.md (PHASE-03).
- Setting up transports (PHASE-05).
- Adding agent icons (PHASE-02).

Open a PR titled `phase-00: foundation + demolition` against branch v1.5. Body must follow the PR template at the bottom of PHASE-00-PLAN-27-MAY.md. Do not merge. Tag @gongahkia for review.
```

---

## Prompt-01 — PTY core + xterm rendering

```text
You are an independent contributor on the Onibi project. Phase 00 is already merged into branch v1.5 (the Tauri scaffold exists at app/). Read in full:

1. /Users/gongahkia/Desktop/coding/projects/onibi/PHASE-01-PLAN-27-MAY.md
2. /Users/gongahkia/Desktop/coding/projects/onibi/PHASE-00-PLAN-27-MAY.md (for context only)
3. /Users/gongahkia/Desktop/coding/projects/onibi/CLAUDE.md
4. https://docs.rs/portable-pty
5. https://xtermjs.org/docs/

Branch: v1.5/phase-01-pty (cut from v1.5).

Deliver D1–D8 in PHASE-01:
- D1: Rust PTY module at app/src-tauri/src/pty/ using portable-pty + tokio. Surface: PtyManager with spawn/write/resize/kill/subscribe/list. Per-session broadcast channel. Tracks exit; emits PtyEvent::Exit. Internal storage Arc<RwLock<HashMap<PtyId, PtySession>>>.
- D2: Tauri commands pty_spawn, pty_write, pty_resize, pty_kill. Emit `pty:<id>` events to webview with `{type: "data"|"exit", data?: base64, code?, signal?}`. Register on Builder + manage(PtyManager).
- D3: app/src/components/TerminalView.tsx mounting xterm.js + fit + webgl + web-links addons. Bidirectional pipe to Rust. Resize via ResizeObserver. Cleanup on unmount.
- D4: app/src/App.tsx single-terminal proof of life.
- D5: app/src-tauri/src/util/shell.rs + app/src/lib/tauri-bridge.ts.
- D6: Rust tests (echo, cat, sleep+kill, resize) + frontend vitest cases.
- D7: perf smoke (yes | head -n 100000) renders smoothly.
- D8: tracing logging.

Verify all 8 exit criteria. The integration test `cargo test --workspace` must be green on macOS and Linux.

What is OUT of scope:
- Multiple tabs (PHASE-02).
- File tree (PHASE-02).
- Editor pane (PHASE-02).
- Agent metadata on tabs (PHASE-02).
- HTTP/WS server (PHASE-03).
- Adapter logic (PHASE-03).
- Mobile (PHASE-04).
- Transports (PHASE-05).

Open a PR titled `phase-01: pty core + terminal rendering` against branch v1.5. Body per PHASE-01 PR template. Attach a screencast of `claude code --help`, `vim`, and `htop` running in the terminal. Do not merge.
```

---

## Prompt-02 — UI shell: tabs, file tree, editor (PARALLEL with Prompt-03)

```text
You are an independent contributor on the Onibi project. PHASE-01 (PTY core + TerminalView) is merged on branch v1.5. You are working in parallel with another contributor on PHASE-03 (HTTP server + adapters). Do NOT touch their file scope (see below). Read in full:

1. /Users/gongahkia/Desktop/coding/projects/onibi/PHASE-02-PLAN-27-MAY.md
2. /Users/gongahkia/Desktop/coding/projects/onibi/PHASE-01-PLAN-27-MAY.md (for the PTY model your tabs will own)
3. /Users/gongahkia/Desktop/coding/projects/onibi/PHASE-03-PLAN-27-MAY.md (so you know which types they share with you — Session.pendingApprovals: string[] is owned BY YOU)
4. /Users/gongahkia/Desktop/coding/projects/onibi/CLAUDE.md

Branch: v1.5/phase-02-ui-shell (cut from v1.5).

Deliver D1–D11 of PHASE-02:
- D1: App.tsx PanelGroup layout with three resizable panes. Layout flips when tab bar orientation = horizontal.
- D2: Session + Workspace model at app/src/lib/sessions.ts; persist via @tauri-apps/plugin-store; in-memory store via zustand.
- D3: AgentTabBar.tsx with vertical/horizontal orientation, status flash animation (CSS keyframe), agent icons from app/src/assets/agents/.
- D4: NewSessionDialog.tsx — agent dropdown, workspace dropdown, optional initial prompt; spawns PTY with correct launch command per agent kind.
- D5: FileTree.tsx — lazy-loaded, sandboxed paths (Rust fs_list_dir / fs_read_file / fs_write_file commands at app/src-tauri/src/fs/), no rename/create, filter input.
- D6: EditorBuffer.tsx — plain monospace textarea, Save/Discard/Close, dirty indicator, binary-file detection, 2MB cap.
- D7: MainPane.tsx — switches between TerminalView and EditorBuffer based on selectedFile.
- D8: SettingsPane.tsx — General / Layout / Agents / Workspaces sections.
- D9: Optional Ghostty config parsing for theme. Do not block startup on parse errors.
- D10: Create or import 8 agent SVG icons under asset/agent-icons/ and copy to app/src/assets/agents/. Add ATTRIBUTION.md.
- D11: vitest cases for all components + Rust fs path-sandbox tests.

Verify all 8 exit criteria. CI must be green.

FILES YOU MAY EDIT (exclusive to PHASE-02):
- app/src/App.tsx
- app/src/components/AgentTabBar.tsx, FileTree.tsx, EditorBuffer.tsx, MainPane.tsx, NewSessionDialog.tsx, SettingsPane.tsx, EmptyState.tsx
- app/src/lib/sessions.ts
- app/src/styles/*
- app/src/assets/agents/* (icons)
- app/src-tauri/src/fs/* (filesystem commands)
- app/src-tauri/Cargo.toml (add fs-related deps only — no axum, no rusqlite)
- app/package.json (add react-resizable-panels, zustand, @tauri-apps/plugin-store)
- asset/agent-icons/*
- Existing component tests

FILES YOU MUST NOT TOUCH (owned by Prompt-03):
- app/src-tauri/src/server/*
- app/src-tauri/src/approval/*
- app/src-tauri/src/adapters/*
- app/src-tauri/src/protocol/*
- app/src-tauri/src/secret/*
- app/src-tauri/src/cli/*
- adapters/* (templates)
- SPEC.md
- app/src/components/ApprovalModal.tsx

Open a PR titled `phase-02: ui shell (tabs, file tree, editor buffer)` against branch v1.5. Attach screencast: two simultaneous tabs, file edit/save, orientation toggle. Body per PHASE-02 PR template. Do not merge.
```

---

## Prompt-03 — Adapters + approval protocol (PARALLEL with Prompt-02)

```text
You are an independent contributor on the Onibi project. PHASE-01 (PTY core) is merged on branch v1.5. You are working in parallel with another contributor on PHASE-02 (UI shell). Do NOT touch their file scope (see below). Read in full:

1. /Users/gongahkia/Desktop/coding/projects/onibi/PHASE-03-PLAN-27-MAY.md
2. /Users/gongahkia/Desktop/coding/projects/onibi/PHASE-01-PLAN-27-MAY.md (PTY model)
3. /Users/gongahkia/Desktop/coding/projects/onibi/PHASE-02-PLAN-27-MAY.md (the Session type they own — your ApprovalModal consumes it)
4. /Users/gongahkia/Desktop/coding/projects/onibi/docs/archive/SPEC-v1-draft.md (prior protocol; use as reference, then rewrite from scratch)
5. /Users/gongahkia/Desktop/coding/projects/onibi/docs/archive/WORKON-PIVOT-ASAP.md (Claude Code hook research findings)
6. https://docs.claude.com/en/docs/claude-code/hooks
7. /Users/gongahkia/Desktop/coding/projects/onibi/CLAUDE.md

Branch: v1.5/phase-03-adapters (cut from v1.5).

Deliver D1–D8 of PHASE-03:
- D1: Rewrite SPEC.md v1.0 from scratch. Document machine_id/session_id/approval_id (ULID), all endpoints (/v1/approval/request, /v1/approval/pending, /v1/approval/:id/decide, /v1/run/event, /v1/pty/output, /v1/qr, /v1/pair, WS /v1/realtime), auth (bearer), protocol_version "1.0", failure modes.
- D2: axum HTTP server + WS hub at app/src-tauri/src/server/. SQLite via rusqlite (bundled) at ~/.config/onibi/onibi.db. Connection pool. Schema per PHASE-03 D2.
- D3: Bearer token via `keyring` crate; file fallback at ~/.config/onibi/token.txt (0600). VAPID keypair generation here so PHASE-04 can use it.
- D4: Adapter modules at app/src-tauri/src/adapters/{claude_code,codex,opencode,gemini,aider,cursor,goose}.rs. Hook templates at adapters/<name>/. Full intercept for claude-code; bash-only for codex; stubs/mirror-only for others. Claude Code min-version check (refuse <2.0.10).
- D5: CLI via clap derive at app/src-tauri/src/cli/. Subcommands: setup, status, token rotate, token show, adapter list/install/uninstall, _hook <name>. Dispatch from main.rs when argv >1.
- D6: app/src/components/ApprovalModal.tsx wired to WS events (route through Tauri events). Three buttons: Allow / Edit / Deny.
- D7: Rust + frontend tests; integration test for approval round-trip; install/uninstall preserve-existing-hooks test.
- D8: scripts/e2e-claude.sh end-to-end script (gated on Claude Code availability).

Verify all 9 exit criteria. Run `scripts/e2e-claude.sh` and confirm pass.

FILES YOU MAY EDIT (exclusive to PHASE-03):
- app/src-tauri/src/server/*
- app/src-tauri/src/approval/*
- app/src-tauri/src/adapters/*
- app/src-tauri/src/protocol/*
- app/src-tauri/src/secret/*
- app/src-tauri/src/cli/*
- app/src-tauri/src/main.rs (dispatch logic + state wiring; coordinate diff size with PHASE-02 author)
- app/src-tauri/Cargo.toml (axum, tower-http, rusqlite, r2d2, ulid, base64, keyring, clap, web-push, etc.)
- adapters/*
- SPEC.md
- app/src/components/ApprovalModal.tsx
- app/src/lib/approval-client.ts
- scripts/e2e-claude.sh

FILES YOU MUST NOT TOUCH (owned by Prompt-02):
- app/src/App.tsx (only coordinate adding the ApprovalModal mount point; otherwise hands off)
- app/src/components/AgentTabBar.tsx, FileTree.tsx, EditorBuffer.tsx, MainPane.tsx, NewSessionDialog.tsx, SettingsPane.tsx
- app/src/lib/sessions.ts (read only — you import the types; do not change)
- app/src-tauri/src/fs/*
- app/src/assets/agents/*
- asset/agent-icons/*

Open a PR titled `phase-03: adapters + approval protocol (SPEC v1.0)` against branch v1.5. Attach screencast of edit-then-approve flow with Claude Code. Body per PHASE-03 PR template. Do not merge.
```

---

## Prompt-04 — Mobile PWA + web push (PARALLEL with Prompt-05)

```text
You are an independent contributor on the Onibi project. PHASE-03 (HTTP server + approval protocol + VAPID keypair) is merged on branch v1.5. You are working in parallel with another contributor on PHASE-05 (transports). Their work happens at app/src-tauri/src/transport/* — you do not touch that path. Read in full:

1. /Users/gongahkia/Desktop/coding/projects/onibi/PHASE-04-PLAN-27-MAY.md
2. /Users/gongahkia/Desktop/coding/projects/onibi/PHASE-03-PLAN-27-MAY.md (the protocol you consume)
3. /Users/gongahkia/Desktop/coding/projects/onibi/SPEC.md (endpoint contracts)
4. /Users/gongahkia/Desktop/coding/projects/onibi/PHASE-05-PLAN-27-MAY.md (just the transports[] payload field you consume in pairing)
5. /Users/gongahkia/Desktop/coding/projects/onibi/CLAUDE.md
6. https://vite-pwa-org.netlify.app
7. https://webkit.org/blog/13878/web-push-for-web-apps-on-ios-and-ipados/

Branch: v1.5/phase-04-mobile (cut from v1.5).

Deliver D1–D12 of PHASE-04:
- D1: Replace mobile/ scaffold with the directory layout in PHASE-04 D1.
- D2: manifest.webmanifest + icons + sw.ts (workbox injectManifest); handle push + notificationclick.
- D3: PairingView (QR scan + deep link / onibi:// scheme) writing to localStorage.
- D4: InboxView (tabs Pending + Recent runs) wired to WS.
- D5: ApprovalCard + EditCommandView; POST decision payload.
- D6: TerminalMirrorView (read-only xterm.js subscribed to pty-output).
- D7: Web push subscription via push.ts; POST to /v1/pair with subscription. Server-side: VAPID fanout via web-push crate (add the crate here if PHASE-03 didn't).
- D8: Auto-reconnect WS with exponential backoff.
- D9: OnboardingView (iOS detection + add-to-home-screen instructions).
- D10: Styles + safe-area + big tap targets.
- D11: vitest for ApprovalCard, push subscription, WS reconnect.
- D12: scripts/e2e-mobile-demo.md + screencast.

Also: ensure the Rust server serves mobile/dist/ static files under /m/* via tower-http::services::ServeDir. Coordinate this minor route addition with the PHASE-05 author so you do not collide on app/src-tauri/src/server/routes.rs.

Verify all 8 exit criteria. Killer demo must be runnable end-to-end.

FILES YOU MAY EDIT (exclusive to PHASE-04):
- mobile/* (entire tree)
- app/src-tauri/src/server/static.rs (new file for mobile dist serving)
- app/src-tauri/src/server/routes.rs (only to register the /m/* route — small, coordinated diff)
- app/src-tauri/src/push/* (web push fanout, if PHASE-03 didn't add)
- scripts/e2e-mobile-demo.md

FILES YOU MUST NOT TOUCH (owned by Prompt-05):
- app/src-tauri/src/transport/*
- app/src/components/TransportSettings.tsx
- docs/transports.md

Open a PR titled `phase-04: mobile pwa + web push (killer demo)` against branch v1.5. Attach screencast of locked-phone-buzzes-then-approve demo. Body per PHASE-04 PR template. Do not merge.
```

---

## Prompt-05 — Transports: Tailscale + Cloudflare + LAN (PARALLEL with Prompt-04)

```text
You are an independent contributor on the Onibi project. PHASE-03 (HTTP server) is merged on branch v1.5. You are working in parallel with another contributor on PHASE-04 (mobile PWA). Read in full:

1. /Users/gongahkia/Desktop/coding/projects/onibi/PHASE-05-PLAN-27-MAY.md
2. /Users/gongahkia/Desktop/coding/projects/onibi/PHASE-03-PLAN-27-MAY.md (server + pairing payload you extend)
3. /Users/gongahkia/Desktop/coding/projects/onibi/SPEC.md
4. /Users/gongahkia/Desktop/coding/projects/onibi/CLAUDE.md
5. https://tailscale.com/kb/1223/funnel
6. https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/do-more-with-tunnels/trycloudflare/

Branch: v1.5/phase-05-transports (cut from v1.5).

Deliver D1–D10 of PHASE-05:
- D1: Transport trait + TransportHandle + TransportStatus at app/src-tauri/src/transport/mod.rs.
- D2: TailscaleFunnel transport — shell out to `tailscale` CLI; parse JSON status; surface DNS name; handle "not logged in" and "quota" errors.
- D3: CloudflareTunnel transport — spawn cloudflared quick-tunnel; regex-parse trycloudflare.com URL from stderr; clean up child on shutdown.
- D4: LanTransport — rcgen self-signed cert at ~/.config/onibi/lan.{crt,key}; axum-server with rustls; mDNS broadcast via mdns-sd; expose SHA256 cert fingerprint.
- D5: TransportManager — enable/disable + aggregate PairingPayload (machine_id, token, vapid_public_key, transports[]).
- D6: TransportSettings.tsx in app/src/components/ — three toggles with live status, "show install QR" for LAN cert.
- D7: CLI subcommands: onibi transport list/enable/disable/status.
- D8: Pairing QR regenerates on transport change.
- D9: Unit tests for each transport's parse logic + cert fingerprint stability.
- D10: docs/transports.md (setup walkthroughs per OS + cert install for LAN).

Verify all 7 exit criteria. Manual matrix: each transport reachable from phone on LTE or wifi as appropriate.

FILES YOU MAY EDIT (exclusive to PHASE-05):
- app/src-tauri/src/transport/*
- app/src/components/TransportSettings.tsx
- app/src/lib/transports.ts (typed wrappers for transport_* commands)
- docs/transports.md
- app/src-tauri/Cargo.toml (rcgen, rustls, axum-server, mdns-sd, if_addrs, which, async-trait)

FILES YOU MUST NOT TOUCH (owned by Prompt-04):
- mobile/*
- app/src-tauri/src/server/static.rs
- app/src-tauri/src/push/*

Coordinate small diffs against:
- app/src-tauri/src/server/routes.rs (you may register `transport_*` tauri commands + extend the /v1/qr handler to include transports[] in payload)
- app/src/components/SettingsPane.tsx (add TransportSettings tab)

Open a PR titled `phase-05: transports (tailscale + cloudflared + LAN)` against branch v1.5. Attach screencast showing all three transports running and PWA failover. Body per PHASE-05 PR template. Do not merge.
```

---

## Prompt-06 — Headless mode + packaging (Pi/Linux/macOS)

```text
You are an independent contributor on the Onibi project. PHASE-04 (mobile) and PHASE-05 (transports) are both merged on branch v1.5. Read in full:

1. /Users/gongahkia/Desktop/coding/projects/onibi/PHASE-06-PLAN-27-MAY.md
2. /Users/gongahkia/Desktop/coding/projects/onibi/PHASE-03-PLAN-27-MAY.md (CLI structure you extend)
3. /Users/gongahkia/Desktop/coding/projects/onibi/PHASE-05-PLAN-27-MAY.md (transports the wizard auto-enables)
4. /Users/gongahkia/Desktop/coding/projects/onibi/CLAUDE.md
5. https://github.com/cross-rs/cross
6. https://docs.brew.sh/Formula-Cookbook
7. https://wiki.archlinux.org/title/Systemd/User

Branch: v1.5/phase-06-packaging (cut from v1.5).

Deliver D1–D10 of PHASE-06:
- D1: Headless entry at app/src-tauri/src/headless.rs; dispatch in main.rs when --headless arg present.
- D2: Cargo "gui" feature gates Tauri deps; `cargo build --no-default-features` produces a headless-only binary. Cross-compile for aarch64-unknown-linux-gnu via `cross`.
- D3: packaging/systemd/onibi.service (user unit).
- D4: packaging/install-linux.sh and packaging/pi-install.sh (curl-pipe installers).
- D5: packaging/homebrew/onibi.rb (kept in repo; mirrored to gongahkia/homebrew-onibi tap on each release).
- D6: `onibi setup` interactive wizard using dialoguer + qrcode-ASCII printing.
- D7: `onibi status` command.
- D8: Update .github/workflows/release.yml with 4-target matrix (darwin x86_64, darwin arm64, linux x86_64 + bundles, linux arm64 headless-only).
- D9: Run fresh-machine timed dry-run on macOS + Ubuntu 22.04 + Pi 5; record in docs/dry-run-log.md. All ≤5 minutes.
- D10: CI smoke tests including `cross build --target aarch64-unknown-linux-gnu --no-default-features` and headless-binary curl-status check.

Verify all 8 exit criteria. The fresh-machine dry-runs are non-negotiable — if any exceeds 5 min, file a follow-up issue and fix before opening this PR.

FILES YOU MAY EDIT (exclusive to PHASE-06):
- app/src-tauri/src/headless.rs
- app/src-tauri/src/main.rs (dispatch only — keep diff small)
- app/src-tauri/src/cli/setup.rs (the wizard)
- app/src-tauri/src/cli/status.rs
- app/src-tauri/Cargo.toml (gui feature flag, dialoguer, qrcode, cross-compile-friendly tweaks)
- packaging/* (entire tree)
- .github/workflows/release.yml
- scripts/dry-run-fresh-machine.sh
- docs/dry-run-log.md
- docs/install-linux.md
- docs/install-mac.md
- docs/install-pi.md

FILES YOU MUST NOT TOUCH:
- mobile/*
- app/src/* (no frontend changes in PHASE-06)
- adapters/*
- SPEC.md

Open a PR titled `phase-06: headless mode + packaging (macOS, Linux, Pi)` against branch v1.5. Attach 3 dry-run screencasts. Body per PHASE-06 PR template. Do not merge.
```

---

## Prompt-07 — Hardening, docs, launch

```text
You are an independent contributor on the Onibi project. PHASE-06 is merged on branch v1.5. All functionality is built. This is the final phase. Read in full:

1. /Users/gongahkia/Desktop/coding/projects/onibi/PHASE-07-PLAN-27-MAY.md
2. /Users/gongahkia/Desktop/coding/projects/onibi/PHASE-00-PLAN-27-MAY.md through PHASE-06 (skim for cross-reference)
3. /Users/gongahkia/Desktop/coding/projects/onibi/CLAUDE.md
4. https://news.ycombinator.com/showhn.html

Branch: v1.5/phase-07-launch (cut from v1.5).

Deliver D1–D12 of PHASE-07:
- D1: docs/security.md — threat model + hardening checklist; implement rate-limit (tower-governor), body-size limits, WS message limit, HSTS, CSP. Run `cargo audit` + `pnpm audit` and fix any high/critical CVE.
- D2: `onibi doctor` CLI command — environment health check with remediation hints.
- D3: docs/architecture.png + source (Excalidraw or draw.io); replaces old asset/reference/architecture.png.
- D4: README.md final rewrite — hero, screencast, install one-liners (verified), quick start, arch diagram, support matrix, differentiation, roadmap, license, acknowledgements.
- D5: docs/adapters.md — per-agent capability matrix.
- D6: docs/transports.md polish — screenshots + per-OS cert install.
- D7: 90-second screencast per PHASE-07 D7 storyboard; commit as asset/screencast/onibi-launch.{mp4,gif}.
- D8: docs/blog/2026-06-XX-launch.md — ~1500 words on the multi-transport approval protocol design.
- D9: docs/launch/{hn.md, x-thread.md, lobsters.md, devto.md, reddit.md, mastodon.md} drafts.
- D10: Final QA pass — all tests green, all transports working, dry-runs holding, audits clean, install one-liner verified by external party.
- D11: Tag v1.5.0 and push; GitHub Actions auto-build binaries; craft GH Release notes.
- D12: Post-launch monitoring readiness — hotfix branch + triage criteria documented.

Verify all 9 exit criteria. After PR merge and tag, fire launch channels in coordination: HN first, X within 30 min, others same-day.

FILES YOU MAY EDIT:
- docs/* (security, architecture, adapters, transports, blog, launch, dry-run-log)
- README.md
- asset/screencast/*
- app/src-tauri/src/cli/doctor.rs (new file)
- app/src-tauri/src/server/* (only for hardening: rate-limit middleware, body-size limits)
- app/src-tauri/Cargo.toml (tower-governor; any audit-driven version bumps)
- mobile/* (only for hardening: CSP headers in index.html)

FILES YOU MUST NOT TOUCH (everything not listed above):
- adapters/* (frozen)
- SPEC.md (frozen at v1.0)
- app/src-tauri/src/pty/* (frozen)
- app/src-tauri/src/transport/* (frozen)
- app/src-tauri/src/adapters/* (frozen)
- app/src-tauri/src/approval/* (frozen)

Open a PR titled `phase-07: hardening, docs, launch` against branch v1.5. After approval, merge → tag v1.5.0 → fire launch sequence. Then merge v1.5 → main on Day +1.
```

---

## Notes for the dispatcher (the human running these prompts)

- **Do not skip Prompt-00**. Half of the later prompts assume the scaffold layout exists.
- **Parallel groups** (C and D) work best when the two agents are running in separate machines or, at minimum, separate worktrees. Use `git worktree add ../onibi-phase-02 v1.5/phase-02-ui-shell` and `../onibi-phase-03 v1.5/phase-03-adapters` to physically isolate them.
- **Coordinate the small shared diffs** explicitly: PHASE-02 and PHASE-03 both touch `app/src-tauri/src/main.rs` (manage state, register commands). PHASE-04 and PHASE-05 both touch `app/src-tauri/src/server/routes.rs` (route registration). If both agents tried to modify those files at once, they'd merge-conflict. The fix: one agent does its full diff and PRs first; the other rebases.
- **Read CLAUDE.md every time**. The user's communication / coding style rules in `/Users/gongahkia/Desktop/coding/projects/onibi/CLAUDE.md` apply project-wide. Terseness, in-line comments only, lowercase by default, no auto-refactor outside scope.
- **PR reviews**: assign @gongahkia. No agent merges its own PR. Human approves merge in all cases.
- **If a prompt finishes early**: do NOT pull work from the next phase. Ship the PR, wait for review, then await dispatch of the next prompt.
- **If a prompt is blocked**: leave a `BLOCKED:` note in the PR body explaining the dependency, and stop. Do not invent workarounds outside scope.
