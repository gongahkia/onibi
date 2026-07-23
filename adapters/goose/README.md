# Goose adapter

Installed by:

```sh
onibi agent install --agent goose
```

Writes `~/.agents/plugins/onibi/hooks/hooks.json` or `$ONIBI_GOOSE_HOOKS`.

Inspect with:

```sh
onibi agent inspect --agent goose
```

Events used:

- `SessionStart` -> `agent_message`.
- `SessionEnd` -> `session_exited`.
- `UserPromptSubmit` -> `agent_message`.
- `PreToolUse` -> native blocking `approval_request`.
- `PostToolUse` -> `agent_message`.
- `PostToolUseFailure` -> `agent_message`.
- `BeforeReadFile` -> `agent_message`.
- `AfterFileEdit` -> `agent_message`.
- `BeforeShellExecution` -> `agent_message`.
- `AfterShellExecution` -> `agent_message`.
- `Stop` -> `agent_done`.

Support tier: native blocking, non-certified. Goose `PreToolUse` can block a
tool with exit code `2`; owner approval and denial use that documented path.
Goose has no documented modified-input response, so an owner edit blocks the
original tool instead of changing its input. This adapter does not satisfy the
[certified adapter contract](../../docs/adapter-contract.md).

Goose follows the [Open Plugins hook model](https://goose-docs.ai/docs/guides/context-engineering/hooks/). Hook timeouts and failures fail open; only an explicit Onibi deny, expiry, or unsupported edit emits the documented blocking exit code.
