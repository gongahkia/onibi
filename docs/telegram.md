# Telegram operation

Onibi long-polls one Telegram bot and accepts one paired private chat. Configure the token locally, start the daemon, then send the printed `/start <pair-code>` from the account that owns the bot.

## Session control

- `/new shell|codex|pi [--name name] [--cwd path]`: create a session. The working directory must exist locally; names use letters, digits, `.`, `_`, and `-`.
- `/sessions`: select an active session.
- Plain text: literal input plus Enter.
- `/paste`: next text is literal input with no Enter; the armed state expires after five minutes.
- `/tail [1..400]`, `/screen`: inspect output.
- `/keys`, `/esc`, `/enter`, `/interrupt`, `/kill`: terminal controls. `/kill` requires a second command within two seconds.

Screens are rendered from tmux capture, so Onibi does not require macOS screen-recording permission. It sends a screen when generic work completes or fails, an approval becomes actionable, a Codex turn completes/fails, and on `/screen`.

## Decision cards

Telegram inline keyboards, not polls, carry decisions. Callback data is a short opaque token whose complete state lives locally with a 24-hour expiry and is consumed after one use. Onibi records an audit event, applies the decision atomically, resumes/denies the agent, and edits the card to its terminal state.

Codex uses the local [App Server](https://learn.chatgpt.com/docs/app-server) for structured approvals and questions. Its command, file, permission, and input requests map to inline buttons; free-form choices wait for the next message. Onibi never exposes the App Server shell-command endpoint.

Pi is experimental until its live event payload contract is validated. Its extension is intentionally limited to tool-approval events.
If Onibi restarts while Pi is waiting, the tool call is cancelled rather than replayed against a stale process.

## Security

Telegram bots do not use end-to-end encryption. Do not send credentials, recovery phrases, or sensitive production output through the bot. Revoke access with `onibi telegram disable` and rotate the BotFather token if the paired account or token is compromised.
