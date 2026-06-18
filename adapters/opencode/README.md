# OpenCode adapter

Installed by:

```sh
onibi install-hooks --agent opencode
```

Writes `~/.config/opencode/plugins/onibi.js` or `$ONIBI_OPENCODE_PLUGIN`.

Install scope:

- Default: global plugin at `~/.config/opencode/plugins/onibi.js`.
- Project-local: run with `ONIBI_OPENCODE_SCOPE=project` to write `.opencode/plugins/onibi.js`.
- Explicit path: set `ONIBI_OPENCODE_PLUGIN=/path/to/onibi.js`.

OpenCode loads local plugins at startup. Restart OpenCode or start a new session after install.

Events used:

- `event` — provider-event intake; session events are mapped from the event bus.
- `tool.execute.before` — blocking approval.
- `tool.execute.after` — provider-event intake.
- `session.idle` — turn-complete notification.

Session event mapping:

- `session.created`, `session.status`, `session.error` -> `agent_message`.
- `session.deleted` -> `session_exited`.
- `session.idle` -> `agent_done`.

Edited approval input is parsed as JSON and must be an object before assigning to `output.args`.
