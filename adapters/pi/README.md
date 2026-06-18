# Pi Adapter

Install:

```sh
onibi install-hooks --agent pi
```

Onibi writes a TypeScript extension to `~/.pi/agent/extensions/onibi.ts` by default.

Path controls:

- `ONIBI_PI_SCOPE=project`: write `.pi/extensions/onibi.ts`
- `ONIBI_PI_EXTENSION=/abs/path/onibi.ts`: write an explicit path
- `PI_CODING_AGENT_DIR=/abs/pi/root`: write `$PI_CODING_AGENT_DIR/extensions/onibi.ts`

After install, run `/reload` in Pi so auto-discovered extensions are reloaded.

Inspect:

```sh
onibi hooks show --agent pi
```

Events:

- `session_start`, `agent_start`, `input`, `tool_call`, `tool_result` -> Onibi messages
- `agent_end` -> agent done
- `session_shutdown` -> session exited

Approval behavior:

- deny/expired returns `{ block: true, reason }`
- edited input is parsed as a JSON object before mutating `event.input`
- Pi does no post-mutation validation, so Onibi validates shape before mutation
