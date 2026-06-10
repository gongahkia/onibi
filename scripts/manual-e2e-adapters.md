# Manual end-to-end test: non-Claude adapters

Provider hook APIs can drift. Run this before claiming first-class support for
a provider.

## Matrix

Run for each installed provider:

- `codex`
- `opencode`
- `goose`
- `gemini`
- `copilot`
- `pi`
- `amp`

## Procedure

```sh
make install
onibi adapters
onibi install-hooks --agent <provider>
onibi doctor --mode installed
onibi run <provider>
```

From the provider session:

1. Start a short task that prints output.
2. Trigger a safe tool/command if the provider supports blocking hooks.
3. Approve from Telegram.
4. Send a Telegram prompt reply/free-text prompt.
5. Exit the provider.

Expected:

- `onibi adapters` shows detected + installed status.
- hook hash passes in `onibi doctor`.
- turn-complete notification arrives.
- Telegram prompt reaches the provider PTY.
- blocking approval works where the provider supports it.
- unsupported provider hook events fail open and do not break the provider.
