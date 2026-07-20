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

## Evidence and limits

[OpenCode's plugin documentation](https://opencode.ai/docs/plugins/) specifies global and project local-plugin paths, startup loading, and mutable `tool.execute.before` output. It does not specify a compatible-version floor for this adapter or certify subagent interception.

`onibi hooks --show --agent opencode` reports the four generated plugin hooks, recorded hash, backup, and source drift. Hermetic fixtures cover approve, deny, expiry, edit, timeout, daemon-unavailable, session exit, and turn completion.

The authenticated live probe is opt-in and runs a model with OpenCode `--auto` inside a temporary directory. Run it only with an isolated provider account and a reviewed model:

```sh
ONIBI_LIVE_OPENCODE=1 ONIBI_LIVE_OPENCODE_ALLOW_AUTO=1 \
  ONIBI_LIVE_OPENCODE_MODEL=provider/model \
  go test ./internal/adapters/opencode -run TestLiveOpenCodeApprove -count=1
```

It records a redacted artifact at `ONIBI_LIVE_ARTIFACT_DIR` or the temporary artifact directory. Until a reviewed live artifact and a supported OpenCode version floor are recorded, OpenCode remains non-certified.
