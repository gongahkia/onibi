# GitHub Copilot CLI Adapter

Install:

```sh
onibi install-hooks --agent copilot
```

Onibi writes `~/.copilot/hooks/onibi.json` by default. If `COPILOT_HOME` is set, Onibi writes `$COPILOT_HOME/hooks/onibi.json`. `ONIBI_COPILOT_HOOK=/abs/path/onibi.json` overrides both.

After install, restart Copilot CLI. Hook configuration changes are loaded when the CLI starts.

Inspect:

```sh
onibi hooks --show --agent copilot
```

Events:

- `sessionStart`, `userPromptSubmitted`, `postToolUse`, `postToolUseFailure`, `notification`, `errorOccurred` -> Onibi messages
- `preToolUse` -> blocking approval request
- `agentStop` -> agent done
- `sessionEnd` -> session exited

Approval behavior:

- deny/expired returns `permissionDecision: "deny"`
- edited input returns `permissionDecision: "allow"` with `modifiedArgs`

Notes:

- Onibi does not write legacy `onibiManaged` or `onibiIntegrationVersion` fields into Copilot JSON.
- If `disableAllHooks` is true in `onibi.json`, Copilot skips the file's hooks; `onibi hooks --show --agent copilot` and `onibi doctor` report it.
