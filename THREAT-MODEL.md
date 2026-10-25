# Threat model

Onibi grants a paired browser control over existing Ghostty terminals, and optionally grants a single paired Telegram private chat control over managed terminal sessions. Both surfaces act with the local OS user's authority. The Mac, Ghostty, the configured multiplexer, and their processes are trusted. A paired client can execute commands and terminate work.

The web server binds only to loopback. For remote use, Tailscale Serve terminates private HTTPS and forwards to the local listener. Tailscale, the tailnet access policy, the browser, and the Mac's local network stack are trusted parts of that route. Telegram has its own independent credentials and pairing.

## Protections

- Browser pairing uses a random, single-use secret that expires after ten minutes. Sessions expire after twelve hours, are revoked by logout, and do not survive a server restart. Remote cookies are Secure, HttpOnly and SameSite Strict. Secrets are removed from the URL fragment immediately.
- Web requests require the configured Host/Origin and state-changing requests require a session CSRF token. Loopback proxy hosts are explicitly configured; forwarded headers do not establish trust. There is no public listener or CORS API.
- Web actions claim a durable receipt before execution. Replays return that receipt; crashes leave uncertain actions for inspection. Input text is not persisted in receipts. Restart recovery requires an exclusive state-directory lock.
- Native actions use Ghostty process and surface identities rather than terminal titles or positions. Output capture checks the focused pane before and after reading bounded Accessibility text. Pane/tab/window closure requires a named confirmation in the interface, with the known affected panes. Layout and title controls come from a fixed server allowlist and require a verified focused pane; arbitrary native action strings are not exposed.
- Incoming messages and callbacks require the paired private chat and user ID. Unpaired users cannot select sessions or issue commands.
- `onibi telegram disable` sets a persisted kill switch before removing the local token and pairing. The running daemon checks it before processing updates and delivering queued output, then exits after its current long poll. BotFather token rotation is a separate Telegram-side action.
- Local state and uploaded files use restrictive filesystem permissions. The intake socket is same-user only. Callback data contains opaque tokens rather than command or approval bodies.
- Approvals and Claude questions expire or are cancelled when their waiter goes away. Resolved raw payloads are scrubbed after 24 hours; audit hashes and decision metadata remain.
- Telegram update claims are recorded before side effects. An update interrupted by a crash is reported as uncertain, not blindly replayed.

## Residual risks

- A leaked unused browser pairing link or browser session grants terminal control. Stop and restart `onibi web` to revoke browser access. `onibi telegram disable` only revokes the Telegram surface.
- Focus and output capture span separate operating-system calls; they cannot form one atomic Ghostty transaction. The adapter discards detected focus changes, but cannot prove no focus change occurred and reversed between checks. Output is visible plaintext, not full terminal history.
- Focus-dependent layout/title actions also span separate operating-system calls. A concurrent focus change can race the final check; these calls are not atomic with local interaction.
- Follow reads bounded text only while visible and never automatically focuses a pane. Drafts are per-pane and memory-only; terminal output remains inert text.
- Closing a Ghostty pane, tab, or window may terminate its processes. The confirmation lists the last known inventory, which can change on the Mac before execution. An already executing action can finish during logout or shutdown; revocation cannot undo input already sent.
- Telegram bot chats are not end-to-end encrypted. Telegram and anyone with access to the bot account or paired account can see messages and output.
- Local processes running as the same OS user may read Onibi state or control the multiplexer directly. Onibi does not sandbox commands or protect against a compromised host.
- A leaked bot token can operate the bot outside Onibi. Disabling Onibi does not revoke that token; rotate it with BotFather.
- A decision already in flight when disable is invoked may complete. Disable is a local access cutoff, not a rollback of commands already sent.
- A delayed screenshot can become unavailable if its session exits before the queued capture. Bounded text fallbacks are retained for up to 24 hours, but not full terminal history; for a very early exit there may be no captured fallback yet.
- Adopting an existing mux session gives Onibi the ability to send input and kill that session. Existing Pi/Claude processes do not gain hooks merely by being adopted.

Use a dedicated bot and a non-production OS account for evaluation. Do not send credentials or production secrets through Telegram.
