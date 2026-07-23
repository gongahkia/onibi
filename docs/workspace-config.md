# Workspace Configuration

This is a portable project metadata file; it does not select a default project, alter daemon startup, bind a host, or grant another user access.

```bash
onibi workspace init . --name example --agent claude
onibi workspace validate .
```

`init` creates `.onibi/workspace.toml` and refuses to overwrite an existing file. `validate` only parses and validates the supplied file; it does not search parent directories or start a session.

## Portable schema

```toml
schema_version = 1
name = "example"
default_agent = "claude"
```

`name` is a stable lowercase identifier matching `^[a-z0-9][a-z0-9_-]{0,63}$`. `default_agent` is optional and must name a locally known Onibi adapter. It is a project preference only: Onibi does not automatically launch it.

Unknown keys and tables fail closed. The file cannot contain host paths, owner identities, browser state, transport credentials, tokens, secret references, SSH keys, policy rules, budgets, auto-install actions, or workspace bindings. Keep those values in local user-controlled configuration or native provider configuration, never in a committed project file.

## Current boundary

The workspace file is safe to commit because it has no personal-host identity or secret material. It does not create a SQLite workspace record, sync to another machine, infer a tenant, change pairing, or affect `onibi start` until a separately reviewed future integration exists.

Before any broader integration, Onibi requires portable-schema, migration/downgrade, path/secret rejection, default-owner, and integration tests, plus recorded product/security review.
