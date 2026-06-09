# Pi Adapter

Pi support is `native-observe`.

`onibi integration install pi` writes an observe-only extension to `~/.pi/agent/extensions/onibi.ts`, `PI_CODING_AGENT_DIR/extensions/onibi.ts`, or `ONIBI_PI_EXTENSION`.

The extension emits provider events to `/v1/adapters/pi/event`. It does not claim blocking approval coverage.
