# Transport smoke checklist

Default CI is hermetic. Live provider checks run only when the listed env vars are set.

## Always-run local checks

```bash
go test ./...
go vet ./...
go test -race ./internal/web ./internal/web/transport ./internal/daemon ./internal/telegram ./internal/cli
make build
```

## LAN and hotspot

- Same Wi-Fi: run `onibi up --transport=lan`, scan QR with iPhone Safari, pair, open terminal, type `printf ok`.
- iPhone Chrome: repeat pair and confirm owner cookie survives reload.
- Hotspot: connect Mac to iPhone hotspot, rerun `onibi up --transport=lan`, pair, type input, background browser for 10s, return.
- Client-isolated Wi-Fi: confirm pair URL fails from phone, then confirm hotspot succeeds.
- Captive Wi-Fi: confirm captive portal does not silently pair; switch to hotspot.

## Tailscale

```bash
ONIBI_LIVE_TAILSCALE=1 go test ./internal/web/transport -run LiveTailscale
```

- Run `onibi up --transport=tailscale`.
- Scan `*.ts.net` QR from a phone on LTE, not Wi-Fi.
- Confirm terminal input/output, resize, `/ws/events` approvals, and shutdown cleanup log `tailscale funnel --bg off`.

## Telegram

```bash
ONIBI_LIVE_TELEGRAM_TOKEN=... ONIBI_LIVE_TELEGRAM_CHAT_ID=... go test ./internal/telegram ./internal/daemon -run LiveTelegram
```

- Run `onibi telegram setup`, then `onibi up --transport=telegram`.
- Pair with `/start <code>`.
- Check `/new shell`, plain text input, long output chunking, `/target`, approval Approve/Deny/Edit, and restart persistence.
- Optional: repeat with `ONIBI_LIVE_CLAUDE=1` and `ONIBI_LIVE_CODEX=1` using real agent approval flows.

## Matrix

```bash
ONIBI_LIVE_MATRIX=1 ONIBI_MATRIX_HOMESERVER=... ONIBI_MATRIX_ACCESS_TOKEN=... ONIBI_MATRIX_ROOM_ID=... go test ./internal/matrix -run LiveMatrix
```

- Run `onibi up --transport=matrix`.
- Confirm the bot can `/sync`, send a room message, reconnect with the stored since token, and reject a room where it lacks owner/moderator power.
- Confirm encrypted rooms fail unless `ONIBI_MATRIX_ALLOW_ENCRYPTED=1` is intentionally set.
- Restart Onibi and confirm the room does not replay old terminal input.
- Trigger a bad homeserver/token and confirm the error is surfaced.

## Slack

```bash
ONIBI_LIVE_SLACK=1 ONIBI_SLACK_APP_TOKEN=... ONIBI_SLACK_BOT_TOKEN=... ONIBI_SLACK_CHANNEL_ID=... go test ./internal/slack -run LiveSlack
```

- Run `onibi up --transport=slack`.
- Confirm Socket Mode opens, message envelopes are acked, `disconnect`/`refresh_requested` opens a fresh socket URL, and DM/channel allowlists block non-owned sources.
- Trigger an approval and confirm Approve/Deny buttons decide it.

## Discord

```bash
ONIBI_LIVE_DISCORD=1 ONIBI_DISCORD_TOKEN=... ONIBI_DISCORD_CHANNEL_ID=... go test ./internal/discord -run LiveDiscord
```

- Run `onibi up --transport=discord`.
- Confirm Gateway Identify, heartbeat ACKs, Resume after reconnect, DM versus guild allowlist behavior, and terminal text input when Message Content intent is enabled.
- Disable Message Content intent and confirm the slash-command fallback reply.

## Notify-only

Pushover:

```bash
ONIBI_LIVE_PUSHOVER=1 ONIBI_PUSHOVER_TOKEN=... ONIBI_PUSHOVER_USER_KEY=... go test ./internal/pushover -run LivePushover
```

- Trigger an approval and confirm an emergency notification arrives.
- Acknowledge it and confirm receipt polling observes the ack or expiry.
- Confirm audit rows include receipt creation and ack/expiry state.
- Confirm normal terminal messages are not accepted through Pushover.

ntfy:

```bash
ONIBI_LIVE_NTFY=1 ONIBI_NTFY_TOPIC=<20+ char random secret> go test ./internal/ntfy -run LiveNtfy
```

- Confirm publish and WebSocket subscribe receive the same approval text.
- Confirm short, repeated, single-class, or guessable topics are rejected before startup.

Gotify:

```bash
ONIBI_LIVE_GOTIFY=1 ONIBI_GOTIFY_URL=... ONIBI_GOTIFY_APP_TOKEN=... ONIBI_GOTIFY_CLIENT_TOKEN=... go test ./internal/gotify -run LiveGotify
```

- Confirm REST send creates an approval notification.
- Confirm WebSocket subscribe receives the message stream.
- Confirm startup validates the optional client token.
- Confirm no terminal input path is exposed.

## Cloudflare Quick

```bash
ONIBI_LIVE_CLOUDFLARE_QUICK=1 go test ./internal/web/transport -run LiveCloudflareQuick
```

- Run `onibi up --transport=cloudflare-quick`.
- Confirm QR URL is `https://*.trycloudflare.com/pair/<token>#k=...`.
- Pair from iPhone Safari on LTE and confirm terminal bytes work.
- In logs, verify relay cleanup reports `cloudflared process kill`.
- Confirm copied URL without `#k=...` cannot drive encrypted WS/control payloads.

## Cloudflare Named

Required:

```bash
ONIBI_CLOUDFLARE_TUNNEL_NAME=...
ONIBI_CLOUDFLARE_HOSTNAME=...
ONIBI_LIVE_CLOUDFLARE_NAMED=1 go test ./internal/web/transport -run LiveCloudflareNamed
```

- Confirm route belongs to the configured tunnel before running.
- Run `onibi up --transport=cloudflare-named`.
- Pair from iPhone Safari and Chrome over LTE.
- Confirm no account-side tunnel/route teardown occurs by default.

## ngrok

```bash
ONIBI_LIVE_NGROK=1 go test ./internal/web/transport -run LiveNgrok
```

Optional:

```bash
ONIBI_NGROK_AUTHTOKEN=...
ONIBI_NGROK_DOMAIN=...
```

- Run `onibi up --transport=ngrok`.
- Confirm QR URL includes `#k=...`.
- Pair from iPhone Safari and Chrome over LTE.
- Confirm Agent API tunnel cleanup and process cleanup in logs.
