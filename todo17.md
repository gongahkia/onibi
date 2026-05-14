# TODO 17: Add Multi-Platform Evidence Support

## Goal

Expand Piranesi beyond Debian/Ubuntu hosts while preserving the same canonical
snapshot and report model. Wide adoption requires coverage for common production
Linux families and, eventually, Windows/macOS posture inputs.

## Current State

The host workflow is explicitly Debian/Ubuntu-oriented. It uses osquery plus
Debian-specific package/update evidence such as `deb_packages` and `apt list`.

## Desired Scope

Add support tiers:

| Platform | Target Tier |
| --- | --- |
| Ubuntu/Debian | Stable Alpha |
| RHEL/CentOS/Rocky/Alma/Fedora | Alpha |
| Amazon Linux | Alpha |
| Alpine | Experimental |
| macOS | Experimental read-only posture |
| Windows | Future design only |

## Desired Behavior

Collection should detect platform and choose relevant queries/commands:

```bash
piranesi collect --output piranesi-evidence
piranesi doctor --host
```

Assessment should normalize platform-specific evidence into the same host report
shape wherever possible.

## Data Model

Add richer package manager provenance:

```python
class HostPackage(BaseModel):
    name: str
    version: str
    source: str
    package_manager: Literal["deb", "rpm", "apk", "brew", "winget", "unknown"] | None
```

Add platform capabilities to host metadata:

```json
{
  "platform_family": "rhel",
  "package_manager": "rpm",
  "supported_checks": ["packages", "listeners", "users", "services"]
}
```

## Platform-Specific Evidence

Support:

- RPM package inventory.
- `dnf` / `yum` security updates.
- `firewalld` status.
- SELinux state.
- Alpine `apk` packages and updates.
- macOS launch services, users, listeners, and package manager inventory where safe.

## Tests

Fixtures:

```text
tests/fixtures/host/rhel-vulnerable/
tests/fixtures/host/alpine-minimal/
tests/fixtures/host/amazon-linux/
```

Tests should cover:

- Platform detection.
- Package normalization.
- Update evidence parsing.
- Firewall helper selection.
- Unsupported checks becoming health warnings, not false findings.

## Documentation

Update:

- `README.md`
- `docs/host-posture.md`
- `docs/capabilities.md`

Add a support matrix with exact coverage by OS family.

## Acceptance Criteria

- Piranesi can assess at least one non-Debian Linux fixture.
- Platform-specific unsupported checks are explicit.
- Existing Debian/Ubuntu behavior remains unchanged.
- Reports clearly state platform confidence and evidence coverage.

## Out Of Scope

- Full Windows support.
- Mobile OS support.
- Kernel exploit detection.

## Validation Commands

```bash
uv run pytest tests/test_host_platforms.py tests/test_host_posture.py
uv run piranesi assess tests/fixtures/host/rhel-vulnerable --output /tmp/piranesi-rhel
```

