# Threat model

Onibi grants a single paired Telegram private chat remote control over terminal sessions running as the local OS user. That account and its processes, the configured multiplexer, the bot token, and Telegram's transport are trusted dependencies. A paired Telegram account is effectively a remote keyboard for the selected session, not a read-only viewer.

## Protections

- Incoming messages and callbacks require the paired private chat and user ID. Unpaired users cannot select sessions or issue commands.
- `onibi telegram disable` sets a persisted kill switch before removing the local token and pairing. The running daemon checks it before processing updates and delivering queued output, then exits after its current long poll. BotFather token rotation is a separate Telegram-side action.
- Local state and uploaded files use restrictive filesystem permissions. The intake socket is same-user only. Callback data contains opaque tokens rather than command or approval bodies.
- Approvals and Claude questions expire or are cancelled when their waiter goes away. Resolved raw payloads are scrubbed after 24 hours; audit hashes and decision metadata remain.
- Telegram update claims are recorded before side effects. An update interrupted by a crash is reported as uncertain, not blindly replayed.

## Residual risks

- Telegram bot chats are not end-to-end encrypted. Telegram and anyone with access to the bot account or paired account can see messages and output.
- Local processes running as the same OS user may read Onibi state or control the multiplexer directly. Onibi does not sandbox commands or protect against a compromised host.
- A leaked bot token can operate the bot outside Onibi. Disabling Onibi does not revoke that token; rotate it with BotFather.
- A decision already in flight when disable is invoked may complete. Disable is a local access cutoff, not a rollback of commands already sent.
- A delayed screenshot can become unavailable if its session exits before the queued capture. Bounded text fallbacks are retained for up to 24 hours, but not full terminal history; for a very early exit there may be no captured fallback yet.
- Adopting an existing mux session gives Onibi the ability to send input and kill that session. Existing Pi/Claude processes do not gain hooks merely by being adopted.

Use a dedicated bot and a non-production OS account for evaluation. Do not send credentials or production secrets through Telegram.
