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

## Evidence and limits

[Gemini CLI's hooks reference](https://geminicli.com/docs/hooks/reference/) documents `BeforeTool` deny and input replacement, `SessionEnd`, and millisecond hook timeouts. Onibi leaves a one-minute margin between its five-minute approval wait and Gemini's six-minute approval-hook timeout.

`onibi hooks --show --agent gemini` reports generated and observed hooks, backup, hash drift, and `hooksConfig.enabled=false`. Hermetic fixtures cover denial, expiry/edit response mapping, lifecycle, timeout units, unavailable-notifier warning output, and session end.

The authenticated deny probe creates a project-local hook in a temporary directory and blocks every tool before execution. Run it only with an isolated provider account and a reviewed model:

```sh
ONIBI_LIVE_GEMINI=1 ONIBI_LIVE_GEMINI_MODEL=model \
  go test ./internal/adapters/gemini -run TestLiveGeminiDeny -count=1
```

It records a redacted artifact at `ONIBI_LIVE_ARTIFACT_DIR` or the temporary artifact directory.

The current docs do not state a compatible Gemini CLI version floor. Gemini remains non-certified until a reviewed authenticated live artifact and a supported version floor are recorded.

Deliberate scope:

- `AfterTool` is observe-only in Onibi; it does not redact or replace tool output.
- `PreCompress`, `BeforeModel`, and `AfterModel` are not installed until Onibi has a concrete product flow for model-context hooks.
