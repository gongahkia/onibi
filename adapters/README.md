# Onibi Adapter Support

Phase-03 freezes the local approval protocol and ships adapter templates for the agents Onibi can launch.

| Agent | Support | Notes |
| --- | --- | --- |
| Claude Code | Full | HTTP `PreToolUse` hook blocks for Onibi approval and supports edited tool input. Requires Claude Code v2.0.10+. |
| Codex | Bash-only | Command hook posts Bash tool calls to Onibi. `apply_patch` and non-Bash tools are not intercepted in Phase-03. |
| OpenCode | Plugin | Installs the bundled Onibi plugin and emits provider events. |
| Gemini | Resume-only | No approval hook installed; native resume metadata is used when available. |
| Aider | History-restore | No approval hook installed. |
| Cursor | Native-observe | Installs observe-only hooks for stable Cursor events. Blocking approval is not claimed. |
| Goose | Event bridge | Installs lifecycle/provider-event hooks. |
| Qoder | Event bridge | Installs lifecycle/provider-event hooks. |
| GitHub Copilot CLI | Event bridge | Installs lifecycle/provider-event hooks. |
| Pi | Native-observe | Installs an observe-only extension. Blocking approval is not claimed. |
| OMP | Native-observe | Installs an observe-only extension. Blocking approval is not claimed. |
