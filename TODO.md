# Onibi v2 — TODO

> **Authoritative work list.** Written 2026-06-14 from a full repo audit. Intended to be picked up by an independent coding agent with no prior context on this codebase. Every ticket is self-contained: motivation, files with line citations, schema, implementation outline, validation, and gotchas.
>
> Replaces the deleted `TODO-10-JUN.md`. Source of truth for what to build next.

---

## Table of Contents

- [0. Project context (read this first)](#0-project-context-read-this-first)
- [1. Architecture map](#1-architecture-map)
- [2. File index (the 60-second tour)](#2-file-index-the-60-second-tour)
- [3. Conventions and house style](#3-conventions-and-house-style)
- [4. Ticket index](#4-ticket-index)
- [5. Sprint 1 — durability and footguns](#5-sprint-1--durability-and-footguns)
  - [T01 Persist pending UI state to SQLite (P0/M)](#t01-persist-pending-ui-state-to-sqlite-p0m)
  - [T03 Edit-in-place approval message on daemon restart (P0/M)](#t03-edit-in-place-approval-message-on-daemon-restart-p0m)
- [6. Sprint 2 — onboarding cliff](#6-sprint-2--onboarding-cliff)
  - [T07 `onibi up` convenience command (P1/M)](#t07-onibi-up-convenience-command-p1m)
- [7. Sprint 3 — Telegram steady-state UX](#7-sprint-3--telegram-steady-state-ux)
- [8. Sprint 4 — engineering hardening](#8-sprint-4--engineering-hardening)
  - [T15 MCP server test coverage (P1/M)](#t15-mcp-server-test-coverage-p1m)
  - [T16 Versioned shell hook blocks + auto-reinstall on `doctor --fix` (P1/M)](#t16-versioned-shell-hook-blocks--auto-reinstall-on-doctor---fix-p1m)
  - [T17 Prune legacy logo assets (P1/S)](#t17-prune-legacy-logo-assets-p1s)
  - [T18 Cross-platform peer-cred test in CI matrix (P2/M)](#t18-cross-platform-peer-cred-test-in-ci-matrix-p2m)
  - [T19 Structured `log --json` (P2/L)](#t19-structured-log---json-p2l)
- [9. Sprint 5 — docs depth](#9-sprint-5--docs-depth)
  - [T20 `docs/getting-started.md` (P0/S)](#t20-docsgetting-startedmd-p0s)
  - [T21 `docs/architecture.md` (P0/S)](#t21-docsarchitecturemd-p0s)
  - [T22 `docs/mcp.md` with client examples (P1/S)](#t22-docsmcpmd-with-client-examples-p1s)
  - [T23 `docs/encrypted-mode.md` (P0/S)](#t23-docsencrypted-modemd-p0s)
  - [T24 Shell-hook conflict troubleshooting (P1/S)](#t24-shell-hook-conflict-troubleshooting-p1s)
  - [T25 Real `docs/index.html` landing (P1/M)](#t25-real-docsindexhtml-landing-p1m)
- [10. Out of scope — do NOT do](#10-out-of-scope--do-not-do)
- [11. Useful commands (cheat sheet)](#11-useful-commands-cheat-sheet)
- [12. Glossary](#12-glossary)

---

## 0. Project context (read this first)

**Onibi v2** is a single static Go binary that runs as a local user daemon, hosts coding agents (Claude Code, Codex, OpenCode, Goose, Gemini, Copilot, Pi, Amp) under PTYs, and routes approvals / turn-complete signals / prompts through a Telegram bot. The user controls everything from a Telegram chat on their phone.

### Hard invariants (philosophy container — DO NOT VIOLATE)

These are *load-bearing* design constraints. If a change you make breaks one of these, stop and ask the maintainer.

- **No inbound listeners.** Outbound HTTPS to `api.telegram.org` only. No webhooks. No tunnels (ngrok/cloudflared). No LAN exposure. The Unix socket at `paths.Socket` is the only listener and only same-UID peers may dial it (`internal/intake/peercred_{darwin,linux}.go`).
- **Single trusted-OS-user.** No multi-tenant. The OS user that owns the daemon is fully trusted.
- **No accounts, no SaaS, no telemetry.** Apache-2.0, self-hostable, CLI-first install.
- **Single binary distribution.** Static Go build, `cgo` only where modernc.org/sqlite avoids it. Distributed via Homebrew cask + raw tarballs.
- **Telegram is the only transport.** No Signal/Discord/Slack/email/SMS adapters. The discipline of one channel is intentional.
- **Encrypted-mode and plaintext-Telegram mode are equal-parity user preferences.** Neither is the long-term default. Both must remain first-class; user picks via `telegram.encrypted_mode={off,ask,on}`.
- **No third pivot in this codebase.** v2 is the final iteration in this repo. If v2 fails, start a new repo.

### Soft conventions

- Owner check on every inbound update (`internal/telegram/router.go` `Dispatch`).
- Approvals have a hard 5-min TTL (`approval.DefaultTTL`); paranoid mode 60s.
- Bot token in OS keystore (macOS Keychain / Linux Secret Service) with `.env` 0600 fallback.
- Pair-once deeplink: `https://t.me/<bot>?start=pair_<token>`, 5-min TTL, single-use.
- Fail-open hook intake (`internal/intake/server.go`): if the daemon is down, the hook exits 0 and the agent proceeds. The user is alerted later by `doctor`.

### Status (as of audit)

- Core daemon, setup, approvals, prompt queue, Telegram controls, service install, doctor, MCP, encrypted Mini App, adapters: implemented.
- 33+ `*_test.go` files. CI: `go vet`, `go test -race`, `staticcheck`, `govulncheck`, GoReleaser snapshot smoke, reproducible-build check (`.github/workflows/ci.yml`).
- **Stable tag is gated only by live-provider e2e + signed-release validation.** UX/doc polish is NOT a release blocker, so sequence freely.

---

## 1. Architecture map

```
+---------------------------+      +-----------------------------+
| Telegram (api.telegram.org)|<---->|  internal/telegram          |
+---------------------------+ HTTPS|  client.go long-poll        |
            ^                      |  router.go owner-check      |
            |                      +--------------+--------------+
            | tg.sendData                          | OnText/OnCB/OnReply
            v                                      v
+---------------------------+      +-----------------------------+
| Mini App (docs/miniapp/)  |      |  internal/daemon            |
|  static HTML/JS,          |      |  daemon.go    lifecycle     |
|  WebApp.SecureStorage     |      |  approvals.go approval RPC  |
+---------------------------+      |  prompts.go   prompt queue  |
                                   |  commands.go  /commands     |
                                   |  threading.go session route |
                                   |  encrypted.go AES-GCM Mini  |
                                   |  output.go    PNG / tail    |
                                   +--------------+--------------+
                                                  | spawn / signal
                                                  v
                                   +-----------------------------+
                                   |  internal/pty               |
                                   |  host.go creack/pty         |
                                   |  ring buffer (buffer.go)    |
                                   +--------------+--------------+
                                                  | child PTY
                                                  v
                                   +-----------------------------+
                                   | agent binary (claude, codex,|
                                   | goose, gemini, copilot, ...)|
                                   +--------------+--------------+
                                                  | hook fires
                                                  v
                                   +-----------------------------+
                                   | clients/onibi-notify        |
                                   | --type … (--wait for RPC)   |
                                   +--------------+--------------+
                                                  | JSON over Unix socket
                                                  v  (peer-UID checked)
                                   +-----------------------------+
                                   |  internal/intake/server.go  |
                                   |  paths.Socket               |
                                   +--------------+--------------+
                                                  |
                                                  v
                                   +-----------------------------+
                                   |  internal/store (SQLite)    |
                                   |  approvals, prompts,        |
                                   |  sessions, audit, kv,       |
                                   |  pairing_tokens             |
                                   +-----------------------------+
```

**MCP path**: `onibi mcp` runs as a separate process (started by the MCP-capable agent). It speaks stdio JSON-RPC outward and connects to the daemon over the same Unix socket as hooks (`internal/mcpserver/server.go` → `internal/intake/client.go`). Peer-UID is the only auth.

---

## 2. File index (the 60-second tour)

| Area | File | What lives here |
|---|---|---|
| Entry | `cmd/onibi/main.go` | Wires `cli.Root()` |
| CLI | `internal/cli/root.go` | All Cobra command registration |
| CLI | `internal/cli/stubs.go` | Cobra command factories (flags, short descriptions) — **not stubs**, badly named |
| CLI | `internal/cli/setup.go` | `onibi setup` + `setup --complete` orchestration |
| CLI | `internal/cli/run.go`, `shell.go` | `onibi run`, `onibi wrap`, `onibi shell` |
| CLI | `internal/cli/hooks.go` | `onibi install-hooks` + `locateNotifyBinary()` |
| CLI | `internal/cli/doctor.go`, `internal/doctor/doctor.go`, `fix.go` | Doctor checks + `--fix` |
| CLI | `internal/cli/config.go` | `onibi config {show,list,get,set,init,validate,path}` |
| CLI | `internal/cli/uninstall.go` | `onibi uninstall [--state]` |
| CLI | `internal/cli/mcp.go`, `internal/mcpserver/server.go` | MCP stdio server |
| Setup | `internal/setup/wizard.go` | Token paste, getMe, pair-once, 2FA gate |
| Setup | `internal/setup/pairing.go`, `qr.go`, `rotate.go` | Pairing token, QR render, rotate flow |
| Daemon | `internal/daemon/daemon.go` | `Daemon` struct, `New`, `Run`, lifecycle |
| Daemon | `internal/daemon/approvals.go` | Approval RPC, callbacks, edit flow, **in-memory `pendingEdits`** |
| Daemon | `internal/daemon/prompts.go` | Prompt queue, dispatch, **in-memory `pendingPromptEdits`** |
| Daemon | `internal/daemon/threading.go` | Default target, **in-memory `pendingInjects`**, target picker |
| Daemon | `internal/daemon/commands.go` | All `/command` handlers + `helpText()` (single source of `/help` truth) |
| Daemon | `internal/daemon/encrypted.go`, `webapp.go` | Encrypted message build + Mini App data ingest |
| Daemon | `internal/daemon/controls.go` | `/peek` `/interrupt` `/kill` `/rename` `/menu` |
| Daemon | `internal/daemon/totp.go` | TOTP gate for destructive commands |
| Daemon | `internal/daemon/anomaly.go` | Sliding-window anomaly warnings |
| Daemon | `internal/daemon/turn_complete.go` | Idle detector fallback |
| PTY | `internal/pty/host.go`, `internal/daemon/buffer.go` | PTY spawn + ring buffer |
| Render | `internal/render/png.go`, `text.go` | VT100 → PNG + text tail |
| Telegram | `internal/telegram/client.go` | go-telegram/bot wrapper, long-poll |
| Telegram | `internal/telegram/router.go` | Owner-checked dispatch |
| Telegram | `internal/telegram/keyboards.go` | Inline keyboards (approval, prompt, target) |
| Intake | `internal/intake/server.go`, `event.go`, `rpc.go` | Unix-socket protocol |
| Intake | `internal/intake/peercred_{darwin,linux}.go` | Per-OS peer-UID check |
| Store | `internal/store/sqlite.go` | DB open, migrations, KV (`KVSet`/`KVGet`/`KVDel` with TTL) |
| Store | `internal/store/prompts.go`, `sessions.go`, `audit.go` | Per-table accessors |
| Adapters | `internal/adapters/registry.go` | Registry of agent adapters |
| Adapters | `internal/adapters/{claude,codex,goose,opencode,gemini,copilot,pi,amp}/` | Per-agent installers |
| Adapters | `internal/adapters/shell/install.go` | zsh/bash/fish hook installers |
| Adapters | `internal/adapters/common/` | Hook hash recording / verification |
| Client | `clients/onibi-notify/main.go` | The hook-invoked client binary |
| Mini App | `docs/miniapp/index.html` | Static decrypt-and-decide WebApp |
| Packaging | `Makefile`, `.goreleaser.yaml`, `packaging/homebrew/onibi.rb.template` | Build + distribution |
| CI | `.github/workflows/{ci,release}.yml` | Tests, lint, vulncheck, snapshot smoke, reproducible build |

### Key facts to keep in your head

- **The KV store already exists.** `internal/store/sqlite.go:198-244` exposes `KVSet(ctx, key, value, expire)` with unix-second TTL, `KVSetString`, `KVGet`, `KVGetString`, `KVDel`. Use this for any new persistence — do **not** add a new table unless schema demands one.
- **Owner check is chokepointed.** `internal/telegram/router.go` `Dispatch` is the single place owner ID is validated. Don't add a second dispatch path.
- **All audit-worthy events should call `d.audit(ctx, action, sessionID, payload, chatID, detail)`** — `internal/daemon/audit.go`.
- **`/help` text is the single source of truth for Telegram commands** — `internal/daemon/commands.go:191-218` `helpText()`. README command docs are generated from this.
- **Adapter hook hashes** are stored at install time via `common.Record()` and verified by `common.VerifyRecorded()`. The doctor `checkHooks()` iterates these.

---

## 3. Conventions and house style

(From `CLAUDE.md` at repo root + `~/.claude/CLAUDE.md`. Apply to all changes.)

- **Extreme terseness** in user-facing strings. No apologies. No "I'm sorry."
- **Fail fast.** No catch-and-swallow. No silent retries unless you can name the recovery semantics.
- **No refactor creep.** Stick to the diff of the ticket. Don't reformat surrounding code.
- **Comments**: in-line only, lowercase by default. Capitalize technology names (`// use Docker`). No multi-paragraph docstrings. Comment the *why*, not the *what*.
- **Spacing**: minimize whitespace. Maximize vertical density.
- **Truth labels** (in PR descriptions and commits, not in code):
  - `[Inference]` when concluding from indirect evidence.
  - `[Speculation]` when guessing.
  - `[Unverified]` when source is unconfirmed.
  - Reserve plain prose for things you read directly in source.
- **Avoid trigger words** in claims: prevent, guarantee, will never, fixes, eliminates, ensures — unless directly verifiable, mark `[Inference]`.
- **CLI-first**: prefer `make`/`go`/scripts over IDE workflows.
- **Discovery order for new contributors**: `package.json`/`go.mod` → `Makefile` → `*.sh` → README.

### Test conventions

- `go test -race -count=1 ./...` must pass.
- `staticcheck ./...` must pass (CI runs it).
- `govulncheck ./...` must pass.
- Race-condition territory (anything touching `daemon.threadMu`, `daemon.editMu`, `daemon.mu`) — write a `t.Run` parallel subtest if behavior is concurrent.
- Telegram tests use `internal/telegram/mock.go`. Prefer that to a real bot.

### Commit messages

Inspect `git log --oneline` for the project's style — typically `feat(app): <thing>`, terse. Don't introduce a new style.

---

## 4. Ticket index

Sprints are independent; tickets within a sprint are roughly ordered by dependency. P0 = block stable tag's UX baseline. P1 = same-quarter. P2 = nice-to-have.

| # | Title | Priority | Effort | Depends on |
|---|---|---|---|---|
| T01 | Persist pending UI state to SQLite | P0 | M | — |
| T03 | Edit-in-place approval message on daemon restart | P0 | M | T01 (optional) |
| T07 | `onibi up` convenience command | P1 | M | — |
| T15 | MCP server test coverage | P1 | M | — |
| T16 | Versioned shell hook blocks + auto-reinstall on `doctor --fix` | P1 | M | — |
| T17 | Prune legacy logo assets | P1 | S | — |
| T18 | Cross-platform peer-cred test in CI matrix | P2 | M | — |
| T19 | Structured `log --json` | P2 | L | — |
| T20 | `docs/getting-started.md` | P0 | S | — |
| T21 | `docs/architecture.md` | P0 | S | — |
| T22 | `docs/mcp.md` with client examples | P1 | S | T15 |
| T23 | `docs/encrypted-mode.md` | P0 | S | — |
| T24 | Shell-hook conflict troubleshooting | P1 | S | — |
| T25 | Real `docs/index.html` landing | P1 | M | T20, T21 |

---

## 5. Sprint 1 — durability and footguns

### T01 Persist pending UI state to SQLite (P0/M)

**Confirmed bug** (per maintainer). Three in-memory maps in `internal/daemon/daemon.go:73-77` are lost on daemon restart, breaking owner-side UX mid-flow.

#### Motivation

The owner has three "parked" half-completed actions that live only in memory:

1. **Pending approval edit** — owner tapped `[Edit]` on an approval keyboard. Next text reply will be parsed as edited tool-input JSON. (`pendingEdits map[int64]string` — chat_id → approval_id.)
2. **Pending prompt edit** — owner tapped `[Edit]` on a queued prompt. Next text reply will replace the prompt body. (`pendingPromptEdits map[int64]string` — chat_id → prompt_id.)
3. **Pending text inject with ambiguous target** — owner sent text, multiple sessions are live, target picker was shown. (`pendingInjects map[int64]string` — chat_id → text.)

If `launchd`/`systemd` restarts the daemon (or it crashes), the owner's next reply silently misroutes: their JSON edit is injected to a session as plain text, or their replacement prompt becomes a brand-new prompt, or their parked text is just gone.

#### Files

- `internal/daemon/daemon.go:73-77` — struct fields `pendingEdits`, `pendingPromptEdits` (and `editMu sync.Mutex`).
- `internal/daemon/daemon.go:120-124` — initialization of the maps in `New()`.
- `internal/daemon/daemon.go:67-70` — `pendingInjects map[int64]string` (under `threadMu`).
- `internal/daemon/approvals.go:262-264, 286-291, 319-321, 333-335, 349-351` — every reader/writer of `pendingEdits`.
- `internal/daemon/prompts.go:238-240, 283-288` — readers/writers of `pendingPromptEdits`.
- `internal/daemon/threading.go:109-121` — readers/writers of `pendingInjects` via `queuePendingInject()` / `popPendingInject()`.
- `internal/store/sqlite.go:198-244` — KV API to use.

#### Approach

Use the existing KV store. **Do not introduce a new table.** TTL = 10 minutes (longer than approval TTL, short enough that abandoned parks expire). Schema is a single composite key per record:

```
key:   pending:<kind>:<chat_id>
value: payload (raw string; semantics depend on kind)
TTL:   now + 10*60 (unix-seconds via KVSet expire arg)
```

Kinds:

- `pending:approval_edit:<chat_id>` → `value = approval_id` (UUID-ish string)
- `pending:prompt_edit:<chat_id>` → `value = prompt_id`
- `pending:inject:<chat_id>` → `value = the raw text the owner sent`

#### Implementation outline

1. Add a small helper file `internal/daemon/pending.go` with:

   ```go
   const pendingTTL = 10 * time.Minute

   func pendingKey(kind string, chatID int64) string {
       return "pending:" + kind + ":" + strconv.FormatInt(chatID, 10)
   }

   func (d *Daemon) setPending(ctx context.Context, kind string, chatID int64, value string) {
       if d.DB == nil { return }
       _ = d.DB.KVSet(ctx, pendingKey(kind, chatID), []byte(value), time.Now().Add(pendingTTL).Unix())
   }

   func (d *Daemon) takePending(ctx context.Context, kind string, chatID int64) (string, bool) {
       if d.DB == nil { return "", false }
       v, ok, err := d.DB.KVGet(ctx, pendingKey(kind, chatID))
       if err != nil || !ok { return "", false }
       _ = d.DB.KVDel(ctx, pendingKey(kind, chatID))
       return string(v), true
   }

   func (d *Daemon) peekPending(ctx context.Context, kind string, chatID int64) (string, bool) {
       if d.DB == nil { return "", false }
       v, ok, err := d.DB.KVGet(ctx, pendingKey(kind, chatID))
       if err != nil || !ok { return "", false }
       return string(v), true
   }
   ```

2. Replace every read/write of the three in-memory maps with these helpers. Keep `editMu` only to serialize Telegram callback-vs-reply ordering for the same chat — but the source of truth becomes SQLite.

   Specifically:

   - `internal/daemon/approvals.go:262-264` (callback `case "edit"`): replace `d.pendingEdits[q.From.ID] = id` with `d.setPending(ctx, "approval_edit", q.From.ID, id)`.
   - `internal/daemon/approvals.go:286-291`, `:319-321`, `:333-335`, `:349-351` (`onReply` path): replace map read/delete with `d.takePending(ctx, "approval_edit", m.From.ID)`. Re-park on validation failure with `d.setPending(...)`.
   - `internal/daemon/prompts.go:238-240` (`prompt_edit` callback): use `d.setPending(ctx, "prompt_edit", q.From.ID, id)`.
   - `internal/daemon/prompts.go:283-288` (`handlePendingPromptEdit`): use `d.takePending(ctx, "prompt_edit", m.From.ID)`.
   - `internal/daemon/threading.go:109-121` (`queuePendingInject` / `popPendingInject`): use `d.setPending(ctx, "inject", chatID, text)` / `d.takePending(ctx, "inject", chatID)`.

3. Delete the in-memory maps from the struct. Delete their initialization in `New()`.

4. Verify `editMu` is still needed; if it only protected map mutation, delete it. (Likely it is no longer needed since SQLite is single-writer-serialized.)

5. **Cleanup of stale rows on boot.** The KV store auto-expires on read, but a daemon that runs for weeks accumulates dead rows that no one ever reads. Add a one-shot cleanup at startup in `daemon.Run()`:

   ```go
   // best-effort sweep of expired pending rows on boot
   _ = d.DB.KVPurgeExpired(ctx)   // new helper, see below
   ```

   And add to `internal/store/sqlite.go` (after `KVDel`):

   ```go
   // KVPurgeExpired deletes all rows whose expire is set and elapsed.
   func (d *DB) KVPurgeExpired(ctx context.Context) error {
       _, err := d.sql.ExecContext(ctx,
           `DELETE FROM kv WHERE expire > 0 AND expire < ?`, time.Now().Unix())
       return err
   }
   ```

#### Validation

**Tests** (add to `internal/daemon/approvals_test.go` and a new `prompts_test.go` case):

- `TestApprovalEditSurvivesRestart`: simulate Edit callback → "restart" by constructing a new `Daemon` against the same `DB` → owner replies with edited JSON → assert `VerdictEdit` decision lands.
- `TestPromptEditSurvivesRestart`: same shape for prompt edit.
- `TestPendingInjectSurvivesRestart`: same shape for ambiguous-target text inject; after restart owner selects target via callback → text dispatched.
- `TestPendingTTLExpires`: set a pending row with `pendingTTL=1*time.Second` (parameterize via package var for tests), wait 2s, assert `takePending` returns `false`.
- `TestKVPurgeExpiredRemovesRows`: insert expired rows, run purge, assert `sql.QueryRow` finds none.

**Manual e2e** (`scripts/manual-e2e-pending-persistence.md`, new file):

1. Start daemon: `onibi run claude`.
2. From Telegram: trigger an approval (any Claude tool call that hits PreToolUse).
3. Tap `[Edit]`. Bot replies "Reply with edited JSON for approval <id>".
4. SIGTERM the daemon. Restart it: `onibi run claude`.
5. Reply in Telegram with valid edited JSON.
6. **Expect**: the edit is accepted as `VerdictEdit`; agent receives edited tool input.

#### Gotchas

- `pendingEdits` callsites also re-park on JSON validation failure (`approvals.go:333-335, 349-351`). Make sure the re-park goes through `setPending` so subsequent retries within the 10-min window still work.
- `pendingInjects` is currently held under `threadMu.Lock()` not `editMu`. After migration, `threadMu` is no longer needed for that field; check if it's still needed for the rest of `threadMu`'s use (`busySessions`, `messageSessions`, `defaultTargets` still need it — leave `threadMu` for those).
- The KV store has a single `kv` table; verify schema in `internal/store/sqlite.go` migrations. The composite-key approach (`pending:approval_edit:1234`) is cheap and avoids schema work.
- 10-min TTL is a choice. If a user takes longer than 10 min to type a JSON edit, they get a "Unknown approval" path. Acceptable since approval TTL is 5 min anyway.

---

### T03 Edit-in-place approval message on daemon restart (P0/M)

#### Motivation

`internal/daemon/approvals.go:84-110` `RestorePendingApprovals` re-sends pending approval messages on daemon restart. The owner now sees a duplicate: the original message (orphaned, no longer wired to a waiter) and the new one. The footer "Re-sent after daemon restart. The original hook may have already proceeded." (`approvals.go:127`) helps but the dup is noisy.

Fix: edit the *original* Telegram message in place (text + keyboard) instead of sending a new one. Fall back to a new message only if the original is gone (`editMessageText` returns `message to edit not found`).

#### Files

- `internal/daemon/approvals.go:84-110` — `RestorePendingApprovals`.
- `internal/daemon/approvals.go:534-543` — `editDecisionKeyboard` (model for how to edit a message; you'll write a similar `editApprovalMessage`).
- `internal/approval/queue.go` (path inferred — see `internal/approval/`) — `Approval` struct has `ChatID int64` and `MsgID int64` per `approvals.go:528-529`.

#### Approach

When restoring a pending approval:

1. If `a.ChatID != 0 && a.MsgID != 0`: call `Bot.EditMessageText` with the freshly rendered body + an updated keyboard. The keyboard should be a fresh `ApprovalKeyboard(a.ID)` (the buttons still work — the callback handler in `onCallback` re-reads state from DB on every press).
2. If the edit fails because the message is gone (Telegram error code 400 `message to edit not found`), fall back to a new `SendMessage`.
3. Always re-record the (chatID, msgID) via `Queue.SetMessage` (already done in current code).
4. Add a one-line "Re-sent" footer **only** in the fallback path. In the edit-in-place path, the keyboard plus the original timestamp are enough.

#### Implementation outline

```go
// internal/daemon/approvals.go
func (d *Daemon) RestorePendingApprovals(ctx context.Context) error {
    if d.Queue == nil || d.Bot == nil || d.Owner == nil {
        return nil
    }
    pending, err := d.Queue.Pending(ctx)
    if err != nil { return err }
    for _, a := range pending {
        sessLabel := a.SessionID
        if s, err := d.Registry.Get(a.SessionID); err == nil {
            sessLabel = s.Name + " (" + s.ID + ")"
        }
        if a.ChatID != 0 && a.MsgID != 0 {
            if d.tryEditApprovalInPlace(ctx, a, sessLabel) {
                continue
            }
        }
        // fall back: send a new message with the "re-sent" footer
        sent, err := d.sendApprovalMessage(ctx, a.ID, a.Tool, a.InputJSON, sessLabel, true, a.ExpiresAt)
        if err != nil {
            d.Log.Warn("restore approval message", slog.String("id", a.ID), slog.Any("err", err))
            continue
        }
        if sent != nil {
            _ = d.Queue.SetMessage(ctx, a.ID, sent.Chat.ID, int64(sent.ID))
        }
    }
    return nil
}

func (d *Daemon) tryEditApprovalInPlace(ctx context.Context, a *approval.Approval, sessLabel string) bool {
    // skip encrypted-mode restorations for now — those use Mini App URLs
    // that include the original ciphertext payload; editing the body would
    // re-encrypt and may break the URL. Send a fresh message instead.
    switch strings.ToLower(strings.TrimSpace(d.EncryptedMode)) {
    case "on", "ask":
        return false
    }
    body := renderApprovalMessage(a.Tool, a.InputJSON, sessLabel)
    _, err := d.Bot.EditMessageText(ctx, &tgbot.EditMessageTextParams{
        ChatID:      a.ChatID,
        MessageID:   int(a.MsgID),
        Text:        body,
        ParseMode:   models.ParseModeHTML,
        ReplyMarkup: telegram.ApprovalKeyboard(a.ID),
    })
    if err != nil {
        d.Log.Info("restore approval edit-in-place failed, will resend",
            slog.String("id", a.ID), slog.Any("err", err))
        return false
    }
    return true
}
```

#### Validation

**Tests** (`internal/daemon/approvals_test.go`):

- `TestRestorePendingApprovalsEditsInPlace`: build a daemon with `telegram.mock`, request an approval, capture `(chatID, msgID)`, simulate restart (new `Daemon` against same DB and a new mock bot), assert `EditMessageText` was called with the same `MessageID` and that no new `SendMessage` happened.
- `TestRestorePendingApprovalsFallsBackOnEditError`: mock bot returns `400 message to edit not found` for `EditMessageText`. Assert `SendMessage` is called as fallback.
- `TestRestorePendingApprovalsEncryptedSkipsEditInPlace`: with `EncryptedMode="on"`, assert fallback path is always used (because Mini App URL needs a fresh ciphertext).

**Manual e2e**:

1. Trigger an approval. Note the message in the chat.
2. SIGTERM the daemon.
3. Restart. The same message should update; no new message arrives.

#### Gotchas

- Telegram `editMessageText` requires `parse_mode` to match the original or it may error on entity offset mismatches. Use `ParseMode: models.ParseModeHTML` — same as `sendPlainApprovalMessage` (`approvals.go:132`).
- If the original message was sent in encrypted mode and we now restart in plaintext mode (or vice versa), editing in place may be confusing. The guard above skips edit-in-place for encrypted mode entirely; consider also skipping if `d.EncryptedMode` changed since `a` was created (you can detect this by trying to render and inspecting — but the simpler rule is "encrypted → never edit in place").
- Don't accidentally re-trigger `Queue.SetMessage` with the same (chatID, msgID) — that's a no-op but adds log noise.

---

## 6. Sprint 2 — onboarding cliff

### T07 `onibi up` convenience command (P1/M)

#### Motivation

The current happy-path is `onibi setup --complete` for first-time, `onibi run claude` or `onibi install-service` for steady-state. New users don't know which to run. Add one command that does the right thing.

#### Files

- `internal/cli/root.go` — register new command.
- `internal/cli/stubs.go` — add factory.
- New: `internal/cli/up.go`.
- `internal/auth/owner.go` — `IsOwnerSet` (existing) tells us if paired.

#### Implementation

```go
// internal/cli/up.go
package cli

import (
    "context"
    "fmt"
    "github.com/spf13/cobra"

    "github.com/gongahkia/onibi/internal/auth"
    "github.com/gongahkia/onibi/internal/config"
    "github.com/gongahkia/onibi/internal/doctor"
    "github.com/gongahkia/onibi/internal/store"
)

func upCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "up",
        Short: "Bring Onibi up: setup if unpaired, otherwise service + doctor",
        RunE:  runUp,
    }
}

func runUp(cmd *cobra.Command, _ []string) error {
    paths, err := config.DefaultPaths()
    if err != nil { return err }
    if err := paths.EnsureDirs(); err != nil { return err }
    db, err := store.Open(paths.DBFile)
    if err != nil { return err }
    defer db.Close()

    ownerSet, err := auth.IsOwnerSet(cmd.Context(), db)
    if err != nil { return err }
    if !ownerSet {
        fmt.Fprintln(cmd.OutOrStdout(), "Not paired yet — running setup --complete.")
        // delegate
        cmd.Flags().Set("complete", "true")
        return runSetup(cmd, nil)
    }
    fmt.Fprintln(cmd.OutOrStdout(), "Already paired — installing service and running doctor.")
    if err := runInstallService(cmd, nil); err != nil { return err }
    report := doctor.Run(cmd.Context(), doctor.Options{Paths: paths, Mode: "installed"})
    for _, c := range report.Checks {
        fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s: %s\n", c.Status, c.Name, c.Detail)
    }
    if report.Failed() {
        return fmt.Errorf("doctor failed: see output above")
    }
    return nil
}
```

Register in `root.go`:

```go
root.AddCommand(upCmd())
```

Add factory to `stubs.go` (alongside the other `*Cmd` functions).

Update README quick-start: change the lead-in to `onibi up` instead of `onibi setup --complete`. Mention `onibi setup --complete` as the explicit alternative.

#### Validation

**Tests** (`internal/cli/up_test.go`, new):

- `TestUpRunsSetupWhenUnpaired`: empty DB + mock setup → runSetup is invoked.
- `TestUpInstallsServiceWhenPaired`: pre-populate owner, mock service manager → install path runs.

**Manual e2e**:

1. Fresh state dir: `rm -rf ~/Library/Application\ Support/onibi/` (macOS) or `rm -rf ~/.local/share/onibi/` (Linux).
2. `onibi up` → drops into setup-complete flow.
3. After pairing, re-run `onibi up` → installs service + runs doctor.

#### Gotchas

- Reusing `cmd.Flags().Set("complete", "true")` works only if `runSetup` reads `complete` from `cmd.Flags()`, which it does (`internal/cli/setup.go:29`). If you'd rather not couple, refactor `runSetup` to accept an options struct.
- Don't reset the user's preferences (`encrypted-mode`, etc.). `onibi up` only handles the always-on path.

---

## 7. Sprint 3 — Telegram steady-state UX

## 8. Sprint 4 — engineering hardening

### T15 MCP server test coverage (P1/M)

#### Motivation

`internal/mcpserver/` has one test (`internal/cli/mcp_stdio_test.go`). The MCP server is the external contract surface for any MCP-capable agent (Claude SDK, Codex MCP, Cursor MCP). 5 tools, each with a JSON schema, error semantics, and daemon-required vs daemon-optional invariants.

#### Files

- `internal/mcpserver/server.go` — implementation.
- `internal/mcpserver/server_test.go` — new file.

#### Approach

Use the mcp-go library's testing helpers if available; otherwise drive the stdio transport via `bytes.Buffer` for input and capture output.

Each test should:

1. Boot a daemon with a mock bot and a temp socket.
2. Start the MCP server pointing at that socket.
3. Call each tool through the MCP protocol.
4. Assert on the response shape and side effects.

Test list:

- `TestNotifyToolDelivers`: call `onibi_notify` with `{session,text}`; assert intake server received the event; assert response `{ok: true}`.
- `TestNotifyToolDaemonDown`: stop intake server; assert response `{ok: false}`.
- `TestApprovalRequestTimeout`: call `onibi_approval_request` with `timeout_seconds=1`; don't decide; assert response `{decision: "cancelled"}` after timeout.
- `TestApprovalRequestApprove`: call tool; in parallel, decide via `Queue.DecideWithResult`; assert response `{decision: "approve"}`.
- `TestApprovalRequestInvalidJSON`: call with malformed `input_json`; assert validation error.
- `TestSessionListReadsDB`: pre-populate sessions table; call `onibi_session_list`; assert all sessions returned.
- `TestSessionListWithoutDaemon`: stop daemon but keep DB; call tool; assert it still works (it should — `session_list` reads the DB directly per audit).
- `TestSessionInputDispatches`: spawn a session, call `onibi_session_input`; assert PTY received text.
- `TestSessionPeekReturnsTail`: write to PTY ring buffer, call `onibi_session_peek`; assert response contains tail.

Use `testify` only if already in `go.sum`; otherwise use stdlib testing.

#### Validation

`go test -race -count=1 ./internal/mcpserver/...` passes with all new tests.

#### Gotchas

- The MCP server uses jsonschema tags for input/output (per audit). Test that schema generation matches expected JSON-Schema for each tool — there may be an existing `t.Run("schema", ...)` pattern in mcp-go test helpers.
- Owner-uid check via socket peer-cred is hard to test cross-platform. Skip in CI (`testing.Short()`); cover in T18.

---

### T16 Versioned shell hook blocks + auto-reinstall on `doctor --fix` (P1/M)

#### Motivation

Hook block markers in `internal/adapters/shell/install.go` look like `# >>> onibi managed shell hook` / `# <<< onibi managed shell hook` and a content hash. If a new release ships an improved hook body, every existing user's `doctor` will FAIL on hook tamper detection. Today the remediation is `onibi install-hooks --uninstall && onibi install-hooks`. We should auto-migrate on `doctor --fix`.

#### Files

- `internal/adapters/shell/install.go` — block markers + content.
- `internal/adapters/common/` — hash record/verify.
- `internal/doctor/fix.go` — `--fix` actions.

#### Approach

1. Add a content-version int (e.g., `# onibi-shell-hook v=2`) to the block header.
2. At install time, record `(name, content_hash, version)` in the DB's `hooks` table.
3. At `Verify()` time, distinguish three outcomes:
   - **PASS**: content hash matches recorded hash.
   - **OUTDATED**: hash differs but the new content's version matches a known-good newer version (the adapter knows the canonical body for vN+1). Status = WARN.
   - **TAMPERED**: hash differs and content doesn't match any known version.
4. In `doctor --fix`, if a hook is OUTDATED, re-install the new version automatically. If TAMPERED, refuse and tell the user to `install-hooks --uninstall` first (user may have edited the block manually).

#### Implementation outline

1. Add a `Version` field to `common.HookRecord` (read its source first; if it doesn't exist, add it).
2. In each adapter's `Install()`, set the version constant.
3. In `Verify()`, return a richer status struct `(ok bool, outdated bool, err error)`.
4. In `doctor.go`'s `checkHooks`, map `outdated` to a new `StatusOutdated` (or reuse Warn).
5. In `doctor/fix.go`, on `--fix`, call `adapter.Install(forceReinstall=true)` when `outdated`.

#### Validation

**Tests**:

- `TestVerifyDetectsOutdatedHook`: install with version=1, simulate registry bumping to version=2, assert `Verify` returns outdated=true.
- `TestDoctorFixUpgradesOutdated`: same setup; assert `--fix` reinstalls and `Verify` then returns ok.
- `TestDoctorFixRefusesTampered`: tamper manually, assert `--fix` does not overwrite and reports a clear message.

#### Gotchas

- This breaks adapter-internal API. Update all 8 agent adapters + 3 shell adapters in one commit.
- The DB schema likely needs a new column. Use the migration pattern in `internal/store/sqlite.go` (`addColumnIfMissing`-style, see line 178-192).

---

### T17 Prune legacy logo assets (P1/S)

#### Motivation

`asset/logo/onibi-logo.png` (~2.4MB) and `asset/logo/onibi-v1-logo.png` (~7.2MB) ship in tree but are never embedded (`grep -rn "onibi-logo.png\|onibi-v1-logo.png" .` should return only `asset/logo/` itself). Only `internal/brand/onibi-v2-logo.png` is `//go:embed`-ed. Repo bloat ~10MB.

#### Files

- `asset/logo/onibi-logo.png` — delete.
- `asset/logo/onibi-v1-logo.png` — delete.
- `asset/logo/onibi.jpg` — verify usage with `grep -rn "onibi.jpg"`. Delete if unused.
- README: check if `asset/logo/onibi-logo.png` is the README header (`README.md:4` references `./asset/logo/onibi-logo.png` — **this is in use**).

**Wait**: `README.md:4` uses `onibi-logo.png` as the visual header. **Do not delete `onibi-logo.png` until the README is updated** to reference `internal/brand/onibi-v2-logo.png` or a small derivative.

#### Approach

Two paths. Pick **B**.

**A**: Keep `onibi-logo.png` (it's the README header), delete only `onibi-v1-logo.png`.

**B (preferred)**: Generate a small (~50KB) `asset/logo/onibi.png` from the v2 logo for README use; replace v1, v2-big, and v1-big with the one canonical. Update README reference.

#### Implementation

```bash
# given imagemagick installed
magick internal/brand/onibi-v2-logo.png -resize 1024x asset/logo/onibi.png
git rm asset/logo/onibi-logo.png asset/logo/onibi-v1-logo.png asset/logo/onibi.jpg
# update README:4 to use ./asset/logo/onibi.png
```

(If imagemagick isn't on the contributor's machine, skip the regen and just update README to point to `internal/brand/onibi-v2-logo.png` directly.)

#### Validation

- `du -sh asset/logo/` shows ~390KB (down from ~10MB).
- README renders correctly on GitHub.

#### Gotchas

- Don't `git rm` the v2 logo by accident; it's in `internal/brand/`, not `asset/logo/`.
- Check for any docs page or `docs/index.html` that references the same paths.

---

### T18 Cross-platform peer-cred test in CI matrix (P2/M)

#### Motivation

`internal/intake/peercred_darwin.go` and `peercred_linux.go` implement same-UID peer-cred checks on the Unix socket. Test coverage for cross-UID rejection is unclear; CI matrix runs on both `ubuntu-latest` and `macos-latest` (`.github/workflows/ci.yml:18-19`) but tests don't appear to exercise rejection.

#### Files

- `internal/intake/server.go` — peer-cred enforcement.
- `internal/intake/server_test.go` — current tests.
- New test entries.

#### Approach

Same-UID test is straightforward and already (probably) exists. Cross-UID test requires either privilege escalation or careful socket impersonation. Two options:

1. **Mock the peer-cred function**: add a package-private `peerCredFn` variable that defaults to the OS implementation but can be overridden in tests to simulate a different UID. Test that the server rejects when the mock returns a different UID.
2. **Real cross-UID**: use a sudo-needing test that creates a second user. Skip on CI; require manual `sudo -E go test`.

Pick **option 1**. Real cross-UID belongs in a manual e2e doc (`scripts/manual-e2e-peercred.md`).

#### Implementation

1. Refactor `peerCred(conn)` to be assigned as a package-private function variable:

   ```go
   // peercred_linux.go
   var peerCred = peerCredLinux
   func peerCredLinux(conn *net.UnixConn) (int, error) { … }
   ```

2. In the test:

   ```go
   func TestServerRejectsOtherUID(t *testing.T) {
       orig := peerCred
       peerCred = func(conn *net.UnixConn) (int, error) { return os.Getuid() + 1, nil }
       defer func() { peerCred = orig }()

       srv, sockPath := startTestServer(t)
       defer srv.Stop()

       _, err := intake.Send(sockPath, intake.Event{Type: intake.TypeAgentDone})
       if err == nil { t.Fatal("expected rejection, got success") }
   }
   ```

3. Add `os: macos-latest` to the matrix already in CI; the test should pass on both.

#### Validation

`go test -race -count=1 ./internal/intake/...` passes on both ubuntu-latest and macos-latest.

#### Gotchas

- macOS uses `LOCAL_PEERPID`/`LOCAL_PEEREPID` via `getsockopt`; Linux uses `SO_PEERCRED`. The mock approach sidesteps OS-specific quirks.
- Don't break the existing same-UID happy-path test.

---

### T19 Structured `log --json` (P2/L)

#### Motivation

`onibi log` prints free-form audit text. For users piping into `jq` or external SIEMs, structured output is more useful. The schema already exists in SQLite (`audit_log(ts, action, session_id, payload, decided_by_chat_id, detail)`); just emit it.

#### Files

- `internal/cli/log.go` — `runLog`.
- `internal/store/audit.go` — audit query.

#### Implementation

Add a `--json` flag:

```go
cmd.Flags().Bool("json", false, "emit one audit entry per line as JSON")
```

In `runLog`, when `--json`:

```go
enc := json.NewEncoder(cmd.OutOrStdout())
for _, e := range entries {
    _ = enc.Encode(e)
}
```

Make sure `store.AuditEntry` has json tags on every field.

For `--export`, the current behavior writes csv/json by extension; verify that path is unaffected.

#### Validation

`onibi log --json --n 5 | jq '.action'` returns 5 action strings.

---

## 9. Sprint 5 — docs depth

### T20 `docs/getting-started.md` (P0/S)

#### Motivation

`docs/demo-checklist.md` is a 90-second demo recipe, not a tutorial. New users need a walk-through.

#### Content outline

1. **Prerequisites**: Telegram account with 2-step verification, Go 1.26+ or pre-built binary.
2. **Install** (`brew install --cask gongahkia/onibi/onibi` once tap is published; meanwhile `make install`).
3. **Create your bot**: BotFather flow with screenshots/copy-pasteable commands.
4. **Pair**: `onibi setup --complete` (or `onibi up` after T07). Annotate what each prompt does.
5. **First approval**: install Claude Code adapter, run `onibi run claude`, ask Claude to run a shell command, approve from phone.
6. **Common next steps**: switch to encrypted mode, install service for background, configure shell hooks.
7. **What if something goes wrong**: pointer to `docs/troubleshooting.md`.

Target length: ~600 lines, with code blocks copy-pasteable.

#### Validation

- A reviewer follows the doc on a fresh macOS install. Notes any step that required external knowledge.

---

### T21 `docs/architecture.md` (P0/S)

#### Content outline

1. Component diagram (use the one in section 1 of this TODO).
2. Intake JSON schema (copy from `internal/intake/event.go`, with one example per event type).
3. Approval state machine: pending → {approved, denied, edited, expired, cancelled}. Diagram. Mention TTL.
4. Encrypted message envelope (`internal/envelope/envelope.go` — read for the JSON shape).
5. Daemon goroutine inventory (intake, idle detector, sweeper, bot long-poll, optional exit-on-idle).
6. Sessions and PTY: how `creack/pty` is used, ring buffer semantics, PNG render path.
7. Storage: SQLite tables and their purposes (approvals, prompts, sessions, audit_log, kv, pairing_tokens, hooks).

Target length: ~400 lines.

---

### T22 `docs/mcp.md` with client examples (P1/S)

Depends on T15 (tests stabilize the schema).

#### Content outline

1. What is MCP, what onibi exposes, why a user would care.
2. `claude_desktop_config.json` snippet:

   ```json
   {
     "mcpServers": {
       "onibi": {
         "command": "/Users/you/.local/bin/onibi",
         "args": ["mcp"]
       }
     }
   }
   ```

3. Per-tool docs: name, JSON schema (inputs and outputs), example call, daemon-required boolean.
4. Auth model: peer-UID only; you cannot reach the daemon from outside the OS user.
5. Troubleshooting: "daemon down" responses, expected error shapes.

---

### T23 `docs/encrypted-mode.md` (P0/S)

#### Content outline

1. Threat model when Telegram is untrusted (e.g., regulatory environment, journalism, sensitive code review).
2. Encryption details: HKDF-SHA256 → AES-256-GCM, per-message random nonce, seed in Telegram SecureStorage on the device + on disk in the daemon.
3. Mini App self-hosting: serve `docs/miniapp/index.html` from any static host; pass `--mini-app-url` to setup.
4. UX consequences:
   - `/prompt`, `/send`, `/editprompt`, `/rename <id> <name>` refuse plaintext.
   - `/secure` opens the Mini App for encrypted input.
   - Reply-to-message and `//<text>` are blocked in encrypted mode.
5. Key rotation: regenerate seed via `onibi setup --enable-encrypted-mode` (re-pair Mini App).
6. Not-defended: anyone with the seed on the device can decrypt; if Telegram is compromised at the rendering layer (rendering Mini App HTML), they could intercept plaintext post-decrypt.

---

### T24 Shell-hook conflict troubleshooting (P1/S)

Append to `docs/troubleshooting.md`:

- **Starship**: no conflict; both use `precmd`.
- **oh-my-zsh**: append `source ~/.zshrc` may overwrite; Onibi appends after init, so generally safe. If symptoms: empty `cmd_done` events.
- **fish conf.d**: Onibi replaces `~/.config/fish/conf.d/onibi.fish` wholesale. Don't edit in place.
- **bash + nix-shell**: nix-shell may reset `PROMPT_COMMAND`; Onibi uses `preexec`/`precmd` hooks (or DEBUG trap on bash). Test by sourcing `~/.bashrc` after `nix-shell --pure`.

---

### T25 Real `docs/index.html` landing (P1/M)

Depends on T20, T21.

Replace the 6-line redirect with a single-page landing:

- Hero: name, one-line pitch, install command.
- Brief feature list with anchors.
- Link to README on GitHub, getting-started, architecture, mcp, encrypted-mode.
- Link out to the Mini App route at `/miniapp/`.

Use plain HTML/CSS; no JS framework. Match the v2 logo at `internal/brand/onibi-v2-logo.png`.

---

## 10. Out of scope — do NOT do

These items would break the invariants in §0. If you find yourself drawn to one, stop and re-read §0.

- Webhook receiver (any inbound HTTP) — even "just for local dev."
- ngrok / cloudflared / Tailscale tunnel helper baked into `onibi`.
- Multi-user `onibi serve` mode that lets two OS users share a daemon.
- Account system, OAuth, hosted Onibi auth.
- Telemetry/analytics, even opt-in. (HN/OSS audience hates this.)
- Browser-based web dashboard. The Mini App is the only first-party UI surface.
- Signal/Discord/Slack/email/SMS adapters. Telegram-only is a feature, not a limitation.
- Auto-update mechanism inside the binary. Homebrew + GoReleaser is the update path.
- Embedded LLM (or local LLM) for "smart approvals." Out of scope; the agent decides what to do, the owner just gates.
- A second pivot. v2 is final in this repo.

---

## 11. Useful commands (cheat sheet)

```bash
# build + install both binaries to ~/.local/bin/
make install

# run CI-equivalent locally
make test
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@latest ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# snapshot release smoke (requires goreleaser + syft installed)
make release-smoke

# wipe local state for clean re-test (be sure!)
rm -rf "$HOME/Library/Application Support/onibi/"   # macOS
rm -rf "$HOME/.local/share/onibi/"                  # Linux

# tail the daemon log
onibi tail-log --follow

# look at the audit table directly
sqlite3 "$HOME/Library/Application Support/onibi/state.db" \
  "SELECT ts, action, detail FROM audit_log ORDER BY id DESC LIMIT 20"

# rebuild and reinstall just the notify client
go build -o bin/onibi-notify ./clients/onibi-notify
install -m 0755 bin/onibi-notify "$HOME/.local/bin/"
```

---

## 12. Glossary

- **Adapter**: per-agent or per-shell installer that writes a hook into the agent's config or the shell's rc file. Registered in `internal/adapters/registry.go`.
- **Approval**: a blocking RPC initiated by an agent hook (PreToolUse for Claude Code; equivalents for others) asking the owner to allow/deny/edit a tool call. State persisted in SQLite, decided via Telegram callback. TTL 5min (1min in paranoid mode).
- **Daemon**: the long-running Go process. One per OS user. Hosts PTYs, owns the intake socket, owns the Telegram long-poll loop.
- **Encrypted mode**: `telegram.encrypted_mode={off,ask,on}`. When `on`, all approval / prompt / preview bodies travel as AES-256-GCM ciphertext, decrypted client-side in the Mini App. Equal-parity preference, not a deprecation of plaintext.
- **Intake socket**: Unix socket at `paths.Socket` (typically `/tmp/onibi.sock` or under the user's runtime dir). Same-UID peers only. Carries fire-and-forget events (turn-complete, cmd-done, agent-message) and RPCs (approval_request, session_input, session_peek).
- **MCP**: Model Context Protocol. `onibi mcp` is a stdio MCP server exposing 5 tools backed by the local daemon.
- **Mini App**: the Telegram WebApp at `docs/miniapp/index.html`. Static HTML+JS, decrypts ciphertext using a seed stored in Telegram SecureStorage, posts decisions back via `tg.sendData`.
- **Owner**: the single paired Telegram user ID. Stored in DB. Every inbound update is checked against this ID in `internal/telegram/router.go`.
- **Pairing token**: single-use 32-byte random hex token with 5-min TTL, embedded in a deeplink `/start pair_<token>`. Consumed atomically.
- **Paranoid mode**: KV `paranoid=1`. Tightens approval TTL to 60s, requires TOTP on confirm taps and on approval edits.
- **PTY**: pseudo-terminal. Created via `creack/pty`. The daemon owns the master fd; the spawned agent process reads/writes the slave.
- **Ring buffer**: per-session bounded byte buffer (default 64 KiB) holding recent agent output. Source for `/peek`, screenshots, text tails.
- **Session**: a PTY-hosted agent or wrapped CLI. Has an ID, name, agent kind, start time, optional tmux target. Tracked in `internal/daemon/Registry`.
- **TOTP**: 6-digit RFC 6238 code. Optional gate on destructive commands. `internal/daemon/totp.go` + `internal/auth/`.
- **`onibi-notify`**: the small client binary at `clients/onibi-notify/`. Invoked from agent and shell hooks. Two modes: fire-and-forget (silent exit 0 if daemon down) and RPC (`--wait`).

---

End of TODO. When picking up work, confirm the relevant ticket's file references against current code — this doc was authored 2026-06-14 from a verified audit, but the codebase moves.
