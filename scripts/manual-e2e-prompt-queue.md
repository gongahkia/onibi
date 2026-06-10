# Manual end-to-end test: prompt queue + session controls

Requires a paired Telegram bot and a working agent CLI.

## Setup

```sh
make install
onibi doctor --mode installed
onibi run claude
```

In Telegram:

1. Send `/sessions`; note the session id.
2. Send `/prompt say one short word`.
3. Send `/prompt say a second short word`.
4. Send `/queue`.

Expected:

- first prompt sends immediately if the session is ready.
- second prompt remains queued while the agent is busy.
- `/queue` lists queued prompt ids and positions.

## Edit/cancel/reorder

1. Send `/editprompt <id> say edited`.
2. Send `/moveprompt <id> 1`.
3. Send `/cancelprompt <id>` for a different queued prompt.
4. Send `/queue`.

Expected:

- edited text is what reaches the PTY when dispatched.
- cancelled prompt never reaches the PTY.
- queue order changes.

## Buttons

1. Queue a prompt.
2. Tap `Edit`, reply with replacement text.
3. Queue another prompt.
4. Tap `Up`, `Down`, `Cancel`, and `Send now`.

Expected:

- button actions match slash-command behavior.
- no action is accepted from a non-owner Telegram account.

## Session controls

1. Send `/peek <id>`.
2. Send `/interrupt <id>`.
3. Send `/rename <id> queue-test`.
4. Send `/menu` and use `Peek`, `Interrupt`.
5. Send `/kill <id>`.

Expected:

- `/peek` sends text or screenshot preview.
- `/interrupt` sends Ctrl-C.
- `/rename` updates `/sessions`.
- `/kill` ends the session and fails queued prompts for that session.
