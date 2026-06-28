# Onibi transports

Onibi v3 groups connection options by control surface:

- Web URL: browser cockpit + QR.
- Chat: natural text control.
- Notify-only: approvals and alerts without terminal input.

The supported transports are:

- `lan` (default): QR points at the Mac's LAN or hotspot address.
- `tailscale`: QR points at the device's Tailscale Funnel URL.
- `telegram`: Bot API chat control for natural text input/output and approvals.

`auto` tries `tailscale` first and falls back to `lan` when Tailscale is unavailable.
Run `onibi up` from a terminal to choose category first, provider second, or pass `--transport=lan`, `--transport=tailscale`, `--transport=telegram`, or `--transport=auto` for scripts.

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

## Connection catalog

These options are not implemented yet and stay grouped by intended surface:

- Web URL: Cloudflare Quick Tunnel, Cloudflare Named Tunnel, and ngrok.
- Chat: Slack Socket Mode, Discord bot, and Matrix bot.
- Notify-only: Pushover.

## Future Cloudflare transports

Cloudflare Quick Tunnel is not part of v3/v1.1. If added later, it must use app-level end-to-end encryption before release because Cloudflare can terminate TLS for ordinary tunnels.
