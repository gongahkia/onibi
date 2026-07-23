# GitHub Copilot CLI Adapter

Install:

```sh
onibi agent install --agent copilot
```

Onibi writes `~/.copilot/hooks/onibi.json` by default. If `COPILOT_HOME` is set, Onibi writes `$COPILOT_HOME/hooks/onibi.json`. `ONIBI_COPILOT_HOOK=/abs/path/onibi.json` overrides both.

After install, restart Copilot CLI. Hook configuration changes are loaded when the CLI starts.

Inspect:

```sh
onibi agent inspect --agent copilot
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
- If `disableAllHooks` is true in `onibi.json`, Copilot skips the file's hooks; `onibi agent inspect --agent copilot` and `onibi system doctor` report it.

## Evidence and limits

[GitHub Copilot CLI's hooks reference](https://docs.github.com/en/copilot/reference/hooks-reference) documents `preToolUse` allow/deny/ask decisions, `modifiedArgs`, `sessionEnd`, and second-based `timeoutSec`. Onibi uses `30` seconds for lifecycle hooks and `360` seconds for approval, leaving one minute above its five-minute approval wait.

`onibi agent inspect --agent copilot` reports generated and observed hooks, backup, hash drift, and `disableAllHooks=true`; `onibi agent status --json` marks Copilot `certified: false` with no v1 contract. Hermetic fixtures cover approve, deny, expiry, edit, daemon-unavailable no-decision behavior, timeout no-decision behavior, restart instructions, and session end.

Copilot fails `preToolUse` closed when a command hook exits nonzero, but its hook timeout fails open to normal permission handling. Onibi returns exit 0 with no provider decision when its daemon/socket is unavailable. A missing or corrupt `onibi-notify` command is a Copilot hook failure and therefore fails closed; repair the hook installation.

The authenticated deny probe runs a model with `--allow-all` only in a temporary directory. Run it only with an isolated provider account and a reviewed model:

```sh
ONIBI_LIVE_COPILOT=1 ONIBI_LIVE_COPILOT_ALLOW_ALL=1 \
  ONIBI_LIVE_COPILOT_MODEL=model \
  go test ./internal/adapters/copilot -run TestLiveCopilotDeny -count=1
```

It records a redacted artifact at `ONIBI_LIVE_ARTIFACT_DIR` or the temporary artifact directory. The current hooks reference does not state a compatible Copilot CLI version floor. Copilot remains non-certified until a reviewed authenticated live artifact and a supported version floor are recorded.
