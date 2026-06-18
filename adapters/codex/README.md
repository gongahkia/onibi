# Codex CLI adapter

Installed by:

```sh
onibi install-hooks --agent codex
```

Writes `~/.codex/hooks.json` or `$ONIBI_CODEX_HOOKS`.

Inspect with:

```sh
onibi hooks --show --agent codex
```

`hooks --show` prints the provider config path, Onibi's recorded hash/version, the latest backup path if one exists, expected commands, installed commands, drift, and Codex trust instructions.

Trust state:

- Onibi can show expected commands and drift.
- Codex owns hook trust; run Codex, open `/hooks`, review the `onibi-notify` commands, then trust them if they match.
- Do not choose Trust all unless every configured command has been inspected.

Events:

- `PreToolUse` -> blocking Telegram approval.
- `SessionStart`, `PostToolUse` -> provider-event intake.
- `Stop` -> turn-complete notification.

Limits:

- `PreToolUse` is a guardrail, not a complete enforcement boundary.
- Current Codex docs say `PreToolUse` matchers filter by tool name and support `Bash`, `apply_patch`, and MCP tool names.
- For `apply_patch`, matchers may use `apply_patch`, `Edit`, or `Write`, but hook input still reports `tool_name: "apply_patch"`.
- Onibi installs through Codex hook config (`~/.codex/hooks.json` or `$ONIBI_CODEX_HOOKS`) and uses the documented hook event names.

Source: https://developers.openai.com/codex/hooks#pretooluse
