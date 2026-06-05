# Adapters

Adapters let Onibi sit between a local agent and a tool action. Claude Code remains the full approval/edit path, Codex covers Bash approval interception, and newer provider-event adapters feed native lifecycle/session/tool events into Onibi's common arbitration model.

## Capability Matrix

| Agent | Minimum version | Approval intercept | Native events | Native resume | Terminal mirror | Hook surface |
| --- | --- | ---: | ---: | ---: | ---: | --- |
| Claude Code | 2.0.10 | Yes | Yes | Yes | Yes | HTTP `PreToolUse` |
| Codex CLI | Launch-tested current | Bash only | Bash only | Unverified | Yes | `~/.codex/hooks.json` shell hook |
| OpenCode | No minimum | No | Yes | Yes | Yes | `~/.config/opencode/plugins/onibi-provider-events.js` |
| Qoder CLI | No minimum | No | Yes | Yes | Yes | `~/.qoder/settings.json` command hooks |
| GitHub Copilot CLI | No minimum | No | Yes | No | Yes | `~/.copilot/hooks/onibi-provider-events.json` |
| Goose | No minimum | No | Yes | Yes | Yes | `~/.agents/plugins/onibi/hooks/hooks.json` |
| Gemini CLI | No minimum | No | No | Yes | Yes | Resume-only metadata |
| Hermes | No minimum | No | No | Yes | Yes | Resume-only metadata |
| Aider | No minimum | No | No | History restore | Yes | Shell session only |
| Cursor agent | No minimum | No | Pending | Pending | Yes | Native hook pending |
| Pi | No minimum | No | Pending | Pending | Yes | Native extension pending |
| OMP | No minimum | No | Pending | Pending | Yes | Native extension pending |

Legend: "terminal mirror" means Onibi can host the agent in a PTY session and stream output to the desktop and mobile surfaces. "Native events" means the provider emits session/tool lifecycle payloads into `/v1/adapters/:agent/event`. "Approval intercept" means the agent blocks on Onibi before executing a tool call.

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
- Provider session IDs can relaunch stale sessions with `opencode --session <id>`.

Known limitations:

- No tool-call blocking in v1.5.
- No edit-before-approve support.

Sample interaction:

```text
$ opencode
Onibi hosts the process, mirrors output, and records run events where available.
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
- Provider session IDs can relaunch stale sessions through the Goose session resume command.

Known limitations:

- No tool-call blocking.
- No edit-before-approve.

Sample interaction:

```text
$ goose session
Onibi mirrors output and tracks the run as a Goose session.
```

## Qoder CLI

Install:

```sh
onibi adapter install qoder
```

Hook surface: command hooks in `~/.qoder/settings.json`, invoking `onibi _hook qoder`.

Supported features:

- Session, prompt, tool, failure, and stop events feed Onibi's provider-event bridge.
- Qoder session IDs can relaunch stale sessions with `qoder -r <id>`.
- Terminal mirror and run visibility.

Known limitations:

- The current Onibi Qoder hook records lifecycle/status events; it does not yet block tool execution for approval.
- Edit-before-approve is not wired.

## GitHub Copilot CLI

Install:

```sh
onibi adapter install copilot
```

Hook surface: hook configuration file at `~/.copilot/hooks/onibi-provider-events.json`, invoking `onibi _hook copilot`.

Supported features:

- Session, prompt, tool, stop, and error events feed Onibi's provider-event bridge.
- Native `sessionId` values are preserved for arbitration and audit run events.

Known limitations:

- No provider-native resume command is wired.
- Approval interception is not enabled by the Onibi Copilot hook.

## Hermes

Hermes is registered as resume-only. When Onibi has a Hermes provider session ID, stale session attach can prefer `hermes --resume <id>` over plain relaunch. Native plugin hook installation remains pending.

## Pi / OMP

Pi and OMP are registered as pending native integrations. Onibi keeps heuristic detection and terminal mirroring, but does not install extension files until stable public hook/plugin APIs are verified.

## Support

Run:

```sh
onibi doctor
```

The doctor command checks whether each adapter binary is on `PATH`, reports detected versions, and prints remediation hints suitable for GitHub issues.
