# Patterns extracted from antirez/tgterm

Reference: https://github.com/antirez/tgterm (Apache-2.0-compatible BSD-3-Clause).

We do **not** vendor tgterm C code. We reimplement the patterns below in Go, adapted to our domain (sessions instead of windows, blocking approvals instead of just screenshots, opt-in TOTP instead of mandatory).

This doc captures what to reproduce and what to deliberately deviate from. File:line citations below were valid at the snapshot used (commit not pinned — patterns are stable across recent tgterm revisions).

---

## 1. TOTP setup + verify

**Source:** `tgterm/bot.c:107-237` (`totp_setup`, `totp_verify`, `base32_encode`, `hex_to_bytes`).

**Pattern:**
- 20 random bytes from `/dev/urandom` → TOTP secret.
- Stored hex-encoded in SQLite KV under key `totp_secret`.
- Base32-encoded for display + `otpauth://totp/<name>?secret=...&issuer=<name>` URI.
- QR rendered in terminal via ASCII half-blocks (▀ ▄ █).
- Verify: HMAC-SHA1 over 8-byte time counter (Unix time / 30); 6-digit code = `int(hash[offset..offset+4]) % 1_000_000`; check ±1 window for clock skew.

**Go port:** `internal/auth/totp.go` using `crypto/rand`, `crypto/hmac`, `crypto/sha1`, `encoding/base32`, `encoding/hex`. Stored via `internal/secrets/keychain.go` (NOT SQLite — Keychain is hardware-backed). QR rendered via `github.com/skip2/go-qrcode` to terminal stdout.

**Deviation:**
- **TOTP is opt-in, not required.** tgterm gates every command on TOTP after timeout. We pair-once (§6.1 of TODO) and only gate destructive commands when `--enable-totp` set.
- Secret in Keychain, not SQLite KV. Survives DB deletion. Requires user-session unlock.
- Use a per-install random `issuer` (e.g. `onibi@<hostname>`) so multiple Onibi installs don't clash in the authenticator app.

---

## 2. SQLite KV store schema

**Source:** `tgterm/botlib.h:63-68` (`TB_CREATE_KV_STORE`).

```sql
CREATE TABLE IF NOT EXISTS KeyValue(expire INT, key TEXT, value BLOB);
CREATE UNIQUE INDEX IF NOT EXISTS idx_kv_key ON KeyValue(key);
CREATE INDEX IF NOT EXISTS idx_ex_key ON KeyValue(expire);
```

**Pattern:**
- Flat KV with optional `expire` (Unix epoch seconds; 0 = never).
- `kvGet`, `kvSet`, `kvSetLen`, `kvDel` API.

**Go port:** `internal/store/kv.go` using `modernc.org/sqlite`. Same schema verbatim. Add the same indexes. Use prepared statements (`db.Prepare` once, reuse).

**Deviation:**
- We add several **typed tables** alongside KV for things that aren't well-modeled as KV: `pairing_tokens`, `approvals`, `audit`, `hooks`, `sessions`. KV is for one-off scalars (owner_id, last_update_offset, otp_timeout, bot_id).
- KV `expire` column unused for security-sensitive data — we use dedicated tables with explicit `expires_at` columns and constraints (see pairing_tokens in §3 below).

---

## 3. Owner registration (replaced, not ported)

**Source:** `tgterm/bot.c:780-803` (first-user-becomes-owner block in `handle_request`).

**Pattern in tgterm:**
- KV `owner_id` checked on every message.
- If unset: the first chat to message the bot becomes owner. Persisted forever.
- If set and message is from a different `from.id`: dropped + logged.

**Why we deviate:** This pattern has a race condition (threat T5 in §7.1 of TODO). If an attacker discovers the bot's username (BotFather creates discoverable bots; usernames can be enumerated or guessed) and messages first, they become owner. Acceptable for tgterm's solo-developer use. Not acceptable for distribution.

**Go replacement:** `internal/setup/pairing.go` with a single-use deeplink-token flow:

```sql
CREATE TABLE pairing_tokens (
  token       TEXT PRIMARY KEY,        -- base64url of 32 random bytes
  created_at  INTEGER NOT NULL,        -- unix sec
  expires_at  INTEGER NOT NULL,        -- created_at + 300
  consumed    INTEGER NOT NULL DEFAULT 0
);
```

- `onibi setup` generates the token, prints `https://t.me/<bot>?start=pair_<token>` + QR.
- Deeplink handler validates: token row exists, `consumed=0`, `expires_at > now`. Constant-time string compare.
- Atomic `UPDATE pairing_tokens SET consumed=1 WHERE token=? AND consumed=0`; check affected rows == 1. Set `owner_id` in KV.
- After owner is set, further `/start pair_*` calls are silently ignored unless `onibi setup --rotate-owner` was run.

**What we keep from tgterm:** the *concept* of single-owner-persisted-in-KV. Just the establishment mechanism changes.

---

## 4. QR-code rendering in terminal

**Source:** `tgterm/bot.c:129-156` (`print_qr_ascii`) + `tgterm/qrcodegen.{c,h}` (project Nayuki, MIT).

**Pattern:**
- Encode text to QR via `qrcodegen_encodeText` with `Ecc_LOW`.
- Render two QR rows per terminal row using Unicode half-block chars: `▀` (U+2580 top half), `▄` (U+2584 bottom half), `█` (U+2588 full block), space.
- 1-module quiet zone around the matrix.

**Go port:** `internal/setup/qr.go` using `github.com/skip2/go-qrcode` for QR matrix generation; rendering loop is straightforward port of the half-block algorithm.

**Deviation:** none. Half-block ASCII QR is the right rendering for terminal stdout. We use the same algorithm.

---

## 5. Telegram long-poll loop with `getUpdates`

**Source:** `tgterm/botlib.c:828-1037` (`botProcessUpdates`).

**Pattern:**
- `getUpdates` with `offset=last_id+1`, `timeout=15`, `allowed_updates=["message","callback_query"]`.
- Persist `offset` across restarts (in KV) so we don't reprocess.
- For each update, look for `callback_query` first, then `message` / `channel_post`.
- Extract: `update_id`, `callback.id`, `callback.data`, `callback.from.id`, `message.chat.id`, `message.from.id`, `message.message_id`, `message.chat.type`.
- Dispatch each request on a detached pthread.

**Go port:** the `go-telegram/bot` library handles `getUpdates` + offset persistence + dispatcher pattern internally. We provide:
- A storage handler so the offset is persisted in our SQLite KV (`last_update_offset`).
- Handlers registered via `bot.RegisterHandler` for `bot.HandlerTypeMessageText` and `bot.HandlerTypeCallbackQueryData`.
- An `auth.MustBeOwner` middleware applied **before** any handler runs — checks `update.Message.From.ID == owner_id` (or `update.CallbackQuery.From.ID == owner_id`).

**Deviation from tgterm:**
- We use Go goroutines instead of pthreads — same effect, native to the lang.
- We **never use webhook mode**. tgterm doesn't either. We add a defensive `deleteWebhook` on startup to undo any external `setWebhook` (attacker-token scenario).
- `allowed_updates` is the same `["message","callback_query"]`. Restricting allowed update types reduces parsing surface and matches our handler set exactly.
- Long-poll timeout: 30s (longer than tgterm's 15s, still under our 35s HTTP client timeout).

---

## 6. Callback query dispatch + answerCallbackQuery

**Source:** `tgterm/bot.c:831-838` (callback handling in `handle_request`).

**Pattern:**
- On callback: call `answerCallbackQuery(callback_id)` immediately (so Telegram clears the spinner on the button).
- Then act on `callback_data` (opaque string we set when we built the keyboard).

**Go port:** same pattern. `bot.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: cq.ID})` immediately on entry to the handler, **before** any slow work. Then route on `cq.Data`.

**Our convention:** `callback_data` format `<verb>:<approval_id>` (e.g. `approve:01HXY...`, `deny:01HXY...`, `edit:01HXY...`). Resolved against the `approvals` table. Approval state machine enforces single-decision. Approval expiry rejects late callbacks with `answerCallbackQuery(text="Expired", show_alert=true)`.

---

## 7. Sending images with inline keyboards + editing message media

**Source:** `tgterm/bot.c:768-777` (`send_screenshot`, `refresh_screenshot`) + `botSendImageWithKeyboard`, `botEditMessageMedia` in `botlib.c`.

**Pattern:**
- `sendPhoto` with `reply_markup={"inline_keyboard":[[{"text":"🔄 Refresh","callback_data":"refresh"}]]}`.
- On Refresh callback: re-capture screenshot, `editMessageMedia` to swap the photo in-place.

**Go port:** `bot.SendPhoto(ctx, &bot.SendPhotoParams{ChatID: ..., Photo: ..., ReplyMarkup: ...})`. `EditMessageMedia` for in-place updates.

**Our use:** approval messages get `[Approve][Deny][Edit]` keyboard; turn-complete messages get `[Continue][Interrupt][Peek]` keyboard. Refresh-screenshot pattern carried over for `/peek <session>` command (see §9.5 of TODO).

---

## 8. Connection state per-window (mapped to per-session)

**Source:** `tgterm/bot.c:60-77` (`Connected`, `ConnectedWid`, `ConnectedPid`, `ConnectedOwner`, `ConnectedTitle`) + `bot.c:411-453` (`connected_window_exists` with tab-switch fallback by PID).

**Pattern:** single mutable "currently connected" target. `.N` selects window N; subsequent text goes there. Window-closed detection via re-enumeration.

**Go port — what we change:**
- We have N sessions, not "one connected window." State lives in the session registry (`internal/daemon/registry.go`), keyed by stable session ID.
- Replace the "fallback to same-PID window" trick with our reply-to-message → session_id map (more reliable, more intuitive for multi-session).
- `/target <name>` sets the default session for non-reply text inputs (analogue of tgterm's `.N`).

---

## 9. `.list`, `.N`, `.help` command style

**Source:** `tgterm/bot.c:842-908` (command dispatcher).

**Pattern:** dot-prefixed commands; `.N` numeric for selection.

**Go port — adapt:** we use slash-commands (`/sessions`, `/target`, `/status`, `/help`, `/snooze`, `/new`) because Telegram natively highlights `/` commands and `setMyCommands` integrates with them. No dot-prefix.

---

## 10. HTTP / curl options worth replicating

**Source:** `tgterm/botlib.c:273, 369, 441, 530, 716` (curl settings, repeated per call).

**Pattern:**
- `CURLOPT_SSL_VERIFYPEER=1`, `CURLOPT_SSL_VERIFYHOST=1` (i.e. validate the cert chain).
- `CURLOPT_TIMEOUT=15`, `CURLOPT_CONNECTTIMEOUT=15`.

**Go port:** `internal/telegram/client.go`:

```go
httpClient := &http.Client{
    Timeout: 35 * time.Second, // covers 30s long-poll + slack
    Transport: &http.Transport{
        TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
        Proxy:           nil, // refuse env HTTP_PROXY for api.telegram.org unless --allow-proxy
    },
}
```

No `InsecureSkipVerify`. Default cert roots. No pinning (Telegram rotates certs).

---

## 11. Bot token loading & lifecycle

**Source:** `tgterm/botlib.c` (`startBot` flow, `Bot.apikey` global).

**Pattern in tgterm:**
- Token loaded from `--apikey <token>` CLI arg, or env, or KV store.
- `getMe` called once to fetch `Bot.username`.
- Token used in every URL: `https://api.telegram.org/bot<token>/<method>`.

**Go port — what we change:**
- **Never** accept token as CLI positional or `--token=<token>` arg (process argv is readable by other processes via `ps`). Allowed sources: macOS Keychain (default), `.env` file (fallback), `--token-stdin` (paste via pipe).
- `getMe` on startup, compare `bot_id` against persisted value in KV. If mismatch: refuse to start (token swap attack signal — see §7.6 of TODO).
- Token redacted from every log message via a `slog.Handler` middleware that scans records for the token string.
- Rotation: `onibi rotate-token` walks user through @BotFather `/revoke`, paste new, Keychain replace.

---

## 12. What we don't carry over

- **macOS Accessibility API usage** (`AXUIElementCreateApplication`, `_AXUIElementGetWindow`, `AXUIElementPerformAction(_, kAXRaiseAction)`) — `tgterm/bot.c:506-548`. We host agents under our own PTY; we don't need to find and raise an existing terminal window.
- **CoreGraphics window enumeration / screenshot** (`CGWindowListCopyWindowInfo`, `CGWindowListCreateImage`) — `tgterm/bot.c:329-499`. Same reason. Plus this is macOS-only; we render screenshots from our own PTY buffer via `jaguilar/vt100` + `image/png`, which works cross-platform.
- **`CGEventPostToPid` keystroke injection** — `tgterm/bot.c:577-621`. We write to PTY directly, or `tmux send-keys -l` if attached to a tmux session.
- **Dot-prefixed commands** (`.list`, `.N`). We use slash commands.
- **Emoji-as-modifier convention** (❤️ Ctrl, 💙 Alt, etc.) — `tgterm/bot.c:264-305`. Cute but our preset allowlist + inline keyboards cover the same ground more legibly.
- **`--dangerously-attach-to-any-window`** mode. Not applicable to our design.

---

## 13. What we add (tgterm doesn't have)

- Multi-session (vs single-connected-window).
- Blocking approval protocol with `[Approve][Deny][Edit]` + `updatedInput` returned to the agent.
- Audit log (every action persisted).
- Approval expiry (5 min).
- Defensive `deleteWebhook` on startup.
- `getUpdates` race detector.
- Hook hash registry for tamper detection.
- Code signing + notarization.
- LaunchAgent / systemd user unit auto-start.
- Multi-adapter (Claude Code, Codex, OpenCode, Goose…) instead of generic-terminal-only.

---

## 14. License notice

tgterm is BSD-3-Clause. Our reimplementation in Go is original code under Apache-2.0. No tgterm source ships in the Onibi binary. Pattern attribution stays in this doc.
