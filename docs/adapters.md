# Adapters

Adapters let Onibi sit between a local agent and a tool action. Claude Code remains the full approval/edit path, Codex covers Bash approval interception, ACP adapters map protocol permission requests into Onibi approvals, and provider-event adapters feed native lifecycle/session/tool events into Onibi's common arbitration model. OpenCode, Qoder, Copilot CLI, and Goose also block native pre-tool hooks through Onibi where the provider exposes a synchronous hook.

## Capability Matrix

| Agent | Minimum version | Approval intercept | Native events | Native resume | Terminal mirror | Hook surface |
| --- | --- | ---: | ---: | ---: | ---: | --- |
| Claude Code | 2.0.10 | Yes | Yes | Yes | Yes | HTTP `PreToolUse` or ACP |
| Codex CLI | Launch-tested current | Bash only | Bash only | Unverified | Yes | `~/.codex/hooks.json` shell hook |
| OpenCode | No minimum | Yes | Yes | Yes | Yes | `~/.config/opencode/plugins/onibi-provider-events.js` |
| Qoder CLI | No minimum | Yes | Yes | Yes | Yes | `~/.qoder/settings.json` command hooks |
| GitHub Copilot CLI | No minimum | Yes | Yes | No | Yes | `~/.copilot/hooks/onibi-provider-events.json` |
| Goose | No minimum | Yes | Yes | Yes | Yes | `~/.agents/plugins/onibi/hooks/hooks.json` |
| Gemini CLI | No minimum | No | No | Yes | Yes | Resume-only metadata |
| Hermes | No minimum | Yes | Yes | Yes | Yes | ACP `hermes acp` |
| Aider | No minimum | No | No | History restore | Yes | Shell session only |
| Cursor agent | No minimum | No | Pending | Pending | Yes | Native hook pending |
| Pi | No minimum | No | Pending | Pending | Yes | Native extension pending |
| OMP | No minimum | No | Pending | Pending | Yes | Native extension pending |

Legend: "terminal mirror" means Onibi can host the agent in a PTY session and stream output to the desktop and mobile surfaces. "Native events" means the provider emits session/tool lifecycle payloads into `/v1/adapters/:agent/event` or ACP session updates. "Approval intercept" means the agent blocks on Onibi before executing a tool call.

## Claude Code

Install:

```sh
onibi adapter install claude-code
```

Hook surface: Claude Code `PreToolUse` HTTP hook.

Supported features:

- Blocks tool calls through Onibi.
- Supports allow, deny, and approve-with-edits.
- Preserves other Claude settings while adding the Onibi hook.
- Refuses old Claude Code versions that predate `updatedInput`.

Known limitations:

- The hook depends on Claude Code's tool payload shape. If Claude changes that schema, Onibi should fail closed and require an adapter update.
- The default long-poll timeout is 10 minutes. A missed approval becomes a deny.

Sample interaction:

```text
$ claude code "remove old generated fixtures"
Claude wants to run Bash(rm -rf tests/legacy)
Onibi shows the approval on desktop and phone.
User edits command to mv tests/legacy tests/legacy.bak.
Claude receives permissionDecision=allow with updatedInput.
```

## Codex CLI

Install:

```sh
onibi adapter install codex
```

Hook surface: `~/.codex/hooks.json`, invoking `onibi _hook codex`.

Supported features:

- Bash-tool approval interception.
- Deny/allow decisions.
- Terminal mirror when launched inside Onibi.

Known limitations:

- `apply_patch`, MCP tools, and non-Bash tool calls are not interceptable in the launch adapter.
- Edited input is not returned to Codex; use deny-and-rerun if a command needs changes.

Sample interaction:

```text
$ codex
Codex proposes Bash(git clean -xfd)
Onibi blocks the hook and asks for approval.
User denies with reason "too broad".
Codex receives a deny decision.
```

## OpenCode

Install:

```sh
onibi adapter install opencode
```

Hook surface: local OpenCode plugin at `~/.config/opencode/plugins/onibi-provider-events.js`.

Supported features:

- Agent appears as a first-class session kind.
- PTY output can be mirrored to the phone.
- Session/tool lifecycle events update Onibi's native provider metadata.
- `tool.execute.before` blocks on Onibi approval and denies by throwing from the plugin.
- Approve-with-edits maps Onibi's updated input onto OpenCode's mutable tool args.
- Provider session IDs can relaunch stale sessions with `opencode --session <id>`.

Known limitations:

- Blocking depends on OpenCode loading local plugins from the global plugin directory.

Sample interaction:

```text
$ opencode
OpenCode proposes bash(rm -rf build)
Onibi shows the approval on desktop and phone.
User denies; the plugin throws and the tool is skipped.
```

## Gemini CLI

Install:

```sh
onibi adapter install gemini
```

Hook surface: shell session only for v1.5.

Supported features:

- Launch from Onibi as a PTY session.
- Terminal mirror and run visibility.
- Provider-native resume metadata can use Gemini checkpoint IDs when events or user metadata provide one.

Known limitations:

- No approval interception.
- No edit-before-approve.

Sample interaction:

```text
$ gemini
Onibi mirrors the terminal output to the mobile PWA.
```

## Aider

Install:

```sh
onibi adapter install aider
```

Hook surface: shell session only for v1.5.

Supported features:

- Launch from Onibi as a PTY session.
- Terminal mirror and run visibility.

Known limitations:

- Native Aider tool hooks are not wired in v1.5.
- File edits happen through Aider's normal workflow; Onibi does not intercept them.
- Restore is chat-history based, not provider session ID based.

Sample interaction:

```text
$ aider
Onibi shows the session and mirrors output. Approval gating is not active.
```

## Cursor Agent

Install:

```sh
onibi adapter install cursor
```

Hook surface: shell session only for v1.5.

Supported features:

- Launches a configured `cursor-agent` command when present.
- Terminal mirror and run visibility.

Known limitations:

- Cursor's stable blocking hook surface was not part of the launch adapter.
- Users may need to configure the command path manually.

Sample interaction:

```text
$ cursor-agent
Onibi hosts the session; approval gating is not active.
```

## Goose

Install:

```sh
onibi adapter install goose
```

Hook surface: Open Plugins lifecycle hook config at `~/.agents/plugins/onibi/hooks/hooks.json`.

Supported features:

- Launch from Onibi as a PTY session.
- Terminal mirror and run visibility.
- Session/tool lifecycle events update Onibi's native provider metadata.
- `PreToolUse` blocks on Onibi approval.
- Provider session IDs can relaunch stale sessions through the Goose session resume command.

Known limitations:

- Blocking depends on Goose's Open Plugins hook loader and the local plugin directory.
- Edit-before-approve is returned only through Open Plugins-compatible `updatedInput`.

Sample interaction:

```text
$ goose session
Goose proposes a tool call, Onibi blocks the hook, and a deny exits the hook with code 2.
```

## Qoder CLI

Install:

```sh
onibi adapter install qoder
```

Hook surface: command hooks in `~/.qoder/settings.json`, invoking `onibi _hook qoder`.

Supported features:

- Session, prompt, tool, failure, and stop events feed Onibi's provider-event bridge.
- `PreToolUse` blocks on Onibi approval; a denied approval exits with code 2 so Qoder skips the tool.
- Approve-with-edits returns `updatedInput` through Qoder's `hookSpecificOutput`.
- Qoder session IDs can relaunch stale sessions with `qoder -r <id>`.
- Terminal mirror and run visibility.

Known limitations:

- Blocking depends on Qoder's local hook loader and the installed `~/.qoder/settings.json` entry.

## GitHub Copilot CLI

Install:

```sh
onibi adapter install copilot
```

Hook surface: hook configuration file at `~/.copilot/hooks/onibi-provider-events.json`, invoking `onibi _hook copilot`.

Supported features:

- Session, prompt, tool, stop, and error events feed Onibi's provider-event bridge.
- `PreToolUse` blocks on Onibi approval through Copilot's VS Code-compatible command hook JSON response.
- Approve-with-edits maps Onibi's updated input to Copilot `modifiedArgs`.
- Native `sessionId` values are preserved for arbitration and audit run events.

Known limitations:

- No provider-native resume command is wired.
- This adapter targets local Copilot CLI hooks. Copilot cloud agent is remote and cannot call a local Onibi daemon unless separately networked.

## Hermes

Hermes uses ACP through `hermes acp`. Onibi runs the shared stdio ACP runtime, maps ACP permission requests into the approval flow, records provider session metadata, and keeps `hermes --resume <id>` metadata when a Hermes provider session ID is available. No Python plugin hook is installed.

## Pi / OMP

Pi and OMP are registered as pending native integrations. Onibi keeps heuristic detection and terminal mirroring, but does not install extension files until stable public hook/plugin APIs are verified.

## Support

Run:

```sh
onibi doctor
```

The doctor command checks whether each adapter binary is on `PATH`, reports detected versions, and prints remediation hints suitable for GitHub issues.
