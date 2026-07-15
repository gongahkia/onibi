# Trust Policies

Trust policies let Onibi decide some approval requests without showing an approval card. Use them for narrow, repeatable tool calls only.

Policy file path:

```bash
<project>/.onibi/trust.toml
```

Runtime overrides live only in the running daemon. The approval overlay and `onibi trust add` create runtime rules; `onibi trust persist` is the explicit step that writes runtime rules into `.onibi/trust.toml`.

## Rule Model

Rules are evaluated in order. First match wins. Runtime rules precede file rules; newer runtime rules precede older runtime rules. File rule ids are their fixed TOML positions, including expired rules, so a later rule keeps the same id when earlier rules expire.

```toml
[[rule]]
effect = "auto_approve"
expires = "never"

[rule.match]
tool = "Read"
path = "docs/**"
agent = "claude"
```

Fields:

| field | values |
|---|---|
| `effect` | `auto_approve`, `always_prompt`, `deny` |
| `expires` | `never` or a Go duration such as `5m`, `1h` |
| `match.tool` | tool glob, e.g. `Read`, `Edit`, `mcp__*` |
| `match.path` | path glob relative to the project root, e.g. `src/**` |
| `match.agent` | exact agent name, e.g. `claude`, `codex` |

At least one match field is required. Tool and path use doublestar glob matching. Agent matching is exact.

## Examples

Read-only auto-approve:

```toml
[[rule]]
effect = "auto_approve"
expires = "never"
[rule.match]
tool = "Read"
path = "docs/**"

[[rule]]
effect = "auto_approve"
expires = "never"
[rule.match]
tool = "Glob"
path = "docs/**"

[[rule]]
effect = "auto_approve"
expires = "never"
[rule.match]
tool = "Grep"
path = "docs/**"
```

Dangerous paths always prompt before broader rules:

```toml
[[rule]]
effect = "always_prompt"
expires = "never"
[rule.match]
path = ".env*"

[[rule]]
effect = "always_prompt"
expires = "never"
[rule.match]
path = "**/*secret*"

[[rule]]
effect = "auto_approve"
expires = "never"
[rule.match]
tool = "Edit"
path = "docs/**"
```

Time-windowed runtime override:

```bash
onibi trust add --tool Edit --path 'docs/**' --agent claude --expires 5m
onibi trust list
```

Do not run `onibi trust persist` for temporary rules you expect to decay.

Per-agent rule:

```toml
[[rule]]
effect = "auto_approve"
expires = "never"
[rule.match]
tool = "Edit"
path = "docs/**"
agent = "claude"
```

Deny a tool outright:

```toml
[[rule]]
effect = "deny"
expires = "never"
[rule.match]
tool = "Bash"
path = "**"
```

## CLI

```bash
onibi trust list
onibi trust list --json
onibi trust add --tool Edit --path 'docs/**' --expires 5m
onibi trust remove runtime:<id>
onibi trust reload
onibi trust persist
```

Use `--root <project>` when running outside the project directory.

Rule ids:

- `file:N` identifies the Nth disk rule in `.onibi/trust.toml`.
- `runtime:<id>` identifies an ephemeral daemon rule.

`onibi trust persist` appends current runtime rules to `.onibi/trust.toml` and converts them into disk rules. Review `onibi trust list` before persisting.

## Evaluation and Recovery

The evaluator returns a structured trace for every considered rule. Each trace item carries the stable rule id, source, effect, match fields, and one outcome: `expired`, `tool_mismatch`, `path_mismatch`, `agent_mismatch`, or `matched`.

File rules with a duration expire relative to the policy file's last modification time. Restarting the daemon does not reset that timer. Persisting a runtime rule preserves its remaining duration. Policy writes use a synced temporary file followed by rename; an invalid reload retains the last valid in-memory policy and emits a reload error.

## Auditing

Every trust auto-approve writes an audit row with:

- action `trust.auto_approve`
- rule id, e.g. `rule=file:1` or `rule=runtime:abc123`
- matched path, e.g. `path=docs/readme.md`
- approval id
- `trace_json`: structured rule evaluation trace without raw tool input

Inspect with:

```bash
onibi log
onibi log --json
```

## Threat Model

The paired browser can create short-lived runtime rules from an approval card, but it cannot persist rules to disk. Persistence requires typing `onibi trust persist` in a local CLI running as the same OS user. This prevents a drive-by web session from making durable policy changes by itself.

Trust policies do not make untrusted agents safe. They only reduce repeated prompts for tool/path/agent combinations you already accept. Keep broad `auto_approve` rules out of writable source directories unless you are comfortable with that agent modifying them without a prompt.

Good defaults:

- Prefer path-scoped rules over tool-only rules.
- Put `always_prompt` and `deny` rules before broad auto-approve rules.
- Use runtime rules for short bursts of repetitive work.
- Persist only rules you would be willing to review in code.
