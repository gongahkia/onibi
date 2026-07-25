# Transports

Onibi is a single-user phone web cockpit. Use a local network path that reaches the Mac running Onibi; pairing and owner authentication remain required on every path.

## LAN and hotspot

`onibi start --transport=lan` starts the local HTTPS cockpit and prints a single-use pair URL. A phone on the same LAN can open that URL after trusting the generated local CA file for its platform. When managed Wi-Fi blocks peer traffic, use a phone hotspot and start Onibi again to obtain a new URL.

## Tailscale Private

`onibi start --transport=tailscale-private` exposes the same cockpit only through the authenticated tailnet. Tailnet reachability is not authorization: the phone still needs the pair URL and owner session.

`tailscale` has been removed because it exposed the cockpit publicly. Existing `transport.mode: tailscale` configuration fails before cockpit startup; select `tailscale-private`, LAN, WireGuard, ZeroTier, Cloudflare Quick, or ngrok instead.

## Other web paths

WireGuard, ZeroTier, Cloudflare, and ngrok are web transport implementations with their own setup and operational constraints. Public relays require Onibi relay E2E; see [Cloudflare Quick setup](./cloudflare-quick-setup.md) and [ngrok setup](./ngrok-setup.md). They do not add a hosted Onibi account, multi-user control plane, remote host management, chat bridge, or notification provider.

Run `onibi system doctor --transport=<mode>` before a non-LAN start and use [`transport-smoke.md`](./transport-smoke.md) for device validation.

## Telegram chat cockpit

Telegram is a beta, owner-only text cockpit, not a web transport or live terminal. Start it with `onibi start --transport=telegram`; its one-time owner enrollment, local credential storage, session controls, approval callbacks, and bounded/redacted output operate without browser pairing, Web Push, or an open PWA page.

The bridge uses capped exponential long-poll reconnect backoff and Telegram Bot API send limits (30 messages/second globally, one message/second per chat). It audits text input, approval callbacks, and output chunks with payload hashes rather than raw payloads. Bot API messages are not treated as end-to-end encrypted: Telegram's [FAQ](https://telegram.org/faq) distinguishes end-to-end-encrypted Secret Chats from Cloud Chats. See [Telegram Chat Cockpit](./telegram.md) for capability reporting and the secret-gated live verification runbook.

## IRC experimental cockpit

IRC is an explicit experimental text cockpit for a registered Libera.Chat bot. It is not enabled by default, is not a live terminal, and is not certified alongside the local web cockpit or Telegram. It uses verified TLS to `irc.libera.chat:6697` and SASL PLAIN; create a dedicated registered bot account before setup. Libera documents its TLS endpoint in the [connection guide](https://libera.chat/guides/connect) and SASL PLAIN setup in its [SASL guide](https://libera.chat/guides/irssi).

Run `onibi irc setup --nick <bot-nick> --account <account> --owner-nick <your-nick>`, retain the generated owner token, then run `onibi irc status --check` and `onibi start --transport=irc`. From a private message to the bot, every input must use `!onibi <token> <text>`. Controls use the same prefix, for example `!onibi <token> /approve <id>` and `!onibi <token> /deny <id>`.

The per-message token is required because this transport does not rely on nickname or WHOIS identity as owner authentication. Invalid tokens receive no reply. Output is redacted, capped, split to at most 400 characters per IRC message, and rate-limited to one message per second. IRC is not end-to-end encrypted: Libera infrastructure receives the bot's private-message contents, including the owner token and bounded/redacted terminal output. Rotate credentials with `onibi irc disable` followed by `onibi irc setup` if the token or password is exposed.

The opt-in live smoke records redacted TLS/SASL and private-message artifacts: set `ONIBI_LIVE_IRC_NICK`, `ONIBI_LIVE_IRC_ACCOUNT`, `ONIBI_LIVE_IRC_PASSWORD`, and `ONIBI_LIVE_IRC_OWNER_NICK`, then run `go test ./internal/irc -run '^TestLiveIRC$' -count=1`. Review the artifact before promoting any IRC capability claim.
