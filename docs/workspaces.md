# Workspaces

Onibi workspaces separate team-shareable project defaults from user-private machine bindings.

Use workspaces when a repo needs repeatable trust, budget, transport, hook, or agent defaults, but each developer still needs their own checkout path and private SSH/key references.

## Files

Committed project defaults live in:

```text
<repo>/.onibi/workspace.toml
<repo>/.onibi/trust.toml
<repo>/.onibi/budget.toml
```

User-private bindings live outside the repo:

```text
~/.onibi/workspaces/<name>.toml
```

Onibi also mirrors workspace bindings into its SQLite store. The SQLite row keeps the workspace name, encrypted absolute path, SSH key reference, and last-seen timestamp.

Do not commit `~/.onibi/`, Onibi state directories, SQLite files, keychain items, `.env` fallback files, provider tokens, or absolute machine paths.

## Team Share Pattern

Project owner:

```bash
mkdir -p .onibi
$EDITOR .onibi/workspace.toml
$EDITOR .onibi/trust.toml
$EDITOR .onibi/budget.toml
git add .onibi/workspace.toml .onibi/trust.toml .onibi/budget.toml
git commit -m "add Onibi workspace defaults"
onibi workspace add my-repo "$(pwd)" --use
```

Teammate:

```bash
git pull
onibi workspace use ./.onibi
onibi up
```

`onibi workspace use ./.onibi` reads the committed workspace file, creates or updates the private binding for the local checkout path, and sets it as the default workspace for `onibi up`.

## Minimal Workspace

```toml
schema_version = 1
name = "my-repo"
default_agent = "claude"
```

## With Trust, Budget, And Transport Defaults

```toml
schema_version = 1
name = "my-repo"
default_agent = "claude"

[trust]
policy_file = "trust.toml"

[budget.global]
max_tokens_per_day = 100000

[budget.session]
max_tokens = 25000
on_overrun = "interrupt"

[transports]
default = "tailscale"
web = ["lan", "tailscale"]
chat = ["telegram", "signal"]
notify = ["pushover", "ntfy", "sms", "email"]
```

Trust and budget files use the same formats as `docs/trust-policies.md` and `docs/budgets.md`.

## Private Index

The private index file records user-local data:

```toml
path = "/Users/alice/src/my-repo"
last_seen = 2026-06-30T00:00:00Z
ssh_key = "keychain:onibi/my-repo"
default_transport = "tailscale"
```

`path` is the local checkout path. It is encrypted in SQLite and should not appear in committed files.

`ssh_key` is a reference, not a raw private key. It should point at Keychain, an SSH agent identity, or another local secret handle.

`default_transport` is a private override. Prefer committed `[transports]` defaults when the whole team should use the same mode.

## Commands

```bash
onibi workspace add <name> [path]
onibi workspace list
onibi workspace use <name|path>
onibi workspace remove <name>
onibi workspace export <name> <path>
onibi workspace import <path>
```

Useful flags:

```bash
onibi workspace add my-repo . --default-transport tailscale --ssh-key keychain:onibi/my-repo --use
onibi workspace import . --use
```

`workspace export` writes a portable `.onibi/` bundle containing `workspace.toml`, `trust.toml`, and `budget.toml`. It omits private index fields such as local paths and SSH key references.

`workspace import` and `workspace use <path>` accept a repo root, a `.onibi` directory, or a direct `workspace.toml` path.

## Resolution

When `onibi up` starts, Onibi resolves workspace context in this order:

1. The current `--cwd` or process working directory, walking upward for `.onibi/workspace.toml`.
2. The default workspace selected by `onibi workspace use <name|path>`.
3. No workspace; sessions are shown without workspace filtering.

CLI flags still win over workspace defaults. For example, `onibi up --transport lan` overrides `[transports].default`.

## Committed Versus Private

| committed `.onibi/` | private `~/.onibi/workspaces/` |
|---|---|
| workspace name | absolute checkout path |
| default agent | SSH key reference |
| trust policy defaults | last seen timestamp |
| budget limits | local transport override |
| transport preferences | user-specific machine binding |
| hook auto-install preferences | never committed |

Committed defaults should be portable and reviewable. Private bindings should be local, machine-specific, and safe to delete and recreate.
