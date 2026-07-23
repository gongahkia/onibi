# Transports

Onibi is a single-user phone web cockpit. Use a local network path that reaches the Mac running Onibi; pairing and owner authentication remain required on every path.

## LAN and hotspot

`onibi up --transport=lan` starts the local HTTPS cockpit and prints a single-use pair URL. A phone on the same LAN can open that URL after trusting the generated local CA file for its platform. When managed Wi-Fi blocks peer traffic, use a phone hotspot and start Onibi again to obtain a new URL.

## Tailscale Private

`onibi up --transport=tailscale-private` exposes the same cockpit only through the authenticated tailnet. Tailnet reachability is not authorization: the phone still needs the pair URL and owner session.

`tailscale` has been removed because it exposed the cockpit publicly. Existing `transport.mode: tailscale` configuration fails before cockpit startup; select `tailscale-private`, LAN, WireGuard, ZeroTier, Cloudflare Quick, or ngrok instead.

## Other web paths

WireGuard, ZeroTier, Cloudflare, and ngrok are web transport implementations with their own setup and operational constraints. Public relays require Onibi relay E2E; see [Cloudflare Quick setup](./cloudflare-quick-setup.md) and [ngrok setup](./ngrok-setup.md). They do not add a hosted Onibi account, multi-user control plane, remote host management, chat bridge, or notification provider.

Run `onibi doctor --transport=<mode>` before a non-LAN start and use [`transport-smoke.md`](./transport-smoke.md) for device validation.

## Telegram chat cockpit

Telegram is an experimental, owner-only text cockpit, not a web transport or live terminal. Start it with `onibi up --transport=telegram`; its one-time owner enrollment, local credential storage, session controls, approval callbacks, and bounded/redacted output operate without browser pairing, Web Push, or an open PWA page.

The bridge uses capped exponential long-poll reconnect backoff and Telegram Bot API send limits (30 messages/second globally, one message/second per chat). It audits text input, approval callbacks, and output chunks with payload hashes rather than raw payloads. Bot API messages are not treated as end-to-end encrypted: Telegram's [FAQ](https://telegram.org/faq) distinguishes end-to-end-encrypted Secret Chats from Cloud Chats. See [Telegram Chat Cockpit](./telegram.md) for capability reporting and the secret-gated live verification runbook.

## IRC (experimental transport)

Status: [Unverified] IRC is opt-in only. Set `experimental.providers=true`, then store a registered bot's TLS/SASL settings with:

```bash
onibi experimental irc setup \
  --nick onibi_bot --username onibi_bot --password '<NickServ password>' \
  --owner-nick owner_nick --owner-account owner_account
onibi up --transport=irc
```

The bridge defaults to `irc.libera.chat:6697`, requires verified TLS and SASL PLAIN, and stores the password in Onibi secret storage. Libera.Chat [recommends TLS](https://libera.chat/guides/sasl) for SASL PLAIN and publishes `irc.libera.chat:6697` as its TLS endpoint. It also requires IRCv3 [`account-tag`](https://ircv3.net/specs/extensions/account-tag): owner text and approval commands are accepted only from the configured services account, never from a nick alone.

IRC is not end-to-end encrypted. The owner DM accepts `!onibi approve <id>` and `!onibi deny <id>`; ordinary owner text is delivered to the managed session. Live session output is sent as owner-DM messages. No approval or output is sent until that owner has sent an account-tag-authenticated DM on the current connection. Output is redacted/bounded before delivery, split at 400 characters, paced at one message per second, and audited with payload hashes. The bridge reconnects with capped exponential backoff. `onibi experimental irc status` reports local setup; `onibi experimental irc disable` removes it. Its live check requires a registered bot and is secret-gated; no real Libera.Chat result is committed in this repository state.
