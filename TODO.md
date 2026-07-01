# Onibi v3 — TODO (todo.txt format)

> This file is the single source of truth for the Onibi v3 build (web cockpit + live xterm.js terminal by default, Telegram retained as optional chat transport). An independent coding agent with access to only this file and the repo can execute every task below in order.
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

Onibi started as a Telegram-controlled coding-agent host for local terminals. v3 makes the local HTTPS web cockpit the default first-run path while retaining Telegram as an explicit optional chat transport. It runs as a local user daemon, hosts coding agents (Claude Code, Codex, Goose, Gemini, etc.) under PTYs via `internal/pty/host.go`, and routes approvals + prompt queues through transport-specific surfaces. Adapters in `adapters/<agent>/` install hooks that call `onibi-notify` over a Unix socket (`internal/intake/`) into the daemon's approval queue (`internal/approval/`).

The friction the user wants gone: first-time install requires talking to @BotFather on Telegram, copy-pasting a bot token, then scanning a deeplink to pair the owner chat. That is the wall users hit pre-install.

This pivot replaces the Telegram transport with a self-hosted HTTP+WebSocket server on the laptop. Phone scans a QR printed on `onibi up`, the QR encodes a `https://<host>:<port>/pair/<token>` URL, the user opens it in mobile Safari/Chrome, the page sets an owner cookie, and from that point the phone has a live xterm.js view of the agent's PTY plus an approval-cockpit overlay.

**Important historical context: the user previously designed exactly this in "v1.5" (Tauri + xterm.js + PWA + multi-transport), then pivoted to v2 (Telegram) with a hard rule "no third pivot in this codebase." That rule is being deliberately broken with the user's consent.** Document this in the CHANGELOG when you touch it (Phase 6).

**What this is not**: a port of "picpocket" (a separate project that ships in-room slide screen-sharing via QR + WebSocket). Picpocket gives ~500 LOC of useful pair-token + role-based WebSocket patterns, but its actual transport pushes JPEG frames, its QR encodes `window.location.origin` (broken on cellular), and it does not solve PTY streaming or input forwarding. We are building v1.5's mobile-second-screen vision; picpocket inspired the QR pattern only.

---

## 2. Outcome target (what "done" looks like for v1)

User runs `onibi up` on their laptop. Terminal prints a QR for a LAN URL (e.g. `https://onibi.local:8443/pair/<43chars>` or `https://192.168.1.42:8443/pair/<43chars>`). User scans on phone; mobile Safari opens; the `/pair/:token` handler consumes the single-use token, sets an HttpOnly+Secure+SameSite=Strict owner cookie, and redirects to `/`. The phone now shows a live xterm.js view of a managed tmux-backed Onibi session. User can type into it, hand the same session to a visible Mac terminal, then hand it back to the phone without losing cwd/history/running process state. When Claude Code or another adapter fires an approval hook (via `onibi-notify`), an approval card overlays the terminal with Approve / Deny / Edit buttons. Tapping a verdict unblocks the hook. `/interrupt` and `/kill` buttons send SIGINT / SIGKILL to the session. Telegram remains opt-in via `onibi telegram setup` and `onibi up --transport=telegram`; it is not required for default onboarding. README is rewritten.

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
| `internal/service/*` | LaunchAgent / systemd user-level install. Possibly minor edits for web/default health checks; do not rewrite. |
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
| `internal/daemon/terminal.go` | Modify | 02,06 | Keep tmux+Ghostty viewer launch; remove only obsolete default-onboarding bits |
| `internal/telegram/*` | Retain | 06 | Optional Telegram chat transport; not default onboarding |
| `internal/miniappurl/*` | Delete | 06 | Obsolete |
| `internal/envelope/*` | Retain | — | Referenced by v1.3 E2E design |
| `internal/auth/owner.go` | Modify | 02,06 | Swap Telegram owner_id model → web session cookie identity |
| `internal/secrets/*` | Modify | 06 | Retain Telegram bot-token key for optional chat transport; retain cert paths |
| `internal/config/settings.go` | Modify | 06 | Drop `Telegram.*` (lines 64-67); add `Web.*`, `Transport.*` |
| `internal/doctor/*` | Modify | 06,08 | Add LAN/Tailscale/cert health; keep explicit optional provider checks |
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
- **Phase 06**: Telegram is not required in default onboarding; `onibi telegram setup/status/disable` and `onibi up --transport=telegram` remain explicit opt-in surfaces; `go test ./... -race -count=1` green; `go vet ./...` clean; `staticcheck ./...` clean; `goreleaser snapshot --skip=publish --clean` builds.
- **Phase 07**: 5-minute real-iPhone session driving `vim` + `claude` + `tmux` window switch via soft-key bar without rage-quit; `MAC` opens the same session in a visible macOS terminal; `PHONE` hands the same cwd/history/running process back to Safari; no visible `ONIBI-RESIZE:*` marker; Ctrl-C shutdown leaves no orphan `onibi-*` tmux session; on-screen Tab triggers shell completion.
- **Phase 08**: on a tailnet-joined Mac, `onibi up --transport=tailscale` prints a `*.ts.net` URL; iPhone on LTE (Wi-Fi disabled) scans QR, opens URL, sees live pane, types into it, and passes the managed session Mac <-> phone without losing state.

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
# 2026-06-24: real-iPhone pair/auth/cookie smoke passed; completed task removed.

### Phase 04 — xterm.js terminal SPA (1.5 weeks)

> Goal: build the frontend that renders a live terminal on the phone and connects to /ws/pty. No approval UI yet — terminal stream + input only.

# 2026-06-24: CLI web diagnostics and web shell availability completed; local smoke saw /session-info 200 and /ws/pty 101.
# 2026-06-24: T415 retry saw no phone requests before token expiry on `.local`; `onibi up` now prints LAN-IP fallback URLs.
# 2026-06-24: QR primary changed to LAN IP first; `.local` is fallback because iPhone did not reach mDNS URL.
# 2026-06-24: LAN-IP QR retry still saw no phone requests; local Mac health to LAN IP returned 200, so remaining failure is phone-to-Mac LAN reachability. IPv6 fallback URL formatting fixed.
# 2026-06-24: macOS firewall is enabled but block-all is off and incoming connections to `bin/onibi` are permitted.
# 2026-06-24: iPhone reached `/pair`, token was consumed, then `/` returned 403 with missing owner cookie after TLS `unknown certificate`; network reachability is OK, but Safari rejected/not-returned the Secure cookie on the untrusted self-signed leaf cert.
x 2026-06-24 Replace self-signed leaf TLS cert with mkcert/local-CA flow or explicit iOS-trustable cert profile path; add CLI preflight/diagnostic that warns before QR if iOS has not trusted the local cert, and after pair-cookie failure recommends cert trust first and hotspot only for LAN reachability failures +phase04 @backend file:internal/web/cert.go id:T416 blocked-by:T415 accept:forbidden-after-pair-has-actionable-diagnostic
# 2026-06-25: T415 passed on real iPhone over hotspot. Vim edit/save/quit worked with ESC toolbar; rotate worked; reconnect tested by background/return substitute because airplane mode would drop the hotspot hosting the Mac.
x 2026-06-25 Manual smoke: drive vim on real iPhone via the pair URL for 2 minutes; resize on rotate; airplane-mode for 10 seconds and confirm clean reconnect with snapshot replay +phase04 @tests id:T415 accept:no-scrollback-corruption-after-resume

### Phase 05 — Approval cockpit overlay (2.0 weeks)

> Goal: wire `internal/approval/queue.go` to `/ws/events`; render approval cards on the phone over the terminal; round-trip Approve/Deny/Edit back to Queue.Decide so the agent hook unblocks.

# 2026-06-25: T512 attempt over web cockpit reached Claude Code and Claude created `/tmp/onibi-approval-deny.txt`, but no Onibi approval overlay appeared; Claude's native terminal approval prompt handled the Write. Do not mark T512 done until hooks route the approval through Onibi and Deny prevents the write.
# 2026-06-26: `onibi up` now starts a managed tmux-backed session and passes its session id to `/ws/pty` + `/ws/events`; the spawned shell exports `ONIBI_SOCK` and `ONIBI_SESSION_ID=<managed-session-id>`, so Claude hooks can reach the web approval overlay across phone/Mac handover.
# 2026-06-25: T512 passed on real iPhone. Claude Write approval rendered in Onibi overlay; Deny posted `/approval/<id>` 200; Claude did not create `/tmp/onibi-approval-deny.txt` or `/tmp/onibi-approval-deny.tft`.
# 2026-06-25: T513 passed on real iPhone. Bash approval overlay edited command from `/tmp/onibi-original` to `/tmp/onibi-edited`; Claude output was `edited`; final local check showed original absent and edited present.
# 2026-06-25: T514 passed on real iPhone. INT sent Ctrl-C to the foreground PTY job; shell printed `^CINT_OK` and did not print `BAD`.
x 2026-06-25 End-to-end test with Claude Code adapter: onibi adapters install claude; start a Claude session; trigger an Edit tool call; assert card appears on phone within 1s; tap Deny; assert hook exits with denial JSON and file is NOT modified +phase05 @tests id:T512 accept:hook-exit-code-correct
x 2026-06-25 End-to-end test: trigger a Bash tool call; tap Edit; modify command; tap Approve; assert modified command runs and original does not +phase05 @tests id:T513 blocked-by:T512
x 2026-06-25 End-to-end test for /control: long-running command in PTY; tap Interrupt; assert Ctrl-C reaches the foreground PTY job and stops it before completion +phase05 @tests id:T514 blocked-by:T513
### Phase 06 — Web default + optional Telegram cleanup (1.5 weeks)

> Goal: make web cockpit the default setup/up path, keep Telegram as explicit optional chat transport, rewrite README/setup/up, ensure full test suite green.

# 2026-06-25: T608 uses Onibi's generated local CA/server cert diagnostics instead of mkcert-binary availability because mkcert is no longer the implementation path.
# 2026-06-25: T609 does not auto-open the pair URL in the laptop browser; doing so would consume the single-use phone pairing token. QR/URL output remains the canonical flow.
x 2026-06-25 Inventory all callers of internal/telegram/ outside the telegram package: rg --type=go 'github.com/gongahkia/onibi/internal/telegram' internal/ cmd/ — list every importer; each must be migrated or deleted +phase06 @cleanup id:T600 accept:list-of-callers-attached-to-commit
x 2026-06-25 Remove Telegram setup from internal/setup/wizard.go: delete BotFather prompts, bot username, getchatid step; replace with simple "your pair URL is X, scan the QR on your phone" web wizard +phase06 @backend file:internal/setup/wizard.go id:T601 blocked-by:T600
x 2026-06-25 Retain internal/telegram/ as optional chat transport; ensure default setup/up flow does not require Telegram +phase06 @cleanup id:T602 blocked-by:T601
x 2026-06-25 Delete internal/miniappurl/ entirely +phase06 @cleanup id:T603 blocked-by:T602
x 2026-06-25 Delete internal/daemon/webapp.go (Mini App handler obsolete; web UI replaces it) +phase06 @cleanup file:internal/daemon/webapp.go id:T604 blocked-by:T603
x 2026-06-25 Update internal/config/settings.go: remove Telegram struct (currently lines ~64-67 with EncryptedMode + MiniAppURL); add Web struct {ListenAddr string; CertDir string} and Transport struct {Mode string; SAddr string} +phase06 @backend file:internal/config/settings.go id:T605 blocked-by:T604
x 2026-06-25 Update internal/auth/owner.go: remove Telegram owner_id concept; identity is now the session cookie row in web_sessions; ensure existing non-Telegram callers (likely just owner checks) get a thin shim that returns the cookie-bound session +phase06 @backend file:internal/auth/owner.go id:T606 blocked-by:T605
x 2026-06-25 Update internal/secrets/: default web setup no longer requires bot token; keep Telegram bot-token storage for optional chat transport +phase06 @backend file:internal/secrets/ id:T607 blocked-by:T606
x 2026-06-25 Update internal/doctor/: remove Telegram health checks (bot token present, polling reachable); add web-server health (port reachable, cert valid), LAN IP detection, mkcert availability; doctor now warns if no transport is configured +phase06 @backend file:internal/doctor/ id:T608 blocked-by:T607
x 2026-06-25 Rewrite internal/cli/up.go: remove Telegram bot startup branch; up always starts the web server; --transport flag accepts "lan" (default); mints pair token, prints QR, then blocks on signal; open laptop's default browser to the local URL via os/exec call to /usr/bin/open or xdg-open +phase06 @backend file:internal/cli/up.go id:T609 blocked-by:T608
x 2026-06-25 Add internal/cli/pair.go: onibi pair subcommand that mints a fresh pair token, prints QR + URL; useful mid-session for adding a second device +phase06 @backend file:internal/cli/pair.go id:T610 blocked-by:T609
x 2026-06-25 Add internal/cli/unpair.go: onibi unpair [--device-id ID] revokes a session row in web_sessions; with no arg shows interactive picker +phase06 @backend file:internal/cli/unpair.go id:T611 blocked-by:T610
x 2026-06-25 Add internal/cli/devices.go: onibi devices lists all rows of web_sessions with first/last seen timestamps and device-label (set by client User-Agent or by user during pair) +phase06 @backend file:internal/cli/devices.go id:T612 blocked-by:T611
x 2026-06-25 Update cmd/onibi/main.go: register pair, unpair, devices subcommands; drop any Telegram-specific subcommand registration +phase06 @backend file:cmd/onibi/main.go id:T613 blocked-by:T612
x 2026-06-25 Update README.md: replace value-prop line "Telegram-controlled coding-agent host" with web/chat-controlled framing; default Quick Start uses web cockpit while Telegram remains documented as optional chat transport +phase06 @docs file:README.md id:T614 blocked-by:T613
x 2026-06-25 Add CHANGELOG.md entry: "## v0.3.0 — Web cockpit pivot" with sections (Breaking: Telegram-first onboarding removed; Migration: re-pair via web flow; Why: friction; Acknowledgement: this is a third pivot, explicitly breaking the v2 hard rule with user consent); reference original plan +phase06 @docs file:CHANGELOG.md id:T615 blocked-by:T614
x 2026-06-25 Update cmd/gen-readme/* templates if they reference Telegram +phase06 @docs file:cmd/gen-readme/ id:T616 blocked-by:T614
x 2026-06-25 Acceptance check: default `onibi up` and `onibi setup` require no Telegram token; Telegram references are limited to explicit optional chat-transport code/docs; BotFather appears only in Telegram setup docs/help +phase06 @cleanup id:T617 blocked-by:T616 accept:no-default-telegram-requirement
x 2026-06-25 Run go test ./... -race -count=1 — must be green; go vet ./... clean; staticcheck ./... clean +phase06 @tests id:T618 blocked-by:T617 accept:CI-equivalent-green-locally
x 2026-06-25 Run goreleaser snapshot --skip=publish --clean — must build artifacts for all configured targets; record final binary size and confirm >=20% smaller than v2 +phase06 @release id:T619 blocked-by:T618
# 2026-06-25: `goreleaser release --snapshot --clean --skip=publish --skip=sign` succeeds after installing syft; signing skipped because GPG_FINGERPRINT is unset. Size recorded: pre-excision Go baseline 568fa69 darwin-arm64 stripped binary is 16,295,186 bytes; current snapshot darwin-arm64 is 15,365,858 bytes (5.70% smaller). User approved relaxing the >=20% gate to "snapshot builds + size recorded".
x 2026-06-25 Update Makefile: remove any Telegram-specific targets (e.g., telegram-probe); ensure make test includes the new frontend npm test command if added +phase06 @build file:Makefile id:T620 blocked-by:T619
# 2026-06-25: no frontend test script exists; Makefile release dry/smoke targets updated for GoReleaser v2 syntax and local unsigned snapshots.

### Phase 07 — Mobile polish + soft keyboard + PWA (1.0 week)

> Goal: make the phone UX driveable for 5 minutes without rage-quitting. Soft key bar, paste shim, reconnect toast, PWA manifest.

x 2026-06-25 Write frontend/src/softkeys.ts: SoftKeyBar component rendering a bottom-fixed bar of buttons [Esc, Tab, Ctrl, Alt, ↑, ↓, ←, →, ^C, ^D, ^Z] +phase07 @frontend file:frontend/src/softkeys.ts id:T700 blocked-by:T415
x 2026-06-25 Soft-key state machine: Ctrl and Alt are sticky-toggle modifiers (one-shot, deactivate after next key); other keys are direct send; on tap, simulate term.write of the corresponding control sequence to PTY +phase07 @frontend file:frontend/src/softkeys.ts id:T701 blocked-by:T700
x 2026-06-25 Long-press repeat for arrow keys: 500ms long-press starts repeating at 100ms intervals until pointerup +phase07 @frontend file:frontend/src/softkeys.ts id:T702 blocked-by:T701
x 2026-06-25 Paste shim: hidden Paste button reads navigator.clipboard.readText() (requires user gesture) and sends text to PTY; document iOS Safari clipboard prompt expectation +phase07 @frontend file:frontend/src/softkeys.ts id:T703 blocked-by:T702
x 2026-06-25 Reconnect toast: when WS reconnect triggers, show a non-blocking toast "Reconnecting…" that fades on successful resume +phase07 @frontend file:frontend/src/ws.ts id:T704 blocked-by:T703
x 2026-06-25 PWA manifest: add frontend/manifest.webmanifest with name "Onibi", short_name "Onibi", icons (192, 512), start_url "/", display "standalone", theme_color "#000000", background_color "#000000"; link from index.html +phase07 @frontend file:frontend/manifest.webmanifest id:T705 blocked-by:T704
x 2026-06-25 Generate PWA icons at 192 and 512: re-use existing asset/logo/onibi.png cropped to square; place in frontend/public/icons/ +phase07 @frontend file:frontend/public/icons/ id:T706 blocked-by:T705
x 2026-06-25 Service worker (asset cache only, NO push): frontend/src/sw.ts registers via main.ts; caches /assets/* with stale-while-revalidate; never caches root index.html or /ws/* or /api/* +phase07 @frontend file:frontend/src/sw.ts id:T707 blocked-by:T706
x 2026-06-25 iOS viewport pinning: in main.ts attach visualViewport listener; on keyboard show/hide, scroll the terminal to keep cursor visible +phase07 @frontend file:frontend/src/main.ts id:T708 blocked-by:T707 ref:arch-rule-2
x 2026-06-25 Confirm 100dvh sizing applied to html and body in index.html stylesheet; verify no 100vh remains anywhere in frontend/ +phase07 @frontend file:frontend/index.html id:T709 blocked-by:T708
(B) 2026-06-23 Real-iPhone session test: drive vim (open, edit, save, quit) + claude (send prompt, wait, approve) + tmux (switch window) for 5 minutes via soft-key bar; tap `MAC`, confirm same cwd/history/running process opens in macOS terminal; tap `PHONE`, confirm same state returns to Safari; confirm no visible `ONIBI-RESIZE:*` marker +phase07 @tests id:T710 blocked-by:T709 accept:session-completed-without-bug
x 2026-06-26 Add to Home Screen flow de-scoped by user decision; standalone iOS PWA mode is not a release gate +phase07 @tests id:T711 blocked-by:T710
x 2026-06-25 Dark/light mode toggle in soft-key bar (xterm.js theme swap) — optional +phase07 @frontend id:T712 blocked-by:T710
# 2026-06-25: T712 completed early by user request; T710/T711 remain the real-iPhone validation gates.
x 2026-06-26 Local handover regression coverage: managed tmux metadata persists without a permanent web PTY host; `/handover` requires owner cookie; Mac handover closes web attach host; hide headless/end closes web attach host and end marks session ended +phase07 @tests id:T713 blocked-by:T709 accept:handover-regressions-covered

### Phase 08 — Tailscale Funnel transport (1.0 week, v1.1)

> Goal: add `--transport=tailscale` so phone can connect over cellular without LAN.

# 2026-06-28: Tailscale Funnel enabled via login.tailscale.com/f/funnel; public /healthz returned 200 through `*.ts.net`; pair/session traffic reached local loopback through Funnel.
x 2026-06-28 No handler-side code changes needed (Tailscale terminates TLS at the node; requests arrive on loopback :8443 just like LAN); verify by hitting funnel URL from a different machine +phase08 @tests id:T806 blocked-by:T805
# 2026-06-28: iPhone over 5G paired at `*.ts.net`, loaded terminal, accepted PTY/events WS, typed into tmux-backed session, INT returned 200, MAC handover returned 200, PHONE handover returned 200 and PTY reattached.
x 2026-06-28 E2E test on a tailnet-joined Mac with `onibi up --transport=tailscale`: open the funnel URL from an iPhone on LTE (Wi-Fi disabled) — confirm live terminal + input works and managed Mac <-> phone handover preserves session state +phase08 @tests id:T809 blocked-by:T808 accept:cellular-phone-can-drive-terminal

### Phase 09 — Release prep (0.5 week)

> Goal: tag v0.3.0, ship.

(A) 2026-06-23 Final test pass: go test ./... -race -count=1 + go vet + staticcheck + frontend tests (npm test if added) + manual smoke on real iPhone for both LAN and Tailscale +phase09 @tests id:T901
(A) 2026-06-23 goreleaser release --skip=publish --clean dry run; confirm artifacts for darwin-amd64, darwin-arm64, linux-amd64, linux-arm64 +phase09 @release id:T902 blocked-by:T901
(B) 2026-06-23 Update gongahkia/homebrew-onibi tap manifest: new version + sha256s of darwin artifacts +phase09 @release id:T903 blocked-by:T902
(B) 2026-06-23 Developer-ID sign + notarize darwin builds (Gatekeeper requirement); upload notarization tickets +phase09 @release id:T904 blocked-by:T903
(B) 2026-06-23 Tag v0.3.0 in git; push tag; goreleaser release on tag to publish to GitHub releases +phase09 @release id:T905 blocked-by:T904
(C) 2026-06-23 Write a launch announcement (HN-style) — optional, post-tag +phase09 @docs id:T906 blocked-by:T905
x 2026-06-29 Verify direct release-archive installs with SHA256 checksums; fetch checksums.txt, verify selected archive before install, and document GPG verification path when public key is configured +phase09 @release id:T907 accept:archive-install-verifies-checksum
x 2026-06-29 Use Homebrew cask state as source of truth for cask installs: detect pinned/outdated/tap-lag states via brew JSON output and print distinct remediation +phase09 @release id:T908 accept:cask-update-state-is-accurate
x 2026-06-29 Add conditional GitHub release checks with ETag/Last-Modified cache, X-GitHub-Api-Version, and optional token env; 304 must reuse cached latest release metadata +phase09 @backend id:T909 accept:update-check-uses-conditional-requests
x 2026-06-29 Add release artifact contract test after GoReleaser snapshot: parse dist metadata, assert archive names match update-check URLs, and assert onibi + onibi-notify are present +phase09 @tests id:T910 accept:update-command-matches-real-artifact
x 2026-06-29 Require typed confirmation for uninstall --state unless --yes is set; prompt text must require an exact destructive phrase and dry-run remains non-interactive +phase09 @backend id:T911 accept:state-delete-has-typed-confirm

### Phase 10 — Mobile web safety + comfort polish (post-v0.3.0, 0.5 week)

> Origin: external audit of mobile cockpit (2026-06-28). Accepted four gaps: KILL needs confirm, INT/KILL silent on failure, no font-size control, portrait cols cramped. Skipped (per user): discoverability tooltips, connection-health badge, post-pair checklist, in-app transport hint. Reuse map: showToast at frontend/src/main.ts:190-193, high-risk double-tap pattern at frontend/src/approval.ts:72-82, postHandover JSON-error parse at frontend/src/main.ts:149-159, xterm.js term.options.fontSize, FitAddon already wired in frontend/src/terminal.ts:94-120.

(A) 2026-06-28 Add KILL double-tap gate: first tap shows toast "Tap KILL again to send SIGKILL." and arms a 2s window; second tap inside window calls postControl(session,"kill"); INT remains single-tap; mirror frontend/src/approval.ts:72-82 pattern +phase10 @frontend file:frontend/src/main.ts id:T1000 blocked-by:T905
(A) 2026-06-28 Surface /control failures as toasts: convert postControl from void fetch to async; on !response.ok parse {message} like postHandover and showToast(message); on network error showToast("Control failed.") +phase10 @frontend file:frontend/src/main.ts id:T1001 blocked-by:T1000
(A) 2026-06-28 Return structured JSON error from /control handler ({"message": err.Error()}, Content-Type application/json, status 500) matching /handover shape +phase10 @backend file:internal/web/control.go id:T1002 blocked-by:T1001
(B) 2026-06-28 Add A−/A+ font-size buttons to soft-key bar; clamp 10..22; call term.options.fontSize + fit.fit(); persist to localStorage["onibi-font-size"] +phase10 @frontend file:frontend/src/softkeys.ts id:T1010 blocked-by:T1002
(B) 2026-06-28 Read localStorage["onibi-font-size"] on terminal init; default 14 if absent; log one-time [ONIBI] portrait cols=<n> when portrait and cols<40 for future tuning +phase10 @frontend file:frontend/src/terminal.ts id:T1011 blocked-by:T1010
(B) 2026-06-28 Real-iPhone smoke: (1) KILL requires two taps within 2s; (2) stop daemon then tap INT — toast surfaces server error; (3) A−/A+ adjusts font and persists across reload +phase10 @tests id:T1012 blocked-by:T1011 accept:safety-and-comfort-validated-on-device

---

## v0.4 — Locked added-scope (2026-06-29)

> 20 directions locked across 6 clarification rounds with the user. Sequence: Q0 (foundations) → Q1 (approval UX) → Q2 (session model) → Q3 (cockpit visual) → Q4 (reach/distinction/hygiene) → Q5 (distribution, LAST per user directive). Each direction is self-contained; phases gate next. Distribution comes last so feature surface stabilizes before the brew/curl install path is published. Total raw effort estimate: ~25 weeks solo (calendar likely 8–12 months with real-device test cycles).
>
> Token IDs T2000–T2599 reserved for v0.4 to avoid collision with existing T1xxx. Where v0.4 directions supersede existing "Future tasks" entries (T1300–T1303 by E; T1500–T1504 by B), mark the old IDs `x 2026-06-29 superseded by T2…` when starting their replacement.
>
> ### Reuse map additions (do not rewrite — extend only)
> - `internal/envelope/envelope.go` — HKDF-SHA256 + AES-256-GCM. Reused by AC (encrypted state) and E (CF+E2E).
> - `internal/secrets/` — OS keychain interface. Reused by AC for master key storage.
> - `internal/approval/risk.go` — risk heuristics. Extended by Z (anomaly).
> - `internal/approval/scrub.go` — pattern-based redaction. Extended by Z rule 4 + reused by G diff redaction.
> - `internal/setup/pairing.go` — pair-token model. Extended by D (viewer role field).
> - `internal/mcpserver/server.go` — MCP server scaffold. Extended by Y.
> - `internal/updatecheck/` — release version-check infra. Extended by AB.
> - `frontend/src/e2e.ts` — placeholder. Extended by E.
> - `frontend/src/sw.ts` — service worker. Extended by AD (push event handler).
> - `frontend/src/approval.ts` — approval overlay. Extended by G (diff render).
>
> ### Real-device test budget per phase
> Q0: laptop + iPhone + fresh CF account. Q1: iPhone. Q2: iPhone + 3-session laptop. Q3: iPhone (lock-screen push, Add-to-Home-Screen). Q4: iPhone + Raspberry Pi 5 4GB + remote dev box. Q5: fresh macOS VM + fresh Ubuntu VM.

### Phase Q0 — Foundations (~5 weeks)

> Goal: rewrites at the bottom of the stack that everything downstream depends on. Land first; everything else rides them.

#### Q0a — AC: Encrypted SQLite state at rest

> Research locked: no pure-Go SQLCipher exists in 2026. CGO bindings (mutecomm/go-sqlcipher) break static cross-compile to RPi under `CGO_ENABLED=0`. Use app-layer envelope encryption via existing `internal/envelope/envelope.go` (HKDF-SHA256 → AES-256-GCM, 96-bit nonce). Master key (32 random bytes) stored via `github.com/zalando/go-keyring` (already wrapped in `internal/secrets/`). Trade-off: encrypted columns lose SQL `LIKE`/`WHERE` — acceptable because target tables (`pairing_tokens`, `web_sessions`, `audit_events`, plus new `snapshots`, `transcript_turns`, `trust_policies`, `budgets`, `workspaces`, `push_subscriptions`) are append-only or whole-row-read.


#### Q0b — E: Cloudflare Quick Tunnel + E2E (supersedes T1300–T1303)

> Research locked from sshx source (`crates/sshx/src/encrypt.rs`, `src/lib/encrypt.ts`). Onibi diverges from sshx where it helps: AES-256-GCM (AEAD) over sshx's AES-128-CTR — Onibi sends discrete approval messages, needs per-message integrity not just confidentiality. HKDF-SHA256 over Argon2id — Onibi's key is a 256-bit random in URL fragment, not memorizable, so brute-force resistance is by length not KDF cost. Canonical fields: `enc_v` (u8), `iv` (12 random bytes), `ct`, `aad = session_id || direction || seq_u64_be`. Server commitment: `key_verifier = HKDF(K, salt=session_id, info="onibi-verifier-v1", L=32)`. WebCrypto identifiers (interop with Go `crypto/cipher.NewGCM`): `importKey("raw", k, {name:"HKDF"}, false, ["deriveBits","deriveKey"])` → `deriveKey({name:"HKDF", hash:"SHA-256", salt, info}, base, {name:"AES-GCM", length:256}, false, ["encrypt","decrypt"])`. IV MUST be 12 bytes. Cloudflare Quick Tunnel idle timeout: 100s — mitigate with existing app-level WS ping at 30s (TODO §3 rule 10).

(B) 2026-06-29 Real-iPhone E2E smoke: onibi up --transport=cloudflare-quick; iPhone on LTE scans QR with fragment; drives PTY; tcpdump on a third box capturing the trycloudflare.com side shows only ciphertext (no vim/shell strings visible) +phaseQ0 @tests id:T2031 accept:tcpdump-shows-no-plaintext

#### Q0c — F: Plugin / adapter SDK + SPEC.md

### Phase Q1 — Approval UX & policy (~3 weeks)

> Goal: kill approval fatigue with real diffs (G), declarative policies (L), budget enforcement (M), and rules-only anomaly detection (Z). All extend existing internal/approval/.

#### Q1a — G: Diff-aware approval cards

> Research locked: diff2html v3.4.56 (https://github.com/rtfpessoa/diff2html) — pure JS, no React, ~25KB gz core; pair with existing scrub.go for redaction.

(B) 2026-06-29 Real-iPhone smoke: trigger Claude Edit on a 50-line file containing a fake AWS key; approval card shows diff; the key reads `[REDACTED]` in both old and new sides; Approve writes the new file; Deny does not +phaseQ1 @tests id:T2105 accept:diff-card-passes-on-iphone

#### Q1b — L: Trust policies + approval batching

#### Q1c — M: Cost / token budget enforcement


#### Q1d — Z: Heuristic anomaly detection

> Research locked: rules-only, no ML. Survey: llm-guard (protectai), garak (leondz), Nemo Guardrails. 8 rules selected.

### Phase Q2 — Session & state model (~5.5 weeks)

#### Q2a — I: Session snapshots + branching

#### Q2b — C: Multi-session dashboard + recordings + cost

> Research locked: asciinema cast v2 (https://docs.asciinema.org/manual/asciicast/v2/). File = JSON header line + JSONL events. Header `{"version":2,"width":<cols>,"height":<rows>,"timestamp":<unix>,"title":<session_id>}`. Event `[<seconds-float>, "<code>", "<data>"]` where code = "o" (stdout), "i" (stdin), "r" (resize "COLSxROWS"), "m" (marker). Time relative to recording start, monotonic non-decreasing. Player: https://github.com/asciinema/asciinema-player.

#### Q2c — K: Agent reasoning timeline panel

#### Q2d — AF: Workspaces

> Research locked: hybrid model — .onibi/workspace.toml per project (committed, shareable defaults) + ~/.onibi/workspaces/<name>.toml (private index, never committed). Inspired by VS Code .code-workspace (https://code.visualstudio.com/docs/editor/workspaces) + asdf .tool-versions.

### Phase Q3 — Cockpit visual + sharing (~5.5 weeks)

#### Q3a — B: Ghostty depth (supersedes T1500–T1504)

> Research locked. terminfo source: `src/terminfo/ghostty.zig` in https://github.com/ghostty-org/ghostty. Install path: `~/.terminfo/x/xterm-ghostty` (ncurses hashed-DB convention, lowercase first char of terminfo name = `x`). Caps beyond xterm-256color: `Tc` (24-bit truecolor / tmux), `Su` (colored underlines), `setrgbf`/`setrgbb` (direct RGB), Kitty keyboard protocol via `fullkbd` + extended modifier keys, `OSC 8` hyperlinks. Kitty graphics NOT advertised via terminfo (detected via `XTGETTCAP`). xterm.js inline images: `@xterm/addon-image` v0.9.0 (https://www.npmjs.com/package/@xterm/addon-image) supports Sixel + iTerm2 IIP; NO Kitty graphics. Workaround: server-side transcode Kitty `\x1b_G…\x1b\\` → IIP `\x1b]1337;File=…\x1b\\`. xterm.js core: `@xterm/xterm` v6.0.0. Themes from upstream: Ghostty defaults in `src/config/Config.zig`; Catppuccin Mocha (https://github.com/catppuccin/ghostty); Tokyo Night (https://github.com/folke/tokyonight.nvim/tree/main/extras/ghostty); Solarized Dark (`ghostty/src/config/themes`). Font: JetBrains Mono v2.304 (OFL, https://github.com/JetBrains/JetBrainsMono); alternatives Iosevka (https://github.com/be5invis/Iosevka), Maple Mono (https://github.com/subframe7536/maple-font). Berkeley Mono is commercial (https://usgraphics.com/typefaces/berkeley-mono/) — skip.

#### Q3b — U: File tree + inline view/edit

> Research locked: Shiki v4.3.0 (https://shiki.style) — TextMate grammars, matches VS Code output, ~100KB gz core, lazy per-language loads. Reuses existing terminal theme so file view and terminal share a palette.

#### Q3c — AD: Web Push (iOS 16.4+ PWA)

> Research locked. Hard prereqs (any miss = silent failure): HTTPS end-to-end (no HTTP→HTTPS redirect on the SW path), manifest.webmanifest with display:"standalone", Add-to-Home-Screen required (PushManager is undefined in Safari tab), Notification.requestPermission() inside a user-tap handler. Library: github.com/SherClockHolmes/webpush-go (VAPID + RFC 8291 aes128gcm). iOS-only gotchas: NO silent push — every push event MUST showNotification() or Apple revokes the subscription; NO background sync; SW unreliable post-reboot — refresh subscription on app open. EU DMA users: standalone disabled → zero push support; acknowledge in docs.

(B) 2026-06-29 Real-iPhone test: install to Home Screen, lock screen, trigger Claude Write from laptop, expect notification on lock screen, tap → opens session with approval card pre-rendered (deep-link via ?approval= query) +phaseQ3 @tests id:T2346 blocked-by:T2345 accept:lock-screen-notification-arrives

#### Q3d — D: Read-only viewer sharing


### Phase Q4 — Reach, distinction, hygiene (~4 weeks)

#### Q4a — AE: SSH transport (incl. Raspberry Pi 5 4GB target)

> Research locked. Pi 5 = ARMv8 64-bit, canonical GOOS=linux GOARCH=arm64; 32-bit fallback GOARCH=arm GOARM=7 (Pi 2/3/4/Zero2). Libraries: golang.org/x/crypto/ssh + github.com/pkg/sftp + knownhosts. 6-step bootstrap: Dial → uname-detect arch → pick artifact → SFTP push → atomic mv → install user-systemd unit. SQLite under CGO_ENABLED=0 requires modernc.org/sqlite (verify; if mattn/go-sqlite3 in use, migrate). Resource ceiling: ≤80MB RSS idle on RPi 5 4GB.

(A) 2026-06-29 internal/transport/ssh/install.go: SFTP-push the matching onibi + onibi-notify binaries to /tmp/onibi.<random>.<arch>; chmod 0755; atomic mv to ~/.local/bin/; source-of-truth path on local: bin/ or ~/.local/share/onibi/release/<version>/<os>-<arch>/ +phaseQ4 @backend file:internal/transport/ssh/install.go id:T2404 blocked-by:T2403 accept:binary-installed-and-executable
(A) 2026-06-29 internal/transport/ssh/service.go: install user-mode systemd unit at ~/.config/systemd/user/onibi.service with ExecStart=%h/.local/bin/onibi up --transport=lan-loopback --no-qr (the SSH tunnel handles ingress); systemctl --user enable --now onibi; macOS targets get launchd at ~/Library/LaunchAgents/io.onibi.plist +phaseQ4 @backend file:internal/transport/ssh/service.go id:T2405 blocked-by:T2404 accept:service-survives-ssh-disconnect
(A) 2026-06-29 internal/transport/ssh/tunnel.go: open a local-side listener via ssh.Client.Dial+Listen patterns; tunnel localhost:<random> ↔ remote :8443; QR encodes the LOCAL forwarded port URL so the phone connects via the laptop+ssh+remote daemon chain +phaseQ4 @backend file:internal/transport/ssh/tunnel.go id:T2406 blocked-by:T2405 accept:phone-drives-remote-daemon
(A) 2026-06-29 internal/cli/ssh.go: onibi up --ssh user@host orchestrates T2402–T2406; onibi ssh status; onibi ssh teardown stops the remote service and removes the binary +phaseQ4 @backend file:internal/cli/ssh.go id:T2407 blocked-by:T2406 accept:CLI-flag-orchestrates-bootstrap
(B) 2026-06-29 Resource ceiling discipline: linux/arm64 stripped onibi binary must be ≤14MB; idle RSS on RPi 5 must be ≤80MB measured via `ps -o rss -p <pid>`; gate in scripts/rpi-smoke.sh +phaseQ4 @tests file:scripts/rpi-smoke.sh id:T2408 blocked-by:T2407 accept:rss-under-budget
(B) 2026-06-29 Real Raspberry Pi 5 4GB test: physical Pi on user's LAN; onibi up --ssh pi@raspberrypi.local; phone pairs over the local tunnel; drive vim for 2 min on the Pi; verify RSS stays under 80MB; SIGINT tears down cleanly with no orphaned tmux sessions on the Pi +phaseQ4 @tests id:T2409 blocked-by:T2408 accept:rpi-smoke-passes
(B) 2026-06-29 docs/ssh-transport.md: bootstrap flow diagram, known-hosts policy, why user-systemd over root, RPi memory tips (swap, zram), teardown semantics +phaseQ4 @docs file:docs/ssh-transport.md id:T2410 blocked-by:T2409

#### Q4b — Y: Onibi-as-MCP server depth

> Research locked. MCP spec rev 2025-11-25 (https://modelcontextprotocol.io/specification). Go SDK: github.com/mark3labs/mcp-go (community, mature) — recommended; github.com/modelcontextprotocol/go-sdk is newer official. Primitives: tools, resources, prompts. Onibi exposes 6 tools + 1 resource so any MCP client (Claude itself, Cursor, Continue) can list/inspect/kill onibi sessions, query approvals, fetch transcripts.

(A) 2026-06-29 Audit internal/mcpserver/server.go SDK choice; if not on mark3labs/mcp-go at a stable tag, migrate; pin version in go.mod +phaseQ4 @backend file:internal/mcpserver/server.go id:T2430 blocked-by:T2410 accept:sdk-pinned
(A) 2026-06-29 Implement tool onibi_list_sessions: input schema {include_remote: bool default false}; output [{id, agent, cwd, started_at, last_activity, pending_approvals_count, tokens_used, cost_usd, role_required, workspace}] +phaseQ4 @backend file:internal/mcpserver/tools.go id:T2431 blocked-by:T2430 accept:claude-mcp-call-returns-sessions
(A) 2026-06-29 Implement tool onibi_kill_session: input {session_id, force: bool default false}; calls daemon kill path; returns {killed: bool, signal: string}; refuses if the local socket user is not the same UID +phaseQ4 @backend file:internal/mcpserver/tools.go id:T2432 blocked-by:T2431 accept:kill-works-via-mcp
(A) 2026-06-29 Implement tool onibi_fetch_transcript: input {session_id, since_turn: int default 0, max_turns: int default 50}; output array of turn records (role, content, tool_calls) scrubbed via internal/approval/scrub.go +phaseQ4 @backend file:internal/mcpserver/tools.go id:T2433 blocked-by:T2432 accept:transcript-returned
(A) 2026-06-29 Implement tool onibi_list_pending_approvals: input {}; output pending approvals across all sessions (id, session_id, tool, args_scrubbed, risk_level) +phaseQ4 @backend file:internal/mcpserver/tools.go id:T2434 blocked-by:T2433 accept:list-matches-web-inbox
(A) 2026-06-29 Implement tool onibi_decide_approval: input {approval_id, verdict: "approve"|"deny"|"edit", edited_args?: string}; routes through internal/approval/queue.go Decide; REFUSES High-risk decisions (forces human via SPA — agent must not auto-approve its own dangerous actions) +phaseQ4 @backend file:internal/mcpserver/tools.go id:T2435 blocked-by:T2434 accept:high-risk-refused-via-mcp
(B) 2026-06-29 Implement tool onibi_tail_logs: input {session_id, lines: int default 200}; output recent audit + PTY tail (scrubbed) +phaseQ4 @backend file:internal/mcpserver/tools.go id:T2436 blocked-by:T2435 accept:logs-returned
(B) 2026-06-29 Expose resource onibi://sessions/{id}/transcript for streamed read access from MCP clients +phaseQ4 @backend file:internal/mcpserver/resources.go id:T2437 blocked-by:T2436 accept:resource-reads-stream
(B) 2026-06-29 Update docs/mcp.md: install (add `onibi` server to ~/.claude/mcp.json pointing to `onibi mcp`), example calls, tool schemas, refusal policy on high-risk, sample agent-controls-agent script +phaseQ4 @docs file:docs/mcp.md id:T2438 blocked-by:T2437

#### Q4c — X: Guided demo + first-run tour

(A) 2026-06-29 Rewrite internal/cli/demo.go: spawns a self-contained 90s scripted demo session (managed bash running `echo`, sleep, `vim ... :q`, fake approval card injected via direct call to approval.Queue, simulated handover); zero external dependencies — Claude binary NOT required +phaseQ4 @backend file:internal/cli/demo.go id:T2450 blocked-by:T2438 accept:demo-runs-without-claude
(A) 2026-06-29 frontend/src/tour.ts SPA first-run tour: gated on localStorage onibi-tour-done absent; step-through tooltips for 8 steps — pair flow → terminal mount → MAC/PHONE handover → approval card → soft-key bar → trust/policy hint → files panel hint → done; takes ≤60s; skip button always visible +phaseQ4 @frontend file:frontend/src/tour.ts id:T2451 blocked-by:T2450 accept:tour-completes-in-60s
(B) 2026-06-29 Demo screencast: record `onibi demo` end-to-end via `asciinema rec`, save to docs/demo.cast (committed); embed asciinema-player web component in docs/index.html landing +phaseQ4 @docs file:docs/demo.cast id:T2452 blocked-by:T2451 accept:cast-plays-on-landing-page

#### Q4d — AB: In-place self-update

> Research locked. Library: github.com/minio/selfupdate (active 2026). github.com/inconshreveable/go-update is archived 2026-04-25. Flow: pinned-TLS manifest fetch → SHA-256 + GPG-signature verify → selfupdate.Apply (atomic rename, auto-rollback on failure) → re-exec via syscall.Exec. macOS: replacement binary must be Developer-ID-signed + notarized with stapler ticket embedded (existing GoReleaser notarize config handles this).

(A) 2026-06-29 internal/cli/update.go: `onibi update [--channel=stable|beta] [--check-only]`; stable URL https://api.github.com/repos/gongahkia/onibi/releases/latest; beta lists `…/releases?per_page=1` and picks the latest pre-release +phaseQ4 @backend file:internal/cli/update.go id:T2460 blocked-by:T2452 accept:check-only-prints-latest
(A) 2026-06-29 Verify chain: download checksums.txt + checksums.txt.sig + selected archive; verify signature via embedded GPG public key (build-time constant from .goreleaser GPG fingerprint); verify SHA-256 of archive; extract binary; on macOS additionally run `codesign --verify` on extracted binary; only then call selfupdate.Apply(binary, selfupdate.Options{}) +phaseQ4 @backend file:internal/cli/update.go id:T2461 blocked-by:T2460 accept:checksum-mismatch-aborts
(A) 2026-06-29 Re-exec post-update via syscall.Exec(newPath, os.Args, os.Environ()) on Linux/macOS; on Windows defer to next start (out of v0.4 scope); if user-mode service is installed, restart it via systemctl --user restart onibi (or launchctl kickstart) +phaseQ4 @backend file:internal/cli/update.go id:T2462 blocked-by:T2461 accept:new-binary-takes-effect
(B) 2026-06-29 Rollback path: previous binary retained at ~/.local/share/onibi/onibi.prev (selfupdate already does atomic-rename); `onibi update --rollback` swaps it back +phaseQ4 @backend file:internal/cli/update.go id:T2463 blocked-by:T2462 accept:rollback-works
(B) 2026-06-29 Auto-update opt-in: [update] auto=true channel="stable" in config; daemon checks at startup + every 24h; default OFF (per OSS positioning — no surprise upgrades) +phaseQ4 @backend file:internal/updatecheck/ id:T2464 blocked-by:T2463 accept:opt-in-only

### Phase Q5 — Distribution polish (~1.5 weeks, LAST per user)

> Goal: Mole-parity install. Bar = "brew install + run + done" on a fresh macOS box. Comes last so the feature surface is frozen before brew/curl/scripts publish.

(A) 2026-06-29 Verify the existing brew cask tap (gongahkia/homebrew-onibi already declared in .goreleaser.yaml) is live and current; trigger a snapshot release via GoReleaser; `brew install gongahkia/onibi/onibi` on a fresh macOS VM must work end-to-end including hooks install + first onibi up +phaseQ5 @release file:.goreleaser.yaml id:T2500 blocked-by:T2464 accept:fresh-macos-install-works
(A) 2026-06-29 Write scripts/install.sh (curl-pipe-sh installer, sshx pattern): detect uname -sm; fetch matching artifact from latest GitHub release; verify checksums.txt + checksums.txt.sig with embedded GPG public key; extract to ~/.local/bin/onibi; ensure PATH; refuse to install if signature verification fails +phaseQ5 @release file:scripts/install.sh id:T2501 blocked-by:T2500 accept:curl-install-verifies-checksum
(A) 2026-06-29 Publish scripts/install.sh at https://get.onibi.sh via a tiny GitHub Pages site (no SaaS); document the redirect mechanism in docs/install.md +phaseQ5 @docs id:T2502 blocked-by:T2501 accept:curl-sSf-get-onibi-sh-resolves
(A) 2026-06-29 Single-binary discipline audit: post-Q0-Q4, fresh `onibi up` on a clean box must require zero further downloads at runtime (no mkcert binary, no Node, no Python); add scripts/no-runtime-deps-check.sh that strace/dtrace-monitors onibi up for 30s and fails if any new binary fetch is observed +phaseQ5 @tests file:scripts/no-runtime-deps-check.sh id:T2503 blocked-by:T2502 accept:CI-fails-on-runtime-download
(A) 2026-06-29 Profile aliases: `onibi profile add work --transport=tailscale --agent=claude --workspace=work-ws` writes a profiles table row (encrypted via Q0a); `onibi profile list|remove|use`; `onibi up work` resolves the profile and applies all flags transparently +phaseQ5 @backend file:internal/cli/profile.go id:T2504 blocked-by:T2503 accept:up-name-recalls-flags
(A) 2026-06-29 `onibi up` with no args picks last-used profile via profiles.last_used_at; if no profiles exist, fall back to the existing interactive picker (category → provider) +phaseQ5 @backend file:internal/cli/up.go id:T2505 blocked-by:T2504 accept:no-arg-up-recalls-last
(A) 2026-06-29 --detach flag on `onibi up`: backgrounds via the existing install-service path; prints PID + log path; logs go to ~/.local/share/onibi/logs/onibi.log +phaseQ5 @backend file:internal/cli/up.go id:T2506 blocked-by:T2505 accept:detach-survives-terminal-close
(B) 2026-06-29 `onibi tunnel <port>` Mole-shaped ad-hoc port-forward: uses the same transport selector (LAN/Tailscale/Cloudflare); QR points to https://<host>:<port> not the cockpit; repositions Onibi as also-a-tunnel +phaseQ5 @backend file:internal/cli/tunnel.go id:T2507 blocked-by:T2506 accept:tunnel-exposes-arbitrary-port
(B) 2026-06-29 README quickstart rewrite: top of file is `brew install gongahkia/onibi/onibi && onibi up` (3 lines + QR); `make build` quickstart moves to CONTRIBUTING.md +phaseQ5 @docs file:README.md id:T2508 blocked-by:T2507 accept:quickstart-is-3-lines
(B) 2026-06-29 Shell completions: `onibi completion bash|zsh|fish` emits completion script (cobra ships this — just expose); installer (T2501) places script into the right path +phaseQ5 @backend file:internal/cli/completion.go id:T2509 blocked-by:T2508 accept:completions-work
(C) 2026-06-29 docs/index.html polish: hero, asciinema demo cast embed (T2452), brew + curl install snippets, link to docs, no SaaS signup form, no tracker scripts +phaseQ5 @docs file:docs/index.html id:T2510 blocked-by:T2509
(B) 2026-06-29 Tag v0.4.0; GoReleaser publishes to GitHub releases + Homebrew cask tap + scripts/install.sh fetches it; verify end-to-end on a fresh box (brew install path + curl install path) +phaseQ5 @release id:T2511 blocked-by:T2510 accept:tagged-release-published
(C) 2026-06-29 v0.4 launch post draft (HN-shaped) — emphasize the Ghostty depth (B), MCP recursion (Y), Cloudflare E2E (E), and Pi 5 transport (AE); optional +phaseQ5 @docs id:T2512 blocked-by:T2511

---

## v0.4 — End markers

> Reaching here with every (A) and (B) task above complete = v0.4 ships. v0.5 candidates declined this round (reconsider after v0.4 telemetry): H sandboxed agent execution (Apple Containerization / Docker / bubblewrap); J WebAuthn passkey for high-risk approvals; W observability surface (/metrics + OTel); AG cross-session full-text search; AK deny-with-reason feedback loop; multi-agent orchestration (side-by-side or dispatcher); AI explicit minimal-footprint build tag (partially absorbed into AE RPi target).

---

## Future tasks (out of v1 / v1.1 scope; documented now so the agent does not start them by mistake)

> v1.2: Cloudflare Named Tunnels (account-bound, stable URL). No E2E required since CF can MITM in theory and account model gives liability surface. ~1 week.

(C) 2026-06-23 [V1.2 — do not start in v1] Implement --transport=cloudflare-named flag using `cloudflared tunnel run` against a user-configured named tunnel; auth via CF account token in keychain +phase12 @backend id:T1200

> v1.3: Cloudflare Quick Tunnel with full E2E crypto. ~3 weeks careful work. Read arch rule #11 before starting.

x 2026-06-29 superseded by T2020-T2031 [V1.3 — do not start until v1.2 stable] Design E2E protocol document: fragment-keyed bootstrap, HKDF over k_pair to derive enc_key + mac_key, AES-256-GCM frame encryption, sequence-numbered nonces, key commitment via HMAC; reuse internal/envelope/envelope.go primitives +phase13 @backend id:T1300 ref:arch-rule-11
x 2026-06-29 superseded by T2020-T2031 [V1.3 — do not start before T1300 approved] Implement client-side: read k from location.hash on page load; derive key via WebCrypto subtle; wrap WS messages in encrypt/decrypt; never log k +phase13 @frontend id:T1301 blocked-by:T1300
x 2026-06-29 superseded by T2020-T2031 [V1.3] Implement server-side encrypted relay: server holds only HMAC(k) commitment; cannot decrypt traffic; relay flag set in /healthz when active +phase13 @backend id:T1302 blocked-by:T1301
x 2026-06-29 superseded by T2020-T2031 [V1.3] Refuse to ship tagged releases with --transport=cloudflare-quick unless E2E flag is on; build-time gate +phase13 @build id:T1303 blocked-by:T1302

> v1.4: Optional Telegram hardening work. Telegram is included now as an opt-in chat transport; future work should improve its setup and live smoke coverage without making it part of default onboarding.

(C) 2026-06-28 [V1.4 — do not start in v1.x; optional] Harden Telegram setup/live smoke; web cockpit stays default primary transport; preserve explicit opt-in via `onibi telegram setup` and `onibi up --transport=telegram` +phase14 @backend id:T1400
x 2026-06-29 Add onibi telegram status --json --check: report token presence, getMe validity, bot username, owner pairing, and actionable setup/disable next steps without making Telegram part of default onboarding +phase14 @backend id:T1401 accept:telegram-status-json-live-check

> v1.5: Ghostty/product distinction. Do not add more providers before deepening Ghostty/tmux session quality; Onibi's distinction is projecting the same local Ghostty/tmux session to phone, chat, and notify surfaces with auditable approvals.

x 2026-06-29 superseded by T2300-T2310; Add Ghostty capability probe: detect app/CLI/AppleScript support, installed version, and supported launch method; surface in doctor/status +phase15 @backend id:T1500 accept:ghostty-capability-visible
x 2026-06-29 superseded by T2300-T2310; Deepen Ghostty handoff: add focus-first AppleScript path that searches existing terminals for the Onibi tmux title marker before creating a new window; fallback remains current fresh open; real no-duplicate behavior remains gated by T1503 +phase15 @backend id:T1501 blocked-by:T1500 accept:focus-path-unit-covered
x 2026-06-29 superseded by T2300-T2310; Add configurable Ghostty launch profiles: pass initial working directory, optional font size, title marker, and environment through AppleScript surface configuration; preserve current terminal.default behavior +phase15 @backend id:T1502 blocked-by:T1501 accept:ghostty-window-matches-session-context
x 2026-06-29 superseded by T2300-T2310; Add Ghostty parity smoke: start web cockpit, tap MAC/PHONE twice, verify same tmux target, cwd, terminal title, and no duplicate Ghostty windows when focus is possible +phase15 @tests id:T1503 blocked-by:T1502 accept:ghostty-handoff-is-stable
x 2026-06-29 superseded by T2300-T2310; Add “Ghostty wherever text renders” product copy and docs only after T1503 passes; claim must be scoped to web cockpit, Ghostty handoff, chat text summaries, notify approvals, and tmux session continuity +phase15 @docs id:T1504 blocked-by:T1503 accept:claim-backed-by-features

---

## End of file

> If you reach here and have implemented every (A) and (B) task above, the v1 + v1.1 web-cockpit pivot is complete. v1.2 and v1.3 are explicitly future work. Do not start them without re-reading section 3 rule 11 and discussing E2E threat model with the user.
