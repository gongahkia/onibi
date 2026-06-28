# Onibi transports

Onibi v3 groups connection options by control surface:

- Web URL: browser cockpit + QR.
- Chat: natural text control.
- Notify-only: approvals and alerts without terminal input.

The supported transports are:

- `lan` (default): QR points at the Mac's LAN or hotspot address.
- `tailscale`: QR points at the device's Tailscale Funnel URL.
- `cloudflare-quick`: QR points at a temporary `trycloudflare.com` URL.
- `cloudflare-named`: QR points at a configured Cloudflare Tunnel hostname.
- `ngrok`: QR points at an ngrok public HTTPS URL.
- `telegram`: Bot API chat control for natural text input/output and approvals.
- `matrix`: Matrix room control with `/sync` polling and room-power validation.
- `slack`: Slack Socket Mode workspace control.
- `discord`: Discord Gateway bot control with slash-command fallback.
- `pushover`: notify-only approval alerts.
- `ntfy`: notify-only topic alerts.
- `gotify`: notify-only self-hosted alerts.

`auto` tries `tailscale` first and falls back to `lan` when Tailscale is unavailable. It does not select third-party relays.
Run `onibi up` from a terminal to choose category first, provider second, or pass `--transport=<mode>` for scripts.

## LAN

`onibi up --transport=lan` starts the local HTTPS/WebSocket cockpit on `web.listen_addr` (default `:8443`) and prints a QR for a URL such as:

```text
https://192.168.1.42:8443/pair/<token>
```

Security model:

- The browser talks directly to the Mac over the current LAN or phone hotspot.
- Onibi serves HTTPS with its local CA/server cert. iOS must trust the generated profile before Safari will reliably keep the Secure owner cookie.
- Pair tokens are single-use and expire after 5 minutes.
- The owner cookie is required for the cockpit page and WebSocket upgrades.

Operational notes:

- LAN mode fails when Wi-Fi client isolation, captive networks, VPN policy, or firewall rules block phone-to-Mac traffic.
- A phone hotspot is the lowest-friction fallback when the phone cannot reach the pair URL at all.
- `.local` mDNS is secondary; Onibi prefers LAN IP QR URLs because mobile mDNS reachability is inconsistent.

## Tailscale Funnel

`onibi up --transport=tailscale` keeps the same local HTTPS cockpit, enables Funnel in the background, and prints a QR for a URL such as:

```text
https://device-name.tailnet.ts.net/pair/<token>
```

Implementation details:

- Onibi checks `tailscale status --json`; `BackendState` must be `Running`, and the local node must expose the Tailscale HTTPS, Funnel, and Funnel-port capability for public port `443`.
- Onibi runs `tailscale funnel --bg https+insecure://localhost:<web-port>`. The `https+insecure` target is intentional because Onibi's local listener is HTTPS-only with a local CA certificate.
- Onibi reads `tailscale serve status --json` and builds the QR URL from the active `AllowFunnel` host.
- On shutdown, Onibi runs `tailscale funnel --bg off`.

Security model:

- Public clients connect to the Tailscale Funnel URL over HTTPS.
- Tailscale documents that Funnel relay servers do not decrypt traffic between public devices and your device: <https://tailscale.com/docs/features/tailscale-funnel>.
- TLS is terminated on the user's device by Tailscale, then proxied to Onibi's local HTTPS listener.
- Onibi auth is unchanged: single-use pair token first, then owner cookie on every protected page and WebSocket upgrade.

Operational notes:

- The Mac must be logged in to Tailscale and allowed by tailnet policy to use Funnel.
- Public DNS for the Funnel URL can lag after enablement.
- Use `onibi doctor` with `transport.mode: tailscale` or `transport.mode: auto` to check the binary, daemon state, and Funnel capability.

## Telegram

`onibi up --transport=telegram` starts a tmux-backed session and a Telegram long-polling bridge. Setup is optional and explicit:

```bash
onibi telegram setup
onibi up --transport=telegram
```

Natural text input:

- `/new shell` starts a shell session.
- Sending `ls` writes `ls` + Enter to the target session.
- Onibi replies with recent stripped terminal text output.
- `/new claude` or `/new codex` works the same way for agent sessions.

Approval/control:

- Pending tool calls appear as Telegram approval cards with Approve/Deny buttons.
- Use `/edit <approval-id> <edited JSON>` to edit a tool input.
- `/interrupt`, `/esc`, `/enter`, `/kill`, `/show`, `/hide`, and `/end` control the selected target.

Security model:

- Telegram is plaintext to Telegram Bot API infrastructure; use web/Tailscale when you need live terminal UX without chat-provider retention.
- Onibi stores the BotFather token in the OS keystore when available and pairs a single owner chat id through a one-time `/start <code>`.
- Non-owner chats are ignored after pairing.

## Matrix

`onibi up --transport=matrix` requires:

```bash
ONIBI_MATRIX_HOMESERVER=https://matrix.example
ONIBI_MATRIX_ACCESS_TOKEN=...
ONIBI_MATRIX_ROOM_ID='!room:example.org'
```

Optional:

```bash
ONIBI_MATRIX_OWNER_USER_ID='@owner:example.org'
ONIBI_MATRIX_ALLOW_ENCRYPTED=0
```

Onibi validates the configured room through Matrix Client-Server APIs, checks the bot user's room power level, refuses encrypted rooms by default, stores the room-scoped `/sync` `next_batch` token for reconnect, sends terminal output with `m.room.message`, and surfaces homeserver errors. `ONIBI_MATRIX_ALLOW_ENCRYPTED=1` only bypasses the guard for send-only testing; Onibi does not implement Matrix Olm/Megolm E2EE.

## Slack

`onibi up --transport=slack` requires:

```bash
ONIBI_SLACK_APP_TOKEN=xapp-...
ONIBI_SLACK_BOT_TOKEN=xoxb-...
```

Optional:

```bash
ONIBI_SLACK_ALLOWED_CHANNELS=C123,C456
ONIBI_SLACK_ALLOWED_DM_USERS=U123,U456
ONIBI_SLACK_APPROVAL_CHANNEL=C123
```

Onibi opens a Socket Mode WebSocket with `apps.connections.open`, acknowledges every event envelope, refreshes the socket URL on Slack disconnect/refresh events, routes allowed message events to the current session, and accepts approval button callbacks.

## Discord

`onibi up --transport=discord` requires:

```bash
ONIBI_DISCORD_TOKEN=...
```

Optional:

```bash
ONIBI_DISCORD_ALLOWED_IDS=<channel-or-user-id>,...
ONIBI_DISCORD_APPLICATION_ID=...
ONIBI_DISCORD_GUILD_ID=...
```

Run `onibi discord register --guild-id <guild>` to register `/onibi text:<input>` for one guild, or omit `--guild-id` for global registration. Onibi connects to the Discord Gateway, sends Identify or Resume when possible, tracks heartbeats/ACKs, reconnects on Gateway reconnect/invalid-session opcodes, routes DM or allowed guild-channel messages, and routes `/onibi` slash-command text when message content is unavailable.

## Chat redaction

Chat providers receive terminal text through third-party infrastructure. By default, Onibi redacts common secret patterns in outbound chat terminal output and approval payloads. Configure provider output limits with:

```bash
onibi config set provider.output.max_chunks 8
onibi config set provider.output.max_bytes 24576
onibi config set provider.output.redaction strict
```

`provider.output.redaction` accepts `default`, `strict`, or `off`. Set `ONIBI_CHAT_UNREDACTED=1` only when you intentionally want raw chat output.

## Notify-only

Notify-only providers never route terminal text input. They subscribe to approval requests and publish alert text only.

Pushover:

```bash
ONIBI_PUSHOVER_TOKEN=...
ONIBI_PUSHOVER_USER_KEY=...
onibi up --transport=pushover
```

Pushover approval alerts use emergency priority, poll the returned receipt, and write send/receipt/ack/expiry audit rows.

ntfy:

```bash
ONIBI_NTFY_TOPIC=<20+ char random secret>
ONIBI_NTFY_BASE_URL=https://ntfy.sh
ONIBI_NTFY_TOKEN=...
onibi up --transport=ntfy
```

The ntfy topic is treated as a secret; short, repeated, single-class, or guessable topics are rejected. WebSocket subscribe supports replay smoke checks with `since`.

Gotify:

```bash
ONIBI_GOTIFY_URL=https://gotify.example
ONIBI_GOTIFY_APP_TOKEN=...
ONIBI_GOTIFY_CLIENT_TOKEN=...
onibi up --transport=gotify
```

Gotify sends approval notifications through the REST message endpoint, validates the optional client token before startup, writes send/error audit rows, and has a WebSocket receive client for smoke tests.

## Coverage status

`onibi up` prints provider coverage in the picker. `onibi doctor --transport=<mode>` runs the matching preflight check and reports missing env, weak ntfy topics, missing relay binaries, and provider API failures before startup. `onibi status` summarizes recent notify audit state, and `onibi log --notify` filters notify rows.

## Cloudflare Quick Tunnel

`onibi up --transport=cloudflare-quick` runs:

```text
cloudflared tunnel --url https://localhost:<web-port>
```

Onibi parses the emitted `https://*.trycloudflare.com` URL, waits for activation, and prints a QR with the relay key in the URL fragment:

```text
https://example.trycloudflare.com/pair/<token>#k=<relay-key>
```

Security model:

- Cloudflare can observe host, path, timing, and connection metadata.
- Terminal bytes, WebSocket event payloads, control requests, approval decisions, and user input are encrypted at the app layer with per-pair HKDF + AES-GCM.
- The raw relay key is only in the URL fragment and volatile server memory. SQLite stores only an HMAC commitment.
- On shutdown, Onibi kills the `cloudflared` process and logs cleanup.

## Cloudflare Named Tunnel

`onibi up --transport=cloudflare-named` requires:

```bash
ONIBI_CLOUDFLARE_TUNNEL_NAME=<name-or-id>
ONIBI_CLOUDFLARE_HOSTNAME=<hostname>
```

Optional:

```bash
ONIBI_CLOUDFLARED_BIN=/path/to/cloudflared
ONIBI_CLOUDFLARE_TEARDOWN=0
```

Onibi validates `cloudflared tunnel info`, refuses obvious route collisions, runs `cloudflared tunnel run <name-or-id>`, and defaults to no account-side teardown.

## ngrok

`onibi up --transport=ngrok` runs:

```text
ngrok http https://localhost:<web-port>
```

Onibi discovers the public URL from the local Agent API at `http://127.0.0.1:4040/api/tunnels`.

Optional:

```bash
ONIBI_NGROK_AUTHTOKEN=<token>
ONIBI_NGROK_DOMAIN=<reserved-domain>
ONIBI_NGROK_AGENT_API=http://127.0.0.1:4040
```

Reserved domains require an auth token. Cleanup requests tunnel shutdown through the Agent API and then kills the local process.
