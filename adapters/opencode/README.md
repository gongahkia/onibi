# OpenCode plugin templates

Templates for the OpenCode TS plugin API, installed by
`onibi install-hooks --agent opencode` (phase 8).

Events used:

- `tool.execute.before` — blocking approval.
- `permission.replied` — captures user's decision to mirror into our audit log.
- `session.idle` — turn-complete signal.
