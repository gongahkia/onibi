# Onibi transports

Onibi v3 groups connection options by control surface:

- Web URL: browser cockpit + QR.
- Chat: natural text control.
- Notify-only: approvals and alerts without terminal input.

The supported transports are:

- `lan` (default): QR points at the Mac's LAN or hotspot address.
- `tailscale`: QR points at the device's Tailscale Funnel URL.
- `wireguard`: QR points at a self-hosted WireGuard interface address.
- `zerotier`: QR points at a ZeroTier-managed virtual network address.
- `cloudflare-quick`: QR points at a temporary `trycloudflare.com` URL.
- `cloudflare-named`: QR points at a configured Cloudflare Tunnel hostname.
- `ngrok`: QR points at an ngrok public HTTPS URL.
- `telegram`: Bot API chat control for natural text input/output and approvals.
- `matrix`: Matrix room control with `/sync` polling and room-power validation.
- `slack`: Slack Socket Mode workspace control.
- `discord`: Discord Gateway bot control with slash-command fallback.
- `zulip`: Zulip stream/topic control with one topic per session.
- `irc`: IRC DM control with SASL auth, intended for libera.chat-style registered bot nicks.
- `pushover`: notify-only approval alerts.
- `ntfy`: topic alerts with optional signed approval actions.
- `gotify`: self-hosted alerts with optional signed approval deep-links.
- `apns`: direct Apple Push Notification service alerts using a user-provided APNs key and native app device token.
- `sms`: Twilio SMS alerts with signed approval links.
- `email`: SMTP alerts with signed approval links through a user-provided MTA.

Signal support uses a local `signal-cli` JSON-RPC daemon and is selectable with `onibi up --transport=signal`. It still needs live verification with a linked Signal number before it should be treated as production-ready.

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

- LAN mode works on home Wi-Fi when the phone can connect directly to the Mac. It also works well over an iPhone hotspot, which is the recommended fallback for hostile Wi-Fi.
- Enterprise or managed Wi-Fi often enables client isolation. If the phone cannot load the pair URL, move both devices to the iPhone hotspot instead of repeatedly retrying that SSID.
- Onibi prefers routable IPv4 LAN addresses first, then IPv6 addresses. IPv6 URLs use bracket syntax.
- `.local` mDNS is secondary; Onibi prefers LAN IP QR URLs because mobile mDNS reachability is inconsistent.
- Explicit LAN mode fails early with `lan_unreachable` when Onibi cannot detect a routable LAN or `.local` host. Use hotspot, `--transport=tailscale`, `--transport=wireguard`, `--transport=zerotier`, `--transport=cloudflare-quick`, or `--transport=ngrok`.
- The live smoke matrix for home Wi-Fi, hotspot, client isolation, captive Wi-Fi, IPv4/IPv6, and auto fallback is in [`docs/transport-smoke.md`](./transport-smoke.md#lan-and-hotspot).

## Tailscale Funnel

`onibi up --transport=tailscale` keeps the same local HTTPS cockpit, enables Funnel in the background, and prints a QR for a URL such as:

```text
https://device-name.tailnet.ts.net/pair/<token>
```

Implementation details:

- Onibi checks `tailscale status --json`; `BackendState` must be `Running`, and the local node must expose the Tailscale HTTPS, Funnel, and Funnel-port capability for public port `443`.
- Onibi runs `tailscale funnel --bg https+insecure://localhost:<web-port>`. The `https+insecure` target is intentional because Onibi's local listener is HTTPS-only with a local CA certificate.
- Onibi prefers `tailscale funnel status --json`, falls back to `tailscale serve status --json` for older Tailscale 1.x output, and builds the QR URL from the active `AllowFunnel` host.
- On shutdown, Onibi runs `tailscale funnel --bg off`.

Security model:

- Public clients connect to the Tailscale Funnel URL over HTTPS.
- Tailscale documents that Funnel relay servers do not decrypt traffic between public devices and your device: <https://tailscale.com/docs/features/tailscale-funnel>.
- TLS is terminated on the user's device by Tailscale, then proxied to Onibi's local HTTPS listener.
- Onibi auth is unchanged: single-use pair token first, then owner cookie on every protected page and WebSocket upgrade.

Operational notes:

- The Mac must be logged in to Tailscale and allowed by tailnet policy to use Funnel.
- Public DNS for the Funnel URL can lag after enablement.
- Use `onibi doctor --transport=tailscale` or `onibi doctor --transport=auto` to check the binary, daemon login state, HTTPS capability, Funnel node attribute, and allowed public Funnel port.

## WireGuard

`onibi up --transport=wireguard` keeps the cockpit private to a user-managed WireGuard network. Onibi checks `wg show interfaces`, finds a routable address on a WireGuard interface, binds the HTTPS listener to that address only, and prints a QR such as:

```text
https://10.8.0.2:8443/pair/<token>
```

Use `ONIBI_WIREGUARD_INTERFACE=wg0` when several WireGuard interfaces are up. Use `ONIBI_WIREGUARD_BIN=/path/to/wg` when `wg` is not on `PATH`.

Security model:

- WireGuard carries packets between the laptop and phone; Onibi does not manage server keys or peer authorization.
- Onibi auth is unchanged: single-use pair token first, then owner cookie on every protected page and WebSocket upgrade.
- The local HTTPS certificate must include the WireGuard interface IP. This is automatic when the interface is up before `onibi up` starts.

Operational notes:

- Configure WireGuard on the laptop and phone first; see [`wireguard-setup.md`](./wireguard-setup.md).
- `onibi doctor --transport=wireguard` verifies the `wg` binary, checks `wg show interfaces`, and confirms the selected interface has a routable IP.
- If the phone cannot open the QR URL, verify both peers are connected and that the phone can reach the laptop's WireGuard IP with another HTTPS service before debugging Onibi.

## ZeroTier

`onibi up --transport=zerotier` keeps the cockpit private to a user-managed ZeroTier network. Onibi checks `zerotier-cli info`, reads `zerotier-cli listnetworks -j`, selects a joined network with status `OK`, binds the HTTPS listener to that network IP only, and prints a QR such as:

```text
https://10.147.20.4:8443/pair/<token>
```

Use `ONIBI_ZEROTIER_NETWORK=<network-id-or-name>` when the laptop belongs to several ZeroTier networks. Use `ONIBI_ZEROTIER_BIN=/path/to/zerotier-cli` when the CLI is not on `PATH`.

Security model:

- ZeroTier carries packets between the laptop and phone; Onibi does not manage network authorization.
- Onibi auth is unchanged: single-use pair token first, then owner cookie on every protected page and WebSocket upgrade.
- The local HTTPS certificate must include the ZeroTier IP. This is automatic when the network is joined before `onibi up` starts.

Operational notes:

- Join the laptop and phone to the same ZeroTier network first; see [`zerotier-setup.md`](./zerotier-setup.md).
- `onibi doctor --transport=zerotier` verifies `zerotier-one` is online, checks network membership, and confirms the selected network has a routable IP.
- If the phone cannot open the QR URL, verify the phone is authorized in the ZeroTier network and can reach the laptop's ZeroTier IP with another HTTPS service before debugging Onibi.

## Telegram

`onibi up --transport=telegram` starts a tmux-backed session and a Telegram long-polling bridge. Setup is optional and explicit:

```bash
onibi telegram setup
onibi telegram status --check
onibi up --transport=telegram
```

Use `onibi telegram setup --no-check --token "$ONIBI_TELEGRAM_TOKEN"` only when you need offline secret-store setup; run `onibi telegram status --check` before live use.

Natural text input:

- `/new shell` starts a shell session.
- Sending `ls` writes `ls` + Enter to the target session.
- Onibi replies with recent stripped terminal text output.
- `/new claude` or `/new codex` works the same way for agent sessions.

Approval/control:

- Pending tool calls appear as Telegram approval cards with Approve/Deny buttons.
- Use `/edit <approval-id> <edited JSON>` to edit a tool input.
- `/interrupt`, `/esc`, `/enter`, `/kill`, `/show`, `/hide`, and `/end` control the selected target.
- Long-poll drops reconnect with exponential backoff from 1s to 60s, then reset after the next successful poll.
- Sends are paced against Telegram Bot API guidance: roughly 30 messages/sec globally and 1 message/sec per chat, with 429 `retry_after` respected. Text-in, approval button clicks, and tail chunks are written to local audit with payload hashes.

Security model:

- Telegram is plaintext to Telegram Bot API infrastructure; use web LAN, WireGuard, ZeroTier, or Tailscale when you need live terminal UX without chat-provider retention.
- Telegram secret chats are MTProto client features, not Bot API features; Onibi does not tunnel Bot API control through secret chats.
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

Onibi validates the configured room through Matrix Client-Server APIs, checks the bot user's room power level, refuses encrypted rooms by default, stores the room-scoped `/sync` `next_batch` token for reconnect, sends terminal output with `m.room.message`, and surfaces homeserver errors. Approval prompts are sent as room messages; react `✅` or `👍` to approve, react `❌` or `👎` to deny. Matrix text-in, tail chunks, approval sends, and reactions write `provider.matrix.*` audit rows. Local E2EE groundwork includes Client-Server key endpoint shapes, SAS verification to-device message shapes, and encrypted crypto-state storage, but `ONIBI_MATRIX_ALLOW_ENCRYPTED=1` only bypasses the guard for send-only testing, records `provider.matrix.encrypted_bypass`, and still sends plaintext `m.room.message` events; Onibi does not implement Matrix Olm/Megolm E2EE.

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

Onibi opens a Socket Mode WebSocket with `apps.connections.open`, acknowledges every event envelope, refreshes the socket URL on Slack disconnect/refresh events with jittered backoff, routes allowed message events to the current session, and accepts approval button callbacks. Approval cards include approve, deny, and edit actions; edit opens a Slack modal for JSON input and updates the original approval message to approved/denied/edited/expired/failed final state. Slack text-in, tail chunks, button clicks, edit modal opens/submits, and tail send errors are written to local audit with payload hashes instead of plaintext detail.

Slack's 2026 Block Kit [Card, Alert, and Carousel](https://docs.slack.dev/changelog/2026/04/16/block-kit-new-blocks/) additions, [Data Table](https://docs.slack.dev/reference/block-kit/blocks/data-table-block/), and [Work Objects](https://docs.slack.dev/messaging/work-objects-overview/) are richer presentation options. Onibi keeps command approval messages on section/actions/modal primitives because those are enough for approve/deny/edit parity and keep message updates portable across workspaces.

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

Run `onibi discord register --guild-id <guild>` to register `/onibi text:<input>` for one guild, or omit `--guild-id` for global registration. Onibi connects to the Discord Gateway, sends Identify or Resume when possible, tracks heartbeats/ACKs, reconnects on Gateway reconnect/invalid-session opcodes with jittered backoff, routes DM or allowed guild-channel messages, and routes `/onibi` slash-command text when message content is unavailable. Approval requests are sent as Discord Components v2 messages with approve, deny, and edit buttons; edit opens a modal for JSON input. Session output is posted to a per-session thread when thread creation succeeds, and falls back to the parent channel on Discord permission/API errors. Discord text-in, tail chunks, approval buttons, modal opens/submits, thread creation, and send/thread errors are written to local audit with payload hashes instead of plaintext detail. `onibi doctor --transport=discord` reports app auth, channel visibility, optional slash-command presence, and gated send permission failures.

## Zulip

`onibi up --transport=zulip` requires a Zulip bot user API key:

```bash
ONIBI_ZULIP_URL=https://example.zulipchat.com
ONIBI_ZULIP_EMAIL=onibi-bot@example.zulipchat.com
ONIBI_ZULIP_API_KEY=...
ONIBI_ZULIP_STREAM=onibi
onibi up --transport=zulip
```

Optional:

```bash
ONIBI_ZULIP_TOPIC_PREFIX=onibi-
ONIBI_ZULIP_OWNER_EMAIL=owner@example.com
```

Create an Onibi bot user in Zulip and copy its email/API key from the Zulip account/bot settings page. The bridge uses Zulip's REST API with Basic auth, `POST /api/v1/messages` for stream-topic messages, `POST /api/v1/register` plus `GET /api/v1/events` for long-poll event queues, and `DELETE /api/v1/events` during cleanup. Official API references: <https://zulip.com/api/send-message>, <https://zulip.com/api/register-queue>, <https://zulip.com/api/get-events>, and <https://zulip.com/api/delete-queue>.

Onibi creates one Zulip topic per session: `<topic-prefix><session-id>`. Messages from `ONIBI_ZULIP_OWNER_EMAIL` in that topic are written to the matching PTY. Approval prompts are posted into the same topic; reply `approve <id>` or `deny <id>` to decide. Session output is sent back as topic messages with provider output redaction/truncation. Zulip event-queue API errors reconnect with bounded backoff and write `provider.zulip.reconnect`, `provider.zulip.text_in`, `provider.zulip.tail_chunk`, `provider.zulip.tail_error`, `provider.zulip.approval_sent`, and `provider.zulip.approval_error` audit rows. `onibi doctor --transport=zulip` checks required env and event-queue access.

## IRC

`onibi up --transport=irc` requires a registered bot nick. libera.chat supports SASL setup for automatic login and IRCv3 documents SASL PLAIN over `CAP REQ :sasl` plus `AUTHENTICATE PLAIN`.

```bash
ONIBI_IRC_ADDR=irc.libera.chat:6697
ONIBI_IRC_NICK=onibi-bot
ONIBI_IRC_USERNAME=onibi-bot
ONIBI_IRC_PASSWORD=<nickserv-password>
ONIBI_IRC_OWNER_NICK=<your-nick>
onibi up --transport=irc
```

Optional:

```bash
ONIBI_IRC_PLAINTEXT=1
```

The default connection uses TLS. `ONIBI_IRC_PLAINTEXT=1` exists for local test servers only. IRC has no Onibi-supported E2EE path; traffic is visible to the IRC network/server unless your IRC network provides its own encryption layer. Use web LAN, WireGuard, ZeroTier, or Tailscale when terminal text must not pass through a chat network.

The bridge only accepts direct `PRIVMSG` messages from `ONIBI_IRC_OWNER_NICK` to the bot nick. Send `!onibi approve <id>` or `!onibi deny <id>` to decide approvals. Any other owner DM is written to the active PTY. Session output and approval prompts are returned as owner DMs, chunked at 400 chars for IRC line safety. Sends are paced at one message every two seconds to match libera.chat's documented message-rate guidance. IRC reconnect, text input, tail chunks/errors, and approval send/errors write `provider.irc.*` audit rows. `onibi doctor --transport=irc` checks required env and SASL connect reachability.

References: <https://libera.chat/guides/sasl>, <https://libera.chat/guides/faq>, <https://ircv3.net/specs/extensions/sasl-3.1>, and <https://datatracker.ietf.org/doc/html/rfc2812>.

## Signal

Signal integration uses a local `signal-cli` JSON-RPC daemon over HTTP:

```bash
ONIBI_SIGNAL_RPC_URL=http://127.0.0.1:6001
ONIBI_SIGNAL_ACCOUNT=+15551234567
ONIBI_SIGNAL_RECIPIENT=+15557654321
onibi up --transport=signal
```

The client in `internal/signal` wraps `POST /api/v1/rpc`, `GET /api/v1/events`, and `GET /api/v1/check`. The daemon bridge checks the local daemon, posts approval prompts into the target conversation, maps `👍`/`✅` reactions on Onibi approval messages to approve and `👎`/`❌` reactions to deny, writes owner text messages to the active PTY, returns chunked terminal output, reconnects the events stream with bounded backoff, and writes `provider.signal.*` audit rows. Mock coverage lives in `go test -race ./internal/signal -run TestParityAxes`; live smoke is opt-in with a linked account:

```bash
ONIBI_LIVE_SIGNAL=1 ONIBI_SIGNAL_RPC_URL=http://127.0.0.1:6001 ONIBI_SIGNAL_ACCOUNT=+15551234567 ONIBI_SIGNAL_RECIPIENT=+15557654321 go test ./internal/signal -run LiveSignal
```

See [`signal-setup.md`](./signal-setup.md) for install, phone-link, daemon, and local security notes. [Unverified] Real linked-number reaction, text-in, and tail delivery still require live Signal smoke because reactions identify the target message by author and timestamp.

## Chat redaction

Chat providers receive terminal text through third-party infrastructure. By default, Onibi redacts common secret patterns in outbound chat terminal output and approval payloads. Configure provider output limits with:

```bash
onibi config set provider.output.max_chunks 8
onibi config set provider.output.max_bytes 24576
onibi config set provider.output.redaction strict
onibi config set provider.output.slack.max_bytes 12000
onibi config set provider.output.discord.redaction off
onibi config set provider.output.zulip.max_chunks 4
onibi config set provider.output.irc.max_bytes 2048
onibi config set provider.output.signal.redaction strict
```

`provider.output.redaction` accepts `default`, `strict`, or `off`. Provider-specific overrides exist for `telegram`, `matrix`, `slack`, `discord`, `zulip`, `irc`, `signal`, and `notify`; use `inherit` to clear an override. Set `ONIBI_CHAT_UNREDACTED=1` only when you intentionally want raw chat output.

## Notify-only

Notify-only providers never route terminal text input. They subscribe to approval requests and publish alert text only.

Pushover:

```bash
ONIBI_PUSHOVER_TOKEN=...
ONIBI_PUSHOVER_USER_KEY=...
onibi up --transport=pushover
```

Create a Pushover application to get `ONIBI_PUSHOVER_TOKEN`, then copy the target user's User Key into `ONIBI_PUSHOVER_USER_KEY`. Pushover approval alerts use emergency priority (`priority=2`, retry, expire), poll the returned receipt, map acknowledged receipts to approve, and write send/receipt/ack/expiry/approve audit rows. Pushover has one supplementary message URL plus native emergency acknowledgment/callback support; Onibi's default local path uses the native acknowledgment receipt for approval.

ntfy:

```bash
ONIBI_NTFY_TOPIC=<20+ char random secret>
ONIBI_NTFY_BASE_URL=https://ntfy.sh
ONIBI_NTFY_TOKEN=...
ONIBI_NTFY_ACTION_BASE_URL=https://<reachable-onibi-web-origin>
onibi up --transport=ntfy
```

The ntfy topic is treated as a secret; short, repeated, single-class, or guessable topics are rejected. When `ONIBI_NTFY_ACTION_BASE_URL` is set, approval alerts include two ntfy `http` action buttons for Approve/Deny. The action URLs are HMAC-signed, single-use, and expire after 5 minutes. JSON stream subscribe uses `<topic>/json`, replays with `since`, and reconnects with the last message id. As of the 2026-07-10 docs check, official ntfy publish/subscribe docs cover HTTPS, authenticated topics, access tokens, click/actions fields, and JSON streams, while native client E2E remains tracked upstream in ntfy issue #69; Onibi does not claim native ntfy password E2E via `X-Message-Encryption`. References: <https://docs.ntfy.sh/publish/>, <https://docs.ntfy.sh/subscribe/api/>, <https://github.com/binwiederhier/ntfy/issues/69>.

Gotify:

```bash
ONIBI_GOTIFY_URL=https://gotify.example
ONIBI_GOTIFY_APP_TOKEN=...
ONIBI_GOTIFY_CLIENT_TOKEN=...
ONIBI_GOTIFY_ACTION_BASE_URL=https://<reachable-onibi-web-origin>
onibi up --transport=gotify
```

Create a Gotify application token for sending and a client token for receiving. Gotify sends approval notifications through `POST /message` with the app token. When `ONIBI_GOTIFY_ACTION_BASE_URL` is set, messages include `client::notification.click.url` pointing at a signed Onibi approval page; that page is single-use, expires after 5 minutes, and renders Approve/Deny POST actions. The optional client token enables `/stream` WebSocket receive checks with reconnect backoff. Send/retry/error audit rows are written for every approval push.

APNs:

```bash
ONIBI_APNS_KEY_PATH=/secure/path/AuthKey_ABC123DEFG.p8
ONIBI_APNS_KEY_ID=ABC123DEFG
ONIBI_APNS_TEAM_ID=TEAM123456
ONIBI_APNS_TOPIC=com.example.OnibiCompanion
ONIBI_APNS_DEVICE_TOKEN=<hex-device-token>
ONIBI_APNS_ENV=production
onibi up --transport=apns
```

APNs is notify-only and intended for users who bring their own Apple Developer account, APNs auth key, bundle topic, and native app device token. PWA-only Onibi cannot mint a native APNs device token. Approval notifications are sent as alert pushes with `apns-push-type=alert`, priority 10, a 30s TTL, and an `approval_id` custom payload field. APNs send/retry/error rows are written to local audit. When APNs env is absent, Onibi keeps the existing web-push notifier as the fallback path. See [`apns-setup.md`](./apns-setup.md).

SMS:

```bash
ONIBI_TWILIO_ACCOUNT_SID=AC...
ONIBI_TWILIO_AUTH_TOKEN=...
ONIBI_TWILIO_FROM=+15551234567
# or:
ONIBI_TWILIO_MESSAGING_SERVICE_SID=MG...
ONIBI_SMS_TO=+15557654321
ONIBI_SMS_ACTION_BASE_URL=https://onibi.example.com
onibi up --transport=sms
```

Create a Twilio account, buy or verify a sender, and use either `ONIBI_TWILIO_FROM` or a Messaging Service SID. Twilio SMS is not free; live probes and approval sends can bill your Twilio account. SMS is notify-only and does not accept terminal text input. Each approval SMS contains Approve/Deny URLs signed by Onibi, single-use, and valid for 5 minutes. `ONIBI_SMS_ACTION_BASE_URL` must route to this Onibi daemon's web listener; `onibi up --transport=sms` starts that listener when the action base URL is configured. Sends, retries, and errors write `notify.sms.*` audit rows.

Email:

```bash
ONIBI_SMTP_ADDR=smtp.example.com:587
ONIBI_SMTP_HOST=smtp.example.com
ONIBI_SMTP_USERNAME=onibi@example.com
ONIBI_SMTP_PASSWORD=...
ONIBI_EMAIL_FROM=onibi@example.com
ONIBI_EMAIL_TO=owner@example.com
ONIBI_EMAIL_ACTION_BASE_URL=https://onibi.example.com
onibi up --transport=email
```

Email uses the Go standard library SMTP client and a bring-your-own MTA. `ONIBI_SMTP_USERNAME` and `ONIBI_SMTP_PASSWORD` are optional for local or trusted relays; most public SMTP services require them. Email is notify-only and does not accept terminal text input. Each approval email contains Approve/Deny URLs signed by Onibi, single-use, and valid for 5 minutes. `ONIBI_EMAIL_ACTION_BASE_URL` must route to this Onibi daemon's web listener; `onibi up --transport=email` starts that listener when the action base URL is configured. Sends, retries, and errors write `notify.email.*` audit rows.

## Coverage status

`onibi up` prints provider coverage in the picker. `onibi doctor --transport=<mode>` runs the matching preflight check and reports missing env, weak ntfy topics, missing relay binaries, and provider API failures before startup. `onibi status` summarizes recent notify audit state, and `onibi log --notify` filters notify rows.

## Cloudflare Quick Tunnel

`onibi up --transport=cloudflare-quick` runs:

```text
cloudflared tunnel --url https://localhost:<web-port> --no-tls-verify
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
onibi cloudflare setup
ONIBI_CLOUDFLARE_ACCOUNT_ID=<account-id>
ONIBI_CLOUDFLARE_TUNNEL_ID=<tunnel-id>
ONIBI_CLOUDFLARE_TUNNEL_NAME=<name-or-id>
ONIBI_CLOUDFLARE_HOSTNAME=<hostname>
```

Optional:

```bash
ONIBI_CLOUDFLARED_BIN=/path/to/cloudflared
ONIBI_CLOUDFLARE_API_TOKEN=<token>
ONIBI_CLOUDFLARE_TUNNEL_TOKEN=<token>
ONIBI_CLOUDFLARE_TEARDOWN=0
```

`onibi cloudflare setup` stores the Cloudflare API token as `onibi.cloudflare.token.v1` in the OS secret store, with the same 0600 dotenv fallback used by other Onibi credentials. With `ONIBI_CLOUDFLARE_ACCOUNT_ID` and `ONIBI_CLOUDFLARE_TUNNEL_ID`, Onibi retrieves the tunnel token from the Cloudflare API and starts `cloudflared tunnel run` with `TUNNEL_TOKEN` in the child environment so the token is not placed in process arguments. `ONIBI_CLOUDFLARE_API_TOKEN` and `ONIBI_CLOUDFLARE_TUNNEL_TOKEN` are env-only overrides for automation.

Run `onibi doctor --transport=cloudflare-named` before startup to verify the binary, required named-tunnel settings, stored token state for API-token auth, Cloudflare tunnel-token retrieval, and `cloudflared tunnel info` status. Without keychain/API-token auth, Onibi keeps the existing locally-managed path: it validates `cloudflared tunnel info`, refuses obvious route collisions, runs `cloudflared tunnel run <name-or-id>`, and defaults to no account-side teardown.

## ngrok

`onibi up --transport=ngrok` runs:

```text
ngrok http https://localhost:<web-port>
```

Onibi discovers the public URL from the local Agent API at `http://127.0.0.1:4040/api/tunnels` and accepts only `https://` public URLs. If the Agent API exposes only `http://`, startup fails instead of printing an insecure pairing URL.

Optional:

```bash
onibi ngrok setup --authtoken <token>
ONIBI_NGROK_DOMAIN=<reserved-domain>
ONIBI_NGROK_AGENT_API=http://127.0.0.1:4040
```

`onibi ngrok setup` stores the authtoken as `onibi.ngrok.token.v1` in the OS secret store, with the same 0600 dotenv fallback used by other Onibi credentials. `ONIBI_NGROK_AUTHTOKEN` remains an env-only override for automation. Reserved domains require an auth token.

Run `onibi doctor --transport=ngrok` before startup to verify the `ngrok` binary and reserved-domain token state. Cleanup requests tunnel shutdown through the Agent API and then kills the local process.

ngrok free-plan limits change over time; current ngrok docs list HTTP request rate limits, TCP connection rate limits, monthly HTTP request quotas, and data-transfer quotas. Check https://ngrok.com/docs/pricing-limits/free-plan-limits before using ngrok for long-running or shared demos.
