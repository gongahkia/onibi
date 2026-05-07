# TODO 8: Add Fleet-Oriented On-Prem Deployment Shape

## Goal

Add a minimal fleet workflow for multiple VM evidence bundles so Piranesi can support on-prem security team use beyond a single local host assessment.

The proposal talks about infrastructure teams prioritizing across hundreds of findings. Current host mode is single-host local collection and assessment.

## Current State

Relevant files:

- `src/piranesi/cli.py`
- `src/piranesi/host/analyze.py`
- `src/piranesi/host/report.py`
- `src/piranesi/host/models.py`
- `tests/test_host_posture.py`
- `README.md`
- `docs/host-posture.md`

Current workflow:

- Run `piranesi collect` on one VM.
- Run `piranesi assess` on one evidence bundle.
- Review one JSON/Markdown report.

## Desired Workflow

Add commands:

```bash
uv run piranesi fleet assess ./fleet-evidence --output ./fleet-output
uv run piranesi fleet summarize ./fleet-output --format markdown
```

Input layout:

```text
fleet-evidence/
  vm-001/
    host_snapshot.json
  vm-002/
    raw/osquery/...
  vm-003/
    raw/osquery/...
```

Output layout:

```text
fleet-output/
  hosts/
    vm-001/host-report.json
    vm-002/host-report.json
  fleet-report.json
  fleet-report.md
```

If todo1 dashboard exists, optionally add:

```text
fleet-dashboard/
  index.html
```

## Data Model

Add:

```python
class FleetHostSummary(BaseModel):
    target: str
    evidence_path: str
    report_path: str
    posture_score: int
    findings_total: int
    by_severity: dict[str, int]
    top_risks: list[str] = Field(default_factory=list)

class FleetReport(BaseModel):
    schema_version: int = 1
    generated_at: str
    host_count: int
    summary: dict[str, object]
    hosts: list[FleetHostSummary]
```

Keep fleet reporting separate from `HostPostureReport`.

## Assessment Behavior

For each child directory:

- Try `load_host_input(child)`.
- Run `analyze_snapshot()`.
- Apply host suppressions if applicable.
- Write per-host report.
- Continue on individual host failure unless `--fail-fast` is set.
- Record failed hosts in fleet report.

Options:

- `--analysis deterministic|llm|both`
- `--format json|markdown|both`
- `--fail-fast`
- `--fail-severity`
- `--treat-private-as-public`
- `--jobs N`

Use sequential processing first unless parallelism is straightforward. If adding `--jobs`, keep deterministic output ordering.

## Fleet Prioritization

Fleet summary should include:

- Host count.
- Total findings.
- Findings by severity across fleet.
- Worst hosts by score.
- Highest-risk findings across fleet.
- Evidence coverage gaps by host.
- Tool coverage summary.

If todo6 risk scores exist, use them. If not, use severity/confidence ranking.

## Suppressions

Support `.piranesi-ignore` at the fleet root. Decide whether per-host suppressions are supported. Recommended:

- Load root `.piranesi-ignore`.
- Also load child `.piranesi-ignore` if present.
- Apply both.

Document precedence.

## Tests

Create fixtures:

```text
tests/fixtures/fleet/
  vm-clean/
  vm-vulnerable/
  vm-invalid/
```

Tests:

- Fleet assess writes per-host reports.
- Fleet report includes both successful hosts.
- Invalid host is recorded as error without stopping by default.
- `--fail-fast` stops on invalid host.
- `--fail-severity high` exits non-zero when any unsuppressed high finding exists.
- Output ordering is stable.

## Documentation

Update:

- `README.md`
- `docs/host-posture.md`

Add examples for collecting evidence on many hosts manually:

```bash
scp -r vm:/tmp/piranesi-evidence fleet-evidence/vm-name
uv run piranesi fleet assess fleet-evidence --output fleet-output
```

## Acceptance Criteria

- Piranesi can assess a directory of host evidence bundles.
- Fleet report summarizes posture across hosts.
- Individual host failures are captured.
- Existing single-host workflow remains unchanged.
- Tests cover successful, failing, and fail-severity behavior.

## Out Of Scope

- Long-running daemon agent.
- Remote SSH collection.
- Central database.
- Multi-user dashboard authentication.
- Ticket sync.

## Validation Commands

```bash
uv run pytest tests/test_host_posture.py tests/test_cli.py
uv run piranesi fleet assess tests/fixtures/fleet --output /tmp/piranesi-fleet-out
```

