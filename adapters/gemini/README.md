# Gemini CLI adapter

Installed by:

```sh
onibi install-hooks --agent gemini
```

Writes `~/.gemini/settings.json` or `$ONIBI_GEMINI_SETTINGS`.

Events used:

- `SessionStart` -> `agent_message`.
- `BeforeAgent` -> `agent_message`.
- `BeforeTool` -> blocking `approval_request`.
- `AfterTool` -> `agent_message`.
- `Notification` -> `agent_message`.
- `AfterAgent` -> `agent_done`.
- `SessionEnd` -> `session_exited`.

Hook details:

- Gemini `timeout` values are milliseconds; Onibi uses `30000` for normal event hooks and `360000` for blocking approval.
- Managed version metadata lives in the command environment, not provider JSON fields.
- `BeforeTool` denials return `decision: "deny"` with a reason.
- `BeforeTool` edits return `hookSpecificOutput.tool_input` so Gemini merges the approved argument changes before execution.

Deliberate scope:

- `AfterTool` is observe-only in Onibi; it does not redact or replace tool output.
- `PreCompress`, `BeforeModel`, and `AfterModel` are not installed until Onibi has a concrete product flow for model-context hooks.
