# Approval Policies

Onibi can apply local trust rules before an approval reaches the desktop or phone. Rules live at `~/.config/onibi/policies.toml` and are evaluated in order; the first matching rule wins.

```toml
[[policy]]
name = "safe git reads"
decision = "auto-allow"

[policy.match]
tool = "Bash"
command = "^git (status|diff|log|show)"

[[policy]]
name = "destructive shell commands"
decision = "always-ask"
require_edit = true

[policy.match]
tool = "Bash"
command = "^(rm|sudo rm|chmod -R|chown -R) "
```

Decisions:

- `auto-allow` returns an allow decision immediately and still writes the approval into the audit log.
- `always-ask` keeps the normal blocking approval flow.
- `always-deny` returns a deny decision immediately and writes the denied approval into the audit log.

Matchers:

- `agent` matches the adapter/agent id exactly.
- `tool` matches the tool name exactly.
- `command` is a Rust regular expression tested against `input.command`.
- `cwd_prefix` matches approvals whose working directory starts with the prefix.

`require_edit = true` prevents auto-allow for a matching rule. If a rule combines `require_edit = true` with `decision = "auto-allow"`, Onibi downgrades it to `always-ask`.

Validate config and policy files with:

```sh
onibi config validate
onibi config validate --json
```

The desktop Settings `config.toml` page also shows the policy path, rule count, and validation errors when the approval daemon is reachable.
