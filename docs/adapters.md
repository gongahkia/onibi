# Adapters

Adapters let Onibi sit between a local agent and a tool action. The launch target is intentionally uneven: Claude Code is the full demonstration path, Codex covers Bash approval interception, and the other agents are documented honestly as mirror or shell-session integrations until their hook surfaces are stable.

## Capability Matrix

| Agent | Minimum version | Approval intercept | Edit before approve | Terminal mirror | Hook surface |
| --- | --- | ---: | ---: | ---: | --- |
| Claude Code | 2.0.10 | Yes | Yes | Yes | HTTP `PreToolUse` |
| Codex CLI | Launch-tested current | Bash only | No | Yes | `~/.codex/hooks.json` shell hook |
| OpenCode | No minimum | No | No | Yes | Phase-03 spike found no launch-stable blocking hook |
| Gemini CLI | No minimum | No | No | Yes | Shell session only |
| Aider | No minimum | No | No | Yes | Shell session only |
| Cursor agent | No minimum | No | No | Yes | Shell session only |
| Goose | No minimum | No | No | Yes | Shell session only |

Legend: "terminal mirror" means Onibi can host the agent in a PTY session and stream output to the desktop and mobile surfaces. "Approval intercept" means the agent blocks on Onibi before executing a tool call.

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

Hook surface: no launch-stable blocking hook from the Phase-03 spike.

Supported features:

- Agent appears as a first-class session kind.
- PTY output can be mirrored to the phone.

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

Hook surface: shell session only for v1.5.

Supported features:

- Launch from Onibi as a PTY session.
- Terminal mirror and run visibility.

Known limitations:

- No tool-call blocking.
- No edit-before-approve.

Sample interaction:

```text
$ goose session
Onibi mirrors output and tracks the run as a Goose session.
```

## Support

Run:

```sh
onibi doctor
```

The doctor command checks whether each adapter binary is on `PATH`, reports detected versions, and prints remediation hints suitable for GitHub issues.
