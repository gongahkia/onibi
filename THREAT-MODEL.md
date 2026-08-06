# Threat Model

Onibi grants one paired Telegram account remote control of local terminal sessions. Treat the bot token, paired Telegram account, local state directory, and local OS account as privileged.

| Threat | Control | Limit |
| --- | --- | --- |
| Unpaired Telegram user | Private-chat owner pairing; callbacks require paired chat and user IDs | Bot token compromise defeats this boundary. |
| Callback replay | Opaque, expiring local tokens; decisions are persisted and idempotent | An unlocked paired phone remains powerful. |
| Dangerous agent action | Native Codex/Pi decision cards; high-risk Pi confirmation | Approving an action authorizes it. |
| Generic terminal prompt | Screen plus literal keys only | Onibi does not infer prompt semantics. |
| Network observer or Telegram | No browser/relay service is exposed | Telegram bot messages are not E2E encrypted. |
| Same-user malware | Out of scope | It can read local state and control tmux. |

Never expose Codex App Server `thread/shellCommand`; it bypasses the thread sandbox. Keep state and logs private, rotate the bot token and run `onibi telegram disable` if the paired account or token is compromised.
