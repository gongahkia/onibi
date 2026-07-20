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

## Evidence and limits

[Amp's plugin API](https://ampcode.com/manual/plugin-api) documents project and system plugin paths, Bun execution, concurrent thread handling, `tool.call` allow/reject/modify results, and the five supported lifecycle events. It does not expose a `session.end` event. Onibi uses `thread.id` as provider-session metadata; `agent.end` only reports turn completion.

`onibi hooks --show --agent amp` reports all five generated and observed plugin hooks, backup, source/hash drift, and reload instructions. Hermetic fixtures cover approve, deny, expiry, edit, daemon-unavailable allow, timeout allow, plugin reload, and distinct provider thread IDs. `onibi adapters --json` reports Amp's non-certified contract v1; its omitted `minimum_provider_version` means no provider floor has been established.

Amp executes plugins with Bun. Onibi uses asynchronous `Bun.spawn` for its native runtime so one approval wait does not synchronously block the plugin process; the Node fallback exists only for hermetic fixture execution. If the Onibi daemon/socket is unavailable, or its five-minute approval wait expires, the plugin returns `{ action: "allow" }`.

The authenticated deny probe enables Amp auto-permissions only in a temporary directory. Run it only with an isolated API key and reviewed account:

```sh
ONIBI_LIVE_AMP=1 ONIBI_LIVE_AMP_ALLOW_EXECUTE=1 AMP_API_KEY=... \
  go test ./internal/adapters/amp -run TestLiveAmpDeny -count=1
```

It records a redacted artifact at `ONIBI_LIVE_ARTIFACT_DIR` or the temporary artifact directory. The current plugin API does not state a compatible Amp CLI version floor. Amp remains non-certified until a reviewed authenticated live artifact and a supported version floor are recorded.
