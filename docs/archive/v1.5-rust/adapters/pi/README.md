# Pi Adapter

Pi support is `native-blocking`.

`onibi integration install pi` writes a native-blocking extension to `~/.pi/agent/extensions/onibi.ts`, `PI_CODING_AGENT_DIR/extensions/onibi.ts`, or `ONIBI_PI_EXTENSION`.

The extension emits provider events to `/v1/adapters/pi/event` and blocks `tool_call` execution through Onibi approval.
