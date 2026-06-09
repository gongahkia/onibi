# Cursor Adapter

Cursor support is `native-observe`.

`onibi integration install cursor` writes observe-only hook handlers to `~/.cursor/hooks.json` or `ONIBI_CURSOR_HOOKS`.

The hook command is `onibi _hook cursor` and emits provider events to `/v1/adapters/cursor/event`. It does not claim blocking approval coverage.
