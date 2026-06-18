# Goose adapter

Installed by:

```sh
onibi install-hooks --agent goose
```

Writes `~/.agents/plugins/onibi/hooks/hooks.json` or `$ONIBI_GOOSE_HOOKS`.

Inspect with:

```sh
onibi hooks show --agent goose
```

Events used:

- `SessionStart` -> `agent_message`.
- `SessionEnd` -> `session_exited`.
- `UserPromptSubmit` -> `agent_message`.
- `PreToolUse` -> `agent_message`.
- `PostToolUse` -> `agent_message`.
- `PostToolUseFailure` -> `agent_message`.
- `BeforeReadFile` -> `agent_message`.
- `AfterFileEdit` -> `agent_message`.
- `BeforeShellExecution` -> `agent_message`.
- `AfterShellExecution` -> `agent_message`.
- `Stop` -> `agent_done`.

Support tier: event bridge. Goose lifecycle hooks emit events and turn-complete
signals to Onibi; blocking approval is not claimed.

Goose follows the Open Plugins hook model. Hook failures and timeouts are host
logged and should not crash Goose, so Onibi keeps this adapter observe-only.
