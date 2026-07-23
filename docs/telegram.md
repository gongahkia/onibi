# Telegram Chat Cockpit

Status: beta. [Unverified] The live certification result below has not been recorded. It is not a PWA terminal replacement.

`onibi telegram status --json` emits capability report version `1`. Its contract is owner-only, standalone, bounded/redacted output, and `approve`/`deny`/`edit` approval actions. It reports `pwa_required: false`, `live_terminal: false`, and `end_to_end_encrypted: false`.

The local Telegram bridge starts from `onibi start --transport=telegram`; it creates one Onibi-managed tmux session and uses independent one-time owner enrollment in a private chat. It stores both the enrolled chat ID and Telegram user ID, and rejects group chats and callbacks from any other user. It neither starts a web listener nor requires browser pairing, browser cookies, Web Push, or an open PWA page.

Legacy chat-ID-only enrollment is rejected. Reset it with `onibi telegram disable`, run setup again, then pair from a private chat.

Telegram Bot API messages are not treated as end-to-end encrypted. The bounded/redacted text and approval messages traverse Telegram infrastructure; use the web relay transport when an Onibi end-to-end relay boundary is required. Telegram's [FAQ](https://telegram.org/faq) distinguishes end-to-end-encrypted Secret Chats from Cloud Chats.

Every text response, including `/peek`, passes the configured `provider.output` bounds and redaction policy before it reaches Telegram.

## Commands

After owner enrollment, text targets the selected managed session. `/new`, `/sessions`, and `/target` create/select sessions; `/peek` returns bounded output; `/interrupt`, `/esc`, `/enter`, `/show`, `/hide`, `/end`, and the double-confirmed `/kill` control the selected session. Approvals use `/approve <id>`, `/deny <id> [reason]`, and `/edit <id> <edited JSON>`.

Non-owner messages and callbacks are rejected. Callback decisions are idempotent, and polling uses bounded reconnect backoff and Telegram send-rate limits. Audit rows retain payload hashes and compact metadata rather than raw payloads.

## Live Certification Runbook

This runbook is secret-gated and has not produced a committed real-device result in this repository state.

1. Export `ONIBI_LIVE_TELEGRAM_TOKEN` and `ONIBI_LIVE_TELEGRAM_CHAT_ID`; run `go test ./internal/telegram -run '^TestLiveTelegram$' -count=1` to record authenticated `getMe` and text-send artifacts.
2. Run `onibi telegram setup`, then `onibi start --transport=telegram`; enroll only the intended owner from a private chat with the emitted `/start` code.
3. From the enrolled chat, run `/new shell`, send text, select with `/target`, and verify bounded/redacted output.
4. Trigger an Onibi approval; verify approve, deny, invalid edit rejection, valid JSON edit, non-owner rejection, duplicate callback handling, and reconnect after a temporary network interruption.
5. Record the bot version, timestamp, device, redacted transcript, audit hashes, reconnect result, and limitations here before changing the status above or closing #289.
