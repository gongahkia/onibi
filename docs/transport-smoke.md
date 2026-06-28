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
