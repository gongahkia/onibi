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

Current Codex docs support matching `Bash`, `apply_patch`, and MCP tool names.
