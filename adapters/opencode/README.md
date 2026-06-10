# OpenCode adapter

Installed by:

```sh
onibi install-hooks --agent opencode
```

Writes `~/.config/opencode/plugins/onibi.js` or `$ONIBI_OPENCODE_PLUGIN`.

Events used:

- `tool.execute.before` — blocking approval.
- `tool.execute.after` — provider-event intake.
- `session.idle` — turn-complete notification.
