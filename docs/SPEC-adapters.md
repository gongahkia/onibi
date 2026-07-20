# Onibi Adapter Manifest Spec

Version: 1

This document defines the third-party adapter manifest consumed by `onibi adapters add` and `onibi adapters validate`.

## Manifest File

Adapter manifests are TOML files. File names are advisory; the adapter identity is `name`.

Required top-level keys:

```toml
name = "example"
version = "1.0.0"
kind = "agent"
cmd_pattern = { "PreToolUse" = "Edit" }
hook_install = ["mkdir -p ~/.example", "example hooks install"]
hook_verify = ["example hooks verify"]
hook_adopt = ["example hooks adopt"]
hook_uninstall = ["example hooks uninstall"]
min_onibi_version = "0.3.0"

[risk_overrides]
Edit = "medium"
Write = "high"
```

## Keys

`name`
: Stable adapter ID. Must match `^[a-z0-9][a-z0-9_-]{0,63}$`. Names are case-sensitive in manifests and normalized to lowercase by the CLI. In-tree adapter names are reserved.

`version`
: Adapter manifest version. Must be semantic version text: `MAJOR.MINOR.PATCH`, optionally with prerelease/build metadata.

`kind`
: Adapter kind. The only valid value is `agent`.

`cmd_pattern`
: TOML inline table or table mapping provider event names to glob patterns. For `PreToolUse`, the glob matches the tool name before Onibi chooses approval routing. Glob syntax is Go `path.Match` style: `*`, `?`, and character classes. Pattern matching is exact after provider normalization; use `*` to match every tool.

`hook_install`
: Array of shell command strings executed by `onibi install-hooks` or `onibi adapters add` when installing this adapter.

`hook_uninstall`
: Array of shell command strings executed when uninstalling this adapter.

`hook_verify`
: Optional array of shell command strings executed when verifying this adapter. Onibi also verifies the recorded manifest provenance hash when the adapter was installed or adopted through Onibi.

`hook_adopt`
: Optional array of shell command strings executed when adopting this adapter. Onibi records the installed manifest path and hash as adapter provenance after these commands complete.

`risk_overrides`
: Table mapping tool name globs to risk levels. Valid levels are `low`, `medium`, `high`, and `critical`. First match wins after sorting keys lexicographically.

`min_onibi_version`
: Minimum Onibi version required to load the adapter. Versions compare as semantic versions. Development versions may load only when their base version satisfies the constraint.

## Compatibility

Onibi refuses to load an adapter when `min_onibi_version` is greater than the running daemon version. The error must include both versions, for example:

```text
adapter "example" requires Onibi >= 9.0.0, running v0.3.0
```

For development builds, Onibi compares the numeric base. For example, `v2-dev` compares as `2.0.0`.

## Command Restrictions

Hook commands are intentionally restricted. A manifest command is rejected if it contains:

- command substitution: `` `...` `` or `$()`
- shell redirection to device files under `/dev`
- process substitution: `<(...)` or `>(...)`
- network fetch commands: `curl`, `wget`, `nc`, `ncat`, `socat`
- privilege escalation commands: `sudo`, `su`, `doas`
- destructive broad deletes: `rm -rf /`, `rm -rf ~`, or `rm -rf $HOME`

Commands may write only under:

- the provider's documented config path
- `$HOME/.config/onibi`
- `$HOME/.local/share/onibi`
- the explicit adapter install directory chosen by the user

## Runtime Contract

Adapter hooks must call `onibi-notify` or `onibi` with stable JSON input. For approval-capable hooks, the adapter must preserve:

- agent name
- event type
- tool name
- original provider payload or a lossless normalized payload
- session ID when the provider exposes it

`PreToolUse` hooks that require blocking approval must wait for Onibi's decision and return the provider-specific allow or deny response.

## Validation

Validation must fail on:

- missing required key
- unknown `kind`
- invalid adapter `name`
- invalid `version` or `min_onibi_version`
- empty `cmd_pattern`
- empty `hook_install`
- empty `hook_uninstall`
- unknown risk level
- restricted command token
- duplicate adapter name

Validation output should include the manifest path and line number when the TOML parser exposes one.

## Appendix: In-Tree Reference Adapters

The in-tree adapters remain the reference implementations for provider-specific hook behavior:

| Adapter | Kind | Reference behavior |
|---|---|---|
| `amp` | `agent` | Installs an Amp TypeScript plugin and reports blocking tool calls. |
| `claude` | `agent` | Installs Claude Code lifecycle hooks, including blocking `PreToolUse`. |
| `codex` | `agent` | Installs Codex hook JSON for lifecycle and blocking tool approvals. |
| `copilot` | `agent` | Installs GitHub Copilot CLI hook JSON for lifecycle events. |
| `gemini` | `agent` | Installs Gemini CLI lifecycle hooks and native `BeforeTool` approval/edit handling. It is non-certified pending authenticated live evidence and a provider-version floor. |
| `goose` | `agent` | Installs Goose lifecycle hooks and native deny-only tool approvals; no edited-input response, so it is non-certified. |
| `opencode` | `agent` | Installs an OpenCode plugin with mutable `tool.execute.before` approvals. It is non-certified pending authenticated live evidence and a provider-version floor. |
| `pi` | `agent` | Installs a Pi Coding Agent extension and reports blocking tool calls. |
