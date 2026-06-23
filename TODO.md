# Onibi v3 — TODO (todo.txt format)

> This file is the single source of truth for the Onibi v3 build (Telegram excised → web cockpit + live xterm.js terminal). An independent coding agent with access to only this file and the repo can execute every task below in order.
>
> **Read sections 1–6 before starting any task.** They contain the *why*, the *must-not-regress rules*, and the *do-not-rewrite* map. Skipping them will cost time later.

---

## How to read this file

- Task lines below the `=== Tasks ===` marker follow [todo.txt](https://todotxt.org) format.
- Conventions used:
  - `(A)` = day-1 critical / blocking / correctness rule. `(B)` = required for phase to be "done". `(C)` = polish, can defer within phase.
  - `YYYY-MM-DD` = task creation date (treat all tasks as created 2026-06-23 unless otherwise stated).
  - `+phaseNN` groups tasks by phase. Phases are sequential: do not start phase N+1 until phase N is accepted.
  - `@backend` `@frontend` `@build` `@tests` `@docs` `@cleanup` `@release` work-area contexts.
  - `file:<path>` indicates the primary file the task modifies (one per task; secondary files listed inline in description).
  - `id:T###` unique task id. Cross-reference via `blocked-by:T###`.
  - `accept:<short>` inline acceptance criterion summary.
  - Lines starting with `#` or `>` are comments / section dividers, ignored by todo.txt parsers.
- Do not start a `(B)` or `(C)` task before all `(A)` tasks in the same phase pass. Day-1 correctness rules apply across all phases.

---

## 1. Context — why this work exists

Onibi today is a "Telegram-controlled coding-agent host for local terminals" (README.md:15-17). It runs as a local user daemon, hosts coding agents (Claude Code, Codex, Goose, Gemini, etc.) under PTYs via `internal/pty/host.go`, and routes approvals + prompt queues + render snapshots through a Telegram bot living in `internal/telegram/`. Adapters in `adapters/<agent>/` install hooks that call `onibi-notify` over a Unix socket (`internal/intake/`) into the daemon's approval queue (`internal/approval/`).

The friction the user wants gone: first-time install requires talking to @BotFather on Telegram, copy-pasting a bot token, then scanning a deeplink to pair the owner chat. That is the wall users hit pre-install.

This pivot replaces the Telegram transport with a self-hosted HTTP+WebSocket server on the laptop. Phone scans a QR printed on `onibi up`, the QR encodes a `https://<host>:<port>/pair/<token>` URL, the user opens it in mobile Safari/Chrome, the page sets an owner cookie, and from that point the phone has a live xterm.js view of the agent's PTY plus an approval-cockpit overlay.

**Important historical context: the user previously designed exactly this in "v1.5" (Tauri + xterm.js + PWA + multi-transport), then pivoted to v2 (Telegram) with a hard rule "no third pivot in this codebase." That rule is being deliberately broken with the user's consent.** Document this in the CHANGELOG when you touch it (Phase 6).

**What this is not**: a port of "picpocket" (a separate project that ships in-room slide screen-sharing via QR + WebSocket). Picpocket gives ~500 LOC of useful pair-token + role-based WebSocket patterns, but its actual transport pushes JPEG frames, its QR encodes `window.location.origin` (broken on cellular), and it does not solve PTY streaming or input forwarding. We are building v1.5's mobile-second-screen vision; picpocket inspired the QR pattern only.

---

## 2. Outcome target (what "done" looks like for v1)

User runs `onibi up` on their laptop. Terminal prints a QR for a LAN URL (e.g. `https://onibi.local:8443/pair/<43chars>` or `https://192.168.1.42:8443/pair/<43chars>`). User scans on phone; mobile Safari opens; the `/pair/:token` handler consumes the single-use token, sets an HttpOnly+Secure+SameSite=Strict owner cookie, and redirects to `/`. The phone now shows a live xterm.js view of the laptop's current ghostty/tmux pane. User can type into it (input forwards to the PTY's stdin). When Claude Code or another adapter fires an approval hook (via `onibi-notify`), an approval card overlays the terminal with Approve / Deny / Edit buttons. Tapping a verdict unblocks the hook. `/interrupt` and `/kill` buttons send SIGINT / SIGKILL to the PTY's process group. Telegram code is gone. README is rewritten.

Subsequent versions (out of v1 scope, documented in §9):
- **v1.1**: `--transport=tailscale` flag for Tailscale Funnel (cellular OK, persistent URL).
- **v1.2**: Cloudflare Named Tunnels (advanced transport, account-bound).
- **v1.3**: Cloudflare Quick Tunnel with end-to-end crypto (fragment-keyed bootstrap, sshx-pattern). MUST NOT ship Cloudflare Quick Tunnel without E2E.

---

## 3. Architecture rules — do not regress

These rules apply to every task. Violations are bugs and must be fixed before the phase is accepted.

1. **No subscriber may backpressure the PTY reader.** Per-subscriber bounded channels (16-32 KiB). Drop-oldest on overflow. Send a marker frame `[N bytes dropped]` followed by full ring-buffer snapshot replay on the next subscriber message. A slow phone on cellular must never stall an `npm install` running in the local PTY.
2. **PTY size honors client viewport.** Default is `pty.DefaultRows=40 / DefaultCols=100` (see `internal/pty/host.go:18`), which is wrong for a portrait phone (~30 cols × 60 rows). Implement WebSocket `resize` messages on day 1 → `pty.Setsize`. Resize on first attach and on any `visualViewport` change.
3. **TLS is always required.** Even in dev. Use mkcert-issued local CA for LAN. There is no plaintext WS code path to ship.
4. **Pair token is single-use.** Reuse existing `setup.Consume` semantics in `internal/setup/pairing.go` (32B random base64url, 5-min TTL, atomic single-use SQLite consume). Do not rewrite the token model.
5. **Owner enforcement on every WebSocket upgrade.** Cookie required; query token bound to cookie; both rotate on activity. iOS Safari has aggressive cookie rules over WS — verify cookie actually flows on upgrade in a real iPhone test, not just curl.
6. **No SSE.** Use WebSocket for everything (events + PTY stream). Cloudflare Quick Tunnel (v1.3) does not support SSE; consistency across transports avoids divergent code.
7. **Coalesce broadcast writes at 16ms (~60 fps).** A `npm install` produces thousands of small writes; one frame per tick is plenty. Do not raw-pipe each PTY read to all subscribers.
8. **Canvas renderer for xterm.js, not WebGL.** WebGL has a black-screen-on-tab-switch bug on iOS 17+.
9. **Use stable `--json` flags for external tooling.** Specifically: `tailscale status --json`, `tailscale serve status --json`. Never parse text output (changed twice in the last year).
10. **App-level WS ping every 30s.** Cloudflare proxies enforce a 100s idle disconnect; even though Cloudflare ships in v1.3, build the heartbeat now so v1.3 doesn't require a protocol change.
11. **Fragment-keyed E2E bootstrap (v1.3 design intent — DO NOT ship Cloudflare Quick Tunnel before this is in place).** QR must encode `https://<id>.trycloudflare.com/#k=<key>`. Key lives in URL fragment, never sent to relay. Server stores only HMAC commitment. Reuse HKDF+AES-GCM primitives from `internal/envelope/envelope.go`. Any earlier CF implementation MUST be gated behind `--unsafe-cloudflare-no-e2e` and refuse to ship in tagged releases.

---

## 4. Reuse map — these files do not get rewritten

Do not rewrite or refactor the following modules unless a task explicitly says to. They are transport-agnostic or self-contained and survive the pivot:

| Path | What it gives us |
|---|---|
| `internal/setup/pairing.go` (78 LOC) | Pair-token gen (`NewToken`), single-use atomic consume (`Consume`), TTL constants (`PairTokenTTL = 5m`). Reuse verbatim. Only the URL it gets embedded in changes from `t.me/<bot>?start=pair_<tok>` to `https://<host>:<port>/pair/<tok>`. |
| `internal/setup/qr.go` (43 LOC) | `PrintQR(w io.Writer, text string)` Unicode half-block QR renderer. Reuse verbatim. |
| `internal/approval/queue.go` | Transport-agnostic approval state machine (pending → approved/denied/edited/expired). The phone web UI feeds decisions through `Queue.Decide` the same way Telegram does. |
| `internal/approval/scrub.go` | Sensitive-input redaction. Apply to data shown in approval cards on the phone. |
| `internal/approval/risk.go` | High-risk-tool double-confirm logic (file_write, execute, etc.). Apply to approval card UX. |
| `internal/store/sqlite.go` | SQLite schema. You will ADD new tables (`web_sessions`, `web_pair_tokens` if the existing `pairing_tokens` schema needs to diverge) but the existing schema and migration mechanism stays. |
| `internal/intake/server.go` | Unix-socket intake from agent hooks (`onibi-notify`). Unchanged across the pivot — hooks still write to `/tmp/onibi.sock`-style intake. |
| `adapters/*` + `internal/adapters/*` | All agent adapters (Claude Code, Codex, OpenCode, Goose, Gemini, GitHub Copilot CLI, Pi, Amp, shell). Their hooks fire `onibi-notify` independent of transport. Unchanged. |
| `internal/envelope/envelope.go` | HKDF + AES-GCM primitives. Untouched in v1. Reused in v1.3 for E2E. |
| `internal/render/png.go` (485 LOC) | `RenderPNG(buf []byte, opts) ([]byte, error)` — stateless PTY-bytes-to-PNG via `jaguilar/vt100`. NOT used in v1 (xterm.js renders ANSI directly). Keep as low-bandwidth fallback for post-v1 use. |
| `internal/render/text.go` (191 LOC) | Text render path. Possibly used in scrollback hydration on attach. Keep. |
| `internal/pty/host.go` Spawn / SpawnOptions / Wait / Close / Write | The PTY lifecycle. Refactor only `Pipe(io.Writer)` → `Subscribe()` (Phase 1, task T003). Everything else stays. |
| `internal/buildinfo/*` | Version stamping. Unchanged. |
| `internal/service/*` | LaunchAgent / systemd user-level install. Possibly minor edits to drop Telegram health checks; do not rewrite. |
| `internal/cli/<existing subcommand handlers>` except `up`, `setup` | Most subcommands (`adapters`, `doctor`, `run`, `shell`, `wrap`, `config`, `log`, `project`, `install-service`, `uninstall`, `mcp`) stay. `up` and `setup` get rewrites. |

---

## 5. File modification ledger (cross-reference)

| Path | Action | Phase | Notes |
|---|---|---|---|
| `internal/pty/host.go` | Modify | 01 | Add Subscriber hub, bounded chans, resize plumbing |
| `internal/setup/pairing.go` | Reuse | 03 | URL-shape callers change; file itself unchanged |
| `internal/setup/qr.go` | Reuse | 03 | Unchanged |
| `internal/setup/wizard.go` (480 LOC) | Rewrite | 03,06 | Phase 3 adds web pair path; Phase 6 strips BotFather |
| `internal/daemon/daemon.go` | Modify | 02 | Wire web HTTP server alongside intake socket |
| `internal/daemon/approvals.go` (34KB) | Refactor | 05 | Queue retained; rendering moves to `/ws/events` emitter |
| `internal/daemon/commands.go` (39KB) | Refactor | 05,06 | Dispatch becomes WS messages + `/control` POST handlers |
| `internal/daemon/webapp.go` | Delete | 06 | Mini App obsolete |
| `internal/daemon/terminal.go` | Modify | 02,06 | Keep tmux+Ghostty viewer launch; drop Telegram-specific bits |
| `internal/telegram/*` (~1500 LOC live + ~700 tests + ~250 mock) | Delete | 06 | Entire directory |
| `internal/miniappurl/*` | Delete | 06 | Obsolete |
| `internal/envelope/*` | Retain | — | Referenced by v1.3 E2E design |
| `internal/auth/owner.go` | Modify | 02,06 | Swap Telegram owner_id model → web session cookie identity |
| `internal/secrets/*` | Modify | 06 | Drop Telegram bot-token keychain key; retain cert paths |
| `internal/config/settings.go` | Modify | 06 | Drop `Telegram.*` (lines 64-67); add `Web.*`, `Transport.*` |
| `internal/doctor/*` | Modify | 06,08 | Drop Telegram checks; add LAN/Tailscale/cert health |
| `internal/cli/up.go` | Rewrite | 02,06 | Phase 2 starts web server alongside; Phase 6 removes Telegram |
| `internal/cli/setup.go` (or equivalent) | Rewrite | 03,06 | Web pair wizard replaces BotFather flow |
| `internal/cli/pair.go` | New | 06 | `onibi pair` subcommand to print new QR mid-session |
| `internal/cli/unpair.go` | New | 06 | `onibi unpair [device-id]` to revoke a device |
| `internal/cli/devices.go` | New | 06 | `onibi devices` to list paired devices |
| `cmd/onibi/main.go` | Modify | 06 | Wire new init / subcommand registry |
| `cmd/gen-readme/*` | Modify | 06 | Update README generator templates |
| `internal/web/` | New | 02 | HTTP/WS server package |
| `internal/web/server.go` | New | 02 | HTTP listener + TLS + routes |
| `internal/web/auth.go` | New | 02 | Cookie middleware + WS upgrade auth |
| `internal/web/ws_pty.go` | New | 02 | `/ws/pty` handler — binary protocol |
| `internal/web/ws_events.go` | New | 02,05 | `/ws/events` handler — JSON protocol |
| `internal/web/control.go` | New | 02,05 | `/control` POST handler |
| `internal/web/pair.go` | New | 03 | `/pair/:token` handler |
| `internal/web/static/` | New | 04 | Embedded frontend bundle via `go:embed` |
| `internal/web/cert.go` | New | 02 | mkcert-issued local CA management |
| `internal/web/transport/` | New | 02,08 | Transport selectors (LAN, Tailscale) |
| `internal/web/transport/lan.go` | New | 02,03 | LAN IP detection + URL construction |
| `internal/web/transport/tailscale.go` | New | 08 | Tailscale Funnel detection + invocation |
| `frontend/` | New | 04 | Vite + TypeScript + xterm.js source |
| `frontend/package.json` | New | 04 | Deps: xterm, @xterm/addon-fit, vite, typescript |
| `frontend/vite.config.ts` | New | 04 | Content-hashed filenames + manifest output |
| `frontend/tsconfig.json` | New | 04 | Strict TS |
| `frontend/index.html` | New | 04 | Single-page entry |
| `frontend/src/main.ts` | New | 04 | Terminal mount + WS wire |
| `frontend/src/ws.ts` | New | 04 | WS client with resume |
| `frontend/src/terminal.ts` | New | 04 | xterm.js bootstrap + addons |
| `frontend/src/approval.ts` | New | 05 | Approval card overlay UI |
| `frontend/src/softkeys.ts` | New | 07 | Soft key bar component |
| `frontend/src/pwa.ts` | New | 07 | Service worker register |
| `frontend/manifest.webmanifest` | New | 07 | PWA manifest |
| `frontend/src/sw.ts` | New | 07 | Service worker (asset cache only; no push) |
| `Makefile` | Modify | 04,06 | Add `frontend` target + `embed` step before `go build` |
| `README.md` | Rewrite | 06 | Drop "Telegram-controlled" framing; describe LAN web cockpit |
| `CHANGELOG.md` (new or existing) | Modify | 06 | Breaking-change notice; v3 indicator |
| `.gitignore` | Modify | 00 | Add `bin/onibi-notify`, `frontend/dist/`, `frontend/node_modules/`, mkcert local cert files |
| `docs/ios-cert-install.md` | New | 03 | iOS self-signed cert trust profile install one-pager |
| `docs/transports.md` | New | 02,08 | Document LAN + Tailscale + future Cloudflare |

---

## 6. Verification commands (per phase, run as you finish)

Run these from repo root unless noted.

- **Phase 00**: `go test ./...` baseline green; `git status` shows clean working tree before changes; `ls internal/telegram/ | wc -l` > 0 (Telegram still alive).
- **Phase 01**: `go test ./internal/pty/... -race -count=1` green. Manual: spawn `htop` under PTY, attach 2 net listeners, both render in real time; sleep 100ms on one listener and confirm the other does NOT block.
- **Phase 02**: `curl -k https://localhost:8443/healthz` returns 200. `websocat --tls-domain=localhost wss://localhost:8443/ws/pty?token=...` reads/writes against a spawned bash. Cookie-less request to `/ws/pty` returns 401.
- **Phase 03**: `rm -rf ~/.local/share/onibi/onibi.db && onibi up` prints QR; scan from real iPhone (not just curl) → cookie set → `/` returns 200; second iPhone hits `/` and gets 403; reuse of consumed token returns 401.
- **Phase 04**: open phone browser to `/` post-pair, drive `vim` for 2 minutes including save, search, resize on rotate. Force airplane-mode for 10s mid-stream and confirm xterm.js reconnects and replays without scrollback corruption.
- **Phase 05**: install Claude Code via `onibi adapters install claude`, start a Claude session, trigger an Edit tool call; approval card appears on phone; tap Deny; hook exits with denial JSON; verify file was NOT modified. Repeat with Bash tool. Tap `/interrupt`; terminal shows `^C`.
- **Phase 06**: `rg -i telegram internal/` returns nothing live (only historical docs comments allowed); `rg -i botfather .` empty; `go test ./... -race -count=1` green; `go vet ./...` clean; `staticcheck ./...` clean; `goreleaser snapshot --skip=publish --clean` builds; binary size at least 20% smaller than v2.
- **Phase 07**: 5-minute real-iPhone session driving `vim` + `claude` + `tmux` window switch via soft-key bar without rage-quit; on-screen Tab triggers shell completion; "Add to Home Screen" works and launches in standalone mode.
- **Phase 08**: on a tailnet-joined Mac, `onibi up --transport=tailscale` prints a `*.ts.net` URL; iPhone on LTE (Wi-Fi disabled) scans QR, opens URL, sees live pane and types into it.

---

## 7. Open risks (acknowledge; some have tasks below)

- **iOS HTTPS Local Network prompt + self-signed cert profile install.** Apple does not allow programmatic trust. Ship `docs/ios-cert-install.md` (task T030). Friction acknowledged.
- **mkcert dependency.** Bundling as a Go lib (uses `crypto/tls` under the hood) vs shelling out to mkcert binary — decide in Phase 2 (task T015a is decision-point).
- **xterm.js bundle size** ~150 KB gz with required addons. Acceptable; monitor. Fail CI if exceeds 250 KB gz.
- **Telegram excision side-effects in `internal/auth/` and `internal/secrets/`.** Both have non-Telegram callers. Review carefully in Phase 6, not a blind delete (tasks T058, T059).
- **`onibi-notify` accidentally committed at repo root** (per project memory). Clean up in Phase 0 (task T092).
- **"No third pivot" v2 rule.** Explicitly broken. Document in CHANGELOG (task T064).
- **iOS Safari WS cookie behavior** over a `trycloudflare.com` origin (future) — verify before v1.3 ships.

---

## 8. Glossary

- **PTY**: pseudo-terminal device. `creack/pty` wraps system-level PTY for Go. `Master *os.File` is the controller side; the child process sees a real tty.
- **PG**: process group. `kill(-pgid, SIGINT)` sends to whole group — needed for `^C` to propagate to a shell's children.
- **Ring buffer**: fixed-size in-memory buffer of last N bytes of PTY output, used for snapshot replay on (re)attach.
- **Coalescing**: collecting multiple small writes within a time window and emitting one frame. Reduces WS overhead on busy streams.
- **Resume token / `last_seq`**: client-tracked sequence number; on reconnect, client sends it so server can replay from there if still in ring buffer, else send full snapshot + new seq base.
- **mkcert**: FiloSottile's tool that installs a local CA in OS trust store so dev `https://*.local` URLs work without browser warnings — except on iOS, where the user has to install a configuration profile manually.
- **Funnel** (Tailscale): public-internet ingress that proxies HTTPS to a tailnet node. TLS terminates at Tailscale's edge node, but per Tailscale's own docs, the funnel relay does not see decrypted application bytes.
- **Quick Tunnel** (Cloudflare): zero-config `cloudflared tunnel --url http://localhost:port` → random `*.trycloudflare.com` URL. CF relay CAN see plaintext; requires app-level E2E to be safe.

---

## === Tasks ===

> Tasks are grouped by phase. Within a phase, do `(A)` first, then `(B)`, then `(C)`. Mark complete by replacing `(A)`/`(B)`/`(C)` prefix with `x YYYY-MM-DD` (the date you completed it). Example: `x 2026-06-30 Refactor ... id:T001`.

### Phase 03 — Web pair flow (0.5 week)

> Goal: replace the t.me deeplink with a web URL; reuse existing pair-token model verbatim; new `/pair/:token` handler consumes token and sets cookie.

(B) 2026-06-23 Smoke test on real iPhone: rm -rf state db, onibi up, scan QR from phone, verify cookie set in Safari devtools, reload / returns 200; second iPhone hits / and gets 403 +phase03 @tests id:T307 blocked-by:T306 accept:cookie-survives-reload-on-iOS-Safari

### Phase 04 — xterm.js terminal SPA (1.5 weeks)

> Goal: build the frontend that renders a live terminal on the phone and connects to /ws/pty. No approval UI yet — terminal stream + input only.

(B) 2026-06-23 Manual smoke: drive vim on real iPhone via the pair URL for 2 minutes; resize on rotate; airplane-mode for 10 seconds and confirm clean reconnect with snapshot replay +phase04 @tests id:T415 blocked-by:T413 accept:no-scrollback-corruption-after-resume

### Phase 05 — Approval cockpit overlay (2.0 weeks)

> Goal: wire `internal/approval/queue.go` to `/ws/events`; render approval cards on the phone over the terminal; round-trip Approve/Deny/Edit back to Queue.Decide so the agent hook unblocks.

(A) 2026-06-23 Define /ws/events JSON protocol in docs/ws-events-protocol.md: messages are JSON envelopes {type, ts, payload}; types include "approval.requested", "approval.decided", "approval.expired", "session.started", "session.ended", "render.event" +phase05 @docs file:docs/ws-events-protocol.md id:T500
(A) 2026-06-23 Add approval.Queue subscriber API: Queue.Subscribe() (<-chan Event, unsub func()) so multiple consumers (existing audit log + new web emitter) can both observe state transitions +phase05 @backend file:internal/approval/queue.go id:T501 blocked-by:T500
(A) 2026-06-23 Implement web events emitter: internal/web/ws_events.go pump that subscribes to approval.Queue + session registry + selected render events, marshals to JSON envelope, sends as WS text frames to all attached /ws/events clients +phase05 @backend file:internal/web/ws_events.go id:T502 blocked-by:T501
(A) 2026-06-23 On approval.requested event, payload includes id, session_id, tool, scrubbed_input (apply approval/scrub.go), risk_level (apply approval/risk.go), expires_at +phase05 @backend file:internal/web/ws_events.go id:T503 blocked-by:T502
(A) 2026-06-23 Add POST /approval/:id route: JSON {verdict: "approve"|"deny"|"edit", edited_input?: string, reason?: string}; resolves to Queue.Decide(id, Decision{...}); auth required +phase05 @backend file:internal/web/approval.go id:T504 blocked-by:T503
(A) 2026-06-23 On the phone side, write frontend/src/approval.ts: ApprovalCard component (vanilla TS, no framework) rendering tool name, scrubbed input (monospace, line-numbered), risk badge, Approve / Deny / Edit buttons +phase05 @frontend file:frontend/src/approval.ts id:T505 blocked-by:T504
(A) 2026-06-23 Edit button opens an inline edit pane: textarea pre-filled with current input; on submit POST /approval/:id with verdict "edit" and edited_input +phase05 @frontend file:frontend/src/approval.ts id:T506 blocked-by:T505
(A) 2026-06-23 Add EventsWS class in frontend/src/events.ts paralleling ws.ts but for /ws/events JSON stream; emits typed events; auto-reconnect +phase05 @frontend file:frontend/src/events.ts id:T507 blocked-by:T506
(A) 2026-06-23 Render approval card stack as absolutely-positioned overlay above the terminal (z-index 10, max-height 60dvh, bottom-anchored, scrollable); auto-show when approval.requested fires; auto-dismiss on approval.decided +phase05 @frontend file:frontend/src/approval.ts id:T508 blocked-by:T507
(A) 2026-06-23 Add /control phone-side buttons: Interrupt and Kill buttons in a top toolbar; POST to /control with the active session_id +phase05 @frontend file:frontend/src/main.ts id:T509 blocked-by:T508
(A) 2026-06-23 High-risk double-confirm: if risk_level=="high" the Approve button text becomes "Approve (tap twice)" and requires two taps within 2 seconds; reuse approval/risk.go High-list +phase05 @frontend file:frontend/src/approval.ts id:T510 blocked-by:T508
(B) 2026-06-23 Replace internal/daemon/approvals.go Telegram-rendering code path with a no-op stub during Phase 5 (full delete in Phase 6); the web emitter is the new primary; keep hook RPC reply logic intact +phase05 @backend file:internal/daemon/approvals.go id:T511 blocked-by:T504
(B) 2026-06-23 End-to-end test with Claude Code adapter: onibi adapters install claude; start a Claude session; trigger an Edit tool call; assert card appears on phone within 1s; tap Deny; assert hook exits with denial JSON and file is NOT modified +phase05 @tests id:T512 blocked-by:T511 accept:hook-exit-code-correct
(B) 2026-06-23 End-to-end test: trigger a Bash tool call; tap Edit; modify command; tap Approve; assert modified command runs and original does not +phase05 @tests id:T513 blocked-by:T512
(B) 2026-06-23 End-to-end test for /control: long-running command in PTY; tap Interrupt; assert ^C reaches the child process group via syscall.Kill(-pgid, SIGINT) +phase05 @tests id:T514 blocked-by:T513
(B) 2026-06-23 Unit tests: events emitter marshalling; approval.Subscribe receives events for all queue transitions; POST /approval/:id rejects unauthenticated +phase05 @tests file:internal/web/ws_events_test.go id:T515 blocked-by:T504
(C) 2026-06-23 Polish: approval card slide-in animation; haptic feedback on iOS via navigator.vibrate (will silently no-op on Safari) +phase05 @frontend file:frontend/src/approval.ts id:T516 blocked-by:T510

### Phase 06 — Telegram excision (1.5 weeks)

> Goal: delete `internal/telegram/`, strip BotFather flow, rewrite README/setup/up, ensure full test suite green. After this phase the Telegram surface is gone for good (git history retains it).

(A) 2026-06-23 Inventory all callers of internal/telegram/ outside the telegram package: rg --type=go 'github.com/gongahkia/onibi/internal/telegram' internal/ cmd/ — list every importer; each must be migrated or deleted +phase06 @cleanup id:T600 accept:list-of-callers-attached-to-commit
(A) 2026-06-23 Remove Telegram setup from internal/setup/wizard.go: delete BotFather prompts, bot username, getchatid step; replace with simple "your pair URL is X, scan the QR on your phone" web wizard +phase06 @backend file:internal/setup/wizard.go id:T601 blocked-by:T600
(A) 2026-06-23 Delete internal/telegram/ entirely: git rm -r internal/telegram +phase06 @cleanup id:T602 blocked-by:T601
(A) 2026-06-23 Delete internal/miniappurl/ entirely +phase06 @cleanup id:T603 blocked-by:T602
(A) 2026-06-23 Delete internal/daemon/webapp.go (Mini App handler obsolete; web UI replaces it) +phase06 @cleanup file:internal/daemon/webapp.go id:T604 blocked-by:T603
(A) 2026-06-23 Update internal/config/settings.go: remove Telegram struct (currently lines ~64-67 with EncryptedMode + MiniAppURL); add Web struct {ListenAddr string; CertDir string} and Transport struct {Mode string; SAddr string} +phase06 @backend file:internal/config/settings.go id:T605 blocked-by:T604
(A) 2026-06-23 Update internal/auth/owner.go: remove Telegram owner_id concept; identity is now the session cookie row in web_sessions; ensure existing non-Telegram callers (likely just owner checks) get a thin shim that returns the cookie-bound session +phase06 @backend file:internal/auth/owner.go id:T606 blocked-by:T605
(A) 2026-06-23 Update internal/secrets/: remove bot-token keychain key + retrieval helpers; keep cert paths + keychain key for the local CA private key (if any) +phase06 @backend file:internal/secrets/ id:T607 blocked-by:T606
(A) 2026-06-23 Update internal/doctor/: remove Telegram health checks (bot token present, polling reachable); add web-server health (port reachable, cert valid), LAN IP detection, mkcert availability; doctor now warns if no transport is configured +phase06 @backend file:internal/doctor/ id:T608 blocked-by:T607
(A) 2026-06-23 Rewrite internal/cli/up.go: remove Telegram bot startup branch; up always starts the web server; --transport flag accepts "lan" (default); mints pair token, prints QR, then blocks on signal; open laptop's default browser to the local URL via os/exec call to /usr/bin/open or xdg-open +phase06 @backend file:internal/cli/up.go id:T609 blocked-by:T608
(A) 2026-06-23 Add internal/cli/pair.go: onibi pair subcommand that mints a fresh pair token, prints QR + URL; useful mid-session for adding a second device +phase06 @backend file:internal/cli/pair.go id:T610 blocked-by:T609
(A) 2026-06-23 Add internal/cli/unpair.go: onibi unpair [--device-id ID] revokes a session row in web_sessions; with no arg shows interactive picker +phase06 @backend file:internal/cli/unpair.go id:T611 blocked-by:T610
(A) 2026-06-23 Add internal/cli/devices.go: onibi devices lists all rows of web_sessions with first/last seen timestamps and device-label (set by client User-Agent or by user during pair) +phase06 @backend file:internal/cli/devices.go id:T612 blocked-by:T611
(A) 2026-06-23 Update cmd/onibi/main.go: register pair, unpair, devices subcommands; drop any Telegram-specific subcommand registration +phase06 @backend file:cmd/onibi/main.go id:T613 blocked-by:T612
(A) 2026-06-23 Update README.md: replace value-prop line "Telegram-controlled coding-agent host" with "Web-controlled coding-agent host with live xterm.js terminal and approval cockpit"; rewrite Quick Start, Modes, Command Surface, How It Works, Runtime Modes, Security sections to drop Telegram references; replace the "transport=Telegram" badge with one indicating LAN/web +phase06 @docs file:README.md id:T614 blocked-by:T613
(A) 2026-06-23 Add CHANGELOG.md entry: "## v0.3.0 — Web cockpit pivot" with sections (Breaking: Telegram bot transport removed; Migration: re-pair via web flow; Why: friction; Acknowledgement: this is a third pivot, explicitly breaking the v2 hard rule with user consent); reference original plan +phase06 @docs file:CHANGELOG.md id:T615 blocked-by:T614
(A) 2026-06-23 Update cmd/gen-readme/* templates if they reference Telegram +phase06 @docs file:cmd/gen-readme/ id:T616 blocked-by:T614
(B) 2026-06-23 Acceptance check: rg -i 'telegram' internal/ cmd/ adapters/ — only allowed hits are historical docs comments; rg -i 'botfather' . — must be empty except CHANGELOG +phase06 @cleanup id:T617 blocked-by:T616 accept:no-live-telegram-references
(B) 2026-06-23 Run go test ./... -race -count=1 — must be green; go vet ./... clean; staticcheck ./... clean +phase06 @tests id:T618 blocked-by:T617 accept:CI-equivalent-green-locally
(B) 2026-06-23 Run goreleaser snapshot --skip=publish --clean — must build artifacts for all configured targets; record final binary size and confirm >=20% smaller than v2 +phase06 @release id:T619 blocked-by:T618
(B) 2026-06-23 Update Makefile: remove any Telegram-specific targets (e.g., telegram-probe); ensure make test includes the new frontend npm test command if added +phase06 @build file:Makefile id:T620 blocked-by:T619

### Phase 07 — Mobile polish + soft keyboard + PWA (1.0 week)

> Goal: make the phone UX driveable for 5 minutes without rage-quitting. Soft key bar, paste shim, reconnect toast, PWA manifest.

(A) 2026-06-23 Write frontend/src/softkeys.ts: SoftKeyBar component rendering a bottom-fixed bar of buttons [Esc, Tab, Ctrl, Alt, ↑, ↓, ←, →, ^C, ^D, ^Z] +phase07 @frontend file:frontend/src/softkeys.ts id:T700 blocked-by:T415
(A) 2026-06-23 Soft-key state machine: Ctrl and Alt are sticky-toggle modifiers (one-shot, deactivate after next key); other keys are direct send; on tap, simulate term.write of the corresponding control sequence to PTY +phase07 @frontend file:frontend/src/softkeys.ts id:T701 blocked-by:T700
(A) 2026-06-23 Long-press repeat for arrow keys: 500ms long-press starts repeating at 100ms intervals until pointerup +phase07 @frontend file:frontend/src/softkeys.ts id:T702 blocked-by:T701
(A) 2026-06-23 Paste shim: hidden Paste button reads navigator.clipboard.readText() (requires user gesture) and sends text to PTY; document iOS Safari clipboard prompt expectation +phase07 @frontend file:frontend/src/softkeys.ts id:T703 blocked-by:T702
(A) 2026-06-23 Reconnect toast: when WS reconnect triggers, show a non-blocking toast "Reconnecting…" that fades on successful resume +phase07 @frontend file:frontend/src/ws.ts id:T704 blocked-by:T703
(A) 2026-06-23 PWA manifest: add frontend/manifest.webmanifest with name "Onibi", short_name "Onibi", icons (192, 512), start_url "/", display "standalone", theme_color "#000000", background_color "#000000"; link from index.html +phase07 @frontend file:frontend/manifest.webmanifest id:T705 blocked-by:T704
(A) 2026-06-23 Generate PWA icons at 192 and 512: re-use existing asset/logo/onibi.png cropped to square; place in frontend/public/icons/ +phase07 @frontend file:frontend/public/icons/ id:T706 blocked-by:T705
(A) 2026-06-23 Service worker (asset cache only, NO push): frontend/src/sw.ts registers via main.ts; caches /assets/* with stale-while-revalidate; never caches root index.html or /ws/* or /api/* +phase07 @frontend file:frontend/src/sw.ts id:T707 blocked-by:T706
(A) 2026-06-23 iOS viewport pinning: in main.ts attach visualViewport listener; on keyboard show/hide, scroll the terminal to keep cursor visible +phase07 @frontend file:frontend/src/main.ts id:T708 blocked-by:T707 ref:arch-rule-2
(A) 2026-06-23 Confirm 100dvh sizing applied to html and body in index.html stylesheet; verify no 100vh remains anywhere in frontend/ +phase07 @frontend file:frontend/index.html id:T709 blocked-by:T708
(B) 2026-06-23 Real-iPhone session test: drive vim (open, edit, save, quit) + claude (send prompt, wait, approve) + tmux (switch window) for 5 minutes via soft-key bar without rage-quitting +phase07 @tests id:T710 blocked-by:T709 accept:session-completed-without-bug
(B) 2026-06-23 Test "Add to Home Screen" flow on iOS Safari: launch from home screen icon, app opens in standalone mode (no Safari chrome), still pairs with cookie-set, terminal still loads +phase07 @tests id:T711 blocked-by:T710
(C) 2026-06-23 Dark/light mode toggle in soft-key bar (xterm.js theme swap) — optional +phase07 @frontend id:T712 blocked-by:T710

### Phase 08 — Tailscale Funnel transport (1.0 week, v1.1)

> Goal: add `--transport=tailscale` so phone can connect over cellular without LAN.

(A) 2026-06-23 Add --transport flag to cli/up.go: values lan (default), tailscale, auto; auto tries tailscale first then falls back to lan +phase08 @backend file:internal/cli/up.go id:T800 blocked-by:T619
(A) 2026-06-23 Implement internal/web/transport/tailscale.go: Detect() (bool, error) shells out to `tailscale status --json` (use os/exec.Command); parse JSON; return true if BackendState=="Running" and the Self.HasFunnelEligibleCap or equivalent flag is set +phase08 @backend file:internal/web/transport/tailscale.go id:T801 blocked-by:T800 ref:arch-rule-9
(A) 2026-06-23 Implement Tailscale.Enable(port int) error: invokes `tailscale funnel --bg <port>` (background); returns when funnel is active +phase08 @backend file:internal/web/transport/tailscale.go id:T802 blocked-by:T801
(A) 2026-06-23 Parse `tailscale serve status --json` to get the assigned funnel URL (something like https://device-name.tailnet.ts.net/); return as TailscaleTransport.URL() string +phase08 @backend file:internal/web/transport/tailscale.go id:T803 blocked-by:T802
(A) 2026-06-23 Wire transport into pair flow: if Tailscale transport active, web pair URL uses the funnel URL instead of LAN IP; QR encodes funnel URL +phase08 @backend file:internal/cli/up.go id:T804 blocked-by:T803
(A) 2026-06-23 On daemon shutdown, call `tailscale funnel --bg off` to clean up +phase08 @backend file:internal/web/transport/tailscale.go id:T805 blocked-by:T804
(A) 2026-06-23 No handler-side code changes needed (Tailscale terminates TLS at the node; requests arrive on loopback :8443 just like LAN); verify by hitting funnel URL from a different machine +phase08 @tests id:T806 blocked-by:T805
(B) 2026-06-23 Add doctor check for Tailscale: tailscale binary in PATH; tailscaled running; funnel feature enabled in admin (parse status --json AllowFunnel) +phase08 @backend file:internal/doctor/ id:T807 blocked-by:T805
(B) 2026-06-23 Documentation: docs/transports.md describing LAN vs Tailscale Funnel trust models; reference Tailscale's docs claim that Funnel relay does not see decrypted bytes +phase08 @docs file:docs/transports.md id:T808 blocked-by:T807
(B) 2026-06-23 E2E test on a tailnet-joined Mac with `onibi up --transport=tailscale`: open the funnel URL from an iPhone on LTE (Wi-Fi disabled) — confirm live terminal + input works +phase08 @tests id:T809 blocked-by:T808 accept:cellular-phone-can-drive-terminal

### Phase 09 — Release prep (0.5 week)

> Goal: tag v0.3.0, ship.

(A) 2026-06-23 Bump version in internal/buildinfo/ to 0.3.0 (or whatever v3 marker scheme is in use) +phase09 @release file:internal/buildinfo/ id:T900 blocked-by:T809
(A) 2026-06-23 Final test pass: go test ./... -race -count=1 + go vet + staticcheck + frontend tests (npm test if added) + manual smoke on real iPhone for both LAN and Tailscale +phase09 @tests id:T901 blocked-by:T900
(A) 2026-06-23 goreleaser release --skip=publish --clean dry run; confirm artifacts for darwin-amd64, darwin-arm64, linux-amd64, linux-arm64 +phase09 @release id:T902 blocked-by:T901
(B) 2026-06-23 Update gongahkia/homebrew-onibi tap manifest: new version + sha256s of darwin artifacts +phase09 @release id:T903 blocked-by:T902
(B) 2026-06-23 Developer-ID sign + notarize darwin builds (Gatekeeper requirement); upload notarization tickets +phase09 @release id:T904 blocked-by:T903
(B) 2026-06-23 Tag v0.3.0 in git; push tag; goreleaser release on tag to publish to GitHub releases +phase09 @release id:T905 blocked-by:T904
(C) 2026-06-23 Write a launch announcement (HN-style) — optional, post-tag +phase09 @docs id:T906 blocked-by:T905

---

## Future tasks (out of v1 / v1.1 scope; documented now so the agent does not start them by mistake)

> v1.2: Cloudflare Named Tunnels (account-bound, stable URL). No E2E required since CF can MITM in theory and account model gives liability surface. ~1 week.

(C) 2026-06-23 [V1.2 — do not start in v1] Implement --transport=cloudflare-named flag using `cloudflared tunnel run` against a user-configured named tunnel; auth via CF account token in keychain +phase12 @backend id:T1200

> v1.3: Cloudflare Quick Tunnel with full E2E crypto. ~3 weeks careful work. Read arch rule #11 before starting.

(C) 2026-06-23 [V1.3 — do not start until v1.2 stable] Design E2E protocol document: fragment-keyed bootstrap, HKDF over k_pair to derive enc_key + mac_key, AES-256-GCM frame encryption, sequence-numbered nonces, key commitment via HMAC; reuse internal/envelope/envelope.go primitives +phase13 @backend id:T1300 ref:arch-rule-11
(C) 2026-06-23 [V1.3 — do not start before T1300 approved] Implement client-side: read k from location.hash on page load; derive key via WebCrypto subtle; wrap WS messages in encrypt/decrypt; never log k +phase13 @frontend id:T1301 blocked-by:T1300
(C) 2026-06-23 [V1.3] Implement server-side encrypted relay: server holds only HMAC(k) commitment; cannot decrypt traffic; relay flag set in /healthz when active +phase13 @backend id:T1302 blocked-by:T1301
(C) 2026-06-23 [V1.3] Refuse to ship tagged releases with --transport=cloudflare-quick unless E2E flag is on; build-time gate +phase13 @build id:T1303 blocked-by:T1302

---

## End of file

> If you reach here and have implemented every (A) and (B) task above, the v1 + v1.1 web-cockpit pivot is complete. v1.2 and v1.3 are explicitly future work. Do not start them without re-reading section 3 rule 11 and discussing E2E threat model with the user.
