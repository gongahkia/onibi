# TODO 12: Add Agentless Remote Collection Over SSH

## Goal

Enable security teams to collect evidence from many Linux hosts without manually
logging into each VM or installing a long-running agent.

Piranesi should remain local-first and read-only by default, but wide adoption
requires a practical bridge from single-host collection to small and mid-size
fleet workflows.

## Current State

Current collection runs locally on the assessed host:

```bash
piranesi collect --output piranesi-evidence
```

Fleet assessment in `todo8.md` assumes evidence directories already exist, but it
does not define how teams gather those directories at scale.

## Desired CLI

Add:

```bash
piranesi remote collect --host vm-001 --output fleet-evidence/vm-001
piranesi remote collect --hosts hosts.txt --output fleet-evidence
piranesi remote doctor --hosts hosts.txt
```

Optional flags:

- `--user`
- `--identity-file`
- `--port`
- `--sudo-mode never|prompt|passwordless`
- `--jobs N`
- `--timeout`
- `--no-trivy`
- `--dry-run`

## Safety Requirements

- No shell string interpolation.
- Use explicit command arrays where possible.
- Record every remote command in the manifest.
- Never require a persistent agent.
- Never mutate host state.
- Require explicit opt-in for sudo-dependent evidence.
- Bound command output and redact sensitive stderr where appropriate.

## Implementation Notes

Prefer an SSH transport abstraction:

```python
class RemoteHostTarget(BaseModel):
    host: str
    user: str | None = None
    port: int = 22
    identity_file: str | None = None

class RemoteCollectionResult(BaseModel):
    target: RemoteHostTarget
    output_dir: str
    status: Literal["ok", "partial", "failed"]
    manifest_path: str | None = None
    error: str | None = None
```

Reuse the local collector command definitions where possible, but execute them
through the remote transport and store the same raw bundle layout.

## Reporting

Remote collection should produce:

```text
fleet-evidence/
  vm-001/
    host_snapshot.json
    collection-manifest.json
    raw/
```

For multi-host collection, write:

```text
remote-collection-summary.json
remote-collection-summary.md
```

## Tests

Add tests with a fake SSH transport:

- Single remote host collection writes expected layout.
- Multi-host collection continues after one host fails.
- `--dry-run` prints planned commands and writes no evidence.
- Unknown sudo mode fails validation.
- Command execution is not shell-string based.

## Acceptance Criteria

- Piranesi can collect evidence from one or many SSH-accessible Linux hosts.
- Output is compatible with single-host and fleet assessment.
- Partial failures are explicit and auditable.
- The default mode stays read-only and non-mutating.

## Out Of Scope

- Windows Remote Management.
- Kubernetes exec collection.
- Long-running endpoint agent.
- Credential vault integrations.

## Validation Commands

```bash
uv run pytest tests/test_remote_collect.py tests/test_host_posture.py
uv run piranesi remote collect --host localhost --output /tmp/piranesi-remote --no-trivy
```

