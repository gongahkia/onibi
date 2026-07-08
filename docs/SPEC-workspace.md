# Onibi Workspace Spec

Version: 1

This document freezes the committed workspace schema consumed from:

```text
<project>/.onibi/workspace.toml
```

The file is project-local, portable, and safe to commit. User-private state, machine paths outside the repository, owner identity, tokens, and SSH keys belong in the global workspace index under `~/.onibi/workspaces/` and must not be stored here.

## Minimal File

```toml
schema_version = 1
name = "example"
default_agent = "claude"
```

## Full Shape

```toml
schema_version = 1
name = "example"
default_agent = "claude"

[trust]
policy_file = "trust.toml"

[[trust.rule]]
effect = "auto_approve"
expires = "never"
[trust.rule.match]
tool = "Read"
path = "docs/**"
agent = "claude"

[budget.global]
max_tokens_per_day = 100000

[budget.session]
max_tokens = 25000
on_overrun = "interrupt"

[transports]
default = "tailscale"
web = ["lan", "tailscale"]
chat = ["telegram"]
notify = ["pushover", "ntfy"]

[hooks]
auto_install = ["claude", "codex", "shell:zsh"]
```

## Top-Level Keys

`schema_version`
: Required integer. Current value is `1`. Unknown future versions must be rejected.

`name`
: Required stable workspace id. Must match `^[a-z0-9][a-z0-9_-]{0,63}$`.

`default_agent`
: Optional agent id used when `onibi up` starts in the workspace without an explicit agent. Valid values are installed agent adapter ids such as `claude`, `codex`, `gemini`, `goose`, `opencode`, `amp`, `copilot`, `pi`, or a third-party adapter id.

Unknown top-level keys are invalid.

## Trust

`[trust]` configures project trust defaults.

`trust.policy_file`
: Optional path to a trust policy TOML file, relative to `.onibi/workspace.toml`. The usual value is `trust.toml`.

`[[trust.rule]]`
: Optional inline trust rules using the same rule model as `docs/trust-policies.md`.

Use either `trust.policy_file`, inline `trust.rule` entries, or both. When both are present, file rules load first and inline rules append after them.

## Budget

`[budget.global]` and `[budget.session]` use the same field names as `docs/budgets.md`.

Supported fields:

| field | type | values |
|---|---|---|
| `budget.global.max_tokens_per_day` | integer | `> 0`, omitted disables daily cap |
| `budget.session.max_tokens` | integer | `> 0`, omitted disables session cap |
| `budget.session.on_overrun` | string | `interrupt`, `kill`, or `warn` |

If `budget.session.on_overrun` is omitted, Onibi uses `interrupt`.

## Transports

`[transports]` declares workspace-preferred control surfaces.

`transports.default`
: Optional default transport mode. Valid values match `transport.mode`: `lan`, `tailscale`, `wireguard`, `zerotier`, `cloudflare-quick`, `cloudflare-named`, `ngrok`, `telegram`, `matrix`, `slack`, `discord`, `zulip`, `pushover`, `ntfy`, `gotify`, `apns`, or `auto`.

`transports.web`
: Optional ordered allowlist for browser cockpit modes.

`transports.chat`
: Optional ordered allowlist for chat-control modes.

`transports.notify`
: Optional ordered allowlist for notify-only modes.

Transport lists are preferences, not secrets. Provider tokens remain environment variables, OS keychain items, or user-private config.

## Hooks

`[hooks]` declares adapter hooks that may be auto-installed for this workspace.

`hooks.auto_install`
: Optional array of adapter ids. Agent adapters use their adapter id, for example `claude`. Shell hooks use `shell:<name>`, for example `shell:zsh`.

Auto-install is idempotent. It must verify existing hook hashes before writing and must not silently overwrite non-managed hooks.

## Path Rules

All relative paths are resolved from the directory containing `.onibi/workspace.toml`.

Paths must stay inside the repository unless a future schema field explicitly allows a private external path. Portable workspace exports must omit machine-local absolute paths.

## Validation

Validation must fail on:

- missing or unsupported `schema_version`
- missing or invalid `name`
- unknown top-level table or key
- unknown `default_agent` when adapter registry is available
- unknown transport mode
- unknown `budget.session.on_overrun`
- invalid trust rule effect, expiry, or empty matcher
- `hooks.auto_install` item with unknown adapter id
- relative path escaping the workspace root

Validation errors should include the file path and key path.

## Global Index Boundary

The project workspace file is not the global workspace index.

The global index stores user-private bindings such as:

- absolute checkout path
- last seen timestamp
- SSH key reference
- default transport override

Those values belong in `~/.onibi/workspaces/<name>.toml` and the SQLite `workspaces` table defined by the next implementation phase.
