# TODO 10: Add Structured CIS/NIST Control Mapping For Host Findings

## Goal

Add comprehensive, structured control mapping for host posture findings so Piranesi can validate findings against security baselines such as CIS and NIST instead of emitting mostly free-text control references.

The proposal says findings should be validated against CVE databases and security baselines such as CIS and NIST. Current host findings have a few string `control_refs`, but no structured baseline matrix.

## Current State

Relevant files:

- `src/piranesi/host/analyze.py`
- `src/piranesi/host/models.py`
- `src/piranesi/host/report.py`
- `src/piranesi/legal/frameworks.py`
- `src/piranesi/legal/rules/*`
- `rules/*.toml`
- `tests/test_host_posture.py`
- `tests/test_legal/*`
- `docs/host-posture.md`

Current `HostFinding`:

```python
control_refs: list[str] = Field(default_factory=list)
```

Examples are broad strings like:

- `CIS Ubuntu Linux: Disable SSH root login`
- `CIS Ubuntu Linux: Disable SSH password authentication where possible`

## Desired Model

Add structured controls:

```python
class HostControlRef(BaseModel):
    framework: str
    version: str | None = None
    control_id: str
    title: str
    mapping_confidence: float = Field(ge=0.0, le=1.0)
    rationale: str | None = None
```

Update `HostFinding`:

```python
control_refs: list[str] = Field(default_factory=list) # keep for compatibility
structured_control_refs: list[HostControlRef] = Field(default_factory=list)
```

Alternatively, replace `control_refs` only if all serializers/tests are updated. Prefer additive compatibility.

## Baseline Registry

Create a host control mapping registry:

```text
src/piranesi/host/controls.py
```

Example mapping:

```python
HOST_CONTROL_MAPPINGS = {
    "host.ssh.permit_root_login": [
        HostControlRef(
            framework="CIS Ubuntu Linux",
            version="22.04",
            control_id="5.2.x",
            title="Ensure SSH root login is disabled",
            mapping_confidence=0.85,
        ),
        HostControlRef(
            framework="NIST CSF",
            version="2.0",
            control_id="PR.AA",
            title="Identity Management, Authentication and Access Control",
            mapping_confidence=0.55,
        ),
    ],
}
```

Use accurate control IDs where known. If exact IDs are not known, use conservative framework family mappings with lower confidence and document that exact profile mapping is incomplete.

Do not invent precise CIS/NIST IDs without verification. It is better to map to broad categories with lower confidence than to ship incorrect exact IDs.

## Initial Mapping Scope

Map existing host rule IDs:

- `host.cve.trivy`
- `host.listener.high_risk_service`
- `host.listener.ssh_public`
- `host.ssh.permit_root_login`
- `host.ssh.password_authentication`
- `host.ssh.permit_empty_passwords`
- `host.firewall.inactive_public_services`
- `host.updates.security_pending`
- `host.updates.unattended_upgrades_missing`
- `host.sysctl.net.ipv4.ip_forward`
- `host.sysctl.net.ipv6.conf.all.forwarding`
- `host.sysctl.kernel.unprivileged_bpf_disabled`
- `host.sysctl.kernel.kptr_restrict`
- `host.identity.privileged_user`
- `host.coverage.missing_evidence`
- `host.coverage.missing_trivy`

Framework families:

- CIS Ubuntu Linux, exact where practical.
- NIST CSF 2.0 categories.
- NIST SP 800-53 families if local legal framework patterns already support them.

If todo2 lands, map Lynis/OpenSCAP results by their own control IDs too.

## Report Changes

JSON:

- Include `structured_control_refs`.

Markdown:

- Render controls with framework, control ID, title, and confidence.
- Keep old string controls for compatibility during migration.

Dashboard/PDF:

- If todo1 has landed, render structured controls.

## Compliance Summary

Add a host control summary to `HostPostureReport` or metadata:

```python
control_summary: dict[str, object]
```

Example:

```json
{
  "frameworks": {
    "CIS Ubuntu Linux": {
      "mapped_findings": 4,
      "highest_severity": "high"
    },
    "NIST CSF": {
      "mapped_findings": 7,
      "highest_severity": "high"
    }
  }
}
```

## Tests

Add tests for:

- Each existing deterministic host rule ID has at least one structured control mapping or an explicit no-map reason.
- SSH findings include CIS mappings.
- Firewall/listener findings include NIST CSF mappings.
- Coverage findings map to governance/visibility controls or are explicitly excluded.
- Markdown renders structured controls.
- JSON schema remains compatible with existing fixtures.

## Documentation

Update:

- `docs/host-posture.md`
- `README.md`

Add a "Control Mapping" section that explains:

- Supported frameworks.
- Exact vs broad mappings.
- Mapping confidence.
- How Lynis/OpenSCAP mappings interact if todo2 exists.

## Acceptance Criteria

- Host findings include structured control mappings.
- Control mappings are deterministic and tested.
- Reports summarize mapped frameworks.
- No unsupported exact control IDs are invented without evidence.
- Existing host report consumers still work.

## Out Of Scope

- Full legal compliance attestation.
- Complete CIS benchmark coverage.
- Automatic pass/fail compliance certification.
- Vendor-specific profile downloads.

## Validation Commands

```bash
uv run pytest tests/test_host_posture.py tests/test_legal
uv run piranesi assess tests/fixtures/host/debian-vulnerable --output /tmp/piranesi-controls-out --format both
```

