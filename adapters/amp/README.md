# Amp adapter

Installed by:

```sh
onibi install-hooks --agent amp
```

Writes `~/.config/amp/plugins/onibi.ts` or `$ONIBI_AMP_PLUGIN`.

Inspect with:

```sh
onibi hooks --show --agent amp
```

Events used:

- `session.start` -> `agent_message`.
- `agent.start` -> `agent_message`.
- `tool.call` -> blocking `approval_request`.
- `tool.result` -> `agent_message`.
- `agent.end` -> `agent_done`.

After changing the plugin, open the Amp command palette with `Ctrl+O`, run `plugins: reload`, then run `plugins: list` to verify Onibi is loaded.

`tool.call` returns Amp action objects:

- approve -> `{ action: "allow" }`
- deny/expired -> `{ action: "reject-and-continue", message }`
- edited input -> `{ action: "modify", input }`

Edited input must parse as a JSON object before Onibi returns `modify`.
