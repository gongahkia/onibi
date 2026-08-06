# Telegram operation

Onibi long-polls one Telegram bot and accepts one paired private chat. Configure the token locally, start the daemon, then send the printed `/start <pair-code>` from the account that owns the bot.

## Session control

- `/new shell|codex|pi|claude [--name name] [--cwd path]`: create a session. The working directory must exist locally; names use letters, digits, `.`, `_`, and `-`.
- `/sessions`: select an active session.
- Plain text: literal input plus Enter.
- `/paste`: next text is literal input with no Enter; the armed state expires after five minutes.
- Unknown `/commands` go to the selected session. Prefix a conflicting Onibi command with `//`, such as `//help`.
- `/tail [1..400]`, `/screen`: inspect output.
- `/font`: choose the terminal screenshot font.
- `/keys`, `/esc`, `/enter`, `/interrupt`, `/kill`: terminal controls. `/kill` requires a second command within two seconds.

Screens use tmux's resolved screen capture, so Onibi does not require macOS screen-recording permission or brittle ANSI replay. It sends a screen after generic input updates, an approval becomes actionable, a Pi turn ends, a Codex turn completes/fails, and on `/screen`.

`/font` persists the selection and applies to the next screen without a restart. The bundled choices are JetBrainsMono Nerd Font Mono, Caskaydia Cove Nerd Font Mono, and Go Mono Nerd Font Mono. BigBlueTerminal Nerd Font Mono is external-only: configure `screen.font_path` to its local TTF/OTF path, then set `screen.font=custom`; it appears as `External font` in `/font`.

## Decision cards

Telegram inline keyboards, not polls, carry decisions. Callback data is a short opaque token whose complete state lives locally with a 24-hour expiry and is consumed after one use. Onibi records an audit event, applies the decision atomically, resumes/denies the agent, and edits the card to its terminal state.

Codex uses the local [App Server](https://learn.chatgpt.com/docs/app-server) for structured approvals and questions. Its command, file, permission, and input requests map to inline buttons; free-form choices wait for the next message. Onibi never exposes the App Server shell-command endpoint.

Pi uses documented `agent_start` and `agent_end` events for one working card and a final fresh tail/screen. Its extension remains intentionally limited to tool-approval events for decision handling.
If Onibi restarts while Pi is waiting, the tool call is cancelled rather than replayed against a stale process.

Claude Code runs in tmux. Onibi launches it with an owned settings file containing `PermissionRequest`, `Stop`, and `StopFailure` hooks. Permission requests become the same Telegram approval cards; successful or failed turns edit the working card and send a fresh tail/screen. Claude `--bare` and `--settings` arguments are rejected because they would bypass those hooks.

## Codex sessions

Codex App Server sessions do not have a tmux screen. `/new codex` creates a ready card; send normal text to start a turn. While a turn is active, normal text is sent with App Server `turn/steer`. The status card receives coalesced progress and final output; structured approvals and questions remain inline Telegram cards.

## Security

Telegram bots do not use end-to-end encryption. Do not send credentials, recovery phrases, or sensitive production output through the bot. Revoke access with `onibi telegram disable` and rotate the BotFather token if the paired account or token is compromised.
