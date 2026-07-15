# Budgets

Budget policies let Onibi warn, interrupt, or kill a hosted session when token usage crosses a project limit.

Policy file path:

```bash
<project>/.onibi/budget.toml
```

The current usage source is Claude Code JSONL transcripts only. Codex and Pi
are certified for fleet-wide action delivery but report
`interactive token telemetry unavailable` in their adapter contracts, so their
session token limits are not evaluated locally. Other agents are out of scope.

## Policy Model

```toml
[global]
max_tokens_per_day = 100000

[session]
max_tokens = 25000
on_overrun = "interrupt"
```

Fields:

| field | values |
|---|---|
| `global.max_tokens_per_day` | total token limit for the UTC day |
| `session.max_tokens` | token limit for one hosted session |
| `session.on_overrun` | `interrupt`, `kill`, or `warn` |

Unset or zero token limits are disabled. If `session.on_overrun` is omitted, Onibi uses `interrupt`.

## Examples

Warn only:

```toml
[global]
max_tokens_per_day = 200000

[session]
max_tokens = 50000
on_overrun = "warn"
```

Warn-only mode records the overrun and shows budget state, but leaves the PTY running.

Interrupt runaway sessions:

```toml
[global]
max_tokens_per_day = 100000

[session]
max_tokens = 25000
on_overrun = "interrupt"
```

Interrupt sends Ctrl-C to the session when a limit is exceeded.

Kill hard overrun:

```toml
[global]
max_tokens_per_day = 50000

[session]
max_tokens = 10000
on_overrun = "kill"
```

Kill closes the hosted session when a limit is exceeded. Use this only for projects where stopping work is safer than allowing extra spend.

Session-only cap:

```toml
[session]
max_tokens = 15000
on_overrun = "interrupt"
```

Daily-only cap:

```toml
[global]
max_tokens_per_day = 75000
```

## CLI

```bash
onibi budget show
onibi budget show --json
```

Human output shows daily usage plus per-session rows. JSON output is machine-readable and includes token totals, USD estimates where model pricing is known, and remaining capacity when a limit is configured.

Cost estimates use the Claude prices listed in `docs/pricing.md`. They do not model prompt caching, batch discounts, priority tier, regional pricing, marketplace markups, or non-Claude agents.

## Runtime Behavior

Onibi updates budget usage when a Claude Code turn completes and the hook reports a provider session id. The daemon reads the matching Claude JSONL transcript, records the incremental token usage, and updates the daily aggregate.

Approval prompts also check budget state before auto-approve rules. If the next approved tool call is likely to cross a budget limit, Onibi forces the approval card to stay manual.

## Enrolled Fleet Hosts

Each enrolled host includes a signed, token-only UTC-day budget snapshot in its
fleet heartbeat. The hub stores it encrypted with the host record, totals all
active-host snapshots, and applies the lowest positive global limit reported by
the fleet. A same-day host report cannot lower its already recorded token total.
When that aggregate is over limit, it sends persisted `interrupt` or
`kill` controls to every active certified session, including Codex and Pi
sessions whose local token telemetry is unavailable.

Remote control results are persisted as `succeeded`, `failed`, or `timed_out`
and audited without a tool payload. A disconnected host keeps a pending control
until it reconnects or the command expires. Run `make fleet-budget-smoke` to
exercise the signed report, cross-host total, control delivery, and result
acknowledgement path.

## Caveats

- Claude Code JSONL is the only measured budget source today.
- Codex and Pi global-overrun controls are enforced, but their local session
  limits are explicitly non-enforcing until reliable interactive token telemetry
  is available.
- Costs are estimates based on the model id in the transcript.
- Daily reset is UTC.
- Existing sessions without a `.onibi/budget.toml` use no token limit and default overrun action `interrupt`.
- Trust policies do not bypass budget warnings.
