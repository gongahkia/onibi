# Transport smoke checklist

Default CI is hermetic. Live provider checks run only when the listed env vars are set.

Live tests write sanitized JSON artifacts to `ONIBI_LIVE_ARTIFACT_DIR` when set, otherwise `/tmp/onibi-live-smoke`. Artifacts record env-key presence, timings, URLs, and scrubbed errors; they must not contain tokens, approval payload secrets, or terminal secrets.

## Always-run local checks

```bash
go test ./...
go vet ./...
go test -race ./internal/web ./internal/web/transport ./internal/daemon ./internal/telegram ./internal/cli
make build
```

## LAN and hotspot

- Home Wi-Fi: run `onibi up --transport=lan`, scan QR with iPhone Safari, pair, open terminal, type `printf ok`, and confirm terminal output returns.
- iPhone Chrome: repeat pair and confirm owner cookie survives reload after the local CA profile is fully trusted.
- iPhone hotspot: connect the Mac to the iPhone hotspot, rerun `onibi up --transport=lan`, pair, type input, background browser for 10s, return, and confirm the WebSocket resumes.
- Enterprise or managed Wi-Fi with client isolation: confirm the phone cannot load the LAN pair URL from Safari, then switch both devices to the iPhone hotspot and confirm pairing succeeds. Do not keep retrying the isolated SSID.
- Captive Wi-Fi: confirm captive portal interception does not silently pair; switch to hotspot.
- IPv4/IPv6: confirm the printed QR prefers a routable IPv4 address when one is present. If only IPv6 is printed, confirm the URL uses bracket syntax, for example `https://[fd00::1]:8443/pair/<token>`.
- Auto fallback: run `onibi up --transport=auto` with Tailscale unavailable and confirm it falls back to LAN only when a routable LAN or `.local` host exists. If explicit LAN startup reports `lan_unreachable`, use hotspot, Tailscale, Cloudflare Quick, or ngrok.

## Tailscale

```bash
ONIBI_LIVE_TAILSCALE=1 go test ./internal/web/transport -run LiveTailscale
```

- Run `onibi up --transport=tailscale`.
- Scan `*.ts.net` QR from a phone on LTE, not Wi-Fi.
- Run `onibi doctor --transport=tailscale` and confirm the `tailscale` check reports login/Funnel readiness, or an actionable warning for missing login, HTTPS, Funnel, or port-443 capability.
- Capture `tailscale funnel status --json` and `tailscale serve status --json`; at least one must expose the active Funnel host used in the QR.
- Confirm terminal input/output, resize, `/ws/events` approvals, and shutdown cleanup log `tailscale funnel --bg off`.

## Telegram

```bash
ONIBI_LIVE_TELEGRAM_TOKEN=... ONIBI_LIVE_TELEGRAM_CHAT_ID=... go test ./internal/telegram ./internal/daemon -run LiveTelegram
ONIBI_TELEGRAM_TOKEN="$ONIBI_LIVE_TELEGRAM_TOKEN" onibi telegram status --check
```

- Run `onibi telegram setup` or `onibi telegram setup --no-check --token "$ONIBI_LIVE_TELEGRAM_TOKEN"` for offline secret-store setup.
- Run `onibi telegram status --check`, then `onibi up --transport=telegram`.
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
- `ONIBI_MATRIX_ALLOW_ENCRYPTED=1` only bypasses the startup guard. It does not provide Matrix Olm/Megolm E2EE, so encrypted-room support remains unsupported until a real E2EE client stack is integrated.
- Restart Onibi and confirm the room does not replay old terminal input.
- Trigger a bad homeserver/token and confirm the error is surfaced.

## Slack

```bash
ONIBI_LIVE_SLACK=1 ONIBI_SLACK_APP_TOKEN=... ONIBI_SLACK_BOT_TOKEN=... ONIBI_SLACK_CHANNEL_ID=... go test ./internal/slack -run LiveSlack
```

- Run `onibi up --transport=slack`.
- Confirm Socket Mode opens, message envelopes are acked, `disconnect`/`refresh_requested` opens a fresh socket URL, and DM/channel allowlists block non-owned sources.
- Set `ONIBI_SLACK_APPROVAL_CHANNEL` or include a channel in `ONIBI_SLACK_ALLOWED_CHANNELS`.
- Trigger an approval and confirm Approve/Deny Block Kit buttons decide it.
- Confirm the original Slack approval message updates to approved/denied/expired/failed final state and duplicate clicks remain visibly terminal.
- Button values should contain structured approval/session state and not raw tool payload secrets.

## Discord

```bash
ONIBI_LIVE_DISCORD=1 ONIBI_DISCORD_TOKEN=... ONIBI_DISCORD_CHANNEL_ID=... go test ./internal/discord -run LiveDiscord
```

- Run `onibi up --transport=discord`.
- Run `onibi discord register --guild-id <guild>` for fast slash-command registration, or omit `--guild-id` for global registration.
- Confirm Gateway Identify, heartbeat ACKs, Resume after reconnect, DM versus guild allowlist behavior, and terminal text input when Message Content intent is enabled.
- Disable Message Content intent and confirm `/onibi text:<input>` still routes terminal input.

## Doctor provider probes

`onibi doctor --transport <provider>` performs env and non-mutating live API checks unless `--offline` is set.

- Slack: `auth.test`, Socket Mode open, and configured approval/allowed channel membership.
- Discord: current application and channel visibility; set `ONIBI_DOCTOR_LIVE=1` to send a channel permission probe.
- Discord also checks `/onibi` command presence when `ONIBI_DISCORD_APPLICATION_ID` or `ONIBI_DISCORD_GUILD_ID` is set; doctor reports missing command but does not register it.
- Matrix: account ownership power, joined-room state, and encrypted-room refusal.
- Gotify: token validation; set `ONIBI_DOCTOR_LIVE=1` for send/WS probe.
- ntfy: topic secrecy validation; set `ONIBI_DOCTOR_LIVE=1` for publish/WebSocket subscribe probe.

## Provider output policy

```bash
onibi config set provider.output.max_chunks 8
onibi config set provider.output.max_bytes 24576
onibi config set provider.output.redaction strict
onibi config set provider.output.slack.max_bytes 12000
onibi config set provider.output.discord.redaction off
```

- `default` redaction uses Onibi approval/output scrubbing.
- `strict` additionally masks long token-like strings.
- `off` disables config redaction; `ONIBI_CHAT_UNREDACTED=1` remains an env escape hatch.
- Provider-specific overrides exist for `telegram`, `matrix`, `slack`, `discord`, and `notify`; set an override to `inherit` to return to global defaults.
- Output is truncated before provider chunk send, so chat providers cannot stream unlimited terminal output.

## Notify-only

Pushover:

```bash
ONIBI_LIVE_PUSHOVER=1 ONIBI_PUSHOVER_TOKEN=... ONIBI_PUSHOVER_USER_KEY=... ONIBI_PUSHOVER_EMERGENCY=1 ONIBI_PUSHOVER_POLL_RECEIPT=1 go test ./internal/pushover -run LivePushover
```

- Trigger an approval and confirm an emergency notification arrives.
- Acknowledge it and confirm receipt polling observes the ack or expiry; acknowledged receipts map to approve.
- Confirm audit rows include receipt creation, ack/expiry state, and approve/error state.
- Confirm `onibi status` shows notify recent/error counts and `onibi log --notify` filters notify audit rows.
- Confirm normal terminal messages are not accepted through Pushover.

ntfy:

```bash
ONIBI_LIVE_NTFY=1 ONIBI_NTFY_TOPIC=<20+ char random secret> ONIBI_NTFY_STREAM=1 go test ./internal/ntfy -run LiveNtfy
```

- Confirm publish and JSON stream subscribe receive the same approval text.
- With `ONIBI_NTFY_ACTION_BASE_URL` set to a reachable Onibi web origin, confirm ntfy web/Android render Approve/Deny actions.
- Confirm short, repeated, single-class, or guessable topics are rejected before startup.

Gotify:

```bash
ONIBI_LIVE_GOTIFY=1 ONIBI_GOTIFY_URL=... ONIBI_GOTIFY_APP_TOKEN=... ONIBI_GOTIFY_CLIENT_TOKEN=... ONIBI_GOTIFY_STREAM=1 go test ./internal/gotify -run LiveGotify
```

- Confirm REST send creates an approval notification.
- Confirm WebSocket subscribe receives the message stream.
- With `ONIBI_GOTIFY_ACTION_BASE_URL` set to a reachable Onibi web origin, tap the notification/link and confirm the signed approval page renders Approve/Deny.
- Confirm startup validates the optional client token.
- Confirm no terminal input path is exposed.

Signal:

```bash
ONIBI_LIVE_SIGNAL=1 ONIBI_SIGNAL_RPC_URL=http://127.0.0.1:6001 ONIBI_SIGNAL_ACCOUNT=+15551234567 ONIBI_SIGNAL_RECIPIENT=+15557654321 go test ./internal/signal -run LiveSignal
```

- Run a linked `signal-cli` daemon before the test.
- Set `ONIBI_SIGNAL_STREAM=1` only when you can send an inbound Signal message during the test window.
- Confirm `go test -race ./internal/signal -run TestParityAxes` covers send, emoji reaction JSON-RPC, SSE chunking, and reconnect.
- Confirm daemon-level text-in, approval reaction mapping, and audit wiring before enabling `--transport=signal`.

## Cloudflare Quick

```bash
ONIBI_LIVE_CLOUDFLARE_QUICK=1 go test ./internal/web/transport -run LiveCloudflareQuick
```

- Install or select `cloudflared`: `cloudflared --version`, or export `ONIBI_CLOUDFLARED_BIN=/path/to/cloudflared`.
- Run `go test -race ./internal/web -run TestE2E` before live smoke.
- Run `onibi up --transport=cloudflare-quick --log-file /tmp/onibi-cf-quick.log`.
- Confirm the printed QR URL is `https://*.trycloudflare.com/pair/<token>#k=...`; save a copy with the fragment removed for the negative test.
- Pair from iPhone Safari on LTE, open a session, type `printf cf-e2e-ok`, and confirm output returns.
- Reload the page and confirm the WebSocket resumes without plaintext terminal bytes in `/tmp/onibi-cf-quick.log`.
- Open the fragmentless copy on the phone; confirm the UI does not attach and encrypted control/WebSocket traffic cannot proceed.
- Press Ctrl+C in the `onibi up` terminal; confirm `/tmp/onibi-cf-quick.log` reports `cloudflared process kill`.

## Cloudflare Named

Required:

```bash
ONIBI_CLOUDFLARE_TUNNEL_NAME=...
ONIBI_CLOUDFLARE_HOSTNAME=...
ONIBI_LIVE_CLOUDFLARE_NAMED=1 go test ./internal/web/transport -run LiveCloudflareNamed
```

- Confirm route belongs to the configured tunnel before running.
- Run `onibi doctor --transport=cloudflare-named` and confirm token retrieval plus `cloudflared tunnel info` pass.
- Run `onibi up --transport=cloudflare-named`.
- Pair from iPhone Safari and Chrome over LTE.
- Confirm no account-side tunnel/route teardown occurs by default.

## ngrok

```bash
ONIBI_LIVE_NGROK=1 go test ./internal/web/transport -run LiveNgrok
```

Optional:

```bash
onibi ngrok setup --authtoken ...
ONIBI_NGROK_DOMAIN=...
```

- Run `onibi doctor --transport=ngrok` and confirm the `transport provider` check passes. If using `ONIBI_NGROK_DOMAIN`, confirm the authtoken is present from keyring or `ONIBI_NGROK_AUTHTOKEN`.
- Run `onibi up --transport=ngrok`.
- Confirm QR URL includes `#k=...` and starts with `https://`.
- Pair from iPhone Safari and Chrome over LTE.
- Confirm Agent API tunnel cleanup and process cleanup in logs.
- Keep demos under ngrok's current free-plan quotas; check https://ngrok.com/docs/pricing-limits/free-plan-limits before public sharing.
