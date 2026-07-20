# Transports

Onibi is a single-user phone web cockpit. Use a local network path that reaches the Mac running Onibi; pairing and owner authentication remain required on every path.

## LAN and hotspot

`onibi up --transport=lan` starts the local HTTPS cockpit and prints a single-use pair URL. A phone on the same LAN can open that URL after trusting the generated local CA profile. When managed Wi-Fi blocks peer traffic, use the iPhone hotspot and start Onibi again to obtain a new URL.

## Tailscale Private

`onibi up --transport=tailscale-private` exposes the same cockpit only through the authenticated tailnet. Tailnet reachability is not authorization: the phone still needs the pair URL and owner session.

`tailscale` has been removed because it exposed the cockpit publicly. Existing `transport.mode: tailscale` configuration fails before cockpit startup; select `tailscale-private`, LAN, WireGuard, ZeroTier, Cloudflare Quick, or ngrok instead.

## Other web paths

WireGuard, ZeroTier, Cloudflare, and ngrok are web transport implementations with their own setup and operational constraints. Public relays require Onibi relay E2E; see [Cloudflare Quick setup](./cloudflare-quick-setup.md) and [ngrok setup](./ngrok-setup.md). They do not add a hosted Onibi account, multi-user control plane, remote host management, chat bridge, or notification provider.

Run `onibi doctor --transport=<mode>` before a non-LAN start and use [`transport-smoke.md`](./transport-smoke.md) for device validation.

## Telegram chat cockpit

Telegram is an experimental, owner-only text cockpit, not a web transport or live terminal. Start it with `onibi up --transport=telegram`; its one-time owner enrollment, local credential storage, session controls, approval callbacks, and bounded/redacted output operate without browser pairing, Web Push, or an open PWA page.

The bridge uses capped exponential long-poll reconnect backoff and Telegram Bot API send limits (30 messages/second globally, one message/second per chat). It audits text input, approval callbacks, and output chunks with payload hashes rather than raw payloads. Bot API messages are not treated as end-to-end encrypted: Telegram's [FAQ](https://telegram.org/faq) distinguishes end-to-end-encrypted Secret Chats from Cloud Chats. See [Telegram Chat Cockpit](./telegram.md) for capability reporting and the secret-gated live verification runbook.

## IRC (experimental provider)

The unexposed IRC provider uses TLS by default for `irc.libera.chat:6697` and authenticates a registered bot with SASL PLAIN. Libera.Chat [recommends TLS](https://libera.chat/guides/sasl) for SASL PLAIN and publishes `irc.libera.chat:6697` as its TLS endpoint. Store the NickServ password only in Onibi's secret storage when this provider is exposed through an approved experimental profile.

IRC is not end-to-end encrypted. A mapped owner DM accepts `!onibi approve <id>` and `!onibi deny <id>`; ordinary owner DM text is delivered to the configured session. Output is capped at 400 characters per line, paced conservatively at one message per second, and audited as compact interaction metadata with payload hashes. The provider reconnects with capped exponential backoff. Its live check requires a registered bot and is intentionally secret-gated; no real Libera.Chat result is committed in this repository state.
