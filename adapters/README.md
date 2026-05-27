# Onibi Adapter Support

Phase-03 freezes the local approval protocol and ships adapter templates for the agents Onibi can launch.

| Agent | Support | Notes |
| --- | --- | --- |
| Claude Code | Full | HTTP `PreToolUse` hook blocks for Onibi approval and supports edited tool input. Requires Claude Code v2.0.10+. |
| Codex | Bash-only | Command hook posts Bash tool calls to Onibi. `apply_patch` and non-Bash tools are not intercepted in Phase-03. |
| OpenCode | Stub | Registered for future run-event mirroring once a stable hook surface is verified. |
| Gemini | Stub | No approval hook installed in Phase-03. |
| Aider | Stub | No approval hook installed in Phase-03. |
| Cursor | Stub | No approval hook installed in Phase-03. |
| Goose | Stub | No approval hook installed in Phase-03. |
