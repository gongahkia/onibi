# TODO 3: Implement Adaptive Host Probing

## Goal

Add a staged adaptive probing workflow where initial host findings can request follow-up evidence collection and a second assessment pass can refine findings, confidence, and remediation.

The proposal describes adaptive probing as feeding initial findings back into the analysis system to generate follow-up data collection queries. Current Piranesi performs a single-pass assessment over an existing snapshot.

## Current State

Relevant files:

- `src/piranesi/cli.py`
- `src/piranesi/host/collect.py`
- `src/piranesi/host/ingest.py`
- `src/piranesi/host/analyze.py`
- `src/piranesi/host/models.py`
- `tests/test_host_posture.py`
- `docs/host-posture.md`

Current flow:

1. `piranesi collect` writes a raw bundle and `host_snapshot.json`.
2. `piranesi assess` loads the snapshot/bundle.
3. `analyze_snapshot()` emits deterministic and optional LLM findings.

There is no mechanism for `analyze_snapshot()` to say, for example:

- Redis is public, collect Redis config evidence.
- SSH is public and password auth is enabled, collect failed login counts.
- Firewall evidence is missing, run a specific fallback command.

## Desired CLI

Add a new mode rather than silently changing `assess`:

```bash
uv run piranesi probe piranesi-evidence --output piranesi-probe-plan.json
uv run piranesi collect-followup piranesi-probe-plan.json --output piranesi-evidence-followup
uv run piranesi assess piranesi-evidence-followup --output piranesi-output
```

Alternatively, add:

```bash
uv run piranesi assess piranesi-evidence --adaptive --output piranesi-output
```

If implementing both is too much, prefer the explicit `probe` plus `collect-followup` workflow because it is easier to audit and safer for host environments.

## Data Model

Add models similar to:

```python
class FollowupProbe(BaseModel):
    id: str
    reason: str
    finding_ids: list[str] = Field(default_factory=list)
    capability: str
    command: list[str] | None = None
    osquery: str | None = None
    output_name: str
    required: bool = False
    risk: Literal["read_only", "potentially_sensitive", "expensive"] = "read_only"
    redaction_hints: list[str] = Field(default_factory=list)

class ProbePlan(BaseModel):
    schema_version: int = 1
    generated_at: str
    target: str
    probes: list[FollowupProbe]
```

Keep probe plans JSON-serializable and reviewable.

## Probe Generation

Implement deterministic probe suggestions first.

Examples:

- Public SSH plus password auth:
  - collect `last`, `lastb` if available
  - collect auth log summary if readable
  - collect `sshd -T` if absent
- Public Redis:
  - collect process cmdline and service unit detail
  - collect Redis bind/protected-mode config if config path is discoverable
- Public database ports:
  - collect service unit detail
  - collect local firewall rules if missing
- Missing firewall evidence:
  - suggest alternate firewall helpers.
- Privileged user:
  - collect sudoers entries and group memberships if absent.

Then optionally allow LLM-generated probes, but only from a constrained allowlist of safe probe types. The LLM should never be able to execute arbitrary commands.

## Execution Safety

Do not execute arbitrary commands from untrusted probe plans.

Implement an allowlist:

- osquery queries from known templates
- command templates defined in code
- static command arguments only

Reject probe plans with unknown command IDs or free-form shell strings.

No shell invocation. Use argument arrays with `subprocess.run`.

## Bundle Merge

Add logic to merge follow-up evidence into an existing bundle or create a new bundle containing:

```text
piranesi-evidence-followup/
  host_snapshot.json
  collection-manifest.json
  raw/
    osquery/
    commands/
    followup/
      probe-plan.json
      probe-results.json
```

Re-normalize the snapshot after follow-up collection.

## Reporting

Reports should include:

- Whether adaptive probing was used.
- Probe count.
- Probe failures/timeouts.
- Findings whose confidence changed due to follow-up evidence.

Add fields to `HostPostureReport` or metadata as needed.

## Tests

Add tests for:

- Probe plan generation from fixture `debian-vulnerable`.
- No probes for clean/minimal fixture where no applicable findings exist.
- Probe executor rejects unknown commands.
- Probe executor writes manifest entries.
- Follow-up evidence affects a finding or collection health in a deterministic fixture.
- `assess --adaptive` or explicit CLI command writes expected artifacts.

## Acceptance Criteria

- A user can generate a safe follow-up probe plan from an initial evidence bundle.
- A user can execute allowed follow-up probes locally.
- Follow-up evidence is included in the canonical snapshot or raw evidence.
- Reports show adaptive probing metadata.
- No arbitrary shell execution is introduced.

## Out Of Scope

- Remote agent orchestration.
- Automated remediation.
- Unrestricted LLM command generation.
- Continuous monitoring.

## Validation Commands

```bash
uv run pytest tests/test_host_posture.py tests/test_cli.py
uv run piranesi probe tests/fixtures/host/debian-vulnerable --output /tmp/probe-plan.json
```

