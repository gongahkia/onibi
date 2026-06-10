# Onibi — TODO (v2, Telegram pivot, Go rewrite)

Single source of truth. Supersedes all prior plan docs (v0 Swift, v1 Go-daemon, v1.5 Rust/Tauri/PWA). Update this file as work progresses. Agent-guidance, decisions, phases, polish, risks — all here.

Created 2026-06-10. Owner: gongahkia.

---

## 0. Mission

A single static Go binary that runs as a user-agent on macOS/Linux. Hosts coding agents (Claude Code, Codex, OpenCode, Goose, etc.) under PTYs and routes their approval prompts, turn-completion signals, and terminal output to the user via a Telegram bot. The user controls and monitors everything from Telegram on their phone. No accounts, no SaaS, no inbound network, no PWA, no native desktop UI.

The user must be able to:
1. `brew install onibi`
2. Walk through one interactive `onibi setup` (~2 min, requires creating a bot via @BotFather once)
3. Get a Telegram message every time an agent needs approval or finishes a turn
4. Reply in Telegram to inject text into the agent
5. Trust that the daemon only runs while the laptop is open and only talks to `api.telegram.org`

---

## 1. Hard rules (do not break)

- **Local-only.** Outbound HTTPS to `api.telegram.org` only. No inbound listeners. No tunnels, no LAN exposure, no self-signed certs, no public URLs. The whole product runs as a user-agent that only exists while the laptop is awake and the user is logged in.
- **Apache-2.0, no telemetry, no accounts, no SaaS.** Preserved from v1.5.
- **Single user per install.** Owner is established **once** via the deeplink-pair flow (§6.1). Owner never changes silently. Multi-user is v2.
- **Pair once, then linked.** After the one-time pair, the user is permanently the owner. No re-auth prompts on normal use. Re-pair only happens if user explicitly runs `onibi setup --rotate-owner`. TOTP defense-in-depth is opt-in, not default.
- **No shell from chat.** Free text only ever becomes literal `send-keys` / write-to-PTY. System actions only via the preset allowlist.
- **No webhooks. Ever.** Long-poll only. Webhook mode would require an inbound listener and undermines the local-only invariant.
- **Bot token never in plaintext on disk** if macOS Keychain or Linux Secret Service is available. Fall back to `.env` (0600) only if no keystore is present. Token never in process argv, never in logs, never in error messages.
- **Owner check on every inbound update.** All non-owner updates dropped at the router boundary and counted (not full-logged, to avoid log flooding). Includes both message updates and callback queries.
- **Fail-open intake.** When the daemon is down, any hook scripts exit 0 silently. Never block the user's shell or agent.
- **Approval lifetime ≤5 min.** Late callbacks rejected. Prevents stale-approval hijack if owner Telegram is compromised long after a request was raised.
- **Seamless pairing.** Setup must be ≤3 user-visible steps after `brew install`, and after pair zero recurring auth steps. Otherwise it isn't shipped.
- **Code-signed and notarized macOS binary.** Unsigned binaries are blocked by Gatekeeper and undermine the install story. Apple Developer ID is a fixed cost we accept.
- **No third pivot in this codebase.** If this direction fails, archive and start a new repo. Stop pivoting `onibi`.

---

## 2. Decisions (locked)

| Topic | Decision | Rationale |
|---|---|---|
| Language | Go | Better fit than Rust for Telegram-bot + PTY-hosting + SQLite. Simpler build, simpler distribution, mature ecosystem for this scope. |
| Name | `onibi` (unchanged) | User wants brand continuity. |
| UI surface | Telegram only | Tauri desktop and PWA both deleted. No native UI. |
| Output channel | Auto-switch text/PNG | Text by default; PNG when ANSI cursor moves / alt-screen detected; `/text` `/screenshot` overrides. |
| Pane control plane | PTY-owned default, tmux-attach optional | Works for non-tmux users (majority); power-users can attach to existing tmux sessions. |
| Telegram lib | `github.com/go-telegram/bot` | Modern, actively maintained as of Mar 2026, 384+ importers, framework-style with callback router. Fallback: `gopkg.in/telebot.v4`. |
| PTY lib | `github.com/creack/pty` | De facto standard. |
| Terminal emulator (for PNG render) | `github.com/jaguilar/vt100` + stdlib `image/png` | Programmable ANSI emulator, exposes a cell grid. Rasterize cell-by-cell with a fixed-width font. |
| SQLite | `modernc.org/sqlite` | Pure-Go, no cgo. Simpler cross-compile and Homebrew distribution. Performance hit (2x slower at worst) is irrelevant at KV+approvals scale. |
| CLI framework | `github.com/spf13/cobra` | Nested subcommands needed (`onibi setup`, `onibi run`, `onibi install-hooks <agent>`, etc.); cobra is the obvious fit. |
| Config | YAML via `gopkg.in/yaml.v3` + `.env` via `github.com/joho/godotenv` | Standard. |
| Distribution | GoReleaser → Homebrew tap (`gongahkia/onibi`) | One-command install for the target audience. |
| Auto-start | macOS launchd `LaunchAgent` in `~/Library/LaunchAgents/` | Matches "only runs while laptop is open and user is logged in" requirement exactly. Linux: systemd user unit. |
| Agent monitoring chattiness | See §5 defaults | "Sensible first, configurable later." |
| Secret storage | macOS Keychain (via `github.com/99designs/keyring`) → falls back to `.env` (0600) | Keychain requires user-session auth to read; `.env` does not. Cross-platform abstraction so Linux/Windows can use Secret Service / Credential Manager later. |
| Owner establishment | Single-use deeplink token, 5-min TTL, via `t.me/<bot>?start=pair_<token>` | Eliminates first-message-becomes-owner race. Only the user who can see `onibi setup` terminal output can pair. |
| Owner persistence | Permanent after pair | Matches "pair once, then linked" UX requirement. Owner change requires explicit `onibi setup --rotate-owner`. |
| TOTP second factor | Opt-in, off by default | Default UX is frictionless. Documented threat: if disabled, security rests on Telegram account security; setup recommends enabling Telegram 2-step verification. Opt-in via `onibi setup --enable-totp` or `onibi setup --paranoid`. |
| Approval expiry | 5 min hard limit | Stale-approval hijack mitigation. |
| Telegram TLS | stdlib `crypto/tls` defaults, no cert pinning | api.telegram.org cert rotates; pinning is fragile. Go's default root store + HSTS is the right baseline. |
| Code signing | Apple Developer ID + notarization via `mitchellh/gon` integrated with GoReleaser | Required for Gatekeeper to allow execution post-macOS Catalina. |

---

## 3. Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│ Mac/Linux (user-agent — alive while user is logged in & awake)   │
│                                                                  │
│  onibi daemon (single Go binary)                                 │
│  ├─ session registry (per-agent PTY-hosted process)              │
│  ├─ adapter handlers (one per agent: claude, codex, opencode…)   │
│  │    └─ hooks installed in agent's settings → call back to      │
│  │       daemon over local Unix socket /tmp/onibi.sock           │
│  ├─ approval queue (SQLite-backed, survives restart)             │
│  ├─ output renderer (text tail | vt100 sim → PNG)                │
│  ├─ telegram client (long-poll, sendMessage, sendPhoto,          │
│  │    inline keyboards, callback queries, threading by reply-to) │
│  └─ auth (TOTP + first-msg-owner, ported pattern from tgterm)    │
│                                                                  │
└────────────────────────────────────┬─────────────────────────────┘
                                     │ HTTPS outbound only
                                     ▼
                          api.telegram.org → user's phone
```

**Event flow in:**
- Agent hook fires (Claude `Stop` / `PreToolUse`, Codex `PreToolUse`, OpenCode `permission.replied`, shell `precmd` for opt-in cmd-done) → calls local `onibi-notify` client → writes JSON event to Unix socket → daemon formats + sends Telegram message.
- PTY output stream → ring buffer → idle-detector → on agent-turn-complete sends rendered snippet.

**Command flow out:**
- Telegram callback / reply → long-poll loop → resolve target session (reply-to-message pane mapping > `/target` > most-recent) → write to PTY or `tmux send-keys` → optional ack.

**Unix socket:** `~/Library/Application Support/onibi/onibi.sock` (macOS), `$XDG_RUNTIME_DIR/onibi.sock` (Linux). Permissions `0600`.

**State dir:** `~/Library/Application Support/onibi/` (macOS), `$XDG_DATA_HOME/onibi/` (Linux). Contains `onibi.sqlite`, `config.yaml`, `.env`, logs.

---

## 4. Repo layout (Go)

```
onibi/
├── cmd/onibi/main.go              # cobra root; subcommands wired here
├── internal/
│   ├── cli/                       # one file per subcommand: run, setup, doctor,
│   │                              # get-chat-id, install-hooks, install-service,
│   │                              # sessions, log
│   ├── daemon/                    # lifecycle, signal handling, session registry,
│   │                              # event bus, debounce
│   ├── intake/                    # Unix-socket server, event JSON schema validator
│   ├── pty/                       # creack/pty host; spawn, resize, read loop
│   ├── tmux/                      # optional tmux-attach backend
│   ├── approval/                  # blocking protocol; allow/deny/edit semantics
│   ├── adapters/
│   │   ├── claude/                # Stop + PreToolUse handlers
│   │   ├── codex/                 # SessionStart/PreToolUse/PostToolUse/Stop bridge
│   │   ├── opencode/              # permission.replied + tool.execute.before
│   │   ├── goose/                 # lifecycle event bridge
│   │   ├── shell/                 # zsh precmd/preexec, bash, fish (opt-in)
│   │   └── registry.go            # discovery, hook install/uninstall, capability matrix
│   ├── telegram/
│   │   ├── client.go              # long-poll, getUpdates, sendMessage, sendPhoto
│   │   ├── router.go              # message + callback routing
│   │   ├── threading.go           # message_id ↔ session_id map
│   │   └── keyboards.go           # inline keyboard builders
│   ├── render/
│   │   ├── text.go                # last-N-lines, fenced code block, ANSI strip
│   │   ├── png.go                 # vt100 sim → image grid → image/png
│   │   ├── switch.go              # auto-switch heuristic
│   │   └── font.go                # embedded fixed-width font (JetBrains Mono?)
│   ├── auth/                      # TOTP RFC 6238, owner registration, session
│   ├── store/                     # modernc.org/sqlite KV + approvals table
│   ├── config/                    # YAML loader, env loader, validation
│   ├── service/                   # launchd plist gen (macOS), systemd unit (Linux)
│   ├── setup/                     # interactive wizard, pairing deeplink generator
│   └── logging/                   # slog wrapper, token redaction filter
├── adapters/                      # hook script templates (reused from current repo)
│   ├── claude-code/settings-snippet.json
│   ├── codex/hooks.json
│   ├── opencode/plugin.ts
│   └── …
├── hooks/                         # per-shell snippets (opt-in cmd-done)
│   ├── zsh/precmd-preexec.zsh
│   ├── bash/
│   └── fish/
├── clients/
│   └── onibi-notify               # tiny Go binary invoked by hooks; fails open
├── docs/
├── scripts/                       # release, install-hooks, dry-run
├── .goreleaser.yaml
├── Makefile
├── go.mod
└── README.md
```

---

## 5. Notification chattiness defaults (sensible-first)

| Event | Default | Override |
|---|---|---|
| Blocking approval request | ON | always on (the product) |
| Agent turn complete (PTY idle ≥3s after activity) | ON | `notifications.turn_complete: false` |
| Session start | OFF | `notifications.session_start: true` |
| Session end / agent exit | ON (non-zero rc tagged distinctly) | `notifications.session_end: false` |
| Shell command done >5s | OFF (opt-in via `onibi install-hooks --shell zsh`) | per-shell config |
| Per-line output streaming | NEVER | — |
| Heartbeat / liveness | NEVER | — |
| Snooze window | none | `/mute 30m`, `/mute claude`, `/unmute` |

Coalescing rules:
- Dedup events with same `(session_id, type, hash(payload))` within 2s.
- Burst output → single message with tail, not many small messages.
- Token-per-second cap to stay well under Telegram's 30 msgs/sec global limit and 1 msg/sec per chat soft limit.

Expected typical Claude Code task: 3–6 messages.

---

## 6. Setup UX — pair once, then linked

User's hard requirements:
- Pairing happens **once**. After that the user is permanently linked and never sees an auth prompt during normal use.
- No cybersecurity holes from removing recurring auth.

Target: ≤3 user-visible interactions during initial `brew install` setup; **0** during normal operation thereafter.

### 6.1 First-time onboarding (one time, ever)

1. **`brew install gongahkia/onibi/onibi`** (binary is code-signed + notarized).
2. **`onibi setup`** — interactive wizard:

   - **Step A (user-visible 1):** "Open Telegram on your phone, message @BotFather, send `/newbot`, choose a name and a hard-to-guess username (e.g. `onibi_$(openssl rand -hex 4)_bot`), then paste the token here." The wizard prints the suggested random username so the user just copies it.
     - Wizard validates the token via `getMe` (must succeed before continuing).
     - Token is stored: **macOS Keychain** (`service=sh.onibi.daemon`, `account=bot_token`) if available; otherwise `.env` chmod 0600 in `~/Library/Application Support/onibi/`.
   - Wizard generates a 32-byte cryptographically random pairing payload, base64url-encoded, persisted with 5-min TTL, single-use.
   - **Step B (user-visible 2):** Wizard prints:
     - Clickable link: `https://t.me/<bot_username>?start=pair_<payload>`
     - QR code rendered in the terminal (ported from `tgterm/bot.c:129-156` pattern, using `github.com/skip2/go-qrcode`) — scannable from the phone camera.
   - User taps the link or scans the QR → Telegram opens chat with bot → tapping Start sends `/start pair_<payload>` → daemon's deeplink handler:
     - Validates payload is exact match + unexpired + unused (constant-time compare).
     - Marks payload consumed (single-use enforced atomically in SQLite).
     - Stores `from.id` as the permanent owner chat_id.
     - Replies "Paired. Onibi is ready. This chat is now your owner channel."
   - **Step C (user-visible 3):** Wizard prints a one-screen tour of `/sessions`, `/status`, `/help`, then ends with:
     - **"Strongly recommended: enable Telegram 2-step verification."** With a clickable line: "Telegram → Settings → Privacy and Security → Two-Step Verification." This is the single most important step for protecting your daemon and the wizard refuses to mark setup complete until the user has acknowledged it (`[I have enabled Telegram 2FA] [Skip for now]`).
3. **`onibi install-service`** (offered at the end of the wizard, default yes): writes `~/Library/LaunchAgents/sh.onibi.daemon.plist` with `RunAtLoad=true`, `KeepAlive=true`, and `launchctl bootstrap`s it. Daemon starts on every login, paused on sleep.

Total: 3 user-visible interactions, ~2 min. Token-pasting is the only keyboard step.

### 6.2 Normal operation (after pair, forever)

- User opens Telegram, taps the bot chat, reads/responds. Zero auth.
- Owner check on every inbound update is invisible — it's just `update.from.id == owner_id`.
- No TOTP prompt. No re-pair. No expiry.

### 6.3 Auto-detect agents and install hooks (during setup)

After pairing succeeds, `onibi setup` runs `onibi install-hooks --interactive`:
- Detects installed agents (`which claude`, `which codex`, `which opencode`, `which goose`, etc.).
- For each, asks "Install Onibi hooks for <agent>? [Y/n]".
- Writes guarded block to the agent's settings file (idempotent — re-runs are no-ops if block already present and unchanged; updates block in place if version drift detected).
- Records hook hashes in SQLite for tamper detection (see §7.3).
- Prints summary: "Monitoring: claude, codex, opencode. Run `onibi doctor` to verify."

### 6.4 `onibi doctor`

Health + integrity check, colored pass/fail for:
- Bot token: present in Keychain (or `.env` 0600), `getMe` succeeds
- Owner chat_id: present, not zero
- State dir: exists, permissions `0700`
- Unix socket: exists, permissions `0600`, daemon listening
- LaunchAgent (macOS) / systemd user unit (Linux): loaded + running
- For each installed adapter: hook file present + executable + hash matches expected (tamper check)
- Telegram reachable: `getUpdates` with offset 0 succeeds
- Telegram 2FA status: bot can't directly check user's account 2FA, but warns "Have you enabled Telegram 2-step verification? [Y/n]" annually
- TOTP: if enabled, secret present in Keychain; if disabled, prints "Optional second factor not enabled (see `onibi setup --enable-totp`)"

### 6.5 Re-pair / rotate / recover

- `onibi setup --rotate-owner` — invalidates current owner, generates new pairing payload. Used if user changes Telegram accounts or suspects compromise.
- `onibi rotate-token` — walks user through @BotFather `/revoke`, pastes new token, replaces in Keychain. Bot username unchanged. Old token immediately invalid on Telegram's side.
- `onibi setup --enable-totp` — adds TOTP gating to destructive commands (`/kill`, `/interrupt`, `/send` raw); approve/deny stays unauthenticated. Generates secret + QR.
- `onibi setup --paranoid` — enables TOTP + shortens approval expiry to 60s + requires confirm-tap on every preset action.

---

## 7. Security model (read before touching any auth code)

This product gives a Telegram chat the ability to inject text into a terminal. That is a remote shell with extra steps. The threat model must be explicit and the mitigations enforced. Pair-once UX does not mean weak security — it means we move the friction to one-time hardening.

### 7.1 Threat model

| # | Adversary | Capability | Likelihood | Severity | Mitigation status |
|---|---|---|---|---|---|
| T1 | Bot token thief (reads `.env`, env, process memory, dumped Keychain) | Full Telegram bot impersonation; can `getUpdates` (race the real daemon out of messages), `sendMessage` as the bot, switch to webhook (we'll detect & switch back), spoof inline keyboards | Med | High | Keychain storage by default; redaction filter on logs; `onibi doctor` warns if `.env` is the storage backend; rotate via `onibi rotate-token`; `getUpdates` race detected by liveness check (§7.6) |
| T2 | Owner's Telegram account compromised (SIM swap, OTP-harvesting bot, phishing, malicious enhanced Telegram client) | Becomes the owner — full remote shell via send-keys, send Ctrl-C, kill sessions, approve any tool call | **High in 2026 — see references** | Critical | Setup mandates Telegram 2-step verification acknowledgment; opt-in TOTP for destructive ops; approval expiry 5 min; audit log of every action; `--paranoid` mode |
| T3 | Local malware running as user | Reads SQLite, Keychain (after first unlock), injects to Unix socket, edits hook scripts | Low (requires prior compromise) | Critical | Out of scope for v1. Documented limitation. Hook hash verification gives detection signal. |
| T4 | Network MITM between daemon and api.telegram.org | Read/modify traffic | Very low (corporate TLS-intercept proxy possible) | Med | stdlib TLS with system roots; HSTS; explicit refusal to use HTTP_PROXY for api.telegram.org by default |
| T5 | First-message-becomes-owner race (attacker guesses bot username before owner pairs) | Becomes owner | Low (if username is unguessable) | Critical | **Deeplink pairing flow eliminates this entirely.** No fallback to first-message ownership. |
| T6 | Bot username squatting / typosquatting | Phishing future users | Low | Med | Recommend random username suffix in setup wizard |
| T7 | Stale-approval hijack (owner's Telegram compromised days after a request is queued) | Approve old requests | Low | Med | Approval expiry 5 min hard limit |
| T8 | Replay of decided approvals | Re-execute via callback duplication | Low | Low | Approval state machine — terminal states are final; idempotent decide |
| T9 | Hook script tampering (malware edits `~/.claude/settings.json` to redirect notify to attacker socket) | Silent capture of approvals + agent state | Low | High | `onibi doctor` records and checks hook hashes; reinstall on mismatch |
| T10 | Edited-JSON injection via compromised owner | Pass arbitrary args to agent tool | Linked to T2 | High | Schema validation per-tool; audit log; approval expiry; `--paranoid` requires TOTP per edit |
| T11 | Unix socket impersonation by other local user | Inject fake events | Very low (single-user macOS/Linux laptop) | Med | Socket perms 0600 in 0700 state dir; verify peer creds (`SO_PEERCRED` on Linux, `LOCAL_PEEREPID` on macOS) where supported |
| T12 | Setup wizard token paste captured by clipboard manager / shoulder surf | Token leak | Med | High | Wizard offers paste-then-clear; prints reminder; supports stdin pipe (`pbpaste \| onibi setup --token-stdin`) |
| T13 | Bot token committed to user's own repo accidentally | Public leak → T1 | Med | High | `.gitignore` ships with `.env`; pre-commit hook template; storing in Keychain by default sidesteps the .env path entirely |
| T14 | Telegram 2FA SMS-OTP intercepted (SIM swap) | T2 | Med (rising — see Intel 471, Proton) | Critical | Recommend **email-based 2FA recovery** (not SMS) at setup; setup wizard prints a one-screen guide |

### 7.2 Trust boundaries

```
   ┌────────────────────────────────────────────────────────┐
   │ Telegram cloud                                         │
   │  - holds bot token (cannot revoke from outside)        │
   │  - delivers messages from owner Telegram account       │
   │  - is the principal authenticator (chat_id is identity)│
   └─────────────────────────┬──────────────────────────────┘
                             │ HTTPS outbound only
                             │
   ┌─────────────────────────▼──────────────────────────────┐
   │ Daemon (trust boundary 1)                              │
   │  - holds bot token (Keychain preferred)                │
   │  - holds owner chat_id                                 │
   │  - holds approval queue                                │
   └──┬──────────────────────────────────────────┬──────────┘
      │ Unix socket 0600                         │ stdin/stdout/pty
      │                                          │
   ┌──▼────────────┐                  ┌─────────▼────────────┐
   │ Hooks         │                  │ Hosted agents        │
   │ (claude/      │                  │ (claude code,        │
   │  codex/       │                  │  codex, opencode…)   │
   │  shell)       │                  │  — fully trusted     │
   │ — trusted via │                  │  (user spawned)      │
   │  hash check   │                  └──────────────────────┘
   └───────────────┘
```

### 7.3 Concrete enforcements (must-do, not nice-to-have)

- **Owner check (`auth.MustBeOwner`):** every Telegram update goes through one chokepoint. `update.from.id != owner_id` → drop. No exceptions. Includes callback queries (`callback_query.from.id`).
- **Approval state machine:** states are `pending → decided{approved|denied|edited} | expired | cancelled`. Transitions are atomic SQLite updates with `WHERE state='pending'` guards. Idempotent.
- **Constant-time pairing token compare:** `subtle.ConstantTimeCompare` for the deeplink payload check. Timing-safe.
- **Single-use pairing payload:** SQLite UNIQUE constraint + atomic UPDATE that sets `consumed=1` and checks affected rows = 1. Reject if not.
- **Pairing payload TTL:** 5 min. After that the row is unusable.
- **Bot token via Keychain by default:** `internal/secrets/keychain.go` wraps `99designs/keyring`. `.env` only as fallback, always 0600, in 0700 state dir, with a setup-time warning.
- **Token never in logs:** `internal/logging/redact.go` is a `slog.Handler` middleware that scans every record for any string equal to the loaded token and replaces with `[REDACTED]`. Tests for this.
- **Token never in argv:** subcommands accept token via env / Keychain / `--token-stdin`. Never positional or `--token=`.
- **Token never in error messages:** wrap-and-redact at every error boundary that touches the Telegram client.
- **Unix socket perms:** create with explicit `chmod 0600` after bind (umask is unreliable). State dir `chmod 0700` on creation. Refuse to start if perms loosened.
- **Peer credential check on intake:** on macOS use `getsockopt(LOCAL_PEEREPID)` to read connecting PID; verify the PID's effective UID == ours. Reject otherwise. Same on Linux via `SO_PEERCRED`.
- **Approval expiry:** background sweeper marks pending approvals as `expired` after 5 min and notifies the agent's adapter to default-deny.
- **Hook hash registry:** on install, record `sha256(hook_script)` in SQLite keyed by `(agent, path)`. `onibi doctor` compares; mismatch is a red error.
- **HTTP client:** dedicated `*http.Client` with `Timeout: 35s` (covers long-poll 30s + slack), `Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}`. No `InsecureSkipVerify`. No `HTTP_PROXY` honored for api.telegram.org (explicit `Proxy: func(*http.Request) (*url.URL, error) { return nil, nil }` for telegram client) unless `--allow-proxy` set.
- **getUpdates race detection:** if `getUpdates` returns 0 updates for 10 consecutive polls despite the daemon having sent recent messages (and they show as "seen" via `editMessageReplyMarkup` etc.), warn: "Possible token compromise: another client may be consuming bot updates. Run `onibi rotate-token`."
- **Audit log:** every action (approval decided, text injected, session command) written to `audit` table with `ts, action, session_id, payload_hash, decided_by_chat_id`. Reviewable via `/log` and exportable via `onibi log --export`.
- **Approval message scrubbing:** redact any field that looks like a token, password, or env var value from rendered tool inputs before sending to Telegram (regex-based, conservative; user can opt out per-session with `/show-secrets`).
- **Owner re-confirmation on dangerous defaults:** if user runs `onibi setup --rotate-owner`, daemon sends a message to the **current** owner first ("Setup is rotating owner — current owner will lose access. Confirm with `/confirm-rotate <code>`"). Prevents a local attacker who reached the terminal from silently swapping owner.
- **Pairing requires no existing owner OR explicit rotation:** if owner is already set, `/start pair_*` deeplinks are ignored. Only `--rotate-owner` flow can re-pair.
- **Build provenance:** GoReleaser-built binaries are reproducible; release artifacts include SHA256 checksums; Homebrew formula pins to a specific tag + sha256.

### 7.4 What we explicitly do NOT defend against

State up front in README and `docs/security.md`:
- Local malware running as the same user (T3). At that point your machine is owned.
- Compromise of the user's Telegram account (T2). Mitigated by mandatory Telegram 2FA recommendation and optional TOTP, but if the user's Telegram is fully owned, so is the daemon.
- Apple Notarization compromise (we trust Apple).
- Telegram cloud confidentiality. Telegram sees every message. Use the optional E2E content envelope (§9.4) if you need defense from Telegram itself.
- Insider attacks on Anthropic's / OpenAI's / OpenCode's hosted agent APIs (out of scope).

### 7.5 Setup security checklist (rendered by `onibi setup --print-checklist` and `onibi doctor`)

- [ ] Bot username is unguessable (≥4 random chars in suffix)
- [ ] Bot token stored in Keychain (not `.env`)
- [ ] Telegram 2-step verification enabled on owner account
- [ ] Telegram 2FA recovery is email, not SMS
- [ ] LaunchAgent loaded (`launchctl list | grep sh.onibi`)
- [ ] State dir is `0700`; socket is `0600`
- [ ] Hook hashes match registry (no tampering)
- [ ] Last token rotation within 6 months

### 7.6 Operational invariants (the daemon enforces these continuously)

- If owner_id is unset, daemon refuses to start `run` mode; redirects to `onibi setup`.
- If bot token validates but `getMe` returns a different bot_id than last seen, refuse to start (someone reused the persisted owner with a different bot — possible attack or misconfiguration).
- If pairing payload is consumed → silently ignore further `/start pair_*` until next `--rotate-owner`.
- If daemon detects webhook is set on the bot (someone called `setWebhook` with a non-empty URL), call `deleteWebhook` immediately and alert owner: "Webhook was set externally — possible token compromise."
- If two consecutive `getMe` calls succeed but with `username` change, alert owner: "Bot username changed externally."

---

## 8. Phase plan (replaces all PHASE-XX docs)

Solo full-time, ~12 weeks budget, ~6–8 weeks expected.

### Phase 0 — Repo reset & scaffold (3–5d) — **DONE 2026-06-10**

- [x] Archive current Rust + TS code:
  - [x] `git mv app docs/archive/v1.5-rust/app`
  - [x] `git mv mobile docs/archive/v1.5-rust/mobile`
  - [x] `git mv adapters docs/archive/v1.5-rust/adapters` (entire dir; new templates created from scratch under `adapters/<agent>/README.md`)
  - [x] `git mv scripts docs/archive/v1.5-rust/scripts`, `git mv script docs/archive/v1.5-rust/script`, `git mv output docs/archive/v1.5-rust/output`
  - [x] (No `PHASE-*.md` at repo root — already lived under `docs/archive/v1-phases/`)
  - [x] `git mv TO-IMPLEMENT-PIVOT-4-JUN.md docs/archive/v1.5-rust/`
  - [x] `git mv DESIGN.md docs/archive/v1.5-rust/`
  - [x] `git mv package.json pnpm-lock.yaml pnpm-workspace.yaml rust-toolchain.toml Makefile docs/archive/v1.5-rust/`
  - [x] `git mv .github/workflows/ci.yml release.yml docs/archive/v1.5-rust/` (with `.bak` suffix; v2 workflows replace them)
  - [x] Archive v1.5-specific docs: `adapters.md`, `architecture.{excalidraw,png}`, deployment-*, install-*, blog/, launch/, phase-01-smoke/, policies.md, security.md (will rewrite phase 10), transports.md, troubleshooting.md, updating.md, all tunnel/cert PNGs
  - [x] Archive `packaging/` (v1.5 homebrew + systemd; phase 9 regenerates via GoReleaser)
  - [x] New `README.md` written — Telegram-first, references TODO-10-JUN.md as source of truth
  - [x] New `Makefile` written — `build`, `install`, `test`, `vet`, `staticcheck`, `tidy`, `run`, `clean`, `release-dry`
  - [x] `docs/archive/v1.5-rust/README.md` written explaining the archive
- [x] Extract patterns from `tgterm/` (no C vendored):
  - [x] `docs/tgterm-patterns.md` written: TOTP, KV schema, owner registration (replaced), QR rendering, getUpdates loop, callback dispatch, image+keyboard, connection state, command style, HTTP options, token lifecycle, what we drop, what we add, license notice
  - [x] `rm -rf tgterm/` (untracked dir; patterns preserved in `docs/tgterm-patterns.md`)
- [x] Initialize Go module: `go mod init github.com/gongahkia/onibi`
- [x] Add deps (`go get`): `github.com/spf13/cobra`, `github.com/go-telegram/bot`, `github.com/creack/pty`, `github.com/jaguilar/vt100`, `modernc.org/sqlite`, `github.com/skip2/go-qrcode`, `github.com/99designs/keyring`, `gopkg.in/yaml.v3`, `github.com/joho/godotenv`
- [x] Scaffold directory tree per §4 (`cmd/onibi`, `internal/{cli,daemon,intake,pty,tmux,approval,adapters/{claude,codex,opencode,goose,shell},telegram,render,auth,secrets,store,config,service,setup,logging}`, `adapters/{claude-code,codex,opencode,goose}`, `hooks/{zsh,bash,fish}`, `clients/onibi-notify`, `scripts/`)
- [x] `cmd/onibi/main.go` — cobra root delegating to `internal/cli.Root()`
- [x] `internal/cli/{root.go,stubs.go}` — all subcommands registered, return `errNotImplemented` until their phase lands; `version` works
- [x] Per-package `doc.go` stubs naming the phase that implements each package
- [x] `clients/onibi-notify/main.go` — fail-open stub
- [x] `Makefile`: `build`, `install`, `test`, `vet`, `staticcheck`, `tidy`, `run`, `clean`, `release-dry`
- [x] `.goreleaser.yaml` stub: macOS + Linux × amd64 + arm64, CGO_ENABLED=0 (pure Go SQLite works without cgo), notarize + brews stanzas commented for phase 9
- [x] `.github/workflows/{ci.yml,release.yml}` — go test + vet + staticcheck on push/PR; goreleaser on tag
- [x] `.gitignore` rewritten for Go + explicit secret exclusions (.env, *.token, *.totp, *.sqlite)
- [x] `go vet ./...`, `make build`, `./bin/onibi --help`, `./bin/onibi version`, `go test ./...` all green

### Phase 1 — Telegram outbound + secrets + pair-once setup wizard (7–10d) — **CODE DONE 2026-06-10; awaiting user manual e2e**

- [x] `internal/secrets/secrets.go` — wraps `github.com/99designs/keyring`; macOS Keychain (`KeychainAccessibleWhenUnlocked`) primary, `.env` fallback with 0600 + state dir 0700; explicit warning at startup when degraded to dotenv
- [x] `internal/logging/redact.go` + `internal/logging/redact_test.go` — `slog.Handler` middleware that redacts loaded secrets from message + string attrs + error attrs; unit-tested across all paths; ignores secrets <12 chars to avoid false positives
- [x] `internal/telegram/client.go` — wraps `go-telegram/bot`; dedicated `*http.Client` with TLS 1.2+ floor, 35s timeout, refuses HTTP_PROXY for api.telegram.org by default, no `InsecureSkipVerify`; defensive `DeleteWebhook(DropPendingUpdates: true)` on startup; `getMe` populates `Self`
- [x] sendMessage available via embedded `*tgbot.Bot`; sendPhoto/editMessageReplyMarkup/answerCallbackQuery exposed the same way (no need to re-wrap — `go-telegram/bot` covers them). Owner-check middleware deferred to Phase 3 when callback routing lands.
- [x] `internal/auth/owner.go` + `_test.go` — atomic int64 owner; `MustBeOwner` is lock-free + constant-time; `LoadOwner`/`SetOwner`/`IsOwnerSet` storage helpers
- [x] `internal/auth/totp.go` + `_test.go` — RFC 6238, ±1 window verify with branch-free compare; RFC 6238 Appendix B test vectors pass; opt-in only (`--enable-totp` / `--paranoid`)
- [x] `internal/store/sqlite.go` + `_test.go` — `modernc.org/sqlite` (pure-Go); WAL + foreign_keys + busy_timeout pragmas; schema v1 covers: kv, pairing_tokens, approvals, audit, hooks, sessions; 0600 perms enforced; concurrent pair-race test proves "exactly one consumer wins"
- [x] `internal/setup/pairing.go` + `_test.go` — 32 random bytes → base64url → 43-char token; `PutPairingToken(ttl=5min)`; `Consume` is atomic `UPDATE ... WHERE token=? AND consumed=0 AND expires_at > ?` — single-use enforced; unknown / consumed / expired all return `ErrPairExpired` so caller can't distinguish (no info leak)
- [x] `internal/setup/qr.go` — half-block ASCII QR renderer ported from tgterm/bot.c:129-156 (uses `skip2/go-qrcode` for matrix)
- [x] `internal/setup/wizard.go` + `getchatid.go` + `rotate.go` + `suggest.go` — interactive wizard per §6.1: token paste, getMe, store token in Keychain, persist `bot_id`, mint pairing token, print deeplink + QR, long-poll handler with constant-time token compare (`subtle.ConstantTimeCompare`), atomic Consume, set owner, optional TOTP, mandatory Telegram 2FA acknowledgment gate (must type `enabled` or `skip`); username suggestion is `onibi_<8hex>_bot` to mitigate T6
- [x] `internal/cli/setup.go` + wired stubs — `onibi setup` flags: `--rotate-owner`, `--enable-totp`, `--paranoid`, `--print-checklist`, `--token-stdin`; `onibi get-chat-id` fallback; `onibi rotate-token` with bot_id identity check
- [x] `internal/config/paths.go` — platform-aware state/socket/db/env paths; `EnsureDirs` creates with 0700 and refuses loose perms; `EnvFilePerms` enforces 0600
- [x] Constant-time compare unit-tested (pair-payload via `subtle.ConstantTimeCompare`); single-use enforced (concurrent test: exactly 1 winner from 20 goroutines); expired payload rejected; non-owner check tested; token redaction verified across message, string attr, and error attr
- [x] `scripts/manual-e2e-pair.md` — step-by-step manual end-to-end procedure (requires real BotFather bot; ~3 min)
- [ ] **MANUAL**: Run `scripts/manual-e2e-pair.md` against a real BotFather bot. Owner permanently stored, re-run without `--rotate-owner` fails, `rotate-token` refuses different-bot token, expired pair link rejected. (Requires user — cannot be automated.)

### Phase 2 — PTY host + Claude adapter + text renderer (5–7d) — **CODE DONE 2026-06-10; awaiting user manual e2e**

- [x] `internal/pty/host.go` + `_test.go` — `creack/pty` wrapper: SpawnOptions (name/args/env/cwd/rows/cols/replace-env), bidirectional Master, Wait/Pipe/Write/Close; tested with /bin/echo, /bin/cat, error paths
- [x] `internal/daemon/buffer.go` + `_test.go` — concurrent ring buffer; tested below-cap, wrap, multi-wrap, reset, 20-goroutine concurrent writes + readers
- [x] `internal/daemon/registry.go` + `_test.go` — Session (Touch/SinceActivity/MarkEnded) + Registry (Add/Get/Remove/List/sorted-by-start) + `NewID` (8-byte hex); 50-goroutine concurrent add test, 1000-id uniqueness test
- [x] `internal/render/text.go` + `_test.go` — `TextTail` (fenced code block, MaxLines/MaxChars/Lang options), `StripANSI` (CSI + OSC + character-set sequences + bare \\r), UTF-8-safe head-truncation marker; tested ANSI strip, OSC, cursor moves, empty, fencing, max-lines, lang hint
- [x] `internal/adapters/claude/install.go` + `_test.go` — `Install`/`Uninstall`/`VerifyHash` against `~/.claude/settings.json`; idempotent guarded-block merge that preserves user-managed Stop entries; sha256 recorded in `hooks` table; tamper-detection test passes
- [x] `clients/onibi-notify/main.go` — fail-open contract: ONIBI_SOCK + ONIBI_SESSION_ID from env; flags `--type`, `--status`, `--cmd`, `--elapsed-ms`, `--text`, `--tail-stdin`, `--approval-id`, `--tool`; exits 0 silently on every error path
- [x] `internal/intake/event.go` — JSON wire schema (Event struct) for agent_done, agent_awaiting, agent_message, cmd_done, session_exited, approval_request
- [x] `internal/intake/server.go` + peercred_darwin.go + peercred_linux.go + client.go + `_test.go` — Unix socket 0600 perms (umask-resistant, post-bind chmod), `DisallowUnknownFields` JSON decode, peer-cred verification (`Xucred` on macOS, `Ucred` on Linux via `golang.org/x/sys/unix`), 5s deadline; tested: roundtrip, send-fails-open, malformed-rejected, empty-type-rejected
- [x] `internal/daemon/turn_complete.go` — `IdleDetector` with per-session "fired" dedup map; clears on next Touch so subsequent active periods refire; configurable threshold (default 3s) + interval (default 500ms)
- [x] `internal/daemon/daemon.go` — orchestrator: `SpawnAgent` sets ONIBI_SOCK + ONIBI_SESSION_ID env, reads PTY into ring buffer + mirrors to user's tty; `handleEvent` dispatches intake events; once-per-active-period dedup so hook + idle-fallback don't double-fire; SIGINT/SIGTERM clean shutdown
- [x] `internal/cli/run.go` — `onibi run [agent [args...]]`: loads paths/db/secrets/owner/bot/daemon; spawns agent under PTY with `golang.org/x/term` raw-mode stdin pass-through + SIGWINCH resize via `creack/pty.InheritSize`; daemon-only mode when no args
- [x] `internal/cli/hooks.go` — `onibi install-hooks --agent <name>` and `--shell <name>` resolve `onibi-notify` via ONIBI_NOTIFY_BIN env / sibling-of-onibi / PATH lookup; adapter registry covers claude/codex/opencode/goose/gemini/copilot/pi/amp plus shell hooks
- [x] `scripts/manual-e2e-claude-run.md` — step-by-step procedure (~5 min): `make install` → `onibi install-hooks --agent claude` → `onibi run claude` → prompt → Telegram message
- [ ] **MANUAL**: Run `scripts/manual-e2e-claude-run.md`. Start Claude session under `onibi run claude`, type a prompt, verify Telegram message arrives on turn complete (primary hook path) and idle-fallback works if hook is removed. (Requires user — cannot be automated.)

### Phase 3 — Blocking approval protocol + audit log (5–7d)

- [x] `internal/approval/queue.go` — SQLite-backed queue, request/decide/cancel, state machine (`pending → decided{approve|deny|edit} | expired | cancelled`), atomic transitions
- [x] `internal/approval/expiry.go` — background sweeper marks pending → expired after 5 min; notifies adapter to default-deny
- [x] `internal/approval/scrub.go` — conservative regex-based redaction of tokens/passwords/env values in rendered tool inputs before send
- [x] `internal/telegram/keyboards.go` — `[Approve] [Deny] [Edit]` inline keyboard builder
- [x] `internal/telegram/router.go` — callback query dispatcher, owner-checked, maps callback_data → approval_id → decision
- [x] `internal/adapters/claude/pretooluse.go` — handler for blocking, returns updatedInput / deny / allow
- [x] `internal/store/audit.go` — write `(ts, action, session_id, payload_hash, decided_by_chat_id)` for every decision
- [x] On block: send Telegram message with tool name + scrubbed inputs + keyboard
- [x] On callback: resolve, unblock, edit message to show outcome (and decision in audit log)
- [ ] Test: Claude tries `Bash` tool → blocks → Telegram message appears → tap Approve → tool runs.
- [x] Test: kill daemon mid-approval, restart, pending approval reappears. (unit-covered; manual daemon kill still recommended)
- [x] Test: approval older than 5 min auto-expires; late callback rejected with "Expired" toast. (unit-covered)
- [x] Test: callback from non-owner chat dropped without state mutation. (unit-covered)

### Phase 4 — Edit-before-approve (2–3d)

- [ ] On `[Edit]` callback: edit message to "Reply to this message with new JSON args"
- [ ] Reply handler parses JSON, validates against tool input schema (per-adapter), returns as `updatedInput`
- [ ] Malformed JSON → reply "Invalid JSON: <err>. Try again or tap Approve/Deny."
- [ ] Schema mismatch → reply "Doesn't match tool schema: <err>."
- [ ] In `--paranoid` mode: require TOTP code in the reply (`<JSON>\n<6-digit>`)
- [ ] Edited payload also written to audit log with original-vs-edited diff hash
- [ ] Test: edit `rm -rf tests/legacy` → `mv tests/legacy tests/legacy.bak` → Claude receives updated input.

### Phase 5 — Auto-switch renderer (PNG path) (5–7d)

- [x] `internal/render/png.go` — feed PTY bytes into `jaguilar/vt100`, walk cell grid, rasterize via `image.NewRGBA` + stdlib `image/png`
- [x] Use Go `basicfont` fixed-width face (chosen over embedded OFL font for this pass)
- [x] ANSI color palette (16 colors + 256-color extension)
- [x] `internal/render/switch.go` — heuristic: if buffer contains `\x1b[H` / `\x1b[?1049h` / `\x1b[s` cursor save / multiple `\x1b[<n>;<m>H` → PNG; else text
- [x] `/text` and `/screenshot` commands force one or the other (runtime per-session sticky)
- [x] Test: text mode for `npm test` output; PNG mode for Claude Code TUI.

### Phase 6 — Multi-session + reply-to-message routing (3–5d) — **CODE DONE 2026-06-10; awaiting live Telegram e2e**

- [x] `internal/daemon/threading.go` — store `message_id → session_id` for outbound session messages
- [x] On inbound reply: look up parent message_id → resolve session → write to that PTY
- [x] Free text without reply → write to sole/default active session; ambiguous → inline keyboard "Pick session"
- [x] `/sessions` lists all active sessions with name + running command
- [x] `/target <name>` sets default session for non-reply inputs
- [x] `/status` shows daemon uptime, queue depth, all sessions
- [x] `/new <agent>` spawns new agent session from Telegram

### Phase 7 — Tmux attach backend (3–5d)

- [ ] `internal/tmux/attach.go` — `--attach-tmux <session>` mode
- [ ] `tmux list-panes -a -F …` for discovery; `send-keys -l` for injection; `capture-pane -p -S -50` for output
- [ ] Bracketed-paste mitigation: known issue with multi-line + extended-keys; verify-and-retry loop after dispatch (capture-pane, check, resend Enter if needed)
- [ ] Coexists with PTY backend; per-session "transport: pty | tmux"

### Phase 8 — More adapters (5–7d) — **CODE DONE 2026-06-10; awaiting live provider e2e**

- [x] Codex CLI adapter (SessionStart, PreToolUse, PostToolUse, Stop)
- [x] OpenCode adapter (`tool.execute.before` + lifecycle bridge)
- [x] Goose adapter
- [x] Gemini, Copilot, Pi, Amp adapters
- [x] Shell hooks: zsh `precmd`/`preexec` (opt-in), bash `PROMPT_COMMAND`, fish `fish_postexec`
- [x] `onibi install-hooks --agent <name>` and `--shell <name>` idempotent installers
- [x] Capability matrix in docs
- [ ] **MANUAL**: live e2e against installed Codex/OpenCode/Goose/Gemini/Copilot/Pi/Amp CLIs; provider hook APIs can drift.

### Phase 9 — LaunchAgent + systemd + signed distribution (5–7d)

- [x] `internal/service/launchd.go` — generate `sh.onibi.daemon.plist`; `launchctl bootstrap`/`bootout`; `RunAtLoad=true`, `KeepAlive=true`, `ProcessType=Interactive`
- [x] `internal/service/systemd.go` — generate `~/.config/systemd/user/onibi.service`; `systemctl --user enable --now onibi`
- [x] `onibi install-service` / `onibi uninstall-service`
- [x] `onibi doctor` initial implementation: token, owner, state perms, socket, service, hooks, TOTP, 2FA ack, live Telegram `getMe` + non-destructive `getUpdates` probe (`--offline` skips network)
- [x] `internal/telegram/mock.go` + daemon API seam for deterministic CI coverage of setup, router, and approval flow
- [x] Daemon-only `onibi run` lifecycle fixed for LaunchAgent/systemd: stays alive until signal; interactive agent runs still exit when hosted sessions end
- [ ] Apple Developer ID Application certificate obtained ($99/yr, one-time setup)
- [x] `.goreleaser.yaml` integrated with env-gated GoReleaser macOS binary signing + notarization stanzas
- [ ] Hardened Runtime enabled in entitlements
- [x] `.goreleaser.yaml` complete: macOS arm64+x86_64 (signed+notarized when Apple env vars are set), Linux arm64+x86_64
- [ ] Release artifacts include SHA256 checksums file, signed with gpg key (release-key in `docs/security.md`)
- [ ] Homebrew tap repo `homebrew-onibi` set up; GoReleaser pushes formula with pinned tag+sha256 on release
- [ ] Install script `curl -fsSL https://onibi.sh/install.sh | bash` for Linux (sha256-verified)
- [ ] Test: download release on a clean Mac, double-click → Gatekeeper allows; install via brew → no permission popups; `onibi doctor` clean

### Phase 10 — Hardening, polish, launch (5–7d)

- [x] Rate limiter (token-bucket, 1 msg/sec/chat soft cap, 25 msgs/sec global)
- [ ] Long-output chunking (Telegram 4096-char per text message; switch to `sendDocument` for >5 messages worth)
- [ ] Sleep/wake recovery — long-poll auto-resumes; queue replay for missed events; resync owner_id check after wake
- [ ] `getUpdates` race detector (warns owner if 10 consecutive empty polls despite expected traffic)
- [ ] Defensive `deleteWebhook` on every startup + alert if pre-existing webhook found
- [ ] Bot identity check: `getMe` on startup, compare `id` against stored value, refuse to start if mismatched
- [ ] `onibi doctor` complete per §6.4
- [x] `onibi log` (last N audit events from SQLite); `onibi log --export <file>`
- [x] `/snooze 30m`, `/snooze claude`, `/unsnooze`
- [x] `setMyCommands` registered with Telegram so `/sessions`, `/status`, `/help`, `/snooze`, `/new`, `/log` appear in Telegram's native command UI
- [x] `docs/security.md` written: threat model (copy §7.1), enforcements (copy §7.3), non-defenses (copy §7.4), checklist (copy §7.5), responsible disclosure contact
- [ ] README rewrite (Telegram-first, drop PWA/Tauri references, include security model summary + 2FA call-out)
- [ ] Screencast demo (90s: brew install → pair → enable 2FA → Claude task → approve from phone)
- [ ] HN/blog launch with explicit framing: "v1.5 archived; this is the right shape per tgterm validation; here's the threat model"

---

## 9. Polish / nice-to-have / future (not in v2 critical path)

Listed for explicit visibility — don't expand scope of phases unless agreed.

### 9.1 Setup seamlessness
- [ ] Auto-detect installed agents and offer hook install in `onibi setup` (already in Phase 6.2)
- [ ] `onibi setup --import-bot <token>` for users who already have a bot
- [ ] Recovery mode: if `mybot.sqlite` deleted, `onibi setup --rotate` regenerates everything but re-uses the same Telegram bot (only TOTP + owner rotate)
- [ ] Tray-icon-less status indicator: `onibi status` prints a one-line summary; users alias to their shell prompt if they want
- [ ] Telegram `setMyCommands` so `/sessions`, `/status`, `/snooze`, `/new`, `/help` show up in Telegram's command suggestions UI
- [ ] Send a one-time welcome message on first pair with a 30-second tour

### 9.2 Multi-machine
- [ ] Same user with 2+ Macs / one Mac + one Linux box: one bot, machine-label routing
- [ ] `onibi setup` asks "Machine label" (default: hostname)
- [ ] Outbound messages prefixed with `[label]`; inbound `/target <label>:<session>` syntax
- [ ] `/machines` lists all paired hosts

### 9.3 Voice & multimodal
- [ ] Voice message → Whisper.cpp local → inject as text (works offline; uses small model)
- [ ] Image reply → forward as file to agent if agent supports image input

### 9.4 Security (deeper, beyond what §7 already mandates)
- [ ] E2E content envelope: wrap sensitive message payloads in NaCl `secretbox` with a key derived during deeplink pair (the deeplink payload also seeds an HKDF for an envelope key both sides keep). Telegram sees only ciphertext. Optional, off by default. Solves T4 + Telegram-server-side confidentiality.
- [ ] Hardware-backed Keychain entry (`kSecAttrAccessibleWhenUnlockedThisDeviceOnly`) — token never syncs to iCloud Keychain
- [ ] Per-tool risk classifier — destructive tools (`Bash` with `rm`, `git push --force`, etc.) get extra-prominent UI in Telegram and optional confirm-tap even when not in `--paranoid` mode
- [ ] Anomaly detection: alert owner if approval rate suddenly spikes (possible automated abuse), or if commands from owner come from a new Telegram client (`from.is_premium`, `from.language_code` changes)
- [ ] Optional WebAuthn second factor via a phone-side mini-PWA (later; would require the very infra we just deleted, so unlikely)
- [ ] `onibi audit` command: export audit log as CSV/JSON for compliance use
- [ ] SBOM generation in release pipeline (Syft); attach to GitHub releases
- [ ] Reproducible builds verification job in CI

### 9.5 UX
- [ ] Session naming (`onibi run --name fix-tests claude -- …`)
- [ ] Color theme for PNG render (light/dark, configurable palette)
- [ ] Inline session preview in Telegram: `/peek <name>` sends current screenshot
- [ ] `/interrupt <name>` sends Ctrl-C to a session
- [ ] `/kill <name>` terminates a session
- [ ] Preset menu (`/menu`) with allowlisted quick actions per agent ("yes", "Continue", "Skip tests", "Run again")
- [ ] Telegram MarkdownV2 formatting for tool name + args
- [ ] Approval edit history: tap "Edit" multiple times, keep last attempt

### 9.6 Observability
- [ ] `--debug` flag; structured logs via `log/slog`
- [ ] Daily rotating log in state dir
- [ ] `onibi tail-log` convenience subcommand
- [ ] Optional Prometheus textfile for self-monitoring (off by default)

### 9.7 Distribution
- [ ] Homebrew core formula (after enough stars / stability)
- [ ] Nix flake
- [ ] AUR package
- [ ] Apt/deb via GoReleaser nfpm
- [ ] Windows: out of scope for v2; document.

### 9.8 Agent breadth
- [ ] Aider, Cursor, Pi, Gemini, Hermes, Copilot CLI — port whatever was working in v1.5
- [ ] MCP server adapter (any MCP-capable agent gets free coverage via the MCP protocol)
- [ ] Generic "wrap any TUI" mode: spawn under PTY, screenshot on idle, accept text-in (works for `gh issue list`, `lazygit`, etc.)

---

## 10. Project risks (security threats are in §7; this is product/delivery risk)

| Risk | Likelihood | Severity | Mitigation |
|---|---|---|---|
| Telegram regional block (RU/CN) excludes users | Med | Med | Document. Add Signal/Matrix transport later if demanded. |
| Third pivot kills credibility | High | High | Frame v2 release notes as "validated by tgterm's reception; v1.5 work archived in repo." Don't hide it. |
| Edit-before-approve UX is clumsy in chat | High | Low | Acceptable — that's why desktop modal existed. Document and move on. |
| Telegram 4096-char message limit clips terminal output | High | Low | Chunk + auto-switch to `sendDocument` for long buffers. |
| Telegram rate limits (30 msgs/sec global) | Low | Med | Already capped via debounce + per-chat token bucket. Won't hit in single-user mode. |
| tmux send-keys multi-line bracketed-paste breakage | Med | Med (only affects tmux backend) | Verify-and-retry loop. PTY default backend avoids entirely. |
| Codex CLI hooks only fire for Bash, not apply_patch/MCP | Confirmed | Med | Document gap in capability matrix. Not solvable from our side. |
| modernc.org/sqlite slower than mattn/go-sqlite3 | Confirmed | Low | At KV+approval scale, irrelevant. Worth it for pure-Go cross-compile. |
| jaguilar/vt100 isn't actively maintained / missing edge cases | Med | Med | Fall back to text-only if PNG render fails; investigate `tcell/v2` cell grid as alternative. |
| LaunchAgent doesn't run if user disables it in System Settings → Login Items | Med | Low | `onibi doctor` detects + warns. |
| Bot token leak via committed `.env` | Med | High | Keychain is default storage; `.env.example` shipped; `.env` in `.gitignore`; pre-commit hook in repo template; log redaction filter (§7.3). |
| Apple Developer ID account loss / suspension | Low | High | Have backup contact on account; release artifacts also published unsigned with sha256 + gpg signature for users willing to bypass Gatekeeper. |
| Linux Secret Service not available (headless server / minimal distro) | Med | Low | Fall back to `.env` 0600 with explicit warning at startup. |
| Keychain prompts user on every read on some macOS configurations | Low | Med | Use `kSecAttrAccessibleAfterFirstUnlock` so daemon can read after first login unlock without prompting. |

---

## 11. Agent-guidance (for AI coding assistants working on this)

If you are an AI assistant working in this repo, read this section before editing.

1. **This file (`TODO-10-JUN.md`) is the source of truth.** Update it as work progresses. Tick checkboxes. Add to the Decisions Log (§12) when making non-trivial choices.
2. **CLAUDE.md applies.** Extreme terseness, no emoji, fail-fast, in-line lowercase comments only, no auto-refactor outside scope, no unverified claims (label [Inference] / [Speculation] / [Unverified] when appropriate).
3. **Stay in phase.** Don't jump ahead. If a phase's exit criteria aren't met, finish them before starting the next.
4. **Don't break the hard rules in §1.** If a request would break one, push back.
5. **§7 Security is non-negotiable.** Any code in `internal/auth`, `internal/secrets`, `internal/setup/pairing.go`, `internal/telegram/router.go`, `internal/approval/` must satisfy the corresponding enforcement in §7.3. PR/commit message must reference which threat (T1–T14) the change addresses.
6. **No new top-level dirs without updating §4.** Repo layout is authoritative.
7. **Library choices in §2 are locked.** Don't swap libraries unless you've added a row to the Decisions Log (§12) with rationale.
8. **Tests before merge.** Each phase exit criterion includes a real-world test (not just unit tests). For Phase 2+ that means actually invoking the agent end-to-end. Security tests (constant-time compare, redaction, owner check, pair-payload single-use, approval expiry) are mandatory.
9. **Hooks scripts go in `adapters/<agent>/`** (templates) and are installed by `internal/adapters/<agent>/install.go`. Never write hook installers ad hoc. Always record the script's sha256 in the hooks table.
10. **Never commit `.env`, `*.sqlite`, `*.token`, `*.totp`, anything matching `bot_token*`.** `.gitignore` enforces; pre-commit hook double-checks.
11. **Never send live tokens to web tools** (web search, web fetch, code uploaded to public services). Bot tokens stay local.
12. **All Telegram messages from the daemon must pass the rate limiter.** Don't bypass for "just one message" — the bypass becomes a habit.
13. **Owner check is at the router level, not per-handler.** Don't duplicate the check inside handlers (forgetting one becomes a vulnerability). Trust the chokepoint.
14. **If you finish a phase, update §8 status, then propose the next phase plan as a diff to this doc and pause for review.**

---

## 12. Decisions log (append-only)

Format: `YYYY-MM-DD — decision — rationale — by`

- 2026-06-10 — Pivot from Rust/Tauri/PWA to Go/Telegram-only — antirez/tgterm validated the shape; v1.5 setup cost was killing adoption; Telegram solves mobile + tunnels in one stroke — user.
- 2026-06-10 — Keep `onibi` name — brand continuity over signaling a clean break — user.
- 2026-06-10 — Go over Rust — better fit for Telegram-bot + PTY + SQLite; existing Rust adapters are mostly hook-script templates which port verbatim — user + claude.
- 2026-06-10 — Auto-switch text/PNG output — TUI agents (Claude Code) render badly in text-tail mode; text is searchable for shell output — user.
- 2026-06-10 — PTY default + tmux attach optional — non-tmux users are the majority; don't force tmux — user.
- 2026-06-10 — modernc.org/sqlite over mattn/go-sqlite3 — pure-Go, no cgo, simpler cross-compile and brew distribution; perf hit irrelevant at our scale — claude.
- 2026-06-10 — `go-telegram/bot` over `telebot.v4` and `go-telegram-bot-api/v5` — modern API, callback router, active maintenance Mar 2026, 384+ importers — claude.
- 2026-06-10 — Pair-once design: owner is established permanently via deeplink pair, no recurring TOTP by default — user requirement; tgterm's per-N-min TOTP was friction without payback for the typical threat profile; TOTP becomes opt-in for paranoid users — user + claude.
- 2026-06-10 — TOTP opt-in default off — eliminates user-visible recurring auth; threat T2 (owner Telegram compromise) addressed by mandatory Telegram 2FA acknowledgment in setup wizard + approval expiry — claude.
- 2026-06-10 — Bot token in macOS Keychain by default, `.env` fallback only — Keychain is hardware-backed and survives `cat`; addresses T1 partial; based on research showing token leaks are 2026's top Telegram bot incident class — claude.
- 2026-06-10 — Code signing + notarization mandatory for macOS release — Gatekeeper blocks unsigned binaries post-Catalina; unsigned would gate adoption; $99/yr Apple Developer ID is an accepted cost — claude.
- 2026-06-10 — Deeplink pairing replaces first-message-becomes-owner — eliminates T5 race; tgterm's pattern was acceptable for single-user but a race condition for any bot whose username might be discovered — claude.
- 2026-06-10 — Mandatory Telegram 2FA acknowledgment in setup wizard — research (Intel 471, Proton, Malwarebytes) shows OTP-bot industry exploits accounts without 2FA at industrial scale; this is the highest-impact mitigation per dollar of friction — claude.
- 2026-06-10 — Approval lifetime 5 min hard limit — addresses T7 (stale-approval hijack); short enough to prevent late attack, long enough for human reaction — claude.
- 2026-06-10 — No webhook support, ever — would require inbound listener; long-poll preserves local-only invariant; defensive `deleteWebhook` on startup blocks T1 attacker from switching modes — claude.
- 2026-06-10 — Hook hash registry for tamper detection — addresses T9; cheap to implement; gives a concrete signal in `onibi doctor` — claude.
- 2026-06-10 — No api.telegram.org cert pinning — cert rotates; pinning would brick the bot on rotation; stdlib defaults are the right call — claude.

---

## 13. External references

Libraries:
- [go-telegram/bot](https://github.com/go-telegram/bot) — Telegram Bot API framework, callback router
- [creack/pty](https://github.com/creack/pty) — PTY interface for Go
- [jaguilar/vt100](https://pkg.go.dev/github.com/jaguilar/vt100) — programmable ANSI emulator with cell grid access
- [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) — pure-Go SQLite
- [spf13/cobra](https://github.com/spf13/cobra) — CLI framework
- [skip2/go-qrcode](https://github.com/skip2/go-qrcode) — QR generation
- [99designs/keyring](https://github.com/99designs/keyring) — cross-platform secret storage (Keychain / Secret Service / Credential Manager)
- [mitchellh/gon](https://github.com/mitchellh/gon) — macOS codesign + notarize for any-language CLI binaries
- [goreleaser](https://goreleaser.com/customization/homebrew/) — release + Homebrew tap automation

Protocols / docs:
- [Claude Code hooks reference](https://code.claude.com/docs/en/hooks) — Stop, PreToolUse, Notification events; `updatedInput` field; CLI v2.1.165 (Jun 2026) supports 24+ lifecycle events
- [OpenAI Codex hooks](https://developers.openai.com/codex/hooks) — Codex hook events and blocking approval output
- [OpenCode plugin API](https://opencode.ai/docs/tools/) — 25+ lifecycle events incl. `permission.replied`, `tool.execute.before`
- [Telegram Bot API](https://core.telegram.org/bots/api) — sendMessage, sendPhoto, callback_query, deep linking, `setMyCommands`
- [Telegram Bot FAQ — rate limits](https://core.telegram.org/bots/faq) — 30 msgs/sec global, ~1 msg/sec per chat, 4096-char per text message
- [Telegram deep linking](https://core.telegram.org/api/links) — `t.me/<bot>?start=<param>` for one-tap pairing
- [Apple Notarization](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution) — required for distribution outside App Store
- [macOS launchd / LaunchAgent](https://www.launchd.info/) — `~/Library/LaunchAgents/`, runs only while user logged in (matches "while laptop open" requirement)
- [tmux send-keys bracketed-paste issue](https://github.com/tmux/tmux/issues/4663) — known multi-line breakage with extended-keys-format=csi-u; mitigation = verify-and-retry

Security references:
- [Telegram bot token leak remediation — GitGuardian](https://www.gitguardian.com/remediation/telegram-bot-token) — what an attacker can do with a leaked token, mitigation steps
- [Telegram Bot API Abuse — Netlas](https://netlas.io/blog/abuse_of_telegram_bot_api/) — bidirectional C2 via leaked tokens, `deleteWebhook`/`getUpdates` switch attack pattern (informs defensive `deleteWebhook` in §7.6)
- [Intel 471 — OTP bot industry](https://www.intel471.com/blog/otp-password-bots-telegram) — scale of Telegram 2FA-bypass automation (informs mandatory 2FA recommendation)
- [Proton — OTP bot defenses](https://proton.me/business/blog/otp-bot) — recommends email-based 2FA recovery over SMS (informs setup wizard guidance)
- [Malwarebytes — Telegram-powered 2FA bypass](https://www.malwarebytes.com/blog/news/2021/09/telegram-powered-bots-circumvent-2fa) — adversary capabilities
- [macOS Keychain Services](https://developer.apple.com/documentation/security/keychain-services) — `kSecAttrAccessibleAfterFirstUnlock` semantics
- [Unix socket peer credentials (macOS LOCAL_PEEREPID, Linux SO_PEERCRED)](https://man7.org/linux/man-pages/man7/unix.7.html) — IPC trust verification

Reference repos:
- [antirez/tgterm](https://github.com/antirez/tgterm) — pattern source for TOTP + first-msg-owner + QR ASCII rendering. Do not vendor C code; reimplement in Go.

---

## 14. Current focus

**Phase 3 — Blocking approval protocol + audit log.** Status: ready to start once manual e2e of Phases 1 + 2 pass.

Phase 2 code completed 2026-06-10: PTY host (creack/pty wrapper with raw-mode stdin forwarding + SIGWINCH resize), per-session concurrent ring buffer, text renderer with ANSI strip + UTF-8-safe head truncation + fenced code blocks, intake Unix socket (0600 + peer-cred check via x/sys/unix `Xucred`/`Ucred`), JSON event wire schema, fail-open `onibi-notify` client, session registry, idle-fallback detector with per-session dedup, daemon orchestrator with once-per-active-period guard so hook + idle don't double-fire, Claude Stop-hook installer with idempotent guarded-block JSON merge + sha256 tamper registry, `onibi run [agent [args...]]` + `onibi install-hooks --agent claude`. `go vet` clean, `go test -race ./...` green (~45 unit tests across 10 packages including ring-buffer concurrency, PTY echo, ANSI strip, OSC sequences, socket-malformed rejection, peer-cred enforcement, hook idempotency, hook tamper detection). `make build` produces working `bin/onibi` (11 MB) + `bin/onibi-notify` (2.6 MB).

Pending before Phase 3: user runs `scripts/manual-e2e-claude-run.md` (~5 min, requires Phase 1 + Claude CLI). Should confirm the Stop hook fires, Telegram receives the text-tail message on turn complete, idle-fallback works when hook is missing, and dedup prevents double-fires.

Next action when Phase 3 starts: implement `internal/approval/queue.go` (SQLite-backed state machine: pending → approved/denied/edited/expired/cancelled with atomic transitions), `internal/approval/expiry.go` (5-min sweeper), `internal/approval/scrub.go` (regex-based secret-redaction of tool inputs before render), `internal/telegram/keyboards.go` ([Approve][Deny][Edit] inline keyboard), `internal/telegram/router.go` (callback query dispatcher with owner middleware), `internal/adapters/claude/pretooluse.go` (blocking handler returning updatedInput/deny/allow via exit codes + JSON output), `internal/store/audit.go` (append-only audit log). Phase 3 deliverable: Claude attempts a tool call → blocks → Telegram message with keyboard → tap Approve → tool runs; tap Deny → tool blocked; daemon restart mid-approval restores pending state from SQLite.
