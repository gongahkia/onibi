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
- [6. Sprint 2 — onboarding cliff](#6-sprint-2--onboarding-cliff)
- [7. Sprint 3 — Telegram steady-state UX](#7-sprint-3--telegram-steady-state-ux)
- [8. Sprint 4 — engineering hardening](#8-sprint-4--engineering-hardening)
- [9. Sprint 5 — docs depth](#9-sprint-5--docs-depth)
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
| Daemon | `internal/daemon/approvals.go` | Approval RPC, callbacks, edit flow |
| Daemon | `internal/daemon/prompts.go` | Prompt queue, dispatch, pending prompt edit routing |
| Daemon | `internal/daemon/threading.go`, `pending.go` | Default target, KV-backed pending inject/edit state, target picker |
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
- Race-condition territory (anything touching `daemon.threadMu`, `daemon.mu`, or KV-backed pending state) — write a `t.Run` parallel subtest if behavior is concurrent.
- Telegram tests use `internal/telegram/mock.go`. Prefer that to a real bot.

### Commit messages

Inspect `git log --oneline` for the project's style — typically `feat(app): <thing>`, terse. Don't introduce a new style.

---

## 4. Ticket index

Sprints are independent; tickets within a sprint are roughly ordered by dependency. P0 = block stable tag's UX baseline. P1 = same-quarter. P2 = nice-to-have.

| # | Title | Priority | Effort | Depends on |
|---|---|---|---|---|

No active tickets remain.

---

## 5. Sprint 1 — durability and footguns

## 6. Sprint 2 — onboarding cliff

## 7. Sprint 3 — Telegram steady-state UX

## 8. Sprint 4 — engineering hardening

## 9. Sprint 5 — docs depth

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
