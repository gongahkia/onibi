# Cursor Adapter

Cursor support is `event-bridge`.

`onibi integration install cursor` writes blocking pre-execution hook handlers to `~/.cursor/hooks.json` or `ONIBI_CURSOR_HOOKS`.

The hook command is `onibi _hook cursor`, emits provider events to `/v1/adapters/cursor/event`, and blocks shell/MCP/read-file pre-events through Onibi. Cursor edited-input forwarding is not enabled because a stable edit contract is not verified.
