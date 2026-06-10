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

## Safe Mode Wrapper

`onibi safe <agent> --workspace <path> [--prompt "..."] [--name <name>]` launches an agent session with `safeMode = true`. Safe mode is session-scoped and does not mutate `policies.toml`.

Safe sessions auto-allow only conservative read-only Bash basics: `pwd`, `ls`, `cat`, `head`, `tail`, `grep`, `rg`, `sed -n`, non-mutating `find`, and `git status`, `git diff`, `git log`, `git show`. Commands with shell chaining, pipes, redirects, command substitution, backticks, or destructive `find` actions fall back to manual approval. Non-Bash tools also fall back to manual approval.
