# Codex CLI adapter

Installed by:

```sh
onibi install-hooks --agent codex
```

Writes `~/.codex/hooks.json` or `$ONIBI_CODEX_HOOKS`.

Events:

- `PreToolUse` -> blocking Telegram approval.
- `SessionStart`, `PostToolUse` -> provider-event intake.
- `Stop` -> turn-complete notification.

Limits:

- `PreToolUse` is a guardrail, not a complete enforcement boundary.
- Current Codex docs say it can intercept `Bash`, `apply_patch` file edits, and MCP tool calls.
- It does not intercept every shell path, `WebSearch`, or other non-shell/non-MCP tool calls.
- For `apply_patch`, matchers may use `apply_patch`, `Edit`, or `Write`, but hook input still reports `tool_name: "apply_patch"`.
- Onibi installs through Codex hook config (`~/.codex/hooks.json` or `$ONIBI_CODEX_HOOKS`) and uses the documented hook event names.

Source: https://developers.openai.com/codex/hooks#pretooluse
