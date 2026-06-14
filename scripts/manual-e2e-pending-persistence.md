# Manual E2E: Pending UI Persistence

Goal: verify owner-side parked UI state survives daemon restart.

## Approval edit

1. Start a daemon session:

   ```sh
   onibi run claude
   ```

2. Trigger any approval that reaches the `PreToolUse` hook.
3. In Telegram, tap `Edit`.
4. Confirm the bot asks for edited JSON.
5. Stop the daemon with `SIGTERM`.
6. Restart it:

   ```sh
   onibi run claude
   ```

7. Reply in Telegram with valid edited JSON.
8. Expect: the approval becomes `edited`; the agent receives the edited input.

## Prompt edit

1. Queue a prompt while a session is busy.
2. Tap the prompt `Edit` button.
3. Stop and restart the daemon.
4. Reply with replacement prompt text.
5. Expect: the queued prompt text is replaced.

## Ambiguous target inject

1. Start at least two live sessions.
2. Send plain text from Telegram so Onibi asks for a target.
3. Stop and restart the daemon.
4. Tap the target picker.
5. Expect: the original text is sent to the selected session.

## Expiry

Parked state has a 10-minute TTL. After that window, repeat the original action instead of replying to the old prompt.
