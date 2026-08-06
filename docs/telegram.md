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
- `/keys`: inline arrows, Tab, Shift-Tab, Backspace, Delete, Home, End, PgUp, PgDn, Esc, Ctrl-C, Ctrl-D, Ctrl-Z, Ctrl-L, Ctrl-R, Enter, screen, and tmux viewport controls.
- `/key <name>`: send one named key. It accepts the keys above, `ctrl-a` through `ctrl-z`, `meta-a` through `meta-z`, and `f1` through `f12`.
- `/size small|medium|large`: resize the selected tmux window to 80×24, 100×30, or 120×40. Codex App Server sessions do not have a tmux viewport.
- `/esc`, `/enter`, `/interrupt`, `/kill`: fast controls. `/kill` requires a second command within two seconds.

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

## Reliability and files

Onibi writes the Telegram update claim and next polling offset before it performs an input side effect. On restart, a claimed but incomplete update is reported as uncertain and is not replayed. It persists outbound screen, final-tail, and session-ended intents, then retries delivery with exponential backoff. The queue contains intent metadata only; it captures terminal output and PNGs when delivering, not when enqueueing.

The daemon checks every managed tmux target every five seconds by default. If a target exits locally, Onibi marks it ended, clears it as the selected target when the notice is delivered, and rejects its input, screen, and controls. Configure `daemon.liveness_interval` from 1s to 5m.

Telegram documents are accepted only for the selected live session. Onibi verifies the advertised and downloaded size, writes the bytes as a mode-0600 file under `state/uploads/<session-id>/`, and reports that path. It does not insert, paste, or execute the file. The default maximum is 20 MiB and expiry is seven days; configure `daemon.upload_max_bytes` (1–100 MiB) and `daemon.upload_ttl` (1h–30d).

## Security

Telegram bots do not use end-to-end encryption. Do not send credentials, recovery phrases, or sensitive production output through the bot. Revoke access with `onibi telegram disable` and rotate the BotFather token if the paired account or token is compromised.
